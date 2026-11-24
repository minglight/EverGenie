//go:build integration

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// P3 階段整合測試：fixture 家 ＋ 真的 Claude ＋ 真的 tmux（由 code 在本機跑，執行者只跑 go vet -tags integration）。
//
//	EVERGENIE_IT_CONFIG_DIR=~/.claude-acme EVERGENIE_IT_MODEL=haiku \
//	  go test -tags integration -timeout 40m ./cmd/evergenie -run 'Integration|Conformance' -v
//
// 怎麼跑、會碰到什麼見 ai_docs/conventions/development-workflow.md「整合測試怎麼跑」。
// tmux socket 一律 hg-test（integrationTmuxSocket），結束時 kill-server；帳號的 settings.json 前後雜湊必須相同。

const (
	itBootTimeout  = 90 * time.Second
	itChatTimeout  = 3 * time.Minute
	itGoalTimeout  = 8 * time.Minute // Goal 生效 30 秒＋帶 Goal 寫檔 3 分鐘＋沒帶 Goal 寫檔 3 分鐘＋緩衝
	itPingTimeout  = 2 * time.Minute
	itDueTimeout   = 3 * time.Minute // 待辦 1 分鐘後到期＋2 分鐘內出現通知
	itPollEvery    = 2 * time.Second
	itDenyLogLine  = "委派動作未獲授權"
	itServeLogTail = 200
	// itReadmeFirstLine 是測試家 README.md 的第一行。
	itReadmeFirstLine = "# 整合測試的家"
)

// integrationConfig 讀兩個整合測試共用的環境變數；沒設就略過（不跑真的 Claude）。
func integrationConfig(t *testing.T) (configDir, model string) {
	t.Helper()
	configDir, model = sdk.Getenv("EVERGENIE_IT_CONFIG_DIR"), sdk.Getenv("EVERGENIE_IT_MODEL")
	if configDir == "" || model == "" {
		t.Skip("沒有設 EVERGENIE_IT_CONFIG_DIR／EVERGENIE_IT_MODEL（跑真的 Claude 要指定測試用帳號的設定資料夾與模型）")
	}
	return configDir, model
}

// scrubbedEnv 去掉會影響測試帳號的環境變數；EVERGENIE_* 由測試自己補。
func scrubbedEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CLAUDE") && !strings.HasPrefix(kv, "ANTHROPIC_") && !sdk.IsProductEnv(kv) {
			env = append(env, kv)
		}
	}
	return env
}

func fileHash(path string) string {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "absent"
	}
	if err != nil {
		return "error: " + err.Error()
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// setFrontmatter 改（或補上）frontmatter 的一個欄位。
func setFrontmatter(t *testing.T, path, key, value string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:.*$`)
	line := key + ": " + value
	var out string
	if re.Match(data) {
		out = string(re.ReplaceAllLiteral(data, []byte(line)))
	} else {
		out = strings.Replace(string(data), "---\n", "---\n"+line+"\n", 1)
	}
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
}

// prepareITHome 把 fixture 家複製到 home，整份重寫 home.toml（不 patch 原檔），
// 啟用 code、life 兩位常駐，打開 ping。tasks、reminders 的 fixture 資料不需要，移掉，免得到期的舊待辦吵到真的 session。
func prepareITHome(t *testing.T, home, configDir, model string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(home, os.DirFS("testdata/home")); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"vault/tasks", "vault/reminders"} {
		if err := os.RemoveAll(filepath.Join(home, dir)); err != nil {
			t.Fatal(err)
		}
	}
	toml := fmt.Sprintf(`[user]
name = "測試使用者"
timezone = "Asia/Taipei"
[slots]
default = "life"
[slots.life]
label = "生活"
remind = "08:30"
check = "18:00"
[accounts]
default = "main"
[accounts.main]
engine = "claude"
engine_config = {config_dir = %q, tmux_socket = %q, isolate = true, permission_mode = "default"}
[web]
port = 18850
[sessions]
autostart = true
`, configDir, integrationTmuxSocket)
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	homefixture.FreeWebPort(t, home)
	for _, id := range []string{"code", "life"} {
		path := filepath.Join(home, "vault/agents", id, "agent.md")
		setFrontmatter(t, path, "enabled", "true")
		setFrontmatter(t, path, "mode", "resident")
		setFrontmatter(t, path, "account", "main")
		setFrontmatter(t, path, "model", model)
	}
	// ② 問 code「README 第一行是什麼」：家的根目錄放一份 README.md，問題才答得出來（不然 code 會去家外面到處找）。
	if err := os.WriteFile(filepath.Join(home, "README.md"), []byte(itReadmeFirstLine+"\n\n整合測試用的家。\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// P3 沒有執行中打開 job 的 API：在起 serve 之前就改成啟用。
	setFrontmatter(t, filepath.Join(home, "vault/agents/code/jobs/ping.md"), "enabled", "true")
}

type itEvent struct {
	Topic   string
	Payload map[string]any
	At      time.Time // 測試收到的時間
}

type itRun struct {
	tmp, home, logPath, settings string

	bus       sdk.BusAPI
	session   sdk.SessionAPI
	notify    sdk.NotifyAPI
	goals     sdk.GoalsAPI
	reminders sdk.RemindersAPI
	engine    sdk.EngineAPI

	mu     sync.Mutex
	events []itEvent
}

var itOwner = &sdk.Actor{Name: "owner(it)"}

// startIT 建置、複製家、起 serve、等 API。收尾（SIGINT、kill-server、雜湊比對）登記在 t.Cleanup。
func startIT(t *testing.T) *itRun {
	t.Helper()
	configDir, model := integrationConfig(t)
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("make", "build")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("make build 失敗：%v\n%s", err, out)
	}
	tmp, err := os.MkdirTemp("/tmp", "hg-it-")
	if err != nil {
		t.Fatal(err)
	}
	r := &itRun{tmp: tmp, home: filepath.Join(tmp, "home"), logPath: filepath.Join(tmp, "serve.log"), settings: filepath.Join(configDir, "settings.json")}
	settingsBefore := fileHash(r.settings)
	prepareITHome(t, r.home, configDir, model)

	logFile, err := os.Create(r.logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(repo, "bin", "evergenie"), "serve", "--home", r.home)
	cmd.Env = append(scrubbedEnv(), "EVERGENIE_ENGINE_BIN_DIR="+filepath.Join(repo, "bin"))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = cmd.Process.Signal(syscall.SIGINT)
		select {
		case err := <-exited:
			if err != nil {
				t.Errorf("serve 沒有乾淨結束：%v", err)
			}
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
			t.Errorf("serve 30 秒內沒有結束，已強制結束")
		}
		_ = exec.Command("tmux", "-L", integrationTmuxSocket, "kill-server").Run()
		_ = logFile.Close()
		if after := fileHash(r.settings); after != settingsBefore {
			t.Errorf("帳號的 settings.json 被改了：%s → %s", settingsBefore, after)
		}
		if t.Failed() {
			t.Logf("serve.log 最後 %d 行（完整檔案與測試家保留在 %s）：\n%s", itServeLogTail, tmp, r.logTail(itServeLogTail))
			return
		}
		_ = os.RemoveAll(tmp)
	})

	apiSocket := filepath.Join(r.home, "runtime", "sockets", "api.sock")
	if err := itWait(ctx, 60*time.Second, 250*time.Millisecond, "serve 的 API socket", func() (bool, error) {
		select {
		case err := <-exited:
			exited <- err
			return false, fmt.Errorf("serve 提早結束：%v", err)
		default:
		}
		info, err := os.Stat(apiSocket)
		return err == nil && info.Mode()&os.ModeSocket != 0, nil
	}); err != nil {
		t.Fatal(err)
	}
	c := sdk.Dial(apiSocket)
	r.bus, r.session, r.notify = sdk.NewBusClient(c), sdk.NewSessionClient(c), sdk.NewNotifyClient(c)
	r.goals, r.reminders = sdk.NewGoalsClient(c), sdk.NewRemindersClient(c)
	r.engine = sdk.NewEngineClient(sdk.Dial(filepath.Join(r.home, "runtime", "sockets", "engine-claude.sock")))
	go func() {
		_ = r.bus.Subscribe(ctx, &sdk.BusSubscribeRequest{Topics: []string{"session.state", "session.pending", "schedule.done"}}, func(m *sdk.BusSubscribeResponse) error {
			r.mu.Lock()
			r.events = append(r.events, itEvent{Topic: m.Topic, Payload: m.Payload, At: time.Now()})
			r.mu.Unlock()
			return nil
		})
	}()
	return r
}

func (r *itRun) logTail(n int) string {
	data, err := os.ReadFile(r.logPath)
	if err != nil {
		return err.Error()
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (r *itRun) logCount(s string) int {
	data, _ := os.ReadFile(r.logPath)
	return strings.Count(string(data), s)
}

// eventsSince 回測試在 since 之後收到的某個主題的事件。
func (r *itRun) eventsSince(topic string, since time.Time) []itEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []itEvent
	for _, e := range r.events {
		if e.Topic == topic && !e.At.Before(since) {
			out = append(out, e)
		}
	}
	return out
}

// itWait 每隔 every 呼叫 cond，直到成立、ctx 結束或 timeout。cond 回錯只記下來（暫時性錯誤不算失敗）。
func itWait(ctx context.Context, timeout, every time.Duration, what string, cond func() (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last error
	for {
		ok, err := cond()
		if ok {
			return nil
		}
		if err != nil {
			last = err
		}
		select {
		case <-ctx.Done():
			if last != nil {
				return fmt.Errorf("等不到%s（逾時 %s；最後一次錯誤：%v）", what, timeout, last)
			}
			return fmt.Errorf("等不到%s（逾時 %s）", what, timeout)
		case <-time.After(every):
		}
	}
}

func sameAgent(name, id string) bool { return name == id || strings.HasPrefix(name, id+"-") }

func hasStates(e *sdk.Envelope, want ...sdk.Lifecycle) (missing []sdk.Lifecycle) {
	seen := map[sdk.Lifecycle]bool{e.Lifecycle: true}
	for _, step := range e.History {
		seen[step.State] = true
	}
	for _, w := range want {
		if !seen[w] {
			missing = append(missing, w)
		}
	}
	return missing
}

// envelopesSince 列出 since 之後建立、符合 keep 的封包（不分收件人）。
func (r *itRun) envelopesSince(ctx context.Context, since time.Time, keep func(sdk.Envelope) bool) ([]sdk.Envelope, error) {
	resp, err := r.bus.List(ctx, &sdk.BusListRequest{})
	if err != nil {
		return nil, err
	}
	var out []sdk.Envelope
	for _, e := range resp.Envelopes {
		if !e.CreatedAt.Before(since) && keep(e) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *itRun) sessionState(ctx context.Context, name string) (*sdk.Session, error) {
	resp, err := r.session.Get(ctx, &sdk.SessionGetRequest{Name: name})
	if err != nil {
		return nil, err
	}
	return resp.Session, nil
}

// answerBootPrompts 第一次進新資料夾的信任對話框：選標籤含「信任」的選項（跟看板做的事一樣）。
func (r *itRun) answerBootPrompts(ctx context.Context, t *testing.T) error {
	list, err := r.engine.ListBootPrompts(ctx, &sdk.EngineListBootPromptsRequest{})
	if err != nil {
		return err
	}
	for _, p := range list.Prompts {
		if len(p.Choices) == 0 {
			continue
		}
		choice := p.Choices[0]
		trusted := false
		for _, c := range p.Choices {
			if strings.Contains(c, "信任") {
				choice, trusted = c, true
				break
			}
		}
		if !trusted {
			t.Logf("啟動對話框「%s」沒有含「信任」的選項，改選第一個：%q", p.Title, choice)
		}
		if _, err := r.engine.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: p.ID, Choice: choice, By: itOwner}); err != nil {
			return err
		}
		t.Logf("啟動對話框「%s」(%s) 選了 %q", p.Title, p.Session, choice)
	}
	return nil
}

// allowIncidentalPermissions 放行 code、life 附帶跳出的平台權限確認（例如 find 越出工作資料夾）：
// 那不是這一段要驗的，不放行的話對方會卡在 blocked 等 10 分鐘。⑥ 不用它。
func (r *itRun) allowIncidentalPermissions(ctx context.Context, t *testing.T) {
	for _, name := range []string{"code", "life"} {
		s, err := r.sessionState(ctx, name)
		if err != nil || s.Pending == nil || s.Pending.Kind != sdk.PendingKindPermission {
			continue
		}
		t.Logf("%s 有權限確認 %q，放行", name, s.Pending.Summary)
		_, _ = r.session.Resolve(ctx, &sdk.SessionResolveRequest{Session: name, PendingID: s.Pending.ID, Decision: &sdk.Decision{Allow: true}, By: itOwner})
	}
}

func (r *itRun) waitResidentsIdle(ctx context.Context, t *testing.T) error {
	return itWait(ctx, itBootTimeout, itPollEvery, "code、life 兩位常駐都 idle", func() (bool, error) {
		if err := r.answerBootPrompts(ctx, t); err != nil {
			t.Logf("處理啟動對話框：%v", err)
		}
		for _, name := range []string{"code", "life"} {
			s, err := r.sessionState(ctx, name)
			if err != nil {
				return false, err
			}
			if s.State != sdk.SessionStateIdle {
				return false, fmt.Errorf("%s 目前是 %s", name, s.State)
			}
		}
		return true, nil
	})
}

// section 跑一段（t.Run）：各自逾時；retry 的整項最多跑兩次（模型不照做時允許重試一次，兩次都失敗才算不過）。
func (r *itRun) section(t *testing.T, name string, timeout time.Duration, retry bool, fn func(ctx context.Context, t *testing.T) error) {
	t.Run(name, func(t *testing.T) {
		attempts := 1
		if retry {
			attempts = 2
		}
		var err error
		for i := 1; i <= attempts; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			err = fn(ctx, t)
			cancel()
			if err == nil {
				if i > 1 {
					t.Logf("第 %d 次才通過（第一次失敗的原因見上面）", i)
				}
				return
			}
			t.Logf("第 %d 次失敗：%v", i, err)
		}
		t.Fatal(err)
	})
}

// Guards: REQ-14, REQ-15, REQ-30, REQ-59, REQ-75, REQ-76, REQ-118, REQ-152, REQ-153, REQ-154
func TestIntegration(t *testing.T) {
	r := startIT(t)
	bootCtx, cancel := context.WithTimeout(context.Background(), itBootTimeout+30*time.Second)
	defer cancel()
	if err := r.waitResidentsIdle(bootCtx, t); err != nil {
		t.Fatal(err)
	}

	// ① 看板聊天經派送器貼上 → 封包 queued→notified→received→replied；session busy 再回 idle；Chat 讀得到回覆。
	r.section(t, "1-chat-via-dispatcher", itChatTimeout, false, r.chat)
	// ② 兩位秘書直接傳話；不用回的那則走到 replied 或 unanswered（unanswered 時寄件人收到 echo）。
	r.section(t, "2-direct-message", itChatTimeout, true, r.directMessage)
	// ③ 授權：帶 Goal 與沒帶 Goal 的委派寫檔都成功（傳話就是打字，不 deny）。
	r.section(t, "3-goal-authorization", itGoalTimeout, true, r.goalAuthorization)
	// ④ ping job → schedule.done{id: code/ping}。
	r.section(t, "4-schedule-ping", itPingTimeout, false, r.schedulePing)
	// ⑤ 1 分鐘後到期的待辦 → M-<n> → 按「做了」→ done。
	r.section(t, "5-reminder-due", itDueTimeout, false, r.reminderDue)
	// ⑥ 一次權限確認經 notify ＋ Resolve 回到 session。
	r.section(t, "6-permission-approval", itChatTimeout, false, r.permissionApproval)
}

func (r *itRun) chat(ctx context.Context, t *testing.T) error {
	start := time.Now()
	sent, err := r.bus.Send(ctx, &sdk.BusSendRequest{From: "owner(web)", To: "code", Kind: sdk.EnvelopeKindChat, Body: "回我一個字：好"})
	if err != nil {
		return err
	}
	id := sent.Envelope.ID
	var final *sdk.Envelope
	if err := itWait(ctx, itChatTimeout, itPollEvery/2, "封包 queued、notified、received、replied", func() (bool, error) {
		got, err := r.bus.Get(ctx, &sdk.BusGetRequest{ID: id})
		if err != nil {
			return false, err
		}
		final = got.Envelope
		if missing := hasStates(final, sdk.LifecycleQueued, sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleReplied); len(missing) > 0 {
			return false, fmt.Errorf("目前 %s，缺 %v", final.Lifecycle, missing)
		}
		return true, nil
	}); err != nil {
		return err
	}
	t.Logf("封包 %s 的生命週期：%+v", id, final.History)
	if err := itWait(ctx, 30*time.Second, itPollEvery, "session 先 busy 再回 idle", func() (bool, error) {
		sawBusy := false
		for _, e := range r.eventsSince("session.state", start) {
			if e.Payload["name"] != "code" {
				continue
			}
			switch e.Payload["state"] {
			case string(sdk.SessionStateBusy):
				sawBusy = true
			case string(sdk.SessionStateIdle):
				if sawBusy {
					return true, nil
				}
			}
		}
		return false, fmt.Errorf("busy=%v", sawBusy)
	}); err != nil {
		return err
	}
	return itWait(ctx, 30*time.Second, itPollEvery, "session.Chat 讀得到含「好」的回覆", func() (bool, error) {
		found := false
		err := r.session.Chat(ctx, &sdk.SessionChatRequest{Session: "code"}, func(m *sdk.SessionChatResponse) error {
			if msg := m.Message; msg != nil && msg.Kind == sdk.ChatKindAssistant && msg.Ts.After(start.Add(-2*time.Second)) && strings.Contains(msg.Text, "好") {
				found = true
			}
			return nil
		})
		return found, err
	})
}

func (r *itRun) directMessage(ctx context.Context, t *testing.T) error {
	start := time.Now()
	if _, err := r.bus.Send(ctx, &sdk.BusSendRequest{From: "owner(web)", To: "life", Kind: sdk.EnvelopeKindChat, Body: "請直接傳話給 code，問它 README 第一行是什麼，等它回覆後告訴我"}); err != nil {
		return err
	}
	isDirect := func(e sdk.Envelope) bool {
		return e.Route == sdk.EnvelopeRouteDirect && sameAgent(e.From, "life") && sameAgent(e.To, "code")
	}
	var first sdk.Envelope
	if err := itWait(ctx, 2*time.Minute, itPollEvery, "life → code 的直接傳話走到 replied", func() (bool, error) {
		r.allowIncidentalPermissions(ctx, t)
		list, err := r.envelopesSince(ctx, start, isDirect)
		if err != nil || len(list) == 0 {
			return false, fmt.Errorf("還沒有直接傳話的封包 %v", err)
		}
		first = list[0]
		return first.Lifecycle == sdk.LifecycleReplied, fmt.Errorf("封包 %s 目前 %s", first.ID, first.Lifecycle)
	}); err != nil {
		return err
	}
	t.Logf("直接傳話封包 %s：%+v", first.ID, first.History)

	second := time.Now()
	if _, err := r.bus.Send(ctx, &sdk.BusSendRequest{From: "owner(web)", To: "life", Kind: sdk.EnvelopeKindChat, Body: "請直接傳話給 code 說『這則不用回』"}); err != nil {
		return err
	}
	var outcome sdk.Envelope
	if err := itWait(ctx, 90*time.Second, itPollEvery, "第二則直接傳話走到 replied 或 unanswered", func() (bool, error) {
		r.allowIncidentalPermissions(ctx, t)
		list, err := r.envelopesSince(ctx, second, isDirect)
		if err != nil || len(list) == 0 {
			return false, fmt.Errorf("還沒有第二則直接傳話的封包 %v", err)
		}
		outcome = list[0]
		return outcome.Lifecycle == sdk.LifecycleReplied || outcome.Lifecycle == sdk.LifecycleUnanswered, fmt.Errorf("封包 %s 目前 %s", outcome.ID, outcome.Lifecycle)
	}); err != nil {
		return err
	}
	t.Logf("第二則直接傳話封包 %s 結果：%s", outcome.ID, outcome.Lifecycle)
	if outcome.Lifecycle == sdk.LifecycleUnanswered {
		return itWait(ctx, 60*time.Second, itPollEvery, "life 收到 echo 封包", func() (bool, error) {
			list, err := r.envelopesSince(ctx, second, func(e sdk.Envelope) bool { return e.Kind == sdk.EnvelopeKindEcho && sameAgent(e.To, "life") })
			return len(list) > 0, err
		})
	}
	return nil
}

func (r *itRun) goalAuthorization(ctx context.Context, t *testing.T) error {
	goalFile := filepath.Join(r.home, "it-goal.txt")
	noGoalFile := filepath.Join(r.home, "it-nogoal.txt")
	_ = os.Remove(goalFile)
	_ = os.Remove(noGoalFile)

	proposed, err := r.goals.Propose(ctx, &sdk.GoalsProposeRequest{Criteria: "測試驗收條件", Title: "整合測試", Text: "在家的根目錄建立 it-goal.txt", Agent: "life", By: &sdk.Actor{Name: "life"}})
	if err != nil {
		return err
	}
	goal := proposed.Goal
	// 計畫寫「notify.List 找 approve 內文含 GOAL id 的一則」；確認通知的內文沒有 GOAL id，Propose 的回應有 goal.Notice，直接用它。
	ask, err := r.notify.Get(ctx, &sdk.NotifyGetRequest{ID: goal.Notice})
	if err != nil {
		return err
	}
	if ask.Notification.Category != sdk.CategoryApprove {
		return fmt.Errorf("Goal 的確認通知 %s 的分類是 %s，不是 approve", goal.Notice, ask.Notification.Category)
	}
	if _, err := r.notify.Answer(ctx, &sdk.NotifyAnswerRequest{ID: goal.Notice, Text: "確認", Via: sdk.AnswerViaButton, By: itOwner}); err != nil {
		return err
	}
	if err := itWait(ctx, 30*time.Second, time.Second, goal.ID+" 變成 active", func() (bool, error) {
		got, err := r.goals.Get(ctx, &sdk.GoalsGetRequest{ID: goal.ID})
		if err != nil {
			return false, err
		}
		return got.Goal.State == sdk.GoalStateActive, fmt.Errorf("目前 %s", got.Goal.State)
	}); err != nil {
		return err
	}

	if _, err := r.bus.Send(ctx, &sdk.BusSendRequest{From: "owner(web)", To: "life", Kind: sdk.EnvelopeKindChat, Body: "主人的 Goal " + goal.ID + "：請直接傳話給 code 照做"}); err != nil {
		return err
	}
	if err := itWait(ctx, 3*time.Minute, itPollEvery, "家的根目錄出現 it-goal.txt", func() (bool, error) {
		if _, err := os.Stat(goalFile); err == nil {
			return true, nil
		}
		// 預設權限模式下寫檔可能跳出平台的確認框：那不是這一段要驗的，放行它。
		r.allowIncidentalPermissions(ctx, t)
		return false, nil
	}); err != nil {
		return err
	}

	// 傳話就是打字給對方（法律第 4 條 v1.4，DEV-92）：沒帶 Goal 的委派一般動作照常放行，不會被 deny。
	deniedBefore := r.logCount(itDenyLogLine)
	if _, err := r.bus.Send(ctx, &sdk.BusSendRequest{From: "owner(web)", To: "life", Kind: sdk.EnvelopeKindChat, Body: "請直接傳話給 code，請它在家的根目錄建立 it-nogoal.txt"}); err != nil {
		return err
	}
	if err := itWait(ctx, 3*time.Minute, itPollEvery, "沒帶 Goal 的傳話：家的根目錄出現 it-nogoal.txt", func() (bool, error) {
		if _, err := os.Stat(noGoalFile); err == nil {
			return true, nil
		}
		r.allowIncidentalPermissions(ctx, t)
		return false, nil
	}); err != nil {
		return err
	}
	if n := r.logCount(itDenyLogLine); n > deniedBefore {
		return fmt.Errorf("沒帶 Goal 的傳話不該被 deny，但 serve.log 的「%s」多了 %d 行", itDenyLogLine, n-deniedBefore)
	}
	return nil
}

func (r *itRun) schedulePing(ctx context.Context, t *testing.T) error {
	start := time.Now()
	return itWait(ctx, itPingTimeout, itPollEvery, "schedule.done{id: code/ping}", func() (bool, error) {
		for _, e := range r.eventsSince("schedule.done", start) {
			if e.Payload["id"] == "code/ping" {
				return true, nil
			}
		}
		return false, nil
	})
}

func (r *itRun) reminderDue(ctx context.Context, t *testing.T) error {
	created, err := r.reminders.Create(ctx, &sdk.RemindersCreateRequest{Content: "整合測試：喝水", Follow: sdk.FollowRemind, At: time.Now().Add(time.Minute), Owner: "code", By: itOwner})
	if err != nil {
		return err
	}
	rem := created.Reminder.ID
	var notice sdk.Notification
	if err := itWait(ctx, 2*time.Minute+30*time.Second, itPollEvery, "待辦 "+rem+" 的 due 通知 M-<n>", func() (bool, error) {
		list, err := r.notify.List(ctx, &sdk.NotifyListRequest{})
		if err != nil {
			return false, err
		}
		for _, n := range list.Notifications {
			if n.Category == sdk.CategoryDue && n.Reminder == rem {
				notice = n
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		return err
	}
	t.Logf("待辦 %s 到期，通知 %s", rem, notice.ID)
	if _, err := r.notify.Answer(ctx, &sdk.NotifyAnswerRequest{ID: notice.ID, Text: "做了", Via: sdk.AnswerViaButton, By: itOwner}); err != nil {
		return err
	}
	return itWait(ctx, 60*time.Second, itPollEvery, "待辦 "+rem+" 變成 done", func() (bool, error) {
		got, err := r.reminders.Get(ctx, &sdk.RemindersGetRequest{ID: rem})
		if err != nil {
			return false, err
		}
		return got.Reminder.Status == sdk.ReminderStatusDone, fmt.Errorf("目前 %s", got.Reminder.Status)
	})
}

func (r *itRun) permissionApproval(ctx context.Context, t *testing.T) error {
	file := filepath.Join(r.home, "it-approval")
	_ = os.Remove(file)
	start := time.Now()
	if _, err := r.bus.Send(ctx, &sdk.BusSendRequest{From: "owner(web)", To: "code", Kind: sdk.EnvelopeKindChat, Body: "請在目前資料夾執行 touch it-approval"}); err != nil {
		return err
	}
	var pending *sdk.Pending
	var ask sdk.Notification
	if err := itWait(ctx, 2*time.Minute, itPollEvery, "code 有權限確認（pending）和 approve 通知", func() (bool, error) {
		s, err := r.sessionState(ctx, "code")
		if err != nil {
			return false, err
		}
		if s.Pending == nil {
			if _, err := os.Stat(file); err == nil {
				return false, fmt.Errorf("it-approval 沒經過確認就出現了（帳號的規則直接放行了這個動作，換一個會被問的指令或帳號）")
			}
			return false, fmt.Errorf("還沒有 pending（session %s）", s.State)
		}
		pending = s.Pending
		list, err := r.notify.List(ctx, &sdk.NotifyListRequest{Agent: "code"})
		if err != nil {
			return false, err
		}
		for _, n := range list.Notifications {
			if n.Category == sdk.CategoryApprove && n.CreatedAt.After(start.Add(-2*time.Second)) && n.State != sdk.NotificationStateAnswered {
				ask = n
				return true, nil
			}
		}
		return false, fmt.Errorf("有 pending %q 但還沒有 approve 通知", s.Pending.Summary)
	}); err != nil {
		return err
	}
	t.Logf("權限確認 %s（%s）→ 通知 %s", pending.ID, pending.Summary, ask.ID)
	if _, err := r.notify.Answer(ctx, &sdk.NotifyAnswerRequest{ID: ask.ID, Text: "允許", Via: sdk.AnswerViaButton, By: itOwner}); err != nil {
		return err
	}
	return itWait(ctx, time.Minute, itPollEvery, "it-approval 出現且 pending 清空", func() (bool, error) {
		if _, err := os.Stat(file); err != nil {
			return false, err
		}
		s, err := r.sessionState(ctx, "code")
		if err != nil {
			return false, err
		}
		return s.Pending == nil, fmt.Errorf("pending 還在")
	})
}

// TestIntegrationSessionsCLI（資料夾用測試家：常駐已經信任過，換新資料夾會多一個啟動對話框）：用 CLI（跟 evergenie sessions … 同一份程式）對真的 Claude 開一個專屬對話、peek、attach、close（Q2）。
// Guards: REQ-72, REQ-74
func TestIntegrationSessionsCLI(t *testing.T) {
	r := startIT(t)
	ctx, cancel := context.WithTimeout(context.Background(), itBootTimeout+itChatTimeout)
	defer cancel()
	if err := r.waitResidentsIdle(ctx, t); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(r.home, "runtime", "sockets", "api.sock")
	out := hgOK(t, sock, "sessions", "open", "code", "--repo", r.home, "--topic", "it-cli", "--first", "回我一個字：好")
	if !strings.Contains(out, "已開 code-it-cli") || !strings.Contains(out, " -L "+integrationTmuxSocket+" attach") {
		t.Fatalf("open：%s", out)
	}
	if err := itWait(ctx, itChatTimeout, itPollEvery, "peek 看得到回覆", func() (bool, error) {
		// 新資料夾第一次開會跳「要信任它嗎？」：跟常駐一樣代答。
		if err := r.answerBootPrompts(ctx, t); err != nil {
			t.Logf("處理啟動對話框：%v", err)
		}
		code, peek, errOut := hg(t, sock, "", "sessions", "peek", "code-it-cli", "--lines", "5")
		if code != 0 {
			return false, fmt.Errorf("peek：%s", errOut)
		}
		return strings.Contains(peek, "秘書：") && strings.Contains(peek, "好"), nil
	}); err != nil {
		t.Fatal(err)
	}
	if attach := hgOK(t, sock, "sessions", "attach", "code-it-cli"); !strings.Contains(attach, "attach -t") {
		t.Fatalf("attach：%q", attach)
	}
	if opened := hgOK(t, sock, "sessions", "opened"); !strings.Contains(opened, "code-it-cli") || !strings.Contains(opened, r.home) {
		t.Fatalf("opened：%s", opened)
	}
	hgOK(t, sock, "sessions", "close", "code-it-cli")
	if err := itWait(ctx, 30*time.Second, itPollEvery, "close 之後狀態是 stopped", func() (bool, error) {
		g, err := r.session.Get(ctx, &sdk.SessionGetRequest{Name: "code-it-cli"})
		return err == nil && g.Session.State == sdk.SessionStateStopped, err
	}); err != nil {
		t.Fatal(err)
	}
}
