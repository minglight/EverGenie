// internal/feature/system/backupsweep/module.go — ai_docs/modules/system.md（system/backupsweep：每週的備份掃描）。
package backupsweep

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/settings"
	"github.com/minglight/evergenie/sdk"
)

// Action 是排程工作的 action（schema job）：系統級的備份掃描，排程到了由這裡做、不經過秘書。
const Action = "backup_sweep"

// alertTitle 是掃描出錯時系統通知的標題。
const alertTitle = "備份掃描出錯"

// Events 是底層 bus 給這裡的：聽排程的 schedule.fire、出錯發 system.alert。
type Events interface {
	Publish(ctx context.Context, topic string, payload map[string]any) error
	Subscribe(ctx context.Context, topics ...string) (<-chan *sdk.BusSubscribeResponse, error)
}

// Deps 都可以是 nil／空的：沒有 Bus 就不聽排程、出錯只寫日誌。
type Deps struct {
	Bus   Events
	Clock platform.Clock
	// UserHome 是使用者家目錄；空的用系統的。Probe、GitFiles 換掉「在用」檢查與 git（nil＝真的跑）。三個都給測試用。
	UserHome string
	Probe    func(Check) ([]Ref, error)
	GitFiles func(root string) ([]string, error)
}

// Module 是每週的備份掃描。
type Module struct {
	d       Deps
	mu      sync.Mutex
	home    string
	log     *slog.Logger
	loc     *time.Location
	sandbox bool
	cancel  context.CancelFunc
	run     context.Context // Start 開的、Stop 時取消；手動掃也跟著它（看板關掉連線不中斷掃描）
	wg      sync.WaitGroup
	running atomic.Bool
	stopped bool // Stop 之後不再開新的掃描（手動的也是）
	// done 每跑完一次叫一次（測試等背景那一次跑完用）；可以是 nil。
	done func(*sdk.BackupSweepReport)
}

func New(d Deps) *Module { return &Module{d: d} }

func (m *Module) Name() string { return "backupsweep" }

func (m *Module) SetHome(dir string) { m.mu.Lock(); defer m.mu.Unlock(); m.home = dir }

func (m *Module) homeDir() string { m.mu.Lock(); defer m.mu.Unlock(); return m.home }

func (m *Module) logger() *slog.Logger {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return m.log
}

func (m *Module) now() time.Time {
	if m.d.Clock != nil {
		return m.d.Clock.Now()
	}
	return platform.SystemClock{}.Now()
}

func (m *Module) location() *time.Location {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loc == nil {
		return time.Local
	}
	return m.loc
}

// Start 讀家的設定、聽排程的 schedule.fire（action backup_sweep 才做）；不掃，立刻回。
func (m *Module) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	home := m.homeDir()
	if home == "" {
		return nil
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return err
	}
	run, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.mu.Lock()
	m.log, m.loc, m.sandbox, m.cancel, m.run = platform.NewLogger(h, "backupsweep"), h.Location(), h.Runtime.Sandbox, cancel, run
	m.mu.Unlock()
	if m.d.Bus == nil {
		return nil
	}
	events, err := m.d.Bus.Subscribe(run, "schedule.fire")
	if err != nil {
		m.logger().Warn("訂閱排程失敗，每週的備份掃描不會執行", "error", err)
		return nil
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-run.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				if ev == nil || ev.Payload == nil || ev.Payload["action"] != Action {
					continue
				}
				m.scheduled(run)
			}
		}
	}()
	return nil
}

// scheduled：排程到了。沙盒家不掃（不碰這台電腦真的資料夾）；上一次還在跑就不再開一次。掃描在背景跑，不擋住事件。
func (m *Module) scheduled(ctx context.Context) {
	m.mu.Lock()
	sandbox := m.sandbox
	m.mu.Unlock()
	if sandbox {
		m.logger().Info("沙盒家不做備份掃描")
		return
	}
	if !m.running.CompareAndSwap(false, true) {
		m.logger().Info("上一次備份掃描還在跑，這次略過")
		return
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		m.running.Store(false)
		return
	}
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		defer m.running.Store(false)
		m.sweep(ctx, "system", true)
	}()
}

// manual 是手動掃一次（看板按鈕、`backup sweep --run`）：用服務的 context（收工時取消，跟叫的人斷不斷線無關；還沒 Start 就用叫的人的、不跟著取消），
// 算在 Stop 要等的工作裡。服務收工了回 false。手動的不看沙盒（主人自己按的）。
func (m *Module) manual(ctx context.Context, by string, prune bool) (*sdk.BackupSweepReport, bool) {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil, false
	}
	run := m.run
	if run == nil {
		run = context.WithoutCancel(ctx)
	}
	m.wg.Add(1)
	m.mu.Unlock()
	defer m.wg.Done()
	return m.sweep(run, by, prune), true
}

func (m *Module) Stop(context.Context) error {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel, m.stopped = nil, true
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.wg.Wait()
	return nil
}

// sweep 做一次：prune＝先照保留規則清 runtime/backups/（BSW-5），再掃、寫報告；任何一步出錯都發 system.alert（BSW-7）。
func (m *Module) sweep(ctx context.Context, by string, prune bool) (rep *sdk.BackupSweepReport) {
	home := m.homeDir()
	loc := m.location()
	start := m.now()
	defer func() {
		if p := recover(); p != nil {
			m.logger().Error("備份掃描當掉了", "panic", fmt.Sprint(p))
			rep = &sdk.BackupSweepReport{At: start, By: by, Errors: []string{"掃描程式當掉了，這次沒有報告"}}
			m.alert(ctx, rep)
		}
		if m.done != nil {
			m.done(rep)
		}
	}()
	var errs []string
	var pruned []settings.Backup
	if prune {
		deleted, err := settings.AutoPrune(home, start)
		if err != nil {
			errs = append(errs, "照保留規則清 runtime/backups/ 沒做成："+err.Error())
		}
		pruned = deleted
	}
	userHome := m.d.UserHome
	if userHome == "" {
		var err error
		if userHome, err = os.UserHomeDir(); err != nil {
			errs = append(errs, "找不到使用者家目錄，沒辦法掃")
			rep = &sdk.BackupSweepReport{At: start, By: by, Errors: errs}
			m.finish(ctx, home, rep, loc)
			return rep
		}
	}
	cfg, err := LoadConfig(home, userHome)
	if err != nil {
		if cfg.UserHome == "" {
			errs = append(errs, err.Error())
			rep = &sdk.BackupSweepReport{At: start, By: by, Errors: errs}
			m.finish(ctx, home, rep, loc)
			return rep
		}
		errs = append(errs, err.Error())
	}
	cfg.Probe, cfg.GitFiles = m.d.Probe, m.d.GitFiles
	rep = Scan(ctx, cfg, start, loc)
	rep.By = by
	if ctx.Err() != nil {
		// 服務收工把掃描切斷了：不完整的不寫（留著上一次完整的報告）、不通知，下次排程再掃。
		m.logger().Info("備份掃描做到一半服務收工了，這次不寫報告")
		rep.Errors = append(errs, "掃描做到一半被中斷了（服務重開或收工），這次沒寫報告")
		return rep
	}
	rep.Errors = append(errs, rep.Errors...)
	for _, b := range pruned {
		rep.Pruned++
		rep.PrunedBytes += b.Size
	}
	rep.TookMs = m.now().Sub(start).Milliseconds()
	m.finish(ctx, home, rep, loc)
	return rep
}

// finish 寫報告；寫不了也算出錯。有錯就通知主人。
func (m *Module) finish(ctx context.Context, home string, rep *sdk.BackupSweepReport, loc *time.Location) {
	if err := saveReport(home, rep, loc); err != nil {
		rep.Errors = append(rep.Errors, err.Error())
	}
	del, used, unchecked := Counts(rep)
	m.logger().Info("備份掃描做完了", "by", rep.By, "deletable", del, "in_use", used, "unchecked", unchecked,
		"misnamed", len(rep.Misnamed), "current", rep.Current, "skipped", len(rep.Skipped), "pruned", rep.Pruned, "errors", len(rep.Errors))
	if len(rep.Errors) > 0 {
		m.alert(ctx, rep)
	}
}

// alert 發 system.alert（inbox 塊的 notify 把它變成給主人的通知）；發不出去只寫日誌。
func (m *Module) alert(ctx context.Context, rep *sdk.BackupSweepReport) {
	detail := strings.Join(rep.Errors, "；")
	if rep.File != "" {
		detail += "。報告：" + rep.File
	}
	if m.d.Bus == nil {
		m.logger().Warn("備份掃描出錯，沒有 bus 可以通知", "detail", detail)
		return
	}
	if err := m.d.Bus.Publish(context.WithoutCancel(ctx), "system.alert", map[string]any{"title": alertTitle, "context": detail}); err != nil {
		m.logger().Warn("送不出系統通知", "title", alertTitle, "error", err)
	}
}

var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
)
