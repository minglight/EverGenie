package job

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// listOne 列出排程、回 id 那一筆。
func listOne(t *testing.T, m *rig, id string) sdk.Job {
	t.Helper()
	jobs, err := m.ListJobs(context.Background(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if j.ID == id {
			return j
		}
	}
	t.Fatalf("清單裡沒有 %s：%+v", id, jobs)
	return sdk.Job{}
}

func envelopeState(m *rig, e *sdk.Envelope, state sdk.Lifecycle) {
	m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "bus.envelope.state", Payload: map[string]any{"id": e.ID, "kind": string(sdk.EnvelopeKindRoutine), "state": string(state)}})
}

// 排程到期送給秘書：清單帶最近一次的時間、「還在跑」、今天次數；封包回覆了變成 ok，再跑一次次數加一。
func TestListJobsShowsLastRunAndRunsToday(t *testing.T) {
	m, c, b, _ := setup(t, job("* * * * *", "skip"))
	if j := listOne(t, m, "code/ping"); j.LastRun != nil || j.LastResult != "" || j.RunsToday != 0 {
		t.Fatalf("沒跑過就不帶最近一次：%+v", j)
	}
	tick(m, c, time.Minute)
	fired := c.Now()
	j := listOne(t, m, "code/ping")
	if j.LastRun == nil || !j.LastRun.Equal(fired) || j.LastResult != sdk.JobResultRunning || j.RunsToday != 1 {
		t.Fatalf("送出後是還在跑、今天一次：%+v", j)
	}
	envelopeState(m, b.envelopes()[0], sdk.LifecycleReplied)
	if j := listOne(t, m, "code/ping"); j.LastResult != sdk.JobResultOk || j.LastReason != "" {
		t.Fatalf("回覆了是 ok：%+v", j)
	}
	tick(m, c, time.Minute)
	if j := listOne(t, m, "code/ping"); j.RunsToday != 2 || j.LastResult != sdk.JobResultRunning || !j.LastRun.Equal(c.Now()) {
		t.Fatalf("第二次：%+v", j)
	}
}

// 封包沒做完：沒送進對話（failed）寫 session 記的原因；做到一半停了（interrupted）前面加「做到一半停了」。
func TestFailedJobRunCarriesReason(t *testing.T) {
	cases := []struct {
		state  sdk.Lifecycle
		reason string
		want   string
	}{
		{sdk.LifecycleFailed, "這台沒有這個對話", "這台沒有這個對話"},
		{sdk.LifecycleFailed, "", "沒送進秘書的對話"},
		{sdk.LifecycleInterrupted, "額度用完了", "做到一半停了（額度用完了）"},
		{sdk.LifecycleInterrupted, "", "做到一半停了"},
	}
	for _, tc := range cases {
		t.Run(string(tc.state)+tc.reason, func(t *testing.T) {
			m, c, b, _ := setup(t, job("* * * * *", "skip"))
			tick(m, c, time.Minute)
			e := b.envelopes()[0]
			e.Reason = tc.reason
			envelopeState(m, e, tc.state)
			j := listOne(t, m, "code/ping")
			if j.LastResult != sdk.JobResultFailed || j.LastReason != tc.want || j.RunsToday != 1 {
				t.Fatalf("要是沒跑成「%s」：%+v", tc.want, j)
			}
		})
	}
}

// 前一次的封包很晚才結束，不能蓋掉之後那一次的狀態。
func TestOlderEnvelopeOutcomeDoesNotOverwriteLatestRun(t *testing.T) {
	m, c, b, _ := setup(t, job("* * * * *", "skip"))
	tick(m, c, time.Minute)
	tick(m, c, time.Minute)
	envelopeState(m, b.envelopes()[0], sdk.LifecycleFailed)
	if j := listOne(t, m, "code/ping"); j.LastResult != sdk.JobResultRunning || !j.LastRun.Equal(c.Now()) {
		t.Fatalf("舊的那次不蓋掉最近一次：%+v", j)
	}
}

// 錯過時間不補跑、過了每日次數：記成略過並附原因，不算今天的次數。
func TestSkippedJobRunsAreRecordedNotCounted(t *testing.T) {
	m, c, _, _ := setup(t, job("* * * * *", "skip"))
	tick(m, c, 3*time.Hour)
	j := listOne(t, m, "code/ping")
	if j.LastResult != sdk.JobResultSkipped || !strings.Contains(j.LastReason, "不補跑") || j.RunsToday != 0 || j.LastRun == nil {
		t.Fatalf("錯過不補要記略過：%+v", j)
	}

	m, c, _, v := setup(t, job("* * * * *", "skip"))
	v.budget = 1
	tick(m, c, time.Minute)
	tick(m, c, time.Minute)
	j = listOne(t, m, "code/ping")
	if j.LastResult != sdk.JobResultSkipped || !strings.Contains(j.LastReason, "次數用完") || j.RunsToday != 1 {
		t.Fatalf("次數用完要記略過、只算真的跑的那次：%+v", j)
	}
}

// 補跑的那次（catchup: once）算真的跑了，不記略過。
func TestCaughtUpRunIsNotSkipped(t *testing.T) {
	m, c, _, _ := setup(t, job("* * * * *", "once"))
	tick(m, c, 3*time.Hour)
	if j := listOne(t, m, "code/ping"); j.LastResult != sdk.JobResultRunning || j.RunsToday != 1 {
		t.Fatalf("補跑：%+v", j)
	}
}

// 系統級排程（沒有秘書）只知道觸發了：記 fired；排程到期時讀不到內容送不出去：記沒跑成。
func TestSystemJobFiredAndUndeliverableJobFailed(t *testing.T) {
	r := job("* * * * *", "skip")
	r.Fields["agent"] = ""
	m, c, _, _ := setup(t, r)
	tick(m, c, time.Minute)
	if j := listOne(t, m, "code/ping"); j.LastResult != sdk.JobResultFired || j.RunsToday != 1 {
		t.Fatalf("系統級：%+v", j)
	}

	m, c, _, v := setup(t, job("* * * * *", "skip"))
	gone := v.records
	v.records = nil // 排程檔不見了，計時迴圈還沒重載
	tick(m, c, time.Minute)
	v.records = gone
	if j := listOne(t, m, "code/ping"); j.LastResult != sdk.JobResultFailed || j.LastReason != "讀不到排程內容" {
		t.Fatalf("送不出去：%+v", j)
	}
}

// 手動跑一次也記（算今天的次數）；回覆了一樣變 ok。
func TestRunJobIsRecorded(t *testing.T) {
	m, _ := setupJobStore(t)
	ctx := context.Background()
	if _, _, err := m.RunJob(ctx, "code/ping", "owner(board)"); err != nil {
		t.Fatal(err)
	}
	j := listOne(t, m, "code/ping")
	if j.LastResult != sdk.JobResultRunning || j.RunsToday != 1 {
		t.Fatalf("手動跑：%+v", j)
	}
	b := m.d.Bus.(*fakeBus)
	envelopeState(m, b.envelopes()[0], sdk.LifecycleReplied)
	if j := listOne(t, m, "code/ping"); j.LastResult != sdk.JobResultOk {
		t.Fatalf("手動跑回覆了：%+v", j)
	}
}

// serve 重開（新的模組讀同一個家）：最近一次與今天的次數還在；過了午夜（家的時區）今天的次數歸零，最近一次留著。
func TestLastRunSurvivesRestartAndRunsTodayResetsAtMidnight(t *testing.T) {
	h := homefixture.New(t)
	calls := []string{}
	records := []vault.Record{job("* * * * *", "skip")}
	m := newRig(t, h, &fakeBus{calls: &calls}, &fakeVault{records: records, calls: &calls})
	tick(m, h.Clock, time.Minute)
	fired := h.Clock.Now()
	if err := m.Module.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	again := newRig(t, h, &fakeBus{calls: &calls}, &fakeVault{records: records, calls: &calls})
	j := listOne(t, again, "code/ping")
	if j.LastRun == nil || !j.LastRun.Equal(fired) || j.LastResult != sdk.JobResultRunning || j.RunsToday != 1 {
		t.Fatalf("重開後要讀回來：%+v", j)
	}
	h.Clock.Advance(12 * time.Hour) // 12:01 → 隔天 00:01（Asia/Taipei）
	j = listOne(t, again, "code/ping")
	if j.RunsToday != 0 || j.LastRun == nil {
		t.Fatalf("隔天今天的次數歸零、最近一次留著：%+v", j)
	}
}

// 略過的原因照排程設定寫：時段型醒來時過了時段；catchup: once 睡超過 6 小時。
func TestSkippedReasonFollowsJobKind(t *testing.T) {
	m, c, _ := setupWith(t, func(string) {}, windowJobRecord([]any{"12:15-13:15"}, nil, 1))
	c.Advance(2 * time.Hour)
	m.Tick(context.Background(), c.Now())
	if j := listOne(t, m, "life/chat"); j.LastResult != sdk.JobResultSkipped || !strings.Contains(j.LastReason, "過了時段") {
		t.Fatalf("時段型：%+v", j)
	}

	m2, c2, _, _ := setup(t, job("* * * * *", "once"))
	tick(m2, c2, 7*time.Hour)
	if j := listOne(t, m2, "code/ping"); j.LastResult != sdk.JobResultSkipped || !strings.Contains(j.LastReason, "超過 6 小時") {
		t.Fatalf("補跑太久：%+v", j)
	}
}

// 同一次觸發早就送過、封包已經結束（冪等鍵相同，例如時間到的那一秒又按了「現在跑一次」）：直接記那個結局，不會停在「還在跑」。
func TestAlreadyFinishedEnvelopeIsRecorded(t *testing.T) {
	m, c, b, _ := setup(t, job("* * * * *", "skip"))
	tick(m, c, time.Minute)
	b.envelopes()[0].Lifecycle = sdk.LifecycleReplied // 回覆了，但事件還沒到
	if _, _, err := m.RunJob(context.Background(), "code/ping", "owner(board)"); err != nil {
		t.Fatal(err)
	}
	if j := listOne(t, m, "code/ping"); j.LastResult != sdk.JobResultOk {
		t.Fatalf("封包已經回覆就記 ok：%+v", j)
	}
}

// last-runs.json 壞了：從空的開始，排程照常跑、照常記。
func TestBrokenRunsFileStartsEmpty(t *testing.T) {
	m, c, _ := setupWith(t, func(dir string) {
		if err := os.MkdirAll(filepath.Join(dir, "runtime", "jobs"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "runtime", "jobs", "last-runs.json"), []byte("{壞掉"), 0o600); err != nil {
			t.Fatal(err)
		}
	}, job("* * * * *", "skip"))
	if j := listOne(t, m, "code/ping"); j.LastRun != nil {
		t.Fatalf("壞檔不帶最近一次：%+v", j)
	}
	tick(m, c, time.Minute)
	if j := listOne(t, m, "code/ping"); j.LastResult != sdk.JobResultRunning {
		t.Fatalf("壞檔之後照常記：%+v", j)
	}
}

// 排程刪掉（重載後不在清單上）就丟掉它的紀錄，檔案不會一直長。
func TestRemovedJobRunIsPruned(t *testing.T) {
	m, c, _, v := setup(t, job("* * * * *", "skip"))
	tick(m, c, time.Minute)
	v.records = nil
	m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "vault.changed", Payload: map[string]any{"kind": "job"}})
	m.runsMu.Lock()
	n := len(m.runs)
	m.runsMu.Unlock()
	if n != 0 {
		t.Fatalf("刪掉的排程要清掉紀錄：%d", n)
	}
}
