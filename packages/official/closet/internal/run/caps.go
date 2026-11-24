package run

import (
	"context"
	"slices"
	"time"

	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
	"github.com/minglight/evergenie/sdk"
)

// 能力與動作插座（CLO-32、CLO-33，DEV-160 S4）。
const (
	KindCapabilities = "closet.capabilities"
	KindAction       = "closet.action"

	capabilitiesID = "main"
)

// capabilities 是這個家支援的能力：「是不是開發家」（home.toml [runtime] env = dev，Service.Dev 問核心的 system.get_mode）
// 整個衣櫃只在這裡問一次，別的地方（Go 與頁面）一律看能力（CLO-32、CLO-37）。
// known＝這一輪確定知道是哪種家（問到了核心）：只有 known 的一般家才自動上架設計中的我的衣服（CLO-38）。
// 問不到（Dev 回錯）時不改寫能力紀錄（write = false）：照上一輪寫的（讀不到上一輪就當沒有 edit，寧可遮）；
// 也不當成一般家放行較寬的路（憲法第 10 條）。沒接 Dev（測試、舊的組裝）＝沒有 edit，但不是 known。
func (s *Service) capabilities(ctx context.Context) (caps []string, known, write bool) {
	if s.Dev == nil {
		return rules.Capabilities(false), false, true
	}
	dev, err := s.Dev(ctx)
	if err == nil {
		s.mu.Lock()
		delete(s.logged, "capabilities:dev")
		s.mu.Unlock()
		return rules.WithEditTab(rules.Capabilities(dev), s.editTabOn(ctx)), true, true
	}
	s.logOnce("capabilities:dev", "問不到這個家是不是開發家，這一輪照上一輪的能力、不自動上架", "err", err)
	if cur, gerr := s.V.Get(ctx, KindCapabilities, capabilitiesID); gerr == nil && cur != nil && !cur.Archived {
		var prev []string
		for _, c := range rules.Strings(cur.Fields["supports"]) {
			if slices.Contains(rules.KnownCaps, c) {
				prev = append(prev, c)
			}
		}
		return prev, false, false
	}
	return rules.Capabilities(false), false, false
}

// editTabOn：「修改」分頁的開關（擴充點 closet.edit-tab）。沒接（測試、舊的組裝）或讀不到都當開：
// 這只決定分頁掛不掛，edit 的權限另外看，維持原本的樣子比較不會嚇到人。
func (s *Service) editTabOn(ctx context.Context) bool {
	if s.EditTab == nil {
		return true
	}
	return s.EditTab(ctx)
}

// writeCapabilities 寫 closet.capabilities/main：supports（這個家的能力）與 actions（登記好、這個家撐得起的動作）。
// 核心的 show_when（`cap:closet.<能力>`）與衣櫃的頁面都只讀這一筆（MNU-11）。內容沒變就不寫；封存了就重寫一筆（writeIfChanged）。
func (s *Service) writeCapabilities(ctx context.Context, caps []string, now time.Time) {
	actions := s.actions(ctx, caps)
	list := make([]any, 0, len(actions))
	for _, a := range actions {
		list = append(list, a.Fields)
	}
	supports := make([]any, 0, len(caps))
	for _, c := range caps {
		supports = append(supports, c)
	}
	fields := map[string]any{"supports": supports, "actions": list}
	if err := s.writeIfChanged(ctx, KindCapabilities, capabilitiesID, fields, now.Format(time.RFC3339)); err != nil {
		// 寫不成只記一次 log，不擋這一輪別的事（兌換照常）；核心與頁面讀不到就當沒有能力（fail closed）。
		s.logOnce("capabilities:"+err.Error(), "寫能力紀錄失敗", "err", err)
	}
}

// actions 讀別的套件登記的 closet.action，驗過、只留這個家撐得起的。
// 讀不到套件清單（Service.Packages 沒接或出錯）就一個都不列：不知道登記的套件還在不在，寧可不給鈕。
func (s *Service) actions(ctx context.Context, caps []string) []rules.Action {
	recs, err := s.V.List(ctx, KindAction, false)
	if err != nil {
		s.logOnce("actions:list", "讀登記的動作失敗，這一輪不列動作", "err", err)
		return nil
	}
	if len(recs) == 0 {
		return nil
	}
	if s.Packages == nil {
		return nil
	}
	pkgs, err := s.Packages(ctx)
	if err != nil {
		s.logOnce("actions:packages", "讀不到套件清單，這一輪不列登記的動作", "err", err)
		return nil
	}
	regs := registrants(pkgs)
	var parsed []rules.Action
	for _, r := range recs {
		a, err := rules.ParseAction(r.ID, r.Fields, regs)
		if err != nil {
			s.logOnce("action:"+r.ID+":"+err.Error(), "登記的動作不合格，不列", "action", r.ID, "err", err)
			continue
		}
		parsed = append(parsed, a)
	}
	return rules.SupportedActions(parsed, caps)
}

// registrants：看得到的套件（啟用而且沒被拒，跟看板列套件頁的規則一樣）、是不是官方套件、宣告的資料種類與欄位。
func registrants(pkgs []sdk.Package) map[string]rules.Registrant {
	out := map[string]rules.Registrant{}
	for _, p := range pkgs {
		if !p.Enabled || p.State == sdk.PackageStateRejected {
			continue
		}
		kinds := map[string]map[string]bool{}
		for _, d := range p.Data {
			fields := map[string]bool{}
			for _, f := range d.Fields {
				fields[f.Name] = true
			}
			kinds[d.Kind] = fields
		}
		out[p.ID] = rules.Registrant{ID: p.ID, Official: p.Tier == sdk.TierOfficial, Kinds: kinds}
	}
	return out
}
