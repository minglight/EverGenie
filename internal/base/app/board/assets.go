package board

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// PackageCatalog 是看板讀套件用的最小唯讀介面（由 packages 業務模組提供，registry 注入）。
type PackageCatalog interface {
	Packages() []sdk.Package             // 全部有效的套件（含 rejected），同 PackageAPI.List 的內容
	PackageDir(id string) (string, bool) // 有效套件的資料夾絕對路徑
}

// Assets 是套件的前端資源：頁面編譯（esbuild）與快取、公開檔案、套件提供的頭像。
// 看板外殼（Module）用它送檔；選單（menu 業務模組）與名冊（agents 業務模組）經各自的小 interface 用它算頁面網址與動態頭像網址。
// 它不聽任何連接埠、沒有 HTTP 路由，所以能先於看板建好，交給業務模組。
type Assets struct {
	pk PackageCatalog

	mu   sync.Mutex
	home string
	log  *slog.Logger

	bundleMu sync.Mutex
	bundles  map[string]*bundleEntry // "<套件>/<頁面>" → 上次編譯的結果
	builds   int                     // esbuild 實際跑過幾次（測試用）
}

// NewAssets 不做任何 I/O（MOD-N4）。pk 可以是 nil（沒有套件）。
func NewAssets(pk PackageCatalog) *Assets {
	return &Assets{pk: pk, log: slog.New(slog.NewTextHandler(io.Discard, nil)), bundles: map[string]*bundleEntry{}}
}

// SetHome 設定家的資料夾（編譯產物、公開檔案、人設預設頭像都在家裡）；啟動前呼叫。
func (a *Assets) SetHome(dir string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.home = dir
}

// SetLogger 換掉 log（看板啟動時交進來）；預設丟掉。
func (a *Assets) SetLogger(l *slog.Logger) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log = l
}

func (a *Assets) homeDir() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.home
}

func (a *Assets) logger() *slog.Logger {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.log
}

// Builds 是 esbuild 實際跑過幾次（測試用，確認快取有效）。
func (a *Assets) Builds() int {
	a.bundleMu.Lock()
	defer a.bundleMu.Unlock()
	return a.builds
}

// VisiblePackages 是看板看得到的套件：啟用而且沒被拒（附程式的套件行程掛了，頁面照樣顯示）。
func (a *Assets) VisiblePackages() []sdk.Package {
	if a.pk == nil {
		return nil
	}
	var out []sdk.Package
	for _, p := range a.pk.Packages() {
		if p.Enabled && p.State != sdk.PackageStateRejected {
			out = append(out, p)
		}
	}
	return out
}

// Avatar 回秘書頭像要送的檔案與類型（slug 是現任人設，ext 是 .png 或 .mp4）。
// 有套件提供這位的頭像（contributes.avatars 的子資料夾有 <人設>.*）：照它給的（D-29）；.mp4 只回它的影片，沒有就回 false，不退回預設影片（免得圖是這套、影片是別套）。
// 沒有套件管就是人設自己的預設圖／影片（defaultAvatar；檔案不在時回的路徑照舊會 404）。
func (a *Assets) Avatar(slug, ext string) (file, contentType string, ok bool) {
	return a.AvatarClip(slug, ext, 1)
}

// HasImage：這個人設現在有沒有頭像圖——照 /avatars/… 送圖的同一條鏈（AvatarClip 的 .png）解析到的是存在的一般檔案才算。
// 名冊與角色庫只在有圖時給 avatar_url（DEV-157）：看板對沒圖的角色直接畫名字，不去抓一張一定 404 的圖。
func (a *Assets) HasImage(slug string) bool {
	if a.homeDir() == "" {
		return false
	}
	file, _, ok := a.AvatarClip(slug, ".png", 1)
	if !ok || file == "" {
		return false
	}
	info, err := os.Lstat(file) // 跟 serveRegular 同一條：符號連結不算有圖（送圖那邊也會 404）
	return err == nil && info.Mode().IsRegular()
}

// AvatarClip：跟 Avatar 一樣，clip 是第幾支動態（只對 .mp4 有意思，DEV-125）。
// 第 2 支起只有衣櫃（contributes.avatars）管這位時才有（<人設>+k.mp4）；沒人管就只有第 1 支。
func (a *Assets) AvatarClip(slug, ext string, clip int) (file, contentType string, ok bool) {
	if !personaSlug.MatchString(slug) || strings.Contains(slug, "..") {
		return "", "", false
	}
	if wornFile, wornType, managed := a.wornImage(slug); managed {
		if ext == ".mp4" {
			v, found := a.wornVideo(slug, clip)
			return v, avatarTypes[".mp4"], found
		}
		return wornFile, wornType, true
	}
	if clip > 1 {
		return "", "", false
	}
	return a.defaultAvatar(slug, ext), avatarTypes[ext], true
}

// VideoURL：有套件提供這位的頭像（contributes.avatars）時只看它的影片（D-29）；否則人設的預設影片 personas/avatars/<persona>.mp4 是一般檔案才回網址
// （符號連結不算、人設名不合規則不填）；套件角色可以自己帶動態頭像。沒有動態頭像回空字串。
func (a *Assets) VideoURL(agentID, slug string) string {
	if urls := a.VideoURLs(agentID, slug); len(urls) > 0 {
		return urls[0]
	}
	return ""
}

// VideoURLs：這位現在所有的動態頭像網址（DEV-125，同一張可以有最多 6 支，前端隨機挑一支播）。
// 衣櫃管這位：worn 有幾支就幾個（/avatars/<id>.mp4、/avatars/<id>+2.mp4…，從第 1 支起連續，缺一支就停）；
// 沒人管：最多一個（同 VideoURL 原本的規則）。沒有動態頭像回 nil。
func (a *Assets) VideoURLs(agentID, slug string) []string {
	if !personaSlug.MatchString(slug) || strings.Contains(slug, "..") || a.homeDir() == "" {
		return nil
	}
	if _, _, managed := a.wornImage(slug); managed {
		// 套件管這位：只有它的影片才算，不退回預設影片（D-29）。
		var out []string
		for k := 1; k <= maxClips; k++ {
			if _, ok := a.wornVideo(slug, k); !ok {
				break
			}
			out = append(out, ClipURL("/avatars/"+agentID, k))
		}
		return out
	}
	url := "/avatars/" + agentID + ".mp4"
	info, err := os.Lstat(platform.OfficialPersonasDir(a.homeDir(), "avatars", slug+".mp4"))
	if err != nil || !info.Mode().IsRegular() {
		if _, ok := a.packageAvatar(slug, ".mp4"); ok { // 套件角色自己帶的動態頭像
			return []string{url}
		}
		return nil
	}
	return []string{url}
}

// ClipURL：第 k 支動態的網址（base 是不含副檔名的網址，例 /avatars/code）；k=1 不帶 +1。
func ClipURL(base string, k int) string {
	if k <= 1 {
		return base + ".mp4"
	}
	return base + "+" + strconv.Itoa(k) + ".mp4"
}

// defaultAvatar 是人設自己的預設圖／影片（衣櫃沒管這位時）：官方 personas/avatars/<人設><副檔名> 優先；
// 沒有而且人設是套件角色（<套件id>.<slug>）→ 套件裡的 personas/avatars/<slug><副檔名>；都沒有回官方那條路徑（照舊會 404）。
func (a *Assets) defaultAvatar(slug, ext string) string {
	official := platform.OfficialPersonasDir(a.homeDir(), "avatars", slug+ext)
	if info, err := os.Lstat(official); err == nil && info.Mode().IsRegular() {
		return official
	}
	if f, ok := a.packageAvatar(slug, ext); ok {
		return f
	}
	return official
}

// packageAvatar 找套件角色的頭像：套件資料夾（只限有效、沒被拒的套件）裡的 personas/avatars/<slug><副檔名>，
// 一般檔案才算，符號連結跑出套件資料夾外不算。
func (a *Assets) packageAvatar(code, ext string) (string, bool) {
	pkgID, slug, ok := strings.Cut(code, ".")
	if !ok || a.pk == nil || !agentID.MatchString(pkgID) || slug == "" || strings.ContainsAny(slug, "./\\") {
		return "", false
	}
	dir, ok := a.pk.PackageDir(pkgID)
	if !ok {
		return "", false
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	file, err := filepath.EvalSymlinks(filepath.Join(root, "personas", "avatars", slug+ext))
	if err != nil || !strings.HasPrefix(file, root+string(filepath.Separator)) {
		return "", false
	}
	if info, err := os.Lstat(file); err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return file, true
}
