package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWorkMaxReadDefaults：沒檔、沒寫、不合理都是預設 3（set＝false）；讀不懂另外回錯誤。
// Guards: SES-73
func TestWorkMaxReadDefaults(t *testing.T) {
	home := t.TempDir()
	if n, set, err := ReadWorkMax(home); n != DefaultWorkMax || set || err != nil {
		t.Fatalf("沒檔：%d %v %v", n, set, err)
	}
	write := func(s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(home, "settings"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(WorkSettingsPath(home), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("schema = 1\n")
	if n, set, _ := ReadWorkMax(home); n != DefaultWorkMax || set {
		t.Fatalf("沒寫：%d %v", n, set)
	}
	write("work_max = 0\n")
	if n, set, _ := ReadWorkMax(home); n != DefaultWorkMax || set {
		t.Fatalf("不合理：%d %v", n, set)
	}
	write("work_max = 20\n") // 手改的大值照樣生效（看板只是不讓改到那麼大）
	if n, set, _ := ReadWorkMax(home); n != 20 || !set {
		t.Fatalf("手改的：%d %v", n, set)
	}
	write("work_max = = =\n")
	if n, set, err := ReadWorkMax(home); n != DefaultWorkMax || set || err == nil {
		t.Fatalf("讀不懂：%d %v %v", n, set, err)
	}
}

// TestWorkMaxWriteKeepsTheRest：只改 work_max 那一行的值（註解、空行、其他欄位、表格都不動）；沒寫就加在最上層；沒檔就新建；
// 範圍外白話拒絕；原本讀不懂的檔不改；原檔有備份。
// Guards: SES-73
func TestWorkMaxWriteKeepsTheRest(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	home := t.TempDir()

	// 沒檔：新建
	if backup, err := WriteWorkMax(home, 5, now); err != nil || backup != "" {
		t.Fatalf("新建：%q %v", backup, err)
	}
	if got := read(t, WorkSettingsPath(home)); got != "schema = 1\nwork_max = 5\n" {
		t.Fatalf("新檔內容：%q", got)
	}

	cases := []struct{ name, old, want string }{
		{
			"改那一行、留註解",
			"# 工作對話上限\nschema = 1\nwork_max   =  3   # 主人定的\n\n# 其他\nfoo = \"bar\"\n",
			"# 工作對話上限\nschema = 1\nwork_max   =  7   # 主人定的\n\n# 其他\nfoo = \"bar\"\n",
		},
		{
			"沒寫：加在第一個表格前",
			"schema = 1\n# 說明\n\n[extra]\nwork_max = 9\n",
			"schema = 1\n# 說明\n\nwork_max = 7\n[extra]\nwork_max = 9\n",
		},
		{
			"沒寫、最後沒換行",
			"schema = 1",
			"schema = 1\nwork_max = 7\n",
		},
		{
			"表格裡同名的不算",
			"[extra]\nwork_max = 9\n",
			"work_max = 7\n[extra]\nwork_max = 9\n",
		},
	}
	for _, c := range cases {
		if err := os.WriteFile(WorkSettingsPath(home), []byte(c.old), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(WorkSettingsPath(home), 0o640); err != nil {
			t.Fatal(err)
		}
		backup, err := WriteWorkMax(home, 7, now)
		if err != nil {
			t.Fatalf("%s：%v", c.name, err)
		}
		if got := read(t, WorkSettingsPath(home)); got != c.want {
			t.Fatalf("%s：\n got %q\nwant %q", c.name, got, c.want)
		}
		if n, set, _ := ReadWorkMax(home); n != 7 || !set {
			t.Fatalf("%s：讀回來 %d %v", c.name, n, set)
		}
		if backup == "" || read(t, filepath.Join(home, backup)) != c.old {
			t.Fatalf("%s：備份 %q", c.name, backup)
		}
		if info, _ := os.Stat(WorkSettingsPath(home)); info.Mode().Perm() != 0o640 {
			t.Fatalf("%s：權限要照原檔：%v", c.name, info.Mode().Perm())
		}
	}

	for _, n := range []int{0, 11, -1} {
		_, err := WriteWorkMax(home, n, now)
		if err == nil || !strings.Contains(err.Error(), "只能設 1 到 10 個") {
			t.Fatalf("%d 要白話拒絕：%v", n, err)
		}
	}

	bad := "work_max = = =\n"
	if err := os.WriteFile(WorkSettingsPath(home), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteWorkMax(home, 4, now); err == nil || !strings.Contains(err.Error(), "讀不懂") {
		t.Fatalf("讀不懂的檔不改：%v", err)
	}
	if got := read(t, WorkSettingsPath(home)); got != bad {
		t.Fatalf("讀不懂的檔被改了：%q", got)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
