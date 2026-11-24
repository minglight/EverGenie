package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// fakeSystem 是假的 system 服務：只有 Doctor，回事先放好的檢查。
type fakeSystem struct {
	sdk.SystemAPI
	checks []sdk.DoctorCheck
}

func (s *fakeSystem) Doctor(context.Context, *sdk.SystemDoctorRequest) (*sdk.SystemDoctorResponse, error) {
	return &sdk.SystemDoctorResponse{Checks: s.checks}, nil
}

func serveSystem(t *testing.T, checks []sdk.DoctorCheck) *sdk.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, map[string]any{"system": &fakeSystem{checks: checks}}) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		_ = os.RemoveAll(dir)
	})
	// 等到 socket 真的連得上（檔案在 bind 時就出現、listen 之後才收連線）。
	for i := 0; i < 500; i++ {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			return sdk.Dial(sock)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket 一直連不上")
	return nil
}

func run(t *testing.T, c *sdk.Client, extra []OfflineCheck, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{Out: &out, Err: &errOut}, Commands(extra...), "用法：evergenie doctor")
	return code, out.String(), errOut.String()
}

func okCheck(g sdk.DoctorGroup, detail string) sdk.DoctorCheck {
	return sdk.DoctorCheck{ID: string(g) + ".x", Group: g, Ok: true, Severity: sdk.DoctorSeverityOk, Detail: detail}
}

// 線上的檢查接在離線的後面；有 error（這裡是找不到家）退出碼 1，錯誤不印在 stderr（已經印在報告裡）。
// 輸出格式本身在 cmdline.PrintDoctor 的測試；全部正常退出碼 0 的情況在 cmd/evergenie/doctor_test.go（要真的健康的家）。
// Guards: REQ-117
func TestDoctorPrintsOnlineChecksAfterOffline(t *testing.T) {
	c := serveSystem(t, []sdk.DoctorCheck{
		{ID: "data.invalid", Group: sdk.DoctorGroupData, Severity: sdk.DoctorSeverityError, Detail: "task SEC-9 壞了", Fix: "打開那個檔案修正"},
		{ID: "push.status", Group: sdk.DoctorGroupPush, Severity: sdk.DoctorSeverityWarn, Detail: "還沒有裝置開啟推播"},
	})
	code, out, errOut := run(t, c, nil, "doctor", "--home", t.TempDir())
	if code != 1 || errOut != "" {
		t.Fatalf("有 error 要退出碼 1、不印錯誤行：%d %q", code, errOut)
	}
	for _, want := range []string{"【設定】\n  ✗ 家的設定讀不起來", "【資料】\n  ✗ task SEC-9 壞了\n    → 打開那個檔案修正\n", "【推播】\n  ! 還沒有裝置開啟推播\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("輸出缺 %q：\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "0 項正常、1 項注意、2 項要處理\n") {
		t.Fatalf("摘要不對：%q", out)
	}
}

// serve 沒在跑：只有離線檢查，加一條「EverGenie 沒在跑」；額外的離線檢查（launchd）拿到 --home。
// Guards: REQ-117
func TestDoctorServeDown(t *testing.T) {
	dir := t.TempDir()
	var seen []string
	extra := []OfflineCheck{func(_ context.Context, home string) []sdk.DoctorCheck {
		seen = append(seen, home)
		return []sdk.DoctorCheck{okCheck(sdk.DoctorGroupConfig, "離線：額外的檢查")}
	}}
	code, out, _ := run(t, nil, extra, "doctor", "--home", dir)
	if code != 1 || !strings.Contains(out, "✗ EverGenie 沒在跑") || !strings.Contains(out, "→ 先等一分鐘，它當掉會自己重開") || !strings.Contains(out, "evergenie 修復") || !strings.Contains(out, "離線：額外的檢查") {
		t.Fatalf("serve 沒在跑：%d\n%s", code, out)
	}
	if len(seen) != 1 || seen[0] != dir {
		t.Fatalf("額外的離線檢查要拿到 --home：%q", seen)
	}
	// 給的是不存在的 socket 路徑（client 非 nil 但連不上）也是同一個結果。
	if code, _, _ = run(t, sdk.Dial(filepath.Join(dir, "nope.sock")), nil, "doctor"); code != 1 {
		t.Fatalf("連不上要退出碼 1：%d", code)
	}
}

// --json 印一行：離線的接線上的。
// Guards: REQ-117
func TestDoctorJSON(t *testing.T) {
	c := serveSystem(t, []sdk.DoctorCheck{okCheck(sdk.DoctorGroupData, "資料都符合規格")})
	extra := []OfflineCheck{func(context.Context, string) []sdk.DoctorCheck {
		return []sdk.DoctorCheck{okCheck(sdk.DoctorGroupConfig, "離線檢查")}
	}}
	code, out, _ := run(t, c, extra, "doctor", "--json", "--home", t.TempDir())
	if code != 1 || strings.Count(out, "\n") != 1 { // 假的家讀不起來，所以有 error
		t.Fatalf("--json 印一行：%d %q", code, out)
	}
	var res sdk.SystemDoctorResponse
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Checks) < 2 {
		t.Fatalf("輸出不是檢查清單：%+v %v", res, err)
	}
	n := len(res.Checks)
	if res.Checks[n-2].Detail != "離線檢查" || res.Checks[n-1].Group != sdk.DoctorGroupData {
		t.Fatalf("要含額外的離線檢查再接線上的：%+v", res.Checks)
	}
	if code, _, errOut := run(t, c, nil, "doctor", "--nope"); code != 2 || errOut == "" {
		t.Fatalf("未知旗標是用法錯誤：%d", code)
	}
}
