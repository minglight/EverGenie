package roster

// 名冊的管理規則（Q3：evergenie agents new|rm|mv|assign|swap|account|set、customize、agents link 的資料來源）。
// 會改名冊或帳號設定的只有主人（by 是 owner(…)）或 system；附加規則另外允許秘書本人（AGT-8）。
// 資料的讀寫經底層 agent 的單筆操作（Deps.Roster）；名下的排程經擁有者 jobs（Deps.Jobs）。2026-11-01 從 internal/base/agent/manage.go 搬來。

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// IDPattern 是職位 id 的格式：小寫英文開頭，後面是小寫英文、數字、減號，共 2–31 個字。
var IDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)

func requireOwner(by string) error {
	if platform.IsOwnerOrSystem(by) {
		return nil
	}
	return platform.Forbidden("只有主人能管理秘書名冊")
}

func validID(field, id string) error {
	if !IDPattern.MatchString(id) {
		return platform.Invalid(field, "職位 id 只能用小寫英文、數字、減號，英文開頭，2 到 31 個字")
	}
	return nil
}

func (m *Module) ready() (Roster, error) {
	if m.d.Roster == nil {
		return nil, platform.Unavailable("名冊管理還沒接上")
	}
	return m.d.Roster, nil
}

// active 讀一位還沒封存的秘書；封存過的當成找不到。
func (m *Module) active(ctx context.Context, id string) (sdk.Record, error) {
	r, err := m.d.Roster.Record(ctx, "agent", id)
	if err != nil {
		return r, err
	}
	if r.Archived {
		return r, platform.NotFound("這位秘書已經封存")
	}
	return r, nil
}

// Create 建一位新秘書：職責沒給就用套件樣板的（沒有樣板是「（還沒寫）」）、空的記憶，enabled=false（不會被自動開常駐對話）。
// knowledge 是要勾的可選知識 id（DEV-161）：nil＝照職責的預設（不寫欄位），其他照給的記；不認得的 id 擋。
// 她是這個家的第一位秘書（建之前名冊含封存的一筆都沒有）時記 first_day，第一次啟用時核心排一則招呼提示給她（AGS-11，firstday.go）。
func (m *Module) Create(ctx context.Context, id, persona, display, duty string, knowledge []string, by string) (*sdk.Record, error) {
	ro, err := m.ready()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(by); err != nil {
		return nil, err
	}
	if err := validID("id", id); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := ro.LivePersona(persona); err != nil {
		return nil, err
	}
	if r, err := ro.Record(ctx, "agent", id); err == nil {
		if r.Archived {
			return nil, platform.Conflict("這個 id 封存過，請換一個名字或先還原")
		}
		return nil, platform.Conflict("已經有這位秘書")
	} else if platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		return nil, err
	}
	first := m.rosterEmpty(ctx)
	if strings.TrimSpace(duty) == "" && m.d.Templates != nil {
		duty = m.d.Templates.TemplateDuty(id) // 套件帶來的職位（官方 health 這種）用樣板寫好的職責（DEV-91）
	}
	if strings.TrimSpace(duty) == "" {
		duty = "（還沒寫）"
	}
	fields := map[string]any{"duty": duty, "persona": persona, "enabled": false}
	if first {
		fields["first_day"] = true // 啟用時才排招呼：停用的秘書收不到話（session SES-58）
	}
	if m.d.Templates != nil { // 官方套件帶來的職位：職責跟著程式走（讀樣板），家裡不抄一份（DEV-154）
		if ref := m.d.Templates.TemplateDutiesRef(id); ref != "" {
			fields["duties_from"] = ref
		}
	}
	if strings.TrimSpace(display) != "" {
		fields["display_name"] = display
	}
	if knowledge != nil {
		blocks := ro.KnowledgeOf(sdk.Record{Kind: "agent", ID: id, Fields: fields})
		ids, err := knowledgeIDs(blocks, knowledge)
		if err != nil {
			return nil, err
		}
		fields["knowledge"], fields["knowledge_seen"] = ids, blockIDs(blocks)
	}
	r, err := ro.PutAgent(ctx, id, fields, by)
	if err != nil {
		return nil, err
	}
	if err := ro.SetBody(ctx, "memory", id, "", by); err != nil {
		return nil, err
	}
	if err := ro.Changed(ctx, id, "created"); err != nil {
		return nil, err
	}
	return &r, nil
}

// belonging 是一位秘書名下的一筆資料：agent 擁有的種類經名冊搬、封存，排程（job）經 jobs。
type belonging struct {
	kind, id string
	hasAgent bool   // 有 agent 欄位（改名時一起改）
	display  string // 名冊那一筆原本的 display_name（改名失敗時改回去）
}

func (b belonging) child() bool { return strings.Contains(b.id, "/") }

// belongings 列出一位秘書名下還沒封存的資料：agent、addendum、memory，排程，再筆記、日誌、變更紀錄。
func (m *Module) belongings(ctx context.Context, id string) ([]belonging, error) {
	rows, err := m.d.Roster.Records(ctx, id)
	if err != nil {
		return nil, err
	}
	var own, children []belonging
	for _, r := range rows {
		_, has := r.Fields["agent"]
		b := belonging{kind: r.Kind, id: r.ID, hasAgent: has}
		b.display, _ = r.Fields["display_name"].(string)
		if b.child() {
			children = append(children, b)
		} else {
			own = append(own, b)
		}
	}
	var jobs []belonging
	if m.d.Jobs != nil {
		ids, err := m.d.Jobs.AgentJobIDs(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, j := range ids {
			jobs = append(jobs, belonging{kind: "job", id: j})
		}
	}
	return append(append(own, jobs...), children...), nil
}

func (m *Module) delegatesTo(ctx context.Context, id string) ([]string, error) {
	rows, err := m.d.Roster.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if target, _ := r.Fields["shares_conversation_with"].(string); target == id && r.ID != id {
			out = append(out, r.ID)
		}
	}
	return out, nil
}

// Archive 把一位秘書整包封存（只搬進 archive/，不刪除，REQ-5）。session 要先由呼叫端停掉。
func (m *Module) Archive(ctx context.Context, id, by string) ([]string, string, error) {
	ro, err := m.ready()
	if err != nil {
		return nil, "", err
	}
	if err := requireOwner(by); err != nil {
		return nil, "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.active(ctx, id); err != nil {
		return nil, "", err
	}
	if others, err := m.delegatesTo(ctx, id); err != nil {
		return nil, "", err
	} else if len(others) > 0 {
		return nil, "", platform.Conflict(strings.Join(others, "、") + " 的對話代管給它，請先拿掉代管再封存")
	}
	items, err := m.belongings(ctx, id)
	if err != nil {
		return nil, "", err
	}
	// 排程先封存：中途失敗時，至少不會留下沒有秘書的排程在跑。秘書本身最後封存。
	sort.SliceStable(items, func(i, j int) bool { return archiveOrder(items[i].kind) < archiveOrder(items[j].kind) })
	var done []string
	for _, b := range items {
		if b.kind == "job" {
			err = m.d.Jobs.ArchiveAgentJob(ctx, b.id, by)
		} else {
			err = ro.ArchiveRecord(ctx, b.kind, b.id, by)
		}
		if err != nil {
			return done, "", err
		}
		done = append(done, b.kind+"/"+b.id)
	}
	restore := "停掉 evergenie serve，把 vault/agents/" + id + "/ 底下每個 archive/<年-月>/ 資料夾裡的檔案搬回它的上一層，再啟動 serve"
	return done, restore, ro.Changed(ctx, id, "archived")
}

func archiveOrder(kind string) int {
	switch kind {
	case "job":
		return 0
	case "agent":
		return 2
	}
	return 1
}

// move 把一筆名下資料搬到新 id；agent 是 agent 欄位要改成的值（沒有這個欄位的不改），display 只給名冊那一筆。
func (m *Module) move(ctx context.Context, b belonging, to, agent string, extra map[string]any, by string) (sdk.Record, error) {
	if b.kind == "job" {
		return sdk.Record{}, m.d.Jobs.MoveAgentJob(ctx, b.id, to, agent, by)
	}
	set := map[string]any{}
	for k, v := range extra {
		set[k] = v
	}
	if b.hasAgent {
		set["agent"] = agent
	}
	return m.d.Roster.MoveRecord(ctx, b.kind, b.id, to, set, by)
}

// Rename 改職位 id：名下還沒封存的資料全部搬到新 id（排程的 id 前綴與 agent 欄位一起改），
// 別人代管指向它的也改成新 id；回傳搬過去的資料與還提到舊 id 的檔案（內文不自動改）。
func (m *Module) Rename(ctx context.Context, old, next, display, by string) (*sdk.Record, []string, []string, error) {
	ro, err := m.ready()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := requireOwner(by); err != nil {
		return nil, nil, nil, err
	}
	if err := validID("new_id", next); err != nil {
		return nil, nil, nil, err
	}
	if old == next {
		return nil, nil, nil, platform.Invalid("new_id", "新舊 id 一樣")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.active(ctx, old); err != nil {
		return nil, nil, nil, err
	}
	if _, err := ro.Record(ctx, "agent", next); err == nil {
		return nil, nil, nil, platform.Conflict("新的 id 已經有人用了（含封存過的）")
	} else if platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		return nil, nil, nil, err
	}
	items, err := m.belongings(ctx, old)
	if err != nil {
		return nil, nil, nil, err
	}
	type step struct {
		b     belonging // 搬過去之後的那一筆（id 是新的）
		from  string
		extra map[string]any // 還原時要改回去的欄位（不含 agent）
	}
	var done []step
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			s := done[i]
			if _, err := m.move(context.WithoutCancel(ctx), s.b, s.from, old, s.extra, by); err != nil && m.log != nil {
				m.log.Warn("改名失敗後還原不完整", "kind", s.b.kind, "id", s.b.id)
			}
		}
	}
	var moved []string
	var agentRec sdk.Record
	for _, b := range items {
		to := next
		if b.child() {
			to = next + strings.TrimPrefix(b.id, old)
		}
		set, undo := map[string]any{}, map[string]any{}
		if b.kind == "agent" && strings.TrimSpace(display) != "" {
			set["display_name"], undo["display_name"] = display, b.display
		}
		nr, err := m.move(ctx, b, to, next, set, by)
		if err != nil {
			rollback()
			return nil, nil, nil, err
		}
		done = append(done, step{belonging{kind: b.kind, id: to, hasAgent: b.hasAgent}, b.id, undo})
		moved = append(moved, b.kind+"/"+to)
		if b.kind == "agent" {
			agentRec = nr
		}
	}
	if others, err := m.delegatesTo(ctx, old); err == nil {
		for _, other := range others {
			if _, err := ro.PatchAgent(ctx, other, map[string]any{"shares_conversation_with": next}, nil, by); err != nil {
				return nil, moved, nil, err
			}
		}
	}
	if err := ro.RenameNoteFollower(ctx, old, next, by); err != nil {
		return &agentRec, moved, nil, err
	}
	mentions, err := platform.FilesMentioning(m.home, old, "home.toml", "settings", "vault")
	if err != nil {
		return &agentRec, moved, nil, err
	}
	if err := ro.Changed(ctx, next, "renamed"); err != nil {
		return &agentRec, moved, mentions, err
	}
	return &agentRec, moved, mentions, nil
}

// currentName 是一位秘書現在對外的名字：display_name，沒有就用人設的 name。
func (m *Module) currentName(r sdk.Record) string {
	if d, _ := r.Fields["display_name"].(string); strings.TrimSpace(d) != "" {
		return d
	}
	slug, _ := r.Fields["persona"].(string)
	if p, err := m.d.Roster.Persona(slug); err == nil {
		n, _ := p.Fields["name"].(string)
		return n
	}
	return ""
}

// assignPatch 算出換人設要改的欄位：persona 與 former_names（舊名字加在最後，不重複）。
func (m *Module) assignPatch(r sdk.Record, persona string) (map[string]any, error) {
	if cur, _ := r.Fields["persona"].(string); cur == persona {
		return nil, platform.Conflict("已經是這個角色")
	}
	p, err := m.d.Roster.LivePersona(persona)
	if err != nil {
		return nil, err
	}
	newName, _ := p.Fields["name"].(string)
	former := append([]string{}, stringList(r.Fields["former_names"])...)
	if old := m.currentName(r); old != "" && old != newName && !slices.Contains(former, old) {
		former = append(former, old)
	}
	return map[string]any{"persona": persona, "former_names": former}, nil
}

// Assign 換人設：只改 persona 與 former_names；記憶、日誌、職責、附加規則都不動（AGT-3）。
func (m *Module) Assign(ctx context.Context, id, persona, by string) error {
	ro, err := m.ready()
	if err != nil {
		return err
	}
	if err := requireOwner(by); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.active(ctx, id)
	if err != nil {
		return err
	}
	set, err := m.assignPatch(r, persona)
	if err != nil {
		return err
	}
	if _, err := ro.PatchAgent(ctx, id, set, nil, by); err != nil {
		return err
	}
	return ro.Changed(ctx, id, "assigned")
}

// Swap 兩位秘書的人設互換；第二筆寫失敗就把第一筆改回去，兩筆不會只改一筆。
func (m *Module) Swap(ctx context.Context, a, b, by string) ([]*sdk.Record, error) {
	ro, err := m.ready()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(by); err != nil {
		return nil, err
	}
	if a == b {
		return nil, platform.Invalid("b", "要兩位不同的秘書")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ra, err := m.active(ctx, a)
	if err != nil {
		return nil, err
	}
	rb, err := m.active(ctx, b)
	if err != nil {
		return nil, err
	}
	pa, _ := ra.Fields["persona"].(string)
	pb, _ := rb.Fields["persona"].(string)
	setA, err := m.assignPatch(ra, pb)
	if err != nil {
		return nil, err
	}
	setB, err := m.assignPatch(rb, pa)
	if err != nil {
		return nil, err
	}
	na, err := ro.PatchAgent(ctx, a, setA, nil, by)
	if err != nil {
		return nil, err
	}
	nb, err := ro.PatchAgent(ctx, b, setB, nil, by)
	if err != nil {
		undo := map[string]any{"persona": pa, "former_names": stringList(ra.Fields["former_names"])}
		if _, uerr := ro.PatchAgent(context.WithoutCancel(ctx), a, undo, nil, by); uerr != nil {
			return nil, platform.Internal("互換失敗，而且 " + a + " 沒有改回去，請檢查名冊")
		}
		return nil, err
	}
	if err := ro.Changed(ctx, a, "assigned"); err != nil {
		return nil, err
	}
	if err := ro.Changed(ctx, b, "assigned"); err != nil {
		return nil, err
	}
	return []*sdk.Record{&na, &nb}, nil
}

// SetAccount 改這位用的帳號（可同時改 model、effort）。常駐對話重開後才生效（由呼叫端提醒）。
func (m *Module) SetAccount(ctx context.Context, id, account, model, effort, by string) (*sdk.Record, error) {
	ro, err := m.ready()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(by); err != nil {
		return nil, err
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return nil, err
	}
	if _, ok := h.Accounts[account].(map[string]any); !ok || account == "default" {
		return nil, platform.Invalid("account", "home.toml 沒有這個帳號")
	}
	set := map[string]any{"account": account}
	if model != "" {
		set["model"] = model
	}
	if effort != "" {
		set["effort"] = effort
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.active(ctx, id); err != nil {
		return nil, err
	}
	r, err := ro.PatchAgent(ctx, id, set, nil, by)
	if err != nil {
		return nil, err
	}
	return &r, ro.Changed(ctx, id, "updated")
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// SettableFields 是 agents set 可以改的欄位（schemas/agent.md）。其他欄位有自己的指令：
// persona 用 assign、account 用 account、id 用 mv；former_names、sites、extras、shares_conversation_with 不開放。
var SettableFields = []string{"duty", "display_name", "enabled", "mode", "model", "effort", "remote_access", "color", "color_dark", "reminder_slot", "budget.max_runs_per_day"}

// SetField 改名冊紀錄的單一欄位；值是文字，依欄位轉型與檢查。選填欄位給空字串就拿掉。
func (m *Module) SetField(ctx context.Context, id, field, value, by string) (*sdk.Record, error) {
	ro, err := m.ready()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(by); err != nil {
		return nil, err
	}
	if !slices.Contains(SettableFields, field) {
		return nil, platform.Invalid("field", "這個欄位不能用 agents set 改；可以改的有 "+strings.Join(SettableFields, "、"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.active(ctx, id)
	if err != nil {
		return nil, err
	}
	set := map[string]any{}
	var unset []string
	value = strings.TrimSpace(value)
	switch field {
	case "duty", "mode":
		if value == "" {
			return nil, platform.Invalid(field, "這個欄位必填，不能拿掉")
		}
		if field == "mode" && value != "resident" && value != "ondemand" && value != "none" {
			return nil, platform.Invalid("mode", "mode 只能是 resident、ondemand、none")
		}
		set[field] = value
	case "enabled":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, platform.Invalid("enabled", "enabled 要寫 true 或 false")
		}
		set[field] = b
	case "remote_access":
		if value == "" {
			unset = append(unset, field)
			break
		}
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, platform.Invalid(field, field+" 要寫 true 或 false")
		}
		set[field] = b
	case "color", "color_dark":
		if value != "" && !colorPattern.MatchString(value) {
			return nil, platform.Invalid(field, "顏色要寫成 #rrggbb")
		}
		fallthrough
	case "display_name", "model", "effort":
		if value == "" {
			unset = append(unset, field)
		} else {
			set[field] = value
		}
	case "reminder_slot":
		h, err := platform.LoadHome(m.home)
		if err != nil {
			return nil, err
		}
		if _, ok := h.Slots[value].(map[string]any); !ok || value == "default" {
			return nil, platform.Invalid("reminder_slot", "home.toml 的 slots 沒有這一組")
		}
		set[field] = value
	case "budget.max_runs_per_day":
		budget := map[string]any{}
		if old, ok := r.Fields["budget"].(map[string]any); ok {
			for k, v := range old {
				budget[k] = v
			}
		}
		if value == "" {
			delete(budget, "max_runs_per_day")
		} else {
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return nil, platform.Invalid(field, "每天最多執行次數要是 0 以上的整數")
			}
			budget["max_runs_per_day"] = n
		}
		set["budget"] = budget
	}
	out, err := ro.PatchAgent(ctx, id, set, unset, by)
	if err != nil {
		return nil, err
	}
	if err := ro.Changed(ctx, id, "updated"); err != nil {
		return &out, err
	}
	if set["enabled"] == true {
		out = m.greetFirst(ctx, out) // 新家第一位秘書第一次啟用：排招呼提示（AGS-11）
	}
	return &out, nil
}

// blockIDs 是職責現在全部的可選知識 id（寫進 knowledge_seen：主人這次看過的；之後官方新加的照預設）。
func blockIDs(blocks []sdk.AgentKnowledgeBlock) []string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.ID)
	}
	return out
}

// knowledgeIDs 檢查要勾的 id 都在職責的可選知識裡，回照職責順序排好、不重複的清單（永遠不是 nil：空清單＝全部不勾）。
func knowledgeIDs(blocks []sdk.AgentKnowledgeBlock, want []string) ([]string, error) {
	on := map[string]bool{}
	for _, id := range want {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !slices.ContainsFunc(blocks, func(b sdk.AgentKnowledgeBlock) bool { return b.ID == id }) {
			known := make([]string, 0, len(blocks))
			for _, b := range blocks {
				known = append(known, b.ID)
			}
			if len(known) == 0 {
				return nil, platform.Invalid("knowledge", "這個職位的職責沒有可選知識，不認得「"+id+"」")
			}
			return nil, platform.Invalid("knowledge", "不認得可選知識「"+id+"」；這個職位有 "+strings.Join(known, "、"))
		}
		on[id] = true
	}
	out := []string{}
	for _, b := range blocks {
		if on[b.ID] {
			out = append(out, b.ID)
		}
	}
	return out, nil
}

// Knowledge 列出這位秘書職責裡的可選知識與有沒有勾（只讀，DEV-161）。
// 名冊上還沒有這位（新家歡迎頁派職位前）：照官方樣板的可選知識回，enabled＝預設；不是官方職位回空清單。
func (m *Module) Knowledge(ctx context.Context, id string) ([]sdk.AgentKnowledgeBlock, error) {
	ro, err := m.ready()
	if err != nil {
		return nil, err
	}
	r, err := m.active(ctx, id)
	if platform.CodeOf(err) == sdk.ErrorCodeNotFound && !r.Archived {
		if err := validID("agent", id); err != nil {
			return nil, err
		}
		fields := map[string]any{}
		if m.d.Templates != nil {
			if ref := m.d.Templates.TemplateDutiesRef(id); ref != "" {
				fields["duties_from"] = ref
			}
		}
		if len(fields) == 0 {
			return []sdk.AgentKnowledgeBlock{}, nil
		}
		return ro.KnowledgeOf(sdk.Record{Kind: "agent", ID: id, Fields: fields}), nil
	}
	if err != nil {
		return nil, err
	}
	return ro.KnowledgeOf(r), nil
}

// SetKnowledge 整份換掉主人勾的可選知識（名冊紀錄的 knowledge，DEV-161）：主人、system，或擔任這個職位的秘書本人
// （第一天聊完、主人同意後代改；別的秘書不行）；不認得的 id 擋。同時把當下全部可選 id 記進 knowledge_seen。
func (m *Module) SetKnowledge(ctx context.Context, id string, enabled []string, by string) ([]sdk.AgentKnowledgeBlock, error) {
	ro, err := m.ready()
	if err != nil {
		return nil, err
	}
	if by != id {
		if err := requireOwner(by); err != nil {
			return nil, platform.Forbidden("只有主人或擔任這個職位的秘書本人能改可選知識")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.active(ctx, id)
	if err != nil {
		return nil, err
	}
	blocks := ro.KnowledgeOf(r)
	ids, err := knowledgeIDs(blocks, enabled)
	if err != nil {
		return nil, err
	}
	out, err := ro.PatchAgent(ctx, id, map[string]any{"knowledge": ids, "knowledge_seen": blockIDs(blocks)}, nil, by)
	if err != nil {
		return nil, err
	}
	if err := ro.Changed(ctx, id, "updated"); err != nil {
		return nil, err
	}
	return ro.KnowledgeOf(out), nil
}

// AddRule 把一條新規則加進附加規則（REQ-46）：每條前面一行註記日期與誰加的；官方職責不動（REQ-48）。
// 放在哪裡照 agent.AddToAddendum（DEV-161）：一般規則接在第一個 `## ` 段落之前（沒有段落就接尾端）；
// section 不是空的就加在 `## <section>` 那一段的最後，沒有那一段就在尾端開一段
// （例「聊到什麼先看什麼」：官方職責與兼任的職位都會讀同名的這一段，AGT-43）。
func (m *Module) AddRule(ctx context.Context, id, section, rule, by string) (string, error) {
	ro, err := m.ready()
	if err != nil {
		return "", err
	}
	if by != id && !platform.IsOwner(by) {
		return "", platform.Forbidden("只有使用者或這個職位的秘書能修改附加規則")
	}
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return "", platform.Invalid("rule", "請寫要加的規則")
	}
	section = strings.TrimSpace(section)
	if err := agent.CheckAddendumRule(section, rule); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.active(ctx, id); err != nil {
		return "", err
	}
	body := ""
	if r, err := ro.Record(ctx, "addendum", id); err == nil {
		body = r.Body
	} else if platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		return "", err
	}
	body = agent.AddToAddendum(body, section, "<!-- customize add "+m.today()+" by "+by+" -->\n"+rule)
	if err := ro.SetBody(ctx, "addendum", id, body, by); err != nil {
		return "", err
	}
	return body, ro.Changed(ctx, id, "updated")
}

// today 是家的時區的今天（附加規則的註記用）；沒接時鐘用系統時鐘。
func (m *Module) today() string {
	var clock Clock = platform.SystemClock{}
	if m.d.Clock != nil {
		clock = m.d.Clock
	}
	now := clock.Now()
	if m.loc != nil {
		now = now.In(m.loc)
	}
	return now.Format("2006-01-02")
}

// Customizations 回官方職責與附加規則（只讀）。
func (m *Module) Customizations(ctx context.Context, id string) (duties, addendum string, err error) {
	ro, err := m.ready()
	if err != nil {
		return "", "", err
	}
	r, err := m.active(ctx, id)
	if err != nil {
		return "", "", err
	}
	if a, err := ro.Record(ctx, "addendum", id); err == nil {
		addendum = a.Body
	} else if platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		return "", "", err
	}
	return ro.DutiesOf(r), addendum, nil
}

// Cards 是啟用中的每位秘書給引擎的資料（agents link）：名字、別名、一句話職責、官方職責＋附加規則（附加規則在後，AGT-N3）。
// 第二個回傳值是使用者稱呼。只讀。
func (m *Module) Cards(ctx context.Context) ([]sdk.AgentCard, string, error) {
	ro, err := m.ready()
	if err != nil {
		return nil, "", err
	}
	userName := ""
	if h, err := platform.LoadHome(m.home); err == nil {
		userName = h.User.Name
	}
	rows, err := ro.List(ctx)
	if err != nil {
		return nil, "", err
	}
	out := []sdk.AgentCard{}
	for _, r := range rows {
		if enabled, _ := r.Fields["enabled"].(bool); !enabled {
			continue
		}
		name := m.currentName(*r)
		if name == "" {
			name = r.ID
		}
		var aliases []string
		add := func(s string) {
			if s != "" && s != name && !slices.Contains(aliases, s) {
				aliases = append(aliases, s)
			}
		}
		slug, _ := r.Fields["persona"].(string)
		if p, err := ro.Persona(slug); err == nil {
			n, _ := p.Fields["name"].(string)
			add(n)
			for _, a := range stringList(p.Fields["aka"]) {
				add(a)
			}
		}
		for _, f := range stringList(r.Fields["former_names"]) {
			add(f)
		}
		duty, _ := r.Fields["duty"].(string)
		body := strings.TrimRight(ro.DutiesOf(*r), "\n")
		if a, err := ro.Record(ctx, "addendum", r.ID); err == nil && !a.Archived && strings.TrimSpace(a.Body) != "" {
			if body != "" {
				body += "\n\n"
			}
			body += strings.TrimRight(a.Body, "\n")
		}
		out = append(out, sdk.AgentCard{ID: r.ID, Name: name, Aliases: aliases, Duty: duty, Body: body})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, userName, nil
}

// WriteMemory、AppendJournal 直接轉給名冊（權限由 agent 判斷：本人或 system）。
func (m *Module) WriteMemory(ctx context.Context, id, body, by string) error {
	ro, err := m.ready()
	if err != nil {
		return err
	}
	return ro.WriteMemory(ctx, id, body, by)
}

// WriteNote 轉給名冊寫秘書自己的做事筆記（權限、名字、版本由 agent 判斷）。
func (m *Module) WriteNote(ctx context.Context, id, name, body, revision, by string) (string, bool, string, error) {
	ro, err := m.ready()
	if err != nil {
		return "", false, "", err
	}
	return ro.WriteNote(ctx, id, name, body, revision, by)
}

func (m *Module) AppendJournal(ctx context.Context, id, line string) error {
	ro, err := m.ready()
	if err != nil {
		return err
	}
	return ro.AppendJournal(ctx, id, line)
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
