package attach

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCheckFollowsLayout：附件檢查照家的佈局找分享資料夾：第 1 版 <家>/runtime/share/，第 2 版 <家>/media/share/（DEV-145）。
func TestCheckFollowsLayout(t *testing.T) {
	v1 := t.TempDir()
	writeFile(t, filepath.Join(v1, "home.toml"))
	writeFile(t, filepath.Join(v1, "runtime", "share", "a.png"))
	if got, err := Check(v1, "attachments", []string{"/share/a.png"}); err != nil || len(got) != 1 {
		t.Fatalf("第 1 版：%v %v", got, err)
	}

	v2 := t.TempDir()
	toml := fmt.Sprintf("[layout]\nversion = 2\nstate = %q\n", filepath.Join(t.TempDir(), "state"))
	if err := os.WriteFile(filepath.Join(v2, "home.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(v2, "runtime", "share", "old.png")) // 第 2 版不看 runtime/share
	writeFile(t, filepath.Join(v2, "media", "share", "b.png"))
	if got, err := Check(v2, "attachments", []string{"/share/b.png"}); err != nil || len(got) != 1 {
		t.Fatalf("第 2 版要找 media/share：%v %v", got, err)
	}
	if _, err := Check(v2, "attachments", []string{"/share/old.png"}); err == nil {
		t.Fatal("第 2 版不該認 runtime/share 的檔")
	}
}
