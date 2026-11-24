package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 同一台跑兩個家（GOAL-2）：開 session 前要先記下這個家的 socket 再查存活，
// 不然會去預設的 hg 查，把另一個家同名的秘書當成「已經在執行」。
func TestStartChecksAliveOnConfiguredSocket(t *testing.T) {
	v := newEngEnv(t)
	r := v.req("code")
	r.EngineConfig["tmux_socket"] = "hg-ws"
	if _, err := v.e.Start(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	calls := v.tmux.snapshot()
	remembered, checked := -1, -1
	for i, c := range calls {
		if c.Name != "code" {
			continue
		}
		if c.Op == "remember" && c.Arg == "hg-ws" && remembered < 0 {
			remembered = i
		}
		if c.Op == "alive" && checked < 0 {
			checked = i
		}
	}
	if remembered < 0 || checked < 0 || remembered > checked {
		t.Fatalf("要先記 socket hg-ws 再查存活：%+v", calls)
	}
}

// 引擎重開後不記得 socket：從核心記的 attach 補回，存活檢查才查對的家。
func TestOrphanSocketRecoveredFromAttach(t *testing.T) {
	v := newEngEnv(t)
	dir := filepath.Join(v.home, "runtime", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: code\nstate: idle\nattach: tmux -L hg-ws attach -t '=code'\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "code.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	v.e.CheckAlive(context.Background())
	calls := v.tmux.snapshot()
	for _, c := range calls {
		if c.Op == "remember" && c.Name == "code" && c.Arg == "hg-ws" {
			return
		}
	}
	t.Fatalf("沒有從 attach 補回 socket：%+v", calls)
}

// 真的 tmux 實作：記了 socket 之後，存活檢查用那個 socket，不用預設的 hg。
func TestExecTmuxAliveUsesRememberedSocket(t *testing.T) {
	var got []string
	tm := &ExecTmux{run: func(_ context.Context, _ string, args ...string) (string, error) {
		got = args
		return "", nil
	}}
	tm.Remember("code", "hg-ws")
	tm.Alive("code")
	if len(got) < 2 || got[0] != "-L" || got[1] != "hg-ws" {
		t.Fatalf("要用記下的 socket：%v", strings.Join(got, " "))
	}
}
