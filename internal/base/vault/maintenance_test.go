package vault

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

// Guards: REQ-5
func TestSweeperReportThenArchive(t *testing.T) {
	m, home := openStore(t)
	putTask(t, m, "SEC-1")
	ctx := context.Background()
	now := m.d.Clock.Now()
	report, err := m.Sweep(ctx, now, false)
	if err != nil || len(report.Items) != 1 {
		t.Fatal(report, err)
	}
	r, err := m.Get(ctx, "task", "SEC-1")
	if err != nil || r.Archived {
		t.Fatal("dry run changed data")
	}
	if _, err := m.Sweep(ctx, now, true); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(home, "runtime/sweep/2026-10-23.md")
	if !strings.Contains(string(readFixture(t, reportPath)), "archive task SEC-1") {
		t.Fatal("missing report")
	}
	r, err = m.Get(ctx, "task", "SEC-1")
	if err != nil || !r.Archived {
		t.Fatal("not archived")
	}
}

// Guards: REQ-5
func TestSweeperNeverDeletesCanonical(t *testing.T) {
	m, home := openStore(t)
	putTask(t, m, "SEC-1")
	old := filepath.Join(home, "runtime/logs/2026-01-01.jsonl")
	if err := platform.AtomicWrite(old, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sweep(context.Background(), m.d.Clock.Now(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old log not removed")
	}
	if _, err := m.Get(context.Background(), "task", "SEC-1"); err != nil {
		t.Fatal("canonical deleted")
	}
}

// Guards: REQ-6
func TestTaskArchiveTiming(t *testing.T) {
	m, _ := openStore(t)
	ctx := context.Background()
	now := m.d.Clock.Now()
	m.RegisterSchema(Spec{Kind: "timing", Path: func(id string) string { return "vault/rules/timing/" + id + ".md" }, ArchiveAt: func(r Record, loc *time.Location) (time.Time, bool) {
		at, err := time.Parse(time.RFC3339, r.Fields["when"].(string))
		if err != nil {
			return time.Time{}, false
		}
		if r.Fields["status"] == "shelved" {
			return at.AddDate(0, 0, 30), true
		}
		day := at.In(loc)
		return time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc), true
	}})
	for _, status := range []string{"done", "shelved"} {
		if _, err := m.Put(ctx, Record{Kind: "timing", ID: status, Fields: map[string]any{"status": status, "when": now.Format(time.RFC3339)}}, "code", ""); err != nil {
			t.Fatal(err)
		}
	}
	report, err := m.Sweep(ctx, now, false)
	if err != nil || len(report.Items) != 0 {
		t.Fatal("early archive", err)
	}
	report, err = m.Sweep(ctx, now.AddDate(0, 0, 1), false)
	if err != nil || len(report.Items) != 1 || report.Items[0].ID != "done" {
		t.Fatal("next day rule", err)
	}
	report, err = m.Sweep(ctx, now.AddDate(0, 0, 30), false)
	archived := 0
	for _, item := range report.Items {
		if item.Action == "archive" {
			archived++
		}
	}
	if err != nil || archived != 2 {
		t.Fatal("30 day rule", err)
	}
}

// Guards: REQ-10
func TestDailySnapshot(t *testing.T) {
	m, home := openStore(t)
	putTask(t, m, "SEC-1")
	ctx := context.Background()
	dest := t.TempDir()
	path, err := m.Snapshot(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Snapshot(ctx, dest)
	if err != nil || again != path {
		t.Fatal("snapshot not daily", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := gz.Close(); err != nil {
			t.Error(err)
		}
	}()
	archive := tar.NewReader(gz)
	files := map[string][]byte{}
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(archive)
			if err != nil {
				t.Fatal(err)
			}
			files[header.Name] = data
		}
	}
	count := 0
	err = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(files[filepath.ToSlash(rel)]) != string(data) {
			t.Errorf("snapshot differs: %s", rel)
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != len(files) {
		t.Fatal("snapshot file count mismatch")
	}
}
