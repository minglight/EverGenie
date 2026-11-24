package settings

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// fakeSystem 是假的 system 服務：只實作這個業務模組的指令會打到的方法，加上 config set 之後會跑的 doctor。
type fakeSystem struct {
	sdk.SystemAPI
	mu      sync.Mutex
	config  map[string]string
	doctor  int
	backups []sdk.BackupFile
	lastReq *sdk.SystemBackupsRequest
	pre     []sdk.PreflightCheck
}

func (s *fakeSystem) GetConfig(_ context.Context, r *sdk.SystemGetConfigRequest) (*sdk.SystemGetConfigResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Key == "" {
		return &sdk.SystemGetConfigResponse{Text: "[user]\nname = \"Amy\"\n"}, nil
	}
	v, ok := s.config[r.Key]
	if !ok {
		return nil, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "home.toml 沒有 " + r.Key}
	}
	return &sdk.SystemGetConfigResponse{Key: r.Key, Value: v}, nil
}
func (s *fakeSystem) SetConfig(_ context.Context, r *sdk.SystemSetConfigRequest) (*sdk.SystemSetConfigResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config[r.Key] = r.Value
	return &sdk.SystemSetConfigResponse{Key: r.Key, Value: r.Value, Backup: "runtime/backups/home/x.toml"}, nil
}
func (s *fakeSystem) Doctor(context.Context, *sdk.SystemDoctorRequest) (*sdk.SystemDoctorResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doctor++
	return &sdk.SystemDoctorResponse{Checks: []sdk.DoctorCheck{{ID: "config.ok", Group: sdk.DoctorGroupConfig, Ok: true, Severity: sdk.DoctorSeverityOk, Detail: "設定都正常"}}}, nil
}
func (s *fakeSystem) Backups(_ context.Context, r *sdk.SystemBackupsRequest) (*sdk.SystemBackupsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastReq = r
	out := &sdk.SystemBackupsResponse{Applied: r.Apply}
	for _, f := range s.backups {
		f.Prune = r.Prune && f.Group == "trust"
		if f.Prune {
			out.Pruned++
			out.Freed += f.Size
		}
		out.Total += f.Size
		out.Files = append(out.Files, f)
	}
	return out, nil
}
func (s *fakeSystem) Preflight(context.Context, *sdk.SystemPreflightRequest) (*sdk.SystemPreflightResponse, error) {
	return &sdk.SystemPreflightResponse{Checks: s.pre}, nil
}

func serve(t *testing.T, s *fakeSystem) *sdk.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, map[string]any{"system": s}) }()
	t.Cleanup(func() { cancel(); <-done; os.RemoveAll(dir) })
	for i := 0; ; i++ {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			break
		}
		if i > 500 {
			t.Fatal("socket never accepted a connection")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return sdk.Dial(sock)
}

// runWith 用 settings 的指令表跑一個指令，回結束碼與輸出。
func runWith(t *testing.T, c *sdk.Client, preflight cmdline.HostFunc, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(""), Out: &out, Err: &errOut}, Commands(preflight), "用法")
	return code, out.String(), errOut.String()
}

func mustRun(t *testing.T, c *sdk.Client, args ...string) string {
	t.Helper()
	code, out, errOut := runWith(t, c, nil, args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errOut)
	}
	return out
}

// config 指令：讀、改 home.toml，改完馬上跑 doctor。
func TestConfigCLI(t *testing.T) {
	t.Setenv("EVERGENIE_AGENT", "")
	sys := &fakeSystem{config: map[string]string{"user.timezone": `"Asia/Taipei"`}}
	c := serve(t, sys)

	if out := mustRun(t, c, "config", "get", "user.timezone"); strings.TrimSpace(out) != `"Asia/Taipei"` {
		t.Fatalf("config get：%q", out)
	}
	if out := mustRun(t, c, "config", "show"); !strings.Contains(out, "[user]") {
		t.Fatalf("config show：%q", out)
	}
	out := mustRun(t, c, "config", "set", "user.timezone", `"Asia/Tokyo"`)
	if sys.config["user.timezone"] != `"Asia/Tokyo"` || sys.doctor != 1 || !strings.Contains(out, "runtime/backups/home/x.toml") || !strings.Contains(out, "設定都正常") {
		t.Fatalf("config set 要備份、改完跑 doctor：%q doctor=%d", out, sys.doctor)
	}
	if code, _, _ := runWith(t, c, nil, "config", "get", "no.such"); code != 1 {
		t.Fatalf("不存在的設定：%d", code)
	}
}

// backup 指令：list 列出、prune 預設只預演、--apply 才送出刪除。
// Guards: REQ-166
func TestBackupCommands(t *testing.T) {
	t.Setenv("EVERGENIE_AGENT", "")
	s := &fakeSystem{backups: []sdk.BackupFile{
		{Path: "runtime/backups/trust/a.toml", Group: "trust", Size: 2048, ModTime: time.Now()},
		{Path: "runtime/backups/home/b.toml", Group: "home", Size: 10, ModTime: time.Now()},
	}}
	c := serve(t, s)
	if out := mustRun(t, c, "backup", "list"); !strings.Contains(out, "trust/a.toml") || !strings.Contains(out, "2.0 KB") || s.lastReq.Prune {
		t.Fatalf("list：%s", out)
	}
	out := mustRun(t, c, "backup", "prune", "--keep", "3", "--days", "7")
	if !strings.Contains(out, "預演：會刪 1 個") || s.lastReq.Apply || s.lastReq.Keep != 3 || s.lastReq.Days != 7 || !strings.Contains(out, "刪 trust") {
		t.Fatalf("prune 預演：%s %+v", out, s.lastReq)
	}
	out = mustRun(t, c, "backup", "prune", "--apply")
	if !strings.Contains(out, "已刪 1 個") || !s.lastReq.Apply || s.lastReq.By == nil || s.lastReq.By.Name != "owner(cli)" {
		t.Fatalf("prune --apply：%s %+v", out, s.lastReq)
	}
	if code, _, _ := runWith(t, c, nil, "backup", "prune", "--keep", "0"); code != 2 {
		t.Fatalf("--keep 0：%d", code)
	}
	if code, _, _ := runWith(t, c, nil, "backup", "wipe"); code != 2 {
		t.Fatalf("不認得的子指令：%d", code)
	}
}

// preflight 指令交給 registry 注入的本機實作，並帶上 serve 回報的各帳號檢查；沒注入就說不支援。
// Guards: REQ-164, REQ-165
func TestPreflightCommandCarriesEngineChecks(t *testing.T) {
	t.Setenv("EVERGENIE_AGENT", "")
	t.Setenv("EVERGENIE_HOME", "/tmp/some-home")
	c := serve(t, &fakeSystem{pre: []sdk.PreflightCheck{{ID: "acct-a", Name: "引擎程式", Ok: true}}})
	var got cmdline.HostRequest
	code, _, _ := runWith(t, c, func(_ context.Context, r cmdline.HostRequest) int { got = r; return 1 }, "preflight", "--fix")
	if code != 1 || got.Engines == nil || len(got.Args) != 1 || got.Args[0] != "--fix" || got.Home != "/tmp/some-home" {
		t.Fatalf("preflight 要帶問引擎的方法，結束碼跟著實作：%d %+v", code, got)
	}
	if extra, ok := got.Engines(); !ok || len(extra) != 1 || extra[0].ID != "acct-a" {
		t.Fatalf("問 serve 要拿到引擎的檢查：%v %+v", ok, extra)
	}
	if code, _, errOut := runWith(t, c, nil, "preflight"); code != 2 || !strings.Contains(errOut, "不支援") {
		t.Fatalf("沒注入的指令：%d %s", code, errOut)
	}
}
