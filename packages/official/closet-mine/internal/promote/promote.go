// Package promote 是「升成官方」（DEV-141，作者 2026-11-07）：只在開發家，把一套採用了的主人自己的衣服
// 從 personas/mine/ 搬到官方位置，衣櫃目錄那筆改成官方的，之後建角色包就會帶出去。
// 要搬哪些圖照照片紀錄（closet.photo，DEV-160 S2）：這套衣服的一般照片（hidden = false、source = mine）搬到官方位置
// （檔名不改，檔名沒有意思）：舊的 avatars/… 搬到官方的同一個相對路徑，新上傳的 photos/<照片編號>.<ext>（S3）搬到官方的
// avatars/photos/<照片編號>.<ext>（官方的圖都在 avatars/ 底下：建包、匯出只收那裡），紀錄改成 source = official、file 跟著換；
// 自己的隱藏版的照片不升、留在 personas/mine/，紀錄跟著換編號。
// 由 closet-mine.request 的 type = promote 觸發（只認主人本人送的），這個套件的程式處理；核心不認得衣櫃（法律第 3 條）。
// 這是本套件唯一會寫 personas/avatars/ 的地方（package.toml 宣告 files:personas/avatars）。
// 官方位置照家的佈局（DEV-145）：第 1 版是 <家>/personas/，第 2 版在家外面的狀態資料夾（sdk.OfficialPersonasDir）；
// 這時 Move.To 與官方角色卡是絕對路徑，主人的 personas/mine/ 仍是家底下的相對路徑。
// 能退回：舊的圖改名成備份（<原名>.bak-<日期>-promote，照備份規則（system 卡 BSW-4））、舊的紀錄只封存不刪；照片全是隱藏版的衣服不能升。
package promote

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

const (
	kindOutfit   = "closet.outfit"
	kindRotation = "closet.rotation"
	kindPhoto    = "closet.photo"
	minePrefix   = "my-" // 上傳衣服自動給的衣服 id；官方的衣服不能用，免得撞到別的家之後上傳的
)

var (
	personaPattern = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	outfitPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	mediaExt       = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".mp4": true}
)

// Vault 是升官方要用的紀錄讀寫（closet-mine 的 run.Vault 有這些；測試用假的）。Get 找不到回 nil、nil（封存的也讀得到）。
type Vault interface {
	Get(ctx context.Context, kind, id string) (*sdk.Record, error)
	List(ctx context.Context, kind string, includeArchived bool) ([]sdk.Record, error)
	Put(ctx context.Context, rec sdk.Record, expectedRevision string) error
	Archive(ctx context.Context, kind, id string) error
}

// Rejected 是白話的拒絕原因（請求回 rejected）；其他錯誤是這次沒做成（請求維持 pending，下一輪再試）。
type Rejected struct{ Reason string }

func (r *Rejected) Error() string { return r.Reason }

func reject(format string, a ...any) error { return &Rejected{Reason: fmt.Sprintf(format, a...)} }

// Options 是一次升官方的參數。
type Options struct {
	Home   string
	Target string // <人設>-<衣服 id>，例 athena-my-12（closet.outfit 的編號）
	NewID  string // 新的衣服 id；空＝照舊（舊的是 my- 開頭時一定要給）
	DryRun bool
	Vault  Vault
	Now    time.Time
}

// Move 是一個要搬的檔：From、Backup 是家底下的相對路徑；To 在第 1 版的家是家底下的相對路徑，第 2 版是絕對路徑。
type Move struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Backup  string `json:"backup"`  // 舊檔改名成的備份
	Written bool   `json:"written"` // 這次才寫進官方位置的（原本就有一樣的檔時是 false，退回時不刪）
}

// Result 是結果（預演也是這一份）。
type Result struct {
	DryRun    bool     `json:"dry_run"`
	Resumed   bool     `json:"resumed,omitempty"` // 上次做到一半，這次接著做
	From      string   `json:"from"`              // 原本的編號
	To        string   `json:"to"`                // 升官方後的編號
	Files     []Move   `json:"files"`
	Rotations []string `json:"rotations,omitempty"` // 跟著改了衣服 id 的輪播設定
	Photos    []string `json:"photos,omitempty"`    // 跟著改的照片紀錄（換了 id 時是新的編號）
	Hidden    []string `json:"hidden,omitempty"`    // 自己的隱藏版的照片（新的編號）：不升、圖留在 personas/mine/
	Undo      []string `json:"undo"`                // 怎麼退回（一行一步，都用現成的指令或改檔名做得到）

	photos []photoChange // 照片紀錄要怎麼改（Run 內部用）
}

// photoChange 是一筆照片紀錄要怎麼改：old 是現在的紀錄；id 是改完的編號（跟 old.ID 一樣＝原地改）；
// move＝這張的圖要搬到官方位置（紀錄改 source = official）；done＝上次已經改好（接著做時）。
type photoChange struct {
	old  sdk.Record
	id   string
	move bool
	done bool
}

// Run 升一套衣服。順序：全部先驗 → 複製圖到官方位置（不蓋不同的檔）→ 改紀錄 → 舊的圖改名成備份。
// 改紀錄失敗時把這次新寫的官方圖刪掉；紀錄改好之後才失敗（封存、輪播、改名）時，再送一次同樣的請求會認出新的那筆（from 指回舊編號）接著做。
// 開發家與主人本人的判斷在呼叫端（run.Service）。
func Run(ctx context.Context, o Options) (*Result, error) {
	slug, outfit, ok := strings.Cut(strings.TrimSpace(o.Target), "-")
	if !ok || !personaPattern.MatchString(slug) || !outfitPattern.MatchString(outfit) {
		return nil, reject("outfit 要寫 <人設>-<衣服 id>（例：athena-my-12），跟 closet.outfit 的編號一樣")
	}
	// 找官方卡的共用入口（PRS-25）：主人有同代號的卡蓋過跟著程式的那張時，這個代號是主人的、不算官方角色（PRS-27）
	if card, ok := sdk.OfficialCardPath(o.Home, slug); !ok || !isRegular(card) {
		return nil, reject("%s 不是官方角色（沒有 personas/%s.md）：只有官方角色的衣服能升官方", slug, slug)
	}
	newID := strings.TrimSpace(o.NewID)
	if newID == "" {
		newID = outfit
	}
	switch {
	case !outfitPattern.MatchString(newID):
		return nil, reject("new_id 要用小寫英文、數字與減號（例：seaside）")
	case strings.HasPrefix(newID, minePrefix) && strings.TrimSpace(o.NewID) == "":
		return nil, reject("%s 是上傳時自動給的編號（my- 開頭），官方的衣服要換一個：加 new_id（例：\"new_id\":\"seaside\"）", outfit)
	case strings.HasPrefix(newID, minePrefix):
		return nil, reject("官方的衣服 id 不能用 my- 開頭（那是上傳衣服自動給的）")
	case newID == "base" && outfit != "base":
		return nil, reject("base 是每位角色的第一套，不能用")
	}
	record, newRecord := slug+"-"+outfit, slug+"-"+newID
	rec, err := o.Vault.Get(ctx, kindOutfit, record)
	if err != nil {
		return nil, err
	}
	var cur *sdk.Record
	if newRecord != record {
		if cur, err = o.Vault.Get(ctx, kindOutfit, newRecord); err != nil {
			return nil, err
		}
	}
	// 上次做到一半：新的那筆已經在（官方、from 指回舊編號），接著做封存、輪播、改名。
	resume := cur != nil && !cur.Archived && text(cur.Fields["from"]) == record && text(cur.Fields["source"]) == "official"
	if newRecord == record && rec != nil && !rec.Archived && text(rec.Fields["source"]) == "official" {
		// 同一個編號：衣服已經改成官方，照片紀錄還有沒改完的、或主人那份跟官方一模一樣還沒改名成備份的，就是上次做到一半。
		if resume, err = sameIDUnfinished(ctx, o.Vault, o.Home, record); err != nil {
			return nil, err
		}
	}
	if !resume {
		switch {
		case rec == nil || rec.Archived:
			return nil, reject("衣服目錄裡沒有 %s", record)
		case text(rec.Fields["source"]) != "mine":
			return nil, reject("%s 已經是官方的衣服", record)
		case text(rec.Fields["state"]) != "active":
			return nil, reject("%s 還沒上架：先在角色頁的「修改」分頁採用、上架，上架了的才能升官方", record)
		case cur != nil:
			return nil, reject("已經有 %s 這套衣服（含封存的），換一個 new_id", newRecord)
		}
	}
	if err := safeDirs(o.Home); err != nil {
		return nil, err
	}

	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	changes, err := planPhotos(ctx, o.Vault, record, newRecord, resume)
	if err != nil {
		return nil, err
	}
	moves, err := planFiles(o.Home, changes, now, resume)
	if err != nil {
		return nil, err
	}
	res := &Result{DryRun: o.DryRun, Resumed: resume, From: record, To: newRecord, Files: moves, photos: changes}
	for _, c := range changes {
		res.Photos = append(res.Photos, c.id)
		if !c.move && boolField(c.old.Fields["hidden"]) {
			res.Hidden = append(res.Hidden, c.id)
		}
	}
	rotations, err := rotationsToFix(ctx, o.Vault, slug, outfit)
	if err != nil {
		return nil, err
	}
	if newRecord != record {
		for _, r := range rotations {
			res.Rotations = append(res.Rotations, r.ID)
		}
	}
	if o.DryRun {
		res.Undo = undoSteps(res)
		return res, nil
	}

	// ① 複製圖到官方位置（先寫暫存檔再改名）；記下哪些是這次才寫的。
	for i, m := range res.Files {
		wrote, err := copyNew(at(o.Home, m.From), at(o.Home, m.To))
		if err != nil {
			removeWritten(o.Home, res.Files[:i])
			return nil, err
		}
		res.Files[i].Written = wrote
	}
	res.Undo = undoSteps(res)
	// ② 改紀錄：同一個編號就改 source；換編號就新增一筆（帶 from，兌換過的照樣擁有）、照片紀錄換到新編號、
	// 舊的衣服先封存再封存舊的照片紀錄（中間衣櫃跑一輪也不會把舊檔當成沒登記的補回去）、輪播設定跟著換。
	if !resume {
		fields := map[string]any{}
		for k, v := range rec.Fields {
			if !coreField[k] {
				fields[k] = v
			}
		}
		fields["source"] = "official"
		id, rev := record, rec.Revision
		if newRecord != record {
			fields["outfit"], fields["from"] = newID, record
			id, rev = newRecord, ""
		}
		if err := o.Vault.Put(ctx, sdk.Record{Kind: kindOutfit, ID: id, Fields: fields, Body: rec.Body}, rev); err != nil {
			removeWritten(o.Home, res.Files)
			return nil, err
		}
	}
	if err := putPhotos(ctx, o.Vault, res.photos, record, newRecord); err != nil {
		return nil, fmt.Errorf("照片紀錄沒改完（下一輪接著做）：%w", err)
	}
	if newRecord != record {
		if rec != nil && !rec.Archived {
			if err := o.Vault.Archive(ctx, kindOutfit, record); err != nil {
				return nil, fmt.Errorf("封存舊的 %s 失敗（下一輪接著做）：%w", record, err)
			}
		}
		for _, c := range res.photos {
			if c.id != c.old.ID && !c.old.Archived {
				if err := o.Vault.Archive(ctx, kindPhoto, c.old.ID); err != nil {
					return nil, fmt.Errorf("封存舊的照片紀錄 %s 失敗（下一輪接著做）：%w", c.old.ID, err)
				}
			}
		}
		for _, r := range rotations {
			f := map[string]any{}
			for k, v := range r.Fields {
				if !coreField[k] {
					f[k] = v
				}
			}
			f["outfits"] = replaceOutfit(r.Fields["outfits"], outfit, newID)
			if err := o.Vault.Put(ctx, sdk.Record{Kind: kindRotation, ID: r.ID, Fields: f, Body: r.Body}, r.Revision); err != nil {
				return nil, fmt.Errorf("輪播設定 closet.rotation/%s 沒改到（下一輪接著做）：%w", r.ID, err)
			}
		}
	}
	// ③ 舊的圖改名成備份（衣櫃不讀 .bak- 的檔；要退回改回原名就好）。
	for _, m := range res.Files {
		if err := os.Rename(at(o.Home, m.From), at(o.Home, m.Backup)); err != nil {
			return nil, fmt.Errorf("舊的圖 %s 沒能改名成備份（下一輪接著做）：%w", m.From, err)
		}
	}
	return res, nil
}

// Summary 把結果寫成給人看的幾行（回填到請求的 detail）。
// sharingNote 說實話：升成官方只放進這個家，要帶到別的家還要自己做兩件事。
const sharingNote = "已放進這個家的官方位置（官方角色的 avatars/；第 1 版的家是 personas/avatars/，第 2 版在家外面的狀態資料夾）；要帶到別的家，還要在 home.toml [admin] public_personas 列這位角色，再建角色包（evergenie personas pack build）\n"

func (r *Result) Summary() string {
	var b strings.Builder
	switch {
	case r.DryRun:
		fmt.Fprintf(&b, "預演（什麼都沒動）：會把 %s 升成官方 %s\n", r.From, r.To)
	case r.Resumed:
		fmt.Fprintf(&b, "接著上次做完，%s 升成官方 %s 了\n", r.From, r.To)
	default:
		fmt.Fprintf(&b, "%s 升成官方 %s 了\n", r.From, r.To)
	}
	if !r.DryRun {
		b.WriteString(sharingNote)
	}
	for _, m := range r.Files {
		fmt.Fprintf(&b, "  %s → %s（舊檔改名 %s）\n", m.From, m.To, m.Backup)
	}
	for _, id := range r.Rotations {
		fmt.Fprintf(&b, "  輪播設定 closet.rotation/%s 換成新的 id\n", id)
	}
	if n := len(r.Photos) - len(r.Hidden); n > 0 {
		fmt.Fprintf(&b, "  照片紀錄 %d 筆改成官方的（closet.photo，檔名不改）\n", n)
	}
	for _, id := range r.Hidden {
		fmt.Fprintf(&b, "  注意：closet.photo/%s 是自己的隱藏版的照片，不升，圖留在 personas/mine/（紀錄跟著換到新編號）\n", id)
	}
	b.WriteString("要退回：\n")
	for _, s := range r.Undo {
		fmt.Fprintf(&b, "  %s\n", s)
	}
	return strings.TrimRight(b.String(), "\n")
}

// coreField 是 vault 自己管的欄位（寫回時不帶）。
var coreField = map[string]bool{"schema": true, "id": true, "created_at": true, "updated_at": true, "by": true, "kind": true, "archived_at": true}

func text(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }

func boolField(v any) bool { b, _ := v.(bool); return b }

func isRegular(p string) bool { i, err := os.Lstat(p); return err == nil && i.Mode().IsRegular() }

// at 把 Move 的路徑變成實際位置：相對路徑是家底下的，絕對路徑（第 2 版的官方位置）照用。
func at(home, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(home, p)
}

// official 是官方角色位置（接上 parts）的寫法：在家底下（第 1 版）就用家底下的相對路徑，不在（第 2 版）用絕對路徑。
func official(home string, parts ...string) string {
	dir := sdk.OfficialPersonasDir(home, parts...)
	if rel, err := filepath.Rel(home, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return dir
}

// safeDirs：personas/、personas/mine/（與它的 avatars/、photos/）、官方角色的位置（第 1 版就是 personas/）與它的 avatars/、avatars/photos/
// 不能是捷徑（符號連結），免得寫到別的地方。後面幾個還沒有也可以（主人只有新上傳的就沒有 avatars/；官方的複製時建）。
func safeDirs(home string) error {
	dirs := []string{"personas", filepath.Join("personas", "mine")}
	if root := official(home); root != "personas" {
		dirs = append(dirs, root)
	}
	optional := []string{filepath.Join("personas", "mine", "avatars"), filepath.Join("personas", "mine", minePhotos),
		official(home, "avatars"), official(home, "avatars", minePhotos)}
	for _, rel := range append(dirs, optional...) {
		info, err := os.Lstat(at(home, rel))
		if os.IsNotExist(err) && slices.Contains(optional, rel) {
			continue
		}
		if err != nil || !info.IsDir() {
			return reject("%s 不是一般資料夾（可能是捷徑），為了安全沒有升，請跟任何一位秘書說", rel)
		}
	}
	return nil
}

// planPhotos 找這套衣服的照片紀錄（closet.photo，outfit 指著它；接著做時也找已經換到新編號的），算出每筆改成什麼。
// 一般照片（hidden = false、source = mine）的圖要搬；自己的隱藏版留著。沒有任何要搬的（照片全是隱藏版、或衣櫃還沒登記）就不做。
func planPhotos(ctx context.Context, v Vault, record, newRecord string, resume bool) ([]photoChange, error) {
	list, err := v.List(ctx, kindPhoto, false)
	if err != nil {
		return nil, err
	}
	var out []photoChange
	target, moving := map[string]bool{}, 0
	for _, r := range list {
		if text(r.Fields["outfit"]) != record {
			continue
		}
		c := photoChange{old: r, id: renameID(r.ID, record, newRecord), move: !boolField(r.Fields["hidden"]) && text(r.Fields["source"]) == "mine"}
		if c.move {
			moving++
		} else if resume && newRecord == record && !boolField(r.Fields["hidden"]) && text(r.Fields["source"]) == "official" {
			c.move, c.done = true, true // 同一個編號接著做：紀錄上次已經改好，只剩主人那份改名成備份
		}
		target[c.id] = true
		out = append(out, c)
	}
	if resume && newRecord != record {
		for _, r := range list {
			if text(r.Fields["outfit"]) == newRecord && !target[r.ID] {
				// 上次已經換好、舊的也封存了：只剩搬檔或改名（主人那份還在就接著做）。
				out = append(out, photoChange{old: r, id: r.ID, move: !boolField(r.Fields["hidden"]) && text(r.Fields["source"]) == "official", done: true})
			}
		}
	}
	if moving == 0 && !resume {
		return nil, reject("%s 沒有可以升的照片（照片全是隱藏版，或衣櫃還沒登記這套的照片）", record)
	}
	if !resume {
		// 要公開的每一張都要主人採用過（作者 2026-11-15：秘書換了新圖會回到還沒採用，沒看過的不能公開出去；CMI-14）。
		// 自己的隱藏版不升、不公開，不看。
		var wip []string
		for _, c := range out {
			if c.move && c.old.Fields["adopted"] != true {
				wip = append(wip, c.old.ID)
			}
		}
		if len(wip) > 0 {
			sort.Strings(wip)
			return nil, reject("%s 還有 %d 張照片沒採用（%s）：先在角色頁的「修改」分頁看過、按「採用」，每張都採用了才能升成官方", record, len(wip), strings.Join(wip, "、"))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

// sameIDUnfinished：同一個編號升成官方時上次做到一半嗎（衣服已經是官方）——這套有一般照片的紀錄還是 source = mine（照片紀錄沒改完），
// 或紀錄已經是官方、主人那份（personas/mine/<file>）還在而且跟官方位置的一模一樣（舊的圖還沒改名成備份）。
func sameIDUnfinished(ctx context.Context, v Vault, home, record string) (bool, error) {
	list, err := v.List(ctx, kindPhoto, false)
	if err != nil {
		return false, err
	}
	for _, r := range list {
		if text(r.Fields["outfit"]) != record || boolField(r.Fields["hidden"]) {
			continue
		}
		file := text(r.Fields["file"])
		switch text(r.Fields["source"]) {
		case "mine":
			return true, nil
		case "official":
			if !validFile(file) {
				continue
			}
			mine := filepath.Join(home, "personas", "mine", filepath.FromSlash(mineFile(file)))
			if info, err := os.Lstat(mine); err == nil && info.Mode().IsRegular() && sameOrMissing(mine, at(home, official(home, filepath.FromSlash(file)))) == nil {
				if _, err := os.Lstat(at(home, official(home, filepath.FromSlash(file)))); err == nil {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// renameID 是照片紀錄換到新衣服編號後的編號：<衣服編號>--… 的前綴換掉（照 DEV-160 S1 的編號規則）；不是這個樣子的不換。
func renameID(id, record, newRecord string) string {
	if record != newRecord && strings.HasPrefix(id, record+"--") {
		return newRecord + id[len(record):]
	}
	return id
}

// planFiles 算出要搬的圖：照片紀錄的 file（相對於 personas/mine/）搬到官方位置的 officialFile（舊的 avatars/… 是同一個相對路徑、
// photos/X 是 avatars/photos/X）、舊檔改名成備份。接著做時，紀錄已經改成官方、主人那份還沒改名的也列進來（只剩改名）。
// 目的地已經有不同的檔、或檔不在 avatars/ 或 photos/ 底下都不做。
func planFiles(home string, changes []photoChange, now time.Time, resume bool) ([]Move, error) {
	stamp := now.Format("20060102")
	var moves []Move
	for _, c := range changes {
		if !c.move {
			continue
		}
		file, to := text(c.old.Fields["file"]), ""
		if c.done { // 紀錄已經是官方的：file 是官方的位置
			file, to = mineFile(file), file
		}
		if !validFile(file) {
			return nil, reject("照片紀錄 closet.photo/%s 的 file（%s）不是 avatars/ 或 photos/ 底下的一般圖檔，沒有升", c.old.ID, file)
		}
		if to == "" {
			to = officialFile(file)
		}
		from := filepath.Join("personas", "mine", filepath.FromSlash(file))
		if _, err := os.Lstat(filepath.Join(home, from)); resume && os.IsNotExist(err) {
			continue // 接著做：這張上次已經改名成備份
		}
		if err := noLinks(home, from); err != nil {
			return nil, err
		}
		to = official(home, filepath.FromSlash(to))
		if c.done && sameOrMissing(filepath.Join(home, from), at(home, to)) != nil {
			continue // 接著做：紀錄已經是官方的，主人那份跟官方的不一樣（不是上次留下的）就不碰
		}
		if err := sameOrMissing(filepath.Join(home, from), at(home, to)); err != nil {
			return nil, err
		}
		_, statErr := os.Lstat(at(home, to))
		moves = append(moves, Move{From: from, To: to, Backup: from + ".bak-" + stamp + "-promote", Written: os.IsNotExist(statErr)})
	}
	if len(moves) == 0 && !resume {
		return nil, reject("personas/mine/ 裡沒有這套照片紀錄指著的圖")
	}
	sort.Slice(moves, func(i, j int) bool { return moves[i].From < moves[j].From })
	return moves, nil
}

// validFile：照片紀錄的 file 是 avatars/ 底下（舊的檔）或 photos/<名字>（S3 起新上傳的）的一般圖檔（相對路徑、沒有 ..、
// 不以 . 開頭、圖或影片的副檔名）。升成官方只能寫官方位置的 avatars/（package.toml 宣告 files:personas/avatars）。
func validFile(file string) bool {
	if file == "" || strings.ContainsAny(file, "\\\x00") || path.IsAbs(file) || path.Clean(file) != file {
		return false
	}
	segs := strings.Split(file, "/")
	if len(segs) < 2 || (segs[0] != "avatars" && !(segs[0] == minePhotos && len(segs) == 2)) {
		return false
	}
	for _, seg := range segs {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return mediaExt[strings.ToLower(path.Ext(file))]
}

// minePhotos 是主人新上傳的照片在 personas/mine/ 底下的資料夾；升成官方搬到官方的 avatars/photos/。
const minePhotos = "photos"

// officialFile 是主人的 file（相對 personas/mine/）升成官方後的 file（相對官方位置）：photos/X → avatars/photos/X，avatars/… 不變。
func officialFile(file string) string {
	if strings.HasPrefix(file, minePhotos+"/") {
		return "avatars/" + file
	}
	return file
}

// mineFile 反過來：官方紀錄的 file 原本在 personas/mine/ 的哪裡（接著做時找主人那份還在不在）。
func mineFile(file string) string {
	if rest, ok := strings.CutPrefix(file, "avatars/"+minePhotos+"/"); ok && !strings.Contains(rest, "/") {
		return minePhotos + "/" + rest
	}
	return file
}

// noLinks：從 personas/mine/ 到這個檔，中間的資料夾與檔本身都不能是捷徑（符號連結），免得讀到或改名到別的地方。
func noLinks(home, rel string) error {
	cur := filepath.Join(home, "personas", "mine")
	parts := strings.Split(filepath.ToSlash(rel), "/")[2:] // 去掉 personas/mine
	for i, p := range parts {
		cur = filepath.Join(cur, p)
		info, err := os.Lstat(cur)
		if err != nil {
			return reject("%s 不見了，沒有升", rel)
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) || (i == len(parts)-1 && !info.Mode().IsRegular()) {
			return reject("%s 不是一般的檔案（可能是捷徑），沒有升", rel)
		}
	}
	return nil
}

// putPhotos 改照片紀錄：換編號的新增一筆（outfit、of 換成新的；要搬的改 source = official、file 換成官方的位置 officialFile），原地的照版本改；
// 已經改好的（接著做）、新的那筆已經在的不重寫。封存舊的在 Run 裡做（要先封存舊的衣服）。
func putPhotos(ctx context.Context, v Vault, changes []photoChange, record, newRecord string) error {
	for _, c := range changes {
		if c.done {
			continue
		}
		fields := map[string]any{}
		for k, val := range c.old.Fields {
			if !coreField[k] {
				fields[k] = val
			}
		}
		fields["outfit"] = newRecord
		if of := text(fields["of"]); of != "" {
			fields["of"] = renameID(of, record, newRecord)
		}
		if c.move {
			fields["source"], fields["file"] = "official", officialFile(text(fields["file"]))
		}
		rev := c.old.Revision
		if c.id != c.old.ID {
			rev = ""
			if cur, err := v.Get(ctx, kindPhoto, c.id); err != nil {
				return err
			} else if cur != nil {
				continue // 上次已經寫好（封存過的編號也不能再用）
			}
		}
		if err := v.Put(ctx, sdk.Record{Kind: kindPhoto, ID: c.id, Fields: fields, Body: c.old.Body}, rev); err != nil {
			return err
		}
	}
	return nil
}

// sameOrMissing：目的地沒有檔，或已經是一樣的內容（重跑）才可以；不同的檔不蓋。
func sameOrMissing(src, dst string) error {
	info, err := os.Lstat(dst)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return reject("官方位置的 %s 已經有東西，沒有升", filepath.Base(dst))
	}
	a, err1 := os.ReadFile(src)
	b, err2 := os.ReadFile(dst)
	if err1 != nil || err2 != nil || !bytes.Equal(a, b) {
		return reject("官方位置已經有不同的 %s，沒有升（換一個 new_id，或先移走那個檔）", filepath.Base(dst))
	}
	return nil
}

// copyNew 把 src 複製到 dst；回傳這次有沒有真的寫（dst 原本就是一樣的內容時不寫）。先寫暫存檔再改名。
func copyNew(src, dst string) (bool, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return false, fmt.Errorf("讀不了 %s：%w", src, err)
	}
	if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, fmt.Errorf("無法建立資料夾 %s：%w", filepath.Dir(dst), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".promote-*")
	if err != nil {
		return false, err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return false, fmt.Errorf("無法寫入 %s", dst)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		_ = os.Remove(tmp.Name())
		return false, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		_ = os.Remove(tmp.Name())
		return false, err
	}
	return true, nil
}

// removeWritten 刪掉這次才寫進官方位置的圖（改紀錄失敗時，免得留下沒有紀錄的官方圖被建包帶出去）。
func removeWritten(home string, moves []Move) {
	for _, m := range moves {
		if m.Written {
			_ = os.Remove(at(home, m.To))
		}
	}
}

// rotationsToFix：輪播設定（closet.rotation/<人設>）裡有勾這套的。
func rotationsToFix(ctx context.Context, v Vault, slug, outfit string) ([]sdk.Record, error) {
	list, err := v.List(ctx, kindRotation, false)
	if err != nil {
		return nil, err
	}
	var out []sdk.Record
	for _, r := range list {
		if r.ID != slug || r.Archived {
			continue
		}
		for _, x := range asStrings(r.Fields["outfits"]) {
			if x == outfit {
				out = append(out, r)
				break
			}
		}
	}
	return out, nil
}

func asStrings(v any) []string {
	var out []string
	switch l := v.(type) {
	case []any:
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, l...)
	}
	return out
}

func replaceOutfit(v any, old, new string) []any {
	out := []any{}
	for _, s := range asStrings(v) {
		if s == old {
			s = new
		}
		out = append(out, s)
	}
	return out
}

// undoSteps：退回用現成的指令與改檔名就做得到（vault 封存的那筆拿不回來，所以換了 id 時是把新的那筆改回 mine，不是復活舊的）。
// 檔名沒改：刪掉這次寫進官方位置的圖、把備份改回原名，照片紀錄改回 source = mine（photos/ 來的 file 也改回去）就指回原來的圖。
func undoSteps(r *Result) []string {
	var steps []string
	for _, m := range r.Files {
		if m.Written {
			steps = append(steps, fmt.Sprintf("刪掉 %s", m.To))
		}
		steps = append(steps, fmt.Sprintf("把 %s 改回 %s", m.Backup, m.From))
	}
	for _, c := range r.photos {
		if !c.move {
			continue
		}
		file := text(c.old.Fields["file"])
		if c.done {
			file = mineFile(file)
		}
		if f := officialFile(file); f != file {
			steps = append(steps, fmt.Sprintf("evergenie data set closet.photo %s --json '{\"source\":\"mine\",\"file\":\"%s\"}'", c.id, file))
		} else {
			steps = append(steps, fmt.Sprintf("evergenie data set closet.photo %s --json '{\"source\":\"mine\"}'", c.id))
		}
	}
	steps = append(steps, fmt.Sprintf("evergenie data set closet.outfit %s --json '{\"source\":\"mine\"}'", r.To))
	if r.From == r.To {
		return steps
	}
	return append(steps,
		fmt.Sprintf("（舊編號 %s 與它的照片紀錄留在封存裡；新的那筆 from 指著它，兌換過的照樣擁有，輪播設定不用改。舊的圖備份 *.bak-*-promote 30 天後可刪）", r.From))
}
