package pub

import (
	"fmt"
	"strings"

	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
)

// URLPrefix 是核心送公開檔案的網址前綴（D-29）。
const URLPrefix = "/pkg-files/closet/"

// 公開資料夾的結構（相對於 public/）：
//
//	o/<人設>/<衣服>/<n>.<ext>   真圖（只放擁有的與 draft 的；<n>.mp4 是同張的動態）
//	                            例外只有開發家（CLO-27）：沒兌換的上架衣服也照擁有的放真圖、不做剪影
//	s/<人設>/<衣服>/1.png       剪影（只給沒擁有的、非草稿、非下架的衣服，只做第 1 張）
//	f/<人設>/<衣服>/1.<ext>     全身照（只放擁有的與 draft 的；沒擁有的不放，也不給剪影）
//	worn/<人設>.<ext>、.mp4     現在穿的那張（沒有 mp4 就沒有 worn 的 mp4）
//	<n>+k.mp4、worn/<人設>+k.mp4 同張的第 k 支動態（k ≥ 2，DEV-125；編號連續，第 1 支不帶 +1）
//	o/<人設>/<衣服>/h-<n>.<ext>、f/…/h-1.<ext>  隱藏版開著時的隱藏版圖（跟正常版並存，2026-11-13；只有一組，固定前綴 h-，DEV-160 S2）
//
// 不變條件：沒擁有的衣服，真圖與影片的檔案在公開資料夾裡不存在（開發家除外，CLO-27）。

// RealPath 是真圖的相對路徑。ext 含點。
func RealPath(persona, outfit string, n int, ext string) string {
	return fmt.Sprintf("o/%s/%s/%d%s", persona, outfit, n, ext)
}

// PhotoPath 是一張照片（含隱藏版）的相對路徑：正常版同 RealPath；隱藏版是 o/<人設>/<衣服>/h-<n><ext>，
// 跟正常版的同一張並存、不互相蓋掉（帶 h-，不會跟正常版的 <n> 撞名）。
func PhotoPath(persona, outfit string, hidden bool, n int, ext string) string {
	if !hidden {
		return RealPath(persona, outfit, n, ext)
	}
	return fmt.Sprintf("o/%s/%s/%s-%d%s", persona, outfit, rules.HiddenID, n, ext)
}

// FullPathOf 是全身照（含隱藏版）的相對路徑：正常版同 FullPath；隱藏版是 f/<人設>/<衣服>/h-1<ext>。
func FullPathOf(persona, outfit string, hidden bool, ext string) string {
	if !hidden {
		return FullPath(persona, outfit, ext)
	}
	return fmt.Sprintf("f/%s/%s/%s-1%s", persona, outfit, rules.HiddenID, ext)
}

// VideoSuffix：第 k 支動態的副檔名部分（含點）：第 1 支 ".mp4"，之後 "+2.mp4"。
func VideoSuffix(k int) string {
	if k <= 1 {
		return ".mp4"
	}
	return fmt.Sprintf("+%d.mp4", k)
}

// FullPath 是全身照的相對路徑。ext 含點。
func FullPath(persona, outfit, ext string) string {
	return fmt.Sprintf("f/%s/%s/1%s", persona, outfit, ext)
}

// SilhouettePath 是剪影的相對路徑。
func SilhouettePath(persona, outfit string) string {
	return fmt.Sprintf("s/%s/%s/1.png", persona, outfit)
}

// WornPath 是現在穿的那張的相對路徑。
func WornPath(persona, ext string) string { return "worn/" + persona + ext }

// URL 是相對路徑的網址。
func URL(rel string) string { return URLPrefix + rel }

// OutfitInput 是算公開清單時一套衣服要知道的事。
type OutfitInput struct {
	Persona, Outfit string
	State           string
	Owned           bool
	Photos          []Photo // 照片紀錄的半身照：一般版照 order 排，隱藏版開著時接在後面（Hidden）
	Full            Photo   // 全身照；Src 空＝沒有
	FullHidden      Photo   // 隱藏版開著時這套的隱藏版全身照；跟正常版的全身照並存
	// Preview：這個家是開發家（CLO-27），沒兌換的衣服也照擁有的給真圖。正式家永遠是 false。
	Preview bool
}

// Wearing 是某位角色現在穿的那一張。
type Wearing struct {
	Persona string
	Outfit  string
	Photo   Photo
}

// Revealed：這套衣服的真圖可以公開嗎？擁有的與草稿可以；支援 edit 的家（Preview，開發家全開不遮，CLO-27）沒兌換的也可以，
// 下架的也一樣（「修改」分頁要看得到每一張，DEV-160 審查 S3b；展示間照舊不列下架又沒擁有的，view.go）。
// 一般家下架而且沒擁有的照舊什麼都不放。
func Revealed(o OutfitInput) bool {
	return o.Owned || o.State == rules.StateDraft || o.Preview
}

// BuildPlan 算出公開資料夾「應該存在的檔案」的完整清單（照片從照片紀錄來，順序是 order、影片是 clip，DEV-160 S2）。
func BuildPlan(outfits []OutfitInput, worn []Wearing) Plan {
	plan := Plan{}
	revealed := map[string]bool{}
	for _, o := range outfits {
		if !rules.ValidPersona(o.Persona) || !rules.ValidOutfit(o.Outfit) {
			continue
		}
		switch {
		case Revealed(o):
			revealed[o.Persona+"/"+o.Outfit] = true
			if o.Full.Src != "" {
				plan[FullPath(o.Persona, o.Outfit, o.Full.Ext)] = Entry{Src: o.Full.Src}
			}
			if o.FullHidden.Src != "" {
				plan[FullPathOf(o.Persona, o.Outfit, true, o.FullHidden.Ext)] = Entry{Src: o.FullHidden.Src}
			}
			for _, p := range o.Photos {
				plan[PhotoPath(o.Persona, o.Outfit, p.Hidden, p.N, p.Ext)] = Entry{Src: p.Src}
				for i, v := range p.Videos {
					plan[PhotoPath(o.Persona, o.Outfit, p.Hidden, p.N, VideoSuffix(i+1))] = Entry{Src: v}
				}
			}
		case o.State == rules.StateRetired:
			// 下架而且沒擁有：什麼都不放。
		default:
			// 剪影只用一般版的第 1 張（沒擁有的本來就不帶隱藏版）。
			for _, p := range o.Photos {
				if !p.Hidden {
					plan[SilhouettePath(o.Persona, o.Outfit)] = Entry{Src: p.Src, Silhouette: true}
					break
				}
			}
		}
	}
	for _, w := range worn {
		// 防禦：現穿的一定是公開得了的衣服，不然不放。
		if !rules.ValidPersona(w.Persona) || !revealed[w.Persona+"/"+w.Outfit] || w.Photo.Src == "" {
			continue
		}
		plan[WornPath(w.Persona, strings.ToLower(w.Photo.Ext))] = Entry{Src: w.Photo.Src}
		for i, v := range w.Photo.Videos {
			plan[WornPath(w.Persona, VideoSuffix(i+1))] = Entry{Src: v}
		}
	}
	return plan
}
