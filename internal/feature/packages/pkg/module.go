// internal/feature/packages/pkg/module.go — ai_docs/modules/packages.md（REQ-91, REQ-92, REQ-93, REQ-94, REQ-95, REQ-96, REQ-97, REQ-98, REQ-158）。
package pkg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"log/slog"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

type AgentLookup interface {
	Resolve(ctx context.Context, nameOrAlias string) (string, bool)
}
type BusSender interface {
	Publish(ctx context.Context, topic string, payload map[string]any) error
}
type VaultStore interface {
	NextID(ctx context.Context, kind string) (string, error)
	// Get 讀一筆紀錄（含封存的）；找不到回 NOT_FOUND。prompt_append 的 until_record 用它看那筆紀錄在不在（DEV-157）。
	Get(ctx context.Context, kind, id string) (vault.Record, error)
	// SetPackageSchemas 把這個套件的資料種類整批換成 specs（空＝拿掉，資料留在原地）；每次重掃後呼叫（D-28、REQ-173）。
	SetPackageSchemas(pkgID string, specs []vault.Spec)
}

// busSubscriber：組裝時接的 bus 也能訂閱事件（認領通知的回答，SEC-302 S9）；測試的假 bus 只會 Publish 時不訂閱。
type busSubscriber interface {
	Subscribe(ctx context.Context, topics ...string) (<-chan *sdk.BusSubscribeResponse, error)
}
type Deps struct {
	Agents AgentLookup
	Bus    BusSender
	Vault  VaultStore
	Clock  platform.Clock
}

// ExternalSupervisor 是「已經有別的模組在監督」的附程式套件（目前是引擎，由 engine 模組啟動並握手）。
// pkg 只把它列進清單、顯示狀態，不重複編譯或啟動。binary 是 program.entry 的最後一段。
type ExternalSupervisor interface {
	Claim(binary string) (state sdk.PackageState, detail string, claimed bool)
}

// Runner 跑編譯（go build）；測試換成假的，不碰真的 go。env 是加在 goEnv 後面的（同名的以 env 為準），例如 GOWORK（sdkwork.go）。
type Runner interface {
	Run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error)
}

// execRunner 是編譯用的（只叫 go）：環境照 goEnv（GOTOOLCHAIN=local、不帶外面的 GOROOT），go 不會自己去下載別的版本。
type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(goEnv(), env...)
	// 自己給了 Env，exec 就不幫忙把 PWD 換成 Dir（留著的是服務自己的）；照 exec 沒給 Env 時的做法補上，go 認的目前資料夾才是
	// 這裡給的寫法，跟工作區 go.work 寫的套件資料夾逐字一樣（PKG-66）。
	if dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			cmd.Env = append(cmd.Env, "PWD="+abs)
		}
	}
	return cmd.CombinedOutput()
}

const (
	maxRestarts      = 5                // 連續失敗幾次之後不再自動重啟
	healthyAfter     = 60 * time.Second // 報到後跑滿這麼久才算「健康」，失敗次數歸零
	defaultHelloWait = 15 * time.Second
	pollInterval     = 2 * time.Second
	crashAlertEvery  = 10 * time.Minute // 同一個套件「掛了」的通知最多這麼久一次（LOG-6）
)

type Module struct {
	d Deps

	// 以下可由組裝或測試調整（Start 之前設定）。
	RestartDelay   time.Duration
	HelloTimeout   time.Duration
	runner         Runner
	lookGo         func() (string, error) // 編譯用的 go（預設 tools.look）；測試換掉
	tools          *goTools               // 找 Go、主人同意後下載 Go（toolchain.go）
	launch         func(ctx context.Context, bin, dir string, env []string, logPath string) (*process, error)
	git            func(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) // 測試換掉（預設真的 git）
	coreVersion    func() string                                                                                // 測試換掉（預設 platform.Version）
	async          func(func())
	manual         bool                                                   // 測試：不訂閱認領的回答（監督由測試直接呼叫 Tick）
	atomicWrite    func(path string, data []byte, mode os.FileMode) error // 測試換掉（預設 platform.AtomicWrite；AddPackagePersona 用）
	editedOfficial func(ctx context.Context, home string) []string        // 測試換掉（預設 EditedOfficial；packages.official_edited 健檢用）

	migrations MigrationLog // 資料轉換跑過的紀錄（組裝點接 update 的 settings/migrations.toml）；nil＝不在載入時做轉換

	mu         sync.Mutex
	home       string
	officialAt string
	external   ExternalSupervisor
	engines    EngineStatuses // 引擎套件的狀態（packages.engine 健檢；SetEngines）
	log        *slog.Logger
	run        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	done       chan struct{}

	entries    map[string]*entry  // 目前有效的（每個 id 一個）
	rejected   []*entry           // 因為同名衝突被擋下的分享套件
	shadowed   map[string]*loaded // 被 mine 蓋掉的官方／分享套件（fork 的基準來源）
	order      []string           // 相依排序後的 id
	seen       map[string]string  // 資料夾 → 已套用的指紋
	pend       map[string]string  // 資料夾 → 上一輪看到、還沒穩定的指紋
	buildSeq   uint64             // 編譯的流水號（entry.buildID）
	warnings   []string
	alerts     []alert
	crashAlert map[string]time.Time // 套件 id → 上次發「掛了」通知的時間

	installing bool // InstallGo 正在下載

	sync     SyncHost          // packages sync 用的別的模組的能力（SetSyncHost）
	claim    ClaimHost         // 認領職位的通知用的別的模組的能力（SetClaimHost，SEC-302 S9）
	claimMu  sync.Mutex        // 認領通知的發送與回答一次一個（不持有 mu 呼叫 Sync）
	dataSigs map[string]string // 套件 id → 已經註冊進 vault 的資料種類內容（沒變就不重建索引）
}

type alert struct{ title, context string }

func New(d Deps) *Module {
	if d.Clock == nil {
		d.Clock = platform.SystemClock{}
	}
	tools := newGoTools()
	m := &Module{d: d, RestartDelay: 5 * time.Second, HelloTimeout: defaultHelloWait, runner: execRunner{}, lookGo: tools.look, tools: tools, launch: startProgram}
	m.async = func(f func()) { m.wg.Add(1); go func() { defer m.wg.Done(); f() }() }
	return m
}
func (m *Module) Name() string { return "pkg" }

// SetHome 設定家的位置；沒設定時 Start 什麼都不做（模組形狀測試用）。
// MigrationLog 是「哪些資料轉換跑過了」的紀錄（家的 settings/migrations.toml，由 update 模組管；pkg 不 import 它）。
type MigrationLog interface {
	Applied(home string) ([]string, error)
	Mark(home string, ids []string) error
}

// SetMigrationLog 接上轉換紀錄：接了之後，主人自己的與別人分享的套件在載入時自己做還沒跑過的資料轉換（migrateOwnerPackagesLocked）。
func (m *Module) SetMigrationLog(l MigrationLog) { m.mu.Lock(); m.migrations = l; m.mu.Unlock() }

func (m *Module) SetHome(home string) { m.mu.Lock(); m.home = home; m.mu.Unlock() }

// SetOfficialDir 指定官方套件的資料夾（沒指定時依 officialDir 的順序找）。
func (m *Module) SetOfficialDir(dir string) { m.mu.Lock(); m.officialAt = dir; m.mu.Unlock() }

// SetExternal 接上「已經有人監督」的套件（引擎）。
func (m *Module) SetExternal(e ExternalSupervisor) { m.mu.Lock(); m.external = e; m.mu.Unlock() }

// Warnings 回傳目前的警告（例如設定引用了已被移除的擴充點）；看板與 doctor 顯示，不影響運作。
func (m *Module) Warnings() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.warnings...)
}

// Start 掃描套件並開始監督。套件自己的任何問題（壞掉的 manifest、缺相依、編不過）都只讓那個套件 rejected／failed，
// 絕不讓 Start 失敗：serve 照常起來（REQ-158）。
func (m *Module) Start(ctx context.Context) error {
	if t := m.tools; t != nil && t.dir != "" {
		cleanGoLeftovers(t.dir) // 上次下載 Go 被中斷留下的暫存（PKG-65）
	}
	m.mu.Lock()
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.home == "" || m.cancel != nil {
		m.mu.Unlock()
		return nil
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		// 家有問題 serve 前面就會擋下；這裡保守地當作沒有套件。
		m.mu.Unlock()
		return nil
	}
	h.Clock = m.d.Clock
	m.log = platform.NewLogger(h, "pkg")
	m.run, m.cancel = context.WithCancel(ctx)
	m.done = make(chan struct{})
	run, done := m.run, m.done
	m.seen, m.pend, m.crashAlert = map[string]string{}, map[string]string{}, map[string]time.Time{}
	m.rescanLocked(m.d.Clock.Now())
	m.seen = m.scanFingerprintsLocked()
	m.mu.Unlock()
	// 啟動時先監督一次（開機就把套件行程叫起來，不等第一個間隔）；之後每 2 秒是 Jobs() 登記的 packages.supervise。
	m.Tick(run, m.d.Clock.Now())
	if m.manual {
		close(done)
		return nil
	}
	// 認領職位通知的回答（SEC-302 S9）：bus 能訂閱才接；訂閱不到不影響套件監督。
	var answers <-chan *sdk.BusSubscribeResponse
	if sub, ok := m.d.Bus.(busSubscriber); ok {
		if ch, err := sub.Subscribe(run, "notice.answered"); err == nil {
			answers = ch
		}
	}
	if answers == nil {
		close(done)
		return nil
	}
	go func() {
		defer close(done)
		defer func() {
			if recover() != nil {
				m.mu.Lock()
				m.log.Warn("認領職位的回答處理中斷")
				m.mu.Unlock()
			}
		}()
		for {
			select {
			case <-run.Done():
				return
			case ev, ok := <-answers:
				if !ok {
					return
				}
				if ev != nil && ev.Topic == "notice.answered" {
					if err := m.handleClaimAnswer(run, ev.Payload); err != nil && run.Err() == nil {
						m.warn("無法處理認領職位通知的回答")
					}
				}
			}
		}
	}()
	return nil
}

// Jobs 登記套件監督（每 2 秒：檔案有變就重載、行程掛了依設定重啟、報到逾時）；Start 之前或 Stop 之後跑是空操作。
func (m *Module) Jobs() []app.Job {
	return []app.Job{{Name: "packages.supervise", Every: pollInterval, Run: func(ctx context.Context) error {
		m.Tick(ctx, m.d.Clock.Now())
		return nil
	}}}
}

var _ app.JobProvider = (*Module)(nil)

func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	if m.cancel == nil {
		m.mu.Unlock()
		return nil
	}
	m.cancel()
	done := m.done
	var procs []*process
	for _, e := range m.entries {
		if e.proc != nil {
			procs = append(procs, e.proc)
			e.proc.kill()
		}
	}
	m.mu.Unlock()
	finished := make(chan struct{})
	go func() { m.wg.Wait(); close(finished) }()
	for _, c := range []<-chan struct{}{done, finished} {
		select {
		case <-c:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, p := range procs {
		select {
		case <-p.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	m.cancel, m.run = nil, nil
	m.mu.Unlock()
	return nil
}

// 路徑。
func (m *Module) tierDir(tier sdk.Tier) string {
	if tier == sdk.TierOfficial {
		return m.officialDir()
	}
	if tier == sdk.TierInstalled {
		return platform.LayoutFor(m.home).InstalledPackages() // 別人的套件可重裝；第 2 版在狀態資料夾
	}
	return filepath.Join(m.home, "packages", string(tier))
}
func (m *Module) runtimeDir(id string) string {
	return platform.RuntimeDir(m.home, "packages", id)
}
func (m *Module) dataDir(id string) string { return filepath.Join(m.home, "vault", "pkg", id) }

// officialDir：明確指定 > EVERGENIE_OFFICIAL_PACKAGES_DIR > <家>/packages/official（存在才用）> 執行檔旁 ../packages/official > 執行檔旁 packages/official。
func (m *Module) officialDir() string { return resolveOfficialDir(m.officialAt, m.home) }

// OfficialDir 是不經過模組實體時官方套件資料夾在哪（同 officialDir 的順序，沒有 SetOfficialDir 那一步）；組裝與角色包用。
func OfficialDir(home string) string { return resolveOfficialDir("", home) }

// PersonasPackPointer 是角色包指標 pack.toml 的路徑（官方套件 personas 裡，REQ-283）；環境變數 EVERGENIE_PERSONAS_PACK_POINTER 優先（測試與發版整套測試用）。
func PersonasPackPointer(home string) string {
	if v := sdk.Getenv("EVERGENIE_PERSONAS_PACK_POINTER"); v != "" {
		return v
	}
	return filepath.Join(OfficialDir(home), "personas", "pack.toml")
}

// PersonasBundledCards 是跟著程式的公開角色卡資料夾（官方套件 personas 的 cards/，PRS-25）：發行檔帶著、在那一版的程式資料夾裡（唯讀，換版跟著換）。
func PersonasBundledCards(home string) string { return bundledCardsIn(OfficialDir(home)) }

// bundledCardsIn 是官方套件資料夾 official 裡跟著程式的公開卡（官方套件 personas 的 cards/）；套件子行程的 EVERGENIE_BUNDLED_PERSONAS 也用這個。
func bundledCardsIn(official string) string { return filepath.Join(official, "personas", "cards") }

func resolveOfficialDir(officialAt, home string) string {
	if officialAt != "" {
		return officialAt
	}
	if v := sdk.Getenv("EVERGENIE_OFFICIAL_PACKAGES_DIR"); v != "" {
		return v
	}
	inHome := filepath.Join(home, "packages", "official")
	if isDir(inHome) {
		return inHome
	}
	// 執行檔旁邊：照原樣的路徑先（下載版的 current/bin/.. 就是那一版），經過符號連結叫起來的再看解開後的位置。
	for _, dir := range platform.ExecutableDirs() {
		for _, rel := range []string{"../packages/official", "packages/official"} {
			if p := filepath.Join(dir, rel); isDir(p) {
				return p
			}
		}
	}
	return inHome
}

func isDir(p string) bool { i, err := os.Stat(p); return err == nil && i.IsDir() }
