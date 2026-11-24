package report

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

func runCLI(t *testing.T, collect Collect, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cmdline.Run(context.Background(), args, nil, cmdline.IO{Out: &out, Err: &errOut}, Commands(collect), "用法：evergenie report")
	return code, out.String(), errOut.String()
}

// `evergenie report`：服務沒起來也產生得出來（只讀家與日誌），印出內容並說明沒有送出；--out 存檔、--json 給程式讀。
func TestReportCommand(t *testing.T) {
	home := homefixture.New(t).Dir
	if err := os.MkdirAll(filepath.Join(home, "runtime", "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	var gotHome string
	collect := func(_ *cmdline.Env, h string) []sdk.DoctorCheck {
		gotHome = h
		return []sdk.DoctorCheck{{ID: "serve.down", Group: sdk.DoctorGroupConfig, Severity: sdk.DoctorSeverityError, Detail: "EverGenie 沒在跑"}}
	}
	code, out, errOut := runCLI(t, collect, "report", "--home", home)
	if code != 0 || gotHome != home || !strings.Contains(out, "# 問題回報：") || !strings.Contains(out, "- ✗ serve.down\n") || strings.Contains(out, "沒在跑") || !strings.Contains(out, "沒有送出任何東西") {
		t.Fatalf("code=%d home=%q\n%s\n%s", code, gotHome, out, errOut)
	}
	if strings.Contains(out, "詳細日誌") {
		t.Errorf("沒加 --detail 不該有詳細日誌：\n%s", out)
	}

	file := filepath.Join(t.TempDir(), "回報.md")
	code, out, _ = runCLI(t, collect, "report", "--detail", "--home="+home, "--out", file)
	data, err := os.ReadFile(file)
	if code != 0 || err != nil || !strings.Contains(string(data), "## 詳細日誌") || !strings.Contains(string(data), "✗ EverGenie 沒在跑（serve.down）") || !strings.Contains(out, "沒有送出任何東西") {
		t.Fatalf("code=%d err=%v\n%s\n%s", code, err, out, data)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Errorf("回報檔的權限要是 0600：%v", st.Mode().Perm())
	}

	code, out, _ = runCLI(t, collect, "report", "--json", "--home", home)
	var res sdk.SystemReportResponse
	if code != 0 || json.Unmarshal([]byte(strings.TrimSpace(out)), &res) != nil || res.Title == "" || !strings.Contains(res.Body, "## 健康檢查") || res.IssueLink != "" {
		t.Fatalf("code=%d\n%s", code, out)
	}

	if code, _, errOut = runCLI(t, collect, "report", "--send"); code != 2 || !strings.Contains(errOut, "用法：evergenie report") {
		t.Fatalf("不認得的選項要回用法：code=%d %s", code, errOut)
	}
}

type fakeDoctor struct{ checks []sdk.DoctorCheck }

func (d fakeDoctor) Doctor(context.Context, *sdk.SystemDoctorRequest) (*sdk.SystemDoctorResponse, error) {
	return &sdk.SystemDoctorResponse{Checks: d.checks}, nil
}

// 看板的 system.report：健檢是服務裡跑的那一份加上不用服務的那幾項；detail 才帶日誌；設了回報頁才有網址。
func TestReportService(t *testing.T) {
	home := homefixture.New(t).Dir
	m := New(Deps{
		Doctor: fakeDoctor{[]sdk.DoctorCheck{{ID: "push.ok", Group: sdk.DoctorGroupPush, Ok: true, Severity: sdk.DoctorSeverityOk, Detail: "好"}}},
		Offline: func(context.Context, string) []sdk.DoctorCheck {
			return []sdk.DoctorCheck{{ID: "config.settings", Group: sdk.DoctorGroupConfig, Severity: sdk.DoctorSeverityWarn, Detail: "settings/x.toml 格式不對"}}
		},
	})
	m.SetHome(home)
	svc := m.Routes()[0].Impl.(*service)
	res, err := svc.Report(context.Background(), &sdk.SystemReportRequest{})
	if err != nil || !strings.Contains(res.Body, "要處理 0 項、注意 1 項、正常 1 項") || !strings.Contains(res.Body, "- ! config.settings\n") || strings.Contains(res.Body, "格式不對") || strings.Contains(res.Body, "詳細日誌") || res.IssueLink != "" {
		t.Fatalf("err=%v %+v", err, res)
	}
	t.Setenv("EVERGENIE_REPORT_ISSUE_URL", "https://example.invalid/issues/new")
	res, err = svc.Report(context.Background(), &sdk.SystemReportRequest{Detail: true})
	if err != nil || !strings.Contains(res.Body, "## 詳細日誌") || !strings.Contains(res.Body, "! settings/x.toml 格式不對（config.settings）") || !strings.HasPrefix(res.IssueLink, "https://example.invalid/issues/new?title=") {
		t.Fatalf("err=%v %+v", err, res)
	}
}
