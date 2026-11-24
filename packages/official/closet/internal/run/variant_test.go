package run

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// variantScene：builder 擁有 base（免費），沒擁有 night、sea；隱藏版資料夾 personas/extra/set 裡三套都有圖。
func variantScene(t *testing.T) (*scene, map[string][]byte) {
	sc := newScene(t)
	dir := filepath.Join(sc.home, "personas", "extra", "set")
	hidden := map[string][]byte{
		"builder":       pngFile(t, filepath.Join(dir, "builder.png"), 40),
		"builder~2":     pngFile(t, filepath.Join(dir, "builder~2.png"), 41),
		"builder@night": pngFile(t, filepath.Join(dir, "builder@night.png"), 42),
		"full/builder":  pngFile(t, filepath.Join(dir, "full", "builder.png"), 43),
	}
	if err := os.WriteFile(filepath.Join(dir, "builder.mp4"), []byte("hidden-mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	return sc, hidden
}

// publicHas：公開資料夾裡有沒有任何一個檔案的內容是 data。
func (sc *scene) publicHas(data []byte) bool {
	found := false
	root := filepath.Join(sc.home, "runtime", "packages", "closet", "public")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, _ := os.ReadFile(path); bytes.Equal(b, data) {
				found = true
			}
		}
		return nil
	})
	return found
}

func (sc *scene) variant(fields map[string]any) {
	sc.v.seed(KindVariant, "extra", fields)
}

// 按鈕：開了就把隱藏版的圖（照片、影片、全身照）接在正常版後面一起列，正常版照樣在（2026-11-13 作者：兩版都顯示，不是二選一）；
// 關了拿掉隱藏版的圖；隱藏版的網址帶 ?v=。
// 沒擁有的衣服的隱藏版圖不論開不開都不會出現在公開資料夾。
func TestVariantButtonListsBothAndRestores(t *testing.T) {
	sc, hidden := variantScene(t)
	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": false})
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/1.png"), sc.img["builder"]) || sc.public("o/builder/base/2.png") != nil {
		t.Fatal("關著時跟正常版一樣")
	}
	vs := sc.fields(KindState, "main")["variants"].([]any)
	// 隱藏版只有一組（DEV-160 S2）：一項 id = h，ids 是這一組的每一筆設定（頁面按開關時一起改）。
	if len(vs) != 1 || vs[0].(map[string]any)["id"] != "h" || vs[0].(map[string]any)["name"] != "隱藏版" || vs[0].(map[string]any)["button"] != true || vs[0].(map[string]any)["on"] != false ||
		len(vs[0].(map[string]any)["ids"].([]any)) != 1 || vs[0].(map[string]any)["ids"].([]any)[0] != "extra" {
		t.Fatalf("state.variants：%v", vs)
	}
	silhouette := sc.public("s/builder/night/1.png")
	if len(silhouette) == 0 {
		t.Fatal("沒擁有的 night 要有剪影")
	}

	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": true})
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/1.png"), sc.img["builder"]) {
		t.Fatal("開著時一般版照樣在")
	}
	if !bytes.Equal(sc.public("o/builder/base/h-1.png"), hidden["builder"]) || !bytes.Equal(sc.public("o/builder/base/h-2.png"), hidden["builder~2"]) {
		t.Fatal("開著時擁有的衣服多列隱藏版的圖（含第 2 張）")
	}
	if string(sc.public("o/builder/base/h-1.mp4")) != "hidden-mp4" || !bytes.Equal(sc.public("f/builder/base/h-1.png"), hidden["full/builder"]) {
		t.Fatal("隱藏版的影片與全身照也列")
	}
	base := sc.outfitView("base")
	photos := base["photos"].([]any)
	if len(photos) != 3 || strings.Contains(photos[0].(map[string]any)["url"].(string), "?v=") ||
		!strings.HasSuffix(photos[1].(map[string]any)["url"].(string), "/o/builder/base/h-1.png?v=h") ||
		base["full"] != "" || !strings.HasSuffix(base["full_hidden"].(string), "/f/builder/base/h-1.png?v=h") || base["full_hidden_variant"] != "h" {
		t.Fatalf("一般版在前、隱藏版在後，隱藏版網址帶 ?v=：%v", base)
	}
	if sc.publicHas(hidden["builder@night"]) || sc.public("o/builder/night/1.png") != nil || !bytes.Equal(sc.public("s/builder/night/1.png"), silhouette) {
		t.Fatal("沒擁有的 night，隱藏版的圖不能出現在公開資料夾，剪影也照舊用正常版")
	}

	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": false})
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/1.png"), sc.img["builder"]) || sc.public("o/builder/base/h-2.png") != nil || sc.public("f/builder/base/h-1.png") != nil {
		t.Fatal("關掉要拿掉隱藏版的照片與全身照")
	}
	for _, h := range hidden {
		if sc.publicHas(h) {
			t.Fatal("關掉後公開資料夾不留任何隱藏版的圖")
		}
	}
	if u := sc.outfitView("base")["photos"].([]any)[0].(map[string]any)["url"].(string); strings.Contains(u, "?v=") {
		t.Fatalf("換回後網址不帶 ?v=：%s", u)
	}
}

// 時段：時段內自動開、時段外自動關（開關沒開也一樣）；Round 記下下一個邊界給 Run 排計時器。
func TestVariantWindowTurnsOnAndOff(t *testing.T) {
	sc, hidden := variantScene(t)
	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "windows": []any{"10:00-11:00"}})
	sc.now = time.Date(2026, 11, 4, 10, 30, 0, 0, time.Local)
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/h-1.png"), hidden["builder"]) {
		t.Fatal("時段內要列隱藏版")
	}
	if v := sc.fields(KindState, "main")["variants"].([]any)[0].(map[string]any); v["in_window"] != true || v["button"] != false {
		t.Fatalf("state 要寫在時段裡：%v", v)
	}
	if want := time.Date(2026, 11, 4, 11, 0, 0, 0, time.Local); !sc.svc.boundary.Equal(want) {
		t.Fatalf("下一個邊界 %v，要 %v", sc.svc.boundary, want)
	}
	sc.now = time.Date(2026, 11, 4, 11, 0, 0, 0, time.Local)
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/1.png"), sc.img["builder"]) || sc.publicHas(hidden["builder"]) {
		t.Fatal("時段結束要拿掉隱藏版")
	}
}

// 設定不合格（資料夾跑出 personas/）的隱藏版不作用；好幾個開著時照 order 先用前面的。
func TestVariantInvalidIgnoredAndOrderWins(t *testing.T) {
	sc, hidden := variantScene(t)
	pngFile(t, filepath.Join(sc.home, "vault", "builder.png"), 50)
	sc.v.seed(KindVariant, "bad", map[string]any{"name": "壞", "dir": "../vault", "on": true})
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/1.png"), sc.img["builder"]) {
		t.Fatal("資料夾越界的隱藏版不作用")
	}
	other := filepath.Join(sc.home, "personas", "second")
	second := pngFile(t, filepath.Join(other, "builder.png"), 60)
	sc.v.seed(KindVariant, "aaa", map[string]any{"name": "二", "dir": "second", "on": true, "order": 2})
	sc.v.seed(KindVariant, "extra", map[string]any{"name": "一", "dir": "extra/set", "on": true, "order": 1})
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/h-1.png"), hidden["builder"]) || sc.publicHas(second) {
		t.Fatal("order 小的先用")
	}
}

// 設計中（draft）的衣服、使用者自己的衣服（personas/mine/avatars）也套用隱藏版；
// 隱藏版的張數不必連續：只有第 1、3 張也都列。
func TestVariantAppliesToDraftAndMine(t *testing.T) {
	sc, _ := variantScene(t)
	sc.outfit("sea", 2, "draft", 3)
	dir := filepath.Join(sc.home, "personas", "extra", "set")
	seaHidden := pngFile(t, filepath.Join(dir, "builder@sea.png"), 70)
	third := pngFile(t, filepath.Join(dir, "builder@sea~3.png"), 71)
	pngFile(t, filepath.Join(sc.home, "personas", "mine", "avatars", "pet.png"), 7)
	sc.v.seed(KindOutfit, "pet-base", map[string]any{"persona": "pet", "outfit": "base", "name": "原樣", "cost": 0, "state": "active", "source": "mine"})
	petHidden := pngFile(t, filepath.Join(dir, "pet.png"), 72)
	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "on": true})
	sc.round()
	if !bytes.Equal(sc.public("o/builder/sea/h-1.png"), seaHidden) || sc.public("o/builder/sea/1.png") == nil {
		t.Fatal("設計中的衣服也列隱藏版，一般版照樣在")
	}
	if !bytes.Equal(sc.public("o/builder/sea/h-3.png"), third) {
		t.Fatal("隱藏版第 3 張也列")
	}
	if !bytes.Equal(sc.public("o/pet/base/h-1.png"), petHidden) {
		t.Fatal("使用者自己的衣服也列隱藏版")
	}
}

// 隱藏版的資料夾經過符號連結跑出 personas/ 的不用。
func TestVariantDirSymlinkEscapeIgnored(t *testing.T) {
	sc, _ := variantScene(t)
	outside := t.TempDir()
	leak := pngFile(t, filepath.Join(outside, "pics", "builder.png"), 80)
	if err := os.Symlink(outside, filepath.Join(sc.home, "personas", "link")); err != nil {
		t.Fatal(err)
	}
	sc.variant(map[string]any{"name": "特別版", "dir": "link/pics", "on": true})
	sc.round()
	if sc.publicHas(leak) || !bytes.Equal(sc.public("o/builder/base/1.png"), sc.img["builder"]) {
		t.Fatal("跑出 personas/ 的資料夾不能用")
	}
}

// 邊界計時器：沒時段不排；邊界已經過了（那一輪中途失敗）隔 retry 再試，不馬上空轉。
func TestBoundaryDelayNeverSpins(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	if _, ok := BoundaryDelay(time.Time{}, now, 3*time.Second); ok {
		t.Fatal("沒有時段不排計時器")
	}
	if d, ok := BoundaryDelay(now.Add(time.Hour), now, 3*time.Second); !ok || d != time.Hour+time.Second {
		t.Fatalf("一小時後的邊界：%v", d)
	}
	if d, ok := BoundaryDelay(now.Add(-time.Minute), now, 3*time.Second); !ok || d != 3*time.Second {
		t.Fatalf("過期的邊界隔 retry 再試：%v", d)
	}
}
