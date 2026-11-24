package claude

import (
	"context"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 逾時後留在畫面上的確認，主人從看板回答時由引擎替他按（ECL-37、ENG-14、DEV-46 S3）。
// 按鍵順序用 testdata/spike/screens 的真實畫面：permission-dialog（1 Yes／2 Yes, and always…／3 switch to auto／4 No）、
// hook-ask-dialog（1 Yes／2 No）。

func keysOf(v *engEnv) []string {
	var out []string
	for _, c := range v.tmux.callsOf("key") {
		out = append(out, c.Arg)
	}
	return out
}

func permission() *sdk.Pending {
	return &sdk.Pending{ID: "p1", Kind: sdk.PendingKindPermission, Tool: "Bash", Summary: "touch DENY-ME"}
}

// Guards: REQ-377
func TestAnswerApprovalPressesSampleKeys(t *testing.T) {
	cases := []struct {
		screen string
		allow  bool
		want   string
	}{
		{"permission-dialog.txt", true, "1"},  // 允許只按剛好是 Yes 的那一項，不按「永遠允許」或「切換模式」
		{"permission-dialog.txt", false, "4"}, // 拒絕＝No
		{"hook-ask-dialog.txt", true, "1"},
		{"hook-ask-dialog.txt", false, "2"},
	}
	for _, c := range cases {
		v := newEngEnv(t)
		v.tmux.setAlive("code", true)
		v.tmux.setScreen("code", readScreen(t, c.screen))
		// 按數字就選定（spike 2）：確認框收掉。
		v.tmux.onKey = func(string, string) (string, bool) { return readScreen(t, "remote-control-active.txt"), true }
		p := permission()
		if c.screen == "hook-ask-dialog.txt" {
			p.Summary = "touch a-ask.txt && echo \"exit=$?\""
		}
		r, err := v.e.AnswerApproval(context.Background(), &sdk.EngineAnswerApprovalRequest{Session: "code", Pending: p, Decision: &sdk.Decision{Allow: c.allow, Message: "不要"}})
		if err != nil || !r.Answered {
			t.Fatalf("%s allow=%v：要答成功：%+v %v", c.screen, c.allow, r, err)
		}
		if got := keysOf(v); strings.Join(got, ",") != c.want {
			t.Fatalf("%s allow=%v：按鍵 %v，要 %s（確認框收掉就不補 Enter）", c.screen, c.allow, got, c.want)
		}
		if len(v.tmux.callsOf("paste")) != 0 {
			t.Fatal("代答不貼任何文字")
		}
	}
}

// Guards: REQ-377
func TestAnswerApprovalEnterOnlyWhenSameDialogStays(t *testing.T) {
	// 數字只移動了游標（同一個確認框、游標停在那一項）→ 補一個 Enter。
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	screen := readScreen(t, "permission-dialog.txt")
	v.tmux.setScreen("code", screen)
	v.tmux.onKey = func(_, key string) (string, bool) {
		if key == "4" {
			moved := strings.Replace(screen, dialogCursor+" 1. Yes", "  1. Yes", 1)
			return strings.Replace(moved, "   4. No", " "+dialogCursor+" 4. No", 1), true
		}
		return readScreen(t, "remote-control-active.txt"), true
	}
	r, err := v.e.AnswerApproval(context.Background(), &sdk.EngineAnswerApprovalRequest{Session: "code", Pending: permission(), Decision: &sdk.Decision{}})
	if err != nil || !r.Answered || strings.Join(keysOf(v), ",") != "4,Enter" {
		t.Fatalf("游標停在 No 還開著要補 Enter：%+v %v %v", r, err, keysOf(v))
	}
	// 按完換成另一個確認框（內容不同）→ 不補 Enter（Enter 會變成對新確認框按「Yes」）。
	v = newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", screen)
	v.tmux.onKey = func(string, string) (string, bool) {
		return strings.Replace(screen, "touch DENY-ME", "rm -rf build", -1), true
	}
	r, err = v.e.AnswerApproval(context.Background(), &sdk.EngineAnswerApprovalRequest{Session: "code", Pending: permission(), Decision: &sdk.Decision{Allow: true}})
	if err != nil || !r.Answered || strings.Join(keysOf(v), ",") != "1" {
		t.Fatalf("換了一個確認框不能補 Enter：%+v %v %v", r, err, keysOf(v))
	}
}

// Guards: REQ-377
func TestAnswerApprovalRefusesWithoutDialog(t *testing.T) {
	for _, c := range []struct {
		name   string
		screen string
		alive  bool
		kind   sdk.PendingKind
		want   string
	}{
		{"畫面上沒有確認框（有人在別處答了）", "remote-control-active.txt", true, sdk.PendingKindPermission, sdk.AnswerNone},
		{"啟動時的信任資料夾不是權限確認", "trust-folder.txt", true, sdk.PendingKindPermission, sdk.AnswerNone},
		{"畫面上是權限確認框、不是選擇題：不代答", "permission-dialog.txt", true, sdk.PendingKindQuestion, sdk.AnswerUnsupported},
		{"session 不在", "permission-dialog.txt", false, sdk.PendingKindPermission, "gone"},
	} {
		v := newEngEnv(t)
		v.tmux.setAlive("code", c.alive)
		v.tmux.setScreen("code", readScreen(t, c.screen))
		p := permission()
		p.Kind = c.kind
		r, err := v.e.AnswerApproval(context.Background(), &sdk.EngineAnswerApprovalRequest{Session: "code", Pending: p, Decision: &sdk.Decision{Allow: true}})
		if err != nil || r.Answered || r.Reason != c.want {
			t.Fatalf("%s：要回 %s：%+v %v", c.name, c.want, r, err)
		}
		if len(v.tmux.callsOf("key")) != 0 {
			t.Fatalf("%s：不能按任何鍵", c.name)
		}
	}
	v := newEngEnv(t)
	if _, err := v.e.AnswerApproval(context.Background(), &sdk.EngineAnswerApprovalRequest{Session: "code", Pending: permission()}); !isCode(err, sdk.ErrorCodeInvalid) {
		t.Fatalf("沒有決定要擋：%v", err)
	}
}

// Guards: REQ-377
func TestParseConfirmDialogSamples(t *testing.T) {
	d, ok := parseConfirmDialog(readScreen(t, "permission-dialog.txt"))
	if !ok || len(d.options) != 4 || d.cursor != 1 || d.options[3].label != "No" || d.options[1].label != "Yes, and always allow access to" {
		t.Fatalf("四選一：%+v", d)
	}
	if !strings.Contains(d.body, "touch DENY-ME") || strings.Contains(d.body, dialogCursor) || strings.Contains(d.body, "Use the Bash tool") {
		t.Fatalf("body 只有確認框那一段、去掉游標：%q", d.body)
	}
	d, ok = parseConfirmDialog(readScreen(t, "hook-ask-dialog.txt"))
	if !ok || len(d.options) != 2 || d.choice(true) != 1 || d.choice(false) != 2 {
		t.Fatalf("兩選一：%+v", d)
	}
}

// 畫面上的確認框不是看板那一題（摘要對不上）：不按任何鍵，回 unsupported（確認留著，主人自己去終端機看）。
// Guards: REQ-377
func TestAnswerApprovalRefusesOtherDialog(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", readScreen(t, "permission-dialog.txt"))
	p := permission()
	p.Summary = "rm -rf /tmp/something-else"
	r, err := v.e.AnswerApproval(context.Background(), &sdk.EngineAnswerApprovalRequest{Session: "code", Pending: p, Decision: &sdk.Decision{Allow: true}})
	if err != nil || r.Answered || r.Reason != sdk.AnswerUnsupported {
		t.Fatalf("對不上要回 unsupported：%+v %v", r, err)
	}
	if got := keysOf(v); len(got) != 0 {
		t.Fatalf("對不上不能按鍵：%v", got)
	}
}
