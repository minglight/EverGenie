package data

import (
	"context"
	"fmt"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 資料的健檢（doctor 的 data 群組；2026-11-01 從 doctor 搬來，doctor 只收集）：vault 裡不符合規格的紀錄。

// maxListed 是壞資料最多逐條列出幾條，其餘併成一句「另有 n 筆」。
const maxListed = 20

// dataChecker 是 vault「額外有」的掃描（真的模組有；測試的假物件可以沒有，沒有就不登記這一項）。
type dataChecker interface {
	InvalidRecords(ctx context.Context) ([]vault.InvalidRecord, error)
}

// Checks 登記 data.invalid：每筆不符合規格的紀錄一條紅字（最多 20 筆），都符合給一條綠字。
func (m *Module) Checks() []app.Check {
	dc, ok := m.d.Vault.(dataChecker)
	if !ok {
		return nil
	}
	return []app.Check{{Name: "data.invalid", RunAll: func(ctx context.Context) []app.CheckResult { return invalidRecords(ctx, dc) }}}
}

func invalidRecords(ctx context.Context, dc dataChecker) []app.CheckResult {
	bad, err := dc.InvalidRecords(ctx)
	if err != nil {
		msg := "發生內部錯誤"
		if e := platform.SDKError(err); e.Code != sdk.ErrorCodeInternal {
			msg = e.Message
		}
		return []app.CheckResult{{Status: app.StatusWarn, Message: "沒辦法掃描資料：" + msg}}
	}
	var out []app.CheckResult
	for i, r := range bad {
		if i == maxListed {
			out = append(out, app.CheckResult{Status: app.StatusError, Message: fmt.Sprintf("另外還有 %d 筆資料有問題，修好上面的再檢查一次", len(bad)-maxListed)})
			break
		}
		what := r.Path
		if r.Kind != "" {
			what = fmt.Sprintf("%s %s（%s）", r.Kind, r.ID, r.Path)
		}
		if r.Field != "" {
			what += "，欄位 " + r.Field
		}
		out = append(out, app.CheckResult{Status: app.StatusError, Message: what + "：" + r.Reason, Fix: "打開那個檔案修正，或請負責的秘書處理；修好後不用重啟"})
	}
	if len(out) == 0 {
		out = append(out, app.CheckResult{Status: app.StatusOK, Message: "所有資料都符合規格"})
	}
	return out
}
