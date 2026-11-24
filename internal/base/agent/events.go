package agent

import (
	"context"
	"github.com/minglight/evergenie/sdk"
)

func (m *Module) subscribe(ctx context.Context) error {
	run, cancel := context.WithCancel(ctx)
	events, err := m.d.Bus.Subscribe(run, "vault.changed")
	if err != nil {
		cancel()
		return err
	}
	m.cancel = cancel
	m.done = make(chan struct{})
	done := m.done
	go func() {
		defer close(done)
		defer func() {
			if recover() != nil {
				m.log.Warn("agent 事件處理中斷")
			}
		}()
		for {
			select {
			case <-run.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				if ev == nil {
					continue
				}
				if err := m.handle(run, ev); err != nil && run.Err() == nil {
					m.log.Warn("agent 事件處理失敗", "error", err)
				}
			}
		}
	}()
	return nil
}
func (m *Module) startEvents(ctx context.Context) error { return m.subscribe(ctx) }
func (m *Module) handle(ctx context.Context, ev *sdk.BusSubscribeResponse) error {
	if ev.Topic != "vault.changed" {
		return nil
	}
	switch ev.Payload["kind"] {
	case "agent":
		id, _ := ev.Payload["id"].(string)
		if err := m.RebuildNoteIndexes(ctx); err != nil { // 名冊變了：職位索引跟著多一份或少一份
			m.warn("主題筆記的索引重產失敗", err)
		}
		return m.changed(ctx, id, "updated")
	case TopicKind, CategoryKind:
		// 主題筆記變了（含主人在 Obsidian 手改，VFS-8）：重產兩層索引；內容沒變的檔不寫。
		return m.RebuildNoteIndexes(ctx)
	}
	return nil
}
