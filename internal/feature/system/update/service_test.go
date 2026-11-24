package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// fakeUpgrades 取代真的檢查與叫起升級行程（不連網、不叫真的 EverGenie）。
type fakeUpgrades struct {
	checked int
	applied []string
	rescued int // 叫起「退回上一版」（upgrade --rollback）幾次
}

// newFakeModule 建一個接上假檢查與假升級行程的 Module；found＝找得到 EverGenie 程式。
func newFakeModule(home string, found bool) (*Module, *fakeUpgrades) {
	f := &fakeUpgrades{}
	m := New(Deps{})
	m.SetHome(home)
	m.findCore = func(*platform.Home, string) string {
		if found {
			return "/fake/core"
		}
		return ""
	}
	m.check = func(context.Context, *platform.Home, string) (*UpgradeCheck, error) {
		f.checked++
		return &UpgradeCheck{}, nil
	}
	m.launch = func(home, exe, tag string) (string, error) { f.applied = append(f.applied, tag); return "", nil }
	m.rescue = func(home, exe string) (string, error) { f.rescued++; return "", nil }
	return m, f
}

// 「退回上一版」（update_rollback，UPD-14）：只有主人能按；沒有換不回去的升級（rescue.json）就 INVALID、不去退別次升級；
// 有就叫起 upgrade --rollback、現況帶 rescue；升級正在跑時 CONFLICT；找不到程式時說白話。
func TestUpdateRollbackRescue(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	m, up := newFakeModule(home, true)
	owner, secretary := &sdk.Actor{Name: "owner(web)"}, &sdk.Actor{Name: "code"}
	if _, err := m.Rollback(ctx, owner); codeOf(err) != sdk.ErrorCodeInvalid || up.rescued != 0 {
		t.Fatalf("沒有換不回去的升級：%v %+v", err, up)
	}
	if info, _ := m.Status(ctx); info.Rescue {
		t.Fatal("沒有 rescue.json 時不顯示「退回上一版」")
	}
	if err := writeRescue(home, upgradeRescue{From: "0.1.0", To: "0.2.0", Commit: "abc", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rollback(ctx, secretary); codeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書要 FORBIDDEN：%v", err)
	}
	if _, err := m.Rollback(ctx, nil); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒帶身分要 INVALID：%v", err)
	}
	if up.rescued != 0 {
		t.Fatal("被擋的呼叫不能動手")
	}
	running := UpgradeState{Kind: sdk.UpdateKindRescue, Phase: UpgradePhaseBuild, From: "0.2.0", To: "0.1.0", StartedAt: time.Now()}
	if err := platform.WriteJSONFile(filepath.Join(UpgradeDir(home), UpgradeStateFile), running); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rollback(ctx, owner); codeOf(err) != sdk.ErrorCodeConflict || up.rescued != 0 {
		t.Fatalf("正在跑時要 CONFLICT：%v", err)
	}
	failed := UpgradeState{Kind: sdk.UpdateKindUpgrade, Phase: UpgradePhaseFailed, Outcome: sdk.UpdateOutcomeStranded, From: "0.1.0", To: "0.2.0", StartedAt: time.Now()}
	if err := platform.WriteJSONFile(filepath.Join(UpgradeDir(home), UpgradeStateFile), failed); err != nil {
		t.Fatal(err)
	}
	info, err := m.Rollback(ctx, owner)
	if err != nil || up.rescued != 1 || !info.Rescue || info.State == nil || info.State.Outcome != sdk.UpdateOutcomeStranded {
		t.Fatalf("主人按退回上一版：%+v %v %+v", info, err, up)
	}
	none, _ := newFakeModule(home, false)
	if _, err := none.Rollback(ctx, owner); codeOf(err) != sdk.ErrorCodeUnavailable {
		t.Fatalf("找不到程式要 UNAVAILABLE：%v", err)
	}
}

func codeOf(err error) sdk.ErrorCode {
	var pe *platform.Err
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// 看「版本與更新」誰都可以（只讀檔案）；檢查、現在更新、今晚更新、跳過只有主人能做（秘書 FORBIDDEN）；
// 要更新的版本必須等於目前檢查到的新版（不然 INVALID）；找不到程式（不是用安裝程式裝的）時說白話。
// Guards: REQ-280, REQ-281
func TestUpdateOwnerOnly(t *testing.T) {
	ctx := context.Background()
	h := struct{ Dir string }{testHome(t)}
	svc, up := newFakeModule(h.Dir, true)
	owner, secretary := &sdk.Actor{Name: "owner(web)"}, &sdk.Actor{Name: "code"}

	info, err := svc.Status(ctx)
	if err != nil || !info.CoreFound || info.Current == "" || info.Target != "" || info.Channel != "release" || info.TonightAt != "03:30" {
		t.Fatalf("沒檢查過的現況：%+v %v", info, err)
	}

	// 沒有身分、秘書：都不行
	for name, call := range map[string]func(*sdk.Actor) error{
		"check": func(a *sdk.Actor) error { _, err := svc.CheckNow(ctx, a); return err },
		"apply": func(a *sdk.Actor) error {
			_, err := svc.Apply(ctx, a, "0.3.0")
			return err
		},
		"decide": func(a *sdk.Actor) error {
			_, err := svc.Decide(ctx, a, "0.3.0", "skip")
			return err
		},
	} {
		if code := codeOf(call(secretary)); code != sdk.ErrorCodeForbidden {
			t.Errorf("%s：秘書該是 FORBIDDEN，得到 %q", name, code)
		}
		if code := codeOf(call(nil)); code != sdk.ErrorCodeInvalid {
			t.Errorf("%s：沒帶身分該是 INVALID，得到 %q", name, code)
		}
	}
	if up.checked != 0 || len(up.applied) != 0 {
		t.Fatalf("被擋的呼叫不能動手：%+v", up)
	}

	// 主人檢查：檢查被叫到
	if _, err := svc.CheckNow(ctx, owner); err != nil || up.checked != 1 {
		t.Fatalf("check：%v %+v", err, up)
	}

	// 沒有新版時 apply／decide 都是 INVALID
	if code := codeOf(func() error {
		_, err := svc.Apply(ctx, owner, "0.3.0")
		return err
	}()); code != sdk.ErrorCodeInvalid {
		t.Fatalf("沒有新版：%q", code)
	}

	// 檢查到 0.3.0 之後
	chk := UpgradeCheck{Current: "0.2.0", Target: "0.3.0", Tag: "v0.3.0", Notes: "## 新增\n- 好用", Channel: "release", CheckedAt: time.Now()}
	if err := platform.WriteJSONFile(filepath.Join(UpgradeDir(h.Dir), UpgradeCheckFile), chk); err != nil {
		t.Fatal(err)
	}
	info, _ = svc.Status(ctx)
	if info.Target != "0.3.0" || info.Tag != "v0.3.0" || info.Notes == "" || info.CheckedAt.IsZero() {
		t.Fatalf("有新版：%+v", info)
	}
	if code := codeOf(func() error {
		_, err := svc.Apply(ctx, owner, "0.2.9")
		return err
	}()); code != sdk.ErrorCodeInvalid {
		t.Fatalf("版本跟檢查到的不同該是 INVALID：%q", code)
	}
	if _, err := svc.Apply(ctx, owner, "0.3.0"); err != nil || len(up.applied) != 1 || up.applied[0] != "v0.3.0" {
		t.Fatalf("apply：%v %+v", err, up)
	}
	// 升級行程還在跑：不能再叫第二個
	st := sdk.UpdateState{Phase: "build", From: "0.2.0", To: "0.3.0", StartedAt: time.Now()}
	if err := platform.WriteJSONFile(filepath.Join(UpgradeDir(h.Dir), UpgradeStateFile), st); err != nil {
		t.Fatal(err)
	}
	if code := codeOf(func() error {
		_, err := svc.Apply(ctx, owner, "0.3.0")
		return err
	}()); code != sdk.ErrorCodeConflict {
		t.Fatalf("升級中該是 CONFLICT：%q", code)
	}
	info, _ = svc.Status(ctx)
	if info.State == nil || info.State.Phase != "build" {
		t.Fatalf("state：%+v", info.State)
	}
	_ = os.Remove(filepath.Join(UpgradeDir(h.Dir), UpgradeStateFile))

	// 今晚更新、跳過、亂寫的決定
	info, err = svc.Decide(ctx, owner, "0.3.0", "tonight")
	if err != nil || info.Decision != "tonight" || info.DecisionAt.IsZero() {
		t.Fatalf("tonight：%+v %v", info, err)
	}
	info, err = svc.Decide(ctx, owner, "0.3.0", "skip")
	if err != nil || info.Decision != "" || len(info.Skipped) != 1 || info.Target != "" {
		t.Fatalf("skip：%+v %v", info, err)
	}
	if code := codeOf(func() error {
		_, err := svc.Decide(ctx, owner, "0.3.0", "later")
		return err
	}()); code != sdk.ErrorCodeInvalid {
		t.Fatalf("亂寫的決定：%q", code)
	}

	// 找不到程式：現況說白話、檢查與更新都是 UNAVAILABLE
	none, _ := newFakeModule(h.Dir, false)
	info, _ = none.Status(ctx)
	if info.CoreFound || info.Detail == "" {
		t.Fatalf("沒有 Updater：%+v", info)
	}
	if code := codeOf(func() error { _, err := none.CheckNow(ctx, owner); return err }()); code != sdk.ErrorCodeUnavailable {
		t.Fatalf("check：%q", code)
	}
}

// 每分鐘的檢查是 Jobs() 登記的定時工作：一般的家會去找程式、看要不要檢查；只標成開發家（env = "dev"）照常檢查；沙盒家（sandbox = true）什麼都不做。
// Guards: REQ-280
func TestJobsCheckEveryMinuteExceptSandboxHome(t *testing.T) {
	dir := testHome(t)
	m, _ := newFakeModule(dir, false)
	looked := 0
	m.findCore = func(*platform.Home, string) string { looked++; return "" }
	jobs := m.Jobs()
	if len(jobs) != 1 || jobs[0].Name != "update.check" || jobs[0].Every != time.Minute {
		t.Fatalf("%+v", jobs)
	}
	if err := jobs[0].Run(context.Background()); err != nil || looked != 1 {
		t.Fatalf("一般的家要檢查：%v %d", err, looked)
	}
	toml := filepath.Join(dir, "home.toml")
	data, _ := os.ReadFile(toml)
	if err := os.WriteFile(toml, append(append([]byte(nil), data...), []byte("[runtime]\nenv = \"dev\"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := jobs[0].Run(context.Background()); err != nil || looked != 2 {
		t.Fatalf("env = dev 只是標記，照常檢查：%v %d", err, looked)
	}
	if err := os.WriteFile(toml, append(append([]byte(nil), data...), []byte("[runtime]\nsandbox = true\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := jobs[0].Run(context.Background()); err != nil || looked != 2 {
		t.Fatalf("沙盒家不檢查：%v %d", err, looked)
	}
}
