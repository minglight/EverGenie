package accounts

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// fakeSystem 回固定的用量；指令只認 sdk 的 client，所以用真的 unix socket。
type fakeSystem struct {
	sdk.SystemAPI
	usage []sdk.AccountUsage
}

func (s *fakeSystem) Usage(_ context.Context, r *sdk.SystemUsageRequest) (*sdk.SystemUsageResponse, error) {
	var out []sdk.AccountUsage
	for _, u := range s.usage {
		if r.Account == "" || r.Account == u.Account {
			out = append(out, u)
		}
	}
	if r.Account != "" && len(out) == 0 {
		return nil, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "找不到帳號 " + r.Account}
	}
	return &sdk.SystemUsageResponse{Accounts: out}, nil
}

func serveFake(t *testing.T, s *fakeSystem) *sdk.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "api.sock")
	mux := http.NewServeMux()
	if err := sdk.Mount(mux, "system", s); err != nil {
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
	return sdk.Dial(sock)
}

func hg(t *testing.T, c *sdk.Client, args ...string) (int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(""), Out: &out, Err: &errb}, Commands(), "用法\n")
	return code, out.String() + errb.String()
}

// quota 印各帳號的視窗；quota check 的結束碼 0 可以繼續、1 該停、2 沒資料、3 不在重置範圍。
func TestQuotaCommands(t *testing.T) {
	now := time.Now()
	c := serveFake(t, &fakeSystem{usage: []sdk.AccountUsage{
		{Account: "acct-a", UsedRatio: 0.3, MeasuredAt: now.Add(-5 * time.Minute), Windows: []sdk.UsageWindow{
			{Kind: sdk.UsageWindowKindShort, Hours: 5, UsedRatio: 0.3, ResetsAt: now.Add(2 * time.Hour)},
			{Kind: sdk.UsageWindowKindWeekly, Hours: 168, UsedRatio: 0.9, ResetsAt: now.Add(30 * time.Hour), Rolled: true}}},
		{Account: "acct-b", Note: "還沒有用量資料（這個帳號還沒跑過 session）"},
	}})

	code, out := hg(t, c, "quota")
	if code != 0 {
		t.Fatalf("quota → %d %s", code, out)
	}
	for _, want := range []string{"acct-a", "5 小時 30%", "每週 90%", "資料較舊", "5 分鐘前", "acct-b", "還沒有用量資料"} {
		if !strings.Contains(out, want) {
			t.Errorf("quota 少了 %q：\n%s", want, out)
		}
	}
	if _, out := hg(t, c, "quota", "--json", "acct-a"); strings.Count(out, "\n") != 1 || !strings.Contains(out, `"account":"acct-a"`) {
		t.Fatalf("quota --json：%q", out)
	}

	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"沒超過", []string{"acct-a", "--5h-max", "50", "--week-max", "95"}, 0, ""},
		{"每週超過 80%", []string{"acct-a", "--week-max", "80"}, 1, "該停"},
		{"沒資料", []string{"acct-b"}, 2, ""},
		{"最近的重置在 2 小時後，不在 1 小時內", []string{"acct-a", "--reset-within", "1"}, 3, ""},
		{"在 3 小時內", []string{"acct-a", "--reset-within", "3"}, 0, ""},
		{"找不到帳號當成沒資料", []string{"nobody"}, 2, ""},
		{"數字寫錯是用法錯誤", []string{"acct-a", "--5h-max", "abc"}, 2, ""},
		{"全部帳號，該停(1)比沒資料(2)優先", []string{"--week-max", "10"}, 1, ""},
	} {
		code, out := hg(t, c, append([]string{"quota", "check"}, tc.args...)...)
		if code != tc.code || !strings.Contains(out, tc.want) {
			t.Errorf("%s：結束碼 %d（要 %d）\n%s", tc.name, code, tc.code, out)
		}
	}
	if code, _ := hg(t, c, "quota", "a", "b"); code != 2 {
		t.Fatalf("兩個帳號是用法錯誤：%d", code)
	}
}
