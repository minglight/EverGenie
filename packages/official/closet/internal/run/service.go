// Package run 是 closet 套件行程要做的事：讀 vault 的資料、用 rules 算規則、處理兌換請求、維護公開資料夾、把結果寫回 vault。
// 只經 SDK 的 client 呼叫核心（PKC-N1、PKC-N3）；不做 RPC，頁面與秘書讀寫的是同一份 vault 資料（D-28）。
// 公開資料夾 <暫存>/packages/closet/public/ 是唯一直接寫的檔案（PKC-10），內容是可重建的衍生檔；
// <暫存> 是 sdk.RuntimeDir（第 1 版 <家>/runtime/，第 2 版在家外面的狀態資料夾，DEV-145）。
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
	"sync"
	"time"

	"github.com/minglight/evergenie/packages/official/closet/internal/photomigrate"
	"github.com/minglight/evergenie/packages/official/closet/internal/pub"
	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
	"github.com/minglight/evergenie/sdk"
)

// vault 種類（完整名稱 `<套件 id>.<種類>`，D-28）。
const (
	KindOutfit   = "closet.outfit"
	KindUnlock   = "closet.unlock"
	KindRotation = "closet.rotation"
	KindRequest  = "closet.request"
	KindPerson   = "closet.person"
	KindState    = "closet.state"
	KindVariant  = "closet.variant"
	KindPhoto    = "closet.photo"

	kindWallet = "habits.wallet"
	// kindMineRequest 是 closet-mine 套件的請求（它改了衣服目錄後，衣櫃要馬上重算）。
	kindMineRequest = "closet-mine.request"
	kindAgent       = "agent"

	stateID  = "main"
	walletID = "main"
	actor    = "closet"

	requestKeep = 7 * 24 * time.Hour // 已處理的請求保留幾天才封存
)

// WatchedKinds 是有變就要重算的種類。
// closet-mine.request 只當「有變就做一輪」，不解析內容（closet-mine 沒裝時這個種類不存在，watch 不會因此失敗）。
// closet.photo：頁面採用、closet-mine 上傳、角色包同步帶來的照片紀錄（DEV-160 S2）。
// closet.action 是別的套件登記的動作（CLO-33），有變就重算 closet.capabilities。
var WatchedKinds = []string{KindOutfit, KindPhoto, KindRotation, KindRequest, KindUnlock, KindVariant, kindWallet, kindAgent, kindMineRequest, KindAction}

// Vault 是 Service 用到的 vault 能力（真的由 sdk.VaultAPI 提供，測試用假的）。
type Vault interface {
	// List 列出一個種類；沒註冊的種類（套件沒裝）回空。
	List(ctx context.Context, kind string, includeArchived bool) ([]sdk.Record, error)
	// Get 找不到回 nil, nil。
	Get(ctx context.Context, kind, id string) (*sdk.Record, error)
	// Put 整筆寫入；expectedRevision 空＝必須是新的。
	Put(ctx context.Context, rec sdk.Record, expectedRevision string) error
	// Patch 只改指定欄位。
	Patch(ctx context.Context, kind, id string, set map[string]any) error
	// Archive 封存一筆。
	Archive(ctx context.Context, kind, id string) error
	// Watch 訂閱變更，每次有變呼叫 onChange；跑到 ctx 結束或連線斷掉為止。
	Watch(ctx context.Context, kinds []string, onChange func()) error
}

// Service 是 closet 行程的本體。
type Service struct {
	V        Vault
	Home     string           // 家的路徑
	Now      func() time.Time // 本機時區
	Log      *slog.Logger
	Debounce time.Duration // 有變之後等多久才再做一輪（預設 1 秒）
	Retry    time.Duration // 訂閱斷線後等多久重連（預設 3 秒）
	Rescan   time.Duration // 多久重算一次（有人把圖複製進家裡不會觸發 vault，這一輪補照片紀錄；預設 5 分鐘）
	// Adult 回傳家層級的「自己的隱藏版」開關（擴充點 closet.adult）是不是開著；nil 或讀不到都當關（寧可不顯示）。
	Adult func(ctx context.Context) bool
	// Dev 回傳這個家是不是開發家（DevHome 問核心）；讀不到回錯（這一輪照上一輪的能力、不自動上架，caps.go），nil＝沒有 edit。
	// 只有 capabilities（caps.go）用它換算成能力（CLO-32）：開發家多一組 edit，沒兌換的衣服也給真圖（CLO-27），擁有、點數、兌換的規則不變。
	Dev func(ctx context.Context) (bool, error)
	// EditTab 回傳「修改」分頁開關（擴充點 closet.edit-tab）是不是開著；nil 或讀不到都當開（見 caps.go editTabOn）。
	EditTab func(ctx context.Context) bool
	// Packages 列出家裡的套件（核心的 package.list）：驗別的套件登記的動作用（CLO-33）；nil 或讀不到就不列動作。
	Packages func(ctx context.Context) ([]sdk.Package, error)
	// AdultPoll 是多久看一次開關（預設 3 秒）：擴充點的值不在 vault，改了不會觸發訂閱；開關一變就馬上做一輪，關掉後幾秒內公開資料夾就沒有自己的隱藏版的圖。
	AdultPoll time.Duration

	mu       sync.Mutex
	logged   map[string]bool
	boundary time.Time // 下一個隱藏版時段的開頭或結尾（上一輪算的；零值＝沒有時段）
}

// NewService 組一個 Service；log 寫到 w（核心把行程的 stderr 收進 out.log）。
func NewService(v Vault, home string, w io.Writer) *Service {
	return &Service{V: v, Home: home, Now: time.Now, Log: slog.New(slog.NewTextHandler(w, nil)),
		Debounce: time.Second, Retry: 3 * time.Second, Rescan: 5 * time.Minute, AdultPoll: 3 * time.Second, logged: map[string]bool{}}
}

func homeFromEnv() (string, error) {
	home := sdk.Getenv("EVERGENIE_HOME")
	if home == "" {
		return "", fmt.Errorf("缺少 EVERGENIE_HOME：closet 要由 EverGenie 核心啟動")
	}
	return home, nil
}

func (s *Service) publicDir() string {
	return sdk.RuntimeDir(s.Home, "packages", "closet", "public")
}

// logOnce：同一個問題（key）只記一次 log，免得每 5 分鐘一輪都洗版。
func (s *Service) logOnce(key, msg string, args ...any) {
	s.mu.Lock()
	seen := s.logged[key]
	s.logged[key] = true
	s.mu.Unlock()
	if !seen {
		s.Log.Error(msg, args...)
	}
}

// Run：開機先做一輪；vault 有變就去抖再做一輪；每個整點做一輪（輪播換衣）；每 5 分鐘做一輪（發現複製進來的圖）；
// 隱藏版的時段一開始、一結束各做一輪（CLO-17）。跑到 ctx 結束。
func (s *Service) Run(ctx context.Context) error {
	var boundary <-chan time.Time
	var btimer *time.Timer
	round := func(hourly bool) {
		s.round(ctx, hourly)
		if btimer != nil {
			btimer.Stop()
		}
		boundary = nil
		s.mu.Lock()
		next := s.boundary
		s.mu.Unlock()
		if d, ok := BoundaryDelay(next, s.Now(), s.Retry); ok {
			btimer = time.NewTimer(d)
			boundary = btimer.C
		}
	}
	defer func() {
		if btimer != nil {
			btimer.Stop()
		}
	}()
	round(true)
	trigger := make(chan struct{}, 1)
	poke := func() {
		select {
		case trigger <- struct{}{}:
		default:
		}
	}
	go s.watchLoop(ctx, poke)
	go s.adultLoop(ctx, poke)

	var debounce <-chan time.Time
	var dtimer *time.Timer
	hourly := time.NewTimer(time.Until(NextHour(s.Now())))
	defer hourly.Stop()
	rescan := time.NewTicker(s.Rescan)
	defer rescan.Stop()
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
			round(false)
		case <-hourly.C:
			round(true)
			hourly.Reset(time.Until(NextHour(s.Now())))
		case <-rescan.C:
			round(false)
		case <-boundary:
			round(false)
		}
	}
}

// adultLoop 每 AdultPoll 看一次兩個開關（自己的隱藏版 closet.adult、「修改」分頁 closet.edit-tab），跟上次不一樣就 poke（去抖後做一輪）。
// 擴充點的值不在 vault，改了不會觸發訂閱，所以要輪詢（edit-tab 不輪詢的話要等 5 分鐘的重掃，SEC-12 審查）。兩個都沒接就不看。
func (s *Service) adultLoop(ctx context.Context, poke func()) {
	if (s.Adult == nil && s.EditTab == nil) || s.AdultPoll <= 0 {
		return
	}
	read := func() [2]bool {
		return [2]bool{s.adultOn(ctx), s.editTabOn(ctx)}
	}
	last := read()
	t := time.NewTicker(s.AdultPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if now := read(); now != last {
				last = now
				poke()
			}
		}
	}
}

// BoundaryDelay：下一個隱藏版邊界要等多久（多等一秒：計時器早一點點醒來時，時段的開頭還沒到）。
// 沒有時段回 false。邊界已經過了（這一輪中途失敗、沒算到新的邊界）就隔 retry 再試，不馬上重跑，免得空轉。
func BoundaryDelay(next, now time.Time, retry time.Duration) (time.Duration, bool) {
	if next.IsZero() {
		return 0, false
	}
	if d := next.Sub(now) + time.Second; d > 0 {
		return d, true
	}
	return retry, true
}

// NextHour 是下一個本機時間的整點。
func NextHour(now time.Time) time.Time {
	return now.Truncate(time.Hour).Add(time.Hour)
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
		poke() // 重連之後補做一輪，斷線期間的變更才不會漏
	}
}

func (s *Service) round(ctx context.Context, hourly bool) {
	if err := s.Round(ctx, hourly); err != nil && ctx.Err() == nil {
		s.Log.Error("這一輪沒做完", "err", err)
	}
}

// accepted 是已經寫了 unlock、等 person／state 寫好才標成 done 的請求。
type accepted struct{ request, unlock string }

// Round 做一輪：⓪寫能力紀錄（closet.capabilities，CLO-32）①清不認的兌換 ②處理待處理的請求 ③算 person／state ④對帳公開資料夾 ⑤內容有變才寫 person／state，
// 最後把成功的請求標成 done（這樣看到 done 時 person 已經是兌換後的樣子）；hourly 時順便封存太舊的已處理請求。
// 全部在一個循環裡一次做一件事，所以不會有兩個請求同時扣點。
func (s *Service) Round(ctx context.Context, hourly bool) error {
	now := s.Now()
	caps, known, write := s.capabilities(ctx)
	if write {
		s.writeCapabilities(ctx, caps, now) // 先寫：這一輪後面讀不到別的資料，能力照樣是新的
	}
	edit := rules.HasCap(caps, rules.CapEdit)
	outfitRecs, err := s.V.List(ctx, KindOutfit, false)
	if err != nil {
		return fmt.Errorf("讀衣服目錄失敗：%w", err)
	}
	catalog := s.parseCatalog(outfitRecs)

	unlocks, err := s.trustedUnlocks(ctx)
	if err != nil {
		return err
	}
	wallet, err := s.V.Get(ctx, kindWallet, walletID)
	if err != nil {
		return fmt.Errorf("讀錢包失敗：%w", err)
	}
	earned, haveWallet := 0, wallet != nil
	if wallet != nil {
		earned, _ = rules.Int(wallet.Fields["earned"])
	}

	if err := s.carryUnlocks(ctx, catalog, &unlocks, now); err != nil {
		return err
	}
	done, err := s.processRequests(ctx, catalog, &unlocks, earned, haveWallet, now)
	if err != nil {
		return err
	}

	unlocked := map[string]bool{}
	for _, u := range unlocks {
		unlocked[u.Record] = true
	}
	rotations, picks, err := s.rotations(ctx)
	if err != nil {
		return err
	}
	seats, err := s.seats(ctx)
	if err != nil {
		return err
	}
	variants, err := s.variants(ctx)
	if err != nil {
		return err
	}
	adultOn := s.adultOn(ctx)
	group := rules.HiddenGroup(variants, adultOn, photomigrate.OwnerVariant)
	s.mu.Lock()
	s.boundary = rules.NextBoundary(group, now)
	s.mu.Unlock()
	// 照片紀錄：先補還沒登記的舊圖（轉換器看所有隱藏版設定的資料夾，不看開關），再讀（DEV-160 S2）。
	photos, err := s.fillPhotos(ctx, catalog, variants, edit, !write)
	if err != nil {
		return err
	}
	if known && !edit {
		// 一般家沒有上架／下架（作者 2026-11-15，CLO-38）：設計中的我的衣服自動上架。
		s.publishMineDrafts(ctx, catalog, photos)
	}
	hs := hiddenState{exists: len(group) > 0, open: rules.HiddenOpen(group, now), adultOn: adultOn}
	// 沒兌換的也給真圖（CLO-27）跟著能力 edit 走，不另外問是不是開發家。
	view := s.buildView(catalog, unlocked, rotations, picks, seats, photos, hs, edit, now)
	res, err := pub.Reconcile(s.publicDir(), pub.BuildPlan(view.plan.outfits, view.plan.worn))
	if err != nil {
		return fmt.Errorf("對帳公開資料夾失敗：%w", err)
	}
	for dest, e := range res.Errors {
		s.logOnce("pub:"+dest, "公開資料夾這個檔案沒做成，略過", "file", dest, "err", e)
	}

	computedAt := now.Format(time.RFC3339)
	var firstErr error
	for _, p := range view.people {
		if err := s.writeIfChanged(ctx, KindPerson, p.persona, p.fields(res), computedAt); err != nil {
			s.Log.Error("寫角色檢視失敗", "persona", p.persona, "err", err)
			firstErr = err
		}
	}
	s.tombstonePeople(ctx, view, computedAt)
	spent := rules.SpentPoints(unlocks)
	state := map[string]any{"earned": earned, "spent": spent, "points": rules.Available(earned, unlocks), "wallet": haveWallet, "variants": variantStates(group, now)}
	if err := s.writeIfChanged(ctx, KindState, stateID, state, computedAt); err != nil {
		s.Log.Error("寫點數檢視失敗", "err", err)
		firstErr = err
	}
	if firstErr != nil {
		return firstErr // 沒寫成就不標 done；下一輪重做（unlock 已存在，當作已成功）
	}
	for _, a := range done {
		set := map[string]any{"state": "done"}
		if a.unlock != "" { // 現在穿這套／復原的請求沒有兌換紀錄
			set["unlock"] = a.unlock
		}
		if err := s.V.Patch(ctx, KindRequest, a.request, set); err != nil {
			s.Log.Error("標記請求完成失敗", "request", a.request, "err", err)
		}
	}
	if hourly {
		s.archiveOldRequests(ctx, now)
	}
	return nil
}

// ---- 讀資料 ----

func (s *Service) parseCatalog(recs []sdk.Record) map[string]rules.Outfit {
	out := map[string]rules.Outfit{}
	for _, r := range recs {
		o, err := rules.OutfitFromFields(r.ID, r.Fields)
		if err != nil {
			s.logOnce("outfit:"+r.ID+":"+err.Error(), "衣服的資料不合格，略過", "outfit", r.ID, "err", err)
			continue
		}
		out[o.Record] = o
	}
	return out
}

// trustedUnlocks 讀兌換紀錄：只認 by 是 closet 或 migrate 的；別的來源寫的一律封存並記 log（免得繞過點數、也免得 habits 算錯 spent）。
func (s *Service) trustedUnlocks(ctx context.Context) ([]rules.Unlock, error) {
	recs, err := s.V.List(ctx, KindUnlock, false)
	if err != nil {
		return nil, fmt.Errorf("讀兌換紀錄失敗：%w", err)
	}
	var out []rules.Unlock
	for _, r := range recs {
		if by := rules.Text(r.Fields["by"]); !rules.TrustedActor(by) {
			s.Log.Error("兌換紀錄不是 closet 寫的，封存", "unlock", r.ID, "by", by)
			if err := s.V.Archive(ctx, KindUnlock, r.ID); err != nil {
				s.Log.Error("封存不認的兌換失敗", "unlock", r.ID, "err", err)
			}
			continue
		}
		out = append(out, rules.UnlockFromFields(r.ID, r.Fields))
	}
	return out, nil
}

// carryUnlocks 把升成官方前的兌換搬到新編號（rules.CarryPlan）：先寫新的兌換（by = closet、cost 照舊的、request = carry:<舊編號>），
// 再封存舊的；上次只寫了新的就接著封存舊的（rules.CarriedLeftovers）。花掉的點數不變，之後誰改了那筆 outfit 擁有都不會掉。
func (s *Service) carryUnlocks(ctx context.Context, catalog map[string]rules.Outfit, unlocks *[]rules.Unlock, now time.Time) error {
	for _, c := range rules.CarryPlan(catalog, *unlocks) {
		o := catalog[c.To]
		fields := map[string]any{"persona": o.Persona, "outfit": o.Outfit, "cost": c.Unlock.Cost, "name": o.Name, "at": now.Format(time.RFC3339), "request": rules.CarryPrefix + c.From}
		if err := s.V.Put(ctx, sdk.Record{Kind: KindUnlock, ID: c.To, Fields: fields}, ""); err != nil {
			if e, ok := err.(*sdk.Error); ok && e.Code == sdk.ErrorCodeConflict {
				s.logOnce("carry:"+c.To, "新編號的兌換紀錄已被用掉（可能被封存過），不搬", "from", c.From, "to", c.To)
				continue
			}
			return fmt.Errorf("搬兌換紀錄失敗：%w", err)
		}
		*unlocks = append(*unlocks, rules.Unlock{Record: c.To, Persona: o.Persona, Outfit: o.Outfit, Cost: c.Unlock.Cost, Request: rules.CarryPrefix + c.From})
	}
	for _, from := range rules.CarriedLeftovers(*unlocks) {
		if err := s.V.Archive(ctx, KindUnlock, from); err != nil {
			return fmt.Errorf("封存搬走的舊兌換紀錄失敗：%w", err)
		}
		kept := (*unlocks)[:0]
		for _, u := range *unlocks {
			if u.Record != from {
				kept = append(kept, u)
			}
		}
		*unlocks = kept
	}
	return nil
}

// pick 是「現在穿這套」指定的那套與穿到什麼時候（closet.rotation 的 wear、wear_until，CLO-30）。
type pick struct {
	wear  string
	until int64 // Unix 秒；0＝沒有或寫壞了（不作用）
}

func (s *Service) rotations(ctx context.Context) (map[string][]string, map[string]pick, error) {
	recs, err := s.V.List(ctx, KindRotation, false)
	if err != nil {
		return nil, nil, fmt.Errorf("讀輪播設定失敗：%w", err)
	}
	out := map[string][]string{}
	picks := map[string]pick{}
	for _, r := range recs {
		out[r.ID] = rules.Strings(r.Fields["outfits"])
		if w := rules.Text(r.Fields["wear"]); w != "" {
			if t, err := time.Parse(time.RFC3339, rules.Text(r.Fields["wear_until"])); err == nil {
				picks[r.ID] = pick{wear: w, until: t.Unix()}
			}
		}
	}
	return out, picks, nil
}

// variants 讀隱藏版設定（CLO-17），排好順序；不合格的記一次 log、不作用。全部都回（轉換器找舊圖要看每筆的資料夾）；
// 算那一組開不開用 rules.HiddenGroup（自己的隱藏版開關關著時拿掉主人自己的設定）。
func (s *Service) variants(ctx context.Context) ([]rules.Variant, error) {
	recs, err := s.V.List(ctx, KindVariant, false)
	if err != nil {
		return nil, fmt.Errorf("讀隱藏版設定失敗：%w", err)
	}
	var out []rules.Variant
	for _, r := range recs {
		v, err := rules.VariantFromFields(r.ID, r.Fields)
		if err != nil {
			s.logOnce("variant:"+r.ID+":"+err.Error(), "隱藏版的設定不合格，不作用", "variant", r.ID, "err", err)
			continue
		}
		out = append(out, v)
	}
	rules.SortVariants(out)
	return out, nil
}

// adultOn：自己的隱藏版開關（沒接、讀不到都是關）。
func (s *Service) adultOn(ctx context.Context) bool {
	return s.Adult != nil && s.Adult(ctx)
}

// variantStates 是 closet.state 的 variants：給頁面畫開關（不用自己算時段）。隱藏版只有一組（DEV-160 S2）：
// 有任何一筆設定就寫一項 id = h——button＝任一筆要畫開關、on＝任一筆開著、in_window＝現在在任一筆的時段裡；
// ids 是這一組的每一筆設定，頁面按開關時每一筆的 on 一起改。
func variantStates(group []rules.Variant, now time.Time) []any {
	out := []any{}
	if len(group) == 0 {
		return out
	}
	button, on, in := false, false, false
	ids := []any{}
	for _, v := range group {
		button, on, in = button || v.Button, on || v.On, in || v.InWindow(now)
		ids = append(ids, v.ID)
	}
	return append(out, map[string]any{"id": rules.HiddenID, "name": rules.HiddenName, "button": button, "on": on, "in_window": in, "ids": ids})
}

// seats：人設 → 現在坐這個人設的職位 id（多位坐同一人設取編號最小的啟用中那位）。
func (s *Service) seats(ctx context.Context) (map[string]string, error) {
	recs, err := s.V.List(ctx, kindAgent, false)
	if err != nil {
		return nil, fmt.Errorf("讀名冊失敗：%w", err)
	}
	out := map[string]string{}
	for _, r := range recs {
		if on, _ := r.Fields["enabled"].(bool); !on {
			continue
		}
		p := rules.Text(r.Fields["persona"])
		if p == "" {
			continue
		}
		if cur, ok := out[p]; !ok || r.ID < cur {
			out[p] = r.ID
		}
	}
	return out, nil
}

// ---- 兌換請求 ----

func seqOf(id string) int {
	if _, n, ok := strings.Cut(id, "-"); ok {
		if v, err := strconv.Atoi(n); err == nil {
			return v
		}
	}
	return 1 << 30
}

func (s *Service) processRequests(ctx context.Context, catalog map[string]rules.Outfit, unlocks *[]rules.Unlock, earned int, haveWallet bool, now time.Time) ([]accepted, error) {
	recs, err := s.V.List(ctx, KindRequest, false)
	if err != nil {
		return nil, fmt.Errorf("讀請求失敗：%w", err)
	}
	sort.Slice(recs, func(i, j int) bool { return seqOf(recs[i].ID) < seqOf(recs[j].ID) })
	var done []accepted
	for _, r := range recs {
		if rules.Text(r.Fields["state"]) != "pending" {
			continue
		}
		persona, outfit := rules.Text(r.Fields["persona"]), rules.Text(r.Fields["outfit"])
		unlockID := rules.RecordID(persona, outfit)
		reject := func(reason string) {
			if err := s.V.Patch(ctx, KindRequest, r.ID, map[string]any{"state": "rejected", "reason": reason}); err != nil {
				s.Log.Error("標記請求被拒失敗", "request", r.ID, "err", err)
			}
		}
		switch rules.Text(r.Fields["type"]) {
		case "unlock":
		case "wear", "unwear":
			// 現在穿這套／復原（CLO-30）：不扣點、不看是誰寫的；跟兌換一樣等 person 寫好才標 done。重做一次結果相同。
			unlocked := map[string]bool{}
			for _, u := range *unlocks {
				unlocked[u.Record] = true
			}
			reason, err := s.applyWear(ctx, rules.Text(r.Fields["type"]) == "wear", persona, outfit, catalog, unlocked, now)
			if err != nil {
				return nil, err
			}
			if reason != "" {
				reject(reason)
				continue
			}
			done = append(done, accepted{request: r.ID})
			continue
		default:
			reject("不認得的請求種類")
			continue
		}
		// 重播安全：上次已經寫了這筆請求的 unlock、卻沒來得及標成 done，這次直接當成功，不再扣點。
		replay := false
		for _, u := range *unlocks {
			if u.Record == unlockID && u.Request == r.ID {
				replay = true
			}
		}
		if replay {
			done = append(done, accepted{r.ID, unlockID})
			continue
		}
		unlocked := map[string]bool{}
		for _, u := range *unlocks {
			unlocked[u.Record] = true
		}
		o, reason := rules.CheckUnlock(rules.RequestInput{
			By: rules.Text(r.Fields["by"]), Persona: persona, Outfit: outfit,
			Catalog: catalog, Unlocks: unlocked, Wallet: haveWallet, Points: rules.Available(earned, *unlocks),
		})
		if reason != "" {
			reject(reason)
			continue
		}
		fields := map[string]any{"persona": o.Persona, "outfit": o.Outfit, "cost": o.Cost, "name": o.Name, "at": now.Format(time.RFC3339), "request": r.ID}
		if err := s.V.Put(ctx, sdk.Record{Kind: KindUnlock, ID: unlockID, Fields: fields}, ""); err != nil {
			if e, ok := err.(*sdk.Error); !ok || e.Code != sdk.ErrorCodeConflict {
				return nil, fmt.Errorf("寫兌換紀錄失敗：%w", err)
			}
			// 編號已存在（含被封存的不認兌換）：不是這個請求寫的，不能當成功也不扣點。
			s.Log.Error("兌換編號已被用掉，請求不處理", "request", r.ID, "unlock", unlockID)
			reject(rules.ReasonUnlockBroken)
			continue
		}
		*unlocks = append(*unlocks, rules.Unlock{Record: unlockID, Persona: o.Persona, Outfit: o.Outfit, Cost: o.Cost, Request: r.ID})
		done = append(done, accepted{r.ID, unlockID})
	}
	return done, nil
}

// applyWear 處理「現在穿這套」（wear）與復原（unwear）：只改那位角色 closet.rotation 的 wear、wear_until，不動 outfits 清單
// （不在輪流穿清單裡的也不會被加進去）。回白話的拒絕原因（空字串＝成功）。
func (s *Service) applyWear(ctx context.Context, wear bool, persona, outfit string, catalog map[string]rules.Outfit, unlocked map[string]bool, now time.Time) (string, error) {
	// 先用目錄做純檢查，再碰資料：persona 是請求裡的自由字串，寫壞的（「../x」這類）拿去讀紀錄會回錯、
	// 整輪中止，那筆請求永遠 pending、之後每一輪都卡在它（兌換、輪播都停）。目錄裡沒有這位角色的衣服就不讀。
	if wear {
		if _, reason := rules.CheckWear(persona, outfit, catalog, unlocked); reason != "" {
			return reason, nil
		}
	} else if !hasPersona(catalog, persona) {
		return "", nil // 復原：衣櫃裡沒有這位角色，本來就沒有指定的衣服，當成功不做事
	}
	cur, err := s.V.Get(ctx, KindRotation, persona)
	if err != nil {
		return "", fmt.Errorf("讀輪播設定失敗：%w", err)
	}
	if cur != nil && cur.Archived {
		cur = nil
	}
	if !wear {
		// 復原：現在指定的就是這套才清掉；不是（已經換成別套、或本來就沒有）當成功不做事。
		if cur == nil || rules.Text(cur.Fields["wear"]) != outfit {
			return "", nil
		}
		if err := s.V.Patch(ctx, KindRotation, persona, map[string]any{"wear": "", "wear_until": ""}); err != nil {
			return "", fmt.Errorf("寫輪播設定失敗：%w", err)
		}
		return "", nil
	}
	set := map[string]any{"wear": outfit, "wear_until": rules.WearUntil(now).Format(time.RFC3339)}
	if cur != nil {
		if err := s.V.Patch(ctx, KindRotation, persona, set); err != nil {
			return "", fmt.Errorf("寫輪播設定失敗：%w", err)
		}
		return "", nil
	}
	// 還沒有輪播設定：新建一筆，outfits 空清單（跟沒有記錄一樣＝第一套擁有的）。
	set["outfits"] = []any{}
	if err := s.V.Put(ctx, sdk.Record{Kind: KindRotation, ID: persona, Fields: set}, ""); err != nil {
		if e, ok := err.(*sdk.Error); ok && e.Code == sdk.ErrorCodeConflict {
			delete(set, "outfits") // 剛好有人先建了（頁面按了輪流穿）：改成只補這兩欄
			if err := s.V.Patch(ctx, KindRotation, persona, set); err != nil {
				return "", fmt.Errorf("寫輪播設定失敗：%w", err)
			}
			return "", nil
		}
		return "", fmt.Errorf("寫輪播設定失敗：%w", err)
	}
	return "", nil
}

// hasPersona：目錄裡有沒有這位角色的衣服。
func hasPersona(catalog map[string]rules.Outfit, persona string) bool {
	for _, o := range catalog {
		if o.Persona == persona {
			return true
		}
	}
	return false
}

// archiveOldRequests 封存已處理超過 7 天的請求（整點那輪做）。
func (s *Service) archiveOldRequests(ctx context.Context, now time.Time) {
	recs, err := s.V.List(ctx, KindRequest, false)
	if err != nil {
		s.Log.Error("讀請求失敗", "err", err)
		return
	}
	for _, r := range recs {
		if st := rules.Text(r.Fields["state"]); (st == "done" || st == "rejected") && !r.UpdatedAt.IsZero() && now.Sub(r.UpdatedAt) > requestKeep {
			if err := s.V.Archive(ctx, KindRequest, r.ID); err != nil {
				s.Log.Error("封存舊請求失敗", "request", r.ID, "err", err)
			}
		}
	}
}

// ---- 寫回 vault ----

// writeIfChanged：欄位（不含 computed_at）跟現有的一樣就不寫；否則整筆寫入（換版本號時重試一次）。
// 這些是衣櫃自己算的檢視（自己給編號的種類）：被封存了就用同一個編號重寫一筆沒封存的（vault 的 put 不帶版本號＝新的一筆，
// 跟 closet-mine 拿回刪掉的衣服同一個做法），不然能力紀錄封存後就永遠寫不回去、頁面與核心一直當沒有能力（DEV-160 審查 S5）。
func (s *Service) writeIfChanged(ctx context.Context, kind, id string, fields map[string]any, computedAt string) error {
	for attempt := 0; ; attempt++ {
		cur, err := s.V.Get(ctx, kind, id)
		if err != nil {
			return err
		}
		revision := ""
		if cur != nil && !cur.Archived {
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
	x, _ := json.Marshal(normalize(a))
	y, _ := json.Marshal(normalize(now))
	return string(x) == string(y)
}

// normalize 把「沒寫」和「空字串」當成一樣（存成檔案再讀回來，空值可能不見），並讓數字型別一致。
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			if e == nil || e == "" {
				continue
			}
			out[k] = normalize(e)
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			out = append(out, normalize(e))
		}
		return out
	case []string:
		out := make([]any, 0, len(x))
		for _, e := range x {
			out = append(out, e)
		}
		return out
	}
	return v
}
