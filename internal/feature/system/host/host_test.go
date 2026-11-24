package host

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
)

// fakeRunner 記下每個被呼叫的外部指令，依 script 回答（前綴比對）；沒有登記的指令回 127。
type fakeRunner struct {
	mu     sync.Mutex
	calls  []string
	script map[string]reply
	have   map[string]string // LookPath 找得到的程式
}
type reply struct {
	out  string
	code int
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.calls = append(f.calls, line)
	best, bestLen := reply{"沒有這個指令", 127}, -1
	for prefix, r := range f.script {
		if strings.HasPrefix(line, prefix) && len(prefix) > bestLen {
			best, bestLen = r, len(prefix)
		}
	}
	return best.out, best.code
}
func (f *fakeRunner) LookPath(name string) string { return f.have[name] }
func (f *fakeRunner) called(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func testHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	toml := "[user]\nname = \"測試\"\ntimezone = \"Asia/Taipei\"\n[slots]\ndefault = \"life\"\n[slots.life]\nlabel = \"日常\"\nremind = \"08:30\"\ncheck = \"18:00\"\n[accounts]\ndefault = \"main\"\n[accounts.main]\nengine = \"x\"\n[web]\nport = 18850\n"
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func testExe(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "evergenie")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestServicePlistRender：plist 開機就跑、掛了重開，參數帶 serve 與家，日誌在家的 runtime/logs，路徑裡的特殊字元會跳脫。
// Guards: REQ-164
func TestServicePlistRender(t *testing.T) {
	got := RenderPlist(ServiceSpec{Exe: "/opt/hg/bin/evergenie", Home: "/Users/a&b/home <x>", Path: "/opt/homebrew/bin:/usr/bin"})
	for _, want := range []string{
		"<key>Label</key><string>com.evergenie.serve</string>",
		"<string>/opt/hg/bin/evergenie</string>\n    <string>serve</string>\n    <string>--home</string>\n    <string>/Users/a&amp;b/home &lt;x&gt;</string>",
		"<key>RunAtLoad</key><true/>", "<key>KeepAlive</key><true/>",
		"<key>EVERGENIE_ENGINE_BIN_DIR</key><string>/opt/hg/bin</string>",
		"<key>PATH</key><string>/opt/homebrew/bin:/usr/bin</string>",
		"/Users/a&amp;b/home &lt;x&gt;/runtime/logs/serve.out.log",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plist 少了 %q：\n%s", want, got)
		}
	}
	if homeOf(got) != "/Users/a&b/home <x>" {
		t.Fatalf("從 plist 讀回家：%q", homeOf(got))
	}
}

// TestServiceLifecycle：install → status → restart → stop → start → uninstall，全程用假的 launchctl；安裝前要確認家與執行檔。
// Guards: REQ-164
func TestServiceLifecycle(t *testing.T) {
	t.Setenv(ServiceModeEnv, "") // 測的是 launchd 模式：呼叫的人設了 EVERGENIE_SERVICE_MODE=process 也一樣
	ctx := context.Background()
	home, dir := testHome(t), t.TempDir()
	loaded := false
	f := &fakeRunner{script: map[string]reply{}}
	f.script["launchctl bootstrap"] = reply{"", 0}
	f.script["launchctl bootout"] = reply{"", 0}
	f.script["launchctl kickstart"] = reply{"", 0}
	svc := Service{Runner: f, Dir: dir, UID: 501, Spec: ServiceSpec{Exe: testExe(t), Home: home}}

	if _, err := svc.Start(ctx); platform.CodeOf(err) != "ERROR_CODE_NOT_FOUND" && err == nil {
		t.Fatalf("沒安裝不能 start：%v", err)
	}
	if _, err := (Service{Runner: f, Dir: dir, Spec: ServiceSpec{Exe: "/nope/evergenie", Home: home}}).Install(ctx); err == nil {
		t.Fatal("執行檔不存在要擋")
	}
	if _, err := (Service{Runner: f, Dir: dir, Spec: ServiceSpec{Exe: testExe(t), Home: t.TempDir()}}).Install(ctx); err == nil {
		t.Fatal("家沒有 home.toml 要擋")
	}

	// launchctl print：載入前回 113，載入後回 running。
	printed := func() {
		if loaded {
			f.script["launchctl print"] = reply{"gui/501/com.evergenie.serve = {\n\tstate = running\n\tpid = 4242\n}", 0}
		} else {
			f.script["launchctl print"] = reply{"Could not find service", 113}
		}
	}
	printed()
	lines, err := svc.Install(ctx)
	if err != nil || !f.called("launchctl bootstrap gui/501 "+svc.PlistPath()) {
		t.Fatalf("install：%v %v %v", lines, err, f.calls)
	}
	if data, err := os.ReadFile(svc.PlistPath()); err != nil || !strings.Contains(string(data), home) {
		t.Fatalf("plist：%v", err)
	}
	loaded = true
	printed()
	if st := svc.Status(ctx); !st.Installed || !st.Loaded || !st.Running || st.PID != 4242 || st.PlistHome != home {
		t.Fatalf("status：%+v", st)
	}
	// 重跑 install（已經載入）：先卸再載，用來更新。
	f.calls = nil
	if _, err := svc.Install(ctx); err != nil || !f.called("launchctl bootout gui/501/com.evergenie.serve") || !f.called("launchctl bootstrap") {
		t.Fatalf("重裝要先卸再載：%v %v", err, f.calls)
	}
	f.calls = nil
	if _, err := svc.Restart(ctx); err != nil || !f.called("launchctl kickstart -k gui/501/com.evergenie.serve") {
		t.Fatalf("restart：%v %v", err, f.calls)
	}
	if lines, _ := svc.Start(ctx); len(lines) == 0 || lines[0] != "已經在跑了" {
		t.Fatalf("已經在跑：%v", lines)
	}
	if _, err := svc.Stop(ctx); err != nil || !f.called("launchctl bootout") {
		t.Fatalf("stop：%v", err)
	}
	loaded = false
	printed()
	f.calls = nil
	if _, err := svc.Start(ctx); err != nil || !f.called("launchctl bootstrap") {
		t.Fatalf("沒載入時 start 要 bootstrap：%v %v", err, f.calls)
	}
	loaded = true
	printed()
	if _, err := svc.Uninstall(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(svc.PlistPath()); !os.IsNotExist(err) {
		t.Fatalf("uninstall 要移除 plist：%v", err)
	}
	if lines, err := svc.Uninstall(ctx); err != nil || len(lines) == 0 {
		t.Fatalf("重複 uninstall 不是錯誤：%v %v", lines, err)
	}
	if _, err := os.Stat(filepath.Join(home, "home.toml")); err != nil {
		t.Fatal("家的資料不能動")
	}
}

// TestServiceRootDoesNotCallLaunchctl：--root 只寫檔，不呼叫 launchctl（試跑與測試用）。
// Guards: REQ-164
func TestServiceRootDoesNotCallLaunchctl(t *testing.T) {
	t.Setenv(ServiceModeEnv, "") // 測的是 launchd 模式：呼叫的人設了 EVERGENIE_SERVICE_MODE=process 也一樣
	root := t.TempDir()
	f := &fakeRunner{}
	svc := Service{Runner: f, Root: root, Spec: ServiceSpec{Exe: testExe(t), Home: testHome(t)}}
	if _, err := svc.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("--root 不該呼叫外部指令：%v", f.calls)
	}
	if _, err := os.Stat(filepath.Join(root, "Library", "LaunchAgents", "com.evergenie.serve.plist")); err != nil {
		t.Fatalf("plist 要在 <root>/Library/LaunchAgents：%v", err)
	}
}

// TestPreflightReport：缺的工具給 brew 的安裝建議；--fix 只裝 ✘，而且只跑 brew install。Tailscale 不走 brew：請主人到官網下載 App（DEV-153）。
// Guards: REQ-165
func TestPreflightReport(t *testing.T) {
	f := &fakeRunner{have: map[string]string{"brew": "/opt/homebrew/bin/brew", "git": "/usr/bin/git"}, script: map[string]reply{}}
	checks := Preflight(f, PreflightOptions{OS: "darwin", Tools: []string{"git", "jq"}})
	by := map[string]Check{}
	for _, c := range checks {
		by[c.ID] = c
	}
	if by["git"].Level != LevelOK || by["jq"].Level != LevelBad || by["tailscale"].Level != LevelWarn || by["os"].Level != LevelOK {
		t.Fatalf("檢查：%+v", checks)
	}
	if strings.Join(by["jq"].Install, " ") != "brew install jq" {
		t.Fatalf("jq 的安裝建議：%v", by["jq"].Install)
	}
	var buf bytes.Buffer
	if bad := Print(&buf, checks); bad != 1 || !strings.Contains(buf.String(), "✘ jq") || !strings.Contains(buf.String(), "→ brew install jq") || !strings.Contains(buf.String(), "還有 1 項") {
		t.Fatalf("印出來：%d\n%s", bad, buf.String())
	}
	if linux := Preflight(f, PreflightOptions{OS: "linux", Tools: []string{"git"}}); linux[0].Level != LevelBad {
		t.Fatalf("不是 macOS 要擋：%+v", linux[0])
	}

	// --need tailscale：沒裝就是 ✘。
	need := map[string]bool{"tailscale": true}
	checks = Preflight(f, PreflightOptions{OS: "darwin", Need: need, Tools: []string{"git", "jq"}})
	for _, c := range checks {
		if c.ID == "tailscale" && (c.Level != LevelBad || !strings.Contains(c.Fix, TailscaleDownload) || strings.Contains(c.Fix, "brew") || len(c.Install) != 0) {
			t.Fatalf("要求 tailscale 但沒裝：要 ✘、請他到官網下載、不建議 brew：%+v", c)
		}
	}
	f.script["/opt/homebrew/bin/brew install"] = reply{"", 0}
	buf.Reset()
	ran, err := Fix(context.Background(), f, &buf, checks, need)
	if err != nil || ran != "brew install jq" || !f.called("/opt/homebrew/bin/brew install jq") || f.called("/opt/homebrew/bin/brew install jq tailscale") {
		t.Fatalf("fix：%q %v %v", ran, err, f.calls)
	}
	f.script["/opt/homebrew/bin/brew install"] = reply{"Error: no", 1}
	if _, err := Fix(context.Background(), f, &buf, checks, need); err == nil {
		t.Fatal("brew 失敗要回錯誤")
	}
	// 沒有 Homebrew：不裝，說明原因。
	noBrew := &fakeRunner{have: map[string]string{}}
	if _, err := Fix(context.Background(), noBrew, &buf, Preflight(noBrew, PreflightOptions{OS: "darwin", Tools: []string{"git", "jq"}}), nil); err == nil {
		t.Fatal("沒有 Homebrew 要說明")
	}
}

// TestTailscaleSetup：只把看板（web.port）serve 出去，網址寫進 web.public_url；沒連上就停在那一步說原因；down 只關 serve。
// Guards: REQ-165
func TestTailscaleSetup(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	f := &fakeRunner{have: map[string]string{"tailscale": "/usr/local/bin/tailscale"}, script: map[string]reply{
		"/usr/local/bin/tailscale status --json": {`{"BackendState":"NeedsLogin"}`, 0},
	}}
	ts := Tailscale{Runner: f, Home: home}
	if _, err := ts.Setup(ctx); err == nil || !strings.Contains(err.Error(), "NeedsLogin") && !strings.Contains(platformMessage(err), "NeedsLogin") {
		t.Fatalf("沒登入要停在這一步：%v", err)
	}
	if f.called("/usr/local/bin/tailscale serve") {
		t.Fatal("沒登入不能 serve")
	}
	f.script["/usr/local/bin/tailscale status --json"] = reply{`{"BackendState":"Running","Self":{"DNSName":"mac.example.ts.net."}}`, 0}
	f.script["/usr/local/bin/tailscale serve --bg"] = reply{"", 0}
	lines, err := ts.Setup(ctx)
	if err != nil || !f.called("/usr/local/bin/tailscale serve --bg --https=443 http://127.0.0.1:18850") {
		t.Fatalf("setup：%v %v %v", lines, err, f.calls)
	}
	if h, _ := platform.LoadHome(home); h.Web.PublicURL != "https://mac.example.ts.net" {
		t.Fatalf("public_url：%q", h.Web.PublicURL)
	}
	if _, err := ts.Setup(ctx); err != nil { // 可以重跑
		t.Fatal(err)
	}
	if st := strings.Join(ts.Status(ctx), "\n"); !strings.Contains(st, "Running") || !strings.Contains(st, "https://mac.example.ts.net") {
		t.Fatalf("status：\n%s", st)
	}
	f.script["/usr/local/bin/tailscale serve --https=443 off"] = reply{"", 0}
	if _, err := ts.Down(ctx); err != nil || !f.called("/usr/local/bin/tailscale serve --https=443 off") {
		t.Fatalf("down：%v %v", err, f.calls)
	}
	if _, err := (Tailscale{Runner: &fakeRunner{}, Home: home}).Setup(ctx); err == nil {
		t.Fatal("沒裝 Tailscale 要說明")
	}
	// 沒寫 web.service：照舊是機器 serve，不掛服務、不 clear 服務（只讀 get-config 做防呆）。
	for _, c := range f.calls {
		if strings.Contains(c, "--service") || strings.Contains(c, "serve clear") {
			t.Fatalf("沒有 web.service 不該動服務：%v", f.calls)
		}
	}
}

const tsBin = "/usr/local/bin/tailscale"

// serviceHome 是有 [web] service 的家（testHome 最後一段就是 [web]）。
func serviceHome(t *testing.T, name string) string {
	t.Helper()
	return appendWeb(t, "service = \""+name+"\"\n")
}

// appendWeb 在 testHome 的 [web] 尾端加幾行。
func appendWeb(t *testing.T, lines string) string {
	t.Helper()
	home := testHome(t)
	f, err := os.OpenFile(filepath.Join(home, "home.toml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(lines); err != nil {
		t.Fatal(err)
	}
	return home
}

// tsStatusJSON 是 tailscale status --json：tags 空＝個人帳號登入；approved 給了就在 CapMap["service-host"] 列出它。
func tsStatusJSON(tags string, approved string) string {
	caps := `"some-cap":null`
	if approved != "" {
		caps += `,"service-host":[{"` + approved + `":["192.0.2.1","fd7a::1"]}]`
	}
	return `{"BackendState":"Running","MagicDNSSuffix":"example.ts.net","Self":{"DNSName":"my-mini.example.ts.net.","Tags":[` + tags + `],"CapMap":{` + caps + `}}}`
}

// serveConfig 是 serve get-config --all 的輸出（格式照 2026-11-01 實機的 App CLI 1.104.2）：每個 svc 的 tcp:443 指到哪。
func serveConfig(targets map[string]string) reply {
	parts := []string{}
	for svc, target := range targets {
		parts = append(parts, `"`+svc+`":{"endpoints":{"tcp:443":"`+target+`"}}`)
	}
	return reply{`{"version":"0.0.1","services":{` + strings.Join(parts, ",") + `}}`, 0}
}

// 家寫了 web.service：setup 用 serve --service=svc:<名> 掛到 Tailscale 服務（不是機器 serve），
// 網址是 https://<名>.<tailnet 網域>；後台還沒核准就提示去核准，但掛上與網址照寫；已經指到這個家就不再掛。
func TestTailscaleSetupMountsConfiguredService(t *testing.T) {
	ctx := context.Background()
	home := serviceHome(t, "genie-amy-mini")
	f := &fakeRunner{have: map[string]string{"tailscale": tsBin}, script: map[string]reply{
		tsBin + " status --json":          {tsStatusJSON(`"tag:hub"`, ""), 0},
		tsBin + " serve":                  {"", 0},
		tsBin + " serve get-config --all": serveConfig(map[string]string{"svc:notes-amy-office": "http://127.0.0.1:8080"}),
	}}
	lines, err := Tailscale{Runner: f, Home: home}.Setup(ctx)
	if err != nil || !f.called(tsBin+" serve --service=svc:genie-amy-mini --https=443 http://127.0.0.1:18850") {
		t.Fatalf("setup：%v %v %v", lines, err, f.calls)
	}
	if f.called(tsBin + " serve --bg") {
		t.Fatalf("有服務就不該再開機器 serve：%v", f.calls)
	}
	if h, _ := platform.LoadHome(home); h.Web.PublicURL != "https://genie-amy-mini.example.ts.net" {
		t.Fatalf("public_url：%q", h.Web.PublicURL)
	}
	if out := strings.Join(lines, "\n"); !strings.Contains(out, "還沒核准") || !strings.Contains(out, "autoApprovers") {
		t.Fatalf("沒核准要提示：\n%s", out)
	}
	f.script[tsBin+" status --json"] = reply{tsStatusJSON(`"tag:hub"`, "svc:genie-amy-mini"), 0}
	f.script[tsBin+" serve get-config --all"] = serveConfig(map[string]string{"svc:genie-amy-mini": "http://127.0.0.1:18850"})
	f.calls = nil
	if lines, err = (Tailscale{Runner: f, Home: home}).Setup(ctx); err != nil || !strings.Contains(strings.Join(lines, "\n"), "已核准") || f.called(tsBin+" serve --service") {
		t.Fatalf("重跑、已掛好又核准：%v %v %v", lines, err, f.calls)
	}
}

// 服務名稱對上、目標卻不是這個家的看板（例：家人的另一個家、別的程式用了同名服務）：setup 拒絕、說現在指到哪，不 serve、不改 home.toml。
func TestTailscaleSetupRefusesServicePointingElsewhere(t *testing.T) {
	ctx := context.Background()
	home := serviceHome(t, "genie-family-mini")
	f := &fakeRunner{have: map[string]string{"tailscale": tsBin}, script: map[string]reply{
		tsBin + " status --json":          {tsStatusJSON(`"tag:hub"`, ""), 0},
		tsBin + " serve":                  {"", 0},
		tsBin + " serve get-config --all": serveConfig(map[string]string{"svc:genie-family-mini": "http://127.0.0.1:8851"}),
	}}
	_, err := Tailscale{Runner: f, Home: home}.Setup(ctx)
	if err == nil || !strings.Contains(platformMessage(err), "http://127.0.0.1:8851") || f.called(tsBin+" serve --service") {
		t.Fatalf("指到別處要拒絕：%v %v", err, f.calls)
	}
	if h, _ := platform.LoadHome(home); h.Web.PublicURL != "" {
		t.Fatalf("不該改 public_url：%q", h.Web.PublicURL)
	}
	// get-config 讀不到（CLI 太舊）也不敢掛。
	f.script[tsBin+" serve get-config --all"] = reply{"unknown subcommand", 1}
	if _, err := (Tailscale{Runner: f, Home: home}).Setup(ctx); err == nil || f.called(tsBin+" serve --service") {
		t.Fatalf("讀不到 serve 設定不該掛：%v %v", err, f.calls)
	}
}

// 沒寫 web.service，但 public_url 的第一段剛好是這台的某個服務：機器 serve 模式拒絕、提示用 --service，
// 不把機器 443 重新打開、不把 public_url 改回機器網址。
func TestTailscaleMachineServeRefusesWhenPublicURLIsService(t *testing.T) {
	ctx := context.Background()
	home := appendWeb(t, "public_url = \"https://genie-amy-mini.example.ts.net\"\n")
	f := &fakeRunner{have: map[string]string{"tailscale": tsBin}, script: map[string]reply{
		tsBin + " status --json":          {tsStatusJSON(`"tag:hub"`, ""), 0},
		tsBin + " serve":                  {"", 0},
		tsBin + " serve get-config --all": serveConfig(map[string]string{"svc:genie-amy-mini": "http://127.0.0.1:18850"}),
	}}
	_, err := Tailscale{Runner: f, Home: home}.Setup(ctx)
	if err == nil || !strings.Contains(platformMessage(err), "--service genie-amy-mini") || f.called(tsBin+" serve --bg") {
		t.Fatalf("要拒絕並提示 --service：%v %v", err, f.calls)
	}
	if h, _ := platform.LoadHome(home); h.Web.PublicURL != "https://genie-amy-mini.example.ts.net" {
		t.Fatalf("public_url 不該被改：%q", h.Web.PublicURL)
	}
}

// setup --service <名>：家裡還沒寫 web.service 也走服務，做完把名稱寫進 web.service（給 status、down、doctor 用）。
func TestTailscaleSetupServiceFlagWritesHome(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	f := &fakeRunner{have: map[string]string{"tailscale": tsBin}, script: map[string]reply{
		tsBin + " status --json":          {tsStatusJSON(`"tag:hub"`, ""), 0},
		tsBin + " serve":                  {"", 0},
		tsBin + " serve get-config --all": serveConfig(nil),
	}}
	if _, err := (Tailscale{Runner: f, Home: home, Service: "svc:genie-dev-amy-mini"}).Setup(ctx); err != nil {
		t.Fatal(err)
	}
	if !f.called(tsBin + " serve --service=svc:genie-dev-amy-mini --https=443") {
		t.Fatalf("要走服務：%v", f.calls)
	}
	h, _ := platform.LoadHome(home)
	if h.Web.Service != "genie-dev-amy-mini" || h.Web.PublicURL != "https://genie-dev-amy-mini.example.ts.net" {
		t.Fatalf("home.toml：service=%q public_url=%q", h.Web.Service, h.Web.PublicURL)
	}
	if _, err := (Tailscale{Runner: f, Home: home, Service: "Bad_Name"}).Setup(ctx); err == nil {
		t.Fatal("不合格的服務名稱要擋")
	}
}

// 服務主機一定要是 tag 身分：個人帳號登入的這台，setup 停下來說要改 tag，什麼都不 serve、不改 home.toml。
func TestTailscaleSetupServiceRefusesUntaggedHost(t *testing.T) {
	ctx := context.Background()
	home := serviceHome(t, "genie-amy-mini")
	f := &fakeRunner{have: map[string]string{"tailscale": tsBin}, script: map[string]reply{
		tsBin + " status --json": {tsStatusJSON("", ""), 0},
		tsBin + " serve":         {"", 0},
	}}
	_, err := Tailscale{Runner: f, Home: home}.Setup(ctx)
	if err == nil || !strings.Contains(platformMessage(err), "服務主機要用 tag 身分") {
		t.Fatalf("非 tag 身分要報錯：%v", err)
	}
	if f.called(tsBin + " serve") {
		t.Fatalf("沒 tag 不能 serve：%v", f.calls)
	}
	if h, _ := platform.LoadHome(home); h.Web.PublicURL != "" {
		t.Fatalf("不該改 public_url：%q", h.Web.PublicURL)
	}
}

// 找 CLI：先用 Tailscale App 內那支（跟常駐程式同版），沒有才用 PATH 上的 tailscale。
func TestTailscaleCLIPrefersAppBundle(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	both := &fakeRunner{have: map[string]string{"tailscale": tsBin, AppCLI: AppCLI}}
	Tailscale{Runner: both, Home: home}.Backend(ctx)
	if !both.called(AppCLI+" status --json") || both.called(tsBin) {
		t.Fatalf("兩支都有要用 App 那支：%v", both.calls)
	}
	pathOnly := &fakeRunner{have: map[string]string{"tailscale": tsBin}}
	Tailscale{Runner: pathOnly, Home: home}.Backend(ctx)
	if !pathOnly.called(tsBin + " status --json") {
		t.Fatalf("沒有 App 用 PATH 上的：%v", pathOnly.calls)
	}
}

// status 有服務時列出服務名、網址、這台有沒有掛上（名稱與 tcp:443 的目標都要對上）、後台有沒有核准（CapMap["service-host"]）。
func TestTailscaleStatusReadsServiceMountAndApproval(t *testing.T) {
	ctx := context.Background()
	home := serviceHome(t, "genie-amy-mini")
	f := &fakeRunner{have: map[string]string{"tailscale": tsBin}, script: map[string]reply{
		tsBin + " status --json":          {tsStatusJSON(`"tag:hub"`, ""), 0},
		tsBin + " serve get-config --all": serveConfig(map[string]string{"svc:other": "http://127.0.0.1:9"}),
	}}
	ts := Tailscale{Runner: f, Home: home}
	out := strings.Join(ts.Status(ctx), "\n")
	for _, want := range []string{"服務：svc:genie-amy-mini，https://genie-amy-mini.example.ts.net", "掛上：還沒", "核准：還沒", "身分：tag（tag:hub）"} {
		if !strings.Contains(out, want) {
			t.Fatalf("缺 %q：\n%s", want, out)
		}
	}
	f.script[tsBin+" serve get-config --all"] = serveConfig(map[string]string{"svc:genie-amy-mini": "http://127.0.0.1:8851"})
	if s := ts.CheckService(ctx); s.Mounted || !s.Elsewhere || s.Target != "http://127.0.0.1:8851" {
		t.Fatalf("名稱對、目標不對不算掛上：%+v", s)
	}
	if out := strings.Join(ts.Status(ctx), "\n"); !strings.Contains(out, "指到別的地方（http://127.0.0.1:8851") {
		t.Fatalf("要說指到別處：\n%s", out)
	}
	f.script[tsBin+" status --json"] = reply{tsStatusJSON(`"tag:hub"`, "svc:genie-amy-mini"), 0}
	f.script[tsBin+" serve get-config --all"] = serveConfig(map[string]string{"svc:genie-amy-mini": "http://localhost:18850/"})
	if s := ts.CheckService(ctx); !s.Mounted || s.Elsewhere || !s.Approved || !s.Tagged || s.URL != "https://genie-amy-mini.example.ts.net" {
		t.Fatalf("CheckService：%+v", s)
	}
	if out := strings.Join(ts.Status(ctx), "\n"); !strings.Contains(out, "掛上：有") || !strings.Contains(out, "核准：有") {
		t.Fatalf("掛上又核准：\n%s", out)
	}
}

// home.toml 讀不懂時，down 不能當成「沒寫服務」去關這台機器的 443 serve（可能是別的家在用），status 說讀不懂；
// 服務名稱格式錯一樣擋；服務模式的 setup 與 down 都不接受 443 以外的 --https-port；serve 失敗不改 home.toml；get-config 讀不到時 status 說「不知道」。
func TestTailscaleServiceErrorPaths(t *testing.T) {
	ctx := context.Background()
	broken := testHome(t)
	if err := os.WriteFile(filepath.Join(broken, "home.toml"), []byte("[web\nport = \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	badName := serviceHome(t, "Bad_Name")
	f := &fakeRunner{have: map[string]string{"tailscale": tsBin}, script: map[string]reply{
		tsBin + " status --json":          {tsStatusJSON(`"tag:hub"`, ""), 0},
		tsBin + " serve":                  {"", 0},
		tsBin + " serve get-config --all": serveConfig(nil),
	}}
	for _, home := range []string{broken, badName} {
		if _, err := (Tailscale{Runner: f, Home: home}).Down(ctx); err == nil || f.called(tsBin+" serve") {
			t.Fatalf("%s：不該關任何 serve：%v %v", home, err, f.calls)
		}
	}
	if out := strings.Join(Tailscale{Runner: f, Home: broken}.Status(ctx), "\n"); !strings.Contains(out, "home.toml 讀不懂") {
		t.Fatalf("status 要說讀不懂：\n%s", out)
	}

	home := serviceHome(t, "genie-amy-mini")
	if _, err := (Tailscale{Runner: f, Home: home, Port: 8443}).Setup(ctx); err == nil || !strings.Contains(platformMessage(err), "443") || f.called(tsBin+" serve") {
		t.Fatalf("setup 服務模式要擋 8443：%v %v", err, f.calls)
	}
	if _, err := (Tailscale{Runner: f, Home: home, Port: 8443}).Down(ctx); err == nil || !strings.Contains(platformMessage(err), "443") || f.called(tsBin+" serve") {
		t.Fatalf("down 服務模式要擋 8443：%v %v", err, f.calls)
	}
	f.script[tsBin+" serve --service"] = reply{"service not found", 1}
	if _, err := (Tailscale{Runner: f, Home: home}).Setup(ctx); err == nil || !strings.Contains(platformMessage(err), "service not found") {
		t.Fatalf("serve 失敗要說明：%v", err)
	}
	if h, _ := platform.LoadHome(home); h.Web.PublicURL != "" {
		t.Fatalf("serve 失敗不該寫 public_url：%q", h.Web.PublicURL)
	}
	f.script[tsBin+" serve get-config --all"] = reply{"unknown subcommand", 1}
	if out := strings.Join(Tailscale{Runner: f, Home: home}.Status(ctx), "\n"); !strings.Contains(out, "掛上：不知道") {
		t.Fatalf("get-config 讀不到要說不知道：\n%s", out)
	}
}

// down 有服務時：目標是這個家的看板才 serve clear svc:<名>（不用機器 serve 的 --https=443 off）；
// 沒掛在這台就說沒東西要關；指到別處（別的家、別的程式）就拒絕、不 clear。
func TestTailscaleDownClearsOnlyOwnService(t *testing.T) {
	ctx := context.Background()
	home := serviceHome(t, "genie-amy-mini")
	f := &fakeRunner{have: map[string]string{"tailscale": tsBin}, script: map[string]reply{
		tsBin + " serve clear":            {"", 0},
		tsBin + " serve get-config --all": serveConfig(map[string]string{"svc:genie-amy-mini": "http://127.0.0.1:18850"}),
	}}
	if _, err := (Tailscale{Runner: f, Home: home}).Down(ctx); err != nil || !f.called(tsBin+" serve clear svc:genie-amy-mini") || f.called(tsBin+" serve --https") {
		t.Fatalf("down：%v %v", err, f.calls)
	}
	f.calls = nil
	f.script[tsBin+" serve get-config --all"] = serveConfig(nil)
	if lines, err := (Tailscale{Runner: f, Home: home}).Down(ctx); err != nil || !strings.Contains(strings.Join(lines, ""), "沒有東西要關") || f.called(tsBin+" serve clear") {
		t.Fatalf("沒掛上：%v %v %v", lines, err, f.calls)
	}
	f.script[tsBin+" serve get-config --all"] = serveConfig(map[string]string{"svc:genie-amy-mini": "http://127.0.0.1:8851"})
	if _, err := (Tailscale{Runner: f, Home: home}).Down(ctx); err == nil || !strings.Contains(platformMessage(err), "8851") || f.called(tsBin+" serve clear") {
		t.Fatalf("指到別處不該 clear：%v %v", err, f.calls)
	}
}
