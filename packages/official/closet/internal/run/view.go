package run

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/packages/official/closet/internal/photomigrate"
	"github.com/minglight/evergenie/packages/official/closet/internal/pub"
	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
	"github.com/minglight/evergenie/sdk"
)

// person 是一位角色的檢視（closet.person 的內容，還沒填照片網址）。
type person struct {
	persona string
	name    string
	agent   string
	worn    string
	wornN   int // 第幾張，1 起算；0＝沒有圖
	// picked：現在這套是主人指定穿的（現在穿這套，CLO-30）；wornUntil 是指定穿到什麼時候（RFC 3339），沒指定是空字串。
	picked    bool
	wornUntil string
	rotation  []string
	outfits   []personOutfit
	hidden    bool // 這個家有隱藏版（那一組有任何一筆設定；自己的隱藏版開關關著時主人自己的設定不算），只用來列狀態
	// ownerHidden：這位的照片裡「自己的隱藏版」的編號（hidden 而且 photomigrate.OwnerHidden，CLO-18；不管開關開沒開都列）。
	// 角色頁「修改」分頁不給把它們改成一般版：改了就不再跟著「自己的隱藏版」開關、會跟一般版一起公開與匯出（CLO-35）。
	ownerHidden []string
	// hiddenOff：這位的隱藏版照片裡，因為隱藏版開關關著（不在時段、沒打開；自己的隱藏版另看「自己的隱藏版」開關）這一輪不公開的編號。
	// 「修改」分頁照它整張不列、也不出「顯示隱藏版」（作者 2026-11-15：開關關著不留框，CLO-35），不自己猜開關。
	hiddenOff []string
}

type personOutfit struct {
	rules.Outfit
	owned bool
	shown bool // 真圖給不給（pub.Revealed）：擁有的、設計中的；開發家沒兌換的也給（CLO-27）
	// unlisted：開發家的下架又沒擁有的衣服——展示間不列（CLO-4），只把照片網址給「修改」分頁（photo_urls，DEV-160 審查 S3b）。
	unlisted bool
	rotating bool
	// 可以升成官方（rules.Promotable，CLO-29）；正式家永遠是 false。
	promotable bool
	photos     []pub.Photo   // 一般版，隱藏版開著時接在後面（Hidden）
	full       pub.Photo     // 一般版全身照
	fullHid    pub.Photo     // 隱藏版開著時的隱藏版全身照；跟 full 並存
	adopted    []string      // 已採用的那幾張（照片紀錄的 adopted，DEV-160 S2）；只有設計中的會寫出去
	status     *outfitStatus // 每張照片有沒有圖、幾支動畫（只算擁有的與草稿的；沒擁有的是 nil）
}

// slotState 是一張照片在某一版（一般版或隱藏版）有沒有圖、做了幾支動畫。
type slotState struct {
	img    bool
	videos int
}

// outfitStatus：一套衣服每張照片、全身照在一般版與隱藏版的狀態（CLO-21，DEV-143；DEV-160 S2 起隱藏版只有一組 h）。只有布林與數字，不帶網址。
type outfitStatus struct {
	photos map[int]map[bool]slotState // 第 N 張 → 是不是隱藏版 → 狀態
	full   map[bool]bool
	max    int
}

type planInputs struct {
	outfits []pub.OutfitInput
	worn    []pub.Wearing
}

type view struct {
	people []person
	plan   planInputs
}

// hiddenState 是這一輪隱藏版的狀態：exists＝這個家有隱藏版（列狀態用）；open＝現在開著（開著才列隱藏版的照片）；
// adultOn＝自己的隱藏版開關開著（主人自己的隱藏版照片才算）。
type hiddenState struct{ exists, open, adultOn bool }

// buildView 算出每位角色的檢視與公開資料夾的清單（純計算＋確認檔在不在，不寫任何東西）。
// photos 是每套衣服的照片紀錄（fillPhotos）。隱藏版開著時，擁有的與設計中的衣服有隱藏版的照片，就接在一般版後面一起列（不取代，2026-11-13 作者）。
// preview 是「這個家是開發家」（CLO-27）：沒兌換的上架衣服也照擁有的給真圖、狀態與開著的隱藏版；擁有與輪播照舊只算真的擁有的。
func (s *Service) buildView(catalog map[string]rules.Outfit, unlocked map[string]bool, rotations map[string][]string, picks map[string]pick, seats map[string]string, photos map[string][]photoRec, hs hiddenState, preview bool, now time.Time) view {
	byPersona := map[string][]rules.Outfit{}
	for _, o := range catalog {
		byPersona[o.Persona] = append(byPersona[o.Persona], o)
	}
	personas := make([]string, 0, len(byPersona))
	for p := range byPersona {
		personas = append(personas, p)
	}
	sort.Strings(personas)

	hour := rules.Hour(now.Unix())
	var v view
	for _, persona := range personas {
		list := byPersona[persona]
		rules.SortOutfits(list)
		p := person{persona: persona, name: s.personaName(persona), agent: seats[persona], hidden: hs.exists}
		official := preview && s.officialPersona(persona) // 只有開發家才用得到，正式家不去看檔案
		arranged := map[string]outfitPhotos{}
		listed := map[string][]pub.Photo{}
		shown := map[string]bool{}
		statuses := map[string]*outfitStatus{}
		var owned []rules.Outfit
		for _, o := range list {
			for _, r := range photos[o.Record] {
				owner := r.hidden && photomigrate.OwnerHidden(r.source, r.file)
				if owner {
					p.ownerHidden = append(p.ownerHidden, r.id)
				}
				// 跟 arrange 與下面接隱藏版的規則同一套：隱藏版要開著；自己的隱藏版還要「自己的隱藏版」開關開著。
				if r.hidden && (!hs.open || (owner && !hs.adultOn)) {
					p.hiddenOff = append(p.hiddenOff, r.id)
				}
			}
			ap := s.arrange(photos[o.Record], hs.adultOn)
			isOwned := rules.Owned(o, unlocked)
			// 沒擁有的不看隱藏版：它的剪影照舊用一般版，隱藏版的圖連檔名都不會進公開資料夾。
			// 開發家（preview）沒兌換的照擁有的算；隱藏版仍然只有開著才接上。
			shown[o.Outfit] = pub.Revealed(pub.OutfitInput{State: o.State, Owned: isOwned, Preview: preview})
			ph := ap.std
			if shown[o.Outfit] {
				statuses[o.Outfit] = statusOf(ap)
				if hs.open {
					ph = append(append([]pub.Photo{}, ap.std...), ap.hid...)
				} else {
					ap.fullHid = pub.Photo{}
				}
			} else {
				ap.fullHid = pub.Photo{}
			}
			arranged[o.Outfit] = ap
			listed[o.Outfit] = ph
			if isOwned {
				owned = append(owned, o)
			}
			v.plan.outfits = append(v.plan.outfits, pub.OutfitInput{Persona: persona, Outfit: o.Outfit, State: o.State, Owned: isOwned, Preview: preview, Photos: ph, Full: ap.full, FullHidden: ap.fullHid})
		}
		p.rotation = rules.EffectiveRotation(rotations[persona], owned)
		inRotation := map[string]bool{}
		for _, id := range p.rotation {
			inRotation[id] = true
		}
		// 現在穿這套（CLO-30）：指定的那套真的擁有、時間還沒到就穿它；否則照輪流穿的清單。清單本身不變。
		if pk := picks[persona]; rules.Picked(pk.wear, pk.until, now.Unix(), owned) {
			p.worn, p.picked = pk.wear, true
			p.wornUntil = time.Unix(pk.until, 0).In(now.Location()).Format(time.RFC3339)
		} else if i := rules.WearingIndex(hour, len(p.rotation)); i >= 0 {
			p.worn = p.rotation[i]
		}
		if p.worn != "" {
			ph := listed[p.worn]
			if j := rules.PhotoIndex(hour, len(p.rotation), len(ph)); j >= 0 {
				p.wornN = j + 1
				v.plan.worn = append(v.plan.worn, pub.Wearing{Persona: persona, Outfit: p.worn, Photo: ph[j]})
			}
		}
		for _, o := range list {
			isOwned := rules.Owned(o, unlocked)
			unlisted := o.State == rules.StateRetired && !isOwned
			if unlisted && !shown[o.Outfit] {
				continue // 下架而且沒擁有：看不到（開發家只把照片網址給「修改」分頁，unlisted）
			}
			ap := arranged[o.Outfit]
			p.outfits = append(p.outfits, personOutfit{Outfit: o, owned: isOwned, shown: shown[o.Outfit], unlisted: unlisted, rotating: isOwned && inRotation[o.Outfit],
				promotable: rules.Promotable(preview, official, o, ap.allHidden), photos: listed[o.Outfit], full: ap.full, fullHid: ap.fullHid,
				adopted: ap.adopted, status: statuses[o.Outfit]})
		}
		v.people = append(v.people, p)
	}
	return v
}

// statusOf 算一套衣服每張照片、全身照在一般版與隱藏版有沒有（CLO-21）。隱藏版不管現在開沒開都算（主人看得到缺哪張）。
func statusOf(ap outfitPhotos) *outfitStatus {
	st := &outfitStatus{photos: map[int]map[bool]slotState{}, full: map[bool]bool{false: ap.full.Src != "", true: ap.fullHid.Src != ""}}
	for _, ph := range append(append([]pub.Photo{}, ap.std...), ap.hid...) {
		if st.photos[ph.N] == nil {
			st.photos[ph.N] = map[bool]slotState{}
		}
		st.photos[ph.N][ph.Hidden] = slotState{img: true, videos: len(ph.Videos)}
		if ph.N > st.max {
			st.max = ph.N
		}
	}
	return st
}

// fields 是 status 寫進 closet.person 的樣子：photos 依張數排（1..max，中間缺的也列），每張 std＋hidden（隱藏版只有一組，鍵是 h）。
func (st *outfitStatus) fields(hidden bool) map[string]any {
	cell := func(c slotState) map[string]any { return map[string]any{"img": c.img, "videos": c.videos} }
	photos := []any{}
	for n := 1; n <= st.max; n++ {
		row := st.photos[n]
		h := map[string]any{}
		if hidden {
			h[rules.HiddenID] = cell(row[true])
		}
		photos = append(photos, map[string]any{"n": n, "std": cell(row[false]), "hidden": h})
	}
	fh := map[string]any{}
	if hidden {
		fh[rules.HiddenID] = st.full[true]
	}
	return map[string]any{"photos": photos, "full": map[string]any{"std": st.full[false], "hidden": fh}}
}

// withVariant 給隱藏版的圖網址加 ?v=h：檔名本來就不同，這個是讓頁面（與舊資料的判斷）一眼看出是隱藏版。
func withVariant(url string, hidden bool) string {
	if !hidden {
		return url
	}
	return url + "?v=" + rules.HiddenID
}

func variantOf(hidden bool) string {
	if hidden {
		return rules.HiddenID
	}
	return ""
}

// fields 是 closet.person 的欄位；res 是對帳的結果（沒做出來的檔案不給網址）。
// photo_urls：照片編號 → 公開網址（這一輪真的放進公開資料夾的每張半身、每支影片、全身照；隱藏版照 h-<n>），
// 「修改」分頁照編號找圖，不再照位置猜（DEV-160，CLO-35）。沒擁有的衣服只有剪影、不列；開發家的下架又沒擁有的只列網址（unlisted）。
// hidden_off：這一輪因為隱藏版開關關著不公開的隱藏版照片編號（「修改」分頁照它不列、不留框）。
func (p person) fields(res pub.Result) map[string]any {
	outfits := []any{}
	urls := map[string]any{}
	link := func(id string, url string) {
		if id != "" && url != "" {
			urls[id] = url
		}
	}
	for _, o := range p.outfits {
		photos := []any{}
		full := "" // 全身照只給擁有的與草稿（開發家沒兌換的也給，CLO-27）；其他沒擁有的不給網址（也不給剪影）
		fullHidden, fullVariant := "", ""
		fullID, fullHiddenID := "", ""
		switch {
		case o.shown:
			for _, ph := range o.photos {
				real := pub.PhotoPath(p.persona, o.Outfit.Outfit, ph.Hidden, ph.N, ph.Ext)
				if !res.Present[real] {
					continue
				}
				// variant：隱藏版是 h（只有一組），一般版空字串；隱藏版跟一般版的同一張（同一個 n）並列。
				// id、video_ids 是 closet.photo 編號（DEV-160 S2；以前給展示間「設計中」寫採用與意見，S5b 起頁面不讀，先留著不改紀錄格式，CLO-22）。
				item := map[string]any{"n": ph.N, "id": ph.ID, "variant": variantOf(ph.Hidden), "url": withVariant(pub.URL(real), ph.Hidden), "real": true, "video": "", "videos": []any{}, "video_ids": []any{}}
				link(ph.ID, item["url"].(string))
				// 動態：只列做出來的，第 1 支也放進 videos（DEV-125）；video 仍是第 1 支，舊頁面照用。
				var videos, ids []any
				for i := range ph.Videos {
					if mp4 := pub.PhotoPath(p.persona, o.Outfit.Outfit, ph.Hidden, ph.N, pub.VideoSuffix(i+1)); res.Present[mp4] {
						videos = append(videos, withVariant(pub.URL(mp4), ph.Hidden))
						ids = append(ids, ph.VideoIDs[i])
						link(ph.VideoIDs[i], withVariant(pub.URL(mp4), ph.Hidden))
					}
				}
				if len(videos) > 0 {
					item["video"] = videos[0]
					item["videos"] = videos
					item["video_ids"] = ids
				}
				photos = append(photos, item)
			}
			if o.full.Src != "" {
				if rel := pub.FullPath(p.persona, o.Outfit.Outfit, o.full.Ext); res.Present[rel] {
					full, fullID = pub.URL(rel), o.full.ID
					link(fullID, full)
				}
			}
			if o.fullHid.Src != "" {
				if rel := pub.FullPathOf(p.persona, o.Outfit.Outfit, true, o.fullHid.Ext); res.Present[rel] {
					fullHidden, fullVariant, fullHiddenID = withVariant(pub.URL(rel), true), rules.HiddenID, o.fullHid.ID
					link(fullHiddenID, fullHidden)
				}
			}
		case len(o.photos) > 0:
			// 沒擁有：只有一項（不透露照片張數）；剪影做不出來（WEBP、壞檔）時 url 空，頁面自己畫灰色人形。
			url := ""
			if sil := pub.SilhouettePath(p.persona, o.Outfit.Outfit); res.Present[sil] {
				url = pub.URL(sil)
			}
			photos = append(photos, map[string]any{"url": url, "real": false, "video": "", "videos": []any{}})
		}
		item := map[string]any{
			"record": o.Record, "outfit": o.Outfit.Outfit, "name": o.Name, "desc": o.Desc, "cost": o.Cost, "state": o.State,
			"owned": o.owned, "rotating": o.rotating, "photos": photos, "full": full, "source": o.Source,
			"full_hidden": fullHidden, "full_hidden_variant": fullVariant, "full_id": fullID, "full_hidden_id": fullHiddenID,
		}
		if o.status != nil {
			item["status"] = o.status.fields(p.hidden)
		}
		if o.shown && !o.owned && o.State != rules.StateDraft {
			// 沒兌換、但這個家是開發家所以照樣給真圖（CLO-27）；正式家不會有這個欄位。頁面照它改說明，不自己猜。
			item["preview"] = true
		}
		if o.promotable {
			// 可以升成官方（CLO-29）：只有開發家會有；展示間的篩選與 closet-mine 的按鈕都讀這個欄位。
			item["promotable"] = true
		}
		if o.unlisted {
			continue // 展示間不列（CLO-4）；照片網址上面已經放進 photo_urls 給「修改」分頁
		}
		if o.State == rules.StateDraft {
			// 設計中每張圖各自採用了沒（SEC-10；DEV-160 S2 起讀 closet.photo 的 adopted，key 寫法同頁面）；沒有也給空 list，頁面不必分有沒有。
			adopted := []any{}
			for _, k := range o.adopted {
				adopted = append(adopted, k)
			}
			item["adopted"] = adopted
		}
		outfits = append(outfits, item)
	}
	ownerHidden := []any{}
	sort.Strings(p.ownerHidden)
	for _, id := range p.ownerHidden {
		ownerHidden = append(ownerHidden, id)
	}
	hiddenOff := []any{}
	sort.Strings(p.hiddenOff)
	for _, id := range p.hiddenOff {
		hiddenOff = append(hiddenOff, id)
	}
	rotation := []any{}
	for _, id := range p.rotation {
		rotation = append(rotation, id)
	}
	hidden := []any{}
	if p.hidden {
		hidden = append(hidden, map[string]any{"id": rules.HiddenID, "name": rules.HiddenName})
	}
	return map[string]any{
		"persona": p.persona, "name": p.name, "agent": p.agent, "worn": p.worn, "worn_photo": p.wornN,
		"rotation": rotation, "outfits": outfits, "hidden": hidden,
		"picked": p.picked, "worn_until": p.wornUntil, "photo_urls": urls, "owner_hidden": ownerHidden, "hidden_off": hiddenOff,
	}
}

// tombstonePeople：目錄裡已經沒有任何衣服的角色，person 改寫成空的（outfits 空）。
// 不用封存：封存過的紀錄 vault 沒辦法還原，這位角色之後又有衣服就寫不進去了。
func (s *Service) tombstonePeople(ctx context.Context, v view, computedAt string) {
	recs, err := s.V.List(ctx, KindPerson, false)
	if err != nil {
		s.Log.Error("讀角色檢視失敗", "err", err)
		return
	}
	live := map[string]bool{}
	for _, p := range v.people {
		live[p.persona] = true
	}
	for _, r := range recs {
		if live[r.ID] {
			continue
		}
		empty := map[string]any{"persona": r.ID, "name": rules.Text(r.Fields["name"]), "worn_photo": 0, "rotation": []any{}, "outfits": []any{}}
		if err := s.writeIfChanged(ctx, KindPerson, r.ID, empty, computedAt); err != nil {
			s.Log.Error("清空角色檢視失敗", "persona", r.ID, "err", err)
		}
	}
}

// officialPersona：這個代號是不是官方角色（有官方角色卡；主人有同代號的卡蓋過跟著程式的那張時不算，PRS-27）。
// 跟 closet-mine 升成官方時的檢查同一個入口（sdk.OfficialCardPath）。
func (s *Service) officialPersona(persona string) bool {
	card, ok := sdk.OfficialCardPath(s.Home, persona)
	if !ok {
		return false
	}
	info, err := os.Lstat(card)
	return err == nil && info.Mode().IsRegular()
}

// personaName 取人設的顯示名：官方角色卡（找官方卡的共用入口 sdk.OfficialCardPath：官方角色資料夾先，沒有才用跟著程式的那張，PRS-25；
// 主人有同代號的卡時跟著程式的那張不算，agents 的 PRS-27），找不到再找 <家>/personas/mine/<人設>.md；檔頭的 name，YAML（---）與 TOML（+++）都認；
// 都沒有就用人設代號。
func (s *Service) personaName(persona string) string {
	official, _ := sdk.OfficialCardPath(s.Home, persona)
	for _, p := range []string{official, filepath.Join(s.Home, "personas", "mine", persona+".md")} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if name := HeaderName(string(data)); name != "" {
			return name
		}
	}
	return persona
}

// HeaderName 在檔頭（--- 或 +++ 包起來的區塊）逐行找 `name:` 或 `name =`，去引號；找不到回空字串。
func HeaderName(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	delim := strings.TrimSpace(lines[0])
	if delim != "---" && delim != "+++" {
		return ""
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == delim {
			return ""
		}
		rest, ok := strings.CutPrefix(line, "name")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "" || (rest[0] != ':' && rest[0] != '=') {
			continue
		}
		val := strings.TrimSpace(rest[1:])
		if i := strings.Index(val, " #"); i >= 0 && !strings.HasPrefix(val, `"`) && !strings.HasPrefix(val, "'") {
			val = strings.TrimSpace(val[:i])
		}
		if strings.HasPrefix(val, `"`) {
			if u, err := strconv.Unquote(val); err == nil { // 雙引號字串：跳脫（\" \\ \n）還原，YAML 與 TOML 共用這幾種寫法
				return strings.TrimSpace(u)
			}
		}
		val = strings.Trim(val, `"'`)
		return strings.TrimSpace(val)
	}
	return ""
}
