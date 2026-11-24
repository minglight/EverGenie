package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Group 把同一個業務塊的幾個子模組包成一個 Module（APP-13，S8-3）：業務塊根目錄的組裝 Module 內嵌它，
// registry 只要把塊放進 app.New 的清單。
//
// 啟停跟 App 一樣：Start 依 NewGroup 的順序，任何一個失敗就把已啟動的反序停掉再回錯；Stop 反序、每個子模組有逾時、
// 錯誤收集後一起回。登記（Routes、Checks、Jobs、Board、NoticeSources）照子模組順序合併；消費者介面
// （ConsumeJobs、ConsumeChecks、ConsumeNoticeSources、ConsumeBoard）轉給實作它的子模組，沒有就什麼都不做。
// 重名不在這裡擋：App 收集時照樣擋（錯誤講的是塊名）。
type Group struct {
	name        string
	mods        []Module
	stopTimeout time.Duration

	mu      sync.Mutex
	started []Module
}

// NewGroup 建一個名叫 name 的塊；mods 的順序就是啟動順序。清單的錯誤（nil、空名稱、同名）在 Start 時回報（MOD-N4）。
func NewGroup(name string, mods ...Module) *Group {
	return &Group{name: name, mods: append([]Module(nil), mods...), stopTimeout: DefaultStopTimeout}
}

// Name 是塊名。
func (g *Group) Name() string { return g.name }

// Modules 回子模組清單（複本）。
func (g *Group) Modules() []Module { return append([]Module(nil), g.mods...) }

// SetHome 交給每個有 SetHome(string) 的子模組（registry 對每個模組都叫一次 SetHome）。
func (g *Group) SetHome(dir string) {
	for _, m := range g.mods {
		if h, ok := m.(interface{ SetHome(string) }); ok {
			h.SetHome(dir)
		}
	}
}

// Start 依序啟動子模組；某個失敗就反序停掉已啟動的，回「<子模組>：<原因>」。已經啟動過再 Start 回錯。
func (g *Group) Start(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.started) > 0 {
		return fmt.Errorf("%s 已經啟動", g.name)
	}
	if err := validateModules(g.mods); err != nil {
		return err
	}
	for _, m := range g.mods {
		if err := ctx.Err(); err != nil {
			return g.rollback(ctx, fmt.Errorf("啟動前被取消：%w", err))
		}
		if err := m.Start(ctx); err != nil {
			return g.rollback(ctx, fmt.Errorf("%s：%w", m.Name(), err))
		}
		g.started = append(g.started, m)
	}
	return nil
}

func (g *Group) rollback(ctx context.Context, cause error) error {
	if err := g.stopLocked(ctx); err != nil {
		return errors.Join(cause, fmt.Errorf("回滾時：%w", err))
	}
	return cause
}

// Stop 反序停掉已啟動的子模組，跟 App.Stop 一樣每個有逾時、不繼承呼叫者的取消、錯誤全部收集。
func (g *Group) Stop(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stopLocked(ctx)
}

// stopLocked 不繼承呼叫者的取消，但保留期限：放在 App 裡時 App 給整塊「子模組數 × 每個的逾時」（APP-13），
// 期限到了後面的子模組就不再等，整塊跟著 App 一起放棄、不會在 App 去停底層之後還在停。
func (g *Group) stopLocked(ctx context.Context) error {
	parent := context.WithoutCancel(ctx)
	if dl, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		parent, cancel = context.WithDeadline(parent, dl)
		defer cancel()
	}
	var errs []error
	for i := len(g.started) - 1; i >= 0; i-- {
		if err := stopWithTimeout(parent, g.started[i], g.stopTimeout); err != nil {
			errs = append(errs, err)
		}
	}
	g.started = nil
	return errors.Join(errs...)
}

// stopBudget 讓 App 停這一塊之前，把它自己的每個模組逾時交給子模組，並回整塊要等多久（子模組數 × 逾時，至少一份）。
// 方法沒匯出：只有 app 套件裡的 App 會叫（內嵌 *Group 的塊根 Module 也有這個方法）。
func (g *Group) stopBudget(each time.Duration) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopTimeout = each
	if n := len(g.mods); n > 1 {
		return each * time.Duration(n)
	}
	return each
}

// Routes 合併子模組的路由。
func (g *Group) Routes() []Mount {
	var all []Mount
	for _, m := range g.mods {
		if p, ok := m.(RouteProvider); ok {
			all = append(all, p.Routes()...)
		}
	}
	return all
}

// Checks 合併子模組的健檢。
func (g *Group) Checks() []Check {
	var all []Check
	for _, m := range g.mods {
		if p, ok := m.(CheckProvider); ok {
			all = append(all, p.Checks()...)
		}
	}
	return all
}

// Jobs 合併子模組的內建定時工作。
func (g *Group) Jobs() []Job {
	var all []Job
	for _, m := range g.mods {
		if p, ok := m.(JobProvider); ok {
			all = append(all, p.Jobs()...)
		}
	}
	return all
}

// Board 合併子模組的首頁資料段。
func (g *Group) Board() []BoardPart {
	var all []BoardPart
	for _, m := range g.mods {
		if p, ok := m.(BoardProvider); ok {
			all = append(all, p.Board()...)
		}
	}
	return all
}

// NoticeSources 合併子模組的通知來源。
func (g *Group) NoticeSources() []NoticeSource {
	var all []NoticeSource
	for _, m := range g.mods {
		if p, ok := m.(NoticeSourceProvider); ok {
			all = append(all, p.NoticeSources()...)
		}
	}
	return all
}

// ConsumeJobs 轉給實作 JobConsumer 的子模組。
func (g *Group) ConsumeJobs(jobs []Job) error {
	for _, m := range g.mods {
		if c, ok := m.(JobConsumer); ok {
			if err := c.ConsumeJobs(jobs); err != nil {
				return fmt.Errorf("%s：%w", m.Name(), err)
			}
		}
	}
	return nil
}

// ConsumeChecks 轉給實作 CheckConsumer 的子模組。
func (g *Group) ConsumeChecks(checks []Check) error {
	for _, m := range g.mods {
		if c, ok := m.(CheckConsumer); ok {
			if err := c.ConsumeChecks(checks); err != nil {
				return fmt.Errorf("%s：%w", m.Name(), err)
			}
		}
	}
	return nil
}

// ConsumeNoticeSources 轉給實作 NoticeConsumer 的子模組。
func (g *Group) ConsumeNoticeSources(sources []NoticeSource) error {
	for _, m := range g.mods {
		if c, ok := m.(NoticeConsumer); ok {
			if err := c.ConsumeNoticeSources(sources); err != nil {
				return fmt.Errorf("%s：%w", m.Name(), err)
			}
		}
	}
	return nil
}

// ConsumeBoard 轉給實作 BoardConsumer 的子模組。
func (g *Group) ConsumeBoard(parts []BoardPart) error {
	for _, m := range g.mods {
		if c, ok := m.(BoardConsumer); ok {
			if err := c.ConsumeBoard(parts); err != nil {
				return fmt.Errorf("%s：%w", m.Name(), err)
			}
		}
	}
	return nil
}

var (
	_ Module               = (*Group)(nil)
	_ RouteProvider        = (*Group)(nil)
	_ CheckProvider        = (*Group)(nil)
	_ JobProvider          = (*Group)(nil)
	_ BoardProvider        = (*Group)(nil)
	_ NoticeSourceProvider = (*Group)(nil)
	_ JobConsumer          = (*Group)(nil)
	_ CheckConsumer        = (*Group)(nil)
	_ NoticeConsumer       = (*Group)(nil)
	_ BoardConsumer        = (*Group)(nil)
)
