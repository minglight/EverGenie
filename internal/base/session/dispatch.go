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

const (
	OwnerQuiet       = 3 * time.Minute
	SayCloneAfter    = 5 * time.Minute
	RoutineTempAfter = 2 * time.Minute
	// PasteBackoff：引擎說沒送到（paste／capture／enter、呼叫失敗）後，第 n 次等 PasteBackoff×2^(n-1) 再試，
	// 最多 PasteRetryMax（REQ-391、S5-2）。只決定多久再試：話留在排隊，不因為試了幾次或過了多久就判送不了。
	PasteBackoff  = 500 * time.Millisecond
	PasteRetryMax = 10 * time.Second
	// DialogRetryEvery：引擎說畫面開著確認框（dialog）時，隔多久再試一次（話不算失敗，原因是「等你回答」，REQ-391）。
	DialogRetryEvery = time.Second
	// MainReopenEvery：主要 session 停了又有話要給她時，多久最多叫一次（開不起來時不要每秒重試）。
	MainReopenEvery = 30 * time.Second
	// interjectReason 記在封包歷程上：主人的話在秘書忙的時候送進去（D-5 2026-10-30 改、SES-41）。
	interjectReason = "她忙時送到"
	// legacyInterjectReason：2026-11-03 以前寫在封包歷程上的舊詞（作者說那個字不好聽，改掉），讀的時候照樣認得。
	legacyInterjectReason = "\u63d2\u8a71"
)

type DispatchConfig struct {
	OwnerQuiet, SayCloneAfter, RoutineTempAfter time.Duration
}

func DefaultDispatchConfig() DispatchConfig {
	return DispatchConfig{OwnerQuiet, SayCloneAfter, RoutineTempAfter}
}
func (m *Module) wakeDelivery() {
	select {
	case m.deliveryWake <- struct{}{}:
	default:
	}
}
func (m *Module) deliveryLoop(ctx context.Context) {
	defer m.wg.Done()
	defer m.recoverWorker()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.deliveryWake:
			m.dispatch(ctx, m.d.Clock.Now())
		}
	}
}
func lastStep(e *sdk.Envelope) time.Time {
	if len(e.History) > 0 {
		return e.History[len(e.History)-1].At
	}
	return e.CreatedAt
}
func (m *Module) pastedText(ctx context.Context, e *sdk.Envelope) string {
	return printable(m.renderText(ctx, e))
}

// BlankBodyReason：話裡只有看不見的控制字元，拿掉後沒東西可貼（SES-68）。
const BlankBodyReason = "這句話只有看不見的字元"

// printable 拿掉看不見的控制字元（換行、tab 留著）：例如手機打字夾帶的 ESC 貼進畫面後看不到，
// 引擎永遠對不上「貼進去了沒」，那句話就一直重試（2026-11-07 事件，SES-68）。
func printable(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r == '\r' {
			return '\n'
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

func (m *Module) renderText(ctx context.Context, e *sdk.Envelope) string {
	switch e.Kind {
	case sdk.EnvelopeKindAnswer:
		return "［主人回答 " + e.Notice + "］" + e.Body
	case sdk.EnvelopeKindSay:
		if e.From == "system" {
			return "［核心］" + e.Body // 核心自己發的，不冒用秘書的名字
		}
		// 傳話就是打字給對方：只標誰說的，不標「傳話」（2026-11-01 作者）。
		verb := ""
		if e.ReplyTo != "" {
			verb = "回覆你"
		}
		return "［" + m.person(ctx, e.From) + "（" + e.From + "）" + verb + "］" + e.Body
	case sdk.EnvelopeKindEcho:
		return "［派送通知］" + e.Body
	default:
		return e.Body
	}
}

// pasteRetry：引擎說這次沒送到（paste／capture／enter，或呼叫引擎失敗）。轉回排隊、原因寫進歷程與 log，
// 照沒送到的次數退避（0.5、1、2、4、8、10、10…秒）再試。不判送不了：計時只決定多久再試（S5-2）。
func (m *Module) pasteRetry(ctx context.Context, e *sdk.Envelope, why string) error {
	now := m.d.Clock.Now()
	// 先記次數再轉回排隊：轉狀態時推出去的 session 就帶得到「正在重試」（SES-51）。
	m.recvMu.Lock()
	m.tries[e.ID]++
	n := m.tries[e.ID]
	wait := PasteRetryMax
	if n < 6 {
		wait = PasteBackoff << (n - 1)
	}
	if wait > PasteRetryMax {
		wait = PasteRetryMax
	}
	m.retryAt[e.ID] = now.Add(wait)
	m.recvMu.Unlock()
	if err := m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleQueued, bus.TransitionOpt{Reason: why}); err != nil {
		return err
	}
	// 同一則連續沒送到時不要每次一行（2026-11-14：一則話重試上千次，log 每 12 秒一行洗了好幾個小時，DEV-159、SES-71）：
	// 前 PasteLogFirst 次每次記，之後每 PasteLogEvery 次記一行（退避到 10 秒後大約 5 分鐘一行），try 是累計次數。
	if m.log != nil && pasteLogDue(n) {
		m.log.Warn("貼上沒成功", "session", e.To, "id", e.ID, "reason", why, "try", n)
	}
	return nil
}

// PasteLogFirst、PasteLogEvery：同一則話貼上沒成功的 log 頻率（SES-71）。
const (
	PasteLogFirst = 3
	PasteLogEvery = 30
)

func pasteLogDue(n int) bool { return n <= PasteLogFirst || n%PasteLogEvery == 0 }

// deliverWhy 把引擎回的原因翻成寫進歷程的白話（REQ-391）。
func deliverWhy(reason string, err error) string {
	switch {
	case err != nil:
		return "叫引擎貼上失敗：" + err.Error()
	case reason == "paste":
		return "貼不進她的對話（paste）"
	case reason == "capture":
		return "看不到她的畫面，不敢按送出（capture）"
	case reason == "enter":
		return "按了送出，沒看到她收下（enter）"
	case reason == "":
		return "引擎說沒送到"
	}
	return "引擎說沒送到（" + reason + "）"
}

func (m *Module) dispatch(ctx context.Context, now time.Time) {
	defer m.forgetTries(ctx)
	m.dispatchMu.Lock()
	m.drainLocked(ctx)
	rows, err := m.d.Bus.List(ctx, "")
	if err != nil {
		m.dispatchMu.Unlock()
		m.warn("取得派送封包失敗", err)
		return
	}
	for t, name := range m.pendingPeers {
		if t.source == sdk.InputSourcePeer && t.Envelope == "" && !t.peerDeadline.IsZero() && !now.Before(t.peerDeadline) {
			if err = m.matchPeer(ctx, name, t); err != nil {
				m.warn("配對直接傳話失敗", err)
			}
			if t.Envelope == "" && m.log != nil { // 收件方開了一輪傳話卻沒有對應封包：留下線索（DEV-96 當時完全沒有 log）
				// 日誌不寫說了什麼（DEV-153、ERR-8）：只留長度，夠判斷是不是同一句。
				m.log.Info("收到傳話但找不到對應的封包", "session", name, "from", t.From, "input_len", len([]rune(t.input)))
			}
			t.peerDeadline = time.Time{}
			delete(m.pendingPeers, t)
		}
	}
	if err = m.retryAnswers(ctx, rows, now); err != nil {
		m.warn("重送回答失敗", err)
	}
	rows, err = m.d.Bus.List(ctx, "")
	if err != nil {
		m.dispatchMu.Unlock()
		return
	}
	// 沒有事件會來的那幾種掛著（SES-76、SES-77）：到期的放掉，動過封包就重讀一次再派送。
	moved := m.expireAdopted(ctx, rows, now)
	stuck, rescued := m.watchStuck(ctx, rows, now)
	defer func() { m.publishStuck(ctx, stuck) }()
	if moved || rescued {
		if rows, err = m.d.Bus.List(ctx, ""); err != nil {
			m.dispatchMu.Unlock()
			return
		}
	}
	active := map[string]bool{}
	for _, e := range rows {
		if e.Route == sdk.EnvelopeRouteCore && e.Lifecycle != sdk.LifecycleQueued && !terminal(e.Lifecycle) {
			active[e.To] = true
		}
	}
	var deliveries []delivery
	var typed []typedOver
	held := map[string]bool{}
	for _, e := range rows {
		if e.Route != sdk.EnvelopeRouteCore || terminal(e.Lifecycle) || m.inFlight[e.To] {
			continue
		}
		s, getErr := m.Get(ctx, e.To)
		if getErr != nil {
			// 隨選（ondemand）秘書的第一句話：這時才把主要 session 開起來，封包留在 queued 等它閒著（SES-1）。
			if platform.CodeOf(getErr) == sdk.ErrorCodeNotFound && e.Lifecycle == sdk.LifecycleQueued {
				if !m.openOnDemand(ctx, e.To) && m.cannotWake(ctx, e.To) {
					m.failNoTarget(ctx, e) // 這台沒有這個對話、也沒有叫得起來的秘書（SES-58）
				}
			}
			continue
		}
		if e.Lifecycle != sdk.LifecycleQueued {
			continue
		}
		if m.agentDisabled(ctx, s.Agent) {
			m.failNoTarget(ctx, e) // 秘書停用了：她的工作對話、房間不再叫起來，也不貼話進去（SES-58）
			continue
		}
		if held[e.To] {
			continue // 同一個對話較早的那句還在退避：後面的先不送，不搶先
		}
		if m.holdRetry(e, now) {
			held[e.To] = true
			continue // 暫時錯誤的退避、確認框開著的間隔還沒到（REQ-391）
		}
		if s.State == sdk.SessionStateStopped {
			if s.Kind == sdk.SessionKindWork && s.Task != "" {
				m.reopenWorkFor(ctx, s, e) // 主人說的話直接開（不看上限，SES-52）；其他的排隊中等名額、停了的接回
			} else if s.Kind != sdk.SessionKindResident {
				// 開不起來時（例：平台說同名的還在跑）MainReopenEvery 內不重試：以前每個派送週期（2 秒）都重開、每次記一行，
				// 一天半洗了 5.8 萬行（DEV-153、SES-74）。主人的新一句來了還沒叫過就馬上叫，跟主要對話同一條規則。
				if at, ok := m.mainReopen[s.Name]; ok && now.Before(at.Add(MainReopenEvery)) && !(isOwnerChat(e) && at.Before(e.CreatedAt)) {
					continue
				}
				m.mainReopen[s.Name] = now
				m.openLater(ctx, &sdk.SessionOpenRequest{Name: s.Name, Agent: s.Agent, Kind: s.Kind}, func(_ Session, err error) {
					if err != nil {
						m.warn("接回對話失敗", err)
					}
				})
			} else if s.Name == s.Agent {
				if m.cannotWake(ctx, s.Agent) {
					m.failNoTarget(ctx, e) // 停用、封存、mode 不是 resident／ondemand 的秘書留下的舊主要對話：不會再開（SES-58）
				} else {
					m.openMain(ctx, s.Agent, now, e) // 隨選的閒置收掉、常駐的不在了（電腦重開）：有話要給她就開起來
				}
			}
			continue
		}
		// 額度用完（REQ-378）：恢復前什麼都不送，也不改送別的對話（同一個帳號也用完了）；過了恢復時間 Tick 把她記回待命再照常送（REQ-379）。
		if s.State == sdk.SessionStateLimited {
			continue
		}
		if t, ok := m.typedOverQuestion(e, s); ok {
			typed = append(typed, t) // 鎖外收掉選擇題；這句照舊排著，她閒下來就送到（SES-70）
		}
		interject := m.interjectable(e, s, rows)
		if !interject && (s.State != sdk.SessionStateIdle || s.Pending != nil || active[e.To]) {
			if err = m.fallback(ctx, e, s, now); err != nil {
				m.warn("改送訊息失敗", err)
			}
			continue
		}
		m.mu.Lock()
		lastOwner := m.entries[s.Name].lastOwner
		m.mu.Unlock()
		if !lastOwner.IsZero() && now.Before(lastOwner.Add(m.dispatchConfig.OwnerQuiet)) && !owner(e.From) && e.Kind != sdk.EnvelopeKindAnswer {
			continue
		}
		engine, engineErr := m.d.Engine.For(s.Account)
		if engineErr != nil {
			m.warn("取得投遞引擎失敗", engineErr)
			continue
		}
		rendered := m.pastedText(ctx, e)
		if strings.TrimSpace(printable(e.Body)) == "" && strings.TrimSpace(e.Body) != "" {
			// 整句只有看不見的字元：清完是空的，貼不出東西，記送不了，不無限重試（SES-68）。
			if err := m.finish(ctx, e, sdk.LifecycleFailed, BlankBodyReason, "", false); err != nil {
				m.warn("記錄送不了失敗", err)
			}
			continue
		}
		// Re-read after resolving the engine and rendering (both may take time).
		latest, getErr := m.Get(ctx, s.Name)
		if getErr != nil || !pasteReady(latest, interject) {
			continue
		}
		opt := bus.TransitionOpt{}
		if interject && latest.State == sdk.SessionStateBusy {
			opt.Reason = interjectReason
		}
		if err = m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleNotified, opt); err != nil {
			m.warn("標記貼上失敗", err)
			continue
		}
		active[e.To] = true
		m.inFlight[e.To] = true
		deliveries = append(deliveries, delivery{e, engine, rendered, interject, m.forcePaste[e.ID], pastedBefore(e)})
	}
	m.dispatchMu.Unlock()
	// 下面每一件都要等引擎：各自在背景做，一位秘書的引擎不回、或每次拖很久才失敗，別位的話照樣馬上送（DEV-159）。
	// 同一個對話同時只有一則在送（inFlight 到 deliver 回來才放）。
	for _, t := range typed {
		t := t
		m.spawn(ctx, func(ctx context.Context) { m.dismissQuestion(ctx, t) })
	}
	for _, d := range deliveries {
		d := d
		if !m.spawn(ctx, func(ctx context.Context) { m.deliver(ctx, d) }) {
			m.undeliver(ctx, d) // 對話服務正在停：這則放回排隊
		}
	}
}

// delivery 是派送器這一輪決定要貼的一則。
type delivery struct {
	e         *sdk.Envelope
	engine    EngineAccess
	text      string
	interject bool
	force     bool // 一定要重新貼（SES-76：等過那句、她沒有接著做）
	resend    bool // 這則以前貼出去過（引擎不記得時才會去對話記錄找它）
}

// optionDeliverer 是引擎交付時多帶兩件事的能力（engine 的轉接層有）；沒有就照平常交。
type optionDeliverer interface {
	DeliverWith(ctx context.Context, session, envelopeID, text string, force, resend bool) (delivered bool, reason string, err error)
}

// undeliver：決定要貼、還沒交給引擎就不貼了（狀態變了、服務要停）：放掉位置，還停在已貼上就放回排隊。
func (m *Module) undeliver(ctx context.Context, d delivery) {
	m.dispatchMu.Lock()
	defer m.dispatchMu.Unlock()
	delete(m.inFlight, d.e.To)
	current, readErr := m.d.Bus.Get(ctx, d.e.ID)
	if readErr == nil && current.Lifecycle == sdk.LifecycleNotified {
		if err := m.d.Bus.Transition(ctx, d.e.ID, sdk.LifecycleQueued, bus.TransitionOpt{}); err != nil {
			m.warn("取消貼上失敗", err)
		}
	}
}

// deliver 把一則話交給引擎貼進對話、等它回來（最多 limits.Deliver），照結果記帳。在背景跑，不拿派送器的鎖等引擎。
func (m *Module) deliver(ctx context.Context, d delivery) {
	var err error
	// Deliver may synchronously report events and ask for authorization. No dispatcher lock is held.
	latest, getErr := m.Get(ctx, d.e.To)
	if getErr != nil || !pasteReady(latest, d.interject) {
		m.dispatchMu.Lock()
		delete(m.inFlight, d.e.To)
		current, readErr := m.d.Bus.Get(ctx, d.e.ID)
		if readErr == nil && current.Lifecycle == sdk.LifecycleNotified {
			if err = m.d.Bus.Transition(ctx, d.e.ID, sdk.LifecycleQueued, bus.TransitionOpt{}); err != nil {
				m.warn("取消貼上失敗", err)
			}
		}
		m.dispatchMu.Unlock()
		return
	}
	call, cancel := context.WithTimeout(ctx, m.limits.Deliver)
	var ok bool
	var reason string
	var deliverErr error
	if with, can := d.engine.(optionDeliverer); can && (d.force || d.resend) {
		ok, reason, deliverErr = with.DeliverWith(call, d.e.To, d.e.ID, d.text, d.force, d.resend)
	} else {
		ok, reason, deliverErr = d.engine.Deliver(call, d.e.To, d.e.ID, d.text)
	}
	// 時限到了才回來的，當成這次沒送到（引擎之後才說送到了也不算；下一次用同一個編號再交，引擎會先查上一次到了沒）。
	late := deliverErr != nil && gaveUp(call, ctx)
	cancel()
	if late {
		ok = false
	}
	m.drainEvents(ctx)
	m.dispatchMu.Lock()
	delete(m.inFlight, d.e.To)
	if ok && deliverErr == nil {
		delete(m.forcePaste, d.e.ID) // 貼進去了才算用掉；沒貼進去（確認框、暫時錯誤）下一次照樣要重貼
	}
	m.dispatchMu.Unlock()
	if ok && deliverErr == nil && reason == sdk.DeliverAlready {
		m.adoptDelivered(ctx, d.e) // 重試：上一次其實到了，只是開始處理的事件沒到（引擎查過，SES-51）
		return
	}
	if ok && deliverErr == nil {
		m.recvMu.Lock()
		delete(m.dialogAt, d.e.To)
		tried := m.tries[d.e.ID]
		delete(m.tries, d.e.ID) // 貼進去了：之後再回到排隊（對話結束、額度）不是「正在重試」
		delete(m.retryAt, d.e.ID)
		m.recvMu.Unlock()
		if tried > PasteLogFirst && m.log != nil {
			m.log.Info("貼上恢復了", "session", d.e.To, "id", d.e.ID, "tries", tried) // 中間的重試沒有每次記，補一行收尾（SES-71）
		}
		// 貼的這段時間她的對話結束了（SESSION_ENDED 不動正在貼的那則）：還停在已貼上就放回排隊，開回來再送。
		if after, err := m.Get(ctx, d.e.To); err == nil && after.State == sdk.SessionStateStopped {
			m.dispatchMu.Lock()
			if current, readErr := m.d.Bus.Get(ctx, d.e.ID); readErr == nil && current.Lifecycle == sdk.LifecycleNotified {
				if err := m.d.Bus.Transition(ctx, d.e.ID, sdk.LifecycleQueued, bus.TransitionOpt{Reason: "她的對話結束了，開回來再送"}); err != nil {
					m.warn("放回排隊失敗", err)
				}
			}
			m.dispatchMu.Unlock()
			m.wakeDelivery()
			return
		}
	}
	if !ok && deliverErr == nil && reason == "dialog" {
		// 她的畫面開著原生確認框（她的對話也放不進去）：不算失敗，放回排隊，原因是「她在等你回答」，確認框收掉就送（REQ-391）。
		m.dialogOpen(ctx, d.e)
		return
	}
	if !ok && deliverErr == nil && (reason == "gone" || reason == "stopped") { // stopped：舊的假引擎與保險
		// 引擎說 session 根本不在（電腦重開後核心還記得「待命」）：記成停了、訊息放回排隊，
		// 下一輪派送器會把她開起來，閒著後再送，不算一次失敗（SES-40、REQ-300）。
		m.sessionGone(ctx, d.e.To)
		if m.log != nil {
			m.log.Info("秘書沒在跑，叫醒後再送", "session", d.e.To, "id", d.e.ID, "reason", reason)
		}
		m.dispatchMu.Lock()
		current, readErr := m.d.Bus.Get(ctx, d.e.ID)
		if readErr == nil && current.Lifecycle == sdk.LifecycleNotified {
			if err := m.d.Bus.Transition(ctx, d.e.ID, sdk.LifecycleQueued, bus.TransitionOpt{Reason: "秘書沒在跑，叫醒後再送"}); err != nil {
				m.warn("放回排隊失敗", err)
			}
		}
		m.dispatchMu.Unlock()
		m.wakeDelivery()
		return
	}
	if !ok || deliverErr != nil {
		m.dispatchMu.Lock()
		current, readErr := m.d.Bus.Get(ctx, d.e.ID)
		if readErr == nil && current.Lifecycle == sdk.LifecycleNotified {
			why := deliverWhy(reason, deliverErr)
			if late {
				why = fmt.Sprintf("秘書那邊沒回應：貼上等了 %d 秒沒有結果，過一下再試", int(m.limits.Deliver/time.Second))
			}
			if retryErr := m.pasteRetry(ctx, current, why); retryErr != nil {
				m.warn("貼上失敗處理失敗", retryErr)
			}
		}
		m.dispatchMu.Unlock()
		if deliverErr != nil && !late { // 逾時的那一行由 pasteRetry 照降頻的規則記（SES-71）
			m.warn("貼上訊息失敗", deliverErr)
		}
	}
}

// pasteReady：送之前再看一次最新狀態。一般只在閒著時貼；主人的中途補話（interject）在忙的時候也可以，
// 但有確認框（Pending）、等主人回答、停了都不貼（確認框開著時 Enter 會被當成「允許」，D-5）。
func pasteReady(s Session, interject bool) bool {
	if s.Pending != nil {
		return false
	}
	return s.State == sdk.SessionStateIdle || (interject && s.State == sdk.SessionStateBusy)
}

// interjectable：主人從看板或 CLI 說的話，秘書在忙也送進去，讓她做完手上那一步就看到（作者 2026-10-30，D-5 改、SES-41）。
// 限制：只限主人的 chat；這個 session 沒有其他「已送出、還沒收到」的訊息（一次只貼一則，收到的比對才不會亂）。
func (m *Module) interjectable(e *sdk.Envelope, s Session, rows []*sdk.Envelope) bool {
	if !owner(e.From) || e.Kind != sdk.EnvelopeKindChat || s.State != sdk.SessionStateBusy || s.Pending != nil {
		return false
	}
	for _, r := range rows {
		if r.To == e.To && r.ID != e.ID && r.Route == sdk.EnvelopeRouteCore && r.Lifecycle == sdk.LifecycleNotified {
			return false
		}
	}
	return true
}

// sessionGone 把引擎說「不在」的 session 記成停了（跟引擎回報 SESSION_ENDED 一樣走事件，狀態、看板、派送器一致）。
func (m *Module) sessionGone(ctx context.Context, name string) {
	if err := m.ReportEvent(ctx, &sdk.EngineEvent{Session: name, Type: sdk.EngineEventTypeSessionEnded, At: m.d.Clock.Now(), Reason: "gone"}); err != nil {
		m.warn("記錄對話已不在失敗", err)
	}
}

// openMain：主要 session 停了又有話要給她。隨選的照 openOnDemand；常駐的（電腦重開、引擎回報不在）馬上開回來，
// 不等 30 秒的巡檢；開不起來時 MainReopenEvery 內不重試。
func (m *Module) openMain(ctx context.Context, id string, now time.Time, e *sdk.Envelope) {
	if m.openOnDemand(ctx, id) {
		return
	}
	enabled, mode := m.agentMode(ctx, id)
	if !enabled || mode != "resident" {
		return
	}
	// 主人的話：這句來了之後還沒叫過就馬上叫（不等 30 秒的節流，SES-52）；叫過了開不起來才照節流。
	if at, ok := m.mainReopen[id]; ok && now.Before(at.Add(MainReopenEvery)) && !(isOwnerChat(e) && at.Before(e.CreatedAt)) {
		return
	}
	m.mainReopen[id] = now
	m.openLater(ctx, &sdk.SessionOpenRequest{Name: id, Agent: id, Kind: sdk.SessionKindResident}, func(_ Session, err error) {
		if err != nil {
			m.warn("有話要給她，開常駐對話失敗", err)
		}
	})
}

func (m *Module) fallback(ctx context.Context, e *sdk.Envelope, s Session, now time.Time) error {
	if s.Kind == sdk.SessionKindClone || s.Kind == sdk.SessionKindTemp || s.Kind == sdk.SessionKindWork || m.rerouting[e.ID] {
		return nil
	}
	name := ""
	kind := sdk.SessionKindWork // 秘書傳話排太久：開同一位秘書的工作 session（clone 併進 work，P7-5、D-33）
	if e.Kind == sdk.EnvelopeKindSay && !now.Before(e.CreatedAt.Add(m.dispatchConfig.SayCloneAfter)) {
		for i := 1; i <= 2; i++ {
			candidate := fmt.Sprintf("%s-c%d", s.Agent, i)
			state, exists := m.State(candidate)
			if exists && state == sdk.SessionStateIdle && !m.inFlight[candidate] {
				rows, err := m.d.Bus.List(ctx, candidate, sdk.LifecycleQueued, sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleWorking, sdk.LifecycleBlocked)
				if err != nil {
					return err
				}
				if len(rows) == 0 {
					name = candidate
					break
				}
			}
			if !exists || state == sdk.SessionStateStopped {
				name = candidate
				break
			}
		}
	} else if id := jobID(e); id != "" && !now.Before(e.CreatedAt.Add(m.dispatchConfig.RoutineTempAfter)) {
		sessions, err := m.List(ctx, "", nil)
		if err != nil {
			return err
		}
		count := 0
		for _, row := range sessions {
			if row.Kind == sdk.SessionKindTemp && row.State != sdk.SessionStateStopped {
				count++
			}
		}
		if count < 4 {
			name = s.Agent + "-job-" + strings.TrimPrefix(id, s.Agent+"/")
			kind = sdk.SessionKindTemp
		}
	}
	if name == "" {
		return nil
	}
	// 開那個對話要等引擎：放到背景，開好了再把這則改送過去；開的這段時間這則留在原地排著，不再替它另開一個。
	id, from := e.ID, e.To
	m.rerouting[id] = true
	opening := m.openLater(ctx, &sdk.SessionOpenRequest{Name: name, Agent: s.Agent, Kind: kind}, func(_ Session, err error) {
		m.dispatchMu.Lock()
		delete(m.rerouting, id)
		if err == nil {
			// 等的這段時間這則可能已經送進原本的對話了：還在原地排著才改送
			if current, readErr := m.d.Bus.Get(ctx, id); readErr == nil && current.Lifecycle == sdk.LifecycleQueued && current.To == from {
				err = m.d.Bus.Reroute(ctx, id, name)
			}
		}
		m.dispatchMu.Unlock()
		if err != nil {
			m.warn("改送訊息失敗", err)
		}
		m.wakeDelivery()
	})
	if opening == openBusy {
		delete(m.rerouting, id) // 那個對話正在開關：下一輪再看
	}
	return nil
}

// 主人打字蓋過選擇題（SES-70；作者 2026-11-08：平台自己的畫面打字就能消掉彈出的選項，看板也要）：
// 她在等主人選（Pending 是 question），主人沒選、直接在看板打了一句話——跟在平台自己的畫面上打字一樣，
// 那一題就當沒答收掉，告訴她「他打的話緊接著到」，她那一步做完閒下來，這句照舊送進去。權限確認不收（照舊排著等允許／拒絕）。
const TypedOverQuestion = "主人沒有選這一題，直接打字回覆了；他打的話緊接著送到，照他的話做，不用再問這一題。"

type typedOver struct {
	session, pending, by string
}

// typedOverQuestion 在 dispatchMu 底下呼叫：這句是不是主人打字蓋過正在等的選擇題（同一題只收一次）。
func (m *Module) typedOverQuestion(e *sdk.Envelope, s Session) (typedOver, bool) {
	p := s.Pending
	if !owner(e.From) || e.Kind != sdk.EnvelopeKindChat || p == nil || p.Kind != sdk.PendingKindQuestion || m.typedOver[s.Name] == p.ID {
		return typedOver{}, false
	}
	if e.CreatedAt.Before(p.Since) {
		return typedOver{}, false // 題目出現前就在排隊的話：主人送的時候還沒看到題目，不算蓋過
	}
	m.typedOver[s.Name] = p.ID
	return typedOver{session: s.Name, pending: p.ID, by: e.From}, true
}

// dismissQuestion：還在等核心回答的，回「沒答」並附上 TypedOverQuestion；已經交回她畫面上的（native），
// 只有一題時由引擎把 TypedOverQuestion 當成自己打的答案填進去（引擎不會在畫面上按取消）；多題的留著，照舊等主人答。
func (m *Module) dismissQuestion(ctx context.Context, t typedOver) {
	m.mu.Lock()
	e, a := m.entries[t.session], m.approvals[t.session]
	if e == nil || a == nil || e.session.Pending == nil || e.session.Pending.ID != t.pending || a.id != t.pending {
		m.mu.Unlock()
		return
	}
	// 過了期限、Tick 還沒交回畫面的也照畫面上的做（Resolve 會先交回畫面再請引擎代填）
	native, questions := a.native || !m.d.Clock.Now().Before(a.deadline), a.questions
	m.mu.Unlock()
	d := &sdk.Decision{Allow: false, Message: TypedOverQuestion}
	if native {
		if len(questions) != 1 {
			return
		}
		d = &sdk.Decision{Allow: true, Answers: map[string]string{questions[0].Text: TypedOverQuestion}}
	}
	// UNAVAILABLE＝畫面上那題引擎不代填（有分頁、游標不在選項上）或一時按不到：題目留著、不再試，主人照舊在卡片上答。
	if _, err := m.Resolve(ctx, t.session, t.pending, d, t.by); err != nil && platform.CodeOf(err) != sdk.ErrorCodeUnavailable {
		m.warn("收掉打字蓋過的選擇題失敗", err)
	}
}
