package doctor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// stubChecks 是假的「業務模組登記的健檢」。
func stubChecks(cs ...app.Check) []app.Check { return cs }

// consume 代替 app 組裝：把健檢交給 doctor（名稱不合格就讓測試失敗）。
func consume(t *testing.T, m *Module, cs []app.Check) {
	t.Helper()
	if err := m.ConsumeChecks(cs); err != nil {
		t.Fatal(err)
	}
}

func scheduleCheck(r app.CheckResult) app.Check {
	return app.Check{Name: "schedule.jobs", Run: func(context.Context) app.CheckResult { return r }}
}

func doctorOf(t *testing.T, m *Module, groups ...sdk.DoctorGroup) []sdk.DoctorCheck {
	t.Helper()
	res, err := m.Doctor(context.Background(), &sdk.SystemDoctorRequest{Groups: groups})
	if err != nil {
		t.Fatal(err)
	}
	return res.Checks
}

// 各群組的嚴重度、業務模組登記的健檢（Run 一條、RunAll 多條）併進對應群組、config 第一條是核心版本、groups 過濾、不認得的群組。
// Guards: REQ-117
func TestDoctorGroupsAndSeverity(t *testing.T) {
	m := New(Deps{})
	consume(t, m, stubChecks(
		scheduleCheck(app.CheckResult{Status: app.StatusError, Message: "排程「壞的」（code/bad）：排程格式不正確", Fix: "修排程檔"}),
		app.Check{Name: "permissions.drift", RunAll: func(context.Context) []app.CheckResult {
			return []app.CheckResult{{Status: app.StatusError, Message: "帳號 a：設定檔被手改", Fix: "重啟"}, {Status: app.StatusWarn, Message: "帳號 b：沒在跑"}}
		}},
		app.Check{Name: "data.invalid", RunAll: func(context.Context) []app.CheckResult { return nil }},
	))
	checks := doctorOf(t, m)
	byGroup := map[sdk.DoctorGroup][]sdk.DoctorCheck{}
	for _, c := range checks {
		byGroup[c.Group] = append(byGroup[c.Group], c)
		if c.Severity == sdk.DoctorSeverityOk != c.Ok {
			t.Errorf("Ok 要跟 Severity 一致：%+v", c)
		}
		if c.Detail == "" {
			t.Errorf("每條都要有白話說明：%+v", c)
		}
	}
	// 沒有任何登記的 group（含 RunAll 回空的 data）也要有一條（沒問題給 ok）。
	for _, g := range doctorGroups {
		if len(byGroup[g]) == 0 {
			t.Errorf("group %s 至少一條", g)
		}
	}
	if d := byGroup[sdk.DoctorGroupData]; len(d) != 1 || d[0].ID != "data.ok" {
		t.Errorf("RunAll 回空的整組補一條 ok：%+v", d)
	}
	if cfg := byGroup[sdk.DoctorGroupConfig]; len(cfg) != 1 || cfg[0].ID != "config.version" || !strings.HasPrefix(cfg[0].Detail, VersionDetailPrefix) {
		t.Errorf("config 第一條是核心版本：%+v", cfg)
	}
	perm := byGroup[sdk.DoctorGroupPermissions]
	if len(perm) != 2 || perm[0].Severity != sdk.DoctorSeverityError || perm[0].ID != "permissions.drift" || perm[0].Fix == "" || perm[1].Severity != sdk.DoctorSeverityWarn || perm[1].ID != "permissions.drift" {
		t.Errorf("RunAll 每條結果一行、同一個編號：%+v", perm)
	}
	if sched := byGroup[sdk.DoctorGroupSchedule]; len(sched) != 1 || sched[0].ID != "schedule.jobs" || sched[0].Severity != sdk.DoctorSeverityError || !strings.Contains(sched[0].Detail, "code/bad") || sched[0].Fix != "修排程檔" {
		t.Errorf("業務模組登記的健檢要進 schedule 群組，帶 ID、錯誤等級與修法：%+v", sched)
	}

	// 只跑指定的 group。
	only := doctorOf(t, m, sdk.DoctorGroupSchedule)
	if len(only) != 1 || only[0].Group != sdk.DoctorGroupSchedule {
		t.Errorf("groups 過濾：%+v", only)
	}
	_, err := m.Doctor(context.Background(), &sdk.SystemDoctorRequest{Groups: []sdk.DoctorGroup{"nope"}})
	if e := platform.SDKError(err); err == nil || e.Code != sdk.ErrorCodeInvalid || e.Field != "groups" {
		t.Errorf("不認得的 group 要回 groups 欄位的錯誤：%v", err)
	}
}

// 狀態對應：OK／提醒／錯誤；一項健檢 panic 只讓那一項變黃字，同群組其他項照常。
// Guards: REQ-117
func TestDoctorFeatureCheckStatusAndPanic(t *testing.T) {
	m := New(Deps{})
	consume(t, m, stubChecks(
		app.Check{Name: "push.status", Run: func(context.Context) app.CheckResult {
			return app.CheckResult{Status: app.StatusWarn, Message: "還沒有裝置開啟推播", Fix: "在手機開啟"}
		}},
		app.Check{Name: "push.boom", Run: func(context.Context) app.CheckResult { panic("x") }},
		app.Check{Name: "push.fine", Run: func(context.Context) app.CheckResult { return app.CheckResult{Status: app.StatusOK, Message: "好"} }},
	))
	got := doctorOf(t, m, sdk.DoctorGroupPush)
	if len(got) != 3 {
		t.Fatalf("三項：%+v", got)
	}
	if got[0].Severity != sdk.DoctorSeverityWarn || got[0].Fix != "在手機開啟" || got[0].Ok {
		t.Errorf("提醒對應黃字：%+v", got[0])
	}
	if got[1].ID != "push.boom" || got[1].Severity != sdk.DoctorSeverityWarn || !strings.Contains(got[1].Detail, "中斷") {
		t.Errorf("panic 只讓那一項變黃字：%+v", got[1])
	}
	if got[2].Severity != sdk.DoctorSeverityOk || !got[2].Ok {
		t.Errorf("其他項照常：%+v", got[2])
	}
}

// 健檢名稱不是「群組.項目」或群組不認得：app 組裝（ConsumeChecks）時就報錯，不等到有人跑 doctor。
// Guards: REQ-117
func TestConsumeChecksRejectsBadName(t *testing.T) {
	for _, name := range []string{"nodot", "nope.thing", ".x"} {
		m := New(Deps{})
		bad := app.Check{Name: name, Run: func(context.Context) app.CheckResult { return app.CheckResult{} }}
		if err := app.New(m, &checkSource{checks: []app.Check{bad}}).Assemble(); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%q 要在組裝時報錯：%v", name, err)
		}
	}
	m := New(Deps{})
	if err := app.New(m, &checkSource{checks: []app.Check{scheduleCheck(app.CheckResult{})}}).Assemble(); err != nil {
		t.Fatal(err)
	}
}

// checkSource 是登記健檢的假業務模組。
type checkSource struct{ checks []app.Check }

func (c *checkSource) Name() string                { return "src" }
func (c *checkSource) Start(context.Context) error { return nil }
func (c *checkSource) Stop(context.Context) error  { return nil }
func (c *checkSource) Checks() []app.Check         { return c.checks }

// Guards: REQ-117
func TestDoctorGroupTimeoutDoesNotHangEverything(t *testing.T) {
	old := doctorTimeout
	doctorTimeout = 50 * time.Millisecond
	t.Cleanup(func() { doctorTimeout = old })
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	m := New(Deps{})
	consume(t, m, stubChecks(
		app.Check{Name: "permissions.drift", RunAll: func(context.Context) []app.CheckResult { <-block; return nil }},
		scheduleCheck(app.CheckResult{Status: app.StatusOK, Message: "排程都載入了"}),
	))
	started := time.Now()
	checks := doctorOf(t, m, sdk.DoctorGroupPermissions, sdk.DoctorGroupSchedule)
	if time.Since(started) > 2*time.Second {
		t.Fatal("逾時的 group 不該拖住整個 doctor")
	}
	if len(checks) != 2 || checks[0].Severity != sdk.DoctorSeverityWarn || !strings.Contains(checks[0].Detail, "逾時") || checks[1].Severity != sdk.DoctorSeverityOk {
		t.Fatalf("逾時一條 warn，其他照常：%+v", checks)
	}
}
