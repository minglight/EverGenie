package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

const trustToml = `schema = 1

[[repo]]
path = "/tmp/hg-trust-repo"
push = false
writers = ["code"]

[deny]
actions = ["send_external"]
`

// 起一個有名冊（code、life）的測試家，用 app 的方式把 Routes() 掛起來。
func fixture(t *testing.T) (*Module, *homefixture.Home, http.Handler) {
	t.Helper()
	h := homefixture.New(t)
	h.Vault.RegisterSchema(vault.Spec{Kind: "agent", Path: func(id string) string { return "vault/agents/" + id + "/agent.md" }})
	h.WithRecords(vault.Record{Kind: "agent", ID: "code", Fields: map[string]any{}}, vault.Record{Kind: "agent", ID: "life", Fields: map[string]any{}})
	m := New(Deps{Vault: h.Vault, Clock: h.Clock})
	m.SetHome(h.Dir)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	return m, h, mux
}

func writeTrust(t *testing.T, dir, body string) {
	t.Helper()
	p := platform.TrustPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTrust(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(platform.TrustPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func post(t *testing.T, h http.Handler, path string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)))
	return rec.Code, rec.Body.Bytes()
}

// 只登記 system 服務的兩個方法，網址維持 /api/system/…；其他方法這裡沒有。
func TestRoutesTakeOnlyTrustMethodsOfSystemService(t *testing.T) {
	m, _, h := fixture(t)
	got := m.Routes()
	if len(got) != 2 || got[0].Service != "system" || strings.Join(got[0].Methods, ",") != "trust,set_permission_levels" || got[1].Service != "web" || strings.Join(got[1].Methods, ",") != "get_trust" {
		t.Fatalf("Routes = %+v", got)
	}
	if code, _ := post(t, h, "/api/system/trust", &sdk.SystemTrustRequest{}); code != 200 {
		t.Fatalf("trust：%d", code)
	}
	if code, _ := post(t, h, "/api/system/doctor", &sdk.SystemDoctorRequest{}); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Fatalf("doctor 不歸這個業務模組：%d", code)
	}
}

// 改等級一定要帶操作者，而且只有主人（owner(…)）可以；被擋時設定檔不變。
func TestSetPermissionLevelsRequiresOwner(t *testing.T) {
	_, h, mux := fixture(t)
	writeTrust(t, h.Dir, trustToml)
	req := &sdk.SystemSetPermissionLevelsRequest{DefaultLevel: sdk.PermissionLevelReadOnly}

	code, body := post(t, mux, "/api/system/set_permission_levels", req)
	var e sdk.Error
	if err := json.Unmarshal(body, &e); err != nil || code != 400 || e.Code != sdk.ErrorCodeInvalid || e.Field != "by" {
		t.Fatalf("沒帶 by：%d %s", code, body)
	}
	req.By = &sdk.Actor{Name: "code"}
	code, body = post(t, mux, "/api/system/set_permission_levels", req)
	if err := json.Unmarshal(body, &e); err != nil || code != 403 || !strings.Contains(e.Message, "只有主人") {
		t.Fatalf("秘書改權限：%d %s", code, body)
	}
	if readTrust(t, h.Dir) != trustToml {
		t.Fatal("被擋時設定檔不可以變")
	}
}

// 主人改：寫 default_level、保留其他欄位、舊檔備份；單獨設定與 clear。
func TestOwnerSetsLevelsAndKeepsOtherFields(t *testing.T) {
	_, h, mux := fixture(t)
	writeTrust(t, h.Dir, trustToml)
	set := func(req *sdk.SystemSetPermissionLevelsRequest) sdk.SystemSetPermissionLevelsResponse {
		t.Helper()
		req.By = &sdk.Actor{Name: "owner(cli)"}
		code, body := post(t, mux, "/api/system/set_permission_levels", req)
		var out sdk.SystemSetPermissionLevelsResponse
		if code != 200 || json.Unmarshal(body, &out) != nil {
			t.Fatalf("改等級：%d %s", code, body)
		}
		return out
	}
	out := set(&sdk.SystemSetPermissionLevelsRequest{DefaultLevel: sdk.PermissionLevelStandard})
	if s := readTrust(t, h.Dir); !strings.Contains(s, `default_level = "standard"`) || !strings.Contains(s, "/tmp/hg-trust-repo") || !strings.Contains(s, "send_external") {
		t.Fatalf("要寫 default_level 並保留其他欄位：\n%s", s)
	}
	if !strings.HasPrefix(out.Backup, "runtime/backups/trust/") {
		t.Fatalf("舊檔要備份：%q", out.Backup)
	}
	if b, err := os.ReadFile(filepath.Join(h.Dir, out.Backup)); err != nil || string(b) != trustToml {
		t.Fatalf("備份內容要是舊檔：%v", err)
	}
	if len(out.Agents) != 2 || out.Agents[0].Level != sdk.PermissionLevelStandard || out.Agents[0].Source != "default_level" {
		t.Fatalf("回傳每位秘書的等級：%+v", out.Agents)
	}

	out = set(&sdk.SystemSetPermissionLevelsRequest{Levels: []sdk.AgentPermission{{Agent: "life", Level: sdk.PermissionLevelFull}}})
	if life := out.Agents[1]; life.Agent != "life" || life.Level != sdk.PermissionLevelFull || life.Source != "agent" {
		t.Fatalf("life 單獨設定：%+v", life)
	}
	set(&sdk.SystemSetPermissionLevelsRequest{Levels: []sdk.AgentPermission{{Agent: "life"}}})
	if strings.Contains(readTrust(t, h.Dir), "[agent.life]") {
		t.Fatalf("Level 空字串要拿掉單獨設定：\n%s", readTrust(t, h.Dir))
	}

	// 不合格的等級被擋下，設定檔不變。
	before := readTrust(t, h.Dir)
	code, _ := post(t, mux, "/api/system/set_permission_levels", &sdk.SystemSetPermissionLevelsRequest{By: &sdk.Actor{Name: "owner(cli)"}, DefaultLevel: "root"})
	if code != 400 || readTrust(t, h.Dir) != before {
		t.Fatalf("壞等級：%d", code)
	}
}

// 沒有 trust.toml 也能列：全部內建預設；有名冊上每位秘書。
func TestTrustWithoutFileUsesBuiltinForEveryAgent(t *testing.T) {
	_, _, mux := fixture(t)
	code, body := post(t, mux, "/api/system/trust", &sdk.SystemTrustRequest{})
	var out sdk.SystemTrustResponse
	if code != 200 || json.Unmarshal(body, &out) != nil {
		t.Fatalf("%d %s", code, body)
	}
	if out.DefaultLevel != BuiltinLevel || len(out.Agents) != 2 || out.Agents[0].Agent != "code" || out.Agents[0].Source != "builtin" {
		t.Fatalf("%+v", out)
	}
}

// 權限來源攤表：repo、不開放的動作、單獨設定的秘書都在；snapshot 存檔並比出跟上一份的差異；被擋紀錄依天數篩選。
func TestTrustSourcesSnapshotAndDenied(t *testing.T) {
	_, h, mux := fixture(t)
	writeTrust(t, h.Dir, trustToml+"\n[agent.life]\nlevel = \"readonly\"\n")
	now := h.Clock.Now()
	for _, d := range []sdk.DeniedAction{
		{At: now.Add(-time.Hour), Agent: "life", Class: "send_external", Verdict: "deny", Reason: "近"},
		{At: now.AddDate(0, 0, -10), Agent: "life", Class: "delete", Verdict: "ask", Reason: "遠"},
	} {
		if err := AppendDenied(h.Dir, d); err != nil {
			t.Fatal(err)
		}
	}
	trust := func(req *sdk.SystemTrustRequest) sdk.SystemTrustResponse {
		t.Helper()
		code, body := post(t, mux, "/api/system/trust", req)
		var out sdk.SystemTrustResponse
		if code != 200 || json.Unmarshal(body, &out) != nil {
			t.Fatalf("%d %s", code, body)
		}
		return out
	}
	out := trust(&sdk.SystemTrustRequest{})
	kinds := map[string]string{}
	for _, s := range out.Sources {
		kinds[s.Kind+"|"+s.Scope] = s.Value
	}
	if kinds["repo|repo"] == "" || kinds["deny|整個家"] != "send_external" || kinds["level|秘書 life"] != "readonly" {
		t.Fatalf("來源攤表：%+v", out.Sources)
	}
	if len(out.Denied) != 1 || out.Denied[0].Reason != "近" {
		t.Fatalf("預設只看 7 天內：%+v", out.Denied)
	}
	if out = trust(&sdk.SystemTrustRequest{DeniedDays: 30}); len(out.Denied) != 2 || out.Denied[0].Reason != "遠" {
		t.Fatalf("30 天內兩筆、舊的在前：%+v", out.Denied)
	}

	first := trust(&sdk.SystemTrustRequest{Snapshot: true})
	if !strings.HasPrefix(first.SnapshotPath, "runtime/trust/snapshots/") || len(first.Changes) != 0 {
		t.Fatalf("第一份快照沒有差異：%+v", first)
	}
	h.Clock.Advance(time.Minute)
	writeTrust(t, h.Dir, trustToml) // 拿掉 life 的單獨設定
	second := trust(&sdk.SystemTrustRequest{Snapshot: true})
	if len(second.Changes) == 0 || !strings.Contains(strings.Join(second.Changes, "\n"), "拿掉 秘書 life 的 level：readonly") {
		t.Fatalf("要比出拿掉了什麼：%v", second.Changes)
	}
	if entries, _ := os.ReadDir(filepath.Join(h.Dir, "runtime", "trust", "snapshots")); len(entries) != 2 {
		t.Fatalf("兩份快照：%v", entries)
	}
}

// 還不知道家在哪裡 → unavailable，不是 panic。
func TestWithoutHomeIsUnavailable(t *testing.T) {
	mux, err := app.New(New(Deps{})).Mux()
	if err != nil {
		t.Fatal(err)
	}
	code, _ := post(t, mux, "/api/system/trust", &sdk.SystemTrustRequest{})
	if code != http.StatusServiceUnavailable {
		t.Fatalf("要 503，得到 %d", code)
	}
}

func TestGetTrustReloadsAndRejectsInvalidConfig(t *testing.T) {
	_, h, mux := fixture(t)
	code, body := post(t, mux, "/api/web/get_trust", &sdk.WebGetTrustRequest{})
	var out sdk.WebGetTrustResponse
	if code != 200 || json.Unmarshal(body, &out) != nil || out.Policy == nil {
		t.Fatalf("%d %s", code, body)
	}
	writeTrust(t, h.Dir, trustToml)
	code, body = post(t, mux, "/api/web/get_trust", &sdk.WebGetTrustRequest{})
	if code != 200 || json.Unmarshal(body, &out) != nil || len(out.Policy.Repos) == 0 {
		t.Fatalf("%d %s", code, body)
	}
	writeTrust(t, h.Dir, "[[repo]\n")
	code, _ = post(t, mux, "/api/web/get_trust", &sdk.WebGetTrustRequest{})
	if code != 400 {
		t.Fatal(code)
	}
}
