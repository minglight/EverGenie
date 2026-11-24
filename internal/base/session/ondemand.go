package session

import (
	"context"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 本機隨選秘書的啟用判斷、排程與閒置生命週期。

// OndemandIdleClose 是隨選（ondemand）秘書的主要 session 閒置多久收掉（作者 2026-10-26 確認）。
const OndemandIdleClose = 60 * time.Minute

func (m *Module) agentMode(ctx context.Context, id string) (enabled bool, mode string) {
	r, err := m.d.Vault.Get(ctx, "agent", id)
	if err != nil {
		return false, ""
	}
	enabled, _ = r.Fields["enabled"].(bool)
	mode = text(r.Fields, "mode")
	return enabled, mode
}

// refreshModes 更新每位秘書的 mode 與「今天有沒有排程」的快取（每個 watchdog 週期一次）。
func (m *Module) refreshModes(ctx context.Context, now time.Time) {
	rows, err := m.d.Agents.List(ctx)
	if err != nil {
		return
	}
	day := now.In(m.config.Location())
	key := day.Format("2006-01-02")
	modes := map[string]string{}
	today := map[string]string{}
	m.mu.Lock()
	fn := m.scheduledFn
	m.mu.Unlock()
	for _, r := range rows {
		modes[r.ID] = text(r.Fields, "mode")
		if text(r.Fields, "mode") == "ondemand" && fn != nil && fn(ctx, r.ID, day) {
			today[r.ID] = key
		}
	}
	m.mu.Lock()
	m.modes, m.scheduledOn = modes, today
	m.mu.Unlock()
}

// ondemandClosable 回報這個 session 現在是不是「隨選、而且今天沒有排程」，是的話閒置 OndemandIdleClose 後要收掉。
// 呼叫時持有 m.mu。
func (m *Module) ondemandClosable(e *entry, now time.Time) bool {
	if e.session.Kind != sdk.SessionKindResident || m.modes[e.session.Agent] != "ondemand" {
		return false
	}
	return m.scheduledOn[e.session.Agent] != now.In(m.config.Location()).Format("2006-01-02")
}

// openOnDemand 在第一句話來的時候把隨選秘書的主要 session 開起來（SES-1）。沒有這位秘書、停用、不是隨選、
// 就回 false，維持「送不進去就排隊」的行為。等引擎的那一段在背景（派送器不等）；回 true＝是隨選的秘書、已經在開。
func (m *Module) openOnDemand(ctx context.Context, id string) bool {
	enabled, mode := m.agentMode(ctx, id)
	if !enabled || mode != "ondemand" {
		return false
	}
	m.openLater(ctx, &sdk.SessionOpenRequest{Name: id, Agent: id, Kind: sdk.SessionKindResident}, func(_ Session, err error) {
		if err != nil {
			m.warn("第一句話來了，開隨選的主要對話失敗", err)
		}
	})
	return true
}
