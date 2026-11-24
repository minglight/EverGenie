package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// startFixtureAPI 用 fixture 家起全部模組（含真的 api），回家目錄與 socket 路徑。
func startFixtureAPI(t *testing.T) (string, string) {
	t.Helper()
	return startFixtureAPIWith(t, nil)
}

// startFixtureAPIWith 跟 startFixtureAPI 一樣，但在模組啟動之前先讓 prep 對家做準備（例如放一個套件進 packages/mine/）。
func startFixtureAPIWith(t *testing.T, prep func(dir string)) (string, string) {
	t.Helper()
	dir, sock, _ := startFixtureWebWith(t, prep)
	return dir, sock
}

// startFixtureWebWith 同上，另外多回傳看板外殼（base/app/board）的 Handler，讓測試用 httptest 打公開路由（/avatars、/pkg-files）。
func startFixtureWebWith(t *testing.T, prep func(dir string)) (string, string, http.Handler) {
	t.Helper()
	dir := copyFixtureHome(t, false)
	if prep != nil {
		prep(dir)
	}
	m, err := buildModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Cleanup(func() {
		if err := m.app.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := m.app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return dir, filepath.Join(dir, "runtime/sockets/api.sock"), viaLoopback(m.board.Handler())
}

// viaLoopback 把 httptest 預設的 Host（example.com）換成 127.0.0.1，當成從本機進來：看板只聽 127.0.0.1，
// 而且 Host 不是 public_url、loopback 或 *.ts.net 的頁面一律 403（S3-2b，擋 DNS rebinding）。要測攻擊者的 Host 別用 example.com（會被換掉）。
func viaLoopback(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "example.com" || r.Host == "" {
			r.Host = "127.0.0.1"
		}
		h.ServeHTTP(w, r)
	})
}

// hg 跑一個 EverGenie 指令，回退出碼、標準輸出、標準錯誤。
func hg(t *testing.T, sock, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	features, err := featureCommands()
	if err != nil {
		t.Fatal(err)
	}
	code := runCLI(context.Background(), args, sdk.Dial(sock), cmdline.IO{In: strings.NewReader(stdin), Out: &out, Err: &errOut}, features)
	return code, out.String(), errOut.String()
}

func hgOK(t *testing.T, sock string, args ...string) string {
	t.Helper()
	code, out, errOut := hg(t, sock, "", args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errOut)
	}
	return out
}

func jsonLine(t *testing.T, s string) map[string]any {
	t.Helper()
	if strings.Count(s, "\n") != 1 {
		t.Fatalf("expected one JSON line: %q", s)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	return m
}

// Guards: REQ-49, REQ-59
func TestTasksAndRemindersCommands(t *testing.T) {
	_, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_AGENT", "code")

	task := jsonLine(t, hgOK(t, sock, "tasks", "add", "--type", "ask", "--title", "查一下報表", "--status", "doing", "--ask", "報表在哪"))
	id, _ := task["id"].(string)
	if id != "SEC-3" || task["by"] != "code" || task["status"] != "doing" {
		t.Fatalf("add: %v", task)
	}
	if got := jsonLine(t, hgOK(t, sock, "tasks", "get", id)); got["title"] != "查一下報表" {
		t.Fatalf("get: %v", got)
	}
	if got := jsonLine(t, hgOK(t, sock, "tasks", "set", id, "--result", "在共用資料夾", "--add_links", "https://example.test/a")); got["result"] != "在共用資料夾" {
		t.Fatalf("set: %v", got)
	}
	list := strings.Split(strings.TrimSpace(hgOK(t, sock, "tasks", "list", "--agent", "code")), "\n")
	if len(list) < 1 || !strings.Contains(strings.Join(list, "\n"), `"id":"SEC-3"`) {
		t.Fatalf("list: %q", list)
	}
	hgOK(t, sock, "tasks", "stale")
	hgOK(t, sock, "tasks", "waiting")
	if got := jsonLine(t, hgOK(t, sock, "tasks", "done", id, "--result", "在共用資料夾", "--verify", "verified", "--verify_how", "打開確認過")); got["status"] != "done" {
		t.Fatalf("done: %v", got)
	}

	normal := jsonLine(t, hgOK(t, sock, "tasks", "add", "--title", "一般任務", "--status", "doing", "--criteria", "報表對過"))
	code, out, errOut := hg(t, sock, "", "tasks", "done", normal["id"].(string), "--result", "做完了", "--verify", "verified", "--verify_how", "跑過")
	if code != 1 || out != "" || errOut == "" || strings.Count(errOut, "\n") != 1 {
		t.Fatalf("a secretary closing a normal task must be refused with the API message: %d %q %q", code, out, errOut)
	}
	if got := jsonLine(t, hgOK(t, sock, "tasks", "submit", normal["id"].(string), "--result", "做完了", "--verify", "verified", "--verify_how", "跑過", "--proposed", "done")); got["status"] == "done" {
		t.Fatalf("submit must wait for the owner: %v", got)
	}

	rem := jsonLine(t, hgOK(t, sock, "reminders", "add", "--content", "回覆廠商", "--follow", "none", "--owner", "life"))
	rid, _ := rem["id"].(string)
	if rid == "" || rem["content"] != "回覆廠商" || rem["by"] != "code" {
		t.Fatalf("reminder add: %v", rem)
	}
	if got := jsonLine(t, hgOK(t, sock, "reminders", "get", rid)); got["id"] != rid {
		t.Fatalf("reminder get: %v", got)
	}
	if got := jsonLine(t, hgOK(t, sock, "reminders", "set", rid, "--content", "回覆廠商報價")); got["content"] != "回覆廠商報價" {
		t.Fatalf("reminder set: %v", got)
	}
	if got := jsonLine(t, hgOK(t, sock, "reminders", "snooze", rid, "--until", "3h")); got["id"] != rid {
		t.Fatalf("reminder snooze: %v", got)
	}
	if got := jsonLine(t, hgOK(t, sock, "reminders", "doing", rid)); got["status"] != "doing" {
		t.Fatalf("reminder doing: %v", got)
	}
	if got := jsonLine(t, hgOK(t, sock, "reminders", "done", rid)); got["status"] != "done" {
		t.Fatalf("reminder done: %v", got)
	}
	other := jsonLine(t, hgOK(t, sock, "reminders", "add", "--content", "不用做了", "--follow", "none", "--owner", "life"))
	if got := jsonLine(t, hgOK(t, sock, "reminders", "cancel", other["id"].(string))); got["status"] != "cancelled" {
		t.Fatalf("reminder cancel: %v", got)
	}
	if out := hgOK(t, sock, "reminders", "list", "--owner", "life"); !strings.Contains(out, `"id":"R-1"`) {
		t.Fatalf("reminder list: %q", out)
	}
	if out := hgOK(t, sock, "roster"); !strings.Contains(out, `"id":"code"`) || !strings.Contains(out, `"id":"life"`) {
		t.Fatalf("roster: %q", out)
	}
}

// Guards: REQ-116
func TestCliErrorFirstLineIsAPIMessage(t *testing.T) {
	_, sock := startFixtureAPI(t)
	code, out, errOut := hg(t, sock, "", "tasks", "get", "SEC-404")
	if code != 1 || out != "" || strings.Count(errOut, "\n") != 1 || strings.Contains(errOut, "goroutine") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	var apiErr *sdk.Error
	_, err := sdk.NewTasksClient(sdk.Dial(sock)).Get(context.Background(), &sdk.TasksGetRequest{ID: "SEC-404"})
	if e, ok := err.(*sdk.Error); !ok {
		t.Fatalf("%v", err)
	} else {
		apiErr = e
	}
	if errOut != apiErr.Message+"\n" {
		t.Fatalf("stderr %q is not the API message %q", errOut, apiErr.Message)
	}
	code, _, errOut = hg(t, filepath.Join(t.TempDir(), "x.sock"), "", "tasks", "get", "SEC-1")
	if code != 1 || errOut != "連不到 EverGenie，請確認 serve 在跑\n" {
		t.Fatalf("no server: %d %q", code, errOut)
	}
}

// Guards: REQ-57
func TestCliByFromEnvAgainstRealAPI(t *testing.T) {
	_, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_AGENT", "life")
	if got := jsonLine(t, hgOK(t, sock, "tasks", "add", "--title", "生活秘書開的", "--criteria", "看得到這張單")); got["by"] != "life" {
		t.Fatalf("by from EVERGENIE_AGENT: %v", got)
	}
	t.Setenv("EVERGENIE_AGENT", "")
	if got := jsonLine(t, hgOK(t, sock, "tasks", "add", "--title", "命令列開的")); got["by"] != "owner(cli)" {
		t.Fatalf("default by: %v", got)
	}
}

// Guards: REQ-23
func TestNotifyCommand(t *testing.T) {
	_, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_AGENT", "code")
	n := jsonLine(t, hgOK(t, sock, "notify", "--agent", "code", "--category", "approve", "--title", "要推送嗎？", "--todo", "決定要不要推", "--options", "好|不要"))
	id, _ := n["id"].(string)
	if !strings.HasPrefix(id, "M-") || n["category"] != "approve" {
		t.Fatalf("notify: %v", n)
	}
	got, err := sdk.NewNotifyClient(sdk.Dial(sock)).Get(context.Background(), &sdk.NotifyGetRequest{ID: id})
	if err != nil || got.Notification.Category != sdk.CategoryApprove || len(got.Notification.Options) != 2 || got.Notification.Options[1] != "不要" {
		t.Fatalf("stored notice: %+v %v", got, err)
	}
	hgOK(t, sock, "notify", "--agent", "code", "--category", "done", "--title", "做完了")
	code, _, errOut := hg(t, sock, "", "notify", "--agent", "code", "--category", "approve", "--title", "缺 todo")
	if code != 1 || errOut == "" {
		t.Fatalf("the API's refusal must be shown: %d %q", code, errOut)
	}
}

// Guards: REQ-152
func TestGoalCommands(t *testing.T) {
	_, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_AGENT", "code")
	ctx := context.Background()
	notify := sdk.NewNotifyClient(sdk.Dial(sock))

	g := jsonLine(t, hgOK(t, sock, "goal", "propose", "--title", "整理報表", "--text", "把三月的報表整理好", "--criteria", "三月每天都有一列"))
	id, _ := g["id"].(string)
	if id != "GOAL-1" || g["state"] != "draft" {
		t.Fatalf("propose: %v", g)
	}
	notice, _ := g["notice"].(string)
	got, err := notify.Get(ctx, &sdk.NotifyGetRequest{ID: notice})
	if err != nil || got.Notification.Category != sdk.CategoryApprove || !strings.Contains(got.Notification.Title, "整理報表") {
		t.Fatalf("propose should raise a confirmation notice: %+v %v", got, err)
	}
	if _, err = notify.Answer(ctx, &sdk.NotifyAnswerRequest{ID: notice, Text: "確認", Via: sdk.AnswerViaButton, By: &sdk.Actor{Name: "owner(web)"}}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(path, []byte("1. 收集資料\n2. 整理\n"), 0600); err != nil {
		t.Fatal(err)
	}
	updated := jsonLine(t, hgOK(t, sock, "goal", "planbook", id, "--file", path, "--note", "先寫兩步"))
	if updated["text"] != "把三月的報表整理好" || updated["state"] != "active" || !strings.Contains(updated["planbook"].(string), "收集資料") {
		t.Fatalf("planbook edit must keep the owner's text: %v", updated)
	}
	show := hgOK(t, sock, "goal", "show", id)
	for _, want := range []string{"把三月的報表整理好", "active", "1. 收集資料"} {
		if !strings.Contains(show, want) {
			t.Fatalf("goal show lacks %q: %s", want, show)
		}
	}
	if out := hgOK(t, sock, "goal", "list", "--state", "active"); !strings.Contains(out, id) {
		t.Fatalf("goal list: %q", out)
	}
	if out := hgOK(t, sock, "goal", "list", "--state", "done"); out != "" {
		t.Fatalf("no done goals yet: %q", out)
	}

	code, out, errOut := hg(t, sock, "", "goal", "withdraw", id)
	if code != 1 || out != "" || errOut == "" || strings.Count(errOut, "\n") != 1 {
		t.Fatalf("a secretary must not withdraw a goal: %d %q %q", code, out, errOut)
	}
	if after := hgOK(t, sock, "goal", "show", id); !strings.Contains(after, "active") {
		t.Fatalf("goal must stay active: %s", after)
	}
	t.Setenv("EVERGENIE_AGENT", "")
	if w := jsonLine(t, hgOK(t, sock, "goal", "withdraw", id)); w["state"] != "withdrawn" {
		t.Fatalf("the owner may withdraw: %v", w)
	}
}

// Guards: REQ-14
func TestSayCommandCoreRoute(t *testing.T) {
	_, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_AGENT", "code")
	env := jsonLine(t, hgOK(t, sock, "say", "life", "請幫我看", "一下報表"))
	if env["from"] != "code" || env["to"] != "life" || env["kind"] != "say" || env["route"] != "core" || env["body"] != "請幫我看 一下報表" {
		t.Fatalf("say: %v", env)
	}
	got, err := sdk.NewBusClient(sdk.Dial(sock)).Get(context.Background(), &sdk.BusGetRequest{ID: env["id"].(string)})
	if err != nil || got.Envelope.Route != sdk.EnvelopeRouteCore {
		t.Fatalf("stored envelope: %+v %v", got, err)
	}
}

// Guards: REQ-34
func TestShareCommand(t *testing.T) {
	dir, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_HOME", dir)
	t.Setenv("EVERGENIE_AGENT", "code")
	src := filepath.Join(t.TempDir(), "報告 final.txt")
	if err := os.WriteFile(src, []byte("分享的內容\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := hgOK(t, sock, "share", src)
	url := strings.TrimSpace(out)
	if !strings.HasPrefix(url, "/share/") || strings.Count(out, "\n") != 1 || strings.ContainsAny(url, " ") {
		t.Fatalf("share should print one /share/ URL line: %q", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "runtime", url))
	if err != nil || string(data) != "分享的內容\n" {
		t.Fatalf("shared copy: %q %v", data, err)
	}
	env := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(env, []byte("TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := hg(t, sock, "", "share", env); code != 1 || out != "" || !strings.Contains(errOut, "不能分享") {
		t.Fatalf("a .env file must be refused with exit 1: %d %q %q", code, out, errOut)
	}
}

// TestJobsCLIOnFixtureHome：在 fixture 家用真的 serve 跑一輪 jobs（Q1）：範本與自己加的排程寫進 vault、排程服務重新載入；壞掉的 cron 在寫入時就被擋下，不會進 vault。
// Guards: REQ-18, REQ-22, REQ-159
func TestJobsCLIOnFixtureHome(t *testing.T) {
	_, sock := startFixtureAPI(t)
	file := filepath.Join(t.TempDir(), "job.md")
	if err := os.WriteFile(file, []byte("做一件事"), 0o600); err != nil {
		t.Fatal(err)
	}
	hgOK(t, sock, "jobs", "add", "life/chat", "--template", "chat")
	hgOK(t, sock, "jobs", "add", "code/ai-topics", "--template", "ai-topics")
	if code, _, errOut := hg(t, sock, "", "jobs", "add", "code/broken", "--cron", "every day", "--file", file); code != 1 || !strings.Contains(errOut, "五欄位") {
		t.Fatalf("壞掉的 cron 要在寫入時被擋下（結束碼 1）：%d %q", code, errOut)
	}
	out := hgOK(t, sock, "jobs", "list")
	if !strings.Contains(out, "life/chat") || !strings.Contains(out, "code/ai-topics") || strings.Contains(out, "code/broken") {
		t.Fatalf("jobs list：\n%s", out)
	}
	hgOK(t, sock, "jobs", "set", "code/ai-topics", "--enabled", "false")
	hgOK(t, sock, "jobs", "rm", "life/chat")
	var doc string
	for i := 0; i < 100; i++ {
		_, doc, _ = hg(t, sock, "", "doctor")
		if strings.Contains(doc, "排程都載入了") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(doc, "排程都載入了") || strings.Contains(doc, "code/broken") {
		t.Fatalf("doctor 應該說排程都載入了：\n%s", doc)
	}
	if out := hgOK(t, sock, "jobs", "list"); strings.Contains(out, "life/chat") {
		t.Fatalf("移除後 jobs list 不該再有 life/chat：\n%s", out)
	}
}

// TestAgentsCommandsOnFixtureHome：在 fixture 家用真的 serve 跑一輪秘書管理（Q3）：new → set → assign → swap → mv（排程跟著搬）→ rm（封存）、
// customize、config、agents link 預演。
// Guards: REQ-41, REQ-46, REQ-161, REQ-162, REQ-163
func TestAgentsCommandsOnFixtureHome(t *testing.T) {
	dir, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_HOME", dir)

	hgOK(t, sock, "agents", "new", "scout", "--persona", "helper", "--display", "偵察", "--duty", "查資料")
	if out := hgOK(t, sock, "agents", "list"); !strings.Contains(out, "scout") || !strings.Contains(out, "code") {
		t.Fatalf("agents list：\n%s", out)
	}
	if code, _, errOut := hg(t, sock, "", "agents", "new", "scout", "--persona", "helper"); code != 1 || !strings.Contains(errOut, "已經有") {
		t.Fatalf("重複建立：%d %q", code, errOut)
	}
	hgOK(t, sock, "agents", "set", "scout", "enabled", "true")
	hgOK(t, sock, "agents", "assign", "scout", "builder")
	hgOK(t, sock, "agents", "swap", "scout", "life")
	if code, _, errOut := hg(t, sock, "", "agents", "swap", "scout", "life"); code != 0 {
		t.Fatalf("換回去：%d %s", code, errOut)
	}

	// 秘書自己不能改名冊（EVERGENIE_AGENT 不是主人）。
	t.Setenv("EVERGENIE_AGENT", "life")
	if code, _, errOut := hg(t, sock, "", "agents", "set", "scout", "duty", "亂改"); code != 1 || !strings.Contains(errOut, "只有主人") {
		t.Fatalf("秘書不能改名冊：%d %q", code, errOut)
	}
	if code, _, _ := hg(t, sock, "", "config", "set", "user.name", "駭客"); code != 1 {
		t.Fatalf("秘書不能改家的設定：%d", code)
	}
	t.Setenv("EVERGENIE_AGENT", "")

	// mv：code 的排程（code/ping）跟著搬成 dev/ping。
	hgOK(t, sock, "agents", "mv", "code", "dev")
	if out := hgOK(t, sock, "jobs", "list"); !strings.Contains(out, "dev/ping") || strings.Contains(out, "code/ping") {
		t.Fatalf("排程要跟著改名：\n%s", out)
	}
	if code, _, _ := hg(t, sock, "", "agents", "mv", "dev", "life"); code != 1 {
		t.Fatalf("新 id 已被用：%d", code)
	}

	hgOK(t, sock, "customize", "add", "life", "先講結論")
	if out := hgOK(t, sock, "customize", "show", "life"); !strings.Contains(out, "先講結論") {
		t.Fatalf("customize show：\n%s", out)
	}

	out := hgOK(t, sock, "config", "set", "user.name", `"Amy"`)
	if !strings.Contains(out, "runtime/backups/home/") {
		t.Fatalf("config set：\n%s", out)
	}
	if out := hgOK(t, sock, "config", "get", "user.name"); strings.TrimSpace(out) != `"Amy"` {
		t.Fatalf("config get：%q", out)
	}

	if out := hgOK(t, sock, "agents", "link"); !strings.Contains(out, "預演") {
		t.Fatalf("agents link：\n%s", out)
	}

	hgOK(t, sock, "agents", "rm", "scout")
	if out := hgOK(t, sock, "agents", "list"); strings.Contains(out, "scout") {
		t.Fatalf("封存後名冊不該再有 scout：\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "vault", "agents", "scout")); err != nil {
		t.Fatalf("封存不是刪除，資料夾還在：%v", err)
	}
}

// 業務模組的指令表跟 cli 自己那張表合併後不重名，help 照樣列得到 reminders。
func TestFeatureCommandsMergeWithCLIAndAppearInHelp(t *testing.T) {
	features, err := featureCommands()
	if err != nil {
		t.Fatal(err)
	}
	if features["reminders add"].Run == nil || features["reminders list"].Run == nil {
		t.Fatalf("業務模組的指令表缺 reminders：%d 個", len(features))
	}
	// 其他來源的指令也都在：settings（config、backup、preflight）、角色庫（feature/agents/persona）、要動這台電腦的本機指令（hostCommands）、doctor。
	for _, name := range []string{"config get", "config set", "backup list", "backup prune", "preflight", "personas list", "personas pack", "init", "service install", "service restart", "tailscale setup", "doctor"} {
		if features[name].Run == nil {
			t.Errorf("指令表缺 %q", name)
		}
	}
	var out, errOut bytes.Buffer
	if code := runCLI(context.Background(), []string{"help"}, nil, cmdline.IO{Out: &out, Err: &errOut}, features); code != 0 || !strings.Contains(out.String(), "reminders add|set|snooze|done|doing|cancel|list|get") {
		t.Fatalf("help：%d %s %s", code, out.String(), errOut.String())
	}
	// 沒有登記業務模組指令時（例如舊的呼叫方）reminders 不存在，會是「不認得的指令」。
	out.Reset()
	errOut.Reset()
	if code := runCLI(context.Background(), []string{"reminders", "list"}, nil, cmdline.IO{Out: &out, Err: &errOut}, nil); code != 2 || !strings.Contains(errOut.String(), "不認得的指令：reminders list") {
		t.Fatalf("沒登記：%d %s", code, errOut.String())
	}
}

// 每個頂層指令都自己帶了用法與分段（help 不會掉回自動列出的「名稱 子指令|子指令」）。
func TestEveryTopCommandShowsUsage(t *testing.T) {
	features, err := featureCommands()
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for name, c := range features {
		top := strings.Fields(name)[0]
		if _, ok := has[top]; !ok {
			has[top] = false
		}
		if c.Usage != "" {
			has[top] = true
			if c.Group == cmdline.GroupOther {
				t.Errorf("%s 的用法沒有選分段", top)
			}
		}
	}
	for top, ok := range has {
		if !ok {
			t.Errorf("%s 沒有登記用法", top)
		}
	}
}
