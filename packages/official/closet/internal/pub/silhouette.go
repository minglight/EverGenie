package pub

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"

	// 標準庫只解得開 PNG 與 JPEG（WEBP 不引入第三方解碼：最保守，頁面自己畫灰色人形）。
	_ "image/jpeg"
)

const (
	// SilhouetteVersion 是剪影演算法的版本：改了演算法就加 1，對帳會重做所有剪影。
	SilhouetteVersion = 1
	maxPixels         = 40_000_000
	grid              = 16
	outSize           = 128
	lumLow, lumHigh   = 90, 190
)

// Silhouette 產生剪影：解碼 PNG／JPEG（寬×高超過 4000 萬像素就放棄）→ 有透明度先墊 (128,128,128) → 轉亮度 →
// 面積平均成 16×16 → 雙線性放大成 128×128 → 亮度壓進 [90,190] → 灰階 PNG。
// 因為只剩 16×16 個格子的平均值，細節（臉、字、輪廓）不可能還原；輸出只由這 256 個平均值決定。
func Silhouette(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("檔案太大")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("不是標準庫解得開的 PNG／JPEG：%w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, fmt.Errorf("圖片尺寸不合（%d×%d）", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解碼失敗：%w", err)
	}
	cells := cellAverages(img)
	out := image.NewGray(image.Rect(0, 0, outSize, outSize))
	for y := 0; y < outSize; y++ {
		for x := 0; x < outSize; x++ {
			v := bilinear(cells, (float64(x)+0.5)*grid/outSize-0.5, (float64(y)+0.5)*grid/outSize-0.5)
			out.SetGray(x, y, color.Gray{Y: uint8(lumLow + int(v*(lumHigh-lumLow)/255+0.5))})
		}
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// cellAverages 把圖切成 16×16 格，每格的亮度（0–255）取平均。透明的地方墊灰色 128。
func cellAverages(img image.Image) [grid][grid]float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	var cells [grid][grid]float64
	for cy := 0; cy < grid; cy++ {
		y0, y1 := span(cy, h)
		for cx := 0; cx < grid; cx++ {
			x0, x1 := span(cx, w)
			var sum, n int64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					sum += luminance(img.At(b.Min.X+x, b.Min.Y+y))
					n++
				}
			}
			cells[cy][cx] = float64(sum) / float64(n)
		}
	}
	return cells
}

// span 是第 i 格（共 16 格）在長度 n 裡的範圍，至少一個像素。
func span(i, n int) (int, int) {
	a, b := i*n/grid, (i+1)*n/grid
	if b <= a {
		b = a + 1
	}
	if b > n {
		b = n
		if a >= b {
			a = b - 1
		}
	}
	return a, b
}

func luminance(c color.Color) int64 {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	a := int64(n.A)
	mix := func(ch uint8) int64 { return (int64(ch)*a + 128*(255-a)) / 255 }
	return (299*mix(n.R) + 587*mix(n.G) + 114*mix(n.B)) / 1000
}

func bilinear(cells [grid][grid]float64, fx, fy float64) float64 {
	clamp := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > grid-1 {
			return grid - 1
		}
		return v
	}
	fx, fy = clamp(fx), clamp(fy)
	x0, y0 := int(fx), int(fy)
	x1, y1 := min(x0+1, grid-1), min(y0+1, grid-1)
	tx, ty := fx-float64(x0), fy-float64(y0)
	top := cells[y0][x0]*(1-tx) + cells[y0][x1]*tx
	bottom := cells[y1][x0]*(1-tx) + cells[y1][x1]*tx
	return top*(1-ty) + bottom*ty
}
