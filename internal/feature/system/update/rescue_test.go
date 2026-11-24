package update

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 「退回上一版」照 rescue.json 換回之前，先確認紀錄寫的東西能用（UPD-14）：要還原資料時備份要在這個家的備份資料夾底下、
// 而且整份讀得開；下載版的舊版資料夾名稱要合法。不行就是「換不回去」（結束碼 3），而且什麼都沒動：current 還在新版、
// 資料沒被蓋、沒有另外留一份現在的家、紀錄留著；給主人的話是白話、下一步是請秘書處理。
func TestRescueRefusesUnusableRecord(t *testing.T) {
	cases := []struct {
		name, want string
		setup      func(t *testing.T, e *dlEnv, u *Upgrader, r *upgradeRescue)
	}{
		{"備份不見了", "備份不見了或讀不開", func(t *testing.T, e *dlEnv, u *Upgrader, r *upgradeRescue) {}},
		{"備份讀不開", "備份不見了或讀不開", func(t *testing.T, e *dlEnv, u *Upgrader, r *upgradeRescue) {
			writeFile(t, r.Backup, "not a tgz")
		}},
		{"備份只剩一半", "備份不見了或讀不開", func(t *testing.T, e *dlEnv, u *Upgrader, r *upgradeRescue) {
			big := filepath.Join(e.home, "vault", "tasks", "big.md")
			writeFile(t, big, strings.Repeat("資料很多 ", 20000))
			if err := u.backupHome(r.Backup); err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(big)
			data, _ := os.ReadFile(r.Backup)
			writeFile(t, r.Backup, string(data[:len(data)/2]))
		}},
		{"備份不在這個家的備份資料夾", "備份不在這個家的備份資料夾裡", func(t *testing.T, e *dlEnv, u *Upgrader, r *upgradeRescue) {
			r.Backup = filepath.Join(t.TempDir(), "elsewhere.tgz")
			if err := u.backupHome(r.Backup); err != nil {
				t.Fatal(err)
			}
		}},
		{"備份是指到外面的連結", "備份不在這個家的備份資料夾裡", func(t *testing.T, e *dlEnv, u *Upgrader, r *upgradeRescue) {
			outside := filepath.Join(t.TempDir(), "elsewhere.tgz")
			if err := u.backupHome(outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, r.Backup); err != nil {
				t.Fatal(err)
			}
		}},
		{"備份寫成相對路徑", "備份不在這個家的備份資料夾裡", func(t *testing.T, e *dlEnv, u *Upgrader, r *upgradeRescue) {
			r.Backup = filepath.Join("runtime", "backups", "upgrade-0.1.0-0.2.0.tgz")
		}},
		{"舊版資料夾的名稱不對", "舊版資料夾不對", func(t *testing.T, e *dlEnv, u *Upgrader, r *upgradeRescue) {
			r.Dir, r.Restore = "../0.1.0", false
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newDLEnv(t)
			u, _, notes := e.upgrader()
			// 換不回去的樣子：current 已經在新版 0.2.0、資料是新版寫的
			archive := filepath.Join(t.TempDir(), "new.tar.gz")
			writeFile(t, archive, string(releaseTar(t, "0.2.0")))
			if err := extractRelease(archive, filepath.Join(e.app, versionsDirName, "0.2.0")); err != nil {
				t.Fatal(err)
			}
			if err := switchCurrent(e.app, "0.2.0"); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(platform.RuntimeDir(e.home, "backups"), 0o700); err != nil {
				t.Fatal(err)
			}
			data := filepath.Join(e.home, "vault", "tasks", "data.md")
			writeFile(t, data, "newer")
			r := upgradeRescue{From: "0.1.0", To: "0.2.0", Install: InstallDownload, Dir: "0.1.0",
				Backup: platform.RuntimeDir(e.home, "backups", "upgrade-0.1.0-0.2.0.tgz"), Restore: true, At: time.Now()}
			c.setup(t, e, u, &r)
			if err := writeRescue(e.home, r); err != nil {
				t.Fatal(err)
			}
			err := u.Rescue(context.Background())
			if UpgradeExitCode(err) != UpgradeExitStranded || err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "沒有動任何東西") || asciiWord.MatchString(err.Error()) {
				t.Fatalf("要回「換不回去」、什麼都沒動：%v", err)
			}
			if e.current() != "0.2.0" {
				t.Fatalf("current 不能動：%s", e.current())
			}
			if got, _ := os.ReadFile(data); string(got) != "newer" {
				t.Fatalf("資料不能動：%q", got)
			}
			if kept, _ := filepath.Glob(platform.RuntimeDir(e.home, "backups", "rollback-*.tgz")); len(kept) != 0 {
				t.Fatalf("不能另外留一份（什麼都沒動）：%v", kept)
			}
			if readRescue(e.home) == nil {
				t.Fatal("紀錄要留著")
			}
			if st, _ := ReadUpgradeState(e.home); st == nil || st.Outcome != sdk.UpdateOutcomeStranded || st.Next != nextRescueBroken || english(st.Reason) {
				t.Fatalf("state：%+v", st)
			}
			if len(*notes) != 1 || english((*notes)[0].title+(*notes)[0].detail) {
				t.Fatalf("通知：%+v", *notes)
			}
		})
	}
}

// 原始碼版的紀錄：commit 只收完整的 40 碼十六進位（分支名、tag、git 的選項、縮短的都不收）；不對就「換不回去」，不跑 git。
func TestRescueCommitMustBeFullHex(t *testing.T) {
	u := &Upgrader{Home: testHome(t), Core: t.TempDir(), From: "0.2.0", Out: &bytes.Buffer{}, Notify: func(string, string) {}}
	good := "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []string{"", "main", "HEAD", "v0.1.0", "--upload-pack=touch /tmp/x", good[:39], strings.ToUpper(good), good + "0"} {
		if why := u.rescueProblem(&upgradeRescue{Commit: c}, false); why == "" || english(why) {
			t.Errorf("commit %q 要擋（白話）：%q", c, why)
		}
	}
	if why := u.rescueProblem(&upgradeRescue{Commit: good}, false); why != "" {
		t.Fatalf("40 碼十六進位要收：%q", why)
	}
	if err := writeRescue(u.Home, upgradeRescue{From: "0.1.0", To: "0.2.0", Commit: "main", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	err := u.Rescue(context.Background())
	if UpgradeExitCode(err) != UpgradeExitStranded || err == nil || !strings.Contains(err.Error(), "程式版本不對") || !strings.Contains(err.Error(), "沒有動任何東西") {
		t.Fatalf("原始碼版的紀錄寫的 commit 不對：%v", err)
	}
	if strings.Contains(readLog(t, u.Home), "git checkout") {
		t.Fatal("不能跑 git")
	}
}

// 有一次沒換回的升級還沒處理（rescue.json）時，「確認升級沒問題」要擋住：確認會刪掉升級前的備份與舊版的程式資料夾，
// 換回要用的可能就是它們。擋住時什麼都不刪；紀錄處理掉之後照常確認。
func TestAcceptBlockedWhileRescuePending(t *testing.T) {
	ctx := context.Background()
	e := newDLEnv(t)
	u, log, _ := e.upgrader()
	e.publish("0.2.0", "release", nil, "migrate-ok")
	if err := u.Apply(ctx, ""); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	last := PendingAcceptance(e.home)
	if last == nil || last.Backup == "" {
		t.Fatalf("要有等確認的升級：%+v", last)
	}
	if err := writeRescue(e.home, upgradeRescue{From: "0.1.0", To: "0.2.0", Install: InstallDownload, Dir: "0.1.0", Backup: last.Backup, Restore: true, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, err := u.Accept()
	if UpgradeExitCode(err) != UpgradeExitFailed || err == nil || !strings.Contains(err.Error(), "沒換回") || !strings.Contains(err.Error(), "退回上一版") || asciiWord.MatchString(err.Error()) {
		t.Fatalf("要擋：%v", err)
	}
	if _, err := os.Stat(last.Backup); err != nil {
		t.Fatalf("備份不能刪：%v", err)
	}
	if strings.Join(e.versions(), " ") != "0.1.0 0.2.0" || PendingAcceptance(e.home) == nil {
		t.Fatalf("舊版不能刪、也不記確認：%v", e.versions())
	}
	removeRescue(e.home)
	if _, err := u.Accept(); err != nil {
		t.Fatalf("紀錄處理掉之後照常確認：%v", err)
	}
	if strings.Join(e.versions(), " ") != "0.2.0" {
		t.Fatalf("確認之後只留 current：%v", e.versions())
	}
}

// RescueBackups 回紀錄還要用的備份（升級前的、換回前留的），給整理備份的 settings 避開；沒有紀錄回 nil。
func TestRescueBackupsListsWhatTheRecordNeeds(t *testing.T) {
	home := testHome(t)
	if got := RescueBackups(home); got != nil {
		t.Fatalf("沒有紀錄：%v", got)
	}
	b, k := platform.RuntimeDir(home, "backups", "upgrade-0.1.0-0.2.0.tgz"), platform.RuntimeDir(home, "backups", "rollback-0.2.0-0.1.0-1.tgz")
	if err := writeRescue(home, upgradeRescue{From: "0.1.0", To: "0.2.0", Backup: b, Kept: k, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if got := RescueBackups(home); len(got) != 2 || got[0] != b || got[1] != k {
		t.Fatalf("RescueBackups：%v", got)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readLog(t *testing.T, home string) string {
	t.Helper()
	data, _ := os.ReadFile(platform.RuntimeDir(home, "logs", "upgrade.log"))
	return string(data)
}

// upgrade --to 指了比現在舊的版本（UPD-12）：升級不換成舊版，白話說明、下一步是「退回上一版」，什麼都沒動（沒下載、沒備份、
// current 與服務不變、不跳過、不發通知）。下載版與原始碼版一樣。
func TestUpgradeRefusesOlderTarget(t *testing.T) {
	ctx := context.Background()
	t.Run("下載版", func(t *testing.T) {
		e := newDLEnv(t)
		u, _, notes := e.upgrader()
		e.publish("0.0.9", "release", nil)
		e.mu.Lock()
		e.requests = nil
		e.mu.Unlock()
		pid := servePID(t, e.home)
		err := u.Apply(ctx, "v0.0.9")
		if UpgradeExitCode(err) != UpgradeExitFailed || err == nil || !strings.Contains(err.Error(), "0.0.9 比現在的 0.1.0 舊") || !strings.Contains(err.Error(), "退回上一版") || asciiWord.MatchString(err.Error()) {
			t.Fatalf("要擋：%v", err)
		}
		e.mu.Lock()
		reqs := strings.Join(e.requests, " ")
		e.mu.Unlock()
		if strings.Contains(reqs, "/rel/") {
			t.Fatalf("不能下載：%s", reqs)
		}
		if e.current() != "0.1.0" || strings.Join(e.versions(), " ") != "0.1.0" || servePID(t, e.home) != pid || len(*notes) != 0 || len(ReadUpgradeSkipped(e.home)) != 0 {
			t.Fatalf("什麼都不能動：current=%s versions=%v notes=%v", e.current(), e.versions(), *notes)
		}
		if st, _ := ReadUpgradeState(e.home); st == nil || st.Outcome != sdk.UpdateOutcomeUntouched || st.Next != nextDowngrade {
			t.Fatalf("state：%+v", st)
		}
		if backups, _ := filepath.Glob(platform.RuntimeDir(e.home, "backups", "*.tgz")); len(backups) != 0 {
			t.Fatalf("不能備份：%v", backups)
		}
	})
	t.Run("原始碼版", func(t *testing.T) {
		e := newUpEnv(t)
		e.release("v0.0.5", "0.0.5", nil)
		u, _, notes := e.upgrader(platform.UpdateChannelRelease)
		head := e.headVersion()
		err := u.Apply(ctx, "v0.0.5")
		if UpgradeExitCode(err) != UpgradeExitFailed || err == nil || !strings.Contains(err.Error(), "0.0.5 比現在的 0.1.0 舊") || !strings.Contains(err.Error(), "退回上一版") {
			t.Fatalf("要擋：%v", err)
		}
		if e.headVersion() != head || e.binVersion("bin") != "0.1.0" || len(*notes) != 0 {
			t.Fatalf("什麼都不能動：head %s→%s bin=%s", head, e.headVersion(), e.binVersion("bin"))
		}
		if backups, _ := filepath.Glob(platform.RuntimeDir(e.home, "backups", "*.tgz")); len(backups) != 0 {
			t.Fatalf("不能備份：%v", backups)
		}
	})
}

// 比不出新舊（UPD-12，DEV-155）：現在的版本號看不懂（開發中的版本、只有 commit 之類），upgrade --to 不猜、不換過去（fail closed）：
// 白話說看不出哪個比較新、下一步問秘書，英文細節進升級日誌，什麼都沒動。下載版與原始碼版一樣。
func TestUpgradeRefusesUnknownVersionOrder(t *testing.T) {
	ctx := context.Background()
	for _, from := range []string{"dev", "abc1234", "0.1"} {
		t.Run("下載版 "+from, func(t *testing.T) {
			e := newDLEnv(t)
			u, out, notes := e.upgrader()
			u.From = from
			e.publish("0.2.0", "release", nil)
			e.mu.Lock()
			e.requests = nil
			e.mu.Unlock()
			pid := servePID(t, e.home)
			err := u.Apply(ctx, "v0.2.0")
			if UpgradeExitCode(err) != UpgradeExitFailed || err == nil || !strings.Contains(err.Error(), msgVersionUnknown) || !strings.Contains(err.Error(), nextAsk) || asciiWord.MatchString(err.Error()) || asciiWord.MatchString(out.String()) {
				t.Fatalf("要擋、白話：%v｜%s", err, out.String())
			}
			e.mu.Lock()
			reqs := strings.Join(e.requests, " ")
			e.mu.Unlock()
			if strings.Contains(reqs, "/rel/") {
				t.Fatalf("不能下載：%s", reqs)
			}
			if e.current() != "0.1.0" || strings.Join(e.versions(), " ") != "0.1.0" || servePID(t, e.home) != pid || len(*notes) != 0 || len(ReadUpgradeSkipped(e.home)) != 0 {
				t.Fatalf("什麼都不能動：current=%s versions=%v notes=%v", e.current(), e.versions(), *notes)
			}
			if st, _ := ReadUpgradeState(e.home); st == nil || st.Outcome != sdk.UpdateOutcomeUntouched || st.Next != nextAsk || english(st.Reason+st.Next) {
				t.Fatalf("state：%+v", st)
			}
			if !strings.Contains(e.upgradeLog(), "cannot compare target version") {
				t.Fatalf("英文細節要進 upgrade.log：%s", e.upgradeLog())
			}
		})
	}
	t.Run("原始碼版", func(t *testing.T) {
		e := newUpEnv(t)
		e.release("v0.2.0", "0.2.0", nil)
		u, _, notes := e.upgrader(platform.UpdateChannelRelease)
		u.From = "abc1234"
		head := e.headVersion()
		err := u.Apply(ctx, "v0.2.0")
		if UpgradeExitCode(err) != UpgradeExitFailed || err == nil || !strings.Contains(err.Error(), msgVersionUnknown) || asciiWord.MatchString(err.Error()) {
			t.Fatalf("要擋：%v", err)
		}
		if e.headVersion() != head || e.binVersion("bin") != "0.1.0" || len(*notes) != 0 {
			t.Fatalf("什麼都不能動：head %s→%s bin=%s", head, e.headVersion(), e.binVersion("bin"))
		}
		if backups, _ := filepath.Glob(platform.RuntimeDir(e.home, "backups", "*.tgz")); len(backups) != 0 {
			t.Fatalf("不能備份：%v", backups)
		}
	})
}

func TestVersionOrder(t *testing.T) {
	for _, c := range []struct {
		a, b  string
		order int
		ok    bool
	}{
		{"0.2.0", "0.1.0", 1, true},
		{"v0.1.0", "0.2.0", -1, true},
		{"0.1.0", "0.1.0", 0, true},
		{"0.10.0", "0.9.0", 1, true},
		{"0.2.0", "dev", 0, false},
		{"abc1234", "0.1.0", 0, false},
		{"0.2", "0.1.0", 0, false},
		{"", "0.1.0", 0, false},
	} {
		order, ok := versionOrder(c.a, c.b)
		if ok != c.ok || (ok && (order > 0) != (c.order > 0)) || (ok && (order < 0) != (c.order < 0)) {
			t.Errorf("versionOrder(%q, %q) = %d, %v；要 %d, %v", c.a, c.b, order, ok, c.order, c.ok)
		}
	}
}
