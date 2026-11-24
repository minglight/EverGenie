package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-121, REQ-6
func TestImportArchivedWritesIntoArchiveMonth(t *testing.T) {
	m, home := openStore(t)
	ctx := context.Background()
	rec, err := m.ImportArchived(ctx, Record{Kind: "task", ID: "SEC-9", Fields: map[string]any{"title": "舊單"}, Body: "內文"}, "2026-09", "migrate")
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Archived {
		t.Error("匯入的資料應標成已封存")
	}
	want := filepath.Join(home, "vault", "tasks", "archive", "2026-09", "SEC-9.md")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("檔案應在封存月份資料夾：%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "vault", "tasks", "SEC-9.md")); !os.IsNotExist(err) {
		t.Error("不該同時出現在進行中的資料夾")
	}
	got, err := m.Get(ctx, "task", "SEC-9")
	if err != nil || !got.Archived || got.Body != "內文" {
		t.Fatalf("封存的資料應查得到：%+v %v", got, err)
	}
	// 重新開啟後仍是封存、仍查得到（以 id 找，不看路徑）。
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	if again, err := m.Get(ctx, "task", "SEC-9"); err != nil || !again.Archived {
		t.Fatalf("重開後封存狀態遺失：%+v %v", again, err)
	}
	// 已存在的不能再匯入；原地更新用一般的 Put 且仍在封存資料夾。
	if _, err := m.ImportArchived(ctx, Record{Kind: "task", ID: "SEC-9", Fields: map[string]any{"title": "x"}}, "2026-09", "migrate"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Errorf("已存在的資料再匯入應回 CONFLICT：%v", err)
	}
	cur, _ := m.Get(ctx, "task", "SEC-9")
	cur.Fields["title"] = "改過"
	if _, err := m.Put(ctx, cur, "migrate", cur.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("更新後仍應在封存資料夾：%v", err)
	}
}

// Guards: REQ-121, REQ-6
func TestImportArchivedRejectsBadMonth(t *testing.T) {
	m, _ := openStore(t)
	for _, month := range []string{"", "2026-13", "26-09", "2026-9", "../x", "2026-09/x"} {
		if _, err := m.ImportArchived(context.Background(), Record{Kind: "task", ID: "SEC-1", Fields: map[string]any{"title": "x"}}, month, "migrate"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("月份 %q 應被拒絕：%v", month, err)
		}
	}
	if _, err := m.ImportArchived(context.Background(), Record{Kind: "journal", ID: "life/2026-09-01", Fields: map[string]any{}}, "2026-09", "migrate"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Errorf("日誌不能匯入封存：%v", err)
	}
}

// Guards: REQ-121, REQ-4
func TestImportArchivedStillValidates(t *testing.T) {
	m, home := openStore(t)
	_, err := m.ImportArchived(context.Background(), Record{Kind: "task", ID: "SEC-3", Fields: map[string]any{"goal": "[[GOAL-404]]"}}, "2026-09", "migrate")
	if platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("連結指向不存在的資料應被拒絕：%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "vault", "tasks", "archive")); !os.IsNotExist(statErr) {
		t.Error("驗證失敗時不該留下任何檔案")
	}
}

// Guards: REQ-121, REQ-49
func TestEnsureCounterContinuesFromImported(t *testing.T) {
	m, home := openStore(t)
	ctx := context.Background()
	if changed, err := m.EnsureCounter(ctx, "task", 320); err != nil || !changed {
		t.Fatalf("第一次應改動計數器：%v %v", changed, err)
	}
	if changed, err := m.EnsureCounter(ctx, "task", 320); err != nil || changed {
		t.Fatalf("同樣的值不該再寫：%v %v", changed, err)
	}
	if changed, err := m.EnsureCounter(ctx, "task", 100); err != nil || changed {
		t.Fatalf("計數器不能往回調：%v %v", changed, err)
	}
	if id, err := m.NextID(ctx, "task"); err != nil || id != "SEC-321" {
		t.Fatalf("下一號應接續：%q %v", id, err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	if id, err := m.NextID(ctx, "task"); err != nil || id != "SEC-322" {
		t.Fatalf("重開後仍應接續：%q %v", id, err)
	}
	data, _ := os.ReadFile(filepath.Join(home, "vault", ".counters.toml"))
	if !strings.Contains(string(data), "task = 322") {
		t.Errorf("計數檔內容不對：%q", data)
	}
	if _, err := m.EnsureCounter(ctx, "note", 1); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Errorf("沒有遞增識別碼的種類應被拒絕：%v", err)
	}
}
