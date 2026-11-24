package engine

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-117, REQ-84
func TestCheckPermissionsDryRunReportsDrift(t *testing.T) {
	m, api, clock, _, exit := supervised(t)
	ctx := context.Background()

	got, err := m.CheckPermissions(ctx)
	check(t, err)
	if !reflect.DeepEqual(got, map[string][]string{"one": {}, "two": {}}) {
		t.Fatalf("一致時每個帳號都是空串列：%v", got)
	}
	api.Configure(fake.Behavior{Delivered: true, Drift: []string{"設定檔被手改"}})
	got, err = m.CheckPermissions(ctx)
	check(t, err)
	if !reflect.DeepEqual(got, map[string][]string{"one": {"設定檔被手改"}, "two": {"設定檔被手改"}}) {
		t.Fatalf("要把引擎回的 Drift 原樣帶出來：%v", got)
	}
	dry := 0
	for _, c := range api.Calls() {
		if r, ok := c.Request.(*sdk.EngineSetPermissionRequest); ok && r.DryRun {
			dry++
			if r.Policy == nil || r.Policy.Mode != sdk.PermissionModeAsk {
				t.Fatalf("DryRun 要帶目前的政策：%+v", r)
			}
		}
	}
	if dry != 4 {
		t.Fatalf("兩次檢查、每次兩個帳號 = 4 次 DryRun，實際 %d", dry)
	}

	// 引擎行程掛了：這個帳號沒辦法檢查，要明說，不能當成一致。
	exit()
	m.Tick(ctx, clock.Now())
	got, err = m.CheckPermissions(ctx)
	check(t, err)
	if !reflect.DeepEqual(got, map[string][]string{"one": {NotRunning}, "two": {NotRunning}}) {
		t.Fatalf("沒在跑要回 NotRunning：%v", got)
	}
	clock.Advance(5 * time.Second)
}

// Guards: REQ-117
func TestStatusesAndAttach(t *testing.T) {
	m := New(Deps{})
	m.SetTrustSource(testPolicy)
	m.manual = true
	m.SetHome(home(t, "unknown-test-engine"))
	check(t, m.Start(context.Background()))
	defer m.Stop(context.Background())
	got := m.Statuses()
	if len(got) != 1 || got[0].Known || got[0].Found || got[0].Ready || !reflect.DeepEqual(got[0].Accounts, []string{"one", "two"}) {
		t.Fatalf("不認得的引擎：%+v", got)
	}
	check(t, m.Attach("unknown-test-engine", fake.New(nil)))
	got = m.Statuses()
	if !got[0].Ready || !got[0].Found {
		t.Fatalf("接上之後算 Ready：%+v", got)
	}
	out, err := m.CheckPermissions(context.Background())
	check(t, err)
	if len(out["one"]) != 0 || len(out["two"]) != 0 {
		t.Fatalf("接上的假引擎沒有 Drift：%v", out)
	}
}
