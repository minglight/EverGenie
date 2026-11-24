package charter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

var ctx = context.Background()

type fakeBus struct {
	mu   sync.Mutex
	sent []*sdk.Envelope
}

func (b *fakeBus) Send(_ context.Context, e *sdk.Envelope) (*sdk.Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, e)
	return e, nil
}

type fakeNotify struct {
	mu      sync.Mutex
	sent    []*sdk.Notification
	retired []string
}

func (n *fakeNotify) Send(_ context.Context, in *sdk.Notification) (*sdk.Notification, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := *in
	out.ID = "M-" + string(rune('0'+len(n.sent)+1))
	n.sent = append(n.sent, &out)
	return &out, nil
}
func (n *fakeNotify) Retire(_ context.Context, ids []string, _ string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.retired = append(n.retired, ids...)
	return nil
}

const constitution = "# 憲法\n\n1. 主人說了算。\n2. 能退回的直接做。\n"

const project = `# EverGenie

## 宗旨

把秘書做好。

## Repos

- EverGenie

## 法律

### 第 1 條 秘書怎麼做事

叫就做。

### 第 2 條 檔案是正本

檔案為準。
`

type rig struct {
	m      *Module
	h      *homefixture.Home
	notify *fakeNotify
	bus    *fakeBus
}

func newRig(t *testing.T, extra string, files map[string]string) *rig {
	t.Helper()
	h := homefixture.NewWith(t, extra)
	for rel, body := range files {
		p := filepath.Join(h.Dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, b := &fakeNotify{}, &fakeBus{}
	m := New(Deps{Vault: h.Vault, Notify: n, Bus: b, Clock: h.Clock})
	m.SetHome(h.Dir)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })
	return &rig{m: m, h: h, notify: n, bus: b}
}

// 預設的家：作者正式家的放法（docs/constitution.md、docs/projects/evergenie.md 的宗旨與法律）。
func defaultRig(t *testing.T) *rig {
	return newRig(t, "", map[string]string{"docs/constitution.md": constitution, "docs/projects/evergenie.md": project})
}

func (r *rig) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.h.Dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (r *rig) get(t *testing.T, doc string) *sdk.RulesGetResponse {
	t.Helper()
	got, err := r.m.Get(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func code(err error) sdk.ErrorCode { return platform.CodeOf(err) }

func TestModuleShape(t *testing.T) {
	m := New(Deps{})
	if m.Name() != "charter" {
		t.Fatal(m.Name())
	}
	var _ app.RouteProvider = m
	var _ app.CheckProvider = m
	if err := m.Start(ctx); err != nil {
		t.Fatal("沒有家時 Start 不做事：", err)
	}
}

// 沒寫 [rules] 時照預設位置讀：憲法整份、宗旨與法律各取 evergenie.md 裡那一段，位階照順序。
func TestDefaultDocsReadConstitutionPurposeAndLawSections(t *testing.T) {
	r := defaultRig(t)
	list, err := r.m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if list.Problem != "" || len(list.Docs) != 3 {
		t.Fatalf("List = %+v", list)
	}
	for i, want := range []string{"constitution", "purpose", "law"} {
		if d := list.Docs[i]; d.ID != want || d.Rank != i+1 || !d.Exists || d.Revision == "" {
			t.Fatalf("第 %d 份 = %+v", i+1, d)
		}
	}
	if got := r.get(t, "purpose").Text; got != "把秘書做好。" {
		t.Fatalf("宗旨只取那一段：%q", got)
	}
	law := r.get(t, "law").Text
	if !strings.HasPrefix(law, "### 第 1 條") || !strings.Contains(law, "檔案為準。") || strings.Contains(law, "Repos") {
		t.Fatalf("法律取到下一個同級標題為止：%q", law)
	}
}

// 檔案不在（例：家裡沒有 docs/）：頁面是空的、不是錯誤，健檢也不是黃字或紅字。
func TestMissingDocsShowEmptyStateWithoutError(t *testing.T) {
	r := newRig(t, "", nil)
	list, err := r.m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if list.Problem != "" || len(list.Docs) != 3 {
		t.Fatalf("List = %+v", list)
	}
	for _, d := range list.Docs {
		if d.Exists || d.Revision != "" {
			t.Fatalf("不在的文件要標成不存在：%+v", d)
		}
	}
	got := r.get(t, "law")
	if got.Doc.Exists || got.Text != "" {
		t.Fatalf("Get = %+v", got)
	}
	if res := r.m.checkDocs(ctx); res.Status != app.StatusOK {
		t.Fatalf("預設位置沒有檔案不提醒：%+v", res)
	}
	if h, err := r.m.History(ctx, "law"); err != nil || len(h) != 0 {
		t.Fatalf("沒有版本：%v %v", h, err)
	}
}

// 只有主人能直接改：秘書存檔回 403、檔案不動、不留版本；主人存檔成功。
func TestOnlyOwnerCanEditRules(t *testing.T) {
	r := defaultRig(t)
	cur := r.get(t, "constitution")
	_, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "constitution", Text: "# 憲法\n\n秘書說了算。", Revision: cur.Doc.Revision}, "code")
	if code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書直接改要被擋：%v", err)
	}
	if got := r.read(t, "docs/constitution.md"); got != constitution {
		t.Fatalf("被擋的存檔不能改到檔案：%q", got)
	}
	if h, _ := r.m.History(ctx, "constitution"); len(h) != 0 {
		t.Fatalf("被擋的存檔不能留版本：%+v", h)
	}
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "constitution", Text: "# 憲法\n\n主人改過。", Revision: cur.Doc.Revision}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if got := r.read(t, "docs/constitution.md"); got != "# 憲法\n\n主人改過。\n" {
		t.Fatalf("主人存檔寫進檔案：%q", got)
	}
}

// 秘書不能改，但可以送修改提案：文件不動、提案待決、送一則安靜的（digest）通知給主人。
func TestSecretaryEditBlockedButCanPropose(t *testing.T) {
	r := defaultRig(t)
	cur := r.get(t, "law")
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "law", Text: "全部重寫", Revision: cur.Doc.Revision}, "code"); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書存檔要被擋：%v", err)
	}
	p, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "law", Section: "第 2 條 檔案是正本", Text: "檔案為準，看板只是窗口。", Why: "上次搞混看板跟檔案"}, "code")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "RP-1" || p.Status != sdk.RulesProposalStatusPending || p.From != "code" || p.Base != cur.Doc.Revision || p.Notice == "" {
		t.Fatalf("提案 = %+v", p)
	}
	if got := r.read(t, "docs/projects/evergenie.md"); got != project {
		t.Fatalf("提案不改文件：%q", got)
	}
	if len(r.notify.sent) != 1 {
		t.Fatalf("通知主人一次：%+v", r.notify.sent)
	}
	n := r.notify.sent[0]
	if n.Category != sdk.CategoryDigest || n.Agent != "code" || !strings.Contains(n.Title, "法律") || n.Context != "上次搞混看板跟檔案" || !strings.Contains(n.URL, "RP-1") {
		t.Fatalf("通知 = %+v", n)
	}
	list, err := r.m.Proposals(ctx, "law", []sdk.RulesProposalStatus{sdk.RulesProposalStatusPending})
	if err != nil || len(list) != 1 || list[0].Stale || len(list[0].Diff) == 0 {
		t.Fatalf("待決提案附差異：%+v %v", list, err)
	}
	if d := r.get(t, "law").Doc; d.Pending != 1 {
		t.Fatalf("文件上看得到待決數：%+v", d)
	}
	if _, err := r.m.Decide(ctx, &sdk.RulesDecideRequest{ID: p.ID, Accept: true}, "code"); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能採用自己的提案：%v", err)
	}
}

// 存檔留版本：第一次改先留「改之前的樣子」，再留新版；還原成舊版後檔案回到原本、又多一版。
func TestSaveKeepsVersionThatCanBeRestored(t *testing.T) {
	r := defaultRig(t)
	cur := r.get(t, "constitution")
	saved, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "constitution", Text: "# 憲法\n\n1. 主人說了算。\n", Revision: cur.Doc.Revision, Note: "刪掉第 2 條"}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version == nil || saved.Version.Source != sdk.RulesVersionSourceSave || saved.Version.Note != "刪掉第 2 條" || saved.Version.By != "owner(web)" {
		t.Fatalf("新版 = %+v", saved.Version)
	}
	h, err := r.m.History(ctx, "constitution")
	if err != nil || len(h) != 2 || h[0].ID != saved.Version.ID || h[1].Source != sdk.RulesVersionSourceOutside {
		t.Fatalf("版本 = %+v %v", h, err)
	}
	original := h[1].ID
	v, err := r.m.Version(ctx, saved.Version.ID, "")
	if err != nil || v.Against != original {
		t.Fatalf("跟前一版比：%+v %v", v, err)
	}
	if !hasLine(v.Diff, sdk.RulesDiffOpDel, "2. 能退回的直接做。") {
		t.Fatalf("差異看得到刪掉的行：%+v", v.Diff)
	}
	back, err := r.m.Restore(ctx, &sdk.RulesRestoreRequest{ID: original, Revision: saved.Doc.Revision}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.read(t, "docs/constitution.md"); got != constitution {
		t.Fatalf("還原後檔案回到原本：%q", got)
	}
	if back.Version == nil || back.Version.Source != sdk.RulesVersionSourceRestore || back.Version.RestoredFrom != original {
		t.Fatalf("還原也留一版：%+v", back.Version)
	}
	if h, _ := r.m.History(ctx, "constitution"); len(h) != 3 {
		t.Fatalf("還原不刪任何版本：%+v", h)
	}
	if _, err := r.m.Restore(ctx, &sdk.RulesRestoreRequest{ID: original}, "code"); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能還原：%v", err)
	}
}

func hasLine(diff []sdk.RulesDiffLine, op sdk.RulesDiffOp, text string) bool {
	for _, l := range diff {
		if l.Op == op && l.Text == text {
			return true
		}
	}
	return false
}

// 採用提案留版本：只換提案那一段、其他段與標題不動；提案記成已採用、通知收掉、結果回給提案的秘書。
func TestAcceptProposalKeepsVersion(t *testing.T) {
	r := defaultRig(t)
	p, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "law", Section: "第 1 條 秘書怎麼做事", Text: "叫就做，做完回報。", Why: "常忘了回報"}, "code")
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.m.Decide(ctx, &sdk.RulesDecideRequest{ID: p.ID, Accept: true, Note: "好"}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(project, "叫就做。", "叫就做，做完回報。", 1)
	if got := r.read(t, "docs/projects/evergenie.md"); got != want {
		t.Fatalf("只換那一段：\n%s", got)
	}
	if out.Version == nil || out.Version.Source != sdk.RulesVersionSourceProposal || out.Version.Proposal != p.ID {
		t.Fatalf("採用留一版：%+v", out.Version)
	}
	if out.Proposal.Status != sdk.RulesProposalStatusAccepted || out.Proposal.Version != out.Version.ID || out.Proposal.DecidedBy != "owner(web)" || out.Proposal.Note != "好" {
		t.Fatalf("提案 = %+v", out.Proposal)
	}
	if h, _ := r.m.History(ctx, "law"); len(h) != 2 || h[1].Source != sdk.RulesVersionSourceOutside {
		t.Fatalf("先留採用前的樣子：%+v", h)
	}
	if len(r.notify.retired) != 1 || r.notify.retired[0] != p.Notice {
		t.Fatalf("提案的通知收掉：%+v", r.notify.retired)
	}
	if len(r.bus.sent) != 1 || r.bus.sent[0].To != "code" || r.bus.sent[0].Kind != sdk.EnvelopeKindEcho || !strings.Contains(r.bus.sent[0].Body, "採用") {
		t.Fatalf("結果回給秘書：%+v", r.bus.sent)
	}
	if _, err := r.m.Decide(ctx, &sdk.RulesDecideRequest{ID: p.ID, Accept: false}, "owner(web)"); code(err) != sdk.ErrorCodeConflict {
		t.Fatalf("決定過的不能再決定：%v", err)
	}
}

// 退回提案：文件不動、不留版本，秘書收到退回與主人的話。
func TestRejectProposalLeavesDocUnchanged(t *testing.T) {
	r := defaultRig(t)
	p, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "purpose", Text: "隨便做。", Why: "想偷懶"}, "life")
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.m.Decide(ctx, &sdk.RulesDecideRequest{ID: p.ID, Accept: false, Note: "不行"}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != nil || out.Proposal.Status != sdk.RulesProposalStatusRejected {
		t.Fatalf("退回 = %+v", out)
	}
	if got := r.read(t, "docs/projects/evergenie.md"); got != project {
		t.Fatalf("退回不改文件：%q", got)
	}
	if h, _ := r.m.History(ctx, "purpose"); len(h) != 0 {
		t.Fatalf("退回不留版本：%+v", h)
	}
	if len(r.bus.sent) != 1 || !strings.Contains(r.bus.sent[0].Body, "退回") || !strings.Contains(r.bus.sent[0].Body, "不行") {
		t.Fatalf("秘書收到退回：%+v", r.bus.sent)
	}
}

// 提案之後文件改過了：列表標「過期」，不能採用（要退回請秘書照新版重送），文件不被舊提案蓋掉。
func TestStaleProposalCannotBeAccepted(t *testing.T) {
	r := defaultRig(t)
	p, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "purpose", Text: "把秘書做得更好。", Why: "更積極"}, "code")
	if err != nil {
		t.Fatal(err)
	}
	cur := r.get(t, "purpose")
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "purpose", Text: "主人先改了。", Revision: cur.Doc.Revision}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	list, _ := r.m.Proposals(ctx, "purpose", nil)
	if len(list) != 1 || !list[0].Stale {
		t.Fatalf("文件改過的提案標過期：%+v", list)
	}
	if _, err := r.m.Decide(ctx, &sdk.RulesDecideRequest{ID: p.ID, Accept: true}, "owner(web)"); code(err) != sdk.ErrorCodeConflict {
		t.Fatalf("過期的提案不能採用：%v", err)
	}
	if got := r.get(t, "purpose").Text; got != "主人先改了。" {
		t.Fatalf("文件維持主人改的：%q", got)
	}
}

// 存檔帶的版本不是現在的（別的分頁或在電腦上改過）：回衝突，不蓋掉。
func TestSaveWithOldRevisionConflicts(t *testing.T) {
	r := defaultRig(t)
	cur := r.get(t, "constitution")
	if err := os.WriteFile(filepath.Join(r.h.Dir, "docs/constitution.md"), []byte("# 憲法\n\n在電腦上改的。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "constitution", Text: "看板改的", Revision: cur.Doc.Revision}, "owner(web)"); code(err) != sdk.ErrorCodeConflict {
		t.Fatalf("舊版本存檔要衝突：%v", err)
	}
	if got := r.read(t, "docs/constitution.md"); got != "# 憲法\n\n在電腦上改的。\n" {
		t.Fatalf("不蓋掉別處的修改：%q", got)
	}
}

// 在看板外改過（編輯器、git）：下次經看板改之前，先把那時的樣子留成一版，外面改的內容不會沒有紀錄。
func TestOutsideEditIsKeptBeforeNextSave(t *testing.T) {
	r := defaultRig(t)
	cur := r.get(t, "constitution")
	first, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "constitution", Text: "# 憲法\n\n第一版。", Revision: cur.Doc.Revision}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.h.Dir, "docs/constitution.md"), []byte("# 憲法\n\n外面改的。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cur = r.get(t, "constitution")
	if cur.Doc.Revision == first.Doc.Revision {
		t.Fatal("外面改過，版本雜湊要跟著變")
	}
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "constitution", Text: "# 憲法\n\n第三版。", Revision: cur.Doc.Revision}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	h, _ := r.m.History(ctx, "constitution")
	if len(h) != 4 || h[1].Source != sdk.RulesVersionSourceOutside {
		t.Fatalf("版本 = %+v", h)
	}
	v, err := r.m.Version(ctx, h[1].ID, "")
	if err != nil || v.Version.Text != "# 憲法\n\n外面改的。" {
		t.Fatalf("外面改的那一版：%+v %v", v, err)
	}
}

// 內容一樣的存檔不留版本、不動檔案。
func TestSaveSameTextIsNoop(t *testing.T) {
	r := defaultRig(t)
	cur := r.get(t, "law")
	out, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "law", Text: cur.Text + "\n\n", Revision: cur.Doc.Revision}, "owner(web)")
	if err != nil || out.Version != nil {
		t.Fatalf("沒變不留版本：%+v %v", out, err)
	}
	if h, _ := r.m.History(ctx, "law"); len(h) != 0 {
		t.Fatalf("版本 = %+v", h)
	}
}

// 文件不在時主人存檔：建出資料夾與檔案；一段的文件會在檔裡加上那一段的標題。
func TestSaveCreatesMissingFileWithSection(t *testing.T) {
	r := newRig(t, "", nil)
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "law", Text: "### 第 1 條\n\n新的。"}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if got := r.read(t, "docs/projects/evergenie.md"); got != "## 法律\n\n### 第 1 條\n\n新的。\n" {
		t.Fatalf("新檔 = %q", got)
	}
	if h, _ := r.m.History(ctx, "law"); len(h) != 1 || h[0].Source != sdk.RulesVersionSourceSave {
		t.Fatalf("原本不在就只留新版：%+v", h)
	}
}

// 提案的檢查：要理由、內容、文件裡有那個標題；主人不用提案；每位秘書最多 10 個待決。
func TestProposeValidation(t *testing.T) {
	r := defaultRig(t)
	cases := map[string]*sdk.RulesProposeRequest{
		"why":     {Doc: "law", Text: "x"},
		"text":    {Doc: "law", Why: "y"},
		"section": {Doc: "law", Section: "第 9 條", Text: "x", Why: "y"},
	}
	for field, req := range cases {
		if _, err := r.m.Propose(ctx, req, "code"); code(err) != sdk.ErrorCodeInvalid {
			t.Errorf("%s 不對要回 invalid：%v", field, err)
		}
	}
	if _, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "nope", Text: "x", Why: "y"}, "code"); code(err) != sdk.ErrorCodeNotFound {
		t.Errorf("沒有這份文件：%v", err)
	}
	if _, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "law", Text: "x", Why: "y"}, "owner(web)"); code(err) != sdk.ErrorCodeInvalid {
		t.Errorf("主人不用提案：%v", err)
	}
	for i := 0; i < MaxPendingPerSender; i++ {
		if _, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "purpose", Text: "版本 " + string(rune('a'+i)), Why: "y"}, "code"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "purpose", Text: "再一個", Why: "y"}, "code"); code(err) != sdk.ErrorCodeConflict {
		t.Fatalf("超過待決上限：%v", err)
	}
	if _, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "purpose", Text: "別人的", Why: "y"}, "life"); err != nil {
		t.Fatalf("上限是每位秘書自己的：%v", err)
	}
}

// home.toml 寫了 [[rules.docs]]：照它的順序與標題；寫的檔案不在 → 黃字（不是紅字）、頁面照樣能開。
func TestExplicitConfigMissingFileWarnsNotError(t *testing.T) {
	r := newRig(t, "[[rules.docs]]\nid = \"house\"\ntitle = \"家規\"\npath = \"docs/house.md\"\n", nil)
	list, err := r.m.List(ctx)
	if err != nil || list.Problem != "" || len(list.Docs) != 1 || list.Docs[0].Title != "家規" || list.Docs[0].Exists {
		t.Fatalf("List = %+v %v", list, err)
	}
	if res := r.m.checkDocs(ctx); res.Status != app.StatusWarn || !strings.Contains(res.Message, "docs/house.md") {
		t.Fatalf("明寫的檔案不在是黃字：%+v", res)
	}
}

// 設定寫錯（id 重複、跑出家的路徑、不認得的欄位）：頁面看得到原因、文件清單是空的，健檢黃字。
func TestBadConfigShowsProblemAndWarns(t *testing.T) {
	bad := map[string]string{
		"重複":   "[[rules.docs]]\nid=\"a\"\ntitle=\"A\"\npath=\"a.md\"\n[[rules.docs]]\nid=\"a\"\ntitle=\"B\"\npath=\"b.md\"\n",
		"跑出去":  "[[rules.docs]]\nid=\"a\"\ntitle=\"A\"\npath=\"../a.md\"\n",
		"欄位":   "[[rules.docs]]\nid=\"a\"\ntitle=\"A\"\npath=\"a.md\"\nrank=1\n",
		"不是md": "[[rules.docs]]\nid=\"a\"\ntitle=\"A\"\npath=\"a.txt\"\n",
	}
	for name, extra := range bad {
		r := newRig(t, extra, nil)
		list, err := r.m.List(ctx)
		if err != nil || list.Problem == "" || len(list.Docs) != 0 {
			t.Errorf("%s：%+v %v", name, list, err)
		}
		if res := r.m.checkDocs(ctx); res.Status != app.StatusWarn {
			t.Errorf("%s 健檢要黃字：%+v", name, res)
		}
	}
}

// 版本存在 vault/rules/ 底下：重開（重新掃描）之後還認得是規矩的版本與提案。
func TestVersionsSurviveRestart(t *testing.T) {
	r := defaultRig(t)
	cur := r.get(t, "constitution")
	if _, err := r.m.Save(ctx, &sdk.RulesSaveRequest{Doc: "constitution", Text: "新的", Revision: cur.Doc.Revision}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Propose(ctx, &sdk.RulesProposeRequest{Doc: "purpose", Text: "提案", Why: "y"}, "code"); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Vault.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	store := vault.New(vault.Deps{Clock: r.h.Clock})
	if err := store.Open(r.h.Dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Stop(ctx) })
	m := New(Deps{Vault: store, Clock: r.h.Clock})
	m.SetHome(r.h.Dir)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h, err := m.History(ctx, "constitution"); err != nil || len(h) != 2 {
		t.Fatalf("重開後版本還在：%+v %v", h, err)
	}
	if p, err := m.Proposals(ctx, "", nil); err != nil || len(p) != 1 {
		t.Fatalf("重開後提案還在：%+v %v", p, err)
	}
}
