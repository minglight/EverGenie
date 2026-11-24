package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

const validTask = "---\nschema: 1\nid: SEC-1\nstatus: doing\n---\n"

// Guards: REQ-117, REQ-2
func TestInvalidListsBadRecords(t *testing.T) {
	home := t.TempDir()
	if err := platform.AtomicWrite(filepath.Join(home, "home.toml"), []byte(storeConfig), 0600); err != nil {
		t.Fatal(err)
	}
	write := func(rel, text string) {
		t.Helper()
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("vault/tasks/SEC-1.md", validTask)
	write("vault/tasks/SEC-2.md", "---\nschema: 1\nid: SEC-2\nstatus: sleeping\n---\n") // 列舉值不在 schema 裡
	write("vault/tasks/archive/2026-09/SEC-3.md", "---\nschema: 1\nid: SEC-3\n---\n")   // 封存的也要掃；缺必要欄位
	write("vault/tasks/SEC-4.md", "---\ntitle: [broken\n---\n")                         // 檔案讀不了
	write("vault/agents/x.md", "---\nid: x\nstatus: whatever\n---\n")                   // 核心資料夾裡放錯地方的檔案
	write("vault/stray/x.md", "---\nid: x\nstatus: whatever\n---\n")                    // 主人自己的資料夾：不算壞檔（DEV-154）
	loc, _ := time.LoadLocation("Asia/Taipei")
	m := New(Deps{Clock: platform.NewFakeClock(time.Date(2026, 10, 25, 12, 0, 0, 0, loc))})
	m.RegisterSchema(Spec{Kind: "task", Fields: []Field{
		{Name: "id", Type: String, Required: true},
		{Name: "status", Type: Enum, Required: true, Enum: []string{"doing", "done"}},
	}})
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })

	got, err := m.InvalidRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("要恰好 4 筆（SEC-2、SEC-3、SEC-4、放錯地方的檔案），實際 %+v", got)
	}
	by := map[string]InvalidRecord{}
	for _, r := range got {
		by[r.ID] = r
	}
	if r := by["SEC-2"]; r.Kind != "task" || r.Field != "status" || r.Reason == "" || r.Path != "vault/tasks/SEC-2.md" {
		t.Errorf("SEC-2 = %+v", r)
	}
	if r := by["SEC-3"]; r.Field != "status" || r.Path != "vault/tasks/archive/2026-09/SEC-3.md" {
		t.Errorf("封存的 SEC-3 = %+v", r)
	}
	if r := by["SEC-4"]; r.Field != "" || r.Reason == "" || r.Path != "vault/tasks/SEC-4.md" {
		t.Errorf("讀不了的 SEC-4 = %+v", r)
	}
	if r := by[""]; r.Path != "vault/agents/x.md" || r.Reason == "" {
		t.Errorf("放錯地方的檔案 = %+v", r)
	}
	if _, ok := by["SEC-1"]; ok {
		t.Error("正常的 SEC-1 不該列")
	}

	// 外部編輯時監看會把欄位不合的檔案直接標成壞的；說明要能指出是哪個欄位。
	write("vault/tasks/SEC-5.md", "---\nschema: 1\nid: SEC-5\nstatus: sleeping\n---\n")
	if r := m.explainBroken(filepath.Join(home, "vault/tasks/SEC-5.md"), "vault/tasks/SEC-5.md"); r.Kind != "task" || r.ID != "SEC-5" || r.Field != "status" {
		t.Errorf("explainBroken = %+v", r)
	}

	// 修好 SEC-2 之後不再列（每次都重新掃）。
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	write("vault/tasks/SEC-2.md", "---\nschema: 1\nid: SEC-2\nstatus: done\n---\n")
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	got, err = m.InvalidRecords(context.Background())
	if err != nil || len(got) != 4 {
		t.Fatalf("修好後剩 4 筆（多了剛放的 SEC-5）：%+v %v", got, err)
	}
}
