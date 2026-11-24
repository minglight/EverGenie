package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 新主題要給分類（主人例外）；add 自動在行尾標寫的人和日期，跟進預設是寫的那位。
func TestNoteAddMarksWriterAndNeedsCategory(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	if _, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "小美", Line: "女兒，8 歲", Summary: "女兒"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "分類") {
		t.Fatalf("新主題沒給分類要拒絕：%v", err)
	}
	if _, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "小美", Line: "女兒", Category: "寵物", Summary: "女兒"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒有的分類要拒絕：%v", err)
	}
	out, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "小美", Line: "女兒，8 歲", Category: "人", Summary: "女兒", Aliases: []string{"美美"}}, "life")
	must(t, err)
	if !out.Created || out.Line != "女兒，8 歲（life 10-23）" {
		t.Fatalf("add 要自動標名：%+v", out)
	}
	_, err = m.AddNote(ctx, &sdk.NotesAddRequest{Name: "美美", Line: "對花生過敏"}, "code") // 用別名找、別的職位補一行
	must(t, err)
	r := record(t, h, TopicKind, "人/小美")
	if r.Body != "女兒，8 歲（life 10-23）\n對花生過敏（code 10-23）\n" {
		t.Fatalf("內文：%q", r.Body)
	}
	if got := fieldList(r, "follow"); !slices.Equal(got, []string{"life"}) {
		t.Fatalf("跟進預設是建的人：%v", got)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "vault/notes/人/小美.md")); err != nil {
		t.Fatal("主題筆記要放在 vault/notes/<分類>/<名字>.md：", err)
	}
	out, err = m.AddNote(ctx, &sdk.NotesAddRequest{Name: "主人", Line: "在台北工作"}, owner)
	must(t, err)
	if out.Line != "在台北工作（主人 10-23）" || out.Topic.ID != OwnerTopicID {
		t.Fatalf("主人寫的標「主人」、主人主題固定在人：%+v", out)
	}
	if _, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "x", Line: "y", Category: "人", Summary: "z"}, "stranger"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("不在名冊上的人不能寫：%v", err)
	}
	if _, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "a/b", Line: "y", Category: "人", Summary: "z"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("名字不能有斜線：%v", err)
	}
}

// set 整份取代：秘書新寫的行沒標名就拒絕並列出行號；主人原本手寫、沒標記的行原樣留著不算；主人自己 set 不檢查。
func TestNoteSetRejectsUnmarkedLines(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	_, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "跑步機", Line: "客廳那台", Category: "東西", Summary: "家裡的跑步機"}, "life")
	must(t, err)
	rev := func(id string) string { return record(t, h, TopicKind, id).Revision }
	_, err = m.SetNote(ctx, &sdk.NotesSetRequest{Name: "跑步機", Revision: rev("東西/跑步機"), Body: "客廳那台（life 10-23）\n皮帶鬆了\n\n## 保養\n每月上油\n"}, "code")
	if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "第 2、5 行") {
		t.Fatalf("要列出沒標名的行號：%v", err)
	}
	_, err = m.SetNote(ctx, &sdk.NotesSetRequest{Name: "跑步機", Revision: rev("東西/跑步機"), Body: "客廳那台（life 10-23）\n主人手寫的一行\n"}, owner)
	must(t, err)
	_, err = m.SetNote(ctx, &sdk.NotesSetRequest{Name: "跑步機", Revision: rev("東西/跑步機"), Body: "主人手寫的一行\n客廳那台，皮帶鬆了（code 10-23）\n", Follow: []string{"life", "code"}}, "code")
	must(t, err)
	r := record(t, h, TopicKind, "東西/跑步機")
	if r.Body != "主人手寫的一行\n客廳那台，皮帶鬆了（code 10-23）\n" || !slices.Equal(fieldList(r, "follow"), []string{"life", "code"}) {
		t.Fatalf("set 結果：%q %v", r.Body, r.Fields["follow"])
	}
	if _, err := m.SetNote(ctx, &sdk.NotesSetRequest{Name: "跑步機", KeepBody: true, Follow: []string{"nobody"}}, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("跟進的職位要在名冊上：%v", err)
	}
}

// set 改內文一定要帶 show 看到的版本：沒帶拒絕、舊版本拒絕（別人剛加了行）、對的版本成功；只改檔頭不用帶。
func TestNoteSetNeedsRevision(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	_, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "跑步機", Line: "客廳那台", Category: "東西", Summary: "家裡的跑步機"}, "life")
	must(t, err)
	old := record(t, h, TopicKind, "東西/跑步機").Revision
	if _, err := m.SetNote(ctx, &sdk.NotesSetRequest{Name: "跑步機", Body: "客廳那台（life 10-23）\n"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "notes show") {
		t.Fatalf("沒帶版本要拒絕並說先 show：%v", err)
	}
	_, err = m.AddNote(ctx, &sdk.NotesAddRequest{Name: "跑步機", Line: "皮帶鬆了"}, "code") // 別人在中間加了一行
	must(t, err)
	_, err = m.SetNote(ctx, &sdk.NotesSetRequest{Name: "跑步機", Revision: old, Body: "客廳那台，整理過（life 10-23）\n"}, "life")
	if platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "別人改過") {
		t.Fatalf("舊版本要拒絕：%v", err)
	}
	if r := record(t, h, TopicKind, "東西/跑步機"); !strings.Contains(r.Body, "皮帶鬆了（code 10-23）") {
		t.Fatalf("被拒絕後別人的行要還在：%q", r.Body)
	}
	_, err = m.SetNote(ctx, &sdk.NotesSetRequest{Name: "跑步機", Revision: record(t, h, TopicKind, "東西/跑步機").Revision, Body: "客廳那台，整理過（life 10-23）\n皮帶鬆了（code 10-23）\n"}, "life")
	must(t, err)
	if _, err := m.SetNote(ctx, &sdk.NotesSetRequest{Name: "跑步機", KeepBody: true, Summary: "客廳的跑步機"}, "life"); err != nil {
		t.Fatalf("只改檔頭不用帶版本：%v", err)
	}
}

// 封存重複的主題：list 與兩層索引都不見、檔案搬進 archive/；「主人」與分類說明檔不能封存；
// --into 有沒併過去的行就拒絕並列出、併完（標記不同也算有）才封存；目標不存在拒絕。
func TestNoteArchive(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	add := func(name, line, by string, extra ...string) {
		t.Helper()
		req := &sdk.NotesAddRequest{Name: name, Line: line}
		if len(extra) > 0 {
			req.Category, req.Summary = extra[0], extra[1]
		}
		_, err := m.AddNote(ctx, req, by)
		must(t, err)
	}
	add("mini 開發家", "跑 EverGenie 開發", "code", "專案", "mini 的開發家")
	add("mini開發家", "跑 EverGenie 開發", "life", "專案", "mini 的開發家（重複）")
	add("mini開發家", "看板在 3000 埠", "life")
	if _, err := m.ArchiveNote(ctx, &sdk.NotesArchiveRequest{Name: "主人"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("沒有的主題要拒絕：%v", err)
	}
	add("主人", "在台北工作", owner)
	if _, err := m.ArchiveNote(ctx, &sdk.NotesArchiveRequest{Name: "主人"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "不能封存") {
		t.Fatalf("主人不能封存：%v", err)
	}
	if _, err := m.ArchiveNote(ctx, &sdk.NotesArchiveRequest{Name: "專案"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "說明檔") {
		t.Fatalf("分類說明檔不能封存：%v", err)
	}
	if _, err := m.ArchiveNote(ctx, &sdk.NotesArchiveRequest{Name: "mini開發家", Into: "不存在"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("目標不存在要拒絕：%v", err)
	}
	if _, err := m.ArchiveNote(ctx, &sdk.NotesArchiveRequest{Name: "mini開發家", Into: "mini開發家"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("不能封存進自己：%v", err)
	}
	_, err := m.ArchiveNote(ctx, &sdk.NotesArchiveRequest{Name: "mini開發家", Into: "mini 開發家"}, "life")
	if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "看板在 3000 埠") || strings.Contains(err.Error(), "跑 EverGenie 開發") {
		t.Fatalf("要列出沒併的行（標記不同的同一行不算沒併）：%v", err)
	}
	if got := record(t, h, TopicKind, "專案/mini開發家"); got.Archived {
		t.Fatal("被拒絕就不能封存")
	}
	add("mini 開發家", "看板在 3000 埠", "code")
	out, err := m.ArchiveNote(ctx, &sdk.NotesArchiveRequest{Name: "mini開發家", Into: "mini 開發家"}, "life")
	must(t, err)
	if out.Into != "mini 開發家" || !strings.Contains(out.Path, "vault/notes/專案/archive/") || !strings.HasSuffix(out.Path, "/mini開發家.md") || !strings.Contains(out.Message, out.Path) {
		t.Fatalf("回應：%+v", out)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, out.Path)); err != nil {
		t.Fatal("封存的檔案要在回報的位置：", err)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "vault/notes/專案/mini開發家.md")); !os.IsNotExist(err) {
		t.Fatal("原位置不該還有檔")
	}
	rows, err := m.topics(ctx)
	must(t, err)
	for _, r := range rows {
		if TopicName(r) == "mini開發家" {
			t.Fatal("封存後 list 不該看到")
		}
	}
	for _, rel := range []string{"vault/notes/索引.md", "vault/agents/life/索引.md"} {
		data, err := os.ReadFile(filepath.Join(h.Dir, rel))
		if err != nil || strings.Contains(string(data), "[[mini開發家]]") || (rel == "vault/notes/索引.md" && !strings.Contains(string(data), "[[mini 開發家]]")) {
			t.Fatalf("%s 要拿掉封存的主題、留著另一份：%v\n%s", rel, err, data)
		}
	}
	if _, err := m.ArchiveNote(ctx, &sdk.NotesArchiveRequest{Name: "mini開發家"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("封存過的再封存找不到：%v", err)
	}
	if _, err := m.ArchiveNote(ctx, &sdk.NotesArchiveRequest{Name: "mini 開發家"}, "stranger"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("不在名冊上的人不能封存：%v", err)
	}
	add("臨時", "沒用的主題", "life", "專案", "不要了")
	if _, err := m.ArchiveNote(ctx, &sdk.NotesArchiveRequest{Name: "臨時"}, "life"); err != nil {
		t.Fatalf("沒帶 --into 可以直接封存：%v", err)
	}
	if got := record(t, h, TopicKind, "專案/mini 開發家"); got.Archived {
		t.Fatal("留下的那份不該被動到")
	}
}

// 索引照檔頭產生、決定性：同樣的筆記產生兩次位元組一樣；職位索引只列跟進的主題，「主人」每一位都有。
func TestNoteIndexesDeterministic(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	for _, a := range []sdk.NotesAddRequest{
		{Name: "主人", Line: "叫 Amy"},
		{Name: "公司", Line: "在信義區", Category: "地方", Summary: "主人上班的地方", Follow: []string{"code"}},
		{Name: "伴侶", Line: "喜歡貓", Category: "人", Summary: "主人的伴侶", Aliases: []string{"另一半"}},
	} {
		_, err := m.AddNote(ctx, &a, "life")
		must(t, err)
	}
	must(t, m.RebuildNoteIndexes(ctx))
	read := func(rel string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(h.Dir, rel))
		must(t, err)
		return data
	}
	all, life, code := read("vault/notes/索引.md"), read("vault/agents/life/索引.md"), read("vault/agents/code/索引.md")
	must(t, m.RebuildNoteIndexes(ctx))
	if !bytes.Equal(all, read("vault/notes/索引.md")) || !bytes.Equal(life, read("vault/agents/life/索引.md")) {
		t.Fatal("同樣的筆記重產兩次要一模一樣")
	}
	cats, _ := m.categories(ctx)
	topics, _ := m.topics(ctx)
	slices.Reverse(topics) // 順序不同也要一樣
	if RenderNoteIndex(cats, topics, "") != strings.TrimPrefix(string(all), indexHeader) {
		t.Fatal("索引不看讀出來的順序")
	}
	for _, want := range []string{"核心照主題筆記的檔頭產生，別手改", "- [[伴侶]]（別名：另一半） — 主人的伴侶 · 跟進：life · 分類：人", "- [[公司]] — 主人上班的地方 · 跟進：code · 分類：地方", "- [[主人]] — " + ownerTopicSummary + " · 跟進：全部職位 · 分類：人", "## 喜好（"} {
		if !strings.Contains(string(all), want) {
			t.Fatalf("總索引少了 %q：\n%s", want, all)
		}
	}
	if strings.Contains(string(life), "[[公司]]") || !strings.Contains(string(life), "[[伴侶]]") || !strings.Contains(string(life), "[[主人]]") {
		t.Fatalf("life 的職位索引：\n%s", life)
	}
	if strings.Contains(string(code), "[[伴侶]]") || !strings.Contains(string(code), "[[公司]]") || !strings.Contains(string(code), "[[主人]]") {
		t.Fatalf("code 的職位索引：\n%s", code)
	}
	if p := m.NoteProblems(ctx); len(p) != 0 {
		t.Fatalf("索引剛產生不該有問題：%v", p)
	}
	must(t, os.WriteFile(filepath.Join(h.Dir, "vault/notes/索引.md"), []byte("手改\n"), 0600))
	if p := strings.Join(m.NoteProblems(ctx), "\n"); !strings.Contains(p, "vault/notes/索引.md 跟主題筆記對不上") {
		t.Fatalf("doctor 要報索引不符：%s", p)
	}
}

// 開場：記憶核心＋「主人」全文＋自己的職位索引；舊的 profile 不放進來。
func TestInstructionsCarryOwnerTopicAndIndex(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.WriteMemory(ctx, "life", "核心：回話短一點", "life"))
	must(t, m.WriteProfile(ctx, "舊的 profile 內容", "life"))
	_, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "主人", Line: "有兩個小孩"}, "life")
	must(t, err)
	_, err = m.AddNote(ctx, &sdk.NotesAddRequest{Name: "跑步機", Line: "客廳那台", Category: "東西", Summary: "家裡的跑步機"}, "life")
	must(t, err)
	got, err := m.Instructions(ctx, "life", "life")
	must(t, err)
	for _, want := range []string{"## 記憶\n\n核心：回話短一點", "## 主人\n\n有兩個小孩（life 10-23）", "## 職位索引\n\nlife 跟進的主題", "### 東西（", "- [[跑步機]] — 家裡的跑步機 · 跟進：life · 分類：東西"} {
		if !strings.Contains(got, want) {
			t.Fatalf("開場少了 %q", want)
		}
	}
	if strings.Contains(got, "舊的 profile 內容") || strings.Contains(got, "# life 的職位索引") {
		t.Fatal("開場不帶 profile，也不重複索引的大標題")
	}
	_ = h
}

// 新家的預設分類說明檔帶建議段落（DEV-161；只是建議，不驗證）：例「東西」＝設定／上次的感想／要注意的。
func TestDefaultCategoriesSuggestSections(t *testing.T) {
	_, h, _ := fixture(t)
	for name, want := range map[string]string{
		"人":  "建議段落：`## 他是誰`、`## 喜好與禁忌`、`## 最近的事`。",
		"東西": "建議段落：`## 設定`、`## 上次的感想`、`## 要注意的`。",
		"地方": "建議段落：`## 怎麼去`、`## 注意的事`、`## 上次去的感想`。",
		"專案": "建議段落：`## 目標`、`## 現況`、`## 決定過的事`。",
		"喜好": "建議段落：`## 喜歡`、`## 不喜歡`、`## 糾正過的`。",
	} {
		if got := record(t, h, CategoryKind, name).Body; !strings.Contains(got, want) {
			t.Fatalf("分類「%s」的說明要有 %q：\n%s", name, want, got)
		}
	}
	// 說明卡 schemas/note.md 的建議段落表（給說明檔沒有這一段的舊家看）要跟 DefaultCategories 一致。
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "ai_docs", "schemas", "note.md"))
	must(t, err)
	for _, c := range DefaultCategories {
		_, line, _ := strings.Cut(c.Body, "建議段落：")
		line, _, _ = strings.Cut(line, "。")
		names := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(line, "`## ", ""), "`", ""), "、", "／")
		if row := "| " + c.Name + " | " + names + " |"; names == "" || !strings.Contains(string(doc), row) {
			t.Errorf("schemas/note.md 的建議段落表要有 %q", row)
		}
	}
}

// 核心啟動時補上缺的官方預設分類；已經有的（主人改過的）一個字都不動。
func TestDefaultCategoriesOnlyMissing(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	for _, c := range DefaultCategories {
		record(t, h, CategoryKind, c.Name)
	}
	r := record(t, h, CategoryKind, "人")
	r.Body = "主人改過的說明\n"
	_, err := h.Vault.Put(ctx, r, owner, r.Revision)
	must(t, err)
	must(t, m.Stop(ctx))
	must(t, h.Vault.Stop(ctx))
	must(t, os.Remove(filepath.Join(h.Dir, "vault/notes/地方/地方.md")))
	store := vault.New(vault.Deps{Clock: h.Clock}) // 重開一次家（核心重新啟動）
	must(t, store.Open(h.Dir))
	t.Cleanup(func() { _ = store.Stop(context.Background()) })
	again := New(Deps{Vault: store, Clock: h.Clock, Bus: &recordingBus{}})
	again.SetHome(h.Dir)
	again.SetPersonaSource(testLibrary(h))
	must(t, again.Start(ctx))
	t.Cleanup(func() { _ = again.Stop(context.Background()) })
	get := func(id string) vault.Record {
		t.Helper()
		r, err := store.Get(ctx, CategoryKind, id)
		must(t, err)
		return r
	}
	if got := strings.TrimSpace(get("人").Body); got != "主人改過的說明" {
		t.Fatalf("主人改過的分類被動到：%q", got)
	}
	if got := get("地方"); got.Archived || fieldString(got, "kind") != "category" {
		t.Fatalf("缺的預設分類要補上：%+v", got)
	}
	data, err := os.ReadFile(filepath.Join(h.Dir, "vault/notes/東西/東西.md"))
	must(t, err)
	for _, want := range []string{"kind: category", "summary: ", "by: system"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("分類說明檔少了 %q：\n%s", want, data)
		}
	}
}

// 記憶核心超過上限（預設 120 行；舊預設 200 寫進檔頭的當成沒設）doctor 報黃字。
func TestNoteProblemsMemoryOverLimit(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	must(t, m.WriteMemory(ctx, "code", strings.Repeat("一行\n", 121), "code"))
	if p := strings.Join(m.NoteProblems(ctx), "\n"); !strings.Contains(p, "code 的記憶核心有 121 行，超過上限 120 行") {
		t.Fatalf("要報記憶核心超過上限：%s", p)
	}
}

// 不能冒名：add 送進來的行尾標記拿掉、一律標寫的人；set 新寫的行標成別人要拒絕（別人原本的行保留可以）。
func TestNoteMarksCannotImpersonate(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	out, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "跑步機", Line: "客廳那台（主人 01-01）", Category: "東西", Summary: "家裡的跑步機"}, "life")
	must(t, err)
	if out.Line != "客廳那台（life 10-23）" {
		t.Fatalf("add 要換成寫的人：%q", out.Line)
	}
	_, err = m.SetNote(ctx, &sdk.NotesSetRequest{Name: "跑步機", Revision: record(t, h, TopicKind, "東西/跑步機").Revision, Body: "客廳那台（life 10-23）\n壞了（health 10-23）\n"}, "code")
	if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "第 2 行") {
		t.Fatalf("新寫的行標成別人要拒絕：%v", err)
	}
	_, err = m.SetNote(ctx, &sdk.NotesSetRequest{Name: "跑步機", Revision: record(t, h, TopicKind, "東西/跑步機").Revision, Body: "客廳那台（life 10-23）\n壞了（code 10-23）\n"}, "code")
	must(t, err)
	if r := record(t, h, TopicKind, "東西/跑步機"); !strings.Contains(r.Body, "壞了（code 10-23）") {
		t.Fatalf("內文：%q", r.Body)
	}
}

// 主人自己同名的 索引.md（不是核心產生的）不蓋；「主人」的標題在開場降成一般文字。
func TestNoteIndexKeepsOwnerFileAndDemotesOwnerHeadings(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	mine := filepath.Join(h.Dir, "vault/agents/code/索引.md")
	must(t, os.WriteFile(mine, []byte("主人自己的筆記\n"), 0600))
	must(t, m.RebuildNoteIndexes(ctx))
	if data, _ := os.ReadFile(mine); string(data) != "主人自己的筆記\n" {
		t.Fatalf("主人的檔被蓋掉：%q", data)
	}
	if got, err := os.ReadFile(filepath.Join(h.Dir, "vault/agents/life/索引.md")); err != nil || !bytes.Contains(got, []byte("generated_by: evergenie")) {
		t.Fatalf("核心產生的照寫：%v %s", err, got)
	}
	_, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "主人", Line: "# 基本"}, owner)
	must(t, err)
	got, err := m.Instructions(ctx, "life", "life")
	must(t, err)
	if strings.Contains(got, "\n# 基本") || !strings.Contains(got, "\\# 基本") {
		t.Fatalf("「主人」的標題要降級：%s", got)
	}
}

// 主人把分類說明檔改壞了，核心照樣啟動（不蓋、不擋）。
func TestBrokenCategoryDoesNotBlockStart(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.Stop(ctx))
	must(t, h.Vault.Stop(ctx))
	bad := filepath.Join(h.Dir, "vault/notes/人/人.md")
	must(t, os.WriteFile(bad, []byte("---\nkind: [\n---\n壞掉\n"), 0600))
	store := vault.New(vault.Deps{Clock: h.Clock})
	must(t, store.Open(h.Dir))
	t.Cleanup(func() { _ = store.Stop(context.Background()) })
	again := New(Deps{Vault: store, Clock: h.Clock, Bus: &recordingBus{}})
	again.SetHome(h.Dir)
	again.SetPersonaSource(testLibrary(h))
	must(t, again.Start(ctx))
	t.Cleanup(func() { _ = again.Stop(context.Background()) })
	if data, _ := os.ReadFile(bad); !strings.Contains(string(data), "壞掉") {
		t.Fatal("壞掉的說明檔不能被蓋")
	}
}

// 職位改名：主題的 follow 換成新 id；舊位置核心產生的索引收掉、新位置重產。
func TestRenameNoteFollower(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	_, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "跑步機", Line: "客廳那台", Category: "東西", Summary: "家裡的跑步機"}, "life")
	must(t, err)
	_, err = m.MoveRecord(ctx, "agent", "life", "home", nil, owner) // agents mv 先搬名下資料，再叫收尾
	must(t, err)
	must(t, m.RenameNoteFollower(ctx, "life", "home", owner))
	if got := fieldList(record(t, h, TopicKind, "東西/跑步機"), "follow"); !slices.Equal(got, []string{"home"}) {
		t.Fatalf("follow 要換成新 id：%v", got)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "vault/agents/life/索引.md")); !os.IsNotExist(err) {
		t.Fatal("舊位置的索引要收掉")
	}
	data, err := os.ReadFile(filepath.Join(h.Dir, "vault/agents/home/索引.md"))
	if err != nil || !bytes.Contains(data, []byte("[[跑步機]]")) {
		t.Fatalf("新位置要重產：%v\n%s", err, data)
	}
}

// 封存的職位不能寫主題筆記、不能被列為跟進，也不產生職位索引。
func TestArchivedAgentAndNotes(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.ArchiveRecord(ctx, "agent", "code", owner))
	if _, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "x", Line: "y", Category: "人", Summary: "z"}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("封存的不能寫：%v", err)
	}
	if _, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "x", Line: "y", Category: "人", Summary: "z", Follow: []string{"code"}}, "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("封存的不能跟進：%v", err)
	}
	idx := filepath.Join(h.Dir, "vault/agents/code/索引.md")
	_ = os.Remove(idx)
	must(t, m.RebuildNoteIndexes(ctx))
	if _, err := os.Stat(idx); !os.IsNotExist(err) {
		t.Fatal("封存的職位不產生索引")
	}
}

// 退回遷移的收尾：只刪核心產生的索引，主人自己同名的檔留著。
func TestRemoveGeneratedNoteIndexes(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.RebuildNoteIndexes(ctx))
	mine := filepath.Join(h.Dir, "vault/agents/code/索引.md")
	must(t, os.WriteFile(mine, []byte("主人自己的\n"), 0600))
	must(t, RemoveGeneratedNoteIndexes(h.Dir))
	for _, p := range []string{"vault/notes/索引.md", "vault/agents/life/索引.md"} {
		if _, err := os.Stat(filepath.Join(h.Dir, p)); !os.IsNotExist(err) {
			t.Fatalf("%s 要刪掉", p)
		}
	}
	if data, _ := os.ReadFile(mine); string(data) != "主人自己的\n" {
		t.Fatal("主人的檔不能刪")
	}
	must(t, RemoveGeneratedNoteIndexes(t.TempDir())) // 沒有 vault 的家什麼都不做
}

// 主人自己放在 vault/notes/ 的筆記（不是主題筆記格式）：不進索引、不算壞檔、doctor 不報、不被蓋；自稱主題但欄位壞的才報。
func TestOwnerFilesInNotesAreLeftAlone(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.Stop(ctx))
	must(t, h.Vault.Stop(ctx))
	files := map[string]string{
		"vault/notes/人/日記.md":   "今天去爬山\n",
		"vault/notes/隨手.md":     "---\ntags: [x]\n---\n隨手記\n",
		"vault/notes/旅行/京都.md":  "---\ntags: [trip]\n---\n清水寺\n",
		"vault/notes/東西/壞掉的.md": "---\nname: 壞掉的\n---\n少了 summary\n",
		"vault/notes/東西/東西.md":  "主人自己寫的東西說明（沒有檔頭）\n",
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
	for p, body := range files {
		if data, _ := os.ReadFile(filepath.Join(h.Dir, p)); string(data) != body {
			t.Fatalf("%s 被改了：%q", p, data)
		}
	}
	bad := strings.Join(store.Invalid(), "\n")
	if strings.Contains(bad, "日記") || strings.Contains(bad, "隨手") || strings.Contains(bad, "京都") || strings.Contains(bad, "東西.md") {
		t.Fatalf("壞檔只算自稱主題的：%s", bad)
	}
	probs := strings.Join(again.NoteProblems(ctx), "\n")
	if strings.Contains(probs, "日記") || strings.Contains(probs, "隨手") || strings.Contains(probs, "旅行") || !strings.Contains(probs, "壞掉的") {
		t.Fatalf("doctor 只報自稱主題的：%s", probs)
	}
	if _, err := again.AddNote(ctx, &sdk.NotesAddRequest{Name: "日記", Line: "y", Category: "人", Summary: "z"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("不能蓋主人的檔：%v", err)
	}
}
