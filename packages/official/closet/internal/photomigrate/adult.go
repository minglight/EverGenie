package photomigrate

import (
	"regexp"
	"strings"

	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
)

// adultName 是舊資料認隱藏版用的名字關鍵字（任一段含這些字，不分大小寫）。
var adultName = regexp.MustCompile(`(?i)(adult|nsfw|r18|成人)`)

// IsAdultDir：路徑（用 /）是不是看起來是隱藏版——有一段叫 level（舊家放這種圖的資料夾，例 avatars/level/hidden），
// 或任一段的名字含 adult／nsfw／r18／成人；寧可多擋。
//
// DEV-160 起官方套件裡只有這裡「看名字猜隱藏版」（以前衣櫃、closet-mine 升成官方、頁面各抄一份）：只在轉換舊圖時用，
// 看起來像的照片一律登記成 hidden = true；之後各處只讀紀錄的 hidden。closet-mine 新上傳的照片（S3 起存在
// personas/mine/photos/，檔名沒有意思）一上傳就有紀錄、hidden 只看請求，轉換器不會再看到它們。
// 跟核心的 personaref.IsAdultPath 同一份規則（核心只在沒有照片紀錄可讀的時候當保底），一致性由
// TestIsAdultDirSharedCases 讀核心的同一組案例（personaref/testdata/adult_paths.txt）守。
func IsAdultDir(dir string) bool {
	for _, seg := range strings.Split(strings.Trim(dir, "/"), "/") {
		if strings.EqualFold(seg, "level") || adultName.MatchString(seg) {
			return true
		}
	}
	return false
}

// OwnerHidden：這張隱藏版的照片算不算「自己的隱藏版」（要 closet.adult 開著才列，CLO-17、CLO-18 的開關規則不變）——
// 放在 personas/mine/ 的（source = mine），或放在官方位置、路徑看起來是自己的隱藏版的（舊家的 avatars/level/…、名字含 adult 等）。
// 後者是過渡：舊家把自己的隱藏版放在官方位置，DEV-154 的轉換把它們搬進 personas/mine/ 之後就只剩 source = mine；
// 在那之前寧可多擋（憲法第 10 條：安全底線不因方便放寬）。只用在紀錄的 source 與 file，是不是隱藏版仍只看 hidden。
func OwnerHidden(source, file string) bool { return source == rules.SourceMine || IsAdultDir(file) }

// OwnerVariant：這筆隱藏版設定是不是自己的隱藏版的（dir 在 mine/ 底下，或看起來是自己的隱藏版；過渡同 OwnerHidden）。
// 開關關著時它的開關與時段不算進那一組（rules.HiddenGroup）。
func OwnerVariant(v rules.Variant) bool { return rules.MineVariant(v) || IsAdultDir(v.Dir) }
