package bus

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

func fixture(t *testing.T) (*Module, *homefixture.Home) {
	t.Helper()
	h := homefixture.New(t)
	m := New(Deps{Vault: h.Vault, Clock: h.Clock})
	m.SetHome(h.Dir)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return m, h
}
func send(t *testing.T, m *Module) Envelope {
	t.Helper()
	e, err := m.Send(context.Background(), &sdk.Envelope{From: "owner(web)", To: "code", Kind: sdk.EnvelopeKindChat, Body: "完整訊息"})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func advance(t *testing.T, m *Module, id string, states ...Lifecycle) {
	t.Helper()
	for _, s := range states {
		if err := m.Transition(context.Background(), id, s, TransitionOpt{}); err != nil {
			t.Fatal(err)
		}
	}
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("channel closed")
		}
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}
	var v T
	return v
}

// Guards: REQ-11
func TestLifecycleOrder(t *testing.T) {
	m, _ := fixture(t)
	ch, err := m.Watch(context.Background(), "code", "")
	if err != nil {
		t.Fatal(err)
	}
	e := send(t, m)
	advance(t, m, e.ID, sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleWorking, sdk.LifecycleBlocked, sdk.LifecycleWorking, sdk.LifecycleReplied)
	want := []Lifecycle{sdk.LifecycleQueued, sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleWorking, sdk.LifecycleBlocked, sdk.LifecycleWorking, sdk.LifecycleReplied}
	for _, s := range want {
		v := receive(t, ch)
		if v.Envelope.Lifecycle != s || v.Step.State != s {
			t.Fatalf("watch=%+v want=%s", v, s)
		}
	}
	got, err := m.Get(context.Background(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.History) != len(want) {
		t.Fatal(got.History)
	}
	for i, s := range want {
		if got.History[i].State != s {
			t.Fatal(got.History)
		}
	}
}

// Guards: REQ-11
func TestHistoryMonotonic(t *testing.T) {
	m, h := fixture(t)
	e := send(t, m)
	h.Clock.Advance(-time.Hour)
	advance(t, m, e.ID, sdk.LifecycleNotified, sdk.LifecycleReceived)
	got, err := m.Get(context.Background(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(got.History); i++ {
		if got.History[i].At.Before(got.History[i-1].At) {
			t.Fatal(got.History)
		}
	}
}

// Guards: REQ-11, REQ-378
func TestLifecycleTable(t *testing.T) {
	m, _ := fixture(t)
	paths := map[Lifecycle][]Lifecycle{
		sdk.LifecycleQueued: {}, sdk.LifecycleNotified: {sdk.LifecycleNotified}, sdk.LifecycleReceived: {sdk.LifecycleNotified, sdk.LifecycleReceived},
		sdk.LifecycleWorking: {sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleWorking}, sdk.LifecycleBlocked: {sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleBlocked},
		sdk.LifecycleReplied: {sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleReplied}, sdk.LifecycleUnanswered: {sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleUnanswered},
		sdk.LifecycleInterrupted: {sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleInterrupted}, sdk.LifecycleFailed: {sdk.LifecycleFailed},
	}
	allowed := map[Lifecycle][]Lifecycle{
		sdk.LifecycleQueued: {sdk.LifecycleNotified, sdk.LifecycleFailed}, sdk.LifecycleNotified: {sdk.LifecycleQueued, sdk.LifecycleReceived, sdk.LifecycleFailed},
		// received → queued：收件方額度用完，那一輪什麼都沒做，退回排隊（REQ-378）。
		sdk.LifecycleReceived: {sdk.LifecycleWorking, sdk.LifecycleBlocked, sdk.LifecycleReplied, sdk.LifecycleUnanswered, sdk.LifecycleInterrupted, sdk.LifecycleQueued},
		sdk.LifecycleWorking:  {sdk.LifecycleBlocked, sdk.LifecycleReplied, sdk.LifecycleUnanswered, sdk.LifecycleInterrupted}, sdk.LifecycleBlocked: {sdk.LifecycleWorking, sdk.LifecycleInterrupted},
	}
	for from, path := range paths {
		for to := range paths {
			t.Run(string(from)+"-"+string(to), func(t *testing.T) {
				e := send(t, m)
				advance(t, m, e.ID, path...)
				valid := false
				for _, s := range allowed[from] {
					if s == to {
						valid = true
					}
				}
				err := m.Transition(context.Background(), e.ID, to, TransitionOpt{Reason: "原因", LastMessage: strings.Repeat("字", 201)})
				if valid && err != nil {
					t.Fatal(err)
				}
				if !valid && platform.CodeOf(err) != sdk.ErrorCodeConflict {
					t.Fatalf("invalid transition accepted: %v", err)
				}
				if valid {
					got, err := m.Get(context.Background(), e.ID)
					if err != nil {
						t.Fatal(err)
					}
					if got.Reason != "原因" || len([]rune(got.LastMessage)) != 200 {
						t.Fatal(got)
					}
					if from == sdk.LifecycleNotified && to == sdk.LifecycleQueued && got.Attempts != 1 {
						t.Fatal(got.Attempts)
					}
				}
			})
		}
	}
}

// Guards: REQ-12
func TestSendIdempotent(t *testing.T) {
	m, _ := fixture(t)
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := m.Send(context.Background(), &sdk.Envelope{From: "code", To: "life", Kind: sdk.EnvelopeKindSay, IdempotencyKey: "same"})
			if err != nil {
				t.Error(err)
				return
			}
			ids <- e.ID
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate send")
		}
	}
	list, err := m.List(context.Background(), "life")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
}

// Guards: REQ-13
func TestQueuedWhenTargetMissing(t *testing.T) {
	m, _ := fixture(t)
	e := send(t, m)
	if e.Lifecycle != sdk.LifecycleQueued {
		t.Fatal(e)
	}
	list, err := m.List(context.Background(), "code", sdk.LifecycleQueued)
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, err)
	}
}

// Guards: REQ-14
func TestDirectRouteStartsNotified(t *testing.T) {
	m, _ := fixture(t)
	e, err := m.Send(context.Background(), &sdk.Envelope{From: "work", To: "code", Kind: sdk.EnvelopeKindSay, Route: sdk.EnvelopeRouteDirect})
	if err != nil || e.Lifecycle != sdk.LifecycleNotified {
		t.Fatalf("%v %v", e, err)
	}
}

// Guards: REQ-14
func TestSayAcrossAccounts(t *testing.T) {
	m, _ := fixture(t)
	e, err := m.Send(context.Background(), &sdk.Envelope{From: "work", To: "code-other-account", Kind: sdk.EnvelopeKindSay})
	if err != nil || e.Route != sdk.EnvelopeRouteCore || e.Lifecycle != sdk.LifecycleQueued {
		t.Fatalf("%v %v", e, err)
	}
}

// Guards: REQ-11
func TestRestartRestoresQueue(t *testing.T) {
	m, h := fixture(t)
	first := send(t, m)
	advance(t, m, first.ID, sdk.LifecycleNotified, sdk.LifecycleReceived)
	second := send(t, m)
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := New(Deps{Clock: h.Clock, Vault: h.Vault})
	next.SetHome(h.Dir)
	if err := next.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer next.Stop(context.Background())
	list, err := next.List(context.Background(), "code")
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %v", list, err)
	}
	if list[0].ID != first.ID || list[0].Lifecycle != sdk.LifecycleReceived || list[0].Body != first.Body || list[1].ID != second.ID {
		t.Fatalf("restored=%+v", list)
	}
}

// Guards: REQ-12
func TestConcurrentSendKeepsOrder(t *testing.T) {
	m, _ := fixture(t)
	watch, err := m.Watch(context.Background(), "code", "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := m.Send(context.Background(), &sdk.Envelope{From: "work", To: "code", Kind: sdk.EnvelopeKindSay, Body: strconv.Itoa(i)})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	list, err := m.List(context.Background(), "code")
	if err != nil || len(list) != 100 {
		t.Fatalf("count=%d err=%v", len(list), err)
	}
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		v := receive(t, watch)
		if seen[v.Envelope.ID] {
			t.Fatal("duplicate")
		}
		seen[v.Envelope.ID] = true
		if v.Envelope.ID != list[i].ID {
			t.Fatal("mailbox order differs from committed event order")
		}
	}
}

// Guards: REQ-12
func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	m, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slow, err := m.Subscribe(ctx, "item.*")
	if err != nil {
		t.Fatal(err)
	}
	fast, err := m.Subscribe(ctx, "item.*")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 1100; i++ {
			if err := m.Publish(ctx, "item.changed", map[string]any{"n": i, "nested": []string{"original"}}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sender blocked")
	}
	for i := 0; i < 1100; i++ {
		v := receive(t, fast)
		if v.Payload["n"] != i {
			t.Fatal("lost or reordered event")
		}
		v.Payload["nested"].([]string)[0] = "changed"
	}
	for i := 0; i < 1100; i++ {
		v := receive(t, slow)
		if v.Payload["n"] != i || v.Payload["nested"].([]string)[0] != "original" {
			t.Fatal("shared payload or lost event")
		}
	}
	cancel()
	select {
	case _, ok := <-slow:
		if ok {
			t.Fatal("expected close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("subscription did not close")
	}
}

// Guards: REQ-89
func TestNoTypedTextInBusPackage(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if p == "os/exec" || p == "golang.org/x/term" || p == "github.com/creack/pty" {
				t.Fatalf("forbidden import %s", p)
			}
		}
	}
}

// Guards: REQ-3
func TestVaultChangesForwarded(t *testing.T) {
	m, h := fixture(t)
	ch, err := m.Subscribe(context.Background(), "vault.changed")
	if err != nil {
		t.Fatal(err)
	}
	h.Vault.RegisterSchema(vault.Spec{Kind: "task", Path: func(id string) string { return "vault/tasks/" + id + ".md" }})
	if _, err := h.Vault.Put(context.Background(), sdk.Record{Kind: "task", ID: "SEC-1", Fields: map[string]any{}}, "system", ""); err != nil {
		t.Fatal(err)
	}
	e := receive(t, ch)
	if e.Payload["id"] != "SEC-1" || e.Payload["kind"] != "task" || e.Payload["by"] != "system" || e.Payload["type"] != "created" {
		t.Fatal(e)
	}
}

// Guards: REQ-7
func TestSweepDeletesOldEnded(t *testing.T) {
	m, h := fixture(t)
	for _, state := range []Lifecycle{sdk.LifecycleReplied, sdk.LifecycleUnanswered, sdk.LifecycleInterrupted, sdk.LifecycleFailed} {
		e := send(t, m)
		if state != sdk.LifecycleFailed {
			advance(t, m, e.ID, sdk.LifecycleNotified, sdk.LifecycleReceived)
		}
		advance(t, m, e.ID, state)
	}
	pending := send(t, m)
	h.Clock.Advance(7 * 24 * time.Hour)
	n, err := m.Sweep(context.Background(), h.Clock.Now())
	if err != nil || n != 3 {
		t.Fatalf("7d n=%d err=%v", n, err)
	}
	h.Clock.Advance(23 * 24 * time.Hour)
	n, err = m.Sweep(context.Background(), h.Clock.Now())
	if err != nil || n != 1 {
		t.Fatalf("30d n=%d err=%v", n, err)
	}
	if _, err := os.Stat(m.path(pending.ID)); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(h.Dir, "runtime", "queue", "*.md"))
	if err != nil || len(files) != 1 {
		t.Fatal(fmt.Sprintf("files=%v err=%v", files, err))
	}
}
