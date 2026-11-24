package session

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 主人從看板對一段對話下的指令（SES-59、D-45）：清掉重來、整理成摘要、換模型、換思考力度。
// 不是訊息：不進佇列、不寫封包，直接交給引擎在 session 上執行。指令名稱與參數格式在這裡擋；模型名稱、力度等級
// 認不認得是引擎的事（法律第 3 條：核心不認得任何一家平台），不認得回 CommandBadArg。

// 引擎做一個指令最多等 limits.Command（limits.go）。不跟著看板的請求取消：手機切走時指令可能已經送進去了，
// 要等引擎回來才知道做成沒有（清掉之後要換接續用的編號）。

// commandArg：模型名稱、力度等級的格式（一個詞，英數與 . _ -，最多 40 字）。
var commandArg = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

// 擋下指令時給主人看的話（看板照原字顯示）。
const (
	commandBusyText    = "還在忙，等這一輪做完或先按停止再下指令"
	commandWaitingText = "正在等你回答上面的確認，先回答再下指令"
	commandStoppedText = "對話還沒開起來，先傳一句話把秘書叫醒再下指令"
	// 任務單、Goal、待辦的專屬對話開頭的交代只送一次，清掉她就忘了這件事（D-45 ③）。
	commandClearKindText = "這是專屬對話，清掉就會忘了這件事的交代；只有主要對話能清"
)

func checkCommand(cmd sdk.SessionCommand, arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	switch cmd {
	case sdk.SessionCommandClear, sdk.SessionCommandCompact, sdk.SessionCommandInterrupt:
		if arg != "" {
			return "", platform.Invalid("arg", fmt.Sprintf("/%s 不用參數", cmd))
		}
		return "", nil
	case sdk.SessionCommandModel, sdk.SessionCommandEffort:
		arg = strings.ToLower(arg)
		if !commandArg.MatchString(arg) {
			if cmd == sdk.SessionCommandModel {
				return "", platform.Invalid("arg", "請選一個模型")
			}
			return "", platform.Invalid("arg", "請選一個思考力度")
		}
		return arg, nil
	}
	return "", platform.Invalid("command", "看板支援 /clear、/compact、/model、/effort")
}

// resets 是會把對話內容清掉或改寫的指令：她在忙或在等確認時不下（做到一半被清掉，主人會以為她做完了）。
func resets(cmd sdk.SessionCommand) bool {
	return cmd == sdk.SessionCommandClear || cmd == sdk.SessionCommandCompact
}

// Command 執行主人的對話指令。成功回這次的紀錄，也記進 Session.LastCommand、發 session.command。
func (m *Module) Command(ctx context.Context, name string, cmd sdk.SessionCommand, arg, by string) (*sdk.SessionCommandNote, error) {
	if !platform.IsOwner(by) {
		return nil, platform.Forbidden("只有主人能對她的對話下指令")
	}
	arg, err := checkCommand(cmd, arg)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if err := m.ready(ctx); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	e := m.entries[name]
	if e == nil {
		m.mu.Unlock()
		return nil, platform.NotFound("找不到這個對話")
	}
	s := e.session
	if cmd == sdk.SessionCommandInterrupt {
		return m.interrupt(ctx, e) // m.mu 由它放開
	}
	switch {
	case s.State == sdk.SessionStateStopped || s.State == sdk.SessionStateStarting || s.Account == "":
		m.mu.Unlock()
		return nil, platform.Unavailable(commandStoppedText)
	case s.Pending != nil || s.State == sdk.SessionStateWaiting:
		m.mu.Unlock()
		if resets(cmd) {
			return nil, platform.Conflict(commandBusyText)
		}
		return nil, platform.Conflict(commandWaitingText)
	case resets(cmd) && s.State != sdk.SessionStateIdle:
		m.mu.Unlock()
		return nil, platform.Conflict(commandBusyText) // 忙著、額度用完（limited）都不清
	case cmd == sdk.SessionCommandClear && s.Kind != sdk.SessionKindResident:
		m.mu.Unlock()
		return nil, platform.Conflict(commandClearKindText)
	case e.commanding:
		m.mu.Unlock()
		return nil, platform.Conflict("上一個指令還在送，等一下再試")
	}
	e.commanding = true
	account := s.Account
	m.mu.Unlock()

	// 派送器正在貼一句話給她時不清（一次只貼一則；model、effort 由引擎的同一把鎖排在那句後面）。
	m.dispatchMu.Lock()
	pasting := m.inFlight[name]
	held := false
	if !pasting {
		m.inFlight[name], held = true, true // 指令送完之前派送器不貼新的話進來
	}
	m.dispatchMu.Unlock()
	release := func() {
		if held {
			m.dispatchMu.Lock()
			delete(m.inFlight, name)
			m.dispatchMu.Unlock()
			m.wakeDelivery()
		}
		m.mu.Lock()
		e.commanding = false
		m.mu.Unlock()
	}
	if pasting && resets(cmd) {
		release()
		return nil, platform.Conflict(commandBusyText)
	}
	if resets(cmd) {
		// 佔到位置之後再看一次：判斷閒著到佔位置之間，她可能自己開始了一輪（例如主人在終端機打字）。
		m.mu.Lock()
		idle := e.session.State == sdk.SessionStateIdle && e.session.Pending == nil
		m.mu.Unlock()
		if !idle {
			release()
			return nil, platform.Conflict(commandBusyText)
		}
	}

	var res *sdk.EngineCommandResponse
	eng, err := m.d.Engine.For(account)
	if err == nil {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.limits.Command)
		res, err = eng.Command(cctx, name, cmd, arg, by)
		if err != nil && cctx.Err() == context.DeadlineExceeded {
			err = silent("送指令", m.limits.Command, "不確定有沒有送進去；看一下對話，再決定要不要重送。")
		}
		cancel()
	}
	release()
	if err != nil {
		return nil, err
	}
	if !res.Done {
		return nil, commandFailure(res.Reason)
	}

	note := &sdk.SessionCommandNote{Command: cmd, Arg: arg, At: m.d.Clock.Now(), By: by}
	m.mu.Lock()
	e = m.entries[name]
	if e == nil {
		m.mu.Unlock()
		return note, nil
	}
	copy := *note
	e.session.LastCommand = &copy
	model, label, effort := commandModel(cmd, arg, res)
	applyModel(&e.session, model, label, effort) // 換模型、換力度馬上記（SES-60）
	if cmd == sdk.SessionCommandClear {
		// 清掉重來：平台開了新的對話記錄，已讀位置與未讀從頭算；之後接回用新的編號（SES-59）。
		if res.EngineSessionID != "" {
			e.session.EngineSessionID = res.EngineSessionID
		}
		e.seen = ""
		e.messages = nil
		e.lastAssistant = nil
		e.ended = false
		e.version++
		m.transcriptDirty[name] = true
	}
	err = m.changed(context.WithoutCancel(ctx), e) // 看板斷線也要記下來（之後接回要用新的編號）
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	// 放開 m.mu 之後才發，訂閱的人回頭呼叫 session 不會卡住（同 host.go 的事實事件）。
	m.publish(context.WithoutCancel(ctx), "session.command", map[string]any{"name": name, "command": string(cmd), "arg": arg, "by": by})
	return note, nil
}

// commandFailure 把引擎說沒做成的原因換成主人看得懂的話（引擎的原因碼不直接給主人看，FEN-10）。
func commandFailure(reason string) error {
	switch reason {
	case sdk.CommandUnsupported:
		return platform.Unavailable("這位秘書用的 AI 不支援這個指令")
	case sdk.CommandBadArg:
		return platform.Invalid("arg", "這位秘書用的 AI 沒有這個選項")
	case sdk.CommandDialog:
		return platform.Conflict("秘書的畫面上開著一個要按的確認，先回答再下指令")
	case sdk.CommandTyping:
		return platform.Conflict("秘書的輸入框裡有打到一半的字，先清掉再下指令")
	case "gone":
		return platform.Unavailable("對話不在了，先傳一句話把秘書叫醒再下指令")
	}
	return platform.Unavailable("指令沒送進去，再試一次")
}

// 叫她停下時擋下的話。
const (
	commandIdleText             = "現在沒在做事，不用停"
	commandInterruptWaitingText = "正在等你回答上面的確認，按「拒絕」就會停下"
	commandInterruptingText     = "已經叫停了，等一下"
)

// interrupt 叫她停下這一輪（SES-62）：跟其他指令不同，就是要在她忙著時下，所以不看派送器、不排隊；
// 她停了、沒在做事、在等確認（那要用「拒絕」）就不送。引擎停下她之後回報帶 interrupted 的閒著（IDLE_HINT，At＝按停止的時間），狀態回到待命、這一輪的話記中斷（origin.go）。同時只送一次。不記 last_command（不是改對話設定），之後她這一輪照常以「被中斷」結束。進來時 m.mu 是鎖著的。
func (m *Module) interrupt(ctx context.Context, e *entry) (*sdk.SessionCommandNote, error) {
	s := e.session
	name, account := s.Name, s.Account
	switch {
	case s.State == sdk.SessionStateStopped || s.State == sdk.SessionStateStarting || account == "":
		m.mu.Unlock()
		return nil, platform.Unavailable(commandStoppedText)
	case s.Pending != nil || s.State == sdk.SessionStateWaiting:
		// 在等確認：回答「拒絕」她就會停下；在她畫面上按停止會讓核心跟她對不上那張確認。
		m.mu.Unlock()
		return nil, platform.Conflict(commandInterruptWaitingText)
	case s.State != sdk.SessionStateBusy:
		m.mu.Unlock()
		return nil, platform.Conflict(commandIdleText)
	case e.interrupting:
		// 兩台裝置同時按、或重按：只按一次（平台連按兩次停止會打開「回到前面」的選單，之後貼不進去）。
		m.mu.Unlock()
		return nil, platform.Conflict(commandInterruptingText)
	}
	e.interrupting = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		e.interrupting = false
		m.mu.Unlock()
	}()
	eng, err := m.d.Engine.For(account)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.limits.Command)
	defer cancel()
	res, err := eng.Command(cctx, name, sdk.SessionCommandInterrupt, "", "")
	if err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			err = silent("叫停", m.limits.Command, "不確定有沒有停；看一下對話，還在跑的話再按一次。")
		}
		return nil, err
	}
	if !res.Done {
		return nil, commandFailure(res.Reason)
	}
	return &sdk.SessionCommandNote{Command: sdk.SessionCommandInterrupt, At: m.d.Clock.Now()}, nil
}
