package charter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 一段的新內容帶了同級的標題（會把後面的內容切到段外）或沒收尾的程式碼區塊（後面的標題全變成程式碼）：存檔與提案都擋，檔案不動。
func TestSectionTextThatBreaksBoundariesIsRejected(t *testing.T) {
	r := defaultRig(t)
	cur := r.get(t, "purpose")
	bad := map[string]string{
		"同級標題":   "讓主人少操心。\n\n## 附則\n\n偷渡",
		"更高級標題":  "讓主人少操心。\n\n# 大標\n",
		"沒收尾的區塊": "讓主人少操心。\n\n```\n還沒收尾",
	}
	for name, text := range bad {
		if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "purpose", Text: text, Revision: cur.Doc.Revision}, "owner(web)"); code(err) != sdk.ErrorCodeInvalid {
			t.Errorf("%s：存檔要回 invalid：%v", name, err)
		}
		if _, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "purpose", Text: text, Why: "y"}, "code"); code(err) != sdk.ErrorCodeInvalid {
			t.Errorf("%s：提案要回 invalid：%v", name, err)
		}
	}
	// 提案只改法律裡的一條（### 級）：內容裡不能再有 ### 或更高級的標題。
	if _, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "law", Section: "第 2 條 檔案是正本", Text: "檔案為準。\n\n### 第 3 條 偷渡", Why: "y"}, "code"); code(err) != sdk.ErrorCodeInvalid {
		t.Errorf("一條的提案帶同級標題要擋：%v", err)
	}
	if got := r.read(t, "docs/projects/evergenie.md"); got != project {
		t.Fatalf("被擋的改動不能動到檔案：%q", got)
	}
	if h, _ := r.m.History(ctx, "purpose"); len(h) != 0 {
		t.Fatalf("被擋的存檔不能留版本：%+v", h)
	}
	// 比這一段低級的小標題可以。
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "purpose", Text: "讓主人少操心。\n\n### 細項\n\n```\n## 程式碼裡的不算\n```", Revision: cur.Doc.Revision}, "owner(web)"); err != nil {
		t.Fatalf("低級小標題與收尾的程式碼區塊可以：%v", err)
	}
}

// 宗旨與法律在同一個檔：改其中一段，另一段（和中間的「現在在跑的」）逐字不變。
func TestSavingOneSectionLeavesOtherSectionsUnchanged(t *testing.T) {
	r := defaultRig(t)
	law := r.get(t, "law").Text
	cur := r.get(t, "purpose")
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "purpose", Text: "宗旨改過了。", Revision: cur.Doc.Revision}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if got := r.get(t, "law").Text; got != law {
		t.Fatalf("法律被動到了：%q", got)
	}
	file := r.read(t, "docs/projects/evergenie.md")
	if !strings.Contains(file, "## Repos\n\n- EverGenie\n") || !strings.Contains(file, "## 宗旨\n\n宗旨改過了。\n") {
		t.Fatalf("其他段要留著：%q", file)
	}
}

// 一份讀不到（路徑是資料夾）：其他份照常列出，那一份標原因；健檢黃字。
func TestOneUnreadableDocDoesNotHideOthers(t *testing.T) {
	r := defaultRig(t)
	if err := os.Remove(filepath.Join(r.h.Dir, "docs/constitution.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(r.h.Dir, "docs/constitution.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	list, err := r.m.List(ctx)
	if err != nil || len(list.Docs) != 3 {
		t.Fatalf("List = %+v %v", list, err)
	}
	if list.Docs[0].Problem == "" || list.Docs[1].Problem != "" || !list.Docs[1].Exists {
		t.Fatalf("只有讀不到的那份標原因：%+v", list.Docs)
	}
}

// 寫檔失敗：回 unavailable、檔案不變；下次存檔先補一版「改之前的樣子」（現在的內容），不會掉內容。
func TestWriteFailureKeepsCurrentTextRecoverable(t *testing.T) {
	r := defaultRig(t)
	dir := filepath.Join(r.h.Dir, "docs")
	cur := r.get(t, "constitution")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	_, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "constitution", Text: "寫不進去", Revision: cur.Doc.Revision}, "owner(web)")
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if code(err) != sdk.ErrorCodeUnavailable {
		t.Fatalf("寫不進去回 unavailable：%v", err)
	}
	if got := r.read(t, "docs/constitution.md"); got != constitution {
		t.Fatalf("檔案不變：%q", got)
	}
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "constitution", Text: "這次寫得進去", Revision: cur.Doc.Revision}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	h, _ := r.m.History(ctx, "constitution")
	if len(h) != 4 || h[1].Source != sdk.RulesVersionSourceOutside {
		t.Fatalf("寫檔失敗後下次存檔要先補現在的樣子：%+v", h)
	}
	v, err := r.m.Version(ctx, h[1].ID, "")
	if err != nil || v.Version.Text != strings.TrimRight(constitution, "\n") {
		t.Fatalf("補的那版是原本的全文：%+v %v", v, err)
	}
}

// 文件是符號連結：寫到它指到的檔、連結留著、檔案權限不變。
func TestSymlinkedDocWritesTargetAndKeepsMode(t *testing.T) {
	r := newRig(t, "[[rules.docs]]\nid = \"house\"\ntitle = \"家規\"\npath = \"docs/house.md\"\n", nil)
	target := filepath.Join(t.TempDir(), "real.md")
	if err := os.WriteFile(target, []byte("# 家規\n\n舊的\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(r.h.Dir, "docs/house.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	cur := r.get(t, "house")
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "house", Text: "# 家規\n\n新的", Revision: cur.Doc.Revision}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("連結要留著：%v %v", fi, err)
	}
	data, _ := os.ReadFile(target)
	fi, _ := os.Stat(target)
	if string(data) != "# 家規\n\n新的\n" || fi.Mode().Perm() != 0o600 {
		t.Fatalf("寫到指到的檔、權限不變：%q %v", data, fi.Mode())
	}
}

// 採用時文件改好了、但提案狀態沒記上：列提案時照版本補記成已採用並收掉通知，不會卡在「過期、只能退回」。
func TestAcceptedProposalStatusIsHealedFromVersion(t *testing.T) {
	r := defaultRig(t)
	p, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "purpose", Text: "新的宗旨", Why: "y"}, "code")
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.h.Vault.NextID(ctx, versionKind)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.h.Vault.Put(ctx, vault.Record{Kind: versionKind, ID: id, Body: "新的宗旨", Fields: map[string]any{
		"kind": versionKind, "doc": "purpose", "title": "宗旨", "source": "proposal", "proposal": p.ID, "created_at": time.Now().Format(time.RFC3339Nano),
	}}, "owner(web)", ""); err != nil {
		t.Fatal(err)
	}
	list, err := r.m.Proposals(ctx, "purpose", nil)
	if err != nil || len(list) != 1 || list[0].Status != sdk.RulesProposalStatusAccepted || list[0].Version != id || list[0].DecidedBy != "owner(web)" {
		t.Fatalf("補記成已採用：%+v %v", list, err)
	}
	if len(r.notify.retired) != 1 || r.notify.retired[0] != p.Notice {
		t.Fatalf("通知跟著收掉：%+v", r.notify.retired)
	}
}

// 篩選狀態只認 pending、accepted、rejected；備註有長度上限。
func TestUnknownStatusAndLongNoteAreRejected(t *testing.T) {
	r := defaultRig(t)
	if _, err := r.m.Proposals(ctx, "", []sdk.RulesProposalStatus{"done"}); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("不認得的狀態：%v", err)
	}
	cur := r.get(t, "purpose")
	long := strings.Repeat("字", MaxNoteRunes+1)
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "purpose", Text: "x", Revision: cur.Doc.Revision, Note: long}, "owner(web)"); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("備註太長：%v", err)
	}
	if _, err := r.m.Decide(ctx, &sdk.RulesDecideRequest{ID: "RP-1", Note: long}, "owner(web)"); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("決定的備註太長：%v", err)
	}
}

// 路徑：~ 只能接 /；不能放進家的 vault/、runtime/。
func TestConfigRejectsOddHomePathsAndSystemFolders(t *testing.T) {
	for name, path := range map[string]string{"~x": "~x/../../a.md", "vault": "vault/a.md", "runtime": "./runtime/a.md"} {
		r := newRig(t, "[[rules.docs]]\nid=\"a\"\ntitle=\"A\"\npath=\""+path+"\"\n", nil)
		if list, err := r.m.List(ctx); err != nil || list.Problem == "" || len(list.Docs) != 0 {
			t.Errorf("%s：%+v %v", name, list, err)
		}
	}
}
