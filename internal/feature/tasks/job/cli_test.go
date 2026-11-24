package job

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// fakeScheduler 是有狀態的假排程服務：只記下 CLI 送來的請求、改記憶體裡的排程，不做排程模組的驗證。
type fakeScheduler struct {
	mu   sync.Mutex
	jobs map[string]*sdk.Job
	runs []string
	by   []string
}

func (s *fakeScheduler) ListJobs(_ context.Context, r *sdk.SchedulerListJobsRequest) (*sdk.SchedulerListJobsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []sdk.Job{}
	for _, j := range s.jobs {
		if (j.Archived && !r.IncludeArchived) || (r.Agent != "" && j.Agent != r.Agent) {
			continue
		}
		out = append(out, *j)
	}
	return &sdk.SchedulerListJobsResponse{Jobs: out}, nil
}
func (s *fakeScheduler) SaveJob(_ context.Context, r *sdk.SchedulerSaveJobRequest) (*sdk.SchedulerSaveJobResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[r.Job.ID]; ok {
		return nil, &sdk.Error{Code: sdk.ErrorCodeConflict, Message: "已經有這個排程"}
	}
	j := *r.Job
	if agent, _, ok := strings.Cut(j.ID, "/"); ok {
		j.Agent = agent
	}
	s.jobs[j.ID] = &j
	s.by = append(s.by, r.By.Name)
	return &sdk.SchedulerSaveJobResponse{Job: &j}, nil
}
func (s *fakeScheduler) SetJob(_ context.Context, r *sdk.SchedulerSetJobRequest) (*sdk.SchedulerSetJobResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[r.ID]
	if !ok {
		return nil, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "找不到這個排程"}
	}
	if r.Enabled != nil {
		j.Enabled = *r.Enabled
	}
	if len(r.Cron) > 0 {
		j.Cron = r.Cron
	}

	if r.Name != nil {
		j.Name = *r.Name
	}
	if r.Body != nil {
		j.Body = *r.Body
	}
	s.by = append(s.by, r.By.Name)
	c := *j
	return &sdk.SchedulerSetJobResponse{Job: &c}, nil
}
func (s *fakeScheduler) RemoveJob(_ context.Context, r *sdk.SchedulerRemoveJobRequest) (*sdk.SchedulerRemoveJobResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[r.ID]
	j.Enabled = false
	archived := j.Source == "user"
	j.Archived = archived
	s.by = append(s.by, r.By.Name)
	c := *j
	return &sdk.SchedulerRemoveJobResponse{Job: &c, Archived: archived}, nil
}
func (s *fakeScheduler) RunJob(_ context.Context, r *sdk.SchedulerRunJobRequest) (*sdk.SchedulerRunJobResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[r.ID]
	if !ok {
		return nil, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "找不到這個排程"}
	}
	s.runs = append(s.runs, r.ID)
	s.by = append(s.by, r.By.Name)
	c := *j
	return &sdk.SchedulerRunJobResponse{Job: &c, At: time.Now()}, nil
}

func serveJobs(t *testing.T) (*sdk.Client, *fakeScheduler) {
	t.Helper()
	s := &fakeScheduler{jobs: map[string]*sdk.Job{
		"code/nightly": {ID: "code/nightly", Agent: "code", Name: "重整記憶", Cron: []string{"0 3 * * *"}, Enabled: true, Source: "migrate", Action: "rotate", Body: "整理"},
	}}
	return serve(t, map[string]any{"scheduler": s}), s
}

// Guards: REQ-18, REQ-21, REQ-22, REQ-159
func TestJobsCLIRoundTrip(t *testing.T) {
	c, s := serveJobs(t)
	file := filepath.Join(t.TempDir(), "standup.md")
	if err := os.WriteFile(file, []byte("提醒我寫站會筆記"), 0o600); err != nil {
		t.Fatal(err)
	}

	// add：--cron 可以重複；指令內文來自 --file。
	out := mustRun(t, c, "jobs", "add", "work/standup", "--cron", "0 9 * * 1-5", "--cron=30 13 * * 1-5", "--file", file, "--name", "站會")
	if !strings.Contains(out, "已新增排程 work/standup「站會」") {
		t.Fatalf("add：%s", out)
	}
	r := s.jobs["work/standup"]
	if r.Agent != "work" || r.Source != "user" || !r.Enabled || r.Body != "提醒我寫站會筆記" ||
		!reflect.DeepEqual(r.Cron, []string{"0 9 * * 1-5", "30 13 * * 1-5"}) {
		t.Fatalf("add 送給排程服務的內容：%+v", r)
	}

	// 範本：chat 是時段型，內文帶 slot；ai-topics 是 cron 範本（舊 cron 的星期 7 改成 0）。
	mustRun(t, c, "jobs", "add", "life/chat", "--template", "chat")
	chat := s.jobs["life/chat"]
	if chat.Cron != nil || !reflect.DeepEqual(chat.Windows, []string{"12:15-13:15", "15:30-17:00"}) || chat.PerDay != 1 || chat.IdleMin != 15 ||
		chat.Name != "主動聊天" || !strings.Contains(chat.Body, "{{slot chat-avoid}}") || !strings.Contains(chat.Body, "{{slot chat-priorities}}") {
		t.Fatalf("chat 範本：%+v", chat)
	}
	mustRun(t, c, "jobs", "add", "code/ai-topics", "--template", "ai-topics")
	if got := s.jobs["code/ai-topics"]; !reflect.DeepEqual(got.Cron, []string{"0 15 * * 1,3,5,0"}) || !strings.Contains(got.Body, "{{slot ai-topics-owner}}") {
		t.Fatalf("ai-topics 範本：%+v", got)
	}
	mustRun(t, c, "jobs", "add", "code/reflect", "--template", "reflect", "--disabled")
	if got := s.jobs["code/reflect"]; !reflect.DeepEqual(got.Cron, []string{"40 20 * * *"}) || !got.Silent || got.Enabled {
		t.Fatalf("reflect 範本：%+v", got)
	}
	for _, bad := range [][]string{
		{"jobs", "add", "work/x", "--file", file},                    // 沒有時間
		{"jobs", "add", "work/x", "--cron", "0 9 * * *"},             // 沒有內文
		{"jobs", "add", "work/x", "--template", "nope"},              // 沒有這個範本
		{"jobs", "add", "/x", "--cron", "0 9 * * *", "--file", file}, // id 格式
		{"jobs", "add", "work/x", "--cron", "0 9 * * *", "--file", file, "--sites", ""},
		{"jobs", "set", "work/standup"}, // 沒改任何東西
		{"jobs", "set", "work/standup", "--enabled", "maybe"},
		{"jobs", "rm"},
		{"jobs", "run", "a", "b"},
	} {
		if code, _, _ := run(t, c, "", bad...); code != 2 {
			t.Fatalf("%v 要回用法錯誤（2），得到 %d", bad, code)
		}
	}

	// list：一行一筆、有 --agent 篩選、時段型列出時段、限定電腦的會標出來。
	out = mustRun(t, c, "jobs", "list")
	for _, want := range []string{"code/nightly", "life/chat", "時段 12:15-13:15、15:30-17:00", "work/standup"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list 缺 %q：\n%s", want, out)
		}
	}
	if out = mustRun(t, c, "jobs", "list", "--agent", "life"); strings.Contains(out, "work/standup") || !strings.Contains(out, "life/chat") {
		t.Fatalf("list --agent：\n%s", out)
	}

	// set：改 enabled、cron。
	mustRun(t, c, "jobs", "set", "work/standup", "--enabled", "false", "--cron", "0 10 * * 1-5")
	r = s.jobs["work/standup"]
	if r.Enabled || !reflect.DeepEqual(r.Cron, []string{"0 10 * * 1-5"}) {
		t.Fatalf("set：%+v", r)
	}

	// run：交給排程服務。
	if out = mustRun(t, c, "jobs", "run", "code/nightly"); !strings.Contains(out, "已排進 code 的派送佇列") || !reflect.DeepEqual(s.runs, []string{"code/nightly"}) {
		t.Fatalf("run：%s %v", out, s.runs)
	}

	// rm：自己加的封存；遷移來的只關掉。
	if out = mustRun(t, c, "jobs", "rm", "work/standup"); !s.jobs["work/standup"].Archived || !strings.Contains(out, "已移除") {
		t.Fatalf("自己加的排程要封存：%s", out)
	}
	out = mustRun(t, c, "jobs", "rm", "code/nightly")
	if n := s.jobs["code/nightly"]; n.Archived || n.Enabled || !strings.Contains(out, "已關掉") {
		t.Fatalf("遷移來的排程只關掉：%+v %s", n, out)
	}
	if out = mustRun(t, c, "jobs", "list"); strings.Contains(out, "work/standup") {
		t.Fatalf("封存的不列：\n%s", out)
	}
	if out = mustRun(t, c, "jobs", "list", "--all"); !strings.Contains(out, "已移除") {
		t.Fatalf("--all 要列出已移除的：\n%s", out)
	}
	// 服務回的錯誤照樣顯示，退出碼 1。
	if code, _, errOut := run(t, c, "", "jobs", "add", "code/nightly", "--cron", "0 9 * * *", "--file", file); code != 1 || !strings.Contains(errOut, "已經有這個排程") {
		t.Fatalf("重複新增：%d %s", code, errOut)
	}
	for _, by := range s.by {
		if by != "owner(cli)" {
			t.Fatalf("寫入都要帶 by：%v", s.by)
		}
	}
}

// Guards: REQ-18, REQ-159
func TestJobsCLISetNameBody(t *testing.T) {
	c, s := serveJobs(t)
	file := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// --name、--body 只改它們自己；cron 不動。
	mustRun(t, c, "jobs", "set", "code/nightly", "--name", "新名稱")
	n := s.jobs["code/nightly"]
	if n.Name != "新名稱" || n.Body != "整理" || !reflect.DeepEqual(n.Cron, []string{"0 3 * * *"}) {
		t.Fatalf("--name：%+v", n)
	}
	mustRun(t, c, "jobs", "set", "code/nightly", "--body", "新的內文")
	if n = s.jobs["code/nightly"]; n.Body != "新的內文" || n.Name != "新名稱" {
		t.Fatalf("--body：%+v", n)
	}

	// --body - 從標準輸入讀，換行原樣保留。
	if code, _, errOut := run(t, c, "第一行\n第二行\n", "jobs", "set", "code/nightly", "--body", "-"); code != 0 {
		t.Fatalf("--body - 失敗（%d）：%s", code, errOut)
	}
	if n = s.jobs["code/nightly"]; n.Body != "第一行\n第二行\n" {
		t.Fatalf("--body -：%q", n.Body)
	}

	// 只給 --name 或 --body 也算「改了一項」，不再被擋成用法錯誤。
	if code, _, _ := run(t, c, "", "jobs", "set", "code/nightly"); code != 2 {
		t.Fatalf("什麼都沒給要回用法錯誤：%d", code)
	}
}
