package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// 一輪因為平台的錯誤沒做完（ECL-41，S5-1）：平台不觸發 Stop，改觸發 StopFailure，`error` 是平台的錯誤名稱、
// `last_assistant_message` 是平台給的那一句（例如「API Error: Repeated 529 Overloaded errors」）。
// 額度用完（rate_limit）照舊報 RATE_LIMITED（ECL-38）；其他的報 TURN_FAILED，種類照下面的對照表（核心看種類決定
// 能不能「再說一次」、要不要主人出手），平台原話放 LastMessage 給主人看。

// platformErrorKinds：平台的 error 值 → EverGenie 的錯誤種類。沒列的（含 unknown 與以後的新值）一律 unknown。
// max_output_tokens（回覆太長被截斷）再說一次通常能接著做，歸 unknown，不歸 rejected。
var platformErrorKinds = map[string]sdk.EngineErrorKind{
	"authentication_failed":  sdk.EngineErrorKindAuth,
	"oauth_org_not_allowed":  sdk.EngineErrorKindAuth,
	"account_on_hold":        sdk.EngineErrorKindAuth,
	"cloud_credential_error": sdk.EngineErrorKindAuth,
	"verification_required":  sdk.EngineErrorKindAuth,
	"billing_error":          sdk.EngineErrorKindBilling,
	"overloaded":             sdk.EngineErrorKindOverloaded,
	"server_error":           sdk.EngineErrorKindOverloaded,
	"invalid_request":        sdk.EngineErrorKindRejected,
}

func platformErrorKind(platform string) sdk.EngineErrorKind {
	if k, ok := platformErrorKinds[strings.TrimSpace(platform)]; ok {
		return k
	}
	return sdk.EngineErrorKindUnknown
}

// turnFailed 回報 TURN_FAILED：種類照對照表，原話是平台那一句；平台沒附說明就填 sdk.EngineNoPlatformMessage
// （固定一句，核心認得、不當原話給主人看；發生什麼事由種類的白話原因講，DEV-156 U-08）。
func (h *hookRun) turnFailed(ctx context.Context) {
	kind := platformErrorKind(h.p.Error)
	msg := strings.TrimSpace(h.p.LastAssistantMessage)
	if msg == "" {
		msg = sdk.EngineNoPlatformMessage
	}
	h.report(ctx, &sdk.EngineEvent{Type: sdk.EngineEventTypeTurnFailed, Error: kind, LastMessage: msg})
}

// compactActivity 是自動整理對話時回報的事情（ECL-43）：讓畫面知道她在整理，不是當機。
const compactActivity = "整理對話"

// preCompact：平台要壓縮對話了。只有在一輪裡面的自動壓縮（之後照樣會有 Stop）回報 ACTIVITY_STARTED。
// 不在一輪裡的不報，報了會讓核心把她記成處理中、卻等不到這一輪的結束：
//   - 主人在終端機打的 /compact（manual）；
//   - 閒著時平台自己做的自動壓縮（trigger 也是 auto；2026-11-08 實測：一輪結束 54 分鐘後壓縮，看板卡「處理中」7 小時）。
//
// 壓縮完平台會再發一次 SessionStart（source = compact），照 DEV-94 不報；之後的工具事件或 Stop 自然蓋掉這件事。
func (h *hookRun) preCompact(ctx context.Context) {
	if h.p.Trigger != "auto" || !turnOpen(h.p.TranscriptPath) {
		return
	}
	h.report(ctx, &sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted, Activity: compactActivity})
}

// turnEntry 是對話記錄一行裡判斷「這一輪結束了沒」要的欄位。
type turnEntry struct {
	Type             string `json:"type"`
	Subtype          string `json:"subtype"`
	IsMeta           bool   `json:"isMeta"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	IsSidechain      bool   `json:"isSidechain"` // 背景小幫手的記錄，不算她這一輪
	IsAPIError       bool   `json:"isApiErrorMessage"`
	Message          struct {
		StopReason string          `json:"stop_reason"`
		Content    json.RawMessage `json:"content"`
	} `json:"message"`
}

// turnOpen 讀對話記錄的尾巴，看最後一件事是不是還在一輪中間。
// 一輪結束的記號：平台寫的 turn_duration／stop_hook_summary、她最後一則回話 stop_reason = end_turn、平台合成的 API 錯誤、主人按停止的那一行。
// 讀不到或認不出來就當成在一輪裡（照舊回報，寧可多報一下也不漏掉真的在整理）。
func turnOpen(path string) bool {
	data := transcriptTailOf(path)
	if data == nil {
		return true
	}
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		var e turnEntry
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil || e.IsSidechain {
			continue
		}
		switch e.Type {
		case "system":
			if e.Subtype == "turn_duration" || e.Subtype == "stop_hook_summary" {
				return false
			}
		case "assistant":
			// 平台合成的 API 錯誤（額度、過載…）也是一輪的結尾：StopFailure 已經報過，後面不會再有 Stop。
			return !e.IsAPIError && e.Message.StopReason != "end_turn"
		case "user":
			if e.IsMeta || e.IsCompactSummary {
				continue
			}
			return !strings.HasPrefix(strings.TrimSpace(entryText(e.Message.Content)), "[Request interrupted by user")
		}
	}
	return true
}
