package pkg

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

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// fakes 記下收到的請求（cli 的 fakes 精簡版）。
type fakes struct {
	mu   sync.Mutex
	last map[string]any
	fail *sdk.Error
}

func (f *fakes) record(name string, req any) *sdk.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last[name] = req
	return f.fail
}

func (f *fakes) got(name string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[name]
}

type fakePackage struct {
	sdk.PackageAPI
	f *fakes
}

func (s fakePackage) SetEnabled(_ context.Context, r *sdk.PackageSetEnabledRequest) (*sdk.PackageSetEnabledResponse, error) {
	if err := s.f.record("package.set_enabled", r); err != nil {
		return nil, err
	}
	p := &sdk.Package{ID: r.ID, Enabled: r.Enabled, State: sdk.PackageStateRunning}
	switch {
	case r.ID == "needs-closet" && r.Enabled:
		p.State, p.Error = sdk.PackageStateRejected, "需要套件「closet」，但它已停用"
	case !r.Enabled:
		p.State = sdk.PackageStateStopped
	}
	return &sdk.PackageSetEnabledResponse{Package: p}, nil
}

func (s fakePackage) New(_ context.Context, r *sdk.PackageNewRequest) (*sdk.PackageNewResponse, error) {
	if err := s.f.record("package.new", r); err != nil {
		return nil, err
	}
	if r.ID == "core" {
		return nil, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "「core」是系統保留的名字，換一個"}
	}
	return &sdk.PackageNewResponse{
		Package: &sdk.Package{ID: r.ID, Name: r.Name, State: sdk.PackageStateRunning},
		Dir:     "packages/mine/" + r.ID,
		Files:   []string{"README.md", "package.toml"},
	}, nil
}

func (s fakePackage) Preview(_ context.Context, r *sdk.PackagePreviewRequest) (*sdk.PackagePreviewResponse, error) {
	if err := s.f.record("package.preview", r); err != nil {
		return nil, err
	}
	out := &sdk.PackagePreviewResponse{ID: "notes", Name: "筆記", Version: "1.2.0", RunsProgram: true, Ref: "v1.2.0", Commit: "0123456789abcdef",
		Personas: []sdk.PackagePreviewPersona{{ID: "notes.fairy", Slug: "fairy", Name: "Fairy", Avatar: true}, {ID: "notes.owl", Slug: "owl"}}}
	if strings.Contains(r.Source, "bad") {
		out.Problems = []string{"跟官方套件同名"}
	}
	return out, nil
}

func (s fakePackage) Install(_ context.Context, r *sdk.PackageInstallRequest) (*sdk.PackageInstallResponse, error) {
	if err := s.f.record("package.install", r); err != nil {
		return nil, err
	}
	return &sdk.PackageInstallResponse{Package: &sdk.Package{ID: "notes", Name: "筆記", Version: "1.2.0", State: sdk.PackageStateRunning}}, nil
}

func (s fakePackage) Outdated(_ context.Context, r *sdk.PackageOutdatedRequest) (*sdk.PackageOutdatedResponse, error) {
	return &sdk.PackageOutdatedResponse{Packages: []sdk.PackageOutdated{{ID: "notes", Current: "1.2.0", Latest: "1.3.0"}}}, nil
}

func (s fakePackage) Upgrade(_ context.Context, r *sdk.PackageUpgradeRequest) (*sdk.PackageUpgradeResponse, error) {
	if err := s.f.record("package.upgrade", r); err != nil {
		return nil, err
	}
	return &sdk.PackageUpgradeResponse{From: "1.2.0", To: "1.3.0"}, nil
}

func (s fakePackage) Export(_ context.Context, r *sdk.PackageExportRequest) (*sdk.PackageExportResponse, error) {
	if err := s.f.record("package.export", r); err != nil {
		return nil, err
	}
	if r.ID == "diary" {
		return nil, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "官方套件不用匯出，別人升級 EverGenie 就有"}
	}
	out := &sdk.PackageExportResponse{Format: "dir", Files: []string{"README.md", "package.toml"}, Skipped: []string{".git：git 紀錄"}}
	if r.ID == "leaky" {
		out.Findings = []sdk.PackageExportFinding{{Path: "config.toml", Line: 3, Kind: "secret", What: "看起來像 GitHub token（ghp_…，共 40 字）"}, {Path: ".env", Kind: "secret_file", What: "環境變數檔"}}
		if !r.AllowFindings {
			out.Blocked = true
			return out, nil
		}
	}
	out.Path = r.Out
	return out, nil
}

// serveFakes 把假的 package 服務經 sdk.Mount 掛在 /tmp/hg-* 下的 unix socket（路徑上限 104 字元）。
func serveFakes(t *testing.T) (*sdk.Client, *fakes) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakes{last: map[string]any{}}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, map[string]any{"package": fakePackage{f: f}}) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	for i := 0; ; i++ {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			break
		}
		if i > 500 {
			t.Fatal("socket never accepted a connection")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return sdk.Dial(sock), f
}

func run(t *testing.T, c *sdk.Client, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(stdin), Out: &out, Err: &errOut}, Commands(), "usage\n")
	return code, out.String(), errOut.String()
}

func mustRun(t *testing.T, c *sdk.Client, args ...string) string {
	t.Helper()
	code, out, errOut := run(t, c, "", args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errOut)
	}
	return out
}

// TestPackagesEnableCLI：packages enable／disable 轉成 PackageSetEnabled（帶操作者）；被拒時退出碼非 0 並說明原因；help 提醒先問使用者。
// Guards: REQ-176
func TestPackagesEnableCLI(t *testing.T) {
	c, f := serveFakes(t)
	out := mustRun(t, c, "packages", "enable", "closet-mine")
	if g := f.got("package.set_enabled").(*sdk.PackageSetEnabledRequest); g.ID != "closet-mine" || !g.Enabled || g.By == nil || g.By.Name != "owner(cli)" {
		t.Fatalf("enable: %+v", g)
	}
	if !strings.Contains(out, "已啟用") || !strings.Contains(out, "running") {
		t.Fatalf("enable 輸出：%s", out)
	}
	out = mustRun(t, c, "packages", "disable", "closet-mine")
	if g := f.got("package.set_enabled").(*sdk.PackageSetEnabledRequest); g.Enabled {
		t.Fatalf("disable: %+v", g)
	}
	if !strings.Contains(out, "已停用") {
		t.Fatalf("disable 輸出：%s", out)
	}
	if code, _, errOut := run(t, c, "", "packages", "enable", "needs-closet"); code == 0 || !strings.Contains(errOut, "已停用") {
		t.Fatalf("被拒要失敗並說原因：%d %s", code, errOut)
	}
	if code, _, _ := run(t, c, "", "packages", "enable"); code != 2 {
		t.Fatalf("少了套件 id 要是用法錯誤（2）：%d", code)
	}
	if help := mustRun(t, c, "packages", "--help"); !strings.Contains(help, "先在對話裡問過使用者") {
		t.Fatalf("help 要提醒先問使用者：%s", help)
	}
}

// TestPackagesNewCLI：packages new <id> 轉成 PackageNew（帶 name 與操作者）；輸出寫出資料夾、檔案、頁面網址與下一步；被拒時結束碼 1 並說原因；少了 id 是用法錯誤。
// Guards: REQ-311
func TestPackagesNewCLI(t *testing.T) {
	c, f := serveFakes(t)
	out := mustRun(t, c, "packages", "new", "garden", "--name", "我的花園")
	g := f.got("package.new").(*sdk.PackageNewRequest)
	if g.ID != "garden" || g.Name != "我的花園" || g.By == nil || g.By.Name != "owner(cli)" {
		t.Fatalf("new 送出：%+v", g)
	}
	for _, want := range []string{"已建立你的新套件「garden」：packages/mine/garden", "package.toml", "/p/garden/index", "packages sync garden"} {
		if !strings.Contains(out, want) {
			t.Fatalf("輸出少了 %q：%s", want, out)
		}
	}
	if out := mustRun(t, c, "packages", "new", "garden", "--json"); !strings.Contains(out, `"dir":"packages/mine/garden"`) {
		t.Fatalf("--json：%s", out)
	}
	if code, _, errOut := run(t, c, "", "packages", "new", "core"); code != 1 || !strings.Contains(errOut, "系統保留") {
		t.Fatalf("保留字要失敗並說原因：%d %s", code, errOut)
	}
	if code, _, _ := run(t, c, "", "packages", "new"); code != 2 {
		t.Fatalf("少了 id 要是用法錯誤：%d", code)
	}
	if help := mustRun(t, c, "packages", "--help"); !strings.Contains(help, "new <id>") {
		t.Fatalf("help 要列 new：%s", help)
	}
}

// TestPackagesExportCLI：packages export 轉 PackageExport（帶操作者；--out 相對路徑換成完整路徑、--git、--allow-findings）；
// 掃到疑似個資印檔案與行號、結束碼 1；--allow-findings 照樣匯出；官方被拒結束碼 1；少了 id 是用法錯誤；help 列 export；預覽印帶來的角色。
// Guards: REQ-340, REQ-338
func TestPackagesExportCLI(t *testing.T) {
	c, f := serveFakes(t)
	cwd, _ := os.Getwd()
	out := mustRun(t, c, "packages", "export", "garden", "--out", "share/garden", "--git")
	g := f.got("package.export").(*sdk.PackageExportRequest)
	if g.ID != "garden" || g.Out != filepath.Join(cwd, "share", "garden") || !g.Git || g.AllowFindings || g.By == nil || g.By.Name != "owner(cli)" {
		t.Fatalf("export 送出：%+v", g)
	}
	for _, want := range []string{"已匯出「garden」", "2 個檔", "沒有帶的", ".git：git 紀錄", "git init", "packages install"} {
		if !strings.Contains(out, want) {
			t.Fatalf("輸出少了 %q：%s", want, out)
		}
	}
	code, out, _ := run(t, c, "", "packages", "export", "leaky")
	if code != 1 || !strings.Contains(out, "沒有匯出") || !strings.Contains(out, "config.toml:3") || !strings.Contains(out, ".env　環境變數檔") || !strings.Contains(out, "--allow-findings") || strings.Contains(out, "git init") {
		t.Fatalf("有疑似個資要停下、列出、結束碼 1：%d %s", code, out)
	}
	out = mustRun(t, c, "packages", "export", "leaky", "--allow-findings", "--out", "/tmp/x")
	if g := f.got("package.export").(*sdk.PackageExportRequest); !g.AllowFindings || g.Out != "/tmp/x" || !strings.Contains(out, "帶著 2 處疑似個資") {
		t.Fatalf("--allow-findings：%+v %s", g, out)
	}
	if code, out, _ := run(t, c, "", "packages", "export", "leaky", "--json"); code != 1 || !strings.Contains(out, `"blocked":true`) {
		t.Fatalf("--json 被擋也是結束碼 1：%d %s", code, out)
	}
	if code, _, errOut := run(t, c, "", "packages", "export", "diary"); code != 1 || !strings.Contains(errOut, "別人升級") {
		t.Fatalf("官方要被拒：%d %s", code, errOut)
	}
	if code, _, _ := run(t, c, "", "packages", "export"); code != 2 {
		t.Fatalf("少了 id 要是用法錯誤：%d", code)
	}
	if help := mustRun(t, c, "packages", "--help"); !strings.Contains(help, "export <套件>") || !strings.Contains(help, "--allow-findings") {
		t.Fatalf("help 要列 export：%s", help)
	}
	if out := mustRun(t, c, "packages", "preview", "https://example.com/notes.git"); !strings.Contains(out, "帶來的角色：Fairy（notes.fairy），有頭像、notes.owl") {
		t.Fatalf("預覽要列帶來的角色：%s", out)
	}
}

// TestPackagesInstallFromURLCLI：網址沒有 --yes 只印預覽（不送 install）；有問題的預覽結束碼 1；--yes 才送 confirmed；資料夾直接裝；outdated、upgrade 轉 API。
// Guards: REQ-282
func TestPackagesInstallFromURLCLI(t *testing.T) {
	c, f := serveFakes(t)
	out := mustRun(t, c, "packages", "install", "https://example.com/notes.git")
	if f.got("package.install") != nil || !strings.Contains(out, "會在你的電腦上編譯並執行") || !strings.Contains(out, "確定要裝就加 --yes 再跑一次") {
		t.Fatalf("沒有 --yes 只能預覽：%v\n%s", f.got("package.install"), out)
	}
	if code, out, _ := run(t, c, "", "packages", "install", "https://example.com/bad.git"); code != 1 || !strings.Contains(out, "不能安裝：跟官方套件同名") || strings.Contains(out, "--yes") {
		t.Fatalf("有問題的預覽要失敗：%d %s", code, out)
	}
	out = mustRun(t, c, "packages", "install", "https://example.com/notes.git", "--ref", "v1.2.0", "--yes")
	if g := f.got("package.install").(*sdk.PackageInstallRequest); !g.Confirmed || g.Ref != "v1.2.0" || g.Source != "https://example.com/notes.git" || g.By == nil || g.By.Name != "owner(cli)" || !strings.Contains(out, "已安裝「筆記」1.2.0") {
		t.Fatalf("--yes 要送 confirmed：%+v\n%s", g, out)
	}
	mustRun(t, c, "packages", "install", "/tmp/some-folder")
	if g := f.got("package.install").(*sdk.PackageInstallRequest); g.Confirmed || g.Source != "/tmp/some-folder" {
		t.Fatalf("本機資料夾直接裝、不帶 confirmed：%+v", g)
	}
	if out := mustRun(t, c, "packages", "outdated"); !strings.Contains(out, "notes　1.2.0 → 1.3.0") {
		t.Fatalf("outdated：%s", out)
	}
	if out := mustRun(t, c, "packages", "upgrade", "notes"); !strings.Contains(out, "從 1.2.0 更新到 1.3.0") {
		t.Fatalf("upgrade：%s", out)
	}
	if code, _, _ := run(t, c, "", "packages", "install"); code != 2 {
		t.Fatalf("少了來源要是用法錯誤：%d", code)
	}
}

// sync 預演會標出預設角色的來源（職位樣板預設）。
func TestPersonaNote(t *testing.T) {
	if got := personaNote(sdk.SyncAgent{Persona: "garden.fairy", PersonaFrom: "template"}); !strings.Contains(got, "預設角色：garden.fairy（職位樣板預設）") {
		t.Fatalf("sync 預演的預設角色：%s", got)
	}
}
