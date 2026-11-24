package update

import (
	"os"
	"path/filepath"
	"testing"
)

// testHome 建一個最小的家（只有 home.toml 與 settings/）。
func testHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	toml := "[user]\nname = \"測試\"\ntimezone = \"Asia/Taipei\"\n[slots]\ndefault = \"life\"\n[slots.life]\nlabel = \"日常\"\nremind = \"08:30\"\ncheck = \"18:00\"\n[accounts]\ndefault = \"main\"\n[accounts.main]\nengine = \"x\"\n[web]\nport = 18850\n"
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "settings"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}
