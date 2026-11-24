package homebackup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// backupThenChange 做一份備份，然後把家改掉（改一個檔、刪一個檔、加一個檔、加一整類），回備份檔與「備份當時」的樣子。
func backupThenChange(t *testing.T, home string) (archive string, before map[string]string) {
	t.Helper()
	c, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	before = snapshot(t, home, volatile...)
	write(t, filepath.Join(home, "vault", "tasks", "T-1.md"), "後來改過的單\n")
	if err := os.Remove(filepath.Join(home, "vault", "notes", "日記.md")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(home, "vault", "tasks", "T-2.md"), "備份之後才有的單\n")
	write(t, filepath.Join(home, "personas", "mine", "new", "card.md"), "備份之後才有的角色\n")
	return c.Path, before
}

// 還原是整個換掉：備份之後改的、加的都不見，刪掉的回來；換之前自動留的那份現況裡有換之前的樣子；兩種佈局都一樣。
func TestRestoreReplacesEverythingAndKeepsACopyOfNow(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		home := fakeHome(t, "h", v2, 18001)
		archive, before := backupThenChange(t, home)
		changed := snapshot(t, home, volatile...)

		p, err := Stage(StageOptions{Home: home, Archive: archive, Now: testNow.Add(1)})
		if err != nil {
			t.Fatalf("v2=%v: stage: %v", v2, err)
		}
		// 準備好、還沒重開：家裡一個檔都沒動。
		sameSnapshot(t, "after stage", changed, snapshot(t, home, append(volatile, restoreDirName)...))
		safety := filepath.Join(platform.HomeBackupDir(home, sdk.HomeBackupKindBeforeRestore), p.Safety)
		if got := entries(t, safety)["home/vault/tasks/T-2.md"]; got != "備份之後才有的單\n" {
			t.Fatalf("v2=%v: the copy kept before restoring should hold the current state, got %q", v2, got)
		}

		res, err := ApplyPending(ApplyOptions{Home: home, Now: testNow.Add(2)})
		if err != nil || res == nil || !res.OK {
			t.Fatalf("v2=%v: apply: %+v %v", v2, res, err)
		}
		// home.toml 與秘密檔是重新寫出來的（排版會變），比內容的意思；其他檔逐位元組比。
		rewritten := append([]string{"home.toml", "secrets"}, volatile...)
		got := snapshot(t, home, rewritten...)
		delete(before, "home.toml")
		delete(before, filepath.Join("secrets", "push.toml"))
		sameSnapshot(t, "after restore", before, got)
		if h, err := platform.LoadHome(home); err != nil || h.User.Name != "測試主人" || h.Web.Port != 18001 {
			t.Fatalf("v2=%v: restored home.toml does not load as before: %v", v2, err)
		}
		if exists(restoreDir(home)) || HasPending(home) {
			t.Fatalf("v2=%v: restore leftovers remain", v2)
		}
		if r := readResult(home); r == nil || !r.OK || r.Safety != p.Safety {
			t.Fatalf("v2=%v: result not recorded: %+v", v2, r)
		}
		if again, err := ApplyPending(ApplyOptions{Home: home}); again != nil || err != nil {
			t.Fatalf("v2=%v: nothing should be pending any more", v2)
		}
	}
}

// 換到任何一步失敗（第 n 次改名失敗），家都跟還原前一模一樣，結果寫的是沒做成。
func TestRestoreFailureAtAnyStepLeavesHomeUntouched(t *testing.T) {
	for n := 1; ; n++ {
		home := fakeHome(t, "h", true, 18001)
		archive, _ := backupThenChange(t, home)
		changed := snapshot(t, home)
		if _, err := Stage(StageOptions{Home: home, Archive: archive, Now: testNow.Add(1)}); err != nil {
			t.Fatal(err)
		}
		calls, failed := 0, false
		rename := func(from, to string) error {
			calls++
			if calls == n {
				failed = true
				return errors.New("disk full")
			}
			return os.Rename(from, to)
		}
		res, err := ApplyPending(ApplyOptions{Home: home, Rename: rename})
		if !failed {
			if err != nil || !res.OK {
				t.Fatalf("run without failure should succeed: %+v %v", res, err)
			}
			if n < 8 {
				t.Fatalf("expected more rename steps than %d", n)
			}
			return
		}
		if err != nil || res == nil || res.OK {
			t.Fatalf("step %d: want a recorded failure, got %+v %v", n, res, err)
		}
		sameSnapshot(t, "failed at step", changed, snapshot(t, home))
		if HasPending(home) || exists(restoreDir(home)) {
			t.Fatalf("step %d: leftovers remain", n)
		}
	}
}

// 換上去之後的檢查沒過（這份備份在這台跑不起來）：全部換回去，包括推播金鑰。
func TestRestoreRollsBackWhenTheRestoredHomeDoesNotVerify(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	archive, _ := backupThenChange(t, home)
	secrets := filepath.Join(filepath.Dir(home), "state-h", "secrets", "push.toml")
	write(t, secrets, "[webpush]\npublic = \"NEWER\"\nprivate = \"NEWER\"\n[other]\ntoken = \"keep\"\n")
	changed := snapshot(t, filepath.Dir(home))
	if _, err := Stage(StageOptions{Home: home, Archive: archive, Now: testNow.Add(1)}); err != nil {
		t.Fatal(err)
	}
	res, err := ApplyPending(ApplyOptions{Home: home, Verify: func(string) error { return errors.New("migration failed") }})
	if err != nil || res.OK || !strings.Contains(res.Message, "換回") {
		t.Fatalf("want a recorded failure, got %+v %v", res, err)
	}
	got := snapshot(t, filepath.Dir(home), filepath.Join("state-h", "runtime"))
	for k := range changed {
		if strings.HasPrefix(k, filepath.Join("state-h", "runtime")) {
			continue
		}
		if got[k] != changed[k] {
			t.Fatalf("%s changed after a failed restore", k)
		}
	}
	if read(t, secrets) != "[webpush]\npublic = \"NEWER\"\nprivate = \"NEWER\"\n[other]\ntoken = \"keep\"\n" {
		t.Fatalf("secrets file not put back: %s", read(t, secrets))
	}
}

// 換到一半被關掉（這裡用「連換回去都失敗」模擬行程死在半路）：pending 留著，下次啟動全部換回去。
func TestRestoreInterruptedMidSwapRollsBackOnNextStart(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	archive, _ := backupThenChange(t, home)
	changed := snapshot(t, home)
	if _, err := Stage(StageOptions{Home: home, Archive: archive, Now: testNow.Add(1)}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	dying := func(from, to string) error {
		if calls++; calls > 5 {
			return errors.New("process killed")
		}
		return os.Rename(from, to)
	}
	if _, err := ApplyPending(ApplyOptions{Home: home, Rename: dying}); err == nil {
		t.Fatal("a restore that cannot even roll back must report an error so the service does not start on a half-swapped home")
	}
	if !HasPending(home) {
		t.Fatal("pending marker must survive so the next start can roll back")
	}
	res, err := ApplyPending(ApplyOptions{Home: home})
	if err != nil || res == nil || res.OK {
		t.Fatalf("next start should roll back and record a failure: %+v %v", res, err)
	}
	sameSnapshot(t, "after rollback on next start", changed, snapshot(t, home))
}

// 還原到別台（別的位置、別的埠、別的佈局設定）：綁機器的設定用這個家的，其他用備份的；寫死的舊路徑改成新的；推播金鑰帶過來。
func TestRestoreIntoAnotherHomeKeepsMachineSettings(t *testing.T) {
	old := fakeHome(t, "oldmac", true, 18001)
	c, err := Create(CreateOptions{Home: old, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	fresh := fakeHome(t, "newmac", true, 19999)
	if err := os.Remove(filepath.Join(filepath.Dir(fresh), "state-newmac", "secrets", "push.toml")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(old, "vault", "notes", "只在舊家.md"), "x") // 備份之後才加的，不該出現
	p, err := Stage(StageOptions{Home: fresh, Archive: c.Path, Now: testNow.Add(1), NewHome: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Safety != "" || !p.FromOtherMachine {
		t.Fatalf("building a new home keeps no copy of the empty home and counts as another machine: %+v", p)
	}
	res, err := ApplyPending(ApplyOptions{Home: fresh})
	if err != nil || !res.OK || len(res.Todo) == 0 {
		t.Fatalf("apply: %+v %v", res, err)
	}
	h, err := platform.LoadHome(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if h.Web.Port != 19999 || h.Runtime.Machine != "newmac" || h.Paths().State != filepath.Join(filepath.Dir(fresh), "state-newmac") {
		t.Fatalf("machine-bound settings should stay this machine's: port=%d machine=%s state=%s", h.Web.Port, h.Runtime.Machine, h.Paths().State)
	}
	raw := map[string]any{}
	if _, err := toml.DecodeFile(filepath.Join(fresh, "home.toml"), &raw); err != nil {
		t.Fatal(err)
	}
	if got := raw["notes"].(map[string]any)["folder"]; got != filepath.Join(fresh, "vault", "notes") {
		t.Fatalf("hard-coded old path not rewritten: %v", got)
	}
	if read(t, filepath.Join(fresh, "vault", "notes", "日記.md")) != "今天天氣很好\n" || exists(filepath.Join(fresh, "vault", "notes", "只在舊家.md")) {
		t.Fatal("data should be exactly the backup's")
	}
	if s := read(t, h.SecretsPath()); !strings.Contains(s, "PRIV-oldmac") {
		t.Fatalf("push keys should come from the backup: %s (%s)", s, h.SecretsPath())
	}
}

// 不能還原的情況在動手前就擋下來，而且不留東西：壞檔、比程式新的備份、已經有一份在等。
func TestStageRefusesWithoutTouchingHome(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	archive, _ := backupThenChange(t, home)
	changed := snapshot(t, home)
	junk := filepath.Join(t.TempDir(), "junk.egbackup")
	write(t, junk, "not a backup")
	if _, err := Stage(StageOptions{Home: home, Archive: junk, Now: testNow}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("junk file: %v", err)
	}
	write(t, filepath.Join(home, "settings", "migrations.toml"), "applied = [\"core-from-the-future\", \"pkg.x.1\"]\n")
	newer, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow.Add(5)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, "settings", "migrations.toml")); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(StageOptions{Home: home, Archive: newer.Path, Now: testNow, KnownMigrations: []string{"core-1"}}); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "比較新的程式") {
		t.Fatalf("backup newer than the program: %v", err)
	}
	if exists(restoreDir(home)) {
		t.Fatal("refused restore left files behind")
	}
	sameSnapshot(t, "after refusals", changed, snapshot(t, home))
	if _, err := Stage(StageOptions{Home: home, Archive: archive, Now: testNow}); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(StageOptions{Home: home, Archive: archive, Now: testNow}); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("second restore while one is pending: %v", err)
	}
}

// 還原用的家設定：綁機器的設定用這個家現在的，其他用備份的；寫死的舊路徑換成這台的。
func TestRebindHomeTomlKeepsMachineKeysFromDestination(t *testing.T) {
	backup := `
[user]
name = "小明"
[web]
port = 1111
public_url = "https://old.example.ts.net"
[runtime]
machine = "old"
service_label = "com.old"
[layout]
version = 2
state = "/Users/someone/.evergenie/state/home"
[update]
core = "/Users/someone/evergenie"
channel = "stable"
[sessions]
workdir = "/Users/someone/home/work"
[extra]
paths = ["/Users/someone/Documents/a", "/elsewhere/b"]
`
	dest := `
[user]
name = "空白"
[web]
port = 2222
[layout]
version = 2
state = "/Users/you/.evergenie/state/home"
`
	out, changed, err := rebindHomeToml([]byte(backup), []byte(dest), rebindPaths{OldHome: "/Users/someone/home", NewHome: "/Users/you/home", OldUser: "/Users/someone", NewUser: "/Users/you"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	if _, err := toml.Decode(string(out), &got); err != nil {
		t.Fatal(err)
	}
	get := func(path string) any { v, _ := lookup(got, strings.Split(path, ".")); return v }
	for path, want := range map[string]any{
		"user.name": "小明", "web.port": int64(2222), "web.public_url": nil, "runtime": nil,
		"layout.state": "/Users/you/.evergenie/state/home", "update.core": nil, "update.channel": "stable",
		"sessions.workdir": "/Users/you/home/work",
	} {
		if g := get(path); g != want {
			t.Errorf("%s = %v, want %v", path, g, want)
		}
	}
	if p := get("extra.paths").([]any); p[0] != "/Users/you/Documents/a" || p[1] != "/elsewhere/b" {
		t.Errorf("paths in lists: %v", p)
	}
	if len(changed) == 0 {
		t.Error("changes should be listed")
	}
}
