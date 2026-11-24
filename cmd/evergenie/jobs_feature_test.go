package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 從 registry 起暫存家與 fake 引擎，驗 jobs 業務模組的接線：指令、help、排程服務（next_run 來自共用的計時迴圈）、夜班清單。
func TestJobsFeatureOnFakeEngineHome(t *testing.T) {
	dir := doctorHome(t)
	_, sock := startDoctorAPI(t, dir)
	if help := hgOK(t, sock, "help"); !strings.Contains(help, "jobs list|add|set|rm|run") || !strings.Contains(help, "night list|add|done|block|rm") {
		t.Fatalf("help 缺指令: %s", help)
	}
	hgOK(t, sock, "jobs", "add", "code/ai-topics", "--template", "ai-topics")
	var j sdk.Job
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.SplitN(hgOK(t, sock, "jobs", "list", "--json"), "\n", 2)[0])), &j); err != nil || j.ID != "code/ai-topics" {
		t.Fatalf("jobs list --json: %+v %v", j, err)
	}
	// 新增之後 vault.changed 讓 jobs 重新載入計時迴圈；等 next_run 出現代表 jobs 與 timer 接上了。
	waitFor(t, "next_run", func() bool {
		var got sdk.Job
		line := strings.TrimSpace(strings.SplitN(hgOK(t, sock, "jobs", "list", "--json"), "\n", 2)[0])
		return json.Unmarshal([]byte(line), &got) == nil && got.NextRun != nil
	})
	hgOK(t, sock, "night", "add", "整理 jobs 搬移驗收")
	if out := hgOK(t, sock, "night", "list"); !strings.Contains(out, "整理 jobs 搬移驗收") {
		t.Fatalf("night list: %s", out)
	}
	if code, _, errOut := hg(t, sock, "", "jobs", "add", "code/bad", "--cron", "every day", "--file", "/dev/null"); code == 0 || errOut == "" {
		t.Fatalf("壞設定要被擋：%d %q", code, errOut)
	}
}

// waitFor 每 50 毫秒查一次條件，最多等 5 秒。
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等不到 %s", what)
}
