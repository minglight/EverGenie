package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

const storeConfig = `[user]
name="測試"
timezone="Asia/Taipei"
[slots]
default="life"
[slots.life]
label="日常"
remind="08:30"
check="18:00"
[accounts]
default="main"
[accounts.main]
engine="test-engine"
[web]
port=8850
`

func openStore(t *testing.T) (*Module, string) {
	t.Helper()
	home := t.TempDir()
	if err := platform.AtomicWrite(filepath.Join(home, "home.toml"), []byte(storeConfig), 0600); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Clock: platform.NewFakeClock(time.Date(2026, 10, 23, 12, 0, 0, 0, loc))})
	for _, kind := range []string{"task", "goal", "reminder", "journal"} {
		m.RegisterSchema(Spec{Kind: kind, Fields: []Field{{Name: "goal", Type: Link, LinkKind: "goal"}}, ArchiveAt: func(Record, *time.Location) (time.Time, bool) { return time.Date(2026, 10, 1, 0, 0, 0, 0, loc), true }})
	}
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return m, home
}
func putTask(t *testing.T, m *Module, id string) Record {
	t.Helper()
	r, err := m.Put(context.Background(), Record{Kind: "task", ID: id, Fields: map[string]any{"title": "initial"}}, "code", "")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Guards: REQ-3
func TestWriteIsAtomic(t *testing.T) {
	m, home := openStore(t)
	r := putTask(t, m, "SEC-1")
	data, err := os.ReadFile(filepath.Join(home, "vault/tasks/SEC-1.md"))
	if err != nil {
		t.Fatal(err)
	}
	if revision(data) != r.Revision {
		t.Fatal("disk and index differ")
	}
	r.Body = "replacement"
	updated, err := m.Put(context.Background(), r, "code", r.Revision)
	if err != nil {
		t.Fatal(err)
	}
	disk, _, err := Decode(readFixture(t, filepath.Join(home, "vault/tasks/SEC-1.md")))
	if err != nil || disk.Body != updated.Body {
		t.Fatal("incomplete replacement")
	}
}

// Guards: REQ-3
func TestNoPartialReads(t *testing.T) {
	m, home := openStore(t)
	r := putTask(t, m, "SEC-1")
	start := make(chan struct{})
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		close(start)
		for {
			select {
			case <-done:
				return
			default:
			}
			data, err := os.ReadFile(filepath.Join(home, "vault/tasks/SEC-1.md"))
			if err != nil {
				t.Error(err)
				return
			}
			if _, _, err := Decode(data); err != nil {
				t.Error("partial record", err)
				return
			}
		}
	}()
	<-start
	for i := 0; i < 40; i++ {
		r.Body = strings.Repeat("complete", i+1)
		var err error
		r, err = m.Put(context.Background(), r, "code", r.Revision)
		if err != nil {
			t.Error(err)
			break
		}
	}
	close(done)
	wg.Wait()
}

// Guards: REQ-2
func TestSingleWriterLock(t *testing.T) {
	m, home := openStore(t)
	second := New(Deps{})
	if err := second.Open(home); platform.CodeOf(err) != sdk.ErrorCodeUnavailable {
		t.Fatalf("lock was not exclusive: %v", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := second.Open(home); err != nil {
		t.Fatal(err)
	}
	if err := second.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-2
func TestConcurrentPatchSameRecord(t *testing.T) {
	m, _ := openStore(t)
	putTask(t, m, "SEC-1")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := m.Patch(context.Background(), "task", "SEC-1", map[string]any{fmt.Sprintf("field%d", i): "value"}, nil, "code")
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	r, err := m.Get(context.Background(), "task", "SEC-1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if r.Fields[fmt.Sprintf("field%d", i)] != "value" {
			t.Fatal("patch lost")
		}
	}
}

// Guards: REQ-2
func TestConcurrentPutOneConflicts(t *testing.T) {
	m, _ := openStore(t)
	r := putTask(t, m, "SEC-1")
	results := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func(i int) {
			<-start
			copy := cloneRecord(r)
			copy.Body = fmt.Sprint(i)
			_, err := m.Put(context.Background(), copy, "code", r.Revision)
			results <- err
		}(i)
	}
	close(start)
	ok, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			ok++
		} else if platform.CodeOf(err) == sdk.ErrorCodeConflict {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", ok, conflicts)
	}
}

// Guards: REQ-2
func TestShardedLocksDoNotBlockOtherRecords(t *testing.T) {
	m, _ := openStore(t)
	putTask(t, m, "SEC-1")
	other := "SEC-2"
	for i := 2; m.shard("task", other) == m.shard("task", "SEC-1"); i++ {
		other = fmt.Sprintf("SEC-%d", i)
	}
	lock := m.shard("task", "SEC-1")
	lock.Lock()
	defer lock.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := m.Put(ctx, Record{Kind: "task", ID: other}, "code", ""); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("other record was blocked")
	}
}

// Guards: REQ-4
func TestSchemaRejectsBadLink(t *testing.T) {
	m, _ := openStore(t)
	_, err := m.Put(context.Background(), Record{Kind: "task", ID: "SEC-1", Fields: map[string]any{"goal": "[[GOAL-1]]"}}, "code", "")
	if platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal("dangling link accepted")
	}
	if _, err := m.Put(context.Background(), Record{Kind: "goal", ID: "GOAL-1"}, "owner(web)", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.Archive(context.Background(), "goal", "GOAL-1", "system"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Put(context.Background(), Record{Kind: "task", ID: "SEC-1", Fields: map[string]any{"goal": "[[GOAL-1]]"}}, "code", ""); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-8
func TestChangeLogOneLinePerWrite(t *testing.T) {
	m, home := openStore(t)
	putTask(t, m, "SEC-1")
	if _, err := m.Patch(context.Background(), "task", "SEC-1", map[string]any{"title": "changed"}, nil, "code"); err != nil {
		t.Fatal(err)
	}
	data := readFixture(t, filepath.Join(home, "runtime/logs/changes-2026-10-23.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d changes", len(lines))
	}
	for _, line := range lines {
		var v map[string]string
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatal(err)
		}
		if v["id"] != "SEC-1" || v["by"] != "code" || v["kind"] != "task" {
			t.Fatal("change identity missing")
		}
	}
}

// Guards: REQ-44
func TestJournalAppendOnly(t *testing.T) {
	m, _ := openStore(t)
	ctx := context.Background()
	for _, line := range []string{"- 12:00 first", "- 12:01 second"} {
		if err := m.Append(ctx, "journal", "code/2026-10-23", line, "code"); err != nil {
			t.Fatal(err)
		}
	}
	r, err := m.Get(ctx, "journal", "code/2026-10-23")
	if err != nil {
		t.Fatal(err)
	}
	if r.Body != "- 12:00 first\n- 12:01 second" {
		t.Fatal("journal changed")
	}
	if _, err := m.Put(ctx, r, "code", r.Revision); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal("overwrite allowed")
	}
	if _, err := m.Patch(ctx, "journal", r.ID, map[string]any{"body": "lost"}, nil, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal("patch allowed")
	}
}

// Guards: REQ-6
func TestArchivedStillGettable(t *testing.T) {
	m, home := openStore(t)
	r := putTask(t, m, "SEC-1")
	if err := m.Archive(context.Background(), "task", r.ID, "system"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(context.Background(), "task", r.ID)
	if err != nil || !got.Archived || got.Revision != r.Revision {
		t.Fatal("archived record missing or changed")
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	got, err = m.Get(context.Background(), "task", r.ID)
	if err != nil || !got.Archived {
		t.Fatal("archive lost on restart")
	}
}

// Guards: REQ-49
func TestNextIDContinuesCounter(t *testing.T) {
	m, home := openStore(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		id, err := m.NextID(ctx, "task")
		if err != nil || id != fmt.Sprintf("SEC-%d", i) {
			t.Fatal(id, err)
		}
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	id, err := m.NextID(ctx, "task")
	if err != nil || id != "SEC-4" {
		t.Fatal(id, err)
	}
}

// Guards: REQ-3
func TestPutRejectsExternalChange(t *testing.T) {
	m, home := openStore(t)
	r := putTask(t, m, "SEC-1")
	path := filepath.Join(home, "vault/tasks/SEC-1.md")
	external := []byte("---\nid: SEC-1\ntitle: external\n---\n")
	if err := platform.AtomicWrite(path, external, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Put(context.Background(), r, "code", r.Revision); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatal("external edit overwritten")
	}
	if string(readFixture(t, path)) != string(external) {
		t.Fatal("external bytes lost")
	}
}

// 舊機器來源是歷史資料，更新時保留；新資料不再由核心注入機器來源。
func TestMachineOriginRemainsHistoricalMetadata(t *testing.T) {
	m, _ := openStore(t)
	ctx := context.Background()
	fresh := putTask(t, m, "SEC-1")
	if _, ok := fresh.Fields["origin"]; ok {
		t.Fatal("新資料不應注入機器來源")
	}
	old, err := m.Put(ctx, Record{Kind: "task", ID: "SEC-2", Fields: map[string]any{"origin": "office", "title": "舊資料"}}, "amy", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, "task", old.ID)
	if err != nil {
		t.Fatal(err)
	}
	got.Body = "更新內文"
	updated, err := m.Put(ctx, got, "amy", got.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Fields["origin"] != "office" {
		t.Fatalf("舊來源遺失：%v", updated.Fields["origin"])
	}
}
