package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLayoutV1KeepsEverythingInHome(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte("schema = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := LayoutFor(home)
	if l.V2 {
		t.Fatal("沒寫 [layout] 應該是第 1 版")
	}
	for got, want := range map[string]string{
		l.Runtime("sockets"):          filepath.Join(home, "runtime", "sockets"),
		l.Secrets("push.toml"):        filepath.Join(home, "secrets", "push.toml"),
		l.Shares():                    filepath.Join(home, "runtime", "share"),
		l.Media("art"):                filepath.Join(home, "runtime", "art"),
		l.OfficialPersonas("avatars"): filepath.Join(home, "personas", "avatars"),
		l.InstalledPackages():         filepath.Join(home, "packages", "installed"),
	} {
		if got != want {
			t.Errorf("got %s want %s", got, want)
		}
	}
}

func TestLayoutV2MovesRuntimeOutOfHome(t *testing.T) {
	home := t.TempDir()
	state := t.TempDir()
	cfg := "schema = 1\n[layout]\nversion = 2\nstate = \"" + state + "\"\n"
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	l := LayoutFor(home)
	if !l.V2 || l.State != state {
		t.Fatalf("layout = %+v", l)
	}
	for got, want := range map[string]string{
		l.Runtime("sockets", "api.sock"): filepath.Join(state, "runtime", "sockets", "api.sock"),
		l.Secrets():                      filepath.Join(state, "secrets"),
		l.Shares():                       filepath.Join(home, "media", "share"),
		l.Media("art"):                   filepath.Join(home, "media", "art"),
		l.OfficialPersonas():             filepath.Join(state, "personas"),
		l.InstalledPackages("x"):         filepath.Join(state, "packages", "installed", "x"),
	} {
		if got != want {
			t.Errorf("got %s want %s", got, want)
		}
	}
	env := strings.Join(l.Env(), "\n")
	if !strings.Contains(env, "EVERGENIE_STATE="+state) || !strings.Contains(env, "EVERGENIE_MEDIA="+filepath.Join(home, "media")) {
		t.Fatalf("env = %s", env)
	}
}

func TestLayoutV2DefaultState(t *testing.T) {
	l := NewLayout("/x/evergenie-amy", LayoutConfig{Version: 2})
	if !strings.HasSuffix(l.State, filepath.Join(".evergenie", "state", "evergenie-amy")) {
		t.Fatalf("state = %s", l.State)
	}
}

// v2HomeDir 建一個第 2 版的家（狀態資料夾在另一個暫存資料夾），回家與狀態資料夾。
func v2HomeDir(t *testing.T) (string, string) {
	t.Helper()
	dir, state := t.TempDir(), t.TempDir()
	cfg := "schema = 2\n" + cfgHomeToml + "[layout]\nversion = 2\nstate = \"" + state + "\"\n"
	if err := os.WriteFile(HomePath(dir), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, state
}

// TestLayoutV2LogsAndBackupsLeaveHome：第 2 版的 log 與設定備份寫在狀態資料夾，家裡不會長出 runtime/。
func TestLayoutV2LogsAndBackupsLeaveHome(t *testing.T) {
	dir, state := v2HomeDir(t)
	h, err := LoadHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	NewLogger(h, "test").Info("hello")
	if logs, _ := filepath.Glob(filepath.Join(state, "runtime", "logs", "*.jsonl")); len(logs) != 1 {
		t.Fatalf("log 要在狀態資料夾：%v", logs)
	}
	backup, _, err := SetHomeValue(dir, "user.name", `"Amy"`, time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(backup) || !strings.HasPrefix(backup, filepath.Join(state, "runtime", "backups", "home")+string(filepath.Separator)) {
		t.Fatalf("備份要在狀態資料夾、回絕對路徑：%s", backup)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("家裡不該有 runtime/：%v", err)
	}
	if got := PersonaUploadDir(dir); got != filepath.Join(state, "runtime", "persona-import", "uploads") {
		t.Fatalf("匯入上傳：%s", got)
	}
}

// schema 跟著佈局走（DEV-145）：schema = 2 只在 [layout] version = 2；第 2 版要寫 schema = 2；official_personas 只收 "home"。
func TestHomeSchemaMatchesLayout(t *testing.T) {
	cases := []struct {
		cfg string
		ok  bool
	}{
		{"schema = 1\n" + cfgHomeToml, true},
		{"schema = 2\n" + cfgHomeToml, false},
		{"schema = 1\n" + cfgHomeToml + "[layout]\nversion = 2\nstate = \"/tmp/s\"\n", false},
		{"schema = 2\n" + cfgHomeToml + "[layout]\nversion = 2\nstate = \"/tmp/s\"\n", true},
		{"schema = 2\n" + cfgHomeToml + "[layout]\nversion = 2\nstate = \"/tmp/s\"\nofficial_personas = \"home\"\n", true},
		{"schema = 2\n" + cfgHomeToml + "[layout]\nversion = 2\nstate = \"/tmp/s\"\nofficial_personas = \"state\"\n", false},
	}
	for i, c := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(HomePath(dir), []byte(c.cfg), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadHome(dir)
		if (err == nil) != c.ok {
			t.Errorf("第 %d 個：ok=%v err=%v", i, c.ok, err)
		}
	}
}

func TestSecretsPathDotSlash(t *testing.T) {
	dir, state := v2HomeDir(t)
	h, err := LoadHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	h.Secrets.File = "./secrets/push.toml"
	if got, want := h.SecretsPath(), filepath.Join(state, "secrets", "push.toml"); got != want {
		t.Errorf("./secrets/ 也要接在狀態資料夾：%s want %s", got, want)
	}
}
