package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

const cfgHomeToml = `[user]
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

func cfgHomeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(HomePath(dir), []byte(cfgHomeToml), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHome(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestConfigSetBacksUpAndRestores：config set 先備份舊檔；寫完重讀驗證，不合格就還原成改之前的樣子。
// Guards: REQ-163
func TestConfigSetBacksUpAndRestores(t *testing.T) {
	dir := cfgHomeDir(t)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	if v, _, err := GetHomeValue(dir, "user.timezone"); err != nil || v != `"Asia/Taipei"` {
		t.Fatalf("get：%q %v", v, err)
	}
	if _, _, err := GetHomeValue(dir, "user.nothing"); CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("不存在的設定：%v", err)
	}
	if _, text, err := GetHomeValue(dir, ""); err != nil || !strings.Contains(text, "測試使用者") {
		t.Fatalf("show：%v", err)
	}

	before, _ := os.ReadFile(HomePath(dir))
	backup, value, err := SetHomeValue(dir, "user.name", `"Amy"`, now)
	if err != nil || value != `"Amy"` {
		t.Fatalf("set：%q %v", value, err)
	}
	if saved, err := os.ReadFile(filepath.Join(dir, backup)); err != nil || string(saved) != string(before) {
		t.Fatalf("備份要是改之前的內容：%v", err)
	}
	if h, err := LoadHome(dir); err != nil || h.User.Name != "Amy" {
		t.Fatalf("改完要讀得到：%+v %v", h, err)
	}

	// 改壞：port 寫成文字，重讀驗證不過 → 還原成改壞之前（也就是上一個好的版本）。
	good, _ := os.ReadFile(HomePath(dir))
	backup, _, err = SetHomeValue(dir, "web.port", `"not-a-port"`, now.Add(time.Second))
	if err == nil {
		t.Fatal("web.port 寫成文字應該被拒絕")
	}
	if after, _ := os.ReadFile(HomePath(dir)); string(after) != string(good) {
		t.Fatalf("失敗後要還原：\n%s", after)
	}
	if backup == "" {
		t.Fatal("失敗時也要留下備份路徑，方便找舊檔")
	}

	// 不能用指令改的與寫法錯誤。
	if _, _, err := SetHomeValue(dir, "schema", `2`, now); CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("schema 不能改：%v", err)
	}
	if _, _, err := SetHomeValue(dir, "user..name", `"x"`, now); err == nil {
		t.Fatal("key 寫法錯誤要拒絕")
	}
	if _, _, err := SetHomeValue(dir, "user.name.deeper", `"x"`, now); CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("往字串底下設：%v", err)
	}
}

// Guards: REQ-130
func TestPersonaStyleSettingValidated(t *testing.T) {
	dir := cfgHomeDir(t)
	now := time.Now()
	for _, level := range []string{"a", "b", "c", "d", "custom"} {
		if _, _, err := SetHomeValue(dir, "persona.style", `"`+level+`"`, now); err != nil {
			t.Fatalf("%s：%v", level, err)
		}
		if h, err := LoadHome(dir); err != nil || h.Persona.Style != level {
			t.Fatalf("%s：%v %v", level, h, err)
		}
	}
	before, _ := os.ReadFile(HomePath(dir))
	if _, _, err := SetHomeValue(dir, "persona.style", `"loud"`, now.Add(time.Second)); CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("不認得的強度要被擋：%v", err)
	}
	if after, _ := os.ReadFile(HomePath(dir)); string(after) != string(before) {
		t.Fatal("不合格的設定要還原")
	}
}
