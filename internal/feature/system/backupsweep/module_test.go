package backupsweep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// sweepHome 建一個假的家與假的使用者家目錄，回已 Start 的模組與 bus（測試結束 Stop）。
func sweepHome(t *testing.T, extra string, probe func(Check) ([]Ref, error)) (*Module, *fakeBus, string, string) {
	t.Helper()
	h := homefixture.NewWith(t, extra)
	user := fakeUser(t)
	bus := newFakeBus()
	m := New(Deps{Bus: bus, Clock: platform.NewFakeClock(testNow), UserHome: user, Probe: probe,
		GitFiles: func(string) ([]string, error) { return nil, nil }})
	m.SetHome(h.Dir)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return m, bus, h.Dir, user
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// TestScheduledSweepWritesReportAndPrunesOnlySettingsBackups：排程到了（action backup_sweep）就掃、寫報告；
// 順便照保留規則清掉 runtime/backups/ 太舊的（每組最新 10 個、30 天內的留），整個家的每日備份一個都不動。
func TestScheduledSweepWritesReportAndPrunesOnlySettingsBackups(t *testing.T) {
	m, bus, home, user := sweepHome(t, "", noRefs)
	put(t, filepath.Join(user, "Workspace", "p", "a.bak-20260101-old"), testNow)
	backups := platform.RuntimeDir(home, "backups")
	day := 24 * time.Hour
	for i := 0; i < 13; i++ {
		age := time.Duration(31+i) * day
		if i == 0 {
			age = day
		}
		put(t, filepath.Join(backups, "trust", string(rune('a'+i))+".toml"), testNow.Add(-age))
	}
	daily := filepath.Join(platform.HomeBackupDir(home, "daily"), "home-20250101.tar.gz")
	put(t, daily, testNow.AddDate(-1, 0, 0))

	// 別的 action 不理。
	bus.events <- &sdk.BusSubscribeResponse{Topic: "schedule.fire", Payload: map[string]any{"id": "x", "action": "usage_warmup"}}
	rep := fire(t, m, bus)

	if rep.By != "system" || len(rep.Expired) != 1 || rep.Pruned != 3 || len(rep.Errors) != 0 {
		t.Fatalf("報告：by %s、到期 %d、清掉 %d、出錯 %v", rep.By, len(rep.Expired), rep.Pruned, rep.Errors)
	}
	left, _ := os.ReadDir(filepath.Join(backups, "trust"))
	if len(left) != 10 {
		t.Fatalf("trust 組要留最新 10 個：剩 %d", len(left))
	}
	if !exists(daily) {
		t.Fatal("整個家的每日備份不能動")
	}
	if !exists(filepath.Join(user, "Workspace", "p", "a.bak-20260101-old")) {
		t.Fatal("掃描只出報告、不刪")
	}
	md, err := os.ReadFile(ReportPath(home))
	if err != nil || !strings.Contains(string(md), "a.bak-20260101-old") || !strings.Contains(string(md), "清掉 3 個") {
		t.Fatalf("報告要寫到 %s：%v\n%s", ReportPath(home), err, md)
	}
	if got, err := loadReport(home); err != nil || got == nil || got.Pruned != 3 {
		t.Fatalf("讀回上一次的報告：%+v %v", got, err)
	}
	if len(bus.alerts()) != 0 {
		t.Fatalf("沒出錯不通知：%v", bus.alerts())
	}
}

// TestSweepErrorAlertsOwner：掃描出錯（「在用」檢查跑不起來）發 system.alert 給主人，內容帶原因與報告位置。
func TestSweepErrorAlertsOwner(t *testing.T) {
	probe := func(c Check) ([]Ref, error) {
		if c.Name == "launchd" {
			return nil, errors.New("讀不了")
		}
		return nil, nil
	}
	m, bus, home, user := sweepHome(t, "", probe)
	put(t, filepath.Join(user, "Workspace", "p", "a.bak"), testNow)
	rep := fire(t, m, bus)
	if len(rep.Errors) == 0 {
		t.Fatal("要記成出錯")
	}
	alerts := bus.alerts()
	if len(alerts) != 1 || alerts[0].payload["title"] != alertTitle {
		t.Fatalf("要發一則系統通知：%v", alerts)
	}
	ctx, _ := alerts[0].payload["context"].(string)
	if !strings.Contains(ctx, "開機服務") || !strings.Contains(ctx, ReportPath(home)) {
		t.Fatalf("通知要帶原因與報告位置：%s", ctx)
	}
}

// TestSandboxHomeSkipsScheduledSweep：沙盒家不掃（不碰這台電腦真的資料夾），也不清。
func TestSandboxHomeSkipsScheduledSweep(t *testing.T) {
	m, _, home, _ := sweepHome(t, "[runtime]\nsandbox = true\n", noRefs)
	ran := false
	m.done = func(*sdk.BackupSweepReport) { ran = true }
	m.scheduled(context.Background()) // 沙盒家當場就回，不開背景工作
	if ran || m.running.Load() {
		t.Fatal("沙盒家不該掃")
	}
	if exists(ReportPath(home)) {
		t.Fatal("沙盒家不該有報告")
	}
}

// TestManualSweepThroughAPI：手動掃一次（看板按鈕、`backup sweep --run`）回完整的報告；不帶 run 回上一次的。
// 主人叫的才順便清 runtime/backups/，秘書叫的只掃。
func TestManualSweepThroughAPI(t *testing.T) {
	m, _, home, user := sweepHome(t, "", noRefs)
	svc := &service{m: m}
	ctx := context.Background()
	if r, err := svc.BackupSweep(ctx, &sdk.SystemBackupSweepRequest{}); err != nil || r.Report != nil {
		t.Fatalf("還沒掃過：%+v %v", r, err)
	}
	if _, err := svc.BackupSweep(ctx, &sdk.SystemBackupSweepRequest{Run: true}); platform.CodeOf(err) == "" {
		t.Fatal("沒有身分不能叫")
	}
	put(t, filepath.Join(user, "Workspace", "p", "a.bak-20260101-old"), testNow)
	put(t, filepath.Join(user, "Workspace", "p", "b.bak"), testNow)
	backups := platform.RuntimeDir(home, "backups")
	for i := 0; i < 11; i++ {
		put(t, filepath.Join(backups, "trust", string(rune('a'+i))+".toml"), testNow.AddDate(0, 0, -40-i))
	}

	r, err := svc.BackupSweep(ctx, &sdk.SystemBackupSweepRequest{Run: true, By: &sdk.Actor{Name: "system-agent"}})
	if err != nil || r.Report == nil || len(r.Report.Expired) != 1 || len(r.Report.Misnamed) != 1 || r.Report.Pruned != 0 {
		t.Fatalf("秘書叫的只掃：%+v %v", r, err)
	}
	r, err = svc.BackupSweep(ctx, &sdk.SystemBackupSweepRequest{Run: true, By: &sdk.Actor{Name: "owner(web)"}})
	if err != nil || r.Report.Pruned != 1 || r.Report.By != "owner(web)" {
		t.Fatalf("主人叫的順便清：%+v %v", r.Report, err)
	}
	last, err := svc.BackupSweep(ctx, &sdk.SystemBackupSweepRequest{})
	if err != nil || last.Report == nil || last.Report.By != "owner(web)" || last.Report.File != ReportPath(home) {
		t.Fatalf("回上一次的報告：%+v %v", last, err)
	}
}

// TestInterruptedSweepKeepsLastReport：服務收工把掃描切斷時，不完整的報告不寫（上一次完整的留著）、不通知；收工後手動的也不再開。
func TestInterruptedSweepKeepsLastReport(t *testing.T) {
	m, bus, home, user := sweepHome(t, "", noRefs)
	put(t, filepath.Join(user, "Workspace", "p", "a.bak"), testNow)
	first := fire(t, m, bus)
	if first == nil || !exists(ReportPath(home)) {
		t.Fatal("第一次要寫報告")
	}
	put(t, filepath.Join(user, "Workspace", "p", "b.bak"), testNow)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.sweep(ctx, "system", true)
	if got, _ := loadReport(home); got == nil || len(got.Misnamed) != 1 || len(bus.alerts()) != 0 {
		t.Fatalf("切斷的那次不寫、不通知：%+v %v", got, bus.alerts())
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := (&service{m: m}).BackupSweep(context.Background(), &sdk.SystemBackupSweepRequest{Run: true, By: &sdk.Actor{Name: "owner(web)"}}); !isCode(err, sdk.ErrorCodeUnavailable) {
		t.Fatalf("收工後不再掃：%v", err)
	}
}

func isCode(err error, code sdk.ErrorCode) bool {
	var e *sdk.Error
	return errors.As(err, &e) && e.Code == code
}
