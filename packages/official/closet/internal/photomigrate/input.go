package photomigrate

import (
	"fmt"
	"sort"

	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
	"github.com/minglight/evergenie/sdk"
)

// InputFromRecords 把 vault 讀出來的紀錄轉成 Input：outfits、variants 只收沒封存而且合格的（不合格的回在 bad，白話）。
// 已經有的 closet.photo 交給 Fill 對照。
func InputFromRecords(outfits, variants []sdk.Record, officialRoot, mineRoot string) (Input, []string) {
	in := Input{OfficialRoot: officialRoot, MineRoot: mineRoot}
	var bad []string
	for _, r := range outfits {
		if r.Archived {
			continue
		}
		o, err := rules.OutfitFromFields(r.ID, r.Fields)
		if err != nil {
			bad = append(bad, fmt.Sprintf("closet.outfit/%s：%v", r.ID, err))
			continue
		}
		in.Outfits = append(in.Outfits, o)
	}
	for _, r := range variants {
		if r.Archived {
			continue
		}
		v, err := rules.VariantFromFields(r.ID, r.Fields)
		if err != nil {
			bad = append(bad, fmt.Sprintf("closet.variant/%s：%v", r.ID, err))
			continue
		}
		in.Variants = append(in.Variants, v)
	}
	sort.Strings(bad)
	return in, bad
}
