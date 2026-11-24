package session

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 工作 session（P7-5、D-33、ai_docs/modules/session.md SES-27～SES-31）：一張任務單一個，生命週期跟著單的狀態。
// 這個檔案放：開之前的上限檢查與排隊、單狀態對帳（開、關、接回、50 分鐘提醒）、上限提問的回答。

const (
	// WorkSettle：狀態讓出的關閉（review、waiting_external、結束）要等 session 閒了這麼久才關，
	// 不剪掉秘書送驗收之後的最後一段話，也不剛開起來就關掉。
	WorkSettle = 15 * time.Second
	// WorkCloseAfter：waiting_user 保留 50 分鐘（引擎的 prompt cache 一小時），第 50 分鐘提醒一次，第 60 分鐘關掉；
	// 從單開始等、開起來、主人最後一句、最後一輪結束裡最晚的那個算起。
	WorkCloseAfter = 60 * time.Minute
)

// 工作對帳與等待提醒的週期。
var (
	WorkReconcileEvery = 10 * time.Second
	// WorkNudgeAfter 是 waiting_user 等多久提醒一次（預設 50 分鐘）。
	WorkNudgeAfter = 50 * time.Minute
)

// roomOfTask 認工作 session 名稱尾巴的 -<字首小寫>-<n>（字首是不是這個家的單號由 ids 決定）。
var roomOfTask = regexp.MustCompile(`-([a-z]+)-([0-9]+)$`)

// ids 是這個家的編號字首（home.toml [ids]）；還沒載入設定（測試）時是預設 SEC-。
func (m *Module) ids() platform.IDPrefixes {
	if m.config == nil {
		return platform.IDPrefixes{}
	}
	return m.config.IDs
}

// keepReviewOpen 是家的 [tasks] keep_review_open：等你驗收（review）時不關工作對話（還沒載入設定時是 false）。
func (m *Module) keepReviewOpen() bool { return m.config != nil && m.config.Tasks.KeepReviewOpen }

// workReview 是工作 session 記的「交付等驗收」：任務單的狀態是等你（waiting_user）且有 proposed（原本的 review，
// 2026-11-01 併進等你）。工作 session 對它的處理照舊（keep_review_open、等閒了就關、被退回告訴秘書），只是從單上算出來。
const workReview = "review"

// workStatus 是工作 session 看的單狀態：交付等驗收算 workReview，其他照單上的狀態。
func workStatus(t *sdk.Task) string {
	if t.Status == sdk.TaskStatusWaitingUser && t.Proposed != "" {
		return workReview
	}
	return string(t.Status)
}

// taskWork 回報這個 session 是不是綁了任務單的工作 session（呼叫時持有 m.mu 或在建立中）。
func taskWork(e *entry) bool { return e.session.Kind == sdk.SessionKindWork && e.session.Task != "" }

// openTaskWork：單還沒結（不是完成、取消）的工作 session。收起來了也留在對話清單，主人點進去說話就接回（DEV-147）。
func openTaskWork(e *entry) bool {
	if !taskWork(e) {
		return false
	}
	switch e.taskStatus {
	case "", string(sdk.TaskStatusDone), string(sdk.TaskStatusCancelled), "failed":
		return false
	}
	return true
}

// workTaskID 算出一個工作 session 對應哪張單：Topic 是單號就用它，其次是已存在的記錄，再來是名稱 <職位>-<字首小寫>-<n>。
func workTaskID(ids platform.IDPrefixes, req *sdk.SessionOpenRequest, old string) string {
	if ids.IsTask(req.Topic) {
		return req.Topic
	}
	if old != "" {
		return old
	}
	if m := roomOfTask.FindStringSubmatch(req.Name); m != nil {
		if id := strings.ToUpper(m[1]) + "-" + m[2]; ids.IsTask(id) {
			return id
		}
	}
	return ""
}

// workRoom 是一張單（<字首><n>）的工作 session 名稱：<職位>-<字首小寫>-<n>，例 code-sec-5（REQ-270）。
func workRoom(agent, id string) string {
	prefix, num, _ := strings.Cut(id, "-")
	return agent + "-" + strings.ToLower(prefix) + "-" + num
}

// readWork 讀一張單（任何佇列）並組出它交給工作 session 的第一句（只有這裡組，REQ-270）。
func (m *Module) readWork(ctx context.Context, id string) (*sdk.Task, string, error) {
	if m.d.Tasks == nil {
		return nil, "", platform.Unavailable("沒有任務單")
	}
	t, err := m.d.Tasks.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	brief := t.ID + "：" + t.Title + "\n" + t.Body // 跟 tasks.BriefText 同一個格式（session 不能 import tasks）
	// 附件（TSK-28）：/share/ 連結，秘書照共用規則讀（AGT-14）。
	if len(t.Attachments) > 0 {
		brief += "\n附件：\n- " + strings.Join(t.Attachments, "\n- ")
	}
	if t.ReviewNote != "" {
		brief += "\n退回意見：" + t.ReviewNote
	}
	return t, brief, nil
}

// workItem 讀一張單（只要單，不要第一句）。
func (m *Module) workItem(ctx context.Context, id string) (*sdk.Task, error) {
	t, _, err := m.readWork(ctx, id)
	return t, err
}

// workMax 讀 settings/sessions.toml 的 work_max（每次都重讀，改了不用重開 serve）；沒有或不合理就用預設 3（ReadWorkMax，worklimit.go）。
func (m *Module) workMax() int {
	if m.home == "" {
		return DefaultWorkMax
	}
	n, _, err := ReadWorkMax(m.home)
	if err != nil {
		m.warn("settings/sessions.toml 讀不懂，工作 session 上限用預設值", err)
	}
	return n
}

type workInfo struct{ name, task, title string }

// openDoing 列出這台現在開著、而且單是 doing 的工作 session（只有它們算進上限，排除 except 那個）。
func (m *Module) openDoing(except string) []workInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []workInfo
	for name, e := range m.entries {
		if name == except || !taskWork(e) || e.session.State == sdk.SessionStateStopped || e.taskStatus != string(sdk.TaskStatusDoing) {
			continue
		}
		out = append(out, workInfo{name, e.session.Task, strings.TrimSpace(strings.TrimPrefix(e.title, e.session.Task))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].task < out[j].task })
	return out
}

// workGate 在開工作 session 之前檢查：單存在、開在對的電腦、沒超過上限。超過的話建好但不啟動那一筆（排隊），
// 主人開的在討論串裡問，其他的直接排隊（討論串黃點）。回傳的 Session 不是 nil 就是「已排隊，不要啟動」。
func (m *Module) workGate(ctx context.Context, req *sdk.SessionOpenRequest, id string, force bool) (*sdk.Task, Session, error) {
	task, err := m.workItem(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	max := m.workMax()
	m.mu.Lock()
	if e := m.entries[req.Name]; e != nil {
		e.suspended = false // 明確要開（主人或派送器），不再當成手動關掉的
	}
	m.mu.Unlock()
	if force || task.Status != sdk.TaskStatusDoing {
		return task, nil, nil
	}
	open := m.openDoing(req.Name)
	if len(open) < max {
		return task, nil, nil
	}
	parked, err := m.parkWork(ctx, req, task, open)
	return task, parked, err
}

// parkWork 把這個工作 session 排進佇列：建好（或更新）記錄、不啟動引擎。主人開的單在那張單的討論串裡問
// （選項：照開／先排隊／關掉 <單號>…，附目前開著的清單，不推播）；不是主人開的直接排隊，討論串是 queued＋work_queue（灰色，不算要你回覆，S5 D6）。
func (m *Module) parkWork(ctx context.Context, req *sdk.SessionOpenRequest, task *sdk.Task, open []workInfo) (Session, error) {
	now := m.d.Clock.Now()
	account := ""
	if r, err := m.d.Vault.Get(ctx, "agent", req.Agent); err == nil {
		account, _, _, _ = m.seatAccount(r)
	}
	m.mu.Lock()
	e := m.entries[req.Name]
	if e == nil {
		e = &entry{session: sdk.Session{Name: req.Name, Agent: req.Agent, Kind: sdk.SessionKindWork, Account: account, State: sdk.SessionStateStopped, StateSince: now}, title: task.ID + " " + task.Title, idleClose: 30 * time.Minute}
		m.entries[req.Name] = e
	}
	e.session.Task, e.taskStatus = task.ID, workStatus(task)
	if e.session.Account == "" {
		e.session.Account = account
	}
	if e.session.QueuedAt.IsZero() {
		e.session.QueuedAt = now
	}
	ask := e.workAsk == "" && req.By != nil && platform.IsOwner(req.By.Name)
	err := m.changed(ctx, e)
	snap := copySession(e.session)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if ask {
		m.askWorkLimit(ctx, req.Name, req.Agent, task, open)
	}
	return snap, nil
}

// askWorkLimit 在那張單的討論串裡問主人：討論串內提問（Inline）只進看板與那個討論串，不推播。
func (m *Module) askWorkLimit(ctx context.Context, name, agent string, task *sdk.Task, open []workInfo) {
	if m.d.Notify == nil {
		return
	}
	opts := []string{"照開", "先排隊"}
	var list []string
	for _, o := range open {
		opts = append(opts, "關掉 "+o.task)
		list = append(list, strings.TrimSpace(o.task+" "+o.title))
	}
	n, err := m.d.Notify.Send(ctx, &sdk.Notification{
		Category: sdk.CategoryApprove, Agent: agent, Room: name, URL: "/chat/" + name, Inline: true,
		Title:   task.ID + " 的工作 session 要現在開嗎？",
		Context: fmt.Sprintf("這台已經有 %d 個進行中的工作 session：\n%s", len(open), strings.Join(list, "\n")),
		Todo:    "照開、先排隊，還是關掉其中一個？",
		Options: opts,
	})
	if err != nil || n == nil {
		m.warn("送出工作 session 上限的提問失敗", err)
		return
	}
	m.mu.Lock()
	e := m.entries[name]
	stale := e == nil
	if e != nil {
		e.workAsk = n.ID
		m.workAsks[n.ID] = name
		m.notices[n.ID] = name
		// 問的這段時間排隊已經取消了（單離開進行中、她已經在跑、主人手動關）：這一問馬上收掉，不留沒人收的提問。
		if e.session.QueuedAt.IsZero() {
			m.dropAsk(e)
		}
		if err := m.changed(ctx, e); err != nil {
			m.warn("儲存提問記錄失敗", err)
		}
	}
	m.mu.Unlock()
	if stale {
		m.mu.Lock()
		m.staleAsks = append(m.staleAsks, n.ID)
		m.mu.Unlock()
	}
	m.retireAsks(ctx)
}

// workStarting 在啟動引擎之前把記錄整理好（呼叫時持有 m.mu）：綁單號、離開排隊、決定接回哪個引擎 session。
func (m *Module) workStarting(e *entry, task *sdk.Task, resume *string) {
	e.session.Task = task.ID
	e.session.QueuedAt = time.Time{}
	e.suspended = false
	e.taskStatus = workStatus(task)
	e.opened = m.d.Clock.Now()
	// 記錄丟了（或這個 session 從沒開過）時，用單上記的引擎 session id 接回（每台一個家，任務單不再記開在哪台）。
	if *resume == "" && task.EngineSession != "" {
		*resume = task.EngineSession
	}
}

// autoOpenWork：單從別的狀態改成 doing（from 不是空的、也不是 doing）就開它的工作 session，第一句是單的內容，跟看板「開對話」一樣
// （D-33；作者 2026-10-28 定：已經是 doing 的舊單不動，免得一上線就開滿上限、其他全部排隊）。上限照 workGate：
// 主人改的狀態超過上限在討論串問，秘書改的直接排隊。已經有記錄的（關掉、排隊、手動關）交給 reconcileWork；
// AutoOpenWork 開這張單的專屬工作 session 並送單（P7-5、SES-27）：已經有這個 session、單不是進行中或負責人對不上就不動。
// 什麼時候叫由 tasks 決定（單從別的狀態改成 doing）；session 只負責開與名額。
func (m *Module) AutoOpenWork(ctx context.Context, id, agent, by string) {
	if m.d.Tasks == nil || agent == "" || !m.ids().IsTask(id) {
		return
	}
	name := workRoom(agent, id)
	m.mu.Lock()
	_, known := m.entries[name]
	m.mu.Unlock()
	if known {
		return
	}
	t, brief, err := m.readWork(ctx, id)
	if err != nil || t.Status != sdk.TaskStatusDoing || t.Agent != agent {
		return
	}
	s, err := m.Open(ctx, &sdk.SessionOpenRequest{Name: name, Agent: agent, Kind: sdk.SessionKindWork, Topic: id, By: &sdk.Actor{Name: by}})
	if err != nil || s == nil {
		m.warn("自動開工作 session 失敗", err)
		return
	}
	if err := m.queueBrief(ctx, s.Name, brief); err != nil {
		m.warn("送工作 session 的第一句失敗", err)
		return
	}
	m.wakeDelivery()
}

func (m *Module) OpenWork(ctx context.Context, id, agent string, by *sdk.Actor) (Session, error) {
	if !m.ids().IsTask(id) {
		return nil, platform.Invalid("topic", "請給單號（例："+m.ids().Task()+"1）")
	}
	t, brief, err := m.readWork(ctx, id)
	if err != nil {
		return nil, err
	}
	if agent == "" {
		agent = t.Agent
	}
	if agent == "" {
		return nil, platform.Invalid("agent", "這張單還沒有負責的秘書")
	}
	spec := &sdk.SessionOpenRequest{Name: workRoom(agent, id), Agent: agent, Kind: sdk.SessionKindWork, Topic: id, By: by} // 單的專屬對話是工作 session（P7-5、D-33）
	old, err := m.Get(ctx, spec.Name)
	if err == nil && old != nil && (old.State != sdk.SessionStateStopped || !old.QueuedAt.IsZero()) {
		return old, nil // 在跑的、或已經排隊等名額的，不重送單
	}
	if err == nil && old != nil && old.EngineSessionID != "" {
		// 停了但接得回原本的對話（單早就在裡面）：只把它開起來，主人看到的就是原本那串，不再送一次單（REQ-268）。
		return m.Open(ctx, spec)
	}
	v, err := m.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	if err := m.queueBrief(ctx, v.Name, brief); err != nil {
		return nil, err
	}
	m.wakeDelivery()
	return v, nil
}

// suspendWork：主人手動關掉的工作 session 不自動接回，直到單離開 doing 再回來（或主人再開它）。
func (m *Module) suspendWork(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[name]; e != nil && taskWork(e) {
		e.suspended = true
		e.session.QueuedAt = time.Time{}
		m.dropAsk(e) // 主人手動關：名額詢問當下收掉（S5-2）
		if err := m.save(e); err != nil {
			m.warn("儲存工作 session 狀態失敗", err)
		}
	}
}

func (m *Module) recordWork(ctx context.Context, s Session) {
	if m.d.Tasks == nil || s.State == sdk.SessionStateStopped {
		return
	}
	task, err := m.workItem(ctx, s.Task)
	if err != nil {
		m.warn("讀不到工作 session 的任務單", err)
		return
	}
	var engineSession *string
	if s.EngineSessionID != "" && task.EngineSession != s.EngineSessionID {
		id := s.EngineSessionID
		engineSession = &id
	}

	if engineSession == nil {
		return
	}
	by := task.Agent
	if by == "" {
		by = s.Agent
	}
	_, err = m.d.Tasks.Update(ctx, s.Task, &sdk.TasksUpdateRequest{ID: s.Task, EngineSession: engineSession}, by)
	if err != nil && platform.CodeOf(err) != sdk.ErrorCodeConflict {
		m.warn("把工作 session 記回單失敗", err)
	}
}

// reopenWorkFor：有訊息要給一個已關掉的工作 session。排隊中的等名額；其他的接回（單是 doing 時照上限的規則，
// 訊息是主人說的就在討論串裡問）。
func (m *Module) reopenWorkFor(ctx context.Context, s Session, e *sdk.Envelope) {
	// 主人對它說話：不看 work_max 直接開（作者 2026-10-30 的 3 秒規則，SES-52）；排隊中的、停了會被擋在上限的都一樣。
	if isOwnerChat(e) {
		// 排隊中的：開起來並收掉還掛著的上限提問；其他的只開。等引擎的那一段在背景（派送器不等）。
		m.openQueued(ctx, s.Name, s.Agent, true, !s.QueuedAt.IsZero())
		return
	}
	if !s.QueuedAt.IsZero() {
		return
	}
	// 主人手動關掉（或取消排隊）的：別人的話不把它開回來（REQ-218）。話留在 queued，等主人再開或對它說話、
	// 或單離開 doing 再回來；不在這裡 Open，因為 workGate 會把「手動關掉」當成明確要開而洗掉。
	m.mu.Lock()
	suspended := m.entries[s.Name] != nil && m.entries[s.Name].suspended
	m.mu.Unlock()
	if suspended {
		return
	}
	m.openLater(ctx, &sdk.SessionOpenRequest{Name: s.Name, Agent: s.Agent, Kind: sdk.SessionKindWork, By: &sdk.Actor{Name: e.From}}, func(_ Session, err error) {
		if err != nil {
			m.warn("接回工作 session 失敗", err)
		}
	})
}

// reconcileWork 讀每個工作 session 的任務單，讓 session 跟著單的狀態（D-33）：
//
//	doing            開著（超過上限就排隊）；關掉的接回
//	waiting_user     等你決定：保留；第 50 分鐘提醒一次；第 60 分鐘關
//	                 交付等驗收（有 proposed，workReview）：關掉（家設了 keep_review_open 就留著）
//	waiting_external、待做、完成、不做了   關掉（等它閒了才關）
func (m *Module) reconcileWork(ctx context.Context, now time.Time) {
	if m.d.Tasks == nil {
		return
	}
	m.mu.Lock()
	if !m.workDirty && !m.workAt.IsZero() && now.Before(m.workAt.Add(WorkReconcileEvery)) {
		m.mu.Unlock()
		return
	}
	m.workDirty, m.workAt = false, now
	var names []string
	for name, e := range m.entries {
		if !taskWork(e) {
			continue
		}
		// 單已經完成或不做了（不能再改回來；舊記錄的 failed 也算）、session 也關了、沒在排隊的：不用再讀它的單。
		switch e.taskStatus {
		case string(sdk.TaskStatusDone), string(sdk.TaskStatusCancelled), "failed":
			if e.session.State == sdk.SessionStateStopped && e.session.QueuedAt.IsZero() {
				continue
			}
		}
		names = append(names, name)
	}
	m.mu.Unlock()

	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	for _, name := range names {
		m.mu.Lock()
		e := m.entries[name]
		id := ""
		if e != nil {
			id = e.session.Task
		}
		m.mu.Unlock()
		if id == "" {
			continue
		}
		task, err := m.workItem(ctx, id)
		if err != nil {
			if code := platform.CodeOf(err); code != sdk.ErrorCodeNotFound {
				if code != sdk.ErrorCodeUnavailable {
					m.warn("讀工作 session 的任務單失敗", err)
				}
				continue
			}

			task = &sdk.Task{ID: id, Status: sdk.TaskStatusCancelled} // 單不見了：當成結束
		}
		m.applyTaskState(ctx, now, name, task)
	}
	m.startQueued(ctx, now)
}

func (m *Module) applyTaskState(ctx context.Context, now time.Time, name string, t *sdk.Task) {
	m.mu.Lock()
	e := m.entries[name]
	if e == nil {
		m.mu.Unlock()
		return
	}
	prevStatus := e.taskStatus
	status := workStatus(t)
	e.taskStatus = status
	running := e.session.State != sdk.SessionStateStopped
	queued := !e.session.QueuedAt.IsZero()
	dirty := false
	keepReview := status == workReview && m.keepReviewOpen() // 家設了 keep_review_open：等你驗收時手動關掉與排隊的都保留
	if t.Status != sdk.TaskStatusDoing && !keepReview && (e.suspended || queued) {
		e.suspended, e.session.QueuedAt, dirty = false, time.Time{}, true // 單離開 doing：手動關掉的與排隊的都作廢，回來時重新排
		m.dropAsk(e)                                                      // 不排了：名額詢問當下收掉（S5-2）
	}
	suspended := e.suspended
	nudged := e.nudgedFor
	agent, sessionName := e.session.Agent, e.session.Name
	// waiting_user 的計時從「最後一次有動靜」起算：單等了多久不算，主人剛打開、剛聊過的不會一停就被關（REQ-268）。
	waitFrom := t.WaitingSince
	for _, at := range []time.Time{e.opened, e.session.LastUserAt, e.session.LastTurnEndAt} {
		if at.After(waitFrom) {
			waitFrom = at
		}
	}
	m.mu.Unlock()
	save := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if e := m.entries[name]; e != nil {
			if err := m.changed(ctx, e); err != nil {
				m.warn("儲存工作 session 狀態失敗", err)
			}
		}
	}
	switch status {
	case string(sdk.TaskStatusDoing):
		if running {
			m.mu.Lock()
			if e.nudgedFor != "" || !e.session.QueuedAt.IsZero() || e.workAsk != "" {
				e.nudgedFor, e.session.QueuedAt, dirty = "", time.Time{}, true
				m.dropAsk(e) // 她已經在跑：名額詢問當下收掉（S5-2）
			}
			m.mu.Unlock()
			if prevStatus == workReview {
				m.tellResumed(ctx, name, t) // 等你驗收時沒關對話（keep_review_open），被退回時直接告訴它
			}
		} else if !suspended && !queued {
			m.mu.Lock()
			e.session.QueuedAt, dirty = now, true // 回到 doing 要接回：排進佇列，下面有名額就開
			m.mu.Unlock()
		}
	case string(sdk.TaskStatusWaitingUser):
		if !running || t.WaitingSince.IsZero() {
			break
		}
		switch {
		case !now.Before(waitFrom.Add(WorkCloseAfter)):
			m.closeWhenSettled(ctx, now, name)
		case !now.Before(waitFrom.Add(WorkNudgeAfter)):
			key := waitFrom.Format(time.RFC3339Nano)
			if nudged != key {
				m.nudge(ctx, WorkNudge{Agent: agent, Session: sessionName, Task: t.ID, Title: t.Title, Ask: t.Ask, WaitingSince: t.WaitingSince, URL: "/chat/" + sessionName})
				m.mu.Lock()
				e.nudgedFor, dirty = key, true
				m.mu.Unlock()
			}
		}
	case workReview:
		// 家設了 [tasks] keep_review_open：等你驗收時開著不關（主人要看過程、秘書可能還沒做完），驗收／取消才關；
		// 已經被關掉的（重開機等）自動接回，結單才算結束。沒設＝照舊，等閒了就關。
		if m.keepReviewOpen() {
			if !running && !suspended && !queued {
				m.mu.Lock()
				e.session.QueuedAt, dirty = now, true
				m.mu.Unlock()
			}
		} else if running {
			m.closeWhenSettled(ctx, now, name)
		}
	default:
		if running {
			m.closeWhenSettled(ctx, now, name)
		}
	}
	if dirty {
		save()
	}
}

// closeWhenSettled：單的狀態要這個工作 session 關掉時，等它閒了（沒有進行中的一輪、沒有要確認的事、
// 沒有排隊或處理中的訊息、閒了 WorkSettle 以上；主人 idle_close_after 內說過話也先不關）才關，不剪掉最後一段話。這不是手動關，之後單回到 doing 會接回。
func (m *Module) closeWhenSettled(ctx context.Context, now time.Time, name string) {
	m.mu.Lock()
	e := m.entries[name]
	if e == nil {
		m.mu.Unlock()
		return
	}
	state, pending := e.session.State, e.session.Pending != nil
	since := e.session.StateSince
	if e.opened.After(since) {
		since = e.opened
	}
	// 主人在這個對話裡說過話：跟一般對話一樣閒 idle_close_after（30 分鐘）才收，
	// 不在討論到一半、秘書把單切成等外部或結掉時就關（DEV-147）。
	talked := e.ownerTalk
	hold := e.idleClose
	m.mu.Unlock()
	if state != sdk.SessionStateIdle || pending || now.Before(since.Add(WorkSettle)) {
		return
	}
	if !talked.IsZero() && now.Before(talked.Add(hold)) {
		return
	}
	live, err := m.d.Bus.List(ctx, name, sdk.LifecycleQueued, sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleWorking, sdk.LifecycleBlocked)
	if err != nil || len(live) > 0 {
		return
	}
	m.background(ctx, name, func(ctx context.Context) { // 等引擎關的那一段在背景：定時那一輪不等
		release, ok := m.quiet(name, false)
		if !ok {
			return
		}
		defer release()
		if err := m.close(ctx, name); err != nil {
			m.warn("關閉工作 session 失敗", err)
		}
	})
}

// startQueued：有名額就依排隊的先後把排隊中的工作 session 開起來（接回時帶原本的引擎 session）。
func (m *Module) startQueued(ctx context.Context, now time.Time) {
	max := m.workMax()
	type queuedWork struct {
		name, agent string
		at          time.Time
		hadSession  bool
		review      bool
	}
	tried := map[string]bool{} // 這一輪試過的不再試：引擎那一段在背景，馬上失敗的不在這裡打轉
	for {
		m.mu.Lock()
		var q []queuedWork
		for name, e := range m.entries {
			if tried[name] || !taskWork(e) || e.session.State != sdk.SessionStateStopped || e.session.QueuedAt.IsZero() || e.suspended {
				continue
			}
			// keep_review_open 的家：等你驗收時被關掉的對話接回不占進行中的名額（驗收中不算進行中）。
			if e.taskStatus == string(sdk.TaskStatusDoing) || (e.taskStatus == workReview && m.keepReviewOpen()) {
				q = append(q, queuedWork{name, e.session.Agent, e.session.QueuedAt, e.session.EngineSessionID != "", e.taskStatus == workReview})
			}
		}
		m.mu.Unlock()
		if len(q) > 0 && len(m.openDoing("")) >= max {
			kept := q[:0]
			for _, w := range q {
				if w.review {
					kept = append(kept, w)
				}
			}
			q = kept
		}
		if len(q) == 0 {
			return
		}
		sort.Slice(q, func(i, j int) bool {
			if !q[i].at.Equal(q[j].at) {
				return q[i].at.Before(q[j].at)
			}
			return q[i].name < q[j].name
		})
		first := q[0]
		tried[first.name] = true
		// 登記當場做（馬上是開機中、占一個名額），等引擎的那一段在背景：前一個的引擎不回，後面排隊的照樣輪得到。
		if m.openQueued(ctx, first.name, first.agent, false, true) == openSettled {
			return // 開不起來（引擎沒準備好等）：下一輪再試，不在這裡打轉
		}
	}
}

// openQueued 開排隊中的工作 session（force：照開，不看上限），不等引擎：開好之後在背景記回單；
// after：再收掉還掛著的上限提問、接回的跟秘書說一聲（afterQueuedStart）。
func (m *Module) openQueued(ctx context.Context, name, agent string, force, after bool) openTry {
	m.mu.Lock()
	e := m.entries[name]
	resumed := e != nil && e.session.EngineSessionID != ""
	m.mu.Unlock()
	return m.openBackground(ctx, &sdk.SessionOpenRequest{Name: name, Agent: agent, Kind: sdk.SessionKindWork}, force, func(s Session, err error) {
		if err != nil {
			m.warn("開排隊中的工作 session 失敗", err)
			return
		}
		if s == nil || s.State == sdk.SessionStateStopped {
			return
		}
		m.recordWork(ctx, s)
		if after {
			m.afterQueuedStart(ctx, s, resumed)
		}
	})
}

// afterQueuedStart：排隊的開起來之後，記回單、收掉還掛著的提問、接回的跟秘書說一聲。
func (m *Module) afterQueuedStart(ctx context.Context, s Session, resumed bool) {
	m.mu.Lock()
	var ask string
	if e := m.entries[s.Name]; e != nil {
		ask, e.workAsk = e.workAsk, ""
		delete(m.workAsks, ask)
		delete(m.notices, ask)
		if err := m.changed(ctx, e); err != nil {
			m.warn("儲存工作 session 狀態失敗", err)
		}
	}
	m.mu.Unlock()
	if ask != "" {
		if a, ok := m.d.Notify.(interface {
			Answer(context.Context, string, string, sdk.AnswerVia, string) (*sdk.Notification, error)
		}); ok {
			if _, err := a.Answer(ctx, ask, "已自動開始", sdk.AnswerViaAuto, "system"); err != nil {
				m.warn("收掉工作 session 上限的提問失敗", err)
			}
		}
	}
	if !resumed {
		return // 第一次開：第一句話（任務單的 Brief）已經由開的人排好了
	}
	t, _ := m.workItem(ctx, s.Task)
	m.tellResumed(ctx, s.Name, t)
}

// resendBrief：工作 session 接不回原本的對話、開了新的，單的內容要再送一次，不然秘書在空白對話裡不知道要做什麼（SES-67）。
func (m *Module) resendBrief(ctx context.Context, name, id string) {
	_, brief, err := m.readWork(ctx, id)
	if err != nil {
		m.warn("讀工作 session 的單失敗", err)
		return
	}
	if err := m.queueBrief(ctx, name, brief); err != nil {
		m.warn("重送工作 session 的單失敗", err)
		return
	}
	m.wakeDelivery()
}

// queueBrief 把單的內容排給工作 session；同一份還排著沒送出就不再排（接不回對話時單常常還沒送出，秘書不該收到兩份）。
func (m *Module) queueBrief(ctx context.Context, name, brief string) error {
	rows, err := m.d.Bus.List(ctx, name, sdk.LifecycleQueued)
	if err != nil {
		return err
	}
	for _, e := range rows {
		if e.Body == brief {
			return nil
		}
	}
	_, err = m.d.Bus.Send(ctx, &sdk.Envelope{From: "system", To: name, Kind: sdk.EnvelopeKindRoutine, Body: brief})
	return err
}

// tellResumed 送「單回到進行中」給工作 session（t 可為 nil；有退回意見就附上）。
func (m *Module) tellResumed(ctx context.Context, name string, t *sdk.Task) {
	id := ""
	if t != nil {
		id = t.ID
	}
	body := "［單 " + id + " 回到進行中，請從上次的地方接著做］"
	if t != nil && t.ReviewNote != "" {
		body += "\n主人退回的意見：" + t.ReviewNote
	}
	if _, err := m.d.Bus.Send(ctx, &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, Route: sdk.EnvelopeRouteCore, From: "system", To: name, Body: body}); err != nil {
		m.warn("通知接回的工作 session 失敗", err)
	}
	m.wakeDelivery()
}

// handleWorkAnswer：主人回答了「工作 session 超過上限」的提問。不是這種提問回 false（交給一般的答案流程）。
func (m *Module) handleWorkAnswer(ctx context.Context, noticeID, answer string) (bool, error) {
	m.mu.Lock()
	name, ok := m.workAsks[noticeID]
	if !ok {
		m.mu.Unlock()
		return false, nil
	}
	delete(m.workAsks, noticeID)
	delete(m.notices, noticeID)
	m.answered[noticeID] = true
	agent := ""
	if e := m.entries[name]; e != nil {
		e.workAsk = ""
		agent = e.session.Agent
		if err := m.changed(ctx, e); err != nil {
			m.warn("儲存提問記錄失敗", err)
		}
	}
	m.mu.Unlock()
	answer = strings.TrimSpace(answer)
	switch {
	case answer == "照開":
		m.startForced(ctx, name, agent)
	case strings.HasPrefix(answer, "關掉 "):
		if err := m.parkTask(ctx, strings.TrimSpace(strings.TrimPrefix(answer, "關掉 "))); err != nil {
			m.warn("關掉工作 session 失敗", err)
		}
		m.startForced(ctx, name, agent)
	}
	// 「先排隊」與其他回答：留在佇列，有名額會自動開。
	m.publish(ctx, "session.approval.resolved", map[string]any{"name": name, "pending_id": "work-limit", "notices": []string{noticeID}})
	return true, nil
}

// startForced：主人明確說要開（名額提問回答「照開」「關掉 <單號>」）。那個對話剛好正在開關時不能當沒這回事
// （提問已經收掉了，不會再問）：在背景等它做完再開。
func (m *Module) startForced(ctx context.Context, name, agent string) {
	if m.openQueued(ctx, name, agent, true, true) != openBusy {
		return
	}
	m.mu.Lock()
	e := m.entries[name]
	resumed := e != nil && e.session.EngineSessionID != ""
	m.mu.Unlock()
	m.spawn(ctx, func(ctx context.Context) {
		l := m.sessionLock(name)
		l.Lock()
		defer l.Unlock()
		s, err := m.open(ctx, &sdk.SessionOpenRequest{Name: name, Agent: agent, Kind: sdk.SessionKindWork}, true)
		if err != nil {
			m.warn("開排隊中的工作 session 失敗", err)
			return
		}
		if s == nil || s.State == sdk.SessionStateStopped {
			return
		}
		m.recordWork(ctx, s)
		m.afterQueuedStart(ctx, s, resumed)
	})
}

// parkTask 把某張單的工作 session 關掉並排回佇列（「關掉 <單號>」：不是結束它，有名額再接回）。
func (m *Module) parkTask(ctx context.Context, taskID string) error {
	m.mu.Lock()
	name := ""
	for n, e := range m.entries {
		if taskWork(e) && e.session.Task == taskID && e.session.State != sdk.SessionStateStopped {
			name = n
		}
	}
	m.mu.Unlock()
	if name == "" {
		return nil
	}
	l := m.sessionLock(name)
	l.Lock()
	err := m.close(ctx, name)
	l.Unlock()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[name]; e != nil {
		e.session.QueuedAt = m.d.Clock.Now()
		return m.changed(ctx, e)
	}
	return nil
}

// nudge 是 waiting_user 第 50 分鐘的提醒：交給 Deps.Nudge（P7-6 的 notify.Nudge）；沒設就發一則一般的 approve。
func (m *Module) nudge(ctx context.Context, n WorkNudge) {
	var err error
	switch {
	case m.d.Nudge != nil:
		_, err = m.d.Nudge.Nudge(ctx, &sdk.NotifyNudgeRequest{
			Agent: n.Agent, Session: n.Session, Task: n.Task, URL: n.URL,
			Title:   n.Task + " 還在等你處理",
			Context: n.Title + "（工作 session 再 10 分鐘會關掉，之後接回來會比較慢）",
			Todo:    n.Ask,
		})
	case m.d.Notify != nil:
		todo := n.Ask
		if todo == "" {
			todo = "請回覆"
		}
		_, err = m.d.Notify.Send(ctx, &sdk.Notification{Category: sdk.CategoryApprove, Agent: n.Agent, Task: n.Task, URL: n.URL, Todo: todo,
			Title:   n.Task + " 還在等你處理",
			Context: n.Title + "\n工作 session 再 10 分鐘會關掉，之後接回來會比較慢。"})
	}
	if err != nil {
		m.warn("送出等待提醒失敗", err)
	}
}

// markWorkDirty 讓下一次 Tick 立刻對帳（task.changed 來了）。
// RefreshWork 請 session 下一次 Tick 就對帳工作 session（單有變化時 tasks 叫）。
func (m *Module) RefreshWork() { m.markWorkDirty() }

func (m *Module) markWorkDirty() {
	m.mu.Lock()
	m.workDirty = true
	m.mu.Unlock()
}
