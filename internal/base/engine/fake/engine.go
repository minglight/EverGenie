// Package fake provides a scriptable engine that uses the same host boundary as a process engine.
package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/minglight/evergenie/sdk"
	"strings"
	"sync"
	"time"
)

type Call struct {
	Method  string
	Request any
}
type Behavior struct {
	// Delivered：Deliver 回 delivered=true＝平台已經收下這句（ENG-13；假引擎記進對話後馬上回，不模擬確認的等待）。
	// false 時回 Reason（paste／capture／enter／dialog／gone…），核心照它退避再試。
	Delivered bool
	Reason    string
	// Recheck：同一個封包 id 再交來（核心重試，ENG-13）時，回 delivered＋sdk.DeliverAlready、不再記一則（模擬「其實已經到了」）；請求帶 Force 就照常再貼。
	Recheck   bool
	Events    []sdk.EngineEvent
	Approvals []sdk.EngineHostRequestApprovalRequest
	Actions   []sdk.EngineHostCheckActionRequest
	Messages  []sdk.ChatMessage
	// Drift 是 DryRun 的 SetPermission 要回報的「產物跟政策不一致」（測試用，給 doctor 的案例）。
	Drift []string
	// Usage 是 Usage 要回的用量（測試用；沒設就回空的）；UsageError 不是空的就回這個錯誤。Preflight 是安裝前檢查要回的項目。
	Usage      *sdk.EngineUsageResponse
	UsageError string
	Preflight  []sdk.PreflightCheck
	// Ask 的行為（P6-5b）：AskReply 不是空的就回它，否則回 "[fake] " + Message；AskDelayMillis 是回覆前的延遲；
	// AskError 不是空的就一律回 UNAVAILABLE；AskFailOn 不是空的時，訊息含這段文字的回 UNAVAILABLE（一則失敗、其他照跑）。
	AskReply       string
	AskDelayMillis int
	AskError       string
	AskFailOn      string
	// AnswerReason：AnswerApproval（逾時後在畫面上代答，ENG-14）要回的原因；空＝答成功（answered=true）。
	AnswerReason string
	// CommandReason：Command（主人下的對話指令，ENG-17）要回的原因；空＝執行成功（done=true）。session 沒開著一律回 gone。
	CommandReason string
	// CommandSessionID：清掉重來（clear）做成時帶回的新對話編號（ENG-17）；空＝不帶。
	CommandSessionID string
	// ResumeLost：接回時接不回原本的對話、開新的（回不同的 id，ENG-12）。
	ResumeLost bool
	// StartError：不是空的時 Start 一律回 CONFLICT 帶這句（模擬平台說「同名的還在跑」，開不起來）。
	StartError string
	// Unlabeled：不給模型的名字（Start、Command、事件都只帶 model 原字），測看板與核心在引擎沒給名字時的樣子（D-46）。
	Unlabeled bool
}
type Results struct {
	Approvals []*sdk.EngineHostRequestApprovalResponse
	Actions   []*sdk.EngineHostCheckActionResponse
}
type Engine struct {
	mu       sync.Mutex
	host     sdk.EngineHostAPI
	behavior Behavior
	calls    []Call
	results  Results
	sessions map[string]bool
	messages map[string][]sdk.ChatMessage
	models   map[string]fakeModel // 每個 session 現在的模型與力度（D-46）：開的時候照設定、指令換了跟著換，事件帶著回報
	changed  chan struct{}
	seen     map[string]bool // 交過的封包 id（Behavior.Recheck 用）
	stuck    bool            // Stick：從現在起每個呼叫都不回
	// 只卡一個 session（其他 session 照常）：stuckOne＝這個 session 的呼叫都不回（StickSession）；
	// slowOne＝這個 session 的呼叫拖這麼久才回失敗（SlowSession）；lateOne＝這個 session 的 Deliver 等到呼叫的人放棄之後才真的送進去（LandLate）。
	stuckOne map[string]bool
	slowOne  map[string]time.Duration
	lateOne  map[string]bool
}

func New(host sdk.EngineHostAPI) *Engine {
	return &Engine{host: host, behavior: Behavior{Delivered: true}, sessions: map[string]bool{}, messages: map[string][]sdk.ChatMessage{}, models: map[string]fakeModel{}, changed: make(chan struct{}), seen: map[string]bool{}, stuckOne: map[string]bool{}, slowOne: map[string]time.Duration{}, lateOne: map[string]bool{}}
}

type fakeModel struct{ model, effort string }

// ModelLabel 是假引擎給模型的名字：「opus」→「Fake Opus」（假引擎自己的命名，只給測試看）；空的回空。
func ModelLabel(model string) string {
	if model == "" {
		return ""
	}
	return "Fake " + strings.ToUpper(model[:1]) + model[1:]
}

// current 回這個 session 現在的模型、名字、力度（不知道就空；Behavior.Unlabeled 時不給名字）。
func (e *Engine) current(session string) (model, label, effort string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.models[session]
	if !e.behavior.Unlabeled {
		label = ModelLabel(m.model)
	}
	return m.model, label, m.effort
}
func copyOf[T any](v T) T {
	data, err := json.Marshal(v)
	if err != nil {
		panic("fake input is not JSON")
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		panic("fake input cannot round trip")
	}
	return out
}
func (e *Engine) Configure(b Behavior) { e.mu.Lock(); defer e.mu.Unlock(); e.behavior = copyOf(b) }
func (e *Engine) Calls() []Call {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Call(nil), e.calls...)
}
func (e *Engine) Results() Results { e.mu.Lock(); defer e.mu.Unlock(); return copyOf(e.results) }

// Stick 讓引擎從現在起「完全不回應」：之後每個呼叫都卡著，等到呼叫的人自己放棄（ctx 結束）才回錯誤。
// 模擬真的引擎整個卡住（行程還在、socket 接得上、就是不回）；測核心與看板不會跟著卡。
func (e *Engine) Stick() { e.mu.Lock(); defer e.mu.Unlock(); e.stuck = true }

// StickSession 只讓一個 session 不回應：她的 Start、Stop、Deliver、Transcript、ListBootPrompts、AnswerApproval、Command
// 都卡到呼叫的人放棄；其他 session 照常。模擬「一位秘書那一側卡住」（真的引擎每個 session 各自一把鎖）。on=false 放開。
func (e *Engine) StickSession(session string, on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if on {
		e.stuckOne[session] = true
	} else {
		delete(e.stuckOne, session)
	}
}

// SlowSession 讓一個 session 的每個呼叫拖 d 才回失敗（UNAVAILABLE）；d<=0 取消。模擬「每次都拖很久才失敗」。
func (e *Engine) SlowSession(session string, d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d > 0 {
		e.slowOne[session] = d
	} else {
		delete(e.slowOne, session)
	}
}

// LandLate 讓一個 session 的 Deliver 等到呼叫的人放棄（ctx 結束）之後才真的把話記進對話、回錯誤：
// 模擬「核心等不到、放手了，引擎那邊後來其實送進去了」。on=false 取消。
func (e *Engine) LandLate(session string, on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if on {
		e.lateOne[session] = true
	} else {
		delete(e.lateOne, session)
	}
}

// hold：引擎卡住時（Stick、StickSession）等到呼叫的人放棄；SlowSession 的拖一段時間再回失敗。session 空＝不屬於哪個 session 的呼叫。
func (e *Engine) hold(ctx context.Context, session string) error {
	e.mu.Lock()
	stuck := e.stuck || (session != "" && e.stuckOne[session])
	slow := e.slowOne[session]
	e.mu.Unlock()
	if stuck {
		<-ctx.Done()
		return ctx.Err()
	}
	if session == "" || slow <= 0 {
		return nil
	}
	t := time.NewTimer(slow)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "假引擎：這位拖了很久才失敗"}
	}
}

// bootSession：啟動提示的編號是「<session>#<種類>」，取前面的 session 名稱。
func bootSession(id string) string {
	name, _, _ := strings.Cut(id, "#")
	return name
}
func record[T any](e *Engine, name string, r T) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, Call{Method: name, Request: copyOf(r)})
}
func (e *Engine) emit(ctx context.Context, event sdk.EngineEvent) error {
	if e.host == nil {
		return nil
	}
	_, err := e.host.ReportEvent(ctx, &sdk.EngineHostReportEventRequest{Event: &event})
	return err
}
func (e *Engine) Capabilities(ctx context.Context, r *sdk.EngineCapabilitiesRequest) (*sdk.EngineCapabilitiesResponse, error) {
	record(e, "Capabilities", r)
	if err := e.hold(ctx, ""); err != nil {
		return nil, err
	}
	return &sdk.EngineCapabilitiesResponse{Engine: "fake", Version: "1", Features: map[string]bool{"ask": true, "resume": true}}, ctx.Err()
}
func (e *Engine) Start(ctx context.Context, r *sdk.EngineStartRequest) (*sdk.EngineStartResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record(e, "Start", r)
	e.mu.Lock()
	startErr := e.behavior.StartError
	e.mu.Unlock()
	if startErr != "" {
		return nil, &sdk.Error{Code: sdk.ErrorCodeConflict, Message: startErr}
	}
	if err := e.hold(ctx, r.Name); err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.sessions[r.Name] = true
	e.models[r.Name] = fakeModel{model: r.Model, effort: r.Effort} // 照設定開；沒設定就是不知道
	e.mu.Unlock()
	if err := e.emit(ctx, sdk.EngineEvent{Session: r.Name, Type: sdk.EngineEventTypeSessionStarted}); err != nil {
		return nil, err
	}
	id := "fake-" + r.Name
	e.mu.Lock()
	lost := e.behavior.ResumeLost
	e.mu.Unlock()
	if r.ResumeSessionID != "" {
		id = r.ResumeSessionID // 接回：引擎回同一個 session id（P7-5，Capabilities.Features["resume"]）
		if lost {
			id = "fake-new-" + r.Name
		}
	}
	model, label, effort := e.current(r.Name)
	return &sdk.EngineStartResponse{EngineSessionID: id, Attach: "fake-attach " + r.Name, Model: model, ModelLabel: label, Effort: effort}, nil
}
func (e *Engine) Stop(ctx context.Context, r *sdk.EngineStopRequest) (*sdk.EngineStopResponse, error) {
	record(e, "Stop", r)
	if err := e.hold(ctx, r.Name); err != nil {
		return nil, err
	}
	e.mu.Lock()
	delete(e.sessions, r.Name)
	close(e.changed)
	e.changed = make(chan struct{})
	e.mu.Unlock()
	err := e.emit(ctx, sdk.EngineEvent{Session: r.Name, Type: sdk.EngineEventTypeSessionEnded})
	return &sdk.EngineStopResponse{}, err
}
func (e *Engine) Deliver(ctx context.Context, r *sdk.EngineDeliverRequest) (*sdk.EngineDeliverResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record(e, "Deliver", r)
	if err := e.hold(ctx, r.Session); err != nil {
		return nil, err
	}
	e.mu.Lock()
	b := copyOf(e.behavior)
	active := e.sessions[r.Session]
	late := e.lateOne[r.Session]
	e.mu.Unlock()
	if !active {
		return &sdk.EngineDeliverResponse{Reason: "gone"}, nil // 跟真的引擎一樣（session 不在）
	}
	if late {
		// 呼叫的人放棄之後才送進去（LandLate）：記進對話、記下這個封包編號（之後重查回 already），照樣回錯誤。
		<-ctx.Done()
		e.mu.Lock()
		if r.EnvelopeID != "" {
			e.seen[r.EnvelopeID] = true
		}
		n := len(e.messages[r.Session])
		e.messages[r.Session] = append(e.messages[r.Session], sdk.ChatMessage{Uuid: fmt.Sprintf("%s-%d", r.Session, n+1), Kind: sdk.ChatKindUser, Text: r.Text})
		e.mu.Unlock()
		return nil, ctx.Err()
	}
	if !b.Delivered {
		return &sdk.EngineDeliverResponse{Reason: b.Reason}, nil
	}
	e.mu.Lock()
	if b.Recheck && !r.Force && r.EnvelopeID != "" && e.seen[r.EnvelopeID] {
		e.mu.Unlock()
		return &sdk.EngineDeliverResponse{Delivered: true, Reason: sdk.DeliverAlready}, nil
	}
	if r.EnvelopeID != "" {
		e.seen[r.EnvelopeID] = true
	}
	n := len(e.messages[r.Session])
	e.messages[r.Session] = append(e.messages[r.Session], sdk.ChatMessage{Uuid: fmt.Sprintf("%s-%d", r.Session, n+1), Kind: sdk.ChatKindUser, Text: r.Text})
	for _, msg := range b.Messages {
		if msg.Uuid == "" {
			msg.Uuid = fmt.Sprintf("%s-%d", r.Session, len(e.messages[r.Session])+1)
		}
		e.messages[r.Session] = append(e.messages[r.Session], msg)
	}
	close(e.changed)
	e.changed = make(chan struct{})
	e.mu.Unlock()
	// Publish the scripted turn/action prefix before asking the host about it.
	split := 0
	for i, ev := range b.Events {
		if ev.Type == sdk.EngineEventTypeTurnStarted && split == 0 {
			split = i + 1
		}
		if ev.Type == sdk.EngineEventTypeActivityStarted {
			split = i + 1
			break
		}
	}
	for _, ev := range b.Events[:split] {
		if ev.Session == "" {
			ev.Session = r.Session
		}
		if err := e.emit(ctx, ev); err != nil {
			return nil, err
		}
	}
	for _, a := range b.Actions {
		if e.host == nil {
			return nil, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "測試引擎沒有核心連線"}
		}
		if a.Session == "" {
			a.Session = r.Session
		}
		out, err := e.host.CheckAction(ctx, &a)
		if err != nil {
			return nil, err
		}
		e.mu.Lock()
		e.results.Actions = append(e.results.Actions, copyOf(out))
		e.mu.Unlock()
	}
	for _, a := range b.Approvals {
		if e.host == nil {
			return nil, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "測試引擎沒有核心連線"}
		}
		if a.Session == "" {
			a.Session = r.Session
		}
		out, err := e.host.RequestApproval(ctx, &a)
		if err != nil {
			return nil, err
		}
		e.mu.Lock()
		e.results.Approvals = append(e.results.Approvals, copyOf(out))
		e.mu.Unlock()
	}
	for _, ev := range b.Events[split:] {
		if ev.Session == "" {
			ev.Session = r.Session
		}
		if err := e.emit(ctx, ev); err != nil {
			return nil, err
		}
	}
	return &sdk.EngineDeliverResponse{Delivered: true}, nil
}
func (e *Engine) Transcript(ctx context.Context, r *sdk.EngineTranscriptRequest, send func(*sdk.EngineTranscriptResponse) error) error {
	record(e, "Transcript", r)
	if err := e.hold(ctx, r.Session); err != nil {
		return err
	}
	cursor := r.SinceUuid
	for {
		e.mu.Lock()
		rows := copyOf(e.messages[r.Session])
		changed := e.changed
		active := e.sessions[r.Session]
		e.mu.Unlock()
		start := 0
		if cursor != "" {
			start = len(rows)
			for i, msg := range rows {
				if msg.Uuid == cursor {
					start = i + 1
					break
				}
			}
		}
		for _, msg := range rows[start:] {
			m := msg
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := send(&sdk.EngineTranscriptResponse{Message: &m}); err != nil {
				return err
			}
			cursor = m.Uuid
		}
		if !r.Follow || !active {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
func (e *Engine) SetPermission(ctx context.Context, r *sdk.EngineSetPermissionRequest) (*sdk.EngineSetPermissionResponse, error) {
	record(e, "SetPermission", r)
	if err := e.hold(ctx, ""); err != nil {
		return nil, err
	}
	if r.DryRun {
		e.mu.Lock()
		drift := append([]string(nil), e.behavior.Drift...)
		e.mu.Unlock()
		return &sdk.EngineSetPermissionResponse{Drift: drift}, ctx.Err()
	}
	return &sdk.EngineSetPermissionResponse{Artifacts: []string{}}, ctx.Err()
}
func (e *Engine) ListBootPrompts(ctx context.Context, r *sdk.EngineListBootPromptsRequest) (*sdk.EngineListBootPromptsResponse, error) {
	record(e, "ListBootPrompts", r)
	if err := e.hold(ctx, r.Session); err != nil {
		return nil, err
	}
	return &sdk.EngineListBootPromptsResponse{}, ctx.Err()
}
func (e *Engine) AnswerApproval(ctx context.Context, r *sdk.EngineAnswerApprovalRequest) (*sdk.EngineAnswerApprovalResponse, error) {
	record(e, "AnswerApproval", r)
	if err := e.hold(ctx, r.Session); err != nil {
		return nil, err
	}
	e.mu.Lock()
	reason := e.behavior.AnswerReason
	e.mu.Unlock()
	return &sdk.EngineAnswerApprovalResponse{Answered: reason == "", Reason: reason}, ctx.Err()
}

// Command 記錄呼叫；session 沒開著回 gone（跟真的引擎一樣），否則照 Behavior.CommandReason（空＝done）。
func (e *Engine) Command(ctx context.Context, r *sdk.EngineCommandRequest) (*sdk.EngineCommandResponse, error) {
	record(e, "Command", r)
	if err := e.hold(ctx, r.Session); err != nil {
		return nil, err
	}
	e.mu.Lock()
	reason, alive, id := e.behavior.CommandReason, e.sessions[r.Session], e.behavior.CommandSessionID
	e.mu.Unlock()
	if !alive {
		reason = "gone"
	}
	res := &sdk.EngineCommandResponse{Done: reason == "", Reason: reason}
	if res.Done && r.Command == sdk.SessionCommandClear {
		res.EngineSessionID = id
	}
	if res.Done && (r.Command == sdk.SessionCommandModel || r.Command == sdk.SessionCommandEffort) {
		e.mu.Lock()
		m := e.models[r.Session]
		if r.Command == sdk.SessionCommandModel {
			m.model = r.Arg
		} else {
			m.effort = r.Arg
		}
		e.models[r.Session] = m
		e.mu.Unlock()
		res.Model, res.ModelLabel, res.Effort = e.current(r.Session)
	}
	return res, ctx.Err()
}
func (e *Engine) AnswerBootPrompt(ctx context.Context, r *sdk.EngineAnswerBootPromptRequest) (*sdk.EngineAnswerBootPromptResponse, error) {
	record(e, "AnswerBootPrompt", r)
	if err := e.hold(ctx, bootSession(r.ID)); err != nil {
		return nil, err
	}
	return &sdk.EngineAnswerBootPromptResponse{}, ctx.Err()
}
func (e *Engine) Usage(ctx context.Context, r *sdk.EngineUsageRequest) (*sdk.EngineUsageResponse, error) {
	record(e, "Usage", r)
	if err := e.hold(ctx, ""); err != nil {
		return nil, err
	}
	e.mu.Lock()
	b := copyOf(e.behavior)
	e.mu.Unlock()
	if b.UsageError != "" {
		return nil, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: b.UsageError}
	}
	out := &sdk.EngineUsageResponse{Account: r.Account}
	if b.Usage != nil {
		out = b.Usage
		out.Account = r.Account
	}
	out.Warmed = r.Warmup
	return out, ctx.Err()
}

// Preflight 回 Behavior.Preflight（測試用）；沒設就回空的。
func (e *Engine) Preflight(ctx context.Context, r *sdk.EnginePreflightRequest) (*sdk.EnginePreflightResponse, error) {
	record(e, "Preflight", r)
	if err := e.hold(ctx, ""); err != nil {
		return nil, err
	}
	e.mu.Lock()
	b := copyOf(e.behavior)
	e.mu.Unlock()
	return &sdk.EnginePreflightResponse{Checks: b.Preflight}, ctx.Err()
}

// Ask 回 Behavior 設定的文字（見 Behavior.AskReply）；不開 session、不回報事件。
func (e *Engine) Ask(ctx context.Context, r *sdk.EngineAskRequest) (*sdk.EngineAskResponse, error) {
	record(e, "Ask", r)
	if err := e.hold(ctx, ""); err != nil {
		return nil, err
	}
	e.mu.Lock()
	b := copyOf(e.behavior)
	e.mu.Unlock()
	if b.AskDelayMillis > 0 {
		t := time.NewTimer(time.Duration(b.AskDelayMillis) * time.Millisecond)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if b.AskError != "" {
		return nil, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: b.AskError}
	}
	if b.AskFailOn != "" && strings.Contains(r.Message, b.AskFailOn) {
		return nil, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "假引擎指定這則失敗"}
	}
	reply := b.AskReply
	if reply == "" {
		reply = "[fake] " + r.Message
	}
	return &sdk.EngineAskResponse{Reply: reply, Model: r.Model}, ctx.Err()
}

// WriteAgentCards 只記錄呼叫：每張卡回一個假的路徑（DryRun 是 create，否則 same），不寫任何檔。
func (e *Engine) WriteAgentCards(ctx context.Context, r *sdk.EngineWriteAgentCardsRequest) (*sdk.EngineWriteAgentCardsResponse, error) {
	record(e, "WriteAgentCards", r)
	if err := e.hold(ctx, ""); err != nil {
		return nil, err
	}
	out := &sdk.EngineWriteAgentCardsResponse{Files: []sdk.AgentCardFile{}}
	for _, c := range r.Cards {
		action := "same"
		if r.DryRun {
			action = "create"
		}
		out.Files = append(out.Files, sdk.AgentCardFile{Agent: c.ID, Path: "fake/" + r.Account + "/" + c.ID, Action: action, Summary: c.Duty})
	}
	return out, ctx.Err()
}

var _ sdk.EngineAPI = (*Engine)(nil)
