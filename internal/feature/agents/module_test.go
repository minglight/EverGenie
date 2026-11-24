package agents

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/agents/notes"
	"github.com/minglight/evergenie/internal/feature/agents/persona"
	"github.com/minglight/evergenie/internal/feature/agents/roster"
	"github.com/minglight/evergenie/sdk"
)

// 名冊（底層 agent）用假的：只有名冊清單有東西，其他方法 Start 不會叫到。
type fakeRoster struct{ roster.Roster }

func (fakeRoster) List(context.Context) ([]*sdk.Record, error) {
	return []*sdk.Record{{Kind: "agent", ID: "code", Fields: map[string]any{"persona": "athena"}}}, nil
}

type fakeHealth struct{}

func (fakeHealth) Problems(context.Context) []string { return nil }
func (fakeHealth) Warnings(context.Context) []string { return nil }

type fakeTester struct{ persona.Tester }

// 用假物件組起整塊，放進 App：登記合併子模組的（名冊的 agents／看板名冊、角色庫的 personas、健檢、角色包再試）；
// 接上 setter 之後 Start／Stop 跑一輪。
func TestBlockAssemblesStartsAndStops(t *testing.T) {
	h := homefixture.New(t)
	ctx := context.Background()
	blk := New(Deps{Roster: fakeRoster{}, Clock: h.Clock, Health: fakeHealth{}})
	blk.SetTester(fakeTester{})
	blk.SetPackPointer(func(home string) string { return home + "/pointer" })
	blk.SetHome(h.Dir)
	// 子模組的啟動順序照 registry 原本的先後（停止反序）。
	var order []string
	for _, m := range blk.Modules() {
		order = append(order, m.Name())
	}
	if got := strings.Join(order, ","); got != "roster,persona,notes" {
		t.Fatalf("子模組順序 %s", got)
	}
	all := app.New(blk)
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
	if got := strings.Join(services, ","); got != "agents(),notes(),personas(),web(get_roster|get_agents)" {
		t.Fatalf("路由 %s", got)
	}
	jobs, err := all.Jobs()
	if err != nil || len(jobs) != 3 || jobs[0].Name != "personas.pack_retry" || jobs[1].Name != "personas.intros" || jobs[2].Name != "personas.transfer_sweep" {
		t.Fatalf("定時工作 %+v %v", jobs, err)
	}
	if checks, err := all.Checks(); err != nil || len(checks) != 2 {
		t.Fatalf("健檢 %+v %v", checks, err)
	}
	if err := all.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if blk.Admin() == nil || blk.Library() == nil {
		t.Fatal("子模組要拿得到")
	}
	if err := all.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

// 塊的指令就是兩個子套件的指令合起來。
func TestBlockCommandsMergeSubpackages(t *testing.T) {
	label := func(sdk.Session) string { return "" }
	pointer := func(string) string { return "" }
	cmds, err := Commands(label, pointer, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, set := range []map[string]app.Command{roster.Commands(label), persona.Commands(pointer, nil, nil), notes.Commands()} {
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
