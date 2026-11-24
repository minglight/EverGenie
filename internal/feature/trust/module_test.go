package trust

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/trust/requests"
	"github.com/minglight/evergenie/internal/feature/trust/rules"
	"github.com/minglight/evergenie/sdk"
)

type fakeNotify struct{}

func (fakeNotify) Send(_ context.Context, n *sdk.Notification) (*sdk.Notification, error) {
	return n, nil
}

type fakeEngine struct{ pushed int }

func (*fakeEngine) CheckPermissions(context.Context) (map[string][]string, error) { return nil, nil }
func (e *fakeEngine) PushPolicy(context.Context) error                            { e.pushed++; return nil }

// 用假物件組起整塊，放進 App：登記合併兩個子模組的（system／web 的部分方法、policy 服務、通知來源、每分鐘的 tick、權限健檢）；
// Start／Stop 跑一輪，Start 之後申請子模組的 guidance 已經讀進來（引擎握手讀得到）。
func TestBlockAssemblesStartsAndStops(t *testing.T) {
	h := homefixture.New(t)
	ctx := context.Background()
	messages := bus.New(bus.Deps{Vault: h.Vault, Clock: h.Clock})
	messages.SetHome(h.Dir)
	eng := &fakeEngine{}
	blk := New(Deps{Vault: h.Vault, Notify: fakeNotify{}, Bus: messages, Engine: eng, Clock: h.Clock})
	blk.SetHome(h.Dir)
	// 子模組的啟動順序照 registry 原本的先後（停止反序）。
	var order []string
	for _, m := range blk.Modules() {
		order = append(order, m.Name())
	}
	if got := strings.Join(order, ","); got != "rules,requests" {
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
		services = append(services, mt.Service+"("+strings.Join(mt.Methods, "|")+")")
	}
	sort.Strings(services)
	if got := strings.Join(services, ","); got != "policy(),system(trust|set_permission_levels),web(get_trust)" {
		t.Fatalf("路由 %s", got)
	}
	if sources, err := all.NoticeSources(); err != nil || len(sources) != 1 || sources[0].Name != sdk.NotifySourcePolicy {
		t.Fatalf("通知來源 %+v %v", sources, err)
	}
	if jobs, err := all.Jobs(); err != nil || len(jobs) != 1 || jobs[0].Name != "policy.tick" {
		t.Fatalf("定時工作 %+v %v", jobs, err)
	}
	if checks, err := all.Checks(); err != nil || len(checks) == 0 {
		t.Fatalf("權限健檢要登記（接了 Engine）：%+v %v", checks, err)
	}
	if err := all.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if g := blk.Requests().ActiveGuidance(); len(g) != 0 {
		t.Fatalf("空的家沒有 guidance：%v", g)
	}
	if blk.Levels() == nil {
		t.Fatal("信任等級子模組要拿得到")
	}
	if err := all.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

// 塊的指令就是兩個子套件的指令合起來。
func TestBlockCommandsMergeSubpackages(t *testing.T) {
	printDoctor := func(*cmdline.Env, []sdk.DoctorCheck) error { return nil }
	cmds, err := Commands(printDoctor)
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, set := range []map[string]app.Command{rules.Commands(printDoctor), requests.Commands()} {
		want += len(set)
		for name := range set {
			if _, ok := cmds[name]; !ok {
				t.Errorf("少了 %s", name)
			}
		}
	}
	if len(cmds) != want {
		t.Fatalf("指令數 %d，要 %d", len(cmds), want)
	}
}
