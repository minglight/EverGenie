package claude

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// 確認貼上的話真的送出去了（ECL-42，S5-1 D4）：送 Enter 之後，引擎自己看到下面任一件事才回 delivered
// （S5-2 起核心不再自己隔 1 秒重查）：
//   - 平台觸發了這句的 UserPromptSubmit：hook 寫的 turns 檔時間在這次貼上之後，而且輸入開頭含這句的開頭；
//   - 對話記錄裡，這次貼上之後有一則使用者的話含這句的開頭（同 ECL-39，不放寬時間）；
//   - 畫面上的輸入框：送 Enter 前看得到這句（或「[Pasted text」），送 Enter 後不見了（她在忙時那句會排在輸入框外面等）。
//
// submitConfirmWithin 內都沒看到就回 delivered=false、reason "enter"；核心照退避再交來同一個封包，由重查（recheck）決定
// 只補 Enter、回 already、再等一下或重貼，引擎自己不重試。
const (
	submitConfirmWithin = 2 * time.Second
	submitPollInterval  = 200 * time.Millisecond
	// transcriptEvery：確認時每幾次才讀一次對話記錄（要讀最後 512KB 並解析，比 turns 檔與畫面貴）；最後一次一定讀。
	transcriptEvery = 5
	// transcriptSlack：重查時對話記錄的時間往前放寬多少（ECL-39）。
	transcriptSlack = 2 * time.Second
	// forgottenWindow：引擎不記得貼過什麼（重開過）時，對話記錄往回看多久來認「這句其實已經貼進去了」。
	// 比核心「貼上沒開始」重送的等候（10 分鐘）長；太長會把每天同一句的排程認成已經貼過。
	forgottenWindow = 30 * time.Minute
)

// reasonNotSubmitted：按了 Enter，但在 submitConfirmWithin 內沒看到送出。沿用既有的 "enter"（sdk 沒有「沒送出」的碼，
// 不為這個改 sdk）：核心照「送出鍵沒按成」的退避重試（S5-1）。
const reasonNotSubmitted = "enter"

const pastedMarker = "[Pasted text"

// placeholderPrefix：空輸入框的提示字（樣本「❯ Try "write a test for <filepath>"」），比對前拿掉，免得短句剛好中。
const placeholderPrefix = `Try "`

// inputBox 找畫面上的輸入框：最後兩條橫線（整行只有「─」）中間那幾行，去掉提示字。樣本：
// testdata/spike/screens/remote-control-active.txt。找不到回 ok=false。
func inputBox(screen string) (string, bool) {
	lines := strings.Split(screen, "\n")
	bottom := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if !isRule(lines[i]) {
			continue
		}
		if bottom < 0 {
			bottom = i
			continue
		}
		var box []string
		for _, l := range lines[i+1 : bottom] {
			t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), dialogCursor))
			if strings.HasPrefix(t, placeholderPrefix) {
				continue
			}
			box = append(box, l)
		}
		return strings.Join(box, "\n"), true
	}
	return "", false
}

// isRule：整行只有「─」（至少 3 個），或輸入框上緣帶 session 名字的那種：左邊一長串「─」、名字、右邊一小段「─」
// （樣本「────…──── health ─」，testdata/spike/screens/labeled-rule.txt；ECL-48）。
func isRule(line string) bool {
	t := strings.TrimSpace(line)
	if len([]rune(t)) >= 3 && strings.Trim(t, "─") == "" {
		return true
	}
	return isLabeledRule(t)
}

// 帶名字的上緣線：左邊至少 labeledRuleLeft 個「─」，名字 1～labeledRuleName 個字（不含畫框的字），右邊 1～labeledRuleRight 個「─」。
// 對話裡的 markdown 分隔線或表格不會長這樣（名字兩邊都要有線，右邊只有一小段）。
const (
	labeledRuleLeft  = 10
	labeledRuleName  = 40
	labeledRuleRight = 4
)

func isLabeledRule(t string) bool {
	r := []rune(t)
	left := 0
	for left < len(r) && r[left] == '─' {
		left++
	}
	right := 0
	for right < len(r)-left && r[len(r)-1-right] == '─' {
		right++
	}
	if left < labeledRuleLeft || right < 1 || right > labeledRuleRight {
		return false
	}
	mid := r[left : len(r)-right]
	if len(mid) < 3 || mid[0] != ' ' || mid[len(mid)-1] != ' ' {
		return false
	}
	label := strings.TrimSpace(string(mid))
	n := len([]rune(label))
	if n == 0 || n > labeledRuleName {
		return false
	}
	for _, c := range label {
		if c >= 0x2500 && c <= 0x257F { // 畫框的字（─│┌┐└┘├┤…）：不是名字
			return false
		}
	}
	return true
}

// outsideBox：畫面上輸入框以外的部分（找不到輸入框就是整個畫面）。
func outsideBox(screen string) string {
	lines := strings.Split(screen, "\n")
	bottom := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if !isRule(lines[i]) {
			continue
		}
		if bottom < 0 {
			bottom = i
			continue
		}
		return strings.Join(append(append([]string{}, lines[:i]...), lines[bottom+1:]...), "\n")
	}
	return screen
}

// holdsText：這段畫面裡看得到這句（第一行前 12 字；長文字被收成「[Pasted text …]」也算）。
func holdsText(region, text string) bool {
	return strings.Contains(region, probeText(text, 12)) || strings.Contains(region, pastedMarker)
}

// boxHolds：輸入框裡有這句。found=false 表示畫面上找不到輸入框。
func boxHolds(screen, text string) (holds, found bool) {
	box, ok := inputBox(screen)
	if !ok {
		return false, false
	}
	return holdsText(box, text), true
}

// promptSeen：since 之後平台已經收下這句（UserPromptSubmit 或對話記錄）。slack 是對話記錄往前放寬的時間。
func (e *Engine) promptSeen(name, text string, since time.Time, slack time.Duration) bool {
	return e.turnSince(name, text, since) || e.inTranscript(name, text, since, slack)
}

// turnSince：hook 在 UserPromptSubmit 寫的 turns 檔，時間不早於 since、輸入開頭含這句的開頭。
func (e *Engine) turnSince(name, text string, since time.Time) bool {
	if strings.TrimSpace(e.d.Home) == "" {
		return false
	}
	data, err := os.ReadFile(turnFilePath(e.d.Home, name))
	if err != nil {
		return false
	}
	var rec turnRecord
	if json.Unmarshal(data, &rec) != nil || rec.Head == "" || rec.At.Before(since) {
		return false
	}
	return strings.Contains(rec.Head, probeText(text, 40))
}

// confirmSubmit 在送 Enter 之後等平台收下這句（ECL-42）。sawInBox：送 Enter 前在輸入框裡看到這句（輸入框清空才算數）。
// ctx 被取消（引擎要關）就不等了，回 false。
func (e *Engine) confirmSubmit(ctx context.Context, name, text string, since time.Time, sawInBox bool) bool {
	deadline := e.now().Add(submitConfirmWithin)
	for i := 0; ; i++ {
		last := !e.now().Before(deadline) || ctx.Err() != nil
		if e.turnSince(name, text, since) {
			return true
		}
		if sawInBox {
			if screen, err := e.d.Tmux.Capture(name); err == nil {
				if holds, found := boxHolds(screen, text); found && !holds {
					return true
				}
			}
		}
		if (last || i%transcriptEvery == 0) && e.inTranscript(name, text, since, 0) {
			return true
		}
		if last {
			return false
		}
		e.sleep(submitPollInterval)
	}
}
