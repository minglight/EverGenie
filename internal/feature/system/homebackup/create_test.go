package homebackup

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

var testNow = time.Date(2026, 11, 16, 10, 30, 0, 0, time.UTC)

// entries 把備份檔解開讀成「路徑 → 內容」（只看一般檔）。
func entries(t *testing.T, archive string) map[string]string {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[hdr.Name] = string(b)
	}
}

func names(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// 備份檔只裝主人的東西加說明檔：兩種佈局收出來的路徑一樣；log、官方角色、別人的套件、秘密檔裡推播以外的東西都不在裡面。
func TestCreateHoldsOwnerDataOnly(t *testing.T) {
	want := []string{
		"home/home.toml", "home/media/art/pic.png", "home/media/share/photo.png", "home/packages/mine/diary/package.toml",
		"home/personas/mine/aki/card.md", "home/settings/notify.toml", "home/vault/notes/日記.md", "home/vault/tasks/T-1.md",
		"manifest.json", "secrets/push.toml",
	}
	for _, v2 := range []bool{false, true} {
		home := fakeHome(t, "myhome", v2, 18001)
		c, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow})
		if err != nil {
			t.Fatalf("v2=%v: %v", v2, err)
		}
		if c.Name != "myhome-20261116-103000.egbackup" || filepath.Dir(c.Path) != platform.HomeBackupDir(home, sdk.HomeBackupKindManual) {
			t.Fatalf("v2=%v: wrong place or name: %s", v2, c.Path)
		}
		got := entries(t, c.Path)
		if strings.Join(names(got), ",") != strings.Join(want, ",") {
			t.Fatalf("v2=%v: entries = %v", v2, names(got))
		}
		if s := got["secrets/push.toml"]; !strings.Contains(s, "PRIV-myhome") || strings.Contains(s, "不該進備份") {
			t.Fatalf("v2=%v: secrets entry should hold only the push keys: %q", v2, s)
		}
		m := c.Manifest
		if m.Format != FormatVersion || m.HomeName != "myhome" || m.Owner != "測試主人" || m.Machine != "myhome" || m.HomeDir != home || m.Kind != sdk.HomeBackupKindManual {
			t.Fatalf("v2=%v: manifest = %+v", v2, m)
		}
		if len(m.Files) != len(want)-1 || m.TotalFiles() != len(want)-1 {
			t.Fatalf("v2=%v: manifest lists %d files, sections count %d", v2, len(m.Files), m.TotalFiles())
		}
		if strings.Join(m.Installed, ",") != "shared-pkg" {
			t.Fatalf("v2=%v: installed packages should be named, not included: %v", v2, m.Installed)
		}
		if again, err := Inspect(c.Path); err != nil || again.TotalBytes() != m.TotalBytes() {
			t.Fatalf("v2=%v: finished backup does not read back: %v", v2, err)
		}
	}
}

// 符號連結不跟著走也不收，記在說明檔；.DS_Store 不收。
func TestCreateSkipsSymlinksAndJunk(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	outside := filepath.Join(filepath.Dir(home), "outside.txt")
	write(t, outside, "外面的檔")
	if err := os.Symlink(outside, filepath.Join(home, "vault", "link.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(home, "vault", ".DS_Store"), "junk")
	c, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	got := entries(t, c.Path)
	if _, ok := got["home/vault/link.txt"]; ok {
		t.Fatal("symlink was followed into the backup")
	}
	if _, ok := got["home/vault/.DS_Store"]; ok {
		t.Fatal(".DS_Store was included")
	}
	if strings.Join(c.Manifest.Skipped, ",") != "home/vault/link.txt" {
		t.Fatalf("skipped = %v", c.Manifest.Skipped)
	}
}

// 沒做成不留半個檔；同一秒做兩份檔名不撞。
func TestCreateLeavesNoPartialFileAndNeverOverwrites(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	a, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name == b.Name {
		t.Fatalf("second backup overwrote the first: %s", b.Name)
	}
	if _, err := Create(CreateOptions{Home: filepath.Join(filepath.Dir(home), "nohome"), Kind: sdk.HomeBackupKindManual, Now: testNow}); err == nil {
		t.Fatal("backing up a folder that is not a home should fail")
	}
	dir := platform.HomeBackupDir(home, sdk.HomeBackupKindManual)
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("partial file left behind: %s", e.Name())
		}
	}
}

// craft 寫一個手工的備份檔，測讀檔那一側的保險。
func craft(t *testing.T, items []tar.Header, bodies []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.egbackup")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for i := range items {
		h := items[i]
		h.Size = int64(len(bodies[i]))
		h.Mode = 0o644
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(bodies[i])); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// 不是備份檔、被改過、夾了會寫到家外面的路徑或符號連結的檔，一律不收，而且給的是白話。
func TestInspectRejectsBadArchives(t *testing.T) {
	manifest := `{"format":1,"product":"` + platform.ProductName + `","files":[]}`
	reg := byte(tar.TypeReg)
	cases := map[string]string{
		"not an archive": func() string {
			p := filepath.Join(t.TempDir(), "a.egbackup")
			write(t, p, "hello")
			return p
		}(),
		"no manifest":      craft(t, []tar.Header{{Name: "home/home.toml", Typeflag: reg}}, []string{"x"}),
		"path escapes":     craft(t, []tar.Header{{Name: "home/../../evil", Typeflag: reg}, {Name: "manifest.json", Typeflag: reg}}, []string{"x", manifest}),
		"absolute path":    craft(t, []tar.Header{{Name: "/etc/evil", Typeflag: reg}, {Name: "manifest.json", Typeflag: reg}}, []string{"x", manifest}),
		"symlink entry":    craft(t, []tar.Header{{Name: "home/vault/l", Typeflag: tar.TypeSymlink, Linkname: "/etc"}, {Name: "manifest.json", Typeflag: reg}}, []string{"", manifest}),
		"unknown location": craft(t, []tar.Header{{Name: "home/runtime/x", Typeflag: reg}, {Name: "manifest.json", Typeflag: reg}}, []string{"x", manifest}),
		"unlisted file":    craft(t, []tar.Header{{Name: "home/vault/x", Typeflag: reg}, {Name: "manifest.json", Typeflag: reg}}, []string{"x", manifest}),
		"no product":       craft(t, []tar.Header{{Name: "manifest.json", Typeflag: reg}}, []string{`{"format":1,"files":[]}`}),
	}
	for name, path := range cases {
		_, err := Inspect(path)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "不是備份檔") {
			t.Errorf("%s: want a plain invalid error, got %v", name, err)
		}
	}
	// 說明檔的產品名不是這一版的：當成改名前的舊版備份，照樣不收，另一句白話（TestLegacyBackupIsListedButNotRestorable）。
	other := craft(t, []tar.Header{{Name: "manifest.json", Typeflag: reg}}, []string{`{"format":1,"product":"other","files":[]}`})
	if _, err := Inspect(other); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "改名前的舊版備份") {
		t.Errorf("other product: want the before-rename note, got %v", err)
	}
}

// 備份檔裡的內容被改掉一個位元組（檢查碼對不上）就不收。
func TestInspectDetectsCorruption(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	c, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	got := entries(t, c.Path)
	var hdrs []tar.Header
	var bodies []string
	for _, n := range names(got) {
		if n == manifestName {
			continue
		}
		body := got[n]
		if n == "home/vault/tasks/T-1.md" {
			body = "被改過的單\n"
		}
		hdrs, bodies = append(hdrs, tar.Header{Name: n, Typeflag: tar.TypeReg}), append(bodies, body)
	}
	hdrs, bodies = append(hdrs, tar.Header{Name: manifestName, Typeflag: tar.TypeReg}), append(bodies, got[manifestName])
	if _, err := Inspect(craft(t, hdrs, bodies)); err == nil {
		t.Fatal("tampered backup accepted")
	}
}
