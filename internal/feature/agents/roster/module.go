// internal/feature/agents/roster/module.go — ai_docs/modules/agents.md。
package roster

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Roster 是底層 agent 提供的單筆紀錄操作與記憶、日誌寫入（manage.go 的名冊管理規則經它讀寫；記憶與日誌的權限由 agent 判斷）。
type Roster interface {
	List(ctx context.Context) ([]*sdk.Record, error)
	Record(ctx context.Context, kind, id string) (sdk.Record, error)
	DutiesOf(r sdk.Record) string // 職責全文：官方職位讀樣板，自建職位讀紀錄內文
	// KnowledgeOf 列出職責裡的可選知識與這筆紀錄有沒有勾（紀錄的 knowledge；沒有欄位照預設，DEV-161）
	KnowledgeOf(r sdk.Record) []sdk.AgentKnowledgeBlock
	Records(ctx context.Context, id string) ([]sdk.Record, error)
	PutAgent(ctx context.Context, id string, fields map[string]any, by string) (sdk.Record, error)
	PatchAgent(ctx context.Context, id string, set map[string]any, unset []string, by string) (sdk.Record, error)
	SetBody(ctx context.Context, kind, id, body, by string) error
	ArchiveRecord(ctx context.Context, kind, id, by string) error
	MoveRecord(ctx context.Context, kind, from, to string, set map[string]any, by string) (sdk.Record, error)
	Changed(ctx context.Context, id, what string) error
	Persona(slug string) (sdk.Record, error)
	LivePersona(slug string) (sdk.Record, error)
	WriteMemory(ctx context.Context, id, body, by string) error
	WriteNote(ctx context.Context, id, name, body, revision, by string) (final string, created bool, rev string, err error)
	AppendJournal(ctx context.Context, id, line string) error
	// RenameNoteFollower：改名時主題筆記 follow 裡的舊 id 換成新的、舊位置的職位索引收掉並重產（DEV-161）。
	RenameNoteFollower(ctx context.Context, old, next, by string) error
	// HadAgents：名冊上有沒有過秘書（含停用、封存的）；新家第一位秘書的判斷（AGS-11）。
	HadAgents(ctx context.Context) (bool, error)
}

// AgentJobs 是 jobs 提供的「秘書名下的排程」操作：封存、改名秘書時用（job 的擁有者是 jobs）。
type AgentJobs interface {
	AgentJobIDs(ctx context.Context, agent string) ([]string, error)
	ArchiveAgentJob(ctx context.Context, id, by string) error
	MoveAgentJob(ctx context.Context, from, to, agent, by string) error
}

// Clock 給附加規則的日期註記。
type Clock interface{ Now() time.Time }

// manager 是 HTTP 服務要的名冊管理（manage.go 的 Module 方法）；測試換成假的（Module.ops）。
type manager interface {
	Create(ctx context.Context, id, persona, display, duty string, knowledge []string, by string) (*sdk.Record, error)
	Archive(ctx context.Context, id, by string) ([]string, string, error)
	Rename(ctx context.Context, old, next, display, by string) (*sdk.Record, []string, []string, error)
	Assign(ctx context.Context, id, persona, by string) error
	Swap(ctx context.Context, a, b, by string) ([]*sdk.Record, error)
	SetAccount(ctx context.Context, id, account, model, effort, by string) (*sdk.Record, error)
	SetField(ctx context.Context, id, field, value, by string) (*sdk.Record, error)
	AddRule(ctx context.Context, id, section, rule, by string) (string, error)
	Customizations(ctx context.Context, id string) (string, string, error)
	Cards(ctx context.Context) ([]sdk.AgentCard, string, error)
	WriteMemory(ctx context.Context, id, body, by string) error
	WriteNote(ctx context.Context, id, name, body, revision, by string) (final string, created bool, rev string, err error)
	AppendJournal(ctx context.Context, id, line string) error
	Knowledge(ctx context.Context, id string) ([]sdk.AgentKnowledgeBlock, error)
	SetKnowledge(ctx context.Context, id string, enabled []string, by string) ([]sdk.AgentKnowledgeBlock, error)
}

// RecordReader 讀 vault 紀錄（assign 之後回新的名冊列）。
type RecordReader interface {
	Get(ctx context.Context, kind, id string) (sdk.Record, error)
}

// CardWriter 是 engine 把名冊交給各帳號引擎的方法（agents link）。
type CardWriter interface {
	WriteAgentCards(ctx context.Context, cards []sdk.AgentCard, userName string, apply bool) ([]sdk.AgentLinkAccount, error)
}

// Deps 的欄位都是使用方（這個套件）定義的小 interface，由 cmd/evergenie/registry.go 注入（MOD-2）。
type Deps struct {
	Roster    Roster       // 底層 agent：單筆紀錄操作、記憶、日誌
	Jobs      AgentJobs    // 名下的排程（jobs）：可以是 nil（就是沒有排程）
	Clock     Clock        // 可以是 nil（系統時鐘）
	Vault     RecordReader // 可以是 nil（assign 回 unavailable）
	Mail      Mailer       // 送第一位秘書的招呼提示（底層 bus，AGS-11）：可以是 nil（就是不送）
	Cards     CardWriter
	Directory Directory // 看板的名冊（web.get_roster、get_agents）：可以是 nil（就是空名冊）
	Avatars   Avatars   // 看板名冊的動態頭像網址：可以是 nil（都沒有）
	Health    Health    // 名冊的健檢（checks.go）：可以是 nil（只檢查事故負責人）
	Templates Templates // 套件帶來的職位樣板（建秘書沒給職責時用樣板的一句話職責，DEV-91）：可以是 nil
}

// Templates 查啟用中套件的職位樣板：職位 id → 一句話職責；沒有回空字串。
type Templates interface {
	TemplateDuty(id string) string
	// TemplateDutiesRef 回職位 id 對得上的官方職責樣板（<套件>/<職位>，記進 agent 紀錄的 duties_from）；沒有回空字串。
	// 跟 TemplateDuty 一樣只看啟用中的官方套件（PKG-63）。
	TemplateDutiesRef(id string) string
}

// Module 沒有自己的資料與背景工作：名冊管理的規則（manage.go）經底層 agent 讀寫；Start 只開 logger、讀時區。
type Module struct {
	d    Deps
	home string
	log  *slog.Logger
	loc  *time.Location
	mu   sync.Mutex // 一次只做一件名冊管理（建、封存、改名、換角色…），檢查與寫入之間不被別的管理打斷
	ops  manager    // HTTP 服務用的名冊管理：就是 Module 自己；沒接名冊是 nil（回 unavailable）；測試換成假的
}

func New(d Deps) *Module {
	m := &Module{d: d}
	if d.Roster != nil {
		m.ops = m
	}
	return m
}
func (m *Module) Name() string       { return "roster" }
func (m *Module) SetHome(dir string) { m.home = dir }

func (m *Module) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.home == "" {
		return nil
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	m.log = platform.NewLogger(h, "agents")
	m.loc = h.Location()
	return nil
}

func (m *Module) Stop(ctx context.Context) error { return nil }

// 登記方法：Routes 在 http.go，Checks 在 checks.go，Commands 在 cli.go（套件層級函式）。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
	_ app.CheckProvider = (*Module)(nil)
)
