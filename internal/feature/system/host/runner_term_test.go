package host

import (
	"context"
	"strings"
	"testing"
)

// 服務由 launchd 起來時環境沒有 TERM：外部指令要拿到一個 TERM，
// 不然 Tailscale App 內附的程式會當成要開視窗而失敗，手機連線把開著的說成沒開（HST-7）。
func TestExecRunnerGivesCommandsATermWhenServiceHasNone(t *testing.T) {
	t.Setenv("TERM", "")
	got := withTerm([]string{"PATH=/usr/bin"})
	if len(got) != 2 || got[1] != "TERM=dumb" {
		t.Fatalf("沒有 TERM 時要補 TERM=dumb，得到 %v", got)
	}
	if kept := withTerm([]string{"TERM=xterm-256color", "PATH=/usr/bin"}); len(kept) != 2 || kept[0] != "TERM=xterm-256color" {
		t.Fatalf("已經有 TERM 就不動，得到 %v", kept)
	}
	if empty := withTerm([]string{"TERM="}); empty[len(empty)-1] != "TERM=dumb" {
		t.Fatalf("TERM 是空的也算沒有，得到 %v", empty)
	}
	out, code := ExecRunner{}.Run(context.Background(), "/bin/sh", "-c", "env -u TERM true; echo ${TERM:-none}")
	if code != 0 || strings.TrimSpace(out) == "none" || strings.TrimSpace(out) == "" {
		t.Fatalf("真的跑指令時也要帶 TERM，得到 %q（%d）", out, code)
	}
}
