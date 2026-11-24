package session

import (
	"context"
	"net/url"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 通知跟狀態只有一份正本（DEV-49 S2、SES-47）：
//   - 綁在對話上的「等你回覆」（選擇題、權限確認＝session 的 Pending）不建訊息頁的通知，只推播（notify 的 Nudge 帶 key），
//     點下去開那個對話；那件事不在了（答了、在終端機／另一台／Remote Control 答了、逾時、session 關了或重開）就收回提醒（站內收掉、不補推，手機上的等看板打開時收，NTF-36）。
//   - 工作 session 的名額詢問：排隊被取消（單離開 doing、主人手動關、她已經在跑）的當下就收掉（notify.Retire，
//     理由「已經不用回了」，S5-2；取代每 3 秒的對帳）。
// 判斷全部只看記憶體（entries），所以跟 Tick 一起每秒跑；另外每次 session 有變化（announce）就叫醒一次。

// waitKeyPrefix：Pending 推播的 key 是 pending:<確認編號>（換了一題就是另一則）。
const waitKeyPrefix = "pending:"

type waitNudger interface {
	Nudge(context.Context, *sdk.NotifyNudgeRequest) (*sdk.Nudge, error)
}

type noticeRetirer interface {
	Retire(ctx context.Context, ids []string, reason string) error
}

// pushedWait 是已經推出去（交給 notify）的一則對話等待。
type pushedWait struct {
	agent, key string
}

// waitPush 是這一輪要送出或收回的一則。
type waitPush struct {
	session string
	req     *sdk.NotifyNudgeRequest
}

// wakeWaits 讓 syncWaits 盡快跑一次（announce 呼叫，持有 m.mu；不阻塞）。
func (m *Module) wakeWaits() {
	if m.waitWake == nil {
		return
	}
	select {
	case m.waitWake <- struct{}{}:
	default:
	}
}

// waitLoop：session 有變化就對一次（Tick 每秒也會對）。
func (m *Module) waitLoop(ctx context.Context) {
	defer m.wg.Done()
	defer m.recoverWorker()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.waitWake:
			m.syncWaits(ctx, m.d.Clock.Now())
		}
	}
}

// syncWaits 把「現在 session 在等什麼」跟「推出去的」對齊：新的 Pending 推播、不在了的收回；
// 排隊被取消的名額詢問收掉。呼叫 notify 都在放開 m.mu 之後。
func (m *Module) syncWaits(ctx context.Context, _ time.Time) {
	m.waitMu.Lock()
	defer m.waitMu.Unlock()
	m.mu.Lock()
	if m.ready(ctx) != nil {
		m.mu.Unlock()
		return
	}
	nudger, _ := m.d.Notify.(waitNudger)
	var retracts []waitPush
	type newWait struct {
		name, agent string
		pending     sdk.Pending
	}
	var fresh []newWait
	want := map[string]pushedWait{}
	for name, e := range m.entries {
		p := e.session.Pending
		if p == nil || p.ID == "" {
			continue
		}
		w := pushedWait{agent: e.session.Agent, key: waitKeyPrefix + p.ID}
		want[name] = w
		if m.waitPushed[name] == w {
			continue
		}
		fresh = append(fresh, newWait{name, e.session.Agent, *p})
	}
	for name, w := range m.waitPushed {
		if want[name] != w {
			retracts = append(retracts, waitPush{name, &sdk.NotifyNudgeRequest{Agent: w.agent, Session: name, Key: w.key, Resolve: true}})
		}
	}
	m.mu.Unlock()

	sends := make([]waitPush, 0, len(fresh))
	for _, w := range fresh {
		sends = append(sends, waitPush{w.name, m.waitRequest(ctx, w.name, w.agent, &w.pending)})
	}
	for _, r := range retracts {
		if nudger != nil {
			if _, err := nudger.Nudge(ctx, r.req); err != nil {
				m.warn("收回等你回覆的提醒失敗", err)
				continue // 下一輪再試
			}
		}
		m.mu.Lock()
		if m.waitPushed[r.session].key == r.req.Key {
			delete(m.waitPushed, r.session)
		}
		m.mu.Unlock()
	}
	for _, s := range sends {
		if nudger != nil {
			if _, err := nudger.Nudge(ctx, s.req); err != nil {
				m.warn("送出等你回覆的推播失敗", err)
				continue
			}
		}
		m.mu.Lock()
		m.waitPushed[s.session] = pushedWait{agent: s.req.Agent, key: s.req.Key}
		m.mu.Unlock()
	}
	m.retireAsks(ctx)
}

func (m *Module) waitRequest(ctx context.Context, name, agent string, p *sdk.Pending) *sdk.NotifyNudgeRequest {
	who := agent
	if m.d.Agents != nil {
		who = m.person(ctx, agent)
	}
	req := &sdk.NotifyNudgeRequest{Agent: agent, Session: name, Key: waitKeyPrefix + p.ID, URL: "/chat/" + url.PathEscape(name)}
	if p.Kind == sdk.PendingKindPermission {
		req.Title = who + " 要你同意"
		req.Context = p.Summary
		if req.Context == "" {
			req.Context = p.Tool
		}
		req.Todo = "允許或拒絕"
		return req
	}
	req.Title = who + " 要你回答"
	if len(p.Questions) > 0 {
		q := p.Questions[0]
		req.Context = q.Text
		if q.Header != "" {
			req.Context = q.Header + "：" + q.Text
		}
	}
	req.Todo = "回答這一題"
	if len(p.Questions) > 1 {
		req.Todo = "回答這幾題"
	}
	return req
}

// dropAsk（持有 m.mu）：這個工作 session 不再排隊等名額了，還掛著的名額詢問記下來，放開鎖後由 retireAsks 收掉（S5-2）。
func (m *Module) dropAsk(e *entry) {
	if e == nil || e.workAsk == "" {
		return
	}
	m.staleAsks = append(m.staleAsks, e.workAsk)
	delete(m.workAsks, e.workAsk)
	delete(m.notices, e.workAsk)
	e.workAsk = ""
}

// retireAsks 收掉 dropAsk 記下的名額詢問（不持有 m.mu）。收不掉的下一次再試。
func (m *Module) retireAsks(ctx context.Context) {
	m.mu.Lock()
	ids := m.staleAsks
	m.staleAsks = nil
	m.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	retirer, ok := m.d.Notify.(noticeRetirer)
	if !ok {
		return
	}
	if err := retirer.Retire(ctx, ids, ""); err != nil {
		m.warn("收掉已經不用回的名額詢問失敗", err)
		m.mu.Lock()
		m.staleAsks = append(m.staleAsks, ids...)
		m.mu.Unlock()
	}
}
