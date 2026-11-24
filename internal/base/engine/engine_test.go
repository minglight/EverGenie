package engine

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/conformance"
	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/internal/base/engine/registry"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func home(t *testing.T, id string) string {
	t.Helper()
	dir := t.TempDir()
	data := fmt.Sprintf(`[user]
name="測試"
timezone="Asia/Taipei"
[slots]
default="life"
[slots.life]
label="生活"
remind="08:30"
check="18:00"
[accounts]
default="one"
[accounts.one]
engine=%q
[accounts.one.engine_config]
opaque="unchanged"
[accounts.two]
engine=%q
[web]
port=8850
`, id, id)
	check(t, os.WriteFile(filepath.Join(dir, "home.toml"), []byte(data), 0600))
	return dir
}
func supervised(t *testing.T) (*Module, *fake.Engine, *platform.FakeClock, *int, func()) {
	t.Helper()
	id, binary := "", ""
	for k, v := range registry.Executables() {
		id, binary = k, v
		break
	}
	dir := home(t, id)
	bin := t.TempDir()
	check(t, os.WriteFile(filepath.Join(bin, binary), nil, 0700))
	t.Setenv("EVERGENIE_ENGINE_BIN_DIR", bin)
	clock := platform.NewFakeClock(time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC))
	m := New(Deps{Clock: clock})
	m.SetTrustSource(testPolicy)
	m.SetHome(dir)
	m.manual = true
	api := fake.New(nil)
	m.socketExists = func(string) bool { return true }
	m.dial = func(string) sdk.EngineAPI { return api }
	count := 0
	var exit func()
	m.launch = func(ctx context.Context, path string, env []string) (*process, error) {
		count++
		done := make(chan error, 1)
		var once sync.Once
		exit = func() { once.Do(func() { done <- nil; close(done) }) }
		stop := exit
		if path != filepath.Join(bin, binary) {
			t.Error("wrong executable")
		}
		joined := strings.Join(env, "\n")
		for _, key := range []string{"EVERGENIE_HOME=", "EVERGENIE_API_SOCKET=", "EVERGENIE_ENGINE_SOCKET="} {
			if !strings.Contains(joined, key) {
				t.Error("missing environment", key)
			}
		}
		return &process{done: done, kill: func() error { stop(); return nil }}, nil
	}
	check(t, m.Start(context.Background()))
	t.Cleanup(func() { check(t, m.Stop(context.Background())) })
	return m, api, clock, &count, func() { exit() }
}

// Guards: REQ-83
func TestRegistryPicksByAccount(t *testing.T) {
	a, b := fake.New(nil), fake.New(nil)
	r := NewStatic(map[string]sdk.EngineAPI{"one": a, "two": b})
	e, err := r.For("two")
	check(t, err)
	_, err = e.Start(context.Background(), &sdk.EngineStartRequest{Name: "test"})
	check(t, err)
	if len(a.Calls()) != 0 || b.Calls()[0].Request.(*sdk.EngineStartRequest).Account != "two" {
		t.Fatal("wrong account")
	}
	m, _, _, _, _ := supervised(t)
	if _, err = m.For("one"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.For("two"); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-83
func TestUnknownEngineErrors(t *testing.T) {
	m := New(Deps{})
	m.SetTrustSource(testPolicy)
	m.manual = true
	m.SetHome(home(t, "unknown-test-engine"))
	check(t, m.Start(context.Background()))
	defer m.Stop(context.Background())
	if _, err := m.For("one"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatal(err)
	}
	if _, err := NewStatic(nil).For("missing"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatal(err)
	}
}

// Guards: REQ-144
func TestEngineConfigPassThrough(t *testing.T) {
	m, api, _, _, _ := supervised(t)
	e, err := m.For("one")
	check(t, err)
	req := &sdk.EngineStartRequest{Name: "test", Instructions: "原文"}
	_, err = e.Start(context.Background(), req)
	check(t, err)
	calls := api.Calls()
	got := calls[len(calls)-1].Request.(*sdk.EngineStartRequest)
	if !reflect.DeepEqual(got.EngineConfig, map[string]any{"opaque": "unchanged"}) || got.Instructions != req.Instructions || req.EngineConfig != nil {
		t.Fatal(got, req)
	}
}

// Guards: REQ-87
func TestSupervisorRestartsEngine(t *testing.T) {
	m, _, clock, count, exit := supervised(t)
	for i := 0; i < 6; i++ {
		exit()
		m.Tick(context.Background(), clock.Now())
		if _, err := m.For("one"); platform.CodeOf(err) != sdk.ErrorCodeUnavailable {
			t.Fatal(err)
		}
		clock.Advance(4 * time.Second)
		m.Tick(context.Background(), clock.Now())
		if *count != i+1 {
			t.Fatal("early restart", *count)
		}
		clock.Advance(time.Second)
		m.Tick(context.Background(), clock.Now())
	}
	if *count != 6 {
		t.Fatal("restart cap", *count)
	}
}

// Guards: REQ-84
func TestSetPermissionOnReady(t *testing.T) {
	m, api, clock, _, exit := supervised(t)
	policies := func() int {
		n := 0
		for _, c := range api.Calls() {
			if c.Method == "SetPermission" {
				n++
				p := c.Request.(*sdk.EngineSetPermissionRequest)
				if p.Policy.Mode != sdk.PermissionModeAsk {
					t.Fatal(p)
				}
			}
		}
		return n
	}
	if policies() != 2 {
		t.Fatal("must configure both accounts")
	}
	m.Tick(context.Background(), clock.Now())
	if policies() != 2 {
		t.Fatal("repeated setup")
	}
	exit()
	m.Tick(context.Background(), clock.Now())
	clock.Advance(5 * time.Second)
	m.Tick(context.Background(), clock.Now())
	if policies() != 4 {
		t.Fatal("restart must configure accounts again")
	}
}

// Guards: REQ-84
func TestPermissionPolicyHasNoVendorFields(t *testing.T) {
	words, err := os.ReadFile("../../tools/enginecheck/vendor_terms.txt")
	check(t, err)
	typ := reflect.TypeOf(sdk.PermissionPolicy{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		for _, word := range strings.Fields(string(words)) {
			if strings.EqualFold(name, word) {
				t.Fatal("non-neutral permission field", name)
			}
		}
	}

}

var (
	termsOnce     sync.Once
	termsFindings []string
	termsErr      error
)

// checkTerms scans the core once per test binary (both REQ tests share the
// result) and skips files and lines that cannot match before running the
// regexp, which is ~30x slower under -race.
func checkTerms(t *testing.T) {
	t.Helper()
	termsOnce.Do(func() { termsFindings, termsErr = scanTerms("../../..") })
	check(t, termsErr)
	for _, finding := range termsFindings {
		t.Error(finding)
	}
}
func scanTerms(root string) ([]string, error) {
	data, err := os.ReadFile("../../tools/enginecheck/vendor_terms.txt")
	if err != nil {
		return nil, err
	}
	words, lowerWords := []string{}, []string{}
	for _, word := range strings.Fields(string(data)) {
		words = append(words, regexp.QuoteMeta(word))
		lowerWords = append(lowerWords, strings.ToLower(word))
	}
	if len(words) == 0 {
		return nil, fmt.Errorf("empty vocabulary")
	}
	mentions := func(text string) bool {
		lower := strings.ToLower(text)
		for _, word := range lowerWords {
			if strings.Contains(lower, word) {
				return true
			}
		}
		return false
	}
	pattern := regexp.MustCompile(`(?i)\b(` + strings.Join(words, "|") + `)\b`)
	split := regexp.MustCompile(`([a-z0-9])([A-Z])`)
	acronym := regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	var findings []string
	for _, dir := range []string{"internal", "sdk"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if d.Name() == "testdata" || rel == "internal/base/engine/registry" || rel == "internal/tools/enginecheck" {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".go" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !mentions(string(data)) {
				return nil
			}
			for i, line := range strings.Split(string(data), "\n") {
				if !mentions(line) {
					continue
				}
				normalized := split.ReplaceAllString(acronym.ReplaceAllString(strings.ReplaceAll(line, "_", " "), "$1 $2"), "$1 $2")
				if pattern.MatchString(line) || pattern.MatchString(normalized) {
					findings = append(findings, fmt.Sprintf("%s:%d contains non-neutral vocabulary", rel, i+1))
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return findings, nil
}

// Guards: REQ-87
func TestNoEngineNamesInCore(t *testing.T) { checkTerms(t) }

// Guards: REQ-144
func TestNoVendorTermsInCoreAndProto(t *testing.T) { checkTerms(t) }

// Guards: REQ-144, REQ-153, REQ-76, REQ-78, REQ-79, REQ-130
func TestConformanceFakeEngine(t *testing.T) {
	h := conformance.NewHost(t)
	e := fake.New(&orderedHost{Host: h, t: t})
	e.Configure(fake.Behavior{Delivered: true, Events: []sdk.EngineEvent{
		{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: conformance.Text},
		{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourcePeer, From: "other", Input: "原始委派", Interrupted: true},
		{Type: sdk.EngineEventTypeActivityStarted, DirectMessage: &sdk.DirectMessage{To: "other", Text: "回覆"}},
		{Type: sdk.EngineEventTypeActivityEnded, DirectMessage: &sdk.DirectMessage{To: "other", Text: "回覆", OK: true}},
		{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "完成"},
	}, Approvals: []sdk.EngineHostRequestApprovalRequest{{Kind: sdk.ApprovalKindPermission, Tool: "write", Summary: "測試確認"}, {Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "選哪個？"}}}}, Actions: []sdk.EngineHostCheckActionRequest{{Class: sdk.ActionClassWrite, Tool: "write", Summary: "測試動作"}}})
	conformance.Run(t, e, h.Events)
	results := e.Results()
	if len(results.Actions) != 1 || results.Actions[0].Verdict != sdk.ActionVerdictAsk || results.Actions[0].Reason != "測試確認理由" || len(results.Approvals) != 2 || !results.Approvals[0].Decision.Allow || results.Approvals[1].Decision.Answers["選哪個？"] != "測試答案" {
		t.Fatal(results)
	}
	for _, c := range e.Calls() {
		if c.Method == "Deliver" && c.Request.(*sdk.EngineDeliverRequest).Text != conformance.Text {
			t.Fatal("text changed")
		}
	}
}

// 出錯的那一輪：fake 引擎收到「【出錯】」報 TURN_STARTED→TURN_FAILED（overloaded＋平台原話），沒有 TURN_ENDED；
// 事件經過真的傳輸（unix socket）送到核心那一端，error 欄位原樣到（S5-0）。
func TestConformanceTurnFailedFakeEngine(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "hg-")
	check(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "api.sock")
	h := conformance.NewEventHost()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = sdk.Serve(ctx, socket, map[string]any{"engine_host": h}) }()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if info, err := os.Stat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("socket 沒有出現")
		}
	}
	e := fake.NewScriptedContext(ctx, sdk.NewEngineHostClient(sdk.Dial(socket)))
	got := conformance.TurnFailed(t, e, h.Events, "請做 "+fake.MarkFail)
	if got.Error != sdk.EngineErrorKindOverloaded || got.LastMessage != "假引擎：這一輪出錯了" || got.Session != "conformance-failed" {
		t.Fatalf("經過 socket 之後 TURN_FAILED 的內容要原樣到：%+v", got)
	}
}

// 不認得的錯誤種類（含空的）一律當 unknown，列出來的照原樣（S5-0）。
func TestEngineErrorKindNormalize(t *testing.T) {
	for in, want := range map[sdk.EngineErrorKind]sdk.EngineErrorKind{
		sdk.EngineErrorKindOverloaded: sdk.EngineErrorKindOverloaded, sdk.EngineErrorKindAuth: sdk.EngineErrorKindAuth,
		sdk.EngineErrorKindBilling: sdk.EngineErrorKindBilling, sdk.EngineErrorKindRejected: sdk.EngineErrorKindRejected,
		sdk.EngineErrorKindUnknown: sdk.EngineErrorKindUnknown, "": sdk.EngineErrorKindUnknown, "quota_exploded": sdk.EngineErrorKindUnknown,
	} {
		if got := in.Normalize(); got != want {
			t.Errorf("%q.Normalize() = %q，要 %q", in, got, want)
		}
	}
}

type failingAPI struct{ sdk.EngineAPI }

func (f failingAPI) Deliver(context.Context, *sdk.EngineDeliverRequest) (*sdk.EngineDeliverResponse, error) {
	return nil, &sdk.Error{Code: sdk.ErrorCodeForbidden, Field: "text", Message: "不允許這個動作"}
}

// Guards: REQ-87, REQ-144
func TestAdapterErrorsAndTranscript(t *testing.T) {
	ctx := context.Background()
	api := fake.New(nil)
	r := NewStatic(map[string]sdk.EngineAPI{"test": api, "failed": failingAPI{api}})
	failed, err := r.For("failed")
	check(t, err)
	_, _, err = failed.Deliver(ctx, "test", "id", "原文")
	if platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal(err)
	}
	e, err := r.For("test")
	check(t, err)
	_, err = e.Start(ctx, &sdk.EngineStartRequest{Name: "test"})
	check(t, err)
	ok, reason, err := e.Deliver(ctx, "test", "id", "原文\n第二行")
	check(t, err)
	if !ok || reason != "" {
		t.Fatal(ok, reason)
	}
	ch, err := e.Transcript(ctx, "test", "", false)
	check(t, err)
	var messages []*sdk.ChatMessage
	for message := range ch {
		messages = append(messages, message)
	}
	if len(messages) != 1 || messages[0].Text != "原文\n第二行" {
		t.Fatal(messages)
	}
	api.Configure(fake.Behavior{Delivered: false, Reason: "busy"})
	ok, reason, err = e.Deliver(ctx, "test", "id2", "第二則")
	check(t, err)
	if ok || reason != "busy" {
		t.Fatal(ok, reason)
	}
	_, err = e.Capabilities(ctx)
	check(t, err)
	_, err = e.BootPrompts(ctx, "test")
	check(t, err)
	check(t, e.AnswerBootPrompt(ctx, "boot", "yes", "owner(test)"))
	_, err = e.Usage(ctx, "test")
	check(t, err)
	_, err = e.SetPermission(ctx, &sdk.PermissionPolicy{Mode: sdk.PermissionModeAsk}, "test")
	check(t, err)
	check(t, e.Stop(ctx, "test"))
}

type permissionFailure struct {
	sdk.EngineAPI
	calls int
}

func (f *permissionFailure) SetPermission(context.Context, *sdk.EngineSetPermissionRequest) (*sdk.EngineSetPermissionResponse, error) {
	f.calls++
	return nil, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "權限設定不正確"}
}

// Guards: REQ-84
func TestPermissionFailureDoesNotRetryOrExposeEngine(t *testing.T) {
	m, api, clock, _, exit := supervised(t)
	failure := &permissionFailure{EngineAPI: api}
	m.dial = func(string) sdk.EngineAPI { return failure }
	exit()
	m.Tick(context.Background(), clock.Now())
	clock.Advance(5 * time.Second)
	m.Tick(context.Background(), clock.Now())
	m.Tick(context.Background(), clock.Now())
	if failure.calls != 2 {
		t.Fatal("each account must be attempted once", failure.calls)
	}
	if _, err := m.For("one"); platform.CodeOf(err) != sdk.ErrorCodeUnavailable {
		t.Fatal(err)
	}
}

type orderedHost struct {
	*conformance.Host
	t       *testing.T
	started bool
}

func (h *orderedHost) ReportEvent(ctx context.Context, r *sdk.EngineHostReportEventRequest) (*sdk.EngineHostReportEventResponse, error) {
	if r.Event.Type == sdk.EngineEventTypeTurnStarted {
		h.started = true
	}
	return h.Host.ReportEvent(ctx, r)
}
func (h *orderedHost) CheckAction(ctx context.Context, r *sdk.EngineHostCheckActionRequest) (*sdk.EngineHostCheckActionResponse, error) {
	if !h.started {
		h.t.Error("fake checked an action before reporting its turn")
	}
	return h.Host.CheckAction(ctx, r)
}

// TestAskThroughAdapter：轉接層補上帳號與帳號設定；一次性對話不開 session、不留任何對話；錯誤轉成平台錯誤。
// Guards: REQ-130
func TestAskThroughAdapter(t *testing.T) {
	ctx := context.Background()
	api := fake.New(nil)
	r := NewStatic(map[string]sdk.EngineAPI{"acct": api})
	e, err := r.For("acct")
	check(t, err)
	got, err := e.Ask(ctx, &sdk.EngineAskRequest{Agent: "life", Instructions: "指令", Message: "哈囉", Model: "haiku"})
	check(t, err)
	if got.Reply != "[fake] 哈囉" || got.Model != "haiku" {
		t.Fatalf("假引擎的回覆：%+v", got)
	}
	calls := api.Calls()
	if len(calls) != 1 || calls[0].Method != "Ask" || calls[0].Request.(*sdk.EngineAskRequest).Account != "acct" {
		t.Fatalf("轉接層要補上帳號：%+v", calls)
	}
	if _, err := e.Ask(ctx, nil); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒給內容要回 invalid：%v", err)
	}
	api.Configure(fake.Behavior{AskError: "引擎壞了"})
	if _, err := e.Ask(ctx, &sdk.EngineAskRequest{Message: "x"}); platform.CodeOf(err) != sdk.ErrorCodeUnavailable {
		t.Fatalf("引擎的錯誤要原樣轉成平台錯誤：%v", err)
	}
	api.Configure(fake.Behavior{AskFailOn: "壞"})
	if _, err := e.Ask(ctx, &sdk.EngineAskRequest{Message: "一則壞的"}); err == nil {
		t.Fatal("AskFailOn 要失敗")
	}
	if got, err := e.Ask(ctx, &sdk.EngineAskRequest{Message: "好的"}); err != nil || got.Reply == "" {
		t.Fatalf("其他則照跑：%+v %v", got, err)
	}
}

// 對話指令（ENG-17）：原樣轉給引擎，done、原因、新的對話編號原樣回來；session 沒開著假引擎回 gone。
func TestCommandThroughAdapter(t *testing.T) {
	ctx := context.Background()
	api := fake.New(nil)
	e, err := NewStatic(map[string]sdk.EngineAPI{"acct": api}).For("acct")
	check(t, err)
	if r, err := e.Command(ctx, "code", sdk.SessionCommandClear, "", "owner(web)"); err != nil || r.Done || r.Reason != "gone" {
		t.Fatalf("沒開著要回 gone：%+v %v", r, err)
	}
	_, err = api.Start(ctx, &sdk.EngineStartRequest{Name: "code", Agent: "code"})
	check(t, err)
	r, err := e.Command(ctx, "code", sdk.SessionCommandModel, "opus", "owner(web)")
	if err != nil || !r.Done || r.Reason != "" {
		t.Fatalf("假引擎做成：%+v %v", r, err)
	}
	calls := api.Calls()
	req := calls[len(calls)-1].Request.(*sdk.EngineCommandRequest)
	if calls[len(calls)-1].Method != "Command" || req.Session != "code" || req.Command != sdk.SessionCommandModel || req.Arg != "opus" || req.By.Name != "owner(web)" {
		t.Fatalf("原樣轉過去：%+v", req)
	}
	api.Configure(fake.Behavior{CommandReason: sdk.CommandDialog})
	if r, err := e.Command(ctx, "code", sdk.SessionCommandClear, "", "owner(web)"); err != nil || r.Done || r.Reason != sdk.CommandDialog {
		t.Fatalf("原因原樣回來：%+v %v", r, err)
	}
}

// 逾時後替主人在畫面上回答（ENG-14）：轉接層把 session、確認、決定、誰按的原樣交給引擎，引擎的原因原樣回來。
// Guards: REQ-377
func TestAnswerApprovalThroughAdapter(t *testing.T) {
	ctx := context.Background()
	api := fake.New(nil)
	e, err := NewStatic(map[string]sdk.EngineAPI{"acct": api}).For("acct")
	check(t, err)
	p := &sdk.Pending{ID: "p1", Kind: sdk.PendingKindPermission, Summary: "寫入"}
	answered, reason, err := e.AnswerApproval(ctx, "code", p, &sdk.Decision{Allow: true}, "owner(web)")
	if err != nil || !answered || reason != "" {
		t.Fatalf("假引擎答成功：%v %q %v", answered, reason, err)
	}
	calls := api.Calls()
	if len(calls) != 1 || calls[0].Method != "AnswerApproval" {
		t.Fatalf("要記呼叫：%+v", calls)
	}
	req := calls[0].Request.(*sdk.EngineAnswerApprovalRequest)
	if req.Session != "code" || req.Pending.ID != "p1" || !req.Decision.Allow || req.By.Name != "owner(web)" {
		t.Fatalf("原樣轉過去：%+v", req)
	}
	api.Configure(fake.Behavior{AnswerReason: sdk.AnswerNone})
	if answered, reason, err := e.AnswerApproval(ctx, "code", p, &sdk.Decision{}, "owner(web)"); err != nil || answered || reason != sdk.AnswerNone {
		t.Fatalf("原因原樣回來：%v %q %v", answered, reason, err)
	}
}

// TestEnvironmentCarriesLayout（DEV-145）：引擎行程拿到家的佈局；第 2 版的 socket 在狀態資料夾，外面帶進來的同名變數換掉。
func TestEnvironmentCarriesLayout(t *testing.T) {
	dir, state := t.TempDir(), t.TempDir()
	check(t, os.WriteFile(filepath.Join(dir, "home.toml"), []byte("[layout]\nversion = 2\nstate = \""+state+"\"\n"), 0o600))
	t.Setenv("EVERGENIE_STATE", "/somewhere/else")
	t.Setenv("EVERGENIE_BUNDLED_PERSONAS", "/somewhere/cards")
	m := New(Deps{})
	m.SetHome(dir)
	env := m.environment("/e.sock")
	for _, v := range env {
		if strings.HasPrefix(v, "EVERGENIE_BUNDLED_PERSONAS=") {
			t.Errorf("跟著程式的公開卡資料夾只給套件，引擎不帶：%s", v)
		}
	}
	want := map[string]string{
		"EVERGENIE_HOME":              dir,
		"EVERGENIE_API_SOCKET":        filepath.Join(state, "runtime", "sockets", "api.sock"),
		"EVERGENIE_ENGINE_SOCKET":     "/e.sock",
		"EVERGENIE_STATE":             state,
		"EVERGENIE_MEDIA":             filepath.Join(dir, "media"),
		"EVERGENIE_OFFICIAL_PERSONAS": filepath.Join(state, "personas"),
	}
	seen := map[string]int{}
	for _, v := range env {
		name, value, _ := strings.Cut(v, "=")
		if w, ok := want[name]; ok {
			seen[name]++
			if value != w {
				t.Errorf("%s = %s，要 %s", name, value, w)
			}
		}
	}
	for name := range want {
		if seen[name] != 1 {
			t.Errorf("%s 出現 %d 次", name, seen[name])
		}
	}
}
