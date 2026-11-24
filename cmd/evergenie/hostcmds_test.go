package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/host"
	"github.com/minglight/evergenie/sdk"
)

type cmdRunner struct {
	mu    sync.Mutex
	calls []string
	have  map[string]string
}

func (r *cmdRunner) Run(_ context.Context, name string, args ...string) (string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return "", 127
}
func (r *cmdRunner) LookPath(n string) string { return r.have[n] }

func useRunner(t *testing.T, r host.Runner) {
	t.Helper()
	old := hostRunner
	hostRunner = r
	t.Cleanup(func() { hostRunner = old })
}

// TestServiceCommandsOnFixtureHome：service install --root 只寫 plist 不碰 launchctl；status／uninstall 看得到；doctor 的「服務」那一項跟著變。
// Guards: REQ-164
func TestServiceCommandsOnFixtureHome(t *testing.T) {
	dir, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_HOME", dir)
	r := &cmdRunner{}
	useRunner(t, r)
	root := t.TempDir()
	exe := filepath.Join(t.TempDir(), "evergenie")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if out := hgOK(t, sock, "service", "status", "--root", root); !strings.Contains(out, "還沒安裝") {
		t.Fatalf("status：%s", out)
	}
	if code, _, errOut := hg(t, sock, "", "service", "install", "--root", root, "--exe", "/nope/evergenie"); code != 1 || !strings.Contains(errOut, "找不到 EverGenie 執行檔") {
		t.Fatalf("執行檔不存在：%d %q", code, errOut)
	}
	out := hgOK(t, sock, "service", "install", "--root", root, "--exe", exe)
	plist := filepath.Join(root, "Library", "LaunchAgents", "com.evergenie.serve.plist")
	data, err := os.ReadFile(plist)
	if err != nil || !strings.Contains(string(data), "<string>"+dir+"</string>") || !strings.Contains(out, "沒有呼叫 launchctl") {
		t.Fatalf("install：%v\n%s", err, out)
	}
	if len(r.calls) != 0 {
		t.Fatalf("--root 不該呼叫外部指令：%v", r.calls)
	}
	hgOK(t, sock, "service", "uninstall", "--root", root)
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Fatalf("uninstall：%v", err)
	}
	if code, _, _ := hg(t, sock, "", "service", "frobnicate"); code != 2 {
		t.Fatalf("不認得的子指令：%d", code)
	}
	if code, _, _ := hg(t, sock, "", "service"); code != 2 {
		t.Fatalf("缺子指令：%d", code)
	}

	// doctor 的服務那一項讀預設的 LaunchAgents 資料夾（測試指到暫存資料夾）：沒裝是正常，裝了但沒載入要提醒。
	doc := func() string { _, out, _ := hg(t, sock, "", "doctor"); return out }
	if out := doc(); !strings.Contains(out, "還沒安裝成 launchd 服務") {
		t.Fatalf("doctor（沒裝）：\n%s", out)
	}
	launch := sdk.Getenv("EVERGENIE_LAUNCH_AGENTS_DIR")
	if err := os.MkdirAll(launch, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(launch, host.DefaultLabel+".plist")
	if err := os.WriteFile(file, []byte(host.RenderPlist(host.ServiceSpec{Exe: exe, Home: dir})), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(file) })
	if out := doc(); !strings.Contains(out, "裝了但沒有載入") {
		t.Fatalf("doctor（裝了沒載入）：\n%s", out)
	}
}

// 同一台跑兩個家：第二個家 home.toml 寫了 [runtime] service_label，service install 沒給 --label 就用它，
// 兩個家的 launchd plist 名稱不同、各自指到自己的家；status 也找得到那一份。
func TestServiceInstallUsesHomeServiceLabel(t *testing.T) {
	dir, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_HOME", dir)
	useRunner(t, &cmdRunner{})
	root := t.TempDir()
	exe := filepath.Join(t.TempDir(), "evergenie")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	conf, err := os.ReadFile(filepath.Join(dir, "home.toml"))
	if err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "home.toml"), append(conf, []byte("\n[runtime]\nservice_label = \"com.evergenie.workshop\"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	hgOK(t, sock, "service", "install", "--root", root, "--exe", exe)
	hgOK(t, sock, "service", "install", "--root", root, "--exe", exe, "--home", ws)
	agents := filepath.Join(root, "Library", "LaunchAgents")
	for label, home := range map[string]string{"com.evergenie.serve": dir, "com.evergenie.workshop": ws} {
		data, err := os.ReadFile(filepath.Join(agents, label+".plist"))
		if err != nil || !strings.Contains(string(data), "<string>"+label+"</string>") || !strings.Contains(string(data), "<string>"+home+"</string>") {
			t.Fatalf("%s：%v\n%s", label, err, data)
		}
	}
	if out := hgOK(t, sock, "service", "status", "--root", root, "--home", ws); !strings.Contains(out, "com.evergenie.workshop") {
		t.Fatalf("status 要找工坊那一份：%s", out)
	}
}

// TestPreflightCommandOnFixtureHome：preflight 合併這台電腦與引擎回報的檢查；缺工具退出碼 1 並給修法；--fix 只跑 brew install。
// tmux 不在這台電腦的檢查裡（引擎自己照 ECL-63 檢查）：PATH 上沒有也不出現 ✘ tmux。
// Guards: REQ-165
func TestPreflightCommandOnFixtureHome(t *testing.T) {
	dir, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_HOME", dir)
	r := &cmdRunner{have: map[string]string{"brew": "/opt/homebrew/bin/brew"}}
	useRunner(t, r)

	code, out, _ := hg(t, sock, "", "preflight")
	if code != 1 || !strings.Contains(out, "✘ git") || !strings.Contains(out, "brew install git") || strings.Contains(out, "✘ tmux") {
		t.Fatalf("preflight：%d\n%s", code, out)
	}
	if code, _, errOut := hg(t, sock, "", "preflight", "--need", "docker"); code != 2 || !strings.Contains(errOut, "tailscale") {
		t.Fatalf("--need：%d %q", code, errOut)
	}
	_, out, _ = hg(t, sock, "", "preflight", "--fix")
	if !strings.Contains(out, "執行 brew install git") {
		t.Fatalf("--fix：\n%s", out)
	}
	for _, c := range r.calls {
		if !strings.HasPrefix(c, "/opt/homebrew/bin/brew install") {
			t.Fatalf("--fix 只能跑 brew install：%q", c)
		}
	}
}

// 下載版（versions/<版本>/bin/evergenie、current 指過去、沒有 .git）的安裝前檢查不看 git：這台沒有 git 也不能有 ✘（DEV-155 S6）。
// 原始碼版照舊要 git（上面那個測試）。
func TestPreflightDownloadInstallSkipsGit(t *testing.T) {
	app := filepath.Join(t.TempDir(), "app")
	ver := filepath.Join(app, platform.DownloadVersionsDir, "0.0.1")
	if err := os.MkdirAll(filepath.Join(ver, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ver, "bin", "evergenie"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ver, platform.DownloadVersionFile), []byte("0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(platform.DownloadVersionsDir, "0.0.1"), filepath.Join(app, "current")); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(app, "current", "bin", "evergenie")
	if platform.DownloadInstallOf(exe) == "" {
		t.Fatalf("假的下載版要認得：%s", exe)
	}
	old := preflightExe
	preflightExe = func() (string, error) { return exe, nil }
	t.Cleanup(func() { preflightExe = old })
	useRunner(t, &cmdRunner{have: map[string]string{"brew": "/opt/homebrew/bin/brew"}}) // 沒有 git、也沒有 tailscale
	engines := func() ([]sdk.PreflightCheck, bool) {
		return []sdk.PreflightCheck{{ID: "tmux", Name: "tmux", Ok: true, Detail: "附帶的"}}, true
	}
	var out bytes.Buffer
	code := preflightCommand(context.Background(), cmdline.HostRequest{Engines: engines, Out: &out, Err: &out})
	if code != 0 || strings.Contains(out.String(), "✘") || strings.Contains(out.String(), "git") {
		t.Fatalf("下載版沒有 git 不能有 ✘：%d\n%s", code, out.String())
	}
	// 同一台換成原始碼版（不是 versions/ 底下）就照舊要 git。
	preflightExe = func() (string, error) { return filepath.Join(t.TempDir(), "bin", "evergenie"), nil }
	out.Reset()
	if code := preflightCommand(context.Background(), cmdline.HostRequest{Engines: engines, Out: &out, Err: &out}); code != 1 || !strings.Contains(out.String(), "✘ git") {
		t.Fatalf("原始碼版要 git：%d\n%s", code, out.String())
	}
}

// okRunner：brew install 都成功的假 Runner（只記指令，不真的裝）。
type okRunner struct{ cmdRunner }

func (r *okRunner) Run(ctx context.Context, name string, args ...string) (string, int) {
	r.cmdRunner.Run(ctx, name, args...)
	return "", 0
}

// TestPreflightFixRechecksEngines：--fix 裝完重查時重問 serve（引擎回報的 tmux 那行才是新的，不印修之前的結果）；
// serve 沒回應就多一行 ⚠ 說帳號程式（tmux、Claude、登入）沒檢查，不算 ✘（ECL-63）。
func TestPreflightFixRechecksEngines(t *testing.T) {
	r := &okRunner{cmdRunner{have: map[string]string{"brew": "/opt/homebrew/bin/brew", "git": "/usr/bin/git", "tailscale": "/usr/local/bin/tailscale"}}}
	useRunner(t, r)
	asked := 0
	engines := func() ([]sdk.PreflightCheck, bool) {
		asked++
		if asked == 1 {
			return []sdk.PreflightCheck{{ID: "tmux", Name: "tmux", Detail: "找不到", Fix: "brew install tmux", Install: []string{"brew", "install", "tmux"}}}, true
		}
		return []sdk.PreflightCheck{{ID: "tmux", Name: "tmux", Ok: true, Detail: "/opt/homebrew/bin/tmux"}}, true
	}
	var out bytes.Buffer
	if code := preflightCommand(context.Background(), cmdline.HostRequest{Args: []string{"--fix"}, Engines: engines, Out: &out, Err: &out}); code != 0 || asked != 2 {
		t.Fatalf("--fix 裝完要重問引擎、用新結果：%d 問了 %d 次\n%s", code, asked, out.String())
	}
	if !strings.Contains(out.String(), "執行 brew install tmux") || !strings.Contains(out.String(), "✔ tmux") || strings.Contains(out.String(), "✘ tmux") {
		t.Fatalf("要印新的 tmux 結果：\n%s", out.String())
	}
	out.Reset()
	if code := preflightCommand(context.Background(), cmdline.HostRequest{Engines: func() ([]sdk.PreflightCheck, bool) { return nil, false }, Out: &out, Err: &out}); code != 0 || !strings.Contains(out.String(), "⚠ 帳號程式（沒檢查：EverGenie 服務沒在跑") {
		t.Fatalf("serve 沒回應：要有 ⚠ 帳號程式、不算失敗：%d\n%s", code, out.String())
	}
}

// TestOpsCommandsOnFixtureHome：真的 serve 上的 backup 與 quota；秘書不能刪備份。
// Guards: REQ-166
func TestOpsCommandsOnFixtureHome(t *testing.T) {
	dir, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_HOME", dir)
	for i, age := range []time.Duration{time.Hour, 48 * time.Hour, 90 * 24 * time.Hour} {
		p := filepath.Join(dir, "runtime", "backups", "trust", string(rune('a'+i))+".toml")
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		_ = os.Chtimes(p, at, at)
	}
	if out := hgOK(t, sock, "backup", "list"); !strings.Contains(out, "trust/a.toml") || !strings.Contains(out, "共 3 個") {
		t.Fatalf("list：\n%s", out)
	}
	out := hgOK(t, sock, "backup", "prune", "--keep", "1", "--days", "30")
	if !strings.Contains(out, "預演：會刪 1 個") {
		t.Fatalf("prune 預演：\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "runtime", "backups", "trust", "c.toml")); err != nil {
		t.Fatal("預演不能刪")
	}
	t.Setenv("EVERGENIE_AGENT", "life")
	if code, _, errOut := hg(t, sock, "", "backup", "prune", "--keep", "1", "--days", "30", "--apply"); code != 1 || !strings.Contains(errOut, "只有主人") {
		t.Fatalf("秘書不能刪備份：%d %q", code, errOut)
	}
	t.Setenv("EVERGENIE_AGENT", "")
	hgOK(t, sock, "backup", "prune", "--keep", "1", "--days", "30", "--apply")
	if _, err := os.Stat(filepath.Join(dir, "runtime", "backups", "trust", "c.toml")); !os.IsNotExist(err) {
		t.Fatalf("--apply 要真的刪：%v", err)
	}

	// 測試家的引擎是假名字、沒有在跑：quota 說明原因，quota check 當成沒資料（結束碼 2）。
	if out := hgOK(t, sock, "quota"); !strings.Contains(out, "main") {
		t.Fatalf("quota：\n%s", out)
	}
	if code, _, _ := hg(t, sock, "", "quota", "check"); code != 2 {
		t.Fatalf("quota check（沒資料）：%d", code)
	}
}

// scriptRunner 依前綴回答（最長的前綴優先），沒登記的回 127。
type scriptRunner struct {
	have   map[string]string
	script map[string]string // 前綴 → 輸出（結束碼 0）
}

func (r *scriptRunner) Run(_ context.Context, name string, args ...string) (string, int) {
	line, best, n := name+" "+strings.Join(args, " "), "", -1
	for p, out := range r.script {
		if strings.HasPrefix(line, p) && len(p) > n {
			best, n = out, len(p)
		}
	}
	if n < 0 {
		return "沒有這個指令", 127
	}
	return best, 0
}
func (r *scriptRunner) LookPath(n string) string { return r.have[n] }

// doctor 的 config.tailscale：home.toml 寫了 web.service 才檢查；這台沒掛上或後台沒核准是黃字，都好是 ok；沒寫 web.service 這一項不出現。
func TestDoctorTailscaleServiceCheck(t *testing.T) {
	conf, err := os.ReadFile(filepath.Join("testdata", "home", "home.toml"))
	if err != nil {
		t.Fatal(err)
	}
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "home.toml"), conf, 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	withSvc := strings.Replace(string(conf), "[web]\nport = 18850\n", "[web]\nport = 18850\nservice = \"genie-amy-mini\"\n", 1)
	if withSvc == string(conf) {
		t.Fatal("fixture 的 [web] 變了，測試要跟著改")
	}
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte(withSvc), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := "/usr/local/bin/tailscale"
	status := func(approved bool) string {
		caps := ""
		if approved {
			caps = `"service-host":[{"svc:genie-amy-mini":["192.0.2.1"]}]`
		}
		return `{"BackendState":"Running","MagicDNSSuffix":"example.ts.net","Self":{"DNSName":"my-mini.example.ts.net.","Tags":["tag:hub"],"CapMap":{` + caps + `}}}`
	}
	r := &scriptRunner{have: map[string]string{"tailscale": bin}, script: map[string]string{
		bin + " status --json":          status(false),
		bin + " serve get-config --all": `{"services":{}}`,
	}}
	useRunner(t, r)
	ctx := context.Background()

	if _, err := platform.LoadHome(plain); err != nil {
		t.Fatalf("對照組的家要讀得起來：%v", err)
	}
	if got := tailscaleCheck(ctx, plain); len(got) != 0 {
		t.Fatalf("沒寫 web.service 不該出現：%+v", got)
	}
	useRunner(t, &scriptRunner{})
	if got := tailscaleCheck(ctx, home); len(got) != 1 || got[0].Severity != "warn" || !strings.Contains(got[0].Detail, "找不到 Tailscale") {
		t.Fatalf("沒裝 Tailscale：%+v", got)
	}
	useRunner(t, &scriptRunner{have: map[string]string{"tailscale": bin}, script: map[string]string{
		bin + " status --json": strings.Replace(status(true), `"tag:hub"`, "", 1),
	}})
	if got := tailscaleCheck(ctx, home); len(got) != 1 || got[0].Severity != "warn" || !strings.Contains(got[0].Detail, "不是 tag 身分") {
		t.Fatalf("個人帳號登入：%+v", got)
	}
	useRunner(t, r)
	one := func() (string, string) {
		got := tailscaleCheck(ctx, home)
		if len(got) != 1 || got[0].ID != "config.tailscale" {
			t.Fatalf("要一條 config.tailscale：%+v", got)
		}
		return string(got[0].Severity), got[0].Detail + " → " + got[0].Fix
	}
	if sev, msg := one(); sev != "warn" || !strings.Contains(msg, "沒掛上") || !strings.Contains(msg, "evergenie tailscale setup") {
		t.Fatalf("沒掛上：%s %s", sev, msg)
	}
	// 名稱對上、目標是別的 port（例：別的家用了同名服務）：黃字「掛的是別的 port」。
	r.script[bin+" serve get-config --all"] = `{"services":{"svc:genie-amy-mini":{"endpoints":{"tcp:443":"http://127.0.0.1:8851"}}}}`
	if sev, msg := one(); sev != "warn" || !strings.Contains(msg, "掛的是別的 port") || !strings.Contains(msg, "8851") {
		t.Fatalf("指到別處：%s %s", sev, msg)
	}
	r.script[bin+" serve get-config --all"] = `{"services":{"svc:genie-amy-mini":{"endpoints":{"tcp:443":"http://127.0.0.1:18850"}}}}`
	if sev, msg := one(); sev != "warn" || !strings.Contains(msg, "還沒核准") {
		t.Fatalf("沒核准：%s %s", sev, msg)
	}
	r.script[bin+" status --json"] = status(true)
	if sev, msg := one(); sev != "ok" || !strings.Contains(msg, "https://genie-amy-mini.example.ts.net") {
		t.Fatalf("都好：%s %s", sev, msg)
	}

	// web.service 格式不對：家照樣載得起來，doctor 給黃字，不叫 tailscale。
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "home.toml"), []byte(strings.Replace(withSvc, "genie-amy-mini", "Genie_Mini", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := tailscaleCheck(ctx, bad); len(got) != 1 || got[0].Severity != "warn" || !strings.Contains(got[0].Detail, "格式不對") {
		t.Fatalf("格式不對：%+v", got)
	}
}
