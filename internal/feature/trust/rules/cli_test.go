package rules

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// fakeAPI 記下收到的請求、回固定答案；指令只認 sdk 的 client，所以用 sdk.Serve 開一個真的 unix socket。
type fakeAPI struct {
	sdk.SystemAPI
	mu       sync.Mutex
	trustReq []*sdk.SystemTrustRequest
	setReq   *sdk.SystemSetPermissionLevelsRequest
	doctor   *sdk.SystemDoctorRequest
	trust    *sdk.SystemTrustResponse
}

func (f *fakeAPI) Trust(_ context.Context, r *sdk.SystemTrustRequest) (*sdk.SystemTrustResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trustReq = append(f.trustReq, r)
	return f.trust, nil
}
func (f *fakeAPI) SetPermissionLevels(_ context.Context, r *sdk.SystemSetPermissionLevelsRequest) (*sdk.SystemSetPermissionLevelsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setReq = r
	return &sdk.SystemSetPermissionLevelsResponse{
		Agents: []sdk.AgentPermission{{Agent: "code", Level: sdk.PermissionLevelStandard, Source: "default_level"}, {Agent: "life", Level: sdk.PermissionLevelFull, Source: "agent"}},
		Backup: "runtime/backups/trust/x.toml",
	}, nil
}
func (f *fakeAPI) Doctor(_ context.Context, r *sdk.SystemDoctorRequest) (*sdk.SystemDoctorResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.doctor = r
	return &sdk.SystemDoctorResponse{Checks: []sdk.DoctorCheck{{Group: sdk.DoctorGroupPermissions, Severity: sdk.DoctorSeverityOk, Detail: "一致"}}}, nil
}

func serveFake(t *testing.T) (*sdk.Client, *fakeAPI) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAPI{trust: &sdk.SystemTrustResponse{
		DefaultLevel: sdk.PermissionLevelFull,
		Agents:       []sdk.AgentPermission{{Agent: "code", Level: sdk.PermissionLevelReadOnly, Source: "agent"}, {Agent: "life", Level: sdk.PermissionLevelFull, Source: "builtin"}},
		Policy:       &sdk.PermissionPolicy{Repos: []sdk.RepoPolicy{{Path: "/tmp/r", Writers: []string{"code"}}}, Deny: []string{"send_external"}},
		Sources: []sdk.PermissionSource{
			{Scope: "秘書 code", Source: "settings/trust.toml", Kind: "level", Value: "readonly"},
			{Scope: "repo", Source: "settings/trust.toml", Kind: "repo", Value: "/tmp/r"},
		},
		Denied: []sdk.DeniedAction{{At: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), Agent: "code", From: "life", Class: "delete", Tool: "rm", Verdict: "deny", Reason: "不行"}},
	}}
	sock := filepath.Join(dir, "api.sock")
	mux := http.NewServeMux()
	if err := sdk.Mount(mux, "system", f); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	done := make(chan error, 1)
	go func() { done <- server.Serve(ln) }()
	t.Cleanup(func() {
		_ = server.Close()
		if err := <-done; err != http.ErrServerClosed {
			t.Error(err)
		}
		_ = os.RemoveAll(dir)
	})
	return sdk.Dial(sock), f
}

// printChecks 代替 doctor 業務模組的 Print（registry 才接真的）：每條印「健檢 <ID>：<說明>」。
func printChecks(e *cmdline.Env, checks []sdk.DoctorCheck) error {
	for _, c := range checks {
		if _, err := fmt.Fprintf(e.Out, "健檢 %s：%s\n", c.ID, c.Detail); err != nil {
			return err
		}
	}
	return nil
}

func hg(t *testing.T, c *sdk.Client, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(stdin), Out: &out, Err: &errb}, Commands(printChecks), "用法\n")
	return code, out.String(), errb.String()
}

func mustHG(t *testing.T, c *sdk.Client, stdin string, args ...string) string {
	t.Helper()
	code, out, errb := hg(t, c, stdin, args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errb)
	}
	return out
}

func TestCommandsListsTrustAndSetupPermissions(t *testing.T) {
	var _ map[string]app.Command = Commands(nil)
	want := []string{"trust show", "trust scan", "trust diff", "trust denied", "trust snapshot", "setup permissions"}
	got := Commands(nil)
	if len(got) != len(want) {
		t.Fatalf("指令數 %d", len(got))
	}
	for _, name := range want {
		if got[name].Run == nil {
			t.Errorf("缺 %s", name)
		}
	}
}

func TestShowPrintsLevelsReposAndDeny(t *testing.T) {
	c, _ := serveFake(t)
	out := mustHG(t, c, "", "trust", "show")
	for _, want := range []string{"code       唯讀（只看不改）　（單獨設定）", "life       最高（全部讓 AI 做）　（內建預設）", "其他秘書：最高", "/tmp/r　不能 push；可寫：code", "【不開放】send_external"} {
		if !strings.Contains(out, want) {
			t.Errorf("缺 %q：\n%s", want, out)
		}
	}
	if out := mustHG(t, c, "", "trust", "show", "--json"); !strings.HasPrefix(out, "{") {
		t.Errorf("--json 印一行 JSON：%q", out)
	}
}

func TestScanFiltersByScopeAndFlagsAreChecked(t *testing.T) {
	c, _ := serveFake(t)
	if out := mustHG(t, c, "", "trust", "scan"); !strings.Contains(out, "## repo") || !strings.Contains(out, "共 2 條") {
		t.Fatalf("scan：\n%s", out)
	}
	if out := mustHG(t, c, "", "trust", "scan", "--scope", "秘書"); strings.Contains(out, "repo") || !strings.Contains(out, "共 1 條") {
		t.Fatalf("--scope：\n%s", out)
	}
	for _, args := range [][]string{{"trust", "show", "--days", "3"}, {"trust", "denied", "--days", "0"}, {"trust", "denied", "--days", "x"}, {"trust", "scan", "stray"}} {
		if code, out, errb := hg(t, c, "", args...); code != 2 || out != "" || errb == "" {
			t.Errorf("%v → %d %q %q", args, code, out, errb)
		}
	}
}

func TestDeniedAndSnapshotAndDiffSendTheRightRequests(t *testing.T) {
	c, f := serveFake(t)
	if out := mustHG(t, c, "", "trust", "denied", "--days", "3"); !strings.Contains(out, "最近 3 天被擋 1 次") || !strings.Contains(out, "code　delete　deny：1 次") {
		t.Fatalf("denied：\n%s", out)
	}
	if r := f.trustReq[len(f.trustReq)-1]; r.DeniedDays != 3 || r.Snapshot {
		t.Fatalf("denied 請求：%+v", r)
	}
	f.trust.SnapshotPath, f.trust.Changes = "runtime/trust/snapshots/a.json", []string{"新增 repo 的 repo：/tmp/r"}
	out := mustHG(t, c, "", "trust", "snapshot")
	if !strings.Contains(out, "快照：runtime/trust/snapshots/a.json") || !strings.Contains(out, "新增 repo 的 repo") || !strings.Contains(out, "最近 7 天被擋 1 次") {
		t.Fatalf("snapshot：\n%s", out)
	}
	if r := f.trustReq[len(f.trustReq)-1]; !r.Snapshot {
		t.Fatalf("snapshot 請求：%+v", r)
	}
	if out := mustHG(t, c, "", "trust", "diff"); !strings.Contains(out, "：一致") {
		t.Fatalf("diff：\n%s", out)
	}
	if len(f.doctor.Groups) != 1 || f.doctor.Groups[0] != sdk.DoctorGroupPermissions {
		t.Fatalf("diff 只問權限那一組：%+v", f.doctor)
	}
}

// 互動選項、--choice、--agent／--level（clear 拿掉單獨設定）；操作者來自 $EVERGENIE_AGENT。
func TestSetupPermissionsMapsChoicesToRequests(t *testing.T) {
	t.Setenv("EVERGENIE_AGENT", "")
	c, f := serveFake(t)
	out := mustHG(t, c, "2\n", "setup", "permissions")
	for _, want := range []string{"1. 全部讓 AI 幫我做", "2. 做大事先問我", "3. 只讓 AI 看不讓它改", "已更新權限", "life 另外單獨設定過", "舊的設定備份在 runtime/backups/trust/x.toml"} {
		if !strings.Contains(out, want) {
			t.Errorf("缺 %q：\n%s", want, out)
		}
	}
	if f.setReq.DefaultLevel != sdk.PermissionLevelStandard || f.setReq.By.Name != "owner(cli)" {
		t.Fatalf("互動選 2：%+v", f.setReq)
	}
	for choice, want := range map[string]sdk.PermissionLevel{"all": sdk.PermissionLevelFull, "read": sdk.PermissionLevelReadOnly, "1": sdk.PermissionLevelFull, "3": sdk.PermissionLevelReadOnly} {
		mustHG(t, c, "", "setup", "permissions", "--choice", choice)
		if f.setReq.DefaultLevel != want {
			t.Errorf("選 %s：%+v", choice, f.setReq)
		}
	}
	mustHG(t, c, "", "setup", "permissions", "--agent", "life", "--level", "clear")
	if len(f.setReq.Levels) != 1 || f.setReq.Levels[0].Agent != "life" || f.setReq.Levels[0].Level != "" || f.setReq.DefaultLevel != "" {
		t.Fatalf("clear：%+v", f.setReq)
	}
	t.Setenv("EVERGENIE_AGENT", "code")
	mustHG(t, c, "", "setup", "permissions", "--agent", "life", "--level", "FULL")
	if f.setReq.By.Name != "code" || f.setReq.Levels[0].Level != sdk.PermissionLevelFull {
		t.Fatalf("--level 大小寫不計、操作者 $EVERGENIE_AGENT：%+v", f.setReq)
	}
	for _, args := range [][]string{{"setup", "permissions", "--choice", "9"}, {"setup", "permissions", "--agent", "life"}, {"setup", "permissions", "--choice", "all", "--agent", "life", "--level", "full"}, {"setup", "permissions", "--nope"}} {
		if code, _, errb := hg(t, c, "", args...); code != 2 || errb == "" {
			t.Errorf("%v → %d %q", args, code, errb)
		}
	}
}

// Guards: REQ-158
func TestWizardChoices(t *testing.T) {
	for in, want := range map[string]sdk.PermissionLevel{"1": sdk.PermissionLevelFull, " all ": sdk.PermissionLevelFull, "2\n": sdk.PermissionLevelStandard, "ASK": sdk.PermissionLevelStandard, "3": sdk.PermissionLevelReadOnly, "read": sdk.PermissionLevelReadOnly} {
		if got, ok := WizardLevel(in); !ok || got != want {
			t.Errorf("%q → %q %v，要 %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "0", "4", "full"} {
		if _, ok := WizardLevel(in); ok {
			t.Errorf("%q 不該被接受", in)
		}
	}
}
