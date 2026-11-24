package board

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 套件頁面的編譯（D-11、REQ-114、REQ-140；ai_docs/modules/board.md「套件頁面編譯」）。

const (
	sharedNamespace = "hg-shared"
	skipMark        = "hg-skip"
	bareImportMsg   = "套件頁面只能 import 套件裡的檔案，以及 react、@evergenie/ui、@evergenie/sdk-web、@evergenie/host"
	outsideMsg      = "套件頁面不能讀套件資料夾外面的檔案"
)

// sharedImports 是外殼提供、不打包進套件頁面的模組（外殼啟動時放進 globalThis.__hgShared）。
var sharedImports = regexp.MustCompile(`^(react|react/jsx-runtime|react-dom|react-dom/client|@evergenie/ui|@evergenie/sdk-web|@evergenie/host)$`)

var bundleHash = regexp.MustCompile(`^[0-9a-f]{16}$`)

// bundleEntry 是一個頁面上次編譯的結果；指紋沒變就直接回它（含上次的錯誤）。
type bundleEntry struct {
	fp      string
	hash    string
	errText string
}

// bundle 編譯套件 pkgID 的頁面 pageID，回產物的 hash（檔案在暫存資料夾的 runtime/web/bundles/<hash>.js）。
// 錯誤的文字是白話，直接給使用者看。
func (a *Assets) Bundle(pkgID, pageID string) (hash string, err error) {
	title := pageID
	pkg, page, ok := a.FindPage(pkgID, pageID)
	if ok && page.Title != "" {
		title = page.Title
	}
	fail := func(msg string) (string, error) {
		return "", fmt.Errorf("頁面「%s」編不過：%s", title, msg)
	}
	if !ok {
		return fail("找不到這個頁面")
	}
	dir, found := a.pk.PackageDir(pkg.ID)
	if !found {
		return fail("找不到套件的資料夾")
	}
	if a.homeDir() == "" {
		return fail("看板還沒有家，沒辦法存編譯結果")
	}
	if page.Component == "" {
		return fail("manifest 沒有寫這個頁面的 component 檔案")
	}

	a.bundleMu.Lock()
	defer a.bundleMu.Unlock()
	key := pkgID + "/" + pageID
	fp, err := fingerprint(dir)
	if err != nil {
		return fail("讀不了套件資料夾")
	}
	if c := a.bundles[key]; c != nil && c.fp == fp {
		if c.errText != "" {
			return "", fmt.Errorf("%s", c.errText)
		}
		if _, statErr := os.Stat(a.bundlePath(c.hash)); statErr == nil {
			return c.hash, nil
		}
	}
	hash, buildErr := a.compile(dir, page.Component, title)
	entry := &bundleEntry{fp: fp, hash: hash}
	if buildErr != nil {
		entry.errText = buildErr.Error()
	}
	a.bundles[key] = entry
	if buildErr != nil {
		return "", buildErr
	}
	return hash, nil
}

func (a *Assets) bundlePath(hash string) string {
	return platform.RuntimeDir(a.homeDir(), "web", "bundles", hash+".js")
}

// compile 真的跑 esbuild 並把產物存起來；呼叫的人持有 bundleMu。
func (a *Assets) compile(dir, component, title string) (string, error) {
	fail := func(msg string) (string, error) {
		return "", fmt.Errorf("頁面「%s」編不過：%s", title, msg)
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fail("讀不了套件資料夾")
	}
	entry := filepath.Join(dir, filepath.FromSlash(component))
	if !within(realDir, entry) {
		return fail(outsideMsg)
	}
	a.builds++
	res := api.Build(api.BuildOptions{
		EntryPoints:       []string{entry},
		Bundle:            true,
		Format:            api.FormatESModule,
		Platform:          api.PlatformBrowser,
		Target:            api.ES2022,
		JSX:               api.JSXAutomatic,
		Write:             false,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		AbsWorkingDir:     dir,
		LogLevel:          api.LogLevelSilent,
		Plugins:           []api.Plugin{sharedPlugin(realDir)},
	})
	if len(res.Errors) > 0 {
		first := res.Errors[0]
		msg := first.Text
		if first.Location != nil && first.Location.File != "" {
			msg += fmt.Sprintf("（%s:%d）", first.Location.File, first.Location.Line)
		}
		return fail(msg)
	}
	if len(res.OutputFiles) == 0 {
		return fail("編譯沒有產出任何東西")
	}
	out := res.OutputFiles[0].Contents
	sum := sha256.Sum256(out)
	hash := hex.EncodeToString(sum[:])[:16]
	path := a.bundlePath(hash)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		a.logger().Error("建立 bundles 資料夾失敗", "error", err)
		return fail("沒辦法存編譯結果，請檢查家的存取權限")
	}
	if err := platform.AtomicWrite(path, out, 0o600); err != nil {
		a.logger().Error("儲存編譯結果失敗", "error", err)
		return fail("沒辦法存編譯結果，請檢查家的存取權限")
	}
	return hash, nil
}

// within 回 path 是否在 root（已解開符號連結的絕對路徑）裡面；path 本身的符號連結也會解開。
func within(root, path string) bool {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// sharedPlugin 處理套件頁面的 import：外殼提供的幾個模組換成 globalThis.__hgShared，
// 其他只准相對路徑，而且解析後要在套件資料夾裡。
func sharedPlugin(root string) api.Plugin {
	return api.Plugin{Name: sharedNamespace, Setup: func(b api.PluginBuild) {
		b.OnResolve(api.OnResolveOptions{Filter: sharedImports.String()}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			return api.OnResolveResult{Path: args.Path, Namespace: sharedNamespace}, nil
		})
		b.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: sharedNamespace}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
			name, err := json.Marshal(args.Path)
			if err != nil {
				return api.OnLoadResult{}, err
			}
			contents := "module.exports = globalThis.__hgShared[" + string(name) + "];"
			return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
		})
		b.OnResolve(api.OnResolveOptions{Filter: ".*"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			if args.Kind == api.ResolveEntryPoint || args.PluginData == skipMark {
				return api.OnResolveResult{}, nil
			}
			if !strings.HasPrefix(args.Path, "./") && !strings.HasPrefix(args.Path, "../") {
				return api.OnResolveResult{Errors: []api.Message{{Text: bareImportMsg}}}, nil
			}
			r := b.Resolve(args.Path, api.ResolveOptions{
				Importer:   args.Importer,
				Namespace:  args.Namespace,
				ResolveDir: args.ResolveDir,
				Kind:       args.Kind,
				PluginData: skipMark,
			})
			if len(r.Errors) > 0 {
				return api.OnResolveResult{Errors: []api.Message{{Text: fmt.Sprintf("找不到「%s」這個檔案", args.Path)}}}, nil
			}
			if !within(root, r.Path) {
				return api.OnResolveResult{Errors: []api.Message{{Text: outsideMsg}}}, nil
			}
			return api.OnResolveResult{Path: r.Path, Namespace: "file"}, nil
		})
	}}
}

// fingerprint 是套件資料夾內全部檔案的（相對路徑、大小、修改時間）的 sha256；
// 略過 .git、node_modules，以及最上層的 .fork-base 與 cmd（附程式的原始碼，不是頁面的一部分）。
func fingerprint(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules":
				return filepath.SkipDir
			case ".fork-base", "cmd":
				if filepath.Dir(rel) == "." {
					return filepath.SkipDir
				}
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// findPage 在看板看得到的套件裡找頁面。
func (a *Assets) FindPage(pkgID, pageID string) (sdk.Package, sdk.PageDecl, bool) {
	for _, p := range a.VisiblePackages() {
		if p.ID != pkgID {
			continue
		}
		for _, pg := range p.Pages {
			if pg.ID == pageID {
				return p, pg, true
			}
		}
	}
	return sdk.Package{}, sdk.PageDecl{}, false
}

// serveBundle 回 /pkg-bundles/<hash>.js（編譯產物，內容雜湊命名，所以可以永久快取）。
func (a *Assets) ServeBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "不支援這個方法", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/pkg-bundles/")
	hash, isJS := strings.CutSuffix(name, ".js")
	if !isJS || !bundleHash.MatchString(hash) || a.homeDir() == "" {
		http.NotFound(w, r)
		return
	}
	info, err := os.Lstat(a.bundlePath(hash))
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(a.bundlePath(hash))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	h := w.Header()
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, hash+".js", info.ModTime(), f)
}
