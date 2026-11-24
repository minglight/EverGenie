// internal/base/engine/module.go — ai_docs/modules/engine.md（REQ-87, REQ-144, REQ-83, REQ-84）。
package engine

import (
	"context"
	"time"

	"github.com/minglight/evergenie/sdk"
)

type Engine interface {
	Capabilities(ctx context.Context) (Capabilities, error)
	Start(ctx context.Context, s StartSpec) (Started, error)
	Stop(ctx context.Context, name string) error
	Deliver(ctx context.Context, session, envelopeID, text string) (delivered bool, reason string, err error)
	Transcript(ctx context.Context, session, sinceUUID string, follow bool) (<-chan ChatMessage, error)
	SetPermission(ctx context.Context, p PermissionPolicy, account string) ([]string, error)
	BootPrompts(ctx context.Context, session string) ([]BootPrompt, error)
	AnswerBootPrompt(ctx context.Context, id, choice, by string) error
	// AnswerApproval：逾時後留在 session 自己畫面上的確認，由引擎替主人回答（ENG-14、SES-48）。
	AnswerApproval(ctx context.Context, session string, p *sdk.Pending, d *sdk.Decision, by string) (answered bool, reason string, err error)
	// Command：主人下的對話指令，引擎直接在 session 上執行（ENG-17、SES-59）。Done=false 帶 Reason（sdk.CommandUnsupported、
	// sdk.CommandDialog、gone、其他＝沒送進去）。
	Command(ctx context.Context, session string, cmd sdk.SessionCommand, arg, by string) (*sdk.EngineCommandResponse, error)
	Usage(ctx context.Context, account string) (Usage, error)
	// Ask 做一次性對話（P6-5b）：不開工具、不留 session、不回報事件。req 的 Account／EngineConfig 由轉接層填。
	Ask(ctx context.Context, req AskSpec) (Asked, error)
}
type Capabilities = *sdk.EngineCapabilitiesResponse
type StartSpec = *sdk.EngineStartRequest
type Started = *sdk.EngineStartResponse
type ChatMessage = *sdk.ChatMessage
type PermissionPolicy = *sdk.PermissionPolicy
type BootPrompt = *sdk.BootPrompt
type Usage = *sdk.EngineUsageResponse
type AskSpec = *sdk.EngineAskRequest
type Asked = *sdk.EngineAskResponse
type BusSender interface {
	Publish(ctx context.Context, topic string, payload map[string]any) error
}
type Clock interface{ Now() time.Time }
type Deps struct {
	Bus   BusSender
	Clock Clock
}
