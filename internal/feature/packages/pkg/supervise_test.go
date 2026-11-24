package pkg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

var errCrash = errors.New("exit status 2")

func codeOf(err error) sdk.ErrorCode { return platform.CodeOf(err) }

// TestHelloHandshake：核心啟動的套件帶著環境變數，報到成功才算 running；資料路徑是 vault/pkg/<id>，能力照 manifest。
// Guards: REQ-91, REQ-96, REQ-172
func TestHelloHandshake(t *testing.T) {
	h := newHarness(t)
	dir := h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.write(sdk.TierMine, "notes", dataPkg("notes", "1.0.0", ""), nil)
	h.start()
	p := h.lastProc("watcher")
	for k, want := range map[string]string{"EVERGENIE_HOME": h.home.Dir, "EVERGENIE_PACKAGE_ID": "watcher", "EVERGENIE_PACKAGE_API": "evergenie.v1", "EVERGENIE_PACKAGE_VERSION": "1.0.0", "EVERGENIE_PACKAGE_DIR": dir, "EVERGENIE_API_SOCKET": filepath.Join(h.home.Dir, "runtime", "sockets", "api.sock")} {
		if got := envValue(p.env, k); got != want {
			t.Errorf("%s = %q，要 %q", k, got, want)
		}
	}
	if s := h.get("watcher").State; s != sdk.PackageStateBuilding {
		t.Fatalf("還沒報到要是 building：%s", s)
	}
	if _, err := h.m.Hello(t.Context(), &sdk.PackageHelloRequest{ID: "watcher", API: "evergenie.v2"}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("協定不同要 INVALID：%v", err)
	}
	if _, err := h.m.Hello(t.Context(), &sdk.PackageHelloRequest{ID: "watcher", API: "evergenie.v1", Version: "0.9.0"}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("版本不同要 INVALID：%v", err)
	}
	if err := h.hello("notes"); codeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("純資料套件不能報到：%v", err)
	}
	r, err := h.m.Hello(t.Context(), &sdk.PackageHelloRequest{ID: "watcher", API: "evergenie.v1", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if r.DataDir != "vault/pkg/watcher" || r.Token == "" || strings.Join(r.Granted, ",") != "read:vault:task,write:vault:pkg/watcher" {
		t.Fatalf("Hello = %+v", r)
	}
	if s := h.get("watcher").State; s != sdk.PackageStateRunning {
		t.Fatalf("報到後要 running：%s", s)
	}
	if err := h.hello("watcher"); codeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("同一個行程不能報到兩次：%v", err)
	}
}

// TestCrashRestartsAndAlerts：行程異常結束 → failed＋system.alert，退避後重啟；10 分鐘內再掛不重複通知。
// Guards: REQ-96
func TestCrashRestartsAndAlerts(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.start()
	_ = h.hello("watcher")
	h.lastProc("watcher").exit(errCrash)
	h.tick()
	if p := h.get("watcher"); p.State != sdk.PackageStateFailed || p.Error == "" {
		t.Fatalf("掛了要 failed＋原因：%+v", p)
	}
	if a := h.bus.alerts(); len(a) != 1 || !strings.Contains(a[0], "watcher名") {
		t.Fatalf("要發一次 system.alert：%v", a)
	}
	h.advance(4 * time.Second)
	if n := len(h.procs["watcher"]); n != 1 {
		t.Fatalf("退避 5 秒內不重啟：%d", n)
	}
	h.advance(2 * time.Second)
	if n := len(h.procs["watcher"]); n != 2 {
		t.Fatalf("退避後要重啟：%d", n)
	}
	_ = h.hello("watcher")
	h.lastProc("watcher").exit(errCrash)
	h.tick()
	if a := h.bus.alerts(); len(a) != 1 {
		t.Fatalf("10 分鐘內同一個套件只通知一次：%v", a)
	}
	h.advance(10 * time.Second) // 第二次退避 10 秒
	if n := len(h.procs["watcher"]); n != 3 {
		t.Fatalf("第二次退避後要重啟：%d", n)
	}
}

// TestRepeatedFailureGivesUpWithoutAffectingOthers：連續失敗 5 次就停手（failed、通知），別的套件照常跑。
// Guards: REQ-96
func TestRepeatedFailureGivesUpWithoutAffectingOthers(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierMine, "flaky", programPkg("flaky", "", ""), programFiles("flaky"))
	h.write(sdk.TierMine, "steady", programPkg("steady", "", ""), programFiles("steady"))
	h.start()
	if err := h.hello("steady"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		h.lastProc("flaky").exit(errCrash)
		h.tick()
		h.advance(6 * time.Minute) // 超過最長的退避
	}
	p := h.get("flaky")
	if p.State != sdk.PackageStateFailed || !strings.Contains(p.Error, "停止自動重啟") {
		t.Fatalf("要停手：%+v", p)
	}
	n := len(h.procs["flaky"])
	h.advance(time.Hour)
	if len(h.procs["flaky"]) != n {
		t.Fatal("停手之後不能再自己重啟")
	}
	if !strings.Contains(strings.Join(h.bus.alerts(), "|"), "已停止重啟") {
		t.Fatalf("停手要通知：%v", h.bus.alerts())
	}
	if s := h.get("steady"); s.State != sdk.PackageStateRunning || len(h.procs["steady"]) != 1 || h.procs["steady"][0].killed {
		t.Fatalf("steady 不受影響：%+v", s)
	}
	if _, err := h.m.Reload(t.Context(), &sdk.PackageReloadRequest{ID: "flaky"}); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if len(h.procs["flaky"]) != n+1 {
		t.Fatal("手動重新載入要再試一次")
	}
}

// TestRestartPolicyNever：restart = never 掛了不重啟；on-failure 乾淨結束不重啟、異常結束重啟。
// Guards: REQ-96
func TestRestartPolicyNever(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierMine, "once", programPkg("once", "never", ""), programFiles("once"))
	h.write(sdk.TierMine, "careful", programPkg("careful", "on-failure", ""), programFiles("careful"))
	h.start()
	h.lastProc("once").exit(errCrash)
	h.lastProc("careful").exit(nil)
	h.tick()
	h.advance(time.Hour)
	if len(h.procs["once"]) != 1 || h.get("once").State != sdk.PackageStateFailed {
		t.Fatalf("never：%d %+v", len(h.procs["once"]), h.get("once"))
	}
	if len(h.procs["careful"]) != 1 || h.get("careful").State != sdk.PackageStateStopped {
		t.Fatalf("on-failure 乾淨結束：%d %+v", len(h.procs["careful"]), h.get("careful"))
	}
	if _, err := h.m.Reload(t.Context(), &sdk.PackageReloadRequest{ID: "careful"}); err != nil {
		t.Fatal(err)
	}
	h.drain()
	h.lastProc("careful").exit(errCrash)
	h.tick()
	h.advance(6 * time.Second)
	if len(h.procs["careful"]) != 3 {
		t.Fatalf("on-failure 異常結束要重啟：%d", len(h.procs["careful"]))
	}
}

// TestDisableStopsWithoutAlert：停用的套件行程收掉、狀態 stopped，不算掛掉、不通知；再啟用就重新跑。
// Guards: REQ-96
func TestDisableStopsWithoutAlert(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.start()
	_ = h.hello("watcher")
	r, err := h.m.SetEnabled(t.Context(), &sdk.PackageSetEnabledRequest{ID: "watcher", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	h.advance(time.Minute)
	if r.Package.State != sdk.PackageStateStopped || !h.lastProc("watcher").killed || len(h.bus.alerts()) != 0 || h.get("watcher").Enabled {
		t.Fatalf("停用：%+v killed=%v alerts=%v", r.Package, h.lastProc("watcher").killed, h.bus.alerts())
	}
	b, _ := os.ReadFile(filepath.Join(h.home.Dir, "settings", "packages.toml"))
	if !strings.Contains(string(b), "watcher = false") {
		t.Fatalf("settings/packages.toml：%s", b)
	}
	if _, err := h.m.SetEnabled(t.Context(), &sdk.PackageSetEnabledRequest{ID: "watcher", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if len(h.procs["watcher"]) != 2 {
		t.Fatalf("再啟用要重新跑：%d", len(h.procs["watcher"]))
	}
}

// TestStartProgramReportsExit：真的啟動一個行程，結束碼回得來，輸出接在 out.log 後面。
// Guards: REQ-96, REQ-257
func TestStartProgramReportsExit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"hello $EVERGENIE_PACKAGE_ID\"\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "logs", "out.log")
	p, err := startProgram(context.Background(), script, dir, []string{"EVERGENIE_PACKAGE_ID=demo"}, logPath)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-p.done:
		if err == nil || !strings.Contains(err.Error(), "exit status 3") {
			t.Fatalf("要回結束碼 3：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("行程沒有結束")
	}
	b, _ := os.ReadFile(logPath)
	if !strings.Contains(string(b), "啟動 fail.sh") || !strings.HasSuffix(string(b), "hello demo\n") {
		t.Fatalf("out.log = %q", b)
	}
	ok := filepath.Join(dir, "ok.sh")
	if err := os.WriteFile(ok, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err = startProgram(context.Background(), ok, dir, nil, logPath)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-p.done:
		if err != nil {
			t.Fatalf("乾淨結束要回 nil：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("行程沒有結束")
	}
	// 重開不清掉上一次的輸出（DEV-8：掛掉的原因要留得住）。
	if b, _ := os.ReadFile(logPath); !strings.Contains(string(b), "hello demo") || !strings.Contains(string(b), "啟動 ok.sh") {
		t.Fatalf("重開後 out.log 要保留上一次：%q", b)
	}
}

type fakeExternal struct{ state sdk.PackageState }

func (f fakeExternal) Claim(binary string) (sdk.PackageState, string, bool) {
	if binary != "evergenie-engine-demo" {
		return "", "", false
	}
	return f.state, "找不到執行檔", true
}

// TestExternalPackageNotLaunched：已經有別的模組監督的套件（引擎），pkg 只列出它的狀態，不編譯、不啟動、不能報到。
// Guards: REQ-96
func TestExternalPackageNotLaunched(t *testing.T) {
	h := newHarness(t)
	m := strings.Replace(programPkg("engine-demo", "", ""), `entry = "cmd/engine-demo"`, `entry = "cmd/evergenie-engine-demo"`, 1)
	h.write(sdk.TierOfficial, "engine-demo", m, map[string]string{"cmd/evergenie-engine-demo/main.go": "package main\n"})
	h.m.SetExternal(fakeExternal{state: sdk.PackageStateFailed})
	h.start()
	h.advance(time.Minute)
	if len(h.procs) != 0 || len(h.builds) != 0 {
		t.Fatalf("不能自己啟動：procs=%v builds=%v", h.procs, h.builds)
	}
	if p := h.get("engine-demo"); p.State != sdk.PackageStateFailed || p.Error != "找不到執行檔" {
		t.Fatalf("狀態照外部監督的：%+v", p)
	}
	if err := h.hello("engine-demo"); codeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("外部監督的不經 pkg 報到：%v", err)
	}
}

// TestEditMineTriggersRebuild：改了 packages/mine/ 的檔案，連續兩拍一樣才算改完 → 重編、新版起來才換掉舊的；沒改不重編。
// Guards: REQ-92
func TestEditMineTriggersRebuild(t *testing.T) {
	h := newHarness(t)
	dir := h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.start()
	if len(h.builds) != 1 || len(h.procs["watcher"]) != 1 {
		t.Fatalf("第一拍要編譯並啟動：builds=%d procs=%d", len(h.builds), len(h.procs["watcher"]))
	}
	_ = h.hello("watcher")
	h.advance(time.Minute)
	if len(h.builds) != 1 {
		t.Fatal("沒改檔案不能重編")
	}
	main := filepath.Join(dir, "cmd", "watcher", "main.go")
	if err := os.WriteFile(main, []byte("package main\n\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := h.home.Clock.Now().Add(time.Hour)
	if err := os.Chtimes(main, later, later); err != nil {
		t.Fatal(err)
	}
	h.advance(2 * time.Second)
	if len(h.builds) != 1 {
		t.Fatal("看到改動的第一拍先不重編（可能還在存檔）")
	}
	h.advance(2 * time.Second)
	if len(h.builds) != 2 || len(h.procs["watcher"]) != 2 || !h.procs["watcher"][0].killed {
		t.Fatalf("穩定之後要重編並換新行程：builds=%d procs=%d", len(h.builds), len(h.procs["watcher"]))
	}
	if len(h.bus.alerts()) != 0 {
		t.Fatalf("換版不算掛掉：%v", h.bus.alerts())
	}
	h.advance(10 * time.Second)
	if len(h.builds) != 2 {
		t.Fatal("編譯產生的檔案不能讓自己再重編")
	}
}

// TestRescanDuringBuild：編譯還沒完就重掃（偵測到存檔、Reload；真的 Go 第一次編常要幾十秒）→ entry 換了一份、狀態照抄，
// 編完的結果要算在現在這份上，不能卡在「編譯中」再也不編、不啟動；編譯中又要求重編的（第二次 Reload），編完再編一次。
// Guards: REQ-92
func TestRescanDuringBuild(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.start()
	_ = h.hello("watcher")
	reload := func() {
		t.Helper()
		if _, err := h.m.Reload(t.Context(), &sdk.PackageReloadRequest{ID: "watcher"}); err != nil {
			t.Fatal(err)
		}
	}
	reload() // 開始編（排在佇列裡，還沒跑完）
	if len(h.queue) != 1 {
		t.Fatalf("要有一次編譯在跑：%d", len(h.queue))
	}
	reload() // 編譯中又重掃一次
	h.drain()
	if len(h.builds) != 2 || len(h.procs["watcher"]) != 2 {
		t.Fatalf("編完要換上新版：builds=%d procs=%d", len(h.builds), len(h.procs["watcher"]))
	}
	h.tick()
	if len(h.builds) != 3 || len(h.procs["watcher"]) != 3 {
		t.Fatalf("編譯中又要求重編的，編完再編一次：builds=%d procs=%d", len(h.builds), len(h.procs["watcher"]))
	}
	_ = h.hello("watcher")
	h.advance(10 * time.Second)
	if p := h.get("watcher"); p.State != sdk.PackageStateRunning || len(h.builds) != 3 {
		t.Fatalf("之後照常跑、不再重編：%+v builds=%d", p, len(h.builds))
	}
}

// TestNoGoPlainError：沒有能用的 Go → 套件 failed，原因是白話（要先裝編譯工具、約多大、裝在程式自己的資料夾、要我裝嗎），
// 補救代號 install_go，發通知；不自動下載、也不一直重試。不能幫忙下載時（原始碼版、這台晶片沒有包）不帶補救代號，
// 白話寫要怎麼做、不問「要我裝嗎」。套件的 go.mod 要更新的 Go 時（GOTOOLCHAIN=local 的說法）回白話、不下載。
// Guards: REQ-93, REQ-171
func TestNoGoPlainError(t *testing.T) {
	h := newHarness(t)
	f := newFakeGo(t, goTarball(t, goodGoFiles(fakeGoVersion)))
	h.m.tools = f.tools
	h.noGo = true
	h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.start()
	p := h.get("watcher")
	if p.State != sdk.PackageStateFailed || p.Remedy != sdk.PackageRemedyInstallGo || !strings.Contains(p.Error, goMsgNone) ||
		!strings.Contains(p.Error, "這個套件要先裝編譯工具（Go，約 1MB，裝在程式自己的資料夾裡），要我裝嗎？") {
		t.Fatalf("沒有 Go：%+v", p)
	}
	plainOnly(t, p.Error)
	if a := h.bus.alerts(); len(a) != 1 || !strings.Contains(a[0], "要先裝編譯工具（Go）") {
		t.Fatalf("要通知一次：%v", a)
	}
	h.advance(time.Hour)
	if len(h.bus.alerts()) != 1 || len(h.procs) != 0 || f.hits.Load() != 0 {
		t.Fatalf("不能一直重試、重複通知或自己下載：%v hits=%d", h.bus.alerts(), f.hits.Load())
	}

	src := newHarness(t)
	src.m.tools = &goTools{platform: "x", pin: func() (goPin, error) { return officialGo, nil }, lookPath: func(string) (string, error) { return "", exec.ErrNotFound }, version: goVersionOf}
	src.noGo = true
	src.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	src.start()
	if p := src.get("watcher"); p.State != sdk.PackageStateFailed || p.Remedy != "" || !strings.Contains(p.Error, "原始碼版") || strings.Contains(p.Error, "要我裝嗎") {
		t.Fatalf("原始碼版：%+v", p)
	}
	plainOnly(t, src.get("watcher").Error)
	if _, err := src.m.InstallGo(t.Context(), &sdk.PackageInstallGoRequest{Confirmed: true, By: &sdk.Actor{Name: "system"}}); codeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "原始碼版") {
		t.Fatalf("原始碼版按安裝：重試也一樣，要 CONFLICT＋白話：%v", err)
	}

	chip := newHarness(t)
	g := newFakeGo(t, goTarball(t, goodGoFiles(fakeGoVersion)))
	g.tools.platform = "other-arch"
	chip.m.tools = g.tools
	chip.noGo = true
	chip.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	chip.start()
	if p := chip.get("watcher"); p.Remedy != "" || !strings.Contains(p.Error, goMsgArch+goNextSystem) || strings.Contains(p.Error, "要我裝嗎") || g.hits.Load() != 0 {
		t.Fatalf("這台晶片沒有包：%+v", p)
	}
	if _, err := chip.m.InstallGo(t.Context(), &sdk.PackageInstallGoRequest{Confirmed: true, By: &sdk.Actor{Name: "system"}}); codeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), goMsgArch) || g.hits.Load() != 0 {
		t.Fatalf("這台晶片沒有包按安裝：要 CONFLICT、不下載：%v", err)
	}

	mod := newHarness(t)
	dir := mod.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	mod.buildErr[dir] = "go: go.mod requires go >= 1.29 (running go 1.27.4; GOTOOLCHAIN=local)"
	mod.start()
	if p := mod.get("watcher"); p.State != sdk.PackageStateFailed || p.Remedy != "" ||
		!strings.Contains(p.Error, "這個套件的 go.mod 要 Go 1.29 以上，這台的編譯工具（Go）是 1.27.4") || !strings.Contains(p.Error, "「go 1.27」") {
		t.Fatalf("go.mod 要更新的 Go：%+v", p)
	}
}

// 叫 go 的環境：GOTOOLCHAIN=local（go 不會自己去下載別的版本）、不帶外面的 GOROOT 與 GOTOOLCHAIN。
func TestGoEnvStaysLocal(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "auto")
	t.Setenv("GOROOT", "/somewhere/else")
	env := goEnv()
	n := 0
	for _, kv := range env {
		switch {
		case kv == "GOTOOLCHAIN=local":
			n++
		case strings.HasPrefix(kv, "GOTOOLCHAIN=") || strings.HasPrefix(kv, "GOROOT="):
			t.Fatalf("不能帶外面的 %s", kv)
		}
	}
	if n != 1 {
		t.Fatalf("要剛好一個 GOTOOLCHAIN=local：%v", env)
	}
	if _, _, ok := goTooNew("main.go:1: syntax error"); ok {
		t.Fatal("一般的編譯錯誤不是版本問題")
	}
}

// TestInstallGoNeedsConsent：InstallGo 沒有主人同意（confirmed）就拒絕、不下載；同意後下載到工具資料夾（對過檢查碼），
// 重編等 Go 的套件、用的是工具資料夾那支；已經有就不再下載；下載沒成功回 UNAVAILABLE＋白話、套件繼續等。
// Guards: REQ-93, REQ-171
func TestInstallGoNeedsConsent(t *testing.T) {
	h := newHarness(t)
	f := newFakeGo(t, goTarball(t, goodGoFiles(fakeGoVersion)))
	h.m.tools = f.tools
	h.m.lookGo = f.tools.look
	var used []string
	h.m.runner = goRecorder{fakeRunner{h}, &used}
	h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.write(sdk.TierMine, "other", programPkg("other", "", ""), programFiles("other"))
	h.start()
	if h.get("watcher").Remedy != sdk.PackageRemedyInstallGo {
		t.Fatalf("要等 Go：%+v", h.get("watcher"))
	}
	if _, err := h.m.InstallGo(t.Context(), &sdk.PackageInstallGoRequest{By: &sdk.Actor{Name: "system"}}); codeOf(err) != sdk.ErrorCodeInvalid || f.hits.Load() != 0 {
		t.Fatalf("沒同意要 INVALID 而且不能下載：%v hits=%d", err, f.hits.Load())
	}
	// 下載沒成功：白話、UNAVAILABLE，套件繼續等。
	good := f.pin.Files["test-arch"]
	bad := good
	bad.SHA256 = strings.Repeat("0", 64)
	f.pin.Files = map[string]goFile{"test-arch": bad}
	_, err := h.m.InstallGo(t.Context(), &sdk.PackageInstallGoRequest{Confirmed: true, By: &sdk.Actor{Name: "system"}})
	if codeOf(err) != sdk.ErrorCodeUnavailable || !strings.Contains(err.Error(), "檢查碼對不上") {
		t.Fatalf("檢查碼不符要 UNAVAILABLE＋白話：%v", err)
	}
	plainOnly(t, err.(*platform.Err).Message)
	if h.get("watcher").Remedy != sdk.PackageRemedyInstallGo || len(goLeftovers(t, f.tools.dir)) != 0 {
		t.Fatalf("沒裝成要繼續等、不留暫存：%+v", h.get("watcher"))
	}
	f.pin.Files = map[string]goFile{"test-arch": good}
	r, err := h.m.InstallGo(t.Context(), &sdk.PackageInstallGoRequest{Confirmed: true, By: &sdk.Actor{Name: "system"}})
	if err != nil {
		t.Fatal(err)
	}
	h.drain()
	if !strings.Contains(r.Command, "下載 Go "+fakeGoVersion) || r.Version != fakeGoVersion || len(r.Packages) != 2 {
		t.Fatalf("InstallGo = %+v", r)
	}
	toolsGo := filepath.Join(f.tools.dir, "go", "bin", "go")
	if len(h.procs["watcher"]) != 1 || len(h.procs["other"]) != 1 || h.get("watcher").Remedy != "" || len(used) != 2 || used[0] != toolsGo || used[1] != toolsGo {
		t.Fatalf("裝好要用工具資料夾那支重編並啟動：%v %+v %v", h.procs, h.get("watcher"), used)
	}
	hits := f.hits.Load()
	r, err = h.m.InstallGo(t.Context(), &sdk.PackageInstallGoRequest{Confirmed: true, By: &sdk.Actor{Name: "system"}})
	if err != nil || r.Command != "" || r.Version != fakeGoVersion || f.hits.Load() != hits {
		t.Fatalf("已經有 Go 就不再下載：%+v %v", r, err)
	}
}

// goRecorder 記下編譯用的是哪一支 go。
type goRecorder struct {
	fakeRunner
	used *[]string
}

func (r goRecorder) Run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "build" {
		*r.used = append(*r.used, name)
	}
	return r.fakeRunner.Run(ctx, dir, env, name, args...)
}

// 舊 servers 與 EVERGENIE_SITE 不限制本機已啟用套件。
func TestLegacyServersDoNotRestrictPackages(t *testing.T) {
	h := newHarness(t)
	t.Setenv("EVERGENIE_SITE", "office")
	for name, body := range map[string]string{
		"mini.toml":   "schema = 1\nname = \"mini\"\nrole = \"hub\"\n",
		"office.toml": "schema = 1\nname = \"office\"\nrole = \"node\"\npackages = [\"watcher\"]\n",
	} {
		if err := os.MkdirAll(filepath.Join(h.home.Dir, "servers"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h.home.Dir, "servers", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.write(sdk.TierMine, "other", programPkg("other", "", ""), programFiles("other"))
	h.start()
	h.advance(time.Minute)
	if len(h.procs["watcher"]) != 1 {
		t.Fatalf("列了的要啟動：%v", h.procs)
	}
	if len(h.procs["other"]) != 1 {
		t.Fatalf("舊 servers 不限制本機套件：%v", h.procs)
	}

	// 改舊身分並重載，兩個套件仍重新啟動。
	t.Setenv("EVERGENIE_SITE", "mini")
	if _, err := h.m.Reload(t.Context(), &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	h.advance(time.Minute)
	if len(h.procs["other"]) != 2 || len(h.procs["watcher"]) != 2 {
		t.Fatalf("舊身分不限制套件：%v", h.procs)
	}
}

// 每 2 秒的監督是 Jobs() 登記的定時工作（timer 的 JobRunner 跑）；還沒啟動時跑是空操作。
// Guards: REQ-96
func TestJobsSuperviseEveryPoll(t *testing.T) {
	m := New(Deps{})
	jobs := m.Jobs()
	if len(jobs) != 1 || jobs[0].Name != "packages.supervise" || jobs[0].Every != pollInterval {
		t.Fatalf("%+v", jobs)
	}
	if err := jobs[0].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// alertTexts 是發過的 system.alert，標題＋內文。
func (b *fakeBus) alertTexts() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, e := range b.events {
		if e["topic"] == "system.alert" {
			out = append(out, fmt.Sprint(e["title"])+"｜"+fmt.Sprint(e["context"]))
		}
	}
	return out
}

// TestBuildDoneButNoLongerRunnable：編譯中停用、manifest 改壞（重掃後 entry 換一份、照抄編譯中）——編完不啟動、不通知、
// 不 panic；狀態不停在「編譯中」。重新啟用照常跑起來。
func TestBuildDoneButNoLongerRunnable(t *testing.T) {
	t.Run("編譯中停用", func(t *testing.T) {
		h := newHarness(t)
		h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
		if err := h.m.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(h.queue) != 1 {
			t.Fatalf("要有一次編譯排著：%d", len(h.queue))
		}
		if _, err := h.m.SetEnabled(t.Context(), &sdk.PackageSetEnabledRequest{ID: "watcher", Enabled: false}); err != nil {
			t.Fatal(err)
		}
		h.drain()
		h.tick()
		if p := h.get("watcher"); len(h.procs["watcher"]) != 0 || len(h.bus.alerts()) != 0 || p.State != sdk.PackageStateStopped {
			t.Fatalf("停用的不能啟動、不通知：procs=%d alerts=%v %+v", len(h.procs["watcher"]), h.bus.alerts(), p)
		}
		if _, err := h.m.SetEnabled(t.Context(), &sdk.PackageSetEnabledRequest{ID: "watcher", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		h.drain()
		h.tick()
		if len(h.procs["watcher"]) != 1 {
			t.Fatalf("再啟用要跑起來：%d", len(h.procs["watcher"]))
		}
	})
	t.Run("編譯中 manifest 改壞", func(t *testing.T) {
		h := newHarness(t)
		h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
		if err := h.m.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		h.write(sdk.TierMine, "watcher", "schema = 1\nid = \"watcher\"\nkind = \n", nil)
		if _, err := h.m.Reload(t.Context(), &sdk.PackageReloadRequest{ID: "watcher"}); err != nil {
			t.Fatal(err)
		}
		h.drain() // 修之前：拿壞掉的 manifest 啟動，nil pointer panic
		h.tick()
		if p := h.get("watcher"); len(h.procs["watcher"]) != 0 || p.State == sdk.PackageStateBuilding {
			t.Fatalf("manifest 壞了不能啟動、不停在編譯中：procs=%d %+v", len(h.procs["watcher"]), p)
		}
	})
}

// TestRejectedWhileRunningStops：跑著的套件後來被拒（相依的套件停用了），行程要收掉，不留一個沒人管的在跑。
func TestRejectedWhileRunningStops(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierMine, "dep", dataPkg("dep", "1.0.0", ""), nil)
	h.write(sdk.TierMine, "watcher", programPkg("watcher", "", "[[requires]]\nid = \"dep\"\nversion = \"\"\n"), programFiles("watcher"))
	h.start()
	_ = h.hello("watcher")
	if _, err := h.m.SetEnabled(t.Context(), &sdk.PackageSetEnabledRequest{ID: "dep", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if p := h.get("watcher"); p.Error == "" || !h.lastProc("watcher").killed {
		t.Fatalf("被拒了行程要收掉：%+v killed=%v", p, h.lastProc("watcher").killed)
	}
}

// TestOldExitsAfterNewBuildFails：新版編不過（舊版還在跑），之後舊版也結束了——通知只放白話（不夾編譯工具的原文），不說「會重啟」；
// 套件的錯誤換成編譯的那個（給秘書看原文），不再說「仍在跑舊版」；也真的不重啟（等改檔）。
func TestOldExitsAfterNewBuildFails(t *testing.T) {
	h := newHarness(t)
	dir := h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.start()
	_ = h.hello("watcher")
	h.buildErr = map[string]string{dir: "./main.go:3:1: syntax error: unexpected }"}
	if _, err := h.m.Reload(t.Context(), &sdk.PackageReloadRequest{ID: "watcher"}); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if p := h.get("watcher"); !strings.Contains(p.Error, "仍在跑舊版") {
		t.Fatalf("新版編不過、舊版照跑：%+v", p)
	}
	h.lastProc("watcher").exit(errors.New("exit status 2"))
	h.tick()
	a := h.bus.alertTexts()
	if len(a) != 2 || !strings.Contains(a[1], "掛了") || !strings.Contains(a[1], goMsgBuildBroken) || !strings.Contains(a[1], "不會自動重啟") ||
		strings.Contains(a[1], "syntax") || strings.Contains(a[1], "原文") || strings.Contains(a[1], "依設定重啟") {
		t.Fatalf("舊版掛了的通知只放白話、不說會重啟：%q", a)
	}
	p := h.get("watcher")
	if p.State != sdk.PackageStateFailed || strings.Contains(p.Error, "仍在跑舊版") || !strings.Contains(p.Error, "syntax error") {
		t.Fatalf("錯誤要換成編譯的那個（帶原文給秘書）：%+v", p)
	}
	h.advance(10 * time.Minute)
	if len(h.procs["watcher"]) != 1 || len(h.bus.alertTexts()) != 2 {
		t.Fatalf("編不過不重啟、不再通知：procs=%d alerts=%q", len(h.procs["watcher"]), h.bus.alertTexts())
	}
}
