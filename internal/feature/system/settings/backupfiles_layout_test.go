package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBackupsLayoutV2：第 2 版的家（DEV-145），備份在狀態資料夾的 runtime/backups/；列出的路徑照樣是 runtime/backups/…（相對狀態資料夾），
// 刪也刪在那裡；家裡的 runtime/backups/（舊的）不看。
func TestBackupsLayoutV2(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte(fmt.Sprintf("[layout]\nversion = 2\nstate = %q\n", state)), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	putBackup(t, state, "trust/a.toml", time.Hour, now)
	putBackup(t, home, "trust/old.toml", time.Hour, now)
	list, err := ListBackups(home)
	if err != nil || len(list) != 1 || list[0].Path != filepath.Join("runtime", "backups", "trust", "a.toml") || list[0].Group != "trust" {
		t.Fatalf("列出：%+v %v", list, err)
	}
	if err := DeleteBackups(home, []string{list[0].Path}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "runtime", "backups", "trust", "a.toml")); !os.IsNotExist(err) {
		t.Fatalf("要刪狀態資料夾裡的那份：%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "runtime", "backups", "trust", "old.toml")); err != nil {
		t.Fatal("家裡的不該動")
	}
}
