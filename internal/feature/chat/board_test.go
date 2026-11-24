package chat

import (
	"context"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

type threadSessions struct {
	Sessions
	list []*sdk.ThreadSummary
}

func (s threadSessions) Threads(context.Context, string, bool) ([]*sdk.ThreadSummary, int, error) {
	return s.list, 0, nil
}

// 首頁「等你回覆的討論串」：Board 登記 waiting_threads，只收 waiting_user；沒接 Sessions 不登記。
// Guards: REQ-109
func TestBoardWaitingThreads(t *testing.T) {
	m := New(Deps{Sessions: threadSessions{list: []*sdk.ThreadSummary{
		{Name: "code", Status: sdk.ThreadStatusWaitingUser}, {Name: "life", Status: sdk.ThreadStatusIdle}, nil,
	}}})
	parts := m.Board()
	if len(parts) != 1 || parts[0].Key != "waiting_threads" {
		t.Fatalf("%+v", parts)
	}
	v, err := parts[0].Load(context.Background())
	got, ok := v.([]sdk.ThreadSummary)
	if err != nil || !ok || len(got) != 1 || got[0].Name != "code" {
		t.Fatalf("%#v %v", v, err)
	}
	if len(New(Deps{}).Board()) != 0 {
		t.Fatal("沒接 Sessions 不登記")
	}
}
