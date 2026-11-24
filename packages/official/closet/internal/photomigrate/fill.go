package photomigrate

import (
	"fmt"

	"github.com/minglight/evergenie/sdk"
)

// FileKey 是一個檔的身分：放在哪一邊（official／mine）＋相對路徑。比對「這個檔有沒有紀錄指著」用。
func FileKey(source, file string) string { return source + "\x00" + file }

// Fill 從轉換結果（Plan）挑出這一輪要補寫的照片（DEV-160 S2，衣櫃的程式每一輪都跑；預演入口也用它，結果一樣）：
//   - 檔案（source＋file）已經有沒封存的紀錄指著：已經登記了，不補。
//   - 編號被沒封存的紀錄用掉（那一格已經有別的檔）：不補，不重複登記同一格。
//   - 編號只被封存的紀錄用過（退路：主人把轉錯的紀錄封存了，要重轉）：封存過的編號不能再用，改用 <編號>-r2、-r3…；
//     配這張的影片的 of 跟著換。
//   - skip 回 true 的不補（例：官方層在家外時官方的照片，紀錄要跟角色包走，不寫進家裡）。
//
// have 是 vault 裡現有的 closet.photo（含封存的）。回的 Result 每套的 Photos 只剩要新增的、Kept 是已經登記的；
// 不碰任何檔；同樣的輸入重算結果一樣（冪等）。
func Fill(res Result, have []sdk.Record, skip func(Photo) bool) Result {
	live, archived, files := map[string]bool{}, map[string]bool{}, map[string]string{} // files：檔 → 指著它的紀錄編號
	for _, r := range have {
		if r.Archived {
			archived[r.ID] = true
			continue
		}
		live[r.ID] = true
		src, _ := r.Fields["source"].(string)
		file, _ := r.Fields["file"].(string)
		if file != "" {
			files[FileKey(src, file)] = r.ID
		}
	}
	used := map[string]bool{}
	out := Result{Skipped: res.Skipped}
	for _, o := range res.Outfits {
		next := OutfitResult{Record: o.Record, Persona: o.Persona, Unmatched: o.Unmatched}
		renamed := map[string]string{} // 轉換器的編號 → 這次真的用的編號
		for _, p := range o.Photos {
			if skip != nil && skip(p) {
				continue
			}
			if at, ok := files[FileKey(p.Source, p.File)]; ok {
				renamed[p.ID] = at // 已經登記了（可能是 -r2 那筆）：之後新加的影片配到它
				next.Kept = append(next.Kept, p.ID)
				continue
			}
			if live[p.ID] {
				next.Kept = append(next.Kept, p.ID)
				continue
			}
			id := p.ID
			for k := 2; archived[id] || live[id] || used[id]; k++ {
				id = fmt.Sprintf("%s-r%d", p.ID, k)
			}
			renamed[p.ID] = id
			if p.Type == TypeVideo {
				if to, ok := renamed[p.Of]; ok {
					p.Of = to
				}
			}
			p.ID = id
			used[id] = true
			next.Photos = append(next.Photos, p)
		}
		out.Outfits = append(out.Outfits, next)
	}
	return out
}

// UnadoptLate：Fill 的結果裡，那套衣服已經有照片紀錄（沒封存的或封存的都算：轉換過了）之後才要補登記的照片，一律改成還沒採用
// （之後才放進來的舊命名檔主人沒看過，DEV-160 審查 S6）；這套一筆紀錄都沒有的（第一次轉換）照 adopt 的規則不動。
// 只給支援 edit 的家用（衣櫃的 fillPhotos）：一般家沒有採用這件事。回新的 Result，不改傳進來的。
func UnadoptLate(res Result, have []sdk.Record) Result {
	return mapLate(res, have, func(o OutfitResult) (OutfitResult, bool) {
		photos := make([]Photo, len(o.Photos))
		copy(photos, o.Photos)
		for i := range photos {
			photos[i].Adopted = false
		}
		o.Photos = photos
		return o, true
	})
}

// SkipLate：同 UnadoptLate 認「之後才補登記的」，但這一輪先不補（拿掉）——衣櫃問不到核心、不知道這個家支援不支援 edit 時用
// （不知道該算採用還是沒採用，不先寫一筆較寬的已採用，憲法第 10 條；下一輪問得到再補）。第一次轉換照舊。不改傳進來的。
func SkipLate(res Result, have []sdk.Record) Result {
	return mapLate(res, have, func(o OutfitResult) (OutfitResult, bool) { return o, false })
}

// mapLate：「這套已經有照片紀錄、這一輪又要補」的每一套交給 f（回 false 就整套這一輪不補），其他照原樣。回新的 Result。
func mapLate(res Result, have []sdk.Record, f func(OutfitResult) (OutfitResult, bool)) Result {
	had := map[string]bool{}
	for _, r := range have {
		if o, _ := r.Fields["outfit"].(string); o != "" {
			had[o] = true
		}
	}
	out := Result{Skipped: res.Skipped, Outfits: make([]OutfitResult, 0, len(res.Outfits))}
	for _, o := range res.Outfits {
		if had[o.Record] && len(o.Photos) > 0 {
			var keep bool
			if o, keep = f(o); !keep {
				continue
			}
		}
		out.Outfits = append(out.Outfits, o)
	}
	return out
}
