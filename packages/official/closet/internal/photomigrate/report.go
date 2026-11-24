package photomigrate

import (
	"fmt"
	"io"
	"sort"
)

// Report 印預演結果（白話，給 Fill 的結果）：每位角色每套要補幾筆（半身／全身／影片、其中隱藏版、已採用）、已經登記的幾筆、
// 舊採用對不到的字串、沒登記的檔與原因。
func Report(w io.Writer, res Result) {
	byPersona := map[string][]OutfitResult{}
	var personas []string
	for _, o := range res.Outfits {
		if _, ok := byPersona[o.Persona]; !ok {
			personas = append(personas, o.Persona)
		}
		byPersona[o.Persona] = append(byPersona[o.Persona], o)
	}
	sort.Strings(personas)
	total, kept := 0, 0
	types := map[string]int{}
	hidden, adopted := 0, 0
	for _, p := range personas {
		sum := 0
		for _, o := range byPersona[p] {
			sum += len(o.Photos)
		}
		fmt.Fprintf(w, "%s：%d 筆\n", p, sum)
		for _, o := range byPersona[p] {
			c := map[string]int{}
			h, a := 0, 0
			for _, ph := range o.Photos {
				c[ph.Type]++
				types[ph.Type]++
				if ph.Hidden {
					h++
				}
				if ph.Adopted {
					a++
				}
			}
			hidden += h
			adopted += a
			total += len(o.Photos)
			kept += len(o.Kept)
			line := fmt.Sprintf("  %s：%d 筆（半身 %d、全身 %d、影片 %d；隱藏版 %d；已採用 %d）", o.Record, len(o.Photos), c[TypeHalf], c[TypeFull], c[TypeVideo], h, a)
			if len(o.Kept) > 0 {
				line += fmt.Sprintf("，已經有 %d 筆不重寫", len(o.Kept))
			}
			fmt.Fprintln(w, line)
			for _, k := range o.Unmatched {
				fmt.Fprintf(w, "    舊的採用「%s」對不到任何一張，沒有轉\n", k)
			}
		}
	}
	fmt.Fprintf(w, "合計：要新寫 %d 筆（半身 %d、全身 %d、影片 %d；隱藏版 %d；已採用 %d），已經有 %d 筆\n", total, types[TypeHalf], types[TypeFull], types[TypeVideo], hidden, adopted, kept)
	fmt.Fprintf(w, "沒登記的檔：%d 個（不碰，只列出）\n", len(res.Skipped))
	for _, s := range res.Skipped {
		fmt.Fprintf(w, "  %s：%s\n", s.Path, s.Reason)
	}
}
