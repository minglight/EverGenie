package session

import (
	"context"
	"sort"
	"sync"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// bootPrompter 是引擎「列出啟動時需要人選的對話框」的能力（engine.Engine 有；session 自己的 EngineAccess 不放進去，
// 測試的假引擎不用跟著改）。
type bootPrompter interface {
	BootPrompts(ctx context.Context, session string) ([]*sdk.BootPrompt, error)
	AnswerBootPrompt(ctx context.Context, id, choice, by string) error
}

// BootPrompts 列出現在卡在啟動對話框（例如「信任這個資料夾」）的 session（DEV-87）：沒停的 session 各自問它的引擎。
// 引擎不會回答（沒這個能力、連不上、limits.BootPrompt 內沒回）的 session 略過，不當成錯誤——這是健檢用的，查不到不能擋住其他項目。
// 每個 session 同時問、各自有時限：一位的引擎不回，別位的提示照樣在時限內交回去（DEV-159）。
// 結果依 session 名稱、對話框編號排序。
func (m *Module) BootPrompts(ctx context.Context) ([]*sdk.BootPrompt, error) {
	list, err := m.List(ctx, "", nil)
	if err != nil {
		return nil, err
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []*sdk.BootPrompt
	)
	for _, s := range list {
		if s == nil || s.State == sdk.SessionStateStopped {
			continue
		}
		engine, err := m.d.Engine.For(s.Account)
		if err != nil {
			continue
		}
		bp, ok := engine.(bootPrompter)
		if !ok {
			continue
		}
		name := s.Name
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer m.recoverWorker()
			call, cancel := context.WithTimeout(ctx, m.limits.BootPrompt)
			defer cancel()
			prompts, err := bp.BootPrompts(call, name)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, p := range prompts {
				if p != nil {
					out = append(out, p)
				}
			}
		}()
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Session != out[j].Session {
			return out[i].Session < out[j].Session
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// AnswerBootPrompt 回答一個 session 卡住的啟動對話框：先確認這個對話框現在真的在那個 session 上，再交給它的引擎按鍵。
func (m *Module) AnswerBootPrompt(ctx context.Context, session, id, choice, by string) error {
	list, err := m.List(ctx, "", nil)
	if err != nil {
		return err
	}
	for _, s := range list {
		if s == nil || s.Name != session || s.State == sdk.SessionStateStopped {
			continue
		}
		engine, err := m.d.Engine.For(s.Account)
		if err != nil {
			return err
		}
		bp, ok := engine.(bootPrompter)
		if !ok {
			break
		}
		call, cancel := context.WithTimeout(ctx, m.limits.Answer)
		defer cancel()
		prompts, err := bp.BootPrompts(call, session)
		if err == nil {
			for _, p := range prompts {
				if p != nil && p.ID == id {
					err = bp.AnswerBootPrompt(call, id, choice, by)
					if err == nil {
						return nil
					}
					break
				}
			}
		}
		if err != nil {
			if gaveUp(call, ctx) {
				return silent("替你按秘書畫面上的啟動提示", m.limits.Answer, "不確定按到了沒；看一下秘書的畫面，還在的話再按一次。")
			}
			return err
		}
		break
	}
	return platform.NotFound("這個啟動提示已經不在了")
}
