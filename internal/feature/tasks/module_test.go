package tasks

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/internal/base/timer"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/tasks/goal"
	"github.com/minglight/evergenie/internal/feature/tasks/job"
	"github.com/minglight/evergenie/internal/feature/tasks/reminder"
	"github.com/minglight/evergenie/internal/feature/tasks/task"
	"github.com/minglight/evergenie/sdk"
)

// 塊外的東西用假的：名冊、通知、session。底層的 vault、bus、timer 用測試家上真的。
type fakeAgents struct{}

func (fakeAgents) Resolve(_ context.Context, s string) (string, bool) { return s, s == "code" }
func (fakeAgents) AppendJournalOnce(context.Context, string, string) error {
	return nil
}
func (fakeAgents) RenderJob(_ context.Context, _, body string) (string, error) { return body, nil }

type fakeNotify struct{}

func (fakeNotify) Send(_ context.Context, n *sdk.Notification) (*sdk.Notification, error) {
	return n, nil
}
func (fakeNotify) List(context.Context, []sdk.NotificationState, string, bool) ([]sdk.Notification, error) {
	return nil, nil
}
func (fakeNotify) Retire(context.Context, []string, string) error { return nil }

type fakeSessions struct{}

func (fakeSessions) AutoOpenWork(context.Context, string, string, string) {}
func (fakeSessions) RefreshWork()                                         {}
func (fakeSessions) Open(context.Context, *sdk.SessionOpenRequest) (*sdk.Session, error) {
	return &sdk.Session{}, nil
}
func (fakeSessions) SetRoomWait(string, string, sdk.WaitingReason) {}
func (fakeSessions) Rotate(context.Context, string) error          { return nil }

// 用假物件組起整塊，放進 App：登記合併四個子模組的（沒有重名）、Start 讓四個子模組都起來（跟催與排程整理排進計時）、Stop 乾淨收掉。
func TestBlockAssemblesStartsAndStops(t *testing.T) {
	h := homefixture.New(t)
	ctx := context.Background()
	messages := bus.New(bus.Deps{Vault: h.Vault, Clock: h.Clock})
	messages.SetHome(h.Dir)
	timing := timer.NewScheduler(h.Clock)
	timing.SetHome(h.Dir)
	blk := New(Deps{Vault: h.Vault, Bus: messages, Timer: timing, Agents: fakeAgents{}, Notify: fakeNotify{}, Clock: h.Clock})
	blk.SetSessions(fakeSessions{})
	blk.SetHome(h.Dir)
	// 子模組的啟動順序照 registry 原本的先後（停止反序）。
	var order []string
	for _, m := range blk.Modules() {
		order = append(order, m.Name())
	}
	if got := strings.Join(order, ","); got != "job,task,reminder,goal" {
		t.Fatalf("子模組順序 %s", got)
	}
	all := app.New(messages, timing, blk)
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
	if got := strings.Join(services, ","); got != "goals,reminders,scheduler,tasks" {
		t.Fatalf("路由 %s", got)
	}
	if sources, err := all.NoticeSources(); err != nil || len(sources) != 1 || sources[0].Name != "goals" {
		t.Fatalf("通知來源 %+v %v", sources, err)
	}
	if parts, err := all.Board(); err != nil || len(parts) == 0 {
		t.Fatalf("首頁資料段 %+v %v", parts, err)
	}
	if _, err := all.Checks(); err != nil {
		t.Fatal(err)
	}
	if err := all.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"tasks.nudge", "system.maintenance"} {
		if _, ok := timing.Next(id); !ok {
			t.Errorf("Start 之後計時裡要有 %s", id)
		}
	}
	if blk.TaskStore() == nil || blk.ReminderStore() == nil || blk.GoalStore() == nil || blk.JobStore() == nil {
		t.Fatal("子模組要拿得到")
	}
	if err := all.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

// 塊的指令就是四個子套件的指令合起來，一個不多一個不少。
func TestBlockCommandsMergeSubpackages(t *testing.T) {
	cmds, err := Commands()
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, set := range []map[string]app.Command{task.Commands(), reminder.Commands(), goal.Commands(), job.Commands()} {
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
