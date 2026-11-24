package backupsweep

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// systemAPI 只接 backup_sweep，其他 system 方法沒有（叫到會當掉，這裡不叫）。
type systemAPI struct {
	sdk.SystemAPI
	*service
}

func (s systemAPI) BackupSweep(ctx context.Context, r *sdk.SystemBackupSweepRequest) (*sdk.SystemBackupSweepResponse, error) {
	return s.service.BackupSweep(ctx, r)
}

// serveSweep 用 sdk.Serve 在 /tmp 開一個真的 unix socket（指令只認 sdk 的 client）。
func serveSweep(t *testing.T, m *Module) *sdk.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, map[string]any{"system": systemAPI{service: &service{m: m}}}) }()
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
			return sdk.Dial(sock)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket 一直連不上")
	return nil
}

func runCLI(t *testing.T, c *sdk.Client, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(""), Out: &out, Err: &errOut}, Commands(), "用法：evergenie backup …")
	return code, out.String(), errOut.String()
}

// TestBackupSweepCommand：`backup sweep` 還沒掃過時說怎麼掃；`--run` 掃一次印出完整報告（已到期、不合規、期限內、略過的例外）；
// 之後不帶 --run 印上一次的；--json 給機器讀。
func TestBackupSweepCommand(t *testing.T) {
	m, _, _, user := sweepHome(t, "", noRefs)
	c := serveSweep(t, m)
	if code, out, errOut := runCLI(t, c, "backup", "sweep"); code != 0 || !strings.Contains(out, "還沒掃過") {
		t.Fatalf("code=%d\n%s\n%s", code, out, errOut)
	}
	put(t, filepath.Join(user, "Workspace", "p", "a.bak-20260101-old"), testNow)
	put(t, filepath.Join(user, "Workspace", "p", "b.bak"), testNow)
	code, out, errOut := runCLI(t, c, "backup", "sweep", "--run")
	if code != 0 {
		t.Fatalf("code=%d %s", code, errOut)
	}
	for _, want := range []string{"# 備份掃描報告", "### 可刪（1）", "a.bak-20260101-old", "查過：沒有開機服務、執行中的程式、終端機視窗用到它", "## 名字不合規", "b.bak-", "## 期限內", "## 略過的例外"} {
		if !strings.Contains(out, want) {
			t.Errorf("報告少了「%s」：\n%s", want, out)
		}
	}
	if code, again, _ := runCLI(t, c, "backup", "sweep"); code != 0 || !strings.Contains(again, "a.bak-20260101-old") {
		t.Fatalf("不帶 --run 印上一次的：%d\n%s", code, again)
	}
	if code, js, _ := runCLI(t, c, "backup", "sweep", "--json"); code != 0 || !strings.Contains(js, `"misnamed"`) {
		t.Fatalf("--json：%d\n%s", code, js)
	}
	if code, _, _ := runCLI(t, c, "backup", "sweep", "extra"); code == 0 {
		t.Fatal("多的參數要說用法")
	}
}
