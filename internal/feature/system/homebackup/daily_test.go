package homebackup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

type fakeNotify struct {
	mu   sync.Mutex
	sent []*sdk.Notification
}

func (f *fakeNotify) Send(_ context.Context, n *sdk.Notification) (*sdk.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, n)
	return n, nil
}

func (f *fakeNotify) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.sent) }

func newModule(t *testing.T, home string, at time.Time) (*Module, *platform.FakeClock, *fakeNotify, *int) {
	t.Helper()
	clock, notes, restarts := platform.NewFakeClock(at), &fakeNotify{}, new(int)
	m := New(Deps{Notify: notes, Clock: clock, Restart: func() { *restarts++ }})
	m.SetHome(home)
	return m, clock, notes, restarts
}

func taipei(t *testing.T, day, hour int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, 11, day, hour, 0, 0, 0, loc)
}

func dailyCount(home string) int {
	return len(listDir(platform.HomeBackupDir(home, sdk.HomeBackupKindDaily)))
}

// 不再每天自動備份（HBK-2，2026-11-21）：定時工作過了凌晨 3 點、連跑幾天都不做備份、不發通知；
// 以前每天自動留下的那些只留最新一份（很佔空間），手動做的照規則留。沙盒家也一樣清。
func TestNoDailyBackupAndOldDailyBackupsCleared(t *testing.T) {
	for _, sandbox := range []bool{false, true} {
		home := fakeHome(t, "h", true, 18001)
		if sandbox {
			toml := read(t, filepath.Join(home, "home.toml"))
			write(t, filepath.Join(home, "home.toml"), strings.Replace(toml, "[runtime]\n", "[runtime]\nsandbox = true\n", 1))
		}
		daily := platform.HomeBackupDir(home, sdk.HomeBackupKindDaily)
		manual := platform.HomeBackupDir(home, sdk.HomeBackupKindManual)
		start := taipei(t, 16, 2)
		for i := 1; i <= 3; i++ {
			at := start.AddDate(0, 0, -i)
			for _, d := range []string{daily, manual} {
				p := filepath.Join(d, "h-"+at.Format("20060102-150405")+platform.HomeBackupExt)
				write(t, p, "old")
				if err := os.Chtimes(p, at, at); err != nil {
					t.Fatal(err)
				}
			}
		}
		m, clock, notes, _ := newModule(t, home, start)
		ctx := context.Background()
		for day := 16; day <= 18; day++ {
			for _, hour := range []int{2, 4, 15} {
				clock.Set(taipei(t, day, hour))
				m.tick(ctx, clock.Now())
			}
		}
		if n := dailyCount(home); n != 1 {
			t.Fatalf("sandbox=%v：以前每天自動的只留最新一份、也不能再做，剩 %d 份", sandbox, n)
		}
		if got := listDir(daily)[0].Name; got != "h-"+start.AddDate(0, 0, -1).Format("20060102-150405")+platform.HomeBackupExt {
			t.Fatalf("sandbox=%v：留的要是最新那份，留了 %s", sandbox, got)
		}
		if n := len(listDir(manual)); n != 3 {
			t.Fatalf("sandbox=%v：手動做的照規則留，剩 %d 份", sandbox, n)
		}
		if notes.count() != 0 {
			t.Fatalf("sandbox=%v：沒事不通知，發了 %d 則", sandbox, notes.count())
		}
	}
}

// 保留規則（HBK-3）：手動做的、還原前留的最新 7 份一定留，其餘超過 30 天的刪；以前每天自動的只留最新一份。
// 每次做完備份真的會清。
func TestPruneKeepsNewestSevenAndDailyKeepsOne(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	var list []sdk.HomeBackupFile
	for i := 0; i < 50; i++ {
		list = append(list, sdk.HomeBackupFile{Name: "f" + itoa(i), CreatedAt: now.AddDate(0, 0, -i)})
	}
	keep, days := keepRule(sdk.HomeBackupKindManual)
	drop := PlanPrune(list, keep, days, now)
	if len(drop) != 19 || drop[0].Name != "f31" {
		t.Fatalf("手動：該刪 19 份、從 f31 起，結果 %d 份從 %v 起", len(drop), drop[0].Name)
	}
	if k, d := keepRule(sdk.HomeBackupKindBeforeRestore); k != keep || d != days {
		t.Fatalf("還原前留的跟手動的同一套規則：%d、%d", k, d)
	}
	keep, days = keepRule(sdk.HomeBackupKindDaily)
	drop = PlanPrune(list, keep, days, now)
	if keep != 1 || len(drop) != 49 || drop[0].Name != "f1" {
		t.Fatalf("以前每天自動的：只留最新一份，結果刪 %d 份", len(drop))
	}
	old := list[:3]
	for i := range old {
		old[i].CreatedAt = now.AddDate(0, 0, -100-i)
	}
	for _, kind := range []string{sdk.HomeBackupKindManual, sdk.HomeBackupKindBeforeRestore} {
		keep, days := keepRule(kind)
		if len(PlanPrune(old, keep, days, now)) != 0 {
			t.Fatalf("%s：最新的幾份再舊也要留", kind)
		}
	}

	home := fakeHome(t, "h", true, 18001)
	daily := platform.HomeBackupDir(home, sdk.HomeBackupKindDaily)
	manual := platform.HomeBackupDir(home, sdk.HomeBackupKindManual)
	// 以前每天自動的：最近 12 天每天一份。手動的：12 份都在 30 天內＋3 份 40 天前的。
	for i := 1; i <= 12; i++ {
		for _, d := range []string{daily, manual} {
			at := now.AddDate(0, 0, -i)
			p := filepath.Join(d, "h-"+at.Format("20060102-150405")+platform.HomeBackupExt)
			write(t, p, "old")
			if err := os.Chtimes(p, at, at); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < 3; i++ {
		at := now.AddDate(0, 0, -40-i)
		p := filepath.Join(manual, "h-"+at.Format("20060102-150405")+platform.HomeBackupExt)
		write(t, p, "very old")
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	m, _, _, _ := newModule(t, home, now)
	// 手動做一份：做完就清。
	if _, err := m.makeBackup(home, sdk.HomeBackupKindManual, now); err != nil {
		t.Fatal(err)
	}
	if n := len(listDir(daily)); n != 1 {
		t.Fatalf("以前每天自動的只留最新一份，剩 %d 份", n)
	}
	if n := len(listDir(manual)); n != 13 {
		t.Fatalf("手動的 30 天內 12 份＋新的 1 份都要留、40 天前的 3 份要刪，剩 %d 份", n)
	}
}
