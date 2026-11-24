package session

import (
	"context"
	"reflect"
	"sort"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func (m *Module) Chat(ctx context.Context, name, since string, follow bool) (<-chan ChatMessage, error) {
	s, err := m.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	e, err := m.d.Engine.For(s.Account)
	if err != nil {
		return nil, err
	}
	return e.Transcript(ctx, name, since, follow)
}

// refreshUnread 重讀對話記錄有變的對話、更新未讀。讀對話記錄要等引擎，所以每個對話各自在背景讀、各自有時限
// （limits.Transcript），這裡不等：一位的引擎不回，別位的未讀照常更新（DEV-159）。讀不到的沿用上次的值，
// 留著「要重讀」的記號，下一次再讀。同一個對話同時只有一個在讀。
func (m *Module) refreshUnread(ctx context.Context) {
	m.mu.Lock()
	names := []string{}
	for name := range m.transcriptDirty {
		if m.reading[name] {
			continue // 上一次還在讀（引擎沒回）：等它結束，記號留著
		}
		delete(m.transcriptDirty, name)
		if e := m.entries[name]; e == nil || e.session.Account == "" { // 還沒開過、沒有帳號（排隊中）的沒有對話記錄可讀
			continue
		}
		m.reading[name] = true
		names = append(names, name)
	}
	m.mu.Unlock()
	for _, name := range names {
		name := name
		if !m.spawn(ctx, func(ctx context.Context) {
			// 讀的時候對話又變了：接著重讀，不等下一輪（最多 unreadRereads 次，一直在變的留著記號下次再讀）。
			for i := 0; i < unreadRereads && m.readUnread(ctx, name); i++ {
				m.mu.Lock()
				again := m.transcriptDirty[name] && !m.reading[name]
				if again {
					delete(m.transcriptDirty, name)
					m.reading[name] = true
				}
				m.mu.Unlock()
				if !again {
					break
				}
			}
		}) {
			m.mu.Lock()
			delete(m.reading, name)
			m.transcriptDirty[name] = true
			m.mu.Unlock()
		}
	}
}

// unreadRereads：讀未讀的時候對話又變了，同一次最多接著重讀幾次。
const unreadRereads = 3

// readUnread 讀一個對話已讀位置之後的訊息、記成未讀。沒讀到（引擎出錯、時限到了）沿用上次的值、留著記號等下一次。
// 回 true＝讀的時候對話又變了（或有人又標了要重讀），值得馬上再讀一次。
func (m *Module) readUnread(ctx context.Context, name string) bool {
	m.mu.Lock()
	e := m.entries[name]
	if e == nil {
		delete(m.reading, name)
		m.mu.Unlock()
		return false
	}
	seen, version := e.seen, e.version
	m.mu.Unlock()
	call, cancel := context.WithTimeout(ctx, m.limits.Transcript)
	defer cancel()
	all, err := m.readChat(call, name, seen)
	if err != nil {
		if gaveUp(call, ctx) {
			err = silent("讀對話記錄", m.limits.Transcript, "未讀數先照上次的，下一次再讀。")
		}
		m.mu.Lock()
		delete(m.reading, name)
		m.transcriptDirty[name] = true
		m.readFails[name]++
		n := m.readFails[name]
		m.mu.Unlock()
		// 她一直讀不到時不要每次一行：跟貼上一樣降頻（SES-71 的 pasteLogDue）
		if m.log != nil && pasteLogDue(n) {
			m.log.Warn("讀取對話記錄失敗", "session", name, "error", err.Error(), "try", n)
		}
		return false
	}
	messages := []*sdk.ChatMessage{}
	for _, msg := range all {
		if msg.Kind == sdk.ChatKindAssistant {
			messages = append(messages, msg)
		}
	}
	m.mu.Lock()
	delete(m.reading, name)
	delete(m.readFails, name)
	e = m.entries[name]
	changed := e != nil && (e.version != version || e.seen != seen)
	if changed {
		m.transcriptDirty[name] = true
	} else if e != nil {
		e.messages = messages
		if len(messages) > 0 {
			e.lastAssistant = messages[len(messages)-1]
		}
	}
	again := m.transcriptDirty[name]
	m.mu.Unlock()
	return again
}

// transcriptReader 是引擎「整段讀回對話記錄、讀不到就回錯誤」的能力（engine 的轉接層有）。
// Chat 給的 channel 在引擎出錯時只是關掉，分不出「讀完了」與「沒讀到」。
type transcriptReader interface {
	ReadTranscript(ctx context.Context, session, since string) ([]*sdk.ChatMessage, error)
}

// readChat 把一個對話 since 之後的訊息整段讀回來（不跟著等新的）；引擎出錯、ctx 到期都回錯誤。
func (m *Module) readChat(ctx context.Context, name, since string) ([]*sdk.ChatMessage, error) {
	s, err := m.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	engine, err := m.d.Engine.For(s.Account)
	if err != nil {
		return nil, err
	}
	if r, ok := engine.(transcriptReader); ok {
		all, err := r.ReadTranscript(ctx, name, since)
		if err != nil {
			return nil, err
		}
		out := make([]*sdk.ChatMessage, 0, len(all))
		for _, msg := range all {
			if msg != nil {
				copy := *msg
				out = append(out, &copy)
			}
		}
		return out, nil
	}
	ch, err := engine.Transcript(ctx, name, since, false)
	if err != nil {
		return nil, err
	}
	out := []*sdk.ChatMessage{}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				if err := ctx.Err(); err != nil {
					return nil, err // 到期與關閉同時發生：算沒讀到
				}
				return out, nil
			}
			if msg != nil {
				copy := *msg
				out = append(out, &copy)
			}
		}
	}
}
func hasReason(list []sdk.WaitingReason, r sdk.WaitingReason) bool {
	for _, x := range list {
		if x == r {
			return true
		}
	}
	return false
}
func (m *Module) summary(e *entry) *sdk.ThreadSummary {
	s := e.session
	t := &sdk.ThreadSummary{Name: s.Name, Agent: s.Agent, Title: e.title, BackgroundTasks: e.background, SeenUuid: e.seen, StatusSince: s.StateSince, Unread: int32(len(e.messages)), Kind: s.Kind, Task: s.Task, LastTurnEndAt: s.LastTurnEndAt}
	if e.lastAssistant != nil {
		last := e.lastAssistant
		t.LastPreview = clipped(last.Text)
		t.LastAssistantAt = last.Ts
	}
	if s.Pending != nil {
		if s.Pending.Kind == sdk.PendingKindPermission {
			t.Waiting = append(t.Waiting, sdk.WaitingReasonPermission)
		} else {
			t.Waiting = append(t.Waiting, sdk.WaitingReasonQuestion)
		}
	}
	for _, room := range m.notices {
		if room == s.Name {
			t.Waiting = append(t.Waiting, sdk.WaitingReasonApproval)
			break
		}
	}
	if e.workAsk != "" && !hasReason(t.Waiting, sdk.WaitingReasonApproval) {
		t.Waiting = append(t.Waiting, sdk.WaitingReasonApproval) // 保留工作名額詢問的等待狀態
	}
	for _, w := range m.roomWaits {
		if w.room == s.Name && !hasReason(t.Waiting, w.reason) {
			t.Waiting = append(t.Waiting, w.reason)
		}
	}
	// 排隊等名額（D-33）：說明在等什麼，但不是在等主人——討論串是 QUEUED、灰色、不算進 waiting_user（S5 D6）。
	slotQueued := !s.QueuedAt.IsZero() && len(t.Waiting) == 0
	if slotQueued {
		t.Waiting = append(t.Waiting, sdk.WaitingReasonWorkQueue)
	}
	working := false
	for _, env := range m.envelopes {
		if env.To != s.Name {
			continue
		}
		switch env.Lifecycle {
		case sdk.LifecycleQueued, sdk.LifecycleNotified:
			t.Queued++
		case sdk.LifecycleReceived, sdk.LifecycleWorking, sdk.LifecycleBlocked:
			working = true
		}
	}
	switch {
	case len(t.Waiting) > 0 && !slotQueued:
		t.Status = sdk.ThreadStatusWaitingUser
	case s.State == sdk.SessionStateLimited:
		t.Status = sdk.ThreadStatusLimited // 額度用完：灰色「額度用完，HH:MM 恢復」，不是卡住（REQ-378）
		t.LimitedUntil = s.LimitedUntil
	case s.State == sdk.SessionStateBusy || working:
		t.Status = sdk.ThreadStatusWorking
		t.Activity = e.activity
		if t.Activity == "" {
			t.Activity = "思考中"
		}
	case t.Queued > 0 || slotQueued:
		t.Status = sdk.ThreadStatusQueued
	case s.State == sdk.SessionStateStopped && openTaskWork(e):
		t.Status = sdk.ThreadStatusStopped // 單還沒結、先收起來：寫「已收起」，不寫「完成」免得看成單做完了（未讀數照給，DEV-147）
	case e.ended && t.Unread > 0:
		t.Status = sdk.ThreadStatusDone
	case s.State == sdk.SessionStateStarting:
		t.Status = sdk.ThreadStatusStarting
	case s.State == sdk.SessionStateStopped:
		t.Status = sdk.ThreadStatusStopped
	default:
		t.Status = sdk.ThreadStatusIdle
	}
	if e.status != t.Status {
		e.status = t.Status
		e.statusSince = m.d.Clock.Now()
	}
	t.StatusSince = e.statusSince
	return t
}
func (m *Module) threads(agent string, includeStopped bool) ([]ThreadSummary, int) {
	out := []ThreadSummary{}
	waiting := 0
	for _, e := range m.entries {
		if agent != "" && agent != e.session.Agent {
			continue
		}
		if !includeStopped && e.session.State == sdk.SessionStateStopped && e.session.QueuedAt.IsZero() && !openTaskWork(e) {
			continue
		}
		s := m.summary(e)
		out = append(out, s)
		if s.Status == sdk.ThreadStatusWaitingUser {
			waiting++
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, waiting
}
func (m *Module) Threads(ctx context.Context, agent string, includeStopped bool) ([]ThreadSummary, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, 0, err
	}
	out, n := m.threads(agent, includeStopped)
	return out, n, nil
}
func (m *Module) MarkSeen(ctx context.Context, name, uuid, by string) (ThreadSummary, error) {
	// Resolve arbitrary cursors against the same engine-owned transcript used by Chat.
	call, cancel := context.WithTimeout(ctx, m.limits.Transcript)
	defer cancel()
	all, err := m.readChat(call, name, "")
	if err != nil {
		if gaveUp(call, ctx) {
			return nil, silent("讀對話記錄", m.limits.Transcript, "已讀位置沒有記到，過一下再試。")
		}
		return nil, err
	}
	messages := []*sdk.ChatMessage{}
	found := uuid == ""
	last := ""
	for _, msg := range all {
		last = msg.Uuid
		if msg.Uuid == uuid {
			found = true
			messages = nil
			continue
		}
		if found && msg.Kind == sdk.ChatKindAssistant {
			messages = append(messages, msg)
		}
	}
	if !found {
		return nil, platform.Invalid("uuid", "找不到這個已讀位置")
	}
	if uuid == "" {
		uuid = last
		messages = nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[name]
	if e == nil {
		return nil, platform.NotFound("找不到這個對話")
	}
	e.seen = uuid
	e.seenAt = m.d.Clock.Now()
	e.messages = messages
	e.version++
	m.transcriptDirty[name] = true
	if err = m.save(e); err != nil {
		return nil, err
	}
	return m.summary(e), nil
}
func (m *Module) Watch(ctx context.Context, agent string) (<-chan *sdk.SessionWatchResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	w := &watch{ctx: ctx, agent: agent, ch: make(chan *sdk.SessionWatchResponse, len(m.entries)+64)}
	for _, e := range m.entries {
		if agent == "" || agent == e.session.Agent {
			w.ch <- &sdk.SessionWatchResponse{Session: copySession(e.session)}
		}
	}
	m.watches[w] = struct{}{}
	return w.ch, nil
}
func (m *Module) WatchThreads(ctx context.Context, agent string) (<-chan *sdk.SessionWatchThreadsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	w := &threadWatch{ctx: ctx, agent: agent, ch: make(chan *sdk.SessionWatchThreadsResponse, len(m.entries)+64), last: map[string]sdk.ThreadSummary{}}
	rows, n := m.threads(agent, true)
	for _, r := range rows {
		w.last[r.Name] = *r
		w.ch <- &sdk.SessionWatchThreadsResponse{Thread: r, WaitingUser: int32(n)}
	}
	m.threadWatches[w] = struct{}{}
	return w.ch, nil
}
func (m *Module) flushThreads(now time.Time) {
	if now.Truncate(time.Second).Equal(m.lastFlush) {
		return
	}
	m.lastFlush = now.Truncate(time.Second)
	for w := range m.threadWatches {
		if w.ctx.Err() != nil {
			close(w.ch)
			delete(m.threadWatches, w)
			continue
		}
		rows, n := m.threads(w.agent, true)
		for _, r := range rows {
			if old, ok := w.last[r.Name]; ok && reflect.DeepEqual(old, *r) {
				continue
			}
			select {
			case w.ch <- &sdk.SessionWatchThreadsResponse{Thread: r, WaitingUser: int32(n)}:
				w.last[r.Name] = *r
			default:
				close(w.ch)
				delete(m.threadWatches, w)
			}
			if _, ok := m.threadWatches[w]; !ok {
				break
			}
		}
	}
}

// roomWait 是業務模組登記的一筆「這個對話在等某件事」：討論串列表把 reason 加進那個對話的 Waiting（黃點）。
type roomWait struct {
	room   string
	reason sdk.WaitingReason
}

// SetRoomWait 讓業務模組登記或取消「這個對話在等某件事」（SES-27 的等待原因）：key 是業務模組自己的編號（例：待辦 R-12），
// room 給空字串就取消。session 不知道是什麼事，只照 reason 顯示；業務模組在自己 Start 時把現況整批登記一次。
func (m *Module) SetRoomWait(key, room string, reason sdk.WaitingReason) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if room == "" || reason == "" {
		delete(m.roomWaits, key)
		return
	}
	m.roomWaits[key] = roomWait{room: room, reason: reason}
}
