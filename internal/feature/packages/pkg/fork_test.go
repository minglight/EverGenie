package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

const officialNotes = "[[extension]]\nid = \"greeting\"\nkind = \"prompt_append\"\ndescription = \"開場多說一句\"\n"

// forkNotes：官方 notes 1.0.0（README 與 agent 樣板），fork 進 mine。
func forkNotes(t *testing.T) (*harness, string) {
	t.Helper()
	h := newHarness(t)
	h.write(sdk.TierOfficial, "notes", dataPkg("notes", "1.0.0", officialNotes), map[string]string{
		"README.md":             "# 筆記\n第一行\n第二行\n第三行\n",
		"agents/notes/agent.md": "你是筆記秘書。\n",
		"old.md":                "要被刪掉的檔\n",
	})
	h.start()
	if _, err := h.m.Fork(t.Context(), &sdk.PackageForkRequest{ID: "notes", By: &sdk.Actor{Name: "owner(web)"}}); err != nil {
		t.Fatal(err)
	}
	return h, filepath.Join(h.home.Dir, "packages", "mine", "notes")
}

// TestForkRecordsBase：fork 整包複製進 packages/mine/，[fork] 記下基準版本與時間，基準內容留一份；官方資料夾不動。
// Guards: REQ-95
func TestForkRecordsBase(t *testing.T) {
	h, mine := forkNotes(t)
	p := h.get("notes")
	if p.Tier != sdk.TierMine || p.Fork == nil || p.Fork.Of != "notes" || p.Fork.BaseVersion != "1.0.0" || !p.Fork.ForkedAt.Equal(h.home.Clock.Now()) || p.State != sdk.PackageStateRunning {
		t.Fatalf("fork 之後：%+v fork=%+v", p, p.Fork)
	}
	for _, rel := range []string{"README.md", "agents/notes/agent.md", ".fork-base/package.toml", ".fork-base/README.md"} {
		if !exists(filepath.Join(mine, rel)) {
			t.Errorf("少了 %s", rel)
		}
	}
	base, _ := os.ReadFile(filepath.Join(mine, ".fork-base", "package.toml"))
	if strings.Contains(string(base), "[fork]") {
		t.Fatal("基準內容要是官方原樣")
	}
	off, _ := os.ReadFile(filepath.Join(h.official, "notes", "package.toml"))
	if strings.Contains(string(off), "[fork]") {
		t.Fatal("官方資料夾不能被改")
	}
	if _, err := h.m.Fork(t.Context(), &sdk.PackageForkRequest{ID: "notes"}); codeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("已經 fork 過要 CONFLICT：%v", err)
	}
	if _, err := h.m.Fork(t.Context(), &sdk.PackageForkRequest{ID: "ghost"}); codeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("沒有的官方套件要 NOT_FOUND：%v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(h.home.Dir, "packages", "mine")); len(entries) != 1 {
		t.Fatalf("不能留下暫存資料夾：%v", entries)
	}
	if r, err := h.m.ListUpdates(t.Context(), &sdk.PackageListUpdatesRequest{ID: "notes"}); err != nil || len(r.Items) != 0 || r.ReportPath != "" {
		t.Fatalf("官方沒變就沒有差異：%+v %v", r, err)
	}
}

// officialUpdate：官方 notes 出 1.1.0：改了 README、改了 agent 樣板、刪掉 old.md、新增 jobs/daily.md、拿掉擴充點；使用者在 fork 裡也改了 agent 樣板。
func officialUpdate(t *testing.T, h *harness, mine string) {
	t.Helper()
	h.write(sdk.TierOfficial, "notes", dataPkg("notes", "1.1.0", "[[extension]]\nid = \"footer\"\nkind = \"setting\"\n"), map[string]string{
		"README.md":             "# 筆記\n第一行\n第二行（改了）\n第三行\n",
		"agents/notes/agent.md": "你是筆記秘書，回答要短。\n",
		"jobs/daily.md":         "每天整理\n",
	})
	if err := os.Remove(filepath.Join(h.official, "notes", "old.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mine, "agents", "notes", "agent.md"), []byte("你是我的筆記秘書。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestUpdateReportContents：官方更新後，差異逐檔一項（新增／刪掉／改了、版本與擴充點、逐行差異、使用者也改過的標出來），
// 並寫一份給 AI 的差異清單檔（說明核心不會自己合併）。
// Guards: REQ-95
func TestUpdateReportContents(t *testing.T) {
	h, mine := forkNotes(t)
	officialUpdate(t, h, mine)
	r, err := h.m.ListUpdates(t.Context(), &sdk.PackageListUpdatesRequest{ID: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	if r.BaseVersion != "1.0.0" || r.LatestVersion != "1.1.0" {
		t.Fatalf("版本：%+v", r)
	}
	byPath := map[string]sdk.UpdateItem{}
	for _, it := range r.Items {
		byPath[it.Path] = it
	}
	if len(r.Items) != 5 {
		t.Fatalf("要 5 項：%+v", r.Items)
	}
	checks := []struct {
		path, summary string
		conflict      bool
	}{
		{"README.md", "官方改了 README.md", false},
		{"agents/notes/agent.md", "官方改了 agents/notes/agent.md", true},
		{"old.md", "官方刪掉了 old.md", false},
		{"jobs/daily.md", "官方新增了 jobs/daily.md", false},
		{"package.toml", "版本 1.0.0 → 1.1.0", false},
	}
	for _, c := range checks {
		it, ok := byPath[c.path]
		if !ok || !strings.Contains(it.Summary, c.summary) || it.ConflictsWithFork != c.conflict {
			t.Errorf("%s：%+v", c.path, it)
		}
	}
	if s := byPath["package.toml"].Summary; !strings.Contains(s, "新增擴充點 footer") || !strings.Contains(s, "拿掉擴充點 greeting") {
		t.Errorf("package.toml 要寫擴充點增減：%s", s)
	}
	if d := byPath["README.md"].Diff; !strings.Contains(d, "- 第二行") || !strings.Contains(d, "+ 第二行（改了）") || !strings.Contains(d, "  第一行") {
		t.Errorf("逐行差異：\n%s", d)
	}
	if r.ReportPath != "runtime/packages/notes/updates/1.0.0-to-1.1.0.md" {
		t.Fatalf("report_path = %q", r.ReportPath)
	}
	b, err := os.ReadFile(filepath.Join(h.home.Dir, r.ReportPath))
	if err != nil {
		t.Fatal(err)
	}
	report := string(b)
	for _, want := range []string{"給 AI", "核心不會自己合併", "一項一項問使用者", "基準版本：1.0.0", "官方新版：1.1.0", "5 項（其中 1 項", "使用者在 fork 裡也改過這個檔", "~~~~diff"} {
		if !strings.Contains(report, want) {
			t.Errorf("差異清單檔少了「%s」：\n%s", want, report)
		}
	}
	if _, err := h.m.ListUpdates(t.Context(), &sdk.PackageListUpdatesRequest{ID: "ghost"}); codeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("不是 fork 的要 NOT_FOUND：%v", err)
	}
}

// TestMineSurvivesOfficialUpdate：官方更新不碰 packages/mine/：fork 的內容不變、照樣是有效的那一包；只多一句「官方有新版」的提醒。
// 官方套件整個拿掉時，fork 照常可用，只是不再有更新。
// Guards: REQ-95
func TestMineSurvivesOfficialUpdate(t *testing.T) {
	h, mine := forkNotes(t)
	officialUpdate(t, h, mine)
	before, _ := readTree(mine)
	if _, err := h.m.Reload(t.Context(), &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.ListUpdates(t.Context(), &sdk.PackageListUpdatesRequest{ID: "notes"}); err != nil {
		t.Fatal(err)
	}
	after, _ := readTree(mine)
	if len(before) != len(after) {
		t.Fatalf("mine 的檔案數變了：%d → %d", len(before), len(after))
	}
	for k, v := range before {
		if string(after[k]) != string(v) {
			t.Errorf("mine 的 %s 被改了", k)
		}
	}
	p := h.get("notes")
	if p.Tier != sdk.TierMine || p.Version != "1.0.0" || p.State != sdk.PackageStateRunning {
		t.Fatalf("官方更新後仍用自己的：%+v", p)
	}
	w := strings.Join(h.m.Warnings(), "|")
	if !strings.Contains(w, "官方出了新版 1.0.0 → 1.1.0") {
		t.Fatalf("要提醒官方有新版：%s", w)
	}
	if err := os.RemoveAll(filepath.Join(h.official, "notes")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Reload(t.Context(), &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	r, err := h.m.ListUpdates(t.Context(), &sdk.PackageListUpdatesRequest{ID: "notes"})
	if err != nil || r.BaseVersion != "1.0.0" || r.LatestVersion != "" || len(r.Items) != 0 {
		t.Fatalf("官方拿掉後：%+v %v", r, err)
	}
	if p := h.get("notes"); p.State != sdk.PackageStateRunning || p.Tier != sdk.TierMine {
		t.Fatalf("官方拿掉後 fork 照常：%+v", p)
	}
}
