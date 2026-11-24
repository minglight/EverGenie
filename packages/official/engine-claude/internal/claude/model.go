package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"time"
)

// 這段對話現在用的模型與思考力度（ECL-49、ENG-18、D-46）：Claude Code 自己說的才報，說不出就不報（核心沿用開的時候的設定）。
// 來源（2.1.309 實錄，testdata/spike/hooks/）：
//   - SessionStart 掛勾的 `model`（模型代號，例 claude-haiku-5-5）；
//   - Stop、PreToolUse、PostToolUse、PermissionRequest 掛勾的 `effort.level`（這段對話現在的力度）；
//   - Stop 時對話記錄最後一則她的回話（assistant）的 `message.model`（這一輪實際用的模型）；
//   - /model 做成之後對話記錄的「Set model to `Opus 5.5` …」（給人看的名字）。

// 模型代號：claude-<家族>-<主>[-<次>][-<日期>]（現在的寫法），claude-<主>[-<次>]-<家族>[-<日期>]（舊的寫法）。
var (
	modelID       = regexp.MustCompile(`^claude-([a-z]+)-(\d+)(?:-(\d{1,2}))?(?:-\d{8})?$`)
	legacyModelID = regexp.MustCompile(`^claude-(\d+)(?:-(\d{1,2}))?-([a-z]+)(?:-\d{8})?$`)
	// setModelOut：/model 做成之後平台的回覆（`<local-command-stdout>Set model to `Opus 5.5` and saved …`）。
	setModelOut = regexp.MustCompile("^<local-command-stdout>Set model to `([^`]{1,60})`")
)

// modelAliases 是 --model、/model 收的別名（ECL-46 的 commandModels 以外，帳號設定也可能寫 fable）。
var modelAliases = map[string]bool{"sonnet": true, "opus": true, "haiku": true, "fable": true}

// modelLabel 把模型代號或別名換成給人看的名字：claude-opus-5-5 → Opus 5.5、claude-sonnet-5 → Sonnet 5、opus → Opus。
// 認不出的回空（看板顯示原字，不猜）。
func modelLabel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	base, _, _ := strings.Cut(model, "[") // opus[1m]：後面是平台的附加設定，名字只看前面
	if modelAliases[base] {
		return title(base)
	}
	if m := modelID.FindStringSubmatch(base); m != nil {
		return title(m[1]) + " " + version(m[2], m[3])
	}
	if m := legacyModelID.FindStringSubmatch(base); m != nil {
		return title(m[3]) + " " + version(m[1], m[2])
	}
	return ""
}

func title(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

func version(major, minor string) string {
	if minor == "" {
		return major
	}
	return major + "." + minor
}

// hookEffort 讀掛勾帶的力度：`{"level": "medium"}`（2.1.309 的樣子）；寫成字串的也收。認不出回空。
func hookEffort(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		Level string `json:"level"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Level != "" {
		return effortWord(obj.Level)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return effortWord(s)
	}
	return ""
}

// effortWord 只收一個小寫的詞（核心收的格式，SES-59）；其他回空。
func effortWord(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if !commandArgWord.MatchString(s) {
		return ""
	}
	return s
}

var commandArgWord = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

// transcriptModelEntry 是對話記錄裡跟模型有關的欄位。
type transcriptModelEntry struct {
	Type        string    `json:"type"`
	Timestamp   time.Time `json:"timestamp"`
	IsSidechain bool      `json:"isSidechain"`
	Message     struct {
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// modelSeen 是對話記錄裡 since 之後最後看到的模型：Model 是她回話用的代號（空＝最後一次是換模型、還沒回過話），
// Label 是給人看的名字（換模型的回覆寫的，或從代號換的）。
type modelSeen struct {
	Model string
	Label string
}

// transcriptModel 讀對話記錄的一段（JSONL），找 since 之後最後一次的模型：她的回話（不算 subagent、不算平台合成的
// `<synthetic>`），或 /model 的回覆。since 是零值就全部看。
func transcriptModel(data []byte, since time.Time) modelSeen {
	var seen modelSeen
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.Contains(line, []byte(`"model"`)) && !bytes.Contains(line, []byte("Set model to")) {
			continue
		}
		var e transcriptModelEntry
		if json.Unmarshal(line, &e) != nil || e.IsSidechain || (!since.IsZero() && e.Timestamp.Before(since)) {
			continue
		}
		switch e.Type {
		case "assistant":
			if m := strings.TrimSpace(e.Message.Model); m != "" && !strings.HasPrefix(m, "<") {
				seen = modelSeen{Model: m, Label: modelLabel(m)}
			}
		case "user":
			var text string
			if json.Unmarshal(e.Message.Content, &text) != nil {
				continue
			}
			if m := setModelOut.FindStringSubmatch(text); m != nil {
				label := m[1]
				if l := modelLabel(label); l != "" { // 「Set model to `claude-sonnet-5`」：寫的是代號
					label = l
				}
				seen = modelSeen{Label: label}
			}
		}
	}
	return seen
}

// modelSince：Stop 掛勾讀對話記錄從哪裡開始算。取兩個時間比較晚的那個：
//   - 這一次開起來的時間（啟動記號檔，DEV-10）：接回舊對話時記錄裡有上一次的回話，開之前的不算這一次的；
//   - 主人最近一次 /model 做成的時間（modelFilePath）：她忙著時換了模型，這一輪前面的回話還是舊模型，
//     不能拿來蓋掉主人剛換的（核心已經記了新的，SES-60）。
//
// 都讀不到回零值（全部看）。
func modelSince(home, session string) time.Time {
	var since time.Time
	for _, path := range []string{launchFilePath(home, session), modelFilePath(home, session)} {
		if info, err := os.Stat(path); err == nil && info.ModTime().After(since) {
			since = info.ModTime()
		}
	}
	return since
}

// modelFilePath：主人最近一次 /model 做成的記號（只看檔案時間）。
func modelFilePath(home, session string) string { return sessionFilePath(home, "models", session) }

// markModelSwitch 記下這次 /model 是什麼時候貼進去的（檔案時間設成貼上那一刻）。
func markModelSwitch(home, session string, at time.Time) {
	path := modelFilePath(home, session)
	if writeFileAtomic(path, []byte("{}"), 0o644) == nil {
		_ = os.Chtimes(path, at, at)
	}
}

// stopModelTries：Stop 掛勾讀不到這一輪的回話時最多再讀幾次（每次隔 stopModelWait），合起來不到一秒，
// 掛勾的時限是 5 秒。
const (
	stopModelTries = 8
	stopModelWait  = 100 * time.Millisecond
)

// stopModel：Stop 掛勾找這一輪實際用的模型。Claude Code 有時先叫 Stop、才把回話寫進對話記錄，
// 這時 since 之後一則回話都沒有，等一下再讀；還是沒有就回空的（核心不改）。
func stopModel(path string, since time.Time, sleep func(time.Duration)) modelSeen {
	if path == "" {
		return modelSeen{}
	}
	if _, err := os.Stat(path); err != nil {
		return modelSeen{}
	}
	for i := 0; ; i++ {
		if seen := transcriptModel(transcriptTailOf(path), since); seen.Model != "" || i >= stopModelTries {
			return seen
		}
		sleep(stopModelWait)
	}
}
