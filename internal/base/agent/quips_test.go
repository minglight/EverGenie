package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 梗本第一次記的時候自己建（分類「喜好」、全部職位跟進）；秘書寫的那一行要有出處，日期與寫的人核心標；主人寫的不檢查出處。
func TestQuipBookCreatedOnFirstUseAndNeedsSource(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	if _, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: QuipTopicName, Line: "「人形鬧鐘」主人五點半自己醒"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "出處") {
		t.Fatalf("秘書記梗沒寫出處要拒絕：%v", err)
	}
	if _, err := m.d.Vault.Get(ctx, TopicKind, QuipTopicCategory+"/"+QuipTopicName); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("被拒絕的那次不能留下半建好的梗本：%v", err)
	}
	out, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: QuipTopicName, Line: "「人形鬧鐘」主人五點半自己醒——出處：聊天，主人笑了"}, "life")
	must(t, err)
	if !out.Created || out.Topic.ID != "喜好/梗本" || out.Line != "「人形鬧鐘」主人五點半自己醒——出處：聊天，主人笑了（life 10-23）" {
		t.Fatalf("梗本要自己建在「喜好」、行尾標寫的人與日期：%+v", out)
	}
	r := record(t, h, TopicKind, "喜好/梗本")
	if got := fieldList(r, "follow"); len(got) != 0 || fieldString(r, "summary") == "" {
		t.Fatalf("梗本算全部職位跟進（不列）、有預設的一句說明：%v %q", got, fieldString(r, "summary"))
	}
	if _, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: QuipTopicName, Line: "「泡麵大師」"}, owner); err != nil {
		t.Fatalf("主人自己寫不用出處：%v", err)
	}
}

// 家裡已經有叫「梗本」的主題（放在別的分類、或是別名）就沿用那一份，不另開。
func TestQuipBookReusesExistingTopic(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	_, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "老梗", Line: "「雨神」主人出門就下雨", Category: "東西", Summary: "家裡的老梗", Aliases: []string{QuipTopicName}}, owner)
	must(t, err)
	out, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: QuipTopicName, Line: "「泡麵大師」——出處：晚餐聊天"}, "code")
	must(t, err)
	if out.Created || out.Topic.ID != "東西/老梗" {
		t.Fatalf("要寫進已經有的那一份：%+v", out)
	}
	got, err := m.Instructions(ctx, "life", "life")
	must(t, err)
	if !strings.Contains(got, "## 梗本\n\n") || !strings.Contains(got, "- 「雨神」主人出門就下雨（主人 10-23）\n- 「泡麵大師」——出處：晚餐聊天（code 10-23）") {
		t.Fatalf("開場要帶已經有的那一份梗本：\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "vault/notes/喜好/梗本.md")); !os.IsNotExist(err) {
		t.Fatal("已經有梗本就不另開一份")
	}
}

// 每位秘書開場都帶「梗本」一節（不用自己查），內容跟 `evergenie notes quips` 回的一樣；放在「主人」之後。
func TestInstructionsCarryQuipBookForEveryAgent(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	_, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: QuipTopicName, Line: "「人形鬧鐘」主人五點半自己醒——出處：聊天"}, "life")
	must(t, err)
	q, err := m.NoteQuips(ctx, &sdk.NotesQuipsRequest{}, "code")
	must(t, err)
	if q.Total != 1 || q.Shown != 1 || !strings.Contains(q.Text, "- 「人形鬧鐘」主人五點半自己醒——出處：聊天（life 10-23）") {
		t.Fatalf("notes quips：%+v", q)
	}
	for _, id := range []string{"life", "code"} {
		got, err := m.Instructions(ctx, id, id)
		must(t, err)
		if !strings.Contains(got, "## 梗本\n\n"+q.Text+"\n\n---\n\n## 職位索引") {
			t.Fatalf("%s 的開場要帶梗本（跟 notes quips 一樣），放在職位索引前：\n%s", id, got)
		}
		if strings.Index(got, "## 主人") > strings.Index(got, "## 梗本") {
			t.Fatalf("%s：梗本放在「主人」之後", id)
		}
	}
}

// 開場帶梗有條數上限：先帶「精選」那一段，其餘帶最新的；結果照梗本原本的順序、決定性；少帶了就說一共幾條、怎麼看全部。
func TestPickQuipsLimitsPrefersPinnedAndRecent(t *testing.T) {
	var b strings.Builder
	b.WriteString("## 精選\n「精選一」——出處：a（life 10-01）\n\n## 其他\n")
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "「梗%02d」——出處：b（life 10-%02d）\n", i, i)
	}
	p := PickQuips(b.String(), nil)
	if p.Total != 21 || len(p.Lines) != QuipShowLines {
		t.Fatalf("最多帶 %d 條：%d／%d", QuipShowLines, len(p.Lines), p.Total)
	}
	if !strings.HasPrefix(p.Lines[0], "「精選一」") || !strings.HasPrefix(p.Lines[1], "「梗14」") || !strings.HasPrefix(p.Lines[len(p.Lines)-1], "「梗20」") {
		t.Fatalf("精選先、其餘帶最新的，照原順序：%v", p.Lines)
	}
	again := PickQuips(b.String(), nil)
	if strings.Join(again.Lines, "\n") != strings.Join(p.Lines, "\n") {
		t.Fatal("同樣的梗本要挑出一樣的")
	}
	sec := QuipSection(p)
	if !strings.Contains(sec, "梗本一共 21 條，這裡帶 8 條（精選與最近的）") || !strings.Contains(sec, "`evergenie notes show 梗本`") || strings.Contains(sec, "## 精選") {
		t.Fatalf("少帶了要說一共幾條、怎麼看全部；梗本裡的標題不進開場：\n%s", sec)
	}
}

// 開場帶梗也守字數上限：太長的略過，照樣從最新的往前帶。
func TestPickQuipsKeepsRuneLimit(t *testing.T) {
	long := strings.Repeat("長", 500)
	p := PickQuips("「a」"+long+"——出處：x（life 10-01）\n「b」"+long+"——出處：x（life 10-02）\n「c」"+long+"——出處：x（life 10-03）\n", nil)
	runes := 0
	for _, l := range p.Lines {
		runes += len([]rune(l))
	}
	if len(p.Lines) != 2 || runes > QuipShowRunes || !strings.HasPrefix(p.Lines[0], "「b」") {
		t.Fatalf("字數上限 %d：帶了 %d 條、%d 字：%v", QuipShowRunes, len(p.Lines), runes, p.Lines)
	}
}

// 梗本還沒有梗時，開場那一節也說怎麼記第一條。
func TestQuipSectionEmptyBookSaysHowToAdd(t *testing.T) {
	if sec := QuipSection(PickQuips("", nil)); !strings.Contains(sec, "梗本還沒有梗") || !strings.Contains(sec, "evergenie notes add 梗本") {
		t.Fatalf("沒有梗也要說怎麼記：\n%s", sec)
	}
}

// addQuips 在梗本裡記幾條（life 寫，帶出處）。
func addQuips(t *testing.T, m *Module, lines ...string) {
	t.Helper()
	for _, l := range lines {
		_, err := m.AddNote(context.Background(), &sdk.NotesAddRequest{Name: QuipTopicName, Line: l}, "life")
		must(t, err)
	}
}

// notes quips used 對梗：「」裡剛好是這句的那條算數；對不到 NOT_FOUND、對到好幾條 CONFLICT 並列出；不在名冊上的不能記。
func TestQuipUsedMatchesOneQuip(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	addQuips(t, m, "「人形鬧鐘」主人五點半自己醒——出處：聊天", "「泡麵大師」最拿手的是泡麵——出處：晚餐", "「泡麵」不是料理——出處：吐槽")
	if _, err := m.QuipUsed(ctx, &sdk.NotesQuipUsedRequest{Text: "火鍋"}, "code"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("對不到要回 NOT_FOUND：%v", err)
	}
	if out, err := m.QuipUsed(ctx, &sdk.NotesQuipUsedRequest{Text: "泡麵"}, "code"); err != nil || out.Quip != "「泡麵」不是料理——出處：吐槽" {
		t.Fatalf("「」裡剛好是這句的那條算數：%+v %v", out, err)
	}
	if _, err := m.QuipUsed(ctx, &sdk.NotesQuipUsedRequest{Text: "出處"}, "code"); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "人形鬧鐘") {
		t.Fatalf("對到好幾條要回 CONFLICT 並列出來：%v", err)
	}
	if _, err := m.QuipUsed(ctx, &sdk.NotesQuipUsedRequest{Text: "鬧鐘"}, "stranger"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("不在名冊上的不能記：%v", err)
	}
}

// 提過的梗這一週不帶進任何人的開場（誰提的都算），notes quips 列出來；過了一週又回來。
func TestQuipUsedRestsForAWeek(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	addQuips(t, m, "「人形鬧鐘」主人五點半自己醒——出處：聊天", "「泡麵大師」最拿手的是泡麵——出處：晚餐")
	out, err := m.QuipUsed(ctx, &sdk.NotesQuipUsedRequest{Text: "鬧鐘"}, "code")
	must(t, err)
	if out.Quip != "「人形鬧鐘」主人五點半自己醒——出處：聊天" || !out.Until.Equal(h.Clock.Now().Add(7*24*time.Hour)) {
		t.Fatalf("回應：%+v", out)
	}
	got, err := m.Instructions(ctx, "life", "life")
	must(t, err)
	if strings.Contains(got, "人形鬧鐘") || !strings.Contains(got, "「泡麵大師」") || !strings.Contains(got, "這週提過的 1 條先不帶") {
		t.Fatalf("這週提過的不帶進開場：\n%s", got)
	}
	q, err := m.NoteQuips(ctx, &sdk.NotesQuipsRequest{}, owner)
	must(t, err)
	if q.Total != 2 || q.Shown != 1 || len(q.Resting) != 1 {
		t.Fatalf("notes quips 要列出這週提過的：%+v", q)
	}
	h.Clock.Advance(7*24*time.Hour + time.Minute)
	got, err = m.Instructions(ctx, "life", "life")
	must(t, err)
	if !strings.Contains(got, "「人形鬧鐘」") || strings.Contains(got, "先不帶") {
		t.Fatalf("過了一週又帶回開場：\n%s", got)
	}
}

// 提過的梗紀錄壞掉不擋開場，當成沒提過。
func TestBrokenQuipUsesDoNotBlockOpening(t *testing.T) {
	m, _, _ := fixture(t)
	addQuips(t, m, "「人形鬧鐘」主人五點半自己醒——出處：聊天")
	must(t, os.MkdirAll(filepath.Dir(m.quipUsesPath()), 0o700))
	must(t, os.WriteFile(m.quipUsesPath(), []byte("{壞掉"), 0600))
	got, err := m.Instructions(context.Background(), "life", "life")
	if err != nil || !strings.Contains(got, "「人形鬧鐘」") {
		t.Fatalf("紀錄壞掉要照樣開場、當成沒提過：%v", err)
	}
}

// 梗本跟「主人」一樣全部職位跟進：總索引寫「全部職位」、每個職位索引都列到它。
func TestQuipBookFollowedByEveryAgent(t *testing.T) {
	m, h, _ := fixture(t)
	addQuips(t, m, "「人形鬧鐘」主人五點半自己醒——出處：聊天")
	index, err := os.ReadFile(filepath.Join(h.Dir, "vault/notes/索引.md"))
	must(t, err)
	if !strings.Contains(string(index), "[[梗本]] — "+quipTopicSummary+" · 跟進：全部職位") {
		t.Fatalf("總索引：\n%s", index)
	}
	for _, id := range []string{"life", "code"} {
		pos, err := os.ReadFile(filepath.Join(h.Dir, "vault/agents", id, "索引.md"))
		if err != nil || !strings.Contains(string(pos), "[[梗本]]") {
			t.Fatalf("%s 的職位索引要列到梗本：%v\n%s", id, err, pos)
		}
	}
}

// 梗本是固定主題，不能封存（封存了下次記梗會默默開一份空的）。
func TestQuipBookCannotBeArchived(t *testing.T) {
	m, _, _ := fixture(t)
	addQuips(t, m, "「人形鬧鐘」主人五點半自己醒——出處：聊天")
	if _, err := m.ArchiveNote(context.Background(), &sdk.NotesArchiveRequest{Name: QuipTopicName}, owner); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("梗本不能封存：%v", err)
	}
}

// 用別名叫「梗本」的既有主題，用它本來的名字記也要有出處。
func TestQuipBookAliasStillNeedsSource(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	_, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "老梗", Line: "「雨神」主人出門就下雨", Category: "東西", Summary: "家裡的老梗", Aliases: []string{QuipTopicName}}, owner)
	must(t, err)
	if _, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: "老梗", Line: "「泡麵大師」"}, "life"); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "出處") {
		t.Fatalf("用原名記梗也要出處：%v", err)
	}
}

// 測口吻的情境可以自己帶梗本：開場的梗本段換成情境寫的，家裡的梗本不出現。
func TestTestInstructionsUseScenarioQuips(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	_, err := m.AddNote(ctx, &sdk.NotesAddRequest{Name: QuipTopicName, Line: "「家裡的梗」——出處：聊天"}, "life")
	must(t, err)
	got, err := m.TestInstructions(ctx, "code", "athena", "", "", []string{"「情境的梗」——出處：測試（life 11-02）"})
	must(t, err)
	if !strings.Contains(got, "- 「情境的梗」——出處：測試（life 11-02）") || strings.Contains(got, "家裡的梗") {
		t.Fatalf("梗本段要換成情境寫的：\n%s", got)
	}
	got, err = m.TestInstructions(ctx, "code", "athena", "", "", nil)
	must(t, err)
	if !strings.Contains(got, "家裡的梗") {
		t.Fatal("沒帶就照家裡的梗本")
	}
}
