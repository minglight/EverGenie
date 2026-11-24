package bus

import (
	"context"
	"testing"
)

// Guards: REQ-118
func TestModuleShape(t *testing.T) {
	m := New(Deps{})
	if got := m.Name(); got != "bus" {
		t.Fatalf("Name = %q", got)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
