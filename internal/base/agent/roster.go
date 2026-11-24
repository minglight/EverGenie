package agent

import (
	"context"
	"strings"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

func (m *Module) List(ctx context.Context) ([]Agent, error) {
	rows, err := m.d.Vault.List(ctx, "agent", vault.Query{})
	if err != nil {
		return nil, err
	}
	out := make([]Agent, 0, len(rows))
	for i := range rows {
		out = append(out, &rows[i])
	}
	return out, nil
}

// HadAgents 回這個家的名冊上有沒有過秘書（含停用、封存的）：新家第一位秘書的判斷用（agents AGS-11）。
func (m *Module) HadAgents(ctx context.Context) (bool, error) {
	rows, err := m.d.Vault.List(ctx, "agent", vault.Query{IncludeArchived: true})
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// PersonaName reads the current binding and persona on every call.
func (m *Module) PersonaName(ctx context.Context, agentID string) (string, error) {
	r, err := m.d.Vault.Get(ctx, "agent", agentID)
	if err != nil {
		return "", err
	}
	slug, _ := r.Fields["persona"].(string)
	p, err := m.persona(slug)
	if err != nil {
		return "", err
	}
	name, _ := p.Fields["name"].(string)
	return name, nil
}

// persona 是讀人設的單一入口（不擋設計中）。套件角色（<套件id>.<slug>）的卡讀不到時不回錯：
// 回一張暫代卡（組裝指令、名冊、換名字都照常），這樣套件被移除時已經指派的秘書不會起不來；doctor 會報（check）。
// 卡經 PersonaSource 取（persona_source.go），名冊不直接碰角色庫。
func (m *Module) persona(slug string) (vault.Record, error) {
	info, err := m.src.Card(slug)
	if err != nil && personaref.IsPackageCode(slug) {
		return missingPackagePersona(slug), nil
	}
	return info.Record, err
}

// parseCard 讀人設卡的檔頭：`---` YAML 與舊的 `+++` TOML 都認。
func parseCard(data []byte) (vault.Record, error) { return personaref.ParseCard(data) }

// livePersona 跟 persona 一樣，但「設計中」的卡不能指派（回 Invalid）。
func (m *Module) livePersona(slug string) (vault.Record, error) {
	info, err := m.src.Card(slug)
	r := info.Record
	if err != nil {
		return r, err
	}
	if info.Status == personaref.StatusDesign {
		name, _ := r.Fields["name"].(string)
		return r, platform.Invalid("persona", name+" 還在設計中，先上線才能指派")
	}
	if info.Source == personaref.SourcePackage && info.Inactive != "" {
		name, _ := r.Fields["name"].(string)
		return r, platform.Invalid("persona", name+" 所屬的套件"+info.PackageID+"目前不能用（"+info.Inactive+"），先把套件開起來才能指派")
	}
	return r, nil
}

func stringList(v any) []string {
	out := []string{}
	switch xs := v.(type) {
	case []string:
		return xs
	case []any:
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
func (m *Module) Resolve(ctx context.Context, name string) (string, bool) {
	rows, err := m.List(ctx)
	if err != nil {
		return "", false
	}
	for _, r := range rows {
		if strings.EqualFold(r.ID, name) {
			return r.ID, true
		}
	}
	// 現任的名字（顯示名稱、人設的 name 與別名）優先；都對不到才看以前的名字（互換人設後兩位會共用名字，AGT-2）。
	for _, r := range rows {
		aliases := []string{}
		if d, _ := r.Fields["display_name"].(string); d != "" {
			aliases = append(aliases, d)
		}
		slug, _ := r.Fields["persona"].(string)
		p, err := m.persona(slug)
		if err == nil {
			n, _ := p.Fields["name"].(string)
			aliases = append(aliases, n)
			aliases = append(aliases, stringList(p.Fields["aka"])...)
		}
		for _, alias := range aliases {
			if alias != "" && strings.EqualFold(alias, name) {
				return r.ID, true
			}
		}
	}
	for _, r := range rows {
		for _, alias := range stringList(r.Fields["former_names"]) {
			if alias != "" && strings.EqualFold(alias, name) {
				return r.ID, true
			}
		}
	}
	return "", false
}
func (m *Module) changed(ctx context.Context, id, kind string) error {
	return m.d.Bus.Publish(ctx, "agent.changed", map[string]any{"id": id, "type": kind})
}
func checkShares(rows []Agent) error {
	edges := map[string]string{}
	for _, r := range rows {
		edges[r.ID], _ = r.Fields["shares_conversation_with"].(string)
	}
	for id := range edges {
		seen := map[string]bool{}
		for at := id; at != ""; {
			if seen[at] {
				return platform.Conflict("代管設定不可形成循環")
			}
			seen[at] = true
			next, ok := edges[at]
			if !ok {
				return platform.NotFound("找不到代管的職位")
			}
			at = next
		}
	}
	return nil
}
func (m *Module) SetSharesConversation(ctx context.Context, id, target, by string) error {
	if !platform.IsOwnerOrSystem(by) {
		return platform.Forbidden("只有使用者或系統能修改代管設定")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, err := m.List(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, r := range rows {
		if r.ID == id {
			found = true
			r.Fields["shares_conversation_with"] = target
		}
	}
	if !found {
		return platform.NotFound("找不到這個職位")
	}
	if err = checkShares(rows); err != nil {
		return err
	}
	set := map[string]any{}
	var unset []string
	if target == "" {
		unset = []string{"shares_conversation_with"}
	} else {
		set["shares_conversation_with"] = target
	}
	if _, err = m.d.Vault.Patch(ctx, "agent", id, set, unset, by); err != nil {
		return err
	}
	return m.changed(ctx, id, "updated")
}
func (m *Module) writeBody(ctx context.Context, kind, id, body, by string) error {
	r, err := m.d.Vault.Get(ctx, kind, id)
	if platform.CodeOf(err) == sdk.ErrorCodeNotFound && kind != "agent" {
		r = vault.Record{Kind: kind, ID: id, Fields: map[string]any{}}
		if kind != "profile" {
			r.Fields["agent"] = id
		}
	} else if err != nil {
		return err
	}
	r.Body = body
	_, err = m.d.Vault.Put(ctx, r, by, r.Revision)
	return err
}
func (m *Module) WriteMemory(ctx context.Context, id, body, by string) error {
	if by != id && by != "system" {
		return platform.Forbidden("只能修改自己的記憶")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.d.Vault.Get(ctx, "agent", id); err != nil {
		return err
	}
	return m.writeBody(ctx, "memory", id, body, by)
}
func (m *Module) WriteProfile(ctx context.Context, body, by string) error {
	if by != "life" {
		return platform.Forbidden("只有生活秘書能修改使用者資料")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBody(ctx, "profile", "profile", body, by)
}
func (m *Module) WriteAddendum(ctx context.Context, id, body, by string) error {
	if by != id && !platform.IsOwner(by) {
		return platform.Forbidden("只有使用者或這個職位的秘書能修改附加規則")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.d.Vault.Get(ctx, "agent", id); err != nil {
		return err
	}
	if err := m.writeBody(ctx, "addendum", id, body, by); err != nil {
		return err
	}
	return m.changed(ctx, id, "updated")
}
func (m *Module) WriteDuties(ctx context.Context, id, body, by string) error {
	if by != "system" {
		return platform.Forbidden("官方職責只讀，請寫 addendum")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, err := m.d.Vault.Get(ctx, "agent", id); err == nil && DutiesFrom(r.Fields) != "" {
		return platform.Forbidden("這個職位用官方職責（跟著程式走，家裡不存一份）；要客製請寫附加規則")
	}
	if err := m.writeBody(ctx, "agent", id, body, by); err != nil {
		return err
	}
	return m.changed(ctx, id, "updated")
}
func (m *Module) appendJournal(ctx context.Context, id, line string) error {
	if _, err := m.d.Vault.Get(ctx, "agent", id); err != nil {
		return err
	}
	now := m.d.Clock.Now().In(m.loc)
	return m.d.Vault.Append(ctx, "journal", id+"/"+now.Format("2006-01-02"), "- "+now.Format("15:04")+" "+line, id)
}
func (m *Module) AppendJournal(ctx context.Context, id, line string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.appendJournal(ctx, id, line)
}

// AppendJournalOnce 在這位秘書今天的日誌記一行，今天已經有同一行（不看時間）就不再記；同時兩個呼叫也只記一次。
// 給業務模組記「這件事發生了」用（例：reminders 記「主人完成：喝水（R-1）」，RMD-11）；名冊不知道是什麼事。
func (m *Module) AppendJournalOnce(ctx context.Context, id, line string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	journal, err := m.d.Vault.Get(ctx, "journal", id+"/"+m.d.Clock.Now().In(m.loc).Format("2006-01-02"))
	if err != nil && platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		return err
	}
	for _, l := range strings.Split(journal.Body, "\n") {
		if strings.HasSuffix(l, " "+line) {
			return nil
		}
	}
	return m.appendJournal(ctx, id, line)
}

// missingPackagePersona 是套件不在了（移除、被拒到讀不出宣告）時，已經指派的秘書用的暫代人設：
// 名字用 slug，內文講清楚原因；不讓秘書因為少一張卡而起不來（doctor 會報，Problems）。
func missingPackagePersona(code string) vault.Record {
	_, slug, _ := personaref.SplitPackageCode(code)
	return vault.Record{Fields: map[string]any{"schema": 1, "name": slug, "aka": []string{}},
		Body: "（這位秘書坐的角色 " + code + " 來自的套件現在讀不到，所以暫時沒有個性設定：用平實、有禮貌的口吻就好。請主人在設定→職位與秘書換一位角色，或把套件裝回來。）\n"}
}

// CheckAssignable 回這個人設代號現在能不能指派給新秘書（卡存在、已上線、套件有效）；不能就回白話原因。
// pkg 在驗證套件樣板的預設人設（agents/<id>/agent.md 的 persona）時用（SEC-302 S3，REQ-324）。
func (m *Module) CheckAssignable(slug string) error {
	_, err := m.livePersona(slug)
	return err
}
