package claude

import (
	"regexp"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

const (
	crossSessionOpen   = "<cross-session-message "
	crossSessionClose  = "</cross-session-message>"
	crossSessionPrefix = "Another Claude session sent a message:"
	taskNotification   = "<task-notification>"
	// agentMessage：她開的 subagent 做完交回的報告（Claude Code 包成 <agent-message from="…">）；不是主人打的字（ECL-51）。
	agentMessage    = "<agent-message "
	interruptPrefix = "[Request interrupted by user"
	pastedOpen      = "<pasted_content"
)

var fromNameAttr = regexp.MustCompile(`from-name="([^"]*)"`)
var pastedCloseTag = regexp.MustCompile(`</pasted_content(?:\s[^>]*)?>$`)
var fromAddrAttr = regexp.MustCompile(`(?:^|\s)from="([^"]*)"`)

// Input 是正規化後的一則輸入（UserPromptSubmit → TURN_STARTED）。
type Input struct {
	Text        string
	Source      sdk.InputSource
	From        string
	Interrupted bool
}

// NormalizeInput 把平台給的 prompt 轉成來源、對方名稱與乾淨的文字。
func NormalizeInput(prompt string) Input {
	in := Input{Source: sdk.InputSourceTyped}
	text := prompt
	if strings.HasPrefix(text, interruptPrefix) {
		in.Interrupted = true
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = strings.TrimLeft(text[i+1:], "\r\n")
		} else {
			text = ""
		}
	}
	text = unwrapPasted(text)
	text = strings.TrimPrefix(text, crossSessionPrefix+"\n")
	switch {
	case strings.HasPrefix(text, crossSessionOpen):
		in.Source = sdk.InputSourcePeer
		in.From, in.Text = unwrapCrossSession(text)
	case strings.HasPrefix(text, taskNotification), strings.HasPrefix(text, agentMessage):
		in.Source = sdk.InputSourceSystem
		in.Text = text
	default:
		in.Text = text
	}
	return in
}

// unwrapCrossSession 取出 from-name 與外框裡的文字；外框後面平台附的說明不算內容。
func unwrapCrossSession(text string) (from, body string) {
	open := strings.IndexByte(text, '>')
	if open < 0 {
		return "", text
	}
	if m := fromNameAttr.FindStringSubmatch(text[:open]); m != nil {
		from = m[1]
	}
	rest := text[open+1:]
	if end := strings.Index(rest, crossSessionClose); end >= 0 {
		rest = rest[:end]
	}
	rest = strings.TrimPrefix(rest, "\n")
	rest = strings.TrimSuffix(rest, "\n")
	return from, rest
}

// unwrapPasted 去掉整段被 <pasted_content …>…</pasted_content> 包住的外框；結尾標籤可能也帶屬性（`</pasted_content id="cb09">`，REQ-399）。
func unwrapPasted(text string) string {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, pastedOpen) {
		return text
	}
	loc := pastedCloseTag.FindStringIndex(t)
	if loc == nil {
		return text
	}
	open := strings.IndexByte(t, '>')
	if open < 0 || open >= loc[0] {
		return text
	}
	inner := t[open+1 : loc[0]]
	inner = strings.TrimPrefix(inner, "\n")
	inner = strings.TrimSuffix(inner, "\n")
	return inner
}

// SenderAddress 取出跨 session 訊息外框的 from（寄件人的位址，例如 uds:/tmp/cc-socks/123.sock）與 from-name；
// 不是跨 session 訊息就回空字串。收件方回覆時，SendMessage 的 to 可能填這個位址而不是名字。
func SenderAddress(prompt string) (addr, name string) {
	text := unwrapPasted(prompt)
	text = strings.TrimPrefix(text, crossSessionPrefix+"\n")
	if !strings.HasPrefix(text, crossSessionOpen) {
		return "", ""
	}
	open := strings.IndexByte(text, '>')
	if open < 0 {
		return "", ""
	}
	if m := fromAddrAttr.FindStringSubmatch(text[:open]); m != nil {
		addr = m[1]
	}
	if m := fromNameAttr.FindStringSubmatch(text[:open]); m != nil {
		name = m[1]
	}
	return addr, name
}
