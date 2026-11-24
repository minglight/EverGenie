package run

import (
	"context"

	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
)

// publishMineDrafts：一般家（確定不支援 edit：能力問得到、沒有 edit）沒有上架／下架（作者 2026-11-15，CLO-38）——
// 自己做的（source = mine）設計中衣服每一輪自動改成上架、它的照片標採用（秘書照開發家的做法送了 draft = true、以前留下的設計中都會這樣）。
// 先標照片、再改整套：中途寫不成就停在那套，下一輪接著做；做過的再做一次什麼都不寫（冪等）。問不到是哪種家時 Round 不叫它。
// 這一輪的檢視跟著改（catalog、photos 是 Round 的同一份）。不想要那套就刪掉（「衣服」分頁的「刪掉這套」，拿得回來）。
func (s *Service) publishMineDrafts(ctx context.Context, catalog map[string]rules.Outfit, photos map[string][]photoRec) {
	for key, o := range catalog {
		if o.Source != rules.SourceMine || o.State != rules.StateDraft {
			continue
		}
		ok := true
		list := photos[o.Record]
		for i, p := range list {
			if p.adopted || p.source != rules.SourceMine {
				continue
			}
			if err := s.V.Patch(ctx, KindPhoto, p.id, map[string]any{"adopted": true}); err != nil {
				s.logOnce("autopublish:"+p.id, "設計中的我的衣服自動上架：照片標採用沒寫成，下一輪再試", "outfit", o.Record, "photo", p.id, "err", err)
				ok = false
				break
			}
			list[i].adopted = true
		}
		if !ok {
			continue
		}
		if err := s.V.Patch(ctx, KindOutfit, o.Record, map[string]any{"state": rules.StateActive}); err != nil {
			s.logOnce("autopublish:"+o.Record, "設計中的我的衣服自動上架沒寫成，下一輪再試", "outfit", o.Record, "err", err)
			continue
		}
		o.State = rules.StateActive
		catalog[key] = o
	}
}
