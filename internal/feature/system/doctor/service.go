package doctor

import (
	"context"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 整體健康檢查（`evergenie doctor`、看板健康狀態的推播一項；REQ-117）。只報告，不修復。
// 業務模組各自在 Checks() 登記自己的項目（名稱「群組.項目」），這裡依序呼叫、翻成白話的 DoctorCheck；doctor 自己只有「核心版本」一條。

// Doctor 是 sdk `system.doctor` 的實作（http.go 的 Routes 掛上去）：groups 空＝全部。
func (m *Module) Doctor(ctx context.Context, req *sdk.SystemDoctorRequest) (*sdk.SystemDoctorResponse, error) {
	want := map[sdk.DoctorGroup]bool{}
	for _, g := range req.Groups {
		known := false
		for _, k := range doctorGroups {
			known = known || k == g
		}
		if !known {
			return nil, platform.Invalid("groups", "不認得的檢查項目")
		}
		want[g] = true
	}
	features := m.featureChecks()
	out := &sdk.SystemDoctorResponse{Checks: []sdk.DoctorCheck{}}
	for _, g := range doctorGroups {
		if len(want) > 0 && !want[g] {
			continue
		}
		out.Checks = append(out.Checks, m.runGroup(ctx, g, features[g])...)
	}
	return out, nil
}

// runGroup 給一個 group 的檢查 doctorTimeout 的時間，並擋住 panic。至少回一條。
func (m *Module) runGroup(ctx context.Context, g sdk.DoctorGroup, features []app.Check) []sdk.DoctorCheck {
	cctx, cancel := context.WithTimeout(ctx, doctorTimeout)
	defer cancel()
	done := make(chan []sdk.DoctorCheck, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- []sdk.DoctorCheck{checkWarn(g, string(g)+".error", "這一項檢查中斷了", "請看 runtime/logs 裡的紀錄")}
			}
		}()
		done <- append(m.groupChecks(g), runFeatureChecks(cctx, g, features)...)
	}()
	select {
	case checks := <-done:
		if len(checks) == 0 {
			return []sdk.DoctorCheck{checkOK(g, string(g)+".ok", "沒有發現問題")}
		}
		return checks
	case <-cctx.Done():
		return []sdk.DoctorCheck{checkWarn(g, string(g)+".timeout", "檢查逾時（超過 "+doctorTimeout.String()+"）", "稍後再執行一次；一直逾時請看 runtime/logs")}
	}
}

// groupChecks 是 doctor 自己的檢查：只有 config 群組第一條「核心版本」。其他每一項都由各業務模組的 Checks() 登記
// （2026-11-01：名冊 agents、對話 chat、套件 packages、權限 trust、資料 data、排程 jobs、推播 notify）。
func (m *Module) groupChecks(g sdk.DoctorGroup) []sdk.DoctorCheck {
	if g == sdk.DoctorGroupConfig {
		// 第一條固定是核心版本：別的程式探測這個家的版本讀它（VersionDetailPrefix）。
		return []sdk.DoctorCheck{checkOK(g, "config.version", VersionDetailPrefix+platform.Version())}
	}
	return nil
}

// runFeatureChecks 跑業務模組登記的健檢；一項 panic 只讓那一項變黃字，不影響其他項。
func runFeatureChecks(ctx context.Context, g sdk.DoctorGroup, checks []app.Check) []sdk.DoctorCheck {
	var out []sdk.DoctorCheck
	for _, c := range checks {
		out = append(out, runFeatureCheck(ctx, g, c)...)
	}
	return out
}

func runFeatureCheck(ctx context.Context, g sdk.DoctorGroup, c app.Check) (out []sdk.DoctorCheck) {
	defer func() {
		if recover() != nil {
			out = []sdk.DoctorCheck{checkWarn(g, c.Name, "這一項檢查中斷了", "請看 runtime/logs 裡的紀錄")}
		}
	}()
	if c.RunAll != nil {
		for _, r := range c.RunAll(ctx) {
			out = append(out, toDoctorCheck(g, c.Name, r))
		}
		return out
	}
	return []sdk.DoctorCheck{toDoctorCheck(g, c.Name, c.Run(ctx))}
}

func toDoctorCheck(g sdk.DoctorGroup, id string, r app.CheckResult) sdk.DoctorCheck {
	switch r.Status {
	case app.StatusOK:
		return checkOK(g, id, r.Message)
	case app.StatusError:
		return checkError(g, id, r.Message, r.Fix)
	}
	return checkWarn(g, id, r.Message, r.Fix)
}

func checkOK(g sdk.DoctorGroup, id, detail string) sdk.DoctorCheck {
	return sdk.DoctorCheck{ID: id, Group: g, Ok: true, Severity: sdk.DoctorSeverityOk, Detail: detail}
}
func checkWarn(g sdk.DoctorGroup, id, detail, fix string) sdk.DoctorCheck {
	return sdk.DoctorCheck{ID: id, Group: g, Severity: sdk.DoctorSeverityWarn, Detail: detail, Fix: fix}
}
func checkError(g sdk.DoctorGroup, id, detail, fix string) sdk.DoctorCheck {
	return sdk.DoctorCheck{ID: id, Group: g, Severity: sdk.DoctorSeverityError, Detail: detail, Fix: fix}
}

// VersionDetailPrefix 是 `system.doctor` 的 config 群組裡「核心版本」那一條 detail 的開頭；
// 別的程式（例：admin 套件探測開發家）從這一條讀出那個家的版本。
const VersionDetailPrefix = "核心版本："
