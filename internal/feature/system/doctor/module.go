// internal/feature/system/doctor/module.go — 說明卡 ai_docs/modules/system.md。
package doctor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// SessionAccess 是 session 提供的清單（看板健康頁 web.get_health 用）。
type SessionAccess interface {
	List(ctx context.Context, agent string, states []sdk.SessionState) ([]*sdk.Session, error)
}

// Deps 都可以是 nil（測試與單獨起來的情況）：只有看板健康頁 web.get_health 用得到；
// `system.doctor` 的每一項檢查都由各業務模組的 Checks() 登記，doctor 只收集（2026-11-01）。
type Deps struct {
	Sessions SessionAccess
	Roster   Roster // 看板的健康頁（web.get_health）列常駐秘書用
}

// 每個群組的檢查上限；逾時就寫一條 warn，不讓整個 doctor 卡住。
var doctorTimeout = 5 * time.Second

var doctorGroups = []sdk.DoctorGroup{
	sdk.DoctorGroupConfig, sdk.DoctorGroupSessions, sdk.DoctorGroupPackages, sdk.DoctorGroupPermissions,
	sdk.DoctorGroupSchedule, sdk.DoctorGroupData, sdk.DoctorGroupPush,
}

// Module 是 doctor 業務模組。它沒有背景工作；HTTP 在 http.go：sdk 的 `system.doctor` 是共用 `system` 服務的一個方法，
// 用 Mount.Methods 只登記這一個方法。
type Module struct {
	d      Deps
	mu     sync.Mutex
	home   string
	log    *slog.Logger
	checks map[sdk.DoctorGroup][]app.Check // app 組裝時交進來（ConsumeChecks），依群組分好
}

func New(d Deps) *Module             { return &Module{d: d} }
func (m *Module) Name() string       { return "doctor" }
func (m *Module) SetHome(dir string) { m.mu.Lock(); defer m.mu.Unlock(); m.home = dir }

// ConsumeChecks 收下各業務模組登記的健檢（app.CheckConsumer；app 組裝時呼叫，在 Start 之前）：依群組分好；
// 名稱不是「群組.項目」或群組不認得就回錯——名稱寫錯是程式的錯，組裝時就擋下，不等到有人跑 doctor。
func (m *Module) ConsumeChecks(all []app.Check) error {
	out := map[sdk.DoctorGroup][]app.Check{}
	for _, c := range all {
		g, ok := groupOf(c.Name)
		if !ok {
			return fmt.Errorf("健檢項目「%s」的名稱要寫成「群組.項目」，群組是 %s 其中之一", c.Name, groupNames())
		}
		out[g] = append(out[g], c)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checks = out
	return nil
}

// Start 只開 log。
func (m *Module) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	home := m.home
	m.mu.Unlock()
	if home != "" {
		if h, err := platform.LoadHome(home); err == nil {
			l := platform.NewLogger(h, "doctor")
			m.mu.Lock()
			m.log = l
			m.mu.Unlock()
		}
	}
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) logger() *slog.Logger { m.mu.Lock(); defer m.mu.Unlock(); return m.log }

// featureChecks 回依群組分好的業務模組健檢（還沒組裝就是空的）。
func (m *Module) featureChecks() map[sdk.DoctorGroup][]app.Check {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.checks
}

func groupOf(name string) (sdk.DoctorGroup, bool) {
	prefix, _, ok := strings.Cut(name, ".")
	if !ok {
		return "", false
	}
	for _, g := range doctorGroups {
		if string(g) == prefix {
			return g, true
		}
	}
	return "", false
}

func groupNames() string {
	names := make([]string, len(doctorGroups))
	for i, g := range doctorGroups {
		names[i] = string(g)
	}
	return strings.Join(names, "、")
}

// 登記方法（app 的登記介面）：Routes 在 http.go，Commands 在 cli.go。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
	_ app.CheckConsumer = (*Module)(nil)
)
