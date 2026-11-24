// internal/base/agent/module.go — ai_docs/modules/agent.md（REQ-39, REQ-40, REQ-41, REQ-42, REQ-43, REQ-44, REQ-45, REQ-46, REQ-47, REQ-48, REQ-69, REQ-142）。
package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

type Roster interface {
	PersonaName(ctx context.Context, agentID string) (string, error) // 每次讀取現任人設的 name，不回退或快取
	List(ctx context.Context) ([]Agent, error)
	Resolve(ctx context.Context, nameOrAlias string) (agentID string, ok bool)
	SetSharesConversation(ctx context.Context, agentID, target, by string) error          // target "" 表示拿掉；target 不存在 → NotFound；形成循環 → Conflict
	Instructions(ctx context.Context, agentID string, sessionName string) (string, error) // P3
	AppendJournal(ctx context.Context, agentID, line string) error
	WriteMemory(ctx context.Context, agentID, body, by string) error                        // by 是 agentID 本人或 "system"，否則 Forbidden
	WriteProfile(ctx context.Context, body, by string) error                                // by 只能是 "life"，否則 Forbidden
	WriteAddendum(ctx context.Context, agentID, body, by string) error                      // by 是 owner(…) 或 agentID 本人，否則 Forbidden
	WriteDuties(ctx context.Context, agentID, body, by string) error                        // 官方職責（agent.md 內文）：只有 "system"（官方更新時用）；其他一律 Forbidden，訊息「官方職責只讀，請寫 addendum」
	Context(ctx context.Context, sessionName string) (text string, changed bool, err error) // P3
}

type Agent = *sdk.Record
type BusSender interface {
	Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error)
	Publish(ctx context.Context, topic string, payload map[string]any) error
}
type VaultStore interface {
	RegisterSchema(vault.Spec)
	Get(context.Context, string, string) (vault.Record, error)
	List(context.Context, string, vault.Query) ([]vault.Record, error)
	Put(context.Context, vault.Record, string, string) (vault.Record, error)
	Patch(context.Context, string, string, map[string]any, []string, string) (vault.Record, error)
	Append(context.Context, string, string, string, string) error
	NextID(ctx context.Context, kind string) (string, error)
	ArchiveNow(ctx context.Context, kind, id, by string) error
	Move(ctx context.Context, kind, from, to string, set map[string]any, by string) (vault.Record, error)
}
type Clock interface{ Now() time.Time }
type Deps struct {
	Bus   BusSender
	Vault VaultStore
	Clock Clock
}
type Module struct {
	log        *slog.Logger
	cancel     context.CancelFunc
	done       chan struct{}
	registered bool
	d          Deps
	home       string
	loc        *time.Location
	mu         sync.Mutex
	notesMu    sync.Mutex                          // 主題筆記的寫入與索引重產一次一件（notes.go）
	core       func() string                       // EverGenie repo 在哪（SetCoreFinder）；nil＝不知道
	appends    func(agentID string) []PromptAppend // 套件補充（SetPromptAppends）；nil＝沒有
	duties     func(ref string) (string, bool)     // 官方職責（SetOfficialDuties）；nil＝讀不到
	src        PersonaSource                       // 名冊讀角色的唯一入口（SetPersonaSource；角色庫是業務模組 feature/agents/persona，由 registry 注入）
}

func New(d Deps) *Module {
	return &Module{d: d, src: noPersonas{}}
}

// SetPersonaSource 接上名冊讀角色卡與說話層的來源（組裝時、Start 之前呼叫；registry 傳 feature/agents/persona）。
// 不放進 Deps：角色庫的 Deps 要名冊、名冊要角色庫，互相需要的一邊用 setter 接。
func (m *Module) SetPersonaSource(s PersonaSource) { m.src = s }

// PromptAppend 是套件要接在秘書指令後面的一段補充（套件 prompt_append 擴充點的值）：Source 是「<套件>.<擴充點>」，Title 是它的白話說明。
type PromptAppend struct {
	Source, Title, Text string
}

// SetPromptAppends 接上「這個職位要多帶哪些套件補充」的來源（組裝時呼叫；registry 傳套件模組的）。名冊不 import 套件。
func (m *Module) SetPromptAppends(f func(agentID string) []PromptAppend) { m.appends = f }
func (m *Module) Name() string                                           { return "agent" }

// SetOfficialDuties 接上官方職責的來源：ref 是 agent 紀錄的 duties_from（<套件>/<樣板的職位>），回那份樣板的職責全文
// （組裝時呼叫；registry 傳套件模組的）。名冊不 import 套件。
func (m *Module) SetOfficialDuties(f func(ref string) (string, bool)) { m.duties = f }

func (m *Module) SetHome(dir string) { m.home = dir }
func (m *Module) Start(ctx context.Context) error {
	if m.cancel != nil {
		return platform.Conflict("服務已啟動")
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	m.loc = h.Location()
	h.Clock = m.d.Clock
	m.log = platform.NewLogger(h, "agent")
	rows, err := m.List(ctx)
	if err != nil {

		return err

	} else if err = checkShares(rows); err != nil {
		return platform.Invalid("shares_conversation_with", "代管設定有循環或指向不存在的職位")
	}
	if !m.registered {
		for _, sc := range specs(h) {
			m.d.Vault.RegisterSchema(sc)
		}
		m.registered = true
	}
	// 主題筆記（DEV-161）：缺的官方預設分類補上（只建缺的），兩層索引照現在的筆記重產一次（主人在外面改過、刪過的也對得上）。
	if err := m.ensureDefaultCategories(ctx); err != nil {
		return err
	}
	if err := m.RebuildNoteIndexes(ctx); err != nil {
		m.warn("主題筆記的索引重產失敗", err)
	}
	return m.startEvents(ctx)
}
func (m *Module) Stop(ctx context.Context) error {
	if m.cancel == nil {
		return nil
	}
	m.cancel()
	select {
	case <-m.done:
		m.cancel = nil
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
