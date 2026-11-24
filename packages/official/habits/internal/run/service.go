// Package run 是 habits 套件行程要做的事：讀 vault 的資料、用 rules 算規則、把結果寫回 vault、賺到新點數就通知。
// 只經 SDK 的 client 呼叫核心（PKC-N1、PKC-N3、PKC-N4）；不做 RPC，頁面與秘書讀寫的是同一份 vault 資料（D-28）。
package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/packages/official/habits/internal/rules"
	"github.com/minglight/evergenie/sdk"
)

// vault 種類（完整名稱 `<套件 id>.<種類>`，D-28）。
const (
	KindHabit   = "habits.habit"
	KindCheckin = "habits.checkin"
	KindStatus  = "habits.status"
	KindWallet  = "habits.wallet"
	// KindExchange：主人在待辦頁按「兌換」寫一筆，程式用一份待辦點數換 1 個錢包點（改價前按的 20、之後 30，HAB-18、DEV-149）。
	KindExchange = "habits.exchange"
	// KindWelcome：秘書第一次跟主人介紹過習慣了（done，秘書寫），或這個家不用介紹（skipped，程式寫）；只用 main（HAB-21）。
	KindWelcome = "habits.welcome"

	kindWorkout = "fitness.workout"
	kindBody    = "body.entry"
	kindMeal    = "nutrition.meal"
	kindUnlock  = "closet.unlock"
	// kindClosetState：衣櫃程式寫的檢視（只有 main）；讀得到＝衣櫃套件在用（closetOn）。
	kindClosetState = "closet.state"
	// kindOutfit：衣櫃的衣服目錄；有上架、要點數的衣服，點數才換得到東西（outfitsReady）。
	kindOutfit = "closet.outfit"
	kindRemind = "reminder"

	walletID  = "main"
	welcomeID = "main"
	actor     = "habits"
	weeksKept = 8 // status 留幾週的週結果給詳情頁的格子

	// pointsURL：點數拿去哪裡用——衣櫃的展示間（HAB-9、HAB-20）。通知上的按鈕「到展示間」開這裡。
	pointsURL = "/p/closet/showroom"
	// habitsURL：點數還換不到衣服的家（衣櫃沒在用、或還沒有上架要點數的衣服），通知的按鈕「到習慣頁」開這裡——
	// 展示間在那種家是空頁或只說衣服包還沒裝（HAB-9、HAB-23）。
	habitsURL = "/p/habits/list"
)

// WatchedKinds 是有變就要重算的種類。closet.state、closet.outfit：衣櫃開關、衣服上下架會改錢包的 outfits_ready（HAB-23）。
var WatchedKinds = []string{KindHabit, KindCheckin, KindExchange, kindWorkout, kindBody, kindMeal, kindUnlock, kindClosetState, kindOutfit, kindRemind}

// Vault 是 Service 用到的 vault 能力（真的由 sdk.VaultAPI 提供，測試用假的）。
type Vault interface {
	// List 列出一個種類；沒註冊的種類（套件沒裝）回空。
	List(ctx context.Context, kind string, includeArchived bool) ([]sdk.Record, error)
	// Get 找不到回 nil, nil。
	Get(ctx context.Context, kind, id string) (*sdk.Record, error)
	// Put 整筆寫入；expectedRevision 空＝必須是新的。
	Put(ctx context.Context, rec sdk.Record, expectedRevision string) error
	// Watch 訂閱變更，每次有變呼叫 onChange；跑到 ctx 結束或連線斷掉為止。
	Watch(ctx context.Context, kinds []string, onChange func()) error
}

// Notifier 送通知給使用者（只回報分類，送不送由 notify 決定，D-14）。
type Notifier interface {
	Send(ctx context.Context, req *sdk.NotifySendRequest) error
}

// Service 是 habits 行程的本體。
type Service struct {
	V        Vault
	N        Notifier
	Now      func() time.Time // 要回本機時區的時間
	Log      *slog.Logger
	Debounce time.Duration // 有變之後等多久才重算（預設 2 秒）
	Retry    time.Duration // 訂閱斷線後等多久重連（預設 3 秒）
}

// NewService 組一個 Service；log 寫到 w（核心把行程的 stderr 收進 out.log）。
func NewService(v Vault, n Notifier, w io.Writer) *Service {
	return &Service{V: v, N: n, Now: time.Now, Log: slog.New(slog.NewTextHandler(w, nil)), Debounce: 2 * time.Second, Retry: 3 * time.Second}
}

// Run：開機先算一次，之後有變就去抖重算，另外每天 00:05（本機時區）重算一次（週次推進）。跑到 ctx 結束。
func (s *Service) Run(ctx context.Context) error {
	s.recompute(ctx)
	trigger := make(chan struct{}, 1)
	poke := func() {
		select {
		case trigger <- struct{}{}:
		default:
		}
	}
	go s.watchLoop(ctx, poke)

	var debounce <-chan time.Time
	var dtimer *time.Timer
	daily := time.NewTimer(time.Until(NextDaily(s.Now())))
	defer daily.Stop()
	for {
		select {
		case <-ctx.Done():
			if dtimer != nil {
				dtimer.Stop()
			}
			return nil
		case <-trigger:
			if dtimer != nil {
				dtimer.Stop()
			}
			dtimer = time.NewTimer(s.Debounce)
			debounce = dtimer.C
		case <-debounce:
			debounce = nil
			s.recompute(ctx)
		case <-daily.C:
			s.recompute(ctx)
			daily.Reset(time.Until(NextDaily(s.Now())))
		}
	}
}

// NextDaily 是下一個本機時間 00:05。
func NextDaily(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (s *Service) watchLoop(ctx context.Context, poke func()) {
	for ctx.Err() == nil {
		err := s.V.Watch(ctx, WatchedKinds, poke)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.Log.Error("訂閱 vault 變更失敗，稍後重連", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.Retry):
		}
		poke() // 重連之後補算一次，斷線期間的變更才不會漏
	}
}

func (s *Service) recompute(ctx context.Context) {
	if err := s.Recompute(ctx); err != nil && ctx.Err() == nil {
		s.Log.Error("重算失敗", "err", err)
	}
}

// Recompute 讀全部資料、算每個習慣、寫 status 與 wallet（內容沒變不寫）、新賺到點就通知。
func (s *Service) Recompute(ctx context.Context) error {
	now := s.Now()
	today := rules.DayOf(now)
	loc := now.Location()

	habitRecs, err := s.V.List(ctx, KindHabit, false)
	if err != nil {
		return fmt.Errorf("讀習慣失敗：%w", err)
	}
	checkinRecs, err := s.V.List(ctx, KindCheckin, false)
	if err != nil {
		return fmt.Errorf("讀打卡失敗：%w", err)
	}
	var habits []rules.Habit
	for _, r := range habitRecs {
		h, err := rules.HabitFromFields(r.ID, r.Fields, rules.DayOf(r.UpdatedAt.In(loc)))
		if err != nil {
			s.Log.Error("習慣的設定不合格，略過", "habit", r.ID, "err", err)
			continue
		}
		habits = append(habits, h)
	}
	sort.Slice(habits, func(i, j int) bool { return seqOf(habits[i].ID) < seqOf(habits[j].ID) })

	in := rules.Inputs{Checkins: parseCheckins(checkinRecs, s.Log)}
	s.loadAuto(ctx, habits, &in, loc)
	active := rules.ActiveCheckins(in.Checkins)

	wallet, err := s.V.Get(ctx, KindWallet, walletID)
	if err != nil {
		return fmt.Errorf("讀錢包失敗：%w", err)
	}
	var oldLedger map[string]int
	starter := rules.StarterBook{OldWallet: wallet != nil}
	if wallet != nil {
		oldLedger = ledgerOf(wallet.Fields["ledger"])
		starter.Old, starter.OldHabit = text(wallet.Fields["starter"]), text(wallet.Fields["starter_habit"])
	}
	if len(habits) > 0 {
		starter.First = habits[0].ID
	}
	// 錢包在、卻沒有 starter：起手禮上線前就在用的家（這次寫錢包之後就分不出來了，所以介紹與起手禮都在寫錢包之前定）。
	upgraded := wallet != nil && starter.Old == ""
	// 先記「不用介紹」：它只看升級前與有沒有習慣，不用等下面讀封存的習慣；那裡出錯也已經記好了。
	if err := s.settleWelcome(ctx, upgraded || len(habitRecs) > 0); err != nil {
		// 沒記下來就不寫錢包：下次重算還認得出是升級前的家。
		return fmt.Errorf("寫第一次介紹的紀錄失敗：%w", err)
	}
	if upgraded {
		had, err := s.hadHabits(ctx, len(checkinRecs) > 0 || len(oldLedger) > 0)
		if err != nil {
			return err
		}
		starter.HadHabits = had
	}
	spent, ready := s.spent(ctx), s.outfitsReady(ctx)

	states := make([]rules.State, 0, len(habits))
	for _, h := range habits {
		states = append(states, rules.Compute(h, active, in, today))
	}
	requested, readOK := s.exchanges(ctx)
	book := rules.TaskBook{Read: s.taskPoints(ctx), Requested: requested, Unread: !readOK}
	if wallet != nil {
		if n, ok := number(wallet.Fields["task_points"]); ok {
			book.Old = int(n)
		}
		if n, ok := number(wallet.Fields["task_exchanged"]); ok {
			book.OldExchanged = int(n)
		}
		if n, ok := number(wallet.Fields["task_legacy"]); ok {
			book.Legacy, book.HasLegacy = int(n), true
		}
		if n, ok := number(wallet.Fields["task_wallet"]); ok {
			book.OldWallet, book.HasWallet = int(n), true
		}
		if n, ok := number(wallet.Fields["task_old_requests"]); ok {
			book.OldRequests, book.HasOld = int(n), true
		}
	}
	starterState, starterHabit, gift := rules.Starter(starter)
	bank := rules.Bank(states, oldLedger, spent, book).WithStarter(starterState, starterHabit)

	computedAt := now.Format(time.RFC3339)
	for _, st := range states {
		if err := s.writeIfChanged(ctx, KindStatus, strings.ToLower(st.Habit.ID), statusFields(st, bank.Banked[st.Habit.ID]), computedAt); err != nil {
			s.Log.Error("寫習慣狀態失敗", "habit", st.Habit.ID, "err", err)
		}
	}
	if err := s.writeIfChanged(ctx, KindWallet, walletID, walletFields(bank, ready), computedAt); err != nil {
		// 錢包沒寫成就不通知：下次重算會再發現新的點數，同一點不會漏也不會發兩次。
		return fmt.Errorf("寫錢包失敗：%w", err)
	}
	if gift {
		s.notifyStarter(ctx, habits[0], ready)
	}
	for _, st := range states {
		id := st.Habit.ID
		if delta := bank.Banked[id] - oldLedger[id]; delta > 0 {
			s.notify(ctx, st, delta, bank.Points, oldLedger[id], ready)
		}
	}
	return nil
}

// pointsLink：通知上的站內按鈕（NTF-66）。ready＝點數換得到衣服（outfitsReady）：到展示間；不然到習慣頁（HAB-9、HAB-23）。
func pointsLink(ready bool) *sdk.NotificationLink {
	if ready {
		return &sdk.NotificationLink{Label: "到展示間", URL: pointsURL}
	}
	return &sdk.NotificationLink{Label: "到習慣頁", URL: habitsURL}
}

// notify：賺到點的通知。ready＝點數換得到衣服（outfitsReady）：說「可以換衣服」、按鈕到展示間；不然只說現在有幾點、按鈕到習慣頁（HAB-9）。
func (s *Service) notify(ctx context.Context, st rules.State, delta, points, old int, ready bool) {
	msg := fmt.Sprintf("%s 達標累積 %d 週，現在有 %d 點", st.Habit.Name, st.Earned.Hits, points)
	switch {
	case st.Earned.JustBonused(): // 這次多的點裡有連續獎勵
		msg = fmt.Sprintf("%s 連續達標 %d 週，加 %d 點，現在有 %d 點", st.Habit.Name, st.Earned.Streak, delta, points)
	case old >= st.Earned.Base: // 累計那份早就拿過：多出來的是補發的連續獎勵（例：規則剛上線、改了連續週數）
		msg = fmt.Sprintf("%s 補上連續達標獎勵 %d 點，現在有 %d 點", st.Habit.Name, delta, points)
	}
	if ready {
		msg += "可以換衣服"
	}
	req := &sdk.NotifySendRequest{
		Agent:    st.Habit.Agent,
		Category: sdk.CategoryDone,
		Title:    fmt.Sprintf("賺到 %d 點", delta),
		Context:  msg,
		Link:     pointsLink(ready),
	}
	if err := s.N.Send(ctx, req); err != nil {
		s.Log.Error("通知失敗", "habit", st.Habit.ID, "err", err)
	}
}

// notifyStarter：家裡第一個習慣約好了，起手禮送出去（HAB-20）。只在錢包寫好之後送一次：錢包的 starter 已經是 given，下次不會再送。
// 點數還換不到衣服的家（ready＝false：衣櫃沒在用、或還沒有上架要點數的衣服）不說換衣服，只說之後用得到、按鈕到習慣頁（HAB-23）。
func (s *Service) notifyStarter(ctx context.Context, h rules.Habit, ready bool) {
	msg := fmt.Sprintf("約好第一個習慣「%s」，送你 %d 點，之後有衣服可以換時用得到。", h.Name, rules.StarterPoints)
	if ready {
		msg = fmt.Sprintf("約好第一個習慣「%s」，送你 %d 點，可以到展示間換一套。", h.Name, rules.StarterPoints)
	}
	req := &sdk.NotifySendRequest{
		Agent:    h.Agent,
		Category: sdk.CategoryDone,
		Title:    fmt.Sprintf("送你 %d 點", rules.StarterPoints),
		Context:  msg,
		Link:     pointsLink(ready),
	}
	if err := s.N.Send(ctx, req); err != nil {
		s.Log.Error("通知失敗", "habit", h.ID, "err", err)
	}
}

// hadHabits：起手禮上線前的錢包，看這個家以前有沒有過習慣（含封存的）；known 是已經知道有（打卡、點數帳）。
// 讀不到就回錯：寧可這次不寫錢包，也不要把用過習慣的家當成新家補送。
func (s *Service) hadHabits(ctx context.Context, known bool) (bool, error) {
	if known {
		return true, nil
	}
	all, err := s.V.List(ctx, KindHabit, true)
	if err != nil {
		return false, fmt.Errorf("讀全部習慣（含封存）失敗：%w", err)
	}
	return len(all) > 0, nil
}

// settleWelcome：這個家不用秘書介紹習慣（升級前就在用、或已經有習慣）時，記一筆 skipped，套件給秘書的「第一次介紹」那段就不再帶（HAB-21）。
// 已經有紀錄（秘書介紹過、之前記過）就不動；同時被秘書寫掉了也算記好了。
func (s *Service) settleWelcome(ctx context.Context, skip bool) error {
	if !skip {
		return nil
	}
	cur, err := s.V.Get(ctx, KindWelcome, welcomeID)
	if err != nil || cur != nil {
		return err
	}
	err = s.V.Put(ctx, sdk.Record{Kind: KindWelcome, ID: welcomeID, Fields: map[string]any{"status": "skipped"}}, "")
	if e, ok := err.(*sdk.Error); ok && e.Code == sdk.ErrorCodeConflict {
		return nil
	}
	return err
}

// loadAuto 讀自動來源要用的種類；只讀有習慣用到的。讀不到（套件沒裝、出錯）就當沒有。
func (s *Service) loadAuto(ctx context.Context, habits []rules.Habit, in *rules.Inputs, loc *time.Location) {
	var needWorkout, needReminder, needWeight, needMeal bool
	for _, h := range habits {
		for _, src := range h.Sources {
			needWorkout = needWorkout || strings.HasPrefix(src, "fitness:")
			needReminder = needReminder || strings.HasPrefix(src, "reminder:")
		}
		for _, sub := range h.Subtasks {
			needWeight = needWeight || sub.Source == rules.SubBodyWeight
			needMeal = needMeal || sub.Source == rules.SubNutritionMeal
		}
	}
	list := func(kind string, archived bool) []sdk.Record {
		recs, err := s.V.List(ctx, kind, archived)
		if err != nil {
			s.Log.Warn("讀不到自動來源的資料，當作沒有", "kind", kind, "err", err)
			return nil
		}
		return recs
	}
	if needWorkout {
		for _, r := range list(kindWorkout, false) {
			if d, ok := rules.ParseDay(text(r.Fields["date"])); ok {
				in.Workouts = append(in.Workouts, rules.Workout{Type: text(r.Fields["type"]), Date: d, Done: text(r.Fields["status"]) == "完成"})
			}
		}
	}
	if needReminder {
		in.Reminders = map[string]rules.Day{}
		for _, r := range list(kindRemind, true) {
			if text(r.Fields["status"]) != "done" {
				continue
			}
			if d, ok := dayIn(text(r.Fields["closed_at"]), loc); ok {
				in.Reminders[r.ID] = d
			}
		}
	}
	if needWeight {
		in.WeightDays = map[rules.Day]bool{}
		for _, r := range list(kindBody, false) {
			d, ok := rules.ParseDay(text(r.Fields["date"]))
			if !ok {
				d, ok = rules.ParseDay(r.ID)
			}
			if _, has := number(r.Fields["weight"]); ok && has {
				in.WeightDays[d] = true
			}
		}
	}
	if needMeal {
		in.MealCounts = map[rules.Day]int{}
		for _, r := range list(kindMeal, false) {
			if d, ok := rules.ParseDay(text(r.Fields["date"])); ok {
				in.MealCounts[d]++
			}
		}
	}
}

// taskPoints 是全部待辦（含封存）累積的點數（done_points，核心在完成時給，HAB-13）。讀不到當 0，錢包舊帳取 max 不會少。
func (s *Service) taskPoints(ctx context.Context) int {
	recs, err := s.V.List(ctx, kindRemind, true)
	if err != nil {
		s.Log.Warn("讀不到待辦，這次不更新待辦點數", "err", err)
		return 0
	}
	total := 0
	for _, r := range recs {
		if n, ok := number(r.Fields["done_points"]); ok && n > 0 {
			total += int(n)
		}
	}
	return total
}

// exchanges 是主人按了幾次兌換（含封存的，封存不退回）。讀不到當 0，錢包舊帳取 max 不會少。
// 第二個值是讀到了沒：讀不到時改價前的筆數（task_old_requests）這次不定，等讀得到再記（DEV-149）。
func (s *Service) exchanges(ctx context.Context) (int, bool) {
	recs, err := s.V.List(ctx, KindExchange, true)
	if err != nil {
		s.Log.Warn("讀不到兌換紀錄，這次不兌換", "err", err)
		return 0, false
	}
	return len(recs), true
}

// spent 是衣櫃花掉的點數：每筆 closet.unlock 花 cost（沒寫就是 1 點）。closet 套件沒裝就是 0。
func (s *Service) spent(ctx context.Context) int {
	recs, err := s.V.List(ctx, kindUnlock, false)
	if err != nil {
		s.Log.Warn("讀不到衣櫃的兌換紀錄，當作沒花點數", "err", err)
		return 0
	}
	total := 0
	for _, r := range recs {
		if n, ok := number(r.Fields["cost"]); ok && n > 0 {
			total += int(n)
		} else {
			total++
		}
	}
	return total
}

// closetOn：衣櫃套件在不在用——讀得到它的程式寫的 closet.state/main 才算。套件停用、沒裝、被拒時那個種類沒註冊、
// 讀不到（資料留在原地）；剛啟用、程式還沒算完第一輪的那幾秒也讀不到，一樣當不在用（連回習慣頁不會是空頁）。
// 核心不認得套件，這裡照原本讀對方資料的方式判斷（vault:pkg/closet），不另開通道。
func (s *Service) closetOn(ctx context.Context) bool {
	r, err := s.V.Get(ctx, kindClosetState, "main")
	return err == nil && r != nil && !r.Archived
}

// outfitsReady：點數現在真的換得到衣服嗎（HAB-23）——衣櫃在用（closetOn），而且這個家有上架、要點數的衣服
// （closet.outfit：沒封存、state = active、cost > 0；官方衣服包裝好的、主人自己加、設了點數的都算，closet CLO-2）。
// 只看衣服本身，不看角色包狀態：這一版有帶衣服包不等於已經裝好（下載中、上次沒成功時展示間還是空的，CLO-25）。
// 這是唯一的判斷：寫進錢包的 outfits_ready，通知、習慣頁、今天頁、給秘書的補充都照它說要不要提換衣服。讀不到當沒有（寧可少說）。
func (s *Service) outfitsReady(ctx context.Context) bool {
	if !s.closetOn(ctx) {
		return false
	}
	recs, err := s.V.List(ctx, kindOutfit, false)
	if err != nil {
		s.Log.Warn("讀不到衣櫃的衣服，當作還沒有衣服可換", "err", err)
		return false
	}
	for _, r := range recs {
		if n, ok := number(r.Fields["cost"]); ok && n > 0 && text(r.Fields["state"]) == "active" {
			return true
		}
	}
	return false
}

// writeIfChanged：欄位（不含 computed_at）跟現有的一樣就不寫；否則整筆寫入（換版本號時重試一次）。
func (s *Service) writeIfChanged(ctx context.Context, kind, id string, fields map[string]any, computedAt string) error {
	for attempt := 0; ; attempt++ {
		cur, err := s.V.Get(ctx, kind, id)
		if err != nil {
			return err
		}
		revision := ""
		if cur != nil {
			if sameFields(cur.Fields, fields) {
				return nil
			}
			revision = cur.Revision
		}
		out := make(map[string]any, len(fields)+1)
		for k, v := range fields {
			out[k] = v
		}
		out["computed_at"] = computedAt
		err = s.V.Put(ctx, sdk.Record{Kind: kind, ID: id, Fields: out}, revision)
		if err == nil || attempt >= 1 {
			return err
		}
		if e, ok := err.(*sdk.Error); !ok || e.Code != sdk.ErrorCodeConflict {
			return err
		}
	}
}

func sameFields(old, now map[string]any) bool {
	a := map[string]any{}
	for k, v := range old {
		switch k {
		case "computed_at", "schema", "id", "kind", "created_at", "updated_at", "by", "archived_at":
		default:
			a[k] = v
		}
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(now)
	return string(x) == string(y)
}

// statusFields 是 habits.status 的欄位（頁面只讀這個，不自己算點數）。
func statusFields(st rules.State, banked int) map[string]any {
	strs := func(in []rules.Day) []string {
		out := []string{}
		for _, d := range in {
			out = append(out, d.String())
		}
		return out
	}
	days := strs(st.ThisWeek.Days)
	weeks := st.Weeks
	if len(weeks) > weeksKept {
		weeks = weeks[len(weeks)-weeksKept:]
	}
	ws := []any{}
	for _, w := range weeks {
		ws = append(ws, map[string]any{"week": w.Week.String(), "count": w.Count, "result": w.Result})
	}
	subs := []any{}
	for _, t := range st.TodaySubs {
		subs = append(subs, map[string]any{"name": t.Name, "done": t.Done, "how": t.How})
	}
	return map[string]any{
		"habit":            st.Habit.ID,
		"streak_weeks":     st.Earned.Streak,
		"achieved_weeks":   st.Earned.Hits,
		"earned_points":    banked,
		"next_need_weeks":  st.Earned.Left(),
		"bonus_weeks":      st.Earned.BonusWeeks,
		"bonus_points":     st.Earned.Bonus,
		"next_bonus_weeks": st.Earned.BonusLeft(),
		"this_week":        map[string]any{"done": st.ThisWeek.Done, "need": st.ThisWeek.Need, "days": days},
		"weeks":            ws,
		"today_subs":       subs,
		"today_manual":     st.TodayManual,
		"done_days":        strs(st.DoneDays),
		"skipped_days":     strs(st.SkippedDays),
		"broken_days":      strs(st.BrokenDays),
		"manual_days":      strs(st.ManualDays),
	}
}

// walletFields：錢包的欄位；ready 是點數換不換得到衣服（outfits_ready，HAB-23）。
func walletFields(w rules.Wallet, ready bool) map[string]any {
	ids := make([]string, 0, len(w.Banked))
	for id := range w.Banked {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return seqOf(ids[i]) < seqOf(ids[j]) })
	ledger := []any{}
	for _, id := range ids {
		ledger = append(ledger, map[string]any{"habit": id, "points": w.Banked[id]})
	}
	out := map[string]any{"earned": w.Earned, "spent": w.Spent, "points": w.Points, "ledger": ledger, "task_points": w.Tasks, "task_exchanged": w.TaskExchanged, "task_legacy": w.TaskLegacy, "task_wallet": w.TaskWallet, "starter": w.Starter, "outfits_ready": ready}
	if !w.TaskOldUnset {
		out["task_old_requests"] = w.TaskOldRequests
	}
	if w.StarterHabit != "" {
		out["starter_habit"] = w.StarterHabit
	}
	return out
}

func ledgerOf(v any) map[string]int {
	out := map[string]int{}
	list, _ := v.([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		id, _ := m["habit"].(string)
		if n, ok := number(m["points"]); ok && id != "" {
			out[id] = int(n)
		}
	}
	return out
}

func parseCheckins(recs []sdk.Record, log *slog.Logger) []rules.Checkin {
	var out []rules.Checkin
	for _, r := range recs {
		d, ok := rules.ParseDay(text(r.Fields["date"]))
		habit := text(r.Fields["habit"])
		if !ok || habit == "" {
			log.Warn("打卡缺 habit 或 date，略過", "checkin", r.ID)
			continue
		}
		undo, _ := r.Fields["undo"].(bool)
		src := text(r.Fields["source"])
		if src == "" {
			src = "manual"
		}
		out = append(out, rules.Checkin{Seq: seqOf(r.ID), Habit: habit, Date: d, Sub: text(r.Fields["sub"]), Source: src, Key: text(r.Fields["key"]), Undo: undo})
	}
	return out
}

// seqOf：H-12 → 12、C-3 → 3；不是這個格式排最後。
func seqOf(id string) int {
	if _, n, ok := strings.Cut(id, "-"); ok {
		if v, err := strconv.Atoi(n); err == nil {
			return v
		}
	}
	return 1 << 30
}

func text(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// dayIn 把時間字串（RFC3339，或 YYYY-MM-DD 開頭）換成指定時區的日期。
func dayIn(s string, loc *time.Location) (rules.Day, bool) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return rules.DayOf(t.In(loc)), true
	}
	return rules.ParseDay(s)
}
