package notify

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func (m *Module) Send(ctx context.Context, input Notice) (Notice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	n := cloneNotice(input)
	if n != nil && !m.ownedByModule(n.Source) {
		n.Source = ""
	}
	if n != nil && n.Source != sdk.NotifySourcePackage {
		n.Claim = "" // 認領的職位只由 pkg 標（SEC-302 S9）
	}
	return m.send(ctx, n, false)
}

// ownedByModule：業務模組自己處理回答的通知（Goal 確認、權限申請、認領職位…），由那個業務模組在 Send 時標 source
// （API 的 Send 不帶 source，所以秘書標不了）。哪些 source 算數看業務模組登記的通知來源（app.NoticeSourceProvider，
// 經 app 組裝時的 ConsumeNoticeSources；2026-11-01 起不再寫死）。這種回答不送回秘書的對話，也不追蹤送達。
func (m *Module) ownedByModule(source string) bool {
	_, ok := m.sources[source]
	return source != "" && ok
}

// noTodo：「不用主人另外做什麼」（或要做的事已經寫在內文）。看板與推播看到它就不畫「請你」那一行。
const noTodo = "無"

func (m *Module) send(ctx context.Context, n Notice, summary bool) (Notice, error) {
	if n == nil || !validCategory(n.Category) {
		return nil, platform.Invalid("category", "請指定通知分類")
	}
	if strings.TrimSpace(n.Title) == "" {
		return nil, platform.Invalid("title", "請填寫通知標題")
	}
	if n.Category == sdk.CategoryApprove && strings.TrimSpace(n.Todo) == "" {
		return nil, platform.Invalid("todo", "請說明需要使用者做什麼")
	}
	if n.Typed { // 只收打字回答（D-57、NTF-60）：不放任何按鈕
		n.Options = nil
	}
	if n.Category == sdk.CategoryApprove && len(n.Options) == 0 && !n.Typed {
		n.Options = []string{"好", "不要"}
	}
	if n.Todo == "" {
		n.Todo = noTodo
	}
	if n.By == "" {
		n.By = n.Agent
	}
	if n.By == "" {
		n.By = "system"
	}
	if n.Agent == "" {
		n.Agent = n.By
	}
	if err := validLane(n.Lane); err != nil {
		return nil, err
	}
	if err := cleanLink(n); err != nil {
		return nil, err
	}
	n.Speaker = strings.TrimSpace(n.Speaker)
	n.Lane = sendLane(n)
	n.State = sdk.NotificationStateUnread
	n.Answer = nil
	n.CreatedAt = m.d.Clock.Now().In(m.config.Location())
	id, err := m.d.Vault.NextID(ctx, "message")
	if err != nil {
		return nil, err
	}
	n.ID = id
	if n.URL == "" {
		n.URL = "/messages/" + id
	}
	labels := map[sdk.Category]string{"approve": "要你同意", "done": "事情做完", "due": "待辦到期", "digest": "摘要", "opening": "秘書主動開場"}
	title := n.Title
	if strings.HasPrefix(title, "[") {
		// 呼叫者把 notify 組的開頭貼進標題：`[職位｜帳號] ` 或沒帳號的 `[職位] `（NTF-62）都拿掉。
		if end := strings.Index(title, "] "); end >= 0 && (strings.Contains(title[:end], "｜") || (n.Agent != "" && title[1:end] == n.Agent)) {
			title = title[end+2:]
		}
	}
	n.Title = title
	n.Body = noticeBody(n.Agent, n.Account, labels[n.Category], title, n.Context, n.Todo)
	n.Delivery = []sdk.Delivery{{Channel: sdk.ChannelBoard, At: n.CreatedAt, Result: "ok"}}
	saved, err := m.save(ctx, n, n.By)
	if err != nil {
		return nil, err
	}
	n = saved
	pref := m.preference(n)
	mode := pref.Mode
	if mode == sdk.DeliveryModeDigest && summary {
		mode = sdk.DeliveryModeInstant
	}
	// 系統那道的例行報告只放看板（NTF-58）：不推、不進摘要桶；要主人回答的與 urgent 照偏好。
	if quietLane(n) && mode != sdk.DeliveryModeOff {
		mode = sdk.DeliveryModeBoard
	}
	// 角色本人說話的（speaker，新角色來打招呼）在法律第 5 條的推播白名單裡（v1.12，2026-11-04 作者）：
	// 不管「主動開場」偏好是只放看板或摘要，一律即時推；主人設「關閉」照舊、安靜時段照舊不推（NTF-66）。
	// 開發家（env = "dev"）的招呼只放看板：同一位新角色正式家已經推過，開發家再推一次就重複了。
	if n.Speaker != "" && mode != sdk.DeliveryModeOff {
		mode = sdk.DeliveryModeInstant
		if m.config != nil && m.config.IsDev() {
			mode = sdk.DeliveryModeBoard
		}
	}
	if mode == sdk.DeliveryModeDigest {
		key := string(n.Category) + ":" + n.Agent
		bucket := m.buckets[key]
		if bucket == nil {
			bucket = &digestBucket{at: n.CreatedAt, hours: pref.DigestHours}
			m.buckets[key] = bucket
		}
		bucket.notices = append(bucket.notices, *cloneNotice(n))
	}
	inQuiet := m.inQuiet(n.CreatedAt)
	// 沙盒家（home.toml [runtime] sandbox = true，舊寫法 dev = true）永遠不推播：只有看板站內通知（D-35、REQ-180）。
	// 討論串內提問（Inline，P7-5、D-33）只進看板與那個討論串，不推播。
	if mode == sdk.DeliveryModeInstant && m.prefs.ChannelApp && !m.config.Runtime.Sandbox && !n.Inline && (!inQuiet || n.Urgent) {
		subs, subErr := m.subscriptions()
		if subErr != nil {
			m.warn("推播訂閱無法讀取或格式不正確")
		}
		if len(subs) > 0 || subErr != nil {
			result := "ok"
			if subErr != nil {
				result = "failed"
			} else {
				pushed := *n
				pushed.Title = m.pushTitle(n.Title)
				pushed.Home = m.homeLabel()
				payload, err := json.Marshal(&pushed)
				if err != nil {
					return nil, platform.Internal("通知無法編碼")
				}
				warned := false
				for _, sub := range subs {
					if err := m.d.Push.Send(ctx, sub, payload); err != nil {
						result = "failed"
						if !warned {
							m.warn(pushFailure(err))
							warned = true
						}
					}
				}
			}
			n.Delivery = append(n.Delivery, sdk.Delivery{Channel: sdk.ChannelApp, At: m.d.Clock.Now().In(m.config.Location()), Result: result})
			n, err = m.save(ctx, n, n.By)
			if err != nil {
				return nil, err
			}
		}
	}
	m.publish(ctx, "notice.sent", map[string]any{"id": n.ID, "category": string(n.Category), "agent": n.Agent, "room": n.Room, "task": n.Task, "todo": n.Todo, "reminder": n.Reminder, "delivery": n.Delivery})
	m.changed(ctx, n)
	return cloneNotice(n), nil
}

// cleanLink 檢查訊息上的連結按鈕（NTF-66，DEV-115）：字與網址都要有，網址只收站內路徑，免得一顆按鈕把主人帶到外面的網站；
// 兩樣都空就當沒有。站內路徑＝/ 開頭、沒有空白與反斜線，路徑部分（? 與 # 之前）沒有空段（//）、也沒有 . 或 .. 段——
// 瀏覽器把 /.//evil.com 這種正規化之後會變成 //evil.com（別的網站）。
func cleanLink(n Notice) error {
	if n.Link == nil {
		return nil
	}
	n.Link.Label, n.Link.URL = strings.TrimSpace(n.Link.Label), strings.TrimSpace(n.Link.URL)
	if n.Link.Label == "" && n.Link.URL == "" {
		n.Link = nil
		return nil
	}
	if n.Link.Label == "" || !siteLocalPath(n.Link.URL) {
		return platform.Invalid("link", "連結按鈕要有字，網址要是站內路徑（/ 開頭）")
	}
	return nil
}

func siteLocalPath(u string) bool {
	if !strings.HasPrefix(u, "/") || strings.ContainsAny(u, " \t\r\n\\") {
		return false
	}
	p := u
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if p == "/" {
		return true
	}
	for _, seg := range strings.Split(strings.TrimSuffix(p[1:], "/"), "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func (m *Module) tracksAnswer(n Notice) bool {
	return n.Answer != nil && n.Answer.Via != sdk.AnswerViaAuto && m.deliversAnswer(n)
}

// deliversAnswer：主人回答之後要等秘書讀到（記 delivered_at、超過 2 小時再問一次）的那種通知——
// 綁在對話上的，或沒綁單／待辦、不是系統提醒、也不是業務模組自己處理的秘書通知。系統提醒、套件通知、
// 單與待辦的通知回答了就算數，看板不說「秘書還沒讀到」（NTF-59，讀出來時寫進 delivers_answer）。
func (m *Module) deliversAnswer(n Notice) bool {
	return !m.ownedByModule(n.Source) && (n.Room != "" || (n.Task == "" && n.Reminder == "" && n.Source != "system.alert" && n.Agent != ""))
}

// relaysAnswer：這則回答要不要由 session 封成 answer 封包送回發問的對話（notice.answered 的 relay）。
// 自動收掉的、業務模組自己處理的、系統提醒不送；沒綁對話的任務／待辦通知由 tasks、reminders 處理（改狀態、驗收），也不送——
// 只收打字回答的任務問句（typed，TSK-27）例外：回答的內容秘書要讀到（D-57「答案要送回秘書」），tasks 只把單送回進行中、
// 不轉話，所以也轉（DEV-156 U-05；session 送到那張單的工作 session，沒有就送給秘書）。
// 規則只在這裡一份，session 只看 relay（2026-11-01 以前 session 自己再寫一次來源清單）。
func (m *Module) relaysAnswer(n Notice) bool {
	return n.Answer != nil && n.Answer.Via != sdk.AnswerViaAuto && !m.ownedByModule(n.Source) && n.Source != "system.alert" && (n.Room != "" || n.Task == "" && n.Reminder == "" || n.Task != "" && n.Typed)
}
func (m *Module) Answer(ctx context.Context, id, text string, via AnswerVia, by string) (Notice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	return m.answer(ctx, id, text, via, by)
}
func (m *Module) answer(ctx context.Context, id, text string, via AnswerVia, by string) (Notice, error) {
	if strings.TrimSpace(text) == "" {
		return nil, platform.Invalid("text", "請填寫回答")
	}
	switch via {
	case sdk.AnswerViaButton, sdk.AnswerViaChat, sdk.AnswerViaAPI, sdk.AnswerViaAuto:
	default:
		return nil, platform.Invalid("via", "回答來源不正確")
	}
	n, err := m.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if n.State == sdk.NotificationStateAnswered {
		return n, nil
	}
	if n.State == sdk.NotificationStateExpired {
		return nil, platform.Conflict("這則通知已過期")
	}
	n.Answer = &sdk.Answer{Text: text, Via: via, At: m.d.Clock.Now().In(m.config.Location())}
	n.State = sdk.NotificationStateAnswered
	n, err = m.save(ctx, n, by)
	if err != nil {
		return nil, err
	}
	m.publish(ctx, "notice.answered", map[string]any{"id": n.ID, "text": text, "via": string(via), "agent": n.Agent, "room": n.Room, "task": n.Task, "todo": n.Todo, "reminder": n.Reminder, "source": n.Source, "by": by, "relay": m.relaysAnswer(n)})
	m.changed(ctx, n)
	return cloneNotice(n), nil
}
func (m *Module) MarkRead(ctx context.Context, ids []string, by string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return err
	}
	for _, id := range ids {
		n, err := m.get(ctx, id)
		if err != nil {
			return err
		}
		if n.State != sdk.NotificationStateUnread {
			continue
		}
		n.State = sdk.NotificationStateRead
		n, err = m.save(ctx, n, by)
		if err != nil {
			return err
		}
		m.changed(ctx, n)
	}
	return nil
}

// Archive 是看板的「刪除」（NTF-40）：只有使用者本人；訊息封存、不刪檔。主人說刪就刪，還沒回答的
// 「要你同意」也一樣（NTF-41，作者 10-28）；skipped 保留給 API 相容，現在一律是空的。找不到的（已經封存、打錯）略過。封存後更新未讀數。
func (m *Module) Archive(ctx context.Context, ids []string, by string) (archived, skipped []string, err error) {
	if !platform.IsOwner(by) {
		return nil, nil, platform.Forbidden("只有主人能刪訊息")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, nil, err
	}
	var last Notice
	for _, id := range ids {
		n, err := m.get(ctx, id)
		if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
			continue
		}
		if err != nil {
			return archived, skipped, err
		}
		if m.records[id].Archived {
			continue
		}
		if err := m.d.Vault.ArchiveNow(ctx, "message", id, by); err != nil {
			return archived, skipped, err
		}
		delete(m.records, id)
		archived = append(archived, id)
		last = n
	}
	if last != nil {
		m.changed(ctx, last)
	}
	return archived, skipped, nil
}

func (m *Module) Watch(ctx context.Context) (<-chan *sdk.NotifyWatchResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	unread, err := m.unreadCount(ctx)
	if err != nil {
		return nil, err
	}
	ch := make(chan *sdk.NotifyWatchResponse, 64)
	ch <- &sdk.NotifyWatchResponse{Unread: int32(unread)}
	m.watchers[ch] = ctx
	return ch, nil
}
func (m *Module) changed(ctx context.Context, n Notice) { m.broadcast(ctx, n, nil) }

// broadcast 把一則通知的變動（或提醒的變動，P7-6）送給所有 Watch 的人。
func (m *Module) broadcast(ctx context.Context, n Notice, nudge *sdk.Nudge) {
	unread, err := m.unreadCount(ctx)
	if err != nil {
		m.warn("無法更新未讀數")
		return
	}
	for ch, watchCtx := range m.watchers {
		if watchCtx.Err() != nil {
			close(ch)
			delete(m.watchers, ch)
			continue
		}
		select {
		case ch <- &sdk.NotifyWatchResponse{Notification: cloneNotice(n), Unread: int32(unread), Nudge: nudge}:
		default:
			close(ch)
			delete(m.watchers, ch)
		}
	}
}

// maxShareBytes is the largest file Share accepts.
const maxShareBytes = 25 << 20

// Share copies a file into runtime/share so the board can serve it at /share/<name>.
// The file is read outside m.mu; only the ready check and the write hold the lock.
func (m *Module) Share(ctx context.Context, path string) (Attachment, error) {
	m.mu.Lock()
	err := m.ready(ctx)
	home := m.home
	var secrets, public string
	if m.config != nil {
		secrets = m.config.SecretsPath()
		public = m.config.PublicBase()
	}
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	data, err := readShareable(path, home, secrets)
	if err != nil {
		return nil, err
	}
	name := platform.NewID() + "-" + platform.ShareName(filepath.Base(path))
	dir := platform.SharesDir(home)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, platform.Unavailable("無法建立分享目錄")
	}
	if err := platform.AtomicWrite(filepath.Join(dir, name), data, 0600); err != nil {
		return nil, platform.Unavailable("無法儲存分享檔案")
	}
	return &sdk.ShareResponse{URL: "/share/" + name, Mime: mime.TypeByExtension(filepath.Ext(path)), PublicBase: public}, nil
}

// readShareable resolves symlinks first, applies the deny list to the real path, and only then
// opens the file. Error messages never carry a path.
func readShareable(path, home, secrets string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, platform.Invalid("path", "請指定要分享的檔案")
	}
	if !filepath.IsAbs(path) {
		return nil, platform.Invalid("path", "請用完整路徑")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, platform.NotFound("找不到這個檔案")
	}
	if shareDenied(path, real, home, secrets) {
		return nil, platform.Forbidden("這個檔案不能分享")
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, platform.Unavailable("無法讀取分享檔案")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, platform.Unavailable("無法讀取分享檔案")
	}
	if !info.Mode().IsRegular() {
		return nil, platform.Invalid("path", "只能分享檔案")
	}
	if info.Size() > maxShareBytes {
		return nil, platform.Invalid("path", "檔案太大（上限 25 MB）")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxShareBytes+1))
	if err != nil {
		return nil, platform.Unavailable("無法讀取分享檔案")
	}
	if len(data) > maxShareBytes {
		return nil, platform.Invalid("path", "檔案太大（上限 25 MB）")
	}
	return data, nil
}

// shareDenied reports whether the resolved file is a secret: the home's secrets file, anything under
// the home's settings/, the home's secrets folder (outside the home in layout v2), the usual credential folders under the user's home, or a .env file. It compares
// by file identity (os.SameFile), not by spelling, so a case-insensitive disk (macOS) or a hard link
// cannot sneak a secret past it.
func shareDenied(given, real, home, secrets string) bool {
	for _, p := range []string{given, real} {
		base := strings.ToLower(filepath.Base(p))
		if base == ".env" || strings.HasPrefix(base, ".env.") {
			return true
		}
	}
	info, err := os.Stat(real)
	if err != nil {
		return true
	}
	if secrets != "" {
		if s, err := os.Stat(secrets); err == nil && os.SameFile(info, s) {
			return true
		}
	}
	paths := []string{filepath.Join(home, "settings"), platform.LayoutFor(home).Secrets()}
	if u, err := os.UserHomeDir(); err == nil && u != "" {
		paths = append(paths, filepath.Join(u, ".ssh"), filepath.Join(u, ".gnupg"), filepath.Join(u, ".aws"), filepath.Join(u, ".config", "gh"))
	}
	var roots []os.FileInfo
	for _, p := range paths {
		if r, err := os.Stat(p); err == nil {
			roots = append(roots, r)
		}
	}
	for dir := filepath.Dir(real); ; dir = filepath.Dir(dir) {
		if d, err := os.Stat(dir); err == nil {
			for _, r := range roots {
				if os.SameFile(d, r) {
					return true
				}
			}
		}
		if parent := filepath.Dir(dir); parent == dir {
			return false
		}
	}
}
func (m *Module) Tick(ctx context.Context, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return
	}
	m.reviewNudges(ctx, now)
	if now.Sub(m.lastSweep) >= time.Minute {
		m.lastSweep = now
		m.sweepClosedTasks(ctx)
	}
	if now.Sub(m.lastOrphanSweep) >= SweepEvery {
		m.lastOrphanSweep = now
		m.sweepOrphans(ctx, now)
	}
	for key, b := range m.buckets {
		if now.Before(b.at.Add(time.Duration(b.hours) * time.Hour)) {
			continue
		}
		titles := []string{}
		for _, n := range b.notices {
			titles = append(titles, n.Title)
		}
		first := b.notices[0]
		_, err := m.send(ctx, &sdk.Notification{Category: sdk.CategoryDigest, Title: "通知摘要", Context: strings.Join(titles, "\n"), Agent: first.Agent, Account: first.Account, By: "system", Lane: sdk.NotifyLaneAgent}, true) // 桶裡都是可以推的，摘要也在秘書那道（NTF-58）
		if err != nil {
			m.warn("通知摘要建立失敗")
			continue
		}
		delete(m.buckets, key)
	}
	ns, err := m.list(ctx, []sdk.NotificationState{sdk.NotificationStateAnswered}, "", false)
	if err != nil {
		m.warn("無法檢查回答投遞")
		return
	}
	for _, value := range ns {
		n := cloneNotice(&value)
		if !m.tracksAnswer(n) || !n.Answer.DeliveredAt.IsZero() || now.Before(n.Answer.At.Add(2*time.Hour)) {
			continue
		}
		n.State = sdk.NotificationStateFailed
		n, err = m.save(ctx, n, "system")
		if err != nil {
			m.warn("無法標記回答投遞失敗")
			continue
		}
		m.changed(ctx, n)
		_, err = m.send(ctx, &sdk.Notification{Category: sdk.CategoryApprove, Title: "回答尚未送達，請重新回答", Context: n.Title, Todo: n.Todo, Options: n.Options, Typed: n.Typed, Agent: n.Agent, Room: n.Room, Task: n.Task, Reminder: n.Reminder, By: "system"}, false)
		if err != nil {
			m.warn("無法建立重新回答通知")
		}
	}
	for ch, watchCtx := range m.watchers {
		if watchCtx.Err() != nil {
			close(ch)
			delete(m.watchers, ch)
		}
	}
}
func (m *Module) handle(ctx context.Context, ev *sdk.BusSubscribeResponse) error {
	if ev == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return err
	}
	for _, src := range m.sources {
		if src.Changed != "" && ev.Topic == src.Changed {
			// 業務模組登記的那件事變了（例 Goal 不再是草稿、權限申請不再待審）：照它現在說的收掉不用回的通知（NTF-49）。
			return m.retireSettled(ctx, src, m.d.Clock.Now())
		}
	}
	str := func(key string) string { s, _ := ev.Payload[key].(string); return s }
	switch ev.Topic {
	case "system.alert":
		return m.alert(ctx, str("title"), str("context"), str("agent"))
	case platform.TopicSessionTurnFailed, platform.TopicSessionEnded:
		// 引擎回報、只有主人親手能解的（登入失效、登出、帳單；S5-3）；其他種類在那個對話裡顯示就好，不推。
		if a, ok := ownerFixable(ev.Topic, ev.Payload, m.alertScope()); ok {
			return m.engineAlert(ctx, a, str("agent"))
		}
		return nil
	case platform.TopicSessionRecovered:
		// 出過登入失效／帳單錯的對話又正常做完一輪：那個帳號好了，這個帳號的那兩則提醒收掉（S5-5）。
		if !recoveredFromOwnerFixable(ev.Payload) {
			return nil
		}
		account := strings.TrimSpace(str("account"))
		legacy := legacyTitles(ev.Payload, m.homeLabel())
		return m.retireAlerts(ctx, func(n *sdk.Notification) bool {
			return n.Account == account && (n.Title == reloginTitle || n.Title == billingTitle) || slices.Contains(legacy, n.Title)
		})
	case platform.TopicSessionStuck:
		return m.stuckAlert(ctx, ev.Payload) // 給她的話送不進去，核心試過也沒通（NTF-68）
	case platform.TopicSessionUnstuck:
		return m.stuckCleared(ctx, ev.Payload)
	case "bus.envelope.state":
		// 主人在那個工作 session 的討論串回話：該 session 的提醒一起收掉（D-34）。
		// 只看新訊息送出的那一刻（queued）：更早的訊息之後才走到「已回覆」這類狀態變化，不算這次的回答。
		// 綁在對話上的等待（帶 key，選擇題、權限確認）不算：打字不是回答那題，題目還在就還在等（DEV-49 S2）。
		if str("state") == "queued" && str("kind") == "chat" && platform.IsOwner(str("from")) && str("to") != "" {
			m.resolveNudgesWhere(ctx, func(n *sdk.Nudge) bool { return n.Session == str("to") && n.Key == "" })
		}
		if str("state") != "received" || str("notice") == "" {
			return nil
		}
		return m.answerDelivered(ctx, str("notice"))
	case "session.approval.resolved":
		ids := []string{}
		switch values := ev.Payload["notices"].(type) {
		case []string:
			ids = values
		case []any:
			for _, value := range values {
				id, ok := value.(string)
				if !ok {
					return platform.Invalid("notices", "通知編號必須是字串陣列")
				}
				ids = append(ids, id)
			}
		default:
			return platform.Invalid("notices", "通知編號必須是字串陣列")
		}
		for _, id := range ids {
			if err := m.answerDelivered(ctx, id); err != nil {
				return err
			}
		}
		return nil
	case "task.changed", "reminder.changed":
		kind := strings.TrimSuffix(ev.Topic, ".changed")
		// 「從哪來」看事件帶的 from（tasks、reminders 都帶改之前的狀態）；notify 不讀 task／reminder 資料。
		id, status, old := str("id"), str("status"), str("from")
		if id == "" || status == "" {
			return nil
		}
		// 單離開「等你回覆」：那張單的提醒收掉（回答、驗收、改狀態都算，D-34）。
		if kind == "task" && old == "waiting_user" && status != "waiting_user" {
			m.resolveNudgesWhere(ctx, func(n *sdk.Nudge) bool { return n.Task == id })
		}
		should := false
		if kind == "task" {
			// 看狀態、不只看邊（DEV-26）：任務一結束（完成、不做了）不管從哪個狀態來，掛在它上面的訊息都收掉；
			// 離開等你（回覆或驗收，2026-11-01 驗收併進等你）也收。
			should = taskClosed(status) || old == "waiting_user" && status != "waiting_user"
		} else {
			should = old == "asking" && status != "asking" || status == "done" || status == "cancelled"
		}
		if !should {
			return nil
		}
		return m.closeLinked(ctx, kind, id)
	}
	return nil
}

// taskClosed：任務已經不會再有下文（掛在它上面的訊息沒有意義了）。
func taskClosed(status string) bool {
	switch status {
	case "done", "cancelled":
		return true
	}
	return false
}

// closeLinked 把掛在某件事（任務或待辦）上、還沒回的訊息標成已處理。呼叫者持有 m.mu。
func (m *Module) closeLinked(ctx context.Context, kind, id string) error {
	ns, err := m.list(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false)
	if err != nil {
		return err
	}
	for _, n := range ns {
		if kind == "task" && n.Task == id || kind == "reminder" && n.Reminder == id {
			if _, err := m.answer(ctx, n.ID, "已處理", sdk.AnswerViaAuto, "system"); err != nil {
				return err
			}
		}
	}
	return nil
}

// sweepClosedTasks：沒收到 task.changed 也會結束的單（例如直接改檔、直接刪檔），每分鐘看一次掛著訊息的單，結束了或找不到了就收掉訊息。
// 單的狀態經 SetTasks 接上的 tasks 讀（沒接就不看）。呼叫者持有 m.mu；tasks.Get 不拿鎖、不回頭叫 notify。
func (m *Module) sweepClosedTasks(ctx context.Context) {
	if m.tasks == nil {
		return
	}
	ns, err := m.list(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false)
	if err != nil {
		return
	}
	missing, next := m.missingTasks, map[string]bool{}
	defer func() { m.missingTasks = next }()
	for _, n := range ns {
		if n.Task == "" {
			continue
		}
		t, err := m.tasks.Get(ctx, n.Task)
		// 單本身不見了（檔案被直接刪掉）也算結束，不然訊息永遠掛著、看板也刪不掉（NTF-11）。
		// 外部工具先刪後寫時檔案會短暫不在，所以要連續兩輪都找不到才收。
		if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
			if !missing[n.Task] {
				next[n.Task] = true
				continue
			}
		} else if err != nil || t == nil || !taskClosed(string(t.Status)) {
			continue
		}
		if _, err := m.answer(ctx, n.ID, "已處理", sdk.AnswerViaAuto, "system"); err != nil {
			m.warn("無法收掉已結束任務的訊息")
		}
	}
}

// answerDelivered shares the idempotent receipt write for both delivery paths.
// The caller holds m.mu.
func (m *Module) answerDelivered(ctx context.Context, id string) error {
	n, err := m.get(ctx, id)
	if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if !m.tracksAnswer(n) || !n.Answer.DeliveredAt.IsZero() {
		return nil
	}
	n.Answer.DeliveredAt = m.d.Clock.Now().In(m.config.Location())
	n, err = m.save(ctx, n, "system")
	if err == nil {
		m.changed(ctx, n)
	}
	return err
}

// alert 把一則系統提醒轉成通知。同一件事還有沒處理的提醒就不再發一則（NTF-43）：套件反覆重開、
// 同一個帳號好幾位秘書一起登入失效，都只留一則。呼叫者持有 m.mu。
func (m *Module) alert(ctx context.Context, title, body, agent string) error {
	if open, err := m.openAlert(ctx, title, ""); err != nil || open {
		return err
	}
	_, err := m.send(ctx, &sdk.Notification{Category: sdk.CategoryApprove, Title: title, Context: body, Todo: "請看一下", Options: []string{"知道了"}, Agent: agent, By: "system", Source: "system.alert"}, false)
	return err
}

// engineAlert：引擎回報、只有主人親手能解的提醒（NTF-54）。同一個帳號同一種事還沒處理就不再發。
// 發生什麼與要主人做什麼都寫在內文（Context）、不另外填「請你」：推播顯示的就是內文開頭那一句（哪位秘書、發生什麼）。
func (m *Module) engineAlert(ctx context.Context, a engineAlert, agent string) error {
	if open, err := m.openAlert(ctx, a.title, a.account); err != nil || open {
		return err
	}
	_, err := m.send(ctx, &sdk.Notification{Category: sdk.CategoryApprove, Title: a.title, Context: a.body, Todo: noTodo, Options: []string{"知道了"}, Agent: agent, Account: a.account, By: "system", Source: "system.alert"}, false)
	return err
}

// retireAlerts：還沒處理的系統提醒裡 match 說要收的，收掉（記回答 auto「已經不用回了」並封存，跟 Retire 一樣）。呼叫者持有 m.mu。
func (m *Module) retireAlerts(ctx context.Context, match func(*sdk.Notification) bool) error {
	open, err := m.list(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false)
	if err != nil {
		return err
	}
	for i := range open {
		n := &open[i]
		if n.Source == "system.alert" && match(n) {
			if err := m.retire(ctx, n.ID, RetireReason); err != nil {
				return err
			}
		}
	}
	return nil
}

// openAlert：同標題、同帳號的系統提醒還沒處理（未讀或已讀、沒封存）。一般的系統提醒沒有帳號（空字串），只看標題（NTF-43）。
func (m *Module) openAlert(ctx context.Context, title, account string) (bool, error) {
	open, err := m.list(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false)
	if err != nil {
		return false, err
	}
	for _, n := range open {
		if n.Source == "system.alert" && n.Title == title && n.Account == account {
			return true, nil
		}
	}
	return false, nil
}

// noticeBody 組通知文字（NTF-7、NTF-62）：`[職位｜帳號] 類型｜標題`＋`背景：…`＋`請你：…`。
// 空的部分不寫：沒有帳號就是 `[code]`（不留「code｜」後面一個空），背景、請你沒內容就不寫那一行。
func noticeBody(agent, account, label, title, context, todo string) string {
	var head []string
	for _, p := range []string{agent, account} {
		if p != "" {
			head = append(head, p)
		}
	}
	var b strings.Builder
	if len(head) > 0 {
		b.WriteString("[" + strings.Join(head, "｜") + "] ")
	}
	b.WriteString(label + "｜" + title)
	if context != "" {
		b.WriteString("\n背景：" + context)
	}
	if todo != "" {
		b.WriteString("\n請你：" + todo)
	}
	return b.String()
}
