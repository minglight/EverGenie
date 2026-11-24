package pub

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func pngOf(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// cellImage 產生 64×64 的灰階圖：每個 4×4 的格子平均亮度是 base(cx,cy)；detail 為 0 時格子是純色，
// 不為 0 時格內的像素在 base±detail 之間交錯（格內平均不變，細節完全不同）。
func cellImage(detail int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			base := 40 + ((x/4)*7+(y/4)*11)%170
			v := base
			if detail != 0 {
				if (x+y)%2 == 0 {
					v += detail
				} else {
					v -= detail
				}
			}
			img.SetGray(x, y, color.Gray{Y: uint8(v)})
		}
	}
	return img
}

// Guards: REQ-125
func TestSilhouetteRevealsNothing(t *testing.T) {
	a, err := Silhouette(bytes.NewReader(pngOf(cellImage(0))))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Silhouette(bytes.NewReader(pngOf(cellImage(35))))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("16×16 格平均相同、格內像素不同的兩張圖，剪影位元組要完全相同")
	}
	img, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	g, ok := img.(*image.Gray)
	if !ok || g.Bounds().Dx() != 128 || g.Bounds().Dy() != 128 {
		t.Fatalf("要 128×128 灰階：%T %v", img, img.Bounds())
	}
	lo, hi := 255, 0
	for _, p := range g.Pix {
		lo, hi = min(lo, int(p)), max(hi, int(p))
	}
	if lo < 90 || hi > 190 || lo == hi {
		t.Fatalf("亮度要壓在 [90,190]：%d..%d", lo, hi)
	}
	// 格子平均不同的圖，剪影不同；JPEG 也解得開；透明的地方墊灰色。
	other := cellImage(0)
	other.SetGray(0, 0, color.Gray{Y: 255})
	other.SetGray(1, 0, color.Gray{Y: 255})
	c, err := Silhouette(bytes.NewReader(pngOf(other)))
	if err != nil || bytes.Equal(a, c) {
		t.Fatalf("格子平均不同要得到不同的剪影：%v", err)
	}
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, cellImage(0), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Silhouette(&jb); err != nil {
		t.Fatalf("JPEG：%v", err)
	}
	transparent := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	flat, err := Silhouette(bytes.NewReader(pngOf(transparent)))
	if err != nil {
		t.Fatal(err)
	}
	gi, _ := png.Decode(bytes.NewReader(flat))
	want := uint8(90 + 128*100/255)
	if got := gi.(*image.Gray).GrayAt(5, 5).Y; got != want {
		t.Fatalf("全透明墊灰 128 → 亮度 %d，要 %d", got, want)
	}
}

// bigHeaderPNG 是宣稱 20000×20000 的 PNG（只有表頭有效）。
func bigHeaderPNG() []byte {
	data := pngOf(image.NewGray(image.Rect(0, 0, 1, 1)))
	binary.BigEndian.PutUint32(data[16:], 20000)
	binary.BigEndian.PutUint32(data[20:], 20000)
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	return data
}

// Guards: REQ-125
func TestSilhouetteSkipsWebpAndCorrupt(t *testing.T) {
	webp := append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 40)...)
	for name, data := range map[string][]byte{
		"webp":    webp,
		"壞掉的 png": pngOf(cellImage(0))[:60],
		"亂碼":      []byte("not an image"),
		"空":       {},
		"超大尺寸":    bigHeaderPNG(),
	} {
		if out, err := Silhouette(bytes.NewReader(data)); err == nil || out != nil {
			t.Errorf("%s 應該產生不出剪影", name)
		}
	}
	// 對帳時解不開的檔不產生剪影檔、不放真圖，只記錯誤。
	dir, src := t.TempDir(), filepath.Join(t.TempDir(), "a@x.webp")
	write(t, src, webp)
	res, err := Reconcile(dir, Plan{"s/a/x/1.png": {Src: src, Silhouette: true}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Present["s/a/x/1.png"] || res.Errors["s/a/x/1.png"] == nil {
		t.Fatalf("webp 不該有剪影檔：%+v", res)
	}
	if files := tree(t, dir); len(files) != 0 {
		t.Fatalf("公開資料夾要是空的：%v", files)
	}
}

// Guards: REQ-125
func TestSafeNamesOnly(t *testing.T) {
	d := t.TempDir()
	for _, c := range [][2]string{{"Athena", "base"}, {"athena", "Night"}, {"../x", "base"}, {"athena", "../x"}, {"a/b", "base"}, {"a.b", "base"}, {"1a", "base"}, {"athena", "-x"}, {"", "base"}, {"athena", ""}, {"athena", "a@b"}} {
		ph := []Photo{{N: 1, Src: filepath.Join(d, "x.png"), Ext: ".png"}}
		if got := BuildPlan([]OutfitInput{{Persona: c[0], Outfit: c[1], Owned: true, Photos: ph}}, []Wearing{{Persona: c[0], Outfit: c[1], Photo: ph[0]}}); len(got) != 0 {
			t.Errorf("%v 不該處理：%+v", c, got)
		}
	}
	for s, want := range map[string]bool{"1.png": true, "athena": true, "a-b_c": true, "1+2.mp4": true, "athena+2.mp4": true, "a++b": true, "a+/b": false, ".state.json": false, "": false, "A.png": false, "a b": false, "a..b": false, "a/b": false, "é": false} {
		if SafeName(s) != want {
			t.Errorf("SafeName(%q) = %v", s, !want)
		}
	}
	// 目的路徑不合格的項目不寫、不能跑出公開資料夾。
	pubDir := filepath.Join(t.TempDir(), "public")
	src := filepath.Join(d, "ok.png")
	write(t, src, pngOf(cellImage(0)))
	res, err := Reconcile(pubDir, Plan{"../escape.png": {Src: src}, "o/../../x.png": {Src: src}, "/abs.png": {Src: src}, ".hidden.png": {Src: src}, "o/Bad Name.png": {Src: src}, "o/ok.png": {Src: src}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Present) != 1 || !res.Present["o/ok.png"] || len(res.Errors) != 5 {
		t.Fatalf("只有合格的路徑會被放：%+v", res)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(pubDir), "escape.png")); err == nil {
		t.Fatal("跑出公開資料夾了")
	}
}

func tree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func has(files []string, want string) bool {
	for _, f := range files {
		if f == want {
			return true
		}
	}
	return false
}

// Guards: REQ-125
func TestReconcilePublishesOnlyOwned(t *testing.T) {
	avatars, public := t.TempDir(), filepath.Join(t.TempDir(), "public")
	real := map[string][]byte{}
	for _, name := range []string{"athena", "athena@night", "athena@sea", "athena@old"} {
		img := cellImage(0)
		img.SetGray(10, 10, color.Gray{Y: uint8(len(name) * 9)}) // 每張不一樣
		real[name] = pngOf(img)
		write(t, filepath.Join(avatars, name+".png"), real[name])
		write(t, filepath.Join(avatars, name+".mp4"), []byte("mp4-of-"+name))
	}
	in := func(owned map[string]bool, drop string) []OutfitInput {
		var list []OutfitInput
		for _, o := range []struct{ id, state string }{{"base", "active"}, {"night", "active"}, {"sea", "draft"}, {"old", "retired"}} {
			if o.id == drop {
				continue
			}
			list = append(list, OutfitInput{Persona: "athena", Outfit: o.id, State: o.state, Owned: owned[o.id], Photos: photosOf(avatars, "athena", o.id)})
		}
		return list
	}
	run := func(owned map[string]bool, drop string) []string {
		res, err := Reconcile(public, BuildPlan(in(owned, drop), nil))
		if err != nil || len(res.Errors) != 0 {
			t.Fatalf("%v %v", err, res.Errors)
		}
		return tree(t, public)
	}

	files := run(map[string]bool{"base": true}, "")
	// 沒擁有的 night：沒有真圖與 mp4，有剪影，而且剪影不是真圖。
	for _, f := range files {
		if strings.HasPrefix(f, "o/athena/night/") || strings.HasPrefix(f, "o/athena/old/") || strings.HasPrefix(f, "s/athena/old/") {
			t.Errorf("不該出現：%s", f)
		}
	}
	if !has(files, "s/athena/night/1.png") {
		t.Fatalf("night 要有剪影：%v", files)
	}
	sil, _ := os.ReadFile(filepath.Join(public, "s/athena/night/1.png"))
	if bytes.Equal(sil, real["athena@night"]) {
		t.Fatal("剪影不能等於真圖")
	}
	// 擁有的與草稿：有真圖與 mp4；草稿沒有剪影。
	for _, f := range []string{"o/athena/base/1.png", "o/athena/base/1.mp4", "o/athena/sea/1.png", "o/athena/sea/1.mp4"} {
		if !has(files, f) {
			t.Errorf("要有 %s：%v", f, files)
		}
	}
	if has(files, "s/athena/sea/1.png") || has(files, "s/athena/base/1.png") {
		t.Errorf("擁有的與草稿不放剪影：%v", files)
	}
	if got, _ := os.ReadFile(filepath.Join(public, "o/athena/base/1.png")); !bytes.Equal(got, real["athena"]) {
		t.Error("真圖要等於來源")
	}

	// 兌換後：剪影被真圖取代。
	files = run(map[string]bool{"base": true, "night": true}, "")
	if has(files, "s/athena/night/1.png") || !has(files, "o/athena/night/1.png") || !has(files, "o/athena/night/1.mp4") {
		t.Fatalf("兌換後：%v", files)
	}
	// 下架但已擁有：真圖照樣有；下架且沒擁有：什麼都沒有（上面已驗）。
	files = run(map[string]bool{"base": true, "night": true, "old": true}, "")
	if !has(files, "o/athena/old/1.png") {
		t.Fatalf("下架但擁有：%v", files)
	}

	// 手動放進去的雜物與符號連結會被清掉，`.` 開頭的不碰。
	write(t, filepath.Join(public, "o/athena/base/extra.png"), []byte("stray"))
	write(t, filepath.Join(public, ".keep"), []byte("k"))
	if err := os.Symlink(filepath.Join(avatars, "athena.png"), filepath.Join(public, "o/athena/base/link.png")); err != nil {
		t.Fatal(err)
	}
	files = run(map[string]bool{"base": true, "night": true, "old": true}, "")
	if has(files, "o/athena/base/extra.png") || has(files, "o/athena/base/link.png") {
		t.Fatalf("雜物要被清掉：%v", files)
	}
	if _, err := os.Stat(filepath.Join(public, ".keep")); err != nil {
		t.Fatal(".keep 不該被碰")
	}

	// 衣服被移走後檔案被刪（含空資料夾）。
	files = run(map[string]bool{"base": true}, "night")
	for _, f := range files {
		if strings.Contains(f, "night") {
			t.Errorf("night 被移走後不該留下：%s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(public, "o/athena/night")); err == nil {
		t.Error("空資料夾要刪掉")
	}
	// 來源檔不見了：公開的那份也跟著拿掉。
	if err := os.Remove(filepath.Join(avatars, "athena.mp4")); err != nil {
		t.Fatal(err)
	}
	files = run(map[string]bool{"base": true}, "night")
	if has(files, "o/athena/base/1.mp4") {
		t.Errorf("來源 mp4 沒了，公開的 mp4 也要拿掉：%v", files)
	}
}

// 沒變的來源不重寫（mtime 不變）；來源變了才重寫。
// Guards: REQ-125
func TestReconcileSkipsUnchanged(t *testing.T) {
	avatars, public := t.TempDir(), filepath.Join(t.TempDir(), "public")
	write(t, filepath.Join(avatars, "athena.png"), pngOf(cellImage(0)))
	plan := func() Plan {
		return BuildPlan([]OutfitInput{{Persona: "athena", Outfit: "base", State: "active", Owned: true, Photos: photosOf(avatars, "athena", "base")}}, nil)
	}
	if _, err := Reconcile(public, plan()); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(public, "o/athena/base/1.png")
	first, _ := os.Stat(target)
	if err := os.Chtimes(target, first.ModTime().Add(-3600e9), first.ModTime().Add(-3600e9)); err != nil {
		t.Fatal(err)
	}
	marked, _ := os.Stat(target)
	if _, err := Reconcile(public, plan()); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.Stat(target); !again.ModTime().Equal(marked.ModTime()) {
		t.Fatal("來源沒變不該重寫")
	}
	write(t, filepath.Join(avatars, "athena.png"), pngOf(cellImage(9)))
	if _, err := Reconcile(public, plan()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, pngOf(cellImage(9))) {
		t.Fatal("來源變了要重寫")
	}
	if again, _ := os.Stat(target); !again.ModTime().After(marked.ModTime()) {
		t.Fatal("重寫後 mtime 要是現在")
	}
}

// Guards: REQ-113, REQ-125
func TestWornFollowsRotationAndDropsStaleVideo(t *testing.T) {
	avatars, public := t.TempDir(), filepath.Join(t.TempDir(), "public")
	write(t, filepath.Join(avatars, "athena.png"), pngOf(cellImage(0)))
	write(t, filepath.Join(avatars, "athena@night.jpg"), []byte("night-jpg"))
	write(t, filepath.Join(avatars, "athena@night.mp4"), []byte("night-mp4"))
	write(t, filepath.Join(avatars, "athena@sea.png"), []byte("sea-png"))
	photos := func(o string) []Photo { return photosOf(avatars, "athena", o) }
	outfits := []OutfitInput{
		{Persona: "athena", Outfit: "base", State: "active", Owned: true, Photos: photos("base")},
		{Persona: "athena", Outfit: "night", State: "active", Owned: true, Photos: photos("night")},
		{Persona: "athena", Outfit: "sea", State: "active", Owned: false, Photos: photos("sea")},
	}
	wear := func(o string) []Wearing { return []Wearing{{Persona: "athena", Outfit: o, Photo: photos(o)[0]}} }
	apply := func(w []Wearing) []string {
		if _, err := Reconcile(public, BuildPlan(outfits, w)); err != nil {
			t.Fatal(err)
		}
		return tree(t, public)
	}

	files := apply(wear("night"))
	if !has(files, "worn/athena.jpg") || !has(files, "worn/athena.mp4") {
		t.Fatalf("穿 night：%v", files)
	}
	if got, _ := os.ReadFile(filepath.Join(public, "worn/athena.jpg")); string(got) != "night-jpg" {
		t.Fatalf("worn 的圖：%q", got)
	}
	// 換成 base：副檔名換了、沒有 mp4，舊的 jpg 與 mp4 都要刪掉（圖是這套、影片不能是別套）。
	files = apply(wear("base"))
	if !has(files, "worn/athena.png") || has(files, "worn/athena.jpg") || has(files, "worn/athena.mp4") {
		t.Fatalf("穿 base：%v", files)
	}
	// 沒擁有的衣服就算被指定現穿，也不會放（防禦）。
	files = apply(wear("sea"))
	for _, f := range files {
		if strings.HasPrefix(f, "worn/") {
			t.Fatalf("沒擁有的不該出現在 worn：%v", files)
		}
	}
	// 沒有現穿 → 沒有 worn（核心退回人設預設圖）。
	if files = apply(nil); has(files, "worn/athena.png") {
		t.Fatalf("沒有現穿：%v", files)
	}
}

// TestReconcileFullOnlyOwned：全身照 full/<代號>.<ext> 只對擁有的與草稿的衣服公開成 f/<人設>/<衣服>/1.<ext>；
// 沒擁有的不在公開樹、也沒有剪影；副檔名順序同半身（png 優先）。
// Guards: REQ-125, REQ-126
func TestReconcileFullOnlyOwned(t *testing.T) {
	avatars, public := t.TempDir(), filepath.Join(t.TempDir(), "public")
	for _, name := range []string{"athena", "athena@night", "athena@sea"} {
		write(t, filepath.Join(avatars, name+".png"), pngOf(cellImage(0)))
	}
	fullPNG, fullJPG := pngOf(cellImage(3)), []byte("jpg-should-lose")
	write(t, filepath.Join(avatars, "full", "athena.png"), fullPNG)
	write(t, filepath.Join(avatars, "full", "athena.jpg"), fullJPG)
	write(t, filepath.Join(avatars, "full", "athena@night.webp"), []byte("webp-night"))
	write(t, filepath.Join(avatars, "full", "athena@sea.jpeg"), []byte("jpeg-sea"))
	var list []OutfitInput
	for _, o := range []struct {
		id, state string
		owned     bool
	}{{"base", "active", true}, {"night", "active", false}, {"sea", "draft", false}} {
		list = append(list, OutfitInput{Persona: "athena", Outfit: o.id, State: o.state, Owned: o.owned,
			Photos: photosOf(avatars, "athena", o.id), Full: fullOf(avatars, "athena", o.id)})
	}
	res, err := Reconcile(public, BuildPlan(list, nil))
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("%v %v", err, res.Errors)
	}
	files := tree(t, public)
	if !has(files, "f/athena/base/1.png") || !has(files, "f/athena/sea/1.jpeg") {
		t.Fatalf("擁有與草稿的全身照要公開：%v", files)
	}
	if got, _ := os.ReadFile(filepath.Join(public, "f/athena/base/1.png")); !bytes.Equal(got, fullPNG) {
		t.Error("同一張只認第一個存在的（png 優先），內容要等於來源")
	}
	for _, f := range files {
		if strings.HasPrefix(f, "f/athena/night/") {
			t.Errorf("沒擁有的全身照不能在公開樹：%s", f)
		}
	}
	if has(files, "f/athena/base/1.jpg") {
		t.Errorf("同一張只公開一個副檔名：%v", files)
	}
	if !has(files, "s/athena/night/1.png") {
		t.Errorf("沒擁有的只有半身剪影：%v", files)
	}
	for _, f := range files {
		if strings.HasPrefix(f, "s/") && strings.Contains(f, "full") {
			t.Errorf("沒擁有的全身照不給剪影：%s", f)
		}
	}
}

// 開發家不遮（CLO-27）：Preview 的衣服沒兌換也照擁有的放真圖、全身照、動畫，不做剪影；沒有 Preview（正式家）照舊只有剪影；
// 下架而且沒擁有的：正式家什麼都不放，開發家（Preview）也放真圖（「修改」分頁要看得到，DEV-160 審查 S3b）。
func TestBuildPlanPreviewRevealsOnlyWhenFlagged(t *testing.T) {
	avatars := t.TempDir()
	for _, name := range []string{"athena@night", "athena@old"} {
		write(t, filepath.Join(avatars, name+".png"), pngOf(cellImage(0)))
		write(t, filepath.Join(avatars, name+".mp4"), []byte("mp4"))
		write(t, filepath.Join(avatars, "full", name+".png"), pngOf(cellImage(1)))
	}
	in := func(preview bool) []OutfitInput {
		var list []OutfitInput
		for _, o := range []struct{ id, state string }{{"night", "active"}, {"old", "retired"}} {
			list = append(list, OutfitInput{Persona: "athena", Outfit: o.id, State: o.state, Preview: preview,
				Photos: photosOf(avatars, "athena", o.id), Full: fullOf(avatars, "athena", o.id)})
		}
		return list
	}
	keys := func(p Plan) []string {
		var out []string
		for k := range p {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	prod := BuildPlan(in(false), nil)
	if got := keys(prod); len(got) != 1 || got[0] != "s/athena/night/1.png" || !prod["s/athena/night/1.png"].Silhouette {
		t.Fatalf("正式家：沒兌換的只有剪影：%v", got)
	}
	dev := keys(BuildPlan(in(true), nil))
	want := []string{"f/athena/night/1.png", "f/athena/old/1.png", "o/athena/night/1.mp4", "o/athena/night/1.png", "o/athena/old/1.mp4", "o/athena/old/1.png"}
	if strings.Join(dev, " ") != strings.Join(want, " ") {
		t.Fatalf("開發家：沒兌換的（含下架的）給真圖、全身照、動畫，不做剪影：%v", dev)
	}
}
