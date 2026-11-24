// Package run 是 admin 套件行程要做的事：定時（與有人要求時）算發版狀態、開發家狀態，寫進 admin.release／admin.devhome，
// 並處理 admin.request 的重新整理請求。只經 SDK 的 client 呼叫核心（PKC-N1、PKC-N3）；不做 RPC，畫面讀的是 vault 資料（D-28）。
package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// vault 種類（完整名稱 `<套件 id>.<種類>`，D-28）。
const (
	KindRelease = "admin.release"
	KindDevHome = "admin.devhome"
	KindRequest = "admin.request"

	currentID = "current"
	actor     = "admin"

	requestKeep = 7 * 24 * time.Hour // 已處理的請求保留幾天才封存
)

// 請求要重算的範圍（admin.request 的 target）。
const (
	TargetAll     = "all"
	TargetRelease = "release"
	TargetDevHome = "devhome"
)

// Vault 是 Service 用到的 vault 能力（真的由 sdk.VaultAPI 提供，測試用假的）。
type Vault interface {
	List(ctx context.Context, kind string, includeArchived bool) ([]sdk.Record, error)
	// Get 找不到回 nil, nil。
	Get(ctx context.Context, kind, id string) (*sdk.Record, error)
	// Put 整筆寫入；expectedRevision 空＝必須是新的。
	Put(ctx context.Context, rec sdk.Record, expectedRevision string) error
	Patch(ctx context.Context, kind, id string, set map[string]any) error
	Archive(ctx context.Context, kind, id string) error
	// Watch 訂閱變更，每次有變呼叫 onChange；跑到 ctx 結束或連線斷掉為止。
	Watch(ctx context.Context, kinds []string, onChange func()) error
}

// Service 是 admin 行程的本體。
type Service struct {
	V        Vault
	Home     string                                    // 家的路徑（讀 home.toml 的 [admin]）
	Version  func(ctx context.Context) (string, error) // 本機 doctor 的核心版本
	Doctor   DoctorFunc                                // 問開發家的 doctor
	Now      func() time.Time
	Log      *slog.Logger
	Timeouts Timeouts
	Probe    time.Duration // 開發家多久沒回應算沒開（預設 1 秒）
	Interval time.Duration // 定時重算的間隔（預設 5 分鐘；git fetch 最多這麼頻繁）
	Debounce time.Duration // 有請求之後等多久才處理（預設 1 秒）
	Retry    time.Duration // 訂閱斷線後等多久重連（預設 3 秒）
}

// NewService 組一個 Service；log 寫到 w（核心把行程的 stderr 收進 out.log）。
func NewService(v Vault, home string, version func(context.Context) (string, error), w io.Writer) *Service {
	return &Service{V: v, Home: home, Version: version, Doctor: DialDoctor, Now: time.Now, Log: slog.New(slog.NewTextHandler(w, nil)),
		Timeouts: DefaultTimeouts, Probe: time.Second, Interval: 5 * time.Minute, Debounce: time.Second, Retry: 3 * time.Second}
}

func homeFromEnv() (string, error) {
	home := sdk.Getenv("EVERGENIE_HOME")
	if home == "" {
		return "", fmt.Errorf("缺少 EVERGENIE_HOME：admin 要由 EverGenie 核心啟動")
	}
	return home, nil
}

// Run：開機先算一輪（含先處理積著的請求）；admin.request 有變就去抖後處理請求；每 Interval 重算一輪並封存舊請求。跑到 ctx 結束。
func (s *Service) Run(ctx context.Context) error {
	s.logErr(s.HandleRequests(ctx))
	s.logErr(s.Round(ctx, TargetAll))
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
	tick := time.NewTicker(s.Interval)
	defer tick.Stop()
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
			s.logErr(s.HandleRequests(ctx))
		case <-tick.C:
			s.logErr(s.Round(ctx, TargetAll))
			s.logErr(s.archiveOld(ctx))
		}
	}
}

func (s *Service) logErr(err error) {
	if err != nil {
		s.Log.Error("這一輪沒做完", "err", err)
	}
}

func (s *Service) watchLoop(ctx context.Context, poke func()) {
	for ctx.Err() == nil {
		err := s.V.Watch(ctx, []string{KindRequest}, poke)
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

// Round 依 target 重算並寫入：release（這台的版本對 repo）、devhome（ComputeDevHome 決定這台要不要寫）。
func (s *Service) Round(ctx context.Context, target string) error {
	cfg, err := LoadConfig(s.Home)
	if err != nil {
		s.Log.Warn("照預設設定繼續", "err", err)
	}
	version := ""
	if s.Version != nil {
		version, err = s.Version(ctx)
		if err != nil {
			s.Log.Warn("讀不到本機版本", "err", err)
		}
	}
	now := s.Now()
	if target == TargetAll || target == TargetRelease {
		rel := ComputeRelease(ctx, cfg, version, s.Timeouts)
		if err := s.putCurrent(ctx, KindRelease, rel, now); err != nil {
			return fmt.Errorf("寫發版狀態失敗：%w", err)
		}
	}
	if target == TargetAll || target == TargetDevHome {
		if st, write := ComputeDevHome(ctx, cfg, s.Doctor, s.Probe); write {
			if err := s.putCurrent(ctx, KindDevHome, st, now); err != nil {
				return fmt.Errorf("寫開發家狀態失敗：%w", err)
			}
		}
	}
	return nil
}

// HandleRequests 處理所有 pending 的請求：合併要重算的範圍、算一次，成功後把它們標成 done；不認得的 type 標 rejected。
func (s *Service) HandleRequests(ctx context.Context) error {
	recs, err := s.V.List(ctx, KindRequest, false)
	if err != nil {
		return fmt.Errorf("讀請求失敗：%w", err)
	}
	var ids []string
	targets := map[string]bool{}
	for _, r := range recs {
		if st, _ := r.Fields["state"].(string); st != "pending" {
			continue
		}
		if typ, _ := r.Fields["type"].(string); typ != "refresh" {
			if err := s.V.Patch(ctx, KindRequest, r.ID, map[string]any{"state": "rejected", "reason": "看不懂這種請求：" + typ}); err != nil {
				return err
			}
			continue
		}
		t, _ := r.Fields["target"].(string)
		switch t {
		case TargetRelease, TargetDevHome:
		default:
			t = TargetAll
		}
		targets[t] = true
		ids = append(ids, r.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	switch {
	case targets[TargetAll] || (targets[TargetRelease] && targets[TargetDevHome]):
		err = s.Round(ctx, TargetAll)
	case targets[TargetRelease]:
		err = s.Round(ctx, TargetRelease)
	default:
		err = s.Round(ctx, TargetDevHome)
	}
	if err != nil {
		return err
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := s.V.Patch(ctx, KindRequest, id, map[string]any{"state": "done"}); err != nil {
			return fmt.Errorf("回填請求 %s 失敗：%w", id, err)
		}
	}
	return nil
}

// archiveOld 封存處理完超過 7 天的請求。
func (s *Service) archiveOld(ctx context.Context) error {
	recs, err := s.V.List(ctx, KindRequest, false)
	if err != nil {
		return err
	}
	cut := s.Now().Add(-requestKeep)
	for _, r := range recs {
		if st, _ := r.Fields["state"].(string); st == "pending" || r.UpdatedAt.IsZero() || r.UpdatedAt.After(cut) {
			continue
		}
		if err := s.V.Archive(ctx, KindRequest, r.ID); err != nil {
			return err
		}
	}
	return nil
}

// putCurrent 把 v（JSON 形狀）加上 computed_at 整筆寫進 <kind>/current。
func (s *Service) putCurrent(ctx context.Context, kind string, v any, now time.Time) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fields := map[string]any{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	fields["computed_at"] = now.Format(time.RFC3339)
	old, err := s.V.Get(ctx, kind, currentID)
	if err != nil {
		return err
	}
	rev := ""
	if old != nil {
		rev = old.Revision
	}
	return s.V.Put(ctx, sdk.Record{Kind: kind, ID: currentID, Fields: fields}, rev)
}
