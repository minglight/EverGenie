package bus

import (
	"context"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-154
func TestRerouteOnlyQueued(t *testing.T) {
	m, _ := fixture(t)
	ctx := context.Background()
	e := send(t, m)
	advance(t, m, e.ID, sdk.LifecycleNotified, sdk.LifecycleQueued)
	before, err := m.Get(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := m.Subscribe(ctx, "bus.envelope.state")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Reroute(ctx, e.ID, "code-c1"); err != nil {
		t.Fatal(err)
	}
	after, err := m.Get(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.To != "code-c1" || after.ID != before.ID || after.Attempts != before.Attempts || !after.CreatedAt.Equal(before.CreatedAt) || len(after.History) != len(before.History)+1 || after.History[len(after.History)-1].Detail != "改送 code-c1" {
		t.Fatal(after)
	}
	ev := receive(t, ch)
	if ev.Payload["to"] != "code-c1" || ev.Payload["state"] != "queued" {
		t.Fatal(ev)
	}
	old, err := m.List(ctx, "code")
	if err != nil || len(old) != 0 {
		t.Fatal(old, err)
	}
	next, err := m.List(ctx, "code-c1")
	if err != nil || len(next) != 1 {
		t.Fatal(next, err)
	}
	advance(t, m, e.ID, sdk.LifecycleNotified)
	if err = m.Reroute(ctx, e.ID, "code-c2"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatal(err)
	}
}
