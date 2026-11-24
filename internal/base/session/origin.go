package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

type turn struct {
	Origin    string    `json:"origin"`
	From      string    `json:"from"`
	Envelope  string    `json:"envelope,omitempty"`
	StartedAt time.Time `json:"started_at"`
	// Adopted：這一輪是 adoptDelivered 替「她的對話裡已經有這句」開的，還沒看到她自己的開始事件（DEV-99）。
	// 跟著存進狀態檔：核心重開後這一輪照樣有期限（SES-76，DEV-162）。
	Adopted bool `json:"adopted,omitempty"`
}
type directCall struct {
	id, to    string
	ended, ok bool
}
type dispatchTurn struct {
	turn
	source       sdk.InputSource
	input        string
	sequence     int
	calls        []*directCall
	peerDeadline time.Time
	ended        *sdk.EngineEvent
	// carried 是同一輪裡先前收到的訊息：主人在秘書忙時中途補話（SES-41），引擎在工具步驟中間收進去，
	// 兩則在同一個回合裡一起回，回合結束時一起記成已回覆。
	carried []string
}

func prefix(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
func owner(from string) bool { return platform.IsOwner(from) && strings.HasSuffix(from, ")") }
func cleanPeer(name string) string {
	if i := strings.LastIndex(name, " ["); i >= 0 && strings.HasSuffix(name, "]") {
		return name[:i]
	}
	return name
}

// knownPeer：直接傳話的對象是這個家的 session 或秘書。引擎自己的子代理、不是這個家的 session 不追蹤（DEV-85）。
func (m *Module) knownPeer(ctx context.Context, name string) bool {
	name = cleanPeer(name)
	m.mu.Lock()
	_, ok := m.entries[name]
	m.mu.Unlock()
	if ok {
		return true
	}
	_, ok = m.d.Agents.Resolve(ctx, name)
	return ok
}

func (m *Module) agentID(ctx context.Context, name string) string {
	name = cleanPeer(name)
	m.mu.Lock()
	e := m.entries[name]
	id := ""
	if e != nil {
		id = e.session.Agent
	}
	m.mu.Unlock()
	if id != "" {
		return id
	}
	if id, ok := m.d.Agents.Resolve(ctx, name); ok {
		return id
	}
	return name
}

// A known session name identifies that session, not every session of its agent.
func (m *Module) targets(ctx context.Context, to, name string) bool {
	to = cleanPeer(to)
	if to == name {
		return true
	}
	m.mu.Lock()
	known := m.entries[to] != nil
	m.mu.Unlock()
	return !known && m.agentID(ctx, to) == m.agentID(ctx, name)
}
func (m *Module) storeTurn(name string, t turn, directOwner bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[name]; e != nil {
		e.turn = t
		if directOwner {
			e.lastOwner = t.StartedAt
		}
		m.dirty[name] = true
		select {
		case m.persist <- struct{}{}:
		default:
		}
	}
}

// ownerSaid 記下主人在這個對話說了話（工作 session 收起來前要閒 idle_close_after，DEV-147）。
// 只算主人的話（寄件人是主人、直接打字、在看板回答選擇題）：核心送的第一句與排程（From system）、秘書傳話都不算。引擎的「打字的」
// （LastUserAt）分不出來：核心排進去的話貼上後也是打字的。
func (m *Module) ownerSaid(name string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[name]; e != nil && at.After(e.ownerTalk) {
		e.ownerTalk = at
		m.dirty[name] = true
	}
}
func (m *Module) drainEvents(ctx context.Context) {
	m.dispatchMu.Lock()
	defer m.dispatchMu.Unlock()
	m.drainLocked(ctx)
}
func (m *Module) drainLocked(ctx context.Context) {
	for {
		m.mu.Lock()
		if len(m.eventQueue) == 0 {
			m.mu.Unlock()
			return
		}
		ev := m.eventQueue[0]
		m.eventQueue[0] = nil
		m.eventQueue = m.eventQueue[1:]
		m.mu.Unlock()
		if err := m.processEvent(ctx, ev); err != nil {
			m.warn("處理派送事件失敗", err)
		}
	}
}
func (m *Module) processEvent(ctx context.Context, ev EngineEvent) error {
	t := m.turns[ev.Session]
	if ev.Type == sdk.EngineEventTypeTurnStarted {
		if t != nil && ev.Interrupted {
			// 上一輪被打斷：這一輪的話和中途補話帶著的話都記中斷（不然下一次正常結束會把它們算成已回覆）。
			ids := append(append([]string(nil), t.carried...), t.Envelope)
			for _, id := range ids {
				if id == "" {
					continue
				}
				e, err := m.d.Bus.Get(ctx, id)
				if err != nil {
					return err
				}
				if e.Lifecycle == sdk.LifecycleReceived || e.Lifecycle == sdk.LifecycleWorking || e.Lifecycle == sdk.LifecycleBlocked {
					if err = m.finish(ctx, e, sdk.LifecycleInterrupted, "中斷", "", false); err != nil {
						return err
					}
				}
			}
		}
		next := &dispatchTurn{turn: turn{Origin: "owner", From: "owner(session)", StartedAt: ev.At}, source: ev.Source, input: ev.Input}
		if t != nil && !ev.Interrupted && t.ended == nil {
			// 上一輪還沒結束就又開始一輪：是中途補話（SES-41），上一則的封包跟著這一輪結束。
			next.carried = append(next.carried, t.carried...)
			if t.Envelope != "" {
				next.carried = append(next.carried, t.Envelope)
			}
		}
		directOwner, inherited := false, false
		said := false // 這一輪是主人說的話（ownerSaid）
		switch ev.Source {
		case sdk.InputSourceSystem:
			if t != nil {
				next.Origin = t.Origin
				next.From = t.From
			} else {
				m.mu.Lock()
				next.Origin = m.entries[ev.Session].turn.Origin
				next.From = m.entries[ev.Session].turn.From
				m.mu.Unlock()
			}
		case sdk.InputSourcePeer:
			next.Origin = "agent"
			next.From = m.agentID(ctx, ev.From)
			next.peerDeadline = ev.At.Add(10 * time.Second)
		default:
			e, err := m.claimPasted(ctx, ev)
			if err != nil {
				return err
			}
			if e != nil {
				if m.voided[ev.Session].id == e.ID {
					delete(m.voided, ev.Session) // 那句重貼後照常開始了：不用再等平台自己接著做
				}
				next.Envelope = e.ID
				next.From = e.From
				if !owner(e.From) && e.From != "system" {
					next.Origin = "agent"
				}
				// 主人在看板回答選擇題，轉回對話的封包寄件人是 system，但那是主人的回答。
				said = owner(e.From) || e.Kind == sdk.EnvelopeKindAnswer
			} else if t != nil && t.Adopted && t.ended == nil && !ev.Interrupted && m.stillAdopted(ctx, t.Envelope) {
				// 平台自己接著做那句已經在對話裡的話（額度恢復後打「continue」，DEV-99）：這一輪就是那句的，
				// 沿用寄件人與來源，不當成主人在終端機打字（不然秘書傳話或排程那一輪會被當成主人放行，法律第 4 條）。
				next.Envelope = t.Envelope
				next.Origin, next.From = t.Origin, t.From
				next.carried = append([]string(nil), t.carried...)
			} else if v, ok := m.voided[ev.Session]; ok && !ev.Interrupted {
				// 那句到期作廢之後平台才接著做（SES-76）：封包已經放掉了，但這一輪多半還是原寄件人觸發的。
				// 分不出是平台接著做還是主人剛好打字：VoidedKeep 內寧可記成原寄件人（比較嚴的那邊，法律第 4 條），
				// 只用一次；過了就是主人自己打的。
				delete(m.voided, ev.Session)
				if !ev.At.After(v.at.Add(VoidedKeep)) {
					next.Origin, next.From = v.origin, v.from
					inherited = true
				}
			}
			directOwner = next.Envelope == "" && !inherited
			// 直接打字算；平台自己接著做那句（上面那條）不算新的一句，不延後收起來的時間。
			said = said || directOwner
		}
		m.turns[ev.Session] = next
		if next.source == sdk.InputSourcePeer {
			m.pendingPeers[next] = ev.Session
		}
		t = next
		if ev.Source == sdk.InputSourcePeer {
			if err := m.matchPeer(ctx, ev.Session, t); err != nil {
				return err
			}
		}
		m.storeTurn(ev.Session, t.turn, directOwner)
		if said {
			m.ownerSaid(ev.Session, ev.At)
		}
	}
	switch ev.Type {
	case sdk.EngineEventTypeRateLimited:
		return m.requeueLimited(ctx, ev, t) // 額度用完：這一輪拿到的話不算收到（REQ-378）
	case sdk.EngineEventTypeTurnFailed, sdk.EngineEventTypeIdleHint, sdk.EngineEventTypeSessionEnded:
		return m.settleTurn(ctx, ev, t) // 引擎說這一輪停了（S5-2）
	}
	if t == nil {
		if ev.Type == sdk.EngineEventTypeTurnEnded {
			return m.settleSession(ctx, ev.Session, sdk.LifecycleReplied, "", ev.LastMessage) // 核心重開丟了這一輪的記憶
		}
		return nil
	}
	switch ev.Type {
	case sdk.EngineEventTypeActivityStarted:
		t.sequence++
		if err := m.turnTransition(ctx, t, sdk.LifecycleWorking); err != nil {
			return err
		}
		if dm := ev.DirectMessage; dm != nil && m.knownPeer(ctx, dm.To) {
			to := cleanPeer(dm.To)
			from := m.agentID(ctx, ev.Session)
			reply := ""
			if t.source == sdk.InputSourcePeer && m.agentID(ctx, to) == t.From {
				reply = t.Envelope
			}
			e, err := m.d.Bus.Send(ctx, &sdk.Envelope{Route: sdk.EnvelopeRouteDirect, Kind: sdk.EnvelopeKindSay, From: from, To: to, Body: dm.Text, ReplyTo: reply, IdempotencyKey: fmt.Sprintf("%s#%d#%d", ev.Session, t.StartedAt.UnixNano(), t.sequence)})
			if err != nil {
				return err
			}
			t.calls = append(t.calls, &directCall{id: e.ID, to: to})
		}
	case sdk.EngineEventTypeActivityEnded:
		if dm := ev.DirectMessage; dm != nil {
			for _, c := range t.calls {
				if !c.ended && c.to == cleanPeer(dm.To) {
					c.ended = true
					c.ok = dm.OK
					if !dm.OK {
						e, err := m.d.Bus.Get(ctx, c.id)
						if err != nil {
							return err
						}
						if err = m.finish(ctx, e, sdk.LifecycleFailed, "對方不在", "", true); err != nil {
							return err
						}
					} else if err := m.directSent(ctx, c.id); err != nil {
						return err
					}
					break
				}
			}
		}
		return m.turnTransition(ctx, t, sdk.LifecycleWorking)
	case sdk.EngineEventType("approval_blocked"):
		return m.turnTransition(ctx, t, sdk.LifecycleBlocked)
	case sdk.EngineEventType("approval_resolved"):
		return m.turnTransition(ctx, t, sdk.LifecycleWorking)
	case sdk.EngineEventTypeTurnEnded:
		t.ended = ev
		defer m.clearAdopted(ev.Session, t)
		if err := m.endTurn(ctx, t); err != nil {
			return err
		}
		// 這一輪結束了：這個對話還掛著收到／處理中的（核心重開、中途補話沒對上）一起記已回覆（S5-2）。
		return m.settleSession(ctx, ev.Session, sdk.LifecycleReplied, "", ev.LastMessage)
	}
	return nil
}
func (m *Module) turnTransition(ctx context.Context, t *dispatchTurn, to sdk.Lifecycle) error {
	if t.Envelope == "" {
		return nil
	}
	e, err := m.d.Bus.Get(ctx, t.Envelope)
	if err != nil {
		return err
	}
	if e.Lifecycle == to || terminal(e.Lifecycle) {
		return nil
	}
	if to == sdk.LifecycleWorking && e.Lifecycle != sdk.LifecycleReceived && e.Lifecycle != sdk.LifecycleBlocked {
		return nil
	}
	return m.d.Bus.Transition(ctx, e.ID, to, bus.TransitionOpt{})
}
func (m *Module) matchPeer(ctx context.Context, name string, t *dispatchTurn) error {
	if t.Envelope != "" {
		return nil
	}
	// 平台說送到了的（received，S5-2）還沒配給任何一輪的也算候選。
	rows, err := m.d.Bus.List(ctx, "", sdk.LifecycleNotified, sdk.LifecycleReceived)
	if err != nil {
		return err
	}
	// 外框的 from-name 是寄件人對話目前的顯示名稱，會被改名（「架構師-V3 QQ」），不一定等於核心記的秘書 id。
	// 所以名字對得上或內文對得上都算候選；兩個都對的優先，其次內文，最後才是只有名字對（DEV-96）。
	var selected *sdk.Envelope
	best := 0
	for _, e := range rows {
		if e.Route != sdk.EnvelopeRouteDirect || !m.targets(ctx, e.To, name) || (e.Lifecycle == sdk.LifecycleReceived && m.bound(e.ID)) {
			continue
		}
		score := 0
		if m.agentID(ctx, e.From) == t.From {
			score++
		}
		if body := prefix(e.Body, 40); body != "" && strings.Contains(t.input, body) {
			score += 2
		}
		// 分數一樣時，還停在「已貼上」的優先：平台說送到了、卻一直沒配上的舊話，不搶新的一輪。
		score *= 2
		if score > 0 && e.Lifecycle == sdk.LifecycleNotified {
			score++
		}
		if score > best {
			selected, best = e, score
		}
	}
	if selected == nil {
		return nil
	}
	if selected.Lifecycle == sdk.LifecycleNotified {
		if err = m.d.Bus.Transition(ctx, selected.ID, sdk.LifecycleReceived, bus.TransitionOpt{}); err != nil {
			return err
		}
	}
	// 配到之後這一輪的寄件人換成真正的秘書 id：回覆才認得是回覆、授權與紀錄也看得到是誰委派的。
	t.From = m.agentID(ctx, selected.From)
	t.Envelope = selected.ID
	if m.turns[name] == t {
		m.storeTurn(name, t.turn, false)
	}
	delete(m.pendingPeers, t)
	if t.ended != nil {
		return m.endTurn(ctx, t)
	}
	if t.sequence > 0 {
		return m.turnTransition(ctx, t, sdk.LifecycleWorking)
	}
	return nil
}

// stillAdopted：這一輪掛著的那一則現在還是「已經在她的對話裡」記的收到（做完、放掉、退回排隊之後就不是）。
func (m *Module) stillAdopted(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}
	e, err := m.d.Bus.Get(ctx, id)
	if err != nil {
		return false
	}
	_, ok := adoptedSince(e)
	return ok
}
func (m *Module) endTurn(ctx context.Context, t *dispatchTurn) error {
	for _, id := range t.carried {
		e, err := m.d.Bus.Get(ctx, id)
		if err != nil || terminal(e.Lifecycle) {
			continue
		}
		if e.Lifecycle == sdk.LifecycleBlocked || e.Lifecycle == sdk.LifecycleReceived {
			if err = m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleWorking, bus.TransitionOpt{}); err != nil {
				return err
			}
		}
		if err = m.finish(ctx, e, sdk.LifecycleReplied, "", t.ended.LastMessage, false); err != nil {
			return err
		}
	}
	t.carried = nil
	if t.Envelope == "" {
		return nil
	}
	e, err := m.d.Bus.Get(ctx, t.Envelope)
	if err != nil {
		return err
	}
	if terminal(e.Lifecycle) {
		return nil
	}
	state := sdk.LifecycleReplied
	if e.Route == sdk.EnvelopeRouteDirect && e.ReplyTo == "" {
		state = sdk.LifecycleUnanswered
		for _, c := range t.calls {
			if c.ended && c.ok && m.agentID(ctx, c.to) == m.agentID(ctx, e.From) {
				state = sdk.LifecycleReplied
				break
			}
		}
	}
	if e.Lifecycle == sdk.LifecycleBlocked {
		if err = m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleWorking, bus.TransitionOpt{}); err != nil {
			return err
		}
	}
	return m.finish(ctx, e, state, "", t.ended.LastMessage, false)
}

// claimPasted：主人那邊（或派送器貼上的）一輪開始了，找出是哪一則。候選是這個對話經核心送、已經貼上（notified）的，
// 和貼上時引擎說沒送到、現在排著等重試的（貼上其實進去了，例如壓縮中排在輸入框，DEV-70 情境 A）。
// 內文對得上（空白不計）的優先；都對不上就是第一則已貼上的（輸入框會改寫內容，例如圖片）；再沒有就不是派送器送的。
func (m *Module) claimPasted(ctx context.Context, ev EngineEvent) (*sdk.Envelope, error) {
	rows, err := m.d.Bus.List(ctx, ev.Session, sdk.LifecycleNotified, sdk.LifecycleQueued)
	if err != nil {
		return nil, err
	}
	input := squash(ev.Input)
	var match, notified *sdk.Envelope
	for _, e := range rows {
		if e.Route != sdk.EnvelopeRouteCore || (e.Lifecycle == sdk.LifecycleQueued && !pastedBefore(e)) {
			continue
		}
		if body := squash(prefix(printable(e.Body), 40)); match == nil && body != "" && strings.Contains(input, body) {
			match = e
		}
		if notified == nil && e.Lifecycle == sdk.LifecycleNotified {
			notified = e
		}
	}
	e := match
	if e == nil {
		e = notified
		if e != nil && m.log != nil {
			m.log.Warn("輸入與貼上內容不同", "session", ev.Session, "id", e.ID)
		}
	}
	if e == nil {
		return nil, nil
	}
	if e.Lifecycle == sdk.LifecycleQueued {
		if err = m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleNotified, bus.TransitionOpt{Reason: "她的對話裡有這句"}); err != nil {
			return nil, err
		}
	}
	if err = m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleReceived, bus.TransitionOpt{}); err != nil {
		return nil, err
	}
	return e, nil
}

// pastedAt：最後一次貼上（notified）的時間。
func pastedAt(e *sdk.Envelope) time.Time {
	for i := len(e.History) - 1; i >= 0; i-- {
		if e.History[i].State == sdk.LifecycleNotified {
			return e.History[i].At
		}
	}
	return time.Time{}
}

// squash 把連續的空白與換行收成一個空白（比對內文用）。
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// pastedBefore：這則貼上過（歷程裡有 notified）。
func pastedBefore(e *sdk.Envelope) bool {
	for _, h := range e.History {
		if h.State == sdk.LifecycleNotified {
			return true
		}
	}
	return false
}

// bound：這則已經是某一輪的話（直接傳話先記成收到、之後才配給收件方那一輪，S5-2）。
func (m *Module) bound(id string) bool {
	for _, t := range m.turns {
		if t.Envelope == id {
			return true
		}
		for _, c := range t.carried {
			if c == id {
				return true
			}
		}
	}
	return false
}

// directSent：寄件方的平台說直接傳話送到了（ACTIVITY_ENDED ok=true）就記收到；收件方那一輪開始時再配上（S5-2、D5）。
func (m *Module) directSent(ctx context.Context, id string) error {
	e, err := m.d.Bus.Get(ctx, id)
	if err != nil || e.Lifecycle != sdk.LifecycleNotified {
		return err
	}
	return m.d.Bus.Transition(ctx, id, sdk.LifecycleReceived, bus.TransitionOpt{Reason: "平台說送到了"})
}

// 一輪停了、話卻沒有走完時，記在那則話上的原因（S5-2）。
const (
	notStartedReason = "這句還沒開始，可以重送"
	endedReason      = "對話結束了"
	// loggedOutReason：對話是因為帳號登出才結束的（SESSION_ENDED 的 reason＝logged_out）——寫出來，主人才知道要重新登入（SES-64）。
	loggedOutReason = "AI 帳號登出了，要重新登入"
	stoppedReason   = "你叫停了"
)

// endedReasonOf：對話結束時記在話上的原因；帳號登出的寫明要重新登入（SES-64）。
func endedReasonOf(ev EngineEvent) string {
	if ev.Reason == sdk.SessionEndReasonLoggedOut {
		return loggedOutReason
	}
	return endedReason
}

// TURN_FAILED 各種錯誤的白話原因（DEV-156：只講一遍，不寫「這一輪出錯了（…）」這種內部話；看板接在「她沒回完這一句：」後面）。
const (
	overloadedReason = "AI 平台暫時忙不過來"
	authReason       = "AI 帳號登入過期了"
	billingReason    = "AI 帳號的帳單或方案有問題"
	rejectedReason   = "AI 平台不收這個請求"
	unknownReason    = "出錯了"
)

// failedReason 把 TURN_FAILED 的錯誤種類翻成白話（不認得的當 unknown，S5-0）。
func failedReason(kind sdk.EngineErrorKind) string {
	switch kind.Normalize() {
	case sdk.EngineErrorKindOverloaded:
		return overloadedReason
	case sdk.EngineErrorKindAuth:
		return authReason
	case sdk.EngineErrorKindBilling:
		return billingReason
	case sdk.EngineErrorKindRejected:
		return rejectedReason
	}
	return unknownReason
}

// reasonKindOf：記在話上的中斷原因是哪一種錯（看板照它決定給「再說一次」還是「看怎麼處理」）。封包只存白話原因、不存種類，
// 所以照同一組字對回去；升級前記下的舊字也認（那些話最多再掛 12 小時）。對不到（主人叫停、對話結束…）回空。
func reasonKindOf(reason string) sdk.EngineErrorKind {
	switch reason {
	case overloadedReason, "這一輪出錯了（平台暫時忙不過來）":
		return sdk.EngineErrorKindOverloaded
	case authReason, loggedOutReason, "這一輪出錯了（帳號登入有問題）", "她的對話結束了（帳號登出了，要重新登入）":
		return sdk.EngineErrorKindAuth
	case billingReason, "這一輪出錯了（帳單或方案有問題）":
		return sdk.EngineErrorKindBilling
	case rejectedReason, "這一輪出錯了（平台不收這個請求）":
		return sdk.EngineErrorKindRejected
	case unknownReason, "這一輪出錯了":
		return sdk.EngineErrorKindUnknown
	}
	return ""
}

// settleTurn：引擎說這一輪停了、但不是 TURN_ENDED（呼叫時持有 dispatchMu）。
//   - IDLE_HINT 帶 interrupted（主人叫她停下，SES-62）：跟 TURN_FAILED 一樣記中斷「你叫她停下了」；停下之後她又開始了一輪就不收。
//   - IDLE_HINT（她停在輸入框）：跟一輪結束一樣收——這一輪與這個對話還掛著收到／處理中／等確認的記已回覆
//     （漏了結束回報，閒著就是做完了）；還停在「已貼上」的記失敗「這句還沒開始，可以重送」，可以重送。
//   - TURN_FAILED：這一輪的話與還掛著的記中斷，附錯誤種類與平台原話；剛貼上的下一句照常等開始。
//   - SESSION_ENDED：還掛著的記中斷「她的對話結束了」；已貼上的放回排隊，開回來再送。
//
// 正在貼的那則（inFlight）不動，由派送器看引擎的回傳。
func (m *Module) settleTurn(ctx context.Context, ev EngineEvent, t *dispatchTurn) error {
	interrupted := ev.Type == sdk.EngineEventTypeIdleHint && ev.Interrupted
	if interrupted && t != nil && staleInterrupt(t.StartedAt, ev.At) {
		return nil // 停下之後她又開始了下一輪：不收那一輪（SES-62）
	}
	defer m.clearAdopted(ev.Session, t)
	if ev.Type == sdk.EngineEventTypeIdleHint && !interrupted {
		if ev.LastMessage == "" && t != nil {
			// 閒著的提示不帶她最後說的話：拿對話記錄裡這一輪開始之後她最後一句（給回覆用）；拿不到就空著，不自動回（echo.go）。
			copy := *ev
			m.mu.Lock()
			if e := m.entries[ev.Session]; e != nil && e.lastAssistant != nil && !e.lastAssistant.Ts.Before(t.StartedAt) {
				copy.LastMessage = e.lastAssistant.Text
			}
			m.mu.Unlock()
			ev = &copy
		}
		if t != nil && t.ended == nil {
			t.ended = ev
			if err := m.endTurn(ctx, t); err != nil {
				return err
			}
		}
		if err := m.settleSession(ctx, ev.Session, sdk.LifecycleReplied, "", ev.LastMessage); err != nil {
			return err
		}
	} else {
		reason, last := endedReasonOf(ev), ""
		if ev.Type == sdk.EngineEventTypeTurnFailed {
			reason, last = failedReason(ev.Error), ev.LastMessage
		}
		if interrupted {
			// 主人叫她停下（SES-62）：這一輪的話記中斷，不當成回覆（不把她說到一半的話回給寄件人）。
			reason = stoppedReason
		}
		if t != nil && t.ended == nil {
			ids := append(append([]string(nil), t.carried...), t.Envelope)
			t.carried = nil
			t.ended = ev
			delete(m.pendingPeers, t) // 這一輪不會再配到傳話了
			for _, id := range ids {
				if id == "" {
					continue
				}
				e, err := m.d.Bus.Get(ctx, id)
				if err != nil {
					return err
				}
				if e.Lifecycle == sdk.LifecycleReceived || e.Lifecycle == sdk.LifecycleWorking || e.Lifecycle == sdk.LifecycleBlocked {
					if err = m.finish(ctx, e, sdk.LifecycleInterrupted, reason, last, false); err != nil {
						return err
					}
				}
			}
		}
		if err := m.settleSession(ctx, ev.Session, sdk.LifecycleInterrupted, reason, last); err != nil {
			return err
		}
		if ev.Type == sdk.EngineEventTypeSessionEnded {
			// 給她的直接傳話平台說送到了、卻還沒配給任何一輪：她的對話結束了，不會再有那一輪，記中斷（寄件的秘書收到回音）。
			rows, err := m.d.Bus.List(ctx, "", sdk.LifecycleReceived)
			if err != nil {
				return err
			}
			for _, e := range rows {
				if e.Route != sdk.EnvelopeRouteDirect || !m.targets(ctx, e.To, ev.Session) || m.bound(e.ID) {
					continue
				}
				if err = m.finish(ctx, e, sdk.LifecycleInterrupted, endedReasonOf(ev), "", false); err != nil {
					return err
				}
			}
		}
	}
	if ev.Type == sdk.EngineEventTypeTurnFailed || m.inFlight[ev.Session] {
		return nil
	}
	rows, err := m.d.Bus.List(ctx, ev.Session, sdk.LifecycleNotified)
	if err != nil {
		return err
	}
	for _, e := range rows {
		if e.Route != sdk.EnvelopeRouteCore {
			continue
		}
		if ev.Type == sdk.EngineEventTypeSessionEnded {
			err = m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleQueued, bus.TransitionOpt{Reason: "她的對話結束了，開回來再送"})
		} else if pastedAt(e).After(ev.At) {
			continue // 閒著的提示比這次貼上還早（晚到的事件），不算這句沒開始
		} else {
			err = m.finish(ctx, e, sdk.LifecycleFailed, notStartedReason, "", false)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// settleSession：這個對話經核心送、還掛著收到／處理中／等確認的話都收成 state（S5-2：一輪結束就不會再有她的回報）。
func (m *Module) settleSession(ctx context.Context, session string, state sdk.Lifecycle, reason, last string) error {
	rows, err := m.d.Bus.List(ctx, session, sdk.LifecycleReceived, sdk.LifecycleWorking, sdk.LifecycleBlocked)
	if err != nil {
		return err
	}
	for _, e := range rows {
		if e.Route != sdk.EnvelopeRouteCore {
			continue
		}
		if state == sdk.LifecycleReplied && e.Lifecycle == sdk.LifecycleBlocked {
			if err = m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleWorking, bus.TransitionOpt{}); err != nil {
				return err
			}
		}
		if err = m.finish(ctx, e, state, reason, last, false); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) checkAction(ctx context.Context, name string, class sdk.ActionClass, tool string) (sdk.ActionVerdict, string, error) {
	sess, err := m.Get(ctx, name)
	if err != nil {
		return "", "", err
	}
	m.drainEvents(ctx)
	m.mu.Lock()
	t := m.entries[name].turn
	m.mu.Unlock()
	if t.Origin == "" {
		t.Origin = "owner"
	}
	if m.d.Goals == nil {
		return "", "", platform.Unavailable("Goal 服務尚未啟動")
	}
	verdict, reason := m.d.Goals.Decide(t.Origin, t.From, class)
	if verdict == sdk.ActionVerdictDeny && m.log != nil {
		m.log.Info("委派動作未獲授權", "session", name, "from", t.From, "class", class, "tool", tool)
	}
	// 擋下與改成問主人的都記下來（evergenie trust denied、Q7 的權限申請引用它）。
	if (verdict == sdk.ActionVerdictDeny || verdict == sdk.ActionVerdictAsk) && m.home != "" {
		d := sdk.DeniedAction{At: m.d.Clock.Now(), Session: name, Agent: sess.Agent, From: t.From, Origin: t.Origin, Class: class, Tool: tool, Verdict: verdict, Reason: reason}
		if err := m.denialRecorder.AppendDenied(m.home, d); err != nil && m.log != nil {
			m.log.Warn("無法記錄被擋的動作", "error", err)
		}
	}
	return verdict, reason, nil
}
