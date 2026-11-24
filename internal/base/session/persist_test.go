package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("background write did not finish")
	}
}
func readState(t *testing.T, f *fixture) vault.Record {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.h.Dir, "runtime/sessions/code.md"))
	must(t, err)
	r, _, err := vault.Decode(data)
	must(t, err)
	return r
}

// Guards: REQ-77
func TestDirtyStateEventuallyPersists(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	written := make(chan struct{}, 8)
	f.m.mu.Lock()
	f.m.writeState = func(path string, data []byte) error {
		err := platform.AtomicWrite(path, data, 0600)
		if err == nil {
			written <- struct{}{}
		}
		return err
	}
	f.m.mu.Unlock()
	f.event(t, sdk.EngineEventTypeTurnStarted)
	waitSignal(t, written)
	if got := text(readState(t, f).Fields, "state"); got != "busy" {
		t.Fatal(got)
	}
}

// Guards: REQ-77
func TestStopFlushesLatestDirtyStateAndRestores(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.m.mu.Lock()
	f.m.writeState = func(path string, data []byte) error {
		once.Do(func() { close(entered); <-release })
		return platform.AtomicWrite(path, data, 0600)
	}
	f.m.mu.Unlock()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	f.event(t, sdk.EngineEventTypeTurnStarted)
	waitSignal(t, entered)
	// A blocked disk write must not hold the memory-state mutex.
	returned := make(chan error, 1)
	go func() {
		returned <- f.m.ReportEvent(context.Background(), &sdk.EngineEvent{Session: "code", Type: sdk.EngineEventTypeActivityStarted, Activity: "latest"})
	}()
	select {
	case err := <-returned:
		must(t, err)
	case <-time.After(time.Second):
		t.Fatal("disk write blocked ReportEvent")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- f.m.Stop(context.Background()) }()
	close(release)
	select {
	case err := <-stopped:
		must(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not flush")
	}
	r := readState(t, f)
	a, _ := r.Fields["activity"].(map[string]any)
	if text(a, "tool") != "latest" {
		t.Fatal(r)
	}
	must(t, f.m.start(context.Background(), false))
	state, err := f.m.Get(context.Background(), "code")
	must(t, err)
	if state.State != sdk.SessionStateBusy || state.LastEvent != string(sdk.EngineEventTypeActivityStarted) {
		t.Fatal(state)
	}
	f.m.mu.Lock()
	activity := f.m.entries["code"].activity
	f.m.mu.Unlock()
	if activity != "latest" {
		t.Fatal(activity)
	}
}

// Guards: REQ-77
func TestDirtyWriteFailureRetries(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	failed, written := make(chan struct{}, 1), make(chan struct{}, 1)
	attempts := 0
	f.m.mu.Lock()
	f.m.writeState = func(path string, data []byte) error {
		attempts++
		if attempts == 1 {
			failed <- struct{}{}
			return errors.New("fixture write failure")
		}
		err := platform.AtomicWrite(path, data, 0600)
		if err == nil {
			select {
			case written <- struct{}{}:
			default:
			}
		}
		return err
	}
	f.m.mu.Unlock()
	f.event(t, sdk.EngineEventTypeTurnStarted)
	waitSignal(t, failed)
	waitSignal(t, written)
	if got := text(readState(t, f).Fields, "state"); got != "busy" {
		t.Fatal(got)
	}
}
