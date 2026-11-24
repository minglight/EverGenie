package claude

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// classRank 由嚴到鬆（數字大＝嚴）。串起來的 Bash 取最嚴的。
var classRank = map[sdk.ActionClass]int{
	sdk.ActionClassSpend:        8,
	sdk.ActionClassSettings:     7,
	sdk.ActionClassDelete:       6,
	sdk.ActionClassPush:         5,
	sdk.ActionClassSendExternal: 4,
	sdk.ActionClassCommit:       3,
	sdk.ActionClassWrite:        2,
	sdk.ActionClassRead:         1,
}

func stricter(a, b sdk.ActionClass) sdk.ActionClass {
	if classRank[b] > classRank[a] {
		return b
	}
	return a
}

// readTools 是不會改任何東西的工具。
var readTools = map[string]bool{
	"Read": true, "Glob": true, "Grep": true, "LS": true, "WebSearch": true, "WebFetch": true,
	"ListAgents": true, "SendMessage": true, "AskUserQuestion": true, "TodoWrite": true,
	// Platform helpers that only load tools, read state, or start a subagent whose own
	// tool calls go through the same hooks; denying them would cripple delegated turns.
	"ToolSearch": true, "Skill": true, "Agent": true, "Task": true, "TaskList": true, "TaskGet": true,
	"TaskOutput": true, "BashOutput": true, "CronList": true, "ListMcpResourcesTool": true, "ReadMcpResourceTool": true,
}

var fileTools = map[string]bool{"Edit": true, "Write": true, "NotebookEdit": true}

// bashReadCommands 是整行只有這些就算唯讀的指令（git 與 EverGenie 另外處理）。
var bashReadCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "grep": true, "rg": true, "find": true,
	"wc": true, "pwd": true, "echo": true, "which": true, "cd": true, "true": true, "test": true,
}

var gitReadSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true, "branch": true, "rev-parse": true,
}

var mcpExternalWords = []string{"send", "post", "reply", "create", "update", "delete", "publish", "comment", "message", "invite"}
var mcpSpendWords = []string{"pay", "purchase", "checkout", "order"}

// ClassifyEnv 是分類用到的環境（測試可注入）。
type ClassifyEnv struct {
	Home      string // 使用者家目錄
	ConfigDir string // CLAUDE_CONFIG_DIR
}

// Classify 把一個工具呼叫分成 ActionClass。tool_input 是原始 JSON。
func Classify(env ClassifyEnv, tool string, input json.RawMessage) sdk.ActionClass {
	fields := map[string]any{}
	_ = json.Unmarshal(input, &fields)
	switch {
	case readTools[tool]:
		return sdk.ActionClassRead
	case fileTools[tool]:
		if isSettingsPath(env, stringField(fields, "file_path", "notebook_path", "path")) {
			return sdk.ActionClassSettings
		}
		return sdk.ActionClassWrite
	case tool == "Bash":
		return classifyBash(stringField(fields, "command"))
	case strings.HasPrefix(tool, "mcp__"):
		return classifyMCP(tool)
	default:
		return sdk.ActionClassWrite
	}
}

func classifyMCP(tool string) sdk.ActionClass {
	name := strings.ToLower(tool)
	for _, w := range mcpSpendWords {
		if strings.Contains(name, w) {
			return sdk.ActionClassSpend
		}
	}
	for _, w := range mcpExternalWords {
		if strings.Contains(name, w) {
			return sdk.ActionClassSendExternal
		}
	}
	return sdk.ActionClassRead
}

func stringField(fields map[string]any, names ...string) string {
	for _, n := range names {
		if s, ok := fields[n].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// isSettingsPath：路徑在帳號設定資料夾、~/.claude*、/etc，或檔名是 settings*.json。
func isSettingsPath(env ClassifyEnv, path string) bool {
	if path == "" {
		return false
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		path = filepath.Join(env.Home, strings.TrimPrefix(path, "~"))
	}
	path = filepath.Clean(path)
	base := filepath.Base(path)
	if strings.HasPrefix(base, "settings") && strings.HasSuffix(base, ".json") {
		return true
	}
	if within(path, "/etc") || (env.ConfigDir != "" && within(path, env.ConfigDir)) {
		return true
	}
	if env.Home != "" {
		rel, err := filepath.Rel(env.Home, path)
		if err == nil && !strings.HasPrefix(rel, "..") && strings.HasPrefix(rel, ".claude") {
			return true
		}
	}
	return false
}

func within(path, dir string) bool {
	dir = filepath.Clean(dir)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// ---- Bash ----

// splitSegments 依 &&、||、;、|、& 與換行拆段（引號與跳脫裡的不算）。
func splitSegments(cmd string) []string {
	var segs []string
	var cur strings.Builder
	var quote rune
	runes := []rune(cmd)
	flush := func() {
		segs = append(segs, cur.String())
		cur.Reset()
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote != 0:
			cur.WriteRune(r)
			if r == '\\' && quote == '"' && i+1 < len(runes) {
				i++
				cur.WriteRune(runes[i])
			} else if r == quote {
				quote = 0
			}
		case r == '\\' && i+1 < len(runes):
			cur.WriteRune(r)
			i++
			cur.WriteRune(runes[i])
		case r == '\'' || r == '"':
			quote = r
			cur.WriteRune(r)
		case r == ';' || r == '\n' || r == '|' || r == '&':
			// 「>&」「&>」是轉向，不是分段
			if r == '&' && ((i > 0 && runes[i-1] == '>') || (i+1 < len(runes) && runes[i+1] == '>')) {
				cur.WriteRune(r)
				continue
			}
			if (r == '|' || r == '&') && i+1 < len(runes) && runes[i+1] == r {
				i++
			}
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return segs
}

// shellWords 切成字，並回報有沒有引號外的 '>'、反引號或 "$("。
func shellWords(seg string) (words []string, redirect, subshell bool) {
	var cur strings.Builder
	var quote rune
	started := false
	runes := []rune(seg)
	push := func() {
		if started {
			words = append(words, cur.String())
		}
		cur.Reset()
		started = false
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(runes):
				i++
				cur.WriteRune(runes[i])
			default:
				if r == '`' || (r == '$' && i+1 < len(runes) && runes[i+1] == '(') {
					subshell = true
				}
				cur.WriteRune(r)
			}
		case r == '\\' && i+1 < len(runes):
			i++
			cur.WriteRune(runes[i])
			started = true
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t':
			push()
		case r == '>':
			// Duplicating a descriptor (2>&1, >&2) or discarding to /dev/null writes no file.
			for i+1 < len(runes) && runes[i+1] == '>' {
				i++
			}
			if i+1 < len(runes) && runes[i+1] == '&' {
				i++
				for i+1 < len(runes) && (runes[i+1] == '-' || (runes[i+1] >= '0' && runes[i+1] <= '9')) {
					i++
				}
				continue
			}
			j := i + 1
			for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t') {
				j++
			}
			k := j
			for k < len(runes) && runes[k] != ' ' && runes[k] != '\t' {
				k++
			}
			if string(runes[j:k]) == "/dev/null" {
				i = k - 1
				continue
			}
			redirect = true
			cur.WriteRune(r)
			started = true
		default:
			if r == '`' || (r == '$' && i+1 < len(runes) && runes[i+1] == '(') {
				subshell = true
			}
			cur.WriteRune(r)
			started = true
		}
	}
	push()
	return words, redirect, subshell
}

// heredocStart 找一行裡的 heredoc 開頭（<<EOF、<<-'EOF'、<<"EOF"；<<< 是 here-string，不算）。
var heredocStart = regexp.MustCompile(`<<(-?)[ \t]*(['"]?)([A-Za-z_][A-Za-z0-9_]*)['"]?`)

// stripHeredocs 拿掉 heredoc 的內文（DEV-96）：內文是餵給指令的資料，不是指令，不然內文寫到 rm、settings 就會誤判。
// 分隔字沒加引號、內文又有 $( 或反引號時，shell 會展開執行，這種保留成一行 $( 讓它至少算寫。
func stripHeredocs(command string) string {
	lines := strings.Split(command, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		out = append(out, lines[i])
		for _, m := range heredocStart.FindAllStringSubmatchIndex(lines[i], -1) {
			if m[0] > 0 && lines[i][m[0]-1] == '<' { // <<< 的後兩個 <
				continue
			}
			dash, quoted, word := lines[i][m[2]:m[3]] == "-", m[5] > m[4], lines[i][m[6]:m[7]]
			expands := false
			for i+1 < len(lines) {
				i++
				body := lines[i]
				if dash {
					body = strings.TrimLeft(body, "\t")
				}
				if body == word {
					break
				}
				if !quoted && (strings.Contains(body, "$(") || strings.Contains(body, "`")) {
					expands = true
				}
			}
			if expands {
				out = append(out, "echo $(heredoc)")
			}
		}
	}
	return strings.Join(out, "\n")
}

func classifyBash(command string) sdk.ActionClass {
	result := sdk.ActionClassRead
	seen := false
	parens, braces := 0, 0
	for _, seg := range splitSegments(stripHeredocs(command)) {
		words, redirect, subshell := shellWords(seg)
		words = stripGrouping(words, &parens, &braces)
		if len(words) == 0 {
			continue
		}
		seen = true
		class := classifySegment(words)
		if class == sdk.ActionClassRead && (redirect || subshell) {
			class = sdk.ActionClassWrite
		}
		result = stricter(result, class)
	}
	if !seen {
		return sdk.ActionClassWrite
	}
	return result
}

// stripGrouping 去掉 `( … )` 子殼與 `{ …; }` 群組的括號，讓裡面的指令照自己分類。
// parens、braces 跨段累計目前開著幾層，只有開著時才剝結尾的 ")" 與 "}"。
func stripGrouping(words []string, parens, braces *int) []string {
	for len(words) > 0 {
		switch {
		case strings.HasPrefix(words[0], "(") && !strings.HasPrefix(words[0], "(("):
			words[0] = words[0][1:]
			*parens++
		case words[0] == "{":
			words = words[1:]
			*braces++
		case words[0] == "":
			words = words[1:]
		default:
			goto tail
		}
	}
tail:
	for len(words) > 0 {
		last := words[len(words)-1]
		switch {
		case last == "}" && *braces > 0:
			words = words[:len(words)-1]
			*braces--
		case last == "":
			words = words[:len(words)-1]
		case strings.HasSuffix(last, ")") && *parens > 0 && !strings.Contains(last, "("):
			words[len(words)-1] = strings.TrimSuffix(last, ")")
			*parens--
		default:
			return words
		}
	}
	return words
}

var commandWrappers = map[string]bool{"sudo": true, "command": true, "time": true, "nohup": true, "env": true}

func isEnvAssignment(w string) bool {
	i := strings.IndexByte(w, '=')
	if i <= 0 {
		return false
	}
	for _, r := range w[:i] {
		if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func classifySegment(words []string) sdk.ActionClass {
	for len(words) > 0 && (isEnvAssignment(words[0]) || commandWrappers[words[0]]) {
		words = words[1:]
	}
	if len(words) == 0 {
		return sdk.ActionClassWrite
	}
	cmd := filepath.Base(words[0])
	args := words[1:]
	switch cmd {
	case "git":
		return classifyGit(args)
	case "gh":
		return classifyGH(args)
	case "rm", "rmdir":
		return sdk.ActionClassDelete
	case sdk.ProductName:
		return classifyEvergenie(args)
	case "find":
		for _, a := range args {
			if a == "-delete" || a == "-exec" || a == "-execdir" || a == "-ok" {
				return sdk.ActionClassWrite
			}
		}
		return sdk.ActionClassRead
	}
	if bashReadCommands[cmd] {
		return sdk.ActionClassRead
	}
	return sdk.ActionClassWrite
}

// gitSubcommand 跳過 git 的全域選項，回傳子指令與它後面的參數。
func gitSubcommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree" || a == "--namespace":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a, args[i+1:]
		}
	}
	return "", nil
}

func hasArg(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n {
				return true
			}
		}
	}
	return false
}

func classifyGit(args []string) sdk.ActionClass {
	sub, rest := gitSubcommand(args)
	switch sub {
	case "push":
		if hasArg(rest, "--force", "-f", "--force-with-lease") {
			return sdk.ActionClassDelete
		}
		return sdk.ActionClassPush
	case "rm", "clean":
		return sdk.ActionClassDelete
	case "reset":
		if hasArg(rest, "--hard") {
			return sdk.ActionClassDelete
		}
		return sdk.ActionClassWrite
	case "branch":
		if hasArg(rest, "-D") {
			return sdk.ActionClassDelete
		}
		return sdk.ActionClassRead
	case "commit":
		return sdk.ActionClassCommit
	}
	if gitReadSubcommands[sub] {
		return sdk.ActionClassRead
	}
	return sdk.ActionClassWrite
}

func classifyGH(args []string) sdk.ActionClass {
	var positional []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
		}
	}
	if len(positional) == 0 {
		return sdk.ActionClassWrite
	}
	if positional[0] == "release" {
		return sdk.ActionClassPush
	}
	if positional[0] == "pr" && len(positional) > 1 && (positional[1] == "create" || positional[1] == "merge") {
		return sdk.ActionClassPush
	}
	return sdk.ActionClassWrite
}

// classifyEvergenie：evergenie roster、say、docs（跟著程式的說明文件，只讀，PKG-67），或前兩個非選項的字裡有 list|get|show|stale|waiting 算唯讀；
// trust scan|diff|denied、status、sessions opened|peek|attach 也算唯讀（Q5、Q2）；setup（改權限等設定）算改設定。
// policy（只看說明）、policy list|show 唯讀，policy request 寫，policy decide|revoke 改設定（Q7）。
// packages（只看說明）、packages list|get|updates|preview|outdated|go-status 唯讀，packages reload|install|upgrade|install-go|export 寫（P6-1、REQ-282；export 會寫檔，SEC-302 S5）；packages sync 預演唯讀、--apply 寫；packages claim-notify（發認領通知，SEC-302 S9）照預設算寫；
// data kinds|list|get 唯讀，data add|set|rm 寫（P6-3a）。
// web sites|allow list 唯讀，web 的其他子指令（看板連多台的設定）改設定（S3-3）。
func classifyEvergenie(args []string) sdk.ActionClass {
	n := 0
	first := ""
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if n == 0 {
			first = a
		}
		if n == 0 && (a == "roster" || a == "say" || a == "status" || a == "docs") {
			return sdk.ActionClassRead
		}
		if n == 0 && (a == "policy" || a == "packages" || a == "data") && len(positionalAfter(args, a)) == 0 {
			return sdk.ActionClassRead
		}
		if n == 1 && first == "packages" && a == "sync" {
			// 預演只讀；--apply 才會新增秘書、改職責、加排程（D-28、REQ-174）。
			if argsHaveFlag(args, "--apply") {
				return sdk.ActionClassWrite
			}
			return sdk.ActionClassRead
		}
		if n == 1 && first == "data" && a == "kinds" {
			return sdk.ActionClassRead // data list|get 由下面的通則算讀，add|set|rm 預設算寫（D-28）
		}
		if n == 1 && first == "packages" && (a == "updates" || a == "preview" || a == "outdated" || a == "go-status") {
			// 只比較差異、寫一份差異清單到 runtime（P6-1）；preview／outdated 只抓來看、不安裝（REQ-282）；go-status 只看 Go 能不能用、不下載（packages PKG-65）；
			// reload、install、upgrade、install-go（下載 Go，要主人同意）照預設算寫。
			return sdk.ActionClassRead
		}
		if n == 1 && first == "sessions" && (a == "opened" || a == "peek" || a == "attach") {
			return sdk.ActionClassRead
		}
		if n == 0 && a == "setup" {
			return sdk.ActionClassSettings
		}
		if n == 0 && a == "quota" {
			return sdk.ActionClassRead
		}
		if n == 0 && a == "preflight" {
			// 只檢查是讀；--fix 會 brew install。
			if argsHaveFlag(args, "--fix") {
				return sdk.ActionClassWrite
			}
			return sdk.ActionClassRead
		}
		if n == 1 && (first == "service" || first == "tailscale") {
			if a == "status" {
				return sdk.ActionClassRead
			}
			return sdk.ActionClassSettings
		}
		if n == 1 && first == "backup" && a == "prune" {
			// 預演是讀；--apply 會刪備份。
			if argsHaveFlag(args, "--apply") {
				return sdk.ActionClassWrite
			}
			return sdk.ActionClassRead
		}
		if n == 1 && first == "trust" && (a == "scan" || a == "diff" || a == "denied") {
			return sdk.ActionClassRead
		}
		if n == 1 && first == "agents" && a == "link" {
			// 預演只讀；--apply 會寫各帳號的檔案。
			if argsHaveFlag(args, "--apply") {
				return sdk.ActionClassSettings
			}
			return sdk.ActionClassRead
		}
		if n == 1 && first == "config" && a == "set" {
			return sdk.ActionClassSettings
		}
		if n == 1 && first == "web" {
			// web sites|allow list 唯讀；其他（sites／allow add|rm、owner set|clear）改 home.toml，跟 config set 一樣算改設定（S3-3）。
			if rest := positionalAfter(args, a); len(rest) > 0 && rest[0] == "list" {
				return sdk.ActionClassRead
			}
			return sdk.ActionClassSettings
		}
		if n == 1 && first == "policy" {
			// 送申請是寫（只寫申請單）；審與撤銷會改權限，算改設定（Q7）。
			switch a {
			case "decide", "revoke":
				return sdk.ActionClassSettings
			case "request":
				return sdk.ActionClassWrite
			}
		}
		switch a {
		case "list", "get", "show", "stale", "waiting":
			return sdk.ActionClassRead
		}
		n++
		if n == 2 {
			break
		}
	}
	return sdk.ActionClassWrite
}

// positionalAfter 回傳第一個等於 word 的非選項參數之後的非選項參數。
func positionalAfter(args []string, word string) []string {
	var out []string
	found := false
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if found {
			out = append(out, a)
		} else if a == word {
			found = true
		}
	}
	return out
}

func argsHaveFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
