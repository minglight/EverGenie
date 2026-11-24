package fake

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 腳本引擎的標記：送進來的文字含這些字，就走對應的劇情（e2e 用，P4 共通定案 14）。
const (
	MarkSlow     = "【慢】"
	MarkApprove  = "【權限】"
	MarkQuestion = "【選擇】"
	// MarkDescribed：跟【選擇】一樣問一題，但選項帶說明、問之前有一段開場白（看板的選擇題卡片 WEB-85）。
	MarkDescribed = "【選說明】"
	// MarkMany：一次問三題（第一題可以選好幾個），選項的說明很長——卡片比手機一屏還高時看板要能捲（WEB-122）。
	MarkMany = "【多題】"
	// MarkFail：這一輪回報 TURN_FAILED（錯誤種類 overloaded，S5）。寫成「【出錯:auth】」這樣可以指定種類。
	MarkFail = "【出錯】"
	// MarkLogout：這一輪開始後帳號登出了——報 SESSION_ENDED（reason＝sdk.SessionEndReasonLoggedOut），session 跟著不在（S5-2）。
	MarkLogout = "【登出】"
	// MarkStuck：收到這句的那一刻起引擎整個不回應（這句的 Deliver 與之後所有呼叫都卡著，Engine.Stick）——
	// 測「秘書那一側卡住，看板照常」（WEB-175）。這個家的引擎行程重開才會好。
	MarkStuck = "【不回】"
	// MarkStuckOne：只有收到這句的那個 session 不回應（這句的 Deliver 與她之後的呼叫都卡著，Engine.StickSession），
	// 別的 session 照常——測「一位秘書卡住，其他秘書照常收話」（DEV-159）。
	MarkStuckOne = "【這位不回】"
	// MarkManyTools：這一輪多用幾種工具、有的用很多次（manyTools），看板的工具清單要合併同一種（WEB-125）。
	MarkManyTools = "【多工具】"
)

// fakeTool 是假引擎記在對話記錄裡的一個工具呼叫：原名與白話（白話空＝像舊資料一樣沒有）。
type fakeTool struct{ name, label string }

// manyTools：【多工具】在平常那一個「讀檔」之後再用的工具——跑指令 12 次、找內容 2 次、再讀一次檔、一個沒有白話的。
var manyTools = func() []fakeTool {
	var out []fakeTool
	for range 12 {
		out = append(out, fakeTool{"Bash", "跑指令"})
	}
	return append(out, fakeTool{"Grep", "找內容"}, fakeTool{activityName, activityLabel}, fakeTool{"Grep", "找內容"}, fakeTool{"OldTool", ""})
}()

// markFailPrefixes 是指定錯誤種類的寫法開頭（「【出錯:<種類>】」，全形冒號也認）。
var markFailPrefixes = []string{"【出錯:", "【出錯："}

// failText 是 TURN_FAILED 帶的「平台原話」（假的）。
const failText = "假引擎：這一輪出錯了"

const (
	activityName    = "Read"
	activityLabel   = "讀檔" // 對話記錄裡工具呼叫的白話（SES-65）；事件的 activity 照舊報原名（測試用）
	toolOutputText  = "工具的輸出（看板不該顯示）"
	fastActivity    = 300 * time.Millisecond
	slowActivity    = 3 * time.Second
	approvalWaitS   = 600
	approvalTool    = "Bash"
	approvalText    = "建立檔案 e2e.txt"
	questionText    = "要哪一個？"
	questionOptionA = "A"
	questionOptionB = "B"
)

// Scripted 是給瀏覽器 e2e 用的引擎：收到文字就跑一輪固定劇情，事件與對話記錄都走跟正式引擎一樣的邊界。
// 對話記錄共用 Engine 已有的訊息儲存與 Transcript。
type Scripted struct {
	*Engine
	ctx   context.Context
	cmu   sync.Mutex
	locks map[string]*sync.Mutex
	// stops：每個 session 正在跑的這一輪，主人叫她停下（interrupt）時關掉（SES-62）。
	stops map[string]chan struct{}
}

// NewScripted 回傳腳本引擎；host 是引擎回報事件與要求確認的對象。
func NewScripted(host sdk.EngineHostAPI) sdk.EngineAPI {
	return NewScriptedContext(context.Background(), host)
}

// NewScriptedContext 的 ctx 結束後，還在跑的劇情會停下來。
func NewScriptedContext(ctx context.Context, host sdk.EngineHostAPI) *Scripted {
	return &Scripted{Engine: New(host), ctx: ctx, locks: map[string]*sync.Mutex{}, stops: map[string]chan struct{}{}}
}

// Deliver 馬上回 delivered，劇情在背景跑；同一個 session 的劇情一輪一輪排隊。
func (s *Scripted) Deliver(ctx context.Context, r *sdk.EngineDeliverRequest) (*sdk.EngineDeliverResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record(s.Engine, "Deliver", r)
	if strings.Contains(r.Text, MarkStuck) {
		s.Stick()
	}
	if strings.Contains(r.Text, MarkStuckOne) {
		s.StickSession(r.Session, true)
	}
	if err := s.hold(ctx, r.Session); err != nil {
		return nil, err
	}
	if !s.alive(r.Session) {
		return &sdk.EngineDeliverResponse{Reason: "gone"}, nil // 跟真的引擎一樣（session 不在）
	}
	go s.runTurn(r.Session, r.Text)
	return &sdk.EngineDeliverResponse{Delivered: true}, nil
}

func (s *Scripted) alive(session string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[session]
}

func (s *Scripted) turnLock(session string) *sync.Mutex {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	l := s.locks[session]
	if l == nil {
		l = &sync.Mutex{}
		s.locks[session] = l
	}
	return l
}

func (s *Scripted) addMessage(session string, kind sdk.ChatKind, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.messages[session])
	s.messages[session] = append(s.messages[session], sdk.ChatMessage{
		Uuid: fmt.Sprintf("%s-%d", session, n+1), Ts: time.Now().UTC(), Kind: kind, Text: text,
	})
	close(s.changed)
	s.changed = make(chan struct{})
}

// addTool 記一則工具呼叫（text 空，帶白話）或它的結果（callID 配對）。
func (s *Scripted) addTool(session string, kind sdk.ChatKind, callID, text string, tool fakeTool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.messages[session])
	m := sdk.ChatMessage{Uuid: fmt.Sprintf("%s-%d", session, n+1), Ts: time.Now().UTC(), Kind: kind, Text: text, Tool: tool.name, CallID: callID}
	if kind == sdk.ChatKindToolCall {
		m.ToolLabel = tool.label
	}
	s.messages[session] = append(s.messages[session], m)
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *Scripted) report(session string, ev sdk.EngineEvent) bool {
	if !s.alive(session) {
		return false
	}
	ev.Session = session
	ev.At = time.Now().UTC()
	return s.emit(s.ctx, ev) == nil
}

// pause 等一段時間；ctx 結束或主人叫她停下（stop 關掉）回 false。
func (s *Scripted) pause(d time.Duration, stop <-chan struct{}) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-s.ctx.Done():
		return false
	case <-stop:
		return false
	case <-t.C:
		return true
	}
}

// Command：叫她停下（interrupt）時收掉正在跑的這一輪、回報閒著（跟真的引擎一樣）；其他指令照 Engine。
func (s *Scripted) Command(ctx context.Context, r *sdk.EngineCommandRequest) (*sdk.EngineCommandResponse, error) {
	if r.Command != sdk.SessionCommandInterrupt {
		return s.Engine.Command(ctx, r)
	}
	if err := s.hold(ctx, r.Session); err != nil {
		return nil, err
	}
	if !s.alive(r.Session) {
		return &sdk.EngineCommandResponse{Reason: "gone"}, nil
	}
	s.cmu.Lock()
	stop := s.stops[r.Session]
	delete(s.stops, r.Session)
	s.cmu.Unlock()
	if stop != nil {
		close(stop)
	}
	return &sdk.EngineCommandResponse{Done: true}, nil
}

func (s *Scripted) runTurn(session, text string) {
	l := s.turnLock(session)
	l.Lock()
	defer l.Unlock()
	// 先登記這一輪的停止鈕再回報開始：核心一看到她在忙就可能叫她停下（SES-62）。
	stop := make(chan struct{})
	s.cmu.Lock()
	s.stops[session] = stop
	s.cmu.Unlock()
	defer func() {
		s.cmu.Lock()
		if s.stops[session] == stop {
			delete(s.stops, session)
		}
		s.cmu.Unlock()
	}()
	if !s.report(session, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Input: text, Source: sdk.InputSourceTyped}) {
		return
	}
	s.addMessage(session, sdk.ChatKindUser, text)
	if kind, ok := failKind(text); ok {
		s.report(session, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnFailed, Error: kind, LastMessage: failText})
		return // 跟真的一樣：出錯的那一輪沒有 TURN_ENDED
	}
	if strings.Contains(text, MarkLogout) {
		s.report(session, sdk.EngineEvent{Type: sdk.EngineEventTypeSessionEnded, Reason: sdk.SessionEndReasonLoggedOut})
		s.mu.Lock()
		delete(s.sessions, session)
		s.mu.Unlock()
		return
	}
	if !s.report(session, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted, Activity: activityName}) {
		return
	}
	callID := fmt.Sprintf("call-%d", time.Now().UnixNano())
	read := fakeTool{activityName, activityLabel}
	s.addTool(session, sdk.ChatKindToolCall, callID, "", read)
	wait := fastActivity
	if strings.Contains(text, MarkSlow) {
		wait = slowActivity
	}
	if !s.pause(wait, stop) {
		select {
		case <-stop: // 主人叫她停下：跟真的一樣，這一輪沒有 TURN_ENDED，記一筆中斷、回報閒著
			s.addMessage(session, sdk.ChatKindInterrupt, "")
			s.report(session, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint, Interrupted: true})
		default:
		}
		return
	}
	s.addTool(session, sdk.ChatKindToolResult, callID, toolOutputText, read)
	if strings.Contains(text, MarkManyTools) {
		for i, tool := range manyTools {
			id := fmt.Sprintf("%s-%d", callID, i)
			s.addTool(session, sdk.ChatKindToolCall, id, "", tool)
			s.addTool(session, sdk.ChatKindToolResult, id, toolOutputText, tool)
		}
	}
	if !s.report(session, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityEnded, Activity: activityName}) {
		return
	}
	reply := "收到：" + text
	switch {
	case strings.Contains(text, MarkApprove):
		reply = s.askPermission(session)
	case strings.Contains(text, MarkMany):
		reply = s.askMany(session)
	case strings.Contains(text, MarkDescribed):
		reply = s.askDescribed(session)
	case strings.Contains(text, MarkQuestion):
		reply = s.askQuestion(session)
	}
	if reply == "" {
		return
	}
	s.addMessage(session, sdk.ChatKindAssistant, reply)
	// 跟真的引擎一樣：一輪結束時回報這一輪實際用的模型與力度（D-46）。
	model, label, effort := s.current(session)
	s.report(session, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: reply, Model: model, ModelLabel: label, Effort: effort})
}

// failKind：文字含「【出錯】」回 overloaded；含「【出錯:<種類>】」（全形冒號也可以）回那個種類（不檢查認不認得，好測核心怎麼處理新種類）。
func failKind(text string) (sdk.EngineErrorKind, bool) {
	if strings.Contains(text, MarkFail) {
		return sdk.EngineErrorKindOverloaded, true
	}
	rest := ""
	for _, prefix := range markFailPrefixes {
		if i := strings.Index(text, prefix); i >= 0 {
			rest = text[i+len(prefix):]
			break
		}
	}
	if rest == "" {
		return "", false
	}
	j := strings.Index(rest, "】")
	if j <= 0 {
		return "", false
	}
	return sdk.EngineErrorKind(rest[:j]), true
}

func (s *Scripted) askPermission(session string) string {
	out, err := s.host.RequestApproval(s.ctx, &sdk.EngineHostRequestApprovalRequest{
		Session: session, Kind: sdk.ApprovalKindPermission, Tool: approvalTool, Summary: approvalText, MaxWaitSeconds: approvalWaitS,
	})
	if err != nil || out == nil || !out.Decided || out.Decision == nil {
		return "已拒絕：沒有收到回答"
	}
	if out.Decision.Allow {
		return "已允許"
	}
	return "已拒絕：" + out.Decision.Message
}

func (s *Scripted) askQuestion(session string) string {
	out, err := s.host.RequestApproval(s.ctx, &sdk.EngineHostRequestApprovalRequest{
		Session: session, Kind: sdk.ApprovalKindQuestion, MaxWaitSeconds: approvalWaitS,
		Questions: []sdk.Question{{
			Header: "選擇", Text: questionText, AllowFreeText: true,
			Options: []sdk.QuestionOption{{Label: questionOptionA}, {Label: questionOptionB}},
		}},
	})
	if err != nil || out == nil || !out.Decided || out.Decision == nil {
		return "你沒有選"
	}
	return "你選了 " + out.Decision.Answers[questionText]
}

// askDescribed：選項帶說明、附開場白的那一題（WEB-85）。
func (s *Scripted) askDescribed(session string) string {
	out, err := s.host.RequestApproval(s.ctx, &sdk.EngineHostRequestApprovalRequest{
		Session: session, Kind: sdk.ApprovalKindQuestion, MaxWaitSeconds: approvalWaitS,
		Summary: describedIntro,
		Questions: []sdk.Question{{
			Header: "選說明", Text: describedText, AllowFreeText: true,
			Options: []sdk.QuestionOption{
				{Label: questionOptionA, Description: describedA},
				{Label: questionOptionB, Description: describedB},
			},
		}},
	})
	if err != nil || out == nil || !out.Decided || out.Decision == nil {
		return "你沒有選"
	}
	return "你選了 " + out.Decision.Answers[describedText]
}

// askMany：三題一起問、第一題可以多選、說明很長（WEB-122）；回覆照題目順序列出答案。
func (s *Scripted) askMany(session string) string {
	long := strings.Repeat("這個選項的說明寫得很長，", 4)
	opts := func(labels ...string) []sdk.QuestionOption {
		out := make([]sdk.QuestionOption, 0, len(labels))
		for _, l := range labels {
			out = append(out, sdk.QuestionOption{Label: l, Description: l + "：" + long})
		}
		return out
	}
	qs := []sdk.Question{
		{Header: "多選", Text: manyTexts[0], MultiSelect: true, AllowFreeText: true, Options: opts("甲", "乙", "丙", "丁")},
		{Header: "單選", Text: manyTexts[1], AllowFreeText: true, Options: opts("一", "二", "三", "四")},
		{Header: "短的", Text: manyTexts[2], Options: []sdk.QuestionOption{{Label: "好"}, {Label: "不要"}}},
	}
	out, err := s.host.RequestApproval(s.ctx, &sdk.EngineHostRequestApprovalRequest{
		Session: session, Kind: sdk.ApprovalKindQuestion, MaxWaitSeconds: approvalWaitS,
		Summary: describedIntro, Questions: qs,
	})
	if err != nil || out == nil || !out.Decided || out.Decision == nil {
		return "你沒有選"
	}
	answers := make([]string, 0, len(manyTexts))
	for _, q := range manyTexts {
		answers = append(answers, out.Decision.Answers[q])
	}
	return "你選了 " + strings.Join(answers, "／")
}

var manyTexts = []string{"假引擎問：第一題可以選好幾個？", "假引擎問：第二題選一個？", "假引擎問：第三題好不好？"}

const (
	describedIntro = "假引擎問之前的說明：兩個選項各有優缺點。"
	describedText  = "假引擎問：要哪一個（有說明）？"
	describedA     = "A 的說明：快"
	describedB     = "B 的說明：穩"
)

var _ sdk.EngineAPI = (*Scripted)(nil)
