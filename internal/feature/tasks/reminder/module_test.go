package reminder

import (
	"context"
	"testing"
)

// Guards: REQ-118
func TestModuleShape(t *testing.T) {
	m, _, _ := fixture(t)
	if got := m.Name(); got != "reminder" {
		t.Fatalf("Name = %q", got)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
