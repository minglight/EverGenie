package session

import (
	"context"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 主人的對話指令（SES-59、D-45）：只有主人能下、指令名稱與參數格式在核心擋（選項認不認得是引擎的事）、清掉與整理在她忙或等確認時不下，
// 不進訊息佇列；成功記在 Session.last_command、發 session.command。

func (f *fixture) commandCalls() []*sdk.EngineCommandRequest {
	var out []*sdk.EngineCommandRequest
	for _, c := range f.e.Calls() {
		if c.Method == "Command" {
			out = append(out, c.Request.(*sdk.EngineCommandRequest))
		}
	}
	return out
}

func idleSession(t *testing.T) *fixture {
	t.Helper()
	f := setup(t)
	f.open(t, "code")
	f.event(t, sdk.EngineEventTypeSessionStarted)
	f.state(t, sdk.SessionStateIdle)
	return f
}

func TestCommandGoesToEngineAndIsRecorded(t *testing.T) {
	f := idleSession(t)
	note, err := f.m.Command(context.Background(), "code", sdk.SessionCommandModel, " opus ", "owner(web)")
	must(t, err)
	if note.Command != sdk.SessionCommandModel || note.Arg != "opus" || note.By != "owner(web)" || note.At.IsZero() {
		t.Fatalf("紀錄：%+v", note)
	}
	calls := f.commandCalls()
	if len(calls) != 1 || calls[0].Session != "code" || calls[0].Command != sdk.SessionCommandModel || calls[0].Arg != "opus" || calls[0].By.Name != "owner(web)" {
		t.Fatalf("要交給引擎：%+v", calls)
	}
	s, err := f.m.Get(context.Background(), "code")
	must(t, err)
	if s.LastCommand == nil || s.LastCommand.Command != sdk.SessionCommandModel || s.LastCommand.Arg != "opus" {
		t.Fatalf("Session.last_command 要記下來：%+v", s.LastCommand)
	}
	published := false
	for _, ev := range f.b.published() {
		if ev.Topic == "session.command" && ev.Payload["name"] == "code" && ev.Payload["command"] == "model" {
			published = true
		}
	}
	if !published {
		t.Fatal("要發 session.command")
	}
	if len(f.b.sent) != 0 {
		t.Fatalf("指令不是訊息，不能進佇列：%+v", f.b.sent)
	}
}

func TestCommandOnlyOwner(t *testing.T) {
	f := idleSession(t)
	for _, by := range []string{"code", "system", ""} {
		_, err := f.m.Command(context.Background(), "code", sdk.SessionCommandClear, "", by)
		if platform.CodeOf(err) != sdk.ErrorCodeForbidden {
			t.Fatalf("%q 不是主人要拒絕：%v", by, err)
		}
	}
	if len(f.commandCalls()) != 0 {
		t.Fatal("不能交給引擎")
	}
}

// 核心只認四個指令、只檢查參數格式；模型名稱、力度等級認不認得是引擎的事（法律第 3 條）。
func TestCommandWhitelistAndArgs(t *testing.T) {
	f := idleSession(t)
	for _, tc := range []struct {
		cmd sdk.SessionCommand
		arg string
	}{
		{"logout", ""}, {"", ""}, {sdk.SessionCommandClear, "now"}, {sdk.SessionCommandCompact, "x"},
		{sdk.SessionCommandModel, ""}, {sdk.SessionCommandModel, "opus; rm -rf"}, {sdk.SessionCommandModel, "gpt 5"},
		{sdk.SessionCommandEffort, ""}, {sdk.SessionCommandEffort, "a/b"}, {sdk.SessionCommandEffort, strings.Repeat("x", 41)},
	} {
		_, err := f.m.Command(context.Background(), "code", tc.cmd, tc.arg, "owner(web)")
		if platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Fatalf("%q %q 要回參數錯誤：%v", tc.cmd, tc.arg, err)
		}
	}
	if len(f.commandCalls()) != 0 {
		t.Fatalf("格式不對的不交給引擎：%d", len(f.commandCalls()))
	}
	for _, effort := range []string{"low", "xhigh", "Max", "auto"} {
		if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandEffort, effort, "owner(web)"); err != nil {
			t.Fatalf("effort %s 要交給引擎：%v", effort, err)
		}
	}
	calls := f.commandCalls()
	if len(calls) != 4 || calls[2].Arg != "max" {
		t.Fatalf("格式對的交給引擎（轉小寫）：%+v", calls)
	}
}

// 她在忙：清掉、整理不下（「她還在忙…」）；換模型、換力度照下。等確認時都不下。
func TestCommandBusyRules(t *testing.T) {
	f := idleSession(t)
	f.event(t, sdk.EngineEventTypeTurnStarted)
	f.state(t, sdk.SessionStateBusy)
	for _, cmd := range []sdk.SessionCommand{sdk.SessionCommandClear, sdk.SessionCommandCompact} {
		_, err := f.m.Command(context.Background(), "code", cmd, "", "owner(web)")
		if platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), commandBusyText) {
			t.Fatalf("%s 忙時要拒絕：%v", cmd, err)
		}
	}
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandModel, "sonnet", "owner(web)"); err != nil {
		t.Fatalf("忙時可以換模型：%v", err)
	}
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandEffort, "low", "owner(web)"); err != nil {
		t.Fatalf("忙時可以換力度：%v", err)
	}
	if n := len(f.commandCalls()); n != 2 {
		t.Fatalf("只有換模型、換力度交給引擎：%d", n)
	}

	w := setup(t)
	w.open(t, "code")
	w.timedOut(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Tool: "Bash", Summary: "git push"})
	if _, err := w.m.Command(context.Background(), "code", sdk.SessionCommandClear, "", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), commandBusyText) {
		t.Fatalf("等確認時不清：%v", err)
	}
	if _, err := w.m.Command(context.Background(), "code", sdk.SessionCommandModel, "opus", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), commandWaitingText) {
		t.Fatalf("等確認時先回答：%v", err)
	}
	if len(w.commandCalls()) != 0 {
		t.Fatal("等確認時不交給引擎")
	}
}

func TestCommandStoppedOrMissing(t *testing.T) {
	f := setup(t)
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandClear, "", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("沒有這個對話：%v", err)
	}
	f.open(t, "code")
	f.event(t, sdk.EngineEventTypeSessionStarted)
	f.event(t, sdk.EngineEventTypeSessionEnded)
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandModel, "opus", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeUnavailable {
		t.Fatalf("對話停了：%v", err)
	}
}

// 引擎說沒做成：換成主人看得懂的話，不記 last_command。
func TestCommandEngineReasons(t *testing.T) {
	for reason, code := range map[string]sdk.ErrorCode{
		sdk.CommandUnsupported: sdk.ErrorCodeUnavailable,
		sdk.CommandBadArg:      sdk.ErrorCodeInvalid,
		sdk.CommandDialog:      sdk.ErrorCodeConflict,
		sdk.CommandTyping:      sdk.ErrorCodeConflict,
		"enter":                sdk.ErrorCodeUnavailable,
	} {
		f := idleSession(t)
		f.e.Configure(fake.Behavior{CommandReason: reason})
		_, err := f.m.Command(context.Background(), "code", sdk.SessionCommandCompact, "", "owner(web)")
		if platform.CodeOf(err) != code {
			t.Fatalf("%s：要回 %s：%v", reason, code, err)
		}
		if strings.Contains(err.Error(), reason) {
			t.Fatalf("%s：引擎的原因碼不直接給主人看：%v", reason, err)
		}
		s, _ := f.m.Get(context.Background(), "code")
		if s.LastCommand != nil {
			t.Fatalf("%s：沒做成不記：%+v", reason, s.LastCommand)
		}
	}
}

// 清掉重來：已讀位置與未讀從頭算，last_command 存進狀態檔、重開 serve 讀得回來。
func TestCommandClearResetsSeenAndPersists(t *testing.T) {
	f := idleSession(t)
	f.m.mu.Lock()
	f.m.entries["code"].seen = "old-uuid"
	f.m.mu.Unlock()
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandClear, "", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	f.m.mu.Lock()
	seen := f.m.entries["code"].seen
	f.m.mu.Unlock()
	if seen != "" {
		t.Fatalf("已讀位置要從頭算：%q", seen)
	}
	// 重讀存檔會換掉派送器也在讀的表：照派送器的順序先拿 dispatchMu 再拿 mu（不然 -race 偶爾抓到）。
	f.m.dispatchMu.Lock()
	f.m.mu.Lock()
	must(t, f.m.restore())
	got := f.m.entries["code"].session.LastCommand
	f.m.mu.Unlock()
	f.m.dispatchMu.Unlock()
	if got == nil || got.Command != sdk.SessionCommandClear {
		t.Fatalf("重開要讀回 last_command：%+v", got)
	}
}

// 清掉之後接回要用新的對話編號：引擎回了就換掉 engine_session_id。
func TestCommandClearSwitchesEngineSessionID(t *testing.T) {
	f := idleSession(t)
	f.e.Configure(fake.Behavior{CommandSessionID: "new-id"})
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandClear, "", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	s, _ := f.m.Get(context.Background(), "code")
	if s.EngineSessionID != "new-id" {
		t.Fatalf("要換成新的編號：%q", s.EngineSessionID)
	}
}

// 看板的請求中途斷了（手機切走）：引擎照樣做完，做成了照樣記下來、換編號。
type cancelAfterFirstCheck struct {
	context.Context
	n int
}

func (c *cancelAfterFirstCheck) Err() error {
	c.n++
	if c.n > 1 {
		return context.Canceled
	}
	return nil
}

func TestCommandFinishesWhenRequestCancelled(t *testing.T) {
	f := idleSession(t)
	f.e.Configure(fake.Behavior{CommandSessionID: "new-id"})
	ctx := &cancelAfterFirstCheck{Context: context.Background()}
	if _, err := f.m.Command(ctx, "code", sdk.SessionCommandClear, "", "owner(web)"); err != nil {
		t.Fatalf("請求斷了也要做完：%v", err)
	}
	s, _ := f.m.Get(context.Background(), "code")
	if s.LastCommand == nil || s.EngineSessionID != "new-id" {
		t.Fatalf("做成了要記下來：%+v %q", s.LastCommand, s.EngineSessionID)
	}
}

// 專屬對話（任務單、Goal、待辦）開頭的交代只送一次，不給清；整理成摘要可以。
func TestCommandClearOnlyMainSession(t *testing.T) {
	f := idleSession(t)
	f.m.mu.Lock()
	f.m.entries["code"].session.Kind = sdk.SessionKindWork
	f.m.mu.Unlock()
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandClear, "", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), commandClearKindText) {
		t.Fatalf("專屬對話不清：%v", err)
	}
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandCompact, "", "owner(web)"); err != nil {
		t.Fatalf("專屬對話可以整理：%v", err)
	}
}

// 額度用完（limited）不清；派送器正在貼話時不清、不整理；上一個指令還在送時擋下；指令佔著位置時派送器不貼，送完放開。
func TestCommandSlotRules(t *testing.T) {
	f := idleSession(t)
	f.m.mu.Lock()
	f.m.entries["code"].session.State = sdk.SessionStateLimited
	f.m.mu.Unlock()
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandClear, "", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("額度用完不清：%v", err)
	}
	f.m.mu.Lock()
	f.m.entries["code"].session.State = sdk.SessionStateIdle
	f.m.entries["code"].commanding = true
	f.m.mu.Unlock()
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandModel, "opus", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("上一個指令還在送要擋：%v", err)
	}
	f.m.mu.Lock()
	f.m.entries["code"].commanding = false
	f.m.mu.Unlock()

	f.m.dispatchMu.Lock()
	f.m.inFlight["code"] = true // 派送器正在貼一句話給她
	f.m.dispatchMu.Unlock()
	for _, cmd := range []sdk.SessionCommand{sdk.SessionCommandClear, sdk.SessionCommandCompact} {
		if _, err := f.m.Command(context.Background(), "code", cmd, "", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), commandBusyText) {
			t.Fatalf("%s：正在貼話時要說她在忙：%v", cmd, err)
		}
	}
	f.m.dispatchMu.Lock()
	still := f.m.inFlight["code"]
	f.m.dispatchMu.Unlock()
	if !still {
		t.Fatal("被擋下的指令不能放掉派送器自己的位置")
	}
	if len(f.commandCalls()) != 0 {
		t.Fatalf("都不交給引擎：%d", len(f.commandCalls()))
	}
	f.m.dispatchMu.Lock()
	delete(f.m.inFlight, "code")
	f.m.dispatchMu.Unlock()
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandCompact, "", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	f.m.dispatchMu.Lock()
	_, held := f.m.inFlight["code"]
	f.m.dispatchMu.Unlock()
	if held {
		t.Fatal("指令送完要放開位置，派送器才貼得進來")
	}
}

// 主人在她畫面上清掉重來（不經看板）：引擎在 SessionStarted 帶新的編號，之後接回用它；沒帶就不改（SES-61）。
func TestSessionStartedWithNewEngineSessionIDSwitchesResume(t *testing.T) {
	f := idleSession(t)
	before, _ := f.m.Get(context.Background(), "code")
	must(t, f.m.ReportEvent(context.Background(), &sdk.EngineEvent{Session: "code", Type: sdk.EngineEventTypeSessionStarted, At: f.h.Clock.Now()}))
	if s, _ := f.m.Get(context.Background(), "code"); s.EngineSessionID != before.EngineSessionID {
		t.Fatalf("沒帶編號不改：%q → %q", before.EngineSessionID, s.EngineSessionID)
	}
	must(t, f.m.ReportEvent(context.Background(), &sdk.EngineEvent{Session: "code", Type: sdk.EngineEventTypeSessionStarted, At: f.h.Clock.Now(), EngineSessionID: "cleared-in-terminal"}))
	if s, _ := f.m.Get(context.Background(), "code"); s.EngineSessionID != "cleared-in-terminal" {
		t.Fatalf("要換成新的編號：%q", s.EngineSessionID)
	}
}

// 叫她停下（SES-62）：只在她忙著時送給引擎；閒著、在等確認、停了都擋下，不碰引擎。
func TestCommandInterruptOnlyWhileBusy(t *testing.T) {
	f := idleSession(t)
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandInterrupt, "", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("閒著要擋：%v", err)
	}
	f.event(t, sdk.EngineEventTypeTurnStarted)
	note, err := f.m.Command(context.Background(), "code", sdk.SessionCommandInterrupt, "", "owner(web)")
	if err != nil || note.Command != sdk.SessionCommandInterrupt {
		t.Fatalf("忙著要送：%+v %v", note, err)
	}
	if s, _ := f.m.Get(context.Background(), "code"); s.LastCommand != nil {
		t.Fatalf("停下不記成對話設定：%+v", s.LastCommand)
	}
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandInterrupt, "x", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("不收參數：%v", err)
	}
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandInterrupt, "", "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("只有主人：%v", err)
	}
	// 同時有人在叫她停：只送一次。
	f.m.mu.Lock()
	f.m.entries["code"].interrupting = true
	f.m.mu.Unlock()
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandInterrupt, "", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), commandInterruptingText) {
		t.Fatalf("正在停就不再送：%v", err)
	}
	f.m.mu.Lock()
	f.m.entries["code"].interrupting = false
	f.m.mu.Unlock()
	if n := len(f.commandCalls()); n != 1 {
		t.Fatalf("只有忙著那次交給引擎：%d", n)
	}

	// 在等確認：用「拒絕」，不送停止。
	w := setup(t)
	w.open(t, "code")
	w.timedOut(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Tool: "Bash", Summary: "git push"})
	if _, err := w.m.Command(context.Background(), "code", sdk.SessionCommandInterrupt, "", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), commandInterruptWaitingText) {
		t.Fatalf("等確認時不停：%v", err)
	}
	if len(w.commandCalls()) != 0 {
		t.Fatal("等確認時不交給引擎")
	}
}
