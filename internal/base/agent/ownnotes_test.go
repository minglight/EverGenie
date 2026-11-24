package agent

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// WriteNote：核心補檔頭（schema、updated_at、by、agent、created_at），秘書只給內文；新檔不用版本、改舊檔要版本。
func TestWriteNoteAddsHeaderAndNeedsRevision(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	name, created, rev, err := m.WriteNote(ctx, "life", " 訓練計畫.md ", "第一週\n", "", "life")
	must(t, err)
	if name != "訓練計畫" || !created || rev == "" {
		t.Fatalf("新檔：%q %v %q", name, created, rev)
	}
	data, err := os.ReadFile(filepath.Join(h.Dir, "vault/agents/life/notes/訓練計畫.md"))
	must(t, err)
	for _, want := range []string{"---\n", "schema: 1", "updated_at:", "by: life", "agent: life", "created_at:", "第一週"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("檔頭缺 %q：\n%s", want, data)
		}
	}
	first := record(t, h, "note", "life/訓練計畫")
	if first.Revision != rev || strings.TrimSpace(first.Body) != "第一週" {
		t.Fatalf("讀回來：%+v", first)
	}

	// 已經有、沒帶版本：拒絕（兩個對話不互蓋）；版本舊了：拒絕；版本對：成功且 created_at 不變。
	if _, _, _, err := m.WriteNote(ctx, "life", "訓練計畫", "覆蓋\n", "", "life"); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("沒帶版本要拒絕並說怎麼做：%v", err)
	}
	_, created, rev2, err := m.WriteNote(ctx, "life", "訓練計畫", "第一週\n第二週\n", rev, "life")
	must(t, err)
	if created || rev2 == rev {
		t.Fatalf("改舊檔：%v %q", created, rev2)
	}
	if _, _, _, err := m.WriteNote(ctx, "life", "訓練計畫", "舊版本想蓋掉\n", rev, "life"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("舊版本要拒絕：%v", err)
	}
	second := record(t, h, "note", "life/訓練計畫")
	if strings.TrimSpace(second.Body) != "第一週\n第二週" || second.Fields["created_at"] != first.Fields["created_at"] {
		t.Fatalf("內文換掉、created_at 不變：%+v", second.Fields)
	}
	// 還不存在的檔帶版本：拒絕。
	if _, _, _, err := m.WriteNote(ctx, "life", "新的", "x\n", "abc", "life"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("新檔帶版本要拒絕：%v", err)
	}
}

// 只能寫自己的（system 例外）；名字與內容要合規。
func TestWriteNoteOnlyOwnAndValidNames(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	if _, _, _, err := m.WriteNote(ctx, "life", "筆記", "x\n", "", "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("別的秘書不能寫：%v", err)
	}
	if _, _, _, err := m.WriteNote(ctx, "life", "筆記", "x\n", "", owner); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("一般主人身分也不經這裡寫秘書的筆記：%v", err)
	}
	if _, _, _, err := m.WriteNote(ctx, "life", "系統寫的", "x\n", "", "system"); err != nil {
		t.Fatalf("system 可以：%v", err)
	}
	if _, _, _, err := m.WriteNote(ctx, "nobody", "筆記", "x\n", "", "nobody"); err == nil {
		t.Fatal("不在名冊上的職位不能寫")
	}
	for _, name := range []string{"", "  ", "a/b", "../x", `a\b`, ".hidden", "x.bak-20261115-y", "archive", "索引", strings.Repeat("長", 81), "tab\there"} {
		if _, _, _, err := m.WriteNote(ctx, "life", name, "x\n", "", "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("名字 %q 要拒絕：%v", name, err)
		}
	}
	if _, _, _, err := m.WriteNote(ctx, "life", "空的", " \n", "", "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("空內容不寫：%v", err)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "vault/agents/life/notes/空的.md")); err == nil {
		t.Fatal("空內容不能留下檔案")
	}
}

// 沒有檔頭的 .md（秘書或主人直接寫的）：讀得到、不算壞檔、doctor 只報注意；寫回時才補檔頭。
// 子資料夾裡的照舊讀（DEV-161 前就這樣）；不是 .md、備份、隱藏檔不掃不碰不擋（VLT-18）。2026-11-15 實際發生過：life 直接寫了沒有檔頭的 .md，部署被健檢退回。
func TestHeaderlessOwnNoteReadableAndOnlyNotice(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.Stop(ctx))
	must(t, h.Vault.Stop(ctx))
	files := map[string]string{
		"vault/agents/life/notes/go-training-plan.md":         "# 圍棋訓練\n- 週一 死活題\n",
		"vault/agents/life/notes/helper.py":                   "print('x')\n",
		"vault/agents/life/notes/old.md.bak-20261101-cleanup": "舊的備份\n",
		"vault/agents/life/notes/old.bak-20261101-x.md":       "舊的備份二\n",
		"vault/agents/life/notes/.draft.md":                   "隱藏的\n",
		"vault/agents/life/notes/舊資料/chat.md":                 "整份搬來的子資料夾，沒有檔頭\n",
		"vault/agents/life/notes/有檔頭.md":                      "---\nschema: 1\nupdated_at: 2026-11-01T10:00:00+08:00\nby: life\n---\n正常的\n",
	}
	for p, body := range files {
		must(t, os.MkdirAll(filepath.Dir(filepath.Join(h.Dir, p)), 0700))
		must(t, os.WriteFile(filepath.Join(h.Dir, p), []byte(body), 0600))
	}
	store := vault.New(vault.Deps{Clock: h.Clock})
	must(t, store.Open(h.Dir))
	t.Cleanup(func() { _ = store.Stop(context.Background()) })
	again := New(Deps{Vault: store, Clock: h.Clock, Bus: &recordingBus{}})
	again.SetHome(h.Dir)
	again.SetPersonaSource(testLibrary(h))
	must(t, again.Start(ctx))
	t.Cleanup(func() { _ = again.Stop(context.Background()) })

	// 沒檔頭的讀得到：整份當內文，欄位照檔案時間補；不算壞檔。
	got, err := store.Get(ctx, "note", "life/go-training-plan")
	must(t, err)
	if got.Body != "# 圍棋訓練\n- 週一 死活題" || got.Fields["by"] != "external" || got.Fields["agent"] != "life" || got.Fields["updated_at"] == "" {
		t.Fatalf("沒檔頭的筆記：%+v", got)
	}
	if bad := store.Invalid(); len(bad) != 0 {
		t.Fatalf("沒檔頭的不能算壞檔（會讓部署被退回）：%v", bad)
	}
	if recs, err := store.InvalidRecords(ctx); err != nil || len(recs) != 0 {
		t.Fatalf("doctor 的資料檢查不能有問題：%v %v", recs, err)
	}
	// 子資料夾的讀得到；.py、備份、隱藏的不在索引裡。
	rows, err := store.List(ctx, "note", vault.Query{})
	must(t, err)
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	if strings.Join(ids, ",") != "life/go-training-plan,life/有檔頭,life/舊資料/chat" {
		t.Fatalf("索引只該有三份：%v", ids)
	}
	// doctor 只報注意：最上層沒檔頭的那份有，子資料夾與其他都不報（只是注意，不吵）。
	probs := strings.Join(again.NoteProblems(ctx), "\n")
	if !strings.Contains(probs, "go-training-plan.md 沒有檔頭") || strings.Count(probs, "沒有檔頭") != 1 {
		t.Fatalf("只該注意 go-training-plan：%s", probs)
	}
	for p, body := range files {
		if data, _ := os.ReadFile(filepath.Join(h.Dir, p)); string(data) != body {
			t.Fatalf("%s 被改了：%q", p, data)
		}
	}

	// 經核心寫回（帶讀到的版本）才補上檔頭，內文保留，之後不再注意。
	if _, _, _, err := again.WriteNote(ctx, "life", "go-training-plan", "# 圍棋訓練\n- 週一 死活題\n- 週二 對局\n", "", "life"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("舊檔沒帶版本要拒絕：%v", err)
	}
	_, created, _, err := again.WriteNote(ctx, "life", "go-training-plan", "# 圍棋訓練\n- 週一 死活題\n- 週二 對局\n", got.Revision, "life")
	must(t, err)
	if created {
		t.Fatal("是改舊檔")
	}
	data, _ := os.ReadFile(filepath.Join(h.Dir, "vault/agents/life/notes/go-training-plan.md"))
	for _, want := range []string{"schema: 1", "by: life", "agent: life", "created_at:", "週二 對局"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("寫回要補檔頭，缺 %q：\n%s", want, data)
		}
	}
	if probs := strings.Join(again.NoteProblems(ctx), "\n"); strings.Contains(probs, "go-training-plan.md 沒有檔頭") {
		t.Fatalf("補了檔頭就不再注意：%s", probs)
	}
}

// 封存的做事筆記重開後還在（封存資料夾 archive/<月>/ 本身不能被當成不認得的資料夾跳過），沒檔頭的封存檔也一樣讀得到。
func TestOwnNoteArchiveSurvivesReopen(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	_, _, _, err := m.WriteNote(ctx, "life", "舊計畫", "內容\n", "", "life")
	must(t, err)
	must(t, h.Vault.ArchiveNow(ctx, "note", "life/舊計畫", "life"))
	must(t, m.Stop(ctx))
	must(t, h.Vault.Stop(ctx))
	bare := filepath.Join(h.Dir, "vault/agents/life/notes/archive/2026-11/沒檔頭.md")
	must(t, os.MkdirAll(filepath.Dir(bare), 0700))
	must(t, os.WriteFile(bare, []byte("封存時就沒有檔頭\n"), 0600))
	store := vault.New(vault.Deps{Clock: h.Clock})
	must(t, store.Open(h.Dir))
	t.Cleanup(func() { _ = store.Stop(context.Background()) })
	rows, err := store.List(ctx, "note", vault.Query{IncludeArchived: true})
	must(t, err)
	if len(rows) != 2 || !rows[0].Archived || !rows[1].Archived {
		t.Fatalf("封存的兩份重開後要都在：%+v", rows)
	}
	if bad := store.Invalid(); len(bad) != 0 {
		t.Fatalf("不能有壞檔：%v", bad)
	}
}
