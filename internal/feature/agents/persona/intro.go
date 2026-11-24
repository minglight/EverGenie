package persona

// 新角色來打招呼（DEV-115，作者 2026-11-04）：角色包同步換進來的新官方角色（SyncResult.NewCards），
// 每位在同步當天白天隨機一個時間，用自己的頭像和口吻發一則主人訊息，附「去角色頁看看」的按鈕。
//
// 同步可能在別的行程（升級時跑的 `evergenie personas pack sync`），所以待送清單存檔
// （runtime/personas/intros.json，flock 保護、原子寫入）；serve 的定時工作 personas.intros 每分鐘看一次，
// 到時間的就送、送完拿掉——重開機不漏送（啟動後第一輪就補送過時的），也不重送。

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Notifier 是角色庫向通知要的：發一則主人訊息（組裝接 notify 的 Send；角色庫不 import notify）。
type Notifier interface {
	Notify(ctx context.Context, n *sdk.Notification) (*sdk.Notification, error)
}

// SetNotifier 接上通知（registry 在 Start 之前呼叫）；沒接＝待送的招呼留在清單裡，接上之後才送。
func (m *Module) SetNotifier(n Notifier) { m.setMu.Lock(); m.notifier = n; m.setMu.Unlock() }

func (m *Module) notices() Notifier {
	m.setMu.RLock()
	defer m.setMu.RUnlock()
	return m.notifier
}

const (
	introEvery      = time.Minute // 定時工作多久看一次待送清單
	introDayStart   = 10          // 白天的窗：10:00 起
	introDayEnd     = 21          // 到 21:00（不含）
	introLatestHour = 20          // 同步時已經過了 20:00 就排隔天（當天剩不到一小時）
	introButton     = "去角色頁看看"
)

// introGiveUp：排進來超過這麼久還送不出去（通知一直失敗）就放棄，免得每分鐘重試、log 一直長。
const introGiveUp = 7 * 24 * time.Hour

// introRand 是測試縫：回 [0, n) 的亂數。
var introRand = func(n int) int { return rand.IntN(n) }

var introSlug = personaref.SlugPattern

// pendingIntro 是待送清單的一筆：哪位、預定什麼時候。
type pendingIntro struct {
	Slug     string    `json:"slug"`
	At       time.Time `json:"at"`
	QueuedAt time.Time `json:"queued_at"`
}

type introQueue struct {
	Intros []pendingIntro `json:"intros"`
}

func introPath(home string) string { return platform.RuntimeDir(home, "personas", "intros.json") }

// introAt 算一位新角色的預定時間：同步當天（家的時區）10:00–21:00 之間隨機一分鐘，而且在 now 之後；
// now 已經過了 20:00 就排隔天的 10:00–21:00。
func introAt(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	y, mo, d := local.Date()
	if local.Hour() >= introLatestHour {
		y, mo, d = time.Date(y, mo, d+1, 12, 0, 0, 0, loc).Date()
	}
	// 用 time.Date 直接取那天的鐘點（不是當天 0 點加幾小時），日光節約切換那天也是牆上的 10:00、21:00。
	start := time.Date(y, mo, d, introDayStart, 0, 0, 0, loc)
	end := time.Date(y, mo, d, introDayEnd, 0, 0, 0, loc)
	if next := local.Truncate(time.Minute).Add(time.Minute); next.After(start) {
		start = next
	}
	minutes := int(end.Sub(start) / time.Minute)
	if minutes <= 0 { // 不會發生（20:00 以後已排隔天），保險起見
		return start
	}
	return start.Add(time.Duration(introRand(minutes)) * time.Minute)
}

// queueIntros 把這次同步多出的新角色排進待送清單（已經在清單裡的不重排）。
func queueIntros(home string, slugs []string, now time.Time) error {
	if len(slugs) == 0 {
		return nil
	}
	var loc *time.Location
	if h, err := platform.LoadHome(home); err == nil {
		loc = h.Location()
	}
	return withIntroQueue(home, func(q *introQueue) bool {
		changed := false
		for _, slug := range slugs {
			if !introSlug.MatchString(slug) || slices.ContainsFunc(q.Intros, func(p pendingIntro) bool { return p.Slug == slug }) {
				continue
			}
			q.Intros = append(q.Intros, pendingIntro{Slug: slug, At: introAt(now, loc), QueuedAt: now})
			changed = true
		}
		return changed
	})
}

// withIntroQueue 鎖住待送清單（跨行程的 flock）、讀出來交給 fn，fn 回 true 才原子寫回。
func withIntroQueue(home string, fn func(q *introQueue) bool) error {
	p := introPath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return platform.Unavailable("無法建立 runtime/personas")
	}
	lock, err := os.OpenFile(p+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return platform.Unavailable("無法鎖住新角色的待送清單")
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return platform.Unavailable("無法鎖住新角色的待送清單")
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	var q introQueue
	data, err := os.ReadFile(p)
	switch {
	case err == nil:
		if json.Unmarshal(data, &q) != nil {
			q = introQueue{} // 壞掉的清單當空的（執行期資料，重寫一份）
		}
	case !errors.Is(err, os.ErrNotExist):
		return platform.Unavailable("讀不了新角色的待送清單")
	}
	if !fn(&q) {
		return nil
	}
	out, _ := json.MarshalIndent(q, "", "  ")
	return platform.AtomicWrite(p, append(out, '\n'), 0o644)
}

// sendDueIntros 是定時工作 personas.intros：到時間的招呼送出去，送成功的從清單拿掉；送失敗的留著下一輪再試；
// 卡已經不在、或格式壞了的直接拿掉（沒人可以打招呼）；排進來超過 introGiveUp 還送不出去的放棄並記一行 log。
// 通知還沒接上就什麼都不做。送通知時不握清單的鎖（推播可能要等十幾秒，別讓同時在跑的同步排隊）：
// 先在鎖裡挑出要送的、送完再鎖一次拿掉送成功的。
func (m *Module) sendDueIntros(ctx context.Context) error {
	notifier := m.notices()
	if notifier == nil || m.home == "" {
		return nil
	}
	if _, err := os.Stat(introPath(m.home)); err != nil {
		return nil // 沒有待送的（大部分時候）：不開鎖
	}
	now := m.d.Clock.Now()
	type due struct {
		p pendingIntro
		n *sdk.Notification
	}
	var todo []due
	var gaveUp []string
	err := withIntroQueue(m.home, func(q *introQueue) bool {
		kept := q.Intros[:0:0]
		for _, p := range q.Intros {
			switch n, ok := m.introNotice(p.Slug); {
			case !ok:
				// 卡不在了：不送、拿掉
			case !p.QueuedAt.IsZero() && now.Sub(p.QueuedAt) > introGiveUp:
				gaveUp = append(gaveUp, p.Slug)
			case p.At.After(now):
				kept = append(kept, p)
			default:
				todo = append(todo, due{p, n})
				kept = append(kept, p) // 送成功後才拿掉
			}
		}
		changed := len(kept) != len(q.Intros)
		q.Intros = kept
		return changed
	})
	if err != nil {
		return err
	}
	if len(gaveUp) > 0 {
		m.introLog().Warn("新角色的招呼排了超過 7 天都送不出去，放棄", "cards", gaveUp)
	}
	var firstErr error
	var sent []pendingIntro
	for _, d := range todo {
		if _, err := notifier.Notify(ctx, d.n); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue // 留著，下一輪再試
		}
		sent = append(sent, d.p)
	}
	if len(sent) > 0 {
		if err := withIntroQueue(m.home, func(q *introQueue) bool {
			before := len(q.Intros)
			q.Intros = slices.DeleteFunc(q.Intros, func(p pendingIntro) bool {
				return slices.ContainsFunc(sent, func(s pendingIntro) bool { return s.Slug == p.Slug && s.At.Equal(p.At) })
			})
			return len(q.Intros) != before
		}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Module) introLog() *slog.Logger {
	if h, err := platform.LoadHome(m.home); err == nil {
		return platform.NewLogger(h, "personas-pack")
	}
	return slog.Default()
}

// introNotice 組一位新角色的招呼：主人訊息、opening、她的頭像與名字（speaker），內文是她的自我介紹，
// 按鈕開到「角色」頁那張卡（DEV-117 起不在設定裡）。只看官方卡（角色包只裝官方卡）。
func (m *Module) introNotice(slug string) (*sdk.Notification, bool) {
	if !introSlug.MatchString(slug) {
		return nil, false
	}
	data, err := os.ReadFile(m.cardPath(personaref.SourceOfficial, slug))
	if err != nil {
		return nil, false
	}
	rec, err := parseCard(data)
	if err != nil {
		return nil, false
	}
	name, _ := rec.Fields["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}
	intro, _ := rec.Fields["intro"].(string)
	text := strings.TrimSpace(intro)
	if text == "" {
		text = composeIntro(name, rec.Body)
	}
	return &sdk.Notification{
		Category: sdk.CategoryOpening,
		Agent:    "system",
		By:       "system",
		Lane:     sdk.NotifyLaneAgent,
		Title:    name + " 來打招呼",
		Context:  text,
		Todo:     "無",
		Image:    "/avatars/persona/" + slug + ".png",
		Speaker:  name,
		Link:     &sdk.NotificationLink{Label: introButton, URL: "/personas/" + url.PathEscape(slug)},
	}, true
}

// composeIntro 在卡上沒有 intro 時組一段自我介紹：「說話」段的開場白第一句＋「我是<name>，<個性的一句話>。」。
// 兩樣都沒有就只有「我是<name>。」。
func composeIntro(name, body string) string {
	opening := firstQuote(lineAfter(body, "開場白："))
	oneLine := strings.TrimSpace(lineAfter(body, "一句話："))
	oneLine = strings.TrimRight(oneLine, "。.!！ ")
	var b strings.Builder
	if opening != "" {
		b.WriteString(opening)
	}
	if oneLine != "" {
		b.WriteString("我是" + name + "，" + oneLine + "。")
	} else {
		b.WriteString("我是" + name + "。")
	}
	return b.String()
}

// lineAfter 找內文裡第一個含 key 的那一行，回 key 之後的文字（去掉清單符號與前後空白）；找不到回空字串。
func lineAfter(body, key string) string {
	for _, line := range strings.Split(body, "\n") {
		if _, rest, ok := strings.Cut(line, key); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// firstQuote 回「…」裡的第一句（沒有引號就整段）；結尾沒有標點的補一個「。」。
func firstQuote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if _, rest, ok := strings.Cut(s, "「"); ok {
		if q, _, ok := strings.Cut(rest, "」"); ok {
			s = q
		}
	} else if before, _, ok := strings.Cut(s, "；"); ok {
		s = before
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if r := []rune(s); !strings.ContainsRune("。！？!?…～~", r[len(r)-1]) {
		s += "。"
	}
	return s
}
