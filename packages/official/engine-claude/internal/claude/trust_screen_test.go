package claude

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 只認畫面最底下的真信任框；對話區引用到那些字不算（秘書引用了關鍵字被誤認卡住，主人按了沒反應）。
func TestIsTrustScreenOnlyRealDialog(t *testing.T) {
	cases := []struct {
		file string
		want bool
	}{
		{"trust-folder.txt", true},
		{"trust-add-dir.txt", true},
		{"trust-quoted.txt", false},
		{"permission-dialog.txt", false},
		{"auto-mode-setup-menu.txt", false},
		{"auto-mode-setup-quoted.txt", false},
		{"remote-control-active.txt", false},
	}
	for _, c := range cases {
		if got := isTrustScreen(readScreen(t, c.file)); got != c.want {
			t.Errorf("%s：isTrustScreen=%v，要 %v", c.file, got, c.want)
		}
	}
}

// 引用的字不算確認框：送訊息不會被當成「有框開著」擋下來。
func TestQuotedTrustTextNotConfirmDialog(t *testing.T) {
	if isConfirmDialog(readScreen(t, "trust-quoted.txt")) {
		t.Fatal("引用的字不算確認框")
	}
}

// cursorOnYes：把信任框的游標從 No, exit 移到 Yes（主人在終端機按過 ↓ 的樣子）。
func cursorOnYes(screen string) string {
	screen = strings.Replace(screen, "❯ No, exit", "  No, exit", 1)
	screen = strings.Replace(screen, "  Yes, I trust", "❯ Yes, I trust", 1)
	return strings.Replace(screen, "  Yes, add it", "❯ Yes, add it", 1)
}

// 游標停在 Yes 也還是信任框。
func TestTrustScreenCursorOnYes(t *testing.T) {
	if onYes, ok := trustCursor(cursorOnYes(readScreen(t, "trust-folder.txt"))); !ok || !onYes {
		t.Fatalf("游標在 Yes：onYes=%v ok=%v", onYes, ok)
	}
}

// footer 下面多一行提示（按過一次 Ctrl-C）也認得。
func TestTrustScreenExtraLineBelowFooter(t *testing.T) {
	if !isTrustScreen(readScreen(t, "trust-add-dir.txt") + "\n Press Ctrl-C again to exit\n") {
		t.Fatal("footer 下多一行提示也要認得")
	}
}

// 回答時照游標現在的位置按鍵，不會按反。
func TestAnswerBootPromptFollowsCursor(t *testing.T) {
	cases := []struct {
		name, choice string
		onYes        bool
		want         []string
	}{
		{"預設位置選信任", bootChoiceTrust, false, []string{"Down", "Enter"}},
		{"預設位置選離開", bootChoiceExit, false, []string{"Enter"}},
		{"游標在 Yes 選信任", bootChoiceTrust, true, []string{"Enter"}},
		{"游標在 Yes 選離開", bootChoiceExit, true, []string{"Up", "Enter"}},
	}
	for _, c := range cases {
		v := newEngEnv(t)
		ctx := context.Background()
		v.tmux.setScreen("code", readScreen(t, "trust-folder.txt"))
		v.start("code", nil)
		v.e.Wait()
		if c.onYes {
			v.tmux.setScreen("code", cursorOnYes(readScreen(t, "trust-folder.txt")))
		}
		if _, err := v.e.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: "code#trust", Choice: c.choice}); err != nil {
			t.Fatalf("%s：%v", c.name, err)
		}
		if got := keysOf(v); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s：按了 %v，要 %v", c.name, got, c.want)
		}
	}
}

// 2.1.326 的「Adding a new directory」框跟信任框一樣登記成啟動對話框。
func TestAddDirDialogRegistered(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setScreen("code", readScreen(t, "trust-add-dir.txt"))
	v.start("code", nil)
	v.e.Wait()
	list, err := v.e.ListBootPrompts(context.Background(), &sdk.EngineListBootPromptsRequest{})
	if err != nil || len(list.Prompts) != 1 || list.Prompts[0].ID != "code#trust" {
		t.Fatalf("要登記一個啟動對話框：%+v %v", list, err)
	}
}

// 「Adding a new directory」框選信任按 ↓＋Enter（樣本實機按過，會進到對話）。
func TestAddDirDialogAnswerTrust(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setScreen("code", readScreen(t, "trust-add-dir.txt"))
	v.start("code", nil)
	v.e.Wait()
	if _, err := v.e.AnswerBootPrompt(context.Background(), &sdk.EngineAnswerBootPromptRequest{ID: "code#trust", Choice: bootChoiceTrust}); err != nil {
		t.Fatal(err)
	}
	if keys := keysOf(v); !reflect.DeepEqual(keys, []string{"Down", "Enter"}) {
		t.Fatalf("要按 ↓＋Enter：%v", keys)
	}
}

// 秘書的回覆裡引用了信任框的字：不登記，看板不會出現「秘書卡住了」。
func TestQuotedTrustTextNotBootPrompt(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	v.tmux.setScreen("code", readScreen(t, "trust-quoted.txt"))
	v.start("code", nil)
	v.e.Wait()
	v.e.CheckAlive(ctx)
	list, err := v.e.ListBootPrompts(ctx, &sdk.EngineListBootPromptsRequest{})
	if err != nil || len(list.Prompts) != 0 {
		t.Fatalf("引用的字不算對話框：%+v %v", list, err)
	}
}

// 框裡問的資料夾：標題下一行。
func TestTrustDir(t *testing.T) {
	if got := trustDir(readScreen(t, "trust-folder.txt")); got != "/tmp/hg-spike/run/m1" {
		t.Errorf("trust-folder：%q", got)
	}
	if got := trustDir(readScreen(t, "trust-add-dir.txt")); got != "/tmp/hg-spike/run/extra" {
		t.Errorf("trust-add-dir：%q", got)
	}
	if !dirListed("/a/b/c", []string{"/a/b"}) || dirListed("/a/bc", []string{"/a/b"}) || dirListed("", []string{"/"}) {
		t.Error("dirListed：底下的算、名字前綴相同的不算、空的不算")
	}
}

// 路徑被折行（剛好切在列過的資料夾結尾）或截斷：當認不出來，不會被誤當成列過的。
func TestTrustDirWrappedOrTruncatedIsUnknown(t *testing.T) {
	sample := readScreen(t, "trust-add-dir.txt")
	wrapped := strings.Replace(sample, " /tmp/hg-spike/run/extra\n", " /a/b\n cd/secret\n", 1)
	if got := trustDir(wrapped); got != "" {
		t.Errorf("折行：%q，要認不出來", got)
	}
	cut := strings.Replace(sample, "/tmp/hg-spike/run/extra", "/a/b…", 1)
	if got := trustDir(cut); got != "" {
		t.Errorf("截斷：%q，要認不出來", got)
	}
	long := strings.Replace(sample, "/tmp/hg-spike/run/extra", "/"+strings.Repeat("x", 200), 1)
	if got := trustDir(long); got != "" {
		t.Errorf("碰到框寬：%q，要認不出來", got)
	}
}

// addDirScreen：2.1.326 的「Adding a new directory」框，問的是 dir（11-17 實機問的是家目錄底下的 Workspace 資料夾）。
func addDirScreen(t *testing.T, dir string) string {
	return strings.Replace(readScreen(t, "trust-add-dir.txt"), "/tmp/hg-spike/run/extra", dir, 1)
}

// workspaceScreen：工作資料夾第一次啟動的信任框，問的是 dir。
func workspaceScreen(t *testing.T, dir string) string {
	return strings.Replace(readScreen(t, "trust-folder.txt"), "/tmp/hg-spike/run/m1", dir, 1)
}

func listedRepo(t *testing.T, v *engEnv, dir string) {
	t.Helper()
	if _, err := v.e.SetPermission(context.Background(), &sdk.EngineSetPermissionRequest{Account: "acct-a", Policy: &sdk.PermissionPolicy{Repos: []sdk.RepoPolicy{{Path: dir}}}}); err != nil {
		t.Fatal(err)
	}
}

func promptsOf(t *testing.T, v *engEnv) []sdk.BootPrompt {
	t.Helper()
	list, err := v.e.ListBootPrompts(context.Background(), &sdk.EngineListBootPromptsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return list.Prompts
}

// DEV-162：設定檔 additionalDirectories 列的資料夾，引擎替主人按 Yes，不登記給看板。
func TestAddDirListedInSettingsAutoTrusted(t *testing.T) {
	v := newEngEnv(t)
	repo := t.TempDir()
	listedRepo(t, v, repo)
	v.start("code", nil) // 開的時候還沒有框
	v.tmux.setScreen("code", addDirScreen(t, repo))
	v.e.scanBoot("code", true)
	if keys := keysOf(v); !reflect.DeepEqual(keys, []string{"Down", "Enter"}) {
		t.Fatalf("要替她按 ↓＋Enter：%v", keys)
	}
	v.tmux.setScreen("code", "") // 框收掉了
	v.e.scanBoot("code", true)
	if p := promptsOf(t, v); len(p) != 0 {
		t.Fatalf("自動按掉的不登記：%+v", p)
	}
}

// 家的資料夾的信任框也自動按；游標已在 Yes 只按 Enter。
func TestHomeTrustAutoAnswered(t *testing.T) {
	v := newEngEnv(t)
	v.start("code", nil)
	v.tmux.setScreen("code", cursorOnYes(workspaceScreen(t, v.home)))
	v.e.scanBoot("code", true)
	if keys := keysOf(v); !reflect.DeepEqual(keys, []string{"Enter"}) {
		t.Fatalf("游標在 Yes 只按 Enter：%v", keys)
	}
}

// 工作資料夾本身不算列過（chat --repo 可以指到任何地方）：不在設定裡就問主人。
func TestCwdNotListedAsksOwner(t *testing.T) {
	v := newEngEnv(t)
	cwd := t.TempDir()
	v.tmux.setScreen("code", workspaceScreen(t, cwd))
	v.start("code", func(r *sdk.EngineStartRequest) { r.Cwd = cwd })
	if keys := keysOf(v); len(keys) != 0 {
		t.Fatalf("工作資料夾不在設定裡不能替她按：%v", keys)
	}
	if p := promptsOf(t, v); len(p) != 1 {
		t.Fatalf("要登記給主人：%+v", p)
	}
}

// 列過的資料夾底下的符號連結指到外面：解開後不在設定裡，不按。
func TestSymlinkOutsideListedAsksOwner(t *testing.T) {
	v := newEngEnv(t)
	repo, secret := t.TempDir(), t.TempDir()
	link := filepath.Join(repo, "x")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	listedRepo(t, v, repo)
	v.start("code", nil)
	v.tmux.setScreen("code", addDirScreen(t, link))
	v.e.scanBoot("code", true)
	if keys := keysOf(v); len(keys) != 0 {
		t.Fatalf("連結指到外面不能替她按：%v", keys)
	}
}

// 沒列過的資料夾不按，看板明講卡在信任資料夾、寫出是哪個資料夾。
func TestAddDirNotListedAsksOwner(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setScreen("code", addDirScreen(t, "/Users/someone/Secret"))
	v.start("code", nil)
	if keys := keysOf(v); len(keys) != 0 {
		t.Fatalf("沒列過的不能替她按：%v", keys)
	}
	p := promptsOf(t, v)
	if len(p) != 1 || !strings.Contains(p[0].Title, "卡在信任資料夾") || !strings.Contains(p[0].Title, "/Users/someone/Secret") {
		t.Fatalf("要登記並寫出資料夾：%+v", p)
	}
}

// 主人看到的是 A，框已經換成問 B：不按，回「不在了」。
func TestAnswerBootPromptBoundToDir(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setScreen("code", addDirScreen(t, "/Users/someone/A"))
	v.start("code", nil)
	v.tmux.setScreen("code", addDirScreen(t, "/Users/someone/B"))
	if _, err := v.e.AnswerBootPrompt(context.Background(), &sdk.EngineAnswerBootPromptRequest{ID: "code#trust", Choice: bootChoiceTrust}); err == nil {
		t.Fatal("問的資料夾換了要拒絕")
	}
	if keys := keysOf(v); len(keys) != 0 {
		t.Fatalf("不能按：%v", keys)
	}
}

// 替她按過了框還在（按鍵沒生效）：只按一次，之後交給主人。
func TestAutoTrustFailedFallsBackToOwner(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setScreen("code", workspaceScreen(t, v.home))
	v.start("code", nil) // watchBoot 一直看到同一個框
	if keys := keysOf(v); !reflect.DeepEqual(keys, []string{"Down", "Enter"}) {
		t.Fatalf("只替她按一次：%v", keys)
	}
	if p := promptsOf(t, v); len(p) != 1 || !strings.Contains(p[0].Title, "替她按過") {
		t.Fatalf("按了沒用要交給主人：%+v", p)
	}
	// 重開後重新判斷，不沿用上一次「按過」的紀錄。
	v.tmux.setScreen("code", "")
	v.e.scanBoot("code", true)
	v.e.forget("code", nil)
	v.tmux.setScreen("code", workspaceScreen(t, v.home))
	v.start("code", nil)
	if keys := keysOf(v); len(keys) != 4 {
		t.Fatalf("重開後要再替她按一次：%v", keys)
	}
}

// 接手的既有 session（不知道啟動參數）：只認家的資料夾，其他交給主人；列出（ListBootPrompts）只登記不按鍵。
func TestAdoptedSessionOnlyHomeAutoTrusted(t *testing.T) {
	v := newEngEnv(t)
	vault := filepath.Join(v.home, "vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	v.e.mu.Lock()
	v.e.orphans = map[string]bool{"old": true, "old2": true}
	v.e.mu.Unlock()
	v.tmux.setScreen("old", addDirScreen(t, vault))
	v.e.scanBoot("old", false)
	if keys := keysOf(v); len(keys) != 0 {
		t.Fatalf("列出時不按鍵：%v", keys)
	}
	v.e.scanBoot("old", true)
	if keys := keysOf(v); !reflect.DeepEqual(keys, []string{"Down", "Enter"}) {
		t.Fatalf("家底下的要替她按：%v", keys)
	}
	v.tmux.setScreen("old2", addDirScreen(t, t.TempDir()))
	v.e.scanBoot("old2", true)
	if keys := keysOf(v); len(keys) != 2 {
		t.Fatalf("家以外的不能按：%v", keys)
	}
}

// 平台改了信任框的寫法（這裡是選項帶編號），嚴格的格式對不上：不能當成沒事往裡面貼字，也絕不替任何人按鍵——
// 只登記給主人，看板明講卡在信任資料夾、請他到那台 Mac 看畫面。
func TestTrustDialogInUnknownFormatOnlyTellsOwner(t *testing.T) {
	numbered := strings.NewReplacer("❯ No, exit", "❯ 1. No, exit", "Yes, I trust this folder", "2. Yes, I trust this folder").Replace(readScreen(t, "trust-folder.txt"))
	if !strings.Contains(numbered, "1. No, exit") || !strings.Contains(numbered, "2. Yes, I trust this folder") {
		t.Fatalf("樣本沒換到編號：%s", numbered)
	}
	if isTrustScreen(numbered) {
		t.Fatal("帶編號的對不上嚴格格式（對得上就不用這條備援）")
	}
	if !looseTrustScreen(numbered) || !isConfirmDialog(numbered) {
		t.Fatal("像信任框、沒有輸入框：要當成有框開著（不貼字）")
	}
	if _, ok := parseConfirmDialog(numbered); ok {
		t.Fatal("不是權限確認框，不能照編號去按")
	}
	// 對話區提到信任框的字、底下開著別種框（權限確認、平台選單、選擇題）：是那個框，不是信任框。
	for _, name := range []string{"permission-dialog.txt", "hook-ask-dialog.txt", "auto-mode-setup-menu.txt", "question-dialog-sidepane.txt"} {
		mixed := "信任框的選項是「No, exit」和「Yes, I trust this folder」\nNo, exit\n" + readScreen(t, name)
		if looseTrustScreen(mixed) {
			t.Fatalf("%s：別種框開著不能當成信任框", name)
		}
	}
	if quoted := readScreen(t, "remote-control-active.txt") + "\nNo, exit / Yes, I trust this folder\n"; looseTrustScreen(quoted) {
		t.Fatal("畫面上有輸入框的是對話區引用，不算")
	}

	v := newEngEnv(t)
	v.tmux.setScreen("code", numbered)
	v.start("code", nil)
	if keys := keysOf(v); len(keys) != 0 {
		t.Fatalf("格式對不上絕不自動按：%v", keys)
	}
	p := promptsOf(t, v)
	if len(p) != 1 || !strings.Contains(p[0].Title, "卡在信任資料夾") || !strings.Contains(p[0].Title, "到那台 Mac") {
		t.Fatalf("要登記給主人並請他到那台 Mac 看：%+v", p)
	}
	if _, err := v.e.AnswerBootPrompt(context.Background(), &sdk.EngineAnswerBootPromptRequest{ID: p[0].ID, Choice: bootChoiceTrust}); err == nil {
		t.Fatal("主人在看板按也不能替他按鍵（不知道游標在哪）")
	}
	if keys := keysOf(v); len(keys) != 0 {
		t.Fatalf("不能按：%v", keys)
	}
	if r, err := v.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "code", Text: "在嗎", EnvelopeID: "E-1"}); err != nil || r.Delivered || r.Reason != "dialog" {
		t.Fatalf("框開著不能往裡面貼字：%+v %v", r, err)
	}
	v.tmux.setScreen("code", "")
	v.e.scanBoot("code", true)
	if p := promptsOf(t, v); len(p) != 0 {
		t.Fatalf("框不在了要撤掉：%+v", p)
	}
}
