// Package claude 是 engine-claude 套件的純邏輯：產生設定、hook 入口、輸入正規化、動作分類、對話記錄解析。
package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

const generatedMarker = "EverGenie — 自動產生，不要手改"

// hook 逾時（秒）：寫進產生的設定，由平台執行。
const (
	statusHookTimeout = 5
	// 平台等 PermissionRequest hook 多久：比 hook 自己等選擇題的 12 小時（questionTimeout）多 10 秒，hook 先收手（ECL-56）。
	// 權限確認 hook 自己 600 秒就收手，不受這個影響。
	approvalHookTimeout = 12*60*60 + 10
)

// hookEvents 是會呼叫狀態回報的 10 種事件（ECL-2；StopFailure 認額度用完與其他錯誤，ECL-38、ECL-41；PreCompact 報整理對話，ECL-43）。
var hookEvents = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"Stop", "StopFailure", "SessionEnd", "Notification", "PermissionRequest", "PreCompact",
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type hookGroup struct {
	Hooks []hookCommand `json:"hooks"`
}

type permissionsBlock struct {
	AdditionalDirectories []string `json:"additionalDirectories"`
}

// autoModeBlock 是 auto 模式分類器的額外說明：主人核准過的 guidance（權限變更申請，ECL-18）。
type autoModeBlock struct {
	Allow []string `json:"allow"`
}

type settingsFile struct {
	Generated   string                 `json:"_generated"`
	Hooks       map[string][]hookGroup `json:"hooks"`
	Permissions *permissionsBlock      `json:"permissions,omitempty"`
	AutoMode    *autoModeBlock         `json:"autoMode,omitempty"`
	// SkillOverrides：關掉平台內建、會自己跳出來擋住輸入框的技能（ECL-69）。
	SkillOverrides map[string]string `json:"skillOverrides"`
}

// skillsOff 是產生的設定一律關掉的平台內建技能（ECL-69，DEV-159）。
// auto-mode-setup：平台 2.1.323 起會在對話中途跳出「Teach auto mode about your environment?」提議跑它，蓋住輸入框、
// 核心的話貼不進去（2026-11-14 事件）。官方文件 auto-mode-config「Turn off /auto-mode-setup」：設成 off 就不再提議，
// /auto-mode-setup 指令也一起關（秘書用不到）。
var skillsOff = []string{"auto-mode-setup"}

func invalid(field, message string) error {
	return &sdk.Error{Code: sdk.ErrorCodeInvalid, Field: field, Message: message}
}

// shellQuote 把字串包成單引號，給 hook 的指令列用。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func hookCommandLine(hookBin, apiSocket, event string) string {
	line := shellQuote(hookBin) + " hook " + event
	if apiSocket != "" {
		line = "EVERGENIE_API_SOCKET=" + shellQuote(apiSocket) + " " + line
	}
	return line
}

// GenerateSettings 產生原生設定（JSON）。只寫 hooks、_generated 與固定的 skillOverrides（skillsOff，ECL-69）；Repos 的路徑寫成 additionalDirectories（只加不限制）；
// Guidance（主人核准過的一句話）寫成 autoMode.allow（ECL-18）。
// Allow／Deny 不轉成平台的權限規則（P5 跟舊家的信任清單遷移一起做），Mode 也不寫。不產生 MCP 設定。
func GenerateSettings(policy sdk.PermissionPolicy, hookBin, apiSocket string) ([]byte, error) {
	if strings.TrimSpace(hookBin) == "" {
		return nil, invalid("hook_bin", "找不到 hook 的執行檔路徑")
	}
	hooks := map[string][]hookGroup{}
	for _, event := range hookEvents {
		timeout := statusHookTimeout
		if event == "PermissionRequest" {
			timeout = approvalHookTimeout
		}
		group := hookGroup{Hooks: []hookCommand{{Type: "command", Command: hookCommandLine(hookBin, apiSocket, event), Timeout: timeout}}}
		switch event {
		case "UserPromptSubmit":
			group.Hooks = append(group.Hooks, hookCommand{Type: "command", Command: hookCommandLine(hookBin, apiSocket, "context"), Timeout: statusHookTimeout})
		case "PreToolUse":
			group.Hooks = append(group.Hooks, hookCommand{Type: "command", Command: hookCommandLine(hookBin, apiSocket, "check"), Timeout: statusHookTimeout})
		}
		hooks[event] = []hookGroup{group}
	}
	file := settingsFile{Generated: generatedMarker, Hooks: hooks, SkillOverrides: map[string]string{}}
	for _, name := range skillsOff {
		file.SkillOverrides[name] = "off"
	}
	seen := map[string]bool{}
	var dirs []string
	for _, repo := range policy.Repos {
		p := strings.TrimSpace(repo.Path)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		dirs = append(dirs, p)
	}
	if len(dirs) > 0 {
		file.Permissions = &permissionsBlock{AdditionalDirectories: dirs}
	}
	var guidance []string
	seenText := map[string]bool{}
	for _, g := range policy.Guidance {
		g = strings.TrimSpace(g)
		if g == "" || seenText[g] {
			continue
		}
		seenText[g] = true
		guidance = append(guidance, g)
	}
	if len(guidance) > 0 {
		file.AutoMode = &autoModeBlock{Allow: guidance}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(file); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Generator 把設定寫到暫存資料夾的 engine-claude/<帳號>/settings.json（第 1 版 <家>/runtime/，第 2 版在狀態資料夾）。
type Generator struct {
	Home      string           // EVERGENIE_HOME
	HookBin   string           // 本套件執行檔的絕對路徑
	APISocket string           // 核心 API socket
	Now       func() time.Time // 空＝time.Now
}

func (g Generator) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// Dir 回傳某帳號產生設定的資料夾。
func (g Generator) Dir(account string) (string, error) {
	if strings.TrimSpace(account) == "" {
		return "", invalid("account", "請指定要產生設定的帳號")
	}
	if strings.ContainsAny(account, `/\`) || account == "." || account == ".." {
		return "", invalid("account", "帳號名稱不能含路徑符號")
	}
	if strings.TrimSpace(g.Home) == "" {
		return "", invalid("home", "找不到家的位置")
	}
	return sdk.RuntimeDir(g.Home, "engine-claude", account), nil
}

// SettingsPath 回傳某帳號的設定檔路徑。
func (g Generator) SettingsPath(account string) (string, error) {
	dir, err := g.Dir(account)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// SetPermission 依政策為一個帳號產生設定檔。Account 空 → Invalid("account")。
func (g Generator) SetPermission(req *sdk.EngineSetPermissionRequest) (*sdk.EngineSetPermissionResponse, error) {
	if req == nil {
		return nil, invalid("request", "缺少請求內容")
	}
	path, err := g.SettingsPath(req.Account)
	if err != nil {
		return nil, err
	}
	var policy sdk.PermissionPolicy
	if req.Policy != nil {
		policy = *req.Policy
	}
	if req.DryRun {
		// DryRun 一個檔都不寫（連資料夾都不建）：用 Doctor 比對現有產物跟政策，把問題放進 Drift（ECL-6、doctor）。
		dir, err := g.Dir(req.Account)
		if err != nil {
			return nil, err
		}
		report, err := Doctor(dir, policy, g.HookBin, g.APISocket)
		if err != nil {
			return nil, err
		}
		return &sdk.EngineSetPermissionResponse{GeneratedAt: g.now(), Drift: report.Problems}, nil
	}
	data, err := GenerateSettings(policy, g.HookBin, g.APISocket)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return nil, &sdk.Error{Code: sdk.ErrorCodeInternal, Message: fmt.Sprintf("寫不進設定檔：%v", err)}
	}
	return &sdk.EngineSetPermissionResponse{Artifacts: []string{path}, GeneratedAt: g.now()}, nil
}

// DoctorReport 是 Doctor 的結果。
type DoctorReport struct {
	OK       bool
	Problems []string
}

// Doctor 比對 dir 裡的 settings.json 與用同一份政策重新產生的結果；不一致就列出問題。
func Doctor(dir string, policy sdk.PermissionPolicy, hookBin, apiSocket string) (DoctorReport, error) {
	want, err := GenerateSettings(policy, hookBin, apiSocket)
	if err != nil {
		return DoctorReport{}, err
	}
	path := filepath.Join(dir, "settings.json")
	got, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DoctorReport{Problems: []string{"設定檔不存在：" + path}}, nil
	}
	if err != nil {
		return DoctorReport{}, err
	}
	if bytes.Equal(got, want) {
		return DoctorReport{OK: true}, nil
	}
	var problems []string
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		problems = append(problems, "設定檔不是合法的 JSON")
	} else {
		if parsed["_generated"] != generatedMarker {
			problems = append(problems, "設定檔缺少自動產生的標記")
		}
		problems = append(problems, grantDrift(got, policy)...)
	}
	problems = append(problems, "設定檔內容與目前的政策不一致（可能被手改）："+path)
	return DoctorReport{Problems: problems}, nil
}

// grantDrift 逐條比對放行的東西（S5 稽核，REQ-393）：autoMode.allow 每一句都要對得上一張套用中的申請（policy.Guidance），
// 少了的也列出來；permissions.allow／deny 產生器從不寫，有就是手加的。
func grantDrift(got []byte, policy sdk.PermissionPolicy) []string {
	var file struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
		AutoMode struct {
			Allow []string `json:"allow"`
		} `json:"autoMode"`
	}
	if json.Unmarshal(got, &file) != nil {
		return nil
	}
	want := map[string]bool{}
	for _, g := range policy.Guidance {
		if g = strings.TrimSpace(g); g != "" {
			want[g] = true
		}
	}
	var out []string
	have := map[string]bool{}
	for _, g := range file.AutoMode.Allow {
		g = strings.TrimSpace(g)
		have[g] = true
		if g != "" && !want[g] {
			out = append(out, "多了一句沒有對應申請的放行說明：「"+g+"」")
		}
	}
	for _, g := range policy.Guidance {
		if g = strings.TrimSpace(g); g != "" && !have[g] {
			have[g] = true
			out = append(out, "少了一句已核准的放行說明：「"+g+"」")
		}
	}
	for _, r := range file.Permissions.Allow {
		out = append(out, "多了一條沒有對應申請的放行規則："+r)
	}
	for _, r := range file.Permissions.Deny {
		out = append(out, "多了一條不是 EverGenie 產生的禁止規則："+r)
	}
	return out
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
