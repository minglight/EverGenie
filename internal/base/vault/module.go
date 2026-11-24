// internal/base/vault/module.go — ai_docs/modules/vault.md（REQ-1, REQ-2, REQ-3, REQ-4, REQ-5, REQ-6, REQ-7, REQ-8, REQ-10, REQ-216）。
package vault

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/minglight/evergenie/sdk"
)

type Store interface {
	Get(ctx context.Context, kind, id string) (Record, error)
	List(ctx context.Context, kind string, q Query) ([]Record, error)
	Put(ctx context.Context, r Record, by string, expectedRev string) (Record, error)
	Patch(ctx context.Context, kind, id string, set map[string]any, unset []string, by string) (Record, error)
	Append(ctx context.Context, kind, id, line, by string) error
	Archive(ctx context.Context, kind, id, by string) error
	NextID(ctx context.Context, kind string) (string, error)
	Watch(ctx context.Context, kinds ...string) (<-chan Change, error)
}
type Schema interface {
	Validate(Record) error
	Closed(Record) (closed bool, at time.Time) // 這筆是不是已結束（封存與群組判斷共用，規則寫在 schema）
}

// RegisterSchema installs a declaration during module startup.
func (m *Module) RegisterSchema(sc Spec) {
	m.schemaMu.Lock()
	defer m.schemaMu.Unlock()
	if sc.Kind == "" {
		panic("schema kind must not be empty")
	}
	if m.schemas == nil {
		m.schemas = make(map[string]Spec)
	}
	if root := specRoot(sc); root != "" && !vaultRoots[root] {
		panic("vault/" + root + "/ 不在核心的資料夾清單裡：先加進 ignore.go 的 vaultRoots（不然會被當成主人自己的東西略過）")
	}
	if _, exists := m.schemas[sc.Kind]; exists {
		panic("schema already registered")
	}
	m.schemas[sc.Kind] = sc
}
func (m *Module) Closed(r Record) (bool, time.Time) {
	m.schemaMu.RLock()
	sc, ok := m.schemas[r.Kind]
	m.schemaMu.RUnlock()
	if !ok {
		return false, time.Time{}
	}
	return sc.Closed(r)
}

type Query struct {
	Where           map[string]string
	IncludeArchived bool
	Page            *sdk.PageRequest
}
type Record = sdk.Record
type Change = *sdk.VaultWatchResponse
type Clock interface{ Now() time.Time }
type Deps struct {
	Clock Clock
}
type Module struct {
	d            Deps
	schemaMu     sync.RWMutex
	schemas      map[string]Spec
	lifeMu       sync.RWMutex
	home         string
	loc          *time.Location
	lockFile     *os.File
	indexMu      sync.RWMutex
	index        map[string]entry
	officialRoot string           // 官方層的資料夾（official.go）；空＝這個家沒有官方層
	officialMu   sync.RWMutex     // 顧 official；鎖的順序：先 officialMu 再 indexMu 都不行——兩把不同時拿
	official     map[string]entry // 官方層的紀錄（<種類>/<編號>）
	invalid      map[string]bool
	shards       [64]sync.Mutex
	counterMu    sync.Mutex
	counterLocks sync.Map
	counters     map[string]int64
	prefixes     map[string]string // 核心種類的編號字首（家的 [ids] 套上預設，Open 時定）
	logMu        sync.Mutex
	watchMu      sync.Mutex
	watcher      *watchState
}

func New(d Deps) *Module       { return &Module{d: d} }
func (m *Module) Name() string { return "vault" }

// SetHome configures startup without filesystem access; call before Start.
func (m *Module) SetHome(home string) { m.lifeMu.Lock(); defer m.lifeMu.Unlock(); m.home = home }
func (m *Module) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.lifeMu.RLock()
	home := m.home
	opened := m.lockFile != nil
	m.lifeMu.RUnlock()
	if opened || home == "" {
		return nil
	}
	return m.Open(home)
}
func (m *Module) Stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return m.close(ctx)
}
