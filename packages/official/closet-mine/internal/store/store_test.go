package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// tree 列出 dir 底下所有檔案（相對路徑）。
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

// TestWritesOnlyUnderMine：只寫 personas/mine/ 底下；符號連結的 personas/mine、avatars、full 不寫；
// 路徑含 .. 或不合格的檔名不寫；官方檔已存在不覆蓋、位元組不變；已存在而且內容不同的圖不覆蓋。
// Guards: REQ-126
func TestWritesOnlyUnderMine(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	official := filepath.Join(home, "personas", "avatars", "athena.png")
	officialBytes := []byte("official-athena")
	mustWrite(t, official, officialBytes)
	mustWrite(t, filepath.Join(home, "personas", "athena.md"), []byte("---\nname: Athena\n---\n"))

	// 正常寫入：只在 personas/mine/ 底下，要的資料夾自己建。
	if w, err := s.PutNew("photos/myfox.png", []byte("fox")); err != nil || !w {
		t.Fatalf("PutNew：%v %v", w, err)
	}
	if _, err := s.PutNew("myfox.md", []byte("card")); err != nil {
		t.Fatal(err)
	}
	for _, f := range tree(t, filepath.Join(home, "personas")) {
		if !strings.HasPrefix(f, "mine/") && f != "avatars/athena.png" && f != "athena.md" {
			t.Errorf("不該出現：%s", f)
		}
	}
	// 沒有暫存檔殘留。
	for _, f := range tree(t, filepath.Join(home, "personas", "mine")) {
		if strings.Contains(f, ".tmp-") {
			t.Errorf("暫存檔殘留：%s", f)
		}
	}

	// 不合格的路徑一律不寫。
	for _, rel := range []string{
		"../avatars/athena.png", "avatars/../../athena.md", "avatars/../x.png", "/etc/passwd", "avatars//x.png", "avatars/x.exe",
		"avatars/.hidden.png", "avatars/UPPER.png", "avatars/a b.png", "full/x.png", "avatars/full/sub/x.png", "x.sh", "",
		"avatars/myfox@my-1.png", "avatars/full/myfox.png", "avatars/myfox.mov", "adult/athena.png", "photos/.x.png", "photos/a b.png",
	} {
		if _, err := s.PutNew(rel, []byte("bad")); err == nil {
			t.Errorf("PutNew(%q) 不該成功", rel)
		}
	}
	if got, _ := os.ReadFile(official); !bytes.Equal(got, officialBytes) {
		t.Fatal("官方圖的位元組變了")
	}

	// 已存在而且內容不同：不覆蓋（ErrExists）；相同＝重播成功、不重寫。
	if _, err := s.PutNew("photos/myfox.png", []byte("another")); !errors.Is(err, ErrExists) {
		t.Fatalf("不同內容要 ErrExists：%v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(home, "personas", "mine", "photos", "myfox.png")); string(got) != "fox" {
		t.Fatalf("既有的圖被覆蓋了：%q", got)
	}
	if w, err := s.PutNew("photos/myfox.png", []byte("fox")); err != nil || w {
		t.Fatalf("相同內容要當作已寫：%v %v", w, err)
	}
	// 目的檔本身是符號連結：不寫、不跟過去。
	secret := filepath.Join(t.TempDir(), "secret.png")
	mustWrite(t, secret, []byte("secret"))
	if err := os.Symlink(secret, filepath.Join(home, "personas", "mine", "photos", "link.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutNew("photos/link.png", []byte("overwrite")); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("符號連結檔要 ErrUnsafe：%v", err)
	}
	if got, _ := os.ReadFile(secret); string(got) != "secret" {
		t.Fatal("符號連結指到的檔被改了")
	}
}

// TestWritesOnlyUnderMineSymlinkedFolders：personas/mine、photos 任何一層是符號連結或檔案，一律不寫、目標資料夾不變。
// Guards: REQ-126
func TestWritesOnlyUnderMineSymlinkedFolders(t *testing.T) {
	for _, layer := range []string{"mine", "photos"} {
		home, outside := t.TempDir(), t.TempDir()
		mustWrite(t, filepath.Join(outside, "keep.txt"), []byte("keep"))
		s := New(home)
		personas := filepath.Join(home, "personas")
		if err := os.MkdirAll(personas, 0o755); err != nil {
			t.Fatal(err)
		}
		link := map[string]string{
			"mine":   filepath.Join(personas, "mine"),
			"photos": filepath.Join(personas, "mine", "photos"),
		}[layer]
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PutNew("photos/x.png", []byte("x")); !errors.Is(err, ErrUnsafe) {
			t.Errorf("%s 是符號連結：PutNew 要 ErrUnsafe，得到 %v", layer, err)
		}
		if got := tree(t, outside); len(got) != 1 || got[0] != "keep.txt" {
			t.Errorf("%s：符號連結指到的資料夾被寫入了：%v", layer, got)
		}
	}
	// 被換成一般檔案也不寫。
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "personas", "mine"), []byte("i am a file"))
	if _, err := New(home).PutNew("x.md", []byte("x")); !errors.Is(err, ErrUnsafe) {
		t.Errorf("personas/mine 是檔案：%v", err)
	}
}

// TestReadUploadOnlyRegularSmallFiles：只讀 runtime/share/ 底下單一檔名的一般檔案（≤ 20 MB）；符號連結、資料夾、超大、路徑跳脫一律當作找不到。
// Guards: REQ-126
func TestReadUploadOnlyRegularSmallFiles(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	share := filepath.Join(home, "runtime", "share")
	mustWrite(t, filepath.Join(share, "ok.png"), []byte("ok"))
	if data, err := s.ReadUpload("ok.png"); err != nil || string(data) != "ok" {
		t.Fatalf("正常讀取：%q %v", data, err)
	}
	secret := filepath.Join(t.TempDir(), "secret.png")
	mustWrite(t, secret, []byte("secret"))
	if err := os.Symlink(secret, filepath.Join(share, "link.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(share, "dir.png"), 0o755); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(share, "big.png")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxUploadBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	mustWrite(t, filepath.Join(home, "runtime", "outside.png"), []byte("outside"))
	for _, name := range []string{"link.png", "dir.png", "big.png", "missing.png", "../outside.png", "sub/ok.png", `a\b.png`, ".hidden", "", "a..b"} {
		if _, err := s.ReadUpload(name); !errors.Is(err, ErrNoUpload) {
			t.Errorf("ReadUpload(%q) 要 ErrNoUpload：%v", name, err)
		}
	}
}

// 新上傳的照片寫在 photos/<照片編號>.<副檔名>（DEV-160 S3）：只有一層、名字小寫英數與 _ -，不收 md 與子資料夾。
func TestValidRelAllowsPhotosFolder(t *testing.T) {
	for _, rel := range []string{"photos/athena-my-12--u15.png", "photos/my_fox-base--u3.jpg", "photos/athena-base--u9.mp4"} {
		if !ValidRel(rel) {
			t.Errorf("%s 要能寫", rel)
		}
	}
	for _, rel := range []string{"photos/sub/x.png", "photos/x.md", "photos/X.png", "photos/a@b.png", "photos/.x.png", "photos/../x.png"} {
		if ValidRel(rel) {
			t.Errorf("%s 不能寫", rel)
		}
	}
}

// 照片紀錄指著的檔（ValidFile）：舊檔名、大寫副檔名、自己的隱藏版資料夾、備份都認得；跳出 personas/mine/ 或不是圖與影片的不認。
func TestValidFileAcceptsRecordFilesAndBackups(t *testing.T) {
	for _, rel := range []string{"avatars/athena@sea~2.PNG", "adult/full/athena.png", "photos/x--u1.mp4", "photos/x--u1.png.bak-20261115-delete-u7",
		"avatars/full/a.jpg.bak-20261115-0930-replace-u3", "avatars/level/hidden/a.png"} {
		if !ValidFile(rel) {
			t.Errorf("%s 要認得", rel)
		}
	}
	for _, rel := range []string{"x.png", "../x.png", "photos/../../x.png", "photos/.x.png", "photos//x.png", "/abs/x.png", `photos\x.png`,
		"photos/x.md", "photos/x.png.bak-bad", "a/b/c/d/e.png", "photos/x.exe"} {
		if ValidFile(rel) {
			t.Errorf("%s 不能認", rel)
		}
	}
}

// 換圖、刪單張的改名：不蓋已經有的檔、路上是符號連結不做；FindBackup 找得到同一個原因最新的那份備份。
func TestMoveAndFindBackup(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	mine := filepath.Join(home, "personas", "mine")
	mustWrite(t, filepath.Join(mine, "photos", "a--u1.png"), []byte("a"))
	mustWrite(t, filepath.Join(mine, "photos", "b--u2.png"), []byte("b"))
	if err := s.Move("photos/a--u1.png", "photos/b--u2.png"); !errors.Is(err, ErrExists) {
		t.Fatalf("目的地有檔要 ErrExists：%v", err)
	}
	if got, _ := s.ReadFile("photos/b--u2.png"); string(got) != "b" {
		t.Fatal("不能蓋掉別的檔")
	}
	bak := BackupName("photos/a--u1.png", time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), "delete-u9")
	if bak != "photos/a--u1.png.bak-20261115-delete-u9" {
		t.Fatalf("備份名字：%s", bak)
	}
	if orig, reason, ok := SplitBackup(bak); !ok || orig != "photos/a--u1.png" || reason != "delete-u9" {
		t.Fatalf("SplitBackup：%q %q %v", orig, reason, ok)
	}
	if _, _, ok := SplitBackup("photos/a--u1.png"); ok {
		t.Fatal("不是備份")
	}
	if err := s.Move("photos/a--u1.png", bak); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.HasFile("photos/a--u1.png"); ok {
		t.Fatal("改名後原檔不在")
	}
	mustWrite(t, filepath.Join(mine, "photos", "a--u1.png.bak-20261101-delete-u9"), []byte("old"))
	mustWrite(t, filepath.Join(mine, "photos", "a--u1.png.bak-20261120-delete-u19"), []byte("other"))
	if got, err := s.FindBackup("photos/a--u1.png", is("delete-u9")); err != nil || got != bak {
		t.Fatalf("要找到最新的同原因備份：%q %v", got, err)
	}
	if got, _ := s.FindBackup("photos/a--u1.png", func(r string) bool { return strings.HasPrefix(r, "delete-u") }); got != "photos/a--u1.png.bak-20261120-delete-u19" {
		t.Fatalf("原因用比對的，回最新的：%q", got)
	}
	if got, _ := s.FindBackup("photos/none.png", is("delete-u9")); got != "" {
		t.Fatalf("沒有備份回空字串：%q", got)
	}
	if got, _ := New(t.TempDir()).FindBackup("photos/a--u1.png", is("delete-u9")); got != "" {
		t.Fatalf("沒有資料夾回空字串：%q", got)
	}
	// 符號連結的檔不改名。
	secret := filepath.Join(t.TempDir(), "s.png")
	mustWrite(t, secret, []byte("secret"))
	if err := os.Symlink(secret, filepath.Join(mine, "photos", "link.png")); err != nil {
		t.Fatal(err)
	}
	if err := s.Move("photos/link.png", "photos/link.png.bak-20261115-delete-u1"); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("符號連結要 ErrUnsafe：%v", err)
	}
}

// 新檔只寫角色卡、角色本身的頭像與 photos/（DEV-160 S3）：以前的衣服照片 avatars/<代號>@…、avatars/full/、adult/ 不再寫新檔
// （舊檔只會改名成備份，照 ValidFile）。
func TestValidRelOnlyCardsAndPhotos(t *testing.T) {
	// 角色本身的頭像 avatars/<代號>.<ext> 也能寫（角色卡的慣例路徑，複製角色進套件只認它）；衣服的照片（<代號>@<衣服>）不行。
	for _, rel := range []string{"my_fox.md", "myfox.md", "photos/athena-base--u1.png", "avatars/my_fox.png", "avatars/myfox.jpg", "avatars/myfox.mp4"} {
		if !ValidRel(rel) {
			t.Errorf("%s 要能寫", rel)
		}
	}
	for _, rel := range []string{"adult/athena.png", "adult/full/athena@my-1.jpg", "avatars/athena@my-1.png", "avatars/athena~2.png", "avatars/full/athena.png", "adult/x/athena.png", "other/athena.png", "adult/../avatars/a.png", "photos/a.md", "a@b.md"} {
		if ValidRel(rel) {
			t.Errorf("%s 不能寫", rel)
		}
	}
}

func is(want string) func(string) bool { return func(r string) bool { return r == want } }

// 同一個檔有好幾個備份時拿最新的：先看日期，再看請求編號的數字（u12 比 u9 新，不是照字串排）。
func TestFindBackupPicksNewest(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	dir := filepath.Join(home, "personas", "mine", "photos")
	isDelete := func(r string) bool { return strings.HasPrefix(r, "delete-") }
	for _, n := range []string{"x.png.bak-20261101-delete-u9", "x.png.bak-20261101-delete-u12", "x.png.bak-20261031-delete-u40", "x.png.bak-20261201-replace-u2"} {
		mustWrite(t, filepath.Join(dir, n), []byte(n))
	}
	if got, err := s.FindBackup("photos/x.png", isDelete); err != nil || got != "photos/x.png.bak-20261101-delete-u12" {
		t.Fatalf("要 u12：%q %v", got, err)
	}
	mustWrite(t, filepath.Join(dir, "x.png.bak-20261102-delete-u1"), []byte("newer"))
	if got, _ := s.FindBackup("photos/x.png", isDelete); got != "photos/x.png.bak-20261102-delete-u1" {
		t.Fatalf("日期新的優先：%q", got)
	}
}

// 官方位置（開發家換官方的照片，CMI-25）：根目錄是 personas/，只讀、改名、寫 avatars/ 底下的圖；官方角色卡、personas/mine/ 碰不到；
// 新檔只寫 avatars/<代號>.<ext> 與 avatars/photos/<照片編號>.<ext>；符號連結的 avatars/ 不寫。官方層在家外面時 OfficialInHome 是 false。
func TestOfficialStoreOnlyAvatars(t *testing.T) {
	home := t.TempDir()
	o := New(home).Official()
	if !o.OfficialInHome() {
		t.Fatal("沒設官方層位置時官方角色就在家裡")
	}
	mustWrite(t, filepath.Join(home, "personas", "athena.md"), []byte("card"))
	mustWrite(t, filepath.Join(home, "personas", "avatars", "athena@base~2.png"), []byte("old"))
	mustWrite(t, filepath.Join(home, "personas", "mine", "photos", "x.png"), []byte("mine"))
	if got, err := o.ReadFile("avatars/athena@base~2.png"); err != nil || string(got) != "old" {
		t.Fatalf("讀官方的圖：%q %v", got, err)
	}
	for _, rel := range []string{"athena.md", "mine/photos/x.png", "photos/x.png"} {
		if _, err := o.ReadFile(rel); err == nil {
			t.Errorf("官方位置不能讀 %s", rel)
		}
	}
	if err := o.Move("avatars/athena@base~2.png", "avatars/athena@base~2.png.bak-20261115-replace-u3"); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"avatars/photos/athena-base--half2.png", "avatars/athena.png"} {
		if w, err := o.PutNew(rel, []byte("new")); err != nil || !w {
			t.Fatalf("PutNew %s：%v %v", rel, w, err)
		}
	}
	for _, rel := range []string{"athena.md", "zeus.md", "photos/a.png", "avatars/full/a.png", "mine/photos/y.png", "avatars/photos/../../athena.md"} {
		if _, err := o.PutNew(rel, []byte("x")); err == nil {
			t.Errorf("官方位置不能寫 %s", rel)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(home, "personas", "athena.md")); string(got) != "card" {
		t.Fatal("官方角色卡不能動")
	}
	// 一般的 Store 照舊寫不到 avatars/photos/（那是官方位置的路徑）。
	if _, err := New(home).PutNew("avatars/photos/a.png", []byte("x")); err == nil {
		t.Fatal("personas/mine/ 不收 avatars/photos/")
	}
	// avatars/ 是符號連結：不寫。
	home2 := t.TempDir()
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home2, "personas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(home2, "personas", "avatars")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(home2).Official().PutNew("avatars/photos/a.png", []byte("x")); err == nil {
		t.Fatal("路上有符號連結不寫")
	}
	if len(tree(t, out)) != 0 {
		t.Fatal("不能寫到連結外面")
	}
	t.Setenv(sdk.EnvOfficialPersonas, t.TempDir())
	if New(home).Official().OfficialInHome() {
		t.Fatal("官方層在家外面")
	}
}
