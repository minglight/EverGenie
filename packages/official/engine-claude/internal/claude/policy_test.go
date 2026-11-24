package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-168, REQ-84
func TestRulesBecomeAllowedTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ws := filepath.Join(home, "Workspace")
	repo := filepath.Join(ws, "amy", "evergenie")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	rules := []sdk.PolicyRule{
		{Agent: "code", Action: sdk.ActionClassWrite, Scope: "~/Workspace/amy/evergenie"},
		{Agent: "code", Action: sdk.ActionClassRead, Scope: "/nope/missing"},
		{Agent: "code", Action: sdk.ActionClassCommit, Scope: "~/Workspace/amy/evergenie"},
		{Agent: "code", Action: sdk.ActionClassPush},
		{Agent: "code", Action: sdk.ActionClassDelete},
		{Agent: "code", Action: sdk.ActionClassRead},
		{Agent: "code", Action: sdk.ActionClassPush},     // 重複只留一個
		{Agent: "code", Action: sdk.ActionClassSettings}, // 不能開成規則的動作直接略過
	}
	tools, dirs := RuleTools(rules)
	want := []string{"Edit(/" + repo + "/**)", "Read(//nope/missing/**)", "Bash(git commit:*)", "Bash(git push:*)", "Bash(rm:*)", "Read"}
	if !reflect.DeepEqual(tools, want) {
		t.Fatalf("tools = %q\nwant  %q", tools, want)
	}
	if !reflect.DeepEqual(dirs, []string{repo}) {
		t.Fatalf("只有存在的資料夾加進 --add-dir：%q", dirs)
	}

	// full 等級：跟 full 那組合併成同一個 --allowedTools；standard 也照樣帶規則。
	for _, tc := range []struct {
		level   sdk.PermissionLevel
		allowed string
		extra   []string
	}{
		{sdk.PermissionLevelFull, "Edit(/" + ws + "/**),Bash(evergenie:*),Bash(cd:*),Edit(/" + repo + "/**),Bash(git push:*)", []string{ws, repo}},
		{sdk.PermissionLevelStandard, "Edit(/" + repo + "/**),Bash(git push:*)", []string{repo}},
	} {
		v := newEngEnv(t)
		c := v.start("s", func(r *sdk.EngineStartRequest) {
			r.PermissionLevel = tc.level
			r.Rules = []sdk.PolicyRule{{Agent: "code", Action: sdk.ActionClassWrite, Scope: "~/Workspace/amy/evergenie"}, {Agent: "code", Action: sdk.ActionClassPush}}
		})
		if n := strings.Count(strings.Join(c.Argv, " "), "--allowedTools"); n != 1 {
			t.Fatalf("%s：--allowedTools 只能有一個，實際 %d", tc.level, n)
		}
		got, _ := argvValue(c.Argv, "--allowedTools")
		if got != tc.allowed {
			t.Errorf("%s：--allowedTools = %q，要 %q", tc.level, got, tc.allowed)
		}
		i := addDirIndex(c.Argv)
		if i < 0 {
			t.Fatalf("--add-dir 要放最後：%q", c.Argv)
		}
		if extra := c.Argv[i+2:]; !reflect.DeepEqual(extra, tc.extra) {
			t.Errorf("%s：--add-dir 多給的資料夾 = %q，要 %q", tc.level, extra, tc.extra)
		}
	}
	// 沒有規則、也不是 full：不帶 --allowedTools。
	v := newEngEnv(t)
	c := v.start("s", func(r *sdk.EngineStartRequest) { r.PermissionLevel = sdk.PermissionLevelStandard })
	if _, ok := argvValue(c.Argv, "--allowedTools"); ok {
		t.Fatal("沒有規則不該帶 --allowedTools")
	}
}

// Guards: REQ-168, REQ-84
func TestGuidanceWritesAutoModeAllow(t *testing.T) {
	home := t.TempDir()
	g := Generator{Home: home, HookBin: "/opt/hg/evergenie-engine-claude", APISocket: "/tmp/hg-x/api.sock"}
	policy := &sdk.PermissionPolicy{Guidance: []string{"在 ~/Workspace 底下跑 make ci 是主人要的", " ", "在 ~/Workspace 底下跑 make ci 是主人要的", "可以讀 ~/Downloads"}}
	if _, err := g.SetPermission(&sdk.EngineSetPermissionRequest{Account: "claude", Policy: policy}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "runtime", "engine-claude", "claude", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		AutoMode struct {
			Allow []string `json:"allow"`
		} `json:"autoMode"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw.AutoMode.Allow, []string{"在 ~/Workspace 底下跑 make ci 是主人要的", "可以讀 ~/Downloads"}) {
		t.Fatalf("autoMode.allow = %q", raw.AutoMode.Allow)
	}
	// doctor：一樣的政策沒有不一致；拿掉 guidance 就報不一致（撤銷後要重推）。
	dry, err := g.SetPermission(&sdk.EngineSetPermissionRequest{Account: "claude", Policy: policy, DryRun: true})
	if err != nil || len(dry.Drift) != 0 {
		t.Fatalf("一致時沒有 Drift：%+v %v", dry, err)
	}
	dry, err = g.SetPermission(&sdk.EngineSetPermissionRequest{Account: "claude", Policy: &sdk.PermissionPolicy{}, DryRun: true})
	if err != nil || len(dry.Drift) == 0 {
		t.Fatalf("guidance 不一致要報：%+v %v", dry, err)
	}
	// 撤銷（空 guidance）後跟從來沒有申請過的產物逐字相同。
	if _, err := g.SetPermission(&sdk.EngineSetPermissionRequest{Account: "claude", Policy: &sdk.PermissionPolicy{}}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if _, err := g.SetPermission(&sdk.EngineSetPermissionRequest{Account: "fresh", Policy: &sdk.PermissionPolicy{}}); err != nil {
		t.Fatal(err)
	}
	fresh, _ := os.ReadFile(filepath.Join(home, "runtime", "engine-claude", "fresh", "settings.json"))
	if !bytes.Equal(after, fresh) || bytes.Contains(after, []byte("autoMode")) {
		t.Fatal("撤銷後要回到申請前的樣子")
	}
}
