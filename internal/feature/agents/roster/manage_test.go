package roster

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/agent/personafixture"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// 名冊管理的規則（manage.go）接真的底層 agent（測試家＋測試角色庫）；排程用假的 jobs（job 的擁有者是 jobs，業務模組之間不 import）。

type quietBus struct {
	mu     sync.Mutex
	events []string
}

func (b *quietBus) Publish(_ context.Context, topic string, p map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, topic+":"+p["type"].(string)+":"+p["id"].(string))
	return nil
}
func (b *quietBus) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return nil, nil
}

// fakeJobs 是 jobs 的「秘書名下的排程」：記住有哪些排程與每次呼叫；failMove 不是空的時，搬那一筆回錯。
type fakeJobs struct {
	ids      []string
	agentOf  map[string]string
	calls    []string
	failMove string
}

func (j *fakeJobs) AgentJobIDs(_ context.Context, a string) ([]string, error) {
	var out []string
	for _, id := range j.ids {
		if strings.HasPrefix(id, a+"/") {
			out = append(out, id)
		}
	}
	return out, nil
}
func (j *fakeJobs) ArchiveAgentJob(_ context.Context, id, by string) error {
	j.calls = append(j.calls, "archive "+id)
	j.ids = slices.DeleteFunc(j.ids, func(x string) bool { return x == id })
	return nil
}
func (j *fakeJobs) MoveAgentJob(_ context.Context, from, to, a, by string) error {
	if from == j.failMove {
		return platform.Conflict("搬不動")
	}
	j.calls = append(j.calls, "move "+from+" "+to+" "+a)
	for i, id := range j.ids {
		if id == from {
			j.ids[i] = to
		}
	}
	j.agentOf[to] = a
	return nil
}

func fixtureManage(t *testing.T) (*Module, *homefixture.Home, *fakeJobs) {
	t.Helper()
	h := homefixture.New(t)
	roster := agent.New(agent.Deps{Vault: h.Vault, Clock: h.Clock, Bus: &quietBus{}})
	roster.SetHome(h.Dir)
	roster.SetPersonaSource(personafixture.New(h.Dir))
	must(t, roster.Start(context.Background()))
	t.Cleanup(func() { _ = roster.Stop(context.Background()) })
	for slug, data := range map[string]string{"athena": "---\nname: Athena\naka: [雅典娜]\n---\n人設\n", "new": "+++\nname = \"New\"\naka = [\"新人\"]\n+++\n新的人設\n"} {
		must(t, os.MkdirAll(filepath.Join(h.Dir, "personas"), 0700))
		must(t, platform.AtomicWrite(filepath.Join(h.Dir, "personas", slug+".md"), []byte(data), 0600))
	}
	for _, id := range []string{"code", "life"} {
		h.WithRecords(vault.Record{Kind: "agent", ID: id, Fields: map[string]any{"duty": "測試職責", "persona": "athena", "former_names": []string{"Old" + id}}, Body: "官方職責"})
	}
	jobs := &fakeJobs{ids: []string{"code/morning", "life/night"}, agentOf: map[string]string{}}
	m := New(Deps{Roster: roster, Jobs: jobs, Clock: h.Clock})
	m.SetHome(h.Dir)
	must(t, m.Start(context.Background()))
	return m, h, jobs
}

// base 是接在 Deps.Roster 的真的底層 agent（測試要用它的 WriteAddendum、WriteDuties）。
func base(m *Module) *agent.Module { return m.d.Roster.(*agent.Module) }

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func record(t *testing.T, h *homefixture.Home, kind, id string) vault.Record {
	t.Helper()
	r, err := h.Vault.Get(context.Background(), kind, id)
	must(t, err)
	return r
}

// Guards: REQ-162
func TestAgentsCreateRules(t *testing.T) {
	m, h, _ := fixtureManage(t)
	ctx := context.Background()
	for _, bad := range []string{"X", "a", "1abc", "has_underscore", "UPPER", strings.Repeat("a", 32)} {
		if _, err := m.Create(ctx, bad, "athena", "", "", nil, "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("id %q 應該被拒絕：%v", bad, err)
		}
	}
	if _, err := m.Create(ctx, "code", "athena", "", "", nil, "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("已存在要回 conflict：%v", err)
	}
	if _, err := m.Create(ctx, "newbie", "athena", "", "", nil, "life"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能建秘書：%v", err)
	}
	if _, err := m.Create(ctx, "newbie", "nobody", "", "", nil, "owner(cli)"); err == nil {
		t.Fatal("人設不存在要拒絕")
	}
	r, err := m.Create(ctx, "newbie", "athena", "新來的", "", nil, "owner(cli)")
	must(t, err)
	if r.Fields["enabled"] != false || r.Fields["display_name"] != "新來的" || r.Fields["duty"] != "（還沒寫）" {
		t.Fatalf("新秘書的預設：%+v", r.Fields)
	}
	if mem := record(t, h, "memory", "newbie"); mem.Body != "" {
		t.Fatalf("記憶要是空的：%q", mem.Body)
	}
}

// 建的是套件帶來的職位（官方 health 這種）而且沒給職責：用樣板的一句話職責，不顯示「（還沒寫）」（DEV-91）。
type fakeTemplates map[string]string

func (f fakeTemplates) TemplateDuty(id string) string { return f[id] }
func (f fakeTemplates) TemplateDutiesRef(id string) string {
	if f[id] == "" {
		return ""
	}
	return "official/" + id
}

func TestAgentsCreateUsesTemplateDuty(t *testing.T) {
	m, _, _ := fixtureManage(t)
	m.d.Templates = fakeTemplates{"health": "健康（運動、飲食、身體紀錄）"}
	ctx := context.Background()
	r, err := m.Create(ctx, "health", "athena", "", "", nil, "owner(cli)")
	must(t, err)
	if r.Fields["duty"] != "健康（運動、飲食、身體紀錄）" {
		t.Fatalf("要用樣板職責：%+v", r.Fields)
	}
	if r.Fields["duties_from"] != "official/health" || r.Body != "" { // 官方套件帶來的職位：職責跟著程式走，家裡不抄一份（DEV-154）
		t.Fatalf("官方職位要記 duties_from、內文留空：%+v %q", r.Fields, r.Body)
	}
	r, err = m.Create(ctx, "plain", "athena", "", "", nil, "owner(cli)")
	must(t, err)
	if _, has := r.Fields["duties_from"]; r.Fields["duty"] != "（還沒寫）" || has {
		t.Fatalf("沒有樣板照舊：%+v", r.Fields)
	}
	r, err = m.Create(ctx, "custom", "athena", "", "自己寫的", nil, "owner(cli)")
	must(t, err)
	if r.Fields["duty"] != "自己寫的" {
		t.Fatalf("有給職責用給的：%+v", r.Fields)
	}
}

// Guards: REQ-162, REQ-5
func TestAgentsArchiveKeepsEverything(t *testing.T) {
	m, h, jobs := fixtureManage(t)
	ctx := context.Background()
	must(t, m.WriteMemory(ctx, "code", "要留下的記憶", "code"))
	must(t, base(m).WriteAddendum(ctx, "code", "規則", "owner(cli)"))
	done, restore, err := m.Archive(ctx, "code", "owner(cli)")
	must(t, err)
	if len(done) < 3 || restore == "" {
		t.Fatalf("封存：%v %q", done, restore)
	}
	// 排程經 jobs 封存、而且最先封存；秘書本身最後；別人的排程不動。
	if done[0] != "job/code/morning" || done[len(done)-1] != "agent/code" || strings.Join(jobs.calls, ",") != "archive code/morning" {
		t.Fatalf("封存順序：%v；jobs：%v", done, jobs.calls)
	}
	if _, _, err := m.Archive(ctx, "code", "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("已封存的再封存：%v", err)
	}
	if got := record(t, h, "memory", "code"); !got.Archived || got.Body != "要留下的記憶" {
		t.Fatalf("記憶要封存而不是刪除：%+v", got)
	}
	if _, err := m.Create(ctx, "code", "athena", "", "", nil, "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("封存過的 id 不能直接重建：%v", err)
	}
}

// Guards: REQ-162
func TestAgentsRenameMovesBelongings(t *testing.T) {
	m, h, jobs := fixtureManage(t)
	ctx := context.Background()
	must(t, m.WriteMemory(ctx, "code", "記憶", "code"))
	must(t, base(m).WriteAddendum(ctx, "code", "規則", "owner(cli)"))
	must(t, os.WriteFile(filepath.Join(h.Dir, "home.toml"), append(mustRead(t, filepath.Join(h.Dir, "home.toml")), []byte("# 提到 code 的備註\n")...), 0o600))
	r, moved, mentions, err := m.Rename(ctx, "code", "dev", "開發", "owner(cli)")
	must(t, err)
	if r.ID != "dev" || r.Fields["display_name"] != "開發" || len(moved) < 3 {
		t.Fatalf("改名：%+v %v", r, moved)
	}
	if got := record(t, h, "memory", "dev"); got.Body != "記憶" {
		t.Fatalf("記憶要跟著搬：%+v", got)
	}
	if got := record(t, h, "addendum", "dev"); got.Body != "規則" {
		t.Fatalf("附加規則要跟著搬：%+v", got)
	}
	if !slices.Contains(moved, "job/dev/morning") || jobs.agentOf["dev/morning"] != "dev" {
		t.Fatalf("排程經 jobs 搬到新 id、agent 欄位改成新 id：%v %v", moved, jobs.calls)
	}
	if got, err := h.Vault.Get(ctx, "agent", "code"); err == nil && !got.Archived {
		t.Fatalf("舊 id 不該還在：%+v", got)
	}
	found := false
	for _, f := range mentions {
		found = found || f == "home.toml"
	}
	if !found {
		t.Fatalf("還提到舊 id 的檔案要列出來：%v", mentions)
	}
	if _, _, _, err := m.Rename(ctx, "dev", "life", "", "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("新 id 已被用：%v", err)
	}
}

// 改名中途搬排程失敗：已經搬過去的名冊資料全部搬回舊 id（含 display_name），新 id 不留東西。
func TestAgentsRenameRollsBackWhenJobMoveFails(t *testing.T) {
	m, h, jobs := fixtureManage(t)
	ctx := context.Background()
	must(t, m.WriteMemory(ctx, "code", "記憶", "code"))
	jobs.failMove = "code/morning"
	if _, _, _, err := m.Rename(ctx, "code", "dev", "開發", "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("排程搬不動要回它的錯：%v", err)
	}
	if got := record(t, h, "agent", "code"); got.Archived || got.Fields["display_name"] != "" {
		t.Fatalf("名冊要搬回舊 id、display_name 改回原值：%+v", got)
	}
	if got := record(t, h, "memory", "code"); got.Body != "記憶" {
		t.Fatalf("記憶要搬回舊 id：%+v", got)
	}
	for _, kind := range []string{"agent", "memory"} {
		if _, err := h.Vault.Get(ctx, kind, "dev"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
			t.Fatalf("新 id 不該留下 %s：%v", kind, err)
		}
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	return b
}

// Guards: REQ-41, REQ-162
func TestAgentsSwapIsAtomic(t *testing.T) {
	m, h, _ := fixtureManage(t)
	ctx := context.Background()
	must(t, m.Assign(ctx, "life", "new", "owner(cli)"))
	rows, err := m.Swap(ctx, "code", "life", "owner(cli)")
	must(t, err)
	if len(rows) != 2 || record(t, h, "agent", "code").Fields["persona"] != "new" || record(t, h, "agent", "life").Fields["persona"] != "athena" {
		t.Fatalf("互換後：%+v", rows)
	}
	// 兩位同一個角色：互換等於沒有變，要報錯而且兩筆都不動。
	must(t, m.Assign(ctx, "code", "athena", "owner(cli)"))
	before := record(t, h, "agent", "code").Revision
	if _, err := m.Swap(ctx, "code", "life", "owner(cli)"); err == nil {
		t.Fatal("同一個角色互換要報錯")
	}
	if record(t, h, "agent", "code").Revision != before {
		t.Fatal("失敗時不該改到任何一位")
	}
	if _, err := m.Swap(ctx, "code", "code", "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("自己跟自己：%v", err)
	}
}

// Guards: REQ-41, REQ-162
func TestAssignRecordsFormerName(t *testing.T) {
	m, h, _ := fixtureManage(t)
	ctx := context.Background()
	if err := m.Assign(ctx, "code", "athena", "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("同一個角色要報錯：%v", err)
	}
	if err := m.Assign(ctx, "code", "nobody", "owner(cli)"); err == nil {
		t.Fatal("角色不存在要報錯")
	}
	mem := filepath.Join(h.Dir, "vault/agents/code/memory.md")
	must(t, m.WriteMemory(ctx, "code", "記憶", "code"))
	before := mustRead(t, mem)
	must(t, m.Assign(ctx, "code", "new", "owner(cli)"))
	if string(mustRead(t, mem)) != string(before) {
		t.Fatal("換角色不能動到記憶")
	}
	if former := stringList(record(t, h, "agent", "code").Fields["former_names"]); !slices.Contains(former, "Athena") {
		t.Fatalf("舊的顯示名稱要進 former_names：%v", former)
	}
}

// Guards: REQ-162
func TestAgentsSetFieldWhitelist(t *testing.T) {
	m, h, _ := fixtureManage(t)
	ctx := context.Background()
	for _, f := range []string{"persona", "id", "former_names", "account", "sites"} {
		if _, err := m.SetField(ctx, "code", f, "x", "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("%s 不在白名單：%v", f, err)
		}
	}
	if _, err := m.SetField(ctx, "code", "enabled", "maybe", "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("布林寫錯：%v", err)
	}
	if _, err := m.SetField(ctx, "code", "color", "red", "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("顏色寫錯：%v", err)
	}
	if _, err := m.SetField(ctx, "code", "duty", "x", "life"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能改名冊：%v", err)
	}
	_, err := m.SetField(ctx, "code", "budget.max_runs_per_day", "7", "owner(cli)")
	must(t, err)
	_, err = m.SetField(ctx, "code", "model", "opus", "owner(cli)")
	must(t, err)
	r := record(t, h, "agent", "code")
	if b, _ := r.Fields["budget"].(map[string]any); b["max_runs_per_day"] == nil || r.Fields["model"] != "opus" {
		t.Fatalf("欄位沒寫進去：%+v", r.Fields)
	}
	_, err = m.SetField(ctx, "code", "effort", "high", "owner(cli)")
	must(t, err)
	_, err = m.SetField(ctx, "code", "effort", "", "owner(cli)")
	must(t, err)
	if _, has := record(t, h, "agent", "code").Fields["effort"]; has {
		t.Fatal("空字串要拿掉選填欄位")
	}
}

// Guards: REQ-162
func TestAgentsSetAccount(t *testing.T) {
	m, h, _ := fixtureManage(t)
	ctx := context.Background()
	if _, err := m.SetAccount(ctx, "code", "ghost", "", "", "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒有這個帳號：%v", err)
	}
	if _, _, err := platform.SetHomeValue(h.Dir, "accounts.second", `{engine="test-engine"}`, h.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r, err := m.SetAccount(ctx, "code", "second", "sonnet", "high", "owner(cli)")
	must(t, err)
	if r.Fields["account"] != "second" || r.Fields["model"] != "sonnet" || r.Fields["effort"] != "high" {
		t.Fatalf("%+v", r.Fields)
	}
}

// TestCustomizeSurvivesUpdate：使用者交代的規則（customize add）在官方職責更新之後原封不動，而且排在新職責之後。
// Guards: REQ-46, REQ-48, REQ-163
func TestCustomizeSurvivesUpdate(t *testing.T) {
	m, h, _ := fixtureManage(t)
	ctx := context.Background()
	if _, err := m.AddRule(ctx, "code", "", "   ", "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("空規則：%v", err)
	}
	if _, err := m.AddRule(ctx, "code", "", "別人說的", "life"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("別的秘書不能加：%v", err)
	}
	_, err := m.AddRule(ctx, "code", "", "先講結論。", "owner(cli)")
	must(t, err)
	body, err := m.AddRule(ctx, "code", "", "不要用表情符號。", "code")
	must(t, err)
	if !strings.Contains(body, "先講結論。") || strings.Index(body, "先講結論。") > strings.Index(body, "不要用表情符號。") {
		t.Fatalf("新規則要附加在尾端、舊的不動：%q", body)
	}
	before := record(t, h, "addendum", "code")
	must(t, base(m).WriteDuties(ctx, "code", "更新後的官方職責", "system"))
	after := record(t, h, "addendum", "code")
	if after.Body != before.Body || after.Revision != before.Revision {
		t.Fatalf("官方更新蓋掉了附加規則：%+v", after)
	}
	duties, addendum, err := m.Customizations(ctx, "code")
	must(t, err)
	if duties != "更新後的官方職責" || addendum != before.Body {
		t.Fatalf("customize show：%q %q", duties, addendum)
	}
	cards, _, err := m.Cards(ctx)
	must(t, err)
	for _, c := range cards {
		if c.ID == "code" && strings.Index(c.Body, "更新後的官方職責") > strings.Index(c.Body, "先講結論。") {
			t.Fatalf("卡片裡附加規則要在官方職責後面：%q", c.Body)
		}
	}
}

// customize add --section（DEV-161）：加在附加規則裡 `## <小節>` 那一段的最後（下一段之前），沒有那一段就在尾端開一段；
// 不帶小節的接在第一個 `## ` 段落之前（不會被算進最後一段）；程式碼區塊裡的 # 不算標題；小節名稱與會切開那一段的標題行被擋。
func TestAddRuleIntoSection(t *testing.T) {
	m, _, _ := fixtureManage(t)
	ctx := context.Background()
	const cues = "聊到什麼先看什麼"
	body, err := m.AddRule(ctx, "code", cues, "聊到跑步先看鞋子的主題。", "code")
	must(t, err)
	mark := "<!-- customize add 2026-10-23 by code -->\n"
	if want := "## " + cues + "\n\n" + mark + "聊到跑步先看鞋子的主題。\n"; body != want {
		t.Fatalf("沒有那一段就開一段：\n%q\nwant\n%q", body, want)
	}
	body, err = m.AddRule(ctx, "code", cues, "聊到單車先看車子的主題。", "code")
	must(t, err)
	if want := "## " + cues + "\n\n" + mark + "聊到跑步先看鞋子的主題。\n\n" + mark + "聊到單車先看車子的主題。\n"; body != want {
		t.Fatalf("那一段在檔尾、已經有條目：\n%q\nwant\n%q", body, want)
	}
	must(t, base(m).WriteAddendum(ctx, "code", "主人寫的規則。\n\n## "+cues+"\n\n"+mark+"聊到跑步先看鞋子的主題。\n```sh\n# 先停服務\n```\n\n\n## 別的小節\n別的內容。\n", "code"))
	body, err = m.AddRule(ctx, "code", cues, "聊到游泳先看泳鏡的主題。", "code")
	must(t, err)
	want := "主人寫的規則。\n\n## " + cues + "\n\n" + mark + "聊到跑步先看鞋子的主題。\n```sh\n# 先停服務\n```\n\n" + mark + "聊到游泳先看泳鏡的主題。\n\n## 別的小節\n別的內容。\n"
	if body != want {
		t.Fatalf("加在那一段的最後、下一段之前（程式碼區塊裡的 # 不算）：\n%q\nwant\n%q", body, want)
	}
	body, err = m.AddRule(ctx, "code", "", "不帶小節的。", "code")
	must(t, err)
	if !strings.HasPrefix(body, "主人寫的規則。\n\n"+mark+"不帶小節的。\n\n## "+cues+"\n") || strings.Count(body, "不帶小節的。") != 1 {
		t.Fatalf("不帶小節的接在第一段之前：%q", body)
	}
	for _, bad := range []struct{ section, rule string }{{"#標題", "x"}, {"兩\n行", "x"}, {strings.Repeat("字", 41), "x"}, {cues, "第一行\n## 會切開"}, {cues, "# 也會"}, {cues, "```\n沒收尾的區塊"}, {"", "~~~\n一般規則也要成對"}} {
		if _, err := m.AddRule(ctx, "code", bad.section, bad.rule, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Fatalf("%q／%q 要擋：%v", bad.section, bad.rule, err)
		}
	}
	for _, ok := range []string{"第一行\n### 小項可以", "```sh\n# 區塊裡的註解可以\n```"} {
		if _, err := m.AddRule(ctx, "code", cues, ok, "code"); err != nil {
			t.Fatalf("%q 可以：%v", ok, err)
		}
	}
}

// 可選知識（DEV-161）：沒選過照預設；主人整份換掉、記在名冊紀錄的 knowledge；不認得的 id 擋；只有主人能改；建秘書時可以一起給。
func TestAgentsKnowledgeChoice(t *testing.T) {
	m, h, _ := fixtureManage(t)
	ctx := context.Background()
	duties := "## 可選知識\n\n### 育兒 {#parenting}\n> 預設：不勾\n> 依年齡給建議。\n- 內容\n\n### 生活管理 {#balance}\n> 預設：勾\n> 留意工作吃掉生活。\n- 內容\n"
	base(m).SetOfficialDuties(func(ref string) (string, bool) { return duties, ref == "official/life" })
	_, err := base(m).PatchAgent(ctx, "life", map[string]any{"duties_from": "official/life"}, nil, "system")
	must(t, err)

	blocks, err := m.Knowledge(ctx, "life")
	must(t, err)
	if len(blocks) != 2 || blocks[0].ID != "parenting" || blocks[0].Enabled || !blocks[1].Enabled || !blocks[1].Default {
		t.Fatalf("沒選過照預設：%+v", blocks)
	}
	if b, err := m.Knowledge(ctx, "code"); err != nil || len(b) != 0 {
		t.Fatalf("沒有可選知識的職位回空清單：%+v %v", b, err)
	}
	// 名冊上還沒有（新家歡迎頁派職位前）：照官方樣板回，enabled＝預設；不是官方職位回空清單、不報找不到。
	m.d.Templates = fakeTemplates{"life9": "生活"}
	base(m).SetOfficialDuties(func(ref string) (string, bool) { return duties, ref == "official/life" || ref == "official/life9" })
	if b, err := m.Knowledge(ctx, "life9"); err != nil || len(b) != 2 || b[0].Enabled || !b[1].Enabled {
		t.Fatalf("還沒建的官方職位照樣板預設：%+v %v", b, err)
	}
	if b, err := m.Knowledge(ctx, "nobody"); err != nil || len(b) != 0 {
		t.Fatalf("還沒建、也不是官方職位回空清單：%+v %v", b, err)
	}
	m.d.Templates = nil

	if _, err := m.SetKnowledge(ctx, "life", []string{"parenting"}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("別的秘書不能改：%v", err)
	}
	// 擔任這個職位的秘書本人可以改（第一天聊完、主人同意後代改）。
	if b, err := m.SetKnowledge(ctx, "life", []string{"parenting"}, "life"); err != nil || !b[0].Enabled || b[1].Enabled {
		t.Fatalf("本人可以改自己職位的勾選：%+v %v", b, err)
	}
	if got := stringList(record(t, h, "agent", "life").Fields["knowledge_seen"]); !reflect.DeepEqual(got, []string{"parenting", "balance"}) {
		t.Fatalf("改勾選時記下當下全部可選 id：%v", got)
	}
	if _, err := m.SetKnowledge(ctx, "life", []string{"cooking"}, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("不認得的 id 要擋：%v", err)
	}
	blocks, err = m.SetKnowledge(ctx, "life", []string{"balance", "parenting", "parenting"}, "owner(web)")
	must(t, err)
	if !blocks[0].Enabled || !blocks[1].Enabled {
		t.Fatalf("兩個都勾：%+v", blocks)
	}
	if got := record(t, h, "agent", "life").Fields["knowledge"]; !reflect.DeepEqual(stringList(got), []string{"parenting", "balance"}) {
		t.Fatalf("記在名冊、照職責順序、不重複：%#v", got)
	}
	blocks, err = m.SetKnowledge(ctx, "life", nil, "owner(web)")
	must(t, err)
	if blocks[0].Enabled || blocks[1].Enabled {
		t.Fatalf("空的＝全部不勾（不是回到預設）：%+v", blocks)
	}
	// 官方之後新加一塊預設勾的：主人沒看過，照預設打開；看過的照他勾的（都不勾）。
	newer := duties + "\n### 新的一塊 {#fresh}\n> 預設：勾\n> 官方後來加的。\n- 內容\n"
	base(m).SetOfficialDuties(func(ref string) (string, bool) { return newer, ref == "official/life" })
	blocks, err = m.Knowledge(ctx, "life")
	must(t, err)
	if len(blocks) != 3 || blocks[0].Enabled || blocks[1].Enabled || !blocks[2].Enabled {
		t.Fatalf("沒看過的新塊照預設、看過的照主人勾的：%+v", blocks)
	}
	base(m).SetOfficialDuties(func(ref string) (string, bool) { return duties, ref == "official/life" })

	m.d.Templates = fakeTemplates{"life2": "生活"}
	base(m).SetOfficialDuties(func(ref string) (string, bool) { return duties, ref == "official/life" || ref == "official/life2" })
	if _, err := m.Create(ctx, "life2", "athena", "", "", []string{"nope"}, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("建秘書時不認得的 id 要擋：%v", err)
	}
	if _, err := h.Vault.Get(ctx, "agent", "life2"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("擋下時不能留下半建好的秘書：%v", err)
	}
	r, err := m.Create(ctx, "life2", "athena", "", "", []string{"parenting"}, "owner(web)")
	must(t, err)
	if !reflect.DeepEqual(stringList(r.Fields["knowledge"]), []string{"parenting"}) || !reflect.DeepEqual(stringList(r.Fields["knowledge_seen"]), []string{"parenting", "balance"}) {
		t.Fatalf("建秘書時記下勾的與看過的：%+v", r.Fields)
	}
	r, err = m.Create(ctx, "life3", "athena", "", "", nil, "owner(web)")
	must(t, err)
	if _, has := r.Fields["knowledge"]; has {
		t.Fatalf("沒給就不寫欄位（照預設）：%+v", r.Fields)
	}
}
