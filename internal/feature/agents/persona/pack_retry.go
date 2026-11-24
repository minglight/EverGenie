package persona

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
)

// 角色包的同步（REQ-283、PRS-6；2026-11-01 從 cmd/evergenie/personaspack.go 的 packRetry 搬來，改用 Jobs() 登記）：
// serve 裡只有一條路——SyncPack，一次一個；定期再試、指令的 `personas pack sync`（personas.pack_sync）、看板的「現在再試」都走它。
// 定期再試：serve 啟動 2 分鐘後第一次，之後每 30 秒看一次「到了沒」：
//   - 上一次檔案換進來了、紀錄還沒寫（套件服務還沒準備好，例如剛裝好的第一次同步；DEV-157）→ 30 秒、1 分、2 分…加倍再試，最多隔 6 小時；
//   - 其他（同步好了、已是最新、沒網路、驗證失敗）→ 6 小時後再看。
//
// 同步本身沒變化時不連網路。發行者的家與沒有角色包的版本什麼都不做。

const (
	packRetryFirst = 2 * time.Minute
	packRetryTick  = 30 * time.Second // 定時工作多久醒一次（到了才真的同步）
	packRetryEvery = 6 * time.Hour    // 一般情況多久再看一次
	packRetryQuick = 30 * time.Second // 紀錄還沒寫、或這個家從來沒同步成功過就失敗時，第一次再試的間隔（之後加倍，最多 packRetryEvery）
)

// packSync 是測試縫：預設是真的 personapack.Sync。
var packSync = personapack.Sync

// packGate 是 serve 裡角色包同步的狀態：一次一個（mu），下一次定期再試的時間與紀錄還沒寫時的間隔。
type packGate struct {
	mu      sync.Mutex
	next    time.Time     // 定期再試這個時間之前不做；零值＝下一次就做
	backoff time.Duration // 上一次要很快再試（紀錄還沒寫、或從來沒同步成功過就失敗）時的間隔；0＝不是
}

// Jobs 登記角色包的定期再試，與新角色打招呼的待送清單（每分鐘看一次，DEV-115）。
func (m *Module) Jobs() []app.Job {
	return []app.Job{
		{Name: "personas.pack_retry", Every: packRetryTick, First: packRetryFirst, Run: m.retryPack},
		{Name: "personas.intros", Every: introEvery, Run: m.sendDueIntros},
		{Name: "personas.transfer_sweep", Every: transferSweepEvery, Run: m.sweepJob},
	}
}

// syncPack 同步角色包，換進新角色時排它們的招呼（DEV-115）；serve 的 SyncPack 與指令自己同步（服務沒在跑）都走這裡。
// 檔案換進來、紀錄還沒寫完（回錯誤）也排：卡已經在了，下一次同步不會再算新。排不進去只寫 log，不算同步失敗。
func syncPack(ctx context.Context, o personapack.SyncOptions) (*personapack.SyncResult, error) {
	res, err := packSync(ctx, o)
	if res != nil && len(res.NewCards) > 0 {
		now := o.Now
		if now.IsZero() {
			now = time.Now()
		}
		if qerr := queueIntros(o.Home, res.NewCards, now); qerr != nil {
			if h, herr := platform.LoadHome(o.Home); herr == nil {
				platform.NewLogger(h, "personas-pack").Warn("新角色的招呼沒排進去", "cards", res.NewCards, "error", qerr.Error())
			}
		}
	}
	return res, err
}

var _ app.JobProvider = (*Module)(nil)

// PackSyncOptions 是 SyncPack 的參數：Pointer 空＝用這一版的指標（registry 接上的算法）。
type PackSyncOptions struct {
	Force   bool
	Pointer string
}

// SyncPack 在 serve 裡同步角色包（紀錄經 registry 接上的 Store 寫，官方角色放家外的家寫官方層）；一次一個，做完排下一次定期再試的時間。
// 不跟著呼叫的人的連線走：指令或看板中途斷了，同步照樣做完（不留半套）。
func (m *Module) SyncPack(ctx context.Context, o PackSyncOptions) (*personapack.SyncResult, error) {
	m.pack.mu.Lock()
	defer m.pack.mu.Unlock()
	return m.syncPackLocked(context.WithoutCancel(ctx), o)
}

func (m *Module) syncPackLocked(ctx context.Context, o PackSyncOptions) (*personapack.SyncResult, error) {
	pointer := o.Pointer
	if pointer == "" {
		pointer = m.packPointer()
	}
	res, err := syncPack(ctx, personapack.SyncOptions{Home: m.home, PointerPath: pointer, Force: o.Force, Now: m.d.Clock.Now(), Store: m.recordStore()})
	m.scheduleNext(res)
	if h, herr := platform.LoadHome(m.home); herr == nil {
		log := platform.NewLogger(h, "personas-pack")
		switch {
		case err != nil && res == nil:
			log.Warn("角色包同步沒有進行", "error", err.Error())
		case err != nil:
			log.Warn("角色包還沒補上", "reason", res.Reason, "records_later", res.RecordsLater)
		case res.Status == personapack.StatusSynced:
			log.Info("角色包已同步", "version", res.Version, "installed", len(res.Installed), "new_cards", res.NewCards)
		}
	}
	return res, err
}

// scheduleNext 照這次的結果排下一次定期再試（要拿著 m.pack.mu）。
func (m *Module) scheduleNext(res *personapack.SyncResult) {
	now := m.d.Clock.Now()
	if retrySoon(res) {
		switch {
		case m.pack.backoff == 0:
			m.pack.backoff = packRetryQuick
		case m.pack.backoff < packRetryEvery:
			m.pack.backoff = min(2*m.pack.backoff, packRetryEvery)
		}
		m.pack.next = now.Add(m.pack.backoff)
		return
	}
	m.pack.backoff = 0
	m.pack.next = now.Add(packRetryEvery)
}

// retrySoon：這次的結果要不要很快再試（30 秒起加倍），不等 6 小時（PRS-6）：
//   - 檔案換進來了、紀錄還沒寫（RecordsLater）；
//   - 這個家從來沒同步成功過（登記的版號是空的）就失敗，不管原因（沒網路、下載或驗證失敗…）：新家裝好時剛好沒網路，
//     不該等 6 小時才有衣服（DEV-157）。同步成功過之後的失敗照舊 6 小時，不會一直連網路。
func retrySoon(res *personapack.SyncResult) bool {
	if res == nil {
		return false
	}
	return res.RecordsLater || (res.Status == personapack.StatusFailed && res.Registered == "")
}

// samePointer：兩個指標檔路徑是不是同一個檔（整理過、解開符號連結再比）。下載版經 ~/.local/bin 的連結叫起來的指令算出
// `<程式>/versions/<版>/…`，服務算出 `<程式>/current/…`，是同一個檔（PRS-28）。
func samePointer(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	return resolvePath(a) == resolvePath(b)
}

func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func (m *Module) retryPack(ctx context.Context) error {
	m.pack.mu.Lock()
	defer m.pack.mu.Unlock()
	if now := m.d.Clock.Now(); !m.pack.next.IsZero() && now.Before(m.pack.next) {
		return nil
	}
	_, _ = m.syncPackLocked(ctx, PackSyncOptions{})
	return nil
}
