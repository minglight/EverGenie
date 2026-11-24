package engine

import (
	"context"
	"sort"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// cardsTimeout 是對每個帳號送 WriteAgentCards 的上限。
const cardsTimeout = 10 * time.Second

// WriteAgentCards 把名冊交給每個設定過的帳號的引擎（agents link，Q3）：apply=false 只預演，引擎不寫任何檔。
// 帳號的 engine_config 原樣帶過去（核心不解讀，ENG-4）。引擎沒在跑或回錯，該帳號只放 Note，其他帳號照常。
func (m *Module) WriteAgentCards(ctx context.Context, cards []sdk.AgentCard, userName string, apply bool) ([]sdk.AgentLinkAccount, error) {
	m.mu.Lock()
	r := m.registry
	m.mu.Unlock()
	if r == nil {
		return nil, platform.Unavailable("引擎服務尚未啟動")
	}
	type target struct {
		name   string
		api    sdk.EngineAPI
		config map[string]any
	}
	r.mu.RLock()
	targets := make([]target, 0, len(r.accounts))
	for name, a := range r.accounts {
		targets = append(targets, target{name, r.clients[a.engine], a.config})
	}
	r.mu.RUnlock()
	sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })
	out := make([]sdk.AgentLinkAccount, 0, len(targets))
	for _, t := range targets {
		row := sdk.AgentLinkAccount{Account: t.name}
		if t.api == nil {
			row.Note = "這個帳號的引擎沒有在跑，略過"
			out = append(out, row)
			continue
		}
		call, cancel := context.WithTimeout(ctx, cardsTimeout)
		resp, err := t.api.WriteAgentCards(call, &sdk.EngineWriteAgentCardsRequest{Account: t.name, EngineConfig: t.config, UserName: userName, Cards: cards, DryRun: !apply})
		cancel()
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case err != nil:
			row.Note = "無法處理：" + failureText(err)
		case resp != nil:
			row.Note, row.Files = resp.Note, resp.Files
		}
		out = append(out, row)
	}
	return out, nil
}
