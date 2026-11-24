package session

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func (m *Module) ReportEvent(ctx context.Context, ev EngineEvent) (err error) {
	defer func() {
		if recover() != nil {
			err = platform.Internal("對話事件處理失敗")
			m.warn("對話事件處理失敗", err)
		}
	}()
	if ev == nil {
		return platform.Invalid("event", "請提供對話事件")
	}
	event := *ev
	if ev.DirectMessage != nil {
		copy := *ev.DirectMessage
		event.DirectMessage = &copy
	}
	// 引擎回報的事實事件（platform/session_events.go）在放開 m.mu 之後才發（defer 先進後出）。
	var fact string
	var factPayload map[string]any
	defer func() {
		if fact == "" {
			return
		}
		// 出錯與結束的事實帶上這位秘書現在的角色名（通知要寫出是誰，法律第 5 條）；放開 m.mu 之後才問名冊。讀不到就不帶。
		if agent, _ := factPayload["agent"].(string); agent != "" && fact != platform.TopicSessionRecovered {
			if name, err := m.d.Agents.PersonaName(ctx, agent); err == nil && name != "" {
				factPayload["agent_name"] = name
			}
		}
		m.publish(ctx, fact, factPayload)
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err = m.ready(ctx); err != nil {
		return err
	}
	e := m.entries[event.Session]
	if e == nil {
		return platform.NotFound("找不到這個對話")
	}
	at := event.At
	if at.IsZero() {
		at = m.d.Clock.Now()
	}
	event.At = at
	if event.Type == sdk.EngineEventTypeTurnStarted {
		copy := event
		e.latestInput = &copy
	}
	e.version++
	applyModel(&e.session, event.Model, event.ModelLabel, event.Effort) // 引擎看到的模型與力度（SES-60）；空的不改
	switch event.Type {
	case sdk.EngineEventTypeSessionStarted:
		if id := event.EngineSessionID; id != "" && id != e.session.EngineSessionID {
			// 在她畫面上清掉重來（不是從看板下的 /clear）：之後接回用新的編號，已讀位置與未讀從頭算（SES-61）。
			m.newConversation(e, id)
		}
		m.setState(e, sdk.SessionStateIdle, at)
	case sdk.EngineEventTypeTurnStarted:
		m.finishApproval(event.Session, nil, false)
		e.session.Pending = nil
		e.ended = false
		m.setState(e, sdk.SessionStateBusy, at)
		e.activity = ""
		e.activitySince = at
		e.turnStartedAt = at
		if event.Source == sdk.InputSourceTyped {
			e.session.LastUserAt = at
		}
	case sdk.EngineEventTypeActivityStarted:
		m.dropNative(e) // 在畫面上等的確認有人答了（SES-48）
		m.setState(e, sdk.SessionStateBusy, at)
		e.activity = event.Activity
		e.activitySince = at
	case sdk.EngineEventTypeActivityEnded:
		m.finishApproval(event.Session, nil, false)
		e.session.Pending = nil
		m.setState(e, sdk.SessionStateBusy, at)
		e.activity = ""
		e.activitySince = at
	case sdk.EngineEventTypeTurnEnded:
		m.finishApproval(event.Session, nil, false)
		e.session.Pending = nil
		m.setState(e, sdk.SessionStateIdle, at)
		e.session.LastTurnEndAt = at
		e.background = event.BackgroundTasks
		e.ended = true
		m.transcriptDirty[event.Session] = true
		if e.failedKind != "" {
			fact, factPayload = platform.TopicSessionRecovered, map[string]any{"name": e.session.Name, "agent": e.session.Agent, "account": e.session.Account, "after": string(e.failedKind)}
			e.failedKind = ""
		}
	case sdk.EngineEventTypeTurnFailed:
		// 這一輪出錯了（S5-0、S5-2）：引擎說她停在輸入框，記回待命；這一輪的話由派送器記中斷、附原因（origin.go）。
		// 不是一輪正常結束：不記 last_turn_end_at、不算「做完了」。
		m.finishApproval(event.Session, nil, false)
		e.session.Pending = nil
		m.setState(e, sdk.SessionStateIdle, at)
		e.ended = false
		m.transcriptDirty[event.Session] = true
		kind := event.Error.Normalize()
		// 記著要主人出手的那種（auth、billing）：之後又碰到平台忙這類，恢復時 after 仍是 auth／billing，notify 才收得到那則提醒（S5-5）。
		if !ownerFixableKind(e.failedKind) || ownerFixableKind(kind) {
			e.failedKind = kind
		}
		fact, factPayload = platform.TopicSessionTurnFailed, map[string]any{"name": e.session.Name, "agent": e.session.Agent, "account": e.session.Account, "error": string(kind), "last_message": event.LastMessage}
	case sdk.EngineEventTypeIdleHint:
		// 額度用完時平台照樣會提示「閒著」：不能把 limited 蓋成待命，派送器才不會把排著的話白送進去（REQ-378）。
		// 在畫面上等的確認被中斷了：收掉，回到閒著（SES-48）。
		// 叫她停下之後的閒著（SES-62，At＝按停止的時間）：停下之後她又開始了下一輪就不算，不然會把在忙的她記成待命。
		if event.Interrupted && staleInterrupt(e.turnStartedAt, at) {
			break
		}
		if (m.dropNative(e) || e.session.Pending == nil) && e.session.State != sdk.SessionStateLimited {
			m.setState(e, sdk.SessionStateIdle, at)
		}
	case sdk.EngineEventTypeRateLimited:
		// 額度用完（REQ-378、SES-49）：不是卡住、不是一輪結束；這一輪拿到的話由派送器退回排隊（origin.go），恢復後補送。
		m.finishApproval(event.Session, nil, false)
		e.session.Pending = nil
		m.setState(e, sdk.SessionStateLimited, at)
		// 已經是 limited 又來一次（恢復後補送的那則又碰到額度）：從這一次重新算，說不出恢復時間的才不會每秒補送。
		e.session.StateSince = at
		e.session.LimitedUntil = event.Until
		e.activity = ""
	case sdk.EngineEventTypeSessionEnded:
		m.finishApproval(event.Session, nil, false)
		e.session.Pending = nil
		m.setState(e, sdk.SessionStateStopped, at)
		if event.Reason == sdk.SessionEndReasonLoggedOut {
			// 帳號登出跟登入失效是同一件事：重新登入後第一輪正常結束發 session.recovered（after＝auth），notify 收掉「要重新登入」（S5-5）。
			e.failedKind = sdk.EngineErrorKindAuth
		}
		fact, factPayload = platform.TopicSessionEnded, map[string]any{"name": e.session.Name, "agent": e.session.Agent, "account": e.session.Account, "reason": event.Reason}
	default:
		return platform.Invalid("type", "無法辨識對話事件")
	}
	e.session.LastEvent = string(event.Type)
	e.session.LastEventAt = at
	m.dirty[event.Session] = true
	select {
	case m.persist <- struct{}{}:
	default:
	}
	m.announce(ctx, e)
	// 閒著的提示只有真的讓她（還是）待命時才交給派送器收掉這一輪的話；在等確認、額度用完時不收（S5-2）。
	if event.Type != sdk.EngineEventTypeIdleHint || e.session.State == sdk.SessionStateIdle {
		m.eventQueue = append(m.eventQueue, &event)
		select {
		case m.dispatcher <- &event:
		default: // The queue retains every event; the channel only wakes the worker.
		}
	}
	if event.Type == sdk.EngineEventTypeTurnEnded || event.Type == sdk.EngineEventTypeTurnFailed {
		select {
		case m.refresh <- struct{}{}:
		default:
		}
	}
	return nil
}
func (m *Module) finishApproval(name string, d Decision, decided bool) {
	if a := m.approvals[name]; a != nil {
		select {
		case a.result <- approvalResult{d, decided}:
		default: // 已經交回畫面（native）的那一次早就拿到 decided=false 了
		}
		delete(m.approvals, name)
	}
}
func (m *Module) RequestApproval(ctx context.Context, req ApprovalRequest) (Decision, bool, error) {
	if req == nil {
		return nil, false, platform.Invalid("approval", "請提供確認內容")
	}
	if req.Kind != sdk.ApprovalKindPermission && req.Kind != sdk.ApprovalKindQuestion {
		return nil, false, platform.Invalid("kind", "確認種類不正確")
	}
	if req.Kind == sdk.ApprovalKindQuestion && len(req.Questions) == 0 {
		return nil, false, platform.Invalid("questions", "請提供問題")
	}
	if req.Kind == sdk.ApprovalKindPermission && req.Summary == "" {
		return nil, false, platform.Invalid("summary", "請提供確認摘要")
	}
	seenQuestions := map[string]bool{}
	for _, q := range req.Questions {
		if q.Text == "" || seenQuestions[q.Text] {
			return nil, false, platform.Invalid("questions", "題目不可空白或重複")
		}
		seenQuestions[q.Text] = true
	}
	m.mu.Lock()
	if err := m.ready(ctx); err != nil {
		m.mu.Unlock()
		return nil, false, err
	}
	e := m.entries[req.Session]
	if e == nil {
		m.mu.Unlock()
		return nil, false, platform.NotFound("找不到這個對話")
	}
	if a := m.approvals[req.Session]; a != nil && a.native && a.answering {
		// 看板正在請引擎代按畫面上那個（重開後接回的權限確認）：不取代，免得代按完又多一張按過的卡（SES-66）。
		m.mu.Unlock()
		return nil, false, platform.Conflict("這個對話的確認正在代答")
	}
	if e.session.Pending != nil && !m.dropNative(e) { // 畫面上等的那個已經過去了，才會有新的確認（SES-48）
		m.mu.Unlock()
		return nil, false, platform.Conflict("這個對話已在等待確認")
	}
	now := m.d.Clock.Now()
	wait := ApprovalTimeout
	if req.Kind == sdk.ApprovalKindQuestion {
		wait = QuestionTimeout
	}
	if req.MaxWaitSeconds > 0 && time.Duration(req.MaxWaitSeconds)*time.Second < wait {
		wait = time.Duration(req.MaxWaitSeconds) * time.Second
	}
	a := &approval{id: platform.NewID(), deadline: now.Add(wait), result: make(chan approvalResult, 1), questions: append([]sdk.Question(nil), req.Questions...), notices: map[string]string{}, answers: map[string]string{}}
	e.session.Pending = &sdk.Pending{ID: a.id, Kind: sdk.PendingKind(req.Kind), Tool: req.Tool, Summary: req.Summary, Questions: a.questions, Since: now}
	m.approvals[req.Session] = a
	m.enqueueApproval(req.Session, "approval_blocked")
	m.setState(e, sdk.SessionStateWaiting, now)
	if err := m.changed(ctx, e); err != nil {
		delete(m.approvals, req.Session)
		e.session.Pending = nil
		m.mu.Unlock()
		return nil, false, err
	}
	m.publish(ctx, "session.pending", map[string]any{"name": req.Session, "kind": string(req.Kind), "id": a.id})
	run := m.ctx
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.approvals[req.Session] == a && !a.native {
			delete(m.approvals, req.Session)
			e.session.Pending = nil
			if err := m.changed(context.Background(), e); err != nil {
				m.warn("清除確認狀態失敗", err)
			}
		}
	}()
	// 綁在對話上的等待只推播、不建訊息頁的通知（DEV-49 S2、SES-47）：Pending 已經記在 session 上，
	// syncWaits（announce 叫醒）把它推出去，那件事不在了就收回；看板在對話裡回答（session.resolve）。
	select {
	case result := <-a.result:
		return result.decision, result.decided, nil
	case <-ctx.Done():
		// 引擎那邊不等了（或核心要停、請求的 ctx 跟著取消）：確認留在畫面上（SES-48）。
		// 一律回 unavailable：引擎自己不等了就讀不到回覆；停機時兩個 ctx 取消的先後不固定，不能靠 run.Err() 分辨（SES-66）。
		m.handOffLocked(a, req.Session)
		return nil, false, errCoreStopping
	case <-run.Done():
		// 核心要停（部署、重開）：確認先留在畫面上（重開後 restoreNative 當 native 接回），回 unavailable，
		// 引擎等核心回來再問一次，新的請求取代那個 native（SES-66）；不回「沒決定」，免得引擎直接交回她的畫面。
		m.handOffLocked(a, req.Session)
		return nil, false, errCoreStopping
	}
}

// handOffLocked：RequestApproval 不等了（引擎斷線、核心要停），確認還是留在畫面上等（SES-48）。
func (m *Module) handOffLocked(a *approval, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[name]; e != nil && m.approvals[name] == a {
		m.handOff(context.Background(), e, a)
	}
}
func (m *Module) Resolve(ctx context.Context, name, id string, d Decision, by string) (bool, error) {
	if handled, applied, err := m.resolveNative(ctx, name, id, d, by); handled {
		return applied, err // 在畫面上等的確認：引擎代答（SES-48）
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.resolve(ctx, name, id, d)
}
func (m *Module) resolve(ctx context.Context, name, id string, d Decision) (bool, error) {
	if err := m.ready(ctx); err != nil {
		return false, err
	}
	e := m.entries[name]
	if e == nil {
		return false, platform.NotFound("找不到這個對話")
	}
	a := m.approvals[name]
	if e.session.Pending == nil || e.session.Pending.ID != id || a == nil || a.id != id || a.native {
		return false, nil // 在畫面上等的（native）只經 resolveNative 由引擎代答
	}
	if !m.d.Clock.Now().Before(a.deadline) {
		m.handOff(ctx, e, a) // 逾時：留在畫面上等（SES-48）
		return false, nil
	}
	if d == nil {
		return false, platform.Invalid("decision", "請提供決定")
	}
	for _, q := range a.questions {
		if _, ok := d.Answers[q.Text]; !ok && (d.Allow || len(d.Answers) > 0) {
			return false, platform.Invalid("answers", "請回答全部問題") // 拒絕（不帶答案）＝這題不答了，例如主人打字蓋過（SES-69）
		}
	}
	e.session.Pending = nil
	m.setState(e, sdk.SessionStateBusy, m.d.Clock.Now())
	if err := m.changed(ctx, e); err != nil {
		return false, err
	}
	for notice := range a.notices {
		delete(m.notices, notice)
	}
	copy := *d
	copy.Answers = map[string]string{}
	for k, v := range d.Answers {
		copy.Answers[k] = v
	}
	m.finishApproval(name, &copy, true)
	m.enqueueApproval(name, "approval_resolved")
	notices := make([]string, 0, len(a.notices))
	for id := range a.notices {
		notices = append(notices, id)
	}
	sort.Strings(notices)
	m.publish(ctx, "session.approval.resolved", map[string]any{"name": name, "pending_id": id, "notices": notices})
	return true, nil
}
func (m *Module) GetContext(ctx context.Context, name, input string) (string, error) {
	if _, err := m.Get(ctx, name); err != nil {
		return "", err
	}
	m.drainEvents(ctx)
	content, changed, err := m.d.Agents.Context(ctx, name)
	if err != nil {
		return "", err
	}
	if !changed {
		content = ""
	} else {
		m.mu.Lock()
		e := m.entries[name]
		e.nowSeen = m.d.Clock.Now().In(m.config.Location())
		err = m.save(e)
		m.mu.Unlock()
		if err != nil {
			return "", err
		}
	}
	return content, nil
}
func (m *Module) CheckAction(ctx context.Context, name string, class sdk.ActionClass, tool, summary string) (sdk.ActionVerdict, string, error) {
	return m.checkAction(ctx, name, class, tool)
}

type host struct{ m *Module }

func (m *Module) EngineHost() sdk.EngineHostAPI { return host{m} }
func (h host) ReportEvent(ctx context.Context, r *sdk.EngineHostReportEventRequest) (*sdk.EngineHostReportEventResponse, error) {
	if r == nil {
		return nil, hostError(platform.Invalid("event", "請提供事件"))
	}
	if err := h.m.ReportEvent(ctx, r.Event); err != nil {
		return nil, hostError(err)
	}
	return &sdk.EngineHostReportEventResponse{}, nil
}
func (h host) RequestApproval(ctx context.Context, r *sdk.EngineHostRequestApprovalRequest) (*sdk.EngineHostRequestApprovalResponse, error) {
	d, decided, err := h.m.RequestApproval(ctx, r)
	if err != nil {
		return nil, hostError(err)
	}
	return &sdk.EngineHostRequestApprovalResponse{Decision: d, Decided: decided}, nil
}
func (h host) GetContext(ctx context.Context, r *sdk.EngineHostGetContextRequest) (*sdk.EngineHostGetContextResponse, error) {
	if r == nil {
		return nil, hostError(platform.Invalid("session", "請提供對話"))
	}
	text, err := h.m.GetContext(ctx, r.Session, r.Input)
	if err != nil {
		return nil, hostError(err)
	}
	return &sdk.EngineHostGetContextResponse{Text: text}, nil
}
func (h host) CheckAction(ctx context.Context, r *sdk.EngineHostCheckActionRequest) (*sdk.EngineHostCheckActionResponse, error) {
	if r == nil {
		return nil, hostError(platform.Invalid("action", "請提供動作"))
	}
	v, reason, err := h.m.CheckAction(ctx, r.Session, r.Class, r.Tool, r.Summary)
	if err != nil {
		return nil, hostError(err)
	}
	return &sdk.EngineHostCheckActionResponse{Verdict: v, Reason: reason}, nil
}

// errCoreStopping：核心正在停（部署、重開）時還在等的確認回這個，引擎看到 unavailable 會等核心回來再問（SES-66）。
var errCoreStopping = platform.Unavailable("核心正在重開，等一下再問")

func hostError(err error) error {
	var e *platform.Err
	if errors.As(err, &e) {
		return &sdk.Error{Code: e.Code, Field: e.Field, Message: e.Message, Details: e.Details}
	}
	return &sdk.Error{Code: sdk.ErrorCodeInternal, Message: "對話服務無法完成請求"}
}

// enqueueApproval is called under mu; the event worker owns bus transitions.
func (m *Module) enqueueApproval(name, kind string) {
	ev := &sdk.EngineEvent{Session: name, Type: sdk.EngineEventType(kind), At: m.d.Clock.Now()}
	m.eventQueue = append(m.eventQueue, ev)
	select {
	case m.dispatcher <- ev:
	default:
	}
}

// ownerFixableKind：只有主人親手能解的錯誤種類（登入失效、帳單）；session.recovered 的 after 優先記這種。
func ownerFixableKind(k sdk.EngineErrorKind) bool {
	return k == sdk.EngineErrorKindAuth || k == sdk.EngineErrorKindBilling
}

// staleInterrupt：叫她停下（At＝按停止的時間）之後她又開始了一輪（SES-62）：這個閒著的提示不算數。
func staleInterrupt(turnStarted, stoppedAt time.Time) bool {
	return !stoppedAt.IsZero() && turnStarted.After(stoppedAt)
}
