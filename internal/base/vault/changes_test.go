package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func writeChanges(t *testing.T, home, day string, lines ...string) {
	t.Helper()
	dir := filepath.Join(home, "runtime", "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "changes-"+day+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func changeLine(day, clock, by, kind, id, op string) string {
	return fmt.Sprintf(`{"at":"%sT%s+08:00","by":"%s","kind":"%s","id":"%s","op":"%s"}`, day, clock, by, kind, id, op)
}

func changeIDs(items []sdk.ChangeEntry) string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return strings.Join(ids, ",")
}

// 跨日檔、壞行、篩選、分頁：新到舊，壞行略過，分頁接得起來不重複不漏。
// Guards: REQ-8, REQ-111
func TestChangesPagingAndFilters(t *testing.T) {
	m, home := openStore(t)
	ctx := context.Background()
	writeChanges(t, home, "2026-10-21",
		changeLine("2026-10-21", "09:00:00", "owner(web)", "task", "SEC-1", "put"),
		"這不是 JSON",
		changeLine("2026-10-21", "09:05:00", "code", "task", "SEC-1", "patch"),
	)
	writeChanges(t, home, "2026-10-22",
		changeLine("2026-10-22", "08:00:00", "owner(cli)", "reminder", "R-1", "put"),
		`{"at":"壞時間","by":"code","kind":"task","id":"SEC-X","op":"put"}`,
		`{"at":"2026-10-22T08:30:00+08:00","by":"code","kind":"","id":"SEC-Y","op":"put"}`,
		changeLine("2026-10-22", "08:30:00", "life", "reminder", "R-2", "put"),
		"",
		changeLine("2026-10-22", "09:00:00", "owner(web)", "goal", "GOAL-1", "put"),
	)
	writeChanges(t, home, "2026-10-23",
		changeLine("2026-10-23", "07:00:00", "system", "task", "SEC-2", "archive"),
		changeLine("2026-10-23", "10:00:00", "code", "task", "SEC-3", "put"),
	)
	// 不是紀錄檔的檔名不能讓它壞掉。
	if err := os.WriteFile(filepath.Join(home, "runtime", "logs", "changes-oops.jsonl"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	all, next, err := m.Changes(ctx, ChangesQuery{})
	if err != nil || next != "" {
		t.Fatalf("全部：%v next=%q", err, next)
	}
	if got := changeIDs(all); got != "SEC-3,SEC-2,GOAL-1,R-2,R-1,SEC-1,SEC-1" {
		t.Fatalf("新到舊、壞行略過，拿到 %s", got)
	}
	if all[2].At.Format("2006-01-02T15:04:05") != "2026-10-22T09:00:00" || all[2].By != "owner(web)" || all[2].Kind != "goal" || all[2].Op != "put" {
		t.Fatalf("欄位要完整：%+v", all[2])
	}

	// 分頁：每頁 2 筆，串起來要等於全部。
	var paged []string
	before := ""
	for i := 0; i < 10; i++ {
		page, nb, err := m.Changes(ctx, ChangesQuery{Limit: 2, Before: before})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) > 2 {
			t.Fatalf("一頁最多 2 筆：%d", len(page))
		}
		paged = append(paged, changeIDs(page))
		if nb == "" {
			break
		}
		before = nb
	}
	if got := strings.Join(paged, ","); got != "SEC-3,SEC-2,GOAL-1,R-2,R-1,SEC-1,SEC-1" {
		t.Fatalf("分頁串起來要等於全部，拿到 %s（%v）", got, paged)
	}
	// 剛好一頁用完時不能留下空的下一頁。
	if _, nb, _ := m.Changes(ctx, ChangesQuery{Limit: 7}); nb != "" {
		t.Fatalf("剛好 7 筆、一頁 7 筆，不該有下一頁：%q", nb)
	}

	// 篩選。
	byOwner, _, _ := m.Changes(ctx, ChangesQuery{By: "owner"})
	if got := changeIDs(byOwner); got != "GOAL-1,R-1,SEC-1" {
		t.Fatalf("owner 要包含 owner(web) 與 owner(cli)：%s", got)
	}
	byCode, _, _ := m.Changes(ctx, ChangesQuery{By: "code"})
	if got := changeIDs(byCode); got != "SEC-3,SEC-1" {
		t.Fatalf("秘書要完全相同：%s", got)
	}
	reminders, _, _ := m.Changes(ctx, ChangesQuery{Kind: "reminder"})
	if got := changeIDs(reminders); got != "R-2,R-1" {
		t.Fatalf("種類：%s", got)
	}
	days, _, _ := m.Changes(ctx, ChangesQuery{Since: "2026-10-22", Until: "2026-10-22"})
	if got := changeIDs(days); got != "GOAL-1,R-2,R-1" {
		t.Fatalf("單日：%s", got)
	}
	since, _, _ := m.Changes(ctx, ChangesQuery{Since: "2026-10-23"})
	if got := changeIDs(since); got != "SEC-3,SEC-2" {
		t.Fatalf("起日：%s", got)
	}
	combo, _, _ := m.Changes(ctx, ChangesQuery{Kind: "task", By: "code", Until: "2026-10-22"})
	if got := changeIDs(combo); got != "SEC-1" {
		t.Fatalf("組合：%s", got)
	}

	// 限制與錯誤。
	big, _, err := m.Changes(ctx, ChangesQuery{Limit: 100000})
	if err != nil || len(big) != 7 {
		t.Fatalf("上限會被夾到 500：%v %d", err, len(big))
	}
	for name, q := range map[string]ChangesQuery{
		"壞日期":     {Since: "10/21"},
		"壞 until": {Until: "昨天"},
		"壞分頁":     {Before: "亂寫"},
		"壞行號":     {Before: "2026-10-22:x"},
		"負的筆數":    {Limit: -1},
	} {
		if _, _, err := m.Changes(ctx, q); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("%s 要回 INVALID：%v", name, err)
		}
	}
}

// 沒有紀錄檔的家回空清單，不是錯誤。
// Guards: REQ-111
func TestChangesEmptyHome(t *testing.T) {
	m, _ := openStore(t)
	items, next, err := m.Changes(context.Background(), ChangesQuery{})
	if err != nil || len(items) != 0 || next != "" {
		t.Fatalf("%v %v %q", err, items, next)
	}
}

// 真的寫入會出現在 Changes（寫與讀用同一種格式）。
// Guards: REQ-8
func TestChangesReadsWhatWritesAppend(t *testing.T) {
	m, _ := openStore(t)
	putTask(t, m, "SEC-1")
	if _, err := m.Patch(context.Background(), "task", "SEC-1", map[string]any{"title": "changed"}, nil, "code"); err != nil {
		t.Fatal(err)
	}
	items, _, err := m.Changes(context.Background(), ChangesQuery{})
	if err != nil || len(items) != 2 || items[0].Op != "patch" || items[0].By != "code" || items[1].Op != "create" {
		t.Fatalf("%v %+v", err, items)
	}
}
