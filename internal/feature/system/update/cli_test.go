package update

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
)

// 節點跟主機同版（K7）：主機是正式版、比自己新、不在跳過清單、自己的頻道不是 off 才升，只升到主機那一版；開發版、比自己舊、認不出的都不動。
// Guards: REQ-280

func upgradeHome(t *testing.T) string {
	t.Helper()
	return testHome(t)
}

// runUpgrade 跑 `evergenie upgrade …`（經 cmdline.Run，跟真的指令列同一條路），回結束碼與 stdout、stderr。
func runUpgrade(t *testing.T, home string, args ...string) (int, string, string) {
	t.Helper()
	return runCmd(t, append([]string{"upgrade"}, append(args, "--home", home)...)...)
}

func runCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cmdline.Run(context.Background(), args, nil, cmdline.IO{Out: &out, Err: &errOut}, Commands(), "usage\n")
	return code, out.String(), errOut.String()
}

// 這台不是用安裝程式裝的（執行檔不在 git repo 的 bin/ 底下）：check／apply／rollback 說白話原因、結束 1；status 照樣能看；
// decide skip 寫進跳過清單、tonight 記下時間；用法錯誤結束 2。
// Guards: REQ-280
func TestUpgradeCommandWithoutCore(t *testing.T) {
	home := upgradeHome(t)
	for _, flags := range [][]string{{"--check"}, {"--apply"}, {"--rollback"}, {"--rollback", "--rescue"}} {
		flag := strings.Join(flags, " ")
		code, _, errOut := runUpgrade(t, home, flags...)
		if code != 1 || !strings.Contains(errOut, "不是用安裝程式裝的") {
			t.Fatalf("%s：%d %q", flag, code, errOut)
		}
	}
	code, out, _ := runUpgrade(t, home, "--status")
	if code != 0 || !strings.Contains(out, "現在的版本") || !strings.Contains(out, "沒辦法自動升級") {
		t.Fatalf("status：%d %q", code, out)
	}
	if code, _, _ := runUpgrade(t, home, "--status", "--check"); code != 2 {
		t.Fatalf("同時給兩個動作是用法錯誤：%d", code)
	}
	if code, _, _ := runUpgrade(t, home); code != 2 {
		t.Fatalf("沒給動作是用法錯誤：%d", code)
	}
	if code, _, _ := runUpgrade(t, home, "--rescue"); code != 2 {
		t.Fatalf("--rescue 只能跟 --rollback 一起給：%d", code)
	}
	// 沒有檢查過就沒有新版可以決定
	if code, _, errOut := runUpgrade(t, home, "--decide", "skip"); code != 1 || !strings.Contains(errOut, "沒有新版") {
		t.Fatalf("decide 沒有版本：%d %q", code, errOut)
	}
	if code, out, _ := runUpgrade(t, home, "--decide", "skip", "--version", "9.9.9"); code != 0 || !strings.Contains(out, "9.9.9") {
		t.Fatalf("decide skip：%d %q", code, out)
	}
	if got := ReadUpgradeSkipped(home); len(got) != 1 || got[0] != "9.9.9" {
		t.Fatalf("跳過清單：%v", got)
	}
	if code, _, _ := runUpgrade(t, home, "--decide", "tonight", "--version", "9.9.8"); code != 0 {
		t.Fatalf("decide tonight：%d", code)
	}
	if d, _ := ReadUpgradeDecision(home); d == nil || d.Version != "9.9.8" || !d.At.After(time.Now()) {
		t.Fatalf("decision：%+v", d)
	}
	code, out, _ = runUpgrade(t, home, "--status", "--json")
	if code != 0 || !strings.Contains(out, `"skipped"`) || !strings.Contains(out, "9.9.8") {
		t.Fatalf("status --json：%q", out)
	}
	// decide now 沒有程式就不能升
	if code, _, errOut := runUpgrade(t, home, "--decide", "now", "--version", "9.9.8"); code != 1 || !strings.Contains(errOut, "不是用安裝程式裝的") {
		t.Fatalf("decide now：%d %q", code, errOut)
	}
}

// migrate-home：沒有要做的就說資料都是新的；有就預設只列出、--apply 才依序做並記下來（重跑不再做）。
// Guards: REQ-280
func TestMigrateHomeCommand(t *testing.T) {
	home := upgradeHome(t)
	old := HomeMigrations
	t.Cleanup(func() { HomeMigrations = old })
	HomeMigrations = nil
	run := func(args ...string) (int, string, string) {
		return runCmd(t, append([]string{"migrate-home"}, append(args, "--home", home)...)...)
	}
	if code, out, _ := run(); code != 0 || !strings.Contains(out, "沒有要整理的") {
		t.Fatalf("%d %q", code, out)
	}
	ran := 0
	HomeMigrations = []HomeMigration{{ID: "t1", Description: "測試用的整理", Run: func(h string) error {
		ran++
		return os.WriteFile(filepath.Join(h, "settings", "t1.txt"), []byte("ok"), 0o644)
	}}}
	if code, out, _ := run(); code != 0 || !strings.Contains(out, "測試用的整理") || !strings.Contains(out, "--apply") || ran != 0 {
		t.Fatalf("預設只列出：%d %q ran=%d", code, out, ran)
	}
	if code, out, _ := run("--apply"); code != 0 || !strings.Contains(out, "已整理：t1") || ran != 1 {
		t.Fatalf("--apply：%d %q ran=%d", code, out, ran)
	}
	if code, _, _ := run("--apply"); code != 0 || ran != 1 {
		t.Fatalf("重跑不再做：ran=%d", ran)
	}
	HomeMigrations = append(HomeMigrations, HomeMigration{ID: "t2", Description: "會失敗的整理", Run: func(string) error { return errors.New("炸了") }})
	if code, _, errOut := run("--apply"); code != 1 || !strings.Contains(errOut, "會失敗的整理") {
		t.Fatalf("失敗：%d %q", code, errOut)
	}
}

// 每天檢查的時間點：今天的 check_at 還沒到不檢查；到了而且今天還沒檢查過才檢查；檢查過（check.json 的時間在 check_at 之後）就不再檢查；隔天又會。
// Guards: REQ-280
func TestUpdaterDailyCheckDue(t *testing.T) {
	home := upgradeHome(t)
	h, _ := platform.LoadHome(home)
	m := New(Deps{})
	loc := h.Location()
	at := func(day, hour, min int) time.Time { return time.Date(2026, 10, day, hour, min, 0, 0, loc) }
	if m.dailyCheckDue(h, at(29, 9, 59)) {
		t.Fatal("10:00 之前不檢查")
	}
	if !m.dailyCheckDue(h, at(29, 10, 0)) {
		t.Fatal("10:00 到了、還沒檢查過")
	}
	if err := platform.WriteJSONFile(filepath.Join(UpgradeDir(home), UpgradeCheckFile), UpgradeCheck{CheckedAt: at(29, 10, 1)}); err != nil {
		t.Fatal(err)
	}
	if m.dailyCheckDue(h, at(29, 15, 0)) {
		t.Fatal("今天檢查過了")
	}
	if !m.dailyCheckDue(h, at(30, 10, 0)) {
		t.Fatal("隔天 10:00 又要檢查")
	}
}

// Guards: REQ-281
func TestFirstNoteLine(t *testing.T) {
	if got := firstNoteLine("## 新增\n- 看板可以更新了\n- 別的"); got != "看板可以更新了" {
		t.Fatalf("%q", got)
	}
	if got := firstNoteLine(""); got == "" {
		t.Fatal("沒有說明也要有一句")
	}
}
