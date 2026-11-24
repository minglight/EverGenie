package run

import (
	"bytes"
	"context"

	"github.com/minglight/evergenie/packages/official/closet-mine/internal/rules"
	"github.com/minglight/evergenie/packages/official/closet-mine/internal/store"
	"github.com/minglight/evergenie/sdk"
)

// 上傳（DEV-160 S3）：檔寫 personas/mine/photos/<照片編號>.<副檔名>（照片編號＝<衣服編號>--u<請求編號>，rules.PhotoIDFor；
// 新角色的第一張是角色本身的頭像 avatars/<代號>.<ext>，見 character），
// 再登記衣服與照片紀錄（closet.photo）；是誰的、哪一種、一般或隱藏版、第幾張都只記在紀錄上，不從檔名推。
// 每一步都可以重播：同一個請求的照片編號一樣、檔的位元組一樣，做過的步驟再做一次結果相同。

// baseName 是基本款的名字（新角色的第一套）。
const baseName = "基本款"

// adultOn：家層級的「自己的隱藏版」開關（衣櫃的擴充點 closet.adult）開著嗎；nil 或讀不到都當關。
func (s *Service) adultOn(ctx context.Context) bool { return s.Adult != nil && s.Adult(ctx) }

// hiddenGate：這次的照片會登記成隱藏版時，要開關開著、自己的隱藏版那一筆設定還在（adultVariantProblem）。
// 請求帶 adult 的在 handle 已經擋過開關；沒寫 adult、因為這套的照片全是隱藏版才算隱藏版的（rules.UploadHidden）在這裡擋，原因說清楚。
func (s *Service) hiddenGate(ctx context.Context, req rules.Request, hidden bool) (string, error) {
	if !hidden {
		return "", nil
	}
	if !s.adultOn(ctx) {
		if req.Adult {
			return rules.ReasonAdultOff, nil
		}
		return rules.ReasonAllHidden, nil
	}
	return s.adultVariantProblem(ctx)
}

// afterHidden：放了隱藏版的照片之後，第一次就建自己的隱藏版那一筆設定（closet.variant/adult）。
func (s *Service) afterHidden(ctx context.Context, hidden bool) error {
	if !hidden {
		return nil
	}
	return s.ensureAdultVariant(ctx)
}

// character：新角色。代號 → 角色本身的頭像 avatars/<代號>.<ext>（也是基本款第一張的檔）→ 角色卡 personas/mine/<代號>.md →
// closet.photo（file 指著頭像）→ closet.outfit/<代號>-base。頭像放角色卡的慣例路徑（不是衣服照片的分類）：複製角色進套件、
// 沒有紀錄可讀時的角色匯出都只認 avatars/<代號>.<ext>。
func (s *Service) character(ctx context.Context, req rules.Request, data []byte, ext string) (string, string, error) {
	code := rules.CharacterCode(req)
	record := rules.RecordID(code, rules.BaseOutfit)
	pid, _ := rules.PhotoIDFor(record, req.ID)
	if s.Store.OfficialCardExists(code) {
		return "", rules.ReasonCodeTaken, nil
	}
	// 頭像 avatars/<代號>.<ext> 已經在：是這個請求上次寫的（同副檔名、位元組相同）才接著做，其他＝這個代號被用了。
	avatar := "avatars/" + code + "." + ext
	for _, e := range store.PhotoExts {
		f := "avatars/" + code + "." + e
		cur, err := s.Store.ReadFile(f)
		if err != nil {
			return "", "", err
		}
		if cur != nil && (f != avatar || !bytes.Equal(cur, data)) {
			return "", rules.ReasonCodeTaken, nil
		}
	}
	// 重播判斷：已經有的東西「就是這個請求上次做的」才接著做；有任何對不上的＝別人用了。
	card, err := s.Store.Read(code + ".md")
	if err != nil {
		return "", "", err
	}
	if card != nil && rules.CardName(string(card)) != req.Name {
		return "", rules.ReasonCodeTaken, nil
	}
	rec, err := s.V.Get(ctx, kindOutfit, record)
	if err != nil {
		return "", "", err
	}
	if rec != nil && (rec.Archived || rules.Text(rec.Fields["source"]) != "mine" || rules.Text(rec.Fields["persona"]) != code) {
		return "", rules.ReasonCodeTaken, nil
	}
	if rec != nil {
		list, err := s.outfitPhotos(ctx, record)
		if err != nil {
			return "", "", err
		}
		for _, p := range list {
			if p.id != pid {
				return "", rules.ReasonCodeTaken, nil
			}
		}
	}
	p := newPhoto{id: pid, record: record, persona: code, typ: photoHalf, ext: ext, order: 1, adopted: true, path: avatar} // 新角色的第一張一定是一般版
	if reason, err := s.putPhotoFile(p, data); reason != "" || err != nil {
		return "", reason, err
	}
	if card == nil {
		if _, err := s.Store.PutNew(code+".md", []byte(rules.CharacterCard(req.Name, req.Desc))); err != nil {
			return mapStoreErr(err, rules.ReasonCodeTaken)
		}
	}
	// 照片紀錄先、衣服後：衣櫃的轉換器每一輪替「已經有的衣服」補登記沒有紀錄指著的 avatars/<代號>.<ext>，
	// 衣服寫出來時頭像已經有紀錄指著，不會被多登記一筆（衣櫃不看沒有衣服的照片紀錄）。
	if err := s.putPhotoRecord(ctx, p); err != nil {
		return "", "", err
	}
	if rec == nil {
		fields := map[string]any{"persona": code, "outfit": rules.BaseOutfit, "name": baseName, "cost": 0, "state": "active", "source": "mine", "order": 0}
		if err := s.V.Put(ctx, sdk.Record{Kind: kindOutfit, ID: record, Fields: fields}, ""); err != nil {
			return "", "", err
		}
	}
	return record, "", nil
}

// outfit：替任何一位角色加衣服。衣服 id ＝ my-<請求編號>（可重算），第一張半身照 photos/<衣服編號>--u<請求編號>.<ext>。
// 要用點數換：cost（沒寫照 free，預設 1 點；0＝一上傳就擁有）。直接生效是預設（state = active、照片 adopted = true）；
// 帶 draft = true 才放進「設計中」（照片 adopted = false，主人在開發家角色頁的「修改」分頁一張一張採用、上架）。
func (s *Service) outfit(ctx context.Context, req rules.Request, data []byte, ext string) (string, string, error) {
	id, _ := rules.OutfitIDFor(req.ID)
	record := rules.RecordID(req.Persona, id)
	pid, _ := rules.PhotoIDFor(record, req.ID)
	exists, err := s.personaExists(ctx, req.Persona)
	if err != nil {
		return "", "", err
	}
	if !exists {
		return "", rules.ReasonNoPersona, nil
	}
	rec, err := s.V.Get(ctx, kindOutfit, record)
	if err != nil {
		return "", "", err
	}
	if rec != nil && (rec.Archived || rules.Text(rec.Fields["source"]) != "mine" || rules.Text(rec.Fields["persona"]) != req.Persona) {
		return "", rules.ReasonOutfitTaken, nil
	}
	state := "active"
	if req.Draft {
		state = "draft"
	}
	if rec != nil {
		state = rules.Text(rec.Fields["state"])
	}
	p := newPhoto{id: pid, record: record, persona: req.Persona, typ: photoHalf, ext: ext, order: 1, adopted: state != "draft"} // 新衣服的第一張一定是一般版
	if reason, err := s.putPhotoFile(p, data); reason != "" || err != nil {
		return "", reason, err
	}
	if rec == nil {
		fields := map[string]any{"persona": req.Persona, "outfit": id, "name": req.Name, "cost": req.OutfitCost(), "state": state, "source": "mine"}
		if req.Desc != "" {
			fields["desc"] = req.Desc
		}
		if err := s.V.Put(ctx, sdk.Record{Kind: kindOutfit, ID: record, Fields: fields}, ""); err != nil {
			return "", "", err
		}
	}
	if err := s.putPhotoRecord(ctx, p); err != nil {
		return "", "", err
	}
	return record, "", nil
}

// personaExists：closet.person、官方角色卡、自己的角色卡其中之一存在。
func (s *Service) personaExists(ctx context.Context, persona string) (bool, error) {
	if s.Store.OfficialCardExists(persona) || s.Store.Exists(persona+".md") {
		return true, nil
	}
	p, err := s.V.Get(ctx, kindPerson, persona)
	if err != nil {
		return false, err
	}
	return p != nil, nil
}

// extra：替衣服加一張半身照（photo）、放／換全身照（full）、放／換某張半身照的動畫（video）。正常版只能改我的衣服；
// 自己的隱藏版（adult = true；沒寫 adult 而這套的照片全是隱藏版也算，rules.UploadHidden）官方的衣服也可以（檔在 personas/mine/、紀錄 source = mine，不碰官方的圖）。
// 第幾張、第幾支都照同一版（一般或隱藏版）現在的照片紀錄排的位置算（頁面看到的順序）；寫了 photo_id 就是換那一張
// （video 寫半身照的編號＝配那一張）。換圖：同一筆紀錄、新檔 photos/<照片編號>.<ext>（頭像還是 avatars/<角色>.<ext>）、舊檔改名成備份（replacePhoto）。
func (s *Service) extra(ctx context.Context, req rules.Request, data []byte, ext string) (string, string, error) {
	record := rules.RecordID(req.Persona, req.Outfit)
	rec, err := s.V.Get(ctx, kindOutfit, record)
	if err != nil {
		return "", "", err
	}
	if rec == nil || rec.Archived {
		return "", rules.ReasonNoOutfit, nil
	}
	facts := rules.Facts{OutfitExists: true, OutfitSource: rules.Text(rec.Fields["source"])}
	if req.PhotoID != "" {
		// 換官方的照片（CMI-25）：只有開發家、官方角色的位置就在家裡時才換，換了回到還沒採用。
		cur, err := s.V.Get(ctx, kindPhoto, req.PhotoID)
		if err != nil {
			return "", "", err
		}
		if cur != nil && !cur.Archived && rules.Text(cur.Fields["source"]) == "official" {
			return s.replaceOfficial(ctx, req, record, parsePhoto(*cur), data, ext)
		}
	}
	if facts.OutfitSource != "mine" && !req.Adult {
		return "", rules.ReasonOfficial, nil
	}
	pid, _ := rules.PhotoIDFor(record, req.ID)
	if cur, err := s.V.Get(ctx, kindPhoto, pid); err != nil || cur != nil {
		return record, "", err // 重播：這個請求新登記的那一張已經在了（之後被刪了也一樣）
	}
	list, err := s.outfitPhotos(ctx, record)
	if err != nil {
		return "", "", err
	}
	hidden := req.UploadHidden(allHidden(list))
	if reason, err := s.hiddenGate(ctx, req, hidden); reason != "" || err != nil {
		return "", reason, err
	}
	adopted := rules.Text(rec.Fields["state"]) != "draft"
	halves := halvesOf(list, hidden)
	facts.PhotoCount = len(halves)

	// 寫了 photo_id：換那一張，或（video）配那一張半身照。
	var target, half *photoRec
	if req.PhotoID != "" {
		cur, err := s.V.Get(ctx, kindPhoto, req.PhotoID)
		if err != nil {
			return "", "", err
		}
		if cur == nil || cur.Archived {
			return "", rules.ReasonNoPhotoRecord, nil
		}
		p := parsePhoto(*cur)
		switch {
		case p.outfit != record:
			return "", rules.ReasonPhotoOtherOutfit, nil
		case p.hidden != hidden:
			return "", rules.ReasonPhotoHiddenDiff, nil
		}
		switch {
		case req.Type == rules.TypePhoto && p.typ == photoHalf, req.Type == rules.TypeFull && p.typ == photoFull,
			req.Type == rules.TypeVideo && p.typ == photoVideo:
			target = &p
		case req.Type == rules.TypeVideo && p.typ == photoHalf:
			half = &p
		default:
			return "", rules.ReasonPhotoWrongType, nil
		}
	}

	n := newPhoto{id: pid, record: record, persona: req.Persona, ext: ext, hidden: hidden, adopted: adopted}
	switch req.Type {
	case rules.TypePhoto:
		if target == nil {
			dup, err := s.sameAsAny(halves, data)
			if err != nil {
				return "", "", err
			}
			facts.DuplicatePhoto = dup
		} else {
			facts.Replace = true
		}
		if reason := rules.CheckFacts(req, facts); reason != "" {
			return "", reason, nil
		}
		if facts.DuplicatePhoto {
			return record, "", nil // 同一張送兩次：已經在了
		}
		n.typ, n.order = photoHalf, nextOrder(list, hidden)
	case rules.TypeFull:
		if target == nil {
			if cur, ok := fullOf(list, hidden); ok && cur.source == "mine" {
				target = &cur // 一套一張全身照：已經有就換掉
			}
		}
		n.typ, n.order = photoFull, 1
	case rules.TypeVideo:
		if target == nil {
			chk := req
			if half == nil {
				if req.Photo >= 1 && req.Photo <= len(halves) {
					half = &halves[req.Photo-1]
				}
			} else {
				chk.Photo, facts.PhotoCount = 1, max(1, facts.PhotoCount) // 用編號指的那張，不看位置
			}
			var videos []photoRec
			if half != nil {
				videos = videosOf(list, *half)
			}
			facts.ClipCount = len(videos)
			if reason := rules.CheckFacts(chk, facts); reason != "" {
				return "", reason, nil
			}
			if req.Clip <= len(videos) {
				target = &videos[req.Clip-1]
			} else {
				n.typ, n.order, n.of, n.clip = photoVideo, half.order, half.id, nextClip(videos)
			}
		}
	}
	if target != nil {
		if target.source != "mine" {
			return "", rules.ReasonOfficialReplace, nil
		}
		// 換了新圖就回到還沒採用（作者 2026-11-15，CMI-22）：支援 edit 的家（開發家）要在「修改」分頁重新看過；
		// 一般家沒有採用這件事，照衣服的狀態直接生效。還不知道這個家支援什麼（能力紀錄還沒有、封存了）不當一般家。
		edit, known, err := s.homeCaps(ctx)
		if err != nil {
			return "", "", err
		}
		if edit || !known {
			adopted = false
		}
		if reason, err := s.replacePhoto(ctx, s.Store, *target, req.ID, data, ext, adopted); reason != "" || err != nil {
			return "", reason, err
		}
		return record, "", s.afterHidden(ctx, hidden)
	}
	if reason, err := s.addPhoto(ctx, n, data); reason != "" || err != nil {
		return "", reason, err
	}
	return record, "", s.afterHidden(ctx, hidden)
}

// replaceOfficial 換一張官方的照片（photo_id 指著 source = official 的紀錄，CMI-25）：秘書照評論重做官方衣服的圖時用。
// 請求的身分規則跟其他上傳一樣（誰都能送，判斷在這裡）。只換一般版、檔在官方位置 avatars/ 底下的（隱藏版的不換：換到 avatars/photos/
// 之後分不出是不是自己的隱藏版、不再跟著那個開關，CLO-18；檔不在 avatars/ 底下的官方位置寫不了）。寫官方位置跟升成官方一樣要核心說
// 這是開發家（system.get_mode；closet.capabilities 誰都寫得進去，只當第二道，CMI-14；問不到就等下一輪），能力也要有 edit
// （還沒有或封存了都不換），而且官方角色的位置就在家裡（store.OfficialInHome；在家外時官方的圖跟著角色包走，寫不進去）。
// 新檔寫在官方位置 avatars/photos/<照片編號>.<ext>（角色本身的頭像還是 avatars/<角色>.<ext>）、舊檔改名成備份留在原處，
// 紀錄的 adopted 一律改 false（換了新圖要主人重新看過，作者 2026-11-15）。只換不加：官方的衣服不能從這裡多一張。
func (s *Service) replaceOfficial(ctx context.Context, req rules.Request, record string, p photoRec, data []byte, ext string) (string, string, error) {
	switch {
	case p.outfit != record:
		return "", rules.ReasonPhotoOtherOutfit, nil
	case req.AdultGiven && req.Adult != p.hidden:
		return "", rules.ReasonPhotoHiddenDiff, nil
	case p.hidden:
		return "", rules.ReasonOfficialHidden, nil
	}
	switch {
	case req.Type == rules.TypePhoto && p.typ == photoHalf, req.Type == rules.TypeFull && p.typ == photoFull,
		req.Type == rules.TypeVideo && p.typ == photoVideo:
	case req.Type == rules.TypeVideo && p.typ == photoHalf:
		return "", rules.ReasonOfficial, nil // 替官方的照片多配一支動畫＝改官方的衣服
	default:
		return "", rules.ReasonPhotoWrongType, nil
	}
	if !store.ValidOfficialFile(p.file) {
		return "", rules.ReasonOfficialFile, nil
	}
	dev := false
	if s.Dev != nil {
		var err error
		if dev, err = s.Dev(ctx); err != nil {
			return "", "", err
		}
	}
	if !dev {
		return "", rules.ReasonOfficialReplace, nil
	}
	edit, known, err := s.homeCaps(ctx)
	if err != nil {
		return "", "", err
	}
	if !edit || !known {
		return "", rules.ReasonOfficialReplace, nil
	}
	if !s.Store.OfficialInHome() {
		return "", rules.ReasonOfficialLayer, nil
	}
	if reason, err := s.replacePhoto(ctx, s.Store.Official(), p, req.ID, data, ext, false); reason != "" || err != nil {
		return "", reason, err
	}
	return record, "", nil
}

// sameAsAny：這幾張（我的）照片的檔有沒有跟 data 位元組相同的。官方的照片（在官方的資料夾）不比。
func (s *Service) sameAsAny(list []photoRec, data []byte) (bool, error) {
	for _, p := range list {
		if p.source != "mine" || !store.ValidFile(p.file) {
			continue
		}
		cur, err := s.Store.ReadFile(p.file)
		if err != nil {
			return false, err
		}
		if cur != nil && bytes.Equal(cur, data) {
			return true, nil
		}
	}
	return false, nil
}

// adultVariantProblem：自己的隱藏版的圖要有一筆 dir = mine/adult 的隱藏版才會顯示。有就好；
// closet.variant/adult 還沒建（第一次放）也好，放完會建；那筆被封存或資料夾改掉、又沒有別筆 mine/adult 時，回白話原因與怎麼恢復。
func (s *Service) adultVariantProblem(ctx context.Context) (string, error) {
	list, err := s.V.List(ctx, kindVariant, false)
	if err != nil {
		return "", err
	}
	for _, v := range list {
		if !v.Archived && rules.Text(v.Fields["dir"]) == adultDir {
			return "", nil
		}
	}
	rec, err := s.V.Get(ctx, kindVariant, adultVariantID)
	if err != nil {
		return "", err
	}
	if rec != nil {
		return rules.ReasonAdultVariantGone, nil
	}
	return "", nil
}

// adultDir 是自己的隱藏版那一筆設定的資料夾（相對 personas/；新上傳的隱藏版照片在 photos/、靠紀錄的 hidden，這個資料夾只剩舊圖）。
const adultDir = "mine/" + rules.AdultFolder

// ensureAdultVariant 第一次放進自己的隱藏版時建 closet.variant/adult（預設關著、有開關按鈕）；已經有（含封存的）就不動。
func (s *Service) ensureAdultVariant(ctx context.Context) error {
	rec, err := s.V.Get(ctx, kindVariant, adultVariantID)
	if err != nil || rec != nil {
		return err
	}
	fields := map[string]any{"name": "自己的隱藏版", "dir": adultDir, "button": true, "on": false}
	return s.V.Put(ctx, sdk.Record{Kind: kindVariant, ID: adultVariantID, Fields: fields}, "")
}
