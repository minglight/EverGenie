package homebackup

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
)

// fakeHome 在暫存資料夾建一個假的家（不碰任何真的家）。v2＝第 2 版佈局（狀態資料夾在家外面）。
// 回家的路徑。裡面有：設定、筆記、自己的角色與套件、附件、推播金鑰，還有不該進備份的東西（log、官方角色、別人的套件）。
func fakeHome(t *testing.T, name string, v2 bool, port int) string {
	t.Helper()
	root := t.TempDir()
	// macOS 的暫存資料夾是符號連結（/var → /private/var）；先解開，路徑比對才一致。
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, name)
	toml := "[user]\nname = \"測試主人\"\ntimezone = \"Asia/Taipei\"\n[slots]\ndefault = \"life\"\n[slots.life]\nlabel = \"生活\"\nremind = \"08:30\"\ncheck = \"18:00\"\n" +
		"[accounts]\ndefault = \"main\"\n[accounts.main]\nengine = \"test-engine\"\n[web]\nport = " + itoa(port) + "\n" +
		"[sessions]\nautostart = false\n[runtime]\nmachine = \"" + name + "\"\n[notes]\nfolder = \"" + filepath.Join(home, "vault", "notes") + "\"\n"
	if v2 {
		toml = "schema = 2\n" + toml + "[layout]\nversion = 2\nstate = \"" + filepath.Join(root, "state-"+name) + "\"\n"
	}
	write(t, filepath.Join(home, "home.toml"), toml)
	write(t, filepath.Join(home, "settings", "notify.toml"), "quiet = true\n")
	write(t, filepath.Join(home, "vault", "tasks", "T-1.md"), "第一張單\n")
	write(t, filepath.Join(home, "vault", "notes", "日記.md"), "今天天氣很好\n")
	write(t, filepath.Join(home, "personas", "mine", "aki", "card.md"), "自己的角色\n")
	write(t, filepath.Join(home, "packages", "mine", "diary", "package.toml"), "name = \"diary\"\n")
	state := home
	media := filepath.Join(home, "runtime")
	if v2 {
		state = filepath.Join(root, "state-"+name)
		media = filepath.Join(home, "media")
	}
	write(t, filepath.Join(media, "share", "photo.png"), "PNGDATA")
	write(t, filepath.Join(media, "art", "pic.png"), "ARTDATA")
	write(t, filepath.Join(state, "secrets", "push.toml"), "[webpush]\npublic = \"PUB-"+name+"\"\nprivate = \"PRIV-"+name+"\"\n[other]\ntoken = \"不該進備份\"\n")
	// 不該進備份的東西
	runtime := filepath.Join(state, "runtime")
	write(t, filepath.Join(runtime, "logs", "2026-11-01.jsonl"), "log\n")
	if v2 {
		write(t, filepath.Join(state, "personas", "official", "card.md"), "官方角色\n")
		write(t, filepath.Join(state, "packages", "installed", "shared-pkg", "package.toml"), "name = \"shared-pkg\"\n")
	} else {
		write(t, filepath.Join(home, "personas", "official", "card.md"), "官方角色\n")
		write(t, filepath.Join(home, "packages", "installed", "shared-pkg", "package.toml"), "name = \"shared-pkg\"\n")
	}
	return home
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// snapshot 把一個資料夾底下每個檔的內容雜湊記下來（相對路徑 → 雜湊）；skip 開頭的相對路徑不看（暫存、備份資料夾）。
func snapshot(t *testing.T, dir string, skip ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		for _, s := range skip {
			if rel == s || strings.HasPrefix(rel, s+string(filepath.Separator)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameSnapshot(t *testing.T, what string, want, got map[string]string) {
	t.Helper()
	var diff []string
	for k, v := range want {
		if got[k] != v {
			diff = append(diff, "changed or missing: "+k)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			diff = append(diff, "extra: "+k)
		}
	}
	sort.Strings(diff)
	if len(diff) > 0 {
		t.Fatalf("%s: home differs:\n%s", what, strings.Join(diff, "\n"))
	}
}

// v1 的家：暫存與備份在 runtime/ 底下，比對家有沒有被動到時不看它們。
var volatile = []string{filepath.Join("runtime", "home-backups"), filepath.Join("runtime", "home-backup"), filepath.Join("runtime", "logs"), filepath.Join("runtime", platform.ServiceLockName)}
