package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 額度用完（ECL-38，DEV-47 S4，REQ-378）：帳號的 5 小時／每週額度用完時，Claude 照樣觸發 UserPromptSubmit，
// 但這一輪沒有呼叫到模型：不觸發 Stop，而是 StopFailure（error = rate_limit），對話記錄最後一則是平台合成的
// assistant 訊息（isApiErrorMessage、apiError usage_limit_reached、rate_limit_info.resetsAt），畫面上那一行像
// 「You've hit your 5-hour limit · resets 6pm (Asia/Taipei)」。之後一分鐘照樣有 Notification idle_prompt。
// 這裡認出它與恢復時間，回報中立的 RATE_LIMITED（Until 認不出就留空）。

// limitTail：只讀對話記錄最後這麼多位元組找最後一則（記錄可能很大，hook 有 5 秒逾時）。
const limitTail = 256 << 10

// limitFresh：idle_prompt 時看到最後一則是額度用完、又認不出恢復時間，只在這段時間內算數（太舊的不再報）。
const limitFresh = 15 * time.Minute

// limitTextRe 認的是平台的那一行（整句，不是模型說到「limit」的話）。
var limitTextRe = regexp.MustCompile(`(?i)^\s*(?:you've|you have)\s+(?:hit|reached)\s+your\b.*\blimit\b|^\s*claude ai usage limit reached\b`)

// resetRe：「resets 6pm (Asia/Taipei)」「resets 6:30pm」「resets Nov 3, 9am (Asia/Taipei)」「resets at 18:00」。
var resetRe = regexp.MustCompile(`(?i)\bresets?\s+(?:at\s+)?(?:([a-z]{3,9})\.?\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(?:at\s+)?)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?(?:\s*\(([^)]+)\))?`)

// epochRe：舊版的「Claude AI usage limit reached|1793354400」。
var epochRe = regexp.MustCompile(`\|(\d{10})\b`)

func isLimitText(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && len(s) < 300 && limitTextRe.MatchString(s)
}

// parseReset 從那一行認出恢復時間；now 是這一行出現的時間（「6pm」指 now 之後的第一個 18:00）。認不出回零值。
func parseReset(s string, now time.Time) time.Time {
	if m := epochRe.FindStringSubmatch(s); m != nil {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return time.Unix(n, 0)
		}
	}
	m := resetRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}
	}
	loc := now.Location()
	if m[6] != "" {
		if l, err := time.LoadLocation(strings.TrimSpace(m[6])); err == nil {
			loc = l
		}
	}
	hour, _ := strconv.Atoi(m[3])
	minute := 0
	if m[4] != "" {
		minute, _ = strconv.Atoi(m[4])
	}
	switch strings.ToLower(m[5]) {
	case "am":
		if hour == 12 {
			hour = 0
		}
	case "pm":
		if hour < 12 {
			hour += 12
		}
	case "":
		if m[4] == "" {
			return time.Time{} // 只有一個數字、沒有 am/pm 也沒有分鐘：不是時間
		}
	}
	if hour > 23 || minute > 59 {
		return time.Time{}
	}
	local := now.In(loc)
	if m[1] != "" {
		month, ok := monthOf(m[1])
		day, _ := strconv.Atoi(m[2])
		if !ok || day < 1 || day > 31 {
			return time.Time{}
		}
		t := time.Date(local.Year(), month, day, hour, minute, 0, 0, loc)
		if t.Before(local.Add(-24 * time.Hour)) {
			t = t.AddDate(1, 0, 0) // 跨年：12 月底看到「resets Jan 2」
		}
		return t
	}
	t := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	if !t.After(local) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func monthOf(s string) (time.Month, bool) {
	s = strings.ToLower(s)
	if len(s) < 3 {
		return 0, false
	}
	for m := time.January; m <= time.December; m++ {
		if strings.HasPrefix(strings.ToLower(m.String()), s[:3]) {
			return m, true
		}
	}
	return 0, false
}

// 平台「恢復後自己接著做」（DEV-102）：撞額度那一刻，平台有時會在那一則之後寫一行 system／informational
// 「Usage limit reached · continuing automatically at 5:10am · esc to cancel」，恢復時自己接著做；不是每次都有
// （2026-11 實測 49 次撞額度只有 6 次），沒有這行的，恢復後平台不會動。這件事之後也可能被取消（按 Esc、Claude 重開、
// 連續撞額度、每週額度…），平台會再寫一行「Automatic continue cancelled／stopped／did not run」：以最新的一行為準。
var (
	autoResumeRe   = regexp.MustCompile(`(?i)\bcontinuing automatically\b|\bautomatic continue\b`)
	autoCanceledRe = regexp.MustCompile(`(?i)\bcancell?ed\b|\bstopped\b|\bdid not run\b|\bnot (?:resume|continue)\b|\bwon't (?:resume|continue)\b`)
)

// limitEntry 是對話記錄一行裡額度判斷要的欄位。
type limitEntry struct {
	Type              string          `json:"type"`
	Subtype           string          `json:"subtype"`
	Content           json.RawMessage `json:"content"` // system 那幾行的文字
	IsMeta            bool            `json:"isMeta"`
	IsCompactSummary  bool            `json:"isCompactSummary"` // 壓縮後的接續摘要，不是主人的話（ECL-57）
	IsAPIErrorMessage bool            `json:"isApiErrorMessage"`
	Error             string          `json:"error"`
	APIError          string          `json:"apiError"`
	Timestamp         time.Time       `json:"timestamp"`
	APIErrorParams    struct {
		RateLimitInfo struct {
			ResetsAt int64 `json:"resetsAt"`
		} `json:"rate_limit_info"`
	} `json:"apiErrorParams"`
	QuotaLimits struct {
		ResetsAt int64 `json:"resetsAt"`
	} `json:"quotaLimits"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// limitSeen 是對話記錄最後一則（user／assistant）是不是「額度用完」，以及恢復時間與那一則的時間。
// AutoResume／AutoResumeAt：那一則之後平台最新說的是「會自己接著做」（沒被取消）與那一行的時間。
// Prompt：額度用完那一則之前、最近一則使用者的話（略過 meta 與工具結果），用來確認撞額度的是哪一句。
type limitSeen struct {
	Limited      bool
	Until        time.Time
	At           time.Time
	AutoResume   bool
	AutoResumeAt time.Time
	Prompt       string
}

// transcriptLimit 讀對話記錄的尾巴，看最後一則 user／assistant 是不是平台合成的額度用完訊息。讀不到回零值。
func transcriptLimit(path string) limitSeen {
	if path == "" {
		return limitSeen{}
	}
	f, err := os.Open(path)
	if err != nil {
		return limitSeen{}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return limitSeen{}
	}
	start := info.Size() - limitTail
	if start < 0 {
		start = 0
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return limitSeen{}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return limitSeen{}
	}
	lines := bytes.Split(data, []byte("\n"))
	var resume struct {
		decided, on bool
		at          time.Time
	}
	for i := len(lines) - 1; i >= 0; i-- {
		e, ok := limitLine(lines[i])
		if !ok {
			continue
		}
		if e.Type == "system" {
			if resume.decided || e.Subtype != "informational" {
				continue
			}
			var text string
			if json.Unmarshal(e.Content, &text) == nil && autoResumeRe.MatchString(text) {
				resume.decided, resume.on, resume.at = true, !autoCanceledRe.MatchString(text), e.Timestamp
			}
			continue
		}
		if e.Type != "user" && e.Type != "assistant" {
			continue
		}
		if e.Type == "user" && (e.IsMeta || e.IsCompactSummary) {
			continue
		}
		if e.Type != "assistant" || !e.IsAPIErrorMessage {
			return limitSeen{}
		}
		text := entryText(e.Message.Content)
		if e.Error != "rate_limit" && !strings.Contains(e.APIError, "limit") && !isLimitText(text) {
			return limitSeen{}
		}
		s := limitSeen{Limited: true, At: e.Timestamp, AutoResume: resume.on, AutoResumeAt: resume.at, Prompt: promptBefore(lines[:i])}
		switch {
		case e.APIErrorParams.RateLimitInfo.ResetsAt > 0:
			s.Until = time.Unix(e.APIErrorParams.RateLimitInfo.ResetsAt, 0)
		case e.QuotaLimits.ResetsAt > 0:
			s.Until = time.Unix(e.QuotaLimits.ResetsAt, 0)
		default:
			at := e.Timestamp
			if at.IsZero() {
				at = time.Now()
			}
			s.Until = parseReset(text, at.In(time.Local))
		}
		return s
	}
	return limitSeen{}
}

// limitLine 解析對話記錄的一行（不是 JSON、或第一行被切掉一半的回 false）。
func limitLine(raw []byte) (limitEntry, bool) {
	var e limitEntry
	line := bytes.TrimSpace(raw)
	if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil {
		return e, false
	}
	return e, true
}

// promptBefore：往回找最近一則有文字的使用者的話（略過 meta、壓縮接續摘要與只有工具結果的）。
func promptBefore(lines [][]byte) string {
	for i := len(lines) - 1; i >= 0; i-- {
		e, ok := limitLine(lines[i])
		if !ok || e.Type != "user" || e.IsMeta || e.IsCompactSummary {
			continue
		}
		if text := entryText(e.Message.Content); strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

// entryText 把 message.content（字串或 [{type:text,text}]）接成一段文字。
func entryText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// stopFailure：一輪因為 API 錯誤結束（平台不觸發 Stop）。額度用完（error＝rate_limit，或最後一句是平台那一行）
// 回報 RATE_LIMITED，恢復時間先看對話記錄的 rate_limit_info.resetsAt，沒有再從那一行認；其他錯誤（過載、登入失效…）
// 回報 TURN_FAILED（ECL-41，failure.go）。
func (h *hookRun) stopFailure(ctx context.Context) {
	seen := transcriptLimit(h.p.TranscriptPath)
	// error 與最後一句都看不出來，但對話記錄最後一則是這次的額度用完（平台合成的那一則）：照樣算額度用完，不報「平台出錯」。
	if h.p.Error != "rate_limit" && !isLimitText(h.p.LastAssistantMessage) && !(seen.Limited && seen.still(h.d.now())) {
		h.turnFailed(ctx)
		return
	}
	var until time.Time
	if seen.Limited {
		until = seen.Until
	}
	if until.IsZero() {
		until = parseReset(h.p.LastAssistantMessage, h.d.now().In(time.Local))
	}
	h.report(ctx, &sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited, Until: until})
}

// idlePrompt：平台提示「閒著」。額度用完之後平台照樣會提示；對話記錄最後一則還是額度用完、而且還沒到恢復時間
// （說不出恢復時間的只看 limitFresh 內的）就回報 RATE_LIMITED，不報 IDLE_HINT——StopFailure 沒接上（舊設定開的對話）時的備援。
func (h *hookRun) idlePrompt(ctx context.Context) {
	if seen := transcriptLimit(h.p.TranscriptPath); seen.Limited && seen.still(h.d.now()) {
		h.report(ctx, &sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited, Until: seen.Until})
		return
	}
	h.report(ctx, &sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint})
}

// limitStranded：text 這句撞到額度用完就停了，而平台沒說會自己接著做（DEV-102）。這時對話記錄裡雖然有這句，
// 恢復後平台也不會動、不會再有事件——核心補送時不能回 already（那則會一直記成收到、後面的話全排著），要照常再貼一次。
//   - 撞額度的要是這句（額度那一則前面最近的使用者的話含這句開頭）：不然是後來別的一輪撞的，這句可能早就做完了；
//   - 「會自己接著做」的記號要在這次啟動之後（launches 檔的時間）：Claude 重開、--resume 接回時，舊的記號已經不算數。
func (e *Engine) limitStranded(name, text string) bool {
	s := e.transcriptSource(name)
	if s == nil {
		return false
	}
	seen := transcriptLimit(transcriptPath(s.configDir, s.cwd, s.id))
	if !seen.Limited || !strings.Contains(seen.Prompt, probeText(text, 40)) {
		return false
	}
	if !seen.AutoResume {
		return true
	}
	if strings.TrimSpace(e.d.Home) == "" {
		return false
	}
	launch, err := os.Stat(launchFilePath(e.d.Home, name))
	return err == nil && seen.AutoResumeAt.Before(launch.ModTime())
}

// still：這次額度用完現在還算數。
func (s limitSeen) still(now time.Time) bool {
	if !s.Until.IsZero() {
		return s.Until.After(now)
	}
	return !s.At.IsZero() && now.Sub(s.At) < limitFresh
}
