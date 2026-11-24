package job

import (
	"context"
	"errors"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// jobStore 是會記住排程的假 vault：Get／Put／Patch 對 `job` 種類照實寫進記憶體，其餘沿用 fakeVault。
type jobStore struct {
	*fakeVault
	jobs    map[string]vault.Record
	patches int
	puts    int
}

func (s *jobStore) Get(ctx context.Context, kind, id string) (vault.Record, error) {
	if kind != "job" {
		return s.fakeVault.Get(ctx, kind, id)
	}
	r, ok := s.jobs[id]
	if !ok {
		return vault.Record{}, platform.NotFound("找不到")
	}
	return r, nil
}
func (s *jobStore) Put(_ context.Context, r vault.Record, _ string, rev string) (vault.Record, error) {
	if old, ok := s.jobs[r.ID]; ok && old.Revision != rev {
		return vault.Record{}, platform.Conflict("資料已更新")
	}
	s.puts++
	r.Revision = r.ID + "-" + string(rune('a'+s.puts))
	s.jobs[r.ID] = r
	return r, nil
}
func (s *jobStore) Patch(_ context.Context, _, id string, set map[string]any, _ []string, _ string) (vault.Record, error) {
	s.patches++
	r := s.jobs[id]
	for k, v := range set {
		r.Fields[k] = v
	}
	s.jobs[id] = r
	return r, nil
}

func setupJobStore(t *testing.T) (*rig, *jobStore) {
	t.Helper()
	h := homefixture.New(t)
	calls := []string{}
	r := job("0 9 * * *", "skip")
	r.Revision = "rev-0"
	s := &jobStore{fakeVault: &fakeVault{calls: &calls, records: []vault.Record{r}}, jobs: map[string]vault.Record{r.ID: r}}
	m := newRig(t, h, &fakeBus{calls: &calls}, s)
	return m, s
}

func ptr[T any](v T) *T { return &v }

// Guards: REQ-18, REQ-159
func TestSetJobNameBody(t *testing.T) {
	ctx := context.Background()
	m, s := setupJobStore(t)

	// 只改名稱：內容、cron 都不動。
	j, err := m.SetJob(ctx, &sdk.SchedulerSetJobRequest{ID: "code/ping", Name: ptr("  新名字 ")}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if j.Name != "新名字" || j.Body != "測試用的指令內文" || len(j.Cron) != 1 || j.Cron[0] != "0 9 * * *" {
		t.Fatalf("只改名稱不該動其他欄位：%+v", j)
	}

	// 只改內容：名稱、cron 不動。
	j, err = m.SetJob(ctx, &sdk.SchedulerSetJobRequest{ID: "code/ping", Body: ptr("新的指令\n第二行")}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if j.Body != "新的指令\n第二行" || j.Name != "新名字" || j.Cron[0] != "0 9 * * *" {
		t.Fatalf("只改內容不該動其他欄位：%+v", j)
	}

	// 名稱與內容一起改，同時改 cron。
	j, err = m.SetJob(ctx, &sdk.SchedulerSetJobRequest{ID: "code/ping", Name: ptr("B"), Body: ptr("內容 B"), Cron: []string{"30 9 * * 1,3"}}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if j.Name != "B" || j.Body != "內容 B" || j.Cron[0] != "30 9 * * 1,3" {
		t.Fatalf("一次改三樣：%+v", j)
	}

	// 空名稱、空內容回 Invalid，指出欄位，也沒有寫進去。
	before := s.puts
	for _, tc := range []struct {
		req   sdk.SchedulerSetJobRequest
		field string
	}{
		{sdk.SchedulerSetJobRequest{ID: "code/ping", Name: ptr("   ")}, "name"},
		{sdk.SchedulerSetJobRequest{ID: "code/ping", Body: ptr("")}, "body"},
		{sdk.SchedulerSetJobRequest{ID: "code/ping"}, "id"},
	} {
		_, err := m.SetJob(ctx, &tc.req, "owner(web)")
		if platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Fatalf("%s 要回 invalid：%v", tc.field, err)
		}
		var pe *platform.Err
		if !errors.As(err, &pe) || pe.Field != tc.field {
			t.Fatalf("錯誤欄位要是 %q：%v", tc.field, err)
		}
	}
	if s.puts != before {
		t.Fatal("被擋下的請求不能寫進 vault")
	}
	if got := s.jobs["code/ping"]; got.Fields["name"] != "B" || got.Body != "內容 B" {
		t.Fatalf("被擋下後原資料要還在：%+v", got)
	}
}

// Guards: REQ-18
func TestListJobsNextRun(t *testing.T) {
	ctx := context.Background()
	m, _ := setupJobStore(t)
	jobs, err := m.ListJobs(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].NextRun == nil {
		t.Fatalf("開著的排程要帶下次執行時間：%+v", jobs)
	}
}

// 秘書封存、改名時，名下的排程經 jobs 的方法列出、搬家、封存（job 只有 jobs 寫）：照 id 前綴認、agent 欄位跟著改、封存不刪。
func TestAgentJobsListMoveArchive(t *testing.T) {
	h := homefixture.New(t)
	calls := []string{}
	m := newRig(t, h, &fakeBus{calls: &calls}, h.Vault)
	ctx := context.Background()
	mine := job("0 9 * * *", "skip")
	mine.ID = "code/morning"
	other := job("0 9 * * *", "skip")
	other.ID, other.Fields = "coder/x", map[string]any{"name": "別人", "cron": []string{"0 9 * * *"}, "agent": "coder", "enabled": true}
	h.WithRecords(mine, other)
	ids, err := m.AgentJobIDs(ctx, "code")
	if err != nil || len(ids) != 1 || ids[0] != "code/morning" {
		t.Fatalf("只列 code/ 底下的：%v %v", ids, err)
	}
	if err := m.MoveAgentJob(ctx, "code/morning", "dev/morning", "dev", "owner(cli)"); err != nil {
		t.Fatal(err)
	}
	moved, err := h.Vault.Get(ctx, "job", "dev/morning")
	if err != nil || moved.Fields["agent"] != "dev" || moved.Body != mine.Body {
		t.Fatalf("搬過去、agent 欄位跟著改、內文不動：%+v %v", moved, err)
	}
	if err := m.ArchiveAgentJob(ctx, "dev/morning", "owner(cli)"); err != nil {
		t.Fatal(err)
	}
	if got, err := h.Vault.Get(ctx, "job", "dev/morning"); err != nil || !got.Archived {
		t.Fatalf("封存而不是刪除：%+v %v", got, err)
	}
	if ids, _ := m.AgentJobIDs(ctx, "dev"); len(ids) != 0 {
		t.Fatalf("封存過的不再列：%v", ids)
	}
}
