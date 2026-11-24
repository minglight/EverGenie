package board

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\nfake-png")

// closetPkg 是只有宣告、沒有實體資料夾的套件：公開資料夾在家的 runtime/packages/<id>/public/。
func pkgOf(id string, kind sdk.PackageKind, enabled bool) sdk.Package {
	return sdk.Package{ID: id, Name: id, Tier: sdk.TierOfficial, Kind: kind, State: sdk.PackageStateRunning, Enabled: enabled}
}

// avatarPkg 是宣告 contributes.avatars = "worn" 的附程式套件（例：衣櫃）。
func avatarPkg(id string, kind sdk.PackageKind, enabled bool) sdk.Package {
	p := pkgOf(id, kind, enabled)
	p.Avatars = "worn"
	return p
}

func publicPath(e *env, id string, rel ...string) string {
	return filepath.Join(append([]string{e.home, "runtime", "packages", id, "public"}, rel...)...)
}

// TestPkgFilesServeOnlyPublicDir：公開檔案路由只服務套件的 public/（REQ-175）。
// Guards: REQ-175
func TestPkgFilesServeOnlyPublicDir(t *testing.T) {
	e := newEnv(t, "")
	e.setPackages(&fakeCatalog{pkgs: []sdk.Package{
		pkgOf("closet", sdk.PackageKindProgram, true),
		pkgOf("datapkg", sdk.PackageKindData, true),
		pkgOf("off", sdk.PackageKindProgram, false),
	}})
	write(t, publicPath(e, "closet", "o", "a", "1.png"), string(pngBytes))
	write(t, publicPath(e, "closet", "o", "a", "1.mp4"), "0123456789")
	write(t, publicPath(e, "closet", "o", "a", "notes.json"), "{}")
	write(t, publicPath(e, "closet", "o", "a", "page.html"), "<script>1</script>")
	write(t, publicPath(e, "closet", "o", "a", ".hidden.png"), string(pngBytes))
	write(t, publicPath(e, "datapkg", "x.png"), string(pngBytes))
	write(t, publicPath(e, "off", "x.png"), string(pngBytes))
	write(t, filepath.Join(e.home, "runtime", "packages", "closet", "secret.png"), "公開資料夾以外")
	write(t, filepath.Join(e.home, "outside.png"), "家裡別的檔案")
	write(t, filepath.Join(e.home, "outdir", "x.png"), "家裡別的資料夾")
	if err := os.Symlink(filepath.Join(e.home, "outside.png"), publicPath(e, "closet", "o", "a", "link.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.home, "outdir"), publicPath(e, "closet", "o", "linkdir")); err != nil {
		t.Fatal(err)
	}

	// 正常檔：200、類型、nosniff、no-cache。
	w := get(e.h, "/pkg-files/closet/o/a/1.png")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), pngBytes) ||
		w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("Content-Disposition") != "inline" {
		t.Fatalf("正常檔：%d %v", w.Code, w.Header())
	}
	// If-Modified-Since → 304。
	r := httptest.NewRequest(http.MethodGet, "/pkg-files/closet/o/a/1.png", nil)
	r.Header.Set("If-Modified-Since", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("If-Modified-Since → %d", rec.Code)
	}
	// Range → 206。
	r = httptest.NewRequest(http.MethodGet, "/pkg-files/closet/o/a/1.mp4", nil)
	r.Header.Set("Range", "bytes=0-3")
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusPartialContent || rec.Header().Get("Content-Type") != "video/mp4" || rec.Body.String() != "0123" {
		t.Fatalf("Range → %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}
	// HEAD 也行；POST 不行。
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/pkg-files/closet/o/a/1.png", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD → %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pkg-files/closet/o/a/1.png", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST → %d", rec.Code)
	}

	for _, p := range []string{
		"/pkg-files/closet/../closet/secret.png", // ..
		"/pkg-files/closet/o/../../secret.png",
		"/pkg-files/closet/%2e%2e/secret.png", // %2e%2e
		"/pkg-files/closet/o/%2e%2e/%2e%2e/secret.png",
		"/pkg-files/closet/o%2fa%2f1.png", // %2f
		"/pkg-files/closet/o%2Fa%2F1.png",
		"/pkg-files/closet/o%5ca%5c1.png",
		"/pkg-files/closet/o/a/link.png",      // 符號連結檔
		"/pkg-files/closet/o/linkdir/x.png",   // 符號連結資料夾
		"/pkg-files/closet/o/a/.hidden.png",   // . 開頭
		"/pkg-files/closet/o/a/notes.json",    // 副檔名不在白名單
		"/pkg-files/closet/o/a/page.html",     //
		"/pkg-files/closet/o/a/missing.png",   // 沒有
		"/pkg-files/closet/o/a",               // 資料夾
		"/pkg-files/closet/o/a/",              // 空的一段
		"/pkg-files/closet//o/a/1.png",        //
		"/pkg-files/closet/o//a/1.png",        //
		"/pkg-files/closet/",                  //
		"/pkg-files/closet",                   //
		"/pkg-files/",                         //
		"/pkg-files/datapkg/x.png",            // 不是附程式的套件
		"/pkg-files/off/x.png",                // 停用的套件
		"/pkg-files/nope/x.png",               // 沒這個套件
		"/pkg-files/Closet/o/a/1.png",         // id 格式
		"/pkg-files/closet/a/b/c/d/e/f/g.png", // 超過 6 段
	} {
		if w := get(e.h, p); w.Code != http.StatusNotFound {
			t.Errorf("%s → %d %q", p, w.Code, w.Body.String())
		}
	}

	// 公開資料夾不存在：404（核心不建它）。
	e.setPackages(&fakeCatalog{pkgs: []sdk.Package{pkgOf("closet", sdk.PackageKindProgram, true), pkgOf("fresh", sdk.PackageKindProgram, true)}})
	if w := get(e.h, "/pkg-files/fresh/x.png"); w.Code != http.StatusNotFound {
		t.Fatalf("沒有公開資料夾 → %d", w.Code)
	}
	if _, err := os.Stat(publicPath(e, "fresh")); !os.IsNotExist(err) {
		t.Fatalf("核心不該建公開資料夾：%v", err)
	}

	// 公開資料夾本身是符號連結：404。
	write(t, filepath.Join(e.home, "elsewhere", "y.png"), "別處")
	if err := os.MkdirAll(filepath.Join(e.home, "runtime", "packages", "linked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.home, "elsewhere"), publicPath(e, "linked")); err != nil {
		t.Fatal(err)
	}
	e.setPackages(&fakeCatalog{pkgs: []sdk.Package{pkgOf("linked", sdk.PackageKindProgram, true)}})
	if w := get(e.h, "/pkg-files/linked/y.png"); w.Code != http.StatusNotFound {
		t.Fatalf("公開資料夾是符號連結 → %d", w.Code)
	}
}

func closetEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, "")
	e.setPackages(&fakeCatalog{pkgs: []sdk.Package{avatarPkg("closet", sdk.PackageKindProgram, true)}})
	write(t, filepath.Join(e.home, "personas", "avatars", "builder.png"), "預設圖")
	write(t, filepath.Join(e.home, "personas", "avatars", "builder.mp4"), "預設影片")
	return e
}

// TestAvatarFollowsWornWhenClosetManages：宣告 contributes.avatars 的套件（衣櫃）管這位時，頭像照 worn 的圖（類型照實際副檔名），.mp4 不退回預設影片。
// Guards: REQ-113, REQ-112
func TestAvatarFollowsWornWhenClosetManages(t *testing.T) {
	e := closetEnv(t)
	jpg := []byte("\xff\xd8\xff\xe0fake-jpeg")
	write(t, publicPath(e, "closet", "worn", "builder.jpg"), string(jpg))

	w := get(e.h, "/avatars/code.png")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(w.Body.Bytes(), jpg) {
		t.Fatalf("worn 的 jpg：%d %v", w.Code, w.Header())
	}
	// 沒有 worn mp4：404，不退回預設影片。
	if w := get(e.h, "/avatars/code.mp4"); w.Code != http.StatusNotFound {
		t.Fatalf("沒有 worn mp4 要 404：%d %q", w.Code, w.Body.String())
	}
	// 有 worn mp4 就回它。
	write(t, publicPath(e, "closet", "worn", "builder.mp4"), "穿的影片")
	if w := get(e.h, "/avatars/code.mp4"); w.Code != http.StatusOK || w.Body.String() != "穿的影片" || w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("worn mp4：%d %v %q", w.Code, w.Header(), w.Body.String())
	}
	// 沒被管的人（helper）維持原本行為（人設沒有圖 → 404）。
	if w := get(e.h, "/avatars/life.png"); w.Code != http.StatusNotFound {
		t.Fatalf("life → %d", w.Code)
	}
	// worn 是符號連結不算：退回預設圖。
	if err := os.Remove(publicPath(e, "closet", "worn", "builder.jpg")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.home, "personas", "avatars", "builder.png"), publicPath(e, "closet", "worn", "builder.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(publicPath(e, "closet", "worn", "builder.mp4")); err != nil {
		t.Fatal(err)
	}
	if w := get(e.h, "/avatars/code.png"); w.Code != http.StatusOK || w.Body.String() != "預設圖" {
		t.Fatalf("符號連結的 worn 不算：%d %q", w.Code, w.Body.String())
	}
	if w := get(e.h, "/avatars/code.mp4"); w.Code != http.StatusOK || w.Body.String() != "預設影片" {
		t.Fatalf("沒被管就用預設影片：%d %q", w.Code, w.Body.String())
	}
}

// TestAvatarFallsBackWithoutCloset：沒裝、停用、不是附程式、或沒宣告 contributes.avatars 的套件，頭像維持原本行為，worn 的檔案不會被送。
// Guards: REQ-113, REQ-112
func TestAvatarFallsBackWithoutCloset(t *testing.T) {
	e := closetEnv(t)
	write(t, publicPath(e, "closet", "worn", "builder.png"), "穿的圖")
	write(t, publicPath(e, "closet", "worn", "builder.mp4"), "穿的影片")
	for name, pkgs := range map[string][]sdk.Package{
		"沒裝":    nil,
		"停用":    {avatarPkg("closet", sdk.PackageKindProgram, false)},
		"不是附程式": {avatarPkg("closet", sdk.PackageKindData, true)},
		"沒宣告":   {pkgOf("closet", sdk.PackageKindProgram, true)},
	} {
		e.setPackages(&fakeCatalog{pkgs: pkgs})
		if w := get(e.h, "/avatars/code.png"); w.Code != http.StatusOK || w.Body.String() != "預設圖" {
			t.Errorf("%s png：%d %q", name, w.Code, w.Body.String())
		}
		if w := get(e.h, "/avatars/code.mp4"); w.Code != http.StatusOK || w.Body.String() != "預設影片" {
			t.Errorf("%s mp4：%d %q", name, w.Code, w.Body.String())
		}
	}
}

// /avatars/persona/<代號>.png 照角色代號給頭像，不必有秘書坐著（新角色打招呼的訊息用，WEB-150）；
// 跟秘書頭像同一條解析鏈（衣櫃管這位就給 worn 的圖），代號不合格、跑出資料夾、沒有檔案一律 404。
func TestPersonaAvatarBySlugWithoutSeat(t *testing.T) {
	e := closetEnv(t)
	write(t, filepath.Join(e.home, "personas", "avatars", "luna.png"), "露娜的圖")
	if w := get(e.h, "/avatars/persona/luna.png"); w.Code != http.StatusOK || w.Body.String() != "露娜的圖" || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("沒坐位的角色也要有頭像：%d %q", w.Code, w.Body.String())
	}
	write(t, publicPath(e, "closet", "worn", "luna.png"), "露娜穿的圖")
	if w := get(e.h, "/avatars/persona/luna.png"); w.Code != http.StatusOK || w.Body.String() != "露娜穿的圖" {
		t.Fatalf("衣櫃管這位就照 worn：%d %q", w.Code, w.Body.String())
	}
	for _, p := range []string{"/avatars/persona/nobody.png", "/avatars/persona/luna.gif", "/avatars/persona/../home.toml", "/avatars/persona/a/luna.png", "/avatars/persona/..png"} {
		if w := get(e.h, p); w.Code != http.StatusNotFound {
			t.Errorf("%s 要 404：%d", p, w.Code)
		}
	}
}

// TestHasImageFollowsAvatarChain（DEV-157）：名冊與角色庫只在有圖時給 avatar_url；判斷跟 /avatars/… 送圖同一條鏈——
// 人設自己的預設圖、衣櫃管這位時的 worn 圖都算，兩個都沒有、代號不合格就是沒有。
func TestHasImageFollowsAvatarChain(t *testing.T) {
	e := closetEnv(t)
	if e.as.HasImage("luna") || e.as.HasImage("../home") {
		t.Fatal("沒有檔案、代號不合格：沒有圖")
	}
	write(t, filepath.Join(e.home, "personas", "avatars", "luna.png"), "露娜的圖")
	if !e.as.HasImage("luna") {
		t.Fatal("有預設圖")
	}
	write(t, publicPath(e, "closet", "worn", "mira.png"), "米拉穿的圖")
	if !e.as.HasImage("mira") {
		t.Fatal("衣櫃管這位、有 worn 圖")
	}
	// 符號連結：送圖那邊（serveRegular）會 404，這裡也不能說有圖。
	link := filepath.Join(e.home, "personas", "avatars", "nova.png")
	if err := os.Symlink(filepath.Join(e.home, "personas", "avatars", "luna.png"), link); err != nil {
		t.Fatal(err)
	}
	if e.as.HasImage("nova") {
		t.Fatal("符號連結不算有圖")
	}
}

// TestVideoURLFollowsWorn：closet 管這位時，動態頭像網址看 worn 的 mp4，沒有就空（名冊的 video_url 用它）。
// Guards: REQ-113
func TestVideoURLFollowsWorn(t *testing.T) {
	e := closetEnv(t)
	videoOf := func() string { return e.as.VideoURL("code", "builder") }
	if got := videoOf(); got != "/avatars/code.mp4" {
		t.Fatalf("沒被管：%q", got)
	}
	write(t, publicPath(e, "closet", "worn", "builder.png"), "穿的圖")
	if got := videoOf(); got != "" {
		t.Fatalf("被管而且沒有 worn mp4 → 空：%q", got)
	}
	write(t, publicPath(e, "closet", "worn", "builder.mp4"), "穿的影片")
	if got := videoOf(); got != "/avatars/code.mp4" {
		t.Fatalf("有 worn mp4：%q", got)
	}
}

// TestMultiClipAvatars（DEV-125）：衣櫃管這位時，worn 的 <人設>+k.mp4 對應 /avatars/<id>+k.mp4 與 video_urls；
// 從第 1 支連續、缺一支就停；沒人管時只有第 1 支，+2 一律 404。
// Guards: REQ-113
func TestMultiClipAvatars(t *testing.T) {
	e := closetEnv(t)
	// 沒被管：只有預設影片一支，+2 404。
	if got := e.as.VideoURLs("code", "builder"); len(got) != 1 || got[0] != "/avatars/code.mp4" {
		t.Fatalf("沒被管：%v", got)
	}
	if w := get(e.h, "/avatars/code+2.mp4"); w.Code != http.StatusNotFound {
		t.Fatalf("沒被管不該有第 2 支：%d", w.Code)
	}
	write(t, publicPath(e, "closet", "worn", "builder.png"), "穿的圖")
	write(t, publicPath(e, "closet", "worn", "builder.mp4"), "影片1")
	write(t, publicPath(e, "closet", "worn", "builder+2.mp4"), "影片2")
	write(t, publicPath(e, "closet", "worn", "builder+4.mp4"), "影片4") // +3 缺了，+4 不算
	got := e.as.VideoURLs("code", "builder")
	if len(got) != 2 || got[0] != "/avatars/code.mp4" || got[1] != "/avatars/code+2.mp4" {
		t.Fatalf("worn 兩支：%v", got)
	}
	if w := get(e.h, "/avatars/code+2.mp4"); w.Code != http.StatusOK || w.Body.String() != "影片2" || w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("+2：%d %q", w.Code, w.Body.String())
	}
	if w := get(e.h, "/avatars/code.mp4"); w.Code != http.StatusOK || w.Body.String() != "影片1" {
		t.Fatalf("第 1 支：%d %q", w.Code, w.Body.String())
	}
	// 缺的、格式不對（+4 檔案雖在，但清單沒有它，前端不會要）的、超過上限的都 404；圖不接受 +k。
	for _, p := range []string{"/avatars/code+3.mp4", "/avatars/code+1.mp4", "/avatars/code+02.mp4", "/avatars/code+7.mp4", "/avatars/code+2.png", "/avatars/code++2.mp4"} {
		if w := get(e.h, p); w.Code != http.StatusNotFound {
			t.Errorf("%s 應該 404：%d", p, w.Code)
		}
	}
	// 照角色代號的路徑也一樣。
	if w := get(e.h, "/avatars/persona/builder+2.mp4"); w.Code != http.StatusOK || w.Body.String() != "影片2" {
		t.Errorf("persona 路徑 +2：%d %q", w.Code, w.Body.String())
	}
	// 換了衣服（worn 圖換、第 2 支消失）：不能留著舊的第 2 支。
	if err := os.Remove(publicPath(e, "closet", "worn", "builder+2.mp4")); err != nil {
		t.Fatal(err)
	}
	if got := e.as.VideoURLs("code", "builder"); len(got) != 1 {
		t.Fatalf("第 2 支沒了：%v", got)
	}
}
