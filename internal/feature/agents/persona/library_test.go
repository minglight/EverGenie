package persona

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/agent/personaref"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

const owner = "owner(web)"

func writeCard(t *testing.T, h *homefixture.Home, rel, data string) {
	t.Helper()
	path := filepath.Join(h.Dir, rel)
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, platform.AtomicWrite(path, []byte(data), 0o644))
}

func readFile(t *testing.T, h *homefixture.Home, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.Dir, rel))
	must(t, err)
	return string(b)
}

func code(err error) sdk.ErrorCode { return platform.CodeOf(err) }

func cardOf(t *testing.T, m *Module, slug string) sdk.PersonaCard {
	t.Helper()
	c, _, _, err := m.ShowPersona(context.Background(), slug)
	must(t, err)
	return c
}

type imagesOf map[string]bool

func (i imagesOf) HasImage(slug string) bool { return i[slug] }

// 沒有頭像圖的角色卡不給 avatar_url（DEV-157）：看板直接畫名字的前兩個字，不去抓一張一定 404 的圖；有圖的照舊給。
func TestPersonaCardAvatarOnlyWithImage(t *testing.T) {
	_, lib, _, _ := fixture(t)
	lib.d.Images = imagesOf{"athena": true}
	if c := cardOf(t, lib, "athena"); c.AvatarURL != "/avatars/code.png" {
		t.Fatalf("有圖、有人坐：%+v", c)
	}
	if c := cardOf(t, lib, "new"); c.AvatarURL != "" {
		t.Fatalf("沒圖的官方卡不給網址：%+v", c)
	}
	lib.d.Images = imagesOf{}
	if c := cardOf(t, lib, "athena"); c.AvatarURL != "" || strings.Join(c.Seats, ",") != "code,life" {
		t.Fatalf("沒圖：不給網址、坐位照列：%+v", c)
	}
}

// Guards: REQ-130
func TestPersonaLookupOfficialThenMine(t *testing.T) {
	_, lib, h, _ := fixture(t)
	ctx := context.Background()
	writeCard(t, h, "personas/mine/my_mina.md", "---\nschema: 1\nname: Mina\n---\n我的角色。\n")
	writeCard(t, h, "personas/mine/athena.md", "---\nschema: 1\nname: Fake Athena\n---\n被蓋住的我的卡。\n")
	writeCard(t, h, "personas/_style.md", "共用的說話層\n") // _ 開頭不是卡
	writeCard(t, h, "personas/README.md", "說明\n")
	p, err := cardRecord(lib, "my_mina")
	must(t, err)
	if p.Fields["name"] != "Mina" {
		t.Fatal(p.Fields)
	}
	p, err = cardRecord(lib, "athena")
	must(t, err)
	if p.Fields["name"] != "Athena" {
		t.Fatalf("同一個代號官方優先：%v", p.Fields["name"])
	}
	cards, err := lib.Library(ctx)
	must(t, err)
	var order []string
	for _, c := range cards {
		order = append(order, c.Source+":"+c.Slug)
		switch {
		case c.Source == personaref.SourceMine && c.Slug == "athena":
			if !c.Shadowed || len(c.Seats) != 0 {
				t.Errorf("被蓋住的卡要標 shadowed 且不算坐位：%+v", c)
			}
		case c.Slug == "athena":
			if c.Shadowed || strings.Join(c.Seats, ",") != "code,life" || c.AvatarURL != "/avatars/code.png" {
				t.Errorf("官方 athena：%+v", c)
			}
		case c.Slug == "my_mina":
			if c.Status != personaref.StatusLive || c.Shadowed || len(c.Seats) != 0 || c.AvatarURL != "/avatars/persona/my_mina.png" {
				t.Errorf("沒人坐的我的 mina 也照代號給頭像（角色頁 feedback-1）：%+v", c)
			}
		case c.Slug == "new":
			// 沒人坐的官方角色照代號給頭像（WEB-150）。
			if len(c.Seats) != 0 || c.AvatarURL != "/avatars/persona/new.png" {
				t.Errorf("沒人坐的官方 new：%+v", c)
			}
		}
	}
	if strings.Join(order, " ") != "official:athena official:new mine:athena mine:my_mina" {
		t.Fatalf("官方在前、我的在後，各自依代號排：%v", order)
	}
	// 沒有這張卡
	if _, err := cardRecord(lib, "ghost"); code(err) != sdk.ErrorCodeInvalid {
		t.Fatal(err)
	}
	// 舊的 +++ TOML 照舊讀得到
	if c := cardOf(t, lib, "new"); c.Name != "New" || c.Aka[0] != "新人" {
		t.Fatal(c)
	}
}

// Guards: REQ-130
func TestAssignRejectsDesignPersona(t *testing.T) {
	m, lib, h, _ := fixture(t)
	ctx := context.Background()
	_, err := lib.CreatePersona(ctx, "my_hotaru", "Hotaru", nil, owner)
	must(t, err)
	if cardOf(t, lib, "my_hotaru").Status != personaref.StatusDesign {
		t.Fatal("新卡要是設計中")
	}
	err = assign(ctx, m, "code", "my_hotaru", owner)
	if code(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "還在設計中，先上線才能指派") {
		t.Fatal(err)
	}
	if err := newAgent(ctx, m, "newbie", "my_hotaru", "", owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("建秘書也不能用設計中的人設：%v", err)
	}
	// 上線後可以指派；坐著的時候不能移回設計中
	_, err = lib.SetPersonaStatus(ctx, "my_hotaru", personaref.StatusLive, owner)
	must(t, err)
	must(t, assign(ctx, m, "code", "my_hotaru", owner))
	if got := record(t, h, "agent", "code").Fields["persona"]; got != "my_hotaru" {
		t.Fatal(got)
	}
	// 設計中的卡還有秘書坐著（手動改檔造出來的）：doctor 要報
	writeCard(t, h, "personas/mine/my_hotaru.md", strings.Replace(readFile(t, h, "personas/mine/my_hotaru.md"), "status: live", "status: design", 1))
	joined := strings.Join(m.Problems(ctx), "\n")
	if !strings.Contains(joined, "code") || !strings.Contains(joined, "設計中") {
		t.Fatalf("doctor 要報坐在設計中的人設：%q", joined)
	}
}

// Guards: REQ-130
func TestPersonaCreateNameClash(t *testing.T) {
	_, lib, h, _ := fixture(t)
	ctx := context.Background()
	h.WithRecords(vault.Record{Kind: "agent", ID: "art", Fields: map[string]any{"duty": "x", "persona": "athena", "former_names": []string{"Gone"}}, Body: "職責"})
	cases := []struct {
		slug, name string
		aka        []string
		want       sdk.ErrorCode
		who        string
	}{
		{"my_hotaru", "athena", nil, sdk.ErrorCodeInvalid, "角色 athena"},                     // 跟別張卡的人名撞，不分大小寫
		{"my_hotaru", "Hotaru", []string{"雅典娜"}, sdk.ErrorCodeInvalid, "角色 athena"},         // 別名撞別名
		{"my_hotaru", "Code", nil, sdk.ErrorCodeInvalid, "職位 id code"},                      // 跟職位 id 撞
		{"my_hotaru", "gone", nil, sdk.ErrorCodeInvalid, "職位 art 以前的人名"},                    // 新增時也比以前的人名
		{"my_hotaru", "Hotaru", []string{"Builder", "new"}, sdk.ErrorCodeInvalid, "角色 new"}, // 別名跟別張卡的代號撞
		{"BadSlug", "Hotaru", nil, sdk.ErrorCodeInvalid, ""},
		{"my_hotaru", "  ", nil, sdk.ErrorCodeInvalid, ""},
	}
	for _, c := range cases {
		_, err := lib.CreatePersona(ctx, c.slug, c.name, c.aka, owner)
		if code(err) != c.want || (c.who != "" && (err == nil || !strings.Contains(err.Error(), "撞名") || !strings.Contains(err.Error(), c.who))) {
			t.Errorf("%+v → %v", c, err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "personas/mine/my_hotaru.md")); err == nil {
		t.Fatal("撞名不能寫檔")
	}
	if _, err := lib.CreatePersona(ctx, "my_hotaru", "Hotaru", []string{"小螢"}, "code"); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能新增：%v", err)
	}
	card, err := lib.CreatePersona(ctx, "my_hotaru", "Hotaru", []string{" 小螢 "}, owner)
	must(t, err)
	if card.Slug != "my_hotaru" || card.Source != personaref.SourceMine || card.Status != personaref.StatusDesign || card.Aka[0] != "小螢" {
		t.Fatalf("%+v", card)
	}
	raw := readFile(t, h, "personas/mine/my_hotaru.md")
	for _, want := range []string{"---\n", "schema: 1", "name: Hotaru", "status: design", "跟主人的關係", "## 說話"} {
		if !strings.Contains(raw, want) {
			t.Errorf("新卡缺 %q：\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "{user}") || strings.Contains(raw, "Amy") {
		t.Fatal("範本不能留 {user} 或真實人名")
	}
	// 複製：名字不能照抄
	if _, err := lib.CopyPersona(ctx, "athena", "athena2", "Athena", nil, owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("複製沿用同名：%v", err)
	}
	cp, err := lib.CopyPersona(ctx, "athena", "athena2", "Athena Two", nil, owner)
	must(t, err)
	if cp.Status != personaref.StatusDesign || cp.Source != personaref.SourceMine {
		t.Fatalf("%+v", cp)
	}
	if !strings.Contains(readFile(t, h, "personas/mine/my_athena2.md"), "人設") {
		t.Fatal("內文要複製過來")
	}
	if _, err := lib.CopyPersona(ctx, "nobody", "x1", "X1", nil, owner); code(err) != sdk.ErrorCodeNotFound {
		t.Fatal(err)
	}
}

// Guards: REQ-130
func TestPersonaUpdateOnlyMineWithLock(t *testing.T) {
	_, lib, h, _ := fixture(t)
	ctx := context.Background()
	str := func(s string) *string { return &s }
	card, err := lib.CreatePersona(ctx, "my_hotaru", "Hotaru", nil, owner)
	must(t, err)
	officialBefore := readFile(t, h, "personas/athena.md")
	if _, _, err := lib.UpdatePersona(ctx, "athena", str("X"), nil, nil, time.Now(), owner); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("官方卡唯讀：%v", err)
	}
	if _, err := lib.SetPersonaStatus(ctx, "athena", personaref.StatusDesign, owner); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("官方卡不能移到設計中：%v", err)
	}
	if readFile(t, h, "personas/athena.md") != officialBefore {
		t.Fatal("官方卡被動到")
	}
	if _, _, err := lib.UpdatePersona(ctx, "ghost", str("X"), nil, nil, time.Now(), owner); code(err) != sdk.ErrorCodeNotFound {
		t.Fatal(err)
	}
	if _, _, err := lib.UpdatePersona(ctx, "my_hotaru", str("New"), nil, nil, card.UpdatedAt, "code"); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能改：%v", err)
	}
	if _, _, err := lib.UpdatePersona(ctx, "my_hotaru", nil, nil, nil, card.UpdatedAt, owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒有要改的：%v", err)
	}
	if _, _, err := lib.UpdatePersona(ctx, "my_hotaru", str("Hotaru2"), nil, nil, time.Time{}, owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒帶 base_updated_at：%v", err)
	}
	// 檔案在讀到之後被別人改過 → conflict，不寫
	stale := card.UpdatedAt.Add(-time.Second)
	if _, _, err := lib.UpdatePersona(ctx, "my_hotaru", str("Hotaru2"), nil, nil, stale, owner); code(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "重新載入") {
		t.Fatalf("樂觀鎖：%v", err)
	}
	before := readFile(t, h, "personas/mine/my_hotaru.md")
	// 撞名不寫
	if _, _, err := lib.UpdatePersona(ctx, "my_hotaru", str("Athena"), nil, nil, card.UpdatedAt, owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("改名撞名：%v", err)
	}
	if _, _, err := lib.UpdatePersona(ctx, "my_hotaru", str(" "), nil, nil, card.UpdatedAt, owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("名字不能空白：%v", err)
	}
	if readFile(t, h, "personas/mine/my_hotaru.md") != before {
		t.Fatal("失敗不能改檔")
	}
	aka := []string{"小螢", "firefly"}
	updated, backup, err := lib.UpdatePersona(ctx, "my_hotaru", str("螢"), &aka, str("## 個性\n新的內文"), card.UpdatedAt, owner)
	must(t, err)
	if updated.Name != "螢" || strings.Join(updated.Aka, ",") != "小螢,firefly" || updated.Status != personaref.StatusDesign {
		t.Fatalf("%+v", updated)
	}
	if got := readFile(t, h, backup); got != before {
		t.Fatalf("備份要是改之前的整份檔案：\n%s", got)
	}
	if !strings.HasPrefix(backup, "runtime/backups/personas/my_hotaru-") {
		t.Fatal(backup)
	}
	raw := readFile(t, h, "personas/mine/my_hotaru.md")
	if !strings.Contains(raw, "name: 螢") || !strings.Contains(raw, "status: design") || !strings.HasSuffix(raw, "## 個性\n新的內文\n") {
		t.Fatalf("改完的檔案：\n%s", raw)
	}
	// 舊的 base_updated_at 現在過期了
	if _, _, err := lib.UpdatePersona(ctx, "my_hotaru", str("again"), nil, nil, card.UpdatedAt, owner); code(err) != sdk.ErrorCodeConflict {
		t.Fatalf("第二次用舊的時間：%v", err)
	}
	// 只改內文；清空別名要用空陣列
	none := []string{}
	again, _, err := lib.UpdatePersona(ctx, "my_hotaru", nil, &none, nil, updated.UpdatedAt, owner)
	must(t, err)
	if len(again.Aka) != 0 || again.Name != "螢" {
		t.Fatalf("%+v", again)
	}
}

// Guards: REQ-130
func TestPersonaStatusRules(t *testing.T) {
	m, lib, h, _ := fixture(t)
	ctx := context.Background()
	// closet-mine 建的最小卡：沒寫 status ＝ 上線
	writeCard(t, h, "personas/mine/myhelper.md", "---\nschema: 1\nname: Helper\n---\n一句話介紹。\n")
	if c := cardOf(t, lib, "myhelper"); c.Status != personaref.StatusLive {
		t.Fatalf("沒寫 status 是上線：%+v", c)
	}
	_, err := lib.CreatePersona(ctx, "my_hotaru", "Hotaru", nil, owner)
	must(t, err)
	// 範本還沒填完可以上線（只擋撞名與格式錯）
	c, err := lib.SetPersonaStatus(ctx, "my_hotaru", personaref.StatusLive, owner)
	must(t, err)
	if c.Status != personaref.StatusLive || !strings.Contains(readFile(t, h, "personas/mine/my_hotaru.md"), "status: live") {
		t.Fatalf("%+v", c)
	}
	if _, err := lib.SetPersonaStatus(ctx, "my_hotaru", "weird", owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatal(err)
	}
	if _, err := lib.SetPersonaStatus(ctx, "my_hotaru", personaref.StatusDesign, "life"); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能改狀態：%v", err)
	}
	// 坐著不能移回設計中
	must(t, assign(ctx, m, "code", "my_hotaru", owner))
	_, err = lib.SetPersonaStatus(ctx, "my_hotaru", personaref.StatusDesign, owner)
	if code(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "現在坐在 code，先換掉才能移到設計中") {
		t.Fatal(err)
	}
	must(t, assign(ctx, m, "code", "athena", owner))
	if c, err = lib.SetPersonaStatus(ctx, "my_hotaru", personaref.StatusDesign, owner); err != nil || c.Status != personaref.StatusDesign {
		t.Fatalf("%+v %v", c, err)
	}
	// 已經是那個狀態：不重寫
	before := readFile(t, h, "personas/mine/my_hotaru.md")
	if _, err = lib.SetPersonaStatus(ctx, "my_hotaru", personaref.StatusDesign, owner); err != nil || readFile(t, h, "personas/mine/my_hotaru.md") != before {
		t.Fatal("重複設定不該動檔案")
	}
	// 有撞名（手動改檔）就不能上線，訊息帶問題清單
	writeCard(t, h, "personas/mine/my_hotaru.md", strings.Replace(before, "name: Hotaru", "name: Athena", 1))
	_, err = lib.SetPersonaStatus(ctx, "my_hotaru", personaref.StatusLive, owner)
	if code(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "撞名") {
		t.Fatalf("撞名不能上線：%v", err)
	}
	// 格式錯
	writeCard(t, h, "personas/mine/broken.md", "沒有檔頭\n")
	if _, err = lib.SetPersonaStatus(ctx, "broken", personaref.StatusLive, owner); code(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "格式錯") {
		t.Fatalf("格式錯不能上線：%v", err)
	}
	if _, err = lib.SetPersonaStatus(ctx, "broken", personaref.StatusDesign, owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("壞卡也不能被改寫：%v", err)
	}
	if readFile(t, h, "personas/mine/broken.md") != "沒有檔頭\n" {
		t.Fatal("壞卡不能被改寫")
	}
}

// Guards: REQ-130
func TestPersonaCheckRules(t *testing.T) {
	_, lib, h, _ := fixture(t)
	ctx := context.Background()
	h.WithRecords(vault.Record{Kind: "agent", ID: "art", Fields: map[string]any{"duty": "x", "persona": "athena", "former_names": []string{"Gone"}}, Body: "職責"})
	full := "---\nschema: 1\nname: %s\naka: [%s]\n---\n%s\n"
	write := func(slug, name, aka, body string) {
		writeCard(t, h, "personas/mine/"+slug+".md", fmt.Sprintf(full, name, aka, body))
	}
	problems := func(slug string) string {
		ps, err := lib.CheckPersonas(ctx, slug)
		must(t, err)
		return strings.Join(ps, "\n")
	}
	write("clean", "Clean", "", "填好的內文")
	if got := problems("clean"); got != "" {
		t.Fatalf("乾淨的卡：%q", got)
	}
	write("nameclash", "ATHENA", "", "x")
	if got := problems("nameclash"); !strings.Contains(got, "nameclash：名字「ATHENA」跟角色 athena撞名") {
		t.Fatalf("人名撞名：%q", got)
	}
	write("akaclash", "Akaclash", "code", "x")
	if got := problems("akaclash"); !strings.Contains(got, "「code」跟職位 id code撞名") {
		t.Fatalf("別名撞職位 id：%q", got)
	}
	write("former", "Gone", "", "x")
	if got := problems("former"); got != "" {
		t.Fatalf("檢查既有的卡不比以前的人名：%q", got)
	}
	write("template", "Template", "", "- 一句話：（他是什麼樣的人；不寫做什麼工作）")
	if got := problems("template"); !strings.Contains(got, "還沒填完") {
		t.Fatalf("範本空格：%q", got)
	}
	write("blank", "Blank", "", "- 口頭禪：「」")
	if got := problems("blank"); !strings.Contains(got, "還沒填完") {
		t.Fatalf("「」空格：%q", got)
	}
	write("mentions", "Mentions", "", "她負責 `code` 的事")
	if got := problems("mentions"); !strings.Contains(got, "職位 id `code`") {
		t.Fatalf("反引號職位 id：%q", got)
	}
	write("plainword", "Plainword", "", "clean code 與 life 不算")
	if got := problems("plainword"); got != "" {
		t.Fatalf("一般英文字不算：%q", got)
	}
	writeCard(t, h, "personas/mine/noname.md", "---\nschema: 1\naka: []\n---\n內文\n")
	if got := problems("noname"); !strings.Contains(got, "noname：少了 name") {
		t.Fatalf("少 name：%q", got)
	}
	writeCard(t, h, "personas/mine/broken.md", "沒有檔頭\n")
	if got := problems("broken"); !strings.Contains(got, "broken：卡片格式錯") {
		t.Fatalf("格式錯：%q", got)
	}
	writeCard(t, h, "personas/mine/athena.md", "---\nschema: 1\nname: Other\n---\n內文\n")
	if got := problems("athena"); !strings.Contains(got, "被同代號的官方卡蓋住") {
		t.Fatalf("被官方蓋住：%q", got)
	}
	if _, err := lib.CheckPersonas(ctx, "ghost"); code(err) != sdk.ErrorCodeNotFound {
		t.Fatal(err)
	}
	if all := problems(""); !strings.Contains(all, "template：") || !strings.Contains(all, "broken：") {
		t.Fatalf("沒給代號＝全部：%q", all)
	}
}

// Guards: REQ-130
func TestStyleLevelSelection(t *testing.T) {
	m, lib, h, _ := fixture(t)
	ctx := context.Background()
	level, text, exists, levels, err := lib.StyleInfo(ctx)
	must(t, err)
	if level != "c" || exists || text != presetStyle("c") || len(levels) != 5 {
		t.Fatalf("沒設定也沒有 _style.md ＝ c：%s %v %d", level, exists, len(levels))
	}
	instr, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	if !strings.Contains(instr, "## 說話層\n\n"+presetStyle("c")) {
		t.Fatal("指令要附上 C 的全文")
	}
	// _style.md 存在 ＝ custom
	writeCard(t, h, "personas/_style.md", "我自己的說話層\n")
	if level, text, exists, _, _ = lib.StyleInfo(ctx); level != "custom" || !exists || text != "我自己的說話層" {
		t.Fatalf("沒設定但有 _style.md ＝ custom：%s %q", level, text)
	}
	for _, l := range []string{"a", "b", "c", "d"} {
		backup, err := lib.SetStyle(ctx, l, owner)
		must(t, err)
		if backup == "" || !strings.HasPrefix(backup, "runtime/backups/home/") {
			t.Fatalf("改 home.toml 要先備份：%q", backup)
		}
		got, err := m.Instructions(ctx, "code", "code")
		must(t, err)
		if !strings.Contains(got, "## 說話層\n\n"+presetStyle(l)) || strings.Contains(got, "我自己的說話層") {
			t.Fatalf("強度 %s 的指令：\n%s", l, got)
		}
		if lv, _, _, _, _ := lib.StyleInfo(ctx); lv != l {
			t.Fatal(lv)
		}
	}
	if !strings.Contains(readFile(t, h, "home.toml"), "style = \"d\"") {
		t.Fatalf("要存在 home.toml 的 [persona] style：\n%s", readFile(t, h, "home.toml"))
	}
	if _, err := lib.SetStyle(ctx, "custom", owner); err != nil {
		t.Fatal(err)
	}
	got, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	if !strings.Contains(got, "## 說話層\n\n我自己的說話層") {
		t.Fatal("custom 讀 _style.md")
	}
	// custom 但檔案不見：退回 C，doctor 報一條
	must(t, os.Remove(filepath.Join(h.Dir, "personas/_style.md")))
	got, err = m.Instructions(ctx, "code", "code")
	must(t, err)
	if !strings.Contains(got, "## 說話層\n\n"+presetStyle("c")) {
		t.Fatal("custom 檔不存在要退回 C")
	}
	if p := strings.Join(m.Problems(ctx), "\n"); !strings.Contains(p, "_style.md") {
		t.Fatalf("doctor 要報 custom 檔不在：%q", p)
	}
	if _, err := lib.SetStyle(ctx, "custom", owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒有 _style.md 不能選自己的：%v", err)
	}
	if _, err := lib.SetStyle(ctx, "e", owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatal(err)
	}
	if _, err := lib.SetStyle(ctx, "a", "code"); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能改強度：%v", err)
	}
}

// Guards: REQ-130
func TestStylePresetsHaveNoRealNames(t *testing.T) {
	m, lib, h, _ := fixture(t)
	ctx := context.Background()
	names := []string{"Amy", "amy"}
	cards, err := lib.Library(ctx)
	must(t, err)
	for _, c := range cards {
		names = append(names, c.Name)
		names = append(names, c.Aka...)
	}
	rows, err := m.List(ctx)
	must(t, err)
	for _, r := range rows {
		names = append(names, r.ID)
		names = append(names, stringsOf(r.Fields["former_names"])...)
	}
	for _, level := range []string{"a", "b", "c", "d"} {
		text := presetStyle(level)
		if text == "" {
			t.Fatalf("%s 沒有內建文字", level)
		}
		if !strings.Contains(text, "主人") {
			t.Errorf("%s 要用「主人」稱呼使用者", level)
		}
		for _, n := range names {
			if n != "" && strings.Contains(strings.ToLower(text), strings.ToLower(n)) {
				t.Errorf("強度 %s 的文字出現了名冊或使用者的名字：%q", level, n)
			}
		}
	}
	_ = h
}

func stringsOf(v any) []string { return stringList(v) }

// 角色要能編自己的小事。說話層不能再整句禁止編造，但跟主人有關的事實還是不能編；新卡範本要帶這段。
func TestDailyMomentsAllowedByStylesAndTemplate(t *testing.T) {
	for _, level := range []string{"a", "b", "c", "d"} {
		text := presetStyle(level)
		if strings.Contains(text, "真實的人生經歷") {
			t.Errorf("強度 %s 還在禁止角色編自己的故事", level)
		}
		if !strings.Contains(text, "不編造沒發生過的工作、紀錄和跟主人有關的事") {
			t.Errorf("強度 %s 拿掉了「不編造跟主人有關的事」", level)
		}
	}
	for _, want := range []string{"## 日常小事", "不取代、不竄改跟他有關的事實"} {
		if !strings.Contains(personaTemplate, want) {
			t.Errorf("新卡範本少了 %q", want)
		}
	}
	if strings.Contains(personaTemplate, "不假裝有真實人生經歷") {
		t.Error("新卡範本還留著「不假裝有真實人生經歷」")
	}
}

// 說話層四個強度都有老梗（梗本）的頻率規則（DEV-164）：一次對話最多回頭提一次、同一條一週內不重複（提了要記）、主人累或出事時不提、不為了梗改寫事實。
func TestStylesCarryQuipFrequencyRules(t *testing.T) {
	for _, level := range []string{"a", "b", "c", "d"} {
		text := presetStyle(level)
		for _, want := range []string{"梗本", "一次對話最多回頭提一次", "同一條梗一週內不重複", "notes quips used", "主人累、心情不好、事情嚴重時不提梗", "不為了梗改寫"} {
			if !strings.Contains(text, want) {
				t.Errorf("強度 %s 的說話層少了 %q", level, want)
			}
		}
	}
}

// 主人自己的角色代號一律 my_<名字>（DEV-154 定案第 3 件的檢查）：跟官方角色同代號也建得起來，兩張都在、都能指派；
// 以前建的、沒有 my_ 的自己的角色照樣讀得到。
func TestMinePersonaCodeNeverClashesWithOfficial(t *testing.T) {
	m, lib, h, _ := fixture(t)
	ctx := context.Background()
	card, err := lib.CreatePersona(ctx, "athena", "Athena Mine", nil, owner) // 官方已經有 athena
	must(t, err)
	if card.Slug != "my_athena" || card.Source != personaref.SourceMine || card.Shadowed {
		t.Fatalf("要得到 my_athena、不被官方蓋住：%+v", card)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "personas/mine/my_athena.md")); err != nil {
		t.Fatal(err)
	}
	if c := cardOf(t, lib, "athena"); c.Source != personaref.SourceOfficial || c.Name != "Athena" {
		t.Fatalf("官方那張照舊：%+v", c)
	}
	_, err = lib.SetPersonaStatus(ctx, "my_athena", personaref.StatusLive, owner)
	must(t, err)
	must(t, assign(ctx, m, "code", "my_athena", owner))
	must(t, assign(ctx, m, "life", "athena", owner))
	if name, err := m.PersonaName(ctx, "code"); err != nil || name != "Athena Mine" {
		t.Fatalf("code 坐自己的那張：%q %v", name, err)
	}
	if name, err := m.PersonaName(ctx, "life"); err != nil || name != "Athena" {
		t.Fatalf("life 坐官方那張：%q %v", name, err)
	}
	// 再建一次同代號：只跟自己的撞。
	if _, err := lib.CreatePersona(ctx, "my_athena", "Another", nil, owner); code(err) != sdk.ErrorCodeConflict {
		t.Fatalf("自己的代號重複要擋：%v", err)
	}
	for _, bad := range []string{"Bad", "a-b", "my_", strings.Repeat("a", 25)} {
		if _, err := lib.CreatePersona(ctx, bad, "X"+bad, nil, owner); code(err) != sdk.ErrorCodeInvalid {
			t.Fatalf("代號 %q 要擋：%v", bad, err)
		}
	}
	// 以前的寫法（沒有 my_）照樣讀得到。
	writeCard(t, h, "personas/mine/oldone.md", "---\nschema: 1\nname: Old One\n---\n舊的自己的角色。\n")
	if c := cardOf(t, lib, "oldone"); c.Source != personaref.SourceMine || c.Name != "Old One" {
		t.Fatalf("舊代號要讀得到：%+v", c)
	}
}

// 角色清單帶一句介紹（歡迎頁選角色用）：取內文「一句話：」那一行；沒寫、或還是新角色範本的括號說明就是空的。
// Guards: WEB-176
func TestLibraryCardCarriesTagline(t *testing.T) {
	_, lib, h, _ := fixture(t)
	writeCard(t, h, "personas/mine/my_mina.md", "---\nschema: 1\nname: Mina\n---\n## 個性\n- 一句話：安靜細心、會記得小事的人。\n- 原型：主「」\n")
	writeCard(t, h, "personas/mine/my_plain.md", "---\nschema: 1\nname: Plain\n---\n沒有寫個性。\n")
	writeCard(t, h, "personas/mine/my_draft.md", "---\nschema: 1\nname: Draft\n---\n"+personaTemplate)
	cards, err := lib.Library(context.Background())
	must(t, err)
	got := map[string]string{}
	for _, c := range cards {
		got[c.Slug] = c.Tagline
	}
	if got["my_mina"] != "安靜細心、會記得小事的人。" {
		t.Fatalf("要帶卡上的一句話：%q", got["my_mina"])
	}
	if got["my_plain"] != "" || got["my_draft"] != "" {
		t.Fatalf("沒寫或還是範本的不帶：%q %q", got["my_plain"], got["my_draft"])
	}
}
