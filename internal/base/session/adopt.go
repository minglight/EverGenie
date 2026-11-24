package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/sdk"
)

// 引擎說「這句已經在她的對話裡」而替它開的一輪有期限（SES-76，DEV-162）：平台沒有接著做時，開始與結束事件都不會來，
// 那一則會永遠是收到、擋住同一個對話後面的話。「計時不改狀態」（SES-4）在這裡不成立（D-69）。

const (
	// AdoptSettleAfter：替「已經在她的對話裡」的那句開的一輪，她待命這麼久還沒看到開始事件就作廢。
	AdoptSettleAfter = 3 * time.Minute
	// VoidedKeep：作廢之後這麼久內，沒有對到封包的下一輪還算是那句的（平台晚一點才接著做）；過了就當主人自己打的。
	VoidedKeep = 30 * time.Minute

	adoptedReason = "她的對話裡已經有這句"
	// adoptExpiredReason：到期，退回排隊、請引擎一定重新貼一次時寫的原因（歷程裡有這一筆＝這一次補送已經自救過）。
	adoptExpiredReason = "等了 3 分鐘她沒有接著做，重新送一次"
	// adoptGaveUpReason：自救過一次、引擎還是說已經在了、對話記錄裡也看不到她回話：記中斷。不斷言她沒做。
	adoptGaveUpReason = "等不到她開始，不確定有沒有做"
)

// voidedTurn 是作廢掉的那一輪是誰觸發的（SES-76）。
type voidedTurn struct {
	id, origin, from string
	at               time.Time
}

// adoptedSince：這則現在是「引擎說已經在她的對話裡」記的收到，回那一筆的時間；不是就回 false。
// 看封包的歷程、不看記憶體裡的那一輪：核心重開、升級前存的狀態檔（turn 沒有 adopted）都一樣判斷得出來。
func adoptedSince(e *sdk.Envelope) (time.Time, bool) {
	if e.Route != sdk.EnvelopeRouteCore || e.Lifecycle != sdk.LifecycleReceived || len(e.History) == 0 {
		return time.Time{}, false
	}
	last := e.History[len(e.History)-1]
	if last.State != sdk.LifecycleReceived || last.Detail != adoptedReason {
		return time.Time{}, false
	}
	return last.At, true
}

// dialogHoldReason：引擎說她的畫面開著確認框，那一則先放回排隊（receive.go）。
const dialogHoldReason = "她的畫面開著確認框，等你回答後再送"

// rescues：這則在最近這一次補送裡，核心已經自救（放回排隊重送）過幾次——SES-76 的到期重貼與 SES-77 的「貼上沒開始」重送都算。
// 往回數到因為別的原因退回排隊（又碰到額度用完、對話結束了…）為止：那之前是另一次補送。
// 畫面開著確認框的退回不算別的原因（只是晚一點再試）。
func rescues(e *sdk.Envelope) int {
	n := 0
	for i := len(e.History) - 1; i >= 0; i-- {
		step := e.History[i]
		if step.State != sdk.LifecycleQueued || step.Detail == dialogHoldReason {
			continue
		}
		if step.Detail != adoptExpiredReason && step.Detail != stuckRetryReason {
			break
		}
		n++
	}
	return n
}

// settled：這個對話現在待命、沒有在等主人回答、沒有正在交給引擎的話、也沒有還沒處理的事件，回她從什麼時候起待命、是哪位秘書的。
// 忙著、等確認、額度用完、停了都回 false（那些各有自己的規則）。呼叫時持有 dispatchMu。
func (m *Module) settled(name string) (since time.Time, agent string, ok bool) {
	if m.inFlight[name] {
		return time.Time{}, "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[name]
	if e == nil || e.session.State != sdk.SessionStateIdle || e.session.Pending != nil {
		return time.Time{}, "", false
	}
	for _, ev := range m.eventQueue {
		if ev != nil && ev.Session == name {
			return time.Time{}, "", false
		}
	}
	since = e.session.StateSince
	if m.bootAt.After(since) {
		since = m.bootAt // 核心停著的那一段不算：重開後第一輪不能因為「已經待命很久」就馬上作廢或報卡住
	}
	return since, e.session.Agent, true
}

// expireAdopted（dispatch 每一輪，持有 dispatchMu）：替「已經在她的對話裡」開的一輪到期了就作廢（SES-76）。
// 第一次：清掉掛著的那一輪、那句退回排隊，照平常的規則重送（引擎再判斷一次要不要貼）；
// 重送後又到期：記中斷（寄件的秘書照 SES-20 收到回音），不無限循環。之後排著的話照常送。
func (m *Module) expireAdopted(ctx context.Context, rows []*sdk.Envelope, now time.Time) (moved bool) {
	if len(m.forcePaste) > 0 {
		// 「一定重貼」的記號只留給還在等著貼的：那一則已經不在排隊（送不了、主人取消…）就丟掉。
		waiting := map[string]bool{}
		for _, e := range rows {
			if e.Lifecycle == sdk.LifecycleQueued || e.Lifecycle == sdk.LifecycleNotified {
				waiting[e.ID] = true
			}
		}
		for id := range m.forcePaste {
			if !waiting[id] {
				delete(m.forcePaste, id)
			}
		}
	}
	for _, e := range rows {
		at, ok := adoptedSince(e)
		if !ok {
			continue
		}
		since, _, ok := m.settled(e.To)
		if !ok {
			continue
		}
		if since.After(at) {
			at = since // 期限只在她待命時算：中間忙過、等過確認、額度用完過，從回到待命起算
		}
		if now.Before(at.Add(AdoptSettleAfter)) {
			continue
		}
		if m.givingUp[e.ID] {
			continue // 正在查對話記錄，等它收尾
		}
		moved = true
		m.markRescued(e.To, now) // 這也是一次自救：之後還是不通才算卡住（SES-77）
		m.dropTurnEnvelope(e.To, e.ID, now)
		if rescues(e) > 0 {
			// 這一次補送已經自救過（重貼過、或貼上沒開始重送過），引擎還是說已經在她的對話裡：不再貼。
			// 查對話記錄她有沒有回這句：有就記已回覆，沒有記中斷。
			if m.log != nil {
				m.log.Warn("已經在她對話裡的那句重送後還是沒有開始，查對話記錄收尾", "session", e.To, "id", e.ID)
			}
			m.giveUp(ctx, e, adoptGaveUpReason)
			continue
		}
		if m.log != nil {
			m.log.Warn("已經在她對話裡的那句她沒有接著做，作廢這一輪、重新貼一次", "session", e.To, "id", e.ID)
		}
		if m.forcePaste == nil {
			m.forcePaste = map[string]bool{}
		}
		m.forcePaste[e.ID] = true // 這次重送要真的貼：額度還沒回來平台會再說一次，照額度的規則等，不會被記成沒做完
		if err := m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleQueued, bus.TransitionOpt{Reason: adoptExpiredReason}); err != nil {
			m.warn("放回排隊失敗", err)
		}
	}
	return moved
}

// dropTurnEnvelope：把這則從掛著的那一輪拿掉（記憶體與狀態檔）；它是這一輪的主要那則就整輪作廢，
// 之後的話不再被當成這一輪的中途補話。呼叫時持有 dispatchMu。
func (m *Module) dropTurnEnvelope(name, id string, now time.Time) {
	t := m.turns[name]
	if t == nil {
		return
	}
	kept := t.carried[:0:0]
	for _, c := range t.carried {
		if c != id {
			kept = append(kept, c)
		}
	}
	t.carried = kept
	if t.Envelope != id {
		return
	}
	t.Envelope = ""
	if t.Adopted {
		// 之後平台才接著做那句：那一輪還是原寄件人的，不算主人打字（origin.go）。記在對話上，別的話的一輪不會蓋掉。
		if m.voided == nil {
			m.voided = map[string]voidedTurn{}
		}
		m.voided[name] = voidedTurn{id: id, origin: t.Origin, from: t.From, at: now}
	}
	t.Adopted = false
	if t.ended == nil {
		t.ended = &sdk.EngineEvent{Session: name, Type: sdk.EngineEventTypeIdleHint, At: now}
	}
	delete(m.pendingPeers, t)
	m.storeTurn(name, t.turn, false)
}

// clearAdopted：這一輪收尾了（結束、停了、額度用完）：不再是「等平台接著做」的一輪，存檔也拿掉。
// 不然核心重開讀回來，主人之後直接打的一輪會被當成那句的。呼叫時持有 dispatchMu。
func (m *Module) clearAdopted(name string, t *dispatchTurn) {
	if t == nil || !t.Adopted {
		return
	}
	t.Adopted = false
	m.storeTurn(name, t.turn, false)
}

// giveUp：不再等這一則了。記中斷之前先在背景查對話記錄（要等引擎，不能拿著派送器的鎖）：這句之後她有回話就記已回覆
// （多半是開始／結束事件丟了，她其實做完了），沒有才記中斷、原因是 reason。查的期間這一則照舊擋著後面的話。
// 呼叫時持有 dispatchMu。
func (m *Module) giveUp(ctx context.Context, e *sdk.Envelope, reason string) {
	if m.givingUp == nil {
		m.givingUp = map[string]bool{}
	}
	if m.givingUp[e.ID] {
		return
	}
	m.givingUp[e.ID] = true
	id, name, body := e.ID, e.To, e.Body
	started := m.spawn(ctx, func(bg context.Context) {
		last, answered := "", false
		func() {
			defer func() {
				if r := recover(); r != nil {
					m.warn("查對話記錄時出錯，當成沒看到她回話", fmt.Errorf("%v", r))
				}
			}()
			call, cancel := context.WithTimeout(bg, m.limits.Deliver)
			defer cancel()
			last, answered = m.answeredInChat(call, name, body)
		}()
		var err error
		func() {
			m.dispatchMu.Lock()
			defer m.dispatchMu.Unlock()
			defer delete(m.givingUp, id)
			cur, getErr := m.d.Bus.Get(bg, id)
			live := getErr == nil && (cur.Lifecycle == sdk.LifecycleReceived || cur.Lifecycle == sdk.LifecycleWorking || cur.Lifecycle == sdk.LifecycleBlocked)
			switch {
			case live && answered:
				if cur.Lifecycle != sdk.LifecycleWorking {
					err = m.d.Bus.Transition(bg, id, sdk.LifecycleWorking, bus.TransitionOpt{})
				}
				if err == nil {
					err = m.finish(bg, cur, sdk.LifecycleReplied, "", last, false)
				}
			case live:
				err = m.finish(bg, cur, sdk.LifecycleInterrupted, reason, "", false)
			}
		}()
		if err != nil {
			m.warn("收尾等不到的那一則失敗", err)
		}
		select {
		case m.deliveryWake <- struct{}{}:
		default:
		}
	})
	if !started {
		delete(m.givingUp, id)
	}
}

// answeredInChat：對話記錄裡，這句（看開頭）最後一次出現之後、下一句使用者的話之前，她有沒有說話；有就回她最後那句。
// 讀不到對話記錄算沒有。
func (m *Module) answeredInChat(ctx context.Context, name, body string) (last string, answered bool) {
	probe := firstLine(body, 40)
	if probe == "" {
		return "", false
	}
	msgs, err := m.readChat(ctx, name, "")
	if err != nil {
		return "", false
	}
	at := -1
	for i, msg := range msgs {
		if msg.Kind == sdk.ChatKindUser && strings.Contains(msg.Text, probe) {
			at = i
		}
	}
	if at < 0 {
		return "", false
	}
	// 要是「做完了」的樣子：那句之後最後一則是她說的話（不是工具呼叫、工具結果、平台的錯誤訊息如額度用完那一行）。
	// 做到一半停住的（最後停在工具上）不算。
	for _, msg := range msgs[at+1:] {
		if msg.Kind == sdk.ChatKindUser {
			break
		}
		if msg.Draft || msg.Kind == sdk.ChatKindNotice {
			continue
		}
		if msg.Kind == sdk.ChatKindAssistant && !msg.IsError && strings.TrimSpace(msg.Text) != "" {
			last, answered = msg.Text, true
			continue
		}
		last, answered = "", false
	}
	return last, answered
}

// firstLine：第一個非空白行的前 n 個字（拿來在對話記錄裡找這句）。
func firstLine(text string, n int) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if r := []rune(line); len(r) > n {
				return string(r[:n])
			}
			return line
		}
	}
	return ""
}
