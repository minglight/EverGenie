package engine

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/registry"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

type process struct {
	done <-chan error
	kill func() error
}
type worker struct {
	id, path, socket string
	process          *process
	next             time.Time
	restarts         int
	ready            bool
}
type Module struct {
	d            Deps
	mu           sync.Mutex
	home         string
	registry     *Registry
	workers      map[string]*worker
	missing      map[string]bool // 登錄過、但找不到執行檔的引擎 id（doctor 用）
	trustSource  func(platform.Trust) *sdk.PermissionPolicy
	policy       *sdk.PermissionPolicy
	guidance     func() []string               // 目前有效的 guidance（policy 模組，SetGuidanceSource）
	rules        func(string) []sdk.PolicyRule // 某位秘書目前有效的規則（policy 模組，SetRuleSource）
	quiet        platform.Quiet                // 安靜時段（額度開通的通知不在這段時間發）
	log          *slog.Logger
	RestartDelay time.Duration
	cancel       context.CancelFunc
	run          context.Context
	done         chan struct{}
	manual       bool
	launch       func(context.Context, string, []string) (*process, error)
	dial         func(string) sdk.EngineAPI
	socketExists func(string) bool
}

func New(d Deps) *Module {
	if d.Clock == nil {
		d.Clock = platform.SystemClock{}
	}
	return &Module{d: d, RestartDelay: 5 * time.Second, launch: startProcess, dial: func(socket string) sdk.EngineAPI { return sdk.NewEngineClient(sdk.Dial(socket)) }, socketExists: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Mode()&os.ModeSocket != 0
	}}
}
func (m *Module) Name() string        { return "engine" }
func (m *Module) SetHome(home string) { m.home = home }
func (m *Module) For(account string) (Engine, error) {
	m.mu.Lock()
	r := m.registry
	m.mu.Unlock()
	if r == nil {
		return nil, platform.Unavailable("引擎服務尚未啟動")
	}
	return r.For(account)
}
func (m *Module) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return platform.Conflict("引擎服務已啟動")
	}
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.trustSource == nil {
		m.mu.Unlock()
		return platform.Unavailable("信任政策尚未接上，無法啟動引擎")
	}
	if m.home == "" {
		m.mu.Unlock()
		return nil
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	h.Clock = m.d.Clock
	m.log = platform.NewLogger(h, "engine")
	m.registry = &Registry{accounts: map[string]account{}, clients: map[string]sdk.EngineAPI{}, log: m.log, rules: m.rules}
	m.workers = map[string]*worker{}
	m.missing = map[string]bool{}
	m.policy = m.trustSource(h.Trust)
	m.quiet = platform.Quiet{Loc: h.Location()}
	if parts := strings.Split(h.User.QuietHours, "-"); len(parts) == 2 {
		m.quiet.Start, m.quiet.End = parts[0], parts[1]
	}
	binaries := registry.Executables()
	for name, value := range h.Accounts {
		table, ok := value.(map[string]any)
		if !ok {
			continue
		}
		id, _ := table["engine"].(string)
		config, _ := table["engine_config"].(map[string]any)
		warm, _ := table["usage_warmup"].(bool)
		m.registry.accounts[name] = account{engine: id, config: config, warmup: warm}
		binary, known := binaries[id]
		if !known {
			continue
		}
		m.registry.clients[id] = nil
		if _, ok := m.workers[id]; ok {
			continue
		}
		path := findBinary(binary)
		if path == "" {
			m.log.Warn("找不到引擎執行檔", "id", id)
			m.missing[id] = true
			continue
		}
		m.workers[id] = &worker{id: id, path: path, socket: platform.RuntimeDir(m.home, "sockets", "engine-"+id+".sock")}
	}
	m.run, m.cancel = context.WithCancel(ctx)
	m.done = make(chan struct{})
	run, done := m.run, m.done
	m.mu.Unlock()
	m.Tick(run, m.d.Clock.Now())
	m.subscribeJobs(run)
	if m.manual {
		close(done)
		return nil
	}
	go func() {
		defer close(done)
		defer func() {
			if recover() != nil {
				m.log.Warn("引擎監督中斷")
			}
		}()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-run.Done():
				return
			case <-ticker.C:
				m.Tick(run, m.d.Clock.Now())
			}
		}
	}()
	return nil
}

// FindBinary 找引擎執行檔：先看 EVERGENIE_ENGINE_BIN_DIR，其次 EverGenie 自己旁邊（platform.ExecutableDirs：照原樣的路徑、解開符號連結後的路徑）；
// 找不到回空字串（serve 與 doctor 共用）。
func FindBinary(name string) string { return findBinary(name) }
func findBinary(name string) string {
	dirs := append([]string{sdk.Getenv("EVERGENIE_ENGINE_BIN_DIR")}, platform.ExecutableDirs()...)
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
}
func startProcess(ctx context.Context, path string, env []string) (*process, error) {
	cmd := exec.CommandContext(ctx, path, "serve")
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	return &process{done: done, kill: cmd.Process.Kill}, nil
}

// environment 是引擎行程的環境：家、API socket、引擎 socket，加上家的佈局（EVERGENIE_STATE、EVERGENIE_MEDIA、EVERGENIE_OFFICIAL_PERSONAS，
// 引擎開的 session 也繼承）；外面帶進來的同名變數一律換成這個家的。跟著程式的公開卡資料夾（EVERGENIE_BUNDLED_PERSONAS）只給套件，
// 引擎與 session 不直接讀卡，外面帶進來的也拿掉。
func (m *Module) environment(socket string) []string {
	layout := platform.LayoutFor(m.home)
	own := append([]string{"EVERGENIE_HOME=" + m.home, "EVERGENIE_API_SOCKET=" + layout.Runtime("sockets", "api.sock"), "EVERGENIE_ENGINE_SOCKET=" + socket}, layout.Env()...)
	env := []string{}
	strip := sdk.EnvPrefixes(append([]string{"EVERGENIE_STATE=", "EVERGENIE_MEDIA=", "EVERGENIE_OFFICIAL_PERSONAS=", "EVERGENIE_BUNDLED_PERSONAS="}, own...)...) // 第 1 版沒有佈局變數，外面帶進來的也要拿掉；改名前的舊名一起拿掉
	for _, v := range os.Environ() {
		if !ownsEnv(strip, v) {
			env = append(env, v)
		}
	}
	return append(env, own...)
}

// ownsEnv：v（NAME=值）的名字是不是 own 裡已經有的。
func ownsEnv(own []string, v string) bool {
	name, _, _ := strings.Cut(v, "=")
	for _, o := range own {
		if strings.HasPrefix(o, name+"=") {
			return true
		}
	}
	return false
}
func (m *Module) Tick(ctx context.Context, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.run == nil || m.run.Err() != nil || ctx.Err() != nil {
		return
	}
	ids := make([]string, 0, len(m.workers))
	for id := range m.workers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		w := m.workers[id]
		if w.process != nil {
			select {
			case <-w.process.done:
				w.process = nil
				w.ready = false
				w.next = now.Add(m.RestartDelay)
				w.restarts++
				m.registry.mu.Lock()
				m.registry.clients[id] = nil
				m.registry.mu.Unlock()
				m.log.Warn("引擎行程已停止", "id", id)
			default:
			}
		}
		if w.process == nil {
			if w.restarts > 5 || now.Before(w.next) {
				continue
			}
			p, err := m.launch(m.run, w.path, m.environment(w.socket))
			if err != nil {
				w.restarts++
				w.next = now.Add(m.RestartDelay)
				m.log.Warn("無法啟動引擎", "id", id)
				continue
			}
			w.process = p
		}
		if w.ready || !m.socketExists(w.socket) {
			continue
		}
		api := m.dial(w.socket)
		call, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := api.Capabilities(call, &sdk.EngineCapabilitiesRequest{})
		cancel()
		if err != nil {
			continue
		}
		accounts := []string{}
		for name, a := range m.registry.accounts {
			if a.engine == id {
				accounts = append(accounts, name)
			}
		}
		sort.Strings(accounts)
		w.ready = true // A successful readiness handshake attempts each account once per process.
		configured := true
		policy := m.currentPolicy()
		for _, name := range accounts {
			call, cancel := context.WithTimeout(ctx, 2*time.Second)
			_, err := api.SetPermission(call, &sdk.EngineSetPermissionRequest{Account: name, Policy: policy})
			cancel()
			if err != nil {
				configured = false
				m.log.Warn("無法設定引擎權限", "id", id)
				continue
			}
		}
		if !configured {
			continue
		}
		m.registry.mu.Lock()
		m.registry.clients[id] = api
		m.registry.mu.Unlock()
	}
}
func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	if m.cancel == nil {
		m.mu.Unlock()
		return nil
	}
	m.cancel()
	done := m.done
	processes := []*process{}
	for _, w := range m.workers {
		if w.process != nil {
			processes = append(processes, w.process)
		}
	}
	m.registry.mu.Lock()
	for id := range m.registry.clients {
		m.registry.clients[id] = nil
	}
	m.registry.mu.Unlock()
	m.mu.Unlock()
	for _, p := range processes {
		if err := p.kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			m.log.Warn("引擎行程停止中")
		}
	}
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	for _, p := range processes {
		select {
		case <-p.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	m.cancel = nil
	m.mu.Unlock()
	return nil
}

// SetTrustSource 由組裝點接上信任政策；啟動前必須設定。
func (m *Module) SetTrustSource(src func(platform.Trust) *sdk.PermissionPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trustSource = src
}
