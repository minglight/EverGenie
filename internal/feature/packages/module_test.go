package packages

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/packages/pkg"
	"github.com/minglight/evergenie/sdk"
)

type blockAgents struct{}

func (blockAgents) Resolve(context.Context, string) (string, bool) { return "", false }

// 用假物件組起整塊（空的官方資料夾、家裡沒有套件），放進 App：登記合併兩個子模組的（package 與 vault 服務、通知來源、監督的定時工作、
// 兩邊的健檢）；Start／Stop 跑一輪。
func TestModuleAssemblesStartsAndStops(t *testing.T) {
	h := homefixture.New(t)
	ctx := context.Background()
	messages := bus.New(bus.Deps{Vault: h.Vault, Clock: h.Clock})
	messages.SetHome(h.Dir)
	blk := New(Deps{Agents: blockAgents{}, Bus: messages, Vault: h.Vault, Clock: h.Clock})
	blk.SetOfficialDir(t.TempDir())
	blk.SetHome(h.Dir)
	// 子模組的啟動順序照 registry 原本的先後（停止反序）。
	var order []string
	for _, m := range blk.Modules() {
		order = append(order, m.Name())
	}
	if got := strings.Join(order, ","); got != "data,pkg" {
		t.Fatalf("子模組順序 %s", got)
	}
	all := app.New(messages, blk)
	if err := all.Assemble(); err != nil {
		t.Fatal(err)
	}
	mounts, err := all.Mounts()
	if err != nil {
		t.Fatal(err)
	}
	var services []string
	for _, mt := range mounts {
		services = append(services, mt.Service)
	}
	sort.Strings(services)
	if got := strings.Join(services, ","); got != "package,vault" {
		t.Fatalf("路由 %s", got)
	}
	if sources, err := all.NoticeSources(); err != nil || len(sources) != 1 || sources[0].Name != sdk.NotifySourcePackage {
		t.Fatalf("通知來源 %+v %v", sources, err)
	}
	if jobs, err := all.Jobs(); err != nil || len(jobs) == 0 {
		t.Fatalf("套件監督的定時工作要登記：%+v %v", jobs, err)
	}
	if checks, err := all.Checks(); err != nil || len(checks) == 0 {
		t.Fatalf("健檢 %+v %v", checks, err)
	}
	if err := all.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if blk.Store() == nil {
		t.Fatal("套件子模組要拿得到")
	}
	stop, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := all.Stop(stop); err != nil {
		t.Fatal(err)
	}
}

// 塊的指令就是套件子模組的指令。
func TestModuleCommandsAreThePackageCommands(t *testing.T) {
	cmds, err := Commands()
	if err != nil {
		t.Fatal(err)
	}
	if want := pkg.Commands(); len(cmds) != len(want) || len(cmds) == 0 {
		t.Fatalf("指令數 %d，要 %d", len(cmds), len(want))
	}
}
