package claude

import (
	"bytes"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// questionIntroMax：題目前那段話最多帶幾個字（看板卡片放得下、手機不用捲太多）。
const questionIntroMax = 600

// questionIntro：她叫 AskUserQuestion 之前、這一輪說的最後一段話（ECL-50）。對話記錄裡那段話寫在 tool_use 之前，
// 看板的確認卡把它放在題目上面，主人不用往上翻就知道為什麼問、每個選項的背景。這一輪（最後一則主人或別人的話之後）
// 沒說話就回空的；太長從頭截、結尾加「…」。上一題答完（AskUserQuestion 的 tool_result）也算新的一段：
// 那之前的話是上一題的開場，不能沿用到這一題（DEV-153 新家第一天實測：第二題的卡片掛著第一題的開場白）。
func questionIntro(tail []byte) string {
	intro := ""
	asks := map[string]bool{}
	for m, err := range Parse(bytes.NewReader(tail)) {
		if err != nil {
			break
		}
		switch m.Kind {
		case sdk.ChatKindUser, sdk.ChatKindInterrupt:
			intro = ""
		case sdk.ChatKindToolCall:
			if m.Tool == "AskUserQuestion" && m.CallID != "" {
				asks[m.CallID] = true
			}
		case sdk.ChatKindToolResult:
			if asks[m.CallID] {
				intro = ""
			}
		case sdk.ChatKindAssistant:
			if t := strings.TrimSpace(m.Text); t != "" {
				intro = t
			}
		}
	}
	if r := []rune(intro); len(r) > questionIntroMax {
		return string(r[:questionIntroMax]) + "…"
	}
	return intro
}
