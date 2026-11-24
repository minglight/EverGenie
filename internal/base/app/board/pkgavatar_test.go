package board

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 套件角色的頭像：/avatars/<職位>.png 的解析鏈 = 衣櫃 worn → 官方人設預設圖 → 套件裡的 personas/avatars/<slug>.png → 404。
// Guards: REQ-317
func TestPackagePersonaAvatarChain(t *testing.T) {
	e := newEnv(t, "")
	e.agents.list = []*sdk.Record{agentRecord("garden", map[string]any{"duty": "花園", "persona": "garden.fairy", "enabled": true, "mode": "resident"})}
	dir := filepath.Join(t.TempDir(), "garden")
	write(t, filepath.Join(dir, "personas", "avatars", "fairy.png"), "套件頭像")
	write(t, filepath.Join(dir, "personas", "avatars", "fairy.mp4"), "套件影片")
	e.setPackages(&fakeCatalog{pkgs: []sdk.Package{pkgOf("garden", sdk.PackageKindData, true)}, dirs: map[string]string{"garden": dir}})

	if w := get(e.h, "/avatars/garden.png"); w.Code != http.StatusOK || w.Body.String() != "套件頭像" {
		t.Fatalf("套件頭像：%d %q", w.Code, w.Body.String())
	}
	if w := get(e.h, "/avatars/garden.mp4"); w.Code != http.StatusOK || w.Body.String() != "套件影片" || w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("套件影片：%d %q", w.Code, w.Body.String())
	}
	if got := e.as.VideoURL("garden", "garden.fairy"); got != "/avatars/garden.mp4" {
		t.Fatalf("videoURL：%q", got)
	}
	// 官方人設預設圖優先（同一個有效代號放在 personas/avatars 底下）。
	write(t, filepath.Join(e.home, "personas", "avatars", "garden.fairy.png"), "官方預設圖")
	if w := get(e.h, "/avatars/garden.png"); w.Body.String() != "官方預設圖" {
		t.Fatalf("官方預設圖優先：%q", w.Body.String())
	}
	// 衣櫃穿的更優先。
	e.setPackages(&fakeCatalog{pkgs: []sdk.Package{avatarPkg("closet", sdk.PackageKindProgram, true), pkgOf("garden", sdk.PackageKindData, true)}, dirs: map[string]string{"garden": dir}})
	write(t, publicPath(e, "closet", "worn", "garden.fairy.png"), "穿的圖")
	if w := get(e.h, "/avatars/garden.png"); w.Body.String() != "穿的圖" {
		t.Fatalf("worn 優先：%q", w.Body.String())
	}
	// 套件停用或被拒（PackageDir 找不到）：404，不壞。
	_ = os.Remove(publicPath(e, "closet", "worn", "garden.fairy.png"))
	_ = os.Remove(filepath.Join(e.home, "personas", "avatars", "garden.fairy.png"))
	e.setPackages(&fakeCatalog{pkgs: []sdk.Package{pkgOf("garden", sdk.PackageKindData, false)}})
	if w := get(e.h, "/avatars/garden.png"); w.Code != http.StatusNotFound {
		t.Fatalf("套件不在：%d", w.Code)
	}
	// 符號連結跑出套件資料夾：不算。
	outside := filepath.Join(t.TempDir(), "secret.png")
	write(t, outside, "外面的檔")
	_ = os.Remove(filepath.Join(dir, "personas", "avatars", "fairy.png"))
	if err := os.Symlink(outside, filepath.Join(dir, "personas", "avatars", "fairy.png")); err != nil {
		t.Fatal(err)
	}
	e.setPackages(&fakeCatalog{pkgs: []sdk.Package{pkgOf("garden", sdk.PackageKindData, true)}, dirs: map[string]string{"garden": dir}})
	if w := get(e.h, "/avatars/garden.png"); w.Code != http.StatusNotFound {
		t.Fatalf("符號連結不能帶出套件資料夾：%d", w.Code)
	}
}
