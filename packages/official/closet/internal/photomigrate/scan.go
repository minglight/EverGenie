package photomigrate

import (
	"fmt"
	"path/filepath"

	"github.com/minglight/evergenie/packages/official/closet/internal/pub"
	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
)

// 舊檔名規則（DEV-160 S2 從 pub 搬來，全 repo 只剩這裡認得）：
//
//	<代號>[~N].png|jpg|jpeg|webp   第 N 張半身照（第 1 張不帶 ~N，之後從 ~2 起連續）；代號＝base 是人設本身，其他 <人設>@<衣服>
//	<代號>[~N].mp4、+k.mp4          同一張的第 1 支、第 k 支（k ≥ 2，從 +2 起連續）影片
//	full/<代號>.<ext>                全身照
//
// 其他檔（子資料夾、*.locked.png、*.live.json、*.voice.mp4）一律不讀。轉換後各處只讀 closet.photo，不再看檔名。

const (
	// MaxPhotos：舊規則一套最多掃 12 張。
	MaxPhotos = 12
	// MaxVideos：舊規則同一張最多掃 6 支影片。
	MaxVideos = 6
)

// StaticExts 是靜態圖的副檔名（同一張只認第一個存在的，順序同這行）。
var StaticExts = []string{".png", ".jpg", ".jpeg", ".webp"}

// scanned 是照舊檔名規則掃到的一張照片（第 N 張，1 起算）。
type scanned struct {
	N     int
	Src   string   // 靜態圖的完整路徑
	Ext   string   // 靜態圖的副檔名（小寫，含點）
	Video string   // 同一張的第 1 支 .mp4；沒有是空字串
	Extra []string // 第 2 支起的 .mp4（+2、+3…），已排序
}

// Videos 是這張所有影片（第 1 支在前，沒有第 1 支就從 Extra 開始）。
func (p scanned) Videos() []string {
	var out []string
	if p.Video != "" {
		out = append(out, p.Video)
	}
	return append(out, p.Extra...)
}

// Code 是舊圖檔的代號：第一套 base 就是人設本身（athena），其他是 <人設>@<衣服>。
func Code(persona, outfit string) string {
	if outfit == rules.BaseOutfit {
		return persona
	}
	return persona + "@" + outfit
}

func photoName(code string, n int) string {
	if n <= 1 {
		return code
	}
	return fmt.Sprintf("%s~%d", code, n)
}

func scanExtraVideos(base string) []string {
	var out []string
	for k := 2; k <= MaxVideos; k++ {
		f := fmt.Sprintf("%s+%d.mp4", base, k)
		if _, ok := pub.RegularFile(f); !ok {
			break
		}
		out = append(out, f)
	}
	return out
}

// ScanPhotos 掃 dir 裡這套衣服的照片：第 1 張不帶 ~N，之後從 ~2 起連續編號，缺一張就停，最多 12 張。
func ScanPhotos(dir, persona, outfit string) []scanned {
	var out []scanned
	for n := 1; n <= MaxPhotos; n++ {
		p, ok := scanPhoto(dir, persona, outfit, n)
		if !ok {
			break
		}
		out = append(out, p)
	}
	return out
}

// ScanSlots 掃隱藏版資料夾：跟 ScanPhotos 同樣的命名，但不要求連續（隱藏版可以只換第 2 張）。回第 N 張 → 照片。
func ScanSlots(dir, persona, outfit string) map[int]scanned {
	out := map[int]scanned{}
	for n := 1; n <= MaxPhotos; n++ {
		if p, ok := scanPhoto(dir, persona, outfit, n); ok {
			out[n] = p
		}
	}
	return out
}

func scanPhoto(dir, persona, outfit string, n int) (scanned, bool) {
	if !rules.ValidPersona(persona) || !rules.ValidOutfit(outfit) {
		return scanned{}, false
	}
	base := filepath.Join(dir, photoName(Code(persona, outfit), n))
	for _, ext := range StaticExts {
		if _, ok := pub.RegularFile(base + ext); ok {
			p := scanned{N: n, Src: base + ext, Ext: ext}
			if _, ok := pub.RegularFile(base + ".mp4"); ok {
				p.Video = base + ".mp4"
			}
			p.Extra = scanExtraVideos(base)
			return p, true
		}
	}
	return scanned{}, false
}

// ScanFull 找這套衣服的全身照 <dir>/full/<代號>.<ext>；沒有回零值（Src 空）。
func ScanFull(dir, persona, outfit string) scanned {
	if !rules.ValidPersona(persona) || !rules.ValidOutfit(outfit) {
		return scanned{}
	}
	base := filepath.Join(dir, "full", Code(persona, outfit))
	for _, ext := range StaticExts {
		if _, ok := pub.RegularFile(base + ext); ok {
			return scanned{N: 1, Src: base + ext, Ext: ext}
		}
	}
	return scanned{}
}
