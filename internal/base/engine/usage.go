package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 用量、額度暖機、額度開通通知、安裝前檢查（Q4，REQ-86）。資料與暖機的做法都在各帳號的引擎裡（ENG-4）；
// 這裡只負責「問每個帳號」「排程到了做什麼」「通知主人」。

// 排程工作的 action（schemas/job.md）：系統級的工作，不經過秘書。
const (
	ActionUsageWarmup = "usage_warmup"
	ActionLimitWatch  = "limit_watch"
)

const (
	usageTimeout = 30 * time.Second
	// blockedRatio：用量到這個比例就當被擋（最後一次畫狀態列可能停在 98–99）。
	blockedRatio = 0.98
	// staleAfter：重置過了這麼久才要通知，就不通知了。
	staleAfter = 12 * time.Hour
)

type target struct {
	name   string
	api    sdk.EngineAPI
	config map[string]any
	warmup bool
}

// targets 列出設定過的帳號（依名稱排序）；引擎沒在跑的 api 是 nil。
func (m *Module) targets() ([]target, error) {
	m.mu.Lock()
	r := m.registry
	m.mu.Unlock()
	if r == nil {
		return nil, platform.Unavailable("引擎服務尚未啟動")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]target, 0, len(r.accounts))
	for name, a := range r.accounts {
		out = append(out, target{name: name, api: r.clients[a.engine], config: a.config, warmup: a.warmup})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// Usages 問每個帳號（或只問 account）的用量。引擎沒在跑或沒有資料的帳號放 Note，不當成錯誤。
func (m *Module) Usages(ctx context.Context, account string) ([]sdk.AccountUsage, error) {
	ts, err := m.targets()
	if err != nil {
		return nil, err
	}
	var out []sdk.AccountUsage
	for _, t := range ts {
		if account != "" && t.name != account {
			continue
		}
		u := sdk.AccountUsage{Account: t.name}
		if t.api == nil {
			u.Note = NotRunning
			out = append(out, u)
			continue
		}
		call, cancel := context.WithTimeout(ctx, usageTimeout)
		resp, err := t.api.Usage(call, &sdk.EngineUsageRequest{Account: t.name, EngineConfig: t.config})
		cancel()
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case err != nil:
			u.Note = CheckFailedPrefix + failureText(err)
		case resp.MeasuredAt.IsZero() && len(resp.Windows) == 0:
			u.Note = "還沒有用量資料（這個帳號還沒跑過 session）"
		default:
			u.UsedRatio, u.ResetsAt, u.Windows, u.MeasuredAt = resp.UsedRatio, resp.ResetsAt, resp.Windows, resp.MeasuredAt
		}
		out = append(out, u)
	}
	if account != "" && len(out) == 0 {
		return nil, platform.NotFound("找不到帳號 " + account)
	}
	return out, nil
}

// Preflight 問每個帳號的引擎：跑起來需要的東西齊不齊。同一個 id 的檢查只列一次（多個帳號共用同一個程式時）；
// 引擎沒在跑的帳號放一條說明。
func (m *Module) Preflight(ctx context.Context) ([]sdk.PreflightCheck, error) {
	ts, err := m.targets()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []sdk.PreflightCheck
	for _, t := range ts {
		if t.api == nil {
			out = append(out, sdk.PreflightCheck{ID: "engine:" + t.name, Name: "帳號 " + t.name + " 的引擎", Detail: NotRunning, Fix: "先啟動 evergenie serve"})
			continue
		}
		call, cancel := context.WithTimeout(ctx, usageTimeout)
		resp, err := t.api.Preflight(call, &sdk.EnginePreflightRequest{Account: t.name, EngineConfig: t.config})
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			out = append(out, sdk.PreflightCheck{ID: "engine:" + t.name, Name: "帳號 " + t.name + " 的引擎", Detail: CheckFailedPrefix + failureText(err)})
			continue
		}
		for _, c := range resp.Checks {
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	return out, nil
}

// Warmup 對每個標了 usage_warmup 的帳號送暖機請求（讓額度視窗從現在起算）。失敗的帳號發 system.alert；回傳每個帳號一句結果。
func (m *Module) Warmup(ctx context.Context) ([]string, error) {
	ts, err := m.targets()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, t := range ts {
		if !t.warmup {
			continue
		}
		if t.api == nil {
			lines = append(lines, t.name+"："+NotRunning)
			m.alert(ctx, "額度暖機沒有做", t.name+" 的引擎沒有在跑")
			continue
		}
		call, cancel := context.WithTimeout(ctx, 3*time.Minute)
		_, err := t.api.Usage(call, &sdk.EngineUsageRequest{Account: t.name, EngineConfig: t.config, Warmup: true})
		cancel()
		if err != nil {
			lines = append(lines, t.name+"：失敗，"+failureText(err))
			m.alert(ctx, "早上額度暖機失敗", t.name+"："+failureText(err))
			continue
		}
		lines = append(lines, t.name+"：OK")
	}
	return lines, nil
}

func (m *Module) alert(ctx context.Context, title, detail string) {
	if m.d.Bus == nil {
		return
	}
	if err := m.d.Bus.Publish(ctx, "system.alert", map[string]any{"title": title, "context": detail}); err != nil && m.log != nil {
		m.log.Warn("送不出系統通知", "title", title)
	}
}

// blockedWindows 是 limit_watch 記下的「被擋的視窗」：帳號 → 視窗名 → 重置時間。
type blockedWindows map[string]map[string]time.Time

func (m *Module) limitStatePath() string {
	return platform.RuntimeDir(m.home, "engine", "limit-watch.json")
}

func (m *Module) loadLimitState() blockedWindows {
	out := blockedWindows{}
	data, err := os.ReadFile(m.limitStatePath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out) // 壞掉就當沒有
	return out
}

func (m *Module) saveLimitState(s blockedWindows) error {
	data, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.limitStatePath()), 0o700); err != nil {
		return err
	}
	return platform.AtomicWrite(m.limitStatePath(), data, 0o600)
}

// LimitWatch 記下被擋的額度視窗（用量 ≥ 98%），等它們全部過了重置時間，發一則「額度開通了」。
// 被擋之後那個帳號沒有 session 在跑，快照不會再更新，所以只靠「記下重置時間」判斷。安靜時段先留著；重置超過 12 小時才發就當過期，不發。
func (m *Module) LimitWatch(ctx context.Context, now time.Time) ([]string, error) {
	ts, err := m.targets()
	if err != nil {
		return nil, err
	}
	state := m.loadLimitState()
	var log []string
	for _, t := range ts {
		if t.api == nil {
			continue
		}
		call, cancel := context.WithTimeout(ctx, usageTimeout)
		resp, err := t.api.Usage(call, &sdk.EngineUsageRequest{Account: t.name, EngineConfig: t.config})
		cancel()
		if err != nil || resp == nil {
			continue
		}
		for _, w := range resp.Windows {
			if w.UsedRatio >= blockedRatio && w.ResetsAt.After(now) {
				if state[t.name] == nil {
					state[t.name] = map[string]time.Time{}
				}
				key := windowName(w)
				if !state[t.name][key].Equal(w.ResetsAt) {
					log = append(log, fmt.Sprintf("%s %s額度 %.0f%%，記下重置時間", t.name, key, w.UsedRatio*100))
				}
				state[t.name][key] = w.ResetsAt
			}
		}
	}
	names := make([]string, 0, len(state))
	for n := range state {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, acc := range names {
		wins := state[acc]
		var latest time.Time
		open := false
		for _, at := range wins {
			open = open || at.After(now)
			if at.After(latest) {
				latest = at
			}
		}
		switch {
		case open:
			continue
		case now.Sub(latest) > staleAfter:
			log = append(log, acc+" 重置已經過了很久，不通知")
			delete(state, acc)
		case m.quiet.Contains(now):
			log = append(log, acc+" 已開通，安靜時段先留著")
		default:
			keys := make([]string, 0, len(wins))
			for k := range wins {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			m.alert(ctx, "額度開通了", fmt.Sprintf("%s 帳號的%s額度已經重置，可以用了。", acc, strings.Join(keys, "、")))
			log = append(log, "已通知："+acc)
			delete(state, acc)
		}
	}
	return log, m.saveLimitState(state)
}

func windowName(w sdk.UsageWindow) string {
	if w.Kind == sdk.UsageWindowKindWeekly {
		return "七天"
	}
	return fmt.Sprintf("%d 小時", w.Hours)
}

// subscribeJobs 聽排程的 schedule.fire：系統級的額度工作（暖機、開通通知）由這裡做。bus 沒有訂閱能力（測試用的假物件）就略過。
func (m *Module) subscribeJobs(run context.Context) {
	sub, ok := m.d.Bus.(interface {
		Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error)
	})
	if !ok || m.d.Bus == nil {
		return
	}
	events, err := sub.Subscribe(run, "schedule.fire")
	if err != nil {
		if m.log != nil {
			m.log.Warn("訂閱排程失敗，額度排程不會執行")
		}
		return
	}
	go func() {
		defer func() { _ = recover() }()
		for {
			select {
			case <-run.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				if ev == nil || ev.Payload == nil {
					continue
				}
				switch ev.Payload["action"] {
				case ActionUsageWarmup:
					lines, _ := m.Warmup(run)
					m.logLines("額度暖機", lines)
				case ActionLimitWatch:
					lines, _ := m.LimitWatch(run, m.d.Clock.Now())
					m.logLines("額度監看", lines)
				}
			}
		}
	}()
}

func (m *Module) logLines(what string, lines []string) {
	if m.log == nil {
		return
	}
	for _, l := range lines {
		m.log.Info(what, "detail", l)
	}
}
