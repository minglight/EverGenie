package board

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 套件的公開檔案（D-29、REQ-175）：附程式的套件可以在暫存資料夾的 runtime/packages/<id>/public/ 放圖片與影片，
// 核心只用唯讀的 GET /pkg-files/<id>/<路徑> 送出。核心不寫、不建、不看內容，也不問套件行程；
// 「什麼可以放進公開資料夾」由套件的程式決定（沒放進去的檔案，網路上就拿不到）。

const (
	pkgFilesPrefix = "/pkg-files/"
	maxPkgSegments = 6
	maxPkgPathLen  = 200
)

var pkgIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// 頭像要找的靜態圖副檔名（順序＝優先順序）。
var wornImageExts = []string{".png", ".jpg", ".jpeg", ".webp"}

// safeRelPath 檢查並拆開公開資料夾裡的相對路徑：每段非空、不以 . 開頭、不含反斜線／NUL／..，最多 6 段、全長 ≤ 200。
func safeRelPath(rel string) ([]string, bool) {
	if rel == "" || len(rel) > maxPkgPathLen || strings.ContainsAny(rel, "\\\x00") {
		return nil, false
	}
	parts := strings.Split(rel, "/")
	if len(parts) > maxPkgSegments {
		return nil, false
	}
	for _, p := range parts {
		if p == "" || strings.HasPrefix(p, ".") || strings.Contains(p, "..") {
			return nil, false
		}
	}
	return parts, true
}

// programPackage：這個 id 是啟用中（看板看得到）而且附程式的套件。
func (a *Assets) programPackage(id string) bool {
	if !pkgIDPattern.MatchString(id) {
		return false
	}
	for _, p := range a.VisiblePackages() {
		if p.ID == id {
			return p.Kind == sdk.PackageKindProgram
		}
	}
	return false
}

// publicFile 回套件公開資料夾裡 rel 對應的一般檔案（已解開符號連結的實際路徑）；任何一步不合就回 false。
// 公開資料夾本身是符號連結、途中有符號連結、解開後跑出公開資料夾，都算沒有。
func (a *Assets) publicFile(pkgID, rel string) (string, bool) {
	home := a.homeDir()
	parts, ok := safeRelPath(rel)
	if !ok || home == "" || !a.programPackage(pkgID) {
		return "", false
	}
	pub := platform.RuntimeDir(home, "packages", pkgID, "public")
	if info, err := os.Lstat(pub); err != nil || !info.IsDir() {
		return "", false
	}
	realPub, err := filepath.EvalSymlinks(pub)
	if err != nil {
		return "", false
	}
	want := filepath.Join(append([]string{realPub}, parts...)...)
	got, err := filepath.EvalSymlinks(want)
	if err != nil || got != want || !strings.HasPrefix(got, realPub+string(filepath.Separator)) {
		return "", false
	}
	info, err := os.Lstat(got)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return got, true
}

func (a *Assets) ServePkgFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "不支援這個方法", http.StatusMethodNotAllowed)
		return
	}
	// %2f／%5c 不當成路徑分隔：解碼後會多出一層目錄，一律不收。
	if esc := strings.ToLower(r.URL.EscapedPath()); strings.Contains(esc, "%2f") || strings.Contains(esc, "%5c") {
		http.NotFound(w, r)
		return
	}
	id, rel, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, pkgFilesPrefix), "/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(rel))
	contentType, known := imageTypes[ext]
	if !known {
		contentType, known = videoTypes[ext]
	}
	if !known {
		http.NotFound(w, r)
		return
	}
	file, ok := a.publicFile(id, rel)
	if !ok {
		http.NotFound(w, r)
		return
	}
	serveRegular(w, r, file, contentType, "inline", "no-cache")
}

// avatarSource 是宣告 contributes.avatars 的套件與它的子資料夾（D-29）：啟用中、附程式的套件裡 id 排最前面的那個；沒有就回空。
func (a *Assets) avatarSource() (pkgID, dir string) {
	for _, p := range a.VisiblePackages() {
		if p.Avatars != "" && p.Kind == sdk.PackageKindProgram && (pkgID == "" || p.ID < pkgID) {
			pkgID, dir = p.ID, p.Avatars
		}
	}
	return pkgID, dir
}

// wornImage 是套件提供的「現在的頭像」靜態圖：<子資料夾>/<人設>.{png,jpg,jpeg,webp} 有一個。
func (a *Assets) wornImage(slug string) (file, contentType string, ok bool) {
	pkgID, dir := a.avatarSource()
	if pkgID == "" {
		return "", "", false
	}
	for _, ext := range wornImageExts {
		if f, found := a.publicFile(pkgID, dir+"/"+slug+ext); found {
			return f, imageTypes[ext], true
		}
	}
	return "", "", false
}

// maxClips：同一張的動態最多幾支（跟 closet 的 pub.MaxVideos 一致，DEV-125）。
const maxClips = 6

// wornVideo 是同一張的第 k 支動態（k=1：<子資料夾>/<人設>.mp4；k ≥ 2：<子資料夾>/<人設>+k.mp4）。
func (a *Assets) wornVideo(slug string, k int) (string, bool) {
	pkgID, dir := a.avatarSource()
	if pkgID == "" || k < 1 || k > maxClips {
		return "", false
	}
	suffix := ".mp4"
	if k > 1 {
		suffix = "+" + strconv.Itoa(k) + ".mp4"
	}
	return a.publicFile(pkgID, dir+"/"+slug+suffix)
}
