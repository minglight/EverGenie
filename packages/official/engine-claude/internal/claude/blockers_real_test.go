package claude

import (
	"strings"
	"testing"
)

// 2026-11-14 真的卡住時抓到的排版：選單在輸入框上方，輸入框上緣的橫線帶著 session 名稱，
// 輸入框裡可能留著沒送出的字。這種排版一定要被當成擋住（ECL-66）。
func TestBlockerRealLayout20261114(t *testing.T) {
	rule := strings.Repeat("─", 150)
	labeled := strings.Repeat("─", 144) + " art ─"
	menu := "  Teach auto mode about your environment?\n  Auto mode works better when it knows your environment. Takes about a minute.\n  ❯ 1. Yes\n    2. Not now\n    3. Don't show again\n  Enter to confirm · Esc to cancel\n"
	status := "  ⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent\n"
	cases := map[string]string{
		"輸入框留著字": "⏺ 輝夜，弄好了。\n\n" + rule + "\n" + menu + labeled + "\n❯ 收到，我等你的消息\n" + rule + "\n" + status,
		"輸入框是空的": "✻ Cooked for 20s · done 3:19 PM\n\n" + rule + "\n" + menu + labeled + "\n❯ \n" + rule + "\n" + status,
	}
	for name, screen := range cases {
		a := findBlocker(screen)
		if a == nil {
			t.Errorf("%s：沒認出選單", name)
			continue
		}
		if !a.blocking() {
			t.Errorf("%s：認出了選單，卻沒當成擋住輸入框", name)
		}
	}
}
