package session

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

// waitPending 等到 session 上有 Pending（確認是引擎在另一個 goroutine 發的）。
func (f *fixture) waitPending(t *testing.T, name string) *sdk.Pending {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := f.m.Get(context.Background(), name); err == nil && s.Pending != nil {
			return s.Pending
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no pending approval")
	return nil
}

// Guards: REQ-87
func TestSessionSuitePassesWithFakeEngine(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	f.e.Configure(fake.Behavior{Delivered: true, Events: []sdk.EngineEvent{{Type: sdk.EngineEventTypeTurnStarted}, {Type: sdk.EngineEventTypeActivityStarted, Activity: "write"}, {Type: sdk.EngineEventTypeActivityEnded}, {Type: sdk.EngineEventTypeTurnEnded}}, Approvals: []sdk.EngineHostRequestApprovalRequest{{Kind: sdk.ApprovalKindPermission, Summary: "寫入"}}, Actions: []sdk.EngineHostCheckActionRequest{{Class: sdk.ActionClassWrite}}})
	done := make(chan error, 1)
	go func() {
		_, err := f.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "code", Text: "fixture"})
		done <- err
	}()
	f.answer(t, f.waitPending(t, "code"), &sdk.Decision{Allow: true})
	must(t, <-done)
	f.state(t, sdk.SessionStateIdle)
	results := f.e.Results()
	if len(results.Approvals) != 1 || !results.Approvals[0].Decided || len(results.Actions) != 1 || results.Actions[0].Verdict != sdk.ActionVerdictAllow {
		t.Fatal(results)
	}
}

// Guards: REQ-144
func TestNoVendorTermsInSession(t *testing.T) {
	cmd := exec.Command("go", "run", "../../tools/enginecheck", "../../..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("neutral vocabulary check: %s: %v", out, err)
	}
}
