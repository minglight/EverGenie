package claude

import (
	"errors"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 淡色的字拿掉、其他控制碼去掉、換行照留；顏色參數裡的 2（38;5;2、38;2;…）不是淡色。
func TestWithoutFaintDropsFaintTextOnly(t *testing.T) {
	cases := []struct{ name, raw, want string }{
		{"建議字", "\x1b[39m❯ \x1b[2m幫我整理一下\x1b[0m", "❯ "},
		{"22 回正常", "a\x1b[2mb\x1b[22mc", "ac"},
		{"空參數重設", "\x1b[2mx\x1b[my", "y"},
		{"256 色的 2", "\x1b[38;5;2m綠字\x1b[39m", "綠字"},
		{"真彩的 2", "\x1b[38;2;2;2;2m字\x1b[0m", "字"},
		{"粗體加淡色", "\x1b[1;2m淡\x1b[0m正常", "正常"},
		{"換行照留", "\x1b[2m第一行\n第二行\x1b[0m\n第三行", "\n\n第三行"},
		{"私有序列不是樣式", "\x1b[>4;2m字", "字"},
		{"底線樣式 4:2", "\x1b[4:2m字\x1b[0m", "字"},
		{"超連結", "\x1b]8;;https://x\x07連結\x1b]8;;\x1b\\", "連結"},
	}
	for _, c := range cases {
		if got := withoutFaint(c.raw); got != c.want {
			t.Errorf("%s：得到 %q，要 %q", c.name, got, c.want)
		}
	}
}

// 帶樣式的輸入框裡只有淡色的建議字（平台猜主人下一句）：不算主人在打字，/compact 照送（ECL-55；2026-11-03 遇過）。
func TestCommandSendsWhenInputBoxHoldsOnlyFaintSuggestion(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("health", true)
	plain := readScreen(t, "labeled-rule.txt")
	v.tmux.setScreen("health", withBox(plain, "幫我整理一下"))
	v.tmux.styled = map[string]string{"health": strings.Replace(withBox(plain, "\x1b[2m幫我整理一下\x1b[0m"), "❯ ", "\x1b[39m❯\u00a0", 1)}
	if r := runCommand(t, v, "health", sdk.SessionCommandCompact, ""); r.Reason == sdk.CommandTyping {
		t.Fatalf("淡色建議字不算打字：%+v", r)
	}
	if len(v.tmux.callsOf("paste")) != 1 {
		t.Fatal("要貼上 /compact")
	}
}

// 主人真的打了字（不是淡色）：照樣擋；帶樣式的畫面抓不到時照純文字判斷，也擋。
func TestCommandStillRefusesRealTypingWithStyledCapture(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("health", true)
	plain := readScreen(t, "labeled-rule.txt")
	v.tmux.setScreen("health", withBox(plain, "幫我看一下"))
	v.tmux.styled = map[string]string{"health": withBox(plain, "\x1b[1m幫我看一下\x1b[0m")}
	if r := runCommand(t, v, "health", sdk.SessionCommandClear, ""); r.Done || r.Reason != sdk.CommandTyping {
		t.Fatalf("真的有字要回 typing：%+v", r)
	}
	v.tmux.styled = nil
	v.tmux.styledErr = errors.New("capture failed")
	if r := runCommand(t, v, "health", sdk.SessionCommandClear, ""); r.Done || r.Reason != sdk.CommandTyping {
		t.Fatalf("抓不到帶樣式的畫面時照純文字擋：%+v", r)
	}
	if len(v.tmux.callsOf("paste")) != 0 {
		t.Fatal("不能貼")
	}
}

// 主人打了一半（「幫我」），平台在後面接淡色的建議（「整理一下」）：只拿掉建議，主人的字還在，照樣擋。
func TestCommandRefusesWhenOwnerTypedBeforeFaintSuggestion(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("health", true)
	plain := readScreen(t, "labeled-rule.txt")
	v.tmux.setScreen("health", withBox(plain, "幫我整理一下"))
	v.tmux.styled = map[string]string{"health": withBox(plain, "幫我\x1b[2m整理一下\x1b[0m")}
	if r := runCommand(t, v, "health", sdk.SessionCommandCompact, ""); r.Done || r.Reason != sdk.CommandTyping {
		t.Fatalf("主人的字要擋：%+v", r)
	}
}

// 框線畫成淡色、框裡有字：去掉淡色後找不到框，不能當成空的照送（寧可擋）。
func TestCommandRefusesWhenBoxRulesAreFaint(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("health", true)
	plain := readScreen(t, "labeled-rule.txt")
	v.tmux.setScreen("health", withBox(plain, "幫我看"))
	lines := strings.Split(withBox(plain, "幫我看"), "\n")
	for i, l := range lines {
		if isRule(l) {
			lines[i] = "\x1b[2m" + l + "\x1b[0m"
		}
	}
	v.tmux.styled = map[string]string{"health": strings.Join(lines, "\n")}
	if r := runCommand(t, v, "health", sdk.SessionCommandCompact, ""); r.Done || r.Reason != sdk.CommandTyping {
		t.Fatalf("框線淡色時要擋：%+v", r)
	}
}
