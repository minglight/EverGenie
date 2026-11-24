package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

type genSettings struct {
	Generated string `json:"_generated"`
	Hooks     map[string][]struct {
		Hooks []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		} `json:"hooks"`
	} `json:"hooks"`
	Permissions map[string]any `json:"permissions"`
}

func generate(t *testing.T, policy sdk.PermissionPolicy) (genSettings, []byte) {
	t.Helper()
	data, err := GenerateSettings(policy, "/opt/hg/evergenie-engine-claude", "/tmp/hg-x/api.sock")
	if err != nil {
		t.Fatal(err)
	}
	var s genSettings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("產生的不是 JSON：%v", err)
	}
	return s, data
}

// Guards: REQ-77, REQ-378
func TestGeneratedHooksTimeouts(t *testing.T) {
	s, _ := generate(t, sdk.PermissionPolicy{})
	if s.Generated != "EverGenie — 自動產生，不要手改" {
		t.Fatalf("_generated = %q", s.Generated)
	}
	wantCommands := map[string][]string{
		"SessionStart": {"SessionStart"}, "UserPromptSubmit": {"UserPromptSubmit", "context"},
		"PreToolUse": {"PreToolUse", "check"}, "PostToolUse": {"PostToolUse"}, "Stop": {"Stop"}, "StopFailure": {"StopFailure"},
		"SessionEnd": {"SessionEnd"}, "Notification": {"Notification"}, "PermissionRequest": {"PermissionRequest"}, "PreCompact": {"PreCompact"},
	}
	if len(s.Hooks) != len(wantCommands) {
		t.Fatalf("事件數 = %d，要 %d", len(s.Hooks), len(wantCommands))
	}
	for event, suffixes := range wantCommands {
		groups := s.Hooks[event]
		if len(groups) != 1 || len(groups[0].Hooks) != len(suffixes) {
			t.Fatalf("%s 的 hook 數不對：%+v", event, groups)
		}
		for i, h := range groups[0].Hooks {
			if h.Type != "command" || !strings.HasSuffix(h.Command, " hook "+suffixes[i]) || !strings.Contains(h.Command, "evergenie-engine-claude") {
				t.Errorf("%s[%d] 指令不對：%q", event, i, h.Command)
			}
			if event == "PermissionRequest" {
				if h.Timeout <= 12*60*60 {
					t.Errorf("PermissionRequest timeout = %d，要比選擇題的 12 小時長（ECL-56）", h.Timeout)
				}
			} else if h.Timeout <= 0 || h.Timeout > 5 {
				t.Errorf("%s[%d] timeout = %d，要 ≤ 5", event, i, h.Timeout)
			}
		}
	}
}

// Guards: REQ-76
func TestNoMCPGenerated(t *testing.T) {
	_, data := generate(t, sdk.PermissionPolicy{Mode: sdk.PermissionModeAuto, Repos: []sdk.RepoPolicy{{Path: "/work/repo"}}})
	text := strings.ToLower(string(data))
	if strings.Contains(text, "mcp") {
		t.Fatalf("產生的設定不該有 MCP 相關內容：%s", data)
	}
}

// Guards: REQ-84
func TestSetPermissionGeneratesNative(t *testing.T) {
	home := t.TempDir()
	g := Generator{Home: home, HookBin: "/opt/hg/evergenie-engine-claude", APISocket: "/tmp/hg-x/api.sock", Now: func() time.Time { return fixedNow }}
	policy := &sdk.PermissionPolicy{
		Mode:  sdk.PermissionModeAuto,
		Repos: []sdk.RepoPolicy{{Path: "/work/a", Push: true}, {Path: "/work/b"}, {Path: "/work/a"}},
		Allow: []string{"git status"},
		Deny:  []string{"rm"},
	}
	resp, err := g.SetPermission(&sdk.EngineSetPermissionRequest{Account: "claude-acme", Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "runtime", "engine-claude", "claude-acme", "settings.json")
	if len(resp.Artifacts) != 1 || resp.Artifacts[0] != want || !resp.GeneratedAt.Equal(fixedNow) {
		t.Fatalf("回應不對：%+v", resp)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	// 只有 hooks、_generated、permissions.additionalDirectories、固定的 skillOverrides；Allow／Deny 不轉成原生規則。
	for k := range raw {
		if k != "hooks" && k != "_generated" && k != "permissions" && k != "skillOverrides" {
			t.Errorf("多出頂層欄位 %q", k)
		}
	}
	perms, _ := raw["permissions"].(map[string]any)
	if len(perms) != 1 {
		t.Fatalf("permissions 只該有 additionalDirectories：%v", perms)
	}
	dirs, _ := perms["additionalDirectories"].([]any)
	if len(dirs) != 2 || dirs[0] != "/work/a" || dirs[1] != "/work/b" {
		t.Fatalf("additionalDirectories = %v", dirs)
	}
	if strings.Contains(string(data), "git status") || strings.Contains(string(data), `"rm"`) {
		t.Fatal("Allow／Deny 不該被轉成原生規則")
	}
	// 沒有 Repos 就沒有 permissions 區塊。
	if _, err := g.SetPermission(&sdk.EngineSetPermissionRequest{Account: "other"}); err != nil {
		t.Fatal(err)
	}
	other, _ := os.ReadFile(filepath.Join(home, "runtime", "engine-claude", "other", "settings.json"))
	if bytes.Contains(other, []byte("permissions")) {
		t.Fatal("沒有 Repos 不該有 permissions 區塊")
	}
	// Account 空 → Invalid("account")。
	_, err = g.SetPermission(&sdk.EngineSetPermissionRequest{Policy: policy})
	var e *sdk.Error
	if !errors.As(err, &e) || e.Code != sdk.ErrorCodeInvalid || e.Field != "account" {
		t.Fatalf("Account 空要回 Invalid(account)：%v", err)
	}
	if _, err := g.SetPermission(&sdk.EngineSetPermissionRequest{Account: "../x"}); err == nil {
		t.Fatal("帳號名稱含路徑符號要拒絕")
	}
}

// Guards: REQ-84
func TestDoctorDetectsEdits(t *testing.T) {
	home := t.TempDir()
	g := Generator{Home: home, HookBin: "/opt/hg/evergenie-engine-claude", APISocket: "/tmp/hg-x/api.sock", Now: func() time.Time { return fixedNow }}
	policy := sdk.PermissionPolicy{Repos: []sdk.RepoPolicy{{Path: "/work/a"}}}
	if _, err := g.SetPermission(&sdk.EngineSetPermissionRequest{Account: "a", Policy: &policy}); err != nil {
		t.Fatal(err)
	}
	dir, _ := g.Dir("a")
	report, err := Doctor(dir, policy, g.HookBin, g.APISocket)
	if err != nil || !report.OK {
		t.Fatalf("剛產生的應該一致：%+v %v", report, err)
	}
	path := filepath.Join(dir, "settings.json")
	data, _ := os.ReadFile(path)
	// 手改一個字。
	edited := bytes.Replace(data, []byte("/work/a"), []byte("/work/b"), 1)
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	if report, _ := Doctor(dir, policy, g.HookBin, g.APISocket); report.OK || len(report.Problems) == 0 {
		t.Fatalf("手改後 doctor 要回報不一致：%+v", report)
	}
	// 政策變了、檔案沒重產，也要回報。
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	changed := sdk.PermissionPolicy{Repos: []sdk.RepoPolicy{{Path: "/work/a"}, {Path: "/work/z"}}}
	if report, _ := Doctor(dir, changed, g.HookBin, g.APISocket); report.OK {
		t.Fatal("政策變了要回報不一致")
	}
	// 檔案不見。
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if report, _ := Doctor(dir, policy, g.HookBin, g.APISocket); report.OK || len(report.Problems) == 0 {
		t.Fatal("檔案不存在要回報")
	}
}

// Guards: REQ-84
func TestSetPermissionDryRunReportsDrift(t *testing.T) {
	home := t.TempDir()
	g := Generator{Home: home, HookBin: "/opt/hg/evergenie-engine-claude", APISocket: "/tmp/hg-x/api.sock", Now: func() time.Time { return fixedNow }}
	policy := &sdk.PermissionPolicy{Repos: []sdk.RepoPolicy{{Path: "/work/a"}}}
	dry := &sdk.EngineSetPermissionRequest{Account: "a", Policy: policy, DryRun: true}

	// 還沒產生過：DryRun 回報「設定檔不存在」，而且不建任何檔或資料夾。
	resp, err := g.SetPermission(dry)
	if err != nil || len(resp.Drift) != 1 || len(resp.Artifacts) != 0 {
		t.Fatalf("沒有產物時要回一條 Drift：%+v %v", resp, err)
	}
	if _, err := os.Stat(filepath.Join(home, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("DryRun 不准建立任何東西：%v", err)
	}

	// 剛產生：一致，Drift 是空的。
	if _, err := g.SetPermission(&sdk.EngineSetPermissionRequest{Account: "a", Policy: policy}); err != nil {
		t.Fatal(err)
	}
	path, _ := g.SettingsPath("a")
	if resp, err = g.SetPermission(dry); err != nil || len(resp.Drift) != 0 {
		t.Fatalf("剛產生的應該一致：%+v %v", resp, err)
	}

	// 手改一個字：Drift 有內容，而且檔案的內容與修改時間都不變。
	data, _ := os.ReadFile(path)
	edited := bytes.Replace(data, []byte("/work/a"), []byte("/work/z"), 1)
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	resp, err = g.SetPermission(dry)
	if err != nil || len(resp.Drift) == 0 || len(resp.Artifacts) != 0 {
		t.Fatalf("被手改要回報 Drift：%+v %v", resp, err)
	}
	info, _ := os.Stat(path)
	after, _ := os.ReadFile(path)
	if !info.ModTime().Equal(past) || !bytes.Equal(after, edited) {
		t.Fatal("DryRun 不准動檔案（內容或 mtime 變了）")
	}

	// Account 空照樣是 Invalid("account")。
	_, err = g.SetPermission(&sdk.EngineSetPermissionRequest{Policy: policy, DryRun: true})
	var e *sdk.Error
	if !errors.As(err, &e) || e.Field != "account" {
		t.Fatalf("DryRun 的 Account 空要回 Invalid(account)：%v", err)
	}
}

// Guards: REQ-393
func TestDoctorNamesUnmatchedGrants(t *testing.T) {
	home := t.TempDir()
	g := Generator{Home: home, HookBin: "/opt/hg/evergenie-engine-claude", APISocket: "/tmp/hg-x/api.sock", Now: func() time.Time { return fixedNow }}
	policy := sdk.PermissionPolicy{Guidance: []string{"在 ~/Workspace 底下跑 make ci 是主人要的"}}
	if _, err := g.SetPermission(&sdk.EngineSetPermissionRequest{Account: "a", Policy: &policy}); err != nil {
		t.Fatal(err)
	}
	dir, _ := g.Dir("a")
	path := filepath.Join(dir, "settings.json")
	data, _ := os.ReadFile(path)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	// 手加一句說明、一條放行規則。
	raw["autoMode"] = map[string]any{"allow": []string{"在 ~/Workspace 底下跑 make ci 是主人要的", "可以刪 ~/Downloads"}}
	raw["permissions"] = map[string]any{"allow": []string{"Bash(rm:*)"}}
	edited, _ := json.Marshal(raw)
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Doctor(dir, policy, g.HookBin, g.APISocket)
	if err != nil || report.OK {
		t.Fatalf("手加放行要回報：%+v %v", report, err)
	}
	all := strings.Join(report.Problems, "\n")
	for _, want := range []string{"多了一句沒有對應申請的放行說明：「可以刪 ~/Downloads」", "多了一條沒有對應申請的放行規則：Bash(rm:*)"} {
		if !strings.Contains(all, want) {
			t.Fatalf("少了 %q：\n%s", want, all)
		}
	}
	if strings.Contains(all, "make ci") {
		t.Fatalf("對得上申請的那句不該報：\n%s", all)
	}
	// 撤銷了但檔案還留著：報「沒有對應申請」；核准了但檔案沒有：報「少了」。
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	report, _ = Doctor(dir, sdk.PermissionPolicy{}, g.HookBin, g.APISocket)
	if !strings.Contains(strings.Join(report.Problems, "\n"), "多了一句沒有對應申請的放行說明：「在 ~/Workspace 底下跑 make ci 是主人要的」") {
		t.Fatalf("撤銷後殘留要報：%+v", report)
	}
	more := sdk.PermissionPolicy{Guidance: []string{"在 ~/Workspace 底下跑 make ci 是主人要的", "可以讀 ~/Downloads"}}
	report, _ = Doctor(dir, more, g.HookBin, g.APISocket)
	if !strings.Contains(strings.Join(report.Problems, "\n"), "少了一句已核准的放行說明：「可以讀 ~/Downloads」") {
		t.Fatalf("核准了沒寫進去要報：%+v", report)
	}
}

// 產生的設定一律關掉平台內建的 auto-mode-setup（它會在對話中途跳出選單擋住輸入框，DEV-159）；
// 政策怎麼寫都一樣，doctor 拿同一份比。
func TestGeneratedSettingsTurnOffAutoModeSetup(t *testing.T) {
	for _, policy := range []sdk.PermissionPolicy{{}, {Guidance: []string{"可以跑 make ci"}, Repos: []sdk.RepoPolicy{{Path: "/work/a"}}}} {
		data, err := GenerateSettings(policy, "/opt/hg/evergenie-engine-claude", "/tmp/hg-x/api.sock")
		if err != nil {
			t.Fatal(err)
		}
		var raw struct {
			SkillOverrides map[string]string `json:"skillOverrides"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		if len(raw.SkillOverrides) != 1 || raw.SkillOverrides["auto-mode-setup"] != "off" {
			t.Fatalf("skillOverrides 要是 {auto-mode-setup: off}：%v", raw.SkillOverrides)
		}
	}
}
