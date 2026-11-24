package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

func validName(name string) bool {
	ok, _ := regexp.MatchString(`^[a-z0-9]+(?:-[a-z0-9]+)*$`, name)
	return ok
}
func text(fields map[string]any, key string) string { s, _ := fields[key].(string); return s }
func instant(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		at, _ := time.Parse(time.RFC3339Nano, t)
		return at
	}
	return time.Time{}
}
func copySession(s sdk.Session) *sdk.Session {
	data, _ := json.Marshal(s)
	var out sdk.Session
	_ = json.Unmarshal(data, &out)
	return &out
}
func (m *Module) restore() error {
	dir := platform.RuntimeDir(m.home, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return platform.Unavailable("無法建立對話狀態目錄")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return err
	}
	m.entries = map[string]*entry{}
	m.workAsks = map[string]string{}
	m.turns = map[string]*dispatchTurn{}
	m.pendingPeers = map[*dispatchTurn]string{}
	m.inFlight = map[string]bool{}
	m.rerouting = map[string]bool{}
	m.mainReopen = map[string]time.Time{}
	m.stuckEpisodes = map[string]*stuckEpisode{}
	m.eventQueue = nil
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return platform.Unavailable("無法讀取對話狀態")
		}
		r, order, err := vault.Decode(data)
		if err != nil {
			return err
		}
		name := text(r.Fields, "name")
		if !validName(name) || filepath.Base(path) != name+".md" {
			return platform.Invalid("name", "對話名稱與檔案不符")
		}
		data, err = json.Marshal(r.Fields)
		if err != nil {
			return platform.Invalid("session", "對話狀態格式不正確")
		}
		var s sdk.Session
		if err = json.Unmarshal(data, &s); err != nil {
			return platform.Invalid("session", "對話狀態格式不正確")
		}
		if s.State == "stuck" {
			s.State = sdk.SessionStateIdle // 舊版猜的「卡住」（S5-2 起不再有）：當成待命，等引擎的下一個回報
		}
		e := &entry{session: s, record: r, order: order, title: text(r.Fields, "title"), seen: text(r.Fields, "seen_uuid"), nowSeen: instant(r.Fields["now_seen_at"]), seenAt: instant(r.Fields["seen_at"]), idleClose: 30 * time.Minute, ended: !s.LastTurnEndAt.IsZero() && (s.LastEvent == string(sdk.EngineEventTypeTurnEnded) || (s.State == sdk.SessionStateIdle && !s.StateSince.After(s.LastTurnEndAt)))}
		if raw, ok := r.Fields["turn"]; ok {
			data, err := json.Marshal(raw)
			if err != nil {
				return err
			}
			if err = json.Unmarshal(data, &e.turn); err != nil {
				return err
			}
		}
		e.lastOwner = instant(r.Fields["last_owner_at"])
		e.ownerTalk = instant(r.Fields["owner_talk_at"])
		e.workAsk, e.nudgedFor, e.taskStatus = text(r.Fields, "work_ask"), text(r.Fields, "nudged_for"), text(r.Fields, "task_status")
		e.suspended, _ = r.Fields["suspended"].(bool)
		if e.workAsk != "" {
			m.workAsks[e.workAsk] = name
		}
		if e.title == "" {
			e.title = name
		}
		if duration := text(r.Fields, "idle_close_after"); duration != "" {
			e.idleClose, err = time.ParseDuration(duration)
			if err != nil || e.idleClose <= 0 {
				return platform.Invalid("idle_close_after", "閒置時間必須大於零")
			}
		}
		if activity, ok := r.Fields["activity"].(map[string]any); ok {
			e.activity = text(activity, "tool")
			e.activitySince = instant(activity["since"])
		}
		if n, ok := r.Fields["background_tasks"].(int); ok {
			e.background = int32(n)
		}
		m.entries[name] = e
		m.restoreNative(e) // 重開前在等的確認還在畫面上（SES-48）
		if e.turn.Origin != "" {
			m.turns[name] = &dispatchTurn{turn: e.turn, source: sdk.InputSourceTyped}
		}
		m.transcriptDirty[name] = true
	}
	return nil
}
func (m *Module) encodeState(e *entry) ([]byte, error) {
	if e.record.Fields == nil {
		e.record.Fields = map[string]any{}
	}
	data, err := json.Marshal(e.session)
	if err != nil {
		return nil, platform.Internal("無法編碼對話狀態")
	}
	var fields map[string]any
	if err = json.Unmarshal(data, &fields); err != nil {
		return nil, platform.Internal("無法編碼對話狀態")
	}
	for _, key := range []string{"pending", "remote_access", "engine_session_id", "attach", "cwd", "queued_at", "task", "last_command", "model", "model_label", "effort"} {
		delete(e.record.Fields, key)
	}
	for k, v := range fields {
		e.record.Fields[k] = v
	}
	f := e.record.Fields
	data, err = json.Marshal(e.turn)
	if err != nil {
		return nil, err
	}
	var turnFields map[string]any
	if err = json.Unmarshal(data, &turnFields); err != nil {
		return nil, err
	}
	f["turn"] = turnFields
	if !e.lastOwner.IsZero() {
		f["last_owner_at"] = e.lastOwner
	}
	if !e.ownerTalk.IsZero() {
		f["owner_talk_at"] = e.ownerTalk
	}
	for key, v := range map[string]string{"work_ask": e.workAsk, "nudged_for": e.nudgedFor, "task_status": e.taskStatus} {
		if v != "" {
			f[key] = v
		} else {
			delete(f, key)
		}
	}
	if e.suspended {
		f["suspended"] = true
	} else {
		delete(f, "suspended")
	}
	f["schema"] = 1
	f["updated_at"] = m.d.Clock.Now().In(m.config.Location())
	f["title"] = e.title
	f["seen_uuid"] = e.seen
	f["background_tasks"] = e.background
	f["idle_close_after"] = e.idleClose.String()
	if !e.nowSeen.IsZero() {
		f["now_seen_at"] = e.nowSeen
	}
	if !e.seenAt.IsZero() {
		f["seen_at"] = e.seenAt
	}
	if e.session.State == sdk.SessionStateBusy {
		f["activity"] = map[string]any{"tool": e.activity, "since": e.activitySince}
	} else {
		delete(f, "activity")
	}
	return vault.Encode(e.record, e.order, vault.Spec{}), nil
}

// save is called with m.mu held. The writer releases writeMu before reacquiring mu.
func (m *Module) save(e *entry) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	data, err := m.encodeState(e)
	if err != nil {
		return err
	}
	if err = m.writeState(platform.RuntimeDir(m.home, "sessions", e.session.Name+".md"), data); err != nil {
		return platform.Unavailable("無法儲存對話狀態，請檢查磁碟")
	}
	delete(m.dirty, e.session.Name)
	return nil
}

func (m *Module) setState(e *entry, state State, at time.Time) {
	if state != sdk.SessionStateLimited {
		e.session.LimitedUntil = time.Time{} // 額度恢復時間只在 limited 時有意義（REQ-378）
	}
	if e.session.State != state {
		e.session.State = state
		e.session.StateSince = at
	}
	if state != sdk.SessionStateBusy {
		e.activity = ""
		e.activitySince = time.Time{}
	}
}
func (m *Module) changed(ctx context.Context, e *entry) error {
	if err := m.save(e); err != nil {
		return err
	}
	m.announce(ctx, e)
	return nil
}
func (m *Module) announce(ctx context.Context, e *entry) {
	m.publish(ctx, "session.state", map[string]any{"name": e.session.Name, "state": string(e.session.State)})
	m.wakeWaits() // 等你回覆的推播跟著 Pending 走（SES-47）
	for w := range m.watches {
		if w.ctx.Err() != nil {
			close(w.ch)
			delete(m.watches, w)
			continue
		}
		if w.agent == "" || w.agent == e.session.Agent {
			select {
			case w.ch <- &sdk.SessionWatchResponse{Session: copySession(e.session)}:
			default:
				close(w.ch)
				delete(m.watches, w)
			}
		}
	}
}
func (m *Module) State(name string) (State, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[name]
	if !ok {
		return "", false
	}
	return e.session.State, true
}
func (m *Module) Get(ctx context.Context, name string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	e := m.entries[name]
	if e == nil {
		return nil, platform.NotFound("找不到這個對話")
	}
	return copySession(e.session), nil
}
func (m *Module) List(ctx context.Context, agent string, states []State) ([]Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	out := []Session{}
	for _, e := range m.entries {
		if agent != "" && agent != e.session.Agent {
			continue
		}
		match := len(states) == 0
		for _, s := range states {
			if s == e.session.State {
				match = true
			}
		}
		if match {
			out = append(out, copySession(e.session))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (m *Module) Open(ctx context.Context, spec OpenSpec) (Session, error) {
	if spec != nil && spec.Wake {
		return m.wake(ctx, spec) // 主人打開聊天頁：能叫醒才開（REQ-386）
	}
	l := m.sessionLock(openName(spec))
	l.Lock()
	defer l.Unlock()
	return m.openHeld(ctx, spec, false)
}

// openName：這次要開的是哪個對話（沒給名稱就是那位秘書的主要對話）。
func openName(spec OpenSpec) string {
	if spec == nil {
		return ""
	}
	if spec.Name != "" {
		return spec.Name
	}
	return spec.Agent
}

// openHeld 把一個對話開到底（呼叫時拿著她的 sessionLock）：檢查與登記、等引擎開起來、開好之後的事。
func (m *Module) openHeld(ctx context.Context, spec OpenSpec, force bool) (Session, error) {
	s, err := m.open(ctx, spec, force)
	return m.opened(ctx, spec, s, err)
}

// opened 是開好之後的事：工作 session 記回單、收掉名額詢問；有第一句就交給派送器。
func (m *Module) opened(ctx context.Context, spec OpenSpec, s Session, err error) (Session, error) {
	if err == nil && s != nil && s.Task != "" {
		m.recordWork(ctx, s)
		if s.QueuedAt.IsZero() {
			m.mu.Lock()
			m.dropAsk(m.entries[s.Name]) // 不經排隊直接開了：名額詢問當下收掉（S5-2）
			m.mu.Unlock()
			m.retireAsks(ctx)
		}
	}
	if err != nil || spec == nil || strings.TrimSpace(spec.FirstMessage) == "" {
		return s, err
	}
	// 引擎不送第一句（ECL）：當成主人說的話交給派送器，對話準備好（idle）才貼進去。
	if _, err = m.d.Bus.Send(ctx, &sdk.Envelope{Kind: sdk.EnvelopeKindChat, Route: sdk.EnvelopeRouteCore, From: "owner", To: s.Name, Body: spec.FirstMessage}); err != nil {
		return s, err
	}
	m.wakeDelivery()
	return s, nil
}

// openTry 是 openBackground 當場的結果。
type openTry int

const (
	openBusy     openTry = iota // 這個對話正在開、關或重開：沒動
	openSettled                 // 不用叫引擎（已經開著、排進佇列、或檢查沒過）
	openLaunched                // 已經記成開機中，引擎那一段在背景等
)

// openBackground：不等引擎的開法（定時那一輪、派送器用）。檢查與登記當場做完（對話馬上是「開機中」，名額、上限照算），
// 等引擎開起來的那一段放到背景；一位秘書的引擎不回，只有她自己停在開機中到時限，別人不用等（DEV-159）。
// 這個對話正在開、關或重開（鎖被拿著）就不動。done（可為 nil）在整件事結束後叫一次：一律在背景叫、
// 叫的時候還拿著這個對話的鎖（不能在裡面再開關同一個對話）；對話服務正在停就不叫。
func (m *Module) openBackground(ctx context.Context, spec OpenSpec, force bool, done func(Session, error)) openTry {
	l := m.sessionLock(openName(spec))
	if !l.TryLock() {
		return openBusy
	}
	p, s, err := m.prepareOpen(ctx, spec, force)
	started := m.spawn(ctx, func(ctx context.Context) {
		defer l.Unlock()
		if p != nil {
			s, err = m.startEngine(ctx, p)
		}
		if done != nil {
			done(s, err)
		}
	})
	if !started {
		if p != nil {
			m.abandon(ctx, p) // 對話服務正在停：不叫引擎，記回停了，下次啟動再開
		}
		l.Unlock()
	}
	if p == nil {
		return openSettled
	}
	return openLaunched
}

// openLater：跟 Open 一樣開一個對話（含開好之後的事），只是不等引擎；done 同 openBackground。
func (m *Module) openLater(ctx context.Context, spec OpenSpec, done func(Session, error)) openTry {
	return m.openBackground(ctx, spec, false, func(s Session, err error) {
		s, err = m.opened(ctx, spec, s, err)
		if done != nil {
			done(s, err)
		}
	})
}

// seatAccount 算出一位秘書用哪個帳號、帳號的設定表與該帳號的引擎。開常駐與測口吻（tone.go）共用。
func (m *Module) seatAccount(r vault.Record) (account string, table map[string]any, engine EngineAccess, err error) {
	account = text(r.Fields, "account")
	if account == "" {
		account, _ = m.config.Accounts["default"].(string)
	}
	table, ok := m.config.Accounts[account].(map[string]any)
	if !ok {
		return "", nil, nil, platform.Invalid("account", "找不到對應的帳號設定")
	}
	engine, err = m.d.Engine.For(account)
	if err != nil {
		return "", nil, nil, err
	}
	return account, table, engine, nil
}

// workdir 是秘書的工作資料夾：home.toml 的 [sessions] workdir，沒設就家。
func (m *Module) workdir() string {
	if m.config.Sessions.Workdir != "" {
		return m.config.Sessions.Workdir
	}
	return m.home
}

// newConversation：這個 session 換了一段新的對話（呼叫時持有 m.mu）：之後接回用新的編號，已讀位置與未讀從頭算。
func (m *Module) newConversation(e *entry, id string) {
	e.session.EngineSessionID = id
	e.seen = ""
	e.messages = nil
	e.lastAssistant = nil
	e.ended = false
	m.transcriptDirty[e.session.Name] = true
}

// launch 是 prepareOpen 準備好、還沒交給引擎的一次啟動。
type launch struct {
	name   string
	engine EngineAccess
	start  *sdk.EngineStartRequest
	e      *entry
	task   *sdk.Task
}

// open 開一個對話（呼叫時拿著她的 sessionLock）：先檢查與登記，再等引擎開起來。
func (m *Module) open(ctx context.Context, spec OpenSpec, force bool) (Session, error) {
	p, s, err := m.prepareOpen(ctx, spec, force)
	if p == nil {
		return s, err
	}
	return m.startEngine(ctx, p)
}

// prepareOpen 是開對話裡不用等引擎的那一段：檢查、過工作 session 的名額、組啟動設定、把對話記成開機中。
// 全家一次只做一個（operations）：兩個工作 session 同時開，才不會都以為還有名額。回 launch＝接著交給引擎；
// launch 是 nil＝不用啟動（已經開著、排隊中、或出錯），照回傳的 Session 與錯誤。
func (m *Module) prepareOpen(ctx context.Context, spec OpenSpec, force bool) (*launch, Session, error) {
	if spec == nil {
		return nil, nil, platform.Invalid("session", "請指定對話")
	}
	m.operations.Lock()
	defer m.operations.Unlock()
	req := *spec
	if req.Name == "" {
		req.Name = req.Agent
	}
	if !validName(req.Name) || !validName(req.Agent) {
		return nil, nil, platform.Invalid("name", "請使用有效的對話與職位名稱")
	}
	if req.Kind == "" {
		req.Kind = sdk.SessionKindRoom
		if req.Name == req.Agent {
			req.Kind = sdk.SessionKindResident
		}
	}
	if req.Kind != sdk.SessionKindResident && req.Kind != sdk.SessionKindRoom && req.Kind != sdk.SessionKindTemp && req.Kind != sdk.SessionKindClone && req.Kind != sdk.SessionKindWork {
		return nil, nil, platform.Invalid("kind", "對話種類不正確")
	}
	m.mu.Lock()
	if err := m.ready(ctx); err != nil {
		m.mu.Unlock()
		return nil, nil, err
	}
	old := m.entries[req.Name]
	if old != nil && old.session.State != sdk.SessionStateStopped {
		s := copySession(old.session)
		m.mu.Unlock()
		return nil, s, nil
	}
	oldCwd := ""
	oldTask := ""
	if old != nil {
		req.Agent = old.session.Agent
		req.Kind = old.session.Kind
		oldCwd = old.session.Cwd
		oldTask = old.session.Task
	}
	m.mu.Unlock()
	// 工作 session（一張任務單一個，P7-5、D-33）：先看單、過上限這一關；排隊的回已建好但沒啟動的那一筆。
	var task *sdk.Task
	var err error
	if req.Kind == sdk.SessionKindWork {
		if id := workTaskID(m.ids(), &req, oldTask); id != "" && m.d.Tasks != nil {
			var parked Session
			if task, parked, err = m.workGate(ctx, &req, id, force); err != nil || parked != nil {
				return nil, parked, err
			}
		}
	}
	r, err := m.d.Vault.Get(ctx, "agent", req.Agent)
	if err != nil {
		return nil, nil, err
	}
	person, err := m.d.Agents.PersonaName(ctx, req.Agent)
	if err != nil {
		return nil, nil, err
	}
	instructions, err := m.d.Agents.Instructions(ctx, req.Agent, req.Name)
	if err != nil {
		return nil, nil, err
	}
	account, table, engine, err := m.seatAccount(r)
	if err != nil {
		return nil, nil, err
	}
	cwd := req.Cwd
	if cwd == "" {
		cwd = oldCwd // 接回時沿用原本的資料夾，引擎才找得到之前的對話
	}
	if cwd == "" {
		cwd = m.workdir()
	}
	remote, _ := table["remote_access"].(bool)
	remote = remote && (req.Kind == sdk.SessionKindResident || (req.Kind == sdk.SessionKindWork && task != nil))
	config, _ := table["engine_config"].(map[string]any)
	start := &sdk.EngineStartRequest{Name: req.Name, Agent: req.Agent, Cwd: cwd, Account: account, EngineConfig: config, PermissionLevel: m.permissionLevel(req.Agent), Model: text(r.Fields, "model"), Effort: text(r.Fields, "effort"), Instructions: instructions, RemoteAccess: remote, FirstMessage: req.FirstMessage, HostEndpoint: platform.RuntimeDir(m.home, "sockets", "api.sock")}
	if remote {
		start.RemoteAccessName = remoteAccessName(m.remotePerson(ctx, req.Agent, person), m.config.Label())
		if task != nil {
			start.RemoteAccessName += "-" + task.ID // 工作 session：<人名>-<owner>-<machine>[@dev]-<單號>（D-33）
		}
	}
	m.mu.Lock()
	e := m.entries[req.Name]
	if e == nil {
		e = &entry{session: sdk.Session{Name: req.Name, Agent: req.Agent, Kind: req.Kind}, title: req.Topic, idleClose: 30 * time.Minute}
		if e.title == "" {
			e.title = req.Name
			if req.Kind == sdk.SessionKindResident {
				e.title = person
			}
		}
		if task != nil {
			e.title = task.ID + " " + task.Title
		}
		m.entries[req.Name] = e
	}
	start.ResumeSessionID = e.session.EngineSessionID
	if task != nil {
		m.workStarting(e, task, &start.ResumeSessionID)
	}
	// 換了帳號：舊的對話在另一個帳號的資料夾，新帳號接不回來（引擎接舊對話會直接失敗），開新的（REQ-261）。
	if e.session.Account != "" && e.session.Account != account {
		start.ResumeSessionID = ""
	}
	e.session.Account = account
	e.session.Cwd = cwd
	e.session.Engine = text(table, "engine")
	// 開的時候先記設定（D-46）；引擎在啟動結果與之後的事件說得出實際的值再換。設定沒寫就是空的（不知道），不沿用上一次的。
	e.session.Model, e.session.ModelLabel, e.session.Effort = start.Model, "", start.Effort
	if remote {
		e.session.RemoteAccess = &sdk.RemoteAccess{Enabled: true, Name: start.RemoteAccessName}
	} else {
		e.session.RemoteAccess = nil
	}
	m.finishApproval(req.Name, nil, false) // 重開：畫面上等的確認也跟著舊的那一次不在了
	e.session.Pending = nil
	m.setState(e, sdk.SessionStateStarting, m.d.Clock.Now())
	err = m.changed(ctx, e)
	m.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	return &launch{name: req.Name, engine: engine, start: start, e: e, task: task}, nil, nil
}

// abandon：登記成開機中之後決定不叫引擎了（對話服務正在停）：記回停了。
func (m *Module) abandon(ctx context.Context, p *launch) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.e.session.State == sdk.SessionStateStarting {
		m.setState(p.e, sdk.SessionStateStopped, m.d.Clock.Now())
		if err := m.changed(context.WithoutCancel(ctx), p.e); err != nil {
			m.warn("儲存啟動失敗狀態失敗", err)
		}
	}
}

// startEngine 把準備好的啟動交給引擎、等它回來（最多 limits.Start），記下結果。不拿 operations：別的對話照常開關。
// 時限到了當成沒開起來：記回停了、回一句白話；引擎之後才真的開起來的話，它回報的事件照常把狀態帶回來（SES-4）。
func (m *Module) startEngine(ctx context.Context, p *launch) (Session, error) {
	e, start, task := p.e, p.start, p.task
	// 接不回原本的對話時，單的內容不在新對話裡：放掉 mu 之後再送一次（defer 順序：比下面的 Unlock 晚跑）。
	lostTask := ""
	defer func() {
		if lostTask != "" {
			m.resendBrief(ctx, p.name, lostTask)
		}
	}()
	// The engine may report SESSION_STARTED before Start returns. Never hold mu across it.
	call, cancel := context.WithTimeout(ctx, m.limits.Start)
	response, err := p.engine.Start(call, start)
	late := gaveUp(call, ctx)
	cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		// 還停在這一次的開機中才記回停了：等的這段時間引擎自己回報了別的狀態（例如已經開好）就照它的。
		if e.session.State == sdk.SessionStateStarting {
			m.setState(e, sdk.SessionStateStopped, m.d.Clock.Now())
			if saveErr := m.changed(context.WithoutCancel(ctx), e); saveErr != nil {
				m.warn("儲存啟動失敗狀態失敗", saveErr)
			}
		}
		if late {
			if m.log != nil {
				m.log.Warn("開對話等不到引擎回應", "session", p.name, "waited", m.limits.Start.String())
			}
			return nil, silent("開對話", m.limits.Start, "先記成沒開起來，過一下再試一次；一直這樣的話到「設定 → 電腦」看看是哪裡出了問題。")
		}
		return nil, err
	}
	if response == nil {
		return nil, platform.Internal("引擎沒有回傳啟動結果")
	}
	if start.ResumeSessionID != "" && response.EngineSessionID != start.ResumeSessionID {
		// 引擎接不回原本的對話、開了新的（ENG-12、SES-67）：已讀位置從頭算，工作 session 要再拿到一次單。
		m.log.Warn("接不回原本的對話，開了新的", "session", p.name, "from", start.ResumeSessionID, "to", response.EngineSessionID)
		m.newConversation(e, response.EngineSessionID)
		if task != nil {
			lostTask = task.ID
		}
	}
	e.session.EngineSessionID = response.EngineSessionID
	e.session.Attach = response.Attach
	applyModel(&e.session, response.Model, response.ModelLabel, response.Effort)
	if e.session.RemoteAccess != nil {
		e.session.RemoteAccess.URL = response.RemoteAccessURL
	}
	if err = m.changed(ctx, e); err != nil {
		return nil, err
	}
	return copySession(e.session), nil
}
func (m *Module) Close(ctx context.Context, name string) error {
	l := m.sessionLock(name)
	l.Lock()
	defer l.Unlock()
	return m.closeByOwner(ctx, name)
}

// CloseAll 把這個家還開著的對話全部收掉（SES-75）：整個家要換成備份之前叫（system 的 homebackup），不然服務重開後
// 她們還帶著還原前的內容繼續做事。跟主人手動關不一樣：不記「不要自動接回」，服務重開後常駐的、進行中的單照平常的規則再開。
// 一個一個關（各拿各的鎖），關不掉的略過繼續；回關了幾個，有關不掉的回最後一個錯。
func (m *Module) CloseAll(ctx context.Context) (closed int, err error) {
	list, err := m.List(ctx, "", nil)
	if err != nil {
		return 0, err
	}
	var last error
	for _, s := range list {
		if s.State == sdk.SessionStateStopped {
			continue
		}
		l := m.sessionLock(s.Name)
		l.Lock()
		cerr := m.close(ctx, s.Name)
		l.Unlock()
		if cerr != nil {
			last = cerr
			continue
		}
		closed++
	}
	return closed, last
}

// closeByOwner：主人（或閒置）關掉一個對話（呼叫時拿著她的 sessionLock）。
func (m *Module) closeByOwner(ctx context.Context, name string) error {
	err := m.close(ctx, name)
	if err == nil {
		m.suspendWork(name) // 主人手動關掉的工作 session，單回到 doing 之前不自動接回（P7-5）
		m.retireAsks(ctx)
	}
	return err
}

// stopEngine 叫引擎關一個對話、等它回來（最多 limits.Stop）。時限到了回一句白話，核心的狀態不動：
// 引擎之後真的關掉了，它回報的結束事件照常把她記成停了（SES-4）。
func (m *Module) stopEngine(ctx context.Context, engine EngineAccess, name string) error {
	call, cancel := context.WithTimeout(ctx, m.limits.Stop)
	defer cancel()
	err := engine.Stop(call, name)
	if err != nil && gaveUp(call, ctx) {
		if m.log != nil {
			m.log.Warn("關對話等不到引擎回應", "session", name, "waited", m.limits.Stop.String())
		}
		return silent("關對話", m.limits.Stop, "對話可能還開著；過一下再試一次。")
	}
	return err
}

// close 關一個對話（呼叫時拿著她的 sessionLock）。
func (m *Module) close(ctx context.Context, name string) error {
	s, err := m.Get(ctx, name)
	if err != nil {
		return err
	}
	if s.State == sdk.SessionStateStopped {
		// 核心記成停了，引擎那邊還是可能開著（DEV-10：舊的結束事件晚到，把剛重開的記成停）。
		// 照樣叫引擎關一次；引擎對不在的 session 不算錯。
		if engine, err := m.d.Engine.For(s.Account); err == nil {
			return m.stopEngine(ctx, engine, name)
		}
		return nil
	}
	engine, err := m.d.Engine.For(s.Account)
	if err != nil {
		return err
	}
	if err = m.stopEngine(ctx, engine, name); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[name]
	m.finishApproval(name, nil, false)
	e.session.Pending = nil
	now := m.d.Clock.Now()
	m.setState(e, sdk.SessionStateStopped, now)
	// 核心自己關的對話，引擎不一定回報結束：照「對話結束了」把還掛著的話收掉（SES-57），不然重開之後
	// 那幾則永遠是收到、後面的話排著（DEV-162：重開對話也救不回來）。
	ended := &sdk.EngineEvent{Session: name, Type: sdk.EngineEventTypeSessionEnded, At: now, Reason: "closed"}
	m.eventQueue = append(m.eventQueue, ended)
	select {
	case m.dispatcher <- ended:
	default:
	}
	return m.changed(ctx, e)
}
func (m *Module) Rotate(ctx context.Context, name string) error {
	l := m.sessionLock(name)
	l.Lock()
	defer l.Unlock()
	return m.rotate(ctx, name)
}

// rotate 重開一個對話、清掉 context（呼叫時拿著她的 sessionLock）。
func (m *Module) rotate(ctx context.Context, name string) error {
	s, err := m.Get(ctx, name)
	if err != nil {
		return err
	}
	if err = m.close(ctx, name); err != nil {
		return err
	}
	m.mu.Lock()
	e := m.entries[name]
	e.session.EngineSessionID = ""
	e.seen = ""
	e.messages = nil
	e.lastAssistant = nil
	e.ended = false
	err = m.save(e)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	_, err = m.open(ctx, &sdk.SessionOpenRequest{Name: name, Agent: s.Agent, Kind: s.Kind}, false)
	return err
}

// quiet：背景裡要關或重開一個閒著的對話之前，先佔住她的派送位置（inFlight，跟對話指令同一個）並再看一次她還閒著。
// 決定要關到真的動手之間隔了一小段（背景），這段時間派送器可能正要貼話給她、或她自己開始了一輪：那就不關，下一輪再看。
// limitedOK：額度用完的也算（換帳號重開）。ok 時做完要叫 release。呼叫時拿著她的 sessionLock。
func (m *Module) quiet(name string, limitedOK bool) (release func(), ok bool) {
	m.dispatchMu.Lock()
	if m.inFlight[name] {
		m.dispatchMu.Unlock()
		return nil, false
	}
	m.inFlight[name] = true
	m.dispatchMu.Unlock()
	release = func() {
		m.dispatchMu.Lock()
		delete(m.inFlight, name)
		m.dispatchMu.Unlock()
		m.wakeDelivery()
	}
	m.mu.Lock()
	e := m.entries[name]
	idle := e != nil && e.session.Pending == nil && (e.session.State == sdk.SessionStateIdle || (limitedOK && e.session.State == sdk.SessionStateLimited))
	m.mu.Unlock()
	if !idle {
		release()
		return nil, false
	}
	return release, true
}

// background：定時那一輪要關或重開一個對話時用——拿得到她的鎖才在背景做（做的時候拿著），
// 她正在開、關或重開就這一輪先跳過。定時那一輪不等引擎（DEV-159）。
func (m *Module) background(ctx context.Context, name string, fn func(context.Context)) bool {
	l := m.sessionLock(name)
	if !l.TryLock() {
		return false
	}
	if !m.spawn(ctx, func(ctx context.Context) { defer l.Unlock(); fn(ctx) }) {
		l.Unlock()
		return false
	}
	return true
}
func (m *Module) Tick(ctx context.Context, now time.Time) {
	m.tickMu.Lock()
	defer m.tickMu.Unlock()
	m.mu.Lock()
	if m.ready(ctx) != nil {
		m.mu.Unlock()
		return
	}
	closeNames := []string{}
	wakeLimited := false
	for name, e := range m.entries {
		if a := m.approvals[name]; a != nil && !a.native && !now.Before(a.deadline) {
			m.handOff(ctx, e, a) // 逾時：引擎退回自己的確認畫面，Pending 留著（SES-48）
		}
		if e.session.State == sdk.SessionStateLimited && m.tickLimited(ctx, e, now) {
			wakeLimited = true // 過了恢復時間：記回待命，排著的話照常送（REQ-379）
		}
		idleAt := e.session.StateSince
		if e.session.LastUserAt.After(idleAt) {
			idleAt = e.session.LastUserAt
		}
		if e.session.Kind != sdk.SessionKindResident && !taskWork(e) && e.session.State == sdk.SessionStateIdle && e.session.Pending == nil && !now.Before(idleAt.Add(e.idleClose)) {
			closeNames = append(closeNames, name)
		} else if m.ondemandClosable(e, now) && e.session.State == sdk.SessionStateIdle && e.session.Pending == nil && !now.Before(idleAt.Add(OndemandIdleClose)) {
			// 隨選秘書的主要 session：閒置 60 分鐘收掉（當天有排程的當天不收，SES-1）。
			closeNames = append(closeNames, name)
		}
	}
	refresh := !now.Before(m.modesAt.Add(WatchdogEvery))
	if refresh {
		m.modesAt = now
	}
	watchdog := m.config.Sessions.Autostart && !now.Before(m.watchdog.Add(WatchdogEvery))
	if watchdog {
		m.watchdog = now
	}
	// 帳號巡檢（REQ-261）跟 autostart 無關：沒有自動開對話的家，換了帳號也要在閒著時換過去。
	accounts := !now.Before(m.accountsAt.Add(WatchdogEvery))
	if accounts {
		m.accountsAt = now
	}
	m.mu.Unlock()
	if wakeLimited {
		m.wakeDelivery()
	}
	if refresh {
		m.refreshModes(ctx, now)
	}
	for _, name := range closeNames {
		// 貼進去、還在等她回報的話（已貼上、收到、處理中）在這個對話裡：不收，等引擎說這一輪怎麼了（S5-2）。
		if live, err := m.d.Bus.List(ctx, name, sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleWorking, sdk.LifecycleBlocked); err != nil || len(live) > 0 {
			continue
		}
		name := name
		m.background(ctx, name, func(ctx context.Context) { // 等引擎的那一段在背景：她的引擎不回，這一輪照樣往下走
			release, ok := m.quiet(name, false)
			if !ok {
				return
			}
			defer release()
			if err := m.closeByOwner(ctx, name); err != nil {
				m.warn("關閉閒置對話失敗", err)
			}
		})
	}
	if watchdog || accounts {
		rows, err := m.d.Agents.List(ctx)
		if err != nil {
			m.warn("取得名冊（常駐對話）失敗", err)
		} else {
			for _, r := range rows {
				enabled, _ := r.Fields["enabled"].(bool)
				if !enabled {
					continue
				}
				if (accounts && m.rotateForAccount(ctx, r)) || !watchdog {
					continue
				}
				// 常駐的一直開著；隨選的只有「今天有排程」才開（當天視為常駐），其餘等第一句話（SES-1）。
				m.mu.Lock()
				today := m.scheduledOn[r.ID] != ""
				m.mu.Unlock()
				mode := text(r.Fields, "mode")
				if mode != "resident" && !(mode == "ondemand" && today) {
					continue
				}
				state, ok := m.State(r.ID)
				if ok && state != sdk.SessionStateStopped {
					continue
				}
				m.openLater(ctx, &sdk.SessionOpenRequest{Name: r.ID, Agent: r.ID, Kind: sdk.SessionKindResident}, func(_ Session, err error) {
					if err != nil {
						if !m.engineStillStarting(err, now) {
							m.warn("開啟常駐對話失敗", err)
						}
						return
					}
					m.mu.Lock()
					m.engineWait = time.Time{}
					m.mu.Unlock()
				})
			}
		}
	}
	m.reconcileWork(ctx, now)
	m.dispatch(ctx, now)
	m.syncWaits(ctx, now)
	m.refreshUnread(ctx)
	m.mu.Lock()
	m.flushThreads(now)
	m.mu.Unlock()
}

// rotateForAccount：秘書綁的帳號跟她正在跑的常駐對話不一樣（設定頁換帳號），閒著就換新帳號
// 開一個新的對話（舊對話在舊帳號的資料夾，接不回來）；在忙、卡確認、有待回答的，下一輪巡檢再看（REQ-261）。
func (m *Module) rotateForAccount(ctx context.Context, r *sdk.Record) bool {
	want := text(r.Fields, "account")
	if want == "" {
		want, _ = m.config.Accounts["default"].(string)
	}
	m.mu.Lock()
	e := m.entries[r.ID]
	// 額度用完（limited）也算閒著：換帳號常常就是為了這個，排著的話換過去之後照送（REQ-378）。
	due := e != nil && e.session.Kind == sdk.SessionKindResident && (e.session.State == sdk.SessionStateIdle || e.session.State == sdk.SessionStateLimited) && e.session.Pending == nil &&
		e.session.Account != "" && want != "" && e.session.Account != want
	from := ""
	if e != nil {
		from = e.session.Account
	}
	m.mu.Unlock()
	if !due {
		return false
	}
	name := r.ID
	m.background(ctx, name, func(ctx context.Context) { // 等引擎關、開的那一段在背景；正在開關的這一輪先跳過，下一輪巡檢再看
		release, ok := m.quiet(name, true)
		if !ok {
			return
		}
		defer release()
		if err := m.rotate(ctx, name); err != nil {
			m.warn("換帳號重開對話失敗", err)
			return
		}
		if m.log != nil {
			m.log.Info("帳號改了，閒著時換新帳號重開", "session", name, "from", from, "to", want)
		}
	})
	return true
}

// engineStillStarting 回 true 表示引擎套件還在啟動（serve 剛起來的前幾秒）：這時不算失敗、不寫 WARN，
// 並讓下一次 Tick 很快再試，而不是等一整個 WatchdogEvery；超過 EngineReadyGrace 還沒好才當真的失敗。
func (m *Module) engineStillStarting(err error, now time.Time) bool {
	var pe *platform.Err
	if !errors.As(err, &pe) || pe.Code != sdk.ErrorCodeUnavailable {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.engineWait.IsZero() {
		m.engineWait = now
	}
	if !now.Before(m.engineWait.Add(EngineReadyGrace)) {
		return false
	}
	m.watchdog = now.Add(EngineRetryEvery - WatchdogEvery)
	return true
}
func clipped(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > 80 {
		r = r[:80]
	}
	return string(r)
}

// permissionLevel 每次開 session 都重讀 settings/trust.toml，改了等級（evergenie setup permissions）不用重啟 serve；
// 讀不到就用啟動時載入的設定。
func (m *Module) permissionLevel(agent string) sdk.PermissionLevel {
	if m.home != "" {
		if h, err := platform.LoadHome(m.home); err == nil {
			return m.trustLevels.LevelFor(h.Trust, agent)
		}
	}
	return m.trustLevels.LevelFor(m.config.Trust, agent)
}

// remotePerson：同一個角色也派給別的啟用中秘書時，名字補「·職位 id」，手機 App 上才分得出兩個對話（DEV-90）。
// 查不到名冊就照原名，不擋開對話。
func (m *Module) remotePerson(ctx context.Context, agent, person string) string {
	rows, err := m.d.Agents.List(ctx)
	if err != nil {
		return person
	}
	for _, r := range rows {
		if r == nil || r.ID == agent {
			continue
		}
		if on, _ := r.Fields["enabled"].(bool); !on {
			continue
		}
		if other, err := m.d.Agents.PersonaName(ctx, r.ID); err == nil && other == person {
			return person + "·" + agent
		}
	}
	return person
}

// remoteAccessName 是常駐 session 在手機 App 裡的遠端存取名稱：<人名>-<家的標籤>，標籤是 <owner>-<machine>，開發家加 @dev
// （例 雅典娜-amy-home、雅典娜-amy-home@dev；2026-11-01 起取代 remote_suffix）。標籤是空的就只有人名。
func remoteAccessName(person, label string) string {
	if label == "" {
		return person
	}
	return person + "-" + label
}
