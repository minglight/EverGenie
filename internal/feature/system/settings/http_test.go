package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

const baseToml = "[user]\nname = \"測試\"\ntimezone = \"Asia/Taipei\"\n[slots]\ndefault = \"life\"\n[slots.life]\nlabel = \"生活\"\nremind = \"08:30\"\ncheck = \"18:00\"\n[accounts]\ndefault = \"main\"\n[accounts.main]\nengine = \"fake\"\n[web]\nport = 18850\n"

type fakeEngine struct {
	rows []sdk.PreflightCheck
	err  error
}

func (f fakeEngine) Preflight(context.Context) ([]sdk.PreflightCheck, error) { return f.rows, f.err }

// settingsHome 建一個有 home.toml 的家，用 app 的方式把 Routes() 掛起來。
func settingsHome(t *testing.T, extra string, d Deps) (string, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(extra+baseToml), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	d.Clock = stepClock{&start}
	m := New(d)
	m.SetHome(dir)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	return dir, mux
}

// stepClock 每問一次就往前一秒（備份檔名到秒，兩次改設定不能撞名）。
type stepClock struct{ t *time.Time }

func (c stepClock) Now() time.Time { *c.t = c.t.Add(time.Second); return *c.t }

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

func getMode(t *testing.T, h http.Handler) sdk.HomeMode {
	t.Helper()
	code, raw := post(t, h, "/api/system/get_mode", &sdk.SystemGetModeRequest{})
	if code != 200 {
		t.Fatalf("get_mode → %d %s", code, raw)
	}
	var res sdk.SystemGetModeResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	return res.Mode
}

var owner = &sdk.Actor{Name: "owner(web)"}

// 介面模式：沒寫預設 simple；主人改了要先備份、立刻讀得到、其他設定留著。
// Guards: REQ-25, REQ-58
func TestModeGetAndSet(t *testing.T) {
	dir, h := settingsHome(t, "", Deps{})
	if got := getMode(t, h); got != sdk.HomeModeSimple {
		t.Fatalf("沒寫 mode 預設要是 simple，拿到 %q", got)
	}
	code, raw := post(t, h, "/api/system/set_mode", &sdk.SystemSetModeRequest{Mode: sdk.HomeModeFull, By: owner})
	if code != 200 {
		t.Fatalf("set_mode → %d %s", code, raw)
	}
	var res sdk.SystemSetModeResponse
	if err := json.Unmarshal(raw, &res); err != nil || res.Mode != sdk.HomeModeFull || res.Backup == "" {
		t.Fatalf("set_mode 回應不對：%s %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(dir, res.Backup)); err != nil {
		t.Fatalf("要先備份：%v", err)
	}
	if got := getMode(t, h); got != sdk.HomeModeFull {
		t.Fatalf("改完要立刻讀得到（不用重開），拿到 %q", got)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "home.toml"))
	if !strings.Contains(string(data), `mode = "full"`) || !strings.Contains(string(data), "測試") {
		t.Fatalf("home.toml 其他設定要留著：\n%s", data)
	}
}

// get_mode 也回家的身分：看板拿 env 與標籤標出是哪個家（開發家的標籤加 @dev）。
func TestGetModeReturnsHomeIdentity(t *testing.T) {
	for env, label := range map[string]string{"prod": "amy-home", "dev": "amy-home@dev"} {
		_, h := settingsHome(t, "[runtime]\nenv = \""+env+"\"\nowner = \"amy\"\nmachine = \"home\"\n", Deps{})
		code, raw := post(t, h, "/api/system/get_mode", &sdk.SystemGetModeRequest{})
		var res sdk.SystemGetModeResponse
		if code != 200 || json.Unmarshal(raw, &res) != nil {
			t.Fatalf("get_mode → %d %s", code, raw)
		}
		if res.UserName == "" {
			t.Fatalf("env=%s：get_mode 沒帶 user_name：%+v", env, res)
		}
		if string(res.Env) != env || res.Label != label || res.Machine != "home" {
			t.Fatalf("env=%s：%+v", env, res)
		}
	}
}

// get_mode 回看板連多台要的值：執行中的版本（跟 update_status 的 current 同來源）、看板介面等級、對外網址（去掉結尾斜線）。
func TestGetModeReturnsVersionBoardAPIAndPublicURL(t *testing.T) {
	dir, h := settingsHome(t, "", Deps{})
	// baseToml 最後一段是 [web]，接在檔尾的 public_url 就落在 [web] 裡；GetMode 每次都重讀 home.toml。
	f, err := os.OpenFile(filepath.Join(dir, "home.toml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("public_url = \"https://genie-amy-mini.example.ts.net/\"\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	code, raw := post(t, h, "/api/system/get_mode", &sdk.SystemGetModeRequest{})
	var res sdk.SystemGetModeResponse
	if code != 200 || json.Unmarshal(raw, &res) != nil {
		t.Fatalf("get_mode → %d %s", code, raw)
	}
	if res.Version == "" || res.Version != platform.Version() {
		t.Errorf("version：%q，應該是 %q", res.Version, platform.Version())
	}
	if res.BoardAPI != sdk.BoardAPILevel || sdk.BoardAPILevel < 1 {
		t.Errorf("board_api：%d，應該是 %d", res.BoardAPI, sdk.BoardAPILevel)
	}
	if res.PublicURL != "https://genie-amy-mini.example.ts.net" {
		t.Errorf("public_url：%q", res.PublicURL)
	}
}

// 對話的顯示方式：沒寫是 default；主人改了要先備份、立刻讀得到；別人和不認得的值不能改。
func TestChatDisplayGetAndSet(t *testing.T) {
	dir, h := settingsHome(t, "", Deps{})
	get := func() sdk.ChatDisplay {
		t.Helper()
		_, raw := post(t, h, "/api/system/get_mode", &sdk.SystemGetModeRequest{})
		var res sdk.SystemGetModeResponse
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatal(err)
		}
		return res.ChatDisplay
	}
	if got := get(); got != sdk.ChatDisplayDefault {
		t.Fatalf("沒寫要是 default，拿到 %q", got)
	}
	code, raw := post(t, h, "/api/system/set_chat_display", &sdk.SystemSetChatDisplayRequest{ChatDisplay: sdk.ChatDisplayEngineer, By: owner})
	if code != 200 {
		t.Fatalf("set_chat_display → %d %s", code, raw)
	}
	var res sdk.SystemSetChatDisplayResponse
	if err := json.Unmarshal(raw, &res); err != nil || res.Backup == "" {
		t.Fatalf("回應不對：%s %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(dir, res.Backup)); err != nil {
		t.Fatalf("要先備份：%v", err)
	}
	if got := get(); got != sdk.ChatDisplayEngineer {
		t.Fatalf("改完要立刻讀得到，拿到 %q", got)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "home.toml"))
	for name, req := range map[string]*sdk.SystemSetChatDisplayRequest{
		"秘書不能改": {ChatDisplay: sdk.ChatDisplayDefault, By: &sdk.Actor{Name: "code"}},
		"不認得的值": {ChatDisplay: "advanced", By: owner},
		"空的值":   {By: owner},
		"沒有操作者": {ChatDisplay: sdk.ChatDisplayDefault},
	} {
		if code, raw := post(t, h, "/api/system/set_chat_display", req); code == 200 {
			t.Errorf("%s：應該被拒絕，拿到 200 %s", name, raw)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "home.toml"))
	if string(before) != string(after) {
		t.Fatalf("被拒絕的請求不能改檔：\n%s", after)
	}
}

// 改模式被拒絕（秘書、不認得的模式、空的、沒有操作者）時，檔案不能動、模式不變。
// Guards: REQ-58
func TestModeSetRejectsBadInput(t *testing.T) {
	dir, h := settingsHome(t, "mode = \"full\"\n", Deps{})
	before, _ := os.ReadFile(filepath.Join(dir, "home.toml"))
	for name, req := range map[string]*sdk.SystemSetModeRequest{
		"秘書不能改":  {Mode: sdk.HomeModeSimple, By: &sdk.Actor{Name: "code"}},
		"不認得的模式": {Mode: "advanced", By: owner},
		"空的模式":   {By: owner},
		"沒有操作者":  {Mode: sdk.HomeModeSimple},
	} {
		if code, raw := post(t, h, "/api/system/set_mode", req); code == 200 {
			t.Errorf("%s：應該被拒絕，拿到 200 %s", name, raw)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "home.toml"))
	if string(before) != string(after) {
		t.Fatalf("被拒絕的請求不能改檔：\n%s", after)
	}
	if got := getMode(t, h); got != sdk.HomeModeFull {
		t.Fatalf("模式要沒變，拿到 %q", got)
	}
}

// 設定：誰都能讀；只有主人能改（秘書 403、沒身分 400）；改了先備份。
func TestConfigGetSetOwnerOnly(t *testing.T) {
	dir, h := settingsHome(t, "", Deps{})
	code, raw := post(t, h, "/api/system/get_config", &sdk.SystemGetConfigRequest{Key: "user.timezone"})
	var got sdk.SystemGetConfigResponse
	if code != 200 || json.Unmarshal(raw, &got) != nil || !strings.Contains(got.Value, "Asia/Taipei") {
		t.Fatalf("get_config：%d %s", code, raw)
	}
	set := func(by *sdk.Actor) (int, []byte) {
		return post(t, h, "/api/system/set_config", &sdk.SystemSetConfigRequest{Key: "user.timezone", Value: `"Asia/Tokyo"`, By: by})
	}
	if code, raw := set(&sdk.Actor{Name: "code"}); code != http.StatusForbidden {
		t.Fatalf("秘書要 403：%d %s", code, raw)
	}
	if code, raw := set(nil); code != http.StatusBadRequest {
		t.Fatalf("沒身分要 400：%d %s", code, raw)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "home.toml")); strings.Contains(string(data), "Tokyo") {
		t.Fatal("被擋的請求不能改檔")
	}
	code, raw = set(owner)
	var res sdk.SystemSetConfigResponse
	if code != 200 || json.Unmarshal(raw, &res) != nil || res.Backup == "" {
		t.Fatalf("主人改：%d %s", code, raw)
	}
	if _, err := os.Stat(filepath.Join(dir, res.Backup)); err != nil {
		t.Fatalf("要先備份：%v", err)
	}
}

// 備份：列出誰都行；prune 預設只預演；真的刪（apply）只有主人，而且要搭配 prune。
func TestBackupsListPruneOwnerOnly(t *testing.T) {
	dir, h := settingsHome(t, "", Deps{})
	for _, v := range []string{`"Asia/Tokyo"`, `"Asia/Seoul"`} { // 改兩次設定＝兩個備份
		if code, raw := post(t, h, "/api/system/set_config", &sdk.SystemSetConfigRequest{Key: "user.timezone", Value: v, By: owner}); code != 200 {
			t.Fatalf("set_config：%d %s", code, raw)
		}
	}
	list := func(req *sdk.SystemBackupsRequest) (int, sdk.SystemBackupsResponse) {
		code, raw := post(t, h, "/api/system/backups", req)
		var res sdk.SystemBackupsResponse
		_ = json.Unmarshal(raw, &res)
		return code, res
	}
	if code, res := list(&sdk.SystemBackupsRequest{}); code != 200 || len(res.Files) < 2 || res.Pruned != 0 {
		t.Fatalf("list：%d %+v", code, res)
	}
	code, res := list(&sdk.SystemBackupsRequest{Prune: true, Keep: 1, Days: 1, By: &sdk.Actor{Name: "code"}})
	if code != 200 || res.Applied {
		t.Fatalf("秘書可以預演但不會真的刪：%d %+v", code, res)
	}
	if code, _ := list(&sdk.SystemBackupsRequest{Prune: true, Apply: true, By: &sdk.Actor{Name: "code"}}); code != http.StatusForbidden {
		t.Fatalf("秘書刪要 403：%d", code)
	}
	if code, _ := list(&sdk.SystemBackupsRequest{Apply: true, By: owner}); code != http.StatusBadRequest {
		t.Fatalf("apply 沒搭配 prune 要 400：%d", code)
	}
	backups, _ := ListBackups(dir)
	if len(backups) < 2 {
		t.Fatalf("前面被拒絕的請求不能刪東西：%d", len(backups))
	}
}

// 整理備份不刪「換不回去的紀錄」（update 的 rescue.json）還要用的備份（system 卡 UPD-14）：同一組裡最舊、照規則該刪的那份留著，
// 別的照常刪；紀錄處理掉之後才照一般的規則整理。
func TestBackupsPruneKeepsRescueBackup(t *testing.T) {
	dir, h := settingsHome(t, "", Deps{})
	put := func(name string, age time.Duration) string {
		p := platform.RuntimeDir(dir, "backups", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("備份"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
		return p
	}
	day := 24 * time.Hour
	put("upgrade-0.2.0-0.3.0.tgz", 50*day)
	other := put("upgrade-0.1.5-0.2.0.tgz", 80*day)
	needed := put("upgrade-0.1.0-0.1.5.tgz", 100*day)
	rescue := map[string]any{"from": "0.1.0", "to": "0.1.5", "backup": needed, "restore": true, "at": time.Now()}
	data, _ := json.Marshal(rescue)
	if err := os.MkdirAll(platform.RuntimeDir(dir, "upgrade"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(platform.RuntimeDir(dir, "upgrade", "rescue.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	code, raw := post(t, h, "/api/system/backups", &sdk.SystemBackupsRequest{Prune: true, Apply: true, Keep: 1, Days: 1, By: owner})
	if code != 200 {
		t.Fatalf("prune：%d %s", code, raw)
	}
	if _, err := os.Stat(needed); err != nil {
		t.Fatalf("換不回去的紀錄要用的備份不能刪：%v", err)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatalf("其他照規則該刪的照常刪：%v", err)
	}
}

// 安裝前檢查：沒接引擎回 503；接了照實回；引擎出錯轉成 API 錯誤。
func TestPreflightRoute(t *testing.T) {
	_, h := settingsHome(t, "", Deps{})
	if code, raw := post(t, h, "/api/system/preflight", &sdk.SystemPreflightRequest{}); code != http.StatusServiceUnavailable {
		t.Fatalf("沒接引擎要 503：%d %s", code, raw)
	}
	_, h = settingsHome(t, "", Deps{Engine: fakeEngine{rows: []sdk.PreflightCheck{{ID: "tool-a", Name: "工具 A", Ok: true}}}})
	code, raw := post(t, h, "/api/system/preflight", &sdk.SystemPreflightRequest{})
	var res sdk.SystemPreflightResponse
	if code != 200 || json.Unmarshal(raw, &res) != nil || len(res.Checks) != 1 || res.Checks[0].ID != "tool-a" {
		t.Fatalf("要照實回：%d %s", code, raw)
	}
	_, h = settingsHome(t, "", Deps{Engine: fakeEngine{err: errors.New("boom")}})
	if code, _ := post(t, h, "/api/system/preflight", &sdk.SystemPreflightRequest{}); code != http.StatusInternalServerError {
		t.Fatalf("引擎出錯要 500：%d", code)
	}
}

// 工作對話上限（SES-73）：沒寫是 3、範圍 1～10；主人改了只動 work_max 那一行、立刻讀得到；別人、範圍外、沒有操作者都拒絕、檔案不動。
// Guards: SES-73
func TestWorkMaxGetAndSet(t *testing.T) {
	dir, h := settingsHome(t, "", Deps{})
	get := func() sdk.SystemGetWorkMaxResponse {
		t.Helper()
		code, raw := post(t, h, "/api/system/get_work_max", &sdk.SystemGetWorkMaxRequest{})
		var res sdk.SystemGetWorkMaxResponse
		if code != 200 || json.Unmarshal(raw, &res) != nil {
			t.Fatalf("get_work_max → %d %s", code, raw)
		}
		return res
	}
	if got := get(); got.WorkMax != 3 || got.Set || got.Default != 3 || got.Min != 1 || got.Max != 10 {
		t.Fatalf("沒寫：%+v", got)
	}
	path := filepath.Join(dir, "settings", "sessions.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# 我的註解\nschema = 1\nwork_max = 2 # 舊的\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, raw := post(t, h, "/api/system/set_work_max", &sdk.SystemSetWorkMaxRequest{WorkMax: 5, By: owner}); code != 200 {
		t.Fatalf("set_work_max → %d %s", code, raw)
	}
	if got := get(); got.WorkMax != 5 || !got.Set {
		t.Fatalf("改完要立刻讀得到：%+v", got)
	}
	if b, _ := os.ReadFile(path); string(b) != "# 我的註解\nschema = 1\nwork_max = 5 # 舊的\n" {
		t.Fatalf("只動那一行：%q", b)
	}
	before, _ := os.ReadFile(path)
	for name, req := range map[string]*sdk.SystemSetWorkMaxRequest{
		"秘書不能改": {WorkMax: 4, By: &sdk.Actor{Name: "code"}},
		"太大":    {WorkMax: 11, By: owner},
		"太小":    {WorkMax: 0, By: owner},
		"沒有操作者": {WorkMax: 4},
	} {
		if code, raw := post(t, h, "/api/system/set_work_max", req); code == 200 {
			t.Errorf("%s：應該被拒絕，拿到 200 %s", name, raw)
		}
	}
	if after, _ := os.ReadFile(path); string(before) != string(after) {
		t.Fatalf("被拒絕的請求不能改檔：%q", after)
	}
}
