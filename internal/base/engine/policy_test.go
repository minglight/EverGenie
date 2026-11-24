package engine

import (
	"context"
	"reflect"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// failingPermission 讓 SetPermission 失敗（其他照常），測 PushPolicy 的錯誤回報。
type failingPermission struct{ sdk.EngineAPI }

func (failingPermission) SetPermission(context.Context, *sdk.EngineSetPermissionRequest) (*sdk.EngineSetPermissionResponse, error) {
	return nil, &sdk.Error{Code: sdk.ErrorCodeInternal, Message: "寫不進設定檔"}
}

// Guards: REQ-168, REQ-84
func TestPushPolicyCarriesGuidance(t *testing.T) {
	m, api, _, _, _ := supervised(t)
	ctx := context.Background()
	guidance := []string{"在 ~/Workspace 底下跑 make ci 是主人要的"}
	m.SetGuidanceSource(func() []string { return guidance })
	check(t, m.PushPolicy(ctx))
	check(t, func() error { _, err := m.CheckPermissions(ctx); return err }())
	var real, dry []string
	for _, c := range api.Calls() {
		r, ok := c.Request.(*sdk.EngineSetPermissionRequest)
		if !ok || r.Policy == nil || !reflect.DeepEqual(r.Policy.Guidance, guidance) {
			continue
		}
		if r.DryRun {
			dry = append(dry, r.Account)
		} else {
			real = append(real, r.Account)
		}
	}
	if !reflect.DeepEqual(real, []string{"one", "two"}) || !reflect.DeepEqual(dry, []string{"one", "two"}) {
		t.Fatalf("每個帳號都要推一次、doctor 也要帶 guidance：real=%v dry=%v", real, dry)
	}
	if m.policy.Guidance != nil {
		t.Fatal("trust.toml 算出的政策不能被 guidance 改到")
	}

	m.mu.Lock()
	id := m.registry.accounts["one"].engine
	m.mu.Unlock()
	check(t, m.Attach(id, failingPermission{api}))
	if err := m.PushPolicy(ctx); err == nil {
		t.Fatal("引擎寫不進去要回錯誤，policy 才能還原")
	}
}

// Guards: REQ-168
func TestStartFillsActiveRules(t *testing.T) {
	m, api, _, _, _ := supervised(t)
	ctx := context.Background()
	m.SetRuleSource(func(agent string) []sdk.PolicyRule {
		if agent != "code" {
			return nil
		}
		return []sdk.PolicyRule{{Agent: "code", Action: sdk.ActionClassWrite, Scope: "~/Workspace/amy/evergenie"}}
	})
	e, err := m.For("one")
	check(t, err)
	_, err = e.Start(ctx, &sdk.EngineStartRequest{Name: "code", Agent: "code"})
	check(t, err)
	_, err = e.Start(ctx, &sdk.EngineStartRequest{Name: "life", Agent: "life"})
	check(t, err)
	var got [][]sdk.PolicyRule
	for _, c := range api.Calls() {
		if r, ok := c.Request.(*sdk.EngineStartRequest); ok {
			got = append(got, r.Rules)
		}
	}
	want := [][]sdk.PolicyRule{{{Agent: "code", Action: sdk.ActionClassWrite, Scope: "~/Workspace/amy/evergenie"}}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("開 session 要帶這位秘書的規則：%+v", got)
	}
}
