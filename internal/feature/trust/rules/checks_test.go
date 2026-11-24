package rules

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/engine"
)

type fakePermissions struct {
	perms map[string][]string
	err   error
}

func (f fakePermissions) CheckPermissions(context.Context) (map[string][]string, error) {
	return f.perms, f.err
}

func runChecks(m *Module) []app.CheckResult {
	var out []app.CheckResult
	for _, c := range m.Checks() {
		out = append(out, c.RunAll(context.Background())...)
	}
	return out
}

// 權限的健檢：產物跟 trust.toml 不一致是紅字（permissions.drift），引擎沒在跑／沒辦法比對是黃字（permissions.check）；
// 都一致給一條綠字；拿不到結果只有一條黃字；沒接引擎就不登記。
// Guards: REQ-117
func TestPermissionChecks(t *testing.T) {
	m := New(Deps{Engine: fakePermissions{perms: map[string][]string{"a": {"設定檔被手改"}, "b": {engine.NotRunning}, "c": {}}}})
	got := runChecks(m)
	if len(got) != 2 || got[0].Status != app.StatusError || !strings.Contains(got[0].Message, "帳號 a") || got[0].Fix == "" ||
		got[1].Status != app.StatusWarn || !strings.Contains(got[1].Message, "帳號 b") {
		t.Fatalf("不一致是紅字、沒在跑是黃字：%+v", got)
	}
	ok := runChecks(New(Deps{Engine: fakePermissions{perms: map[string][]string{"a": nil, "b": nil}}}))
	if len(ok) != 1 || ok[0].Status != app.StatusOK || !strings.Contains(ok[0].Message, "2 個帳號") {
		t.Fatalf("都一致：%+v", ok)
	}
	bad := runChecks(New(Deps{Engine: fakePermissions{err: errors.New("x")}}))
	if len(bad) != 1 || bad[0].Status != app.StatusWarn || !strings.Contains(bad[0].Message, "沒辦法檢查") {
		t.Fatalf("拿不到結果：%+v", bad)
	}
	if len(New(Deps{}).Checks()) != 0 {
		t.Fatal("沒接引擎不登記")
	}
}
