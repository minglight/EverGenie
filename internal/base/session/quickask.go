package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// QuickAsk 問引擎一次短問題（語音入口猜給誰，DEV-144），回它的文字：用家的預設帳號，不開工具、不留對話、不碰常駐 session。
// model 空＝帳號預設；timeout 秒，0＝引擎預設。
func (m *Module) QuickAsk(ctx context.Context, model, effort, instructions, message string, timeout int32) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", platform.Invalid("message", "沒有要問的內容")
	}
	account, table, engine, err := m.seatAccount(vault.Record{}) // 設定只在 Start 寫一次，跟測口吻一樣不加鎖讀
	if err != nil {
		return "", err
	}
	config, _ := table["engine_config"].(map[string]any)
	resp, err := m.ask(ctx, engine, &sdk.EngineAskRequest{
		Cwd: m.workdir(), Account: account, EngineConfig: config, Model: model, Effort: effort,
		Instructions: instructions, Message: message, TimeoutSeconds: timeout,
	})
	if err != nil {
		return "", err
	}
	if resp == nil || strings.TrimSpace(resp.Reply) == "" {
		return "", platform.Unavailable("引擎沒有回覆")
	}
	return strings.TrimSpace(resp.Reply), nil
}

// ask 問引擎一次短問題。引擎照 req.TimeoutSeconds 自己會放手；核心多等 limits.AskGrace，
// 連引擎本身都不回的時候這裡也有個底，回一句白話。
func (m *Module) ask(ctx context.Context, engine EngineAccess, req *sdk.EngineAskRequest) (*sdk.EngineAskResponse, error) {
	wait := time.Duration(req.TimeoutSeconds) * time.Second
	if req.TimeoutSeconds <= 0 {
		wait = askDefault
	}
	wait += m.limits.AskGrace
	call, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	resp, err := engine.Ask(call, req)
	if err != nil && gaveUp(call, ctx) {
		return nil, platform.Unavailable(fmt.Sprintf("引擎沒回應：問一個短問題等了 %d 秒沒有結果。這次沒有答案，過一下再試。", int(wait/time.Second)))
	}
	return resp, err
}
