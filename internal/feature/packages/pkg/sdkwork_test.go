package pkg

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// TestBundledSDK：執行檔（經過 current 這種連結也一樣）的上上層有模組名對的 sdk/go.mod 才算；回的是解開連結後的位置。
func TestBundledSDK(t *testing.T) {
	app := t.TempDir()
	ver := filepath.Join(app, "versions", "0.2.0")
	exe := filepath.Join(ver, "bin", "core")
	for p, body := range map[string]string{
		exe:                                     "#!/bin/sh\n",
		filepath.Join(ver, "sdk", "go.mod"):     "// 附的 sdk\nmodule " + sdkModulePath + " // 註解\n\ngo 1.24\n",
		filepath.Join(app, "other", "bin", "x"): "x",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join("versions", "0.2.0"), filepath.Join(app, "current")); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(filepath.Join(ver, "sdk"))
	for _, p := range []string{exe, filepath.Join(app, "current", "bin", "core")} {
		if got := bundledSDK(p); got != real {
			t.Fatalf("bundledSDK(%s) = %q，要 %q", p, got, real)
		}
	}
	if got := bundledSDK(filepath.Join(app, "other", "bin", "x")); got != "" {
		t.Fatalf("旁邊沒有 sdk 要回空字串：%q", got)
	}
	if err := os.WriteFile(filepath.Join(ver, "sdk", "go.mod"), []byte("module example.com/other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := bundledSDK(exe); got != "" {
		t.Fatalf("模組名不對不算：%q", got)
	}
	if goModGo(filepath.Join(app, "none")) != "" || bundledSDK("") != "" {
		t.Fatal("讀不到要回空字串")
	}
}

// buildingRunner 像 go build 一樣真的寫出 -o 的檔，記下每次加的環境；fail 不是空的時照它回編譯失敗。
// 一定要帶 -buildvcs=false（套件資料夾在 git 裡時 go 不去叫 git）。
type buildingRunner struct {
	env  *[][]string
	fail *string
}

func (r buildingRunner) Run(_ context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	*r.env = append(*r.env, env)
	if len(args) < 2 || args[0] != "build" || args[1] != "-buildvcs=false" {
		return nil, errors.New("go build 要帶 -buildvcs=false")
	}
	if r.fail != nil && *r.fail != "" {
		return []byte(*r.fail), errors.New("exit status 1")
	}
	for i, a := range args {
		if a == "-o" && i+1 < len(args) {
			return nil, os.WriteFile(args[i+1], []byte("bin"), 0o755)
		}
	}
	return nil, errors.New("no -o")
}

func envOf(env []string, key string) (string, bool) {
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); k == key {
			return v, true
		}
	}
	return "", false
}

// TestSDKWorkspaceBuild（PKG-66）：主人的附程式套件用工作區編——go.work 在 runtime/packages/<id>/，use 套件資料夾與程式旁邊的 sdk，
// go 那一行取套件與 sdk 的 go.mod 比較新的；go build 帶 GOWORK；下載版的外部模組只走公開的模組站。套件自己的檔一個都不改。
// 內容沒變不重編；sdk 換了位置（升級換版）就重編、go.work 跟著指到新的。
func TestSDKWorkspaceBuild(t *testing.T) {
	h := newHarness(t)
	var envs [][]string
	h.m.runner = buildingRunner{env: &envs}
	sdk1, sdk2 := fakeSDK(t, "1.24"), fakeSDK(t, "1.24")
	h.m.tools = &goTools{dir: filepath.Join(t.TempDir(), "tools"), sdk: sdk1}
	gomod := "module e2e/watcher\n\ngo 1.22\n"
	files := programFiles("watcher")
	files["go.mod"] = gomod
	dir := h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), files)
	man, err := parseManifestBytes([]byte(programPkg("watcher", "", "")), dir, sdk.TierMine)
	if err != nil {
		t.Fatal(err)
	}
	ensure := func() {
		t.Helper()
		if _, err := h.m.ensureBinary(t.Context(), "watcher", sdk.TierMine, dir, man); err != nil {
			t.Fatal(err)
		}
	}
	work := filepath.Join(h.m.runtimeDir("watcher"), sdkWorkName)
	realDir, err := filepath.EvalSymlinks(dir) // go.work 寫解開符號連結的路徑（暫存資料夾在 macOS 是經過 /var 這個連結）
	if err != nil {
		t.Fatal(err)
	}
	checkWork := func(sdkDir, goLine string) {
		t.Helper()
		b, err := os.ReadFile(work)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, "\ngo "+goLine+"\n") || !strings.Contains(s, strconv.Quote(realDir)) || !strings.Contains(s, strconv.Quote(sdkDir)) {
			t.Fatalf("go.work 不對（要 go %s、use 套件與 %s）：\n%s", goLine, sdkDir, s)
		}
	}
	ensure()
	if len(envs) != 1 {
		t.Fatalf("要編一次：%d", len(envs))
	}
	if v, _ := envOf(envs[0], "GOWORK"); v != work {
		t.Fatalf("GOWORK 要指到 %s：%v", work, envs[0])
	}
	if v, _ := envOf(envs[0], "GOPROXY"); v != downloadGoProxy {
		t.Fatalf("下載版的外部模組只走公開的模組站：%v", envs[0])
	}
	for _, k := range []string{"GOINSECURE", "GOSUMDB", "GO111MODULE", "GOOS", "GOARCH"} {
		if v, ok := envOf(envs[0], k); !ok || v != "" {
			t.Fatalf("下載版不吃外面的 %s：%v", k, envs[0])
		}
	}
	if v, ok := envOf(envs[0], "GOPRIVATE"); !ok || v != "" {
		t.Fatalf("不吃外面設的私有模組：%v", envs[0])
	}
	gp := filepath.Join(h.m.tools.dir, goPathName)
	if v, _ := envOf(envs[0], "GOPATH"); v != gp {
		t.Fatalf("Go 的快取放程式的工具資料夾（不在家目錄多一個 go）：%v", envs[0])
	}
	if v, _ := envOf(envs[0], "GOMODCACHE"); v != filepath.Join(gp, "pkg", "mod") {
		t.Fatalf("外部模組的快取在工具資料夾的 gopath 裡（外面設的不算）：%v", envs[0])
	}
	if v, _ := envOf(envs[0], "GOCACHE"); v != filepath.Join(h.m.tools.dir, goCacheName) {
		t.Fatalf("編譯快取放程式的工具資料夾：%v", envs[0])
	}
	if v, _ := envOf(envs[0], "CGO_ENABLED"); v != "0" {
		t.Fatalf("下載版不用 C 編譯器：%v", envs[0])
	}
	if v, _ := envOf(envs[0], "GOFLAGS"); v != "-modcacherw" {
		t.Fatalf("快取要刪得掉（-modcacherw）：%v", envs[0])
	}
	if v, _ := envOf(envs[0], "GOENV"); v != "off" {
		t.Fatalf("不讀 Go 自己的設定檔（GOENV=off）：%v", envs[0])
	}
	checkWork(sdk1, "1.24") // 套件寫 1.22、sdk 要 1.24：取新的
	if b, _ := os.ReadFile(filepath.Join(dir, "go.mod")); string(b) != gomod {
		t.Fatalf("套件的 go.mod 被改了：%q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.work")); !os.IsNotExist(err) {
		t.Fatalf("套件資料夾裡不能多 go.work：%v", err)
	}
	ensure()
	if len(envs) != 1 {
		t.Fatal("內容沒變不能重編")
	}
	h.m.tools.sdk = sdk2 // 升級換版：sdk 在新版的資料夾
	ensure()
	if len(envs) != 2 {
		t.Fatal("sdk 換了位置要重編")
	}
	checkWork(sdk2, "1.24")
	if err := os.WriteFile(filepath.Join(sdk2, "doc.go"), []byte("package sdk\n\n// 改過\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ensure()
	if len(envs) != 3 {
		t.Fatal("sdk 的內容變了要重編")
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module e2e/watcher\n\ngo 1.26 // 新一點\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ensure()
	checkWork(sdk2, "1.26")

	// 套件的 go.mod 寫了 sdk 的 require／replace（舊的套件、從官方套件 fork 來的）：工作區對那幾個版本各加一行 replace 指到附的 sdk
	forked := "module e2e/watcher\n\ngo 1.24\n\nrequire (\n\t" + sdkModulePath + " v0.0.0\n\tgolang.org/x/text v0.3.0 // indirect\n)\n\n" +
		"require " + sdkModulePath + " v0.3.1\n\nreplace " + sdkModulePath + " => ../../../sdk\n\nreplace (\n\t" + sdkModulePath + " v0.0.2 => ./x\n\tother.example/m v1.0.0 => ./y\n)\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(forked), 0o644); err != nil {
		t.Fatal(err)
	}
	ensure()
	b, _ := os.ReadFile(work)
	wantReplace := "\nreplace (\n"
	for _, v := range []string{"v0.0.0", "v0.0.2", "v0.3.1"} {
		wantReplace += "\t" + sdkModulePath + " " + v + " => " + strconv.Quote(sdk2) + "\n"
	}
	if !strings.Contains(string(b), wantReplace+")\n") || strings.Contains(string(b), "other.example") {
		t.Fatalf("go.work 要對套件寫到的 sdk 版本各加 replace：\n%s", b)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "go.mod")); string(got) != forked {
		t.Fatal("套件的 go.mod 不能被改")
	}

	// go 那一行連修訂版都比；套件那一行寫壞了用 sdk 的
	sdk3 := fakeSDK(t, "1.24.3")
	h.m.tools.sdk = sdk3
	for pkgLine, want := range map[string]string{"1.24": "1.24.3", "1.24.5": "1.24.5", "1.x": "1.24.3", "": "1.24.3"} {
		body := "module e2e/watcher\n"
		if pkgLine != "" {
			body += "\ngo " + pkgLine + "\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		ensure()
		checkWork(sdk3, want)
	}
	h.m.tools.sdk = sdk2
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module e2e/watcher\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 原始碼版：有 sdk 也用工作區，但外部模組照 Go 平常的設定；執行檔旁邊沒有 sdk 就照套件自己的 go.mod（不帶 GOWORK）
	h.m.tools = &goTools{sdk: sdk1}
	_ = os.Remove(filepath.Join(h.m.runtimeDir("watcher"), "build.stamp"))
	ensure()
	last := envs[len(envs)-1]
	if _, ok := envOf(last, "GOWORK"); !ok {
		t.Fatalf("原始碼版有 sdk 也用工作區：%v", last)
	}
	for _, k := range []string{"GOPROXY", "GOPATH", "GOMODCACHE", "GOCACHE", "GOFLAGS", "CGO_ENABLED"} {
		if _, ok := envOf(last, k); ok {
			t.Fatalf("原始碼版不改 %s：%v", k, last)
		}
	}
	h.m.tools = &goTools{}
	_ = os.Remove(filepath.Join(h.m.runtimeDir("watcher"), "build.stamp"))
	ensure()
	if last := envs[len(envs)-1]; len(last) != 0 {
		t.Fatalf("原始碼版沒有 sdk：不帶工作區：%v", last)
	}
}

// TestSDKMismatchOnlyAfterUpgrade（PKG-66）：編不過而且提到 sdk 的名字，只有上次編成功用的是另一份 sdk（升級換版）才說「對不上」；
// 第一次寫就打錯是套件自己的程式寫錯（白話＋原文給秘書看）。
func TestSDKMismatchOnlyAfterUpgrade(t *testing.T) {
	h := newHarness(t)
	var envs [][]string
	fail := ""
	h.m.runner = buildingRunner{env: &envs, fail: &fail}
	sdk1, sdk2 := fakeSDK(t, "1.24"), fakeSDK(t, "1.24")
	h.m.tools = &goTools{dir: filepath.Join(t.TempDir(), "tools"), sdk: sdk1}
	build := func(id string) error {
		t.Helper()
		dir := h.write(sdk.TierMine, id, programPkg(id, "", ""), programFiles(id))
		man, err := parseManifestBytes([]byte(programPkg(id, "", "")), dir, sdk.TierMine)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.m.ensureBinary(t.Context(), id, sdk.TierMine, dir, man)
		return err
	}
	if err := build("old"); err != nil {
		t.Fatal(err)
	}
	h.m.tools.sdk = sdk2
	fail = "# e2e/old\n./main.go:5:22: undefined: sdk.Gone"
	err := build("old")
	if err == nil || !strings.Contains(err.Error(), goMsgSDKMismatch) || !strings.Contains(err.Error(), "undefined: sdk.Gone") {
		t.Fatalf("升級換了 sdk 後提到 sdk 的名字：%v", err)
	}
	err = build("fresh")
	if err == nil || !strings.Contains(err.Error(), goMsgBuildBroken) || strings.Contains(err.Error(), goMsgSDKMismatch) || !strings.Contains(err.Error(), "undefined: sdk.Gone") {
		t.Fatalf("第一次編就打錯 sdk 的名字是套件自己寫錯：%v", err)
	}
}

// TestSDKMissingOnDownload（PKG-66）：下載版找不到程式旁邊的 sdk → 套件 failed、白話（重新安裝這一版會補回來），不去找 Go、
// 不帶 install_go（裝了 Go 也編不了）。
func TestSDKMissingOnDownload(t *testing.T) {
	h := newHarness(t)
	f := newFakeGo(t, goTarball(t, goodGoFiles(fakeGoVersion)))
	f.tools.sdk = ""
	h.m.tools = f.tools
	looked := 0
	h.m.lookGo = func() (string, error) { looked++; return "/fake/go", nil }
	h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.start()
	p := h.get("watcher")
	if p.State != sdk.PackageStateFailed || p.Remedy != "" || !strings.Contains(p.Error, goMsgSDKMissing) || !strings.Contains(p.Error, "重新安裝這一版") {
		t.Fatalf("沒有 sdk：%+v", p)
	}
	plainOnly(t, strings.ReplaceAll(p.Error, "sdk", ""))
	if looked != 0 || len(h.builds) != 0 || f.hits.Load() != 0 {
		t.Fatalf("沒有 sdk 不找 Go、不編、不下載：looked=%d builds=%d hits=%d", looked, len(h.builds), f.hits.Load())
	}
}

// TestBuildFailurePlain（PKG-66）：go build 失敗的原文裡認得出來的幾種換成白話；其他的（套件自己的程式寫錯）是白話＋原文最後幾行
// （給秘書看）。給主人的通知（buildNotice）一律只有白話。提到 sdk 名字的錯誤，只有上次編成功用的是另一份 sdk 時才說「對不上」。
func TestBuildFailurePlain(t *testing.T) {
	sdkDir := fakeSDK(t, "1.24")
	dl := &sdkWork{goLine: "1.30", sdkDir: sdkDir, download: true}
	fromSDK := &sdkWork{goLine: "1.30", fromSDK: true, sdkDir: sdkDir, download: true}
	gone := &sdkWork{goLine: "1.24", sdkDir: filepath.Join(t.TempDir(), "gone"), download: true}
	netOut := "main.go:6:2: golang.org/x/text@v0.14.0: Get \"https://proxy.golang.org/golang.org/x/text/@v/v0.14.0.mod\": dial tcp: lookup proxy.golang.org: no such host"
	cases := []struct {
		name, out  string
		w          *sdkWork
		sdkChanged bool
		want       string
		detail     string // 原文要帶的（空白＝不帶原文）
	}{
		{"套件的 go.mod 要更新的 Go", "go: /h/runtime/packages/w/go.work requires go >= 1.30 (running go 1.27.4; GOTOOLCHAIN=local)", dl, false, "這個套件的 go.mod 要 Go 1.30 以上", ""},
		{"sdk 要更新的 Go", "go: /h/runtime/packages/w/go.work requires go >= 1.30 (running go 1.27.4; GOTOOLCHAIN=local)", fromSDK, false, "這一版程式附的套件工具組（sdk）要 Go 1.30 以上", ""},
		{"外部模組要更新的 Go", "go: golang.org/x/text@v0.40.0 requires go >= 1.30.1 (running go 1.27.4; GOTOOLCHAIN=local)", dl, false, "外部模組 golang.org/x/text@v0.40.0 要 Go 1.30.1 以上", ""},
		{"沒有工作區、找不到 sdk", "main.go:2:19: no required module provides package " + sdkModulePath + "; to add it:\n\tgo get " + sdkModulePath, nil, false, goMsgSDKMissing + goNextSystem, ""},
		{"工作區的 sdk 不見了", "main.go:2:19: no required module provides package " + sdkModulePath + "; to add it:", gone, false, goMsgSDKMissing + goNextReinstall, ""},
		{"import 了 sdk 沒有的套件", "extra.go:3:8: no required module provides package " + sdkModulePath + "/foo; to add it:\n\tgo get " + sdkModulePath + "/foo", dl, false, goMsgSDKNoPkg, sdkModulePath + "/foo"},
		{"外部模組網路不通（第一次抓 .mod）", netOut, dl, false, goMsgModNet, ""},
		{"外部模組網路不通（連線被拒）", strings.Replace(netOut, "lookup proxy.golang.org: no such host", "connect: connection refused", 1), dl, false, goMsgModNet, ""},
		{"外部模組網路不通（已經有 .mod）", "go: downloading golang.org/x/text v0.3.0\nextra.go:2:8: golang.org/x/text@v0.3.0: Get \"https://proxy.golang.org/golang.org/x/text/@v/v0.3.0.zip\": dial tcp: lookup proxy.golang.org: no such host", dl, false, goMsgModNet, ""},
		{"外部模組找不到", "go: example.com/private@v1.0.0: reading https://proxy.golang.org/example.com/private/@v/v1.0.0.mod: 404 Not Found", dl, false, goMsgModMissing, ""},
		{"升級換了 sdk 之後對不上", "# e2e/p2\n./main.go:4:45: undefined: sdk.PackageHost\n./main.go:5:22: undefined: sdk.ServePackage", dl, true, goMsgSDKMismatch, "undefined: sdk.PackageHost"},
		{"第一次寫就打錯 sdk 的名字", "# e2e/p2\n./main.go:5:22: undefined: sdk.Reigster", dl, false, goMsgBuildBroken, "undefined: sdk.Reigster"},
		{"沒實作 sdk 的介面", "./main.go:9:30: prog does not implement sdk.Program (missing method Run)", dl, false, goMsgBuildBroken, "does not implement sdk.Program"},
		{"go.mod 的 replace 把 sdk 指到不存在的地方", "go: e2e/p imports " + sdkModulePath + ": " + sdkModulePath + "@v0.0.0 (replaced by ../../../sdk): reading ../../../sdk/go.mod: open /h/sdk/go.mod: no such file or directory", dl, false, goMsgSDKReplace, "(replaced by ../../../sdk)"},
		{"套件自己的語法錯", "./main.go:3:1: syntax error: unexpected }", dl, false, goMsgBuildBroken, "syntax error: unexpected }"},
		{"原始碼版沒有工作區、自己寫錯", "./main.go:3:1: syntax error: unexpected }", nil, true, goMsgBuildBroken, "syntax error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := buildFailure(c.out, c.w, c.sdkChanged)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("要 %q：%v", c.want, err)
			}
			if c.detail != "" && (!strings.Contains(err.Error(), "編譯工具的原文：\n") || !strings.Contains(err.Error(), c.detail)) {
				t.Fatalf("要帶原文 %q：%v", c.detail, err)
			}
			if c.detail == "" && strings.Contains(err.Error(), "原文") {
				t.Fatalf("認得出來的不用帶原文：%v", err)
			}
			notice := buildNotice(err)
			if strings.Contains(notice, "原文") || strings.Contains(notice, "\n") {
				t.Fatalf("給主人的通知只放白話：%q", notice)
			}
			plainOnly(t, strings.NewReplacer("go.mod", "", "sdk", "", "「go 1.27」", "", "golang.org/x/text@v0.40.0", "", "import", "").Replace(notice))
		})
	}
	if _, _, ok := goTooNew("go: golang.org/x/text@v0.40.0 requires go >= 1.30.1 (running go 1.27.4; GOTOOLCHAIN=local)"); ok {
		t.Fatal("外部模組要更新的 Go 不是套件 go.mod 的 go 那一行")
	}
}

// TestExecRunnerSetsPWD：編譯的指令拿到的 PWD 是套件資料夾本身（經過符號連結也照原樣），不是服務自己的 PWD（PKG-66）。
func TestExecRunnerSetsPWD(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "real"), link); err != nil {
		t.Fatal(err)
	}
	out, err := execRunner{}.Run(context.Background(), link, nil, "/bin/sh", "-c", `printf %s "$PWD"`)
	if err != nil || string(out) != link {
		t.Fatalf("PWD = %q（%v），要 %q", out, err, link)
	}
}

// TestWorkspaceBuildThroughSymlink：家的路徑經過符號連結（macOS 的 /tmp、/var 都是）時，真的 go 照工作區編得過——
// 沒有 PWD，go 會用解開連結的實際路徑當目前資料夾，跟 go.work 寫的套件資料夾對不上（PKG-66）。用 repo 的 sdk、不連網。
func TestWorkspaceBuildThroughSymlink(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil || testing.Short() {
		t.Skip("沒有 go 或 -short")
	}
	sdkDir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "sdk"))
	if err != nil || goModModule(filepath.Join(sdkDir, "go.mod")) != sdkModulePath {
		t.Skip("找不到 repo 的 sdk")
	}
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(base, "link", "p") + "/" // 多一個斜線也要能編
	writeFiles(t, pkgDir, map[string]string{
		"go.mod":  "module e2e/p\n\ngo 1.24\n",
		"main.go": "package main\n\nimport \"" + sdkModulePath + "\"\n\nfunc main() { _ = sdk.Tier(\"\") }\n",
	})
	h := newHarness(t)
	h.m.tools = &goTools{sdk: sdkDir}
	w, err := h.m.sdkWorkFor("p", pkgDir)
	if err != nil || w == nil {
		t.Fatalf("要用工作區：%v", err)
	}
	if real, _ := filepath.EvalSymlinks(pkgDir); w.pkgDir != real {
		t.Fatalf("go.work 的套件資料夾要解開符號連結：%q，要 %q", w.pkgDir, real)
	}
	if err := w.write(); err != nil {
		t.Fatal(err)
	}
	env := append(w.env(), "GOENV=off", "GOFLAGS=", "GOPROXY=off")
	// 編譯在 go.work 寫的那個路徑跑（build.go），也在經過連結的原路徑跑一次（PWD 照給，go 一樣認得）
	for i, dir := range []string{w.pkgDir, filepath.Join(base, "link", "p")} {
		out := filepath.Join(base, "p"+strconv.Itoa(i)+".bin")
		if b, err := (execRunner{}).Run(context.Background(), dir, env, goBin, "build", "-buildvcs=false", "-o", out, "."); err != nil {
			if i == 0 {
				t.Fatalf("在 %s 編不過：%v\n%s", dir, err, b)
			}
			continue // 原路徑與 go.work 寫的不一樣，go 不收：所以 build.go 一定在 w.pkgDir 跑
		}
		if !isExecutable(out) {
			t.Fatal("沒有編出執行檔")
		}
	}
}

// TestSDKVersionsIn：go.mod 的 require、replace 寫到 sdk 的版本都認得（單行、區塊、括號黏著的 require(、引號、註解）；
// 不合法的版本不抄進 go.work。
func TestSDKVersionsIn(t *testing.T) {
	dir := t.TempDir()
	gomod := filepath.Join(dir, "go.mod")
	body := "module e2e/p\n\ngo 1.24\n\nrequire(\n\t\"" + sdkModulePath + "\" v0.0.0 // indirect\n\tgolang.org/x/text v0.3.0\n)\n\n" +
		"replace(\n\t" + sdkModulePath + " v0.0.2 => ../sdk\n)\nreplace " + sdkModulePath + " v1.2.3.. => ../x\nrequire " + sdkModulePath + " v0.3.1-rc.1+meta\n"
	if err := os.WriteFile(gomod, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sdkVersionsIn(gomod), " "); got != "v0.0.0 v0.0.2 v0.3.1-rc.1+meta" {
		t.Fatalf("認出來的 sdk 版本：%q", got)
	}
}

// slowRunner 等到逾時才回（go 只吐了一部分）。
type slowRunner struct{}

func (slowRunner) Run(ctx context.Context, _ string, _ []string, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return []byte("go: downloading golang.org/x/text v0.3.0\n"), ctx.Err()
}

// TestBuildTimeoutPlain：編譯逾時說「編太久停掉了」，不猜成套件自己的程式寫錯。
func TestBuildTimeoutPlain(t *testing.T) {
	old := buildTimeout
	buildTimeout = 20 * time.Millisecond
	t.Cleanup(func() { buildTimeout = old })
	h := newHarness(t)
	h.m.runner = slowRunner{}
	h.m.tools = &goTools{dir: filepath.Join(t.TempDir(), "tools"), sdk: fakeSDK(t, "1.24")}
	dir := h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	man, err := parseManifestBytes([]byte(programPkg("watcher", "", "")), dir, sdk.TierMine)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.m.ensureBinary(t.Context(), "watcher", sdk.TierMine, dir, man)
	if err == nil || !strings.Contains(err.Error(), "還沒好，已經停下來") || strings.Contains(err.Error(), goMsgBuildBroken) || strings.Contains(err.Error(), "downloading") {
		t.Fatalf("逾時要白話：%v", err)
	}
}
