package pkg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/engine"
	"github.com/minglight/evergenie/sdk"
)

func runCheck(t *testing.T, m *Module, name string) []app.CheckResult {
	t.Helper()
	for _, c := range m.Checks() {
		if c.Name == name {
			return c.RunAll(context.Background())
		}
	}
	t.Fatalf("沒有登記 %s", name)
	return nil
}

type fakeEngines []engine.Status

func (f fakeEngines) Statuses() []engine.Status { return f }

// packages.engine：找不到執行檔、不認得的引擎是紅字，還沒就緒是黃字，就緒的不報；都在跑給綠字；沒接引擎不登記。
// Guards: REQ-117
func TestEngineChecks(t *testing.T) {
	h := newHarness(t)
	for _, c := range h.m.Checks() {
		if c.Name == "packages.engine" {
			t.Fatal("沒接引擎不登記")
		}
	}
	h.m.SetEngines(fakeEngines{
		{ID: "ok", Accounts: []string{"a"}, Known: true, Found: true, Ready: true},
		{ID: "gone", Accounts: []string{"b"}, Known: true},
		{ID: "weird", Accounts: []string{"c"}},
		{ID: "slow", Accounts: []string{"d"}, Known: true, Found: true},
	})
	got := runCheck(t, h.m, "packages.engine")
	if len(got) != 3 || got[0].Status != app.StatusError || !strings.Contains(got[0].Message, "找不到引擎「gone」") ||
		got[1].Status != app.StatusError || !strings.Contains(got[1].Message, "沒有內建") || got[2].Status != app.StatusWarn {
		t.Fatalf("%+v", got)
	}
	h.m.SetEngines(fakeEngines{{ID: "ok", Known: true, Found: true, Ready: true}})
	if got := runCheck(t, h.m, "packages.engine"); len(got) != 1 || got[0].Status != app.StatusOK {
		t.Fatalf("都在跑：%+v", got)
	}
}

// packages.feature_broken：自己的或分享來的套件 rejected 各一條黃字（不是紅字，說其他套件照常、怎麼修）；好的不報。
// Guards: REQ-353
func TestChecksWarnBrokenUserFeature(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierMine, "garden", dataPkg("garden", "1.0.0", "[[requires]]\nid = \"nope\"\nversion = \"^1.0.0\"\n"), nil)
	h.write(sdk.TierInstalled, "pond", dataPkg("pond", "1.0.0", "[[requires]]\nid = \"nope\"\nversion = \"^1.0.0\"\n"), nil)
	h.write(sdk.TierMine, "diary", dataPkg("diary", "1.0.0", ""), nil)
	h.start()
	got := runCheck(t, h.m, "packages.feature_broken")
	if len(got) != 2 {
		t.Fatalf("要兩條（garden、pond）：%+v", got)
	}
	g, p := got[0], got[1]
	if !strings.Contains(g.Message, "pond") {
		g, p = p, g
	}
	if p.Status != app.StatusWarn || !strings.Contains(p.Message, "你的套件「garden名（garden）」載不起來，其他套件照常") || !strings.Contains(p.Fix, "packages/mine/garden/") {
		t.Fatalf("garden：%+v", p)
	}
	if g.Status != app.StatusWarn || !strings.Contains(g.Message, "分享來的套件") || !strings.Contains(g.Fix, "packages upgrade pond") {
		t.Fatalf("pond：%+v", g)
	}
}

// packages.official_edited：改過的官方套件各一條黃字；沒改不報；沙盒家不報、只標 env = dev 的照報（git 比對本身的測試在 official_edits_test.go）。
// Guards: REQ-333
func TestChecksWarnEditedOfficialPackage(t *testing.T) {
	h := newHarness(t)
	var edited []string
	h.m.editedOfficial = func(context.Context, string) []string { return edited }
	if got := runCheck(t, h.m, "packages.official_edited"); len(got) != 0 {
		t.Fatalf("沒改不報：%+v", got)
	}
	edited = []string{"alpha", "beta"}
	got := runCheck(t, h.m, "packages.official_edited")
	if len(got) != 2 || got[0].Status != app.StatusWarn || !strings.Contains(got[0].Message, "alpha") || !strings.Contains(got[1].Message, "beta") || got[0].Fix == "" {
		t.Fatalf("改過的官方套件各一條黃字：%+v", got)
	}
	toml := filepath.Join(h.home.Dir, "home.toml")
	data, err := os.ReadFile(toml)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toml, append(append([]byte(nil), data...), []byte("\n[runtime]\nenv = \"dev\"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runCheck(t, h.m, "packages.official_edited"); len(got) != 2 {
		t.Fatalf("env = dev 只是標記，照常報：%+v", got)
	}
	if err := os.WriteFile(toml, append(append([]byte(nil), data...), []byte("\n[runtime]\nsandbox = true\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runCheck(t, h.m, "packages.official_edited"); len(got) != 0 {
		t.Fatalf("沙盒家不報：%+v", got)
	}
}
