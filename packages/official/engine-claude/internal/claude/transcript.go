package claude

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// Stats 記錄 Parse 略過了什麼（ECL-3：未知的 entry 類型略過並計數，不報錯）。
type Stats struct {
	Unknown   map[string]int // 不認得的 type → 次數
	Malformed int            // 不是合法 JSON 的行（例如寫到一半的最後一行）
}

// 認得、但不是對話的 entry 類型：略過，不算未知。
var ignoredEntryTypes = map[string]bool{
	"hg-fixture-meta": true, "mode": true, "permission-mode": true, "atis-latch": true,
	"file-history-snapshot": true, "ai-title": true, "attachment": true, "api-request": true,
	"api-request-shape": true, "api-request-blob": true, "last-prompt": true, "system": true,
	"queue-operation": true, "cost-state": true,
}

// 平台自己寫進去、不是對話的 user 文字開頭。
var internalUserPrefixes = []string{
	taskNotification, "<local-command-caveat>", "<command-name>", "<command-message>",
	"<local-command-stdout>", "<local-command-stderr>",
}

type transcriptEntry struct {
	Type      string `json:"type"`
	UUID      string `json:"uuid"`
	Timestamp string `json:"timestamp"`
	// IsMeta：平台自己加進對話的內容（讀照片時的「[Image: original …]」、跨 session 傳話）；前者不是人說的話（ECL-33）。
	IsMeta bool `json:"isMeta"`
	// IsAPIError：平台自己寫的錯誤訊息（額度用完、連不上那一行），存成 assistant entry；不是她說的話，帶出去標 is_error。
	IsAPIError bool `json:"isApiErrorMessage"`
	// IsCompactSummary：壓縮上下文（自動或 /compact）後 Claude Code 寫的「This session is being continued…」接續摘要，
	// 存成一筆 user entry、只給模型接續用，不是人說的話，不畫進對話（ECL-57）。
	IsCompactSummary bool `json:"isCompactSummary"`
	// SourceToolUseID：這筆是某個工具呼叫帶進來的內容（Skill 工具把整份技能說明寫成 isMeta 的 user entry）；看板已有工具膠囊，不另外畫（ECL-59）。
	SourceToolUseID string `json:"sourceToolUseID"`
	// ThinkingDisplay：這筆裡的 thinking 區塊怎麼顯示。新版 Claude Code 把她說給主人聽的旁白存成 thinking 區塊、
	// 值是 "updates"（畫面標 `· summary`），不再寫 text 區塊；真正的思考內容是空的（只留簽名）。ECL-54。
	ThinkingDisplay string `json:"thinkingDisplay"`
	Message         struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	// Attachment：她工作到一半時送進來的話（主人打字、別的 session 傳話），Claude Code 不寫成 user entry，
	// 只寫一筆 attachment（type = queued_command、queue-operation 的 reason = absorbed_mid_turn）。ECL-45。
	Attachment struct {
		Type        string          `json:"type"`
		Prompt      json.RawMessage `json:"prompt"`
		CommandMode string          `json:"commandMode"`
		Origin      struct {
			Kind string `json:"kind"`
		} `json:"origin"`
	} `json:"attachment"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// Parse 把 transcript JSONL 解析成對話。讀取失敗才回錯；壞行與未知類型略過。
func Parse(r io.Reader) iter.Seq2[sdk.ChatMessage, error] {
	return ParseStats(r, nil)
}

// ParseStats 同 Parse，並把略過的內容記進 stats（可為 nil）。
func ParseStats(r io.Reader, stats *Stats) iter.Seq2[sdk.ChatMessage, error] {
	return func(yield func(sdk.ChatMessage, error) bool) {
		br := bufio.NewReader(r)
		for {
			line, err := br.ReadBytes('\n')
			if len(strings.TrimSpace(string(line))) > 0 {
				for _, msg := range parseEntry(line, stats) {
					if !yield(msg, nil) {
						return
					}
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					yield(sdk.ChatMessage{}, err)
				}
				return
			}
		}
	}
}

func parseEntry(line []byte, stats *Stats) []sdk.ChatMessage {
	var e transcriptEntry
	if err := json.Unmarshal(line, &e); err != nil {
		if stats != nil {
			stats.Malformed++
		}
		return nil
	}
	ts, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
	switch e.Type {
	case "user":
		if e.IsCompactSummary {
			return nil
		}
		return userMessages(e, ts)
	case "assistant":
		return assistantMessages(e, ts)
	case "attachment":
		return queuedMessages(e, ts)
	}
	if !ignoredEntryTypes[e.Type] && stats != nil {
		if stats.Unknown == nil {
			stats.Unknown = map[string]int{}
		}
		stats.Unknown[e.Type]++
	}
	return nil
}

// uuidAt：一個 entry 拆出多則時，第一則沿用原 uuid，其餘加上序號。
func uuidAt(uuid string, i int) string {
	if i == 0 {
		return uuid
	}
	return uuid + "#" + strconv.Itoa(i)
}

func contentBlocks(raw json.RawMessage) (text string, blocks []contentBlock, isString bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil, true
	}
	_ = json.Unmarshal(raw, &blocks)
	return "", blocks, false
}

func userMessages(e transcriptEntry, ts time.Time) []sdk.ChatMessage {
	text, blocks, isString := contentBlocks(e.Message.Content)
	var out []sdk.ChatMessage
	add := func(m sdk.ChatMessage) {
		m.Uuid = uuidAt(e.UUID, len(out))
		m.Ts = ts
		out = append(out, m)
	}
	convert := userText
	if e.IsMeta {
		convert = func(t string) (sdk.ChatMessage, bool) { return metaText(t, e.SourceToolUseID != "") }
	}
	if isString {
		if m, ok := convert(text); ok {
			add(m)
		}
		return out
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if m, ok := convert(b.Text); ok {
				add(m)
			}
		case "tool_result":
			add(sdk.ChatMessage{Kind: sdk.ChatKindToolResult, Text: resultText(b.Content), CallID: b.ToolUseID, IsError: b.IsError})
		}
	}
	return out
}

// queuedMessages：工作中途被她讀進去的話（ECL-45）。背景工作通知（origin task-notification）不是對話；
// 其他照一般 user 文字處理（主人打字、別的 session 傳話的外框都由 userText 解）。
func queuedMessages(e transcriptEntry, ts time.Time) []sdk.ChatMessage {
	a := e.Attachment
	if a.Type != "queued_command" || a.Origin.Kind == "task-notification" || a.CommandMode == "task-notification" {
		return nil
	}
	text, blocks, isString := contentBlocks(a.Prompt)
	if !isString {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				parts = append(parts, b.Text)
			}
		}
		text = strings.Join(parts, "\n")
	}
	m, ok := userText(text)
	if !ok || m.Kind != sdk.ChatKindUser {
		return nil
	}
	m.Uuid, m.Ts = e.UUID, ts
	return []sdk.ChatMessage{m}
}

// userText 把一段 user 文字轉成對話；平台內部的通知回 false。
func userText(text string) (sdk.ChatMessage, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return sdk.ChatMessage{}, false
	}
	// 平台新版會在外框前多一行「Another Claude session sent a message:」，判斷前先拿掉。
	body := strings.TrimPrefix(trimmed, crossSessionPrefix+"\n")
	if strings.HasPrefix(body, agentMessage) { // subagent 的報告是給秘書看的：主人只看到一個「…」（ECL-51）
		return sdk.ChatMessage{Kind: sdk.ChatKindNotice, Text: "…"}, true
	}
	for _, p := range internalUserPrefixes {
		if strings.HasPrefix(body, p) {
			return sdk.ChatMessage{}, false
		}
	}
	if strings.HasPrefix(trimmed, interruptPrefix) {
		return sdk.ChatMessage{Kind: sdk.ChatKindInterrupt, Text: trimmed}, true
	}
	in := NormalizeInput(text)
	return sdk.ChatMessage{Kind: sdk.ChatKindUser, Text: in.Text, FromSession: in.From}, true
}

const skillBasePrefix = "Base directory for this skill:"

// metaNotices：平台 isMeta 文字開頭 → 看板那一小排的字（ECL-59）。
var metaNotices = []struct{ prefix, label string }{
	{"[Usage limit reached", "額度快用完了"},
	{"[Earlier usage-limit", "額度恢復了"},
	{"Your claude.ai usage limit has reset", "額度恢復了"},
	{"[Cross-session idle notice]", "對方的對話閒下來了"},
}

// metaText 處理平台自己加的 user 文字（isMeta，ECL-59）：都不是主人說的話。跨 session 傳話也是 isMeta，照一般話處理；
// 讀圖的說明、系統提醒、工具帶進來的內容不畫；其他變成一行通知。
func metaText(text string, fromTool bool) (sdk.ChatMessage, bool) {
	m, ok := userText(text)
	if !ok || m.Kind != sdk.ChatKindUser || NormalizeInput(text).Source != sdk.InputSourceTyped {
		return m, ok
	}
	trimmed := strings.TrimSpace(text)
	if fromTool || strings.HasPrefix(trimmed, "[Image") || strings.HasPrefix(trimmed, "<system-reminder>") {
		return sdk.ChatMessage{}, false
	}
	return sdk.ChatMessage{Kind: sdk.ChatKindNotice, Text: metaLabel(trimmed)}, true
}

func metaLabel(trimmed string) string {
	first, _, _ := strings.Cut(trimmed, "\n")
	if path, ok := strings.CutPrefix(first, skillBasePrefix); ok {
		path = strings.TrimRight(strings.TrimSpace(path), "/")
		return "載入技能 " + path[strings.LastIndexByte(path, '/')+1:]
	}
	for _, n := range metaNotices {
		if strings.HasPrefix(trimmed, n.prefix) {
			return n.label
		}
	}
	return "自動喚醒：" + trimmed // 看板一小排只畫第一行，點開看全文
}

func resultText(raw json.RawMessage) string {
	text, blocks, isString := contentBlocks(raw)
	if isString {
		return text
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func assistantMessages(e transcriptEntry, ts time.Time) []sdk.ChatMessage {
	text, blocks, isString := contentBlocks(e.Message.Content)
	var out []sdk.ChatMessage
	add := func(m sdk.ChatMessage) {
		m.Uuid = uuidAt(e.UUID, len(out))
		m.Ts = ts
		out = append(out, m)
	}
	if isString {
		if strings.TrimSpace(text) != "" {
			add(sdk.ChatMessage{Kind: sdk.ChatKindAssistant, Text: text, IsError: e.IsAPIError})
		}
		return out
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				add(sdk.ChatMessage{Kind: sdk.ChatKindAssistant, Text: b.Text, IsError: e.IsAPIError})
			}
		case "thinking":
			// 只收 thinkingDisplay = "updates" 而且有字的：那是她的旁白（ECL-54）；其他模式的思考不是說給主人聽的，不顯示。
			if e.ThinkingDisplay == "updates" && strings.TrimSpace(b.Thinking) != "" {
				add(sdk.ChatMessage{Kind: sdk.ChatKindAssistant, Text: strings.TrimSpace(b.Thinking)})
			}
		case "tool_use":
			// tool_label：跟處理中的狀態字同一套白話（ECL-53、SES-65），看板不認工具名。
			add(sdk.ChatMessage{Kind: sdk.ChatKindToolCall, Tool: b.Name, ToolLabel: activityLabel(b.Name), CallID: b.ID, Text: string(b.Input)})
		}
	}
	return out
}
