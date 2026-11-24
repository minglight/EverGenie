package reminder

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// fakeAPI 記下收到的請求，回固定答案；指令只認 sdk 的 client，所以用 sdk.Serve 開一個真的 unix socket。
type fakeAPI struct {
	sdk.RemindersAPI
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
func (f *fakeAPI) Create(_ context.Context, r *sdk.RemindersCreateRequest) (*sdk.RemindersCreateResponse, error) {
	f.record("create", r)
	return &sdk.RemindersCreateResponse{Reminder: &sdk.Reminder{ID: "R-9", Content: r.Content}}, nil
}
func (f *fakeAPI) Update(_ context.Context, r *sdk.RemindersUpdateRequest) (*sdk.RemindersUpdateResponse, error) {
	f.record("update", r)
	return &sdk.RemindersUpdateResponse{Reminder: &sdk.Reminder{ID: r.ID}}, nil
}
func (f *fakeAPI) Snooze(_ context.Context, r *sdk.RemindersSnoozeRequest) (*sdk.RemindersSnoozeResponse, error) {
	f.record("snooze", r)
	return &sdk.RemindersSnoozeResponse{Reminder: &sdk.Reminder{ID: r.ID}}, nil
}
func (f *fakeAPI) Done(_ context.Context, r *sdk.RemindersDoneRequest) (*sdk.RemindersDoneResponse, error) {
	f.record("done", r)
	return &sdk.RemindersDoneResponse{Reminder: &sdk.Reminder{ID: r.ID, Status: sdk.ReminderStatusDone}}, nil
}
func (f *fakeAPI) List(_ context.Context, r *sdk.RemindersListRequest) (*sdk.RemindersListResponse, error) {
	f.record("list", r)
	if r.Page == nil {
		return &sdk.RemindersListResponse{Reminders: []sdk.Reminder{{ID: "R-1"}, {ID: "R-2"}}, Page: &sdk.PageResponse{NextCursor: "p2"}}, nil
	}
	return &sdk.RemindersListResponse{Reminders: []sdk.Reminder{{ID: "R-3"}}}, nil
}

func serveFake(t *testing.T) (*sdk.Client, *fakeAPI) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAPI{last: map[string]any{}}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, map[string]any{"reminders": f}) }()
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

func hg(t *testing.T, c *sdk.Client, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{Out: &out, Err: &errb}, Commands(), "用法\n")
	return code, out.String(), errb.String()
}

func mustHG(t *testing.T, c *sdk.Client, args ...string) string {
	t.Helper()
	code, out, errb := hg(t, c, args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errb)
	}
	return out
}

func TestCommandsListsAllReminderCommands(t *testing.T) {
	var _ map[string]app.Command = Commands()
	want := []string{"reminders add", "reminders set", "reminders snooze", "reminders done", "reminders doing", "reminders cancel", "reminders get", "reminders list"}
	got := Commands()
	if len(got) != len(want) {
		t.Fatalf("指令數 %d", len(got))
	}
	for _, name := range want {
		if got[name].Run == nil {
			t.Errorf("缺 %s", name)
		}
	}
}

// 旗標名就是請求欄位名；操作者來自 $EVERGENIE_AGENT。
func TestFlagsFillRequestFields(t *testing.T) {
	t.Setenv("EVERGENIE_AGENT", "code")
	c, f := serveFake(t)

	mustHG(t, c, "reminders", "add", "--content", "繳費", "--follow", "remind", "--owner", "life")
	if r := f.got("create").(*sdk.RemindersCreateRequest); r.Content != "繳費" || r.Follow != sdk.FollowRemind || r.Owner != "life" || r.By.Name != "code" {
		t.Fatalf("create: %+v", r)
	}
	// 描述與附件（RMD-23）：開的時候 --body／--attachments，之後 set --body／--add_attachments／--remove_attachments。
	mustHG(t, c, "reminders", "add", "--content", "看圖", "--body", "背景", "--attachments", "/share/a.png", "--attachments", "/share/b.pdf")
	if r := f.got("create").(*sdk.RemindersCreateRequest); r.Body != "背景" || !reflect.DeepEqual(r.Attachments, []string{"/share/a.png", "/share/b.pdf"}) {
		t.Fatalf("create 附件: %+v", r)
	}
	mustHG(t, c, "reminders", "set", "R-1", "--body", "新的描述", "--add_attachments", "/share/c.png", "--remove_attachments", "/share/a.png")
	if r := f.got("update").(*sdk.RemindersUpdateRequest); r.Body == nil || *r.Body != "新的描述" || !reflect.DeepEqual(r.AddAttachments, []string{"/share/c.png"}) || !reflect.DeepEqual(r.RemoveAttachments, []string{"/share/a.png"}) {
		t.Fatalf("set 附件: %+v", r)
	}
	mustHG(t, c, "reminders", "snooze", "R-1", "--until", "3h")
	if r := f.got("snooze").(*sdk.RemindersSnoozeRequest); r.ID != "R-1" || r.Until != "3h" || r.By.Name != "code" {
		t.Fatalf("snooze: %+v", r)
	}
	mustHG(t, c, "reminders", "done", "R-1")
	if r := f.got("done").(*sdk.RemindersDoneRequest); r.ID != "R-1" || r.By.Name != "code" {
		t.Fatalf("done: %+v", r)
	}
	mustHG(t, c, "reminders", "list", "--query", "today", "--statuses", "todo", "--statuses", "doing", "--include_archived")
	if r := f.got("list").(*sdk.RemindersListRequest); r.Query != sdk.ReminderQueryToday || !reflect.DeepEqual(r.Statuses, []sdk.ReminderStatus{"todo", "doing"}) || !r.IncludeArchived {
		t.Fatalf("list: %+v", r)
	}
}

// list 一行一筆，有下一頁就接著取。
func TestListPrintsOneLinePerReminderAcrossPages(t *testing.T) {
	c, f := serveFake(t)
	out := mustHG(t, c, "reminders", "list")
	if strings.Count(out, "\n") != 3 || !strings.Contains(out, `"id":"R-3"`) {
		t.Fatalf("%q", out)
	}
	if r := f.got("list").(*sdk.RemindersListRequest); r.Page == nil || r.Page.Cursor != "p2" {
		t.Fatalf("第二頁要帶游標：%+v", r.Page)
	}
}

func TestTimeFlagsParse(t *testing.T) {
	c, f := serveFake(t)
	mustHG(t, c, "reminders", "add", "--content", "繳費", "--at", "2026-10-25T09:30")
	if r := f.got("create").(*sdk.RemindersCreateRequest); !r.At.Equal(time.Date(2026, 10, 25, 9, 30, 0, 0, time.Local)) {
		t.Fatalf("--at 本機時間：%v", r.At)
	}
	if code, _, errb := hg(t, c, "reminders", "add", "--content", "x", "--at", "明天"); code != 2 {
		t.Fatalf("壞時間要是用法錯誤，得到 %d: %s", code, errb)
	}
	if code, _, _ := hg(t, c, "reminders", "list", "--include_archived=maybe"); code != 2 {
		t.Fatalf("布林亂填要是用法錯誤，得到 %d", code)
	}
}

func TestIDIsRequiredForSingleItemCommands(t *testing.T) {
	c, _ := serveFake(t)
	for _, args := range [][]string{{"reminders", "done"}, {"reminders", "get", "R-1", "R-2"}, {"reminders", "add", "stray"}} {
		if code, out, errb := hg(t, c, args...); code != 2 || out != "" || errb == "" {
			t.Errorf("%v → %d %q %q", args, code, out, errb)
		}
	}
}
