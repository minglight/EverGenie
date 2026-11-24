package run

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/minglight/evergenie/packages/official/closet-mine/internal/rules"
	"github.com/minglight/evergenie/packages/official/closet-mine/internal/store"
	"github.com/minglight/evergenie/sdk"
)

// 照片紀錄（closet.photo，DEV-160）：衣櫃只照紀錄找圖。S3 起上傳的檔寫 personas/mine/photos/<照片編號>.<副檔名>
// （名字沒有意思；是誰的、哪一種、一般或隱藏版都只看紀錄；例外是角色本身的頭像 avatars/<代號>.<ext>），先寫檔、再登記紀錄。第幾張、第幾支、換哪一張都照紀錄算，不看檔名。

const kindPhoto = "closet.photo"

// 照片的種類（同 closet 的 schemas/photo.toml）。
const (
	photoHalf  = "half"
	photoFull  = "full"
	photoVideo = "video"
)

// photoRec 是讀回來的一筆照片紀錄（file 相對於 source 那邊：mine＝personas/mine/）。
type photoRec struct {
	id, persona, outfit, typ, of, source, file string
	order, clip                                int
	hidden, adopted, archived                  bool
	rec                                        sdk.Record
}

func parsePhoto(r sdk.Record) photoRec {
	f := r.Fields
	p := photoRec{id: r.ID, persona: rules.Text(f["persona"]), outfit: rules.Text(f["outfit"]), typ: rules.Text(f["type"]),
		of: rules.Text(f["of"]), source: rules.Text(f["source"]), file: rules.Text(f["file"]),
		hidden: rules.Bool(f["hidden"]), adopted: rules.Bool(f["adopted"]), archived: r.Archived, rec: r}
	p.order, _ = rules.Int(f["order"])
	p.clip, _ = rules.Int(f["clip"])
	return p
}

// outfitPhotos 是這套衣服（closet.outfit 編號）現在的照片紀錄（不含封存的）。
func (s *Service) outfitPhotos(ctx context.Context, record string) ([]photoRec, error) {
	list, err := s.V.List(ctx, kindPhoto, false)
	if err != nil {
		return nil, err
	}
	var out []photoRec
	for _, r := range list {
		if r.Archived || rules.Text(r.Fields["outfit"]) != record {
			continue
		}
		out = append(out, parsePhoto(r))
	}
	return out, nil
}

// halvesOf 是同一版（一般或隱藏版）的半身照，照衣櫃排的順序（order，再照編號；同一個 order 只算第一筆，order < 1 不算）：
// 第 i 個就是頁面看到的第 i 張，也是 video 請求 photo 的意思。
func halvesOf(list []photoRec, hidden bool) []photoRec {
	var out []photoRec
	for _, p := range list {
		if p.typ == photoHalf && p.hidden == hidden && p.order >= 1 {
			out = append(out, p)
		}
	}
	sortPhotos(out)
	seen := map[int]bool{}
	kept := out[:0]
	for _, p := range out {
		if !seen[p.order] {
			seen[p.order] = true
			kept = append(kept, p)
		}
	}
	return kept
}

// videosOf 是配這張半身照的動畫（照 clip、再照編號排；第 k 個就是 video 請求 clip = k）。
func videosOf(list []photoRec, half photoRec) []photoRec {
	var out []photoRec
	for _, p := range list {
		if p.typ == photoVideo && p.of == half.id && p.hidden == half.hidden {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].clip != out[j].clip {
			return out[i].clip < out[j].clip
		}
		return out[i].id < out[j].id
	})
	return out
}

// fullOf 是同一版現在的全身照（衣櫃用 order 最小的那張）；沒有回 false。
func fullOf(list []photoRec, hidden bool) (photoRec, bool) {
	var out []photoRec
	for _, p := range list {
		if p.typ == photoFull && p.hidden == hidden {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return photoRec{}, false
	}
	sortPhotos(out)
	return out[0], true
}

func sortPhotos(list []photoRec) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].order != list[j].order {
			return list[i].order < list[j].order
		}
		return list[i].id < list[j].id
	})
}

// allHidden：這套衣服現在（還在的）照片紀錄有、而且全部是隱藏版（rules.UploadHidden：沒寫 adult 的新照片也算隱藏版）。
func allHidden(list []photoRec) bool {
	for _, p := range list {
		if !p.hidden {
			return false
		}
	}
	return len(list) > 0
}

// nextOrder 是同一版新的一張半身照的 order：現在還在的最大的 +1（新的一張排在最後；拿回刪掉的那張時原本的號碼被用了也排到最後）。
func nextOrder(list []photoRec, hidden bool) int {
	n := 0
	for _, p := range list {
		if p.typ == photoHalf && p.hidden == hidden && p.order > n {
			n = p.order
		}
	}
	return n + 1
}

// nextClip 是這張半身照新的一支動畫的 clip：現有最大的 +1。
func nextClip(videos []photoRec) int {
	n := 0
	for _, v := range videos {
		if v.clip > n {
			n = v.clip
		}
	}
	return n + 1
}

// newPhoto 是要新登記的一張：id 是 <衣服編號>--u<請求編號>（rules.PhotoIDFor），檔 photos/<id>.<ext>（path 有寫就用它：
// 新角色的第一張是角色本身的頭像 avatars/<代號>.<ext>），source 一律 mine。
type newPhoto struct {
	id, record, persona, typ, of, ext, path string
	order, clip                             int
	hidden, adopted                         bool
}

func (p newPhoto) file() string {
	if p.path != "" {
		return p.path
	}
	return "photos/" + p.id + "." + p.ext
}

// isAvatar：這張的檔是不是角色本身的頭像 avatars/<角色>.<圖或 mp4>（新角色的第一張；S3 以前的角色也是這樣放，第一支動畫是 <角色>.mp4）。
func isAvatar(p photoRec) bool {
	dir, base := path.Split(p.file)
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(base), "."))
	return dir == "avatars/" && strings.TrimSuffix(base, path.Ext(base)) == p.persona && (ext == "mp4" || slices.Contains(store.PhotoExts, ext))
}

// replacementFile：換圖的新檔放哪——角色本身的頭像換了還是放 avatars/<角色>.<ext>（頭像不能因為換圖不見），
// 其他我的照片放 photos/<照片編號>.<ext>；官方的照片放官方位置的 avatars/photos/<照片編號>.<ext>（官方的圖都在 avatars/ 底下，同升成官方）。
func replacementFile(cur photoRec, ext string) string {
	switch {
	case isAvatar(cur):
		return "avatars/" + cur.persona + "." + ext
	case cur.source == "official":
		return "avatars/photos/" + cur.id + "." + ext
	}
	return "photos/" + cur.id + "." + ext
}

// putPhotoFile 寫這一張的檔（不蓋；同一個請求重播時位元組相同算寫過）。回拒絕原因或錯誤。
func (s *Service) putPhotoFile(p newPhoto, data []byte) (string, error) {
	if _, err := s.Store.PutNew(p.file(), data); err != nil {
		_, reason, err := mapStoreErr(err, rules.ReasonFileTaken)
		return reason, err
	}
	return "", nil
}

// putPhotoRecord 登記這一張（檔要先寫好）。已經有這個編號（重播；或之後被刪了、封存著）就不動。
func (s *Service) putPhotoRecord(ctx context.Context, p newPhoto) error {
	cur, err := s.V.Get(ctx, kindPhoto, p.id)
	if err != nil || cur != nil {
		return err
	}
	fields := map[string]any{"persona": p.persona, "outfit": p.record, "type": p.typ, "order": p.order,
		"hidden": p.hidden, "source": "mine", "file": p.file(), "adopted": p.adopted}
	if p.typ == photoVideo {
		fields["of"], fields["clip"] = p.of, p.clip
	}
	err = s.V.Put(ctx, sdk.Record{Kind: kindPhoto, ID: p.id, Fields: fields}, "")
	if e, ok := err.(*sdk.Error); ok && e.Code == sdk.ErrorCodeConflict {
		return nil
	}
	return err
}

// addPhoto 新增一張：先寫檔、再登記（衣櫃只照紀錄找圖，紀錄不會指到還沒寫的檔）。
func (s *Service) addPhoto(ctx context.Context, p newPhoto, data []byte) (string, error) {
	if reason, err := s.putPhotoFile(p, data); reason != "" || err != nil {
		return reason, err
	}
	return "", s.putPhotoRecord(ctx, p)
}

// replacePhoto 換一張的檔（同一筆紀錄，編號不變）：st 是那張的擁有者位置（我的照片 personas/mine/，官方的照片 Store.Official），
// 新檔寫在 replacementFile、紀錄的 file 指過去，舊檔改名成備份 <舊檔>.bak-<日期>-replace-u<請求編號>（備份規則（system 卡 BSW-4））；
// adopted 由呼叫端給（一般家照衣服的狀態直接生效；開發家、設計中的換了要重新採用，CMI-22）。每一步都可以重播：舊檔已經改名、
// 紀錄已經指到新檔都照現在的接著做；現在的檔就是這次上傳的就不動檔（採用照樣照 adopted 改）。
func (s *Service) replacePhoto(ctx context.Context, st *store.Store, cur photoRec, reqID string, data []byte, ext string, adopted bool) (string, error) {
	n, _ := rules.SeqOf(reqID)
	var old []byte
	if store.ValidFile(cur.file) {
		var err error
		if old, err = st.ReadFile(cur.file); err != nil {
			return "", err
		}
	}
	set := map[string]any{}
	if cur.adopted != adopted {
		set["adopted"] = adopted
	}
	if old != nil && bytes.Equal(old, data) {
		return "", s.patchPhoto(ctx, cur.id, set) // 跟現在的一模一樣：不動檔
	}
	file := replacementFile(cur, ext)
	if file != cur.file {
		// 新檔名跟現在的不同（換了副檔名）：先確定那個名字沒有別的檔（頭像的檔名是角色代號，不像照片編號一定沒人用），
		// 什麼都還沒動就拒；是這個請求上次寫的（位元組一樣）可以接著做。
		there, err := st.ReadFile(file)
		if err != nil {
			_, reason, err := mapStoreErr(err, rules.ReasonFileTaken)
			return reason, err
		}
		if there != nil && !bytes.Equal(there, data) {
			return rules.ReasonFileTaken, nil
		}
		set["file"] = file
	}
	// 順序：舊檔改名成備份 → 紀錄指到新檔 → 寫新檔。新檔一寫出來就有紀錄指著：衣櫃的轉換器只補「沒有紀錄指著」的
	// avatars/<角色>.<ext>，換頭像時不會多登記一筆；中途被殺，重播照現在的紀錄接著做（CMI-7）。
	if old != nil {
		if err := st.Move(cur.file, store.BackupName(cur.file, s.Now(), fmt.Sprintf("replace-u%d", n))); err != nil {
			_, reason, err := mapStoreErr(err, rules.ReasonFileTaken)
			return reason, err
		}
	}
	if err := s.patchPhoto(ctx, cur.id, set); err != nil {
		return "", err
	}
	if _, err := st.PutNew(file, data); err != nil {
		_, reason, err := mapStoreErr(err, rules.ReasonFileTaken)
		return reason, err
	}
	return "", nil
}

func (s *Service) patchPhoto(ctx context.Context, id string, set map[string]any) error {
	if len(set) == 0 {
		return nil
	}
	return s.V.Patch(ctx, kindPhoto, id, set)
}
