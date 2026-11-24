package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// DefaultStopTimeout 是每個模組 Stop 的預設逾時（MOD-4）。
const DefaultStopTimeout = 10 * time.Second

// App 把模組依序啟停，並收集它們的登記。建立後模組清單不再變。
type App struct {
	mods        []Module
	stopTimeout time.Duration

	mu        sync.Mutex
	started   []Module // 已成功 Start 的，依啟動順序
	assembled bool     // Assemble 成功過
}

// New 建立 App。mods 的順序就是啟動順序（由低層到高層，MOD-3）。
// 模組清單的錯誤（nil、同名）在 Start 時回報，因為 New 不做事（MOD-N4）。
func New(mods ...Module) *App {
	return &App{mods: append([]Module(nil), mods...), stopTimeout: DefaultStopTimeout}
}

// WithStopTimeout 改每個模組 Stop 的逾時（要在 Start 之前呼叫），回自己方便串接。
func (a *App) WithStopTimeout(d time.Duration) *App {
	a.stopTimeout = d
	return a
}

// Modules 回模組清單（複本）。
func (a *App) Modules() []Module { return append([]Module(nil), a.mods...) }

func (a *App) validate() error { return validateModules(a.mods) }

// validateModules 檢查模組清單：不能有 nil、空名稱、同名（App 與 Group 共用）。
func validateModules(mods []Module) error {
	seen := map[string]bool{}
	for i, m := range mods {
		if m == nil {
			return fmt.Errorf("第 %d 個模組是 nil", i+1)
		}
		name := m.Name()
		if name == "" {
			return fmt.Errorf("第 %d 個模組沒有名稱", i+1)
		}
		if seen[name] {
			return fmt.Errorf("模組名稱重複：%s", name)
		}
		seen[name] = true
	}
	return nil
}

// Start 依序啟動所有模組。任何一個失敗，就把已經啟動的反序停掉，再回錯（不學參考專案的遇錯繼續）。
// 失敗的那個模組本身不會被 Stop（它沒有啟動成功）。已經啟動過就再 Start 會回錯。
func (a *App) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.started) > 0 {
		return errors.New("App 已經啟動")
	}
	if err := a.validate(); err != nil {
		return err
	}
	if err := a.assembleLocked(); err != nil {
		return err
	}
	for _, m := range a.mods {
		if err := ctx.Err(); err != nil {
			return a.rollback(ctx, fmt.Errorf("啟動前被取消：%w", err))
		}
		if err := m.Start(ctx); err != nil {
			return a.rollback(ctx, fmt.Errorf("啟動 %s 失敗：%w", m.Name(), err))
		}
		a.started = append(a.started, m)
	}
	return nil
}

// Assemble 把收集到的登記交給清單裡的消費者（JobConsumer、CheckConsumer、NoticeConsumer、BoardConsumer）；登記有錯（重名、缺欄位）
// 或消費者不收就回錯。只做一次：registry 在 Build 時先呼叫，在啟動前擋下錯誤；Start 也會呼叫（已組過就略過）。
// 沒有消費者的登記不收集（不會因為它回錯）。
func (a *App) Assemble() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.validate(); err != nil {
		return err
	}
	return a.assembleLocked()
}

func (a *App) assembleLocked() error {
	if a.assembled {
		return nil
	}
	for _, m := range a.mods {
		if c, ok := m.(JobConsumer); ok {
			jobs, err := a.Jobs()
			if err != nil {
				return err
			}
			if err := c.ConsumeJobs(jobs); err != nil {
				return fmt.Errorf("%s 不收定時工作：%w", m.Name(), err)
			}
		}
		if c, ok := m.(CheckConsumer); ok {
			checks, err := a.Checks()
			if err != nil {
				return err
			}
			if err := c.ConsumeChecks(checks); err != nil {
				return fmt.Errorf("%s 不收健檢：%w", m.Name(), err)
			}
		}
		if c, ok := m.(NoticeConsumer); ok {
			sources, err := a.NoticeSources()
			if err != nil {
				return err
			}
			if err := c.ConsumeNoticeSources(sources); err != nil {
				return fmt.Errorf("%s 不收通知來源：%w", m.Name(), err)
			}
		}
		if c, ok := m.(BoardConsumer); ok {
			parts, err := a.Board()
			if err != nil {
				return err
			}
			if err := c.ConsumeBoard(parts); err != nil {
				return fmt.Errorf("%s 不收首頁資料段：%w", m.Name(), err)
			}
		}
	}
	a.assembled = true
	return nil
}

// rollback 把已啟動的反序停掉；回的錯誤包著原因，回滾本身的錯誤一併列出。呼叫時要持有 a.mu。
func (a *App) rollback(ctx context.Context, cause error) error {
	if err := a.stopLocked(ctx); err != nil {
		return errors.Join(cause, fmt.Errorf("回滾時：%w", err))
	}
	return cause
}

// Stop 反序停掉已啟動的模組，每個有逾時（從 ctx 帶值但不繼承它的取消：關機的 ctx 常常已經取消了，不能讓 Stop 一開始就逾時）；某個失敗或逾時不影響其他的，錯誤全部收集後一起回。
// 沒有啟動過或已停掉就什麼都不做。
func (a *App) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopLocked(ctx)
}

func (a *App) stopLocked(ctx context.Context) error {
	timeout := a.stopTimeout
	if timeout <= 0 {
		timeout = DefaultStopTimeout
	}
	var errs []error
	for i := len(a.started) - 1; i >= 0; i-- {
		m := a.started[i]
		budget := timeout
		if g, ok := m.(budgetedStop); ok { // 業務塊（APP-13）：每個子模組各一份逾時
			budget = g.stopBudget(timeout)
		}
		if err := stopWithTimeout(context.WithoutCancel(ctx), m, budget); err != nil {
			errs = append(errs, err)
		}
	}
	a.started = nil
	return errors.Join(errs...)
}

// budgetedStop 是業務塊（內嵌 *Group）：App 停它之前問整塊要等多久（group.go 的 stopBudget）。
type budgetedStop interface {
	stopBudget(each time.Duration) time.Duration
}

// stopWithTimeout 在獨立 goroutine 裡 Stop，這樣即使模組不理 ctx 也不會卡住整個關機；
// 逾時就放棄等它並回錯（那個 goroutine 會留到模組自己回來）。
func stopWithTimeout(parent context.Context, m Module, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.Stop(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("停止 %s 失敗：%w", m.Name(), err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("停止 %s 逾時（%s）", m.Name(), timeout)
	}
}

// Mounts 收集所有 RouteProvider 的路由；服務名（或服務裡的方法）重複、服務名或實作是空的就回錯。
func (a *App) Mounts() ([]Mount, error) {
	var all []Mount
	whole := map[string]string{}  // 服務 → 整個服務的登記者
	parts := map[string]string{}  // 「服務.方法」→ 部分登記的登記者
	partOf := map[string]string{} // 服務 → 最先部分登記它的模組（跟整個服務衝突時講出來）
	for _, m := range a.mods {
		p, ok := m.(RouteProvider)
		if !ok {
			continue
		}
		for _, mt := range p.Routes() {
			if mt.Service == "" {
				return nil, fmt.Errorf("模組 %s 登記了沒有服務名的路由", m.Name())
			}
			if mt.Impl == nil {
				return nil, fmt.Errorf("模組 %s 的服務 %s 沒有實作", m.Name(), mt.Service)
			}
			if prev, dup := whole[mt.Service]; dup {
				return nil, fmt.Errorf("服務 %s 重複登記：%s、%s", mt.Service, prev, m.Name())
			}
			if len(mt.Methods) == 0 {
				if prev, dup := partOf[mt.Service]; dup {
					return nil, fmt.Errorf("服務 %s 重複登記：%s、%s", mt.Service, prev, m.Name())
				}
				whole[mt.Service] = m.Name()
			}
			for _, method := range mt.Methods {
				key := mt.Service + "." + method
				if prev, dup := parts[key]; dup {
					return nil, fmt.Errorf("服務 %s 的方法 %s 重複登記：%s、%s", mt.Service, method, prev, m.Name())
				}
				parts[key] = m.Name()
				if _, ok := partOf[mt.Service]; !ok {
					partOf[mt.Service] = m.Name()
				}
			}
			all = append(all, mt)
		}
	}
	return all, nil
}

// Mux 把所有路由用 sdk.Mount 掛到同一個 http.ServeMux。
// 不掛 "/"（找不到的處理與靜態檔由呼叫的人決定，例如 sdk.NotFoundHandler）。
func (a *App) Mux() (*http.ServeMux, error) {
	mounts, err := a.Mounts()
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	for _, mt := range mounts {
		if err := sdk.MountMethods(mux, mt.Service, mt.Impl, mt.Methods); err != nil {
			return nil, fmt.Errorf("掛載服務 %s：%w", mt.Service, err)
		}
	}
	return mux, nil
}

// Checks 收集所有 CheckProvider 的健檢項目（依模組順序）；名稱重複、Run 與 RunAll 沒有剛好一個就回錯。
func (a *App) Checks() ([]Check, error) {
	var all []Check
	owner := map[string]string{}
	for _, m := range a.mods {
		p, ok := m.(CheckProvider)
		if !ok {
			continue
		}
		for _, c := range p.Checks() {
			if c.Name == "" || (c.Run == nil) == (c.RunAll == nil) {
				return nil, fmt.Errorf("模組 %s 登記了不完整的健檢（要有名稱，Run 與 RunAll 剛好一個）", m.Name())
			}
			if prev, dup := owner[c.Name]; dup {
				return nil, fmt.Errorf("健檢 %s 重複登記：%s、%s", c.Name, prev, m.Name())
			}
			owner[c.Name] = m.Name()
			all = append(all, c)
		}
	}
	return all, nil
}

// Jobs 收集所有 JobProvider 的定時工作（依模組順序）；名稱重複、沒有 Run、Cron 與 Every 不是剛好一個、
// First 配 Cron、間隔是負的就回錯。
func (a *App) Jobs() ([]Job, error) {
	var all []Job
	owner := map[string]string{}
	for _, m := range a.mods {
		p, ok := m.(JobProvider)
		if !ok {
			continue
		}
		for _, j := range p.Jobs() {
			if j.Name == "" || j.Run == nil || (j.Cron == "") == (j.Every == 0) || j.Every < 0 || j.First < 0 || (j.Cron != "" && j.First != 0) {
				return nil, fmt.Errorf("模組 %s 登記了不完整的定時工作（要有名稱、Run，Cron 與 Every 剛好一個，First 只配 Every）", m.Name())
			}
			if prev, dup := owner[j.Name]; dup {
				return nil, fmt.Errorf("定時工作 %s 重複登記：%s、%s", j.Name, prev, m.Name())
			}
			owner[j.Name] = m.Name()
			all = append(all, j)
		}
	}
	return all, nil
}

// Board 收集所有 BoardProvider 的首頁資料段（依模組順序）；Key 重複或沒有 Load 就回錯。
func (a *App) Board() ([]BoardPart, error) {
	var all []BoardPart
	owner := map[string]string{}
	for _, m := range a.mods {
		p, ok := m.(BoardProvider)
		if !ok {
			continue
		}
		for _, b := range p.Board() {
			if b.Key == "" || b.Load == nil {
				return nil, fmt.Errorf("模組 %s 登記了不完整的首頁資料（要有 Key 與 Load）", m.Name())
			}
			if prev, dup := owner[b.Key]; dup {
				return nil, fmt.Errorf("首頁資料 %s 重複登記：%s、%s", b.Key, prev, m.Name())
			}
			owner[b.Key] = m.Name()
			all = append(all, b)
		}
	}
	return all, nil
}

// Commands 合併各業務模組的 Commands() 套件函式回傳的指令表；名稱重複就回錯。
// 指令行程只是 client，不需要 App 的實例，所以這裡不用 a 的狀態，放在 App 上只是讓組裝點只認一個物件。
func (a *App) Commands(sets ...map[string]Command) (map[string]Command, error) {
	return cmdline.Merge(sets...)
}
