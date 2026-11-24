package pub

import (
	"fmt"
	"path/filepath"

	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
)

// photosOf／fullOf 是測試用：照舊檔名（<代號>[~N].<ext>、.mp4、+k.mp4、full/<代號>.<ext>）找圖，給 BuildPlan 當輸入。
// 正式程式不看檔名，改讀照片紀錄（DEV-160 S2）；舊檔名規則只在 photomigrate。
func photosOf(dir, persona, outfit string) []Photo {
	if !rules.ValidPersona(persona) || !rules.ValidOutfit(outfit) {
		return nil
	}
	code := persona
	if outfit != rules.BaseOutfit {
		code = persona + "@" + outfit
	}
	var out []Photo
	for n := 1; n <= MaxPhotos; n++ {
		base := filepath.Join(dir, code)
		if n > 1 {
			base = fmt.Sprintf("%s~%d", base, n)
		}
		ph, ok := Photo{}, false
		for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp"} {
			if _, found := RegularFile(base + ext); found {
				ph, ok = Photo{N: n, Src: base + ext, Ext: ext}, true
				break
			}
		}
		if !ok {
			break
		}
		if _, found := RegularFile(base + ".mp4"); found {
			ph.Videos = append(ph.Videos, base+".mp4")
		}
		for k := 2; k <= MaxVideos; k++ {
			f := fmt.Sprintf("%s+%d.mp4", base, k)
			if _, found := RegularFile(f); !found {
				break
			}
			ph.Videos = append(ph.Videos, f)
		}
		out = append(out, ph)
	}
	return out
}

func fullOf(dir, persona, outfit string) Photo {
	code := persona
	if outfit != rules.BaseOutfit {
		code = persona + "@" + outfit
	}
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp"} {
		if src := filepath.Join(dir, "full", code+ext); func() bool { _, ok := RegularFile(src); return ok }() {
			return Photo{N: 1, Src: src, Ext: ext}
		}
	}
	return Photo{}
}
