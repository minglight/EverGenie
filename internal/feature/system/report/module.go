// internal/feature/system/report/module.go — 說明卡 ai_docs/modules/system.md。
package report

import (
	"context"
	"sync"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Doctor 是服務裡跑健檢的那一個（doctor 模組）；report 只要它的結果。
type Doctor interface {
	Doctor(ctx context.Context, req *sdk.SystemDoctorRequest) (*sdk.SystemDoctorResponse, error)
}

// Deps：Doctor 可以是 nil（回報包的健檢那段就是空的）；Offline 是不用服務的那幾項（doctor.OfflineChecks），可以是 nil。
type Deps struct {
	Doctor  Doctor
	Offline func(ctx context.Context, home string) []sdk.DoctorCheck
	Clock   platform.Clock
}

// Module 是看板「回報問題」的後端：sdk 的 `system.report`。沒有背景工作、不寫任何東西。
type Module struct {
	d    Deps
	mu   sync.Mutex
	home string
}

func New(d Deps) *Module                      { return &Module{d: d} }
func (m *Module) Name() string                { return "report" }
func (m *Module) SetHome(dir string)          { m.mu.Lock(); defer m.mu.Unlock(); m.home = dir }
func (m *Module) Start(context.Context) error { return nil }
func (m *Module) Stop(context.Context) error  { return nil }

// Routes：sdk 的 system 服務裡只有 report 一個方法由這裡實作（/api/system/report）。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "system", Impl: &service{m: m}, Methods: []string{"report"}}}
}

type service struct{ m *Module }

var _ interface {
	Report(context.Context, *sdk.SystemReportRequest) (*sdk.SystemReportResponse, error)
} = (*service)(nil)

// Report 產生一份回報包給看板預覽。只產生、不送出（RPT-4）。
func (s *service) Report(ctx context.Context, req *sdk.SystemReportRequest) (*sdk.SystemReportResponse, error) {
	s.m.mu.Lock()
	home := s.m.home
	s.m.mu.Unlock()
	var checks []sdk.DoctorCheck
	if s.m.d.Offline != nil {
		checks = append(checks, s.m.d.Offline(ctx, home)...)
	}
	if s.m.d.Doctor != nil {
		if res, err := s.m.d.Doctor.Doctor(ctx, &sdk.SystemDoctorRequest{}); err == nil {
			checks = append(checks, res.Checks...)
		} else {
			checks = append(checks, sdk.DoctorCheck{ID: "serve.doctor", Group: sdk.DoctorGroupConfig, Severity: sdk.DoctorSeverityError, Detail: "健康檢查沒跑完"})
		}
	}
	o := Options{Home: home, Detail: req != nil && req.Detail, Checks: checks}
	if s.m.d.Clock != nil {
		o.Now = s.m.d.Clock.Now()
	}
	b := Build(ctx, o)
	return &sdk.SystemReportResponse{Title: b.Title, Body: b.Body, IssueLink: b.Link}, nil
}

var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
)
