package notify

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 不吵的通知（P7-6、D-34）：工作 session 在 waiting_user 第 50 分鐘「還在等你」的提醒。
// 它不是通知：沒有 M-<n>、不進訊息列表，只存在記憶體；去哪裡由主人正在看哪裡決定。
const (
	// PresenceTTL：看板每 30 秒報一次「我在看哪裡」，90 秒沒收到就當不在看。
	PresenceTTL = 90 * time.Second
	// InlineEscalateAfter：頁內卡片 2 分鐘沒回，改推播。
	InlineEscalateAfter = 2 * time.Minute
	// NudgeLifetime：提醒最多留這麼久（工作 session 再 10 分鐘就關了，超過沒有意義）。
	NudgeLifetime = 15 * time.Minute
	// WaitLifetime：綁在對話上的等待（帶 key，DEV-49 S2）由發問的 session 在那件事不在時收回；這只是記憶體的上限。
	WaitLifetime = 24 * time.Hour
	maxViewers   = 64
)

// viewer 是一個看板分頁現在看的地方。
type viewer struct {
	path, thread string
	at           time.Time
}

type nudgeEntry struct {
	n          sdk.Nudge
	tag        string    // Web Push 的 tag：同一件事永遠同一個，新的取代舊的
	escalateAt time.Time // 只有 inline 用
}

// nudgePayload 是推播的內容。跟一般通知的 JSON 同一種形狀（service worker 認 title／todo／url），
// 多一個 nudge：tag 讓同一件事只留一則。home 跟一般通知一樣是這台的標籤（NTF-53）。
type nudgePayload struct {
	Title string    `json:"title"`
	Todo  string    `json:"todo,omitempty"`
	URL   string    `json:"url,omitempty"`
	Home  string    `json:"home,omitempty"`
	Nudge nudgeMeta `json:"nudge"`
}
type nudgeMeta struct {
	Tag string `json:"tag"`
}

// inQuiet 回報 t 在不在安靜時段（偏好優先，沒設用家的使用者設定）。
func (m *Module) inQuiet(t time.Time) bool {
	quiet := m.prefs.QuietHours
	if quiet == "" {
		quiet = m.config.User.QuietHours
	}
	parts := strings.Split(quiet, "-")
	return len(parts) == 2 && (platform.Quiet{Start: parts[0], End: parts[1], Loc: m.config.Location()}).Contains(t)
}

// Nudge 送一則「還在等你」的提醒：只推手機（不建 M-<n>）、遵守安靜時段、記進任務單的 nudges。
// 去哪裡看主人在哪：在看那張單／討論串 → 頁內卡片；看板開著在別頁 → 站內提示；都不是 → 推播。
// 同一個 session 已經有還沒收掉的提醒時回既有的那則，不重複提醒。
func (m *Module) Nudge(ctx context.Context, req *sdk.NotifyNudgeRequest) (*sdk.Nudge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.Agent) == "" {
		return nil, platform.Invalid("agent", "請指定是哪位秘書")
	}
	session := strings.TrimSpace(req.Session)
	if session == "" {
		return nil, platform.Invalid("session", "請指定是哪個 session")
	}
	now := m.d.Clock.Now()
	m.pruneViewers(now)
	key := strings.TrimSpace(req.Key)
	if req.Resolve {
		// 那件事不在了（DEV-49 S2）：收回同一個 session、同一個 key 的那則；沒有也算成功。
		m.resolveNudgesWhere(ctx, func(n *sdk.Nudge) bool { return n.Session == session && n.Key == key })
		return &sdk.Nudge{Agent: req.Agent, Session: session, Key: key, Resolved: true}, nil
	}
	for _, e := range m.nudges {
		if e.n.Session == session && e.n.Key == key {
			out := e.n
			return &out, nil
		}
	}
	task := strings.TrimSpace(req.Task)
	title := strings.TrimSpace(req.Title)
	if title == "" {
		if task != "" {
			title = task + " 還在等你處理"
		} else {
			title = session + " 還在等你處理"
		}
	}
	todo := strings.TrimSpace(req.Todo)
	if todo == "" {
		todo = "請回覆"
	}
	e := &nudgeEntry{
		n: sdk.Nudge{ID: "nudge-" + platform.NewID(), Agent: req.Agent, Session: session, Task: task,
			Title: title, Context: strings.TrimSpace(req.Context), Todo: todo, URL: m.nudgeURL(req.URL, session), Key: key, CreatedAt: now.In(m.config.Location())},
		tag: "nudge-" + session,
	}
	if key != "" {
		e.tag += "-" + key
	}
	switch {
	case m.viewing(now, session, task):
		e.n.Surface, e.escalateAt = sdk.NudgeSurfaceInline, now.Add(InlineEscalateAfter)
	case len(m.viewers) > 0:
		e.n.Surface = sdk.NudgeSurfaceToast
	default:
		e.n.Surface = sdk.NudgeSurfaceNone
		if m.pushNudge(ctx, e) {
			e.n.Surface = sdk.NudgeSurfacePush
		}
	}
	if e.n.Surface == sdk.NudgeSurfaceNone {
		// 沒有地方可以顯示（安靜時段、沒訂閱…）：不記、不算次數；之後也不會再進來（session 那邊同一輪只提醒一次）。
		out := e.n
		return &out, nil
	}
	m.nudges[e.n.ID] = e
	if key == "" {
		m.countNudge(ctx, task) // 對話裡的等待不是「還在等你」的跟進，不記進任務單的 nudges
	}
	m.publish(ctx, "notice.nudged", map[string]any{"id": e.n.ID, "agent": e.n.Agent, "session": session, "task": task, "surface": string(e.n.Surface), "key": key})
	m.broadcastNudge(ctx, e)
	out := e.n
	return &out, nil
}

// nudgeURL 預設是本機 session 的聊天頁。
func (m *Module) nudgeURL(given, session string) string {
	u := strings.TrimSpace(given)
	if u == "" || !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") {
		u = "/chat/" + url.PathEscape(session)
	}

	return u
}

// countNudge 把任務單的 nudges 加 1；失敗只記警告，不影響提醒。
func (m *Module) countNudge(ctx context.Context, task string) {
	if task == "" || m.tasks == nil {
		return
	}
	if err := m.tasks.AddNudge(ctx, task); err != nil {
		m.warn("無法記錄任務單的提醒次數")
	}
}

// Presence 記下一個看板分頁現在看哪裡（只存記憶體），順便檢查有沒有提醒該改推播；回傳要顯示在看板上的提醒。
func (m *Module) Presence(ctx context.Context, req *sdk.NotifyPresenceRequest, by string) (*sdk.NotifyPresenceResponse, error) {
	if err := requireOwner(by); err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.Client) == "" || len(req.Client) > 64 {
		return nil, platform.Invalid("client", "請帶這個分頁的編號（64 字元以內）")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	now := m.d.Clock.Now()
	m.pruneViewers(now)
	if req.Away {
		delete(m.viewers, req.Client)
	} else {
		if _, known := m.viewers[req.Client]; !known && len(m.viewers) >= maxViewers {
			oldest, at := "", now
			for id, v := range m.viewers {
				if !v.at.After(at) {
					oldest, at = id, v.at
				}
			}
			delete(m.viewers, oldest)
		}
		m.viewers[req.Client] = viewer{path: clip(req.Path, 200), thread: clip(req.Thread, 120), at: now}
	}
	m.reviewNudges(ctx, now)
	out := &sdk.NotifyPresenceResponse{}
	for _, e := range m.nudges {
		if e.n.Surface == sdk.NudgeSurfaceInline || e.n.Surface == sdk.NudgeSurfaceToast {
			out.Nudges = append(out.Nudges, e.n)
		}
	}
	sort.Slice(out.Nudges, func(i, j int) bool { return out.Nudges[i].CreatedAt.After(out.Nudges[j].CreatedAt) })
	return out, nil
}

// ResolveNudge：主人在卡片或提示上按「知道了」。沒有這則（已經收掉、過期）也算成功。
func (m *Module) ResolveNudge(ctx context.Context, id, by string) error {
	if err := requireOwner(by); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return err
	}
	m.resolveNudgesWhere(ctx, func(n *sdk.Nudge) bool { return n.ID == id })
	return nil
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

func (m *Module) pruneViewers(now time.Time) {
	for id, v := range m.viewers {
		if now.Sub(v.at) >= PresenceTTL {
			delete(m.viewers, id)
		}
	}
}

// viewing：有沒有分頁正在看那個 session 的討論串，或那張任務單的頁面。呼叫者持有 m.mu，並已 pruneViewers。
func (m *Module) viewing(now time.Time, session, task string) bool {
	for _, v := range m.viewers {
		if now.Sub(v.at) >= PresenceTTL {
			continue
		}
		path, _, _ := strings.Cut(v.path, "?")
		if v.thread == session || path == "/chat/"+session || (task != "" && path == "/tasks/"+task) {
			return true
		}
	}
	return false
}

// reviewNudges 每秒（Tick）與每次回報在場時跑：過期的收掉；頁內卡片滿 2 分鐘或離開頁面、站內提示沒有人在看了，改推播。
func (m *Module) reviewNudges(ctx context.Context, now time.Time) {
	if len(m.nudges) == 0 {
		return
	}
	m.pruneViewers(now)
	for id, e := range m.nudges {
		life := NudgeLifetime
		if e.n.Key != "" {
			life = WaitLifetime
		}
		if !now.Before(e.n.CreatedAt.Add(life)) {
			delete(m.nudges, id)
			if e.n.Surface == sdk.NudgeSurfaceInline || e.n.Surface == sdk.NudgeSurfaceToast {
				e.n.Surface, e.n.Resolved = sdk.NudgeSurfaceNone, true
				m.broadcastNudge(ctx, e)
			}
			continue
		}
		switch e.n.Surface {
		case sdk.NudgeSurfaceInline:
			if !now.Before(e.escalateAt) || !m.viewing(now, e.n.Session, e.n.Task) {
				m.escalate(ctx, e)
			}
		case sdk.NudgeSurfaceToast:
			// 站內提示沒有人看得到了（看板關了、切到背景）：改推播，免得提醒掉在沒人看的地方。
			if len(m.viewers) == 0 {
				m.escalate(ctx, e)
			}
		}
	}
}

// escalate 把站內的提醒改推播（安靜時段等推不出去就收掉）。
func (m *Module) escalate(ctx context.Context, e *nudgeEntry) {
	e.n.Surface = sdk.NudgeSurfaceNone
	if m.pushNudge(ctx, e) {
		e.n.Surface = sdk.NudgeSurfacePush
	}
	m.broadcastNudge(ctx, e)
}

// resolveNudgesWhere 收掉符合條件的提醒：站內的卡片與提示經事件一起收。已經推到手機的不再補推
// （「已處理」不在法律第 5 條的白名單，手機上還會跳出來又看不出是哪件事，DEV-123）。
func (m *Module) resolveNudgesWhere(ctx context.Context, match func(*sdk.Nudge) bool) {
	for id, e := range m.nudges {
		if !match(&e.n) {
			continue
		}
		delete(m.nudges, id)
		e.n.Resolved = true
		m.publish(ctx, "notice.nudge.resolved", map[string]any{"id": e.n.ID, "agent": e.n.Agent, "session": e.n.Session, "task": e.n.Task})
		m.broadcastNudge(ctx, e)
	}
}

func (m *Module) broadcastNudge(ctx context.Context, e *nudgeEntry) {
	n := e.n
	m.broadcast(ctx, nil, &n)
}

func (m *Module) pushNudge(ctx context.Context, e *nudgeEntry) bool {
	if m.config.Runtime.Sandbox || !m.prefs.ChannelApp {
		return false
	}
	pref := m.preference(&sdk.Notification{Category: sdk.CategoryApprove, Agent: e.n.Agent})
	if pref == nil || pref.Mode != sdk.DeliveryModeInstant || m.inQuiet(m.d.Clock.Now()) {
		return false
	}
	subs, err := m.subscriptions()
	if err != nil {
		m.warn("推播訂閱無法讀取或格式不正確")
		return false
	}
	if len(subs) == 0 {
		return false
	}
	p := nudgePayload{Title: m.pushTitle(e.n.Title), Todo: "請你：" + e.n.Todo, URL: e.n.URL, Home: m.homeLabel(), Nudge: nudgeMeta{Tag: e.tag}}
	if e.n.Context != "" {
		p.Todo = e.n.Context + "\n" + p.Todo
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return false
	}
	ok, warned := false, false
	for _, sub := range subs {
		if err := m.d.Push.Send(ctx, sub, payload); err != nil {
			if !warned {
				m.warn(pushFailure(err))
				warned = true
			}
			continue
		}
		ok = true
	}
	return ok
}
