package run

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/packages/official/closet/internal/photomigrate"
	"github.com/minglight/evergenie/packages/official/closet/internal/pub"
	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
	"github.com/minglight/evergenie/sdk"
)

// 照片紀錄（closet.photo，DEV-160 S2）：衣櫃只照紀錄找圖，檔名不再帶意思。每一輪先用轉換器把還沒有紀錄指著的舊圖補登記
// （fillPhotos），再讀紀錄排好每套衣服的照片（arrange）。

// photoRec 是一筆讀懂的 closet.photo。
type photoRec struct {
	id, persona, outfit, typ string
	order, clip              int
	of                       string
	hidden, adopted          bool
	source, file             string
}

func parsePhoto(id string, f map[string]any) (photoRec, error) {
	p := photoRec{id: id, persona: rules.Text(f["persona"]), outfit: rules.Text(f["outfit"]), typ: rules.Text(f["type"]),
		of: rules.Text(f["of"]), source: rules.Text(f["source"]), file: rules.Text(f["file"])}
	p.order, _ = rules.Int(f["order"])
	p.clip, _ = rules.Int(f["clip"])
	p.hidden, _ = f["hidden"].(bool)
	p.adopted, _ = f["adopted"].(bool)
	switch {
	case p.outfit == "":
		return p, fmt.Errorf("沒有 outfit")
	case p.typ != photomigrate.TypeHalf && p.typ != photomigrate.TypeFull && p.typ != photomigrate.TypeVideo:
		return p, fmt.Errorf("type「%s」不認得", p.typ)
	case p.source != rules.SourceOfficial && p.source != rules.SourceMine:
		return p, fmt.Errorf("source「%s」不認得", p.source)
	case !validFile(p.file):
		return p, fmt.Errorf("file「%s」要是相對路徑（用 /，不能有 . 開頭的段）", p.file)
	case p.typ == photomigrate.TypeVideo && p.of == "":
		return p, fmt.Errorf("影片沒有 of")
	}
	return p, nil
}

// validFile：相對路徑、用 / 分段、沒有空段與 . 開頭的段（含 ..）、沒有反斜線（跟隱藏版 dir 同一套規則）。
func validFile(file string) bool { return rules.ValidVariantDir(file) }

// staticExt 是照片（半身、全身）認的副檔名；影片只認 .mp4。
var staticExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true}

// ownerRoot 是照片 file 相對的位置：official＝官方角色資料夾（第 2 版在家外），mine＝<家>/personas/mine。
func (s *Service) ownerRoot(source string) string {
	if source == rules.SourceMine {
		return filepath.Join(s.Home, "personas", "mine")
	}
	return sdk.OfficialPersonasDir(s.Home)
}

// officialWritable：官方照片的紀錄可以寫進這個家嗎——官方層就是家（官方角色在 <家>/personas/：第 1 版、
// official_personas = "home" 的開發家）才可以。官方層在家外時紀錄屬於官方層、跟角色包走，寫進家裡會蓋住官方層（VLT-19）。
func (s *Service) officialWritable() bool {
	return filepath.Clean(sdk.OfficialPersonasDir(s.Home)) == filepath.Join(s.Home, "personas")
}

// SkipOfficial 是補登記時不寫的照片：官方層不是家（writable＝false）時官方的照片（紀錄屬於官方層、跟角色包走）。
// 預演入口（cmd/closet-photos）用同一個，結果才會跟服務一樣。
func SkipOfficial(writable bool) func(photomigrate.Photo) bool {
	return func(p photomigrate.Photo) bool { return !writable && p.Source == rules.SourceOfficial }
}

// resolve 是照片檔的實際位置；經過符號連結跑出擁有者位置的、不是一般檔或太大的回 false（記一次 log）。
func (s *Service) resolve(p photoRec) (string, bool) {
	root, err := filepath.EvalSymlinks(s.ownerRoot(p.source))
	if err != nil {
		return "", false
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(p.file)))
	if err != nil {
		return "", false // 檔不見了：當作沒有這張
	}
	if rel, err := filepath.Rel(root, full); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		s.logOnce("photo-escape:"+p.id, "照片的檔經過連結跑出擁有者位置，不用", "photo", p.id, "file", p.file)
		return "", false
	}
	if _, ok := pub.RegularFile(full); !ok {
		return "", false
	}
	return full, true
}

// fillPhotos 讀照片紀錄，先把還沒有紀錄指著的舊圖補登記（DEV-160 S2：服務啟動、角色包同步完、衣服目錄有變、定期重掃都會跑到；
// 只新增紀錄、不動任何檔，冪等）：
//   - source = mine 的照片（含主人放在官方衣服上的隱藏版圖）：寫進家裡。
//   - source = official 的照片：官方層就是家（officialWritable）才寫；官方層在家外時不寫（紀錄跟角色包走），
//     那套衣服在官方層還沒有任何官方照片紀錄（舊包）就每一輪用轉換器在記憶體裡算出來用，等新包帶紀錄來就用包的。
//
// 退路：封存補錯的紀錄，下一輪會用新的編號（-r2…）重補。回每套衣服（closet.outfit 編號）的照片紀錄（沒封存的、讀得懂的）。
// edit：這個家支援 edit；unsure：這一輪問不到核心（能力照上一輪），之後才補登記的先不補（SkipLate）。
func (s *Service) fillPhotos(ctx context.Context, catalog map[string]rules.Outfit, variants []rules.Variant, edit, unsure bool) (map[string][]photoRec, error) {
	have, err := s.V.List(ctx, KindPhoto, true)
	if err != nil {
		return nil, fmt.Errorf("讀照片紀錄失敗：%w", err)
	}
	outfits := make([]rules.Outfit, 0, len(catalog))
	for _, o := range catalog {
		outfits = append(outfits, o)
	}
	res := photomigrate.Plan(photomigrate.Input{Outfits: outfits, Variants: variants,
		OfficialRoot: s.ownerRoot(rules.SourceOfficial), MineRoot: s.ownerRoot(rules.SourceMine)})
	writable := s.officialWritable()
	var add []photomigrate.Photo
	filled := photomigrate.Fill(res, have, SkipOfficial(writable))
	switch {
	case edit:
		// 支援 edit 的家：這套已經有照片紀錄之後才補登記的（之後才放進來的舊命名檔）主人沒看過，一律還沒採用（DEV-160 審查 S6）；
		// 第一次轉換（這套一筆都沒有）照舊的採用規則。
		filled = photomigrate.UnadoptLate(filled, have)
	case unsure:
		// 問不到核心、上一輪也沒有 edit：不知道該不該算採用，之後才補的這一輪先不補（不寫一筆較寬的已採用，憲法第 10 條）。
		filled = photomigrate.SkipLate(filled, have)
	}
	for _, o := range filled.Outfits {
		add = append(add, o.Photos...)
	}
	for _, p := range add {
		rec := sdk.Record{Kind: KindPhoto, ID: p.ID, Fields: p.Fields()}
		if err := s.V.Put(ctx, rec, ""); err != nil {
			if e, ok := err.(*sdk.Error); ok && e.Code == sdk.ErrorCodeConflict {
				s.logOnce("photo-fill:"+p.ID, "照片紀錄的編號已被用掉，這張先不補", "photo", p.ID, "file", p.File)
				continue
			}
			return nil, fmt.Errorf("補照片紀錄失敗：%w", err)
		}
		have = append(have, rec)
	}
	out := map[string][]photoRec{}
	officialOf := map[string]bool{} // 有官方照片紀錄的衣服（官方層的或家裡的）
	for _, r := range have {
		if r.Archived {
			continue
		}
		p, err := parsePhoto(r.ID, r.Fields)
		if err != nil {
			s.logOnce("photo:"+r.ID+":"+err.Error(), "照片紀錄不合格，略過", "photo", r.ID, "err", err)
			continue
		}
		if p.source == rules.SourceOfficial {
			officialOf[p.outfit] = true
		}
		out[p.outfit] = append(out[p.outfit], p)
	}
	if !writable {
		for _, o := range res.Outfits {
			if officialOf[o.Record] {
				continue
			}
			for _, p := range o.Photos {
				if p.Source == rules.SourceOfficial {
					out[o.Record] = append(out[o.Record], photoRec{id: p.ID, persona: p.Persona, outfit: p.Outfit, typ: p.Type, order: p.Order,
						clip: p.Clip, of: p.Of, hidden: p.Hidden, adopted: p.Adopted, source: p.Source, file: p.File})
				}
			}
		}
	}
	return out, nil
}

// outfitPhotos 是一套衣服排好的照片（找得到檔的）。
type outfitPhotos struct {
	std, hid      []pub.Photo // 半身照：照 order 排（同一個 order 只留第一筆）
	full, fullHid pub.Photo   // 全身照：order 最小的那一張
	adopted       []string    // 已採用的那幾張（頁面的 key：half-<n>、video-<n>-<k>、full；隱藏版前面加 h:）
	allHidden     bool        // 有照片而且全部是隱藏版（不能升成官方）
}

// arrange 把一套衣服的照片紀錄排好：自己的隱藏版（hidden，而且 source = mine 或放在舊家的自己的隱藏版位置，
// photomigrate.OwnerHidden）只有開關開著（adultOn）才算。
// 一般版、隱藏版各自照 order（再照編號）排，同一個 order 只留第一筆；影片照 of 配到那張半身照、照 clip 排；最多 12 張、每張 6 支。
func (s *Service) arrange(recs []photoRec, adultOn bool) outfitPhotos {
	var out outfitPhotos
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].order != recs[j].order {
			return recs[i].order < recs[j].order
		}
		if recs[i].clip != recs[j].clip {
			return recs[i].clip < recs[j].clip
		}
		return recs[i].id < recs[j].id
	})
	var halves, fulls []photoRec
	videos := map[string][]photoRec{}
	nStd, nHidden := 0, 0
	for _, p := range recs {
		if p.hidden { // 不管開關：整套都是隱藏版的照片就不能升成官方
			nHidden++
		} else {
			nStd++
		}
		if p.hidden && !adultOn && photomigrate.OwnerHidden(p.source, p.file) {
			continue
		}
		switch p.typ {
		case photomigrate.TypeHalf:
			halves = append(halves, p)
		case photomigrate.TypeFull:
			fulls = append(fulls, p)
		case photomigrate.TypeVideo:
			videos[p.of] = append(videos[p.of], p)
		}
	}
	out.allHidden = nHidden > 0 && nStd == 0
	prefix := func(hidden bool) string {
		if hidden {
			return rules.HiddenID + ":"
		}
		return ""
	}
	seen := map[string]bool{}
	for _, h := range halves {
		list := &out.std
		if h.hidden {
			list = &out.hid
		}
		key := fmt.Sprint(h.hidden, h.order)
		if h.order < 1 || seen[key] || len(*list) >= pub.MaxPhotos {
			if h.order >= 1 && seen[key] {
				s.logOnce("photo-dup:"+h.id, "同一套衣服同一版有兩張同樣 order 的照片，只用第一張", "photo", h.id, "order", h.order)
			}
			continue
		}
		src, ok := s.resolve(h)
		if !ok || !staticExt[strings.ToLower(filepath.Ext(src))] {
			continue
		}
		seen[key] = true
		ph := pub.Photo{N: h.order, Src: src, Ext: strings.ToLower(filepath.Ext(src)), Hidden: h.hidden, ID: h.id}
		if h.adopted {
			out.adopted = append(out.adopted, fmt.Sprintf("%shalf-%d", prefix(h.hidden), h.order))
		}
		for _, v := range videos[h.id] {
			if len(ph.Videos) >= pub.MaxVideos || v.hidden != h.hidden || !strings.EqualFold(filepath.Ext(v.file), ".mp4") {
				continue
			}
			vsrc, ok := s.resolve(v)
			if !ok {
				continue
			}
			ph.Videos = append(ph.Videos, vsrc)
			ph.VideoIDs = append(ph.VideoIDs, v.id)
			if v.adopted {
				out.adopted = append(out.adopted, fmt.Sprintf("%svideo-%d-%d", prefix(h.hidden), h.order, len(ph.Videos)))
			}
		}
		*list = append(*list, ph)
	}
	for _, f := range fulls {
		slot := &out.full
		if f.hidden {
			slot = &out.fullHid
		}
		if slot.Src != "" {
			continue
		}
		src, ok := s.resolve(f)
		if !ok || !staticExt[strings.ToLower(filepath.Ext(src))] {
			continue
		}
		*slot = pub.Photo{N: 1, Src: src, Ext: strings.ToLower(filepath.Ext(src)), Hidden: f.hidden, ID: f.id}
		if f.adopted {
			out.adopted = append(out.adopted, prefix(f.hidden)+"full")
		}
	}
	return out
}
