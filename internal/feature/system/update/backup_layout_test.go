package update

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
)

func tgzNames(t *testing.T, p string) []string {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var out []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(h.Name, "/") {
			out = append(out, h.Name)
		}
	}
	sort.Strings(out)
	return out
}

func put(t *testing.T, p string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBackupHomeLayoutV2：第 2 版的家（DEV-145），升級前的整家備份放狀態資料夾的 runtime/backups/，只收家裡的東西：
// 主人的 media/ 不收（升級不動它、又大）；狀態資料夾就算設在家裡面也整個不收（包括備份自己）。
func TestBackupHomeLayoutV2(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte("[layout]\nversion = 2\nstate = \".state\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(home, "vault", "task", "T-1.md"), 10)
	put(t, filepath.Join(home, "personas", "mine", "mina.md"), 10)
	put(t, filepath.Join(home, "media", "share", "small.txt"), 10)
	put(t, filepath.Join(home, "media", "art", "big.png"), 2<<20)
	put(t, filepath.Join(home, ".state", "runtime", "logs", "serve.log"), 10)
	put(t, filepath.Join(home, ".state", "secrets", "push.toml"), 10)
	put(t, filepath.Join(home, ".state", "personas", "athena.md"), 10)

	u := &Upgrader{Home: home, BackupMaxMB: 1}
	dest := platform.RuntimeDir(home, "backups", "upgrade-a-b.tgz")
	if want := filepath.Join(home, ".state", "runtime", "backups", "upgrade-a-b.tgz"); dest != want {
		t.Fatalf("備份位置：%s", dest)
	}
	if err := u.backupHome(dest); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(tgzNames(t, dest), ",")
	if want := "home.toml,personas/mine/mina.md,vault/task/T-1.md"; got != want {
		t.Fatalf("備份內容：%s，要 %s", got, want)
	}
}
