package roster

import (
	"context"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
)

type fakeHealth struct{ problems, warnings []string }

func (f fakeHealth) Problems(context.Context) []string { return f.problems }
func (f fakeHealth) Warnings(context.Context) []string { return f.warnings }

func results(t *testing.T, m *Module) map[string][]app.CheckResult {
	t.Helper()
	out := map[string][]app.CheckResult{}
	for _, c := range m.Checks() {
		out[c.Name] = c.RunAll(context.Background())
	}
	return out
}

// 名冊的健檢：人設卡或帳號對不上每個一條紅字（沒問題一條綠字）、角色來自停用的套件是黃字；沒接 Health 不登記。
// 舊家的 home.toml 還寫著 [incidents] owner（S5 之後沒人用）：照樣讀得起來，也不再有「事故負責人」那一項。
// Guards: REQ-117、REQ-359
func TestRosterChecks(t *testing.T) {
	home := homefixture.NewWith(t, "[incidents]\nowner = \"nobody\"\ndispatcher = \"admin\"\n").Dir
	if h, err := platform.LoadHome(home); err != nil || h.Incidents.Owner != "nobody" {
		t.Fatalf("舊的 [incidents] 要讀得起來：%v", err)
	}
	m := New(Deps{Health: fakeHealth{problems: []string{"code：找不到人設卡"}, warnings: []string{"art 的角色來自停用的套件"}}})
	m.SetHome(home)
	got := results(t, m)
	if _, ok := got["config.incidents"]; ok || len(got) != 2 {
		t.Fatalf("只有 config.agent、config.agent_feature：%+v", got)
	}
	if p := got["config.agent"]; len(p) != 1 || p[0].Status != app.StatusError || !strings.Contains(p[0].Message, "找不到人設卡") || p[0].Fix == "" {
		t.Fatalf("問題是紅字：%+v", p)
	}
	if w := got["config.agent_feature"]; len(w) != 1 || w[0].Status != app.StatusWarn {
		t.Fatalf("停用套件的角色是黃字：%+v", w)
	}

	ok := New(Deps{Health: fakeHealth{}})
	ok.SetHome(home)
	got = results(t, ok)
	if p := got["config.agent"]; len(p) != 1 || p[0].Status != app.StatusOK {
		t.Fatalf("沒問題一條綠字：%+v", p)
	}

	if cs := New(Deps{}).Checks(); len(cs) != 0 {
		t.Fatalf("沒接 Health 不登記：%+v", cs)
	}
}
