package pkg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/version"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// toolchain.go — 編譯附程式的套件（主人自己的、分享來的）要用的 Go（packages 卡 PKG-65；沒問過主人不下載是 PKG-N5）。
// 找：下載版先看程式自己的工具資料夾 <app>/tools/go，再看 PATH；原始碼版看 PATH 與常見的安裝位置（同改之前）。版本要 ≥ minGoVersion。
// 補裝：只在主人同意之後（InstallGo 的 confirmed），把 officialGo 寫死的那一版從官方下載點下載、對大小與 sha256、解開、換上。
// 任何一步沒過都是白話＋下一步（英文細節只進 serve 的日誌），不留下載或解到一半的東西，原本的 tools/go 不動。

// minGoVersion：編譯套件最低要的 Go。套件只 import sdk，跟 sdk/go.mod 的 go 那一行一樣（TestMinGoVersionMatchesSDK 對過）。
const minGoVersion = "go1.24"

// officialGo：InstallGo 下載的 Go，只寫在這裡一處；發版時跟著更新（runbooks/release.md「主人同意後下載的 Go」）。
// 版本、檔名、sha256、大小抄自 Go 官方的下載清單（https://go.dev/dl/?mode=json）；執行期不去抓「最新版」。
var officialGo = goPin{
	Version: "go1.27.4",
	Base:    "https://go.dev/dl/",
	Files: map[string]goFile{
		"darwin-arm64": {Name: "go1.27.4.darwin-arm64.tar.gz", SHA256: "91a44a2b8a716600ad94a4f143e156116620db8828086a6e3602291be9bd0bb4", Size: 68231011},
		"darwin-amd64": {Name: "go1.27.4.darwin-amd64.tar.gz", SHA256: "2b799377704ef12db39bd254995d5c348c9cc8d86562ca817724390e56c38f43", Size: 71756705},
	},
}

// testGoPinFile 只有測試編的版本才有值（scripts/dist.sh --test-go-pin <檔>，用 -ldflags -X 編進去）：指到一份 JSON（goPin 的欄位），
// 把下載點、版本與檢查碼換成本機 http 上的假包（同 install.sh 的 --source）；換了也一樣對大小與 sha256。
// 正式的版本是空的，執行期不讀任何環境變數或設定來換掉 officialGo（測試開關不留在正式程式裡）。
var testGoPinFile string

const (
	goSpaceFactor     = 5                // 要的空間：包的 5 倍（下載的包＋解開約 3.5 倍）
	goMaxUnpacked     = 1 << 30          // 解開後最多 1 GiB（官方的包約 240 MB）
	goProbeTimeout    = 20 * time.Second // 問一支 go 的版本最多等多久
	goStaleAfter      = time.Hour        // 上次被中斷留下的暫存，這麼久沒動才收掉
	goDownloadTimeout = time.Hour        // 下載 Go 的包最多多久（約 70MB，慢的網路也夠）
)

type goFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type goPin struct {
	Version string            `json:"version"`
	Base    string            `json:"base"`
	Files   map[string]goFile `json:"files"` // 「darwin-arm64」這種 → 這台晶片的包
}

// 給人看的話（白話、不夾英文；Go 是工具名）。
const (
	goMsgNone     = "這台電腦還沒有編譯工具（Go）"
	goMsgOld      = "這台電腦的編譯工具（Go）太舊了（現在是 %s，要 %s 以上）"
	goMsgBroken   = "這台電腦的編譯工具（Go）沒辦法用"
	goMsgSource   = "這份程式是原始碼版（開發用），不幫忙下載 Go：請自己裝好 Go（" + minGoVersion + " 以上）再試一次"
	goMsgPin      = "下載 Go 的設定讀不到"
	goMsgArch     = "這台電腦的晶片沒有可以下載的 Go"
	goMsgSpace    = "電腦的空間不夠（裝 Go 要大約 %dMB）"
	goMsgNet      = "下載不到 Go（可能是網路不通）"
	goMsgRedirect = "下載 Go 的網址被轉到不安全的地方，已經停下來"
	goMsgCut      = "Go 下載到一半斷掉了"
	goMsgSum      = "下載的 Go 不完整或被改過（檢查碼對不上）"
	goMsgContent  = "下載的 Go 內容不對"
	goMsgWrite    = "沒辦法把 Go 放進程式資料夾"
	goMsgStill    = "Go 裝好了，但還是用不了"

	goNextRetry  = "；等網路正常後再試一次"
	goNextAgain  = "；再試一次，一直這樣就跟任何一位秘書說，請她幫忙看看"
	goNextSystem = "；跟任何一位秘書說，請她幫忙看看"
	goNextSpace  = "；清出一些空間後再試一次"
	goNextWrite  = "；跟任何一位秘書說，請她幫忙看看（可能是空間不夠或資料夾權限不對）"
)

// goAsk 是秘書問主人的那一句（status 的 ask、套件的錯誤訊息都用它）。
func goAsk(mb int) string {
	return fmt.Sprintf("這個套件要先裝編譯工具（Go，約 %dMB，裝在程式自己的資料夾裡），要我裝嗎？", mb)
}

// goMissing：找不到能用的 Go（沒有、太舊、跑不起來）。不自動裝：套件標 failed、補救代號 install_go，主人同意後才 InstallGo。
type goMissing struct{ reason string }

func (e *goMissing) Error() string { return e.reason }
func (e *goMissing) Unwrap() error { return errNoGo }

// goFail：InstallGo 沒成功。plain 是給人看的（白話＋「沒有裝」＋下一步），detail 是只進日誌的原文。
type goFail struct {
	plain  string
	detail error
	never  bool // 這份程式、這台電腦本來就不能幫忙下載（原始碼版、沒有這台晶片的包、設定讀不到、下載點被轉到不安全的地方）：重試也一樣
}

func (e *goFail) Error() string { return e.plain }

// apiError 是 InstallGo 回的錯：可以重試的（網路、空間、檢查碼…）是 UNAVAILABLE；重試也一樣的是 CONFLICT（ERR-2）。
func (e *goFail) apiError() error {
	if e.never {
		return platform.Conflict(e.plain)
	}
	return platform.Unavailable(e.plain)
}

func failGo(msg, next string, detail error) error {
	return &goFail{plain: msg + "，沒有裝" + next, detail: detail}
}

// goTools 決定去哪裡找 Go、能不能幫忙下載。New 照這份執行檔的位置定；測試換掉欄位。
type goTools struct {
	dir      string   // 下載版：<app>/tools（Go 放在 tools/go）；原始碼版空白（不幫忙下載）
	sdk      string   // 編譯套件用的 sdk 原始碼：這份執行檔旁邊附的（bundledSDK，PKG-66）；找不到是空白
	extra    []string // 原始碼版另外看的常見位置（launchd 的 PATH 很短；同改之前）
	platform string   // 挑包用：darwin-arm64 這種
	pin      func() (goPin, error)
	lookPath func(string) (string, error)
	version  func(goBin string) (string, error)
	free     func(dir string) (uint64, error)
	client   *http.Client // nil＝平常的連線；測試換成假的（轉址的規則照樣套上，platform.DownloadClient）
}

func newGoTools() *goTools {
	t := &goTools{platform: runtime.GOOS + "-" + runtime.GOARCH, pin: buildPin, lookPath: exec.LookPath, version: goVersionOf, free: freeSpace}
	exe, _ := os.Executable()
	t.sdk = bundledSDK(exe)
	if app := platform.DownloadInstallOf(exe); app != "" {
		t.dir = filepath.Join(app, platform.DownloadToolsDir)
	} else {
		t.extra = []string{"/opt/homebrew/bin/go", "/usr/local/go/bin/go", "/usr/local/bin/go"}
	}
	return t
}

// buildPin：正式的版本一律是 officialGo；只有測試編的版本（testGoPinFile 有值）讀那份 JSON 換掉。
func buildPin() (goPin, error) { return pinFrom(testGoPinFile) }

// pinFrom：p 空白就是 officialGo，不然讀 p 那份 JSON。
func pinFrom(p string) (goPin, error) {
	if p == "" {
		return officialGo, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return goPin{}, err
	}
	var pin goPin
	if err := json.Unmarshal(b, &pin); err != nil {
		return goPin{}, err
	}
	return pin, nil
}

type goFound struct {
	path, version, source string
	problem               string // 白話：沒有、太舊、跑不起來（空白＝能用）
}

// find 照順序找第一支能用的 go：tools/go/bin/go ＞ PATH ＞（原始碼版）常見位置。都不能用時 problem 說第一個問題。
func (t *goTools) find() goFound {
	type cand struct{ path, source string }
	var cands []cand
	if t.dir != "" {
		cands = append(cands, cand{filepath.Join(t.dir, "go", "bin", "go"), "tools"})
	}
	if p, err := t.lookPath("go"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			cands = append(cands, cand{abs, "path"})
		}
	}
	for _, p := range t.extra {
		cands = append(cands, cand{p, "path"})
	}
	problem := ""
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c.path] || !isExecutable(c.path) {
			continue
		}
		seen[c.path] = true
		v, err := t.version(c.path)
		switch {
		case err != nil:
			if problem == "" {
				problem = goMsgBroken
			}
		case !goAtLeast(v, minGoVersion):
			if problem == "" {
				problem = fmt.Sprintf(goMsgOld, v, minGoVersion)
			}
		default:
			return goFound{path: c.path, version: v, source: c.source}
		}
	}
	if problem == "" {
		problem = goMsgNone
	}
	return goFound{problem: problem}
}

// look 是編譯用的：能用的 go 的位置，或 *goMissing。
func (t *goTools) look() (string, error) {
	f := t.find()
	if f.problem != "" {
		return "", &goMissing{reason: f.problem}
	}
	return f.path, nil
}

// offer：InstallGo 能不能幫忙下載；不能時 why 是白話原因。
func (t *goTools) offer() (pin goPin, file goFile, why string) {
	if t.dir == "" {
		return goPin{}, goFile{}, goMsgSource
	}
	pin, err := t.pin()
	if err != nil {
		return goPin{}, goFile{}, goMsgPin
	}
	f, ok := pin.Files[t.platform]
	if !ok || f.Name == "" || strings.Contains(f.Name, "/") || len(f.SHA256) != 64 || f.Size <= 0 || pin.Version == "" {
		return goPin{}, goFile{}, goMsgArch
	}
	if !platform.DownloadURLAllowed(goURL(pin.Base, f)) { // 下載來源的規則（PLT-25）
		return goPin{}, goFile{}, goMsgPin
	}
	return pin, f, ""
}

// status 是 GoStatus 的內容（唯讀，不下載）。
func (t *goTools) status() *sdk.PackageGoStatusResponse {
	f := t.find()
	r := &sdk.PackageGoStatusResponse{Usable: f.problem == "", Source: f.source, Path: f.path, Version: f.version, Need: minGoVersion}
	pin, file, why := t.offer()
	if why == "" {
		r.Installable, r.Offer, r.SizeMB = true, pin.Version, sizeMB(file.Size)
	}
	if !r.Usable {
		r.Detail = f.problem
		if r.Installable {
			r.Ask = goAsk(r.SizeMB)
		} else {
			r.Detail += "。" + whyNext(why)
		}
	}
	return r
}

// missingText 是套件編譯不了時的白話：原因＋問主人的那一句（能幫忙下載時）或為什麼不能幫忙。
func (t *goTools) missingText(reason string) string {
	_, file, why := t.offer()
	if why != "" {
		return reason + "。" + whyNext(why)
	}
	return reason + "。" + goAsk(sizeMB(file.Size))
}

// whyNext：不能幫忙下載的原因＋下一步（原始碼版的那句自己帶了下一步）。
func whyNext(why string) string {
	if why == goMsgSource {
		return why
	}
	return why + goNextSystem
}

// install 下載寫死的那一版到 <dir>/go。成功回版本；失敗回 *goFail，除了上次留下的暫存什麼都不動。
func (t *goTools) install(ctx context.Context) (string, error) {
	pin, f, why := t.offer()
	if why != "" {
		if why == goMsgSource {
			return "", &goFail{plain: why, never: true}
		}
		fail := failGo(why, goNextSystem, nil).(*goFail)
		fail.never = true
		return "", fail
	}
	if err := os.MkdirAll(t.dir, 0o755); err != nil {
		return "", failGo(goMsgWrite, goNextWrite, err)
	}
	cleanGoLeftovers(t.dir)
	need := uint64(f.Size) * goSpaceFactor
	if free, err := t.free(t.dir); err == nil && free < need {
		return "", failGo(fmt.Sprintf(goMsgSpace, sizeMB(int64(need))), goNextSpace, fmt.Errorf("free %d < need %d", free, need))
	}
	stamp := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	archive := filepath.Join(t.dir, ".go-download-"+stamp)
	unpack := filepath.Join(t.dir, ".go-unpack-"+stamp)
	defer os.Remove(archive)
	defer os.RemoveAll(unpack)
	if err := t.download(ctx, pin.Base, f, archive); err != nil {
		return "", err
	}
	if err := extractGo(archive, unpack); err != nil {
		var bad *platform.UnpackError
		if errors.As(err, &bad) && bad.Bad {
			return "", failGo(goMsgContent, goNextSystem, err)
		}
		return "", failGo(goMsgWrite, goNextWrite, err)
	}
	if v, err := t.version(filepath.Join(unpack, "go", "bin", "go")); err != nil || v != pin.Version {
		return "", failGo(goMsgContent, goNextSystem, fmt.Errorf("unpacked go reports %q (want %s): %v", v, pin.Version, err))
	}
	dst := filepath.Join(t.dir, "go")
	old := filepath.Join(t.dir, ".go-old-"+stamp)
	hadOld := false
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			return "", failGo(goMsgWrite, goNextWrite, err)
		}
		hadOld = true
	}
	if err := os.Rename(filepath.Join(unpack, "go"), dst); err != nil {
		if hadOld {
			_ = os.Rename(old, dst)
		}
		return "", failGo(goMsgWrite, goNextWrite, err)
	}
	if hadOld {
		_ = os.RemoveAll(old)
	}
	cleanGoLeftovers(t.dir)
	return pin.Version, nil
}

// goURL 是這台晶片的 Go 包的網址（下載點＋檔名）。
func goURL(base string, f goFile) string { return strings.TrimSuffix(base, "/") + "/" + f.Name }

// download 照下載來源的規則（platform.OpenDownload，PLT-25：https、本機的 http、file；https 轉成別的一律失敗）抓 Go 的包到 dst，
// 邊寫邊對大小與 sha256（platform.CopyVerified，PLT-27）。
func (t *goTools) download(ctx context.Context, base string, f goFile, dst string) error {
	body, _, err := platform.OpenDownload(ctx, t.client, goURL(base, f), goDownloadTimeout)
	if err != nil {
		var de *platform.DownloadError
		if errors.As(err, &de) && (de.Fault == platform.DownloadNotAllowed || de.Fault == platform.DownloadUnsafeRedirect) {
			// 重試也一樣（CONFLICT）：下載點不照規則、被轉到不安全的地方
			msg := goMsgPin
			if de.Fault == platform.DownloadUnsafeRedirect {
				msg = goMsgRedirect
			}
			fail := failGo(msg, goNextSystem, fmt.Errorf("get %s: %w", f.Name, err)).(*goFail)
			fail.never = true
			return fail
		}
		return failGo(goMsgNet, goNextRetry, fmt.Errorf("get %s: %w", f.Name, err))
	}
	defer body.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return failGo(goMsgWrite, goNextWrite, err)
	}
	_, err = platform.CopyVerified(out, body, f.Size, f.SHA256)
	cerr := out.Close()
	var ce *platform.CopyError
	errors.As(err, &ce)
	switch {
	case ce != nil && ce.Fault == platform.CopyWrite:
		return failGo(goMsgWrite, goNextWrite, err)
	case cerr != nil:
		return failGo(goMsgWrite, goNextWrite, cerr)
	case ce != nil && ce.Fault == platform.CopyCut:
		return failGo(goMsgCut, goNextRetry, fmt.Errorf("%s: %w", f.Name, err))
	case err != nil:
		return failGo(goMsgSum, goNextAgain, fmt.Errorf("%s: %w", f.Name, err))
	}
	return nil
}

// extractGo 把官方的 Go 包解到 dst/go（platform.ExtractTarGz，PLT-27：只收最上層 go/ 底下的資料夾與一般檔，解開後最多 goMaxUnpacked），
// 而且要有 go/bin/go。包的內容不對回 Bad 的 *platform.UnpackError。
func extractGo(archive, dst string) error {
	if err := platform.ExtractTarGz(archive, dst, platform.UnpackOptions{Top: "go", KeepTop: true, MaxBytes: goMaxUnpacked}); err != nil {
		return err
	}
	if !isExecutable(filepath.Join(dst, "go", "bin", "go")) {
		return &platform.UnpackError{Bad: true, Err: errors.New("no go/bin/go")}
	}
	return nil
}

// cleanGoLeftovers 收掉上次被中斷留下的暫存（下載到一半的包、解到一半的資料夾、換下來的舊版）：名字裡的行程已經不在了，
// 或一段時間沒動。還在跑的別的行程（同一份程式的另一個家正在下載）留著。install 前後與套件模組啟動時各收一次。
func cleanGoLeftovers(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		n := e.Name()
		rest := ""
		for _, p := range []string{".go-download-", ".go-unpack-", ".go-old-"} {
			if strings.HasPrefix(n, p) {
				rest = strings.TrimPrefix(n, p)
			}
		}
		if rest == "" {
			continue
		}
		gone := false
		if pid, err := strconv.Atoi(strings.SplitN(rest, "-", 2)[0]); err == nil && pid > 0 && pid != os.Getpid() {
			gone = errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
		}
		if info, err := e.Info(); err == nil && (gone || time.Since(info.ModTime()) > goStaleAfter) {
			_ = os.RemoveAll(filepath.Join(dir, n))
		}
	}
}

// goEnv 是叫 go 的環境（問版本與編譯共用）：GOTOOLCHAIN=local，go 不會因為套件的 go.mod 要更新的版本就自己去下載別的 Go
// （主人沒同意不下載，PKG-N5）；不帶外面的 GOROOT（不然換一支 go 也會用到別的標準函式庫）。
func goEnv() []string {
	env := []string{"GOTOOLCHAIN=local"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOROOT=") && !strings.HasPrefix(kv, "GOTOOLCHAIN=") {
			env = append(env, kv)
		}
	}
	return env
}

// goTooNew 認出「套件的 go.mod（或工作區的 go.work）要的 Go 比這支新」（GOTOOLCHAIN=local 時 go 的說法），回要的與現在的版本。
// 外部模組要更新的 Go（「模組@版本 requires go >= …」）不算：改套件 go.mod 的 go 那一行修不好（sdkwork.go 另外說）。
var goTooNewRe = regexp.MustCompile(`go\.(?:mod|work) requires go >= ([0-9][0-9.a-z]*) \(running go ([0-9][0-9.a-z]*)`)

// goMsgModTooNew 是套件的 go.mod 要的版本比這台的 Go 新時的白話（不換 Go、不下載；改 go.mod 就好）。
const goMsgModTooNew = "這個套件的 go.mod 要 Go %s 以上，這台的編譯工具（Go）是 %s，不會自己去下載別的版本：請把套件 go.mod 裡 go 那一行改成「go %s」再重新載入"

func goModTooNewText(need, have string) string {
	line := strings.TrimPrefix(have, "go")
	if ma, mi, ok := goMajorMinor(have); ok {
		line = fmt.Sprintf("%d.%d", ma, mi)
	}
	return fmt.Sprintf(goMsgModTooNew, strings.TrimPrefix(need, "go"), strings.TrimPrefix(have, "go"), line)
}

func goTooNew(output string) (need, have string, ok bool) {
	m := goTooNewRe.FindStringSubmatch(output)
	if m == nil {
		return "", "", false
	}
	return "go" + m[1], "go" + m[2], true
}

// goVersionOf 問一支 go 的版本（go env GOVERSION）。GOTOOLCHAIN=local：只問它自己，不讓它去下載別的版本；不帶外面的 GOROOT。
func goVersionOf(goBin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), goProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "env", "GOVERSION")
	cmd.Dir = os.TempDir()
	cmd.Env = goEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "go") {
		return "", fmt.Errorf("unexpected GOVERSION %q", strings.TrimSpace(string(out)))
	}
	return fields[0], nil
}

// goAtLeast：v（go1.27.4、go1.25rc1 這種）≥ min（連修訂版都比，標準函式庫的 go/version）。認不得的版本算不夠。
func goAtLeast(v, min string) bool {
	return version.IsValid(v) && version.IsValid(min) && version.Compare(v, min) >= 0
}

func goMajorMinor(v string) (int, int, bool) {
	parts := strings.SplitN(strings.TrimPrefix(v, "go"), ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	digits := parts[1]
	for i, r := range digits {
		if r < '0' || r > '9' {
			digits = digits[:i]
			break
		}
	}
	minor, err := strconv.Atoi(digits)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// sizeMB 換成給人看的 MB：20MB 以上取到十位數（68231011 → 70）。
func sizeMB(n int64) int {
	mb := int((n + 500_000) / 1_000_000)
	if mb >= 20 {
		mb = (mb + 5) / 10 * 10
	}
	if mb < 1 {
		mb = 1
	}
	return mb
}

func freeSpace(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
