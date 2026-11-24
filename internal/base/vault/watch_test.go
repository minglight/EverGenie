package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func subscribe(t *testing.T, m *Module, kinds ...string) (<-chan Change, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	ch, err := m.Watch(ctx, kinds...)
	if err != nil {
		t.Fatal(err)
	}
	return ch, ctx
}
func receive(t *testing.T, ctx context.Context, ch <-chan Change) Change {
	t.Helper()
	select {
	case c, ok := <-ch:
		if !ok {
			t.Fatal("watch closed")
		}
		return c
	case <-ctx.Done():
		t.Fatal("timed out waiting for filesystem change")
		return nil
	}
}
func externalWrite(t *testing.T, m *Module, home string, r Record) {
	t.Helper()
	sc, ok := m.spec(r.Kind)
	if !ok {
		t.Fatal("missing schema")
	}
	path, err := m.recordPath(sc, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := platform.AtomicWrite(path, Encode(r, nil, sc), 0600); err != nil {
		t.Fatal(err)
	}
}

// Each barrier is a genuine external filesystem update. Repeating it allows a scan
// already in progress to finish before the next observation, without sleeps.
func awaitWatchState(t *testing.T, m *Module, home string, ctx context.Context, ch <-chan Change, ready func() bool) {
	t.Helper()
	for n := 0; !ready(); n++ {
		externalWrite(t, m, home, Record{Kind: "task", ID: "SEC-999", Fields: map[string]any{"title": fmt.Sprint(n)}, Body: fmt.Sprint(n)})
		for {
			c := receive(t, ctx, ch)
			if c.Record.ID == "SEC-999" {
				break
			}
			t.Fatalf("unexpected change while awaiting invalid/deleted state: %+v", c)
		}
	}
}

// Guards: REQ-3
func TestExternalEditNeverLost(t *testing.T) {
	m, home := openStore(t)
	old := putTask(t, m, "SEC-1")
	ch, ctx := subscribe(t, m)
	r := cloneRecord(old)
	r.Fields["title"] = "external"
	r.Body = "external body"
	externalWrite(t, m, home, r)
	_, err := m.Put(ctx, old, "code", old.Revision)
	if platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("stale Put: %v", err)
	}
	c := receive(t, ctx, ch)
	if c.Type != sdk.ChangeTypeUpdated || c.By.Name != "external" || c.Record.ID != "SEC-1" {
		t.Fatalf("unexpected change: %+v", c)
	}
	got, err := m.Get(ctx, "task", "SEC-1")
	if err != nil || got.Body != r.Body || got.Fields["title"] != "external" {
		t.Fatalf("external content lost: %+v %v", got, err)
	}
	disk, _, err := Decode(readFixture(t, filepath.Join(home, "vault/tasks/SEC-1.md")))
	if err != nil || disk.Body != r.Body {
		t.Fatalf("external disk content lost: %+v %v", disk, err)
	}
}

// Guards: REQ-4
func TestInvalidExternalFileReported(t *testing.T) {
	m, home := openStore(t)
	r := putTask(t, m, "SEC-1")
	ch, ctx := subscribe(t, m)
	path := filepath.Join(home, "vault/tasks/SEC-1.md")
	if err := platform.AtomicWrite(path, []byte("---\ntitle: [broken\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	awaitWatchState(t, m, home, ctx, ch, func() bool { return len(m.Invalid()) == 1 })
	if !reflect.DeepEqual(m.Invalid(), []string{"vault/tasks/SEC-1.md"}) {
		t.Fatal(m.Invalid())
	}
	if _, err := m.Get(ctx, "task", "SEC-1"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal(err)
	}
	list, err := m.List(ctx, "task", Query{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.ID == "SEC-1" {
			t.Fatal("invalid file listed")
		}
	}
	externalWrite(t, m, home, r)
	for {
		c := receive(t, ctx, ch)
		if c.Record.ID == "SEC-999" {
			continue
		}
		if c.Record.ID != "SEC-1" || c.Type != sdk.ChangeTypeUpdated {
			t.Fatalf("repair: %+v", c)
		}
		break
	}
	if len(m.Invalid()) != 0 {
		t.Fatal("repair did not clear invalid marker")
	}
	if _, err := m.Get(ctx, "task", "SEC-1"); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-3
func TestWatchReportsOwnWrites(t *testing.T) {
	m, home := openStore(t)
	ch, ctx := subscribe(t, m)
	second, _ := subscribe(t, m)
	r := putTask(t, m, "SEC-1")
	c := receive(t, ctx, ch)
	if c.Type != sdk.ChangeTypeCreated || c.By.Name != "code" || c.Record.Revision != r.Revision || c.Record.Fields != nil || c.Record.Body != "" || !c.At.Equal(m.d.Clock.Now()) {
		t.Fatalf("own notification: %+v", c)
	}
	c.Record.ID = "mutated"
	if receive(t, ctx, second).Record.ID != "SEC-1" {
		t.Fatal("subscribers share mutable payload")
	}
	externalWrite(t, m, home, Record{Kind: "task", ID: "SEC-2", Fields: map[string]any{"title": "external"}})
	c = receive(t, ctx, ch)
	if c.Record.ID != "SEC-2" || c.By.Name != "external" {
		t.Fatalf("duplicate own event: %+v", c)
	}
	if _, err := m.Patch(ctx, "task", "SEC-1", map[string]any{"title": "patched"}, nil, "amy"); err != nil {
		t.Fatal(err)
	}
	if c = receive(t, ctx, ch); c.Type != sdk.ChangeTypeUpdated || c.By.Name != "amy" {
		t.Fatalf("patch: %+v", c)
	}
	if err := m.Archive(ctx, "task", "SEC-1", "sweeper"); err != nil {
		t.Fatal(err)
	}
	if c = receive(t, ctx, ch); c.Type != sdk.ChangeTypeArchived || !c.Record.Archived {
		t.Fatalf("archive: %+v", c)
	}
	if err := m.Append(ctx, "journal", "code/2026-10-23", "line", "code"); err != nil {
		t.Fatal(err)
	}
	if c = receive(t, ctx, ch); c.Type != sdk.ChangeTypeCreated || c.Record.Kind != "journal" {
		t.Fatalf("append: %+v", c)
	}
}

// Guards: REQ-3
func TestWatchFiltersKinds(t *testing.T) {
	m, _ := openStore(t)
	ch, ctx := subscribe(t, m, "goal")
	all, _ := subscribe(t, m)
	putTask(t, m, "SEC-1")
	if _, err := m.Put(ctx, Record{Kind: "goal", ID: "GOAL-1"}, "amy", ""); err != nil {
		t.Fatal(err)
	}
	if c := receive(t, ctx, ch); c.Record.Kind != "goal" {
		t.Fatal(c)
	}
	if c := receive(t, ctx, all); c.Record.Kind != "task" {
		t.Fatal(c)
	}
	if c := receive(t, ctx, all); c.Record.Kind != "goal" {
		t.Fatal(c)
	}
}

// Guards: REQ-3
func TestExternalDeleteDropsFromIndex(t *testing.T) {
	m, home := openStore(t)
	putTask(t, m, "SEC-1")
	ch, ctx := subscribe(t, m)
	if err := os.Remove(filepath.Join(home, "vault/tasks/SEC-1.md")); err != nil {
		t.Fatal(err)
	}
	awaitWatchState(t, m, home, ctx, ch, func() bool {
		_, err := m.Get(ctx, "task", "SEC-1")
		return platform.CodeOf(err) == sdk.ErrorCodeNotFound
	})
	list, err := m.List(ctx, "task", Query{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.ID == "SEC-1" {
			t.Fatal("deleted record listed")
		}
	}
	log := readFixture(t, filepath.Join(home, "runtime/logs/2026-10-23.jsonl"))
	if len(log) == 0 {
		t.Fatal("missing deletion warning")
	}
}

// Guards: REQ-3
func TestWatchCancellationAndStop(t *testing.T) {
	m, _ := openStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := m.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel")
		}
	case <-deadline.Done():
		t.Fatal("cancel failed")
	}
	ch, err = m.Watch(deadline)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(deadline); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-ch; ok {
		t.Fatal("Stop did not close channel")
	}
}
