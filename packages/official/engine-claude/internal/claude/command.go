package claude

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 主人從看板下的對話指令（ECL-46、ENG-17、D-45）：EverGenie 的詞換成 Claude Code 的斜線指令，直接貼進她的輸入框、按 Enter。
// 不經訊息佇列、不寫封包（斜線指令不觸發 UserPromptSubmit，當訊息送永遠等不到「收到」）。白名單與參數由核心檢查。

const (
	// commandConfirmWithin：按 Enter 之後等平台收下指令多久（換模型可能先跳一題「要換嗎」）。
	commandConfirmWithin = 5 * time.Second
	// clearSwitchWithin：清掉重來時，輸入框清空之後再等新的對話編號（SessionStart hook 寫紀錄）多久。
	clearSwitchWithin = 3 * time.Second
)

// switchPrompts：換模型、換思考力度時平台可能先問一題（對話中途換模型），按 1（同意）。
var switchPrompts = []string{"Switch model?", "Change effort level?"}

const (
	switchPromptTail   = 15 // 只看畫面最後幾行（確認題在最下面）
	switchOptionWithin = 6  // 題目之後幾行內要有「1.」選項
)

// commandModels、commandEfforts：Claude Code 的 /model、/effort 收的選項（核心只檢查格式，認不認得在這裡，ECL-46）。
var (
	commandModels  = []string{"sonnet", "opus", "fable", "haiku"}
	commandEfforts = []string{"low", "medium", "high", "xhigh", "max", "auto"}
)

// platformCommand 把 EverGenie 的指令換成這個平台的斜線指令；做不到回 reason（CommandUnsupported、CommandBadArg）。
func platformCommand(cmd sdk.SessionCommand, arg string) (string, string) {
	arg = strings.TrimSpace(arg)
	switch cmd {
	case sdk.SessionCommandClear, sdk.SessionCommandCompact:
		if arg != "" {
			return "", sdk.CommandBadArg
		}
		return "/" + string(cmd), ""
	case sdk.SessionCommandModel:
		if !slices.Contains(commandModels, arg) {
			return "", sdk.CommandBadArg
		}
		return "/model " + arg, ""
	case sdk.SessionCommandEffort:
		if !slices.Contains(commandEfforts, arg) {
			return "", sdk.CommandBadArg
		}
		return "/effort " + arg, ""
	}
	return "", sdk.CommandUnsupported
}

// Command 貼上斜線指令、按 Enter，確認輸入框清空（平台收下了）才回 done（ECL-46）。
// 確認框開著回 dialog、session 不在回 gone；換模型或力度時平台先問一題就按 1。
// 清掉重來另外等 SessionStart（source=clear）把新的對話編號寫進紀錄（ECL-47），帶回 engine_session_id。
func (e *Engine) Command(ctx context.Context, req *sdk.EngineCommandRequest) (*sdk.EngineCommandResponse, error) {
	if req == nil || strings.TrimSpace(req.Session) == "" {
		return nil, invalid("session", "請指定 session 名稱")
	}
	e.recall() // 引擎剛重開：先把 socket 補回來，不然查到預設的 socket 會回「不在了」（ECL-68）
	if req.Command == sdk.SessionCommandInterrupt {
		return e.interrupt(ctx, req.Session), nil
	}
	text, reason := platformCommand(req.Command, req.Arg)
	if reason != "" {
		return &sdk.EngineCommandResponse{Reason: reason}, nil
	}
	name := req.Session
	l := e.lockFor(name)
	l.Lock()
	defer l.Unlock()
	if !e.d.Tmux.Alive(name) {
		return &sdk.EngineCommandResponse{Reason: "gone"}, nil
	}
	screen, err := e.d.Tmux.Capture(name)
	if err != nil {
		return &sdk.EngineCommandResponse{Reason: "capture"}, nil
	}
	if isConfirmDialog(screen) {
		return &sdk.EngineCommandResponse{Reason: sdk.CommandDialog}, nil
	}
	if e.boxTyped(name, screen) {
		// 輸入框裡已經有字（主人在終端機打了一半）：貼上去會接在後面變成一句話，不送。
		return &sdk.EngineCommandResponse{Reason: sdk.CommandTyping}, nil
	}
	before := ""
	if req.Command == sdk.SessionCommandClear {
		if s := e.transcriptSource(name); s != nil {
			before = s.id
		}
	}
	pastedAt := e.now()
	if err := e.d.Tmux.Paste(name, text); err != nil {
		return &sdk.EngineCommandResponse{Reason: "paste"}, nil
	}
	e.sleep(pasteEnterDelay)
	screen, err = e.d.Tmux.Capture(name)
	if err != nil {
		if !e.d.Tmux.Alive(name) {
			return &sdk.EngineCommandResponse{Reason: "gone"}, nil
		}
		return &sdk.EngineCommandResponse{Reason: "capture"}, nil
	}
	if isConfirmDialog(screen) {
		return &sdk.EngineCommandResponse{Reason: sdk.CommandDialog}, nil
	}
	if holds, found := boxHolds(screen, text); found && !holds {
		return &sdk.EngineCommandResponse{Reason: "paste"}, nil
	}
	if err := e.d.Tmux.SendKey(name, "Enter"); err != nil {
		return &sdk.EngineCommandResponse{Reason: "enter"}, nil
	}
	if !e.confirmCommand(ctx, name, req.Command, text, pastedAt) {
		return &sdk.EngineCommandResponse{Reason: reasonNotSubmitted}, nil
	}
	res := &sdk.EngineCommandResponse{Done: true}
	switch req.Command {
	case sdk.SessionCommandClear:
		res.EngineSessionID = e.awaitNewTranscript(ctx, name, before)
	case sdk.SessionCommandModel:
		// 換了模型（ECL-49）：代號要等她下一次回話才知道，先回別名；名字用平台的回覆（「Set model to `Opus 5.5`」），沒有就從別名換。
		res.Model, res.ModelLabel = req.Arg, modelLabel(req.Arg)
		markModelSwitch(e.d.Home, name, pastedAt) // 這之前的回話不再拿來回報模型
		if seen := e.commandModelSeen(name, pastedAt); seen.Model == "" && seen.Label != "" {
			res.ModelLabel = seen.Label
		}
	case sdk.SessionCommandEffort:
		res.Effort = req.Arg
	}
	return res, nil
}

// boxTyped：輸入框裡有主人打的字（ECL-55）。平台會在空的輸入框放淡色的字：提示（「Try "…"」）或猜主人下一句的建議
// （2026-11-03 遇過的畫面「❯ ESC[2m<平台猜的下一句>ESC[0m」），純文字畫面看不出淡色，會被當成打了一半、指令不送。
// 所以再抓一次帶樣式的畫面、去掉淡色字再看；帶樣式的抓不到就照純文字判斷（寧可擋，也不把指令接在主人的字後面）。
// 框線用只拿掉控制碼的那份找（框線畫成淡色也找得到）；去掉淡色後反而找不到框，就當有字（擋）。
func (e *Engine) boxTyped(name, screen string) bool {
	if styled, err := e.d.Tmux.CaptureStyled(name); err == nil {
		if _, found := inputBox(withoutStyle(styled)); found {
			box, ok := inputBox(withoutFaint(styled))
			return !ok || boxHasText(box)
		}
	}
	box, found := inputBox(screen)
	return found && boxHasText(box)
}

// boxHasText：輸入框那幾行去掉提示符「❯」之後還有字。
func boxHasText(box string) bool {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(box), dialogCursor)) != ""
}

// commandModelSeen 讀這個 session 的對話記錄，找 since 之後最後一次的模型（換模型的回覆）。
func (e *Engine) commandModelSeen(name string, since time.Time) modelSeen {
	s := e.transcriptSource(name)
	if s == nil {
		return modelSeen{}
	}
	return transcriptModel(transcriptTailOf(transcriptPath(s.configDir, s.cwd, s.id)), since.Add(-time.Second))
}

// confirmCommand 在按 Enter 之後等平台收下指令：輸入框還在、而且沒有這個指令了；或對話記錄裡這次貼上之後有這個指令。
// 換模型、換力度時平台先問一題就按 1（只按一次）。
func (e *Engine) confirmCommand(ctx context.Context, name string, cmd sdk.SessionCommand, text string, since time.Time) bool {
	deadline := e.now().Add(commandConfirmWithin)
	answered := false
	for i := 0; ; i++ {
		last := !e.now().Before(deadline) || ctx.Err() != nil
		if screen, err := e.d.Tmux.Capture(name); err == nil {
			switch {
			case !answered && hasSwitchPrompt(screen):
				answered = true
				if err := e.d.Tmux.SendKey(name, "1"); err != nil {
					return false
				}
				e.sleep(answerSettle)
				continue
			case isConfirmDialog(screen) || hasSwitchPrompt(screen):
				// 還開著要人按的東西：不算收下（等它收掉）。
			default:
				if holds, found := boxHolds(screen, text); found && !holds {
					return true
				}
			}
		}
		if (last || i%transcriptEvery == 0) && e.commandInTranscript(name, cmd, since) {
			return true
		}
		if last {
			return false
		}
		e.sleep(submitPollInterval)
	}
}

// hasSwitchPrompt：畫面最下面那一段（輸入框以外、最後 switchPromptTail 行）有一行以「Switch model?」之類開頭，
// 而且後面幾行內有編號 1 的選項。只看字樣會被對話裡剛好講到這句的內容騙到，把「1」打進空的輸入框。
func hasSwitchPrompt(screen string) bool {
	lines := strings.Split(strings.TrimRight(outsideBox(screen), "\n "), "\n")
	if len(lines) > switchPromptTail {
		lines = lines[len(lines)-switchPromptTail:]
	}
	for i, l := range lines {
		t := strings.TrimSpace(l)
		matched := false
		for _, p := range switchPrompts {
			if strings.HasPrefix(t, p) {
				matched = true
			}
		}
		if !matched {
			continue
		}
		for _, o := range lines[i+1 : min(len(lines), i+1+switchOptionWithin)] {
			if m := optionLine.FindStringSubmatch(o); m != nil && m[2] == "1" {
				return true
			}
		}
	}
	return false
}

// awaitNewTranscript：清掉重來之後等 hook 把新的對話編號寫進紀錄（ECL-47）；等不到回空（核心沿用舊的編號）。
func (e *Engine) awaitNewTranscript(ctx context.Context, name, before string) string {
	deadline := e.now().Add(clearSwitchWithin)
	for {
		if r := e.transcriptRecordOf(name); r != nil && r.ID != before {
			return r.ID
		}
		if !e.now().Before(deadline) || ctx.Err() != nil {
			return ""
		}
		e.sleep(submitPollInterval)
	}
}

// commandInTranscript：對話記錄裡 since 之後有一則使用者的「<command-name>/指令</command-name>」（平台把斜線指令記成這樣）。
// 清掉重來的那一則記在新的對話檔裡，所以先看紀錄（transcriptSource）。
func (e *Engine) commandInTranscript(name string, cmd sdk.SessionCommand, since time.Time) bool {
	tag := "<command-name>/" + string(cmd) + "</command-name>"
	s := e.transcriptSource(name)
	if s == nil {
		return false
	}
	data := transcriptTailOf(transcriptPath(s.configDir, s.cwd, s.id))
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, tag) {
			continue
		}
		var row struct {
			Type      string    `json:"type"`
			Timestamp time.Time `json:"timestamp"`
		}
		if json.Unmarshal([]byte(line), &row) == nil && row.Type == "user" && !row.Timestamp.Before(since.Add(-time.Second)) {
			return true
		}
	}
	return false
}

// interruptSettle：按了停止之後等平台收手再回報閒著。
const interruptSettle = 300 * time.Millisecond

// interrupt 叫她停下這一輪（SES-62、ECL-52）：在她畫面上按一次 Esc（跟主人在終端機按停止一樣）；平台這時不會叫 Stop 掛勾，
// 等一下就回報 IDLE_HINT（interrupted、At＝按下的時間：核心拿它判斷之後她有沒有又開始一輪），核心才不會一直以為她在忙（平台自己的「閒著」提示要 60 秒）。session 不在回 gone。
func (e *Engine) interrupt(ctx context.Context, name string) *sdk.EngineCommandResponse {
	l := e.lockFor(name)
	l.Lock()
	defer l.Unlock()
	if !e.d.Tmux.Alive(name) {
		return &sdk.EngineCommandResponse{Reason: "gone"}
	}
	pressed := e.now()
	if err := e.d.Tmux.SendKey(name, "Escape"); err != nil {
		return &sdk.EngineCommandResponse{Reason: "enter"}
	}
	e.sleep(interruptSettle)
	if e.d.Host != nil {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reportTimeout)
		_, _ = e.d.Host.ReportEvent(rctx, &sdk.EngineHostReportEventRequest{Event: &sdk.EngineEvent{
			Session: name, Type: sdk.EngineEventTypeIdleHint, At: pressed, Interrupted: true,
		}})
		cancel()
	}
	return &sdk.EngineCommandResponse{Done: true}
}
