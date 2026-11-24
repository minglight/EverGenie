package charter

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

// fakeAPI 記下收到的請求；指令只認 sdk 的 client，所以用 sdk.Serve 開一個真的 unix socket。
type fakeAPI struct {
	sdk.RulesAPI
	mu   sync.Mutex
	last map[string]any
}

func (f *fakeAPI) record(name string, req any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last[name] = req
}

func (f *fakeAPI) got(name string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[name]
}

func (f *fakeAPI) Propose(_ context.Context, r *sdk.RulesProposeRequest) (*sdk.RulesProposeResponse, error) {
	f.record("propose", r)
	return &sdk.RulesProposeResponse{Proposal: &sdk.RulesProposal{ID: "RP-1", Status: sdk.RulesProposalStatusPending}}, nil
}

func (f *fakeAPI) Version(_ context.Context, r *sdk.RulesVersionRequest) (*sdk.RulesVersionResponse, error) {
	f.record("version", r)
	return &sdk.RulesVersionResponse{Version: &sdk.RulesVersion{ID: r.ID, Doc: "law", Text: "那一版的全文"}}, nil
}

func (f *fakeAPI) Proposals(_ context.Context, r *sdk.RulesProposalsRequest) (*sdk.RulesProposalsResponse, error) {
	f.record("proposals", r)
	return &sdk.RulesProposalsResponse{}, nil
}

func serveFake(t *testing.T) (*sdk.Client, *fakeAPI) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAPI{last: map[string]any{}}
	sock := filepath.Join(dir, "api.sock")
	sctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(sctx, sock, map[string]any{"rules": f}) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		_ = os.RemoveAll(dir)
	})
	for i := 0; i < 500; i++ {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			return sdk.Dial(sock), f
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket never accepted a connection")
	return nil, nil
}

func hg(t *testing.T, c *sdk.Client, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(stdin), Out: &out, Err: &errb}, Commands(), "用法\n")
	return code, out.String(), errb.String()
}

// propose：--file - 從標準輸入讀內容、操作者是 $EVERGENIE_AGENT；--text 與 --file 只能給一個；不認得的旗標是用法錯誤。
func TestProposeReadsStdinAndNeedsExactlyOneSource(t *testing.T) {
	t.Setenv("EVERGENIE_AGENT", "code")
	c, f := serveFake(t)
	if code, _, errb := hg(t, c, "新的第 2 條\n", "rules", "propose", "law", "--why", "說不清楚", "--section", "第 2 條", "--file", "-"); code != 0 {
		t.Fatalf("propose → %d: %s", code, errb)
	}
	r := f.got("propose").(*sdk.RulesProposeRequest)
	if r.Doc != "law" || r.Section != "第 2 條" || r.Text != "新的第 2 條\n" || r.Why != "說不清楚" || r.By == nil || r.By.Name != "code" {
		t.Fatalf("propose: %+v", r)
	}
	for name, args := range map[string][]string{
		"兩個都給": {"rules", "propose", "law", "--why", "y", "--text", "a", "--file", "-"},
		"都沒給":  {"rules", "propose", "law", "--why", "y"},
		"不認得":  {"rules", "propose", "law", "--why", "y", "--text", "a", "--force"},
		"沒有文件": {"rules", "propose", "--why", "y", "--text", "a"},
	} {
		if code, _, _ := hg(t, c, "", args...); code == 0 {
			t.Errorf("%s 要失敗", name)
		}
	}
}

// show --version 印那一版的全文；proposals --status 帶進請求。
func TestShowVersionAndProposalsStatus(t *testing.T) {
	c, f := serveFake(t)
	code, out, errb := hg(t, c, "", "rules", "show", "law", "--version", "RV-3")
	if code != 0 || out != "那一版的全文\n" {
		t.Fatalf("show --version → %d %q %s", code, out, errb)
	}
	if r := f.got("version").(*sdk.RulesVersionRequest); r.ID != "RV-3" {
		t.Fatalf("version: %+v", r)
	}
	if code, _, errb := hg(t, c, "", "rules", "proposals", "law", "--status", "pending"); code != 0 {
		t.Fatalf("proposals → %d %s", code, errb)
	}
	if r := f.got("proposals").(*sdk.RulesProposalsRequest); r.Doc != "law" || len(r.Statuses) != 1 || r.Statuses[0] != sdk.RulesProposalStatusPending {
		t.Fatalf("proposals: %+v", r)
	}
	if code, out, _ := hg(t, c, "", "rules", "history", "--help"); code != 0 || !strings.Contains(out, "propose") {
		t.Fatalf("--help 印用法：%d %q", code, out)
	}
}
