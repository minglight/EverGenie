package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/agent/personafixture"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

const owner = "owner(web)"

// testLibrary 是接進名冊的測試角色庫：讀家裡真的卡檔與說話層（正式的角色庫是業務模組 personas，底層的測試不 import 業務模組）。
func testLibrary(h *homefixture.Home) *personafixture.Library {
	return personafixture.New(h.Dir)
}

type recordingBus struct{ events []map[string]any }

func (b *recordingBus) Publish(ctx context.Context, topic string, p map[string]any) error {
	p["topic"] = topic
	b.events = append(b.events, p)
	return nil
}
func fixture(t *testing.T) (*Module, *homefixture.Home, *recordingBus) {
	t.Helper()
	h := homefixture.New(t)
	b := &recordingBus{}
	m := New(Deps{Vault: h.Vault, Clock: h.Clock, Bus: b})
	m.SetHome(h.Dir)
	m.SetPersonaSource(testLibrary(h)) // 測試角色庫：名冊的測試要讀家裡真的卡檔
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for slug, data := range map[string]string{"athena": "---\nname: Athena\naka: [雅典娜]\n---\n人設\n", "new": "+++\nname = \"New\"\naka = [\"新人\"]\n+++\n新的人設\n"} {
		if err := os.MkdirAll(filepath.Join(h.Dir, "personas"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := platform.AtomicWrite(filepath.Join(h.Dir, "personas", slug+".md"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"code", "life"} {
		h.WithRecords(vault.Record{Kind: "agent", ID: id, Fields: map[string]any{"duty": "測試職責", "persona": "athena", "former_names": []string{"Old" + id}}, Body: "官方職責"})
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	return m, h, b
}
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

// Guards: REQ-39
func TestRosterSingleSource(t *testing.T) {
	m, h, _ := fixture(t)
	rows, err := m.List(context.Background())
	must(t, err)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	_, err = h.Vault.Patch(context.Background(), "agent", "code", map[string]any{"duty": "修改"}, nil, "system")
	must(t, err)
	rows, err = m.List(context.Background())
	must(t, err)
	for _, r := range rows {
		if r.ID == "code" && r.Fields["duty"] != "修改" {
			t.Fatal("stale roster")
		}
	}
}

// Guards: REQ-40
func TestResolveAliasesAndFormerNames(t *testing.T) {
	m, _, _ := fixture(t)
	for _, s := range []string{"code", "CODE", "athena", "雅典娜", "oldCODE"} {
		id, ok := m.Resolve(context.Background(), s)
		if !ok || id != "code" {
			t.Fatalf("%s: %s %v", s, id, ok)
		}
	}
	if _, ok := m.Resolve(context.Background(), "missing"); ok {
		t.Fatal("unknown resolved")
	}
}

// 改名冊那一筆（換角色就是改 persona）不動記憶、附加規則、日誌；換角色的規則測試在業務模組 agents。
// Guards: REQ-41
func TestPatchAgentKeepsMemory(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.WriteMemory(ctx, "code", "記憶", "code"))
	must(t, m.WriteAddendum(ctx, "code", "規則", "code"))
	must(t, m.AppendJournal(ctx, "code", "日誌"))
	before := map[string][]byte{}
	times := map[string]int64{}
	for _, p := range []string{"memory.md", "addendum.md", "journal/2026-10-23.md"} {
		path := filepath.Join(h.Dir, "vault/agents/code", p)
		b, err := os.ReadFile(path)
		must(t, err)
		st, err := os.Stat(path)
		must(t, err)
		before[path] = b
		times[path] = st.ModTime().UnixNano()
	}
	_, err := m.PatchAgent(ctx, "code", map[string]any{"persona": "new"}, nil, "owner(web)")
	must(t, err)
	for path, b := range before {
		after, err := os.ReadFile(path)
		must(t, err)
		st, err := os.Stat(path)
		must(t, err)
		if string(after) != string(b) || st.ModTime().UnixNano() != times[path] {
			t.Fatal("assignment touched " + path)
		}
	}
	r := record(t, h, "agent", "code")
	if r.Fields["persona"] != "new" || r.Body != "官方職責" {
		t.Fatal(r)
	}
}

// Guards: REQ-42
func TestSharedConversationNoCycle(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	must(t, m.SetSharesConversation(ctx, "code", "life", "owner(web)"))
	if err := m.SetSharesConversation(ctx, "life", "code", "system"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatal(err)
	}
	if err := m.SetSharesConversation(ctx, "life", "missing", "system"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatal(err)
	}
	if err := m.SetSharesConversation(ctx, "life", "code", "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal(err)
	}
	must(t, m.SetSharesConversation(ctx, "code", "", "system"))
	must(t, m.SetSharesConversation(ctx, "life", "code", "system"))
}

// Guards: REQ-44
func TestJournalAppendOnly(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.AppendJournal(ctx, "code", "第一筆"))
	must(t, m.AppendJournal(ctx, "code", "第二筆"))
	r := record(t, h, "journal", "code/2026-10-23")
	if r.Body != "- 12:00 第一筆\n- 12:00 第二筆" {
		t.Fatal(r.Body)
	}
	_, err := h.Vault.Put(ctx, r, "code", r.Revision)
	if platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal(err)
	}
}

// Guards: REQ-44
func TestMemoryPerAgent(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.WriteMemory(ctx, "code", "code memory", "code"))
	must(t, m.WriteMemory(ctx, "life", "life memory", "system"))
	if err := m.WriteMemory(ctx, "code", "bad", "life"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal(err)
	}
	if record(t, h, "memory", "code").Body != "code memory" || record(t, h, "memory", "life").Body != "life memory" {
		t.Fatal("memory leaked")
	}
}

// Guards: REQ-45
func TestProfileOnlyLife(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	for _, by := range []string{"code", "system", "owner(web)"} {
		if err := m.WriteProfile(ctx, "bad", by); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
			t.Fatal(err)
		}
	}
	must(t, m.WriteProfile(ctx, "profile", "life"))
	if record(t, h, "profile", "profile").Body != "profile" {
		t.Fatal("missing profile")
	}
}

// Guards: REQ-46, REQ-48
func TestAddendumSurvivesOfficialUpdate(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.WriteAddendum(ctx, "code", "我的規則", "owner(web)"))
	before := record(t, h, "addendum", "code")
	must(t, m.WriteDuties(ctx, "code", "新職責", "system"))
	after := record(t, h, "addendum", "code")
	if before.Revision != after.Revision || after.Body != "我的規則" {
		t.Fatal(after)
	}
	if err := m.WriteAddendum(ctx, "code", "bad", "life"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal(err)
	}
}

// Guards: REQ-69
func TestAppendJournalOnce(t *testing.T) {
	m, h, _ := fixture(t)
	must(t, m.AppendJournalOnce(context.Background(), "life", "主人完成：喝水（R-1）"))
	if body := record(t, h, "journal", "life/2026-10-23").Body; body != "- 12:00 主人完成：喝水（R-1）" {
		t.Fatal(body)
	}
	if err := m.AppendJournalOnce(context.Background(), "nobody", "x"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatal(err)
	}
}

// Guards: REQ-41
func TestPersonaReadsBothFormats(t *testing.T) {
	m, h, _ := fixture(t)
	for _, slug := range []string{"athena", "new"} {
		path := filepath.Join(h.Dir, "personas", slug+".md")
		before, err := os.ReadFile(path)
		must(t, err)
		p, err := m.persona(slug)
		must(t, err)
		if p.Fields["name"] == "" {
			t.Fatal(p)
		}
		after, err := os.ReadFile(path)
		must(t, err)
		if string(before) != string(after) {
			t.Fatal("persona rewritten")
		}
	}
	if err := m.CheckAssignable("missing"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal(err)
	}
}

// Guards: REQ-48
func TestDutiesWriteForbidden(t *testing.T) {
	m, _, _ := fixture(t)
	for _, by := range []string{"owner(web)", "code"} {
		if err := m.WriteDuties(context.Background(), "code", "bad", by); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
			t.Fatal(err)
		}
	}
}

// Guards: REQ-69
func TestAppendJournalOnceIdempotent(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.AppendJournalOnce(ctx, "life", "主人完成：喝水（R-1）"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	h.Clock.Advance(time.Minute)
	must(t, m.AppendJournalOnce(ctx, "life", "主人完成：喝水（R-1）"))
	if strings.Count(record(t, h, "journal", "life/2026-10-23").Body, "主人完成：") != 1 {
		t.Fatal("duplicate journal entry")
	}
}

// Guards: REQ-42
func TestStartRejectsSharedCycle(t *testing.T) {
	for _, target := range []string{"life", "missing"} {
		t.Run(target, func(t *testing.T) {
			m, h, _ := fixture(t)
			ctx := context.Background()
			_, err := h.Vault.Patch(ctx, "agent", "code", map[string]any{"shares_conversation_with": target}, nil, "system")
			must(t, err)
			_, err = h.Vault.Patch(ctx, "agent", "life", map[string]any{"shares_conversation_with": "code"}, nil, "system")
			must(t, err)
			must(t, m.Stop(ctx))
			if err = m.Start(ctx); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
				t.Fatal(err)
			}
		})
	}
}

// Guards: REQ-41, REQ-46
func TestAgentEventsPublished(t *testing.T) {
	m, _, b := fixture(t)
	ctx := context.Background()
	must(t, m.Changed(ctx, "code", "assigned"))
	must(t, m.WriteAddendum(ctx, "code", "規則", "code"))
	if err := m.WriteAddendum(ctx, "code", "bad", "life"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal(err)
	}
	must(t, m.WriteMemory(ctx, "code", "memory", "code"))
	must(t, m.WriteProfile(ctx, "profile", "life"))
	must(t, m.AppendJournal(ctx, "code", "journal"))
	must(t, m.SetSharesConversation(ctx, "code", "life", "system"))
	must(t, m.WriteDuties(ctx, "code", "duties", "system"))
	if len(b.events) != 4 {
		t.Fatal(b.events)
	}
	for i, e := range b.events {
		want := "updated"
		if i == 0 {
			want = "assigned"
		}
		if e["topic"] != "agent.changed" || e["type"] != want || e["id"] != "code" {
			t.Fatal(e)
		}
	}
}

func (b *recordingBus) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return nil, nil
}

// Guards: REQ-73, REQ-43
func TestPersonaNameReadsCurrentBinding(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	name, err := m.PersonaName(ctx, "code")
	must(t, err)
	if name != "Athena" {
		t.Fatal(name)
	}
	_, err = m.PatchAgent(ctx, "code", map[string]any{"persona": "new"}, nil, "system")
	must(t, err)
	name, err = m.PersonaName(ctx, "code")
	must(t, err)
	if name != "New" {
		t.Fatal(name)
	}
	must(t, platform.AtomicWrite(filepath.Join(h.Dir, "personas/new.md"), []byte("---\nname: Changed\n---\n"), 0600))
	name, err = m.PersonaName(ctx, "code")
	must(t, err)
	if name != "Changed" {
		t.Fatal(name)
	}
	if _, err = m.PersonaName(ctx, "missing"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatal(err)
	}
	must(t, platform.AtomicWrite(filepath.Join(h.Dir, "personas/new.md"), []byte("---\naka: []\n---\n"), 0600))
	if _, err = m.PersonaName(ctx, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal(err)
	}
}

// Guards: REQ-117, REQ-40
func TestProblemsMissingPersona(t *testing.T) {
	m, h, _ := fixture(t)
	if got := m.Problems(context.Background()); len(got) != 0 {
		t.Fatalf("正常的家不該有問題：%q", got)
	}
	h.WithRecords(
		vault.Record{Kind: "agent", ID: "ghost", Fields: map[string]any{"duty": "x", "persona": "nobody", "account": "nowhere"}, Body: "職責"},
		vault.Record{Kind: "agent", ID: "retired", Fields: map[string]any{"duty": "x", "persona": "nobody", "account": "nowhere", "enabled": false}, Body: "職責"},
	)
	got := m.Problems(context.Background())
	if len(got) != 2 {
		t.Fatalf("ghost 的人設卡與帳號各一條，停用的 retired 不查：%q", got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"ghost", "personas/nobody.md", "nowhere"} {
		if !strings.Contains(joined, want) {
			t.Errorf("問題裡要提到 %s：%q", want, got)
		}
	}
	if strings.Contains(joined, "retired") {
		t.Errorf("停用的秘書不該被報告：%q", got)
	}
}
