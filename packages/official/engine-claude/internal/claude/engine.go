package claude

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// Version 是引擎套件的版本（Capabilities 回報）。
const Version = "0.1.0"

const (
	pasteEnterDelay   = time.Second            // 貼上後等 1 秒才送 Enter（spike 07）
	answerSettle      = 300 * time.Millisecond // 在確認框按了選項的數字之後，等這麼久再看一次畫面（ECL-37）
	answerChecks      = 6                      // 代按選擇題後最多看幾次畫面確認收掉（每次隔 answerSettle，ECL-58）
	bootWatchDuration = 20 * time.Second       // Start 後這段時間內找啟動對話框
	bootPollInterval  = time.Second
	// AliveInterval 是 serve 檢查自己開的 session 還在不在的間隔。
	AliveInterval = 30 * time.Second
	// startupAliveDelay 是 engine 起來後第一次存活檢查的時間（ECL-31）。
	startupAliveDelay = 3 * time.Second

	defaultPermissionMode = "auto"
)

// 啟動對話框的選項（信任＝Down、Enter；預設選項是離開，所以離開只要 Enter）。
const (
	bootChoiceTrust = "信任這個資料夾"
	bootChoiceExit  = "不信任（離開）"
)

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// EngineDeps 是 Engine 要用的外部東西；測試注入假的。
type EngineDeps struct {
	Home      string // EVERGENIE_HOME
	HookBin   string // 本套件執行檔的絕對路徑（hooks 呼叫它）
	APISocket string // 核心 API socket（HostEndpoint 沒給時用）
	ClaudeBin string // 空＝claude
	PluginDir string // 官方技能的外掛資料夾（本套件資料夾）；有值就在開 session 時帶 --plugin-dir（ECL-36）

	Tmux    Tmux
	Host    sdk.EngineHostAPI // 回報 session 結束用；空＝不回報
	Now     func() time.Time  // 空＝time.Now
	Sleeper func(time.Duration)
	Environ func() []string // 空＝os.Environ
	NewID   func() string   // 產生 session id；空＝隨機 UUID

	// Run 執行外部指令（額度暖機、檢查工具）；空＝os/exec。測試注入假的，不碰真的 claude。
	Run func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)
	// LookPath 找執行檔；空＝exec.LookPath。
	LookPath func(name string) (string, error)
	// Log 記一行「引擎自己動了手或要主人知道」的事（替她按掉擋住的選單、收掉舊視窗；ECL-66～ECL-68）；空＝不記。
	// serve 寫進 <狀態>/engine-claude/out.log。
	Log func(string)
}

type engineConfig struct {
	ConfigDir      string
	DefaultLogin   bool // engine_config.default_login：用 Claude 自己的預設登入，開 claude 不設 CLAUDE_CONFIG_DIR（ECL-64，account.go）
	TmuxSocket     string
	Isolate        bool
	PermissionMode string
	ModeExplicit   bool     // engine_config.permission_mode 有寫：照它，不依權限等級換（ECL-14）
	FullDirs       []string // full 等級可以直接改檔的資料夾（絕對路徑，存在的才算）
	EnvFile        string   // engine_config.env_file：開 session 前載入的 shell 檔（例 ~/.secret.sh），讓秘書拿到跟終端機一樣的 token（DEV-5）
}

type session struct {
	name      string
	id        string
	cwd       string
	configDir string
	cancel    context.CancelFunc // 停掉啟動對話框的偵測
	// listed 是主人設定裡本來就列的資料夾（工作資料夾、家、--add-dir 帶的、設定檔 additionalDirectories），
	// 信任框問的是這些（或底下）就由引擎替主人按 Yes（DEV-162，作者 2026-11-17 定）。
	listed []string
}

// Engine 實作 sdk.EngineAPI（ECL-1～ECL-9）。
type Engine struct {
	d   EngineDeps
	gen Generator

	mu       sync.Mutex
	sessions map[string]*session
	prompts  map[string]*bootPrompt
	locks    map[string]*sync.Mutex
	wg       sync.WaitGroup
	// orphans 是 engine 重開前就開著、記憶裡沒有的 session（核心記成還開著的）；nil 表示還沒讀過（ECL-31）。
	orphans map[string]bool
	// delivered 是每個 session 最後一次貼上的封包（同一個封包再交來＝核心在重查，ECL-39）。
	delivered map[string]lastDelivery
	// stuck 是連續貼不進去的 session（ECL-67）；cancelTried 是替她按過取消、選單卻還在的 session（ECL-66，不再按）。
	stuck       map[string]*stuckState
	cancelTried map[string]bool
	// autoTrusted 是引擎替她在信任框按過 Yes 的時間（DEV-162）；冷卻內框還在就不再按，改交給主人。
	autoTrusted map[string]time.Time
}

type lastDelivery struct {
	id string
	at time.Time
}

type bootPrompt struct {
	sdk.BootPrompt
	dir string // 信任框問的資料夾（登記當時；認不出來是空的）
	// loose：畫面像信任框、但跟認得的格式對不上（looseTrustScreen）：只讓主人知道，引擎不替任何人按鍵。
	loose bool
}

var _ sdk.EngineAPI = (*Engine)(nil)

func NewEngine(d EngineDeps) *Engine {
	e := &Engine{d: d, sessions: map[string]*session{}, prompts: map[string]*bootPrompt{}, locks: map[string]*sync.Mutex{}, delivered: map[string]lastDelivery{}, stuck: map[string]*stuckState{}, cancelTried: map[string]bool{}, autoTrusted: map[string]time.Time{}}
	e.gen = Generator{Home: d.Home, HookBin: d.HookBin, APISocket: d.APISocket, Now: d.Now}
	return e
}

func (e *Engine) now() time.Time {
	if e.d.Now != nil {
		return e.d.Now()
	}
	return time.Now()
}

func (e *Engine) logf(format string, args ...any) {
	if e.d.Log != nil {
		e.d.Log(fmt.Sprintf(format, args...))
	}
}

func (e *Engine) sleep(d time.Duration) {
	if e.d.Sleeper != nil {
		e.d.Sleeper(d)
		return
	}
	time.Sleep(d)
}

func (e *Engine) environ() []string {
	if e.d.Environ != nil {
		return e.d.Environ()
	}
	return os.Environ()
}

func (e *Engine) claudeBin() string {
	if e.d.ClaudeBin != "" {
		return e.d.ClaudeBin
	}
	return "claude"
}

func (e *Engine) newID() string {
	if e.d.NewID != nil {
		return e.d.NewID()
	}
	return newUUID()
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func unavailable(msg string) error { return &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: msg} }
func notFound(msg string) error    { return &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: msg} }

// Wait 等背景的啟動對話框偵測都結束（關閉與測試用）。
func (e *Engine) Wait() { e.wg.Wait() }

// Close 停掉所有背景偵測（不動 tmux 裡的 session）。
func (e *Engine) Close() {
	e.mu.Lock()
	for _, s := range e.sessions {
		if s.cancel != nil {
			s.cancel()
		}
	}
	e.mu.Unlock()
	e.wg.Wait()
}

func (e *Engine) Capabilities(context.Context, *sdk.EngineCapabilitiesRequest) (*sdk.EngineCapabilitiesResponse, error) {
	return &sdk.EngineCapabilitiesResponse{Engine: "claude", Version: Version, Features: map[string]bool{"ask": true, "resume": true}}, nil
}

func parseEngineConfig(m map[string]any) (engineConfig, error) {
	var c engineConfig
	c.ConfigDir, _ = m["config_dir"].(string)
	c.ConfigDir = expandHome(strings.TrimSpace(c.ConfigDir)) // ~/.claude 要變成真的帳號資料夾，不是 <cwd>/~/.claude（DEV-4）
	switch v := m["default_login"].(type) {
	case bool:
		c.DefaultLogin = v
	case string:
		c.DefaultLogin = v == "true"
	}
	if c.ConfigDir == "" && c.DefaultLogin {
		c.ConfigDir = claudeDefaultDir() // 預設登入沒寫資料夾＝Claude 的預設 ~/.claude
	}
	if c.ConfigDir == "" {
		return c, invalid("engine_config.config_dir", "帳號設定資料夾（config_dir）必填")
	}
	if f, _ := m["env_file"].(string); strings.TrimSpace(f) != "" {
		c.EnvFile = expandHome(strings.TrimSpace(f))
		if !filepath.IsAbs(c.EnvFile) {
			return c, invalid("engine_config.env_file", "環境檔（env_file）要寫絕對路徑或 ~/ 開頭")
		}
	}
	c.TmuxSocket, _ = m["tmux_socket"].(string)
	if c.TmuxSocket == "" {
		c.TmuxSocket = defaultTmuxSocket
	}
	switch v := m["isolate"].(type) {
	case bool:
		c.Isolate = v
	case string:
		c.Isolate = v == "true"
	}
	c.PermissionMode, _ = m["permission_mode"].(string)
	if c.PermissionMode == "" {
		c.PermissionMode = defaultPermissionMode
	} else {
		c.ModeExplicit = true
	}
	dirs, err := parseFullDirs(m)
	if err != nil {
		return c, err
	}
	c.FullDirs = dirs
	return c, nil
}

// defaultFullDirs 是沒寫 engine_config.full_dirs 時，full 等級可以直接改檔的資料夾（作者 2026-10-25 定案）。
var defaultFullDirs = []string{"~/Workspace"}

// parseFullDirs 讀 engine_config.full_dirs（文字陣列；沒寫用 defaultFullDirs，寫空陣列就不給）。
// `~/` 換成家目錄；不存在的資料夾略過，換一台電腦沒有這個資料夾時照樣能啟動。
func parseFullDirs(m map[string]any) ([]string, error) {
	raw, ok := m["full_dirs"]
	var list []string
	switch v := raw.(type) {
	case nil:
		if !ok {
			list = defaultFullDirs
		}
	case []string:
		list = v
	case []any:
		for _, x := range v {
			s, ok := x.(string)
			if !ok {
				return nil, invalid("engine_config.full_dirs", "full_dirs 必須是資料夾路徑的陣列")
			}
			list = append(list, s)
		}
	default:
		return nil, invalid("engine_config.full_dirs", "full_dirs 必須是資料夾路徑的陣列")
	}
	var out []string
	for _, dir := range list {
		if rest, ok := strings.CutPrefix(dir, "~/"); ok {
			home, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			dir = filepath.Join(home, rest)
		}
		if !filepath.IsAbs(dir) {
			return nil, invalid("engine_config.full_dirs", "full_dirs 要寫絕對路徑或 ~/ 開頭")
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, filepath.Clean(dir))
	}
	return out, nil
}

// fullAllowed 是 full 等級預先允許的工具：full_dirs 底下改檔，以及執行 EverGenie 自己的指令（核心另外做 D-24 的判斷）。
// Bash(cd:*)：秘書常寫 `cd <家> && evergenie …`，平台會把複合指令拆開逐段比對，cd 沒列就整句送去自動判斷，
// 每次多 0.5～2 秒（DEV-150 實測）；cd 本身不改任何東西，後面那段照樣要符合規則。
func fullAllowed(dirs []string) string {
	rules := make([]string, 0, len(dirs)+2)
	for _, dir := range dirs {
		rules = append(rules, "Edit(/"+dir+"/**)") // 規則裡 `//` 開頭是絕對路徑
	}
	return strings.Join(append(rules, "Bash(evergenie:*)", "Bash(cd:*)"), ",")
}

// RuleTools 把主人核准過的規則翻成 Claude 的工具規則（ECL-18）：
// read → Read(<資料夾>/**)、write → Edit(<資料夾>/**)、commit → Bash(git commit:*)、push → Bash(git push:*)、delete → Bash(rm:*)。
// 範圍空表示任何地方（Read、Edit 不帶範圍）；`~/` 換成家目錄。指令類的規則沒辦法限定資料夾，範圍只當說明。
// 讀、改的資料夾另外回傳，加進 --add-dir（不存在的略過）。結果去重、照輸入順序。
func RuleTools(rules []sdk.PolicyRule) (tools, dirs []string) {
	seen := map[string]bool{}
	add := func(t string) {
		if !seen[t] {
			seen[t] = true
			tools = append(tools, t)
		}
	}
	for _, r := range rules {
		dir := r.Scope
		if rest, ok := strings.CutPrefix(dir, "~/"); ok || dir == "~" {
			home, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			dir = filepath.Join(home, rest)
		}
		if dir != "" && !filepath.IsAbs(dir) {
			continue // 核心已檢查過；保守起見不認得的範圍不放行
		}
		if dir != "" {
			dir = filepath.Clean(dir)
		}
		pathTool := func(tool string) {
			if dir == "" {
				add(tool)
				return
			}
			add(tool + "(/" + dir + "/**)") // 規則裡 `//` 開頭是絕對路徑
			if info, err := os.Stat(dir); err == nil && info.IsDir() && !slices.Contains(dirs, dir) {
				dirs = append(dirs, dir)
			}
		}
		switch r.Action {
		case sdk.ActionClassRead:
			pathTool("Read")
		case sdk.ActionClassWrite:
			pathTool("Edit")
		case sdk.ActionClassCommit:
			add("Bash(git commit:*)")
		case sdk.ActionClassPush:
			add("Bash(git push:*)")
		case sdk.ActionClassDelete:
			add("Bash(rm:*)")
		}
	}
	return tools, dirs
}

// readOnlyDenied 是唯讀等級禁用的工具（--disallowedTools）；執行指令靠 plan 模式擋。
const readOnlyDenied = "Edit,Write,MultiEdit,NotebookEdit"

// LevelArgs 把權限等級（full／standard／readonly）換成 Claude 的啟動參數（ECL-14）：
// full → auto（分類器判斷），另外在 buildArgv 加 full_dirs 的改檔權限、EverGenie 指令與 cd（fullAllowed）；standard → acceptEdits（改檔自動、其他先問主人）；readonly → plan＋禁用寫檔工具。
// 空字串回 mode 空（用 engine_config 或預設）；不認得的等級回錯誤。
func LevelArgs(level sdk.PermissionLevel) (mode string, extra []string, err error) {
	switch level {
	case "":
		return "", nil, nil
	case sdk.PermissionLevelFull:
		return "auto", nil, nil
	case sdk.PermissionLevelStandard:
		return "acceptEdits", nil, nil
	case sdk.PermissionLevelReadOnly:
		return "plan", []string{"--disallowedTools", readOnlyDenied}, nil
	}
	return "", nil, invalid("permission_level", "權限等級必須是 full、standard 或 readonly")
}

var validEfforts = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}

// addDirsFor 是啟動時 --add-dir 帶的資料夾：附件資料夾、full 等級的 full_dirs、主人核准過的規則資料夾（去重、照順序）。
func addDirsFor(req *sdk.EngineStartRequest, cfg engineConfig, share string) []string {
	addDirs := []string{share}
	if req.PermissionLevel == sdk.PermissionLevelFull {
		addDirs = append(addDirs, cfg.FullDirs...)
	}
	_, ruleDirs := RuleTools(req.Rules)
	for _, d := range ruleDirs {
		if !slices.Contains(addDirs, d) {
			addDirs = append(addDirs, d)
		}
	}
	return addDirs
}

// listedDirs：主人設定裡本來就列的資料夾（DEV-162）＝家、--add-dir 帶的、設定檔的 additionalDirectories。
// 工作資料夾不算（`chat --repo` 可以指到任何資料夾）；它在這些底下才會比對到。
// 都換成解開符號連結後的乾淨絕對路徑（resolveDir）；`~/` 換成家目錄，其他相對路徑、解不開的丟掉。
func listedDirs(home string, addDirs []string, settingsPath string) []string {
	raw := append([]string{home}, addDirs...)
	raw = append(raw, settingsDirs(settingsPath)...)
	var out []string
	for _, d := range raw {
		d = strings.TrimSpace(d)
		if rest, ok := strings.CutPrefix(d, "~/"); ok {
			if h, err := os.UserHomeDir(); err == nil {
				d = filepath.Join(h, rest)
			}
		}
		if d == "" || !filepath.IsAbs(d) {
			continue
		}
		if d = resolveDir(d); d != "" && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// resolveDir 解開符號連結（比對信任框的資料夾時兩邊都用，連結指到外面的不會被當成在列過的資料夾底下）；解不開回空字串。
func resolveDir(d string) string {
	r, err := filepath.EvalSymlinks(d)
	if err != nil {
		return ""
	}
	return filepath.Clean(r)
}

// settingsDirs 讀產生的設定檔裡的 permissions.additionalDirectories（讀不到就沒有）。
func settingsDirs(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f settingsFile
	if json.Unmarshal(data, &f) != nil || f.Permissions == nil {
		return nil
	}
	return f.Permissions.AdditionalDirectories
}

// buildArgv 組出啟動指令（補充契約 S11-2：model 空不帶；effort 只有四個值才帶）。
func (e *Engine) buildArgv(req *sdk.EngineStartRequest, cfg engineConfig, id, settings, share string) []string {
	argv := []string{e.claudeBin()}
	if req.ResumeSessionID != "" {
		argv = append(argv, "--resume", id)
	} else {
		argv = append(argv, "--session-id", id)
	}
	argv = append(argv, "--name", req.Name, "--settings", settings, "--permission-mode", cfg.PermissionMode)
	_, levelExtra, _ := LevelArgs(req.PermissionLevel) // 等級已在 Start 驗證過
	argv = append(argv, levelExtra...)
	addDirs := addDirsFor(req, cfg, share)
	var allowed []string
	if req.PermissionLevel == sdk.PermissionLevelFull {
		allowed = append(allowed, fullAllowed(cfg.FullDirs))
	}
	// 主人核准過的規則（權限變更申請，ECL-18）跟 full 那組合併成同一個 --allowedTools。
	ruleTools, _ := RuleTools(req.Rules)
	allowed = append(allowed, ruleTools...)
	if len(allowed) > 0 {
		argv = append(argv, "--allowedTools", strings.Join(allowed, ","))
	}
	if req.Model != "" {
		argv = append(argv, "--model", req.Model)
	}
	if validEfforts[req.Effort] {
		argv = append(argv, "--effort", req.Effort)
	}
	if req.RemoteAccess {
		name := req.RemoteAccessName
		if name == "" {
			name = req.Name
		}
		argv = append(argv, "--remote-control", name)
	}
	if req.Instructions != "" {
		argv = append(argv, "--append-system-prompt", req.Instructions)
	}
	if cfg.Isolate {
		argv = append(argv, "--setting-sources", "project", "--strict-mcp-config")
	}
	// 官方技能只給秘書：外掛只作用於這個 session，不裝進帳號的 skills/（ECL-36）。
	if e.d.PluginDir != "" {
		argv = append(argv, "--plugin-dir", e.d.PluginDir)
	}
	// --add-dir 吃後面所有的值，放最後。
	return append(append(argv, "--add-dir"), addDirs...)
}

// buildEnv 先把繼承來的 CLAUDE*、ANTHROPIC_* 全拿掉（S0：從 session 底下啟動會繼承子 session 旗標而不存 transcript），
// 再加上這個 session 自己的變數，PATH 前面加 framework/bin。
// 家的佈局變數（EVERGENIE_STATE、EVERGENIE_MEDIA、EVERGENIE_OFFICIAL_PERSONAS，DEV-145）核心開引擎時給，這裡原樣留給 session，秘書跑的工具才找得到路徑。
func buildEnv(environ []string, add map[string]string, frameworkBin string) []string {
	var out []string
	path := ""
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		if strings.HasPrefix(k, "CLAUDE") || strings.HasPrefix(k, "ANTHROPIC_") || sdk.IsEnv(k, "EVERGENIE_ENGINE_SOCKET") {
			continue
		}
		if k == "PATH" {
			path = v
			continue
		}
		if _, overridden := add[sdk.CurrentEnvName(k)]; overridden { // 改名前的舊名也換成這個 session 自己的
			continue
		}
		out = append(out, kv)
	}
	if path != "" {
		path = frameworkBin + string(os.PathListSeparator) + path
	} else {
		path = frameworkBin
	}
	add["PATH"] = path
	keys := make([]string, 0, len(add))
	for k := range add {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+add[k])
	}
	return out
}

func (e *Engine) Start(ctx context.Context, req *sdk.EngineStartRequest) (*sdk.EngineStartResponse, error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, invalid("name", "請指定 session 名稱")
	}
	if strings.TrimSpace(req.Account) == "" {
		return nil, invalid("account", "請指定帳號")
	}
	if strings.TrimSpace(e.d.Home) == "" {
		return nil, invalid("home", "找不到家的位置")
	}
	cfg, err := parseEngineConfig(req.EngineConfig)
	if err != nil {
		return nil, err
	}
	levelMode, _, err := LevelArgs(req.PermissionLevel)
	if err != nil {
		return nil, err
	}
	if levelMode != "" && !cfg.ModeExplicit {
		cfg.PermissionMode = levelMode
	}
	if req.ResumeSessionID != "" && !sessionIDPattern.MatchString(req.ResumeSessionID) {
		return nil, invalid("resume_session_id", "session id 格式不對")
	}
	e.d.Tmux.Remember(req.Name, cfg.TmuxSocket) // 先記 socket 再查：沒記的話查的是預設的 eg，會把別的家同名的秘書當成自己的（GOAL-2）
	if e.d.Tmux.Alive(req.Name) {
		if err := e.replaceLeftover(req.Name, cfg.TmuxSocket); err != nil {
			return nil, err
		}
	}
	launch, err := e.markLaunch(req.Name)
	if err != nil {
		return nil, &sdk.Error{Code: sdk.ErrorCodeInternal, Message: fmt.Sprintf("寫不了啟動記號：%v", err)}
	}
	settings, err := e.gen.SettingsPath(req.Account)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(settings); statErr != nil {
		// 還沒人呼叫過 SetPermission：先用空政策產生，hooks 才會在。
		if _, err := e.gen.SetPermission(&sdk.EngineSetPermissionRequest{Account: req.Account, Policy: &sdk.PermissionPolicy{}}); err != nil {
			return nil, err
		}
	}
	share := sdk.SharesDir(e.d.Home) // 附件資料夾（第 2 版在 <家>/media/share）
	if err := os.MkdirAll(share, 0o755); err != nil {
		return nil, &sdk.Error{Code: sdk.ErrorCodeInternal, Message: fmt.Sprintf("建不了上傳資料夾：%v", err)}
	}
	cwd := req.Cwd
	if cwd == "" {
		cwd = e.d.Home
	}
	id := req.ResumeSessionID
	// 對話檔不在、或裡面只有標題沒有對話時，--resume 一開就退出，核心又會再開，變成無限循環（DEV-133）：
	// 接不回來的對話就開新的，核心從回應拿到新的 id。
	if id != "" && !hasConversation(transcriptPath(cfg.ConfigDir, cwd, id)) {
		fresh := *req
		fresh.ResumeSessionID = ""
		req = &fresh
		id = ""
	}
	if id == "" {
		id = e.newID()
	}
	apiSocket := req.HostEndpoint
	if apiSocket == "" {
		apiSocket = e.d.APISocket
	}
	add := map[string]string{
		"EVERGENIE_SESSION": req.Name,
		"EVERGENIE_AGENT":   req.Agent,
		"EVERGENIE_HOME":    e.d.Home,
		launchVar:           launch,
	}
	if !cfg.usesDefaultLogin() { // 預設登入不設（buildEnv 與啟動指令稿會把外面帶進來的 CLAUDE* 全清掉，ECL-64）
		add["CLAUDE_CONFIG_DIR"] = cfg.ConfigDir
	}
	if apiSocket != "" {
		add["EVERGENIE_API_SOCKET"] = apiSocket
	}
	if cfg.EnvFile != "" {
		add[envFileVar] = cfg.EnvFile // 啟動腳本先載入它，再設這個 session 自己的變數（writeLaunchScript）
	}
	env := buildEnv(e.environ(), add, filepath.Join(e.d.Home, "framework", "bin"))
	argv := e.buildArgv(req, cfg, id, settings, share)
	if err := e.d.Tmux.Start(cfg.TmuxSocket, req.Name, cwd, env, argv); err != nil {
		return nil, unavailable(fmt.Sprintf("開不了 session：%v", err))
	}
	watchCtx, cancel := context.WithCancel(context.Background())
	s := &session{name: req.Name, id: id, cwd: cwd, configDir: cfg.ConfigDir, cancel: cancel,
		listed: listedDirs(e.d.Home, addDirsFor(req, cfg, share), settings)}
	// 先寫紀錄再放進記憶：transcriptSource 以紀錄為準（ECL-47），順序反過來那一瞬間會讀到上一次的編號。
	e.rememberTranscript(s)
	e.mu.Lock()
	if old := e.sessions[req.Name]; old != nil && old.cancel != nil {
		old.cancel()
	}
	e.sessions[req.Name] = s
	delete(e.orphans, req.Name)
	e.clearAutoTrusted(req.Name)
	e.mu.Unlock()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.watchBoot(watchCtx, req.Name)
	}()
	// 帶了 --model 就給名字、帶了 --effort 就說力度（ECL-49）；實際的代號等 SessionStart 掛勾回報。
	// 沒帶的不說（用帳號的預設，開起來才知道）。
	res := &sdk.EngineStartResponse{EngineSessionID: id, Attach: attachCommand(e.d.Tmux.Path(), cfg.TmuxSocket, req.Name), Model: req.Model, ModelLabel: modelLabel(req.Model)}
	if validEfforts[req.Effort] {
		res.Effort = req.Effort
	}
	return res, nil
}

// replaceLeftover：核心要開、同名的視窗卻還在（ECL-68，DEV-159）。核心只在它記成「停了」時才叫 Start，
// 所以還在的那個是核心已經不算數的舊視窗；照舊回「已經在執行」核心就永遠開不起來。
// 收掉舊的再照常開：核心帶著原本的對話編號來，--resume 接回同一段對話。
// 只動「這個家的 socket 裡、名字完全相同、而且是這個家開的」那一個（啟動指令稿在這個家的資料夾，Tmux.LaunchedHere）；
// 認不出是自己開的（別的家用了同一個 socket、舊版開的）就照舊回「已經在執行」，不動它。
func (e *Engine) replaceLeftover(name, socket string) error {
	if !e.d.Tmux.LaunchedHere(name) {
		e.logf("%s：核心要開她，同名的視窗還在、認不出是這個家開的，沒有動它（回衝突）", name)
		return &sdk.Error{Code: sdk.ErrorCodeConflict, Message: "這個 session 已經在執行（同名的視窗認不出是這個家開的，沒有動它）"}
	}
	e.forget(name, nil)
	// 先換記號再砍：舊的那個之後才觸發的 hook（SessionEnd）算舊的，不會把接著開的記成停（同 Stop，DEV-10）。
	if _, err := e.markLaunch(name); err != nil {
		return &sdk.Error{Code: sdk.ErrorCodeInternal, Message: fmt.Sprintf("寫不了啟動記號：%v", err)}
	}
	err := e.d.Tmux.Kill(name)
	e.d.Tmux.Remember(name, socket) // Kill 會忘掉 socket；再記一次，下面才查得到同一個地方
	if err != nil || e.d.Tmux.Alive(name) {
		return unavailable(fmt.Sprintf("這個 session 的舊視窗還在、關不掉（%v）", err))
	}
	e.logf("%s：核心要開她，同名的舊視窗還在；已經收掉舊的、照常重開", name)
	return nil
}

// recall：引擎重開後第一次被叫到時，先從核心的紀錄把每個 session 的 socket 補回來（loadOrphans；ECL-68）。
// 以前只有存活檢查（起來 3 秒後）與列啟動提示會補；這之前進來的 Deliver 查的是預設 socket，socket 不是預設的家
// 就會把還活著的秘書回成「不在了」，核心記成停、視窗還在。
func (e *Engine) recall() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.orphans == nil {
		e.orphans = e.loadOrphans()
	}
}

// attachCommand 是從終端機接上 session 畫面的指令（核心的 attach 指令原樣印出）。
// tmux 用實際會執行的那支的完整路徑（ECL-63：附帶的 tmux 不在 PATH 上，只寫 tmux 主人貼了會找不到）；路徑有空白等字元才加引號。
// 目標用單引號包住：zsh 會把開頭的 = 當成指令路徑展開。
func attachCommand(bin, socket, name string) string {
	return shellWord(bin) + " -L " + socket + " attach -t '" + sessionTarget(name) + "'"
}

var plainShellWord = regexp.MustCompile(`^[A-Za-z0-9_./+@%:-]+$`)

// shellWord 讓一個字在 shell 裡原樣傳過去：只有安全的字元就不加引號（好讀），否則用單引號包住。
func shellWord(s string) string {
	if plainShellWord.MatchString(s) {
		return s
	}
	return shellQuote(s)
}

// attachSocket 從 attachCommand 的字串取回 socket；格式不對回空字串。
// 認最後一個「 -L 」：前面的 tmux 路徑可能是任何字（ECL-63 起是完整路徑，舊紀錄是 tmux），後面的 session 名稱沒有空白。
func attachSocket(attach string) string {
	i := strings.LastIndex(attach, " -L ")
	if i < 0 {
		return ""
	}
	socket, _, _ := strings.Cut(strings.TrimLeft(attach[i+len(" -L "):], " "), " ")
	return socket
}

// launchVar 是每次啟動給 Claude 的記號（DEV-10）。hook 拿它跟 launches/<名稱> 檔裡的比，
// 對不上就是同名的上一次（已經停掉或重開）晚到的事件，丟掉。
const launchVar = "EVERGENIE_LAUNCH"

func launchFilePath(home, session string) string { return sessionFilePath(home, "launches", session) }

// markLaunch 換一個新的啟動記號寫進檔案並回傳。Start 在開 tmux 前、Stop 在砍 tmux 前呼叫。
func (e *Engine) markLaunch(name string) (string, error) {
	token := newUUID()
	data, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	return token, writeFileAtomic(launchFilePath(e.d.Home, name), data, 0o644)
}

// forget 把 session 從記憶裡拿掉（含啟動對話框）。回傳它是不是我們開的。
// only 不是空的時候只有記憶裡還是它才拿掉（存活檢查用：檢查到一半被重開，新的不能被當成掛了）。
func (e *Engine) forget(name string, only *session) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.sessions[name]
	if only != nil && s != only {
		return false
	}
	if s != nil && s.cancel != nil {
		s.cancel()
	}
	delete(e.sessions, name)
	delete(e.stuck, name)
	for id, p := range e.prompts {
		if p.Session == name {
			delete(e.prompts, id)
		}
	}
	e.clearAutoTrusted(name)
	return s != nil
}

func (e *Engine) Stop(ctx context.Context, req *sdk.EngineStopRequest) (*sdk.EngineStopResponse, error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, invalid("name", "請指定 session 名稱")
	}
	e.recall()
	known := e.forget(req.Name, nil)
	// 先換記號再砍：被砍的 Claude 之後才觸發的 hook（SessionEnd）一律算舊的，不會把接著重開的那個記成停（DEV-10）。
	_, _ = e.markLaunch(req.Name)
	if err := e.d.Tmux.Kill(req.Name); err != nil {
		return nil, unavailable(fmt.Sprintf("關不掉 session：%v", err))
	}
	if known {
		// 平台是被砍掉的，不保證 SessionEnd 會觸發；多報一次無害（核心對重複的結束事件是冪等的）。
		e.reportEnded(ctx, req.Name, "stopped")
	}
	return &sdk.EngineStopResponse{}, nil
}

func (e *Engine) reportEnded(ctx context.Context, name, reason string) {
	if e.d.Host == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	_, _ = e.d.Host.ReportEvent(ctx, &sdk.EngineHostReportEventRequest{Event: &sdk.EngineEvent{
		Session: name, Type: sdk.EngineEventTypeSessionEnded, At: e.now(), Reason: reason,
	}})
}

// CheckAlive 對自己開過的 session 做存活檢查，不在了就回報 SESSION_ENDED（reason: gone）並忘掉它。
// engine 重開前就開著的（orphans，ECL-31）也一起看：電腦重開後 tmux 全沒了，核心卻還記得「待命」，
// 不回報的話訊息會貼進不存在的 session、秘書也不會被重新開起來。
func (e *Engine) CheckAlive(ctx context.Context) {
	e.mu.Lock()
	if e.orphans == nil {
		e.orphans = e.loadOrphans()
	}
	names := make([]string, 0, len(e.sessions))
	seen := make(map[string]*session, len(e.sessions))
	for name, s := range e.sessions {
		names = append(names, name)
		seen[name] = s
	}
	orphans := make([]string, 0, len(e.orphans))
	for name := range e.orphans {
		if e.sessions[name] == nil {
			orphans = append(orphans, name)
		}
	}
	e.mu.Unlock()
	sort.Strings(names)
	for _, name := range names {
		if e.d.Tmux.Alive(name) {
			continue
		}
		if e.forget(name, seen[name]) {
			e.reportEnded(ctx, name, "gone")
		}
	}
	sort.Strings(orphans)
	for _, name := range orphans {
		if e.d.Tmux.Alive(name) {
			continue
		}
		// 核心還沒好（serve 剛起來）就留著，下一輪再報。
		if e.reportGone(ctx, name) {
			e.mu.Lock()
			delete(e.orphans, name)
			e.clearAutoTrusted(name)
			e.mu.Unlock()
		}
	}
	e.pruneStuck()
	// 還活著的（自己開的、接手的）順便看一次畫面：開機後才出現的、或重開前登記過的信任框（DEV-87）。
	e.mu.Lock()
	alive := make([]string, 0, len(e.sessions)+len(e.orphans))
	for name := range e.sessions {
		alive = append(alive, name)
	}
	for name := range e.orphans {
		if e.sessions[name] == nil {
			alive = append(alive, name)
		}
	}
	e.mu.Unlock()
	sort.Strings(alive)
	for _, name := range alive {
		if e.d.Tmux.Alive(name) {
			e.scanBoot(name, true)
		}
	}
}

// loadOrphans 讀核心記的 <暫存>/sessions/<名稱>.md，state 不是 stopped 的都算（呼叫時持有 e.mu）。
func (e *Engine) loadOrphans() map[string]bool {
	out := map[string]bool{}
	paths, _ := filepath.Glob(sdk.RuntimeDir(e.d.Home, "sessions", "*.md"))
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".md")
		if !sessionIDPattern.MatchString(name) || e.sessions[name] != nil {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		state, socket := "", ""
		for _, line := range strings.Split(string(data), "\n") {
			if v, ok := strings.CutPrefix(line, "state: "); ok {
				state = strings.Trim(strings.TrimSpace(v), `"`)
			}
			if v, ok := strings.CutPrefix(line, "attach: "); ok {
				socket = attachSocket(strings.Trim(strings.TrimSpace(v), `"`))
			}
		}
		// 引擎重開後不記得 socket：從核心記的 attach 補回（停了的也補，之後關、重開才不會碰到別的家同名的秘書，GOAL-2）。
		e.d.Tmux.Remember(name, socket)
		if state != "" && state != string(sdk.SessionStateStopped) {
			out[name] = true
		}
	}
	return out
}

// reportGone 回報一個 orphan 已經不在；核心收下了（或核心根本不認得這個 session）回 true。
func (e *Engine) reportGone(ctx context.Context, name string) bool {
	if e.d.Host == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	_, err := e.d.Host.ReportEvent(ctx, &sdk.EngineHostReportEventRequest{Event: &sdk.EngineEvent{
		Session: name, Type: sdk.EngineEventTypeSessionEnded, At: e.now(), Reason: "gone",
	}})
	var se *sdk.Error
	return err == nil || (errors.As(err, &se) && se.Code == sdk.ErrorCodeNotFound)
}

func (e *Engine) lockFor(name string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	l := e.locks[name]
	if l == nil {
		l = &sync.Mutex{}
		e.locks[name] = l
	}
	return l
}

// Deliver 貼上文字、1 秒後確認沒有確認框才送 Enter（ECL-4、ECL-N4）。「閒著」由核心判斷。
func (e *Engine) Deliver(ctx context.Context, req *sdk.EngineDeliverRequest) (*sdk.EngineDeliverResponse, error) {
	if req == nil || strings.TrimSpace(req.Session) == "" {
		return nil, invalid("session", "請指定 session 名稱")
	}
	if req.Text == "" {
		return nil, invalid("text", "沒有要送的文字")
	}
	e.recall()
	l := e.lockFor(req.Session)
	l.Lock()
	defer l.Unlock()
	r := e.deliver(ctx, req)
	e.noteDelivery(req.Session, r) // 連續貼不進去要讓主人知道（ECL-67）
	return r, nil
}

func (e *Engine) deliver(ctx context.Context, req *sdk.EngineDeliverRequest) *sdk.EngineDeliverResponse {
	name := req.Session
	if !e.d.Tmux.Alive(name) {
		return &sdk.EngineDeliverResponse{Reason: "gone"}
	}
	if screen, err := e.d.Tmux.Capture(name); err == nil {
		// 確認框已經開著就不貼（貼進去的字可能被當成對確認框的回答）：回 dialog，核心放回排隊、等確認框收掉（ECL-40）。
		if isConfirmDialog(screen) {
			return &sdk.EngineDeliverResponse{Reason: "dialog"}
		}
		// 畫面最底下是清單裡的平台選單（它取代了輸入框）：先替她按取消再貼（ECL-66）。收不掉就不貼。
		if at := findBlocker(screen); at.atBottom() {
			if _, ok := e.dismissBlocker(name, at.b); !ok {
				return &sdk.EngineDeliverResponse{Reason: "paste"}
			}
		}
	}
	e.mu.Lock()
	last, again := e.delivered[name]
	e.mu.Unlock()
	switch {
	case again && req.EnvelopeID != "" && last.id == req.EnvelopeID:
		if r, done := e.recheck(ctx, name, req.Text, last.at, req.Force); done {
			return r
		}
	case !again && req.EnvelopeID != "" && req.Resend && !req.Force:
		// 引擎重開過、不記得貼過什麼（delivered 只在記憶體），而核心說這一則以前貼過（Resend）：可能重開前已經貼進去了。
		// 對話記錄最近 forgottenWindow 內已經有這句就當成重查（免得貼兩次）；核心真的要重貼會帶 Force。
		// 沒帶 Resend 的是第一次送：就算對話裡有一樣開頭的舊話也照常貼。
		since := e.now().Add(-forgottenWindow)
		if e.promptSeen(name, req.Text, since, 0) {
			if r, done := e.recheck(ctx, name, req.Text, since, false); done {
				return r
			}
		}
	}
	var pastedAt time.Time
	var sawInBox, found bool
	for try := 0; ; try++ {
		pastedAt = e.now()
		if err := e.d.Tmux.Paste(name, safeLeading(req.Text)); err != nil {
			return &sdk.EngineDeliverResponse{Reason: "paste"}
		}
		if req.EnvelopeID != "" {
			e.mu.Lock()
			e.delivered[name] = lastDelivery{id: req.EnvelopeID, at: pastedAt}
			e.mu.Unlock()
		}
		e.sleep(pasteEnterDelay)
		screen, err := e.d.Tmux.Capture(name)
		if err != nil {
			if !e.d.Tmux.Alive(name) {
				return &sdk.EngineDeliverResponse{Reason: "gone"}
			}
			// 看不到畫面就不能確定沒有確認框：寧可不送。
			return &sdk.EngineDeliverResponse{Reason: "capture"}
		}
		if isConfirmDialog(screen) {
			return &sdk.EngineDeliverResponse{Reason: "dialog"}
		}
		sawInBox, found = boxHolds(screen, req.Text)
		if found && sawInBox {
			break // 字進了輸入框：畫面上就算看得到選單的字，也只是對話裡引用的
		}
		// 字沒進輸入框、清單裡的選單又開在輸入框的位置（blocking）：是它擋的（ECL-66）。
		// 按取消後再貼一次；按過還在就不再按，也不按 Enter（Enter 在選單上是「選第一項」）。
		at := findBlocker(screen)
		if !at.blocking() {
			break
		}
		after, ok := "", false
		if try == 0 {
			after, ok = e.dismissBlocker(name, at.b)
		}
		if !ok {
			return &sdk.EngineDeliverResponse{Reason: "paste"}
		}
		if sawInBox, found = boxHolds(after, req.Text); found && sawInBox {
			break // 選單收掉後看得到剛才貼的那句已經在輸入框：不用再貼
		}
	}
	if found && !sawInBox {
		// 看得到輸入框、裡面卻沒有這句：這次貼上沒進去（或還沒畫出來）。不按 Enter（主人在終端機打了一半的字會被送出去），
		// 回 paste，核心退避後再交來，重查時還在框裡就補 Enter、不在才重貼（ECL-42）。
		return &sdk.EngineDeliverResponse{Reason: "paste"}
	}
	return e.enterAndConfirm(ctx, name, req.Text, pastedAt, sawInBox)
}

// enterAndConfirm 送 Enter，再等平台收下這句（ECL-42）：看到了才回 delivered，沒看到回 reasonNotSubmitted。
func (e *Engine) enterAndConfirm(ctx context.Context, name, text string, since time.Time, sawInBox bool) *sdk.EngineDeliverResponse {
	if err := e.d.Tmux.SendKey(name, "Enter"); err != nil {
		return &sdk.EngineDeliverResponse{Reason: "enter"}
	}
	if !e.confirmSubmit(ctx, name, text, since, sawInBox) {
		return &sdk.EngineDeliverResponse{Reason: reasonNotSubmitted}
	}
	return &sdk.EngineDeliverResponse{Delivered: true}
}

// recheck：核心把同一個封包再交來一次（上一次沒確認送出，或核心自己要再確認，ECL-39、ECL-42）。先看上一次的到了沒，免得重貼：
//   - 上一次貼上之後平台已經收下這句（UserPromptSubmit 或對話記錄）：回 delivered＋already，不再貼；
//     例外：那一輪撞到額度、平台沒說會自己接著做（limitStranded，DEV-102）——恢復後她不會動，照常重貼；
//   - 還沒收下，但還留在輸入框（找不到輸入框時看整個畫面；長文字被收成「[Pasted text …]」也算）：只補按 Enter，再確認一次；
//   - 輸入框是空的，但輸入框外面（對話區）看得到這句：多半已經送出、平台的回報還沒到——不貼不按，回 reasonNotSubmitted，
//     讓核心退避後再查（重貼會變成兩則，最壞只是記成沒送到，看得見）；
//   - 看不到輸入框、清單裡的平台選單開著（ECL-66）：回 done=false，不補 Enter，由貼上那條路先取消選單；
//   - 都沒有：回 done=false，照常重貼。
//
// force（請求帶 Force，DEV-162）：核心等過那句、她一直沒有接著做，這次一定要重貼——不回 already，對話區看得到這句也照貼；
// 確認框、還留在輸入框只補 Enter 照舊。
func (e *Engine) recheck(ctx context.Context, name, text string, since time.Time, force bool) (*sdk.EngineDeliverResponse, bool) {
	if e.promptSeen(name, text, since, transcriptSlack) {
		if force || e.limitStranded(name, text) {
			return nil, false // 不能走下面「對話區看得到這句就晚點再查」：那句一直會在對話區
		}
		return &sdk.EngineDeliverResponse{Delivered: true, Reason: sdk.DeliverAlready}, true
	}
	screen, err := e.d.Tmux.Capture(name)
	if err != nil {
		return nil, false
	}
	if isConfirmDialog(screen) {
		return &sdk.EngineDeliverResponse{Reason: "dialog"}, true
	}
	holds, found := boxHolds(screen, text)
	if !found && findBlocker(screen).blocking() {
		// 看不到輸入框、清單裡的選單開著：畫面上就算看得到這句也不補 Enter（會選到選單的第一項），走貼上那條路處理（ECL-66）
		return nil, false
	}
	if !found {
		holds = holdsText(screen, text)
	}
	if !holds {
		if !force && found && holdsText(outsideBox(screen), text) {
			return &sdk.EngineDeliverResponse{Reason: reasonNotSubmitted}, true
		}
		return nil, false
	}
	return e.enterAndConfirm(ctx, name, text, since, found), true
}

// probeText 是拿來比對的一小段：第一個非空白行的前 n 個字（長的會在畫面上折行，不比整段）。
func probeText(text string, n int) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			r := []rune(line)
			if len(r) > n {
				r = r[:n]
			}
			return string(r)
		}
	}
	return strings.TrimSpace(text)
}

// safeLeading：Claude 的輸入框把開頭的 `!` 當成「跑終端機指令」（只傳照片時，看板送的是 `![檔名](/share/…)`）、
// 開頭的 `/` 當成斜線指令（訊息要當純文字送；指令走 Command，ECL-46），開頭補一個空白讓它照一般訊息收（ECL-32）。
func safeLeading(text string) string {
	if strings.HasPrefix(text, "!") || strings.HasPrefix(text, "/") {
		return " " + text
	}
	return text
}

func (e *Engine) SetPermission(_ context.Context, req *sdk.EngineSetPermissionRequest) (*sdk.EngineSetPermissionResponse, error) {
	return e.gen.SetPermission(req)
}

// watchBoot：Start 後 20 秒內每秒看一次畫面，只認信任資料夾對話框（ECL-7）。
func (e *Engine) watchBoot(ctx context.Context, name string) {
	deadline := e.now().Add(bootWatchDuration)
	for ctx.Err() == nil {
		if screen, err := e.d.Tmux.Capture(name); err == nil && isTrustScreen(screen) {
			// 框畫到一半（還認不出問哪個資料夾）先不登記，下一輪再看；到期限還認不出才交給主人。
			if trustDir(screen) != "" || !e.now().Before(deadline) {
				// 設定裡列的資料夾替她按掉之後繼續看：接著可能還有下一個框（工作資料夾之後是 --add-dir 的）。
				if e.handleTrust(ctx, name, screen, true) {
					return
				}
			}
		}
		if !e.now().Before(deadline) {
			// 到期限畫面還像信任框、格式卻對不上：只登記給主人看，不按（ECL-7 的備援）。
			if screen, err := e.d.Tmux.Capture(name); err == nil && looseTrustScreen(screen) {
				e.mu.Lock()
				if ctx.Err() == nil {
					e.registerLooseTrust(name)
				}
				e.mu.Unlock()
			}
			return
		}
		e.sleep(bootPollInterval)
	}
}

// autoTrustSettle：替她按了 Yes 之後，這段時間內同一個框還在畫面上先不動（畫面還沒換）；過了還在就交給主人，不再按。
const autoTrustSettle = 5 * time.Second

// handleTrust：畫面是信任框時決定怎麼辦（DEV-162，作者 2026-11-17 定）。問的資料夾（解開符號連結後）是主人設定裡本來就列的
// （session.listed；接手的既有 session 不知道啟動參數，只認家的資料夾），而且 auto（watchBoot、CheckAlive），引擎替主人按 Yes；
// 沒列過的不按，登記成啟動對話框，看板明講卡在信任資料夾。回 true＝已登記給主人（或 session 已經不算數），watchBoot 可以停了。
func (e *Engine) handleTrust(ctx context.Context, name, screen string, auto bool) bool {
	dir := trustDir(screen)
	key := name + "\x00" + dir
	e.mu.Lock()
	s := e.sessions[name]
	if (s == nil && !e.orphans[name]) || ctx.Err() != nil {
		e.mu.Unlock()
		return true
	}
	listed := []string{resolveDir(e.d.Home)}
	if s != nil {
		listed = s.listed
	}
	at, tried := e.autoTrusted[key]
	switch {
	case auto && !tried && dir != "" && dirListed(resolveDir(dir), listed):
		e.autoTrusted[key] = e.now()
		delete(e.prompts, name+"#trust")
		e.mu.Unlock()
		e.autoTrust(name, dir, screen)
		return false
	case tried && e.now().Sub(at) < autoTrustSettle:
		e.mu.Unlock()
		return false
	}
	e.registerTrustPrompt(name, dir, tried)
	e.mu.Unlock()
	return true
}

// clearAutoTrusted 忘掉替這個 session 按過的紀錄（呼叫時持有 e.mu）：session 收掉、重開時用，新的框重新判斷。
func (e *Engine) clearAutoTrusted(name string) {
	for key := range e.autoTrusted {
		if strings.HasPrefix(key, name+"\x00") {
			delete(e.autoTrusted, key)
		}
	}
}

// autoTrust 照游標位置按到 Yes（同 AnswerBootPrompt）。
func (e *Engine) autoTrust(name, dir, screen string) {
	keys := []string{"Down", "Enter"}
	if onYes, _ := trustCursor(screen); onYes {
		keys = []string{"Enter"}
	}
	for _, k := range keys {
		if err := e.d.Tmux.SendKey(name, k); err != nil {
			e.logf("%s：信任框問 %s（主人設定裡有），替她按 Yes 時按鍵送不出去：%v", name, dir, err)
			return
		}
	}
	e.logf("%s：信任框問 %s，是主人設定裡列的資料夾，替她按了 Yes", name, dir)
}

// registerTrustPrompt 登記一個 session 的信任資料夾對話框（呼叫時持有 e.mu）。
// dir 是框裡問的資料夾（認不出來是空的），記在提示上，主人回答時畫面上問的要還是它；autoFailed＝設定裡有、替她按過了框卻還在。
func (e *Engine) registerTrustPrompt(name, dir string, autoFailed bool) {
	id := name + "#trust"
	title := "卡在信任資料夾：要讓她用這個資料夾嗎？"
	switch {
	case dir != "" && autoFailed:
		title = "卡在信任資料夾：" + dir + "（替她按過 Yes 框還在，請你決定）"
	case dir != "":
		title = "卡在信任資料夾：" + dir + " 不在你的設定裡，要讓她用嗎？"
	}
	e.prompts[id] = &bootPrompt{BootPrompt: sdk.BootPrompt{
		ID: id, Session: name, Title: title,
		Choices: []string{bootChoiceTrust, bootChoiceExit},
	}, dir: dir}
}

// registerLooseTrust（呼叫時持有 e.mu）：畫面像信任框、但格式對不上認得的樣本（平台改版）。不知道游標在哪、問的是哪個資料夾，
// 所以絕不按鍵（自動的、主人在看板按的都不按）；只登記，讓看板明講卡在信任資料夾、請主人到那台 Mac 看畫面。
func (e *Engine) registerLooseTrust(name string) {
	if s := e.sessions[name]; s == nil && !e.orphans[name] {
		return
	}
	id := name + "#trust"
	e.prompts[id] = &bootPrompt{BootPrompt: sdk.BootPrompt{
		ID: id, Session: name, Title: "卡在信任資料夾：畫面跟認得的不一樣，請到那台 Mac 看她的畫面、在那裡按",
		Choices: []string{bootChoiceTrust, bootChoiceExit},
	}, loose: true}
}

// scanBoot 現場看一次 session 的畫面（DEV-87）：信任框還開著就處理（auto＝可以替主人按設定裡列的；否則只登記），
// 已經不在了（主人在終端機按掉了）就撤掉。
// 登記只存在記憶體，引擎或 serve 重開、接手既有 session 之後記憶是空的，不看畫面就永遠不知道它還卡在那裡。
func (e *Engine) scanBoot(name string, auto bool) {
	screen, err := e.d.Tmux.Capture(name)
	if err != nil {
		return
	}
	if isTrustScreen(screen) {
		e.handleTrust(context.Background(), name, screen, auto)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if looseTrustScreen(screen) {
		e.registerLooseTrust(name)
		return
	}
	delete(e.prompts, name+"#trust")
}

// adopted 是引擎重開前就開著、這一輪沒有自己開過的 session 名稱（orphans，ECL-31），已排序。
func (e *Engine) adopted() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.orphans == nil {
		e.orphans = e.loadOrphans()
	}
	var out []string
	for name := range e.orphans {
		if e.sessions[name] == nil {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// ListBootPrompts：記憶裡的對話框，加上接手的既有 session 現場看一次畫面（DEV-87）；
// 自己開的 session 由 watchBoot 與 CheckAlive 看，這裡不重複抓畫面。
func (e *Engine) ListBootPrompts(_ context.Context, req *sdk.EngineListBootPromptsRequest) (*sdk.EngineListBootPromptsResponse, error) {
	e.pruneStuck()
	for _, name := range e.adopted() {
		if (req == nil || req.Session == "" || req.Session == name) && e.d.Tmux.Alive(name) {
			e.scanBoot(name, false) // 列出只登記、撤掉，不按鍵；替主人按留給 watchBoot 與 CheckAlive
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	resp := &sdk.EngineListBootPromptsResponse{}
	for _, p := range e.prompts {
		if req != nil && req.Session != "" && p.Session != req.Session {
			continue
		}
		resp.Prompts = append(resp.Prompts, p.BootPrompt)
	}
	sort.Slice(resp.Prompts, func(i, j int) bool { return resp.Prompts[i].ID < resp.Prompts[j].ID })
	return resp, nil
}

// AnswerApproval：核心逾時沒接手的確認還開在畫面上，主人從看板回答時替他按（ECL-37、ENG-14）。
// 只做樣本有的權限確認框：看畫面上的選項，允許按「Yes」、拒絕按「No」那一項的數字（spike 2：按數字就選定）；
// 0.3 秒後還是同一個確認框、游標停在那一項，才補一個 Enter。畫面上沒有確認框回 none；問題（選擇題）還不會代答回 unsupported。
func (e *Engine) AnswerApproval(_ context.Context, req *sdk.EngineAnswerApprovalRequest) (*sdk.EngineAnswerApprovalResponse, error) {
	if req == nil || strings.TrimSpace(req.Session) == "" {
		return nil, invalid("session", "請指定 session 名稱")
	}
	if req.Pending == nil {
		return nil, invalid("pending", "請提供在等的確認")
	}
	if req.Decision == nil {
		return nil, invalid("decision", "請提供決定")
	}
	name := req.Session
	e.recall()
	l := e.lockFor(name)
	l.Lock()
	defer l.Unlock()
	if !e.d.Tmux.Alive(name) {
		return &sdk.EngineAnswerApprovalResponse{Reason: "gone"}, nil
	}
	if req.Pending.Kind == sdk.PendingKindQuestion {
		return e.answerQuestion(name, req.Pending, req.Decision), nil
	}
	if req.Pending.Kind != sdk.PendingKindPermission {
		return &sdk.EngineAnswerApprovalResponse{Reason: sdk.AnswerUnsupported}, nil
	}
	screen, err := e.d.Tmux.Capture(name)
	if err != nil {
		return &sdk.EngineAnswerApprovalResponse{Reason: "capture"}, nil
	}
	d, ok := parseConfirmDialog(screen)
	if !ok {
		return &sdk.EngineAnswerApprovalResponse{Reason: sdk.AnswerNone}, nil
	}
	if !summaryOnScreen(d.body, req.Pending.Summary) {
		// 畫面上的確認框不是主人在看板看到的那一題：不替他按（ECL-37）。
		return &sdk.EngineAnswerApprovalResponse{Reason: sdk.AnswerUnsupported}, nil
	}
	target := d.choice(req.Decision.Allow)
	if target == 0 {
		return &sdk.EngineAnswerApprovalResponse{Reason: sdk.AnswerUnsupported}, nil
	}
	if err := e.d.Tmux.SendKey(name, strconv.Itoa(target)); err != nil {
		return &sdk.EngineAnswerApprovalResponse{Reason: "keys"}, nil
	}
	e.sleep(answerSettle)
	if after, err := e.d.Tmux.Capture(name); err == nil {
		if again, ok := parseConfirmDialog(after); ok && again.body == d.body && again.cursor == target {
			if err := e.d.Tmux.SendKey(name, "Enter"); err != nil {
				return &sdk.EngineAnswerApprovalResponse{Reason: "keys"}, nil
			}
		}
	}
	return &sdk.EngineAnswerApprovalResponse{Answered: true}, nil
}

// answerQuestion：選擇題被交回她的畫面後，主人從看板回答時替他按（ECL-58；作者 2026-11-03「這個卡住我真的快瘋了」）。
// 只代答一題、單選（真機實測：按選項的數字就直接送出）：答案是畫面上某個選項就按那個數字；不是的話（主人自己打字）
// 按「Type something.」的數字、貼上文字、看到字進了那一行才按 Enter。畫面上的題目要對得上看板那一題，不然不按。
// 多題或複選回 unsupported（看板照舊列出題目，請主人到她的對話裡答）。呼叫時已拿著這個 session 的鎖。
func (e *Engine) answerQuestion(name string, p *sdk.Pending, dec *sdk.Decision) *sdk.EngineAnswerApprovalResponse {
	unsupported := &sdk.EngineAnswerApprovalResponse{Reason: sdk.AnswerUnsupported}
	if len(p.Questions) != 1 || p.Questions[0].MultiSelect {
		return unsupported
	}
	q := p.Questions[0]
	answer := strings.TrimSpace(dec.Answers[q.Text])
	if answer == "" {
		return unsupported
	}
	screen, err := e.d.Tmux.Capture(name)
	if err != nil {
		return &sdk.EngineAnswerApprovalResponse{Reason: "capture"}
	}
	d, ok := parseQuestionDialog(screen)
	if !ok {
		if isConfirmDialog(screen) {
			return unsupported // 畫面上是別的確認框，不是這一題
		}
		return &sdk.EngineAnswerApprovalResponse{Reason: sdk.AnswerNone}
	}
	if d.tabs || !questionOnScreen(d.body, q.Text) {
		return unsupported
	}
	// 游標停在不是選項的那一行（有人在她畫面上點了「Type something.」正在打字、或停在「Chat about this」）：
	// 這時按數字會變成打進文字欄，不代按。
	if d.cursor > 0 && !isOption(q, d.labelOf(d.cursor)) {
		return unsupported
	}
	if n := d.pick(answer); n > 0 {
		if err := e.d.Tmux.SendKey(name, strconv.Itoa(n)); err != nil {
			return &sdk.EngineAnswerApprovalResponse{Reason: "keys"}
		}
		e.sleep(answerSettle)
		if again, still := e.questionStill(name, q, d); still && again.cursor == n {
			if err := e.d.Tmux.SendKey(name, "Enter"); err != nil {
				return &sdk.EngineAnswerApprovalResponse{Reason: "keys"}
			}
		}
		return e.answeredWhenGone(name, q, d)
	}
	if d.typeN == 0 {
		return unsupported
	}
	if err := e.d.Tmux.SendKey(name, strconv.Itoa(d.typeN)); err != nil {
		return &sdk.EngineAnswerApprovalResponse{Reason: "keys"}
	}
	e.sleep(answerSettle)
	if err := e.d.Tmux.Paste(name, strings.ReplaceAll(answer, "\n", " ")); err != nil {
		return &sdk.EngineAnswerApprovalResponse{Reason: "keys"}
	}
	e.sleep(answerSettle)
	after, err := e.d.Tmux.Capture(name)
	if err != nil {
		return &sdk.EngineAnswerApprovalResponse{Reason: "capture"}
	}
	typed, ok := parseQuestionDialog(after)
	if !ok || !typedInto(typed, d.typeN, answer) {
		return &sdk.EngineAnswerApprovalResponse{Reason: "typed"} // 字沒進那一行：不按 Enter，免得送出空的或別的
	}
	if err := e.d.Tmux.SendKey(name, "Enter"); err != nil {
		return &sdk.EngineAnswerApprovalResponse{Reason: "keys"}
	}
	return e.answeredWhenGone(name, q, d)
}

// questionStill：畫面上還是同一題（選項一樣、題目對得上）。抓不到畫面當成還在（不能說答好了）。
func (e *Engine) questionStill(name string, q sdk.Question, d questionDialog) (questionDialog, bool) {
	after, err := e.d.Tmux.Capture(name)
	if err != nil {
		return d, true
	}
	again, ok := parseQuestionDialog(after)
	if !ok || !sameQuestion(again, d) || !questionOnScreen(again.body, q.Text) {
		return questionDialog{}, false
	}
	return again, true
}

// answeredWhenGone：按完之後確定那一題從畫面上收掉了才回答好了（最多看 answerChecks 次）；還開著回 still，
// 核心就不收看板的卡（主人可以再按一次或到她的對話裡答），不會說答好了其實沒答。
func (e *Engine) answeredWhenGone(name string, q sdk.Question, d questionDialog) *sdk.EngineAnswerApprovalResponse {
	for i := 0; i < answerChecks; i++ {
		if _, still := e.questionStill(name, q, d); !still {
			return &sdk.EngineAnswerApprovalResponse{Answered: true}
		}
		e.sleep(answerSettle)
	}
	return &sdk.EngineAnswerApprovalResponse{Reason: "still"}
}

// isOption：label 是看板那一題的選項之一。
func isOption(q sdk.Question, label string) bool {
	for _, o := range q.Options {
		if o.Label == label {
			return true
		}
	}
	return false
}

// typedInto：「Type something.」那一行（編號 n）現在寫的是主人打的字（畫面可能截掉後面，比開頭）；還是「Type something.」不算。
func typedInto(d questionDialog, n int, answer string) bool {
	head := []rune(strings.ReplaceAll(answer, "\n", " "))
	if len(head) > 8 {
		head = head[:8]
	}
	label := d.labelOf(n)
	return label != "" && label != questionType && strings.HasPrefix(label, string(head))
}

func (e *Engine) AnswerBootPrompt(_ context.Context, req *sdk.EngineAnswerBootPromptRequest) (*sdk.EngineAnswerBootPromptResponse, error) {
	if req == nil || req.ID == "" {
		return nil, invalid("id", "請指定對話框")
	}
	e.mu.Lock()
	p := e.prompts[req.ID]
	e.mu.Unlock()
	if p == nil {
		return nil, notFound("沒有這個啟動對話框")
	}
	if strings.HasSuffix(req.ID, stuckSuffix) {
		// 貼不進她的對話（ECL-67）：主人選了按 Esc。
		e.pruneStuck()
		if err := e.answerStuck(p, req.Choice); err != nil {
			return nil, err
		}
		return &sdk.EngineAnswerBootPromptResponse{}, nil
	}
	if req.Choice != bootChoiceTrust && req.Choice != bootChoiceExit {
		return nil, invalid("choice", "不認得這個選項")
	}
	if p.loose {
		return nil, unavailable("這個框跟認得的格式不一樣，不知道游標停在哪，沒辦法替你按；請到那台 Mac 看她的畫面，在那裡按")
	}
	// 按鍵前現場再看一次：框已經被答掉時，Down／Enter 會打進工作中的秘書。
	screen, err := e.d.Tmux.Capture(p.Session)
	onYes, isTrust := trustCursor(screen)
	// 框已經換成問別的資料夾（主人在終端機答掉了上一個、或一個接一個跳）：主人沒看過這個，不按，下次列出時重新登記。
	if err != nil || !isTrust || (p.dir != "" && trustDir(screen) != p.dir) {
		e.mu.Lock()
		delete(e.prompts, req.ID)
		e.mu.Unlock()
		return nil, notFound("這個啟動對話框已經不在了")
	}
	// 照游標現在停的位置按：預設停在 No, exit；主人在終端機按過 ↓ 就停在 Yes，這時再按 ↓ 會選錯。
	var keys []string
	switch {
	case req.Choice == bootChoiceTrust && !onYes:
		keys = []string{"Down", "Enter"}
	case req.Choice == bootChoiceExit && onYes:
		keys = []string{"Up", "Enter"}
	default:
		keys = []string{"Enter"}
	}
	for _, k := range keys {
		if err := e.d.Tmux.SendKey(p.Session, k); err != nil {
			return nil, unavailable(fmt.Sprintf("按鍵送不出去：%v", err))
		}
	}
	e.mu.Lock()
	delete(e.prompts, req.ID)
	e.mu.Unlock()
	return &sdk.EngineAnswerBootPromptResponse{}, nil
}
