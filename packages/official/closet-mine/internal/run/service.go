// Package run 是 closet-mine 套件行程要做的事：讀 closet-mine.request，驗證後把使用者上傳的圖複製進 personas/mine/photos/，
// 並在 closet 的目錄（closet.outfit）與照片紀錄（closet.photo）登記（REQ-126、D-30、DEV-160）；也做刪單張、刪一套、拿回、改點數。只經 SDK 的 client 呼叫核心（PKC-N1、PKC-N3）；
// 不做 RPC：頁面與秘書寫請求、程式只回填 state、reason、result（D-28）。
package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/minglight/evergenie/packages/official/closet-mine/internal/promote"
	"github.com/minglight/evergenie/packages/official/closet-mine/internal/rules"
	"github.com/minglight/evergenie/packages/official/closet-mine/internal/store"
	"github.com/minglight/evergenie/sdk"
)

// vault 種類與寫入者。
const (
	KindRequest = "closet-mine.request"
	kindOutfit  = "closet.outfit"
	kindPerson  = "closet.person"
	kindVariant = "closet.variant"
	// kindCaps／capEdit：衣櫃每一輪寫的「這個家支援什麼」（衣櫃卡 CLO-32）；名字跟衣櫃一致由衣櫃的 Go 測試
	// TestCapNamesMatchSchemaAndPages 一起守（套件不能互相 import）。換圖要不要回到還沒採用、能不能換官方的照片看它（CMI-22、CMI-25）。
	kindCaps = "closet.capabilities"
	capEdit  = "edit"

	// adultVariantID 是自己的隱藏版那一筆的編號（第一次放進自己的隱藏版時建，DEV-141）。
	adultVariantID = "adult"

	actor = "closet-mine"

	requestKeep = 7 * 24 * time.Hour // 已處理的請求保留幾天才封存
)

// WatchedKinds 是有變就要做一輪的種類。
var WatchedKinds = []string{KindRequest}

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

// Service 是 closet-mine 行程的本體。
type Service struct {
	V        Vault
	Store    *store.Store
	Now      func() time.Time
	Log      *slog.Logger
	Debounce time.Duration // 有變之後等多久才再做一輪（預設 1 秒）
	Retry    time.Duration // 訂閱斷線後等多久重連（預設 3 秒）
	Rescan   time.Duration // 多久做一輪（預設 5 分鐘）
	// Adult 回傳家層級的「自己的隱藏版」開關（衣櫃的擴充點 closet.adult）是不是開著；nil 或讀不到都當關。
	Adult func(ctx context.Context) bool
	// Dev 回傳這個家是不是開發家（升成官方只在開發家做）；nil 當不是；讀不到回 err（請求維持 pending）。
	Dev func(ctx context.Context) (bool, error)

	mu sync.Mutex
}

// NewService 組一個 Service；log 寫到 w（核心把行程的 stderr 收進 out.log）。
func NewService(v Vault, home string, w io.Writer) *Service {
	return &Service{V: v, Store: store.New(home), Now: time.Now, Log: slog.New(slog.NewTextHandler(w, nil)),
		Debounce: time.Second, Retry: 3 * time.Second, Rescan: 5 * time.Minute}
}

func homeFromEnv() (string, error) {
	home := sdk.Getenv("EVERGENIE_HOME")
	if home == "" {
		return "", fmt.Errorf("缺少 EVERGENIE_HOME：closet-mine 要由 EverGenie 核心啟動")
	}
	return home, nil
}

// Run：開機先做一輪；vault 有變就去抖再做一輪；每 5 分鐘做一輪；訂閱斷線 3 秒後重連並補做一輪。跑到 ctx 結束。
func (s *Service) Run(ctx context.Context) error {
	s.round(ctx)
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
			s.round(ctx)
		case <-rescan.C:
			s.round(ctx)
		}
	}
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
		poke() // 重連之後補做一輪，斷線期間的請求才不會漏
	}
}

func (s *Service) round(ctx context.Context) {
	if err := s.Round(ctx); err != nil && ctx.Err() == nil {
		s.Log.Error("這一輪沒做完", "err", err)
	}
}

// Round 做一輪：依編號由小到大，一次一件，處理 pending 的請求；最後封存超過 7 天的已處理請求。
// 一筆壞資料略過那一筆；基礎設施出錯的請求維持 pending，下一輪再試。
func (s *Service) Round(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, err := s.V.List(ctx, KindRequest, false)
	if err != nil {
		return fmt.Errorf("讀請求失敗：%w", err)
	}
	sort.SliceStable(recs, func(i, j int) bool {
		a, _ := rules.SeqOf(recs[i].ID)
		b, _ := rules.SeqOf(recs[j].ID)
		if a != b {
			return a < b
		}
		return recs[i].ID < recs[j].ID
	})
	now := s.Now()
	for _, r := range recs {
		if ctx.Err() != nil {
			return nil
		}
		switch rules.Text(r.Fields["state"]) {
		case rules.StatePending:
			s.process(ctx, r)
		case rules.StateDone, rules.StateRejected:
			if !r.UpdatedAt.IsZero() && now.Sub(r.UpdatedAt) > requestKeep {
				if err := s.V.Archive(ctx, KindRequest, r.ID); err != nil {
					s.Log.Error("封存舊請求失敗", "request", r.ID, "err", err)
				}
			}
		}
	}
	return nil
}

// process 處理一個 pending 請求並回填結果。
func (s *Service) process(ctx context.Context, r sdk.Record) {
	var result, reason, detail string
	var err error
	if rules.Text(r.Fields["type"]) == rules.TypePromote {
		result, detail, reason, err = s.promote(ctx, r)
	} else {
		result, reason, err = s.handle(ctx, r)
	}
	if errors.Is(err, store.ErrUnsafe) {
		s.Log.Error("personas/mine 的路徑不安全，請求被拒", "request", r.ID, "err", err)
		result, reason, err = "", rules.ReasonUnsafeFolder, nil
	}
	switch {
	case err != nil:
		s.Log.Error("處理請求失敗，下一輪再試", "request", r.ID, "err", err)
	case reason != "":
		if err := s.V.Patch(ctx, KindRequest, r.ID, map[string]any{"state": rules.StateRejected, "reason": reason}); err != nil {
			s.Log.Error("標記請求被拒失敗", "request", r.ID, "err", err)
		}
	default:
		set := map[string]any{"state": rules.StateDone, "result": result, "reason": ""}
		if detail != "" {
			set["detail"] = detail
		}
		if err := s.V.Patch(ctx, KindRequest, r.ID, set); err != nil {
			s.Log.Error("標記請求完成失敗", "request", r.ID, "err", err)
		}
	}
}

// handle 做一個請求：回 (result, reason, err)。reason 非空＝拒絕；err 非空＝這次沒做成、維持 pending。
// 上傳的順序是「照片檔 → 角色卡 → closet.outfit → closet.photo → 標 done」，每一步都可以重播（行程重開後同一個請求再來一次結果相同）。
// 不帶上傳檔的整理（刪、拿回、改點數）在 tidy（remove.go）。
func (s *Service) handle(ctx context.Context, r sdk.Record) (result, reason string, err error) {
	req := rules.ParseRequest(r.ID, r.Fields)
	if reason := rules.Validate(req); reason != "" {
		return "", reason, nil
	}
	if !rules.NeedsUpload(req.Type) {
		return s.tidy(ctx, r, req)
	}
	if req.Adult && !s.adultOn(ctx) {
		return "", rules.ReasonAdultOff, nil // 沒打開：連上傳檔都不讀
	}
	upName, _ := rules.UploadName(req.Upload)
	data, err := s.Store.ReadUpload(upName)
	if errors.Is(err, store.ErrNoUpload) {
		return "", rules.ReasonNoUpload, nil
	}
	if err != nil {
		return "", "", err
	}
	kind := rules.Sniff(data)
	if reason := rules.CheckKind(req.Type, kind); reason != "" {
		return "", reason, nil
	}
	ext := kind.Ext()
	switch req.Type {
	case rules.TypeCharacter:
		return s.character(ctx, req, data, ext)
	case rules.TypeOutfit:
		return s.outfit(ctx, req, data, ext)
	default:
		return s.extra(ctx, req, data, ext)
	}
}

// promote 做一筆升成官方的請求：回 (result, detail, reason, err)。只在開發家、只認主人本人送的。
func (s *Service) promote(ctx context.Context, r sdk.Record) (result, detail, reason string, err error) {
	if !rules.IsOwner(rules.Text(r.Fields["by"])) {
		return "", "", rules.ReasonNotOwnerPromote, nil
	}
	dev := false
	if s.Dev != nil {
		if dev, err = s.Dev(ctx); err != nil {
			return "", "", "", err
		}
	}
	if !dev {
		return "", "", rules.ReasonNotDev, nil
	}
	req := rules.ParseRequest(r.ID, r.Fields)
	if reason := rules.Validate(req); reason != "" {
		return "", "", reason, nil
	}
	res, err := promote.Run(ctx, promote.Options{Home: s.Store.Home, Target: req.Outfit, NewID: req.NewID, DryRun: req.DryRun, Vault: s.V, Now: s.Now()})
	var rj *promote.Rejected
	if errors.As(err, &rj) {
		return "", "", rj.Reason, nil
	}
	if err != nil {
		return "", "", "", err
	}
	return res.To, res.Summary(), "", nil
}

// homeCaps：這個家支援 edit 嗎（衣櫃每一輪寫的 closet.capabilities/main 的 supports，CLO-32）。
// known＝讀到一筆沒封存的能力紀錄；還沒有這筆、封存了是 known = false（不知道是哪種家）；讀失敗回錯（請求維持 pending，下一輪再試）。
// 照能力決定行為的地方，known = false 一律不當成一般家走較寬的那條路（憲法第 10 條）：換圖照樣回到還沒採用、官方的照片不換。
func (s *Service) homeCaps(ctx context.Context) (edit, known bool, err error) {
	rec, err := s.V.Get(ctx, kindCaps, "main")
	if err != nil || rec == nil || rec.Archived {
		return false, false, err
	}
	for _, c := range rules.Strings(rec.Fields["supports"]) {
		if strings.TrimSpace(c) == capEdit {
			return true, true, nil
		}
	}
	return false, true, nil
}

// mapStoreErr：目的檔已存在而且內容不同＝被別人用了（拒絕）；其他錯誤是基礎設施問題（維持 pending）。
func mapStoreErr(err error, taken string) (string, string, error) {
	if errors.Is(err, store.ErrExists) {
		return "", taken, nil
	}
	return "", "", err
}
