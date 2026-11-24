package claude

import (
	"context"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 選擇題被交回她的畫面後，主人在看板回答時由引擎替他按（ECL-58）。
// 畫面樣本是真機抓的：question-dialog.txt（Claude Code 2.1.317，一題單選、4＝Type something.、5＝Chat about this）、
// question-dialog-sidepane.txt（2026-11-03 一位秘書卡住的那一題，右邊有別的窗格）。真機實測：按選項數字就直接送出。

const dinner = "今天晚餐吃什麼？"

func dinnerPending() *sdk.Pending {
	return &sdk.Pending{ID: "q1", Kind: sdk.PendingKindQuestion, Tool: "AskUserQuestion", Questions: []sdk.Question{{
		Header: "晚餐", Text: dinner, AllowFreeText: true,
		Options: []sdk.QuestionOption{{Label: "日式拉麵"}, {Label: "義大利麵"}, {Label: "都不要"}},
	}}}
}

func questionEnv(t *testing.T, screen string) *engEnv {
	v := newEngEnv(t)
	v.tmux.typing = false
	v.tmux.setAlive("art", true)
	v.tmux.setScreen("art", readScreen(t, screen))
	return v
}

func answerQ(v *engEnv, p *sdk.Pending, answer string) (*sdk.EngineAnswerApprovalResponse, error) {
	return v.e.AnswerApproval(context.Background(), &sdk.EngineAnswerApprovalRequest{
		Session: "art", Pending: p, Decision: &sdk.Decision{Allow: true, Answers: map[string]string{p.Questions[0].Text: answer}},
	})
}

func TestParseQuestionDialogSamples(t *testing.T) {
	d, ok := parseQuestionDialog(readScreen(t, "question-dialog.txt"))
	if !ok || d.tabs || d.cursor != 1 || d.typeN != 4 || len(d.options) != 5 || d.pick("義大利麵") != 2 || d.pick("Chat about this") != 0 || d.pick("滷肉飯") != 0 {
		t.Fatalf("一題單選：%+v", d)
	}
	if !questionOnScreen(d.body, dinner) || questionOnScreen(d.body, "明天早餐吃什麼？") {
		t.Fatalf("題目要對得上：%q", d.body)
	}
	// 右邊有別的窗格：選項標籤不帶「│」後面的東西，題目框照樣認得。
	d, ok = parseQuestionDialog(readScreen(t, "question-dialog-sidepane.txt"))
	if !ok || d.typeN != 5 || d.pick("全部重做（Recommended）") != 1 || d.pick("都先不要") != 4 {
		t.Fatalf("右邊有窗格：%+v", d)
	}
	if !questionOnScreen(d.body, "我剛才說「W-41～W-47 你都確認了」是講錯的。實際上你確認了第一季的總覽圖表") {
		t.Fatalf("右邊有窗格時題目要認得：%q", d.body)
	}
	if _, ok := parseQuestionDialog(readScreen(t, "permission-dialog.txt")); ok {
		t.Fatal("權限確認框不是選擇題")
	}
}

func TestAnswerQuestionPressesOptionNumber(t *testing.T) {
	v := questionEnv(t, "question-dialog.txt")
	v.tmux.onKey = func(string, string) (string, bool) { return readScreen(t, "remote-control-active.txt"), true }
	r, err := answerQ(v, dinnerPending(), "義大利麵")
	if err != nil || !r.Answered {
		t.Fatalf("要答成功：%+v %v", r, err)
	}
	if got := strings.Join(keysOf(v), ","); got != "2" {
		t.Fatalf("按數字就送出、不補 Enter：%s", got)
	}
	// 秘書卡住的那一題（右邊有窗格）。
	v = questionEnv(t, "question-dialog-sidepane.txt")
	v.tmux.onKey = func(string, string) (string, bool) { return readScreen(t, "remote-control-active.txt"), true }
	p := &sdk.Pending{ID: "q2", Kind: sdk.PendingKindQuestion, Questions: []sdk.Question{{
		Text:    "我剛才說「W-41～W-47 你都確認了」是講錯的。實際上你確認了第一季的總覽圖表（W-43）和四份月報",
		Options: []sdk.QuestionOption{{Label: "全部重做（Recommended）"}, {Label: "只重做三份週報"}, {Label: "只重做年度總覽圖表"}, {Label: "都先不要"}},
	}}}
	if r, err := answerQ(v, p, "全部重做（Recommended）"); err != nil || !r.Answered || strings.Join(keysOf(v), ",") != "1" {
		t.Fatalf("右邊有窗格也要按對：%+v %v %v", r, err, keysOf(v))
	}
}

func TestAnswerQuestionEnterOnlyWhenCursorMovedOnSameQuestion(t *testing.T) {
	screen := readScreen(t, "question-dialog.txt")
	v := questionEnv(t, "question-dialog.txt")
	v.tmux.onKey = func(_, key string) (string, bool) {
		if key == "3" {
			moved := strings.Replace(screen, dialogCursor+" 1. 日式拉麵", "  1. 日式拉麵", 1)
			return strings.Replace(moved, "  3. 都不要", dialogCursor+" 3. 都不要", 1), true
		}
		return readScreen(t, "remote-control-active.txt"), true
	}
	if r, err := answerQ(v, dinnerPending(), "都不要"); err != nil || !r.Answered || strings.Join(keysOf(v), ",") != "3,Enter" {
		t.Fatalf("數字只移了游標、同一題還開著要補 Enter：%+v %v %v", r, err, keysOf(v))
	}
}

func TestAnswerQuestionTypesFreeText(t *testing.T) {
	screen := readScreen(t, "question-dialog.txt")
	v := questionEnv(t, "question-dialog.txt")
	typing := strings.Replace(strings.Replace(screen, dialogCursor+" 1. 日式拉麵", "  1. 日式拉麵", 1), "  4. Type something.", dialogCursor+" 4. Type something.", 1)
	v.tmux.onKey = func(_, key string) (string, bool) {
		switch key {
		case "4":
			return typing, true
		case "Enter":
			return readScreen(t, "remote-control-active.txt"), true
		}
		return "", false
	}
	v.tmux.mu.Lock()
	v.tmux.typing = false
	v.tmux.mu.Unlock()
	// 貼上的字進「Type something.」那一行（真機：❯ 4. 吃滷肉飯）。
	pasted := strings.Replace(typing, dialogCursor+" 4. Type something.", dialogCursor+" 4. 吃滷肉飯", 1)
	v.tmux.onPaste = func(string, string) string { return pasted }
	r, err := answerQ(v, dinnerPending(), "吃滷肉飯")
	if err != nil || !r.Answered {
		t.Fatalf("打字回答要成功：%+v %v", r, err)
	}
	if got := strings.Join(keysOf(v), ","); got != "4,Enter" {
		t.Fatalf("先按 Type something. 再 Enter：%s", got)
	}
	if ps := v.tmux.callsOf("paste"); len(ps) != 1 || ps[0].Arg != "吃滷肉飯" {
		t.Fatalf("要貼上主人打的字：%+v", ps)
	}
	// 字沒進那一行：不按 Enter（免得送出空的）。
	v = questionEnv(t, "question-dialog.txt")
	v.tmux.onKey = func(_, key string) (string, bool) {
		if key == "4" {
			return typing, true
		}
		return "", false
	}
	if r, _ := answerQ(v, dinnerPending(), "吃滷肉飯"); r.Answered || strings.Join(keysOf(v), ",") != "4" {
		t.Fatalf("字沒進去不能按 Enter：%+v %v", r, keysOf(v))
	}
}

func TestAnswerQuestionRefuses(t *testing.T) {
	multi := dinnerPending()
	multi.Questions[0].MultiSelect = true
	two := dinnerPending()
	two.Questions = append(two.Questions, sdk.Question{Text: "幾點吃？"})
	other := dinnerPending()
	other.Questions[0].Text = "明天早餐吃什麼？"
	tabs := strings.Replace(readScreen(t, "question-dialog.txt"), " ☐ 晚餐", " ☐ 晚餐  ☐ 時間  ✔ Submit", 1)
	for _, c := range []struct {
		name   string
		screen string
		p      *sdk.Pending
		want   string
	}{
		{"複選不代答", "question-dialog.txt", multi, sdk.AnswerUnsupported},
		{"兩題以上不代答", "question-dialog.txt", two, sdk.AnswerUnsupported},
		{"畫面上是別題：不按", "question-dialog.txt", other, sdk.AnswerUnsupported},
		{"畫面上已經沒有選擇題（在別處答了）", "remote-control-active.txt", dinnerPending(), sdk.AnswerNone},
		{"畫面上是多題分頁", "", dinnerPending(), sdk.AnswerUnsupported},
	} {
		v := questionEnv(t, "question-dialog.txt")
		if c.screen == "" {
			v.tmux.setScreen("art", tabs)
		} else {
			v.tmux.setScreen("art", readScreen(t, c.screen))
		}
		r, err := answerQ(v, c.p, "義大利麵")
		if err != nil || r.Answered || r.Reason != c.want {
			t.Fatalf("%s：要回 %s：%+v %v", c.name, c.want, r, err)
		}
		if len(v.tmux.callsOf("key")) != 0 || len(v.tmux.callsOf("paste")) != 0 {
			t.Fatalf("%s：不能按任何鍵", c.name)
		}
	}
}

// review 挑出的：題目裡有「1. 2.」清單不能當成選項；游標停在打字那一行不按；按了題目還開著不能說答好了。
func TestAnswerQuestionReviewCases(t *testing.T) {
	// 題目裡的編號清單：選項是 [部署, 修 bug]，題目寫「1. 修 bug／2. 部署」。按「部署」要按 1。
	listed := strings.Replace(readScreen(t, "question-dialog.txt"), "今天晚餐吃什麼？", "今天晚餐吃什麼？\n1. 修 bug\n2. 部署", 1)
	listed = strings.Replace(strings.Replace(listed, "1. 日式拉麵", "1. 部署", 1), "2. 義大利麵", "2. 修 bug", 1)
	d, ok := parseQuestionDialog(listed)
	if !ok || d.pick("部署") != 1 || d.pick("修 bug") != 2 || len(d.options) != 5 {
		t.Fatalf("題目裡的清單不算選項：%+v", d)
	}

	// 游標停在「Type something.」（有人在她畫面上正在打字）：不按。
	screen := readScreen(t, "question-dialog.txt")
	typing := strings.Replace(strings.Replace(screen, dialogCursor+" 1. 日式拉麵", "  1. 日式拉麵", 1), "  4. Type something.", dialogCursor+" 4. 我打一半", 1)
	v := questionEnv(t, "question-dialog.txt")
	v.tmux.setScreen("art", typing)
	if r, _ := answerQ(v, dinnerPending(), "義大利麵"); r.Answered || r.Reason != sdk.AnswerUnsupported || len(keysOf(v)) != 0 {
		t.Fatalf("有人正在打字不能按：%+v %v", r, keysOf(v))
	}

	// 按了題目還開著（游標也沒停在那項）：不說答好了。
	v = questionEnv(t, "question-dialog.txt")
	if r, _ := answerQ(v, dinnerPending(), "義大利麵"); r.Answered || r.Reason != "still" {
		t.Fatalf("題目還開著不能說答好了：%+v", r)
	}
}
