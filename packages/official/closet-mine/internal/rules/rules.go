// Package rules 是「上傳衣服與角色」的規則（純函式，沒有 I/O）：辨認檔案類型（看開頭，不看檔名）、代號與衣服編號、
// 請求欄位的檢查、各種請求的拒絕原因（REQ-126）。
package rules

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// 請求種類。
const (
	TypeCharacter = "character"
	TypeOutfit    = "outfit"
	TypePhoto     = "photo"
	TypeFull      = "full"
	TypeVideo     = "video"
	// TypePromote：升成官方（DEV-141）。只在開發家、只認主人本人送的；不帶上傳檔（細節在 internal/promote）。
	TypePromote = "promote"
	// 不帶上傳檔的整理（DEV-160 S3）：刪單張／拿回（photo_id）、刪一套／拿回（persona＋outfit；拿回也把下架的我的衣服改回上架）、
	// 改要幾點（persona＋outfit＋cost）。
	// 刪＝封存紀錄（單張的檔改名成備份），都拿得回來；只改我的衣服、我的照片。
	// S5c 加過的 publish（一般家「上架」）沒上線過，2026-11-15 拿掉（一般家沒有上架／下架，CMI-23 作廢）。
	TypeRemovePhoto   = "remove_photo"
	TypeRestorePhoto  = "restore_photo"
	TypeRemoveOutfit  = "remove_outfit"
	TypeRestoreOutfit = "restore_outfit"
	TypeSetCost       = "set_cost"
)

// NeedsUpload：這種請求要不要帶上傳檔（新角色、新衣服、照片、全身照、動畫才要）。
func NeedsUpload(t string) bool {
	switch t {
	case TypeCharacter, TypeOutfit, TypePhoto, TypeFull, TypeVideo:
		return true
	}
	return false
}

// 請求狀態。
const (
	StatePending  = "pending"
	StateDone     = "done"
	StateRejected = "rejected"
)

const (
	// MaxUploadBytes：上傳檔的大小上限（跟核心的上傳上限一樣）。
	MaxUploadBytes = 20 << 20
	// MaxPhotos：一套最多 12 張輪播照片。
	MaxPhotos = 12
	// MaxNameRunes：角色名與衣服名最多 40 字。
	MaxNameRunes = 40
	// SharePrefix 是看板上傳後給的網址前綴。
	SharePrefix = "/share/"
	// BaseOutfit 是每位角色的第一套。
	BaseOutfit = "base"
)

// 白話的拒絕原因（寫進請求的 reason，頁面直接顯示）。
const (
	ReasonNoUpload     = "找不到上傳的檔案，請重新上傳"
	ReasonImageOnly    = "只收 PNG、JPEG、WEBP 圖片"
	ReasonVideoOnly    = "動畫只收 MP4"
	ReasonBadCode      = "代號要用 my 開頭的小寫英數"
	ReasonCodeTaken    = "這個代號已經有人用了"
	ReasonNoPersona    = "找不到這位角色"
	ReasonNoOutfit     = "衣櫃裡沒有這套衣服"
	ReasonOfficial     = "官方的衣服不能改"
	ReasonTooManyPhoto = "一套最多 12 張"
	ReasonOutfitTaken  = "這套衣服的編號已經被用掉了，請重新送出"
	ReasonUnknownType  = "不認得的請求種類"
	ReasonNeedName     = "請填名稱"
	ReasonNameTooLong  = "名稱最多 40 字"
	ReasonNeedPersona  = "請選擇哪位角色"
	ReasonNeedOutfit   = "請選擇哪套衣服"
	ReasonBadCost      = "價錢要是 0 到 99 的整數（0＝免費）"
	// ReasonAdultOff：自己的隱藏版的家設定沒打開（衣櫃的擴充點 closet.adult 不是 on）。
	ReasonAdultOff = "自己的隱藏版沒有打開：要主人在看板「設定 → 套件」把衣櫃的 adult 設成 on，才能放進自己的隱藏版"
	// ReasonAdultKind：自己的隱藏版只是已經有的衣服的另一組圖，不另做角色或衣服。
	ReasonAdultKind = "自己的隱藏版只能替已經有的衣服放照片、全身照或動畫，不另做角色或衣服"
	// ReasonUnsafeFolder：personas/mine 或裡面的資料夾是符號連結（或被換成檔案），程式不寫。
	// ReasonNotDev：升成官方只在開發家做。
	ReasonNotDev = "這要到開發家做：升成官方只在開發家（home.toml [runtime] env = \"dev\"）能用，正式家的衣服留在 personas/mine/"
	// ReasonNotOwnerPromote：升成官方只認主人本人送的請求（秘書送的會被拒）。
	ReasonNotOwnerPromote = "只有主人本人可以把衣服升成官方"
	// ReasonNeedTarget：升成官方要寫哪一套。
	ReasonNeedTarget = "請寫要升哪一套：outfit 是 <人設>-<衣服 id>（例：athena-my-12）"
	// ReasonAdultVariantGone：自己的隱藏版那一筆被封存或資料夾改掉了，放了圖也不會顯示。
	ReasonAdultVariantGone = "自己的隱藏版不見了（closet.variant/adult 被封存，或它的資料夾不是 mine/adult），放了圖也不會顯示。" +
		"要恢復：資料夾被改的話 evergenie data set closet.variant adult --json '{\"dir\":\"mine/adult\"}'；" +
		"被封存的話另建一筆 evergenie data add closet.variant --id adult-2 --json '{\"name\":\"自己的隱藏版\",\"dir\":\"mine/adult\",\"button\":true,\"on\":false}'，再重新送一次"
	ReasonUnsafeFolder = "角色資料夾 personas/mine 不是一般資料夾（可能是捷徑），為了安全沒有寫入，請跟任何一位秘書說"
	// ReasonFileTaken：照片的新檔名（照片編號）已經有別的檔，不蓋。
	ReasonFileTaken = "照片的檔名已經有別的檔了，為了安全沒有寫入，請跟任何一位秘書說"

	// 照片與整套的整理（DEV-160 S3）。
	ReasonNeedPhotoID      = "請寫是哪一張：photo_id 是照片紀錄（closet.photo）的編號"
	ReasonNoPhotoRecord    = "找不到這張照片（photo_id 不對，或已經刪了）"
	ReasonPhotoOtherOutfit = "這張照片不是這套衣服的"
	ReasonPhotoWrongType   = "這張照片的種類不對：photo 換半身照、full 換全身照、video 換動畫（或寫配哪一張半身照）"
	ReasonPhotoHiddenDiff  = "一般版和隱藏版不能互換：這張是隱藏版的要帶 adult = true，一般版的不要帶（或寫 adult = false）"
	// ReasonAllHidden：這套衣服現在的照片全都是隱藏版，沒寫 adult 的新照片也算隱藏版（照紀錄，不看名字），而開關沒開。
	ReasonAllHidden = "這套衣服現在的照片全都是隱藏版，沒說是一般版的新照片也算隱藏版，但自己的隱藏版沒有打開（主人要在看板「設定 → 套件」把衣櫃的 adult 設成 on）；" +
		"要另外放一般版的照片，請秘書送請求時寫 adult = false"
	ReasonOfficialPhoto  = "官方的照片不能刪或拿回"
	ReasonLastHalf       = "這是這套最後一張一般版的半身照，不能單獨刪；整套不要了請用刪一套"
	ReasonRestoreOutfit  = "這套衣服已經刪了，要先拿回整套"
	ReasonRestoreHalf    = "這支動畫配的那張半身照已經刪了，要先拿回那張"
	ReasonFullTaken      = "這套現在已經有全身照了，先刪掉現在那張才能拿回"
	ReasonBackupGone     = "刪掉的檔不見了（備份可能已經清掉），拿不回來"
	ReasonBaseOutfit     = "每位角色的第一套（基本款）不能刪"
	ReasonBaseCost       = "每位角色的第一套（基本款）一定免費，不能改點數"
	ReasonOutfitPromoted = "這套已經升成官方了（用新的編號），不能拿回舊的"
	// ReasonOfficialReplace：官方的照片只有開發家能換（CMI-25：核心說是開發家、衣櫃的能力也有 edit）；還不知道這個家支援什麼（衣櫃還沒寫能力）也不換。
	ReasonOfficialReplace = "官方的照片只能在開發家換（這個家不能修改官方的衣服）"
	// ReasonOfficialHidden：官方衣服的隱藏版照片不從換圖請求換（換到 avatars/photos/ 之後分不出是不是自己的隱藏版，CMI-25）。
	ReasonOfficialHidden = "官方衣服的隱藏版照片不能用換圖請求換（換了會分不出是不是自己的隱藏版、不再跟著那個開關）；要換請跟主人說"
	// ReasonOfficialFile：這張官方照片的檔不在官方位置的 avatars/ 底下（舊資料），換圖寫不了（CMI-25）。
	ReasonOfficialFile = "這張官方照片的檔不在官方角色的 avatars/ 資料夾裡，換圖請求換不了；要換請跟主人說"
	// ReasonOfficialLayer：官方角色的位置在家外面（跟著角色包更新），這個家寫不進去（CMI-25）。
	ReasonOfficialLayer = "這個家的官方角色跟著角色包更新、放在家外面，官方的圖寫不進去；要換官方的圖，請在官方角色放在家裡的開發家換"
)

// MaxClips：同一張照片最多 6 支動畫（跟 closet 的 pub.MaxVideos 一致）。
const MaxClips = 6

// ReasonNoClip：video 的 clip 不在 1～MaxClips。
func ReasonNoClip(n int) string {
	return fmt.Sprintf("動畫只能放第 1 到第 %d 支，沒有第 %d 支", MaxClips, n)
}

// ReasonClipGap：video 的 clip 前面還缺一支（closet 要連續才認）。
func ReasonClipGap(n int) string {
	return fmt.Sprintf("要先有第 %d 支動畫，才能放第 %d 支", n-1, n)
}

// ReasonNoPhoto：video 指定的照片不存在。
func ReasonNoPhoto(n int) string { return fmt.Sprintf("沒有第 %d 張照片", n) }

var (
	// 自己的角色代號：my_<名字>（DEV-154）；以前的寫法 my<名字>（沒有底線）照樣收，已經在用的請求與外部工具不會壞
	mineCodePattern = regexp.MustCompile(`^my(_[a-z0-9]{1,24}|[a-z0-9]{1,20})$`)
	personaPattern  = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	outfitPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	uploadPattern   = regexp.MustCompile(`^[^/\\\x00]+$`)
	// photoIDPattern 跟 vault 的紀錄編號一樣（closet.photo 的編號）。
	photoIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

// ---- 檔案類型 ----

// FileKind 是看檔案開頭認出的類型。
type FileKind int

// 認得的檔案類型。
const (
	KindUnknown FileKind = iota
	KindPNG
	KindJPEG
	KindWEBP
	KindMP4
)

// Sniff 看檔案開頭認類型：PNG（\x89PNG\r\n\x1a\n）、JPEG（FF D8 FF）、WEBP（RIFF????WEBP）、MP4（第 4–8 位元組 ftyp）。
// 完全不看檔名。
func Sniff(head []byte) FileKind {
	switch {
	case len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n":
		return KindPNG
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return KindJPEG
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return KindWEBP
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		return KindMP4
	}
	return KindUnknown
}

// IsImage：PNG、JPEG、WEBP。
func (k FileKind) IsImage() bool { return k == KindPNG || k == KindJPEG || k == KindWEBP }

// Ext 是這種類型存檔用的副檔名（不含點）；不認得回空字串。
func (k FileKind) Ext() string {
	switch k {
	case KindPNG:
		return "png"
	case KindJPEG:
		return "jpg"
	case KindWEBP:
		return "webp"
	case KindMP4:
		return "mp4"
	}
	return ""
}

// CheckKind：video 只收 MP4，其他只收圖。回拒絕原因，空字串＝通過。
func CheckKind(reqType string, k FileKind) string {
	if reqType == TypeVideo {
		if k != KindMP4 {
			return ReasonVideoOnly
		}
		return ""
	}
	if !k.IsImage() {
		return ReasonImageOnly
	}
	return ""
}

// ---- 代號與編號 ----

// SeqOf 取請求編號 U-12 的數字；格式不對回 0, false。
func SeqOf(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "U-")
	if !ok || rest == "" || len(rest) > 9 {
		return 0, false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil
}

// ValidMineCode：自訂代號 my_<名字>（或以前的 my<名字>）。
func ValidMineCode(s string) bool { return mineCodePattern.MatchString(s) }

// DefaultCode：沒給代號時用 my_<請求編號數字>（U-12 → my_12，可重算）。
func DefaultCode(reqID string) (string, bool) {
	n, ok := SeqOf(reqID)
	if !ok {
		return "", false
	}
	return "my_" + strconv.Itoa(n), true
}

// OutfitIDFor：替角色加衣服時的衣服 id（U-12 → my-12，可重算）。
func OutfitIDFor(reqID string) (string, bool) {
	n, ok := SeqOf(reqID)
	if !ok {
		return "", false
	}
	return "my-" + strconv.Itoa(n), true
}

// ValidPersona／ValidOutfit 跟 closet 的格式一樣（編號 = 人設-衣服）。
func ValidPersona(s string) bool {
	return personaPattern.MatchString(s) || mineCodePattern.MatchString(s)
}

// ValidOutfit：衣服 id ^[a-z0-9][a-z0-9-]*$。
func ValidOutfit(s string) bool { return outfitPattern.MatchString(s) }

// RecordID 是 closet.outfit 的編號。
func RecordID(persona, outfit string) string { return persona + "-" + outfit }

// PhotoIDFor 是這個請求新登記的照片的編號：<衣服編號>--u<請求編號數字>（U-15 → athena-my-12--u15，可重算，同一個請求重播得到同一個；
// DEV-160 S3）。編號只是名字，不從種類、第幾張推；檔名就是 photos/<編號>.<副檔名>。
func PhotoIDFor(record, reqID string) (string, bool) {
	n, ok := SeqOf(reqID)
	if !ok {
		return "", false
	}
	return record + "--u" + strconv.Itoa(n), true
}

// ValidPhotoID：照片紀錄的編號格式（小寫英數、_、-）。
func ValidPhotoID(s string) bool { return photoIDPattern.MatchString(s) }

// ---- 上傳檔名 ----

// UploadName 把 `/share/<名>` 或 `<名>` 變成單一檔名；含 `/`、`\`、NUL、`..`、以 `.` 開頭的一律不收。
func UploadName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, SharePrefix)
	if s == "" || !uploadPattern.MatchString(s) || strings.HasPrefix(s, ".") || strings.Contains(s, "..") {
		return "", false
	}
	return s, true
}

// ---- 請求 ----

// Request 是一筆 closet-mine.request 的內容（唯讀的解析結果）。
type Request struct {
	ID      string
	Type    string
	Upload  string // 還沒正規化的原文
	Persona string
	Outfit  string
	Name    string
	Desc    string
	Free    bool
	// Cost：只給 outfit，兌換要幾點（0＝免費）；有寫就照它，沒寫照舊看 Free（true＝0，預設 1）。
	Cost    int
	HasCost bool
	BadCost bool // cost 有寫但不是整數
	Draft   bool
	Photo   int // video 用：配這套（同一版）第幾張半身照，照衣櫃排的順序（頁面看到的位置）；沒寫是 1
	Clip    int // video 用：那張照片的第幾支動畫（同樣是位置，DEV-125）；沒寫是 1。等於現有支數 +1 是加一支，其他是換那一支
	// PhotoID：照片紀錄的編號（DEV-160 S3）。remove_photo、restore_photo 必填；photo、full、video 寫了就是換那一張
	// （video 寫半身照的編號＝配那一張，寫動畫的編號＝換那一支）。
	PhotoID string
	// Adult：自己的隱藏版的圖（photo、full、video；DEV-141），檔一樣放 personas/mine/photos/，照片紀錄 hidden = true；家設定沒打開就拒絕。
	// AdultGiven：請求有沒有寫 adult（寫了 false＝明說是一般版；沒寫才照這套現有的照片紀錄，UploadHidden）。
	Adult, AdultGiven bool
	// NewID、DryRun：只給 promote（新的衣服 id；true＝只預演）。
	NewID  string
	DryRun bool
}

// MaxCost 是自己的衣服最多能設幾點。
const MaxCost = 99

// OutfitCost 是新衣服的價錢：有寫 cost 照它；沒寫時 free＝0、預設 1（跟官方衣服一樣）。
func (r Request) OutfitCost() int {
	switch {
	case r.HasCost:
		return r.Cost
	case r.Free:
		return 0
	}
	return 1
}

// IsOwner：請求是不是主人本人（Actor 名稱 owner(<入口>)）送的；秘書、系統、套件都不算。
func IsOwner(by string) bool { return strings.HasPrefix(by, "owner(") && strings.HasSuffix(by, ")") }

// AdultFolder 是自己的隱藏版那一筆 closet.variant 的資料夾（相對 personas/mine/；closet 的隱藏版 dir = mine/adult）。
// DEV-160 S3 起新上傳的隱藏版照片跟一般的一樣放 photos/、靠照片紀錄的 hidden 分；這個資料夾只剩舊的圖。
const AdultFolder = "adult"

// UploadHidden：這次上傳的照片登記成隱藏版嗎（DEV-160 S3；作者 2026-11-15：隱藏只是照片紀錄上的 hidden，不看名字分類）——
// 請求寫 adult = true；沒寫 adult、而這套衣服現在的照片紀錄全都是隱藏版（allHidden）也算，不然這套會冒出一張一般版的。
// 寫了 adult = false 就是一般版。衣服名、角色代號長什麼樣都不管（名字規則只留在衣櫃轉換器登記舊圖時用）。
func (r Request) UploadHidden(allHidden bool) bool { return r.Adult || (!r.AdultGiven && allHidden) }

// Text 取字串欄位（去頭尾空白）。
func Text(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// Int 取整數欄位（YAML／JSON 解出來可能是 int 或 float64）。
func Int(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
}

// Strings 取字串清單欄位（YAML／JSON 解出來是 []any 或 []string；不是字串的、空的略過）。
func Strings(v any) []string {
	var out []string
	switch l := v.(type) {
	case []string:
		for _, s := range l {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	case []any:
		for _, x := range l {
			if s := Text(x); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// Bool 取布林欄位。
func Bool(v any) bool {
	b, _ := v.(bool)
	return b
}

// ParseRequest 把 vault 欄位轉成 Request（不檢查合不合格，檢查在 Validate）。
func ParseRequest(id string, f map[string]any) Request {
	r := Request{
		ID: id, Type: Text(f["type"]), Upload: Text(f["upload"]), Persona: Text(f["persona"]), Outfit: Text(f["outfit"]),
		Name: Text(f["name"]), Desc: Text(f["desc"]), Free: Bool(f["free"]), Draft: Bool(f["draft"]), Adult: Bool(f["adult"]), NewID: Text(f["new_id"]), DryRun: Bool(f["dry_run"]), Photo: 1, Clip: 1,
		PhotoID: Text(f["photo_id"]),
	}
	if v, ok := f["adult"]; ok && v != nil {
		r.AdultGiven = true
	}
	if n, ok := Int(f["photo"]); ok {
		r.Photo = n
	}
	if n, ok := Int(f["clip"]); ok {
		r.Clip = n
	}
	if v, ok := f["cost"]; ok && v != nil {
		r.HasCost = true
		r.Cost, ok = Int(v)
		r.BadCost = !ok
	}
	return r
}

func checkName(name string) string {
	switch {
	case name == "":
		return ReasonNeedName
	case utf8.RuneCountInString(name) > MaxNameRunes:
		return ReasonNameTooLong
	}
	return ""
}

// Validate 只看請求本身（不看檔案、不看 vault 現況）；回拒絕原因，空字串＝通過。
func Validate(r Request) string {
	switch r.Type {
	case TypePromote:
		if r.Outfit == "" {
			return ReasonNeedTarget
		}
		return ""
	case TypeRemovePhoto, TypeRestorePhoto:
		switch {
		case r.PhotoID == "":
			return ReasonNeedPhotoID
		case !ValidPhotoID(r.PhotoID):
			return ReasonNoPhotoRecord
		case r.Persona != "" && !ValidPersona(r.Persona):
			return ReasonNoPersona
		}
		return ""
	case TypeRemoveOutfit, TypeRestoreOutfit, TypeSetCost:
		switch {
		case r.Persona == "":
			return ReasonNeedPersona
		case r.Outfit == "":
			return ReasonNeedOutfit
		case !ValidPersona(r.Persona) || !ValidOutfit(r.Outfit):
			return ReasonNoOutfit
		case r.Type == TypeRemoveOutfit && r.Outfit == BaseOutfit:
			return ReasonBaseOutfit
		case r.Type == TypeSetCost && r.Outfit == BaseOutfit:
			return ReasonBaseCost
		case r.Type == TypeSetCost && (!r.HasCost || r.BadCost || r.Cost < 0 || r.Cost > MaxCost):
			return ReasonBadCost
		}
		return ""
	}
	if _, ok := UploadName(r.Upload); !ok {
		return ReasonNoUpload
	}
	if r.Adult && (r.Type == TypeCharacter || r.Type == TypeOutfit) {
		return ReasonAdultKind
	}
	switch r.Type {
	case TypeCharacter:
		if reason := checkName(r.Name); reason != "" {
			return reason
		}
		if r.Persona != "" && !ValidMineCode(r.Persona) {
			return ReasonBadCode
		}
		if _, ok := DefaultCode(r.ID); !ok && r.Persona == "" {
			return ReasonBadCode
		}
	case TypeOutfit:
		if r.Persona == "" {
			return ReasonNeedPersona
		}
		if !ValidPersona(r.Persona) {
			return ReasonNoPersona
		}
		if reason := checkName(r.Name); reason != "" {
			return reason
		}
		if r.HasCost && (r.BadCost || r.Cost < 0 || r.Cost > MaxCost) {
			return ReasonBadCost
		}
		if _, ok := OutfitIDFor(r.ID); !ok {
			return ReasonUnknownType
		}
	case TypePhoto, TypeFull, TypeVideo:
		if r.Persona == "" {
			return ReasonNeedPersona
		}
		if r.Outfit == "" {
			return ReasonNeedOutfit
		}
		if !ValidPersona(r.Persona) || !ValidOutfit(r.Outfit) {
			return ReasonNoOutfit
		}
		if r.Type == TypeVideo && r.Photo < 1 {
			return ReasonNoPhoto(r.Photo)
		}
		if r.Type == TypeVideo && (r.Clip < 1 || r.Clip > MaxClips) {
			return ReasonNoClip(r.Clip)
		}
		if r.PhotoID != "" && !ValidPhotoID(r.PhotoID) {
			return ReasonNoPhotoRecord
		}
	default:
		return ReasonUnknownType
	}
	return ""
}

// CharacterCode 是 character 請求要用的代號（給了就用，沒給用 my_<編號>）。Validate 通過之後才呼叫。
func CharacterCode(r Request) string {
	if r.Persona != "" {
		return r.Persona
	}
	code, _ := DefaultCode(r.ID)
	return code
}

// Facts 是 vault 與磁碟的現況（由 run 填），給 CheckFacts 判斷。
type Facts struct {
	// character
	CodeTaken bool // 官方角色卡、自己的角色卡、base 衣服任一已存在，而且不是同一個請求上次做了一半
	// outfit
	PersonaExists bool
	OutfitTaken   bool // 這個請求會用的衣服編號已被別人用掉
	// photo／full／video
	OutfitExists bool
	OutfitSource string // official｜mine
	PhotoCount   int    // 這套（同一版：一般或隱藏版）現有幾張半身照（照片紀錄，同一個 order 算一張）
	ClipCount    int    // video：指定那張照片現有幾支動畫（照片紀錄）
	// photo：現有任何一張跟上傳檔位元組相同（同一張送兩次）
	DuplicatePhoto bool
	// Replace：photo 是換現有的一張（photo_id），不多一張、不受 12 張的限制
	Replace bool
}

// CheckFacts 依現況判斷這個請求能不能做；回拒絕原因，空字串＝通過。
func CheckFacts(r Request, f Facts) string {
	switch r.Type {
	case TypeCharacter:
		if f.CodeTaken {
			return ReasonCodeTaken
		}
	case TypeOutfit:
		if !f.PersonaExists {
			return ReasonNoPersona
		}
		if f.OutfitTaken {
			return ReasonOutfitTaken
		}
	case TypePhoto, TypeFull, TypeVideo:
		if !f.OutfitExists {
			return ReasonNoOutfit
		}
		// 自己的隱藏版的圖放在 personas/mine/、不碰官方的圖，所以官方的衣服也可以放；正常版只能改我的衣服。
		if f.OutfitSource != "mine" && !r.Adult {
			return ReasonOfficial
		}
		switch r.Type {
		case TypePhoto:
			if f.PhotoCount >= MaxPhotos && !f.DuplicatePhoto && !f.Replace {
				return ReasonTooManyPhoto
			}
		case TypeVideo:
			if r.Photo > f.PhotoCount {
				return ReasonNoPhoto(r.Photo)
			}
			if r.Clip > f.ClipCount+1 {
				return ReasonClipGap(r.Clip)
			}
		}
	}
	return ""
}

// ---- 角色卡 ----

// CharacterCard 產生 personas/mine/<代號>.md：`---` YAML 檔頭（schema: 1、name: 雙引號字串），內文＝desc。
func CharacterCard(name, desc string) string {
	var b strings.Builder
	b.WriteString("---\nschema: 1\nname: ")
	b.WriteString(YAMLQuote(name))
	b.WriteString("\n---\n")
	if desc = strings.TrimSpace(desc); desc != "" {
		b.WriteString(desc)
		b.WriteString("\n")
	}
	return b.String()
}

// YAMLQuote 把字串寫成雙引號 YAML 字串：跳脫 `\`、`"`、換行與其他控制字元。
func YAMLQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// CardName 讀角色卡檔頭的 name：`---` YAML 與 `+++` TOML 兩種都認，雙引號字串會還原跳脫；沒有回空字串。
func CardName(text string) string {
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
		if strings.HasPrefix(val, `"`) {
			if u, ok := unquoteYAML(val); ok {
				return strings.TrimSpace(u)
			}
		}
		if i := strings.Index(val, " #"); i >= 0 && !strings.HasPrefix(val, `'`) {
			val = strings.TrimSpace(val[:i])
		}
		return strings.TrimSpace(strings.Trim(val, `"'`))
	}
	return ""
}

// unquoteYAML 還原 YAMLQuote 寫出的雙引號字串（也相容 TOML 的基本字串：同樣的 \\ \" \n \r \t）。
func unquoteYAML(v string) (string, bool) {
	if len(v) < 2 || v[0] != '"' {
		return "", false
	}
	var b strings.Builder
	rs := []rune(v[1:])
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; r {
		case '"':
			return b.String(), true
		case '\\':
			i++
			if i >= len(rs) {
				return "", false
			}
			switch rs[i] {
			case '\\', '"':
				b.WriteRune(rs[i])
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'x':
				if i+2 >= len(rs) {
					return "", false
				}
				n, err := strconv.ParseUint(string(rs[i+1:i+3]), 16, 8)
				if err != nil {
					return "", false
				}
				b.WriteRune(rune(n))
				i += 2
			default:
				return "", false
			}
		default:
			b.WriteRune(r)
		}
	}
	return "", false
}
