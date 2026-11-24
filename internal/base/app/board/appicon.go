package board

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg" // 頭像可能是 jpeg 內容（副檔名是 .png 也一樣解）
	"image/png"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp" // 衣櫃穿的圖可能是 webp

	"github.com/minglight/evergenie/internal/base/platform"
)

// 手機主畫面的名稱與圖示（2026-11-01）：manifest.webmanifest 與 /icons/ 底下的四張圖由伺服器依家產生，一眼分得出是哪個家。
//   - 名稱：正式家「看板 <machine>」、開發家「開發 <machine>」。
//   - 圖示：主要角色的頭像（[web] app_icon 的人設；沒設用 life 秘書、沒有 life 用第一位啟用的秘書），
//     裁成正方形（半身照取上方置中）縮到 192／512（另有 maskable 512、apple-touch 180）；都沒有頭像就用內建的靜態圖示。
//   - 開發家（env = dev）的圖示下緣疊一條橘色橫條寫 DEV（maskable 放在安全區裡）。
//   - 圖示網址帶 ?v=<版本>，版本跟著頭像檔（路徑、大小、修改時間）、人設、env、machine 變，換頭像或改設定手機會重抓。

const (
	appIconRender = "1" // 產圖的做法改了就加一，讓手機重抓
	themeProd     = "#f5f3f9"
	themeDev      = "#f59e0b" // 跟前端 token --attention 同色（橘色）
	devInk        = "#1f1b2d" // 跟 --on-attention 同色
)

// appIcons：網址的檔名 → 邊長與是不是 maskable。
var appIcons = map[string]struct {
	size     int
	maskable bool
	static   string // 沒有頭像時的靜態檔（dist 裡）
}{
	"icon-192.png":         {192, false, "icons/icon-192.png"},
	"icon-512.png":         {512, false, "icons/icon-512.png"},
	"maskable-512.png":     {512, true, "icons/maskable-512.png"},
	"apple-touch-icon.png": {180, false, "icons/apple-touch-icon.png"},
}

// appInfo 是這個家目前的主畫面設定：身分、用誰的頭像、版本。
type appInfo struct {
	dev     bool
	machine string
	avatar  string // 頭像檔的絕對路徑；空＝用靜態圖示
	version string
}

type appIconCache struct {
	mu      sync.Mutex
	version string
	files   map[string][]byte
}

var (
	devFontOnce sync.Once
	devFont     *opentype.Font
)

// appInfo 讀家的設定與名冊，決定圖示用誰的頭像、算版本。讀不到家的設定就當正式家、用靜態圖示。
func (m *Module) appInfo(ctx context.Context) appInfo {
	info := appInfo{machine: platform.ProductName}
	dir := m.homeDir()
	if dir == "" {
		info.version = appVersion(info, "", nil)
		return info
	}
	h, err := platform.LoadHome(dir)
	if err != nil {
		info.version = appVersion(info, "", nil)
		return info
	}
	info.dev, info.machine = h.IsDev(), h.Runtime.Machine
	slug := ""
	for _, s := range m.iconCandidates(ctx, h.Web.AppIcon) {
		if file, _, ok := m.d.Assets.Avatar(s, ".png"); ok {
			if st, err := os.Lstat(file); err == nil && st.Mode().IsRegular() {
				info.avatar, slug = file, s
				info.version = appVersion(info, slug, st)
				return info
			}
		}
	}
	info.version = appVersion(info, slug, nil)
	return info
}

// iconCandidates：先 [web] app_icon，再 life 秘書（啟用的），再第一位啟用的秘書（照職位 id 排）；回人設代號。
func (m *Module) iconCandidates(ctx context.Context, appIcon string) []string {
	out := []string{}
	if s := strings.TrimSpace(appIcon); s != "" {
		out = append(out, s)
	}
	if m.d.Agents == nil {
		return out
	}
	agents, err := m.d.Agents.List(ctx)
	if err != nil {
		return out
	}
	type seat struct{ id, persona string }
	var enabled []seat
	for _, a := range agents {
		if a == nil {
			continue
		}
		on, _ := a.Fields["enabled"].(bool)
		persona, _ := a.Fields["persona"].(string)
		if on && persona != "" {
			enabled = append(enabled, seat{a.ID, persona})
		}
	}
	sort.Slice(enabled, func(i, j int) bool { return enabled[i].id < enabled[j].id })
	for _, s := range enabled {
		if s.id == "life" {
			out = append(out, s.persona)
		}
	}
	if len(enabled) > 0 {
		out = append(out, enabled[0].persona)
	}
	return out
}

func appVersion(info appInfo, slug string, st fs.FileInfo) string {
	key := fmt.Sprintf("%s|%v|%s|%s|%s", appIconRender, info.dev, info.machine, slug, info.avatar)
	if st != nil {
		key += fmt.Sprintf("|%d|%d", st.Size(), st.ModTime().UnixNano())
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:6])
}

// manifest 是 manifest.webmanifest 的內容。
func (m *Module) manifest(info appInfo) ([]byte, error) {
	name, theme := "看板 "+info.machine, themeProd
	if info.dev {
		name, theme = "開發 "+info.machine, themeDev
	}
	icon := func(file, purpose string, size int) map[string]string {
		return map[string]string{"src": "/icons/" + file + "?v=" + info.version, "sizes": fmt.Sprintf("%dx%d", size, size), "type": "image/png", "purpose": purpose}
	}
	return json.MarshalIndent(map[string]any{
		"id": "/", "name": name, "short_name": name, "lang": "zh-TW",
		"start_url": "/", "scope": "/", "display": "standalone",
		"theme_color": theme, "background_color": themeProd,
		"icons": []map[string]string{icon("icon-192.png", "any", 192), icon("icon-512.png", "any", 512), icon("maskable-512.png", "maskable", 512)},
	}, "", "  ")
}

func (m *Module) serveManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "不支援這個方法", http.StatusMethodNotAllowed)
		return
	}
	data, err := m.manifest(m.appInfo(r.Context()))
	if err != nil {
		http.Error(w, "manifest 產生失敗", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "manifest.webmanifest", zeroTime, bytes.NewReader(data))
}

// serveAppIcon 回 /icons/<名>；不是四張動態圖之一就回 false（交給靜態檔）。
func (m *Module) serveAppIcon(w http.ResponseWriter, r *http.Request) bool {
	name := strings.TrimPrefix(r.URL.Path, "/icons/")
	spec, ok := appIcons[name]
	if !ok {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "不支援這個方法", http.StatusMethodNotAllowed)
		return true
	}
	info := m.appInfo(r.Context())
	data, err := m.iconBytes(info, name, spec.size, spec.maskable, spec.static)
	if err != nil {
		m.log.Warn("主畫面圖示產生失敗，改用靜態圖示", "icon", name, "error", err)
		if m.static != nil && serveFS(w, r, m.static, spec.static, "no-cache") {
			return true
		}
		http.NotFound(w, r)
		return true
	}
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "no-cache")
	h.Set("ETag", `"`+info.version+`"`)
	http.ServeContent(w, r, name, zeroTime, bytes.NewReader(data))
	return true
}

// iconBytes 回一張圖示（同一個版本產過就用快取）。正式家又沒有頭像：原樣回靜態圖示。
func (m *Module) iconBytes(info appInfo, name string, size int, maskable bool, static string) ([]byte, error) {
	c := &m.icons
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version != info.version {
		c.version, c.files = info.version, map[string][]byte{}
	}
	if b, ok := c.files[name]; ok {
		return b, nil
	}
	var src image.Image
	if info.avatar != "" {
		f, err := os.Open(info.avatar)
		if err != nil {
			return nil, err
		}
		src, _, err = image.Decode(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("頭像解不開：%w", err)
		}
	} else {
		if m.static == nil {
			return nil, fmt.Errorf("沒有靜態圖示")
		}
		raw, err := fs.ReadFile(m.static, static)
		if err != nil {
			return nil, err
		}
		if !info.dev {
			c.files[name] = raw
			return raw, nil
		}
		if src, err = png.Decode(bytes.NewReader(raw)); err != nil {
			return nil, err
		}
	}
	out, err := renderAppIcon(src, size, maskable, info.dev)
	if err != nil {
		return nil, err
	}
	c.files[name] = out
	return out, nil
}

// renderAppIcon：裁成正方形（直的取上方、橫的取中間）、縮到 size、開發家疊 DEV 帶，編成 png。
func renderAppIcon(src image.Image, size int, maskable, dev bool) ([]byte, error) {
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	if side <= 0 {
		return nil, fmt.Errorf("頭像是空的")
	}
	x0 := b.Min.X + (b.Dx()-side)/2
	y0 := b.Min.Y // 半身照：臉在上面，取上方
	if b.Dx() > b.Dy() {
		y0 = b.Min.Y + (b.Dy()-side)/2
	}
	crop := image.Rect(x0, y0, x0+side, y0+side)
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, xdraw.Src, nil)
	if dev {
		drawDevBand(dst, size, maskable)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// drawDevBand 在下緣畫一條橘色橫條、置中寫 DEV。maskable 圖示會被系統裁成圓或圓角，帶子放在安全區（中間 80%）裡。
func drawDevBand(dst *image.RGBA, size int, maskable bool) {
	bandH := size * 22 / 100
	top := size - bandH
	if maskable {
		top = size * 62 / 100
	}
	draw.Draw(dst, image.Rect(0, top, size, top+bandH), image.NewUniform(hexColor(themeDev)), image.Point{}, draw.Src)
	devFontOnce.Do(func() { devFont, _ = opentype.Parse(gobold.TTF) })
	if devFont == nil {
		return
	}
	face, err := opentype.NewFace(devFont, &opentype.FaceOptions{Size: float64(bandH) * 0.72, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return
	}
	defer face.Close()
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(hexColor(devInk)), Face: face}
	met := face.Metrics()
	textH := met.Ascent + met.Descent
	baseline := fixed.I(top) + (fixed.I(bandH)-textH)/2 + met.Ascent
	d.Dot = fixed.Point26_6{X: (fixed.I(size) - d.MeasureString("DEV")) / 2, Y: baseline}
	d.DrawString("DEV")
}

func hexColor(s string) color.RGBA {
	var r, g, b uint8
	_, _ = fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
	return color.RGBA{r, g, b, 255}
}

var (
	appleIconLink = regexp.MustCompile(`href="/icons/apple-touch-icon\.png[^"]*"`)
	themeColorTag = regexp.MustCompile(`(<meta name="theme-color" content=")[^"]*(")`)
	titleTag      = regexp.MustCompile(`<title>([^<]*)</title>`)
)

// indexHTML 依家改 index.html：apple-touch-icon 加版本；開發家的 theme-color 換橘色、標題前加「開發｜」（畫面載好之前就看得出來）。
func (m *Module) indexHTML(ctx context.Context, raw []byte) []byte {
	info := m.appInfo(ctx)
	out := appleIconLink.ReplaceAll(raw, []byte(`href="/icons/apple-touch-icon.png?v=`+info.version+`"`))
	if info.dev {
		out = themeColorTag.ReplaceAll(out, []byte(`${1}`+themeDev+`${2}`))
		out = titleTag.ReplaceAll(out, []byte(`<title>開發｜${1}</title>`))
	}
	return out
}
