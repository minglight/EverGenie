package engine

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Registry resolves account names; static registries map account names directly to test APIs.
type Registry struct {
	mu       sync.RWMutex
	accounts map[string]account
	clients  map[string]sdk.EngineAPI
	static   map[string]sdk.EngineAPI
	log      *slog.Logger
	rules    func(string) []sdk.PolicyRule // 開 session 時填 Rules（權限變更申請，Q7）
}
type account struct {
	engine string
	config map[string]any
	warmup bool // home.toml 的 accounts.<名>.usage_warmup：每天用額度暖機排程先起算視窗（REQ-86）
}

func NewStatic(apis map[string]sdk.EngineAPI) *Registry {
	r := &Registry{static: map[string]sdk.EngineAPI{}}
	for name, api := range apis {
		r.static[name] = api
	}
	return r
}
func (r *Registry) For(name string) (Engine, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.static != nil {
		api, ok := r.static[name]
		if !ok {
			return nil, platform.NotFound("找不到帳號的引擎")
		}
		if api == nil {
			return nil, platform.Unavailable("引擎尚未就緒")
		}
		return &adapter{api: api, account: name, log: r.log, rules: r.rules}, nil
	}
	a, ok := r.accounts[name]
	if !ok {
		return nil, platform.NotFound("找不到這個帳號")
	}
	api, known := r.clients[a.engine]
	if !known {
		return nil, platform.NotFound("找不到帳號指定的引擎")
	}
	if api == nil {
		return nil, platform.Unavailable("引擎尚未就緒，請稍後重試")
	}
	return &adapter{api: api, account: name, config: a.config, log: r.log, rules: r.rules}, nil
}

type adapter struct {
	api     sdk.EngineAPI
	account string
	config  map[string]any
	log     *slog.Logger
	rules   func(string) []sdk.PolicyRule
}

func fromSDKError(err error) error {
	if err == nil {
		return nil
	}
	var e *sdk.Error
	if errors.As(err, &e) {
		return &platform.Err{Code: e.Code, Field: e.Field, Message: e.Message}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return platform.Unavailable("無法連線到引擎，請稍後重試")
}
func (a *adapter) Capabilities(ctx context.Context) (Capabilities, error) {
	r, e := a.api.Capabilities(ctx, &sdk.EngineCapabilitiesRequest{})
	return r, fromSDKError(e)
}
func (a *adapter) Start(ctx context.Context, s StartSpec) (Started, error) {
	if s == nil {
		return nil, platform.Invalid("session", "請提供對話設定")
	}
	req := *s
	req.Account = a.account
	if a.config != nil {
		req.EngineConfig = a.config
	}
	if a.rules != nil && req.Agent != "" {
		req.Rules = a.rules(req.Agent)
	}
	r, e := a.api.Start(ctx, &req)
	return r, fromSDKError(e)
}
func (a *adapter) Stop(ctx context.Context, name string) error {
	_, e := a.api.Stop(ctx, &sdk.EngineStopRequest{Name: name})
	return fromSDKError(e)
}
func (a *adapter) Deliver(ctx context.Context, session, id, text string) (bool, string, error) {
	return a.deliver(ctx, &sdk.EngineDeliverRequest{Session: session, EnvelopeID: id, Text: text})
}

// DeliverWith：同 Deliver，另外告訴引擎這則以前貼過（resend）、或這次一定要重新貼不接受「已經在對話裡」（force）
// （sdk.EngineDeliverRequest.Resend／Force）。
func (a *adapter) DeliverWith(ctx context.Context, session, id, text string, force, resend bool) (bool, string, error) {
	return a.deliver(ctx, &sdk.EngineDeliverRequest{Session: session, EnvelopeID: id, Text: text, Force: force, Resend: resend})
}

func (a *adapter) deliver(ctx context.Context, req *sdk.EngineDeliverRequest) (bool, string, error) {
	r, e := a.api.Deliver(ctx, req)
	if e != nil {
		return false, "", fromSDKError(e)
	}
	if r == nil {
		return false, "", platform.Unavailable("引擎沒有回覆")
	}
	return r.Delivered, r.Reason, nil
}
func (a *adapter) Transcript(ctx context.Context, session, since string, follow bool) (<-chan ChatMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ch := make(chan ChatMessage)
	go func() {
		defer close(ch)
		defer func() {
			if recover() != nil && a.log != nil {
				a.log.Warn("引擎對話記錄處理中斷", "session", session)
			}
		}()
		err := a.api.Transcript(ctx, &sdk.EngineTranscriptRequest{Session: session, SinceUuid: since, Follow: follow}, func(r *sdk.EngineTranscriptResponse) error {
			if r == nil || r.Message == nil {
				return nil
			}
			select {
			case ch <- r.Message:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if err != nil && ctx.Err() == nil && a.log != nil {
			a.log.Warn("無法讀取引擎對話記錄", "session", session)
		}
	}()
	return ch, nil
}

// ReadTranscript 把 since 之後的對話記錄整段讀回來（不跟讀）。跟 Transcript 不同的是引擎回錯時把錯誤交回去：
// 算未讀、記已讀的人要分得出「讀完了、沒有新的」與「沒讀到」。
func (a *adapter) ReadTranscript(ctx context.Context, session, since string) ([]ChatMessage, error) {
	out := []ChatMessage{}
	err := a.api.Transcript(ctx, &sdk.EngineTranscriptRequest{Session: session, SinceUuid: since}, func(r *sdk.EngineTranscriptResponse) error {
		if r != nil && r.Message != nil {
			out = append(out, r.Message)
		}
		return nil
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, fromSDKError(err)
	}
	return out, nil
}
func (a *adapter) SetPermission(ctx context.Context, p PermissionPolicy, account string) ([]string, error) {
	r, e := a.api.SetPermission(ctx, &sdk.EngineSetPermissionRequest{Policy: p, Account: account})
	if e != nil {
		return nil, fromSDKError(e)
	}
	return r.Artifacts, nil
}
func (a *adapter) BootPrompts(ctx context.Context, session string) ([]BootPrompt, error) {
	r, e := a.api.ListBootPrompts(ctx, &sdk.EngineListBootPromptsRequest{Session: session})
	if e != nil {
		return nil, fromSDKError(e)
	}
	out := make([]BootPrompt, len(r.Prompts))
	for i := range r.Prompts {
		out[i] = &r.Prompts[i]
	}
	return out, nil
}
func (a *adapter) AnswerBootPrompt(ctx context.Context, id, choice, by string) error {
	_, e := a.api.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: id, Choice: choice, By: &sdk.Actor{Name: by}})
	return fromSDKError(e)
}
func (a *adapter) AnswerApproval(ctx context.Context, session string, p *sdk.Pending, d *sdk.Decision, by string) (bool, string, error) {
	r, e := a.api.AnswerApproval(ctx, &sdk.EngineAnswerApprovalRequest{Session: session, Pending: p, Decision: d, By: &sdk.Actor{Name: by}})
	if e != nil {
		return false, "", fromSDKError(e)
	}
	if r == nil {
		return false, "", platform.Unavailable("引擎沒有回覆")
	}
	return r.Answered, r.Reason, nil
}
func (a *adapter) Command(ctx context.Context, session string, cmd sdk.SessionCommand, arg, by string) (*sdk.EngineCommandResponse, error) {
	r, e := a.api.Command(ctx, &sdk.EngineCommandRequest{Session: session, Command: cmd, Arg: arg, By: &sdk.Actor{Name: by}})
	if e != nil {
		return nil, fromSDKError(e)
	}
	if r == nil {
		return nil, platform.Unavailable("引擎沒有回覆")
	}
	return r, nil
}
func (a *adapter) Usage(ctx context.Context, account string) (Usage, error) {
	r, e := a.api.Usage(ctx, &sdk.EngineUsageRequest{Account: account, EngineConfig: a.config})
	return r, fromSDKError(e)
}
func (a *adapter) Ask(ctx context.Context, s AskSpec) (Asked, error) {
	if s == nil {
		return nil, platform.Invalid("ask", "請提供對話內容")
	}
	req := *s
	req.Account = a.account
	if a.config != nil {
		req.EngineConfig = a.config
	}
	r, e := a.api.Ask(ctx, &req)
	return r, fromSDKError(e)
}

var _ Engine = (*adapter)(nil)
