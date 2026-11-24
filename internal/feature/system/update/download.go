package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 下載版的安裝與升級（DEV-155 S2）。安裝資料夾（下面叫 <app>）長這樣：
//
//	<app>/versions/<版本>/   一版一個資料夾：bin/（核心與官方套件的執行檔）、fixed/、packages/official/、sdk/、ai_docs/（跟著程式的說明文件，platform PLT-28）、VERSION（一行版本字串）、LICENSE、LICENSES/
//	<app>/current           符號連結，指到正在用的那一版（相對路徑 versions/<版本>）
//	<app>/fixed/            發行檔 fixed/ 裡的檔（要一直待在同一個路徑的，例如官方套件附帶的程式）：換版前放過來，內容沒變就不動（UPD-15）
//
// 服務跑 <app>/current/bin/evergenie（launchd 的設定或 EVERGENIE_SERVICE_MODE=process 都一樣），所以換版只要換 current，不動服務設定。
// 新版從版本清單（manifest.json，[update] source；格式見 ai_docs/runbooks/release.md 的「發行檔」）下載，不靠 git、不編譯；
// 換 current 用「先建暫時的連結再 rename」，任何時候 current 都指到一個完整的版本。舊版資料夾留到下一次升級成功（或有人執行 upgrade --accept）。
// 給主人看的訊息一律白話、帶一個下一步（plain.go）；英文的細節（網址、錯誤原文）只寫進 runtime/logs/upgrade.log（detail）。

// InstallDownload 是 history.jsonl 的 install 欄位：下載版（versions/＋current，升級＝下載、換 current）做的升級。
// 原始碼版（git repo，升級＝checkout＋make build）不寫這個欄位（舊紀錄也沒有）。
const InstallDownload = "download"

const (
	versionsDirName = platform.DownloadVersionsDir
	currentLinkName = platform.DownloadCurrentLink
	versionFileName = platform.DownloadVersionFile
	fixedDirName    = platform.DownloadFixedDir
	manifestMaxSize = 1 << 20
	assetMaxSize    = 1 << 30 // 一個包最大 1 GiB（現在的包大約 30 MB）；清單寫更大就當清單不對，不下載
	unpackMaxSize   = 2 << 30 // 解開後一般檔加起來最多 2 GiB（現在大約 100 MB）；壓縮得特別小、解開特別大的包不解完就拒絕
)

// 下載版的白話訊息（不夾英文；細節進 upgrade.log）。
const (
	msgNoSource   = "這台還沒設定從哪裡下載新版，沒辦法檢查新版"
	msgManifest   = "讀不到版本清單（可能是網路不通，或下載的地方設錯了）"
	msgNoAsset    = "新版沒有這台電腦的晶片能用的版本"
	msgFetch      = "下載不到新版的檔案（可能是網路不通，或下載的地方暫時有問題）"
	msgCut        = "新版下載到一半斷掉了"
	msgBroken     = "下載的檔案不完整或被改過（檢查碼對不上）"
	msgBadPackage = "下載的新版內容不對（裡面缺東西，或版本號對不上）"
	msgTooBig     = "下載的新版解開後大得不正常，可能不是正確的程式包"
	// 指定的版本或現在的版本號看不懂（例如現在跑的是開發中的版本）：比不出新舊，不換過去。原始碼版也用。
	msgVersionUnknown = "看不出要換的版本跟現在的版本哪個比較新（版本號的格式看不懂）"
	msgDisk           = "沒辦法把新版放進程式資料夾（可能是硬碟滿了或沒有權限）"
	msgUntouched      = "，沒有升級；舊版照常在跑"
	msgNoCurrent      = "找不到現在用的是哪一版（程式資料夾的樣子不對）"
)

// ReleaseManifest 是版本清單 manifest.json（scripts/dist.sh 產生）。
type ReleaseManifest struct {
	Schema     int                     `json:"schema"`
	Version    string                  `json:"version"`
	Channel    string                  `json:"channel"`
	ReleasedAt string                  `json:"released_at"`
	Notes      string                  `json:"notes"`
	Assets     map[string]ReleaseAsset `json:"assets"`
}

// ReleaseAsset 是一種晶片的包：url 可以是相對於版本清單的路徑。
type ReleaseAsset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// plainErr：Error() 是白話，detail 是英文的細節（只進 upgrade.log）。
type plainErr struct{ plain, detail string }

func (e *plainErr) Error() string { return e.plain }

func plainf(plain, format string, a ...any) error {
	return &plainErr{plain: plain, detail: fmt.Sprintf(format, a...)}
}

// IsDownloadInstall：dir 是下載版的安裝資料夾（判斷在 platform.IsDownloadInstall，指標檔也用同一套）。
func IsDownloadInstall(dir string) bool { return platform.IsDownloadInstall(dir) }

// currentVersionDir 回 current 指到的版本資料夾名稱（versions/ 底下那一層）；不是指到 versions/ 底下就回空字串。
func currentVersionDir(app string) string { return platform.DownloadCurrentVersion(app) }

// FindInstall 找升級要動的地方：原始碼版（FindCore：有 .git 與 Makefile 的 repo）或下載版的安裝資料夾（IsDownloadInstall）。
// [update] core 寫了就只看它（兩種都可以）；沒寫就照執行檔的位置找（下載版：解開符號連結後是 <app>/versions/<版本>/bin/evergenie）。
// 找不到回空字串。只想知道「EverGenie 的原始碼 repo 在哪」（給秘書開分支用）的地方照舊用 FindCore。
func FindInstall(h *platform.Home, exe string) string {
	if c := FindCore(h, exe); c != "" {
		return c
	}
	if h != nil && h.Update.Core != "" {
		if IsDownloadInstall(h.Update.Core) {
			return h.Update.Core
		}
		return ""
	}
	return platform.DownloadInstallOf(exe)
}

// platformKey 是這台在版本清單 assets 裡的鍵（darwin-arm64、darwin-amd64）。
func (u *Upgrader) platformKey() string {
	if u.Platform != "" {
		return u.Platform
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

// detail 把英文的細節附加到 runtime/logs/upgrade.log（主人看不到的那一份）；寫不進去就算了。
func (u *Upgrader) detail(format string, a ...any) {
	dir := platform.RuntimeDir(u.Home, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "upgrade.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s [detail] %s\n", u.now().Format(time.RFC3339), fmt.Sprintf(format, a...))
}

// explain 把「做什麼沒成功」加上原因（原始碼版與下載版一樣，UPD-14）：白話的錯誤（plainErr、ErrUpgrade）直接接上；
// 外來的錯誤只接不夾英文的中文那段（plainPart）；其他的（英文的錯誤原文）只寫進 upgrade.log，給主人看「細節記在升級日誌」。
func (u *Upgrader) explain(what string, err error) string {
	if p := u.plainOf(err); p != "" {
		return what + "：" + p
	}
	return what + "（細節記在升級日誌）"
}

// plainOf 回錯誤的白話（plainErr 的英文細節寫進 upgrade.log）。外來的錯誤（例如重開服務的錯誤「載入服務失敗：<launchctl 的英文>」）
// 只留不夾英文的中文那段（plainPart），整段寫進 upgrade.log；沒有白話可說的（英文原文）整段寫進 upgrade.log、回空字串。
func (u *Upgrader) plainOf(err error) string {
	var pe *plainErr
	if errors.As(err, &pe) {
		if pe.detail != "" {
			u.detail("%s: %s", pe.plain, pe.detail)
		}
		return pe.plain
	}
	var ue *ErrUpgrade
	if errors.As(err, &ue) {
		return err.Error()
	}
	p := plainPart(err.Error())
	if p != strings.TrimSpace(err.Error()) {
		u.detail("%v", err)
	}
	return p
}

// reasonOf 是錯誤的白話；沒有白話可說（英文原文，已寫進 upgrade.log）就說「細節記在升級日誌」。
func (u *Upgrader) reasonOf(err error) string {
	if p := u.plainOf(err); p != "" {
		return p
	}
	return "細節記在升級日誌"
}

// plainTail：新版印出來的最後一行，是我們自己的白話（有中文、不夾英文；「中文：英文」只取中文那段，plainPart）才拿來當原因；
// 其他（英文的錯誤原文）不給主人看。輸出整段寫進 upgrade.log（照搬整行時不用）。
func (u *Upgrader) plainTail(what, out string) string {
	line := lastLine(out)
	p := plainPart(line)
	if p != "" && p == line && strings.TrimSpace(out) == line {
		return p
	}
	if strings.TrimSpace(out) != "" {
		u.detail("%s: %s", what, strings.TrimSpace(out))
	}
	if p != "" {
		return p
	}
	return "細節記在升級日誌"
}

// ---- 版本清單 ------------------------------------------------------------------------------------------

func (u *Upgrader) source() (*url.URL, error) {
	if u.Source == "" {
		return nil, &plainErr{plain: msgNoSource, detail: "home.toml [update] source is empty and no default source is built in"}
	}
	src, err := url.Parse(u.Source)
	if err != nil || !platform.DownloadURLAllowed(u.Source) { // https、本機的 http、file（PLT-25）
		return nil, plainf(msgManifest, "source %q is not allowed (https, local http or file only): %v", logURL(src), err)
	}
	return src, nil
}

// logURL 是寫進 upgrade.log 的網址：拿掉帳號密碼、查詢字串與 # 後面（可能帶存取用的代碼）。
func logURL(ref *url.URL) string {
	if ref == nil {
		return ""
	}
	c := *ref
	c.User, c.RawQuery, c.ForceQuery, c.Fragment, c.RawFragment = nil, "", false, "", ""
	return c.String()
}

func (u *Upgrader) downloadTimeout() time.Duration {
	if u.DownloadTimeout > 0 {
		return u.DownloadTimeout
	}
	return 10 * time.Minute
}

// open 打開一個網址（下載來源的規則 PLT-25：https、本機的 http、file；轉址也照規則），回內容、伺服器說的長度（不知道是 -1）。
// 連線與讀檔是 platform.OpenDownload（跟角色包、套件下載 Go 同一支）；失敗回 plainFail（細節進 upgrade.log）。
func (u *Upgrader) open(ctx context.Context, ref *url.URL, timeout time.Duration, plainFail string) (io.ReadCloser, int64, error) {
	body, n, err := platform.OpenDownload(ctx, u.HTTP, ref.String(), timeout)
	if err != nil {
		verb := "get"
		if ref.Scheme == "file" {
			verb = "open"
		}
		return nil, 0, plainf(plainFail, "%s %s: %v", verb, logURL(ref), err)
	}
	return body, n, nil
}

// readManifest 讀版本清單並檢查格式；讀不到、格式不對都回 msgManifest（細節進 upgrade.log）。
func (u *Upgrader) readManifest(ctx context.Context) (*ReleaseManifest, error) {
	src, err := u.source()
	if err != nil {
		return nil, err
	}
	body, _, err := u.open(ctx, src, 30*time.Second, msgManifest)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, manifestMaxSize+1))
	if err != nil {
		return nil, plainf(msgManifest, "read %s: %v", logURL(src), err)
	}
	if len(data) > manifestMaxSize {
		return nil, plainf(msgManifest, "%s is larger than %d bytes", logURL(src), manifestMaxSize)
	}
	var m ReleaseManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, plainf(msgManifest, "parse %s: %v", logURL(src), err)
	}
	if m.Schema != 1 {
		return nil, plainf(msgManifest, "%s: unsupported schema %d", logURL(src), m.Schema)
	}
	if _, ok := platform.ParseVersion(m.Version); !ok || !safeVersionName(m.Version) {
		return nil, plainf(msgManifest, "%s: bad version %q", logURL(src), m.Version)
	}
	switch m.Channel {
	case "", platform.UpdateChannelRelease, platform.UpdateChannelSafe:
	default:
		return nil, plainf(msgManifest, "%s: bad channel %q", logURL(src), m.Channel)
	}
	return &m, nil
}

// safeVersionName：版本字串要能直接當資料夾名稱（不能有 /、不能是 . 或 ..）。
func safeVersionName(v string) bool {
	if v == "" || v == "." || v == ".." || strings.HasPrefix(v, ".") {
		return false
	}
	for _, r := range v {
		if !(r == '.' || r == '-' || r == '+' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

// judgeManifest 依頻道、現在的版本、跳過清單（與 needAsset 時這台的晶片），決定清單上的版本算不算「有新版」（填 res 的 Target 或 Detail）。
func (u *Upgrader) judgeManifest(m *ReleaseManifest, res *UpgradeCheck, needAsset bool) {
	cur, curOK := platform.ParseVersion(u.From)
	tv, _ := platform.ParseVersion(m.Version)
	switch {
	case !curOK:
		res.Detail = "這是開發中的版本，不會自動升級"
	case m.Channel == platform.UpdateChannelSafe && u.Channel != platform.UpdateChannelSafe:
		res.Detail = "最新的 " + m.Version + " 是測試過的版本，你的更新頻道只升正式版"
	case tv.Compare(cur) <= 0:
		res.Detail = "已經是最新版"
	case contains(res.Skipped, m.Version):
		res.Detail = m.Version + " 這版你跳過了"
	default:
		if _, ok := m.Assets[u.platformKey()]; needAsset && !ok {
			res.Detail = msgNoAsset + "（" + m.Version + "）"
			u.detail("manifest %s has no asset for %s", m.Version, u.platformKey())
			return
		}
		res.Target, res.Tag, res.Notes = m.Version, m.Version, m.Notes
	}
}

// checkDownload 是下載版的 Check：讀版本清單。沒設定來源寫進 Detail；讀不到回錯誤（不蓋掉上一次檢查的結果）。
func (u *Upgrader) checkDownload(ctx context.Context, res *UpgradeCheck, finish func() (*UpgradeCheck, error)) (*UpgradeCheck, error) {
	m, err := u.readManifest(ctx)
	if err != nil {
		var pe *plainErr
		if errors.As(err, &pe) && pe.plain == msgNoSource {
			res.Detail = msgNoSource + "（" + nextSource + "）"
			return finish()
		}
		return res, platform.Unavailable(u.reasonOf(err))
	}
	u.judgeManifest(m, res, true)
	return finish()
}

// ---- 下載與解開 ----------------------------------------------------------------------------------------

// fetchVersion 下載 asset、比對大小與檢查碼、解開到 versions/<version>/。失敗時什麼都不留（暫存檔都刪掉），回白話錯誤。
func (u *Upgrader) fetchVersion(ctx context.Context, m *ReleaseManifest, asset ReleaseAsset, version string) (string, error) {
	src, err := u.source()
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(asset.URL)
	if err != nil || asset.URL == "" {
		return "", plainf(msgManifest, "bad asset url: %v", err)
	}
	ref = src.ResolveReference(ref)
	// 包的網址也照下載來源的規則（PLT-25）；清單是 https 的，包也要是 https（跟 scripts/install.sh 一樣）
	if !platform.DownloadURLAllowed(ref.String()) || (src.Scheme == "https" && ref.Scheme != "https") {
		return "", plainf(msgManifest, "asset url %s is not allowed (manifest %s)", logURL(ref), logURL(src))
	}
	want := strings.ToLower(strings.TrimSpace(asset.SHA256))
	if len(want) != 64 || asset.Size <= 0 || asset.Size > assetMaxSize {
		return "", plainf(msgManifest, "asset %s: bad sha256 %q or size %d (max %d)", logURL(ref), asset.SHA256, asset.Size, int64(assetMaxSize))
	}
	versions := filepath.Join(u.Core, versionsDirName)
	if err := os.MkdirAll(versions, 0o755); err != nil {
		return "", plainf(msgDisk, "mkdir %s: %v", versions, err)
	}
	tmp, err := os.CreateTemp(versions, ".download-*")
	if err != nil {
		return "", plainf(msgDisk, "create temp in %s: %v", versions, err)
	}
	defer os.Remove(tmp.Name())
	body, length, err := u.open(ctx, ref, u.downloadTimeout(), msgFetch)
	if err != nil {
		tmp.Close()
		return "", err
	}
	_, copyErr := platform.CopyVerified(tmp, body, asset.Size, want)
	body.Close()
	closeErr := tmp.Close()
	var ce *platform.CopyError
	errors.As(copyErr, &ce)
	switch {
	case ce != nil && ce.Fault == platform.CopyWrite:
		return "", plainf(msgDisk, "write %s: %v", tmp.Name(), copyErr)
	case closeErr != nil:
		return "", plainf(msgDisk, "close %s: %v", tmp.Name(), closeErr)
	case ce != nil && ce.Fault == platform.CopyCut:
		return "", plainf(msgCut, "download %s: %v (server said %d)", logURL(ref), copyErr, length)
	case copyErr != nil:
		return "", plainf(msgBroken, "download %s: %v (manifest)", logURL(ref), copyErr)
	}
	unpack, err := os.MkdirTemp(versions, ".unpack-*")
	if err != nil {
		return "", plainf(msgDisk, "mkdir temp in %s: %v", versions, err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(unpack)
		}
	}()
	if err := extractRelease(tmp.Name(), unpack); err != nil {
		return "", err
	}
	if data, err := os.ReadFile(filepath.Join(unpack, versionFileName)); err != nil || strings.TrimSpace(string(data)) != version {
		return "", plainf(msgBadPackage, "VERSION in the package is %q (%v), want %q", strings.TrimSpace(string(data)), err, version)
	}
	if info, err := os.Stat(filepath.Join(unpack, "bin", platform.CoreExe)); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return "", plainf(msgBadPackage, "bin/%s missing or not executable in the package (%v)", platform.CoreExe, err)
	}
	dest := filepath.Join(versions, version)
	if err := os.RemoveAll(dest); err != nil { // 上一次沒成功留下來的同一版：換成這次剛驗過的
		return "", plainf(msgDisk, "remove old %s: %v", dest, err)
	}
	if err := os.Rename(unpack, dest); err != nil {
		return "", plainf(msgDisk, "rename %s -> %s: %v", unpack, dest, err)
	}
	keep = true
	return dest, nil
}

// syncFixed 把一版發行檔 fixed/ 裡的檔放到 <app>/fixed/（UPD-15；升級用新版、退回用舊版）。換了哪些、放不了的原因只寫進 upgrade.log：
// 放不了不擋升級。放不了的那個檔就沿用 <app>/fixed/ 裡原本的（上一版放的）；那裡原本就沒有，新版的引擎才改用它自己版本資料夾裡那一份。
func (u *Upgrader) syncFixed(verDir string) {
	changed, err := syncFixedFiles(filepath.Join(verDir, fixedDirName), filepath.Join(u.Core, fixedDirName))
	if len(changed) > 0 {
		u.detail("fixed files replaced (content changed): %s", strings.Join(changed, ", "))
	}
	if err != nil {
		u.detail("fixed files: %v", err)
	}
}

// switchCurrentFixed 讓 current 換到 versions/<name>，換之前先把固定位置對成那一版（UPD-15；放不了不擋，原因只進 upgrade.log）。
// 換不過去就把固定位置對回 current 還指著的那一版：不留下「程式是這一版、固定位置是另一版」，「沒有動任何東西」也才是真的。
// 名稱不合法的（rescue.json 這類讀進來的）不放固定位置，交給 switchCurrent 回錯。
func (u *Upgrader) switchCurrentFixed(name string) error {
	if safeVersionName(name) {
		u.syncFixed(filepath.Join(u.Core, versionsDirName, name))
	}
	if err := switchCurrent(u.Core, name); err != nil {
		if cur := currentVersionDir(u.Core); safeVersionName(name) && cur != "" && cur != name {
			u.syncFixed(filepath.Join(u.Core, versionsDirName, cur))
		}
		return err
	}
	return nil
}

// syncFixedFiles 把 src 資料夾裡的一般檔放到 dst（UPD-15；scripts/install.sh 第一次安裝時做一樣的事）：
// 內容（sha256）跟 dst 裡同名的一樣就完全不動（同一個檔、修改時間不變；只有權限不同才改權限）；不一樣或還沒有才換：
// 先寫到 dst 裡的暫存檔再 rename 蓋過去，正在執行舊檔的程式不受影響，任何時候 dst 裡的檔都是完整的。
// src 不在（舊的包沒有 fixed/）什麼都不做。不刪 dst 裡 src 沒有的檔，只收上一次被中斷留下的暫存檔（.<名>.new-*；升級有鎖，不會是別人正在寫的）。
// 回傳換過的檔名；有放不了的回第一個錯誤，其他照放。
func syncFixedFiles(src, dst string) ([]string, error) {
	entries, err := os.ReadDir(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, err
	}
	if old, err := os.ReadDir(dst); err == nil {
		for _, e := range old {
			if n := e.Name(); strings.HasPrefix(n, ".") && strings.Contains(n, ".new-") && e.Type().IsRegular() {
				_ = os.Remove(filepath.Join(dst, n))
			}
		}
	}
	var changed []string
	var first error
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		did, err := syncFixedFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
		if did {
			changed = append(changed, e.Name())
		}
		if err != nil && first == nil {
			first = err
		}
	}
	return changed, first
}

func syncFixedFile(src, dst string) (bool, error) {
	info, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	want, err := fileSHA256(src)
	if err != nil {
		return false, err
	}
	perm := info.Mode().Perm()
	if old, err := os.Lstat(dst); err == nil && old.Mode().IsRegular() {
		if got, err := fileSHA256(dst); err == nil && got == want {
			if old.Mode().Perm() != perm {
				return false, os.Chmod(dst, perm)
			}
			return false, nil
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".new-*")
	if err != nil {
		return false, err
	}
	done := false
	defer func() {
		if !done {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return false, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return false, err
	}
	done = true
	return true, nil
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractRelease 解開發行檔：包裡只有一個最上層資料夾（<名稱>-<版本>/），去掉它放進 dest。
// 只收資料夾與一般檔，其他一律拒絕（platform.ExtractTarGz，PLT-27；scripts/dist.sh 做的包裡本來就沒有連結）；
// 一般檔加起來超過 unpackMaxSize 就不解完、當包不對（壓縮得特別小、解開特別大的包會塞滿硬碟）。
func extractRelease(archive, dest string) error {
	err := platform.ExtractTarGz(archive, dest, platform.UnpackOptions{MaxBytes: unpackMaxSize})
	var ue *platform.UnpackError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ue) && ue.TooBig:
		return plainf(msgTooBig, "%v", err)
	case errors.As(err, &ue) && ue.Bad:
		return plainf(msgBadPackage, "%v", err)
	default:
		return plainf(msgDisk, "unpack %s: %v", archive, err)
	}
}

// ---- 換版 ----------------------------------------------------------------------------------------------

// switchCurrent 把 <app>/current 原子地換成指到 versions/<name>：先在旁邊建一個暫時的連結，再 rename 蓋過去
// （rename 換的是連結本身，任何時候 current 都指到一個完整的版本）。
func switchCurrent(app, name string) error {
	if !safeVersionName(name) {
		return fmt.Errorf("bad version folder %q", name)
	}
	if _, err := os.Stat(filepath.Join(app, versionsDirName, name, "bin", platform.CoreExe)); err != nil {
		return err
	}
	tmp := filepath.Join(app, fmt.Sprintf(".%s-%d-%d", currentLinkName, os.Getpid(), time.Now().UnixNano()))
	if err := os.Symlink(filepath.Join(versionsDirName, name), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(app, currentLinkName)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// findVersionDir 找某個版本的資料夾名稱：versions/<version> 的 VERSION 對得上就是它；不然找 VERSION 寫著這一版的資料夾。找不到回空字串。
func findVersionDir(app, version string) string {
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(app, versionsDirName, name, versionFileName))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
	ok := func(name string) bool {
		_, err := os.Stat(filepath.Join(app, versionsDirName, name, "bin", platform.CoreExe))
		return err == nil && read(name) == version
	}
	if safeVersionName(version) && ok(version) {
		return version
	}
	entries, err := os.ReadDir(filepath.Join(app, versionsDirName))
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() && safeVersionName(e.Name()) && ok(e.Name()) {
			return e.Name()
		}
	}
	return ""
}

// pruneVersions 刪掉 versions/ 底下 keep 以外的資料夾（含上一次沒收乾淨的暫存）。刪不掉的寫進 upgrade.log，不算失敗。
func (u *Upgrader) pruneVersions(keep ...string) {
	dir := filepath.Join(u.Core, versionsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	kept := map[string]bool{}
	for _, k := range keep {
		if k != "" {
			kept[k] = true
		}
	}
	for _, e := range entries {
		if kept[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			u.detail("prune %s: %v", e.Name(), err)
		}
	}
}

// ---- 升級（下載版）--------------------------------------------------------------------------------------

// applyDownload 是下載版的 Apply（鎖已經拿了）：讀清單 → 下載 → 比對檢查碼 → 解開到 versions/<新版> → 備份 → 在複本上試跑資料整理 →
// 整理 → 換 current → 重開 → 檢查。下載、檢查碼、解開、備份沒過：舊版完全沒動（不用重開、不跳過這一版）。試跑沒過：舊版也沒動，
// 但這一版有問題，跳過並請秘書查。整理資料之後沒過：換回舊版（整理過的資料用備份還原）、跳過這一版。
func (u *Upgrader) applyDownload(ctx context.Context, to string) error {
	st := &UpgradeState{Kind: sdk.UpdateKindUpgrade, From: u.From, StartedAt: u.now()}
	u.setState(st, UpgradePhaseFetch, "正在讀版本清單")
	// 換版之前就停的：舊版照常在跑（outcome untouched），每一種原因帶自己的下一步（UPD-14）。
	fail := func(reason, next string) error { return u.untouched(st, reason, next) }
	oldDir := currentVersionDir(u.Core)
	if oldDir == "" {
		u.detail("%s does not point into %s", filepath.Join(u.Core, currentLinkName), filepath.Join(u.Core, versionsDirName))
		return fail(msgNoCurrent+msgUntouched, nextAsk)
	}
	m, err := u.readManifest(ctx)
	if err != nil {
		return fail(u.reasonOf(err)+msgUntouched, nextOf(err))
	}
	want := strings.TrimPrefix(strings.TrimSpace(to), "v")
	if want == "" {
		res := &UpgradeCheck{Current: u.From, Channel: u.Channel, Skipped: ReadUpgradeSkipped(u.Home), CheckedAt: u.now()}
		if res.Skipped == nil {
			res.Skipped = []string{}
		}
		// 跟「檢查新版」同一套判斷（含這台的晶片有沒有包），結果一樣寫進 check.json（看板顯示用；寫不進去不影響升級）。
		u.judgeManifest(m, res, true)
		if err := platform.WriteJSONFile(filepath.Join(UpgradeDir(u.Home), UpgradeCheckFile), res); err != nil {
			u.detail("write %s: %v", UpgradeCheckFile, err)
		}
		if res.Target == "" {
			if _, ok := m.Assets[u.platformKey()]; !ok && strings.HasPrefix(res.Detail, msgNoAsset) {
				return fail(msgNoAsset+msgUntouched, nextWait)
			}
			return fail("沒有新版可以升級："+res.Detail, nextNothing)
		}
		want = res.Target
	} else if want != m.Version {
		return fail(fmt.Sprintf("版本清單上的新版是 %s，沒有 %s%s", m.Version, want, msgUntouched), nextRecheck)
	} else if order, ok := versionOrder(want, u.From); !ok { // 比不出新舊（現在的是開發中的版本之類）：不換過去（UPD-12）
		u.detail("cannot compare target version %q with current %q", want, u.From)
		return fail(msgVersionUnknown+msgUntouched, nextAsk)
	} else if order < 0 {
		return fail(fmt.Sprintf("%s 比現在的 %s 舊，升級不會換成舊版%s", want, u.From, msgUntouched), nextDowngrade)
	}
	if want == u.From || want == oldDir {
		return fail("已經是 "+want+" 了，不用升級", nextNothing)
	}
	asset, ok := m.Assets[u.platformKey()]
	if !ok {
		u.detail("manifest %s has no asset for %s", m.Version, u.platformKey())
		return fail(msgNoAsset+msgUntouched, nextWait)
	}
	st.To = want
	u.setState(st, UpgradePhaseFetch, fmt.Sprintf("要從 %s 升級到 %s，正在下載新版", u.From, want))
	newDir, err := u.fetchVersion(ctx, m, asset, want)
	if err != nil {
		return fail(u.reasonOf(err)+msgUntouched, nextOf(err))
	}
	newExe := filepath.Join(newDir, "bin", platform.CoreExe)

	// 備份家（舊版還在跑，什麼都還沒動）
	u.setState(st, UpgradePhaseBackup, "正在備份你的資料")
	if u.Baseline != nil {
		u.Baseline(ctx)
	}
	backupPath := platform.RuntimeDir(u.Home, "backups", "upgrade-"+safeName(u.From)+"-"+safeName(want)+".tgz")
	if err := u.backupHome(backupPath); err != nil {
		return fail(u.explain("備份失敗", err)+msgUntouched, nextDisk)
	}

	var migrated, switched, synced bool
	appliedBefore, _ := AppliedMigrations(u.Home)
	rollback := func(reason string) error {
		if now, _ := AppliedMigrations(u.Home); len(newSince(appliedBefore, now)) > 0 {
			migrated = true
		}
		if synced { // 固定位置的檔放過新版的：換回舊版那份（內容一樣的不動），新版帶來的壞掉的程式才不會留給舊版用（UPD-15）
			u.syncFixed(filepath.Join(u.Core, versionsDirName, oldDir))
		}
		return u.rollbackDownload(ctx, st, oldDir, want, backupPath, switched, migrated, reason)
	}

	// 家的遷移：用新版的執行檔，先在複本上試跑（跟原始碼版同一條規則）。沒過＝這一版有問題：跟原始碼版一樣跳過這一版、
	// 交報告請秘書查；只是 current 還沒換、資料也沒動，舊版照常在跑（不重開，通知也這樣說）。
	u.setState(st, UpgradePhaseMigrate, "正在複本上試跑資料整理（你的資料還沒動）")
	if out, code := u.run(ctx, 10*time.Minute, newExe, "migrate-home", "--preview", "--home", u.Home); code != 0 && !(code == 2 && strings.Contains(out, "用法：")) {
		return rollback("資料整理在複本上試跑沒過，你的資料沒有動：" + u.plainTail("migrate-home --preview", out))
	}
	u.setState(st, UpgradePhaseMigrate, "正在整理你的資料（遷移）")
	out, code := u.run(ctx, 10*time.Minute, newExe, "migrate-home", "--apply", "--home", u.Home)
	if code != 0 {
		migrated = true
		return rollback("整理資料沒成功：" + u.plainTail("migrate-home --apply", out))
	}
	appliedAfter, _ := AppliedMigrations(u.Home)
	ranMigrations := newSince(appliedBefore, appliedAfter)
	migrated = len(ranMigrations) > 0

	// 換版、重開、檢查。換 current 之前先把新版 fixed/ 裡的檔放到固定位置（內容沒變的不動；放不了不擋升級，UPD-15）。
	u.setState(st, UpgradePhaseRestart, "正在換到新版、重開 EverGenie")
	u.syncFixed(newDir)
	synced = true
	if err := switchCurrent(u.Core, want); err != nil {
		return rollback(u.explain("換不到新版", err))
	}
	switched = true
	if err := u.restart(ctx); err != nil {
		return rollback(u.explain("重開失敗", err))
	}
	u.setState(st, UpgradePhaseVerify, "正在檢查新版有沒有正常")
	if u.Verify != nil {
		if err := u.Verify(ctx, want); err != nil {
			return rollback(u.explain("新版檢查沒過", err))
		}
	}
	if again, err := AppliedMigrations(u.Home); err == nil {
		ranMigrations = newSince(appliedBefore, again)
	}
	u.setState(st, UpgradePhaseDone, fmt.Sprintf("已升級：%s → %s", u.From, want))
	_ = AppendUpgradeHistory(u.Home, UpgradeHistory{At: u.now(), Kind: "upgrade", From: u.From, To: want, OK: true, Migrations: ranMigrations, Backup: backupPath, Install: InstallDownload})
	_ = os.Remove(filepath.Join(UpgradeDir(u.Home), UpgradeDecisionFile))
	removeRescue(u.Home)                       // 之前換不回去的紀錄：這次升級成功了，用不到了
	keepNewestUpdateBackup(u.Home, backupPath) // 更新前的備份只留這次這份（UPD-16）；換不回去的紀錄先清掉才收
	u.pruneVersions(want, oldDir)              // 更早的版本與上一次沒成功留下的都收掉；上一版留著可以退回
	if u.AfterSuccess != nil {
		u.AfterSuccess(ctx)
	}
	if u.Report != nil {
		u.Report(ctx, UpgradeReport{From: u.From, To: want, OK: true, Migrations: ranMigrations, Backup: backupPath})
	}
	u.notify("升級完成："+u.From+" → "+want, "EverGenie 已經升級到 "+want+"，不用做什麼。升級前的備份留著最新一份；用起來有問題就跟秘書說要退回 "+u.From+"，資料也會一起回到升級前。")
	return nil
}

// rollbackDownload 換回舊版：current 換回舊版資料夾（換過才換）、整理過資料就用備份還原、重開（換過或整理過才要）、再檢查一次；
// 這一版放進跳過清單。壞的那一版資料夾留著（查原因用），下一次升級成功（或執行 upgrade --accept）時收掉。
func (u *Upgrader) rollbackDownload(ctx context.Context, st *UpgradeState, oldDir, toVersion, backupPath string, switched, migrated bool, reason string) error {
	u.logf("沒成功：%s；正在換回 %s", reason, u.From)
	u.detail("install folder %s, old version folder %s", u.Core, filepath.Join(u.Core, versionsDirName, oldDir))
	back := &upgradeRescue{Install: InstallDownload, Dir: oldDir, Backup: backupPath, Restore: migrated}
	if switched {
		if err := switchCurrent(u.Core, oldDir); err != nil {
			return u.stranded(st, toVersion, reason, back)(u.explain("換不回舊版的資料夾", err))
		}
	}
	return u.finishRollback(ctx, st, toVersion, backupPath, migrated, switched || migrated, reason, back)
}
