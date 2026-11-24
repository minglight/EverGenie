package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

// doctorHome 把 fixture 家整理成「全部正常」的樣子：引擎帳號改用有登記的 id（執行檔放一個空的占位檔）、
// 推播金鑰與一台裝置都設好。tmp 裡的資料全是人造的。
func doctorHome(t *testing.T) string {
	t.Helper()
	dir := copyFixtureHome(t, false)
	home, err := os.ReadFile(filepath.Join(dir, "home.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(home), `engine = "test-engine"`, `engine = "fake"`, 1)
	text += "\n[incidents]\nowner = \"code\"\n[secrets]\nfile = \"push-secrets.toml\"\n"
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("home.toml", text)
	write("push-secrets.toml", "[webpush]\nvapid_public_key='synthetic-public'\nvapid_private_key='synthetic-private'\nvapid_subject='https://board.example'\n")
	write("settings/push.toml", "[[subscription]]\nendpoint='https://push.example/a'\np256dh='k1'\nauth='a1'\n")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "evergenie-engine-fake"), nil, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVERGENIE_ENGINE_BIN_DIR", bin)
	return dir
}

// startDoctorAPI 起全部模組，並把假引擎直接接到 fake 這個引擎 id 上（不經行程監督）。
func startDoctorAPI(t *testing.T, dir string) (*fake.Engine, string) {
	t.Helper()
	m, err := buildModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Cleanup(func() {
		if err := m.app.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := m.app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	engineAPI := fake.New(nil)
	if err := m.engines.Attach("fake", engineAPI); err != nil {
		t.Fatal(err)
	}
	return engineAPI, filepath.Join(dir, "runtime/sockets/api.sock")
}

func doctorJSON(t *testing.T, sock, dir string) (int, []sdk.DoctorCheck) {
	t.Helper()
	code, out, errOut := hg(t, sock, "", "doctor", "--json", "--home", dir)
	var res sdk.SystemDoctorResponse
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("doctor --json 輸出不是 JSON：%q %q %v", out, errOut, err)
	}
	return code, res.Checks
}

// Guards: REQ-117
func TestDoctorHealthyHome(t *testing.T) {
	dir := doctorHome(t)
	_, sock := startDoctorAPI(t, dir)
	code, out, errOut := hg(t, sock, "", "doctor", "--home", dir)
	if code != 0 || errOut != "" {
		t.Fatalf("正常的家退出碼 0：%d\n%s\n%s", code, out, errOut)
	}
	_, checks := doctorJSON(t, sock, dir)
	seen := map[sdk.DoctorGroup]bool{}
	for _, c := range checks {
		seen[c.Group] = true
		if !c.Ok || c.Severity != sdk.DoctorSeverityOk {
			t.Errorf("正常的家全部要是 ok：%+v", c)
		}
	}
	for _, g := range []sdk.DoctorGroup{sdk.DoctorGroupConfig, sdk.DoctorGroupSessions, sdk.DoctorGroupPackages, sdk.DoctorGroupPermissions, sdk.DoctorGroupSchedule, sdk.DoctorGroupData, sdk.DoctorGroupPush} {
		if !seen[g] {
			t.Errorf("group %s 至少要有一條", g)
		}
	}
	if !strings.Contains(out, "還沒安裝成 launchd 服務（切換前正常") || !strings.Contains(out, "項要處理") || strings.Contains(out, "→") {
		t.Errorf("輸出：\n%s", out)
	}
	if strings.Contains(out, "synthetic") || strings.Contains(out, "push.example") {
		t.Errorf("輸出不能帶金鑰或裝置網址：\n%s", out)
	}
}

// Guards: REQ-117
func TestDoctorFindsBrokenHome(t *testing.T) {
	dir := doctorHome(t)
	// 資料：一張 schema 驗證不過的任務單（status 不是合法值）。
	bad := "---\nschema: 1\ncreated_at: 2026-10-23T12:00:00+08:00\nupdated_at: 2026-10-23T12:00:00+08:00\nby: system\nid: SEC-77\ntitle: 壞掉的單\ntype: task\nagent: code\nstatus: sleeping\nnudges: 0\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "vault/tasks/SEC-77.md"), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	engineAPI, sock := startDoctorAPI(t, dir)
	// 權限：假引擎說產物被手改過。
	engineAPI.Configure(fake.Behavior{Delivered: true, Drift: []string{"設定檔內容與目前的政策不一致（可能被手改）"}})
	// 設定：拿掉 home.toml（serve 已經載入，所以只有離線那條會發現）。
	if err := os.Remove(filepath.Join(dir, "home.toml")); err != nil {
		t.Fatal(err)
	}

	code, checks := doctorJSON(t, sock, dir)
	if code != 1 {
		t.Fatalf("有要處理的項目退出碼 1：%d", code)
	}
	errs := map[string]sdk.DoctorCheck{}
	for _, c := range checks {
		if c.Severity == sdk.DoctorSeverityError {
			errs[c.ID] = c
		}
	}
	if c, ok := errs["config.home"]; !ok || c.Group != sdk.DoctorGroupConfig || !strings.Contains(c.Detail, "家的設定讀不起來") || c.Fix == "" {
		t.Errorf("拿掉 home.toml 要有一條離線的 config.home：%+v", errs)
	}
	if c, ok := errs["permissions.drift"]; !ok || c.Group != sdk.DoctorGroupPermissions || !strings.Contains(c.Detail, "被手改") {
		t.Errorf("假引擎的 Drift 要變成 permissions 的 error：%+v", errs)
	}
	if c, ok := errs["data.invalid"]; !ok || c.Group != sdk.DoctorGroupData || !strings.Contains(c.Detail, "SEC-77") || !strings.Contains(c.Detail, "status") {
		t.Errorf("壞掉的任務單要有一條 data 的 error：%+v", errs)
	}
	if len(errs) != 3 {
		t.Errorf("恰好三處問題，各一條：%+v", errs)
	}
	// 純文字輸出也有同樣三條、摘要、退出碼 1。
	code, out, _ := hg(t, sock, "", "doctor", "--home", dir)
	if code != 1 || !strings.Contains(out, "✗") || !strings.Contains(out, "3 項要處理") {
		t.Errorf("文字輸出：%d\n%s", code, out)
	}
}

// Guards: REQ-117
func TestDoctorServeDown(t *testing.T) {
	dir := doctorHome(t) // 家是好的，但沒有起 serve
	code, out, _ := hg(t, filepath.Join(dir, "runtime/sockets/api.sock"), "", "doctor", "--home", dir)
	if code != 1 || !strings.Contains(out, "✗ EverGenie 沒在跑") || !strings.Contains(out, "→ 先等一分鐘，它當掉會自己重開") || !strings.Contains(out, "evergenie 修復") {
		t.Fatalf("serve 沒在跑：%d\n%s", code, out)
	}
	if strings.Contains(out, "【資料】") || strings.Contains(out, "【排程】") {
		t.Errorf("沒有 serve 時只該有離線檢查：\n%s", out)
	}
	if !strings.Contains(out, "home.toml 讀得起來") || !strings.Contains(out, "引擎執行檔都找得到") {
		t.Errorf("離線檢查要照常跑：\n%s", out)
	}
	if !strings.Contains(out, "1 項要處理") {
		t.Errorf("只有「沒在跑」這一條要處理：\n%s", out)
	}
}

// Guards: REQ-117
func TestDoctorOfflineWithoutHome(t *testing.T) {
	t.Setenv("EVERGENIE_HOME", "")
	t.Setenv("EVERGENIE_API_SOCKET", "")
	code, out, _ := hg(t, "/tmp/hg-nonexistent/api.sock", "", "doctor")
	if code != 1 || !strings.Contains(out, "不知道家在哪裡") {
		t.Fatalf("沒有家要說明白：%d\n%s", code, out)
	}
}

// Guards: REQ-121

// serverToml 是一份合格的 servers/<名>.toml（秘書 code 在 fixture 家裡存在）。
func serverToml(name, role string) string {
	body := "schema = 1\nname = \"" + name + "\"\nrole = \"" + role + "\"\ntailnet_id = \"n" + name + "CNTRL\"\nagents = [\"code\"]\n"
	body += "url = \"http://192.0.2.9:8860\"\n"
	if role == "node" {
		body += "ssh = \"my-" + name + "\"\n"
	}
	return body
}

// Guards: REQ-177
