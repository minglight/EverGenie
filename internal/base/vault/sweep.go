package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

type SweepItem struct{ Kind, ID, Path, Action string }
type Report struct {
	At    time.Time
	Items []SweepItem
}

func (m *Module) Sweep(ctx context.Context, now time.Time, apply bool) (Report, error) {
	report := Report{At: now, Items: []SweepItem{}}
	m.lifeMu.RLock()
	if err := m.ready(ctx); err != nil {
		m.lifeMu.RUnlock()
		return report, err
	}
	home, loc := m.home, m.loc
	m.indexMu.RLock()
	entries := sortedEntries(m.index)
	m.indexMu.RUnlock()
	for _, e := range entries {
		if e.record.Archived {
			continue
		}
		sc, ok := m.spec(e.record.Kind)
		if !ok || sc.ArchiveAt == nil {
			continue
		}
		at, ok := sc.ArchiveAt(cloneRecord(e.record), loc)
		if ok && !now.Before(at) {
			report.Items = append(report.Items, SweepItem{e.record.Kind, e.record.ID, e.path, "archive"})
		}
	}
	logs, err := filepath.Glob(platform.RuntimeDir(home, "logs", "*.jsonl"))
	if err != nil {
		m.lifeMu.RUnlock()
		return report, err
	}
	for _, path := range logs {
		date := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "changes-"), ".jsonl")
		date, _, _ = strings.Cut(date, ".") // 超過大小上限輪替出來的 <日期>.1.jsonl 也照日期清（PLT-29）
		at, err := time.ParseInLocation("2006-01-02", date, loc)
		if err == nil && now.After(at.AddDate(0, 0, 30)) {
			report.Items = append(report.Items, SweepItem{Path: path, Action: "remove-log"})
		}
	}
	m.lifeMu.RUnlock()
	sort.Slice(report.Items, func(i, j int) bool { return report.Items[i].Path < report.Items[j].Path })
	if !apply {
		return report, nil
	}
	var body strings.Builder
	fmt.Fprintf(&body, "# 清道夫報告\n\n時間：%s\n\n", now.In(loc).Format(time.RFC3339))
	for _, item := range report.Items {
		rel, err := filepath.Rel(home, item.Path)
		if err != nil {
			return report, err
		}
		fmt.Fprintf(&body, "- %s %s %s (%s)\n", item.Action, item.Kind, item.ID, rel)
	}
	dir := platform.RuntimeDir(home, "sweep")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return report, err
	}
	if err := platform.AtomicWrite(filepath.Join(dir, now.In(loc).Format("2006-01-02")+".md"), []byte(body.String()), 0600); err != nil {
		return report, err
	}
	for _, item := range report.Items {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if item.Action == "archive" {
			if err := m.Archive(ctx, item.Kind, item.ID, "system"); err != nil {
				return report, err
			}
		} else {
			if err := os.Remove(item.Path); err != nil && !os.IsNotExist(err) {
				return report, err
			}
		}
	}
	return report, nil
}
