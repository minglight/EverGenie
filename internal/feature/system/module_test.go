package system

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/system/accounts"
	"github.com/minglight/evergenie/internal/feature/system/backupsweep"
	"github.com/minglight/evergenie/internal/feature/system/charter"
	"github.com/minglight/evergenie/internal/feature/system/doctor"
	"github.com/minglight/evergenie/internal/feature/system/homebackup"
	"github.com/minglight/evergenie/internal/feature/system/report"
	"github.com/minglight/evergenie/internal/feature/system/settings"
	"github.com/minglight/evergenie/internal/feature/system/update"
	"github.com/minglight/evergenie/sdk"
)

type fakeEngine struct{}

func (fakeEngine) Usages(context.Context, string) ([]sdk.AccountUsage, error) { return nil, nil }
func (fakeEngine) Preflight(context.Context) ([]sdk.PreflightCheck, error)    { return nil, nil }

type fakeNotify struct{}

func (fakeNotify) Send(_ context.Context, n *sdk.Notification) (*sdk.Notification, error) {
	return n, nil
}
func (fakeNotify) Retire(context.Context, []string, string) error { return nil }

type fakeBus struct{}

func (fakeBus) Send(_ context.Context, e *sdk.Envelope) (*sdk.Envelope, error) { return e, nil }

type fakeSessions struct{}

func (fakeSessions) List(context.Context, string, []sdk.SessionState) ([]*sdk.Session, error) {
	return nil, nil
}

type fakeRoster struct{}

func (fakeRoster) List(context.Context) ([]*sdk.Record, error)         { return nil, nil }
func (fakeRoster) PersonaName(context.Context, string) (string, error) { return "", nil }

// 用假物件組起整塊，放進 App：登記合併五個子模組的（system 服務由四個子模組分方法掛、web 的健康頁與設定頁、規矩頁的 rules 服務、
// 每天檢查新版的定時工作）；同一塊的 doctor 經塊收到健檢（跑得到 charter 登記的 config.rules）；Start／Stop 跑一輪。
func TestBlockAssemblesStartsAndStops(t *testing.T) {
	h := homefixture.New(t)
	ctx := context.Background()
	blk := New(Deps{Engine: fakeEngine{}, Vault: h.Vault, Notify: fakeNotify{}, Bus: fakeBus{}, Sessions: fakeSessions{}, Roster: fakeRoster{}, Clock: h.Clock})
	blk.SetHome(h.Dir)
	// 子模組的啟動順序照 registry 原本的先後（停止反序）。
	var order []string
	for _, m := range blk.Modules() {
		order = append(order, m.Name())
	}
	if got := strings.Join(order, ","); got != "accounts,settings,charter,doctor,update,homebackup,report,backupsweep" {
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
	services := map[string]bool{}
	for _, mt := range mounts {
		services[mt.Service] = true
	}
	var names []string
	for s := range services {
		names = append(names, s)
	}
	sort.Strings(names)
	if got := strings.Join(names, ","); got != "backup,rules,system,web" {
		t.Fatalf("路由 %s", got)
	}
	if jobs, err := all.Jobs(); err != nil || len(jobs) == 0 {
		t.Fatalf("update 的定時工作要登記：%+v %v", jobs, err)
	}
	if err := all.Start(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := blk.Doctor().Doctor(ctx, &sdk.SystemDoctorRequest{Groups: []sdk.DoctorGroup{"config"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range res.Checks {
		found = found || c.ID == "config.rules"
	}
	if !found {
		t.Fatalf("doctor 要跑到同一塊 charter 登記的 config.rules：%+v", res.Checks)
	}
	if err := all.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

// 塊的指令就是五個子套件的指令合起來。
func TestBlockCommandsMergeSubpackages(t *testing.T) {
	var preflight cmdline.HostFunc
	cmds, err := Commands(preflight)
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, set := range []map[string]app.Command{accounts.Commands(), settings.Commands(preflight), charter.Commands(), doctor.Commands(), update.Commands(), homebackup.Commands(), report.Commands(nil), backupsweep.Commands()} {
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
