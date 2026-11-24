package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const idsBaseHome = `[user]
name="測試使用者"
timezone="Asia/Taipei"
[slots]
default="life"
[slots.life]
label="日常"
remind="08:30"
check="18:00"
[accounts]
default="main"
[accounts.main]
engine="test-engine"
[web]
port=18850
`

func writeIDsHome(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(idsBaseHome+extra), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// 沒寫 [ids] 的家照舊是 SEC-、GOAL-；寫了就照家的字首認單號，別的字首不算單號。
func TestHomeIDPrefixesDefaultAndCustom(t *testing.T) {
	h, err := LoadHome(writeIDsHome(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if h.IDs.Task() != "SEC-" || h.IDs.Goal() != "GOAL-" || !h.IDs.IsTask("SEC-3") || h.IDs.IsTask("DEV-3") {
		t.Fatalf("預設字首：%+v", h.IDs)
	}
	ws := writeIDsHome(t, "[ids]\ntasks=\"DEV-\"\ngoals=\"DG-\"\n")
	h, err = LoadHome(ws)
	if err != nil {
		t.Fatal(err)
	}
	ids := h.IDs
	if !ids.IsTask("DEV-73") || ids.IsTask("SEC-1") || !ids.IsGoal("DG-1") || ids.IsGoal("GOAL-1") {
		t.Fatalf("工坊字首：%+v", ids)
	}
	if got := ids.GoalPattern().FindAllString("照 DG-2 跟 GOAL-3、XDG-4 做", -1); len(got) != 1 || got[0] != "DG-2" {
		t.Fatalf("在文字裡找 Goal：%v", got)
	}
	if got := ReadIDPrefixes(ws); got != ids {
		t.Fatalf("ReadIDPrefixes：%+v", got)
	}
	if got := ReadIDPrefixes(t.TempDir()); got.Task() != "SEC-" {
		t.Fatalf("沒有設定檔要回預設：%+v", got)
	}
}

// [ids] 寫錯（小寫、沒有 -、跟其他種類撞、兩個一樣）家就載不起來，說出是哪一個欄位。
func TestHomeIDPrefixesRejectBadValues(t *testing.T) {
	for extra, field := range map[string]string{
		"[ids]\ntasks=\"dev-\"\n":                  "ids.tasks",
		"[ids]\ntasks=\"DEV\"\n":                   "ids.tasks",
		"[ids]\ntasks=\"D3V-\"\n":                  "ids.tasks",
		"[ids]\ngoals=\"R-\"\n":                    "ids.goals",
		"[ids]\ntasks=\"GOAL-\"\n":                 "ids.goals",
		"[ids]\ntasks=\"WS-\"\ngoals=\"WS-\"\n":    "ids.goals",
		"[runtime]\nservice_label=\"bad label\"\n": "runtime.service_label",
	} {
		_, err := LoadHome(writeIDsHome(t, extra))
		if err == nil {
			t.Fatalf("%q 應該被擋", extra)
		}
		var e *Err
		if !errors.As(err, &e) || e.Field != field {
			t.Fatalf("%q：要指出 %s，得到 %v", extra, field, err)
		}
	}
}

// [web] service 是 Tailscale 服務名稱：寫成 svc:<名> 讀進來拿掉前綴；格式錯（大寫、底線、頭尾連字號）家照樣載得起來（不讓 serve 起不來、部署退回），
// ValidTailscaleService 判得出不合格，交給 doctor 與 tailscale 指令處理。
func TestHomeWebServiceNameLenientLoad(t *testing.T) {
	for raw, want := range map[string]string{"genie-dev-amy-mini": "genie-dev-amy-mini", "svc:genie-amy-mini": "genie-amy-mini"} {
		h, err := LoadHome(writeIDsHome(t, "service=\""+raw+"\"\n"))
		if err != nil || h.Web.Service != want || !ValidTailscaleService(h.Web.Service) {
			t.Fatalf("%q：%v %q", raw, err, h.Web.Service)
		}
	}
	for _, bad := range []string{"Genie", "genie_amy", "-genie", "genie-"} {
		h, err := LoadHome(writeIDsHome(t, "service=\""+bad+"\"\n"))
		if err != nil || h.Web.Service != bad || ValidTailscaleService(bad) {
			t.Fatalf("%q 要載得起來、但判成不合格：%v %q", bad, err, h.Web.Service)
		}
	}
}

// [runtime] 的 service_label、backup_dir 是核心自己用的（同一台兩個家各設各的），不交給引擎的 engine_config。
func TestHomeRuntimeServiceLabelAndBackupDir(t *testing.T) {
	h, err := LoadHome(writeIDsHome(t, "[runtime]\nservice_label=\"com.evergenie.workshop\"\nbackup_dir=\"~/.evergenie-backup-workshop\"\nengine_socket=\"hg-ws\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if h.Runtime.ServiceLabel != "com.evergenie.workshop" {
		t.Fatalf("service_label：%q", h.Runtime.ServiceLabel)
	}

	if _, ok := h.Runtime.Overrides["service_label"]; ok {
		t.Fatal("service_label 不該交給引擎")
	}
	if _, ok := h.Runtime.Overrides["backup_dir"]; ok {
		t.Fatal("backup_dir 不該交給引擎")
	}
	if h.Runtime.Overrides["engine_socket"] != "hg-ws" {
		t.Fatalf("其他欄位照舊交給引擎：%v", h.Runtime.Overrides)
	}
}
