package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 呼叫核心的逾時。
const (
	reportTimeout   = 3 * time.Second
	contextTimeout  = 3 * time.Second
	checkTimeout    = 3 * time.Second
	approvalTimeout = 600 * time.Second
	// questionTimeout：選擇題等主人在看板回答多久才交回她的畫面（ECL-56；核心的 session.QuestionTimeout 一樣長）。
	questionTimeout = 12 * time.Hour
	// approvalRetryEvery：連不上核心（serve 重開、部署）時隔多久再問一次（ECL-56）。
	approvalRetryEvery = 2 * time.Second
	// approvalReconnectFor：連不上核心時最多再問多久，夠蓋過一次部署；不等到整個期限，
	// 免得主人在她畫面答過之後，核心很久才回來又冒出一題答過的（ECL-56）。
	approvalReconnectFor = 3 * time.Minute

	summaryLimit = 200

	coreDownDenyReason = "核心不在，別的秘書委派的寫入先不做"
)

// HookDeps 是 hook 入口要用到的外部東西（測試可注入）。
type HookDeps struct {
	Host   sdk.EngineHostAPI // 空＝依 EVERGENIE_API_SOCKET 連核心
	Getenv func(string) string
	Now    func() time.Time

	ReportTimeout   time.Duration // 0＝預設值
	ContextTimeout  time.Duration
	CheckTimeout    time.Duration
	ApprovalTimeout time.Duration
	QuestionTimeout time.Duration
	Sleep           func(time.Duration) // 空＝time.Sleep（測試換掉，不真的等）
}

func (d HookDeps) sleep(t time.Duration) {
	if d.Sleep != nil {
		d.Sleep(t)
		return
	}
	time.Sleep(t)
}

func (d HookDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func orDefault(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

type hookPayload struct {
	Prompt               string          `json:"prompt"`
	ToolName             string          `json:"tool_name"`
	ToolInput            json.RawMessage `json:"tool_input"`
	ToolResponse         json.RawMessage `json:"tool_response"`
	Reason               string          `json:"reason"`
	NotificationType     string          `json:"notification_type"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	BackgroundTasks      []any           `json:"background_tasks"`
	Error                string          `json:"error"`           // StopFailure：哪一種 API 錯誤（rate_limit 是額度用完）
	TranscriptPath       string          `json:"transcript_path"` // 對話記錄：額度用完時讀恢復時間（ECL-38）、壓縮時看是不是在一輪裡
	Source               string          `json:"source"`          // SessionStart：startup／resume／clear／compact
	Trigger              string          `json:"trigger"`         // PreCompact：auto（自動壓縮）／manual（主人打 /compact）
	SessionID            string          `json:"session_id"`      // SessionStart（source=clear）：清掉重來後的新對話編號（ECL-47）
	Cwd                  string          `json:"cwd"`
	Model                string          `json:"model"`    // SessionStart：這段對話用的模型代號（ECL-49）
	Effort               json.RawMessage `json:"effort"`   // Stop、工具、權限掛勾：`{"level": "medium"}`，這段對話現在的力度（ECL-49）
	AgentID              string          `json:"agent_id"` // 掛勾在 subagent 裡觸發時才有（平台的共通欄位）；主對話沒有（ECL-60）
}

// RunHook 執行 `hook <事件>`。永遠回 0：任何錯誤都靜默（REQ-77）。
// 只有 PermissionRequest、context、check 可能往 stdout 印東西。
func RunHook(ctx context.Context, event string, stdin io.Reader, stdout io.Writer, d HookDeps) (code int) {
	defer func() {
		if recover() != nil {
			code = 0
		}
	}()
	getenv := d.Getenv
	if getenv == nil {
		getenv = sdk.Getenv
	}
	home, socket, session := getenv("EVERGENIE_HOME"), getenv("EVERGENIE_API_SOCKET"), getenv("EVERGENIE_SESSION")
	if home == "" || socket == "" || session == "" {
		return 0
	}
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return 0
	}
	var p hookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return 0
	}
	switch event {
	case "SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "StopFailure", "SessionEnd", "Notification", "PreCompact":
		if staleLaunch(home, session, getenv(launchVar)) {
			return 0
		}
	}
	host := d.Host
	if host == nil {
		host = sdk.NewEngineHostClient(sdk.Dial(sdk.ResolveAPISocket(socket)))
	}
	h := hookRun{d: d, host: host, home: home, session: session, p: p, classify: ClassifyEnv{Home: getenv("HOME"), ConfigDir: sessionConfigDir(getenv)}}
	var out []byte
	switch event {
	case "SessionStart":
		// 自動壓縮後 Claude Code 會再發一次 SessionStart（source=compact），但對話沒換、還在做事：不回報，狀態照舊（DEV-94）。
		ev := &sdk.EngineEvent{Type: sdk.EngineEventTypeSessionStarted}
		if p.Source == "clear" && h.clearedTranscript() {
			ev.EngineSessionID = p.SessionID // 核心之後接回用新的編號（SES-61）
		}
		if p.Source != "compact" {
			h.report(ctx, ev)
		}
	case "UserPromptSubmit":
		h.userPromptSubmit(ctx)
	case "PreToolUse":
		h.toolEvent(ctx, sdk.EngineEventTypeActivityStarted)
	case "PostToolUse":
		h.toolEvent(ctx, sdk.EngineEventTypeActivityEnded)
	case "Stop":
		ev := &sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, BackgroundTasks: int32(len(p.BackgroundTasks)), LastMessage: p.LastAssistantMessage}
		// 這一輪實際用的模型：對話記錄最後一則她的回話（這一次開起來、主人上次換模型之後的，ECL-49）。
		// Stop 有時比這一輪的回話寫進對話記錄還早（2.1.310 實測：開起來第一輪讀不到），沒讀到就再等一下。
		if seen := stopModel(p.TranscriptPath, modelSince(home, session), h.d.sleep); seen.Model != "" {
			ev.Model, ev.ModelLabel = seen.Model, seen.Label
		}
		h.report(ctx, ev)
	case "StopFailure":
		h.stopFailure(ctx)
	case "SessionEnd":
		// 清掉重來（/clear）不是對話結束：同一個 Claude 接著用新的對話檔，緊接著 SessionStart（source=clear）。不回報，
		// 免得核心把她記成停了、把排著的話放回去（ECL-47）。
		if p.Reason != "clear" {
			h.report(ctx, &sdk.EngineEvent{Type: sdk.EngineEventTypeSessionEnded, Reason: sessionEndReason(p.Reason)})
		}
	case "PreCompact":
		h.preCompact(ctx)
	case "Notification":
		if p.NotificationType == "idle_prompt" {
			h.idlePrompt(ctx)
		}
	case "PermissionRequest":
		out = h.permissionRequest(ctx)
	case "context":
		out = h.contextText(ctx)
	case "check":
		out = h.check(ctx)
	}
	if len(out) > 0 {
		_, _ = stdout.Write(out)
	}
	return 0
}

// sessionEndReason 把 Claude Code SessionEnd 的 reason 換成 EverGenie 契約的詞（S5）：`logout`（主人在這個帳號下了 /logout）
// → sdk.SessionEndReasonLoggedOut，核心據此提醒主人重新登入；其他（clear、prompt_input_exit、other…）照原樣，核心不解讀。
func sessionEndReason(reason string) string {
	if reason == "logout" {
		return sdk.SessionEndReasonLoggedOut
	}
	return reason
}

// staleLaunch：送出這個 hook 的 Claude 不是這個名稱目前的那一次（DEV-10）。
// 重開時舊的 Claude 被砍掉後才觸發 SessionEnd，晚到的話會把剛開好的新 session 記成停了。
// 沒有記號檔（還沒用這版開過）就不擋；舊版開的 Claude 沒有記號，有記號檔就表示它已經被換掉了。
func staleLaunch(home, session, launch string) bool {
	data, err := os.ReadFile(launchFilePath(home, session))
	if err != nil {
		return false
	}
	var current string
	if json.Unmarshal(data, &current) != nil {
		return false
	}
	return current != launch
}

type hookRun struct {
	d        HookDeps
	host     sdk.EngineHostAPI
	home     string
	session  string
	p        hookPayload
	classify ClassifyEnv
}

// clearedTranscript：清掉重來之後平台換了新的對話檔（新的 session_id）。把記下來的對話檔位置改成新的編號，
// 看板的對話與引擎的確認（ECL-42）才讀得到新的那份；工作資料夾與帳號資料夾沿用原本的紀錄（ECL-47）。
// 回傳這次的編號是不是有效的新對話（要告訴核心）。
func (h *hookRun) clearedTranscript() bool {
	if !sessionIDPattern.MatchString(h.p.SessionID) {
		return false
	}
	r := readTranscriptRecord(h.home, h.session)
	if r == nil {
		if h.classify.ConfigDir == "" || h.p.Cwd == "" {
			return true
		}
		r = &transcriptRecord{Cwd: h.p.Cwd, ConfigDir: h.classify.ConfigDir}
	}
	if r.ID == h.p.SessionID {
		return true
	}
	r.ID = h.p.SessionID
	data, err := json.Marshal(r)
	if err != nil {
		return true
	}
	_ = writeFileAtomic(sessionFilePath(h.home, "transcripts", h.session), data, 0o644)
	return true
}

func (h *hookRun) report(ctx context.Context, ev *sdk.EngineEvent) {
	ev.Session = h.session
	ev.At = h.d.now()
	// 掛勾帶著的模型與力度一起回報（ECL-49）：沒帶就空著，核心不改。
	if ev.Model == "" && strings.TrimSpace(h.p.Model) != "" {
		ev.Model = strings.TrimSpace(h.p.Model)
		ev.ModelLabel = modelLabel(ev.Model)
	}
	if ev.Effort == "" {
		ev.Effort = hookEffort(h.p.Effort)
	}
	ctx, cancel := context.WithTimeout(ctx, orDefault(h.d.ReportTimeout, reportTimeout))
	defer cancel()
	_, _ = h.host.ReportEvent(ctx, &sdk.EngineHostReportEventRequest{Event: ev})
}

// ---- 輸入與回合來源 ----

type turnRecord struct {
	Source sdk.InputSource `json:"source"`
	From   string          `json:"from,omitempty"`
	At     time.Time       `json:"at"`
	// Head 是這一輪輸入（正規化後）的開頭：Deliver 拿來確認剛貼上的那句真的送出了（ECL-42）。
	Head string `json:"head,omitempty"`
}

// turnHeadLimit：turns 檔記輸入開頭的長度（字）。比對用的是第一行前 40 字，留寬一點給開頭的空行。
const turnHeadLimit = 400

func headOf(text string) string {
	r := []rune(text)
	if len(r) > turnHeadLimit {
		r = r[:turnHeadLimit]
	}
	return string(r)
}

func turnFilePath(home, session string) string { return sessionFilePath(home, "turns", session) }

// peersFilePath 記「寄件人位址 → 名字」：收件方回覆時 SendMessage 的 to 可能是寄件人外框裡的 uds: 位址，
// 這份對照讓直接傳話的收件人還原成名字（hook 是不同行程，只能靠檔案）。
func peersFilePath(home, session string) string { return sessionFilePath(home, "peers", session) }

func sessionFilePath(home, dir, session string) string {
	name := strings.NewReplacer("/", "_", "\\", "_").Replace(session)
	return sdk.RuntimeDir(home, "engine-claude", dir, name+".json")
}

func (h *hookRun) rememberPeer(addr, name string) {
	if addr == "" || name == "" {
		return
	}
	path := peersFilePath(h.home, h.session)
	peers := map[string]string{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &peers)
	}
	if peers[addr] == name {
		return
	}
	peers[addr] = name
	if data, err := json.Marshal(peers); err == nil {
		_ = writeFileAtomic(path, data, 0o644)
	}
}

// resolvePeer 把 SendMessage 的 to 若是已知寄件人的位址就換成名字；其他原樣。
func (h *hookRun) resolvePeer(to string) string {
	if !strings.HasPrefix(to, "uds:") {
		return to
	}
	data, err := os.ReadFile(peersFilePath(h.home, h.session))
	if err != nil {
		return to
	}
	peers := map[string]string{}
	if json.Unmarshal(data, &peers) != nil || peers[to] == "" {
		return to
	}
	return peers[to]
}

func (h *hookRun) userPromptSubmit(ctx context.Context) {
	in := NormalizeInput(h.p.Prompt)
	at := h.d.now()
	// 先寫檔：核心不在時 check 要靠它；Deliver 也靠它確認貼上的話真的送出了（ECL-42）。
	if data, err := json.Marshal(turnRecord{Source: in.Source, From: in.From, At: at, Head: headOf(in.Text)}); err == nil {
		_ = writeFileAtomic(turnFilePath(h.home, h.session), data, 0o644)
	}
	if in.Source == sdk.InputSourcePeer {
		addr, name := SenderAddress(h.p.Prompt)
		h.rememberPeer(addr, name)
	}
	h.report(ctx, &sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Input: in.Text, Source: in.Source, From: in.From, Interrupted: in.Interrupted})
}

// ---- 工具事件與直接傳話 ----

// toolLabels：看板「處理中」後面那幾個字（SES-63）：activity 是給人看的白話，工具原名（Read、Bash…）主人看不懂。
// 查不到的照原名；外掛工具（mcp__<服務>__<工具>）寫「用 <服務>」。
var toolLabels = map[string]string{
	"Read": "讀檔", "Edit": "改檔", "MultiEdit": "改檔", "Write": "寫檔", "NotebookEdit": "改筆記本",
	"Bash": "跑指令", "BashOutput": "看指令結果", "KillShell": "停指令", "Monitor": "等指令",
	"Grep": "找內容", "Glob": "找檔案", "LS": "看資料夾",
	"WebFetch": "看網頁", "WebSearch": "上網查",
	"Task": "派人去做", "Agent": "派人去做", "TodoWrite": "整理待辦",
	"SendMessage": "傳話", "AskUserQuestion": "問你", "Skill": "照技能做",
	"ToolSearch": "找工具", "ExitPlanMode": "寫計畫",
}

func activityLabel(tool string) string {
	if label, ok := toolLabels[tool]; ok {
		return label
	}
	if rest, ok := strings.CutPrefix(tool, "mcp__"); ok {
		if server, _, found := strings.Cut(rest, "__"); found && server != "" {
			return "用 " + server
		}
	}
	return tool
}

type sendMessageInput struct {
	To      string `json:"to"`
	Message string `json:"message"`
	Content string `json:"content"`
}

type sendMessageResponse struct {
	Success bool `json:"success"`
}

func (h *hookRun) toolEvent(ctx context.Context, typ sdk.EngineEventType) {
	// subagent 的工具事件不回報（ECL-60）：核心把 ActivityEnded 當成「畫面上的確認有人答了」，
	// 背景 subagent 跑完一個指令就會把她正在等主人回答的選擇題收掉，看板只剩「…」。主對話自己的事件照舊。
	// 例外是 SendMessage：傳話紀錄與回執（送到了／對方不在）要記，subagent 傳的也一樣（很少見，撞上等待中的選擇題的機會很小）。
	if h.p.AgentID != "" && h.p.ToolName != "SendMessage" {
		return
	}
	ev := &sdk.EngineEvent{Type: typ, Activity: activityLabel(h.p.ToolName)}
	if h.p.ToolName == "SendMessage" {
		var in sendMessageInput
		_ = json.Unmarshal(h.p.ToolInput, &in)
		text := in.Message
		if text == "" {
			text = in.Content
		}
		dm := &sdk.DirectMessage{To: h.resolvePeer(in.To), Text: text}
		if typ == sdk.EngineEventTypeActivityEnded {
			var resp sendMessageResponse
			_ = json.Unmarshal(h.p.ToolResponse, &resp)
			dm.OK = resp.Success
		}
		ev.DirectMessage = dm
	}
	h.report(ctx, ev)
}

// ---- 權限確認與問題 ----

type approvalOutput struct {
	HookSpecificOutput struct {
		HookEventName string           `json:"hookEventName"`
		Decision      approvalDecision `json:"decision"`
	} `json:"hookSpecificOutput"`
}

type approvalDecision struct {
	Behavior     string          `json:"behavior"`
	Message      string          `json:"message,omitempty"`
	UpdatedInput json.RawMessage `json:"updatedInput,omitempty"`
}

func approvalJSON(d approvalDecision) []byte {
	var o approvalOutput
	o.HookSpecificOutput.HookEventName = "PermissionRequest"
	o.HookSpecificOutput.Decision = d
	data, err := marshalNoEscape(o)
	if err != nil {
		return nil
	}
	return data
}

type askQuestionInput struct {
	Questions []struct {
		Question    string `json:"question"`
		Header      string `json:"header"`
		MultiSelect bool   `json:"multiSelect"`
		Options     []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
	} `json:"questions"`
}

// questionsFromInput 把 AskUserQuestion 的 tool_input 轉成中立的 Question（不改字）。
func questionsFromInput(raw json.RawMessage) []sdk.Question {
	var in askQuestionInput
	_ = json.Unmarshal(raw, &in)
	out := make([]sdk.Question, 0, len(in.Questions))
	for _, q := range in.Questions {
		item := sdk.Question{Header: q.Header, Text: q.Question, MultiSelect: q.MultiSelect, AllowFreeText: true}
		for _, o := range q.Options {
			item.Options = append(item.Options, sdk.QuestionOption{Label: o.Label, Description: o.Description})
		}
		out = append(out, item)
	}
	return out
}

// withAnswers 回傳原 tool_input 加上 answers（key 是題目全文）。
func withAnswers(raw json.RawMessage, answers map[string]string) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
	}
	encoded, err := marshalNoEscape(answers)
	if err != nil {
		return nil, err
	}
	fields["answers"] = encoded
	return marshalNoEscape(fields)
}

func (h *hookRun) permissionRequest(ctx context.Context) []byte {
	req := &sdk.EngineHostRequestApprovalRequest{Session: h.session}
	isQuestion := h.p.ToolName == "AskUserQuestion"
	wait := orDefault(h.d.ApprovalTimeout, approvalTimeout)
	if isQuestion {
		wait = orDefault(h.d.QuestionTimeout, questionTimeout)
	}
	if isQuestion {
		req.Kind = sdk.ApprovalKindQuestion
		req.Tool = h.p.ToolName
		req.Questions = questionsFromInput(h.p.ToolInput)
		// 問之前她說的那段話（選項的來龍去脈），看板在題目上面照樣列出（ECL-50）。
		req.Summary = questionIntro(transcriptTailOf(h.p.TranscriptPath))
	} else {
		req.Kind = sdk.ApprovalKindPermission
		req.Tool = h.p.ToolName
		req.Summary = summarizeInput(h.p.ToolInput)
	}
	deadline := h.d.now().Add(wait)
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	resp, err := h.requestApproval(ctx, req, deadline)
	if err != nil || resp == nil || !resp.Decided || resp.Decision == nil {
		return nil // 讓平台自己的確認畫面出來
	}
	dec := resp.Decision
	if isQuestion && (dec.Allow || len(dec.Answers) > 0) {
		if len(dec.Answers) == 0 {
			return approvalJSON(approvalDecision{Behavior: "allow"})
		}
		updated, err := withAnswers(h.p.ToolInput, dec.Answers)
		if err != nil {
			return nil
		}
		return approvalJSON(approvalDecision{Behavior: "allow", UpdatedInput: updated})
	}
	if dec.Allow {
		return approvalJSON(approvalDecision{Behavior: "allow"})
	}
	return approvalJSON(approvalDecision{Behavior: "deny", Message: dec.Message})
}

// requestApproval 問核心；連不上（不是核心回的錯，或核心說暫時不在）就每 approvalRetryEvery 再問一次，最多 approvalReconnectFor、不超過期限（ECL-56）：
// serve 重開（部署）時等著的選擇題不再立刻交回她的畫面；重開後核心把舊的那題當 native，新的請求取代它（SES-48）。
// 每次問都帶剩下的時間（MaxWaitSeconds），核心不會等超過期限。核心回的其他錯（衝突、找不到）照舊交回畫面。
func (h *hookRun) requestApproval(ctx context.Context, req *sdk.EngineHostRequestApprovalRequest, deadline time.Time) (*sdk.EngineHostRequestApprovalResponse, error) {
	// 每一段連不上各算 approvalReconnectFor：等過一陣子才斷的（核心接上了、後來才重開）重新算。
	// 次數上限給時鐘不走的時候當保險，也跟著重新算。
	maxTries := int(approvalReconnectFor/approvalRetryEvery) + 1
	var since time.Time
	tries := 0
	for {
		left := deadline.Sub(h.d.now())
		if left < time.Second {
			return nil, context.DeadlineExceeded
		}
		req.MaxWaitSeconds = int32(left / time.Second)
		began := h.d.now()
		resp, err := h.host.RequestApproval(ctx, req)
		if err == nil || !coreUnreachable(err) || ctx.Err() != nil {
			return resp, err
		}
		now := h.d.now()
		if since.IsZero() || now.Sub(began) > approvalRetryEvery {
			since, tries = now, 0 // 這一次是接上後等了一陣子才斷：新的一段
		}
		tries++
		if tries >= maxTries || now.Sub(since) >= approvalReconnectFor {
			return resp, err
		}
		h.d.sleep(approvalRetryEvery)
	}
}

// coreUnreachable：連線層的錯（連不上、斷線），或核心回 unavailable；核心回的其他錯不算。
func coreUnreachable(err error) bool {
	var se *sdk.Error
	if errors.As(err, &se) {
		return se.Code == sdk.ErrorCodeUnavailable
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// summarizeInput：command、file_path、url 取第一個有的值，都沒有就用 tool_input 的 JSON，截前 200 字。
func summarizeInput(raw json.RawMessage) string {
	fields := map[string]any{}
	_ = json.Unmarshal(raw, &fields)
	text := stringField(fields, "command", "file_path", "url")
	if text == "" {
		text = strings.TrimSpace(string(raw))
	}
	runes := []rune(text)
	if len(runes) > summaryLimit {
		return string(runes[:summaryLimit])
	}
	return text
}

// ---- context 與 check（會輸出的兩個）----

func (h *hookRun) contextText(ctx context.Context) []byte {
	in := NormalizeInput(h.p.Prompt)
	ctx, cancel := context.WithTimeout(ctx, orDefault(h.d.ContextTimeout, contextTimeout))
	defer cancel()
	resp, err := h.host.GetContext(ctx, &sdk.EngineHostGetContextRequest{Session: h.session, Input: in.Text})
	if err != nil || resp == nil || resp.Text == "" {
		return nil
	}
	text := resp.Text
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return []byte(text)
}

type checkOutput struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func checkJSON(decision, reason string) []byte {
	var o checkOutput
	o.HookSpecificOutput.HookEventName = "PreToolUse"
	o.HookSpecificOutput.PermissionDecision = decision
	o.HookSpecificOutput.PermissionDecisionReason = reason
	data, err := marshalNoEscape(o)
	if err != nil {
		return nil
	}
	return data
}

func (h *hookRun) check(ctx context.Context) []byte {
	class := Classify(h.classify, h.p.ToolName, h.p.ToolInput)
	cctx, cancel := context.WithTimeout(ctx, orDefault(h.d.CheckTimeout, checkTimeout))
	defer cancel()
	resp, err := h.host.CheckAction(cctx, &sdk.EngineHostCheckActionRequest{
		Session: h.session, Class: class, Tool: h.p.ToolName, Summary: summarizeInput(h.p.ToolInput),
	})
	if err != nil || resp == nil {
		return h.checkFallback(class)
	}
	switch resp.Verdict {
	case sdk.ActionVerdictDeny:
		return checkJSON("deny", resp.Reason)
	case sdk.ActionVerdictAsk:
		return checkJSON("ask", resp.Reason)
	}
	return nil // allow：什麼都不印，不繞過平台自己的權限與分類器
}

// checkFallback：核心不在或逾時。這一輪是別的秘書委派的，而且不是唯讀 → 擋下；其他不印。
func (h *hookRun) checkFallback(class sdk.ActionClass) []byte {
	if class == sdk.ActionClassRead {
		return nil
	}
	data, err := os.ReadFile(turnFilePath(h.home, h.session))
	if err != nil {
		return nil
	}
	var rec turnRecord
	if json.Unmarshal(data, &rec) != nil || rec.Source != sdk.InputSourcePeer {
		return nil
	}
	return checkJSON("deny", coreDownDenyReason)
}

func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
