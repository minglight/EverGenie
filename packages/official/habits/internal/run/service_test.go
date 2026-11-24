package run

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// fakeVault 是記憶體裡的 vault：照種類存、每次寫入換版本號。
type fakeVault struct {
	mu      sync.Mutex
	recs    map[string]map[string]sdk.Record
	rev     int
	puts    map[string]int   // kind → 寫了幾次
	failPut map[string]error // kind → 寫入時回這個錯
	events  chan struct{}
}

// newFakeVault：預設是點數換得到衣服的家（衣櫃程式寫的 closet.state/main、一套上架要 1 點的衣服，HAB-23）；
// 要測衣櫃停用就 drop 掉 closet.state、要測還沒有衣服可換就 drop 掉 closet.outfit 的 O-1。
func newFakeVault() *fakeVault {
	f := &fakeVault{recs: map[string]map[string]sdk.Record{}, puts: map[string]int{}, events: make(chan struct{}, 16)}
	f.seed(kindClosetState, "main", map[string]any{"points": 0})
	f.seed(kindOutfit, "O-1", map[string]any{"persona": "luna", "name": "睡衣", "cost": 1.0, "state": "active"})
	return f
}

func linkOf(req *sdk.NotifySendRequest) string {
	if req.Link == nil {
		return ""
	}
	return req.Link.Label + " " + req.Link.URL
}

func (f *fakeVault) seed(kind, id string, fields map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recs[kind] == nil {
		f.recs[kind] = map[string]sdk.Record{}
	}
	f.rev++
	f.recs[kind][id] = sdk.Record{Kind: kind, ID: id, Fields: fields, Revision: fmt.Sprint(f.rev), UpdatedAt: time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)}
}

func (f *fakeVault) drop(kind, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.recs[kind], id)
}

// archive 像 `evergenie data rm`：紀錄留著、標成封存（列表預設不列）。
func (f *fakeVault) archive(kind, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.recs[kind][id]
	r.Archived = true
	f.recs[kind][id] = r
}

func (f *fakeVault) List(_ context.Context, kind string, includeArchived bool) ([]sdk.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sdk.Record
	for _, r := range f.recs[kind] {
		if r.Archived && !includeArchived {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeVault) Get(_ context.Context, kind, id string) (*sdk.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[kind][id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (f *fakeVault) Put(_ context.Context, rec sdk.Record, expected string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failPut[rec.Kind]; err != nil {
		return err
	}
	cur, exists := f.recs[rec.Kind][rec.ID]
	if (!exists && expected != "") || (exists && cur.Revision != expected) {
		return &sdk.Error{Code: sdk.ErrorCodeConflict, Message: "版本不符"}
	}
	if f.recs[rec.Kind] == nil {
		f.recs[rec.Kind] = map[string]sdk.Record{}
	}
	f.rev++
	rec.Revision = fmt.Sprint(f.rev)
	f.recs[rec.Kind][rec.ID] = rec
	f.puts[rec.Kind]++
	return nil
}

func (f *fakeVault) Watch(ctx context.Context, _ []string, onChange func()) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-f.events:
			onChange()
		}
	}
}

type fakeNotifier struct {
	mu   sync.Mutex
	sent []*sdk.NotifySendRequest
}

func (n *fakeNotifier) Send(_ context.Context, req *sdk.NotifySendRequest) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, req)
	return nil
}

func (n *fakeNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.sent)
}

func newSvc(v *fakeVault, n *fakeNotifier, now time.Time) *Service {
	s := NewService(v, n, io.Discard)
	s.Now = func() time.Time { return now }
	s.Debounce = 10 * time.Millisecond
	s.Retry = 10 * time.Millisecond
	return s
}

func weeklyHabit() map[string]any {
	return map[string]any{"name": "重訓", "agent": "fitness", "cadence": "weekly", "target": 1.0, "mode": "cumulative", "start": "2026-10-05", "status": "active"}
}

func checkin(date string) map[string]any {
	return map[string]any{"habit": "H-1", "date": date, "source": "manual", "key": "manual:H-1:" + date + ":"}
}

func statusOf(t *testing.T, v *fakeVault, id string) map[string]any {
	t.Helper()
	r, _ := v.Get(context.Background(), KindStatus, id)
	if r == nil {
		t.Fatalf("沒有 %s 的狀態", id)
	}
	return r.Fields
}

// TestNotifyLinksHabitsWhenNoOutfits：點數還換不到衣服的家（HAB-23）——衣櫃套件停用（讀不到它的 closet.state）、
// 衣櫃在用但沒有上架要點數的衣服（只有送的基本款、草稿、下架、封存的）——起手禮與賺到點的通知不提展示間與換衣服、
// 按鈕到習慣頁（固定連展示間會點到空頁）；錢包的 outfits_ready 是 false。
func TestNotifyLinksHabitsWhenNoOutfits(t *testing.T) {
	cases := map[string]func(v *fakeVault){
		"衣櫃停用": func(v *fakeVault) { v.drop(kindClosetState, "main") },
		"沒有衣服": func(v *fakeVault) { v.drop(kindOutfit, "O-1") },
		"只有送的、草稿、下架、封存的": func(v *fakeVault) {
			v.seed(kindOutfit, "O-1", map[string]any{"persona": "luna", "name": "基本", "cost": 0.0, "state": "active"})
			v.seed(kindOutfit, "O-2", map[string]any{"persona": "luna", "name": "草稿", "cost": 2.0, "state": "draft"})
			v.seed(kindOutfit, "O-3", map[string]any{"persona": "luna", "name": "舊的", "cost": 2.0, "state": "retired"})
			v.seed(kindOutfit, "O-4", map[string]any{"persona": "luna", "name": "刪掉的", "cost": 2.0, "state": "active"})
			v.archive(kindOutfit, "O-4")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			v, n := newFakeVault(), &fakeNotifier{}
			setup(v)
			now := time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local)
			h := weeklyHabit()
			h["name"] = "睡前讀書"
			v.seed(KindHabit, "H-1", h)
			v.seed(KindCheckin, "C-1", checkin("2026-10-05"))
			v.seed(KindCheckin, "C-2", checkin("2026-10-12"))
			s := newSvc(v, n, now)
			if err := s.Recompute(context.Background()); err != nil {
				t.Fatal(err)
			}
			if n.count() != 2 {
				t.Fatalf("起手禮與賺到點各一則，實際 %d 則", n.count())
			}
			for _, got := range n.sent {
				if linkOf(got) != "到習慣頁 /p/habits/list" || got.URL != "" || strings.Contains(got.Context, "展示間") || strings.Contains(got.Context, "換衣服") {
					t.Errorf("換不到衣服時按鈕要到習慣頁、不提展示間：%+v", got)
				}
			}
			if got := n.sent[0]; got.Title != "送你 1 點" || got.Context != "約好第一個習慣「睡前讀書」，送你 1 點，之後有衣服可以換時用得到。" {
				t.Errorf("起手禮的通知：%+v", got)
			}
			if got := n.sent[1]; got.Title != "賺到 1 點" || got.Context != "睡前讀書 達標累積 2 週，現在有 2 點" {
				t.Errorf("賺到點的通知：%+v", got)
			}
			if w := walletOf(t, v); w["outfits_ready"] != false {
				t.Errorf("錢包要記換不到衣服：%v", w["outfits_ready"])
			}
		})
	}
}

// TestOutfitsReadyFollowsCloset：衣服上架、下架會改錢包的 outfits_ready（HAB-23）；衣櫃的兩個種類也在重算清單裡，
// 改了會觸發重算（不用等下一次打卡）。
func TestOutfitsReadyFollowsCloset(t *testing.T) {
	v, n := newFakeVault(), &fakeNotifier{}
	seedNoStarter(v)
	ctx := context.Background()
	s := newSvc(v, n, time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local))
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if w := walletOf(t, v); w["outfits_ready"] != true {
		t.Fatalf("有上架要點數的衣服：%v", w)
	}
	v.seed(kindOutfit, "O-1", map[string]any{"persona": "luna", "name": "睡衣", "cost": 1.0, "state": "retired"})
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if w := walletOf(t, v); w["outfits_ready"] != false {
		t.Fatalf("下架了：%v", w)
	}
	for _, kind := range []string{kindClosetState, kindOutfit} {
		found := false
		for _, k := range WatchedKinds {
			found = found || k == kind
		}
		if !found {
			t.Errorf("%s 要在重算清單裡", kind)
		}
	}
}

// seedNoStarter：起手禮已經不送的家（這幾個測試看的是別的點數規則，不讓起手禮的 1 點混進來）。
func seedNoStarter(v *fakeVault) {
	v.seed(KindWallet, "main", map[string]any{"starter": "none"})
}

func walletOf(t *testing.T, v *fakeVault) map[string]any {
	t.Helper()
	r, _ := v.Get(context.Background(), KindWallet, "main")
	if r == nil {
		t.Fatal("沒有錢包")
	}
	return r.Fields
}

// TestNewPointNotifiesOnce：兩週達標賺到第 1 點，寫 status 與 wallet、通知一次（分類 done、agent＝習慣的 agent）；
// 再重算不會再通知、內容沒變不重寫；點數帳只增不減（習慣被刪、規則改嚴都留著）；衣櫃花點數從錢包扣。
// Guards: REQ-124
func TestNewPointNotifiesOnce(t *testing.T) {
	v, n := newFakeVault(), &fakeNotifier{}
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local) // 週一，第三週的開始
	v.seed(KindHabit, "H-1", weeklyHabit())
	v.seed(KindCheckin, "C-1", checkin("2026-10-05"))
	v.seed(KindCheckin, "C-2", checkin("2026-10-12"))
	v.seed(KindHabit, "H-2", map[string]any{"name": "壞掉的", "agent": "life"}) // 設定不合格：略過，不影響別的習慣
	seedNoStarter(v)
	s := newSvc(v, n, now)
	ctx := context.Background()

	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	st := statusOf(t, v, "h-1")
	if st["earned_points"] != 1 || st["achieved_weeks"] != 2 || st["streak_weeks"] != 2 || st["next_need_weeks"] != 4 {
		t.Fatalf("status：%v", st)
	}
	if st["bonus_weeks"] != 4 || st["bonus_points"] != 0 || st["next_bonus_weeks"] != 2 {
		t.Fatalf("連續獎勵欄位：%v", st)
	}
	if st["habit"] != "H-1" || st["computed_at"] == "" {
		t.Fatalf("status 欄位：%v", st)
	}
	if tw := st["this_week"].(map[string]any); tw["done"] != 0 || tw["need"] != 1 {
		t.Fatalf("this_week：%v", tw)
	}
	if weeks := st["weeks"].([]any); len(weeks) != 3 {
		t.Fatalf("weeks：%v", weeks)
	}
	w := walletOf(t, v)
	if w["earned"] != 1 || w["spent"] != 0 || w["points"] != 1 {
		t.Fatalf("wallet：%v", w)
	}
	if n.count() != 1 {
		t.Fatalf("通知 %d 則", n.count())
	}
	got := n.sent[0]
	if got.Category != sdk.CategoryDone || got.Agent != "fitness" || got.Title != "賺到 1 點" || got.Context != "重訓 達標累積 2 週，現在有 1 點可以換衣服" || linkOf(got) != "到展示間 /p/closet/showroom" || got.URL != "" {
		t.Fatalf("通知內容：%+v", got)
	}
	if _, bad := v.recs[KindStatus]["h-2"]; bad {
		t.Fatal("不合格的習慣不該有狀態")
	}

	// 再算一次：不通知、不重寫（computed_at 不算內容）。
	statusPuts, walletPuts := v.puts[KindStatus], v.puts[KindWallet]
	s.Now = func() time.Time { return now.Add(time.Hour) }
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if n.count() != 1 || v.puts[KindStatus] != statusPuts || v.puts[KindWallet] != walletPuts {
		t.Fatalf("重算不該有動作：通知 %d、status 寫 %d→%d、wallet 寫 %d→%d", n.count(), statusPuts, v.puts[KindStatus], walletPuts, v.puts[KindWallet])
	}

	// 重開程式（新的 Service、同一份 vault）也不會再通知：以點數帳為準。
	if err := newSvc(v, n, now).Recompute(ctx); err != nil || n.count() != 1 {
		t.Fatalf("重開之後：%v 通知 %d", err, n.count())
	}

	// 規則改嚴（每週 7 次）：連續週數歸零，賺到的點不縮水；習慣被刪：帳上的點留在錢包。
	f := weeklyHabit()
	f["target"] = 7.0
	v.seed(KindHabit, "H-1", f)
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	st = statusOf(t, v, "h-1")
	if st["earned_points"] != 1 || st["streak_weeks"] != 0 {
		t.Fatalf("改嚴之後：%v", st)
	}
	v.drop(KindHabit, "H-1")
	v.drop(KindHabit, "H-2")
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if w := walletOf(t, v); w["points"] != 1 || w["earned"] != 1 {
		t.Fatalf("習慣刪掉之後：%v", w)
	}
	// 衣櫃花掉 1 點（沒寫 cost 就是 1 點）→ 錢包 0 點，不會是負的。
	v.seed(kindUnlock, "U-1", map[string]any{"outfit": "x"})
	v.seed(kindUnlock, "U-2", map[string]any{"outfit": "y", "cost": 2.0})
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if w := walletOf(t, v); w["spent"] != 3 || w["points"] != 0 || w["earned"] != 1 {
		t.Fatalf("花點數之後：%v", w)
	}
	if n.count() != 1 {
		t.Fatalf("最後通知 %d 則", n.count())
	}
}

// TestStarterGiftOnce：新家（HAB-20）——還沒約習慣時錢包記 waiting、不送也不通知；第一個習慣約好送 1 點、
// 通知一次（白話、按鈕到展示間、給約的那位秘書）；重算、重開程式、刪掉再約、結束後重約都不再送；花掉了也不收回。
func TestStarterGiftOnce(t *testing.T) {
	v, n := newFakeVault(), &fakeNotifier{}
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local)
	ctx := context.Background()
	s := newSvc(v, n, now)
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if w := walletOf(t, v); w["starter"] != "waiting" || w["earned"] != 0 || w["points"] != 0 {
		t.Fatalf("新家還沒約：%v", w)
	}
	if n.count() != 0 {
		t.Fatalf("還沒約不該通知：%d", n.count())
	}
	if r, _ := v.Get(ctx, KindWelcome, "main"); r != nil {
		t.Fatalf("新家還沒有習慣：不記「不用介紹」，留給秘書介紹一次：%v", r.Fields)
	}

	h := weeklyHabit()
	h["name"], h["agent"] = "睡前讀書", "life"
	v.seed(KindHabit, "H-1", h)
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	w := walletOf(t, v)
	if w["starter"] != "given" || w["starter_habit"] != "H-1" || w["earned"] != 1 || w["points"] != 1 {
		t.Fatalf("第一個習慣約好：%v", w)
	}
	if ledger := w["ledger"].([]any); len(ledger) != 0 {
		t.Fatalf("起手禮不算進習慣自己的點數帳：%v", ledger)
	}
	if n.count() != 1 {
		t.Fatalf("通知 %d 則", n.count())
	}
	if got := n.sent[0]; got.Title != "送你 1 點" || got.Context != "約好第一個習慣「睡前讀書」，送你 1 點，可以到展示間換一套。" || linkOf(got) != "到展示間 /p/closet/showroom" || got.URL != "" || got.Agent != "life" || got.Category != sdk.CategoryDone {
		t.Fatalf("起手禮的通知：%+v", got)
	}
	if r, _ := v.Get(ctx, KindWelcome, "main"); r == nil || r.Fields["status"] != "skipped" {
		t.Fatal("已經約了習慣：記「不用介紹」")
	}

	// 重算、重開程式：不再送。
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if err := newSvc(v, n, now.Add(time.Hour)).Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	// 刪掉再約、結束後重約：不再送。
	v.archive(KindHabit, "H-1")
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	h2 := weeklyHabit()
	h2["status"] = "ended"
	v.seed(KindHabit, "H-2", h2)
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	h2["status"] = "active"
	v.seed(KindHabit, "H-2", h2)
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if w := walletOf(t, v); w["earned"] != 1 || w["starter_habit"] != "H-1" || n.count() != 1 {
		t.Fatalf("只送一次：%v，通知 %d 則", w, n.count())
	}
	// 拿去展示間換掉了：earned 照算、點數用完，不會再補。
	v.seed(kindUnlock, "U-1", map[string]any{"outfit": "x"})
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if w := walletOf(t, v); w["earned"] != 1 || w["spent"] != 1 || w["points"] != 0 || w["starter"] != "given" || n.count() != 1 {
		t.Fatalf("花掉之後：%v，通知 %d 則", w, n.count())
	}
}

// TestStarterAndWelcomeSkipOldHomes：起手禮上線前就在用的家（錢包在、沒有 starter）升級後——有過習慣的（還在的、只剩封存的、
// 只剩打卡）不補送、不通知；沒約過習慣的照新家等第一個。兩種都記「不用介紹」（HAB-21），秘書不會突然介紹一次；秘書介紹過的紀錄不動。
func TestStarterAndWelcomeSkipOldHomes(t *testing.T) {
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local)
	ctx := context.Background()
	oldWallet := map[string]any{"earned": 1.0, "spent": 0.0, "points": 1.0, "ledger": []any{map[string]any{"habit": "H-1", "points": 1.0}}}
	for _, c := range []struct {
		name  string
		seed  func(v *fakeVault)
		state string
	}{
		{"還有習慣", func(v *fakeVault) {
			v.seed(KindWallet, "main", oldWallet)
			v.seed(KindHabit, "H-1", weeklyHabit())
		}, "none"},
		{"習慣都封存了、點數帳還在", func(v *fakeVault) {
			v.seed(KindWallet, "main", oldWallet)
			v.seed(KindHabit, "H-1", weeklyHabit())
			v.archive(KindHabit, "H-1")
		}, "none"},
		{"只剩封存的習慣", func(v *fakeVault) {
			v.seed(KindWallet, "main", map[string]any{"earned": 0.0})
			v.seed(KindHabit, "H-1", weeklyHabit())
			v.archive(KindHabit, "H-1")
		}, "none"},
		{"只剩打卡", func(v *fakeVault) {
			v.seed(KindWallet, "main", map[string]any{"earned": 0.0})
			v.seed(KindCheckin, "C-1", checkin("2026-10-05"))
		}, "none"},
		{"沒約過習慣", func(v *fakeVault) {
			v.seed(KindWallet, "main", map[string]any{"earned": 0.0})
		}, "waiting"},
	} {
		v, n := newFakeVault(), &fakeNotifier{}
		c.seed(v)
		before := 0
		if e, ok := v.recs[KindWallet]["main"].Fields["earned"].(float64); ok {
			before = int(e)
		}
		if err := newSvc(v, n, now).Recompute(ctx); err != nil {
			t.Fatal(err)
		}
		w := walletOf(t, v)
		if w["starter"] != c.state || w["earned"] != before || n.count() != 0 {
			t.Errorf("%s：%v，通知 %d 則", c.name, w, n.count())
		}
		if r, _ := v.Get(ctx, KindWelcome, "main"); r == nil || r.Fields["status"] != "skipped" {
			t.Errorf("%s：升級前就在用的家要記「不用介紹」", c.name)
		}
	}

	// 升級前沒約過習慣的家：之後約了第一個，照送。
	v, n := newFakeVault(), &fakeNotifier{}
	v.seed(KindWallet, "main", map[string]any{"earned": 0.0})
	s := newSvc(v, n, now)
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	v.seed(KindHabit, "H-1", weeklyHabit())
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if w := walletOf(t, v); w["starter"] != "given" || w["earned"] != 1 || n.count() != 1 {
		t.Fatalf("升級後約第一個：%v，通知 %d 則", w, n.count())
	}

	// 秘書已經介紹過（done）：程式不動那筆。
	v, n = newFakeVault(), &fakeNotifier{}
	v.seed(KindWelcome, "main", map[string]any{"status": "done", "agent": "life"})
	v.seed(KindHabit, "H-1", weeklyHabit())
	if err := newSvc(v, n, now).Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if r, _ := v.Get(ctx, KindWelcome, "main"); r.Fields["status"] != "done" || v.puts[KindWelcome] != 0 {
		t.Fatalf("介紹過的紀錄不動：%v", r.Fields)
	}
}

// TestWelcomeWriteFailureKeepsOldWallet：「不用介紹」記不下來時不寫錢包，下次重算還認得出是升級前的家（不會因為錢包先有了 starter 就當成新家）。
func TestWelcomeWriteFailureKeepsOldWallet(t *testing.T) {
	v, n := newFakeVault(), &fakeNotifier{}
	v.seed(KindWallet, "main", map[string]any{"earned": 0.0})
	v.seed(KindHabit, "H-1", weeklyHabit())
	v.failPut = map[string]error{KindWelcome: &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "暫時不行"}}
	s := newSvc(v, n, time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local))
	ctx := context.Background()
	if err := s.Recompute(ctx); err == nil {
		t.Fatal("記不下來要回錯")
	}
	if w := walletOf(t, v); w["starter"] != nil {
		t.Fatalf("錢包不能先寫：%v", w)
	}
	v.failPut = nil
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if w := walletOf(t, v); w["starter"] != "none" || n.count() != 0 {
		t.Fatalf("重試之後照升級前的家算：%v，通知 %d 則", w, n.count())
	}
}

// TestStarterWalletWriteFailureRetries：起手禮（HAB-20）錢包寫不成時不通知、不算送過；下次寫成了才送，而且只通知一則。
func TestStarterWalletWriteFailureRetries(t *testing.T) {
	v, n := newFakeVault(), &fakeNotifier{}
	v.seed(KindHabit, "H-1", weeklyHabit())
	v.failPut = map[string]error{KindWallet: &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "暫時不行"}}
	s := newSvc(v, n, time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local))
	ctx := context.Background()
	if err := s.Recompute(ctx); err == nil {
		t.Fatal("錢包寫不成要回錯")
	}
	if r, _ := v.Get(ctx, KindWallet, "main"); r != nil || n.count() != 0 {
		t.Fatalf("錢包寫不成：不該有錢包、不該通知（錢包 %v，通知 %d 則）", r, n.count())
	}
	v.failPut = nil
	for range 2 {
		if err := s.Recompute(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if w := walletOf(t, v); w["starter"] != "given" || w["earned"] != 1 {
		t.Fatalf("寫成了才算送：%v", w)
	}
	if n.count() != 1 || n.sent[0].Title != "送你 1 點" {
		t.Fatalf("起手禮只通知一則：%d %+v", n.count(), n.sent)
	}
}

// TestStreakBonusNotifies：連續達標 4 週拿到獎勵點，status 寫出獎勵欄位，通知說是連續獎勵；沒寫 bonus_weeks 就是 4。
func TestStreakBonusNotifies(t *testing.T) {
	v, n := newFakeVault(), &fakeNotifier{}
	v.seed(KindHabit, "H-1", weeklyHabit())
	seedNoStarter(v)
	for i, d := range []string{"2026-10-05", "2026-10-12", "2026-10-19"} {
		v.seed(KindCheckin, fmt.Sprintf("C-%d", i+1), checkin(d))
	}
	ctx := context.Background()
	if err := newSvc(v, n, time.Date(2026, 10, 26, 10, 0, 0, 0, time.Local)).Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	v.seed(KindCheckin, "C-4", checkin("2026-10-26"))
	if err := newSvc(v, n, time.Date(2026, 11, 2, 10, 0, 0, 0, time.Local)).Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	st := statusOf(t, v, "h-1")
	if st["streak_weeks"] != 4 || st["earned_points"] != 2 || st["bonus_weeks"] != 4 || st["bonus_points"] != 1 || st["next_bonus_weeks"] != 4 {
		t.Fatalf("status：%v", st)
	}
	if n.count() != 2 || n.sent[1].Context != "重訓 連續達標 4 週，加 1 點，現在有 2 點可以換衣服" {
		t.Fatalf("通知：%d %+v", n.count(), n.sent[len(n.sent)-1])
	}
}

// TestHabitsAutoSourcesFromVault：自動來源直接讀別的種類（fitness、reminder、body、meal）；讀不到的種類當沒有。
// Guards: REQ-124
func TestHabitsAutoSourcesFromVault(t *testing.T) {
	v, n := newFakeVault(), &fakeNotifier{}
	now := time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local) // 週三
	ctx := context.Background()
	f := weeklyHabit()
	f["target"] = 2.0
	f["sources"] = []any{"fitness:重訓", "reminder:R-5"}
	v.seed(KindHabit, "H-1", f)
	v.seed("fitness.workout", "W-1", map[string]any{"type": "重訓", "date": "2026-10-05", "status": "完成"})
	v.seed("fitness.workout", "W-2", map[string]any{"type": "重訓", "date": "2026-10-06", "status": "進行中"})
	v.seed("fitness.workout", "W-3", map[string]any{"type": "跑步", "date": "2026-10-06", "status": "完成"})
	local := time.Date(2026, 10, 7, 8, 30, 0, 0, time.Local).Format(time.RFC3339)
	v.seed("reminder", "R-5", map[string]any{"status": "done", "closed_at": local})
	v.seed("reminder", "R-6", map[string]any{"status": "done", "closed_at": local})
	sub := map[string]any{"name": "照顧身體", "agent": "nutrition", "cadence": "daily", "mode": "cumulative", "start": "2026-10-05", "status": "active",
		"subtasks": []any{"量體重=body:weight", "吃飯=nutrition:meals>=2"}}
	v.seed(KindHabit, "H-2", sub)
	v.seed("body.entry", "2026-10-07", map[string]any{"date": "2026-10-07", "weight": 72.4})
	v.seed("body.entry", "2026-10-06", map[string]any{"date": "2026-10-06", "fat": 18.0}) // 沒體重不算
	v.seed("nutrition.meal", "M-1", map[string]any{"date": "2026-10-07"})
	v.seed("nutrition.meal", "M-2", map[string]any{"date": "2026-10-07"})
	if err := newSvc(v, n, now).Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	tw := statusOf(t, v, "h-1")["this_week"].(map[string]any)
	if tw["done"] != 2 { // 重訓 W-1 一次＋待辦 R-5 完成一次
		t.Fatalf("H-1 本週：%v", tw)
	}
	if subs := statusOf(t, v, "h-2")["today_subs"].([]any); subs[0].(map[string]any)["how"] != "auto" || subs[1].(map[string]any)["done"] != true {
		t.Fatalf("H-2 今天的子項目：%v", subs)
	}
	if tw := statusOf(t, v, "h-2")["this_week"].(map[string]any); tw["done"] != 1 {
		t.Fatalf("H-2 本週：%v", tw)
	}
}

// TestHabitsRunRecomputesOnChange：跑起來之後，開機算一次，vault 有變就去抖重算（連續多次變更只算一次的量），ctx 結束乾淨離開。
// Guards: REQ-124
func TestHabitsRunRecomputesOnChange(t *testing.T) {
	v, n := newFakeVault(), &fakeNotifier{}
	v.seed(KindHabit, "H-1", weeklyHabit())
	now := time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local)
	s := newSvc(v, n, now)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	waitFor(t, "開機先算一次", func() bool { r, _ := v.Get(ctx, KindStatus, "h-1"); return r != nil })
	v.seed(KindCheckin, "C-1", checkin("2026-10-07"))
	for i := 0; i < 5; i++ {
		v.events <- struct{}{}
	}
	waitFor(t, "打卡之後本週進度變了", func() bool {
		r, _ := v.Get(ctx, KindStatus, "h-1")
		return r != nil && r.Fields["this_week"].(map[string]any)["done"] == 1
	})
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 結束後沒有離開")
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等不到：%s", what)
}

// TestNextDailyIsFiveAfterMidnight：每天 00:05（本機時區）重算。
// Guards: REQ-124
func TestNextDailyIsFiveAfterMidnight(t *testing.T) {
	loc := time.FixedZone("t", 8*3600)
	for _, c := range []struct{ now, want time.Time }{
		{time.Date(2026, 10, 25, 10, 0, 0, 0, loc), time.Date(2026, 10, 26, 0, 5, 0, 0, loc)},
		{time.Date(2026, 10, 25, 0, 1, 0, 0, loc), time.Date(2026, 10, 25, 0, 5, 0, 0, loc)},
		{time.Date(2026, 10, 25, 0, 5, 0, 0, loc), time.Date(2026, 10, 26, 0, 5, 0, 0, loc)},
		{time.Date(2026, 12, 31, 23, 59, 0, 0, loc), time.Date(2027, 1, 1, 0, 5, 0, 0, loc)},
	} {
		if got := NextDaily(c.now); !got.Equal(c.want) {
			t.Errorf("%v → %v，要 %v", c.now, got, c.want)
		}
	}
}
