package pkg

import (
	"bufio"
	"errors"
	"fmt"
	"go/version"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// sdkwork.go — 編譯主人自己的、別人分享來的附程式套件時，套件 import 的 sdk 用「正在跑的這份程式旁邊附的 sdk 原始碼」
// （packages 卡 PKG-66）。每次編譯前在 <家的 runtime>/packages/<id>/go.work 寫一份工作區（use 套件資料夾＋sdk 資料夾），
// go build 帶 GOWORK 指過去：套件的 go.mod 不用寫 require／replace（寫了的版本工作區各加一行 replace 指到這份，以這份為準），
// 不連私有的 repo、sdk 不用網路，
// 套件自己的檔一個都不改。升級換版之後 sdk 的位置跟著換（指紋裡有 sdk 的位置與內容），所以會用新版的 sdk 重編。

// sdkModulePath 是 sdk 的模組路徑，從型別讀出來（不在這裡另寫一次；改名 DEV-158 只改 sdk/go.mod 與 import）。
var sdkModulePath = reflect.TypeOf(sdk.Tier("")).PkgPath()

// sdkWorkName 是寫在 runtime/packages/<id>/ 的工作區檔名。
const sdkWorkName = "go.work"

// downloadGoProxy：下載版編譯時外部模組只從 Go 的公開模組站拿（不退回 git 直連：乾淨的 Mac 上 git 是會跳安裝視窗的空殼，
// 也不會去連私有的 repo）。sdk 本身不用網路。
const downloadGoProxy = "https://proxy.golang.org"

// goPathName、goCacheName：下載版編譯時 Go 的 GOPATH（外部模組的快取 pkg/mod 與檢查碼資料庫的快取 pkg/sumdb；沒用到外部模組
// 就不會建）與編譯快取放 <app>/tools/<這兩個名字>：不在主人的家目錄多一個 go 資料夾、不在 ~/Library/Caches 留編譯快取；跟 Go 一樣
// 是程式的東西，移除程式資料夾就一起清掉——所以帶 -modcacherw，快取裡的檔不是唯讀、刪得掉。
const (
	goPathName  = "gopath"
	goCacheName = "gocache"
)

// 給人看的話（白話；Go、sdk 是工具名）。
const (
	goMsgSDKMissing  = "編譯套件要用的工具組（程式資料夾裡的 sdk）找不到，套件編不了"
	goMsgSDKTooNew   = "這一版程式附的套件工具組（sdk）要 Go %s 以上，這台的編譯工具（Go）是 %s，套件編不了"
	goMsgSDKMismatch = "這個套件的程式跟這一版程式附的套件工具組（sdk）對不上（升級之後工具組改過），套件編不了；請秘書照這一版的工具組改套件的程式，再重新載入套件"
	goMsgSDKNoPkg    = "這個套件用到了這一版程式附的套件工具組（sdk）裡沒有的套件，套件編不了；請秘書照這一版的工具組改套件的程式（import 的路徑），再重新載入套件"
	goMsgModNet      = "這個套件用到的外部模組要從網路下載，現在下載不到（可能是網路不通），套件編不了；等網路正常後重新載入套件"
	goMsgModMissing  = "這個套件用到的外部模組在公開的模組站找不到（不會去連私有的程式庫），套件編不了；請秘書看看套件的 go.mod 用到了哪些模組"
	goMsgModNeedsGo  = "這個套件用到的外部模組 %s 要 Go %s 以上，這台的編譯工具（Go）是 %s，套件編不了；請秘書把套件 go.mod 裡這個模組換成這一版 Go 能用的舊一點的版本，再重新載入套件"
	goMsgBuildBroken = "這個套件的程式編譯沒有過（套件自己的程式要改）；請秘書照編譯工具說的改好，再重新載入套件"
	goMsgSDKReplace  = "這個套件的 go.mod 把套件工具組（sdk）指到找不到的資料夾，套件編不了；請秘書把 go.mod 裡跟 sdk 有關的那幾行拿掉（只留模組名稱與 Go 版本兩行），再重新載入套件"
	goMsgBuildSlow   = "編譯超過 %d 分鐘還沒好，已經停下來（多半是網路慢、外部模組還沒抓完）；等一下再重新載入套件，還是不行請秘書看看"

	goNextReinstall = "；跟任何一位秘書說，請她幫忙看看（重新安裝這一版的程式就會補回來）"
)

// sdkWork 是一次編譯要用的工作區。
type sdkWork struct {
	pkgDir   string   // 套件的資料夾
	sdkDir   string   // sdk 原始碼的資料夾
	file     string   // go.work 的位置
	goLine   string   // go.work 的 go 那一行（套件與 sdk 的 go.mod 裡比較新的那個）
	fromSDK  bool     // goLine 是 sdk 要的（比套件的 go.mod 新，或套件沒寫）
	versions []string // 套件的 go.mod 寫到的 sdk 版本：工作區各加一行 replace 指到附的 sdk
	stamp    string   // 加進編譯指紋：sdk 的位置與內容
	download bool     // 下載版（外部模組只走公開的模組站）
	tools    string   // 下載版：<app>/tools（GOPATH、編譯快取放這裡）
}

// bundledSDK：程式資料夾（exe 符號連結先解開的上上層，platform.ProgramDir，PLT-28）有 sdk/go.mod、而且模組名是 sdk 的時回那個 sdk 資料夾，
// 不是回空字串。下載版是 <app>/versions/<版本>/sdk（發行檔附的，runbooks/release.md「發行檔」），原始碼版是 <repo>/sdk（執行檔在 <repo>/bin/）。
func bundledSDK(exe string) string {
	program := platform.ProgramDir(exe)
	if program == "" {
		return ""
	}
	dir := filepath.Join(program, "sdk")
	if goModModule(filepath.Join(dir, "go.mod")) != sdkModulePath {
		return ""
	}
	return dir
}

// goModModule、goModGo 讀 go.mod 的 module 與 go 那一行（只看最外層的單行寫法；讀不到回空字串）。
func goModModule(path string) string { return goModField(path, "module") }
func goModGo(path string) string     { return goModField(path, "go") }

func goModField(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == key {
			v := fields[1]
			if u, err := strconv.Unquote(v); err == nil {
				v = u
			}
			return v
		}
	}
	return ""
}

// goLineRe：go.work 的 go 那一行收的寫法（1.24、1.24.3、1.25rc1）；比大小用標準函式庫的 go/version（連修訂版都比）。
var goLineRe = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?((rc|beta)[0-9]+)?$`)

func validGoLine(l string) bool { return goLineRe.MatchString(l) && version.IsValid("go"+l) }

// sdkWorkFor 決定 dir 這個套件要不要用工作區編。用的時候回 *sdkWork；不用（原始碼版而且執行檔旁邊沒有 sdk）回 nil，照套件自己的
// go.mod 編（同改之前）。下載版找不到 sdk 回白話的錯（不去找 Go、不問要不要下載：裝了 Go 也編不了）。
func (m *Module) sdkWorkFor(id, dir string) (*sdkWork, error) {
	sdkDir, download := m.tools.sdk, m.tools.dir != ""
	if sdkDir == "" || goModModule(filepath.Join(sdkDir, "go.mod")) != sdkModulePath {
		if download {
			return nil, errors.New(goMsgSDKMissing + goNextReinstall)
		}
		return nil, nil
	}
	// 套件資料夾寫成解開符號連結的乾淨絕對路徑，編譯也在這個路徑跑（build.go；execRunner 的 PWD 跟著給一樣的）：go 拿它跟目前
	// 資料夾逐字比，家經過符號連結（/tmp、/var、雲端硬碟的資料夾）或多一個斜線就對不上、編不了。
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	w := &sdkWork{pkgDir: dir, sdkDir: filepath.Clean(sdkDir), file: filepath.Join(m.runtimeDir(id), sdkWorkName), download: download}
	if download {
		w.tools = m.tools.dir
	}
	gomod := filepath.Join(dir, "go.mod")
	pkgLine, sdkLine := goModGo(gomod), goModGo(filepath.Join(sdkDir, "go.mod"))
	if !validGoLine(sdkLine) {
		sdkLine = strings.TrimPrefix(minGoVersion, "go")
	}
	// go 那一行要 ≥ 兩邊的（比到修訂版：sdk 寫 1.24.3、套件寫 1.24 時要 1.24.3，不然 go 說工作區的 go 太舊）；套件那一行寫壞了用 sdk 的。
	w.goLine = sdkLine
	if validGoLine(pkgLine) && version.Compare("go"+pkgLine, "go"+sdkLine) > 0 {
		w.goLine = pkgLine
	}
	w.fromSDK = w.goLine == sdkLine && pkgLine != sdkLine
	w.versions = sdkVersionsIn(gomod)
	w.stamp = "sdk=" + sdkDir + "|" + fingerprint(sdkDir)
	return w, nil
}

// sdkVersionRe：go.mod 裡 sdk 的版本收的寫法（寫進 go.work，只收這種，不把怪字抄過去）。
var sdkVersionRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

var parenSpacer = strings.NewReplacer("(", " ( ", ")", " ) ")

// sdkVersionsIn 讀套件 go.mod 的 require、replace 寫到 sdk 的版本（舊的套件、從官方套件 fork 來的會寫 require sdk v0.0.0＋
// replace 到不存在的相對路徑）。沒寫回空的。
func sdkVersionsIn(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	block := false
	for _, line := range strings.Split(string(b), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(parenSpacer.Replace(line)) // go 也認 require( 這種括號黏著的寫法
		switch {
		case len(f) == 0:
			continue
		case block && f[0] == ")":
			block = false
			continue
		case !block && (f[0] == "require" || f[0] == "replace") && len(f) == 2 && f[1] == "(":
			block = true
			continue
		case !block && (f[0] == "require" || f[0] == "replace"):
			f = f[1:]
		case !block:
			continue
		}
		if len(f) < 2 {
			continue
		}
		mod := f[0]
		if u, err := strconv.Unquote(mod); err == nil {
			mod = u
		}
		if mod == sdkModulePath && sdkVersionRe.MatchString(f[1]) {
			seen[f[1]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// write 寫 go.work（每次編譯前重寫）。套件 go.mod 寫到的 sdk 版本各加一行 replace 指到附的 sdk：go 不會照套件 go.mod 的
// replace（指到不存在的路徑）或去模組站找 sdk（工作區不收不帶版本的 replace：sdk 已經在 use 裡）。
func (w *sdkWork) write() error {
	body := fmt.Sprintf("// %s 編譯套件時寫的工作區（packages PKG-66），每次編譯前重寫，不要手改。\ngo %s\n\nuse (\n\t%s\n\t%s\n)\n",
		platform.ProductName, w.goLine, strconv.Quote(w.pkgDir), strconv.Quote(w.sdkDir))
	if len(w.versions) > 0 {
		body += "\nreplace (\n"
		for _, v := range w.versions {
			body += fmt.Sprintf("\t%s %s => %s\n", sdkModulePath, v, strconv.Quote(w.sdkDir))
		}
		body += ")\n"
	}
	if err := os.MkdirAll(filepath.Dir(w.file), 0o755); err != nil {
		return err
	}
	return platform.AtomicWrite(w.file, []byte(body), 0o644)
}

// env 是 go build 要加的環境：GOWORK 指到工作區；下載版的外部模組只走公開的模組站、不直連（也不吃外面設的私有模組設定，
// GOENV=off 連 Go 自己的設定檔都不讀），快取放程式的工具資料夾（goPathName、goCacheName），而且不用 C 編譯器（CGO_ENABLED=0，
// 跟發行檔一樣：乾淨的 Mac 上 clang、cc 是一叫就跳「安裝開發者工具」視窗的空殼）。
func (w *sdkWork) env() []string {
	env := []string{"GOWORK=" + w.file}
	if w.download {
		env = append(env, "GOENV=off", "GOPROXY="+downloadGoProxy, "GOPRIVATE=", "GONOPROXY=", "GONOSUMDB=", "GOINSECURE=", "GOSUMDB=",
			"GO111MODULE=", "GOOS=", "GOARCH=",
			"GOPATH="+filepath.Join(w.tools, goPathName), "GOMODCACHE="+filepath.Join(w.tools, goPathName, "pkg", "mod"),
			"GOCACHE="+filepath.Join(w.tools, goCacheName), "GOFLAGS=-modcacherw", "CGO_ENABLED=0")
	}
	return env
}

// buildError 是 go build 沒過：plain 是白話（主人的通知只放這段），detail 是編譯工具原文的最後幾行（給秘書看哪裡要改；
// 套件的錯誤 packages get 看得到）。
type buildError struct{ plain, detail string }

func (e *buildError) Error() string {
	if e.detail == "" {
		return e.plain
	}
	return e.plain + "。編譯工具的原文：\n" + e.detail
}

// buildNotice 是編譯失敗時給主人的通知內文：只有白話，不夾編譯工具的原文。
func buildNotice(err error) string {
	var be *buildError
	if errors.As(err, &be) {
		return be.plain
	}
	return errMessage(err)
}

// go 編譯失敗的原文裡認得出來的幾種（其他的都是套件自己的程式寫錯了：白話＋原文最後幾行給秘書看）。
var (
	sdkIdentRe  = regexp.MustCompile(`(?m)^\S+\.go:[0-9]+(:[0-9]+)?: .*\bsdk\.[A-Z]`)
	sdkGoneRe   = regexp.MustCompile(`no required module provides package|cannot find module providing package|missing go\.sum entry|module lookup disabled|unrecognized import path|cannot load module|reading https://|@v[0-9]|git `)
	modFetchRe  = regexp.MustCompile(`go: downloading|reading https://|Get "https://|@v[0-9][^:\s]*: `)
	modNetRe    = regexp.MustCompile(`dial tcp|no such host|i/o timeout|connection refused|network is unreachable|TLS handshake timeout|connection reset`)
	modGoneRe   = regexp.MustCompile(`404 Not Found|410 Gone|module lookup disabled|unknown revision|invalid version`)
	modTooNewRe = regexp.MustCompile(`(\S+@v\S+) requires go >= ([0-9][0-9.a-z]*) \(running go ([0-9][0-9.a-z]*)`)
)

// buildFailure 把 go build 失敗的原文換成白話。sdkChanged：上次編成功用的是另一份 sdk（升級換版了）——只有這時候，提到 sdk
// 的識別字的錯誤才說成「跟這一版的工具組對不上」，不然是套件自己的程式寫錯（第一次寫就打錯 sdk 的名字）。
func buildFailure(output string, w *sdkWork, sdkChanged bool) error {
	if need, have, ok := goTooNew(output); ok {
		if w != nil && w.fromSDK && strings.TrimPrefix(need, "go") == w.goLine {
			return &buildError{plain: fmt.Sprintf(goMsgSDKTooNew, strings.TrimPrefix(need, "go"), strings.TrimPrefix(have, "go")) + goNextSystem}
		}
		return &buildError{plain: goModTooNewText(need, have)}
	}
	if m := modTooNewRe.FindStringSubmatch(output); m != nil {
		return &buildError{plain: fmt.Sprintf(goMsgModNeedsGo, m[1], m[2], m[3])}
	}
	if sdkGone(output) {
		switch {
		case w != nil && sdkReplaced(output): // go.mod 的 replace 指到不存在的路徑、工作區又沒蓋過（版本沒認出來）
			return &buildError{plain: goMsgSDKReplace, detail: tail(output, 6)}
		case w == nil: // 沒用工作區（原始碼版、執行檔旁邊沒有 sdk）：套件的 go.mod 指不到 sdk
			return &buildError{plain: goMsgSDKMissing + goNextSystem}
		case goModModule(filepath.Join(w.sdkDir, "go.mod")) != sdkModulePath: // 編到一半附的 sdk 不見了
			next := goNextSystem
			if w.download {
				next = goNextReinstall
			}
			return &buildError{plain: goMsgSDKMissing + next}
		default: // sdk 在，是套件 import 了 sdk 沒有的套件（重新安裝修不好）
			return &buildError{plain: goMsgSDKNoPkg, detail: tail(output, 6)}
		}
	}
	if modFetchRe.MatchString(output) {
		if modNetRe.MatchString(output) {
			return &buildError{plain: goMsgModNet}
		}
		if modGoneRe.MatchString(output) {
			return &buildError{plain: goMsgModMissing}
		}
	}
	if sdkChanged && sdkIdentRe.MatchString(output) {
		return &buildError{plain: goMsgSDKMismatch, detail: tail(output, 6)}
	}
	return &buildError{plain: goMsgBuildBroken, detail: tail(output, 6)}
}

// sdkReplaced：原文裡有一行提到 sdk 被 go.mod 的 replace 換到別的地方（「sdk@版本 (replaced by …)」）。
func sdkReplaced(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, sdkModulePath+"@") && strings.Contains(line, "(replaced by ") {
			return true
		}
	}
	return false
}

// buildSlow 是編譯逾時（buildTimeout）：go 只吐了一部分，不拿去猜，直接說編太久停掉了。
func buildSlow() error {
	return &buildError{plain: fmt.Sprintf(goMsgBuildSlow, int(buildTimeout/time.Minute))}
}

// sdkGone：原文裡有一行同時提到 sdk 的模組路徑與「找不到／下載不了」。
func sdkGone(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, sdkModulePath) && sdkGoneRe.MatchString(line) {
			return true
		}
	}
	return false
}
