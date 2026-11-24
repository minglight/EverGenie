package chat

import (
	"context"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// 假的底層：組裝與 Start／Stop 都不會呼叫它們的方法（嵌入 nil interface，呼叫到就 panic，等於斷言沒用到）。
type blockSessions struct{ Sessions }
type blockBus struct{ Bus }
type blockTasks struct{ Tasks }
type blockRoster struct{}

func (blockRoster) List(context.Context) ([]*sdk.Record, error) { return nil, nil }

// chat 塊只有一個子模組（就是這個套件的 Module，S8-3 決定不另外包）：用假物件組起來放進 App，
// 登記 session／bus 的門面路由、首頁「等你回覆的討論串」、健檢；Start／Stop 跑一輪。
func TestBlockAssemblesStartsAndStops(t *testing.T) {
	h := homefixture.New(t)
	ctx := context.Background()
	blk := New(Deps{Sessions: blockSessions{}, Bus: blockBus{}, Tasks: blockTasks{}, Roster: blockRoster{}, Clock: h.Clock})
	blk.SetHome(h.Dir)
	all := app.New(blk)
	if err := all.Assemble(); err != nil {
		t.Fatal(err)
	}
	if mounts, err := all.Mounts(); err != nil || len(mounts) == 0 {
		t.Fatalf("路由 %+v %v", mounts, err)
	}
	if parts, err := all.Board(); err != nil || len(parts) != 1 || parts[0].Key != "waiting_threads" {
		t.Fatalf("首頁資料 %+v %v", parts, err)
	}
	if checks, err := all.Checks(); err != nil || len(checks) == 0 {
		t.Fatalf("健檢 %+v %v", checks, err)
	}
	if err := all.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := all.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
