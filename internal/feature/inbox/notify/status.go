package notify

import (
	"context"
	"fmt"

	"github.com/minglight/evergenie/internal/base/app"
)

const pushKeyMissing = "推播金鑰還沒設定"

// PushState 是推播現在能不能用：金鑰設定好、而且至少一台裝置開啟才算 Ok（D-14）。
type PushState struct {
	Ok     bool
	Detail string // 白話：「N 台裝置」或哪裡還沒好
	Fix    string // 沒好的時候下一步怎麼做；Ok 時是空的
}

// PushStatus 是看板「健康狀態」的推播一項與 `evergenie doctor` 的 push 檢查共用的判斷（只有這一份）。
// 不回金鑰、不回裝置網址；錯誤原因不帶欄位值。
func (m *Module) PushStatus(ctx context.Context) PushState {
	key, err := m.PushKey(ctx)
	switch {
	case err != nil && err.Error() == pushKeyMissing:
		return PushState{Detail: "還沒設定推播金鑰", Fix: "執行 evergenie push init（會做好金鑰並接到 home.toml 的 [secrets]），再重開 serve"}
	case err != nil:
		return PushState{Detail: "推播設定讀不了", Fix: "檢查 settings/push.toml 的格式，或重新在手機上開啟推播"}
	case key.Subscriptions < 1:
		return PushState{Detail: "還沒有裝置開啟推播", Fix: "在手機的看板「設定→裝置推播」開啟推播"}
	}
	return PushState{Ok: true, Detail: fmt.Sprintf("%d 台裝置", key.Subscriptions)}
}

// Checks 登記 notify 的健檢項目（app.CheckProvider）：推播現況，判斷只有 PushStatus 這一份。
func (m *Module) Checks() []app.Check {
	return []app.Check{{Name: "push.status", Run: func(ctx context.Context) app.CheckResult {
		s := m.PushStatus(ctx)
		if s.Ok {
			return app.CheckResult{Status: app.StatusOK, Message: s.Detail}
		}
		return app.CheckResult{Status: app.StatusWarn, Message: s.Detail, Fix: s.Fix}
	}}}
}
