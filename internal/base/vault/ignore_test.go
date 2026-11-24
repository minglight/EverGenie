package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func TestNotOurs(t *testing.T) {
	for rel, want := range map[string]bool{
		"tasks/SEC-1.md":                                 false,
		"tasks/archive/2026-11/SEC-1.md":                 false,
		"agents/admin/notes/x.md":                        false,
		"pkg/closet/outfit/a.md":                         false,
		"profile.md":                                     false,
		"pkg.bak-20261109-to-mine/water/entry/a.md":      true, // 整份備份放在 vault 最上層
		"pkg/closet.bak-20261108-x/outfit/a.md":          true,
		"pkg/closet/outfit.bak-20261108-x/a.md":          true,
		"agents/admin.bak-20261031-split/agent.md":       true,
		"tasks/SEC-1.md.bak-20261101-cleanup":            true,
		"我的筆記/想法.md":                                     true, // 主人自己的資料夾
		"README.md":                                      true,
		".obsidian/workspace.md":                         true,
		"agents/admin/.trash/x.md":                       true,
		"tasks.bak-20261101-cleanup/SEC-1.md":            true,
		"pkg/character-studio/lock/archive/2026-11/a.md": false,
		"rules/versions/RV-1.md":                         false,
		"rules.bak-20261112-x/versions/RV-1.md":          true,
		"rulesx/versions/RV-1.md":                        true,
		"notes/人/主人.md":                                  false, // 主題筆記（DEV-161）
		"notes/人/人.md":                                   false,
		"notes/索引.md":                                    true, // 核心產生的索引不是紀錄
		"agents/life/索引.md":                              true,
		"agents/life/notes/索引.md":                        false,
		"agents/life/notes/archive/2026-11/計畫.md":        false, // 做事筆記封存的位置
		"agents/life/notes/archive":                      false, // 封存的資料夾本身也要放行，不然封存檔重開後整批不見
		"agents/life/notes/archive/2026-11":              false,
		"agents/life/notes/舊資料/chat.md":                  false, // 做事筆記的子資料夾照舊認（例：briefings/、reflections/）
		"agents/life/notes/舊資料/深/chat.md":                false,
	} {
		if got := notOurs(rel); got != want {
			t.Errorf("notOurs(%q) = %v, want %v", rel, got, want)
		}
	}
}

// 註冊紀錄種類時用到清單外的最上層資料夾要當場擋下來（曾經漏登記 vault/rules/，重開後規矩的版本全不見）。
func TestRegisterSchemaRejectsUnknownRoot(t *testing.T) {
	m := New(Deps{})
	m.RegisterSchema(Spec{Kind: "rule_version", Path: func(id string) string { return "vault/rules/versions/" + id + ".md" }})
	defer func() {
		if recover() == nil {
			t.Fatal("清單外的資料夾要 panic")
		}
	}()
	m.RegisterSchema(Spec{Kind: "stray", Path: func(id string) string { return "vault/somewhere/" + id + ".md" }})
}

// 主人自己放在 vault 裡的東西（備份、筆記、Obsidian 的設定）不進索引、不算壞檔，啟動時與之後外部加進來的都一樣；
// 核心資料夾裡真的壞掉的檔案照舊報（DEV-154 第 9 項；2026-11-12 曾經因為 vault/pkg.bak-… 健康檢查全紅）。
func TestOwnerFilesInVaultIgnored(t *testing.T) {
	m, home := openStore(t)
	putTask(t, m, "SEC-1")
	live, err := os.ReadFile(filepath.Join(home, "vault/tasks/SEC-1.md"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(rel string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(home, "vault", rel)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := platform.AtomicWrite(filepath.Join(home, "vault", rel), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	owner := []string{
		"tasks.bak-20261101-cleanup/SEC-1.md", // 同一筆的備份：不能被當成重複
		"pkg.bak-20261109-to-mine/water/entry/2026-11-01.md",
		"tasks/SEC-1.md.bak-20261101-x/SEC-1.md",
		"我的筆記/想法.md",
		"README.md",
		".obsidian/note.md",
	}
	ch, ctx := subscribe(t, m)
	for _, rel := range owner {
		write(rel, live)
	}
	write("我的筆記/壞掉.md", []byte("---\ntitle: [broken\n---\n"))
	write("tasks/SEC-2.md", []byte("---\ntitle: [broken\n---\n")) // 核心資料夾裡真的壞掉的：要報
	awaitWatchState(t, m, home, ctx, ch, func() bool { return len(m.Invalid()) == 1 })
	if got := m.Invalid(); len(got) != 1 || got[0] != "vault/tasks/SEC-2.md" {
		t.Fatalf("invalid = %v, want only vault/tasks/SEC-2.md", got)
	}
	if _, err := m.Get(ctx, "task", "SEC-1"); err != nil {
		t.Fatalf("live record lost: %v", err)
	}

	// 重開（啟動時的整份掃描）結果一樣。
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(home); err != nil {
		t.Fatalf("reopen with owner files: %v", err)
	}
	if got := m.Invalid(); len(got) != 1 || got[0] != "vault/tasks/SEC-2.md" {
		t.Fatalf("after reopen invalid = %v", got)
	}
	list, err := m.List(context.Background(), "task", Query{})
	var ids []string
	for _, r := range list {
		if r.ID != "SEC-999" { // awaitWatchState 等監看時寫的探針檔，有時會留著
			ids = append(ids, r.ID)
		}
	}
	if err != nil || len(ids) != 1 || ids[0] != "SEC-1" {
		t.Fatalf("tasks after reopen = %v, %v", list, err)
	}
}

// notes/ 底下沒有自稱主題（name）或分類說明（kind: category）的檔＝主人自己的筆記（DEV-161）。
func TestOwnerNoteFile(t *testing.T) {
	cases := []struct {
		rel    string
		fields map[string]any
		bad    bool
		want   bool
	}{
		{"notes/人/小美.md", map[string]any{"name": "小美"}, false, false},
		{"notes/人/人.md", map[string]any{"kind": "category"}, false, false},
		{"notes/人/日記.md", map[string]any{}, false, true},
		{"notes/旅行/京都.md", map[string]any{"tags": []any{"trip"}}, false, true},
		{"notes/人/壞.md", nil, true, true},
		{"agents/life/memory.md", map[string]any{}, false, false},
	}
	for _, c := range cases {
		var err error
		if c.bad {
			err = errors.New("bad")
		}
		if got := OwnerNoteFile(c.rel, Record{Fields: c.fields}, err); got != c.want {
			t.Errorf("%s：%v，要 %v", c.rel, got, c.want)
		}
	}
}

// 沒有檔頭的做事筆記（agents/<職位>/notes/<名字>.md）：整份當內文、欄位照檔案時間補、不算壞檔；
// 啟動時掃到與執行中被外部加進來一樣，子資料夾裡的也是；非 .md、備份、隱藏檔不進索引。檔頭有但壞掉的照舊算壞檔。
func TestHeaderlessOwnNote(t *testing.T) {
	m, home := openStore(t)
	m.RegisterSchema(Spec{Kind: "note", Fields: []Field{{Name: "schema", Type: Int, Required: true}, {Name: "updated_at", Type: DateTime, Required: true}, {Name: "by", Type: String, Required: true}}})
	write := func(rel, data string) {
		t.Helper()
		path := filepath.Join(home, "vault", rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := platform.AtomicWrite(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("agents/life/notes/計畫.md", "# 計畫\n一行\n")
	write("agents/life/notes/x.py", "print(1)\n")
	write("agents/life/notes/舊/子.md", "子資料夾\n")
	write("agents/life/notes/p.bak-20261101-x.md", "備份\n")
	// 重開：啟動時的整份掃描。
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	r, err := m.Get(context.Background(), "note", "life/計畫")
	if err != nil || r.Body != "# 計畫\n一行" || r.Fields["by"] != "external" || r.Fields["agent"] != "life" || r.Revision == "" {
		t.Fatalf("沒檔頭的筆記：%+v %v", r, err)
	}
	if bad := m.Invalid(); len(bad) != 0 {
		t.Fatalf("不能有壞檔：%v", bad)
	}
	rows, _ := m.List(context.Background(), "note", Query{})
	if len(rows) != 2 {
		t.Fatalf("計畫 與子資料夾的 舊/子 進索引：%v", rows)
	}
	// 執行中外部加進來的，一樣。
	ch, ctx := subscribe(t, m)
	write("agents/life/notes/清單.md", "- 一\n- 二\n")
	if c := receive(t, ctx, ch); c.Record.ID != "life/清單" || c.Type != sdk.ChangeTypeCreated {
		t.Fatalf("外部新增的筆記要有一則建立的通知：%+v", c)
	}
	write("agents/life/notes/壞檔頭.md", "---\ntitle: [broken\n---\n")
	awaitWatchState(t, m, home, ctx, ch, func() bool { return len(m.Invalid()) == 1 })
	if got, err := m.Get(ctx, "note", "life/清單"); err != nil || got.Body != "- 一\n- 二" {
		t.Fatalf("外部新增的沒檔頭筆記：%+v %v", got, err)
	}
	if got := m.Invalid(); len(got) != 1 || got[0] != "vault/agents/life/notes/壞檔頭.md" {
		t.Fatalf("壞掉的檔頭照舊算壞檔：%v", got)
	}
	// 經核心寫回帶版本：補上檔頭、created_at 是原檔時間。
	r.Body = "# 計畫\n一行\n二行\n"
	next, err := m.Put(ctx, r, "life", r.Revision)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(home, "vault/agents/life/notes/計畫.md"))
	if !strings.HasPrefix(string(data), "---\n") || !strings.Contains(string(data), "by: life") || next.Fields["created_at"] != r.Fields["created_at"] {
		t.Fatalf("寫回要補檔頭：\n%s", data)
	}
}
