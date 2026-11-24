package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/feature/inbox/push"
)

// Guards: REQ-118
func TestModuleShape(t *testing.T) {
	m := New(Deps{Push: push.New(t.TempDir())})
	if got := m.Name(); got != "notify" {
		t.Fatalf("Name = %q", got)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// 沒有接推播通道（Deps.Push 是 nil）是組裝錯了：啟動就報錯，不默默當沒人訂閱。
func TestStartFailsWithoutPushChannel(t *testing.T) {
	m := New(Deps{})
	err := m.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Deps.Push") {
		t.Fatalf("Start 要報沒有推播通道：%v", err)
	}
}
