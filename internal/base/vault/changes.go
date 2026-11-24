package vault

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 變更紀錄的讀取（VLT-8 寫、這裡讀）：runtime/logs/changes-YYYY-MM-DD.jsonl，新到舊，分頁。
const (
	changesDefaultLimit = 100
	changesMaxLimit     = 500
)

// ChangesQuery 是 Changes 的篩選與分頁；欄位都可以空。
type ChangesQuery struct {
	Since  string // YYYY-MM-DD（含），依家的時區
	Until  string // YYYY-MM-DD（含）
	By     string // 誰；"owner" 表示任何 owner(…)（主人），其他照字完全相同
	Kind   string
	Limit  int
	Before string // 上一頁回的 next_before
}

func parseDay(field, v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return "", platform.Invalid(field, "日期請用 YYYY-MM-DD")
	}
	return v, nil
}

// parseBefore 把 next_before（"<日期>:<行號>"）拆開；行號是那一天的檔案裡的第幾行（從 0 算）。
func parseBefore(v string) (day string, line int, ok bool, err error) {
	if v == "" {
		return "", 0, false, nil
	}
	d, n, found := strings.Cut(v, ":")
	if !found {
		return "", 0, false, platform.Invalid("before", "分頁位置不正確")
	}
	if _, perr := time.Parse("2006-01-02", d); perr != nil {
		return "", 0, false, platform.Invalid("before", "分頁位置不正確")
	}
	line, perr := strconv.Atoi(n)
	if perr != nil || line < 0 {
		return "", 0, false, platform.Invalid("before", "分頁位置不正確")
	}
	return d, line, true, nil
}

func whoMatches(want, by string) bool {
	if want == "" {
		return true
	}
	if want == "owner" {
		return platform.IsOwner(by)
	}
	return by == want
}

// Changes 讀變更紀錄：新到舊，壞行略過；回最多 Limit 筆和下一頁的位置（沒有更早的是空字串）。
func (m *Module) Changes(ctx context.Context, q ChangesQuery) ([]sdk.ChangeEntry, string, error) {
	if err := m.ready(ctx); err != nil {
		return nil, "", err
	}
	since, err := parseDay("since", q.Since)
	if err != nil {
		return nil, "", err
	}
	until, err := parseDay("until", q.Until)
	if err != nil {
		return nil, "", err
	}
	beforeDay, beforeLine, hasBefore, err := parseBefore(q.Before)
	if err != nil {
		return nil, "", err
	}
	limit := q.Limit
	switch {
	case limit < 0:
		return nil, "", platform.Invalid("limit", "筆數不可小於零")
	case limit == 0:
		limit = changesDefaultLimit
	case limit > changesMaxLimit:
		limit = changesMaxLimit
	}
	dir := platform.RuntimeDir(m.home, "logs")
	names, err := filepath.Glob(filepath.Join(dir, "changes-*.jsonl"))
	if err != nil {
		return nil, "", platform.Unavailable("無法讀取變更紀錄")
	}
	type dayFile struct{ day, path string }
	var files []dayFile
	for _, p := range names {
		day := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "changes-"), ".jsonl")
		if _, err := time.Parse("2006-01-02", day); err != nil {
			continue
		}
		if since != "" && day < since || until != "" && day > until || hasBefore && day > beforeDay {
			continue
		}
		files = append(files, dayFile{day, p})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].day > files[j].day })

	items := []sdk.ChangeEntry{}
	next := ""
	var lastPos string
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		m.logMu.Lock()
		data, rerr := os.ReadFile(f.path)
		m.logMu.Unlock()
		if rerr != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		start := len(lines) - 1
		if hasBefore && f.day == beforeDay && beforeLine-1 < start {
			start = beforeLine - 1
		}
		for i := start; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			var raw struct {
				At   string `json:"at"`
				By   string `json:"by"`
				Kind string `json:"kind"`
				ID   string `json:"id"`
				Op   string `json:"op"`
			}
			if json.Unmarshal([]byte(line), &raw) != nil || raw.Kind == "" || raw.ID == "" || raw.Op == "" {
				continue
			}
			at, perr := time.Parse(time.RFC3339Nano, raw.At)
			if perr != nil {
				continue
			}
			if !whoMatches(q.By, raw.By) || q.Kind != "" && raw.Kind != q.Kind {
				continue
			}
			if len(items) == limit {
				// 還有符合的：下一頁從最後一筆之前接著讀。
				next = lastPos
				return items, next, nil
			}
			items = append(items, sdk.ChangeEntry{At: at, By: raw.By, Kind: raw.Kind, ID: raw.ID, Op: raw.Op})
			lastPos = f.day + ":" + strconv.Itoa(i)
		}
	}
	return items, "", nil
}
