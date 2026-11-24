package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/homebackup"
	"github.com/minglight/evergenie/internal/feature/system/host"
	"github.com/minglight/evergenie/sdk"
)

// rescueFakes 把修復會碰到外面的幾樣（跑子指令、重開服務、看板有沒有回應、開視窗、結果檔位置）換成假的，回開了什麼的紀錄。
type rescueRec struct {
	self, opened []string
	restarts     int
	reportFails  bool
}

func rescueFakes(t *testing.T, up bool, doctorCode int, restartErr error) *rescueRec {
	t.Helper()
	rec := &rescueRec{}
	oSelf, oOpen, oUp, oRestart, oWait, oResult, oBoard := rescueSelf, rescueOpen, rescueBoardUp, rescueRestart, rescueWait, rescueResult, rescueBoard
	t.Cleanup(func() {
		rescueSelf, rescueOpen, rescueBoardUp, rescueRestart, rescueWait, rescueResult, rescueBoard = oSelf, oOpen, oUp, oRestart, oWait, oResult, oBoard
	})
	dir := t.TempDir()
	rescueSelf = func(_ context.Context, _ string, args ...string) (string, int) {
		rec.self = append(rec.self, strings.Join(args, " "))
		if args[0] == "report" {
			// 子行程印在畫面上的字（這裡故意放一段沒遮的）不能進「可以傳」的那個檔：內容只取 --out 寫的檔。
			if rec.reportFails {
				return "讀不到家 /Users/someone/home", 1
			}
			if err := os.WriteFile(args[len(args)-1], []byte("問題回報：1.2.3\n\n## 環境\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return "另一個家 /Users/someone/home\n", 0
		}
		return "【服務】\n  ✗ EverGenie 沒在跑\n\n3 項正常、0 項注意、1 項要處理\n", doctorCode
	}
	rescueOpen = func(_ context.Context, args ...string) { rec.opened = append(rec.opened, strings.Join(args, " ")) }
	rescueBoardUp = func(context.Context, string) bool { return up }
	rescueRestart = func(context.Context, string) error { rec.restarts++; return restartErr }
	rescueWait = 0
	rescueBoard = func(string) string { return "http://127.0.0.1:18999/" }
	rescueResult = func() string { return dir }
	return rec
}

func rescueHome(t *testing.T) string { return t.TempDir() }

// 修復（HST-10）：健檢 → 重開這個家的服務 → 再健檢；看板好了開看板，沒好開結果檔（裡面有可以傳出去的那一段）。
func TestRescueCommand(t *testing.T) {
	home := rescueHome(t)
	run := func(args ...string) (int, string) {
		var out bytes.Buffer
		code := rescueCommand(context.Background(), cmdline.HostRequest{Home: home, Args: args, Out: &out, Err: &out})
		return code, out.String()
	}

	rec := rescueFakes(t, true, 0, nil)
	code, out := run()
	if code != 0 || rec.restarts != 1 || !strings.Contains(out, "都正常了") || len(rec.opened) != 1 || rec.opened[0] != "http://127.0.0.1:18999/" {
		t.Fatalf("好了要開看板：%d %v\n%s", code, rec.opened, out)
	}
	if got := strings.Join(rec.self, "|"); !strings.HasPrefix(got, "doctor|doctor|report --detail --out ") {
		t.Fatalf("子指令的順序：%s", got)
	}
	if strings.Contains(out, "sudo") || strings.Contains(out, "指令") {
		t.Fatalf("給主人看的字不提指令：\n%s", out)
	}

	rec = rescueFakes(t, true, 1, nil)
	if code, out := run(); code != 0 || !strings.Contains(out, "還有幾項要看") || !strings.Contains(out, "跟任何一位秘書說") || len(rec.opened) != 1 {
		t.Fatalf("看板好了但還有問題：%d\n%s", code, out)
	}

	rec = rescueFakes(t, false, 1, errors.New("重開失敗：x"))
	code, out = run()
	if code != 1 || !strings.Contains(out, "還是開不起來") || !strings.Contains(out, "重開沒成功：重開失敗：x") || len(rec.opened) != 1 || !strings.HasPrefix(rec.opened[0], "-t ") {
		t.Fatalf("沒好要開結果檔：%d %v\n%s", code, rec.opened, out)
	}
	// 開給主人、叫他傳的只有遮過的那一份：沒有健檢全文，也沒有子行程印在畫面上的字。
	shared := strings.TrimPrefix(rec.opened[0], "-t ")
	data, err := os.ReadFile(shared)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(shared), "可以傳的回報-") || !strings.Contains(out, shared) || string(data) != "問題回報：1.2.3\n\n## 環境\n" {
		t.Fatalf("可以傳的那份：%s\n%s", shared, data)
	}
	locals, _ := filepath.Glob(filepath.Join(filepath.Dir(shared), "修復結果-*.txt"))
	if len(locals) != 1 {
		t.Fatalf("留在這台的結果檔：%v", locals)
	}
	local, _ := os.ReadFile(locals[0])
	for _, want := range []string{platform.RescueName + " 的結果", "不要傳出去", "看板：沒有回應", "重開服務：沒成功", "EverGenie 沒在跑"} {
		if !strings.Contains(string(local), want) {
			t.Errorf("結果檔少了 %q：\n%s", want, local)
		}
	}
	if strings.Contains(string(local), "問題回報") || strings.Contains(string(local)+string(data), "/Users/someone") {
		t.Fatalf("兩個檔不能混在一起，也不能有子行程印的字：\n%s", local)
	}
	// 遮過的那份產不出來：不開任何檔、不叫他傳檔。
	rec = rescueFakes(t, false, 1, nil)
	rec.reportFails = true
	if code, out := run(); code != 1 || len(rec.opened) != 0 || strings.Contains(out, "把這個檔傳給") || !strings.Contains(out, "請告訴提供程式的人") {
		t.Fatalf("沒有遮過的那份就不叫他傳檔：%d %v\n%s", code, rec.opened, out)
	}
	if code, _ := run("--nope"); code != 2 {
		t.Fatalf("不認得的參數：%d", code)
	}
}

// 還原卡住了（服務因為 .restore/ 的狀態拒絕啟動，HBK-7）：修復認得，白話說先不要動家裡的資料夾、不叫主人一直重點，
// 遮過的那份準備好（裡面寫明是還原卡住）；留在這台的結果檔也記一筆。看板起得來、或沒有卡住的還原，照平常的話說。
// Guards: HST-10
func TestRescueRecognisesStuckRestore(t *testing.T) {
	home := rescueHome(t)
	run := func() (int, string) {
		var out bytes.Buffer
		code := rescueCommand(context.Background(), cmdline.HostRequest{Home: home, Out: &out, Err: &out})
		return code, out.String()
	}
	// 換到一半的還原：pending.json 記著「正在換」，換下來的舊東西還在。
	restore := filepath.Join(home, ".restore")
	if err := os.MkdirAll(filepath.Join(restore, "tok", "old", "vault"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(restore, "tok", "old", "vault", "T-1.md"), []byte("原本的家"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(restore, "pending.json"), []byte(`{"token":"tok","phase":"swapping"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !homebackup.Stuck(home) {
		t.Fatal("換到一半的還原要認得出來")
	}
	rec := rescueFakes(t, false, 1, nil)
	code, out := run()
	if code != 1 || !strings.Contains(out, "還原卡住了") || !strings.Contains(out, "先不要動家裡的資料夾") || strings.Contains(out, "再點一次") {
		t.Fatalf("還原卡住要白話說、不叫他一直重點：%d\n%s", code, out)
	}
	if strings.Contains(out, "sudo") || strings.Contains(out, "指令") || strings.Contains(out, ".restore") {
		t.Fatalf("給主人看的字不提指令與內部資料夾：\n%s", out)
	}
	if len(rec.opened) != 1 || !strings.HasPrefix(rec.opened[0], "-t ") {
		t.Fatalf("遮過的那份要準備好並打開：%v", rec.opened)
	}
	shared, err := os.ReadFile(strings.TrimPrefix(rec.opened[0], "-t "))
	if err != nil || !strings.HasPrefix(string(shared), "問題回報：1.2.3\n") || !strings.Contains(string(shared), "還原卡住了") {
		t.Fatalf("遮過的那份要寫明是還原卡住：%v\n%s", err, shared)
	}
	locals, _ := filepath.Glob(filepath.Join(filepath.Dir(strings.TrimPrefix(rec.opened[0], "-t ")), "修復結果-*.txt"))
	if local, _ := os.ReadFile(locals[0]); len(locals) != 1 || !strings.Contains(string(local), "還原：卡住了") {
		t.Fatalf("留在這台的結果檔要記一筆：%v", locals)
	}
	if _, err := os.Stat(filepath.Join(restore, "tok", "old", "vault", "T-1.md")); err != nil {
		t.Fatal("修復不能動還原的暫存")
	}
	// 遮過的那份產不出來：一樣講明，請他轉告。
	rec = rescueFakes(t, false, 1, nil)
	rec.reportFails = true
	if _, out := run(); len(rec.opened) != 0 || !strings.Contains(out, "還原卡住了") || !strings.Contains(out, "請告訴提供程式的人") {
		t.Fatalf("沒有遮過的那份也要講明：\n%s", out)
	}
	// 看板起得來＝服務自己換回去了：不說卡住。
	rescueFakes(t, true, 0, nil)
	if code, out := run(); code != 0 || strings.Contains(out, "還原卡住") {
		t.Fatalf("看板起來了不說卡住：%d\n%s", code, out)
	}
	// 還原只是準備好、還沒開始換（或根本沒有還原）：不算卡住。
	if err := os.WriteFile(filepath.Join(restore, "pending.json"), []byte(`{"token":"tok","phase":"ready"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rescueFakes(t, false, 1, nil)
	if _, out := run(); strings.Contains(out, "還原卡住") || !strings.Contains(out, "還是開不起來") {
		t.Fatalf("沒卡住照平常的話說：\n%s", out)
	}
	// 紀錄讀不懂、而且有東西換下來了：也是卡住。
	if err := os.WriteFile(filepath.Join(restore, "pending.json"), []byte(`{壞掉`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !homebackup.Stuck(home) {
		t.Fatal("紀錄讀不懂又有換下來的東西要算卡住")
	}
}

// --ask（HST-12）：看板好了才問；主人選了數字才跑引擎給的登入指令，直接按 Enter 就什麼都不跑。
func TestRescueAskLogin(t *testing.T) {
	home := rescueHome(t)
	rescueFakes(t, true, 0, nil)
	var ran [][]string
	old := rescueLogin
	rescueLogin = func(_ context.Context, argv []string, _ io.Reader, _ io.Writer) error {
		ran = append(ran, argv)
		return nil
	}
	t.Cleanup(func() { rescueLogin = old })
	engines := func() ([]sdk.PreflightCheck, bool) {
		return []sdk.PreflightCheck{
			{ID: "tmux", Ok: true},
			{ID: "login:main", Ok: true, Login: []string{"/bin/claude"}},
			{ID: "login:second", Login: []string{"CLAUDE_CONFIG_DIR=/x", "/bin/claude"}, LoginHint: []string{"1. 打 /login 再按 Enter。"}},
		}, true
	}
	run := func(input string, args ...string) string {
		var out bytes.Buffer
		rescueCommand(context.Background(), cmdline.HostRequest{Home: home, Args: args, In: strings.NewReader(input), Out: &out, Err: &out, Engines: engines})
		return out.String()
	}
	if out := run("2\n\n"); strings.Contains(out, "重新登入 AI 帳號") || len(ran) != 0 {
		t.Fatalf("沒有 --ask 不問：\n%s", out)
	}
	if out := run("\n", "--ask"); !strings.Contains(out, "1）重新登入 AI 帳號 main（看起來有登入）") || !strings.Contains(out, "2）重新登入 AI 帳號 second（沒有登入）") || len(ran) != 0 {
		t.Fatalf("直接按 Enter 不登入：%v\n%s", ran, out)
	}
	out := run("2\n\n", "--ask")
	if len(ran) != 1 || strings.Join(ran[0], " ") != "CLAUDE_CONFIG_DIR=/x /bin/claude" || !strings.Contains(out, "/login") || !strings.Contains(out, "回看板再說一次話") {
		t.Fatalf("選 2 要跑第二個帳號的登入：%v\n%s", ran, out)
	}
}

// 登入指令：前面的 KEY=值 當環境變數，後面才是程式。
func TestRescueLoginRunsWithEnv(t *testing.T) {
	var out bytes.Buffer
	if err := rescueLogin(context.Background(), []string{"EVERGENIE_T_RESCUE=yes", "/bin/sh", "-c", "printf %s \"$EVERGENIE_T_RESCUE\""}, nil, &out); err != nil || out.String() != "yes" {
		t.Fatalf("登入指令：%v %q", err, out.String())
	}
}

// rescue install（HST-11）：在指定的資料夾放「<產品名> 修復.command」，可執行、點了就是跑 rescue --ask；
// 同名的檔是別的家的就另取名字，不蓋掉；同一個家重裝就覆寫。
func TestRescueInstall(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Applications")
	home := filepath.Join(t.TempDir(), "my home's")
	log := filepath.Join(t.TempDir(), "ran.txt")
	exe := filepath.Join(t.TempDir(), "evergenie")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nprintf '%s|%s' \"$EVERGENIE_HOME\" \"$*\" >"+shQuote(log)+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	install := func(h string) string {
		var out bytes.Buffer
		if code := rescueInstall(context.Background(), cmdline.HostRequest{Args: []string{"--home", h, "--exe", exe, "--dir", dir}, Out: &out, Err: &out}); code != 0 {
			t.Fatalf("rescue install：%d %s", code, out.String())
		}
		return strings.TrimSpace(out.String())
	}
	path := install(home)
	if path != filepath.Join(dir, "evergenie 修復.command") || filepath.Base(path) != platform.RescueFile {
		t.Fatalf("檔名：%s", path)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("要可以執行：%v %v", err, st)
	}
	if err := exec.Command("/bin/bash", path).Run(); err != nil {
		t.Fatalf("點兩下（跑它）：%v", err)
	}
	if got, _ := os.ReadFile(log); string(got) != home+"|rescue --ask" {
		t.Fatalf("要用這個家跑 rescue --ask：%q", got)
	}
	if again := install(home); again != path {
		t.Fatalf("同一個家重裝要覆寫同一個檔：%s", again)
	}
	other := filepath.Join(t.TempDir(), "second-home")
	if p := install(other); p != filepath.Join(dir, "evergenie 修復（second-home）.command") {
		t.Fatalf("別的家要另取名字：%s", p)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), shQuote(home)) {
		t.Fatalf("第一個家的檔不能被蓋掉：%s", data)
	}
	// 資料夾同名的第三個家：再加編號，不蓋掉第二個家的。
	third := filepath.Join(t.TempDir(), "second-home")
	if p := install(third); p != filepath.Join(dir, "evergenie 修復（second-home 2）.command") {
		t.Fatalf("資料夾同名的家要加編號：%s", p)
	}
	if again := install(other); again != filepath.Join(dir, "evergenie 修復（second-home）.command") {
		t.Fatalf("第二個家重裝要覆寫自己那個：%s", again)
	}
	// 程式不見了：白話說，不是一串錯誤
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/bin/bash", path).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "請重新安裝一次") {
		t.Fatalf("程式不見了：%v %s", err, out)
	}
}

// 結果檔只留 30 天（只認自己的檔名）。
func TestRescueResultPrunesOld(t *testing.T) {
	rescueFakes(t, true, 0, nil)
	dir := rescueResult()
	old, keep := filepath.Join(dir, "修復結果-20200101-000000.txt"), filepath.Join(dir, "別的.txt")
	for _, f := range []string{old, keep} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		past := time.Now().Add(-40 * 24 * time.Hour)
		if err := os.Chtimes(f, past, past); err != nil {
			t.Fatal(err)
		}
	}
	if p, share := rescueWriteResult(context.Background(), "/h", true, false, "", "ok", time.Now()); p == "" || share == "" {
		t.Fatal("要寫得出結果檔")
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("40 天前的結果要刪：%v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("別的檔不能動：%v", err)
	}
}

// 各處寫給主人看的「修復」名字都跟程式一致（改名時一起改）：離線頁、看板文字、引擎的登入說明、說明文件。
func TestRescueNameEverywhere(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, f := range []string{
		"web/apps/board/public/sw.js",
		"web/apps/board/src/i18n/zh-TW.ts",
		"packages/official/engine-claude/internal/claude/usage.go",
		"ai_docs/guides/billing-and-accounts.md",
		"README.md",
	} {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "「"+platform.RescueName+"」") {
			t.Errorf("%s 沒有寫到「%s」", f, platform.RescueName)
		}
	}
}

// 服務裝好或重開後補放修復入口（HST-11）：升級上來的家當初沒有；只有真的裝成服務（plist 在）才放。
func TestRescueEnsureOnlyForInstalledService(t *testing.T) {
	agents, apps, home := t.TempDir(), filepath.Join(t.TempDir(), "Applications"), t.TempDir()
	t.Setenv("EVERGENIE_RESCUE_DIR", apps)
	svc := host.Service{Dir: agents, Spec: host.ServiceSpec{Label: "tw.test.rescue", Home: home}}
	rescueEnsure(svc, home, "/bin/evergenie")
	if _, err := os.Stat(filepath.Join(apps, platform.RescueFile)); !os.IsNotExist(err) {
		t.Fatalf("沒裝成服務的家不放：%v", err)
	}
	if err := os.WriteFile(svc.PlistPath(), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	withRoot := svc
	withRoot.Root = t.TempDir()
	rescueEnsure(withRoot, home, "/bin/evergenie")
	if _, err := os.Stat(filepath.Join(apps, platform.RescueFile)); !os.IsNotExist(err) {
		t.Fatalf("--root 的試裝不放：%v", err)
	}
	rescueEnsure(svc, home, "/bin/evergenie")
	data, err := os.ReadFile(filepath.Join(apps, platform.RescueFile))
	if err != nil || !strings.Contains(string(data), shQuote(home)) {
		t.Fatalf("裝成服務的家要放：%v %s", err, data)
	}
}

// 登入指令前面的「名稱=值」才是環境變數；路徑裡有等號的執行檔不算。
func TestRescueEnvWord(t *testing.T) {
	for word, want := range map[string]bool{"CLAUDE_CONFIG_DIR=/x": true, "A1_b=": true, "/opt/a=b/bin": false, "a=b/bin tool": true, "1A=x": false, "./x=y": false} {
		if got := rescueEnvWord.MatchString(word); got != want {
			t.Errorf("%q → %v", word, got)
		}
	}
}
