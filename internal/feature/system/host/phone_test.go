package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 手機連線的測試用一支假的 tailscale 執行檔（真的去跑它，不碰這台的 Tailscale）：照 state/ 資料夾裡的檔決定回什麼，
// 每次被呼叫都記進 state/calls.log。
//
//	status.json     `status --json` 的輸出；沒有這個檔＝Tailscale 沒開（結束碼 1）
//	serve.json      `serve status --json` 的輸出；沒有＝`{}`
//	dns             這台在 tailnet 的名字（serve --bg 掛上去時用）
//	serve-blocked   有這個檔：`serve --bg` 印出它的內容、結束碼 1（後台還沒開 HTTPS）
//	keep-funnel     有這個檔：`serve --bg` 不會把 Funnel 收回（逼出「直接拿掉」那條路）
const fakeTailscaleScript = `#!/bin/sh
d="$(dirname "$0")/state"
echo "$*" >>"$d/calls.log"
case "$1 $2" in
"status --json")
  [ -f "$d/status.json" ] || { echo "Tailscale is stopped."; exit 1; }
  cat "$d/status.json" ;;
"serve status")
  if [ -f "$d/serve.json" ]; then cat "$d/serve.json"; else echo "{}"; fi ;;
"serve get-config")
  echo '{"version":"0.0.1"}' ;;
"serve --bg")
  if [ -f "$d/serve-blocked" ]; then cat "$d/serve-blocked"; exit 1; fi
  port="${3#--https=}"
  hp="$(cat "$d/dns"):$port"
  funnel=""
  if [ -f "$d/keep-funnel" ] && grep -q AllowFunnel "$d/serve.json" 2>/dev/null; then funnel=",\"AllowFunnel\":{\"$hp\":true}"; fi
  printf '{"TCP":{"%s":{"HTTPS":true}},"Web":{"%s":{"Handlers":{"/":{"Proxy":"%s"}}}}%s}\n' "$port" "$hp" "$4" "$funnel" >"$d/serve.json" ;;
"funnel --https="*|"funnel --tcp="*)
  [ "$3" = "off" ] || { echo "假的 tailscale 不開 Funnel"; exit 1; }
  echo "{}" >"$d/serve.json" ;;
*)
  echo "假的 tailscale 不認得：$*"; exit 1 ;;
esac
`

const (
	fakeDNS        = "mac.example.ts.net"
	fakeRunning    = `{"BackendState":"Running","MagicDNSSuffix":"example.ts.net","CertDomains":["mac.example.ts.net"],"Self":{"DNSName":"mac.example.ts.net.","UserID":42},"User":{"42":{"LoginName":"owner@example.com"}}}`
	fakeNoHTTPS    = `{"BackendState":"Running","MagicDNSSuffix":"example.ts.net","Self":{"DNSName":"mac.example.ts.net.","UserID":42},"User":{"42":{"LoginName":"owner@example.com"}}}`
	fakeNeedsLogin = `{"BackendState":"NeedsLogin","Self":{"DNSName":""}}`
	// 看板（18850）被 Funnel 開出去；另一個程式（3000）自己開的 Funnel 不算看板的。
	fakeFunnelOn    = `{"TCP":{"443":{"HTTPS":true},"8443":{"HTTPS":true}},"Web":{"mac.example.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:18850"}}},"mac.example.ts.net:8443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:3000"}}}},"AllowFunnel":{"mac.example.ts.net:443":true,"mac.example.ts.net:8443":true}}`
	fakeOtherFunnel = `{"TCP":{"8443":{"HTTPS":true}},"Web":{"mac.example.ts.net:8443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:3000"}}}},"AllowFunnel":{"mac.example.ts.net:8443":true}}`
	fakeBlocked     = "Serve is not enabled on your tailnet.\nTo enable, visit:\n\n         https://login.tailscale.com/f/serve?node=n123CNTRL\n\n"
)

// fakeTailscale 放一支假的 tailscale 並讓程式只用它（EVERGENIE_TAILSCALE_BIN）；回 state 資料夾。installed=false：路徑上沒有檔＝沒裝。
func fakeTailscale(t *testing.T, installed bool) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "tailscale")
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if installed {
		if err := os.WriteFile(bin, []byte(fakeTailscaleScript), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(TailscaleBinEnv, bin)
	put(t, state, "dns", fakeDNS)
	return state
}

func put(t *testing.T, state, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(state, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func calls(t *testing.T, state string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(state, "calls.log"))
	return string(b)
}

func realTailscale(home string) Tailscale {
	return Tailscale{Runner: ExecRunner{Timeout: 10 * time.Second}, Home: home}
}

// 沒裝：狀態是「沒裝」、按鈕回官網下載連結；安裝前檢查用同一套找法，也說沒裝、請他到官網下載（不建議 brew）。
func TestPhoneNotInstalled(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	fakeTailscale(t, false)
	ts := realTailscale(home)
	if s := ts.Phone(ctx); s.State != PhoneMissing || s.Mounted || s.Funnel {
		t.Fatalf("沒裝：%+v", s)
	}
	got := ts.SetupPhone(ctx)
	if got.OK || got.Reason != PhoneMissing || got.HelpURL != TailscaleDownload || !strings.Contains(got.Message, "官網下載") {
		t.Fatalf("沒裝時按按鈕：%+v", got)
	}
	for _, c := range Preflight(ExecRunner{}, PreflightOptions{OS: "darwin", Need: map[string]bool{"tailscale": true}}) {
		if c.ID == "tailscale" && (c.Level != LevelBad || !strings.Contains(c.Fix, TailscaleDownload) || strings.Contains(c.Fix, "brew")) {
			t.Fatalf("安裝前檢查：%+v", c)
		}
	}
}

// 檢查與設定用同一支：安裝前檢查找到的程式就是 setup 會跑的那一支。
func TestPreflightAndSetupUseSameTailscale(t *testing.T) {
	home := testHome(t)
	fakeTailscale(t, true)
	bin := realTailscale(home).bin()
	if bin == "" || bin != sdk.Getenv(TailscaleBinEnv) {
		t.Fatalf("setup 用的程式：%q", bin)
	}
	for _, c := range Preflight(ExecRunner{}, PreflightOptions{OS: "darwin"}) {
		if c.ID == "tailscale" && (c.Level != LevelOK || c.Detail != bin) {
			t.Fatalf("安裝前檢查找到的不是同一支：%+v（setup 用 %s）", c, bin)
		}
	}
	// App 內附那支優先於 PATH 上的（沒有設覆蓋時）。
	t.Setenv(TailscaleBinEnv, "")
	both := &fakeRunner{have: map[string]string{"tailscale": tsBin, AppCLI: AppCLI}}
	for _, c := range Preflight(both, PreflightOptions{OS: "darwin"}) {
		if c.ID == "tailscale" && c.Detail != AppCLI {
			t.Fatalf("安裝前檢查要先找 App 內附那支：%+v", c)
		}
	}
}

// 沒開、沒登入：各自的狀態；按按鈕停在那一步、不會去 serve。
func TestPhoneStoppedAndNeedsLogin(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	state := fakeTailscale(t, true)
	ts := realTailscale(home)
	if s := ts.Phone(ctx); s.State != PhoneStopped {
		t.Fatalf("沒開：%+v", s)
	}
	if got := ts.SetupPhone(ctx); got.OK || got.Reason != PhoneStopped {
		t.Fatalf("沒開時按按鈕：%+v", got)
	}
	put(t, state, "status.json", fakeNeedsLogin)
	if s := ts.Phone(ctx); s.State != PhoneLogin {
		t.Fatalf("沒登入：%+v", s)
	}
	if got := ts.SetupPhone(ctx); got.OK || got.Reason != PhoneLogin || !strings.Contains(got.Message, "登入") {
		t.Fatalf("沒登入時按按鈕：%+v", got)
	}
	if strings.Contains(calls(t, state), "serve --bg") {
		t.Fatalf("沒登入不能 serve：\n%s", calls(t, state))
	}
}

// 按按鈕：只跑 serve（絕不開 Funnel），掛好後狀態是「已掛好」、有網址與帳號；再按一次也沒事。
func TestPhoneSetupMountsBoardAndNeverOpensFunnel(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	state := fakeTailscale(t, true)
	put(t, state, "status.json", fakeRunning)
	ts := realTailscale(home)
	if s := ts.Phone(ctx); s.State != PhoneReady || s.Mounted || s.Account != "owner@example.com" {
		t.Fatalf("登入了還沒掛：%+v", s)
	}
	for i := 0; i < 2; i++ {
		got := ts.SetupPhone(ctx)
		if !got.OK || !strings.Contains(strings.Join(got.Lines, "\n"), "不要把別人加進你的 Tailscale 網路") {
			t.Fatalf("第 %d 次按：%+v", i+1, got)
		}
	}
	s := ts.Phone(ctx)
	if !s.Mounted || s.URL != "https://"+fakeDNS || s.Funnel || s.Problem != "" {
		t.Fatalf("掛好後：%+v", s)
	}
	if h, _ := platform.LoadHome(home); h.Web.PublicURL != "https://"+fakeDNS {
		t.Fatalf("public_url：%q", h.Web.PublicURL)
	}
	log := calls(t, state)
	if !strings.Contains(log, "serve --bg --https=443 http://127.0.0.1:18850") || strings.Contains(log, "funnel") {
		t.Fatalf("只能跑 serve、不能出現 funnel：\n%s", log)
	}
}

// 掛上了但 home.toml 的網址不是這個（例如手動 serve 的）：不算做好，按一次按鈕就會補上。
func TestPhoneMountedNeedsPublicURL(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	state := fakeTailscale(t, true)
	put(t, state, "status.json", fakeRunning)
	put(t, state, "serve.json", `{"Web":{"mac.example.ts.net:443":{"Handlers":{"/":{"Proxy":"http://localhost:18850"}}}}}`)
	if s := realTailscale(home).Phone(ctx); s.Mounted || s.URL != "" {
		t.Fatalf("public_url 還沒寫不算做好：%+v", s)
	}
}

// Funnel 開著：認得出看板被公開（別的程式自己的 Funnel 不算）；關掉後手機照樣連得到、別人的 Funnel 沒被動到判斷。
func TestPhoneDetectsAndClosesFunnel(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	state := fakeTailscale(t, true)
	put(t, state, "status.json", fakeRunning)
	ts := realTailscale(home)

	put(t, state, "serve.json", fakeOtherFunnel)
	if s := ts.Phone(ctx); s.Funnel {
		t.Fatalf("別的程式的 Funnel 不算看板的：%+v", s)
	}
	if lines, err := ts.CloseFunnel(ctx); err != nil || strings.Contains(calls(t, state), "funnel") || len(lines) != 1 {
		t.Fatalf("看板沒被公開就什麼都不做：%v %v\n%s", lines, err, calls(t, state))
	}

	if got := ts.SetupPhone(ctx); !got.OK {
		t.Fatal(got)
	}
	put(t, state, "serve.json", fakeFunnelOn)
	s := ts.Phone(ctx)
	if !s.Funnel || s.FunnelURL != "https://"+fakeDNS {
		t.Fatalf("Funnel 開著要認得：%+v", s)
	}
	if warn := strings.Join(ts.Status(ctx), "\n"); !strings.Contains(warn, "你的看板現在整個網路都連得到") {
		t.Fatalf("status 要警告：\n%s", warn)
	}
	lines, err := ts.CloseFunnel(ctx)
	if err != nil {
		t.Fatalf("關 Funnel：%v %v", lines, err)
	}
	if s := ts.Phone(ctx); s.Funnel || !s.Mounted {
		t.Fatalf("關掉後：不公開、手機照樣連得到：%+v", s)
	}
	if strings.Contains(calls(t, state), "funnel") {
		t.Fatalf("這條路只要重新 serve 就收回，不用動 funnel 指令：\n%s", calls(t, state))
	}

	// serve 收不回去的版本：直接把 Funnel 拿掉，再掛回只有自己網路的 serve。
	put(t, state, "serve.json", fakeFunnelOn)
	put(t, state, "keep-funnel", "")
	if _, err := ts.CloseFunnel(ctx); err != nil {
		t.Fatal(err)
	}
	if s := ts.Phone(ctx); s.Funnel || !s.Mounted {
		t.Fatalf("直接拿掉後：%+v", s)
	}
	if log := calls(t, state); !strings.Contains(log, "funnel --https=443 off") {
		t.Fatalf("要跑 funnel off：\n%s", log)
	}
}

// 整個 TCP 轉送的 Funnel（不是網頁代理）指到看板也算，關的時候用 --tcp。
func TestPhoneDetectsTCPFunnel(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	state := fakeTailscale(t, true)
	put(t, state, "status.json", fakeRunning)
	put(t, state, "serve.json", `{"TCP":{"10000":{"TCPForward":"127.0.0.1:18850"}},"AllowFunnel":{"mac.example.ts.net:10000":true}}`)
	ts := realTailscale(home)
	if s := ts.Phone(ctx); !s.Funnel || s.FunnelURL != "https://mac.example.ts.net:10000" {
		t.Fatalf("TCP 轉送的 Funnel：%+v", s)
	}
	if _, err := ts.CloseFunnel(ctx); err != nil || !strings.Contains(calls(t, state), "funnel --tcp=10000 off") {
		t.Fatalf("關 TCP 的 Funnel：%v\n%s", err, calls(t, state))
	}
}

// 新帳號第一次 serve 被後台擋（HTTPS 還沒開）：接住、說要去哪個網頁按什麼，public_url 不寫；開好再按一次就成功。
func TestPhoneSetupBlockedUntilHTTPSEnabled(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	state := fakeTailscale(t, true)
	put(t, state, "status.json", fakeNoHTTPS)
	put(t, state, "serve-blocked", fakeBlocked)
	ts := realTailscale(home)
	got := ts.SetupPhone(ctx)
	if got.OK || got.Reason != PhoneReasonHTTPS || got.HelpURL != "https://login.tailscale.com/f/serve?node=n123CNTRL" {
		t.Fatalf("被後台擋：%+v", got)
	}
	for _, want := range []string{"HTTPS", "Enable", got.HelpURL, "再做一次"} {
		if !strings.Contains(got.Message, want) {
			t.Fatalf("說明少了「%s」：%s", want, got.Message)
		}
	}
	if h, _ := platform.LoadHome(home); h.Web.PublicURL != "" {
		t.Fatalf("沒掛成不能寫 public_url：%q", h.Web.PublicURL)
	}
	// 指令也是同一句白話。
	if _, err := ts.Setup(ctx); err == nil || !strings.Contains(platformMessage(err), "https://login.tailscale.com/f/serve?node=n123CNTRL") {
		t.Fatalf("evergenie tailscale setup 的錯誤：%v", err)
	}
	// Tailscale 沒給連結、但這台還沒有憑證網域：一樣當成 HTTPS 沒開，帶到後台的 DNS 頁。
	put(t, state, "serve-blocked", "error: something about certificates\n")
	if got := ts.SetupPhone(ctx); got.Reason != PhoneReasonHTTPS || got.HelpURL != tailscaleAdminDNS {
		t.Fatalf("沒給連結：%+v", got)
	}
	// 已經有憑證網域的失敗不亂猜：照實附上 Tailscale 的話。
	put(t, state, "status.json", fakeRunning)
	put(t, state, "serve-blocked", "error: port in use\n")
	if got := ts.SetupPhone(ctx); got.Reason != PhoneReasonOther || !strings.Contains(got.Message, "port in use") {
		t.Fatalf("其他失敗：%+v", got)
	}
	// 主人到後台開好了：再按一次就成功。
	os.Remove(filepath.Join(state, "serve-blocked"))
	if got := ts.SetupPhone(ctx); !got.OK {
		t.Fatalf("開好後再按：%+v", got)
	}
	if s := ts.Phone(ctx); !s.Mounted {
		t.Fatalf("開好後：%+v", s)
	}
}

// 早就用 Tailscale 服務（svc:…）掛好看板、home.toml 只寫了 public_url 的家：算掛好了；
// 按「讓手機連得到」不在這台再掛一次、不改網址（2026-11-17 實測抓到：會被說成還沒做）。
func TestPhoneRecognisesBoardMountedByService(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	path := filepath.Join(home, "home.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, []byte("public_url = \"https://genie.example.ts.net\"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	state := fakeTailscale(t, true)
	put(t, state, "status.json", fakeRunning)
	put(t, state, "serve.json", `{"Services":{"svc:genie":{"TCP":{"443":{"HTTPS":true}},"Web":{"genie.example.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:18850"}}}}},"svc:other":{"Web":{"other.example.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:3000"}}}}}}}`)
	ts := realTailscale(home)
	s := ts.Phone(ctx)
	if !s.Mounted || s.URL != "https://genie.example.ts.net" || s.Problem != "" {
		t.Fatalf("用服務掛好的要算掛好：%+v", s)
	}
	got := ts.SetupPhone(ctx)
	if !got.OK || !strings.Contains(strings.Join(got.Lines, "\n"), "不用再設定") {
		t.Fatalf("已經掛好時按鈕：%+v", got)
	}
	if strings.Contains(calls(t, state), "serve --bg") {
		t.Fatalf("不能再掛一次：\n%s", calls(t, state))
	}
	if h, _ := platform.LoadHome(home); h.Web.PublicURL != "https://genie.example.ts.net" {
		t.Fatalf("網址不能被改：%q", h.Web.PublicURL)
	}
	// 服務指到別的 port（不是這個家的看板）不算。
	put(t, state, "serve.json", `{"Services":{"svc:genie":{"Web":{"genie.example.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:3000"}}}}}}}`)
	if s := ts.Phone(ctx); s.Mounted {
		t.Fatalf("指到別的程式不算掛好：%+v", s)
	}
}
