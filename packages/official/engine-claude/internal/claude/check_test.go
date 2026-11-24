package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

var classifyEnv = ClassifyEnv{Home: "/Users/tester", ConfigDir: "/Users/tester/.claude-acct"}

func bashInput(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

func pathInput(key, path string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{key: path})
	return b
}

// Guards: REQ-153
func TestCheckClassifyTable(t *testing.T) {
	R, W, C, P := sdk.ActionClassRead, sdk.ActionClassWrite, sdk.ActionClassCommit, sdk.ActionClassPush
	D, X, S, M := sdk.ActionClassDelete, sdk.ActionClassSendExternal, sdk.ActionClassSettings, sdk.ActionClassSpend
	empty := json.RawMessage(`{}`)
	type row struct {
		tool  string
		input json.RawMessage
		want  sdk.ActionClass
	}
	rows := []row{
		// 讀
		{"Read", pathInput("file_path", "/a"), R}, {"Glob", empty, R}, {"Grep", empty, R}, {"LS", empty, R},
		{"WebSearch", empty, R}, {"WebFetch", empty, R}, {"ListAgents", empty, R},
		{"SendMessage", empty, R}, {"AskUserQuestion", empty, R}, {"TodoWrite", empty, R},
		{"ToolSearch", empty, R}, {"Skill", empty, R}, {"Agent", empty, R}, {"Task", empty, R},
		{"TaskList", empty, R}, {"TaskGet", empty, R}, {"TaskOutput", empty, R}, {"BashOutput", empty, R}, {"CronList", empty, R},
		// Bash：cd／test 不改變串接的分類；轉向到描述子或 /dev/null 不算寫入
		{"Bash", bashInput("cd /repo && git status"), R}, {"Bash", bashInput("test -f a && cat a"), R},
		{"Bash", bashInput("ls missing 2>&1 | head"), R}, {"Bash", bashInput("grep x f 2>/dev/null"), R},
		{"Bash", bashInput("echo hi >&2"), R}, {"Bash", bashInput("cd /repo && rm a"), D},
		{"Bash", bashInput("echo hi 2>&1 > out.txt"), W}, {"Bash", bashInput("cat a >/dev/null >> b"), W},
		// 檔案：設定 vs 一般寫
		{"Edit", pathInput("file_path", "/tmp/proj/main.go"), W},
		{"Write", pathInput("file_path", "/Users/tester/proj/.claude/notes/a.md"), W},
		{"NotebookEdit", pathInput("notebook_path", "/tmp/a.ipynb"), W},
		{"Write", pathInput("file_path", "/Users/tester/.claude-acct/CLAUDE.md"), S},
		{"Edit", pathInput("file_path", "/Users/tester/.claude-acme/agents/x.md"), S},
		{"Edit", pathInput("file_path", "~/.claude/settings.json"), S},
		{"Write", pathInput("file_path", "/etc/hosts"), S},
		{"Write", pathInput("file_path", "/tmp/proj/settings.local.json"), S},
		{"Edit", pathInput("file_path", "/Users/tester/.claude-acct/../proj/a.go"), W},
		// Bash：push
		{"Bash", bashInput("git push origin main"), P}, {"Bash", bashInput("gh pr create --fill"), P},
		{"Bash", bashInput("gh pr merge 3"), P}, {"Bash", bashInput("gh release create v1"), P},
		{"Bash", bashInput("git -C /repo push"), P}, {"Bash", bashInput("FOO=1 git push"), P},
		// Bash：delete
		{"Bash", bashInput("rm -rf build"), D}, {"Bash", bashInput("rmdir d"), D}, {"Bash", bashInput("git rm a.go"), D},
		{"Bash", bashInput("git clean -fd"), D}, {"Bash", bashInput("git reset --hard HEAD~1"), D},
		{"Bash", bashInput("git branch -D old"), D}, {"Bash", bashInput("git push --force origin x"), D},
		{"Bash", bashInput("git push -f origin x"), D},
		// Bash：commit
		{"Bash", bashInput(`git commit -m "x"`), C},
		// Bash：唯讀
		{"Bash", bashInput("ls -la"), R}, {"Bash", bashInput("cat a.txt | head -5"), R}, {"Bash", bashInput("tail -n 3 a"), R},
		{"Bash", bashInput("grep -rn foo ."), R}, {"Bash", bashInput("rg foo"), R}, {"Bash", bashInput("find . -name '*.go'"), R},
		{"Bash", bashInput("wc -l a"), R}, {"Bash", bashInput("pwd"), R}, {"Bash", bashInput("echo hi"), R},
		{"Bash", bashInput("which go"), R}, {"Bash", bashInput("git status"), R}, {"Bash", bashInput("git log --oneline"), R},
		{"Bash", bashInput("git diff HEAD"), R}, {"Bash", bashInput("git show HEAD"), R}, {"Bash", bashInput("git branch"), R},
		{"Bash", bashInput("git rev-parse HEAD"), R}, {"Bash", bashInput("evergenie tasks list"), R}, {"Bash", bashInput("evergenie status"), R}, {"Bash", bashInput("evergenie sessions peek code --lines 5"), R}, {"Bash", bashInput("evergenie sessions restart code"), sdk.ActionClassWrite}, {"Bash", bashInput("evergenie jobs list --agent code"), R}, {"Bash", bashInput("evergenie agents list"), R}, {"Bash", bashInput("evergenie agents link"), R}, {"Bash", bashInput("evergenie agents link --apply"), sdk.ActionClassSettings}, {"Bash", bashInput("evergenie agents assign code athena"), sdk.ActionClassWrite}, {"Bash", bashInput("evergenie config show"), R}, {"Bash", bashInput("evergenie quota check claude --week-max 90"), R}, {"Bash", bashInput("evergenie preflight"), R}, {"Bash", bashInput("evergenie preflight --fix"), sdk.ActionClassWrite}, {"Bash", bashInput("evergenie service status"), R}, {"Bash", bashInput("evergenie service install"), sdk.ActionClassSettings}, {"Bash", bashInput("evergenie tailscale setup"), sdk.ActionClassSettings}, {"Bash", bashInput("evergenie backup list"), R}, {"Bash", bashInput("evergenie backup prune"), R}, {"Bash", bashInput("evergenie backup prune --apply"), sdk.ActionClassWrite}, {"Bash", bashInput("evergenie config get user.timezone"), R}, {"Bash", bashInput("evergenie config set user.timezone Asia/Taipei"), sdk.ActionClassSettings}, {"Bash", bashInput("evergenie web sites list"), R}, {"Bash", bashInput("evergenie web allow list --json"), R}, {"Bash", bashInput("evergenie web sites add https://office.example --color #abc"), sdk.ActionClassSettings}, {"Bash", bashInput("evergenie web allow rm https://mini.example"), sdk.ActionClassSettings}, {"Bash", bashInput("evergenie web owner set amy@example.com"), sdk.ActionClassSettings}, {"Bash", bashInput("evergenie customize show code"), R}, {"Bash", bashInput("evergenie customize add code 先講結論"), sdk.ActionClassWrite}, {"Bash", bashInput("evergenie jobs set code/ai-topics --enabled false"), sdk.ActionClassWrite}, {"Bash", bashInput("evergenie trust denied --days 3"), R}, {"Bash", bashInput("evergenie trust snapshot"), sdk.ActionClassWrite}, {"Bash", bashInput("evergenie setup permissions --choice all"), sdk.ActionClassSettings},
		{"Bash", bashInput("evergenie reminders get R-1"), R}, {"Bash", bashInput("evergenie agent show code"), R},
		{"Bash", bashInput(`echo "exit=$?"`), R}, {"Bash", bashInput(`echo "a > b"`), R}, {"Bash", bashInput("/usr/bin/ls"), R},
		// Bash：EverGenie 唯讀子指令、say、roster；子殼與群組照裡面的指令分類
		{"Bash", bashInput("evergenie tasks stale"), R}, {"Bash", bashInput("evergenie tasks waiting"), R},
		{"Bash", bashInput("evergenie goal show GOAL-1"), R}, {"Bash", bashInput("evergenie roster"), R}, {"Bash", bashInput("evergenie docs"), R}, {"Bash", bashInput("evergenie docs list"), R}, {"Bash", bashInput("evergenie docs show conventions/terms.md"), R},
		{"Bash", bashInput("evergenie policy"), R}, {"Bash", bashInput("evergenie policy --help"), R}, {"Bash", bashInput("evergenie policy list --status pending"), R}, {"Bash", bashInput("evergenie policy show P-1"), R},
		{"Bash", bashInput("evergenie policy request --kind rule --action write --scope ~/Workspace/x --why 被擋兩次"), W},
		{"Bash", bashInput("evergenie policy decide P-1 --approve --fingerprint abc"), sdk.ActionClassSettings}, {"Bash", bashInput("evergenie policy revoke P-1"), sdk.ActionClassSettings},
		{"Bash", bashInput("evergenie packages --help"), R}, {"Bash", bashInput("evergenie packages list"), R}, {"Bash", bashInput("evergenie packages get notes"), R}, {"Bash", bashInput("evergenie packages updates notes"), R},
		{"Bash", bashInput("evergenie packages reload --id notes"), W}, {"Bash", bashInput("evergenie packages install-go --confirmed"), W},
		{"Bash", bashInput("evergenie packages preview https://example.com/x.git"), R}, {"Bash", bashInput("evergenie packages outdated"), R}, {"Bash", bashInput("evergenie packages go-status"), R}, {"Bash", bashInput("evergenie packages install https://example.com/x.git --yes"), W}, {"Bash", bashInput("evergenie packages upgrade notes"), W},
		{"Bash", bashInput("evergenie packages sync fitness"), R}, {"Bash", bashInput("evergenie packages sync fitness --apply"), W}, {"Bash", bashInput("evergenie packages claim-notify garden"), W},
		{"Bash", bashInput("evergenie packages export garden --git --out /tmp/garden"), W}, // 在這台寫檔（SEC-302 S5）
		{"Bash", bashInput("evergenie data --help"), R}, {"Bash", bashInput("evergenie data kinds"), R}, {"Bash", bashInput("evergenie data kinds --json"), R},
		{"Bash", bashInput("evergenie data list nutrition.meal --since 2026-10-01"), R}, {"Bash", bashInput("evergenie data get nutrition.meal M-1"), R},
		{"Bash", bashInput(`evergenie data add nutrition.meal --json '{"kcal":500}'`), W}, {"Bash", bashInput("evergenie data set nutrition.meal M-1 --json '{}'"), W}, {"Bash", bashInput("evergenie data rm nutrition.meal M-1"), W},
		{"Bash", bashInput(`evergenie say life "收到"`), R}, {"Bash", bashInput("evergenie say life hi > f.txt"), W},
		{"Bash", bashInput("evergenie tasks set SEC-1 --status DONE"), W}, {"Bash", bashInput("evergenie goal propose"), W},
		{"Bash", bashInput("(ls /tmp)"), R}, {"Bash", bashInput("( cd /repo && ls ) 2>&1"), R},
		{"Bash", bashInput("(cd /repo && git status)"), R}, {"Bash", bashInput("(ls; cat a)"), R},
		{"Bash", bashInput("(ls; rm x)"), D}, {"Bash", bashInput("(touch a)"), W}, {"Bash", bashInput("((ls))"), W},
		{"Bash", bashInput("{ ls; cat a; }"), R}, {"Bash", bashInput("{ ls; rm x; }"), D}, {"Bash", bashInput("{ ls; touch x; }"), W},
		{"Bash", bashInput("(ls) && git push"), P}, {"Bash", bashInput("(ls; echo hi > f)"), W},
		{"Bash", bashInput("(evergenie say life hi)"), R},
		// Bash：看起來唯讀但會寫
		{"Bash", bashInput("echo hi > f.txt"), W}, {"Bash", bashInput("echo hi >> f.txt"), W},
		{"Bash", bashInput("cat a | tee b"), W}, {"Bash", bashInput("echo $(rm x)"), W}, {"Bash", bashInput("find . -name x -delete"), W},
		{"Bash", bashInput(`find . -name x -exec rm {} \;`), W},
		// Bash：其他
		{"Bash", bashInput("make build"), W}, {"Bash", bashInput("touch a"), W}, {"Bash", bashInput("evergenie tasks create"), W},
		{"Bash", bashInput("git add -A"), W}, {"Bash", bashInput("git checkout -b x"), W}, {"Bash", bashInput("gh pr view 3"), W},
		{"Bash", bashInput(""), W},
		// Bash：heredoc 的內文是資料不是指令（DEV-96）；沒加引號的內文有 $( 或反引號會被展開，照指令算
		{"Bash", bashInput("cat > /tmp/x_test.go <<'EOF'\n// settings.json\nrm -rf /\nevergenie setup\nEOF\ngo test ./..."), W},
		{"Bash", bashInput("cat <<\"EOF\" | head\nrm x\nEOF"), R}, {"Bash", bashInput("cat <<-EOF\n\trm x\n\tEOF\nls"), R},
		{"Bash", bashInput("git commit -F - <<'EOF'\nmsg: git push\nEOF"), C}, {"Bash", bashInput("cat <<'EOF'\nrm x\nEOF\nrm y"), D},
		{"Bash", bashInput("cat <<EOF\n$(rm x)\nEOF"), W}, {"Bash", bashInput("cat <<< 'rm x'"), R},
		// Bash：串起來取最嚴
		{"Bash", bashInput("touch x && git push"), P}, {"Bash", bashInput("ls; rm x"), D}, {"Bash", bashInput("git commit -m a && git push"), P},
		{"Bash", bashInput("ls | grep a || git commit -am b"), C}, {"Bash", bashInput("git push --force && ls"), D},
		{"Bash", bashInput("ls && cat a"), R}, {"Bash", bashInput(`echo "a && rm b"`), R}, {"Bash", bashInput("ls & rm x"), D},
		// MCP
		{"mcp__slack__send_message", empty, X}, {"mcp__tracker__create_issue", empty, X}, {"mcp__x__update_page", empty, X},
		{"mcp__x__delete_page", empty, X}, {"mcp__x__publish", empty, X}, {"mcp__x__add_comment", empty, X},
		{"mcp__x__invite_user", empty, X}, {"mcp__x__reply_thread", empty, X}, {"mcp__x__post_note", empty, X},
		{"mcp__shop__purchase_item", empty, M}, {"mcp__x__checkout", empty, M}, {"mcp__x__pay_invoice", empty, M},
		{"mcp__x__place_order", empty, M}, {"mcp__shop__create_order", empty, M},
		{"mcp__x__get_thing", empty, R}, {"mcp__docs__read", empty, R}, {"mcp__x__list_pages", empty, R},
		// 其他工具
		{"SomethingNew", empty, W},
	}
	for _, r := range rows {
		if got := Classify(classifyEnv, r.tool, r.input); got != r.want {
			t.Errorf("%s %s → %s，要 %s", r.tool, r.input, got, r.want)
		}
	}
	seen := map[sdk.ActionClass]bool{}
	for _, r := range rows {
		seen[r.want] = true
	}
	for _, c := range []sdk.ActionClass{R, W, C, P, D, X, S, M} {
		if !seen[c] {
			t.Errorf("表裡沒有 %s 的例子", c)
		}
	}
}

// Guards: REQ-153
func TestCheckOutputShapes(t *testing.T) {
	for _, tc := range []struct {
		verdict sdk.ActionVerdict
		payload string
		want    string
	}{
		{sdk.ActionVerdictDeny, "hooks/PreToolUse-check-deny.json", "hooks/PreToolUse-check-deny.out.json"},
		{sdk.ActionVerdictAsk, "hooks/PreToolUse-check-ask.json", "hooks/PreToolUse-check-ask.out.json"},
	} {
		e := newHookEnv(t)
		var want struct {
			HookSpecificOutput struct {
				PermissionDecisionReason string `json:"permissionDecisionReason"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(readFixture(t, tc.want), &want); err != nil {
			t.Fatal(err)
		}
		e.host.check = &sdk.EngineHostCheckActionResponse{Verdict: tc.verdict, Reason: want.HookSpecificOutput.PermissionDecisionReason}
		out, code := e.runFixture(t, "check", tc.payload)
		if code != 0 || !jsonEqual(t, []byte(out), readFixture(t, tc.want)) {
			t.Errorf("%s：輸出跟錄下的結構不同：%s", tc.verdict, out)
		}
	}
	// allow（以及不認得的結果）什麼都不印。
	for _, v := range []sdk.ActionVerdict{sdk.ActionVerdictAllow, "", "whatever"} {
		e := newHookEnv(t)
		e.host.check = &sdk.EngineHostCheckActionResponse{Verdict: v, Reason: "不該出現"}
		if out, code := e.runFixture(t, "check", "hooks/PreToolUse-check-deny.json"); out != "" || code != 0 {
			t.Errorf("verdict %q：要什麼都不印，卻印了 %q", v, out)
		}
	}
	// 核心收到的是分類結果與摘要。
	e := newHookEnv(t)
	e.runFixture(t, "check", "hooks/PreToolUse-check-deny.json")
	e.runFixture(t, "check", "hooks/PreToolUse-check-ask.json")
	if len(e.host.checks) != 2 {
		t.Fatalf("checks = %d", len(e.host.checks))
	}
	if c := e.host.checks[0]; c.Session != "code" || c.Class != sdk.ActionClassWrite || c.Tool != "Write" || c.Summary != "/private/tmp/hg-s0c/repo/a-deny.txt" {
		t.Errorf("Write 的檢查請求：%+v", c)
	}
	if c := e.host.checks[1]; c.Class != sdk.ActionClassWrite || c.Tool != "Bash" || !strings.HasPrefix(c.Summary, "touch a-ask.txt") {
		t.Errorf("Bash 的檢查請求：%+v", c)
	}
}

// Guards: REQ-153
func TestCheckCoreDownFallback(t *testing.T) {
	run := func(t *testing.T, turnFixture, checkFixture string, coreErr bool) string {
		t.Helper()
		e := newHookEnv(t)
		if turnFixture != "" {
			e.runFixture(t, "UserPromptSubmit", turnFixture)
		}
		e.host.err = nil
		if coreErr {
			e.host.err = errCoreDown
		}
		out, code := e.runFixture(t, "check", checkFixture)
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		return out
	}
	const peerTurn = "hooks/UserPromptSubmit-named-peer.json"
	// 別的秘書委派的回合、寫入類 → 擋。
	out := run(t, peerTurn, "hooks/PreToolUse-check-deny.json", true)
	if !jsonEqual(t, []byte(out), []byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"核心不在，別的秘書委派的寫入先不做"}}`)) {
		t.Errorf("peer＋寫入＋核心不在要 deny：%q", out)
	}
	// Bash（寫入類）也一樣。
	if out := run(t, peerTurn, "hooks/PreToolUse-Bash.json", true); !strings.Contains(out, `"deny"`) {
		t.Errorf("peer＋Bash 寫入要 deny：%q", out)
	}
	// 唯讀不擋：用一個唯讀工具的 payload。
	e := newHookEnv(t)
	e.runFixture(t, "UserPromptSubmit", peerTurn)
	e.host.err = errCoreDown
	if out, _ := e.run("check", []byte(`{"tool_name":"Read","tool_input":{"file_path":"/a"}}`)); out != "" {
		t.Errorf("peer＋讀取＋核心不在不該擋：%q", out)
	}
	// 主人打字、背景工作 → 不印。
	if out := run(t, "hooks/UserPromptSubmit.json", "hooks/PreToolUse-check-deny.json", true); out != "" {
		t.Errorf("typed＋核心不在不該印：%q", out)
	}
	if out := run(t, "hooks/UserPromptSubmit-task-notification.json", "hooks/PreToolUse-check-deny.json", true); out != "" {
		t.Errorf("system＋核心不在不該印：%q", out)
	}
	// 沒有回合紀錄 → 不印。
	if out := run(t, "", "hooks/PreToolUse-check-deny.json", true); out != "" {
		t.Errorf("沒有 turns 檔不該印：%q", out)
	}
	// 核心在、允許 → 即使 peer 也不印（交給核心判斷）。
	e2 := newHookEnv(t)
	e2.runFixture(t, "UserPromptSubmit", peerTurn)
	if out, _ := e2.runFixture(t, "check", "hooks/PreToolUse-check-deny.json"); out != "" {
		t.Errorf("核心在且允許不該印：%q", out)
	}
}
