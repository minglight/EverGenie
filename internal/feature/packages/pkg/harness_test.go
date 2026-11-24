package pkg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// harness：fixture 家＋暫存的官方資料夾＋假的 go／行程／bus。背景工作排隊，由測試在鎖外跑（drain），不用真的時間。
type harness struct {
	t        *testing.T
	home     *homefixture.Home
	m        *Module
	official string
	bus      *fakeBus
	queue    []func()
	procs    map[string][]*fakeProc
	builds   []string
	buildErr map[string]string // 資料夾 → 編譯錯誤輸出
	noGo     bool
}

type fakeBus struct {
	mu     sync.Mutex
	events []map[string]any
}

func (b *fakeBus) Publish(_ context.Context, topic string, payload map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := map[string]any{"topic": topic}
	for k, v := range payload {
		cp[k] = v
	}
	b.events = append(b.events, cp)
	return nil
}

func (b *fakeBus) alerts() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, e := range b.events {
		if e["topic"] == "system.alert" {
			out = append(out, fmt.Sprint(e["title"]))
		}
	}
	return out
}

type fakeProc struct {
	done   chan error
	once   sync.Once
	killed bool
	env    []string
}

var errKilled = errors.New("signal: terminated")

func (p *fakeProc) exit(err error) { p.once.Do(func() { p.done <- err; close(p.done) }) }

type fakeRunner struct{ h *harness }

func (r fakeRunner) Run(_ context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	r.h.builds = append(r.h.builds, dir)
	if msg, ok := r.h.buildErr[dir]; ok {
		return []byte(msg), errors.New("exit status 1")
	}
	return nil, nil
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, home: homefixture.New(t), official: t.TempDir(), bus: &fakeBus{}, procs: map[string][]*fakeProc{}, buildErr: map[string]string{}}
	m := New(Deps{Bus: h.bus, Clock: h.home.Clock})
	m.manual = true
	m.async = func(f func()) { h.queue = append(h.queue, f) }
	m.runner = fakeRunner{h}
	m.lookGo = func() (string, error) {
		if h.noGo {
			return "", exec.ErrNotFound
		}
		return "/fake/go", nil
	}
	m.launch = func(_ context.Context, bin, dir string, env []string, logPath string) (*process, error) {
		id := envValue(env, "EVERGENIE_PACKAGE_ID")
		p := &fakeProc{done: make(chan error, 1), env: env}
		h.procs[id] = append(h.procs[id], p)
		return &process{done: p.done, kill: func() { p.killed = true; p.exit(errKilled) }}, nil
	}
	m.SetHome(h.home.Dir)
	m.SetOfficialDir(h.official)
	h.m = m
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return h
}

func envValue(env []string, key string) string {
	for _, v := range env {
		if strings.HasPrefix(v, key+"=") {
			return strings.TrimPrefix(v, key+"=")
		}
	}
	return ""
}

func (h *harness) start() {
	h.t.Helper()
	if err := h.m.Start(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	h.drain()
}

func (h *harness) drain() {
	for len(h.queue) > 0 {
		f := h.queue[0]
		h.queue = h.queue[1:]
		f()
	}
}

func (h *harness) tick() {
	h.m.Tick(context.Background(), h.home.Clock.Now())
	h.drain()
}

func (h *harness) advance(d time.Duration) { h.home.Clock.Advance(d); h.tick() }

func (h *harness) get(id string) sdk.Package {
	h.t.Helper()
	r, err := h.m.Get(context.Background(), &sdk.PackageGetRequest{ID: id})
	if err != nil {
		h.t.Fatalf("Get(%s): %v", id, err)
	}
	return *r.Package
}

func (h *harness) list() []sdk.Package {
	h.t.Helper()
	r, err := h.m.List(context.Background(), &sdk.PackageListRequest{})
	if err != nil {
		h.t.Fatal(err)
	}
	return r.Packages
}

// lastProc 是這個套件最近一次啟動的行程。
func (h *harness) lastProc(id string) *fakeProc {
	h.t.Helper()
	ps := h.procs[id]
	if len(ps) == 0 {
		h.t.Fatalf("套件 %s 沒有被啟動過", id)
	}
	return ps[len(ps)-1]
}

func (h *harness) hello(id string) error {
	_, err := h.m.Hello(context.Background(), &sdk.PackageHelloRequest{ID: id, API: "evergenie.v1"})
	return err
}

func (h *harness) tierRoot(t sdk.Tier) string {
	if t == sdk.TierOfficial {
		return h.official
	}
	if t == sdk.TierInstalled {
		return platform.LayoutFor(h.home.Dir).InstalledPackages() // 第 2 版在狀態資料夾（DEV-145）
	}
	return filepath.Join(h.home.Dir, "packages", string(t))
}

// write 建一個套件資料夾；files 的 key 是相對路徑。
func (h *harness) write(tier sdk.Tier, id, manifest string, files map[string]string) string {
	h.t.Helper()
	dir := filepath.Join(h.tierRoot(tier), id)
	all := map[string]string{"package.toml": manifest}
	for k, v := range files {
		all[k] = v
	}
	for rel, body := range all {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			h.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			h.t.Fatal(err)
		}
	}
	return dir
}

// dataPkg、programPkg 產生最小的合格 manifest；extra 接在最後（放 [[requires]]、[[extension]] 之類的表）。
func dataPkg(id, version, extra string) string {
	return fmt.Sprintf("schema = 1\nid = %q\nname = %q\nversion = %q\ndescription = \"測試用的套件\"\nkind = \"data\"\napi = \"evergenie.v1\"\n%s", id, id+"名", version, extra)
}

func programPkg(id, restart, extra string) string {
	r := ""
	if restart != "" {
		r = fmt.Sprintf("restart = %q\n", restart)
	}
	return fmt.Sprintf("schema = 1\nid = %q\nname = %q\nversion = \"1.0.0\"\ndescription = \"測試用的套件\"\nkind = \"program\"\napi = \"evergenie.v1\"\n[capabilities]\nread = [\"vault:task\"]\nwrite = [\"vault:pkg/%s\"]\n[program]\nentry = \"cmd/%s\"\n%s%s", id, id+"名", id, id, r, extra)
}

func programFiles(id string) map[string]string {
	return map[string]string{"cmd/" + id + "/main.go": "package main\n\nfunc main() {}\n"}
}
