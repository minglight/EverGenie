// Package personaref 是「角色」在名冊（agent）、角色庫（feature/agents/persona）、套件（feature/packages）之間共用的名詞：
// 角色代號的格式、套件角色的有效代號（<套件id>.<slug>）、來源與狀態的字、套件宣告的角色卡（資料），以及一張人設卡的讀取結果。
// 只有型別、常數與純函式，沒有 HTTP、沒有指令、不碰檔案（personas-2：從 internal/base/agent 搬來，兩邊的業務模組都能 import，不必互相 import）。
// 業務規則不放這裡（2026-11-01）：卡算不算設計中由角色庫（feature/agents/persona）判斷、填進 CardInfo.Status；套件 id 的格式由 packages 管；
// 角色庫要 packages 做的事（列出、寫入套件角色）是角色庫自己定義的 Deps 介面。
package personaref

import (
	"regexp"
	"strings"

	"github.com/minglight/evergenie/internal/base/vault"
)

const (
	SourceOfficial = "official"
	SourceMine     = "mine"
	SourcePackage  = "package" // 套件帶來的角色（唯讀，代號 <套件id>.<slug>）
	StatusLive     = "live"
	StatusDesign   = "design"
)

// SlugPattern 是官方角色（與套件內角色）代號的格式（檔名，不含 .md）。
var SlugPattern = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// MinePrefix 是主人自己的角色代號的開頭（DEV-154，作者 2026-11-12）：官方代號不收底線，永遠撞不到。
const MinePrefix = "my_"

// MineSlugPattern 是主人自己的角色代號：my_<名字>。
var MineSlugPattern = regexp.MustCompile(`^my_[a-z0-9]{1,24}$`)

// ValidSlug 回答 s 是不是一張卡的代號（官方的，或主人自己的 my_<名字>）。讀取、指派、檔名比對都用它；
// 主人以前建的、沒有 my_ 的自己的角色也是 SlugPattern 的樣子，搬家前照樣讀得到。
func ValidSlug(s string) bool { return SlugPattern.MatchString(s) || MineSlugPattern.MatchString(s) }

// MineSlug 把主人給的名字變成自己的角色代號：已經是 my_ 開頭的照用，其餘前面加 my_。格式對不對由 MineSlugPattern 驗。
func MineSlug(s string) string {
	if strings.HasPrefix(s, MinePrefix) {
		return s
	}
	return MinePrefix + s
}

// packageIDPart 是代號裡套件 id 那一段的格式：只用來拆代號（不含點，所以第一個點把代號切成套件與 slug）。
// 套件 id 本身的規則歸 packages（manifest 的 idPattern）；兩邊一致由 packages 的 TestPersonaCodeAcceptsPackageIDs 守。
var packageIDPart = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// PackageCode 組出套件角色的有效代號。
func PackageCode(pkgID, slug string) string { return pkgID + "." + slug }

// SplitPackageCode 把 <套件id>.<slug> 拆開；不是這個格式回 ok=false。
func SplitPackageCode(code string) (pkgID, slug string, ok bool) {
	pkgID, slug, ok = strings.Cut(code, ".")
	if !ok || !packageIDPart.MatchString(pkgID) || !SlugPattern.MatchString(slug) {
		return "", "", false
	}
	return pkgID, slug, true
}

// IsPackageCode 回答 code 是不是套件角色的有效代號。
func IsPackageCode(code string) bool { _, _, ok := SplitPackageCode(code); return ok }

// PackagePersona 是一個套件宣告的一張角色卡。
type PackagePersona struct {
	Package string // 套件 id
	Slug    string // 套件內的代號（SlugPattern）
	Path    string // 卡檔的絕對路徑（已確認在套件資料夾內）
	Tier    string // 套件層級（official｜installed｜mine）；只有 mine 能寫入（SEC-302 S3，U-2）
	Active  bool   // 套件有效（沒被拒、啟用中）；false＝不能新指派，但卡仍讀得到
	Reason  string // Active 是 false 時的白話原因（「已停用」「被拒」）
}

// CardInfo 是名冊需要知道的一張人設卡（讀角色庫的結果）。
type CardInfo struct {
	Record    vault.Record // 檔頭欄位（name…）與內文
	Source    string       // SourceOfficial｜SourceMine｜SourcePackage
	PackageID string       // Source 是 package 時：所屬套件 id
	Inactive  string       // 非空＝所屬套件停用或被拒（白話原因），這張卡不能新指派
	Status    string       // StatusLive｜StatusDesign：角色庫判斷（設計中的卡不能指派）
}

// ParseCard 讀人設卡的檔頭：`---` YAML 與舊的 `+++` TOML 都認。
func ParseCard(data []byte) (vault.Record, error) {
	var r vault.Record
	var err error
	if strings.HasPrefix(string(data), "+++") {
		r, _, err = vault.DecodeLegacy(data)
	} else {
		r, _, err = vault.Decode(data)
	}
	return r, err
}

// adultName 是自己的隱藏版資料夾的名字關鍵字（任何一段含這些字，不分大小寫）；跟角色包建包的規則同一份（personapack.Excluded，REQ-135）。
var adultName = regexp.MustCompile(`(?i)(adult|nsfw|r18|成人)`)

// IsAdultPath：personas/ 底下的相對路徑（用 /）是不是自己的隱藏版（識別字沿用 adult）——有一段叫 level（舊家放這種圖的資料夾，例 avatars/level/hidden），
// 或任一段的名字含 adult／nsfw／r18／成人（主人新放的自己的隱藏版在 mine/adult/，DEV-141）。寧可多擋，不靠檔案自己標。
// 角色匯出、角色包、衣櫃的自己的隱藏版開關、升成官方都照這一份；官方套件不能 import 核心，各自照抄一份，
// 一致性由 testdata/adult_paths.txt 的同一組案例守（personaref 與衣櫃轉換器 photomigrate 的測試都讀它；closet-mine 新上傳的照片
// 不看名字，只看請求與照片紀錄，DEV-160 S3）。
func IsAdultPath(rel string) bool {
	for _, seg := range strings.Split(strings.Trim(rel, "/"), "/") {
		if strings.EqualFold(seg, "level") || adultName.MatchString(seg) {
			return true
		}
	}
	return false
}
