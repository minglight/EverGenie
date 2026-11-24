package inbox

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/inbox/notify"
	"github.com/minglight/evergenie/internal/feature/inbox/push"
	"github.com/minglight/evergenie/sdk"
)

type fakeTasks struct{}

func (fakeTasks) AddNudge(context.Context, string) error { return nil }
func (fakeTasks) Get(context.Context, string) (*sdk.Task, error) {
	return &sdk.Task{}, nil
}

type fakeRoster struct{}

func (fakeRoster) List(context.Context) ([]*sdk.Record, error) { return nil, nil }

// 別的塊登記的通知來源，看 notify 有沒有經塊收到。
type otherBlock struct{}

func (otherBlock) Name() string                      { return "other" }
func (otherBlock) Start(context.Context) error       { return nil }
func (otherBlock) Stop(context.Context) error        { return nil }
func (otherBlock) NoticeSources() []app.NoticeSource { return []app.NoticeSource{{Name: "goals"}} }

// 用假物件組起整塊，放進 App：登記合併三個子模組的；塊裡的 notify、overview 經塊收到通知來源與首頁資料段；
// 推播通道拿到家（Start 時 notify 擋「沒接推播」）；Start／Stop 跑一輪；選單徽章的數字從同一塊的 overview 來。
func TestBlockAssemblesStartsAndStops(t *testing.T) {
	h := homefixture.New(t)
	ctx := context.Background()
	messages := bus.New(bus.Deps{Vault: h.Vault, Clock: h.Clock})
	messages.SetHome(h.Dir)
	blk := New(Deps{Bus: messages, Vault: h.Vault, Clock: h.Clock, Roster: fakeRoster{}})
	blk.SetTasks(fakeTasks{})
	blk.SetHome(h.Dir)
	// 子模組的啟動順序照 registry 原本的先後（停止反序）。
	var order []string
	for _, m := range blk.Modules() {
		order = append(order, m.Name())
	}
	if got := strings.Join(order, ","); got != "notify,overview,menu" {
		t.Fatalf("子模組順序 %s", got)
	}
	all := app.New(messages, otherBlock{}, blk)
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
	if got := strings.Join(services, ","); got != "notify(),web(get_menu|get_pages|get_menu_prefs|set_menu_prefs),web(get_overview)" {
		t.Fatalf("路由 %s", got)
	}
	if err := all.Start(ctx); err != nil {
		t.Fatal(err)
	}
	sent, err := blk.Notices().Send(ctx, &sdk.Notification{Category: sdk.CategoryApprove, Title: "確認 Goal", Todo: "請確認", Agent: "code", Source: sdk.NotifySourceGoals})
	if err != nil || sent.Source != sdk.NotifySourceGoals {
		t.Fatalf("別塊登記的通知來源要留著（notify 經塊收到）：%+v %v", sent, err)
	}
	if _, err := blk.push.Subscribe(push.Subscription{Endpoint: "https://push.example/1", P256dh: "k", Auth: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "settings", "push.toml")); err != nil {
		t.Fatalf("推播通道要寫在 SetHome 給的家：%v", err)
	}
	ov, err := blk.Overview().Overview(ctx)
	if err != nil || ov == nil {
		t.Fatalf("首頁彙整要能用：%v", err)
	}
	if blk.Menu() == nil {
		t.Fatal("選單要拿得到")
	}
	if err := all.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

// 塊的指令就是 notify 的指令。
func TestBlockCommandsMergeSubpackages(t *testing.T) {
	cmds, err := Commands()
	if err != nil {
		t.Fatal(err)
	}
	if want := notify.Commands(); len(cmds) != len(want) || len(cmds) == 0 {
		t.Fatalf("指令數 %d，要 %d", len(cmds), len(want))
	}
}
