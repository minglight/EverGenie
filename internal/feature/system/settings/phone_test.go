package settings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/host"
	"github.com/minglight/evergenie/sdk"
)

// 假的 tailscale 執行檔（真的去跑它，不碰這台的 Tailscale）：已登入；serve --bg 把看板掛上去；每次呼叫記進 calls.log。
// state/funnel 存在時 serve status 回「看板被 Funnel 開出去」，funnel … off 把它拿掉。
const phoneFakeTailscale = `#!/bin/sh
d="$(dirname "$0")/state"
echo "$*" >>"$d/calls.log"
hp="mac.example.ts.net:443"
case "$1 $2" in
"status --json") echo '{"BackendState":"Running","CertDomains":["mac.example.ts.net"],"Self":{"DNSName":"mac.example.ts.net.","UserID":7},"User":{"7":{"LoginName":"owner@example.com"}}}' ;;
"serve status")
  if [ -f "$d/funnel" ]; then printf '{"Web":{"%s":{"Handlers":{"/":{"Proxy":"%s"}}}},"AllowFunnel":{"%s":true}}\n' "$hp" "$(cat "$d/funnel")" "$hp"
  elif [ -f "$d/serve.json" ]; then cat "$d/serve.json"; else echo "{}"; fi ;;
"serve get-config") echo '{"version":"0.0.1"}' ;;
"serve --bg") rm -f "$d/funnel"; printf '{"Web":{"%s":{"Handlers":{"/":{"Proxy":"%s"}}}}}\n' "$hp" "$4" >"$d/serve.json" ;;
"funnel --https=443") rm -f "$d/funnel" ;;
*) echo "假的 tailscale 不認得：$*"; exit 1 ;;
esac
`

func phoneFake(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "tailscale")
	if err := os.WriteFile(bin, []byte(phoneFakeTailscale), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(host.TailscaleBinEnv, bin)
	return state
}

func phoneCalls(state string) string {
	b, _ := os.ReadFile(filepath.Join(state, "calls.log"))
	return string(b)
}

func getPhone(t *testing.T, h http.Handler, local bool) sdk.WebPhone {
	t.Helper()
	code, raw := post(t, h, "/api/web/get_phone", &sdk.WebGetPhoneRequest{Local: local})
	var res sdk.WebGetPhoneResponse
	if code != 200 || json.Unmarshal(raw, &res) != nil {
		t.Fatalf("get_phone → %d %s", code, raw)
	}
	return res.Phone
}

var phoneOwner = &sdk.Actor{Name: "owner(web)"}

// 「讓手機連得到」：秘書 403、沒身分 400、主人但不是本機（local=false）403——都不會去跑 tailscale。
// 「關掉對外公開」：秘書 403、沒身分 400（一樣不跑 tailscale）；主人不在本機也能關（SET-12）。
func TestPhoneSetupOwnerAndThisMacOnly(t *testing.T) {
	state := phoneFake(t)
	ensured := 0
	_, h := settingsHome(t, "", Deps{Push: PushKeys{Ensure: func(string) (bool, error) { ensured++; return true, nil }}})
	for name, c := range map[string]struct {
		path  string
		by    *sdk.Actor
		local bool
		code  int
	}{
		"設定／秘書":      {"/api/web/phone_setup", &sdk.Actor{Name: "code"}, true, http.StatusForbidden},
		"設定／沒身分":     {"/api/web/phone_setup", nil, true, http.StatusBadRequest},
		"設定／主人但不是本機": {"/api/web/phone_setup", phoneOwner, false, http.StatusForbidden},
		"關閉／秘書":      {"/api/web/phone_close_funnel", &sdk.Actor{Name: "code"}, true, http.StatusForbidden},
		"關閉／沒身分":     {"/api/web/phone_close_funnel", nil, false, http.StatusBadRequest},
	} {
		code, raw := post(t, h, c.path, map[string]any{"by": c.by, "local": c.local})
		if code != c.code {
			t.Errorf("%s：要 %d，得到 %d %s", name, c.code, code, raw)
		}
		if name == "設定／主人但不是本機" && !strings.Contains(string(raw), "只能在跑看板的那台 Mac 上按") {
			t.Errorf("%s：要白話說去那台 Mac 按：%s", name, raw)
		}
	}
	if log := phoneCalls(state); log != "" || ensured != 0 {
		t.Fatalf("被擋的請求不能動 Tailscale 或做金鑰：%q %d", log, ensured)
	}
}

// 主人在本機按「讓手機連得到」：掛上看板、順便做推播金鑰；狀態跟著變成做好了、有網址與帳號。看狀態誰都可以。
func TestPhoneSetupMountsAndMakesPushKeys(t *testing.T) {
	state := phoneFake(t)
	ready := false
	dir, h := settingsHome(t, "", Deps{Push: PushKeys{
		Ready:  func(string) bool { return ready },
		Ensure: func(string) (bool, error) { ready = true; return true, nil },
	}})
	before := getPhone(t, h, false)
	if before.Tailscale != host.PhoneReady || before.Mounted || before.PushReady || before.Local || before.Account != "owner@example.com" || before.DownloadURL != host.TailscaleDownload {
		t.Fatalf("按之前：%+v", before)
	}
	code, raw := post(t, h, "/api/web/phone_setup", &sdk.WebPhoneSetupRequest{By: phoneOwner, Local: true})
	var res sdk.WebPhoneSetupResponse
	if code != 200 || json.Unmarshal(raw, &res) != nil || !res.OK {
		t.Fatalf("phone_setup → %d %s", code, raw)
	}
	if p := res.Phone; !p.Mounted || p.URL != "https://mac.example.ts.net" || !p.PushReady || !p.Local || p.Funnel {
		t.Fatalf("按之後：%+v", p)
	}
	if hm, _ := platform.LoadHome(dir); hm.Web.PublicURL != "https://mac.example.ts.net" {
		t.Fatalf("public_url：%q", hm.Web.PublicURL)
	}
	if log := phoneCalls(state); !strings.Contains(log, "serve --bg --https=443 http://127.0.0.1:") || strings.Contains(log, "funnel") {
		t.Fatalf("只能跑 serve、絕不開 Funnel：\n%s", log)
	}
}

// 看板掛好了但金鑰沒做成：照實說，不當成整個失敗。
func TestPhoneSetupReportsPushKeyFailure(t *testing.T) {
	phoneFake(t)
	_, h := settingsHome(t, "", Deps{Push: PushKeys{Ensure: func(string) (bool, error) { return false, errors.New("寫不進去") }}})
	code, raw := post(t, h, "/api/web/phone_setup", &sdk.WebPhoneSetupRequest{By: phoneOwner, Local: true})
	var res sdk.WebPhoneSetupResponse
	if code != 200 || json.Unmarshal(raw, &res) != nil || !res.OK || !strings.Contains(res.Message, "金鑰沒做成") || res.Phone.PushReady {
		t.Fatalf("phone_setup → %d %s", code, raw)
	}
}

// Funnel 開著：狀態與健檢都警告（白話「整個網路都連得到」）；主人在本機按關掉後兩邊都恢復。
func TestPhoneFunnelWarnedAndClosed(t *testing.T) {
	state := phoneFake(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(baseToml), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(Deps{})
	m.SetHome(dir)
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	hm, err := platform.LoadHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.checkFunnel(context.Background()); len(got) != 0 {
		t.Fatalf("沒開 Funnel 不該有這一條：%+v", got)
	}
	if err := os.WriteFile(filepath.Join(state, "funnel"), []byte("http://127.0.0.1:"+itoa(hm.Web.Port)), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := getPhone(t, mux, true); !p.Funnel || p.FunnelURL != "https://mac.example.ts.net" {
		t.Fatalf("Funnel 開著：%+v", p)
	}
	got := m.checkFunnel(context.Background())
	if len(got) != 1 || got[0].Status != app.StatusError || !strings.Contains(got[0].Message, "你的看板現在整個網路都連得到") || !strings.Contains(got[0].Fix, "關掉對外公開") {
		t.Fatalf("健檢：%+v", got)
	}
	if strings.Contains(got[0].Fix, "evergenie tailscale") || strings.Contains(got[0].Fix, "執行") {
		t.Fatalf("健檢的下一步不叫主人執行指令：%q", got[0].Fix)
	}
	// 主人不在這台 Mac 前面（手機上的看板，local=false）也能關。
	code, raw := post(t, mux, "/api/web/phone_close_funnel", &sdk.WebPhoneCloseFunnelRequest{By: phoneOwner, Local: false})
	var res sdk.WebPhoneCloseFunnelResponse
	if code != 200 || json.Unmarshal(raw, &res) != nil || res.Phone.Funnel {
		t.Fatalf("phone_close_funnel → %d %s", code, raw)
	}
	if got := m.checkFunnel(context.Background()); len(got) != 0 {
		t.Fatalf("關掉後健檢不該再報：%+v", got)
	}
	// 別的程式自己開的 Funnel（不是指到看板）不算。
	if err := os.WriteFile(filepath.Join(state, "funnel"), []byte("http://127.0.0.1:3000"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := m.checkFunnel(context.Background()); len(got) != 0 {
		t.Fatalf("別的程式的 Funnel 不算看板的：%+v", got)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
