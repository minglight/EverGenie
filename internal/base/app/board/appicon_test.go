package board

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 畫一張 w×h 的圖：上半 top 色、下半 bottom 色。
func twoTone(t *testing.T, w, h int, top, bottom color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		c := top
		if y >= h/2 {
			c = bottom
		}
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var (
	red    = color.RGBA{220, 20, 20, 255}
	blue   = color.RGBA{20, 20, 220, 255}
	green  = color.RGBA{20, 200, 20, 255}
	amber  = color.RGBA{0xf5, 0x9e, 0x0b, 255}
	purple = color.RGBA{106, 74, 158, 255}
)

// iconEnv：家的 [runtime] 與 [web] 另外寫；靜態圖示換成單色的小圖，好分辨「用了靜態圖」。
func iconEnv(t *testing.T, runtime, web string) *env {
	t.Helper()
	e := newEnv(t, web+"\n[runtime]\nowner=\"amy\"\nmachine=\"home\"\n"+runtime+"\n")
	staticIcon := twoTone(t, 64, 64, purple, purple)
	e.m.static = fstest.MapFS{
		"index.html":                 {Data: []byte(`<html><head><link rel="apple-touch-icon" href="/icons/apple-touch-icon.png" /><meta name="theme-color" content="#f5f3f9" media="(prefers-color-scheme: light)" /><meta name="theme-color" content="#14121c" media="(prefers-color-scheme: dark)" /><title>看板</title></head><body><div id="root"></div></body></html>`)},
		"icons/icon-192.png":         {Data: staticIcon},
		"icons/icon-512.png":         {Data: staticIcon},
		"icons/maskable-512.png":     {Data: staticIcon},
		"icons/apple-touch-icon.png": {Data: staticIcon},
	}
	e.h = viaLoopback(e.m.Handler())
	return e
}

func manifestOf(t *testing.T, e *env) (m struct {
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	Theme     string `json:"theme_color"`
	Icons     []struct {
		Src, Sizes, Type, Purpose string
	} `json:"icons"`
}) {
	t.Helper()
	w := get(e.h, "/manifest.webmanifest")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/manifest+json") || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("manifest：%d %v", w.Code, w.Header())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func iconOf(t *testing.T, e *env, name string) image.Image {
	t.Helper()
	w := get(e.h, "/icons/"+name)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("%s：%d %v", name, w.Code, w.Header())
	}
	img, err := png.Decode(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func near(c color.Color, want color.RGBA) bool {
	r, g, b, _ := c.RGBA()
	d := func(x uint32, y uint8) bool { v := int(x>>8) - int(y); return v > -24 && v < 24 }
	return d(r, want.R) && d(g, want.G) && d(b, want.B)
}

// manifest 的名稱：正式家「看板 <machine>」、開發家「開發 <machine>」，開發家的 theme_color 是橘色；三個圖示都帶版本。
func TestManifestNamesHomeByEnv(t *testing.T) {
	m := manifestOf(t, iconEnv(t, `env="prod"`, ""))
	if m.Name != "看板 home" || m.ShortName != "看板 home" || m.Theme != themeProd || len(m.Icons) != 3 {
		t.Fatalf("正式家：%+v", m)
	}
	purposes := ""
	for _, ic := range m.Icons {
		if !strings.Contains(ic.Src, "?v=") || ic.Type != "image/png" {
			t.Fatalf("圖示要帶版本：%+v", ic)
		}
		purposes += ic.Purpose + ","
	}
	if purposes != "any,any,maskable," {
		t.Fatalf("purpose：%s", purposes)
	}
	m = manifestOf(t, iconEnv(t, `env="dev"`, ""))
	if m.Name != "開發 home" || m.Theme != themeDev {
		t.Fatalf("開發家：%+v", m)
	}
}

// 圖示用 [web] app_icon 的人設頭像：半身照（直的）裁上方的正方形，縮到 192／512／180。
func TestAppIconCropsTopSquareOfAppIconPersona(t *testing.T) {
	e := iconEnv(t, "", "app_icon=\"artemis\"")
	write(t, filepath.Join(e.home, "personas", "avatars", "artemis.png"), string(twoTone(t, 100, 200, red, blue)))
	write(t, filepath.Join(e.home, "personas", "avatars", "helper.png"), string(twoTone(t, 50, 50, green, green)))
	for name, size := range map[string]int{"icon-192.png": 192, "icon-512.png": 512, "maskable-512.png": 512, "apple-touch-icon.png": 180} {
		img := iconOf(t, e, name)
		if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
			t.Fatalf("%s 的大小：%v", name, img.Bounds())
		}
		for _, p := range []image.Point{{size / 2, 2}, {size / 2, size / 2}, {size / 2, size - 3}} {
			if !near(img.At(p.X, p.Y), red) {
				t.Fatalf("%s 要是頭像上方的正方形（全紅），%v 是 %v", name, p, img.At(p.X, p.Y))
			}
		}
	}
}

// 沒設 app_icon：用 life 秘書的人設；沒有 life（或 life 停用）：第一位啟用的秘書；橫的圖取中間。
func TestAppIconFallsBackToLifeThenFirstEnabled(t *testing.T) {
	e := iconEnv(t, "", "")
	write(t, filepath.Join(e.home, "personas", "avatars", "builder.png"), string(twoTone(t, 40, 40, blue, blue)))
	write(t, filepath.Join(e.home, "personas", "avatars", "helper.png"), string(twoTone(t, 300, 100, green, green)))
	if img := iconOf(t, e, "icon-192.png"); !near(img.At(96, 96), green) {
		t.Fatalf("要用 life（helper）的頭像：%v", img.At(96, 96))
	}
	e.agents.list = []*sdk.Record{
		agentRecord("code", map[string]any{"persona": "builder", "enabled": true}),
		agentRecord("life", map[string]any{"persona": "helper", "enabled": false}),
	}
	if img := iconOf(t, e, "icon-192.png"); !near(img.At(96, 96), blue) {
		t.Fatalf("life 停用就用第一位啟用的（code／builder）：%v", img.At(96, 96))
	}
}

// 都沒有頭像：正式家原樣回靜態圖示；開發家用靜態圖示加 DEV 帶。
func TestAppIconWithoutAvatarUsesStaticIcon(t *testing.T) {
	e := iconEnv(t, "", "")
	want, _ := e.m.static.(fstest.MapFS)["icons/icon-192.png"]
	if w := get(e.h, "/icons/icon-192.png"); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), want.Data) {
		t.Fatalf("正式家沒頭像要原樣回靜態圖示：%d", w.Code)
	}
	e = iconEnv(t, `env="dev"`, "")
	img := iconOf(t, e, "icon-192.png")
	if img.Bounds().Dx() != 192 || !near(img.At(96, 4), purple) || !near(img.At(2, 189), amber) {
		t.Fatalf("開發家：靜態圖示放大＋下緣橘色橫條：%v %v %v", img.Bounds(), img.At(96, 4), img.At(2, 189))
	}
}

// 開發家的圖示下緣有一條橘色橫條寫 DEV（帶子中間有深色的字）；maskable 的帶子在安全區裡；正式家沒有。
func TestDevAppIconHasAmberBandWithText(t *testing.T) {
	e := iconEnv(t, `env="dev"`, "app_icon=\"artemis\"")
	write(t, filepath.Join(e.home, "personas", "avatars", "artemis.png"), string(twoTone(t, 200, 200, red, red)))
	img := iconOf(t, e, "icon-512.png")
	if !near(img.At(256, 100), red) || !near(img.At(4, 500), amber) || !near(img.At(507, 420), amber) {
		t.Fatalf("上面是頭像、下緣是橘色橫條：%v %v", img.At(256, 100), img.At(4, 500))
	}
	dark := 0
	for x := 150; x < 362; x++ {
		if r, g, b, _ := img.At(x, 456).RGBA(); r>>8 < 80 && g>>8 < 80 && b>>8 < 80 {
			dark++
		}
	}
	if dark < 10 {
		t.Fatalf("帶子中間要有 DEV 的字：深色像素 %d", dark)
	}
	mask := iconOf(t, e, "maskable-512.png")
	if !near(mask.At(256, 330), amber) || !near(mask.At(256, 500), red) {
		t.Fatalf("maskable 的帶子在安全區（62%%–84%%）：%v %v", mask.At(256, 330), mask.At(256, 500))
	}
	prod := iconEnv(t, "", "app_icon=\"artemis\"")
	write(t, filepath.Join(prod.home, "personas", "avatars", "artemis.png"), string(twoTone(t, 200, 200, red, red)))
	if img := iconOf(t, prod, "icon-512.png"); !near(img.At(4, 500), red) {
		t.Fatalf("正式家沒有帶子：%v", img.At(4, 500))
	}
}

// 圖示的版本跟著頭像與設定變：換頭像、改 env，manifest 的網址換掉、圖也重產；同一個版本有 ETag，手機帶 If-None-Match 回 304。
func TestAppIconVersionFollowsAvatarAndSettings(t *testing.T) {
	e := iconEnv(t, "", "app_icon=\"artemis\"")
	avatar := filepath.Join(e.home, "personas", "avatars", "artemis.png")
	write(t, avatar, string(twoTone(t, 100, 100, red, red)))
	v1 := manifestOf(t, e).Icons[0].Src
	if v1 != manifestOf(t, e).Icons[0].Src {
		t.Fatal("沒有變動，版本要一樣")
	}
	w := get(e.h, "/icons/icon-192.png")
	r, _ := http.NewRequest(http.MethodGet, "/icons/icon-192.png", nil)
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	if w.Header().Get("ETag") == "" || rec.Code != http.StatusNotModified {
		t.Fatalf("ETag 與 304：%q %d", w.Header().Get("ETag"), rec.Code)
	}
	write(t, avatar, string(twoTone(t, 120, 120, blue, blue)))
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(avatar, later, later)
	v2 := manifestOf(t, e).Icons[0].Src
	if v2 == v1 {
		t.Fatalf("換了頭像版本要變：%s", v2)
	}
	if img := iconOf(t, e, "icon-192.png"); !near(img.At(96, 96), blue) {
		t.Fatalf("換了頭像圖要重產：%v", img.At(96, 96))
	}
	dev := iconEnv(t, `env="dev"`, "app_icon=\"artemis\"")
	write(t, filepath.Join(dev.home, "personas", "avatars", "artemis.png"), string(twoTone(t, 120, 120, blue, blue)))
	if manifestOf(t, dev).Icons[0].Src == v2 {
		t.Fatal("env 不同版本要不同")
	}
}

// index.html 依家改：apple-touch-icon 帶版本；開發家的 theme-color 換橘色、標題前加「開發｜」；正式家不動顏色與標題。
func TestIndexHTMLMarksDevHome(t *testing.T) {
	e := iconEnv(t, `env="dev"`, "")
	body := get(e.h, "/").Body.String()
	if !strings.Contains(body, `href="/icons/apple-touch-icon.png?v=`) || strings.Count(body, `content="`+themeDev+`"`) != 2 || !strings.Contains(body, "<title>開發｜看板</title>") {
		t.Fatalf("開發家的 index.html：%s", body)
	}
	e = iconEnv(t, "", "")
	body = get(e.h, "/chat").Body.String()
	if !strings.Contains(body, `href="/icons/apple-touch-icon.png?v=`) || strings.Contains(body, themeDev) || !strings.Contains(body, "<title>看板</title>") {
		t.Fatalf("正式家的 index.html：%s", body)
	}
}
