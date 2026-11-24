package pkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// demoNoteSchema 是示範套件的 demo.note 欄位規格：date、text（必填）、score（number）。
const demoNoteSchema = `schema = 1
title = "筆記"
date_field = "date"
[[field]]
name = "date"
type = "date"
description = "哪一天"
[[field]]
name = "text"
type = "string"
required = true
description = "內容"
[[field]]
name = "score"
type = "number"
[[field]]
name = "mood"
type = "enum"
enum = ["good", "bad"]
`

const demoNoteDecl = `
[[contributes.data]]
kind = "note"
schema = "schemas/note.toml"
ids = "seq"
prefix = "N-"
`

// withVault 讓 pkg 模組接上真的 vault（fixture 家）。
func withVault(h *harness) { h.m.d.Vault = h.home.Vault }

func putDemoNote(t *testing.T, h *harness, fields map[string]any) (vault.Record, error) {
	t.Helper()
	ctx := context.Background()
	id, err := h.home.Vault.NextID(ctx, "demo.note")
	if err != nil {
		return vault.Record{}, err
	}
	return h.home.Vault.Put(ctx, vault.Record{Kind: "demo.note", ID: id, Fields: fields}, "owner(cli)", "")
}

// 資料種類的宣告與欄位規格檔：每一種不合格都讓那個套件 rejected，field 是 contributes.data，說明寫哪個檔哪一欄；合格的出現在 Package.Data。
// Guards: REQ-173
func TestDataDeclValidated(t *testing.T) {
	h := newHarness(t)
	good := h.write(sdk.TierMine, "demo", dataPkg("demo", "1.0.0", demoNoteDecl+`
[[contributes.data]]
kind = "day"
schema = "schemas/day.toml"
ids = "given"
`), map[string]string{"schemas/note.toml": demoNoteSchema, "schemas/day.toml": "schema = 1\ntitle = \"一天\"\n[[field]]\nname = \"weight\"\ntype = \"number\"\n"})
	m, err := ParseManifest(good, sdk.TierMine)
	if err != nil {
		t.Fatal(err)
	}
	p := m.Public(sdk.TierMine)
	if len(p.Data) != 2 || p.Data[0].Kind != "note" || p.Data[0].Title != "筆記" || p.Data[0].IDs != "seq" || p.Data[0].Prefix != "N-" || p.Data[0].DateField != "date" || len(p.Data[0].Fields) != 4 {
		t.Fatalf("Data = %+v", p.Data)
	}
	if f := p.Data[0].Fields[1]; f.Name != "text" || f.Type != "string" || !f.Required || f.Description != "內容" {
		t.Fatalf("欄位：%+v", f)
	}
	if p.Data[1].IDs != "given" || p.Data[1].Prefix != "" {
		t.Fatalf("given：%+v", p.Data[1])
	}

	field := func(extra string) string { return "schema = 1\ntitle = \"x\"\n" + extra }
	cases := []struct{ name, decl, schema, want string }{
		{"bad kind", strings.Replace(demoNoteDecl, `"note"`, `"Note_1"`, 1), demoNoteSchema, "kind"},
		{"duplicate kind", demoNoteDecl + demoNoteDecl, demoNoteSchema, "重複"},
		{"missing file", strings.Replace(demoNoteDecl, "schemas/note.toml", "schemas/nope.toml", 1), demoNoteSchema, "找不到這個檔案"},
		{"escaping path", strings.Replace(demoNoteDecl, "schemas/note.toml", "../note.toml", 1), demoNoteSchema, "相對路徑"},
		{"bad ids", strings.Replace(demoNoteDecl, `"seq"`, `"auto"`, 1), demoNoteSchema, "ids"},
		{"seq without prefix", strings.Replace(demoNoteDecl, `prefix = "N-"`, "", 1), demoNoteSchema, "prefix"},
		{"bad prefix", strings.Replace(demoNoteDecl, `"N-"`, `"n1"`, 1), demoNoteSchema, "prefix"},
		{"given with prefix", strings.Replace(demoNoteDecl, `"seq"`, `"given"`, 1), demoNoteSchema, "不能有 prefix"},
		{"missing title", demoNoteDecl, "schema = 1\n", "title"},
		{"wrong schema version", demoNoteDecl, "schema = 2\ntitle = \"x\"\n", "schema 必須是 1"},
		{"unknown key", demoNoteDecl, field("color = \"red\"\n"), "不認得的欄位"},
		{"unknown type", demoNoteDecl, field("[[field]]\nname = \"a\"\ntype = \"money\"\n"), "不認得"},
		{"empty enum", demoNoteDecl, field("[[field]]\nname = \"a\"\ntype = \"enum\"\n"), "enum 的選項"},
		{"enum on string", demoNoteDecl, field("[[field]]\nname = \"a\"\ntype = \"string\"\nenum = [\"x\"]\n"), "不是 enum"},
		{"bad field name", demoNoteDecl, field("[[field]]\nname = \"Kcal\"\ntype = \"int\"\n"), "name"},
		{"reserved name", demoNoteDecl, field("[[field]]\nname = \"created_at\"\ntype = \"string\"\n"), "保留"},
		{"duplicate field", demoNoteDecl, field("[[field]]\nname = \"a\"\ntype = \"int\"\n[[field]]\nname = \"a\"\ntype = \"int\"\n"), "重複"},
		{"date_field not a date", demoNoteDecl, field("date_field = \"a\"\n[[field]]\nname = \"a\"\ntype = \"string\"\n"), "date_field"},
		{"date_field missing", demoNoteDecl, field("date_field = \"zzz\"\n"), "date_field"},
		{"broken toml", demoNoteDecl, "schema = = 1", "格式不對"},
	}
	for _, c := range cases {
		dir := h.write(sdk.TierMine, "bad", dataPkg("bad", "1.0.0", c.decl), map[string]string{"schemas/note.toml": c.schema})
		_, err := ParseManifest(dir, sdk.TierMine)
		var pe *platform.Err
		if err == nil || !asPlatformErr(err, &pe) || pe.Field != "contributes.data" || !strings.Contains(pe.Message, c.want) {
			t.Errorf("%s：要 contributes.data 加「%s」，實際 %v", c.name, c.want, err)
			continue
		}
		if c.name != "bad kind" && c.name != "duplicate kind" && c.name != "bad ids" && c.name != "seq without prefix" && c.name != "bad prefix" && c.name != "given with prefix" && c.name != "escaping path" && !strings.Contains(pe.Message, "schemas/") && c.name != "missing file" {
			t.Errorf("%s：說明要寫是哪個檔：%s", c.name, pe.Message)
		}
	}
	// 整個流程：壞的規格讓套件 rejected，其他套件照常。
	h.write(sdk.TierMine, "demo", dataPkg("demo", "1.0.0", demoNoteDecl), map[string]string{"schemas/note.toml": "schema = 1\n"})
	h.write(sdk.TierMine, "fine", dataPkg("fine", "1.0.0", ""), nil)
	h.start()
	if got := h.get("demo"); got.State != sdk.PackageStateRejected || !strings.Contains(got.Error, "title") {
		t.Fatalf("壞規格要 rejected：%+v", got)
	}
	if got := h.get("fine"); got.State != sdk.PackageStateRunning {
		t.Fatalf("別的套件不受影響：%+v", got)
	}
}

func asPlatformErr(err error, target **platform.Err) bool {
	pe, ok := err.(*platform.Err)
	if ok {
		*target = pe
	}
	return ok
}

// 套件的資料種類跟著啟用走：啟用才註冊（純資料與附程式都算）；停用、解除安裝、被拒時資料留在原地、暫時看不到；重新啟用就回來。
// Guards: REQ-173, REQ-172
func TestPackageKindsFollowEnable(t *testing.T) {
	h := newHarness(t)
	withVault(h)
	ctx := context.Background()
	files := map[string]string{"schemas/note.toml": demoNoteSchema}
	h.write(sdk.TierMine, "demo", dataPkg("demo", "1.0.0", demoNoteDecl), files)
	// 附程式的套件也能宣告資料。
	pf := programFiles("worker")
	pf["schemas/log.toml"] = "schema = 1\ntitle = \"紀錄\"\n[[field]]\nname = \"line\"\ntype = \"string\"\n"
	h.write(sdk.TierMine, "worker", programPkg("worker", "", "[[contributes.data]]\nkind = \"log\"\nschema = \"schemas/log.toml\"\nids = \"seq\"\nprefix = \"L-\"\n"), pf)
	h.start()

	r, err := putDemoNote(t, h, map[string]any{"text": "第一則", "date": "2026-10-25", "score": 4.5})
	if err != nil || r.ID != "N-1" {
		t.Fatalf("寫入：%v %v", r, err)
	}
	if _, err := h.home.Vault.Put(ctx, vault.Record{Kind: "worker.log", ID: "L-1", Fields: map[string]any{"line": "x"}}, "owner(cli)", ""); err != nil {
		t.Fatalf("附程式的套件也能宣告資料：%v", err)
	}
	// 欄位不符就拒絕：缺必填。
	if _, err := putDemoNote(t, h, map[string]any{"score": 1}); err == nil {
		t.Fatal("缺必填欄位要被拒")
	}
	path := filepath.Join(h.home.Dir, "vault", "pkg", "demo", "note", "N-1.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("資料在 vault/pkg/demo/note/：%v", err)
	}

	by := &sdk.Actor{Name: "owner(web)"}
	// 停用：看不到、不能寫，檔案還在。
	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "demo", Enabled: false, By: by}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.home.Vault.Get(ctx, "demo.note", "N-1"); codeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("停用後讀不到：%v", err)
	}
	if _, err := h.home.Vault.Put(ctx, vault.Record{Kind: "demo.note", ID: "N-2", Fields: map[string]any{"text": "x"}}, "owner(cli)", ""); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("停用後不能寫：%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("停用不能動資料：%v", err)
	}
	if _, err := h.home.Vault.Get(ctx, "worker.log", "L-1"); err != nil {
		t.Fatalf("別的套件不受影響：%v", err)
	}
	// 重新啟用：資料回來，編號接著發。
	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "demo", Enabled: true, By: by}); err != nil {
		t.Fatal(err)
	}
	if got, err := h.home.Vault.Get(ctx, "demo.note", "N-1"); err != nil || got.Fields["text"] != "第一則" {
		t.Fatalf("啟用後資料回來：%v %v", got, err)
	}
	// 前面缺必填的那次寫入已經領掉 N-2，所以接著是 N-3（編號只增不減）。
	if r2, err := putDemoNote(t, h, map[string]any{"text": "第二則"}); err != nil || r2.ID != "N-3" {
		t.Fatalf("接著發號：%v %v", r2, err)
	}
	// 被拒（規格壞掉）：看不到，修好就回來。
	spec := filepath.Join(h.home.Dir, "packages", "mine", "demo", "schemas", "note.toml")
	if err := os.WriteFile(spec, []byte("schema = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Reload(ctx, &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := h.get("demo"); got.State != sdk.PackageStateRejected {
		t.Fatalf("要被拒：%+v", got)
	}
	if _, err := h.home.Vault.Get(ctx, "demo.note", "N-1"); codeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("被拒後讀不到：%v", err)
	}
	if err := os.WriteFile(spec, []byte(demoNoteSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Reload(ctx, &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.home.Vault.Get(ctx, "demo.note", "N-3"); err != nil {
		t.Fatalf("修好後回來：%v", err)
	}
	// 分享的套件解除安裝：資料還在檔案裡，同一個套件再裝回來就看得到。
	src := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(filepath.Join(src, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{"package.toml": strings.Replace(dataPkg("shared", "1.0.0", demoNoteDecl), `kind = "note"`, `kind = "item"`, 1), "schemas/note.toml": demoNoteSchema} {
		if err := os.WriteFile(filepath.Join(src, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.m.Install(ctx, &sdk.PackageInstallRequest{Source: src, By: by}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.home.Vault.Put(ctx, vault.Record{Kind: "shared.item", ID: "N-1", Fields: map[string]any{"text": "留著"}}, "owner(cli)", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Uninstall(ctx, &sdk.PackageUninstallRequest{ID: "shared", By: by}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.home.Vault.Get(ctx, "shared.item", "N-1"); codeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("解除安裝後讀不到：%v", err)
	}
	if _, err := os.Stat(filepath.Join(h.home.Dir, "vault", "pkg", "shared", "item", "N-1.md")); err != nil {
		t.Fatalf("解除安裝資料要留著：%v", err)
	}
	if _, err := h.m.Install(ctx, &sdk.PackageInstallRequest{Source: src, By: by}); err != nil {
		t.Fatal(err)
	}
	if got, err := h.home.Vault.Get(ctx, "shared.item", "N-1"); err != nil || got.Fields["text"] != "留著" {
		t.Fatalf("重裝後資料回來：%v %v", got, err)
	}
}

// ---- packages sync（REQ-174）----

// fakeSync 記下 Sync 對別的模組做了什麼。
type fakeSync struct {
	persona  string
	duties   map[string]string // 現有秘書 → 職責
	jobs     map[string][]string
	calls    []string
	failOn   string
	checkErr error
	archived map[string]bool
}

func (f *fakeSync) DefaultPersona() string { return f.persona }
func (f *fakeSync) AgentDuties(_ context.Context, id string) (string, bool, bool, error) {
	d, ok := f.duties[id]
	return d, ok, f.archived[id], nil
}
func (f *fakeSync) CreateAgent(_ context.Context, id, persona, duty string, knowledge []string, by string) error {
	call := fmt.Sprintf("create %s duty=%q by=%s", id, duty, by)
	if persona != "" {
		call += " persona=" + persona
	}
	if knowledge != nil {
		call += " knowledge=" + strings.Join(knowledge, ",")
	}
	f.calls = append(f.calls, call)
	if f.failOn == "create" {
		return platform.Conflict("壞掉了")
	}
	f.duties[id] = ""
	return nil
}
func (f *fakeSync) SetAgentField(_ context.Context, id, field, value, by string) error {
	f.calls = append(f.calls, fmt.Sprintf("set %s %s=%s by=%s", id, field, value, by))
	return nil
}
func (f *fakeSync) WriteDuties(_ context.Context, id, body, by string) error {
	f.calls = append(f.calls, fmt.Sprintf("duties %s by=%s lines=%d", id, by, len(strings.Split(body, "\n"))))
	f.duties[id] = body
	return nil
}
func (f *fakeSync) CheckPersona(string) error { return f.checkErr }
func (f *fakeSync) JobIDs(_ context.Context, agent string) ([]string, error) {
	return f.jobs[agent], nil
}
func (f *fakeSync) SaveJob(_ context.Context, j *sdk.Job, by string) error {
	f.calls = append(f.calls, fmt.Sprintf("job %s source=%s enabled=%v cron=%v by=%s body=%q", j.ID, j.Source, j.Enabled, j.Cron, by, j.Body))
	f.jobs[j.Agent] = append(f.jobs[j.Agent], j.ID)
	return nil
}

const trainerAgent = `---
duty: 管健身計畫
color: "#2f8f6f"
color_dark: "#5fd0a8"
mode: ondemand
---
你是健身教練。
第二行規則。
第三行規則。
`

const weighJob = `---
name: 提醒量體重
cron: "0 8 * * 1"
catchup: skip
---
提醒主人早上量體重。
`

func syncPackage(h *harness) {
	h.write(sdk.TierMine, "fit", dataPkg("fit", "1.0.0", `
[[contributes.agent]]
id = "trainer"
template = "agents/trainer/agent.md"
[[contributes.agent]]
id = "coach"
template = "agents/coach/agent.md"
[[contributes.agent]]
id = "helper"
template = "agents/helper/agent.md"
[[contributes.job]]
agent = "trainer"
id = "weigh"
path = "jobs/weigh.md"
[[contributes.job]]
agent = "trainer"
id = "old"
path = "jobs/weigh.md"
`), map[string]string{
		"agents/trainer/agent.md": trainerAgent,
		"agents/coach/agent.md":   "---\nduty: 教練\n---\n舊職責\n新的第二行\n",
		"agents/helper/agent.md":  "---\nduty: 幫手\n---\n一樣的職責\n",
		"jobs/weigh.md":           weighJob,
	})
}

// 預演只列出會發生什麼（新增的秘書、職責差異的增刪行數、會新增的排程），不改任何東西；--apply 才套用，已存在的排程不動。
// Guards: REQ-174
func TestSyncPreviewAndApply(t *testing.T) {
	h := newHarness(t)
	syncPackage(h)
	host := &fakeSync{persona: "builder", duties: map[string]string{"coach": "舊職責\n舊的第二行\n共同行", "helper": "一樣的職責"}, jobs: map[string][]string{"trainer": {"trainer/old"}}}
	h.m.SetSyncHost(host)
	h.start()
	ctx := context.Background()
	owner := &sdk.Actor{Name: "owner(cli)"}

	pre, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "fit", By: owner})
	if err != nil {
		t.Fatal(err)
	}
	if pre.Applied || len(host.calls) != 0 {
		t.Fatalf("預演不能改東西：%+v %v", pre, host.calls)
	}
	wantAgents := []sdk.SyncAgent{{ID: "trainer", Action: "create", Added: 3, Persona: "builder", PersonaFrom: "home"}, {ID: "coach", Action: "update", Added: 1, Removed: 2}, {ID: "helper", Action: "same"}}
	if fmt.Sprint(pre.Agents) != fmt.Sprint(wantAgents) {
		t.Fatalf("秘書：%+v 要 %+v", pre.Agents, wantAgents)
	}
	wantJobs := []sdk.SyncJob{{Agent: "trainer", ID: "weigh", Action: "create"}, {Agent: "trainer", ID: "old", Action: "exists"}}
	if fmt.Sprint(pre.Jobs) != fmt.Sprint(wantJobs) {
		t.Fatalf("排程：%+v 要 %+v", pre.Jobs, wantJobs)
	}
	// 預演不帶操作者也可以（只讀）。
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "fit"}); err != nil {
		t.Fatal(err)
	}

	got, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "fit", Apply: true, By: owner})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Applied || fmt.Sprint(got.Agents) != fmt.Sprint(wantAgents) {
		t.Fatalf("套用結果：%+v", got)
	}
	want := []string{
		`create trainer duty="管健身計畫" by=owner(cli) persona=builder`,
		`set trainer color=#2f8f6f by=owner(cli)`,
		`set trainer color_dark=#5fd0a8 by=owner(cli)`,
		`set trainer mode=ondemand by=owner(cli)`,
		"duties trainer by=system lines=3",
		"duties coach by=system lines=2",
		`job trainer/weigh source=fit enabled=true cron=[0 8 * * 1] by=owner(cli) body="提醒主人早上量體重。"`,
	}
	if fmt.Sprint(host.calls) != fmt.Sprint(want) {
		t.Fatalf("呼叫：\n%s\n要：\n%s", strings.Join(host.calls, "\n"), strings.Join(want, "\n"))
	}
	// 再同步一次：全部「same」或「exists」，沒有任何寫入。
	host.calls = nil
	again, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "fit", Apply: true, By: owner})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range again.Agents {
		if a.Action != "same" {
			t.Errorf("第二次要全部 same：%+v", a)
		}
	}
	for _, j := range again.Jobs {
		if j.Action != "exists" {
			t.Errorf("第二次排程要全部 exists：%+v", j)
		}
	}
	if len(host.calls) != 0 {
		t.Fatalf("第二次不該寫入：%v", host.calls)
	}
}

// 套用只有主人（owner(…)）能做，秘書、系統、沒有操作者都是 FORBIDDEN 且不動任何東西；沒有預設人設要新增秘書時說清楚、不動；樣板壞掉整個不動。
// Guards: REQ-174
func TestSyncApplyOwnerOnly(t *testing.T) {
	h := newHarness(t)
	syncPackage(h)
	host := &fakeSync{duties: map[string]string{}, jobs: map[string][]string{}}
	h.m.SetSyncHost(host)
	h.start()
	ctx := context.Background()
	for _, who := range []*sdk.Actor{{Name: "code"}, {Name: "system"}, {Name: "fitness"}, nil} {
		_, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "fit", Apply: true, By: who})
		if codeOf(err) != sdk.ErrorCodeForbidden || !strings.Contains(err.Error(), "只有主人能套用套件的職責與排程") {
			t.Fatalf("by=%v 要 FORBIDDEN：%v", who, err)
		}
	}
	if len(host.calls) != 0 {
		t.Fatalf("被擋不能動：%v", host.calls)
	}
	// 沒有預設人設又要新增秘書。
	_, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "fit", Apply: true, By: &sdk.Actor{Name: "owner(web)"}})
	if codeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "人設") || len(host.calls) != 0 {
		t.Fatalf("沒有預設人設：%v %v", err, host.calls)
	}
	// 套件不存在、停用。
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "nope"}); codeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("不存在：%v", err)
	}
	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "fit", Enabled: false, By: &sdk.Actor{Name: "owner(web)"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "fit"}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("停用：%v", err)
	}
	// 沒接上 SyncHost。
	h2 := newHarness(t)
	syncPackage(h2)
	h2.start()
	if _, err := h2.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "fit"}); codeOf(err) != sdk.ErrorCodeUnavailable {
		t.Fatalf("沒接上：%v", err)
	}
}

// 只認領一個職位：套件清單列出它帶來的職位樣板（id、一句話職責、顏色）；sync 帶 agent 只動這一位與她的排程，
// 帶 persona 時新增的秘書用那個人設（沒有預設人設也可以）；套件沒有這個職位回 NOT_FOUND。
// Guards: REQ-238
func TestSyncOneFunctionWithPersona(t *testing.T) {
	h := newHarness(t)
	syncPackage(h)
	host := &fakeSync{duties: map[string]string{}, jobs: map[string][]string{}}
	h.m.SetSyncHost(host)
	h.start()
	ctx := context.Background()
	owner := &sdk.Actor{Name: "owner(web)"}

	var decls []sdk.AgentDecl
	for _, p := range h.list() {
		if p.ID == "fit" {
			decls = p.Agents
		}
	}
	want := []sdk.AgentDecl{{ID: "trainer", Duty: "管健身計畫", Color: "#2f8f6f", ColorDark: "#5fd0a8"}, {ID: "coach", Duty: "教練"}, {ID: "helper", Duty: "幫手"}}
	if fmt.Sprint(decls) != fmt.Sprint(want) {
		t.Fatalf("套件清單的職位樣板：%+v 要 %+v", decls, want)
	}

	got, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "fit", Agent: "trainer", Persona: "kaguya", Apply: true, By: owner})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agents) != 1 || got.Agents[0].ID != "trainer" || len(got.Jobs) != 2 {
		t.Fatalf("只同步 trainer：%+v", got)
	}
	if len(host.calls) == 0 || !strings.HasSuffix(host.calls[0], "by=owner(web) persona=kaguya") {
		t.Fatalf("新增時用指定的人設：%v", host.calls)
	}
	if _, ok := host.duties["coach"]; ok {
		t.Fatalf("別的職位不能動：%v", host.calls)
	}
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "fit", Agent: "nobody", By: owner}); codeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("沒有這個職位：%v", err)
	}
}

// 派職位時一起給勾的可選知識（DEV-161）：官方職位轉給建秘書（不認得的 id 由名冊擋）；沒指定職位、不是官方職位都先擋、什麼都不寫。
func TestSyncPassesKnowledgeToNewAgent(t *testing.T) {
	h := newHarness(t)
	syncPackage(h)
	h.write(sdk.TierOfficial, "crew", dataPkg("crew", "1.0.0", `
[[contributes.agent]]
id = "guide"
template = "agents/guide/agent.md"
[[contributes.agent]]
id = "keeper"
template = "agents/keeper/agent.md"
`), map[string]string{
		"agents/guide/agent.md":  "---\nduty: 嚮導\n---\n## 負責什麼\n- 帶路\n",
		"agents/keeper/agent.md": "---\nduty: 管家\n---\n職責\n",
	})
	host := &fakeSync{persona: "builder", duties: map[string]string{}, jobs: map[string][]string{}}
	h.m.SetSyncHost(host)
	h.start()
	ctx := context.Background()
	owner := &sdk.Actor{Name: "owner(web)"}
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "crew", Knowledge: []string{"x"}, Apply: true, By: owner}); codeOf(err) != sdk.ErrorCodeInvalid || len(host.calls) != 0 {
		t.Fatalf("沒指定職位就給可選知識要擋、什麼都不寫：%v %v", err, host.calls)
	}
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "fit", Agent: "trainer", Persona: "kaguya", Knowledge: []string{"x"}, Apply: true, By: owner}); codeOf(err) != sdk.ErrorCodeInvalid || len(host.calls) != 0 {
		t.Fatalf("不是官方職位就給可選知識要擋、什麼都不寫：%v %v", err, host.calls)
	}
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "crew", Agent: "guide", Knowledge: []string{"leading"}, Apply: true, By: owner}); err != nil {
		t.Fatal(err)
	}
	if len(host.calls) == 0 || !strings.Contains(host.calls[0], "create guide") || !strings.Contains(host.calls[0], "knowledge=leading") {
		t.Fatalf("可選知識要轉給建秘書：%v", host.calls)
	}
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "crew", Agent: "keeper", Apply: true, By: owner}); err != nil {
		t.Fatal(err)
	}
	for _, c := range host.calls {
		if strings.HasPrefix(c, "create keeper") && strings.Contains(c, "knowledge=") {
			t.Fatalf("沒給就照預設（不傳）：%v", host.calls)
		}
	}
}

// 樣板格式：職責樣板缺 duty、顏色格式不對、mode 不認得、內文空白，都讓套件 rejected（field = contributes.agent）。
// Guards: REQ-174
func TestAgentTemplateValidated(t *testing.T) {
	h := newHarness(t)
	decl := "[[contributes.agent]]\nid = \"trainer\"\ntemplate = \"agents/trainer/agent.md\"\n"
	cases := map[string]string{
		"沒有標頭":     "只有內文\n",
		"缺 duty":   "---\ncolor: \"#aabbcc\"\n---\n職責\n",
		"顏色不對":     "---\nduty: x\ncolor: red\n---\n職責\n",
		"mode 不認得": "---\nduty: x\nmode: always\n---\n職責\n",
		"不認得的標頭欄位": "---\nduty: x\ntone: soft\n---\n職責\n",
		"內文空白":     "---\nduty: x\n---\n",
	}
	names := make([]string, 0, len(cases))
	for n := range cases {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		dir := h.write(sdk.TierMine, "fit", dataPkg("fit", "1.0.0", decl), map[string]string{"agents/trainer/agent.md": cases[n]})
		_, err := ParseManifest(dir, sdk.TierMine)
		var pe *platform.Err
		if err == nil || !asPlatformErr(err, &pe) || pe.Field != "contributes.agent" || !strings.Contains(pe.Message, "agents/trainer/agent.md") {
			t.Errorf("%s：要 contributes.agent 並寫是哪個檔：%v", n, err)
		}
	}
	dir := h.write(sdk.TierMine, "fit", dataPkg("fit", "1.0.0", decl), map[string]string{"agents/trainer/agent.md": trainerAgent})
	if _, err := ParseManifest(dir, sdk.TierMine); err != nil {
		t.Fatal(err)
	}
}

// 頭像來源（D-29）：附程式的套件用 contributes.avatars 宣告公開資料夾裡的子資料夾，API 上帶出來；資料套件或不合格的名稱讓套件 rejected。
func TestManifestAvatarsDeclaration(t *testing.T) {
	h := newHarness(t)
	dir := h.write(sdk.TierMine, "wardrobe", programPkg("wardrobe", "", "[contributes]\navatars = \"worn\"\n"), programFiles("wardrobe"))
	m, err := ParseManifest(dir, sdk.TierMine)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Public(sdk.TierMine).Avatars; got != "worn" {
		t.Fatalf("Avatars = %q", got)
	}
	for name, src := range map[string]string{
		"data package": dataPkg("bad", "1.0.0", "[contributes]\navatars = \"worn\"\n"),
		"bad name":     programPkg("bad", "", "[contributes]\navatars = \"../worn\"\n"),
	} {
		dir := h.write(sdk.TierMine, "bad", src, programFiles("bad"))
		_, err := ParseManifest(dir, sdk.TierMine)
		var pe *platform.Err
		if err == nil || !asPlatformErr(err, &pe) || pe.Field != "contributes.avatars" {
			t.Errorf("%s：要 contributes.avatars 錯誤，實際 %v", name, err)
		}
	}
}

// 封存過的秘書 id 不能重用（規則在 packages，主機只回 archived）；沒有預設人設也沒指定就不建。
func TestSyncRulesArchivedAndDefaultPersona(t *testing.T) {
	ctx := context.Background()
	host := &fakeSync{duties: map[string]string{}, archived: map[string]bool{"old": true}}
	if _, _, err := agentDuties(ctx, host, "old"); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "封存過") {
		t.Fatalf("封存過要 CONFLICT：%v", err)
	}
	if err := createAgent(ctx, host, AgentTemplate{ID: "x"}, "", nil, "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid || len(host.calls) != 0 {
		t.Fatalf("沒有人設不建：%v %v", err, host.calls)
	}
}

// 官方套件帶來的職位，職責跟著程式走（DEV-154）：同步不把職責抄進家裡，已經有的職位也不整份覆蓋；
// 樣板與以前各版出現過的行讀得到（家的遷移用）。主人自己的套件照舊（TestSyncPreviewAndApply）。
func TestSyncOfficialDutiesStayInProgram(t *testing.T) {
	h := newHarness(t)
	dir := h.write(sdk.TierOfficial, "crew", dataPkg("crew", "1.0.0", `
[[contributes.agent]]
id = "guide"
template = "agents/guide/agent.md"
[[contributes.agent]]
id = "keeper"
template = "agents/keeper/agent.md"
`), map[string]string{
		"agents/guide/agent.md":    "---\nduty: 嚮導\n---\n## 負責什麼\n- 帶路\n",
		"agents/guide/history.txt": "- 舊版才有的一行\n",
		"agents/keeper/agent.md":   "---\nduty: 管家\n---\n新版職責\n",
	})
	host := &fakeSync{persona: "builder", duties: map[string]string{"keeper": "主人改過的職責"}, jobs: map[string][]string{}}
	h.m.SetSyncHost(host)
	h.start()
	got, err := h.m.Sync(context.Background(), &sdk.PackageSyncRequest{ID: "crew", Apply: true, By: &sdk.Actor{Name: "owner(cli)"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Agents[0].Action != "create" || got.Agents[1].Action != "same" {
		t.Fatalf("官方的：沒有就新增、有就不動：%+v", got.Agents)
	}
	for _, c := range host.calls {
		if strings.HasPrefix(c, "duties ") {
			t.Fatalf("官方職責不該寫進家裡：%v", host.calls)
		}
	}
	if body, ok := h.m.OfficialDuties("crew/guide"); !ok || body != "## 負責什麼\n- 帶路" {
		t.Fatalf("讀樣板：%q %v", body, ok)
	}
	if _, ok := h.m.OfficialDuties("other/guide"); ok {
		t.Fatal("套件對不上不該讀到")
	}
	if h.m.TemplateDutiesRef("guide") != "crew/guide" || h.m.TemplateDutiesRef("nobody") != "" {
		t.Fatal("職位對樣板")
	}
	d := officialDutiesIn(filepath.Dir(dir))["guide"]
	if !d.Known["- 帶路"] || !d.Known["- 舊版才有的一行"] || d.Known[""] {
		t.Fatalf("出現過的行：%v", d.Known)
	}
}
