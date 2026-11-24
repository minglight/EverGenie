package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 發行檔 scripts/dist.sh（`make dist`）、它的驗收 scripts/dist-test.sh（`make dist-test`）、下載版升級的整套
// scripts/upgrade-download-test.sh（`make upgrade-download-test`）、乾淨環境的整體驗收 scripts/clean-install-test.sh
// （`make clean-install-test`，S9）、下載版編主人套件的 scripts/package-go-test.sh（`make package-go-test`，要網路）與它們共用的
// scripts/lib/download-e2e.sh 的輕量檢查（DEV-155）：語法正確；
// dist.sh 與兩支整套 --help 結束 0 並印出用法、不認得的選項結束 2。真的編譯與升級太慢，不隨 `make ci` 跑。
func TestDistAndDownloadUpgradeScriptsUsage(t *testing.T) {
	for _, name := range []string{"dist.sh", "dist-test.sh", "upgrade-download-test.sh", "clean-install-test.sh", "package-go-test.sh", "lib/download-e2e.sh"} {
		script, err := filepath.Abs(filepath.Join("..", "..", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
			t.Fatalf("%s 語法不對：%v\n%s", name, err, out)
		}
	}
	for _, tc := range []struct{ name, want string }{
		{"dist.sh", "--base-url"},
		{"upgrade-download-test.sh", "EVERGENIE_E2E_KEEP"},
		{"clean-install-test.sh", "EVERGENIE_E2E_KEEP"},
		{"package-go-test.sh", "要網路"},
	} {
		script, _ := filepath.Abs(filepath.Join("..", "..", "scripts", tc.name))
		out, err := exec.Command("bash", script, "--help").CombinedOutput()
		if err != nil || !strings.Contains(string(out), tc.want) {
			t.Fatalf("%s --help 該結束 0 並印出用法（含 %q）：%v\n%s", tc.name, tc.want, err, out)
		}
		err = exec.Command("bash", script, "--bogus").Run()
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
			t.Fatalf("%s 不認得的選項該結束 2：%v", tc.name, err)
		}
	}
}

// 附帶的 tmux 的啟動程式（engine-claude ECL-65、system 卡 UPD-15）三邊對得上：scripts/build-tmux.sh 裡 C 原始碼的 FALLBACK_NOTE
// ＝引擎的 launcherFallbackNote（引擎靠這個開頭認出「沒能改記在 tmux 名下」那一行、轉進紀錄）；固定位置的資料夾名只定義在
// sdk.DownloadFixedDir（核心 platform.DownloadFixedDir 與引擎都指到它、不自己寫字串），兩支 shell 腳本寫的也是它；
// 啟動程式的檔名在 build-tmux.sh、dist.sh、引擎裡都是同一個。
func TestLauncherNoteMatchesEngine(t *testing.T) {
	read := func(p string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", p))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	build, dist, install := read("scripts/build-tmux.sh"), read("scripts/dist.sh"), read("scripts/install.sh")
	engine := read("packages/official/engine-claude/internal/claude/tmux.go") + read("packages/official/engine-claude/internal/claude/tmuxbin.go")
	find := func(src, pattern string) string {
		t.Helper()
		m := regexp.MustCompile(pattern).FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("找不到 %s", pattern)
		}
		return m[1]
	}
	if c, g := find(build, `(?m)^#define FALLBACK_NOTE "([^"]+)"$`), find(engine, `(?m)^const launcherFallbackNote = "([^"]+)"$`); c != g {
		t.Fatalf("build-tmux.sh 的 FALLBACK_NOTE %q 跟引擎的 launcherFallbackNote %q 不一樣", c, g)
	}
	if !regexp.MustCompile(`(?m)^\s*fixedDirName\s*=\s*sdk\.DownloadFixedDir\b`).MatchString(engine) {
		t.Fatal("引擎找固定位置要用 sdk.DownloadFixedDir（跟核心放檔的位置同一個名字），不要自己寫字串")
	}
	core := read("internal/base/platform/install_layout.go")
	if !regexp.MustCompile(`(?m)^\s*DownloadFixedDir\s*=\s*sdk\.DownloadFixedDir\b`).MatchString(core) {
		t.Fatal("核心的 platform.DownloadFixedDir 要用 sdk.DownloadFixedDir，不要自己寫字串")
	}
	if fixed := sdk.DownloadFixedDir; !strings.Contains(install, `"$DIR/`+fixed+`/$PRODUCT"`) || !strings.Contains(dist, `"$dir/`+fixed+`/$NAME"`) {
		t.Fatalf("install.sh、dist.sh 放檔的資料夾要是 sdk.DownloadFixedDir（%q）", fixed)
	}
	name := find(engine, `launcherExe\s+= "([^"]+)"`)
	if !strings.Contains(build, `bin="$OUT/$1/`+name+`"`) || !strings.Contains(dist, `"$dir/`+sdk.DownloadFixedDir+`/`+name+`"`) {
		t.Fatalf("啟動程式的檔名 %q 在 build-tmux.sh 或 dist.sh 裡對不上", name)
	}
	if !strings.Contains(dist, "engine-claude/internal/claude.bundledTmuxName=$NAME") || !strings.Contains(engine, "var bundledTmuxName string") {
		t.Fatal("dist.sh 要用 -ldflags -X 把產品名編進引擎的 bundledTmuxName")
	}
}
