package job

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
)

// openHome 開一個還沒開資料庫的假家（遷移跑在資料庫開之前），before 先動檔案再開。
func openHome(t *testing.T, before func(dir string)) *homefixture.Home {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "vault"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := "[user]\nname=\"測試\"\ntimezone=\"Asia/Taipei\"\n[slots]\ndefault=\"life\"\n[slots.life]\nlabel=\"日常\"\nremind=\"08:30\"\ncheck=\"18:00\"\n[accounts]\ndefault=\"main\"\n[accounts.main]\nengine=\"test-engine\"\n[web]\nport=18850\n"
	if err := platform.AtomicWrite(filepath.Join(dir, "home.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	before(dir)
	loc, _ := time.LoadLocation("Asia/Taipei")
	clock := platform.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, loc))
	store := vault.New(vault.Deps{Clock: clock})
	if err := store.Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Stop(context.Background()) })
	return &homefixture.Home{Dir: dir, Clock: clock, Vault: store}
}

// TestBackupSweepMigrationAddsWeeklyJob：家的遷移放一份每週的備份掃描排程，排程清單看得到（系統級、週日 04:30、
// 不推播、下次執行時間算得出來）；再跑一次不重複、不蓋掉主人改過的；退回時只收遷移放的那一份。
func TestBackupSweepMigrationAddsWeeklyJob(t *testing.T) {
	h := openHome(t, func(dir string) {
		if err := AddBackupSweepJob(dir); err != nil {
			t.Fatal(err)
		}
		if err := AddBackupSweepJob(dir); err != nil {
			t.Fatal("再跑一次要沒事：", err)
		}
	})
	calls := []string{}
	m := newRig(t, h, &fakeBus{calls: &calls}, h.Vault)
	jobs, err := m.ListJobs(context.Background(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("排程清單要有一筆：%+v（不合格的 %v）", jobs, h.Vault.Invalid())
	}
	j := jobs[0]
	if j.ID != BackupSweepJobID || j.Action != "backup_sweep" || j.Agent != "" || !j.Enabled || !j.Silent || len(j.Cron) != 1 || j.Cron[0] != "30 4 * * 0" || j.NextRun == nil {
		t.Fatalf("每週的備份掃描排程：%+v", j)
	}
	if next := j.NextRun.In(h.Clock.Now().Location()); next.Weekday() != time.Sunday || next.Hour() != 4 || next.Minute() != 30 {
		t.Fatalf("下次執行要在週日 04:30：%v", next)
	}
	if j.Body == "" {
		t.Fatal("要有說明")
	}

	// 退回：遷移放的收掉；主人自己加的同名排程不動。
	if err := RemoveBackupSweepJob(h.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backupSweepPath(h.Dir)); !os.IsNotExist(err) {
		t.Fatal("退回要收掉遷移放的排程")
	}
	mine := []byte("---\nschema: 1\nid: backup-sweep\nname: 我自己的\ncron: [\"0 3 * * *\"]\nenabled: true\naction: backup_sweep\n---\n我的\n")
	if err := platform.AtomicWrite(backupSweepPath(h.Dir), mine, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddBackupSweepJob(h.Dir); err != nil {
		t.Fatal(err)
	}
	if err := RemoveBackupSweepJob(h.Dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(backupSweepPath(h.Dir)); string(got) != string(mine) {
		t.Fatalf("主人自己的不動：%s", got)
	}
}
