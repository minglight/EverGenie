package persona

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-130
func TestFindPersonaByNameOrAlias(t *testing.T) {
	cards := []sdk.PersonaCard{
		{Slug: "athena", Name: "Athena", Aka: []string{"雅典娜"}, Source: "official"},
		{Slug: "athena", Name: "Fake", Source: "mine", Shadowed: true},
		{Slug: "hotaru", Name: "Hotaru", Aka: []string{"Firefly"}, Source: "mine"},
	}
	for key, want := range map[string]string{"athena": "athena", " ATHENA ": "athena", "雅典娜": "athena", "firefly": "hotaru", "Hotaru": "hotaru"} {
		if c, ok := findPersona(cards, key); !ok || c.Slug != want {
			t.Errorf("%q → %+v %v", key, c, ok)
		}
	}
	if c, ok := findPersona(cards, "fake"); ok {
		t.Errorf("被蓋住的卡不能被找到：%+v", c)
	}
	if _, ok := findPersona(cards, "nobody"); ok {
		t.Error("不存在的不該找到")
	}
	if got := splitAka(" a, ,b ,"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("splitAka：%v", got)
	}
}

// fakeAPI 記下收到的請求，回固定答案；指令只認 sdk 的 client，所以用 sdk.Serve 開一個真的 unix socket。
type fakeAPI struct {
	sdk.PersonasAPI
	mu   sync.Mutex
	last map[string]any
}

func (f *fakeAPI) record(name string, req any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last[name] = req
}

func (f *fakeAPI) got(name string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[name]
}

func (f *fakeAPI) List(_ context.Context, r *sdk.PersonasListRequest) (*sdk.PersonasListResponse, error) {
	return &sdk.PersonasListResponse{Personas: []sdk.PersonaCard{
		{Slug: "athena", Name: "Athena", Source: "official", Status: "live"},
		{Slug: "mina", Name: "Mina", Source: "mine", Status: "live"},
		{Slug: "garden.fairy", Name: "Fairy", Source: "package", Package: "garden", Status: "design"},
	}}, nil
}

func (f *fakeAPI) Create(_ context.Context, r *sdk.PersonasCreateRequest) (*sdk.PersonasCreateResponse, error) {
	f.record("create", r)
	return &sdk.PersonasCreateResponse{Persona: &sdk.PersonaCard{Slug: r.Package + "." + r.Slug}}, nil
}

func (f *fakeAPI) Copy(_ context.Context, r *sdk.PersonasCopyRequest) (*sdk.PersonasCopyResponse, error) {
	f.record("copy", r)
	return &sdk.PersonasCopyResponse{Persona: &sdk.PersonaCard{Slug: r.Package + "." + r.From}}, nil
}

func (f *fakeAPI) SetStatus(_ context.Context, r *sdk.PersonasSetStatusRequest) (*sdk.PersonasSetStatusResponse, error) {
	f.record("set_status", r)
	return &sdk.PersonasSetStatusResponse{Persona: &sdk.PersonaCard{Slug: r.Slug, Status: r.Status}}, nil
}

func serveFake(t *testing.T) (*sdk.Client, *fakeAPI) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAPI{last: map[string]any{}}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, map[string]any{"personas": f}) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		_ = os.RemoveAll(dir)
	})
	for i := 0; i < 500; i++ {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			return sdk.Dial(sock), f
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket never accepted a connection")
	return nil, nil
}

func hg(t *testing.T, c *sdk.Client, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{Out: &out, Err: &errb}, Commands(nil, nil, nil), "用法\n")
	return code, out.String(), errb.String()
}

func mustHG(t *testing.T, c *sdk.Client, args ...string) string {
	t.Helper()
	code, out, errb := hg(t, c, args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errb)
	}
	return out
}

// 說明卡列的指令一個都不能少（登記是套件層級函式，名稱重複由 app.Commands 擋）。
func TestCommandsListsAllPersonaCommands(t *testing.T) {
	var _ map[string]app.Command = Commands(nil, nil, nil)
	got := Commands(nil, nil, nil)
	for _, name := range []string{"personas", "personas list", "personas show", "personas new", "personas copy", "personas move", "personas check", "personas live", "personas design", "personas style", "personas test", "personas results", "personas pack"} {
		if got[name].Run == nil {
			t.Errorf("缺 %s", name)
		}
	}
}

// personas new --to-package／move／copy --to-package／live 套件內代號 轉成 API（帶 package 與操作者）；官方角色不能 move；list 標來源套件。
// Guards: REQ-320, REQ-321
func TestPersonaToPackageCLI(t *testing.T) {
	c, f := serveFake(t)
	out := mustHG(t, c, "personas", "new", "--to-package", "garden", "fairy")
	g := f.got("create").(*sdk.PersonasCreateRequest)
	if g.Package != "garden" || g.Slug != "fairy" || g.Name != "" || g.By == nil || g.By.Name != "owner(cli)" || !strings.Contains(out, "garden.fairy") {
		t.Fatalf("new --to-package：%+v %s", g, out)
	}
	out = mustHG(t, c, "personas", "move", "mina", "--to-package", "garden", "--slug", "mina2")
	cp := f.got("copy").(*sdk.PersonasCopyRequest)
	if cp.Package != "garden" || cp.From != "mina" || cp.Slug != "mina2" || !strings.Contains(out, "來源不動") {
		t.Fatalf("move：%+v %s", cp, out)
	}
	mustHG(t, c, "personas", "copy", "athena", "--to-package", "garden", "--name", "雅")
	if cp := f.got("copy").(*sdk.PersonasCopyRequest); cp.From != "athena" || cp.Name != "雅" || cp.Package != "garden" {
		t.Fatalf("copy --to-package：%+v", cp)
	}
	if code, _, errOut := hg(t, c, "personas", "move", "athena", "--to-package", "garden"); code != 1 || !strings.Contains(errOut, "不是我的角色") {
		t.Fatalf("官方角色不能 move：%d %s", code, errOut)
	}
	if code, _, _ := hg(t, c, "personas", "move", "mina"); code != 2 {
		t.Fatalf("少了 --to-package 是用法錯誤：%d", code)
	}
	mustHG(t, c, "personas", "live", "garden.fairy")
	if st := f.got("set_status").(*sdk.PersonasSetStatusRequest); st.Slug != "garden.fairy" || st.Status != "live" {
		t.Fatalf("live：%+v", st)
	}
	if list := mustHG(t, c, "personas", "list"); !strings.Contains(list, "套件 garden") {
		t.Fatalf("list 要標來源套件：%s", list)
	}
}
