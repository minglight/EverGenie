package bus

import (
	"context"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Reroute moves a queued envelope between mailboxes as one exclusive operation.
func (m *Module) Reroute(ctx context.Context, id, to string) error {
	m.life.Lock()
	defer m.life.Unlock()
	if err := m.ready(ctx); err != nil {
		return err
	}
	if to == "" {
		return platform.Invalid("to", "請指定收件者")
	}
	e, err := m.get(ctx, id)
	if err != nil {
		return err
	}
	if e.Lifecycle != sdk.LifecycleQueued {
		return platform.Conflict("只有排隊中的訊息可以改送")
	}
	old := m.box(e.To)
	next := clone(e)
	next.To = to
	at := m.d.Clock.Now()
	if last := next.History[len(next.History)-1].At; at.Before(last) {
		at = last
	}
	next.History = append(next.History, sdk.LifecycleStep{State: sdk.LifecycleQueued, At: at, Detail: "改送 " + to})
	if err = m.persist(next); err != nil {
		return err
	}
	// life excludes Send/Get/List/Transition/Stop, so no reader sees a partial move.
	if err = m.call(context.Background(), old, func() {
		for i, row := range old.records {
			if row.ID == id {
				old.records = append(old.records[:i], old.records[i+1:]...)
				break
			}
		}
	}); err != nil {
		return err
	}
	target := m.box(to)
	// After persistence, finish the in-memory move even if the caller cancels.
	if err = m.call(context.Background(), target, func() { target.records = append(target.records, next) }); err != nil {
		return err
	}
	m.changed(next)
	return nil
}
