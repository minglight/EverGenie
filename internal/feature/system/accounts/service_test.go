package accounts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

const homeToml = `[user]
name = "測試使用者"
timezone = "Asia/Taipei"
[slots]
default = "life"
[slots.life]
label = "日常"
remind = "08:30"
check = "18:00"
[accounts]
default = "main"
[accounts.main]
engine = "test-engine"
[web]
port = 18850
`

func accountsHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(platform.HomePath(dir), []byte(homeToml), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.LoadHome(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// 新增帳號、設成預設、更新時沒在畫面上的 engine_config 欄位保留、不合格的輸入被拒絕。
func TestSaveAddsUpdatesAndRejectsBadInput(t *testing.T) {
	dir := accountsHome(t)
	now := time.Now()
	if _, err := Save(dir, Info{Name: "work", Engine: "test-engine", ConfigDir: "~/.acct-work", RemoteAccess: true}, true, now); err != nil {
		t.Fatal(err)
	}
	list, err := List(dir)
	if err != nil || len(list) != 2 {
		t.Fatalf("應有兩個帳號：%v %v", list, err)
	}
	var work Info
	for _, a := range list {
		if a.Name == "work" {
			work = a
		}
	}
	home, _ := os.UserHomeDir()
	if !work.Default || !work.RemoteAccess || work.ConfigDir != filepath.Join(home, ".acct-work") {
		t.Fatalf("work 帳號不對：%+v", work)
	}
	if _, _, err := platform.SetHomeValue(dir, "accounts.work.engine_config.extra", `"keep"`, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(dir, Info{Name: "work", Engine: "test-engine", ConfigDir: "/tmp/w2"}, false, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if v, _, err := platform.GetHomeValue(dir, "accounts.work.engine_config.extra"); err != nil || !strings.Contains(v, "keep") {
		t.Fatalf("extra 應保留：%q %v", v, err)
	}
	for _, bad := range []Info{
		{Name: "default", Engine: "e", ConfigDir: "/x"},
		{Name: "Bad Name", Engine: "e", ConfigDir: "/x"},
		{Name: "ok", Engine: "", ConfigDir: "/x"},
		{Name: "ok", Engine: "e", ConfigDir: "relative"},
	} {
		if _, err := Save(dir, bad, false, now); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Fatalf("%+v 應被拒絕：%v", bad, err)
		}
	}
}
