package homebackup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 還原後檔案的權限照舊：可以執行的腳本還能執行、只有自己能讀的設定還是只有自己能讀。
func TestRestoreKeepsFilePermissions(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	script := filepath.Join(home, "packages", "mine", "diary", "run.sh")
	private := filepath.Join(home, "settings", "push.toml")
	write(t, script, "#!/bin/sh\n")
	write(t, private, "secret = true\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(StageOptions{Home: home, Archive: c.Path, Now: testNow}); err != nil {
		t.Fatal(err)
	}
	if res, err := ApplyPending(ApplyOptions{Home: home, Now: testNow}); err != nil || !res.OK {
		t.Fatalf("apply: %+v %v", res, err)
	}
	for path, want := range map[string]os.FileMode{script: 0o755, private: 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s 的權限 %v，要 %v（%v）", filepath.Base(path), info.Mode().Perm(), want, err)
		}
	}
}

// 整類資料夾本身是捷徑（符號連結）時不做備份、用白話說原因，不會做出一份少了那一類卻說成功的備份。
func TestCreateRefusesWhenAWholeSectionIsASymlink(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	elsewhere := filepath.Join(filepath.Dir(home), "vault-elsewhere")
	if err := os.Rename(filepath.Join(home, "vault"), elsewhere); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(home, "vault")); err != nil {
		t.Fatal(err)
	}
	_, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err == nil || !strings.Contains(err.Error(), "捷徑") {
		t.Fatalf("err = %v", err)
	}
	if left := List(home); len(left) != 0 {
		t.Fatalf("不該留下備份檔：%+v", left)
	}
}

// 家資料夾裡主人自己放的其他東西（規矩文件 docs/、最上層的檔）也進備份，搬到另一個家時跟著過去；
// 照備份規則改名留著的舊東西（.bak-）、git 的紀錄不收，最上層的捷徑列在「沒收的」。
func TestBackupCarriesOtherThingsInTheHomeFolder(t *testing.T) {
	a := fakeHome(t, "a", true, 18001)
	write(t, filepath.Join(a, "docs", "constitution.md"), "憲法\n")
	write(t, filepath.Join(a, "README.md"), "說明\n")
	write(t, filepath.Join(a, "home.toml.bak-20261101-test"), "old\n")
	write(t, filepath.Join(a, ".git", "HEAD"), "ref\n")
	if err := os.Symlink(filepath.Dir(a), filepath.Join(a, "shortcut")); err != nil {
		t.Fatal(err)
	}
	c, err := Create(CreateOptions{Home: a, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.Manifest.skippedPaths(), ","); got != "shortcut" {
		t.Fatalf("沒收的 = %q", got)
	}
	for _, f := range c.Manifest.Files {
		if strings.Contains(f.Path, ".bak-") || strings.Contains(f.Path, ".git") {
			t.Fatalf("不該收 %s", f.Path)
		}
	}
	b := fakeHome(t, "b", true, 18002)
	write(t, filepath.Join(b, "notes-of-b.txt"), "b 自己的\n")
	if _, err := Stage(StageOptions{Home: b, Archive: c.Path, Now: testNow}); err != nil {
		t.Fatal(err)
	}
	if res, err := ApplyPending(ApplyOptions{Home: b, Now: testNow}); err != nil || !res.OK {
		t.Fatalf("apply: %+v %v", res, err)
	}
	for _, rel := range []string{"docs/constitution.md", "README.md"} {
		if _, err := os.Stat(filepath.Join(b, rel)); err != nil {
			t.Fatalf("%s 沒跟著過來：%v", rel, err)
		}
	}
	// 還原是整個換掉：b 原本自己的其他東西不在備份裡，換掉後不在（還原前那份現況裡有）。
	if _, err := os.Stat(filepath.Join(b, "notes-of-b.txt")); err == nil {
		t.Fatal("b 原本的其他東西應該被換掉")
	}
}

// 官方角色放在家裡的家（官方卡是主人的原稿）：備份收整個 personas/；還原到官方角色放在狀態資料夾的家時，
// 自己的角色進家裡、官方角色進狀態資料夾。
func TestBackupCarriesOfficialPersonasKeptInTheHome(t *testing.T) {
	a := fakeHome(t, "a", true, 18001)
	toml, err := os.ReadFile(filepath.Join(a, "home.toml"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(a, "home.toml"), strings.Replace(string(toml), "[layout]\n", "[layout]\nofficial_personas = \"home\"\n", 1))
	write(t, filepath.Join(a, "personas", "venus.md"), "主人寫的官方卡\n")
	write(t, filepath.Join(a, "personas", "avatars", "venus.png"), "PNG")
	c, err := Create(CreateOptions{Home: a, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	b := fakeHome(t, "b", true, 18002)
	bState := filepath.Join(filepath.Dir(b), "state-b")
	if _, err := Stage(StageOptions{Home: b, Archive: c.Path, Now: testNow}); err != nil {
		t.Fatal(err)
	}
	if res, err := ApplyPending(ApplyOptions{Home: b, Now: testNow}); err != nil || !res.OK {
		t.Fatalf("apply: %+v %v", res, err)
	}
	for _, path := range []string{
		filepath.Join(bState, "personas", "venus.md"),
		filepath.Join(bState, "personas", "avatars", "venus.png"),
		filepath.Join(b, "personas", "mine", "aki", "card.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("少了 %s：%v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(b, "personas", "venus.md")); err == nil {
		t.Fatal("官方卡不該進 b 的家裡（b 的官方角色在狀態資料夾）")
	}
}

// 準備還原到一半被關掉留下的暫存，下次啟動時收掉；pending 讀不懂而且已經有東西換下來時不開服務、也不刪。
func TestStartupCleansHalfStagedRestoreButKeepsSwappedOutData(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	left := filepath.Join(restoreDir(home), "tok", "new", "home", "vault", "big.bin")
	write(t, left, "半套")
	if res, err := ApplyPending(ApplyOptions{Home: home, Now: testNow}); res != nil || err != nil {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(restoreDir(home)); err == nil {
		t.Fatal("暫存該收掉")
	}
	write(t, filepath.Join(restoreDir(home), "tok", "old", "vault", "T-1.md"), "原本的家")
	write(t, pendingPath(home), "{壞掉")
	if _, err := ApplyPending(ApplyOptions{Home: home, Now: testNow}); err == nil {
		t.Fatal("該拒絕啟動")
	}
	if _, err := os.Stat(filepath.Join(restoreDir(home), "tok", "old", "vault", "T-1.md")); err != nil {
		t.Fatal("換下來的舊東西不能刪")
	}
}

// 還原準備好了但服務一直沒重開（重開的訊號沒送成）：過幾分鐘再請一次重開，期間不做自動備份。
func TestPendingRestoreAsksForRestartAgain(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	start := taipei(t, 16, 9)
	c, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: start})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(StageOptions{Home: home, Archive: c.Path, Now: start}); err != nil {
		t.Fatal(err)
	}
	m, _, _, restarts := newModule(t, home, start)
	ctx := context.Background()
	m.tick(ctx, start.Add(time.Minute))
	if *restarts != 0 {
		t.Fatal("剛準備好不用再請重開")
	}
	m.tick(ctx, start.Add(restartRetryAfter+time.Minute))
	if *restarts != 1 || dailyCount(home) != 0 {
		t.Fatalf("restarts=%d daily=%d", *restarts, dailyCount(home))
	}
}
