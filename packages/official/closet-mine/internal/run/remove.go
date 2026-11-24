package run

import (
	"context"
	"fmt"
	"strings"

	"github.com/minglight/evergenie/packages/official/closet-mine/internal/rules"
	"github.com/minglight/evergenie/packages/official/closet-mine/internal/store"
	"github.com/minglight/evergenie/sdk"
)

// 不帶上傳檔的整理（DEV-160 S3）：刪單張／拿回、刪一套／拿回（下架的也改回上架）、改要幾點。只改我的衣服、我的照片（source = mine）。
// 刪＝封存紀錄：單張的檔改名成備份 <原檔>.bak-<日期>-delete-u<請求編號>（備份規則（system 卡 BSW-4），放原檔旁、預設留 30 天），
// 衣櫃下一輪就不列、公開資料夾也拿掉；拿回＝把那筆紀錄寫回來、檔改回原名。整套刪掉時檔不動（衣服封存了衣櫃就不列）。
// 每一步都可以重播（做到一半行程重開，同一個請求再來一次接著做完）。

// deleteTag 是刪單張這個請求放在備份名字裡的原因（拿回時認它）。
func deleteTag(reqID string) string {
	n, _ := rules.SeqOf(reqID)
	return fmt.Sprintf("delete-u%d", n)
}

func isDeleteTag(reason string) bool { return strings.HasPrefix(reason, "delete-u") }

// tidy 做一個不帶上傳檔的請求：回 (result, reason, err)，同 handle。
func (s *Service) tidy(ctx context.Context, r sdk.Record, req rules.Request) (string, string, error) {
	switch req.Type {
	case rules.TypeRemovePhoto:
		return s.removePhoto(ctx, req)
	case rules.TypeRestorePhoto:
		return s.restorePhoto(ctx, req)
	case rules.TypeRemoveOutfit:
		return s.removeOutfit(ctx, r, req)
	case rules.TypeRestoreOutfit:
		return s.restoreOutfit(ctx, req)
	case rules.TypeSetCost:
		return s.setCost(ctx, req)
	}
	return "", rules.ReasonUnknownType, nil
}

// minePhoto 讀 photo_id 那一筆並做共同的檢查：在、是我的照片、角色對得上、隱藏版要開關開著。
func (s *Service) minePhoto(ctx context.Context, req rules.Request) (photoRec, string, error) {
	cur, err := s.V.Get(ctx, kindPhoto, req.PhotoID)
	if err != nil {
		return photoRec{}, "", err
	}
	if cur == nil {
		return photoRec{}, rules.ReasonNoPhotoRecord, nil
	}
	p := parsePhoto(*cur)
	switch {
	case req.Persona != "" && p.persona != req.Persona:
		return photoRec{}, rules.ReasonNoPhotoRecord, nil
	case p.source != "mine":
		return photoRec{}, rules.ReasonOfficialPhoto, nil
	case p.hidden && !s.adultOn(ctx):
		return photoRec{}, rules.ReasonAdultOff, nil
	}
	return p, "", nil
}

// liveOutfit：這套衣服還在（沒封存）嗎。
func (s *Service) liveOutfit(ctx context.Context, record string) (bool, error) {
	rec, err := s.V.Get(ctx, kindOutfit, record)
	if err != nil {
		return false, err
	}
	return rec != nil && !rec.Archived, nil
}

// removePhoto 刪單張：配它的影片先、它自己最後，每一張「檔改名成備份 → 紀錄的 file 指到備份 → 封存紀錄」。
// 已經刪了（封存著）就算做完。這套最後一張一般版的半身照不能單獨刪（整套不要了用刪一套）。
func (s *Service) removePhoto(ctx context.Context, req rules.Request) (string, string, error) {
	p, reason, err := s.minePhoto(ctx, req)
	if reason != "" || err != nil {
		return "", reason, err
	}
	if p.archived {
		return p.outfit, "", nil
	}
	live, err := s.liveOutfit(ctx, p.outfit)
	if err != nil {
		return "", "", err
	}
	if !live {
		return "", rules.ReasonRestoreOutfit, nil
	}
	list, err := s.outfitPhotos(ctx, p.outfit)
	if err != nil {
		return "", "", err
	}
	if p.typ == photoHalf && !p.hidden {
		var rest []photoRec
		for _, q := range list {
			if q.id != p.id {
				rest = append(rest, q)
			}
		}
		if len(halvesOf(rest, false)) == 0 {
			return "", rules.ReasonLastHalf, nil
		}
	}
	tag := deleteTag(req.ID)
	var todo []photoRec
	if p.typ == photoHalf {
		todo = videosOf(list, p)
	}
	for _, q := range append(todo, p) {
		if err := s.shelvePhoto(ctx, q, tag); err != nil {
			return "", "", err
		}
	}
	return p.outfit, "", nil
}

// shelvePhoto 刪一張：檔改名成備份（上次做到一半已經改了就找回那個備份；檔本來就不在就只封存），紀錄的 file 指到備份，封存。
func (s *Service) shelvePhoto(ctx context.Context, p photoRec, tag string) error {
	if _, reason, ok := store.SplitBackup(p.file); !ok || reason != tag {
		bak := ""
		if store.ValidFile(p.file) {
			has, err := s.Store.HasFile(p.file)
			if err != nil {
				return err
			}
			if has {
				bak = store.BackupName(p.file, s.Now(), tag)
				if err := s.Store.Move(p.file, bak); err != nil {
					return err
				}
			} else if bak, err = s.Store.FindBackup(p.file, func(r string) bool { return r == tag }); err != nil {
				return err
			}
		}
		if bak != "" {
			if err := s.V.Patch(ctx, kindPhoto, p.id, map[string]any{"file": bak}); err != nil {
				return err
			}
		}
	}
	return s.V.Archive(ctx, kindPhoto, p.id)
}

// restorePhoto 拿回刪掉的一張：配它、一起刪的影片先寫回，它自己最後；每一張「紀錄寫回來（file 是原名）→ 備份改回原名」。
// 衣服要還在；影片配的那張半身照要還在；全身照要這套現在沒有全身照。原本的 order／clip 被別張用了就排到最後。
// 本來就在（沒刪、或上次已經拿回）就算做完。
func (s *Service) restorePhoto(ctx context.Context, req rules.Request) (string, string, error) {
	p, reason, err := s.minePhoto(ctx, req)
	if reason != "" || err != nil {
		return "", reason, err
	}
	if !p.archived {
		return p.outfit, "", s.unshelveFile(p, nil)
	}
	live, err := s.liveOutfit(ctx, p.outfit)
	if err != nil {
		return "", "", err
	}
	if !live { // 整套刪掉的（衣服封存著）：要先拿回整套
		return "", rules.ReasonRestoreOutfit, nil
	}
	orig, tag, ok := store.SplitBackup(p.file)
	if !ok || !isDeleteTag(tag) {
		return "", rules.ReasonBackupGone, nil // 不是刪單張刪的（例如手動封存的）
	}
	list, err := s.outfitPhotos(ctx, p.outfit)
	if err != nil {
		return "", "", err
	}
	set := map[string]any{"file": orig}
	switch p.typ {
	case photoHalf:
		if len(halvesOf(list, p.hidden)) >= rules.MaxPhotos {
			return "", rules.ReasonTooManyPhoto, nil
		}
		for _, q := range list {
			if q.typ == photoHalf && q.hidden == p.hidden && q.order == p.order {
				set["order"] = nextOrder(list, p.hidden)
				break
			}
		}
	case photoFull:
		if _, ok := fullOf(list, p.hidden); ok {
			return "", rules.ReasonFullTaken, nil
		}
	case photoVideo:
		var half *photoRec
		for i := range list {
			if list[i].id == p.of && list[i].typ == photoHalf {
				half = &list[i]
			}
		}
		if half == nil {
			return "", rules.ReasonRestoreHalf, nil
		}
		videos := videosOf(list, *half)
		for _, v := range videos {
			if v.clip == p.clip {
				set["clip"] = nextClip(videos)
				break
			}
		}
		if c, ok := set["clip"].(int); ok && c > rules.MaxClips {
			return "", rules.ReasonNoClip(c), nil // 那張已經有 6 支了
		}
	}
	// 一起刪的影片（同一個刪單張的請求）先拿回：那時它們都配著這張。
	if p.typ == photoHalf {
		all, err := s.V.List(ctx, kindPhoto, true)
		if err != nil {
			return "", "", err
		}
		for _, r := range all {
			v := parsePhoto(r)
			if v.typ != photoVideo || v.of != p.id || v.source != "mine" {
				continue
			}
			if vo, vt, ok := store.SplitBackup(v.file); ok && vt == tag {
				if v.archived {
					vset := map[string]any{"file": vo}
					if o, moved := set["order"]; moved {
						vset["order"] = o // 動畫的 order 跟著它那張半身照
					}
					if err := s.putBack(ctx, v.rec, vset); err != nil {
						return "", "", err
					}
					v.file = vo
				}
				if err := s.unshelveFile(v, &tag); err != nil {
					return "", "", err
				}
			} else if !v.archived {
				// 上次拿回到一半：紀錄寫回來了、檔還沒改回原名。
				if err := s.unshelveFile(v, &tag); err != nil {
					return "", "", err
				}
			}
		}
	}
	if err := s.putBack(ctx, p.rec, set); err != nil {
		return "", "", err
	}
	p.file = orig
	if err := s.unshelveFile(p, &tag); err != nil {
		return "", "", err
	}
	return p.outfit, "", nil
}

// unshelveFile：紀錄指著的檔不在、而旁邊有刪單張留的備份（tag 是哪一個刪的請求；nil＝任一個刪的請求，取最新的）就改回原名。
// 檔在、或沒有備份都不動（沒有備份的那張衣櫃找不到圖就不列）。
func (s *Service) unshelveFile(p photoRec, tag *string) error {
	if !store.ValidFile(p.file) {
		return nil
	}
	has, err := s.Store.HasFile(p.file)
	if err != nil || has {
		return err
	}
	match := isDeleteTag
	if tag != nil {
		want := *tag
		match = func(r string) bool { return r == want }
	}
	bak, err := s.Store.FindBackup(p.file, match)
	if err != nil || bak == "" {
		return err
	}
	return s.Store.Move(bak, p.file)
}

// putBack 把封存的一筆寫回來（同一個編號，欄位照舊、set 的換掉；核心管的欄位核心會重寫）。已經寫回來了（重播）不算錯。
func (s *Service) putBack(ctx context.Context, r sdk.Record, set map[string]any) error {
	fields := map[string]any{}
	for k, v := range r.Fields {
		switch k {
		case "id", "by", "updated_at", "schema":
			continue
		}
		fields[k] = v
	}
	for k, v := range set {
		fields[k] = v
	}
	err := s.V.Put(ctx, sdk.Record{Kind: r.Kind, ID: r.ID, Fields: fields, Body: r.Body}, "")
	if e, ok := err.(*sdk.Error); ok && e.Code == sdk.ErrorCodeConflict {
		cur, gerr := s.V.Get(ctx, r.Kind, r.ID)
		if gerr == nil && cur != nil && !cur.Archived {
			return nil
		}
	}
	return err
}

// mineOutfit 讀 persona＋outfit 那一套並檢查是我的衣服（含封存的）。
func (s *Service) mineOutfit(ctx context.Context, req rules.Request) (*sdk.Record, string, error) {
	rec, err := s.V.Get(ctx, kindOutfit, rules.RecordID(req.Persona, req.Outfit))
	if err != nil {
		return nil, "", err
	}
	if rec == nil {
		return nil, rules.ReasonNoOutfit, nil
	}
	if rules.Text(rec.Fields["source"]) != "mine" {
		return nil, rules.ReasonOfficial, nil
	}
	return rec, "", nil
}

// removeOutfit 刪一套：先把要一起封存的照片編號記在請求的 photos（拿回時照它），再封存衣服、再封存它的照片紀錄（含隱藏版的）。
// 檔不動（衣服封存了衣櫃就不列、補登記也不看它）。已經封存的衣服（例如以前展示間的「不要了」，S5b 拿掉了）就把還沒封存的照片補封存。
func (s *Service) removeOutfit(ctx context.Context, r sdk.Record, req rules.Request) (string, string, error) {
	rec, reason, err := s.mineOutfit(ctx, req)
	if reason != "" || err != nil {
		return "", reason, err
	}
	list, err := s.outfitPhotos(ctx, rec.ID)
	if err != nil {
		return "", "", err
	}
	ids := rules.Strings(r.Fields["photos"])
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	grew := false
	for _, p := range list {
		if !seen[p.id] {
			seen[p.id], grew = true, true
			ids = append(ids, p.id)
		}
	}
	if grew {
		if err := s.V.Patch(ctx, KindRequest, r.ID, map[string]any{"photos": ids}); err != nil {
			return "", "", err
		}
	}
	if !rec.Archived {
		if err := s.V.Archive(ctx, kindOutfit, rec.ID); err != nil {
			return "", "", err
		}
	}
	for _, p := range list {
		if err := s.V.Archive(ctx, kindPhoto, p.id); err != nil {
			return "", "", err
		}
	}
	return rec.ID, "", nil
}

// restoreOutfit 拿回刪掉的一套：照最近一次刪這套的請求記下的照片編號，先把照片紀錄寫回來、再把衣服寫回來
// （衣服先回來的話，衣櫃補登記可能先替舊檔登記新的一筆）。用「不要了」封存的（沒有刪一套的請求）只寫回衣服，照片本來就還在。
// 已經升成官方換了編號的不能拿回；本來就在就算做完。
// 下架的（state = retired，沒封存）：一般家沒有上架／下架，「衣服」分頁把它列在「刪掉的」（CMI-24），拿回＝改回上架（state = active）；
// 從下架改回上架本來就不用每張採用（衣櫃卡 CLO-35 只有開發中上架才看），所以不分是哪種家。
func (s *Service) restoreOutfit(ctx context.Context, req rules.Request) (string, string, error) {
	rec, reason, err := s.mineOutfit(ctx, req)
	if reason != "" || err != nil {
		return "", reason, err
	}
	if !rec.Archived {
		if rules.Text(rec.Fields["state"]) == "retired" {
			return rec.ID, "", s.V.Patch(ctx, kindOutfit, rec.ID, map[string]any{"state": "active"})
		}
		return rec.ID, "", nil
	}
	outfits, err := s.V.List(ctx, kindOutfit, false)
	if err != nil {
		return "", "", err
	}
	for _, o := range outfits {
		if !o.Archived && rules.Text(o.Fields["from"]) == rec.ID {
			return "", rules.ReasonOutfitPromoted, nil
		}
	}
	reqs, err := s.V.List(ctx, KindRequest, true)
	if err != nil {
		return "", "", err
	}
	var last *sdk.Record
	lastSeq := 0
	for i, q := range reqs {
		f := q.Fields
		if rules.Text(f["type"]) != rules.TypeRemoveOutfit || rules.Text(f["state"]) != rules.StateDone || rules.Text(f["result"]) != rec.ID {
			continue
		}
		if n, ok := rules.SeqOf(q.ID); ok && n > lastSeq {
			last, lastSeq = &reqs[i], n
		}
	}
	if last != nil {
		for _, id := range rules.Strings(last.Fields["photos"]) {
			cur, err := s.V.Get(ctx, kindPhoto, id)
			if err != nil {
				return "", "", err
			}
			if cur == nil || !cur.Archived || rules.Text(cur.Fields["outfit"]) != rec.ID {
				continue
			}
			if err := s.putBack(ctx, *cur, nil); err != nil {
				return "", "", err
			}
		}
	}
	if err := s.putBack(ctx, *rec, nil); err != nil {
		return "", "", err
	}
	return rec.ID, "", nil
}

// setCost 改一套我的衣服要幾點（0＝免費、一上傳就擁有；大於 0＝沒兌換前只看剪影）。
func (s *Service) setCost(ctx context.Context, req rules.Request) (string, string, error) {
	rec, reason, err := s.mineOutfit(ctx, req)
	if reason != "" || err != nil {
		return "", reason, err
	}
	if rec.Archived {
		return "", rules.ReasonNoOutfit, nil
	}
	if cur, ok := rules.Int(rec.Fields["cost"]); ok && cur == req.Cost {
		return rec.ID, "", nil
	}
	return rec.ID, "", s.V.Patch(ctx, kindOutfit, rec.ID, map[string]any{"cost": req.Cost})
}
