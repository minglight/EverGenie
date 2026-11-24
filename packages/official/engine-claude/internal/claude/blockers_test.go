package claude

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 對話中途擋住輸入框的平台選單（ECL-66）、連續貼不進去要讓主人知道（ECL-67）、引擎重開與舊視窗（ECL-68）。
// 畫面樣本 auto-mode-setup-*.txt 是照 2026-11-14 事件回報的選單文字合成的，不是真機擷取。

const idleBox = "⏺ 好。\n\n" + fakeRule + "\n❯ \n" + fakeRule + "\n  ⏵⏵ auto mode on\n"

// did 列出引擎對畫面動過的手（貼上、按鍵、關、開），照順序。
func did(f *fakeTmux) []string {
	var out []string
	for _, c := range f.snapshot() {
		switch c.Op {
		case "paste":
			out = append(out, "paste")
		case "key":
			out = append(out, "key:"+c.Arg)
		case "kill", "start":
			out = append(out, c.Op)
		}
	}
	return out
}

func deliverTo(t *testing.T, v *engEnv, text string) *sdk.EngineDeliverResponse {
	t.Helper()
	r, err := v.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "code", Text: text, EnvelopeID: "E-1"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestFindBlockerOnlyMatchesListedMenus(t *testing.T) {
	if at := findBlocker(readScreen(t, "auto-mode-setup-menu.txt")); !at.atBottom() || !at.blocking() {
		t.Fatalf("選單在畫面最底下要認得：%+v", at)
	}
	if at := findBlocker(readScreen(t, "auto-mode-setup-menu-over-box.txt")); at.atBottom() || !at.blocking() {
		t.Fatalf("游標在別的選項、緊貼在輸入框上面：貼上前不算、貼不進去時算：%+v", at)
	}
	// 對話裡引用的字：找得到，但後面還有對話的其他行，不算開著。
	if at := findBlocker(readScreen(t, "auto-mode-setup-quoted.txt")); at == nil || at.atBottom() || at.blocking() {
		t.Fatalf("引用的字不算開著的選單：%+v", at)
	}
	menu := "  Teach auto mode about your environment?\n  ❯ 1. Yes\n    2. Not now\n    3. Don't show again\n  Enter to confirm · Esc to cancel\n"
	// 畫在輸入框的兩條橫線中間、或看不到輸入框只剩一行狀態列：算開著；沒有輸入框、後面還有一段對話：不算。
	if !findBlocker("⏺ 好。\n" + fakeRule + "\n" + menu + fakeRule + "\n  ⏵⏵ auto mode on\n").blocking() {
		t.Fatal("畫在輸入框裡面要算")
	}
	if !findBlocker("⏺ 好。\n\n" + menu + "\n  ⏵⏵ auto mode on\n").blocking() {
		t.Fatal("沒有輸入框、下面只有狀態列要算")
	}
	if findBlocker("⏺ 像這樣：\n" + menu + "\n  按 Esc 就關了。\n  之後貼得進去。\n\n✻ Worked for 3s\n").blocking() {
		t.Fatal("沒有輸入框、後面還有一段對話不能算")
	}
	for _, name := range []string{"permission-dialog.txt", "question-dialog.txt", "question-dialog-sidepane.txt", "trust-folder.txt", "remote-control-active.txt", "hook-ask-dialog.txt", "labeled-rule.txt"} {
		if at := findBlocker(readScreen(t, name)); at != nil {
			t.Errorf("%s 不是清單裡的選單：%s", name, at.b.label)
		}
	}
	// 選項對不上（別的選單剛好同標題）不算。
	if at := findBlocker("  Teach auto mode about your environment?\n  ❯ 1. Yes\n    2. No\n  Enter to confirm · Esc to cancel\n"); at != nil {
		t.Fatal("選項對不上不能算")
	}
}

// 選單取代了輸入框（畫面最底下就是它）：先按 Esc、看到它收掉，才貼上與送出。
func TestDeliverCancelsListedMenuBeforePasting(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", readScreen(t, "auto-mode-setup-menu.txt"))
	v.tmux.onKey = func(_, key string) (string, bool) {
		if key == "Escape" {
			return idleBox, true
		}
		return "", false
	}
	if r := deliverTo(t, v, "在嗎"); !r.Delivered {
		t.Fatalf("按掉選單後要送得進去：%+v", r)
	}
	if got := did(v.tmux); !reflect.DeepEqual(got, []string{"key:Escape", "paste", "key:Enter"}) {
		t.Fatalf("要先 Esc 再貼、再 Enter：%v", got)
	}
}

// 選單下面還畫著輸入框（貼不進去）：貼了沒進去、畫面底部是清單裡的選單，才按 Esc，然後再貼一次。
func TestDeliverCancelsListedMenuWhenPasteDidNotLand(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", readScreen(t, "auto-mode-setup-menu-over-box.txt"))
	v.tmux.dropPaste = true
	v.tmux.onKey = func(_, key string) (string, bool) {
		if key == "Escape" {
			v.tmux.dropPaste = false // onKey 在假 tmux 的鎖裡面叫
			return idleBox, true
		}
		return "", false
	}
	if r := deliverTo(t, v, "在嗎"); !r.Delivered {
		t.Fatalf("按掉選單後要送得進去：%+v", r)
	}
	if got := did(v.tmux); !reflect.DeepEqual(got, []string{"paste", "key:Escape", "paste", "key:Enter"}) {
		t.Fatalf("貼不進去才 Esc，再貼一次：%v", got)
	}
}

// 秘書在對話裡引用了選單的字（她們自己會討論這件事）、輸入框正常：不能按 Esc（她在忙的話 Esc 會中斷她）。
func TestDeliverDoesNotCancelMenuQuotedInConversation(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", readScreen(t, "auto-mode-setup-quoted.txt"))
	if r := deliverTo(t, v, "在嗎"); !r.Delivered {
		t.Fatalf("輸入框正常要照常送：%+v", r)
	}
	if got := did(v.tmux); !reflect.DeepEqual(got, []string{"paste", "key:Enter"}) {
		t.Fatalf("只是引用的字不能按 Esc：%v", got)
	}
}

// 引用的字＋這次貼上剛好沒進輸入框（還沒畫出來、她在忙）：一樣不能按 Esc，照舊回 paste 讓核心晚點再試。
func TestDeliverDoesNotCancelQuotedMenuEvenWhenPasteMisses(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", readScreen(t, "auto-mode-setup-quoted.txt"))
	v.tmux.dropPaste = true
	for i := 0; i < 3; i++ {
		if r := deliverTo(t, v, "在嗎"); r.Delivered || r.Reason != "paste" {
			t.Fatalf("要回 paste：%+v", r)
		}
	}
	if got := did(v.tmux); !reflect.DeepEqual(got, []string{"paste", "paste", "paste"}) {
		t.Fatalf("引用的字不能按任何鍵：%v", got)
	}
}

// 重查（同一則再交來）：輸入框是空的、對話區看得到這句、畫面上還有引用的選單字——照舊回「晚點再查」，不能重貼成兩則。
func TestRecheckKeepsNoRepasteRuleWithQuotedMenuOnScreen(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", readScreen(t, "auto-mode-setup-quoted.txt"))
	v.tmux.eatEnter = true // 第一次：貼進去了、Enter 沒確認到
	if r := deliverTo(t, v, "在嗎"); r.Delivered || r.Reason != reasonNotSubmitted {
		t.Fatalf("第一次要回沒確認送出：%+v", r)
	}
	v.tmux.setScreen("code", "> 在嗎\n"+readScreen(t, "auto-mode-setup-quoted.txt")) // 其實送出了：框空了、對話區看得到
	if r := deliverTo(t, v, "在嗎"); r.Delivered || r.Reason != reasonNotSubmitted {
		t.Fatalf("重查要回晚點再查：%+v", r)
	}
	if got := did(v.tmux); !reflect.DeepEqual(got, []string{"paste", "key:Enter"}) {
		t.Fatalf("不能重貼、不能再按：%v", got)
	}
}

// 重查：看不到輸入框、選單開著，畫面上（對話區）看得到這句——不能補 Enter（會選到選單的第一項）；先取消選單。
func TestRecheckDoesNotPressEnterOnListedMenu(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", idleBox)
	v.tmux.eatEnter = true
	if r := deliverTo(t, v, "在嗎"); r.Delivered || r.Reason != reasonNotSubmitted {
		t.Fatalf("第一次要回沒確認送出：%+v", r)
	}
	v.tmux.setScreen("code", "> 在嗎\n\n⏺ 好。\n\n  Teach auto mode about your environment?\n  ❯ 1. Yes\n    2. Not now\n    3. Don't show again\n  Enter to confirm · Esc to cancel\n\n  ⏵⏵ auto mode on\n")
	v.tmux.dropPaste = true
	before := len(did(v.tmux))
	if r := deliverTo(t, v, "在嗎"); r.Delivered || r.Reason != "paste" {
		t.Fatalf("要回 paste：%+v", r)
	}
	if got := did(v.tmux)[before:]; !reflect.DeepEqual(got, []string{"paste", "key:Escape"}) {
		t.Fatalf("選單開著不能補 Enter，貼不進去才按取消：%v", got)
	}
}

// 不在清單裡的畫面：一個鍵都不按，回 paste 讓核心照舊退避。
func TestDeliverPressesNothingOnUnknownScreen(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", "⏺ 好。\n\n  Something new from the platform?\n  ❯ 1. Yes\n    2. No\n  Enter to confirm · Esc to cancel\n\n"+fakeRule+"\n❯ \n"+fakeRule+"\n")
	v.tmux.dropPaste = true
	if r := deliverTo(t, v, "在嗎"); r.Delivered || r.Reason != "paste" {
		t.Fatalf("要回 paste：%+v", r)
	}
	if got := did(v.tmux); !reflect.DeepEqual(got, []string{"paste"}) {
		t.Fatalf("不認得的畫面不能按任何鍵：%v", got)
	}
}

// 按了 Esc 選單還在：不貼、更不能按 Enter（Enter 在選單上是選第一項）。
func TestDeliverNeverPressesEnterWhileListedMenuStays(t *testing.T) {
	for _, screen := range []string{"auto-mode-setup-menu.txt", "auto-mode-setup-menu-over-box.txt"} {
		v := newEngEnv(t)
		v.tmux.setAlive("code", true)
		v.tmux.setScreen("code", readScreen(t, screen))
		v.tmux.dropPaste = true
		for i := 0; i < 3; i++ { // 同一則再交來（重查那條路）也一樣
			if r := deliverTo(t, v, "在嗎"); r.Delivered || r.Reason != "paste" {
				t.Fatalf("%s：要回 paste：%+v", screen, r)
			}
		}
		escapes := 0
		for _, a := range did(v.tmux) {
			if a == "key:Enter" {
				t.Fatalf("%s：選單還在不能按 Enter：%v", screen, did(v.tmux))
			}
			if a == "key:Escape" {
				escapes++
			}
		}
		if escapes != 1 {
			t.Fatalf("%s：取消按過一次還在就不再按（交給提示），按了 %d 次：%v", screen, escapes, did(v.tmux))
		}
	}
}

func stuckPrompts(t *testing.T, v *engEnv) []sdk.BootPrompt {
	t.Helper()
	list, err := v.e.ListBootPrompts(context.Background(), &sdk.EngineListBootPromptsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return list.Prompts
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// 連續貼不進去超過 stuckAfter：登記一個提示（哪位、被什麼擋住），送進去了自動撤掉。
func TestStuckDeliveryIsSurfacedAndClearsOnSuccess(t *testing.T) {
	v := newEngEnv(t)
	var logged []string
	v.e.d.Log = func(s string) { logged = append(logged, s) }
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", "⏺ 好。\n\n  Something new from the platform?\n  ❯ 1. Yes\n    2. No\n\n"+fakeRule+"\n❯ \n"+fakeRule+"\n")
	v.tmux.dropPaste = true
	deliverTo(t, v, "在嗎")
	v.clock.advance(stuckAfter - 30*time.Second)
	deliverTo(t, v, "在嗎")
	if got := stuckPrompts(t, v); len(got) != 0 {
		t.Fatalf("還沒到 %s 不提示：%+v", stuckAfter, got)
	}
	v.clock.advance(30 * time.Second)
	deliverTo(t, v, "在嗎")
	got := stuckPrompts(t, v)
	if len(got) != 1 || got[0].Session != "code" || got[0].ID != "code"+stuckSuffix || !strings.Contains(got[0].Title, "貼不進秘書的對話") ||
		!strings.Contains(got[0].Title, "Something new from the platform?") || !reflect.DeepEqual(got[0].Choices, []string{bootChoiceEscape}) {
		t.Fatalf("要提示哪位秘書被什麼擋住：%+v", got)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "code") {
		t.Fatalf("開始提示時記一行（之後不重複）：%v", logged)
	}
	deliverTo(t, v, "在嗎")
	if len(logged) != 1 {
		t.Fatalf("還卡著不重複記：%v", logged)
	}
	// 恢復：貼得進去了，提示自動消掉。
	v.tmux.dropPaste = false
	if r := deliverTo(t, v, "在嗎"); !r.Delivered {
		t.Fatalf("要送得進去：%+v", r)
	}
	if got := stuckPrompts(t, v); len(got) != 0 {
		t.Fatalf("送進去了要撤掉：%+v", got)
	}
}

// 清單裡的選單按了取消還在：提示寫出是哪個選單。
func TestStuckPromptNamesListedMenu(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", readScreen(t, "auto-mode-setup-menu.txt"))
	v.tmux.dropPaste = true
	deliverTo(t, v, "在嗎")
	v.clock.advance(stuckAfter)
	deliverTo(t, v, "在嗎")
	got := stuckPrompts(t, v)
	if len(got) != 1 || !strings.Contains(got[0].Title, "Teach auto mode about your environment?") {
		t.Fatalf("要說出是哪個選單：%+v", got)
	}
}

// 主人在提示上選按 Esc：只按一個 Esc；提示撤掉、重新算時間。那句話不再重試（被取消了）一陣子後也要自己撤掉。
func TestStuckPromptEscapeChoiceAndStaleCleanup(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", "  Something new?\n\n"+fakeRule+"\n❯ \n"+fakeRule+"\n")
	v.tmux.dropPaste = true
	stick := func() sdk.BootPrompt {
		deliverTo(t, v, "在嗎")
		v.clock.advance(stuckAfter)
		deliverTo(t, v, "在嗎")
		got := stuckPrompts(t, v)
		if len(got) != 1 {
			t.Fatalf("要有提示：%+v", got)
		}
		return got[0]
	}
	p := stick()
	if _, err := v.e.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: p.ID, Choice: bootChoiceTrust}); !isCode(err, sdk.ErrorCodeInvalid) {
		t.Fatalf("別的提示的選項不能用：%v", err)
	}
	before := len(v.tmux.callsOf("key"))
	if _, err := v.e.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: p.ID, Choice: bootChoiceEscape}); err != nil {
		t.Fatal(err)
	}
	keys := v.tmux.callsOf("key")[before:]
	if len(keys) != 1 || keys[0].Arg != "Escape" || keys[0].Name != "code" {
		t.Fatalf("只按一個 Esc：%+v", keys)
	}
	if got := stuckPrompts(t, v); len(got) != 0 {
		t.Fatalf("按完撤掉：%+v", got)
	}
	stick()
	v.clock.advance(stuckStale)
	if got := stuckPrompts(t, v); len(got) != 0 {
		t.Fatalf("沒有人再試了要自己撤掉：%+v", got)
	}
}

// 信任資料夾的提示照舊（DEV-87），跟貼不進去的提示各自獨立。
func TestTrustPromptUnchangedNextToStuckPrompt(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setScreen("code", readScreen(t, "trust-folder.txt"))
	v.start("code", nil)
	got := stuckPrompts(t, v)
	if len(got) != 1 || got[0].ID != "code#trust" || !reflect.DeepEqual(got[0].Choices, []string{bootChoiceTrust, bootChoiceExit}) {
		t.Fatalf("信任提示不變：%+v", got)
	}
	if r := deliverTo(t, v, "在嗎"); r.Reason != "dialog" {
		t.Fatalf("信任框開著照舊回 dialog：%+v", r)
	}
	if got := did(v.tmux); !reflect.DeepEqual(got, []string{"start"}) {
		t.Fatalf("信任框開著不貼不按：%v", got)
	}
}

func writeCoreSessionOn(t *testing.T, home, name, state, socket string) {
	t.Helper()
	dir := filepath.Join(home, "runtime", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\nstate: " + state + "\nattach: tmux -L " + socket + " attach -t '=" + name + "'\n---\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 引擎剛重開、還沒做過存活檢查就有話要貼（2026-11-14：卡住的那句每 12 秒重試，部署重開後馬上又交來）：
// 要先從核心的紀錄補回 socket 再查她在不在，不然查到預設的 socket，會把活著的她回成「不在了」。
func TestFirstCallsAfterRestartRecallSocketBeforeLooking(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func(v *engEnv){
		"Deliver": func(v *engEnv) {
			_, _ = v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "在嗎", EnvelopeID: "E-1"})
		},
		"Command": func(v *engEnv) {
			_, _ = v.e.Command(ctx, &sdk.EngineCommandRequest{Session: "code", Command: sdk.SessionCommandInterrupt})
		},
		"AnswerApproval": func(v *engEnv) {
			_, _ = v.e.AnswerApproval(ctx, &sdk.EngineAnswerApprovalRequest{Session: "code", Pending: &sdk.Pending{Kind: sdk.PendingKindPermission}, Decision: &sdk.Decision{}})
		},
		"Stop": func(v *engEnv) { _, _ = v.e.Stop(ctx, &sdk.EngineStopRequest{Name: "code"}) },
	}
	for name, call := range calls {
		v := newEngEnv(t)
		writeCoreSessionOn(t, v.home, "code", "idle", "hg-ws")
		v.tmux.setAlive("code", true)
		call(v)
		remembered, touched := -1, -1
		for i, c := range v.tmux.snapshot() {
			if c.Name != "code" {
				continue
			}
			if c.Op == "remember" && c.Arg == "hg-ws" && remembered < 0 {
				remembered = i
			}
			if c.Op != "remember" && touched < 0 {
				touched = i
			}
		}
		if remembered < 0 || touched < 0 || remembered > touched {
			t.Errorf("%s：要先補回 socket hg-ws 才碰 tmux：%+v", name, v.tmux.snapshot())
		}
	}
}

// 核心要開、同名的舊視窗還在（核心記成停了）：收掉這個家開的那個舊視窗再照常開，不回「已經在執行」。
func TestStartReplacesLeftoverWindowOfThisHome(t *testing.T) {
	v := newEngEnv(t)
	var logged []string
	v.e.d.Log = func(s string) { logged = append(logged, s) }
	v.tmux.setAlive("code", true)
	r := v.req("code")
	r.EngineConfig["tmux_socket"] = "hg-ws"
	if _, err := v.e.Start(context.Background(), r); err != nil {
		t.Fatalf("要開得起來：%v", err)
	}
	v.e.Wait()
	if got := did(v.tmux); !reflect.DeepEqual(got, []string{"kill", "start"}) {
		t.Fatalf("先收掉舊的再開：%v", got)
	}
	// 收掉之後要再記一次 socket（Kill 會忘掉），之後的查詢才在同一個 socket。
	last := ""
	for _, c := range v.tmux.snapshot() {
		if c.Op == "kill" {
			last = ""
		}
		if c.Op == "remember" && c.Name == "code" {
			last = c.Arg
		}
		if c.Op == "start" && (last != "hg-ws" || c.Socket != "hg-ws") {
			t.Fatalf("收掉後、開之前要記回 hg-ws：%+v", v.tmux.snapshot())
		}
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "code") {
		t.Fatalf("收掉舊視窗要留一行紀錄：%v", logged)
	}
}

// 同名的視窗認不出是這個家開的（別的家用了同一個 socket）：不動它，照舊回衝突。
func TestStartLeavesWindowOfAnotherHomeAlone(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.foreign = map[string]bool{"code": true}
	if _, err := v.e.Start(context.Background(), v.req("code")); !isCode(err, sdk.ErrorCodeConflict) {
		t.Fatalf("要回衝突：%v", err)
	}
	if got := did(v.tmux); len(got) != 0 {
		t.Fatalf("不是這個家開的不能動：%v", got)
	}
}

// 真的 tmux 實作：只有啟動指令稿在這個家的資料夾裡的 session 才算自己開的；查的是記下的 socket、名字完全相同的那一個。
func TestExecTmuxLaunchedHereChecksScriptFolder(t *testing.T) {
	out, fail := "", false
	var got []string
	tm := &ExecTmux{ScriptDir: "/homes/a/state/engine-claude/launch", run: func(_ context.Context, _ string, args ...string) (string, error) {
		got = args
		if fail {
			return "can't find session: code", os.ErrExist
		}
		return out, nil
	}}
	tm.Remember("code", "hg-ws")
	out = "/homes/a/state/engine-claude/launch/launch-0123abcd.sh\n"
	if !tm.LaunchedHere("code") {
		t.Fatal("這個家的指令稿開的要算")
	}
	if strings.Join(got, " ") != "-L hg-ws display-message -p -t =code: #{pane_start_command}" {
		t.Fatalf("要查記下的 socket、名字完全相同的那一個：%v", got)
	}
	for _, other := range []string{"/homes/b/state/engine-claude/launch/launch-0123abcd.sh\n", "/homes/a/state/engine-claude/launch/sub/launch-1.sh\n", "claude --resume x\n", "\n"} {
		out = other
		if tm.LaunchedHere("code") {
			t.Fatalf("%q 不是這個家開的", other)
		}
	}
	out, fail = "/homes/a/state/engine-claude/launch/launch-0123abcd.sh\n", true
	if tm.LaunchedHere("code") {
		t.Fatal("查不到要回 false")
	}
	fail = false
	if (&ExecTmux{run: tm.run}).LaunchedHere("code") {
		t.Fatal("沒設指令稿資料夾分不出是誰的，要回 false")
	}
}
