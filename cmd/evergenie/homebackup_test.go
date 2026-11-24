package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/homebackup"
	"github.com/minglight/evergenie/internal/feature/system/update"
	"github.com/minglight/evergenie/sdk"
)

func runInit(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := initCommand(context.Background(), cmdline.HostRequest{Args: args, Out: &out, Err: &errOut})
	return code, out.String(), errOut.String()
}

// 從備份建家：新電腦上 init --from-backup 建出來的家，內容是備份的（筆記、設定、主人的名字），
// 綁機器的設定是新家的（看板的埠、狀態資料夾），而且讀得起來；最後用白話列出還要主人自己做的事。
func TestInitFromBackupBuildsHomeWithBackupDataAndThisMachinesSettings(t *testing.T) {
	t.Setenv("EVERGENIE_ENGINE_BIN_DIR", t.TempDir()) // 沒有引擎：帳號設定不自動填，只多一句提醒
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "old", "myhome")
	if code, _, e := runInit(t, "--home", old, "--name", "小明", "--timezone", "Asia/Taipei", "--state", filepath.Join(root, "old-state"), "--port", "18101"); code != 0 {
		t.Fatalf("init old home: %s", e)
	}
	note := filepath.Join(old, "vault", "notes", "日記.md")
	if err := os.MkdirAll(filepath.Dir(note), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, []byte("舊電腦上寫的\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "settings", "notify.toml"), []byte("quiet = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := homebackup.Create(homebackup.CreateOptions{Home: old, Kind: sdk.HomeBackupKindManual, Dir: filepath.Join(root, "usb")})
	if err != nil {
		t.Fatal(err)
	}

	fresh, state := filepath.Join(root, "new", "home2"), filepath.Join(root, "new-state")
	code, out, e := runInit(t, "--home", fresh, "--state", state, "--port", "18202", "--from-backup", c.Path)
	if code != 0 {
		t.Fatalf("init --from-backup: %s", e)
	}
	h, err := platform.LoadHome(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if h.User.Name != "小明" || h.Web.Port != 18202 || h.Paths().State != state {
		t.Fatalf("name=%s port=%d state=%s", h.User.Name, h.Web.Port, h.Paths().State)
	}
	if b, err := os.ReadFile(filepath.Join(fresh, "vault", "notes", "日記.md")); err != nil || string(b) != "舊電腦上寫的\n" {
		t.Fatalf("note not carried over: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fresh, "settings", "notify.toml")); err != nil {
		t.Fatal("settings not carried over")
	}
	if _, err := os.Stat(filepath.Join(fresh, ".restore")); !os.IsNotExist(err) {
		t.Fatal("restore leftovers in the new home")
	}
	for _, want := range []string{"家建好了（用備份）", "http://127.0.0.1:18202/", "重新登入", "手機要重新連回家", "service install"} {
		if !strings.Contains(out, want) {
			t.Fatalf("finish text should mention %q:\n%s", want, out)
		}
	}
	if err := verifyRestored(fresh); err != nil {
		t.Fatalf("home built from a backup should pass the start-up checks: %v", err)
	}
}

// 備份檔壞的、不存在：不建家（什麼都不留）；備份放不進去也把剛建的收掉。
func TestInitFromBackupLeavesNothingWhenBackupIsBad(t *testing.T) {
	root := t.TempDir()
	junk := filepath.Join(root, "junk.egbackup")
	if err := os.WriteFile(junk, []byte("not a backup"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{junk, filepath.Join(root, "missing.egbackup")} {
		home, state := filepath.Join(root, "h"), filepath.Join(root, "s")
		code, _, e := runInit(t, "--home", home, "--state", state, "--from-backup", file)
		if code == 0 || strings.TrimSpace(e) == "" {
			t.Fatalf("%s: init should fail with a message", file)
		}
		for _, p := range []string{home, state} {
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Fatalf("%s: %s was left behind", file, p)
			}
		}
	}
}

// 還原換上去之後的資料整理照法律 3.1 先在複本上預演：有一項過不了，換上來的家裡就一項都不真的跑
// （前面那幾項沒有先在家裡改到一半），還原整個換回去。
func TestVerifyRestoredPreviewsMigrationsBeforeRunningThem(t *testing.T) {
	old, oldPkg := update.HomeMigrations, update.PackageMigrations
	t.Cleanup(func() { update.HomeMigrations, update.PackageMigrations = old, oldPkg })
	update.PackageMigrations = nil
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "h")
	update.HomeMigrations = nil
	if _, err := initHome(initOptions{Home: home, Name: "小明", Timezone: "Asia/Taipei", State: filepath.Join(root, "s"), Port: 18301}); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join("vault", "整理過.md")
	ran := 0
	update.HomeMigrations = []update.HomeMigration{
		{ID: "first", Description: "會寫檔的整理", Run: func(h string) error {
			ran++
			return os.WriteFile(filepath.Join(h, marker), []byte("x"), 0o644)
		}},
		{ID: "second", Description: "過不了的整理", Run: func(string) error { return errors.New("這份資料轉不過去") }},
	}
	if err := verifyRestored(home); err == nil {
		t.Fatal("預演沒過要回錯")
	}
	if _, err := os.Stat(filepath.Join(home, marker)); !os.IsNotExist(err) {
		t.Fatal("預演沒過，真的家不能被前面的整理改到")
	}
	if applied, err := update.AppliedMigrations(home); err != nil || len(applied) != 0 {
		t.Fatalf("預演沒過，家裡不能記成跑過：%v %v", applied, err)
	}
	if ran != 1 {
		t.Fatalf("第一項只該在複本上跑一次，跑了 %d 次", ran)
	}
	// 都過得了：預演一次、真的家一次，家裡記成跑過。
	update.HomeMigrations = update.HomeMigrations[:1]
	ran = 0
	if err := verifyRestored(home); err != nil {
		t.Fatal(err)
	}
	if applied, _ := update.AppliedMigrations(home); ran != 2 || len(applied) != 1 {
		t.Fatalf("要先預演再真的跑：跑了 %d 次，記了 %v", ran, applied)
	}

	// 整段還原：備份換上去之後整理過不了 → 換回去，家跟還原前一樣（整理只准改 home.toml、settings/、vault/，都在換回去的範圍裡）。
	update.HomeMigrations = nil
	c, err := homebackup.Create(homebackup.CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual})
	if err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(home, "vault", "備份之後寫的.md")
	if err := os.WriteFile(note, []byte("還原沒做成就要留著\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := homebackup.Stage(homebackup.StageOptions{Home: home, Archive: c.Path}); err != nil {
		t.Fatal(err)
	}
	update.HomeMigrations = []update.HomeMigration{{ID: "third", Description: "過不了的整理", Run: func(h string) error {
		_ = os.WriteFile(filepath.Join(h, "vault", "整理到一半.md"), []byte("x"), 0o644) // 複本上寫的，不會到真的家
		return errors.New("轉不過去")
	}}}
	var out, errOut bytes.Buffer
	if !applyPendingRestore(home, &out, &errOut) || !strings.Contains(out.String(), "restore ok=false") {
		t.Fatalf("整理過不了要換回去、服務照常開：%s %s", out.String(), errOut.String())
	}
	if b, err := os.ReadFile(note); err != nil || string(b) != "還原沒做成就要留著\n" {
		t.Fatalf("換回去之後家要跟還原前一樣：%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "vault", "整理到一半.md")); !os.IsNotExist(err) {
		t.Fatal("沒過的整理不能在家裡留東西")
	}
}

// 從備份建家放不進去時只收這次才建的（比照 createHome）：家的資料夾建家之前就在（空的）就留著、只清裡面；這次才建的狀態資料夾整個刪。
func TestInitFromBackupFailureKeepsFoldersThatWereAlreadyThere(t *testing.T) {
	old, oldPkg := update.HomeMigrations, update.PackageMigrations
	t.Cleanup(func() { update.HomeMigrations, update.PackageMigrations = old, oldPkg })
	update.HomeMigrations, update.PackageMigrations = nil, nil
	t.Setenv("EVERGENIE_ENGINE_BIN_DIR", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "src")
	if _, err := initHome(initOptions{Home: src, Name: "小明", Timezone: "Asia/Taipei", State: filepath.Join(root, "src-state"), Port: 18311}); err != nil {
		t.Fatal(err)
	}
	c, err := homebackup.Create(homebackup.CreateOptions{Home: src, Kind: sdk.HomeBackupKindManual, Dir: filepath.Join(root, "usb")})
	if err != nil {
		t.Fatal(err)
	}
	// 備份讀得懂，但換上去之後的資料整理過不了。
	update.HomeMigrations = []update.HomeMigration{{ID: "boom", Description: "過不了的整理", Run: func(string) error { return errors.New("炸了") }}}
	home, state := filepath.Join(root, "already-here"), filepath.Join(root, "new-state")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, e := runInit(t, "--home", home, "--state", state, "--port", "18312", "--from-backup", c.Path)
	if code == 0 || strings.TrimSpace(e) == "" {
		t.Fatal("放不進去要失敗並說原因")
	}
	if left, err := os.ReadDir(home); err != nil || len(left) != 0 {
		t.Fatalf("本來就在的空資料夾要留著、而且還是空的：%v %v", left, err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("這次才建的狀態資料夾要刪掉")
	}
}
