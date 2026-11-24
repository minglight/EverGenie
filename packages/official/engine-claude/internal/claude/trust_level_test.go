package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-84
func TestTrustLevelsRender(t *testing.T) {
	cases := []struct {
		level     sdk.PermissionLevel
		mode      string
		disallow  string
		wantFlags bool
	}{
		{sdk.PermissionLevelFull, "auto", "", false},
		{sdk.PermissionLevelStandard, "acceptEdits", "", false},
		{sdk.PermissionLevelReadOnly, "plan", readOnlyDenied, true},
		{"", "auto", "", false}, // 沒給等級：引擎預設（ECL-1）
	}
	t.Setenv("HOME", t.TempDir()) // 不讓真的家目錄影響參數
	for _, tc := range cases {
		v := newEngEnv(t)
		c := v.start("s", func(r *sdk.EngineStartRequest) { r.PermissionLevel = tc.level })
		if m, _ := argvValue(c.Argv, "--permission-mode"); m != tc.mode {
			t.Errorf("等級 %q：--permission-mode = %q，要 %q", tc.level, m, tc.mode)
		}
		d, has := argvValue(c.Argv, "--disallowedTools")
		if has != tc.wantFlags || d != tc.disallow {
			t.Errorf("等級 %q：--disallowedTools = %q（有 %v）", tc.level, d, has)
		}
		if i := addDirIndex(c.Argv); i < 0 {
			t.Errorf("等級 %q：--add-dir 要放最後：%q", tc.level, c.Argv)
		}
	}
}

// Guards: REQ-84
func TestTrustLevelExplicitModeWinsButReadOnlyStillDeniesWrites(t *testing.T) {
	v := newEngEnv(t)
	c := v.start("a", func(r *sdk.EngineStartRequest) {
		r.PermissionLevel = sdk.PermissionLevelFull
		r.EngineConfig["permission_mode"] = "default"
	})
	if m, _ := argvValue(c.Argv, "--permission-mode"); m != "default" {
		t.Fatalf("engine_config.permission_mode 有寫就照它：%q", m)
	}
	c = v.start("b", func(r *sdk.EngineStartRequest) {
		r.PermissionLevel = sdk.PermissionLevelReadOnly
		r.EngineConfig["permission_mode"] = "auto"
	})
	if d, ok := argvValue(c.Argv, "--disallowedTools"); !ok || d != readOnlyDenied {
		t.Fatalf("唯讀等級不管模式都要禁用寫檔工具：%q", c.Argv)
	}
}

// Guards: REQ-84
func TestTrustLevelRejectsUnknown(t *testing.T) {
	v := newEngEnv(t)
	r := v.req("x")
	r.PermissionLevel = "root"
	_, err := v.e.Start(context.Background(), r)
	var e *sdk.Error
	if !errors.As(err, &e) || e.Code != sdk.ErrorCodeInvalid || e.Field != "permission_level" {
		t.Fatalf("不認得的等級要回 Invalid(permission_level)：%v", err)
	}
	if n := len(v.tmux.callsOf("start")); n != 0 {
		t.Fatalf("不該啟動 session：%d", n)
	}
}

// Guards: REQ-84
func TestTrustLevelNeverTouchesAccountSettings(t *testing.T) {
	v := newEngEnv(t)
	acct := filepath.Join(v.cfgDir, "settings.json")
	if err := os.WriteFile(acct, []byte(`{"autoMode":{"allow":["trust:x"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, l := range []sdk.PermissionLevel{sdk.PermissionLevelFull, sdk.PermissionLevelStandard, sdk.PermissionLevelReadOnly} {
		v.start("s-"+string(l), func(r *sdk.EngineStartRequest) { r.PermissionLevel = l })
	}
	got, _ := os.ReadFile(acct)
	if string(got) != `{"autoMode":{"allow":["trust:x"]}}` {
		t.Fatalf("帳號的 settings.json 被動到：%s", got)
	}
}

// addDirIndex 回傳 --add-dir 的位置；它後面不能再有其他旗標（它會吃掉後面所有的值），沒有或不在最後回 -1。
func addDirIndex(argv []string) int {
	for i := len(argv) - 1; i >= 0; i-- {
		if argv[i] == "--add-dir" {
			return i
		}
		if strings.HasPrefix(argv[i], "--") {
			return -1
		}
	}
	return -1
}

// Guards: REQ-84
func TestFullLevelGrantsWorkspaceEdits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ws := filepath.Join(home, "Workspace")
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	cases := []struct {
		name    string
		level   sdk.PermissionLevel
		dirs    any // nil 表示沒寫 full_dirs
		allowed string
		extra   []string
	}{
		{"預設給 ~/Workspace", sdk.PermissionLevelFull, nil, "Edit(/" + ws + "/**),Bash(evergenie:*),Bash(cd:*)", []string{ws}},
		{"照 full_dirs，不存在的略過", sdk.PermissionLevelFull, []any{other, "~/nope"}, "Edit(/" + other + "/**),Bash(evergenie:*),Bash(cd:*)", []string{other}},
		{"full_dirs 空陣列只留 EverGenie 與 cd", sdk.PermissionLevelFull, []any{}, "Bash(evergenie:*),Bash(cd:*)", nil},
		{"standard 不給", sdk.PermissionLevelStandard, nil, "", nil},
		{"readonly 不給", sdk.PermissionLevelReadOnly, nil, "", nil},
	}
	for _, tc := range cases {
		v := newEngEnv(t)
		c := v.start("s", func(r *sdk.EngineStartRequest) {
			r.PermissionLevel = tc.level
			if tc.dirs != nil {
				r.EngineConfig["full_dirs"] = tc.dirs
			}
		})
		got, _ := argvValue(c.Argv, "--allowedTools")
		if got != tc.allowed {
			t.Errorf("%s：--allowedTools = %q，要 %q", tc.name, got, tc.allowed)
		}
		i := addDirIndex(c.Argv)
		if i < 0 {
			t.Fatalf("%s：--add-dir 要放最後：%q", tc.name, c.Argv)
		}
		if dirs := c.Argv[i+2:]; strings.Join(dirs, "|") != strings.Join(tc.extra, "|") {
			t.Errorf("%s：--add-dir 多給的資料夾 = %q，要 %q", tc.name, dirs, tc.extra)
		}
	}
	v := newEngEnv(t)
	r := v.req("bad")
	r.PermissionLevel = sdk.PermissionLevelFull
	r.EngineConfig["full_dirs"] = "relative/dir"
	var e *sdk.Error
	if _, err := v.e.Start(context.Background(), r); !errors.As(err, &e) || e.Field != "engine_config.full_dirs" {
		t.Fatalf("full_dirs 不是陣列要回 Invalid：%v", err)
	}
}
