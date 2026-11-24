package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// writeExe 寫一個可以執行的檔案（假的引擎或假的 tmux）。
func writeExe(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func noTmuxOnPath(string) (string, error) { return "", errors.New("not found") }

// TestFindTmuxUsesTheOneNextToTheRealEngineBinary：引擎是經符號連結叫起來的（例如 current/bin → versions/<版本>/bin），
// 找的是解開連結後那個資料夾裡的 tmux，不是連結所在資料夾的，也不是 PATH 上的（ECL-63）。
func TestFindTmuxUsesTheOneNextToTheRealEngineBinary(t *testing.T) {
	dir := t.TempDir()
	realBin := filepath.Join(dir, "versions", "1", "bin")
	writeExe(t, filepath.Join(realBin, "evergenie-engine-claude"), "#!/bin/sh\n")
	writeExe(t, filepath.Join(realBin, "tmux"), "#!/bin/sh\n")
	linkDir := filepath.Join(dir, "link")
	writeExe(t, filepath.Join(linkDir, "tmux"), "#!/bin/sh\n") // 連結旁邊的不算
	link := filepath.Join(linkDir, "evergenie-engine-claude")
	if err := os.Symlink(filepath.Join(realBin, "evergenie-engine-claude"), link); err != nil {
		t.Fatal(err)
	}
	onPath := func(string) (string, error) { return "/opt/homebrew/bin/tmux", nil }
	want, err := filepath.EvalSymlinks(filepath.Join(realBin, "tmux"))
	if err != nil {
		t.Fatal(err)
	}
	if got := chooseTmux(link, onPath).bin; got != want {
		t.Fatalf("要用引擎真正所在資料夾旁邊的 tmux：got %q want %q", got, want)
	}
}

// TestFindTmuxFallsBackToPathOnlyWhenNothingUsableIsBundled：旁邊沒有、或旁邊那個不能執行、是資料夾，才用 PATH 上的；
// 兩邊都沒有回空字串。
func TestFindTmuxFallsBackToPathOnlyWhenNothingUsableIsBundled(t *testing.T) {
	onPath := func(string) (string, error) { return "/opt/homebrew/bin/tmux", nil }
	plain := filepath.Join(t.TempDir(), "evergenie-engine-claude")
	writeExe(t, plain, "#!/bin/sh\n")
	notExec := filepath.Join(t.TempDir(), "evergenie-engine-claude")
	writeExe(t, notExec, "#!/bin/sh\n")
	if err := os.WriteFile(filepath.Join(filepath.Dir(notExec), "tmux"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	isDir := filepath.Join(t.TempDir(), "evergenie-engine-claude")
	writeExe(t, isDir, "#!/bin/sh\n")
	if err := os.Mkdir(filepath.Join(filepath.Dir(isDir), "tmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, self := range map[string]string{"旁邊沒有": plain, "旁邊的不能執行": notExec, "旁邊的是資料夾": isDir, "不知道自己在哪": "", "自己不存在": "/nope/evergenie-engine-claude"} {
		if got := chooseTmux(self, onPath).bin; got != "/opt/homebrew/bin/tmux" {
			t.Errorf("%s：要用 PATH 上的，got %q", name, got)
		}
	}
	if got := chooseTmux(plain, noTmuxOnPath).bin; got != "" {
		t.Fatalf("兩邊都沒有要回空字串：%q", got)
	}
}

// TestFindTmuxKeepsTheStablePathWhenItIsTheSameFile：下載版從 current/bin（指到 versions/<版本>/bin 的連結）叫起引擎時，
// 回 current/bin/tmux 而不是帶版本號的路徑：接上畫面的指令記在 session 上，升級刪掉舊版資料夾後還是對的（ECL-63）。
func TestFindTmuxKeepsTheStablePathWhenItIsTheSameFile(t *testing.T) {
	dir := t.TempDir()
	realBin := filepath.Join(dir, "versions", "1.2.3", "bin")
	writeExe(t, filepath.Join(realBin, "evergenie-engine-claude"), "#!/bin/sh\n")
	writeExe(t, filepath.Join(realBin, "tmux"), "#!/bin/sh\n")
	if err := os.Symlink(filepath.Join("versions", "1.2.3"), filepath.Join(dir, "current")); err != nil {
		t.Fatal(err)
	}
	self := filepath.Join(dir, "current", "bin", "evergenie-engine-claude")
	if got, want := chooseTmux(self, noTmuxOnPath).bin, filepath.Join(dir, "current", "bin", "tmux"); got != want {
		t.Fatalf("同一個檔要回不帶版本號的路徑：got %q want %q", got, want)
	}
}

// TestExecTmuxRefindsTmuxInstalledLater：每次執行都重找（ECL-63）：serve 開著時才補上的 tmux 不用重開引擎就用得到。
func TestExecTmuxRefindsTmuxInstalledLater(t *testing.T) {
	bin := t.TempDir()
	self := filepath.Join(bin, "evergenie-engine-claude")
	writeExe(t, self, "#!/bin/sh\n")
	tm := &ExecTmux{Self: self, lookPath: noTmuxOnPath}
	if got := tm.Path(); got != "tmux" {
		t.Fatalf("兩邊都沒有時退回裸的 tmux（錯誤訊息照 tmux 原本的）：%q", got)
	}
	writeExe(t, filepath.Join(bin, "tmux"), "#!/bin/sh\n")
	if got, want := tm.Path(), filepath.Join(bin, "tmux"); got != want {
		t.Fatalf("後來補上的要找得到：got %q want %q", got, want)
	}
}

// TestExecTmuxBinOverridesLookup：指定了 Bin 就用它，不管引擎旁邊或 PATH 上有沒有（測試與特殊安裝用）。
func TestExecTmuxBinOverridesLookup(t *testing.T) {
	bin := t.TempDir()
	self := filepath.Join(bin, "evergenie-engine-claude")
	writeExe(t, self, "#!/bin/sh\n")
	writeExe(t, filepath.Join(bin, "tmux"), "#!/bin/sh\n")
	tm := &ExecTmux{Bin: "/custom/tmux", Self: self, lookPath: func(string) (string, error) { return "/opt/homebrew/bin/tmux", nil }}
	if got := tm.Path(); got != "/custom/tmux" {
		t.Fatalf("指定的 Bin 優先：%q", got)
	}
}

// TestExecTmuxStartsBundledTmuxWhenPathHasNone：PATH 上完全沒有 tmux（一般人的 Mac），引擎旁邊附帶的那支照樣開得起 session，
// Path() 也回它的完整路徑（attach 指令用）。假的 tmux 是一支只記參數的 sh 指令稿。
func TestExecTmuxStartsBundledTmuxWhenPathHasNone(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	bin := t.TempDir()
	self := filepath.Join(bin, "evergenie-engine-claude")
	writeExe(t, self, "#!/bin/sh\n")
	log := filepath.Join(t.TempDir(), "calls.log")
	writeExe(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> "+shellQuote(log)+"\n")
	tm := &ExecTmux{Self: self, ScriptDir: t.TempDir()}
	if got, want := tm.Path(), filepath.Join(bin, "tmux"); got != want {
		t.Fatalf("Path() 要是引擎旁邊那支：got %q want %q", got, want)
	}
	if err := tm.Start("hg-s4", "code", "", []string{"HOME=/Users/tester"}, []string{"claude"}); err != nil {
		t.Fatalf("PATH 沒有 tmux 也要開得起來：%v", err)
	}
	if !tm.Alive("code") {
		t.Fatal("has-session 也要走引擎旁邊那支")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("引擎旁邊的 tmux 沒被叫到：%v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "-L hg-s4 new-session -d -s code ") || lines[1] != "-L hg-s4 has-session -t =code" {
		t.Fatalf("叫 tmux 的參數不對：%q", lines)
	}
}

// TestServeDefaultTmuxLooksNextToEngine：serve 用的真 tmux 拿自己的執行檔當基準找附帶的 tmux（ECL-63）。
func TestServeDefaultTmuxLooksNextToEngine(t *testing.T) {
	cfg, err := ServeConfigFromEnv(func(k string) string { return map[string]string{"EVERGENIE_HOME": t.TempDir()}[k] }, "/opt/hg/bin/evergenie-engine-claude")
	if err != nil {
		t.Fatal(err)
	}
	if tm := defaultTmux(cfg); tm.Self != "/opt/hg/bin/evergenie-engine-claude" || tm.Bin != "" {
		t.Fatalf("serve 的 tmux 要從引擎執行檔旁邊找：%+v", tm)
	}
}

// TestStartAttachUsesFoundTmuxPath：Start 回的 attach 用實際會執行的 tmux 完整路徑（附帶的那支不在 PATH 上）；
// 路徑有空白就加引號，貼到 shell 才不會斷開（ECL-15、ECL-63）。
func TestStartAttachUsesFoundTmuxPath(t *testing.T) {
	for path, want := range map[string]string{
		"/Users/tester/.evergenie/app/versions/1.2.3/bin/tmux": "/Users/tester/.evergenie/app/versions/1.2.3/bin/tmux -L hg-test attach -t '=code'",
		"/Users/tester/My Apps/bin/tmux":                       "'/Users/tester/My Apps/bin/tmux' -L hg-test attach -t '=code'",
	} {
		v := newEngEnv(t)
		v.tmux.path = path
		r := v.req("code")
		r.EngineConfig["tmux_socket"] = "hg-test"
		resp, err := v.e.Start(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Attach != want {
			t.Errorf("接上畫面的指令不對：got %q want %q", resp.Attach, want)
		}
	}
}

// TestAttachSocketReadsOldAndNewFormats：引擎重開後從核心記的 attach 補回 socket（ECL-31）：
// 舊紀錄（tmux -L …）、新的完整路徑、有引號的路徑都要讀得出來。
func TestAttachSocketReadsOldAndNewFormats(t *testing.T) {
	for attach, want := range map[string]string{
		"tmux -L hg attach -t '=code'":                     "hg",
		attachCommand("/a/bin/tmux", "hg-ws", "code"):      "hg-ws",
		attachCommand("/a -L b/tmux", "hg-dev", "code-c1"): "hg-dev", // 路徑裡也有「 -L 」：取最後一個
		"something else": "",
	} {
		if got := attachSocket(attach); got != want {
			t.Errorf("attachSocket(%q) = %q，要 %q", attach, got, want)
		}
	}
}

// TestEnginePreflightTmuxFoundOnlyNextToEngine：PATH 上沒有 tmux、引擎旁邊有附帶的，安裝前檢查要算過（不是紅字）。
func TestEnginePreflightTmuxFoundOnlyNextToEngine(t *testing.T) {
	v := newEngEnv(t)
	bin := t.TempDir()
	v.e.d.HookBin = filepath.Join(bin, "evergenie-engine-claude")
	writeExe(t, v.e.d.HookBin, "#!/bin/sh\n")
	writeExe(t, filepath.Join(bin, "tmux"), "#!/bin/sh\n")
	v.e.d.LookPath = func(name string) (string, error) {
		if name == "tmux" {
			return "", errors.New("not found")
		}
		return "/usr/local/bin/" + name, nil
	}
	r, err := v.e.Preflight(context.Background(), &sdk.EnginePreflightRequest{Account: "acct-a", EngineConfig: map[string]any{"config_dir": v.cfgDir}})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(bin, "tmux")
	for _, c := range r.Checks {
		if c.ID == "tmux" {
			if !c.Ok || !strings.HasPrefix(c.Detail, want) {
				t.Fatalf("引擎旁邊有 tmux 就算過：%+v", c)
			}
			return
		}
	}
	t.Fatalf("沒有 tmux 這一項：%+v", r.Checks)
}

// withProductName 暫時設附帶 tmux 的檔名（發行檔由 scripts/dist.sh 用 -ldflags 帶入產品名），測完還原。
func withProductName(t *testing.T, name string) {
	t.Helper()
	old := bundledTmuxName
	bundledTmuxName = name
	t.Cleanup(func() { bundledTmuxName = old })
}

// downloadLayout 建一個下載版的程式資料夾：<app>/versions/1.2.3/bin/<引擎>（＋舊式 bin/tmux）、<app>/current → versions/1.2.3；
// fixedApp／fixedVer 決定 <app>/fixed/ 與 <版本>/fixed/ 放哪些檔（tmux 檔名是產品名 hgtest、啟動程式 disclaim）。回 <app> 與從 current 叫的引擎路徑。
func downloadLayout(t *testing.T, fixedApp, fixedVer []string) (app, self string) {
	t.Helper()
	app, err := filepath.EvalSymlinks(t.TempDir()) // 程式資料夾的 fixed/ 從解開連結後的路徑算（macOS 的暫存資料夾在 /var → /private/var 底下）
	if err != nil {
		t.Fatal(err)
	}
	ver := filepath.Join(app, "versions", "1.2.3")
	writeExe(t, filepath.Join(ver, "bin", "evergenie-engine-claude"), "#!/bin/sh\n")
	writeExe(t, filepath.Join(ver, "bin", "tmux"), "#!/bin/sh\n")
	for _, n := range fixedApp {
		writeExe(t, filepath.Join(app, "fixed", n), "#!/bin/sh\n")
	}
	for _, n := range fixedVer {
		writeExe(t, filepath.Join(ver, "fixed", n), "#!/bin/sh\n")
	}
	if err := os.Symlink(filepath.Join("versions", "1.2.3"), filepath.Join(app, "current")); err != nil {
		t.Fatal(err)
	}
	return app, filepath.Join(app, "current", "bin", "evergenie-engine-claude")
}

// TestChooseTmuxPrefersFixedLocation（ECL-65）：發行檔（有產品名）先用程式資料夾 fixed/ 裡、檔名是產品名的那支，
// 連同旁邊的啟動程式；不用帶版本號的資料夾裡的，也不用舊式的 bin/tmux 或 PATH 上的。
func TestChooseTmuxPrefersFixedLocation(t *testing.T) {
	withProductName(t, "hgtest")
	app, self := downloadLayout(t, []string{"hgtest", "disclaim"}, []string{"hgtest", "disclaim"})
	onPath := func(string) (string, error) { return "/opt/homebrew/bin/tmux", nil }
	c := chooseTmux(self, onPath)
	if c.bin != filepath.Join(app, "fixed", "hgtest") || c.launcher != filepath.Join(app, "fixed", "disclaim") || !c.bundled {
		t.Fatalf("要用程式資料夾 fixed/ 的那支與它旁邊的啟動程式：%+v", c)
	}
}

// TestChooseTmuxFallsBackToVersionFixed（ECL-65）：程式資料夾的 fixed/ 還沒放好（例如從還不會放的舊版升上來）時，
// 用這一版自己的 fixed/，路徑取不帶版本號的 current/fixed/…（接上畫面的指令記在 session 上，舊版資料夾刪掉也還對）。
// fixed/ 裡的產品名那支不能執行時當作沒有。
func TestChooseTmuxFallsBackToVersionFixed(t *testing.T) {
	withProductName(t, "hgtest")
	app, self := downloadLayout(t, nil, []string{"hgtest", "disclaim"})
	c := chooseTmux(self, noTmuxOnPath)
	if c.bin != filepath.Join(app, "current", "fixed", "hgtest") || c.launcher != filepath.Join(app, "current", "fixed", "disclaim") {
		t.Fatalf("要用這一版的 fixed/（不帶版本號的路徑）：%+v", c)
	}
	if err := os.Chmod(filepath.Join(app, "versions", "1.2.3", "fixed", "hgtest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := chooseTmux(self, noTmuxOnPath); c.bin != filepath.Join(app, "current", "bin", "tmux") || c.launcher != "" {
		t.Fatalf("fixed/ 的不能執行就退回舊式的 bin/tmux、不經啟動程式：%+v", c)
	}
}

// TestChooseTmuxWithoutLauncherOrProductName（ECL-65 第 4 點）：fixed/ 有 tmux 但沒有啟動程式 → 用它、直接開；
// 不是發行檔（沒帶產品名，從原始碼編的）→ 完全不看 fixed/，照 ECL-63 找。
func TestChooseTmuxWithoutLauncherOrProductName(t *testing.T) {
	withProductName(t, "hgtest")
	app, self := downloadLayout(t, []string{"hgtest"}, nil)
	if c := chooseTmux(self, noTmuxOnPath); c.bin != filepath.Join(app, "fixed", "hgtest") || c.launcher != "" {
		t.Fatalf("沒有啟動程式就直接開 fixed/ 的那支：%+v", c)
	}
	withProductName(t, "")
	app, self = downloadLayout(t, []string{"hgtest", "disclaim"}, []string{"hgtest", "disclaim"})
	if c := chooseTmux(self, noTmuxOnPath); c.bin != filepath.Join(app, "current", "bin", "tmux") || c.launcher != "" {
		t.Fatalf("從原始碼編的不看 fixed/：%+v", c)
	}
	onPath := func(string) (string, error) { return "/opt/homebrew/bin/tmux", nil }
	if c := chooseTmux("", onPath); c.bin != "/opt/homebrew/bin/tmux" || c.launcher != "" || c.bundled {
		t.Fatalf("PATH 上的不經啟動程式：%+v", c)
	}
}

// TestExecTmuxStartsServerThroughLauncher（ECL-65）：找得到啟動程式時，開 session（會順便開 tmux server）的 new-session
// 經它：`<disclaim> <tmux> -L … new-session …`；其他指令（has-session、send-keys…）直接叫 tmux。假的 disclaim 記一筆再 exec 後面的指令。
func TestExecTmuxStartsServerThroughLauncher(t *testing.T) {
	withProductName(t, "hgtest")
	t.Setenv("PATH", t.TempDir())
	app, self := downloadLayout(t, nil, nil)
	log := filepath.Join(t.TempDir(), "calls.log")
	tmuxPath := filepath.Join(app, "fixed", "hgtest")
	writeExe(t, tmuxPath, "#!/bin/sh\nprintf 'tmux %s\\n' \"$*\" >> "+shellQuote(log)+"\n")
	writeExe(t, filepath.Join(app, "fixed", "disclaim"), "#!/bin/sh\nprintf 'disclaim %s\\n' \"$*\" >> "+shellQuote(log)+"\nexec \"$@\"\n")
	tm := &ExecTmux{Self: self, ScriptDir: t.TempDir()}
	if got := tm.Path(); got != tmuxPath {
		t.Fatalf("Path() 要是固定位置那支（接上畫面用，不經啟動程式）：%q", got)
	}
	if err := tm.Start("hg-s8", "code", "", nil, []string{"claude"}); err != nil {
		t.Fatal(err)
	}
	if !tm.Alive("code") {
		t.Fatal("has-session 要找得到")
	}
	if err := tm.SendKey("code", "Enter"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 ||
		!strings.HasPrefix(lines[0], "disclaim "+tmuxPath+" -L hg-s8 new-session -d -s code ") ||
		!strings.HasPrefix(lines[1], "tmux -L hg-s8 new-session -d -s code ") ||
		lines[2] != "tmux -L hg-s8 has-session -t =code" ||
		lines[3] != "tmux -L hg-s8 send-keys -t =code: Enter" {
		t.Fatalf("只有 new-session 經啟動程式：%q", lines)
	}
}

// TestExecTmuxWithoutLauncherIsUnchanged（ECL-65 第 4 點）：找不到啟動程式（舊包、從原始碼跑、指定了 Bin）時，
// 每個指令都直接叫 tmux，跟以前一樣。
func TestExecTmuxWithoutLauncherIsUnchanged(t *testing.T) {
	withProductName(t, "hgtest")
	args := []string{"-L", "hg", "new-session", "-d", "-s", "code"}
	_, oldSelf := downloadLayout(t, nil, nil) // 只有舊式的 bin/tmux
	old := &ExecTmux{Self: oldSelf, lookPath: noTmuxOnPath}
	if got := old.command(args); got[0] != old.Path() || strings.Join(got[1:], " ") != strings.Join(args, " ") {
		t.Fatalf("沒有啟動程式就直接開：%q", got)
	}
	app, self := downloadLayout(t, []string{"hgtest", "disclaim"}, nil)
	pinned := &ExecTmux{Bin: "/custom/tmux", Self: self}
	if got := strings.Join(pinned.command(args), " "); got != "/custom/tmux "+strings.Join(args, " ") {
		t.Fatalf("指定了 Bin 不經啟動程式：%q", got)
	}
	found := &ExecTmux{Self: self}
	if got := strings.Join(found.command(args), " "); got != filepath.Join(app, "fixed", "disclaim")+" "+filepath.Join(app, "fixed", "hgtest")+" "+strings.Join(args, " ") {
		t.Fatalf("找得到就經啟動程式：%q", got)
	}
	if got := strings.Join(found.command([]string{"-L", "hg", "send-keys", "-t", "=code:", "new-session"}), " "); got != filepath.Join(app, "fixed", "hgtest")+" -L hg send-keys -t =code: new-session" {
		t.Fatalf("別的指令（參數裡剛好有 new-session 也一樣）不經啟動程式：%q", got)
	}
}

// TestExecTmuxStartWarnsOnLauncherFallback（ECL-65）：啟動程式設不起來時照平常的方式開（session 照樣開得起來），原因轉給 Warn 留紀錄。
func TestExecTmuxStartWarnsOnLauncherFallback(t *testing.T) {
	var warned []string
	tm := &ExecTmux{ScriptDir: t.TempDir(), Warn: func(m string) { warned = append(warned, m) }}
	tm.run = func(_ context.Context, _ string, args ...string) (string, error) {
		return launcherFallbackNote + "Function not implemented\n", nil
	}
	if err := tm.Start("hg-test", "code", "", nil, []string{"claude"}); err != nil {
		t.Fatal(err)
	}
	if len(warned) != 1 || warned[0] != launcherFallbackNote+"Function not implemented" {
		t.Fatalf("要留一行原因：%q", warned)
	}
}

// TestEnginePreflightTmuxFixedLocation：安裝前檢查報固定位置那支，並說權限視窗記在它名下（ECL-65）；
// 固定位置還沒放好、用的是版本資料夾裡那支時，照實說升級後可能要再按一次。
func TestEnginePreflightTmuxFixedLocation(t *testing.T) {
	withProductName(t, "hgtest")
	both := []string{"hgtest", "disclaim"}
	for _, tc := range []struct {
		name            string
		fixedApp, fixed []string
		want            func(app string) string
	}{
		{"固定位置", both, nil, func(app string) string {
			return filepath.Join(app, "fixed", "hgtest") + "（跟著程式一起裝的；系統問資料夾權限時記在這一支名下）"
		}},
		{"版本資料夾", nil, both, func(app string) string {
			return filepath.Join(app, "current", "fixed", "hgtest") + "（跟著程式一起裝的；系統問資料夾權限時記在這一支名下，但它還不在固定位置，升級後可能要再按一次允許）"
		}},
	} {
		v := newEngEnv(t)
		app, self := downloadLayout(t, tc.fixedApp, tc.fixed)
		v.e.d.HookBin = self
		v.e.d.LookPath = func(name string) (string, error) {
			if name == "tmux" {
				return "", errors.New("not found")
			}
			return "/usr/local/bin/" + name, nil
		}
		r, err := v.e.Preflight(context.Background(), &sdk.EnginePreflightRequest{Account: "acct-a", EngineConfig: map[string]any{"config_dir": v.cfgDir}})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range r.Checks {
			if c.ID == "tmux" {
				found = true
				if want := tc.want(app); !c.Ok || c.Detail != want {
					t.Fatalf("%s：%+v，要 %q", tc.name, c, want)
				}
			}
		}
		if !found {
			t.Fatalf("%s：沒有 tmux 這一項：%+v", tc.name, r.Checks)
		}
	}
}

// TestChooseTmuxFixedKeepsUnresolvedPath（ECL-65，同 ECL-63）：程式資料夾的上層是符號連結時（例如 /tmp → /private/tmp），
// 引擎從沒解開的 <app>/current/bin 叫起來，固定位置那支回沒解開的 <app>/fixed/…（給人貼的路徑跟主人認得的一樣）。
func TestChooseTmuxFixedKeepsUnresolvedPath(t *testing.T) {
	withProductName(t, "hgtest")
	real, _ := downloadLayout(t, []string{"hgtest", "disclaim"}, nil)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	self := filepath.Join(link, "current", "bin", "evergenie-engine-claude")
	if c := chooseTmux(self, noTmuxOnPath); c.bin != filepath.Join(link, "fixed", "hgtest") || c.launcher != filepath.Join(link, "fixed", "disclaim") {
		t.Fatalf("要回沒解開的路徑：%+v", c)
	}
}

// TestServeDefaultTmuxWarnsIntoEngineLog（ECL-65）：serve 用的 tmux 把小啟動程式印的那一行寫進 <狀態>/engine-claude/out.log
// （核心開引擎時不收 stderr，寫到 stderr 就沒人看得到）；一行一件、前面是時間、不蓋掉前面的。
func TestServeDefaultTmuxWarnsIntoEngineLog(t *testing.T) {
	home := t.TempDir()
	cfg, err := ServeConfigFromEnv(func(k string) string { return map[string]string{"EVERGENIE_HOME": home}[k] }, "/opt/hg/bin/evergenie-engine-claude")
	if err != nil {
		t.Fatal(err)
	}
	tm := defaultTmux(cfg)
	tm.warn(launcherFallbackNote + "第一次")
	tm.warn(launcherFallbackNote + "第二次")
	data, err := os.ReadFile(sdk.RuntimeDir(home, "engine-claude", "out.log"))
	if err != nil {
		t.Fatalf("要寫進引擎的紀錄檔：%v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], launcherFallbackNote+"第一次") || !strings.HasSuffix(lines[1], launcherFallbackNote+"第二次") {
		t.Fatalf("紀錄檔內容不對：%q", data)
	}
	if _, err := time.Parse(time.RFC3339, strings.SplitN(lines[0], " ", 2)[0]); err != nil {
		t.Fatalf("每行前面要有時間：%q", lines[0])
	}
}
