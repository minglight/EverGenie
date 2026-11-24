package session

import (
	"context"
	"testing"
)

// Guards: REQ-118
func TestModuleShape(t *testing.T) {
	m := New(Deps{})
	tr := newTestTrust()
	m.SetTrust(tr, tr)
	if got := m.Name(); got != "session" {
		t.Fatalf("Name = %q", got)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStartRejectsMissingTrust(t *testing.T) {
	m := New(Deps{})
	if err := m.Start(context.Background()); err == nil {
		t.Fatal("未接信任能力仍啟動")
	}
}
