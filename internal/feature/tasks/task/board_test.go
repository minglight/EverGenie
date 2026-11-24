package task

import (
	"context"
	"reflect"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 首頁「等你處理的任務單」：Board 登記 waiting_tasks，內容跟 Waiting（tasks.waiting）一樣。
// Guards: REQ-109
func TestBoardWaitingTasks(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	parts := m.Board()
	if len(parts) != 1 || parts[0].Key != "waiting_tasks" {
		t.Fatalf("%+v", parts)
	}
	got, err := parts[0].Load(ctx)
	must(t, err)
	want, err := m.Waiting(ctx, h.Clock.Now())
	must(t, err)
	if _, ok := got.([]sdk.WaitingTask); !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("%#v != %#v", got, want)
	}
}
