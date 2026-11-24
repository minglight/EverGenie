package persona

// 主人自己的角色跟官方角色同代號（PRS-27）：`evergenie doctor` 的離線檢查多一條黃字，白話說是哪一張、角色庫現在用的是哪一張、
// 建議改成 my_ 開頭；不自動改（改代號要連名冊、圖與紀錄一起改，交給 home tidy 或主人決定）。

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// slugClash 是一張跟官方角色同代號的我的卡：bundled＝撞到的是跟著程式的公開卡（角色庫用我的），否則是官方角色資料夾的卡（角色庫用官方的）。
type slugClash struct {
	slug    string
	bundled bool
}

// slugClashes 找 <家>/personas/mine/ 頂層跟官方角色同代號的卡（依代號排）。my_ 開頭的撞不到（官方代號不收底線），
// 資料夾不是卡（跟角色庫的 cards() 一樣）；官方那邊照找官方卡的共用入口，只是不看我的卡（要知道「沒有我的卡時會是哪一張」）。
func slugClashes(home, bundledDir string) []slugClash {
	mineDir := filepath.Join(home, "personas", "mine")
	entries, err := os.ReadDir(mineDir)
	if err != nil {
		return nil
	}
	officialDir := platform.OfficialPersonasDir(home)
	var out []slugClash
	for _, e := range entries {
		slug, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || e.IsDir() || !personaref.SlugPattern.MatchString(slug) {
			continue
		}
		card, found := sdk.OfficialCardIn(officialDir, bundledDir, "", slug)
		if !found {
			continue
		}
		out = append(out, slugClash{slug: slug, bundled: card != filepath.Join(officialDir, slug+".md")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].slug < out[j].slug })
	return out
}

// SlugClashChecks 回 `evergenie doctor` 的一項離線檢查（registry 傳進 system.Commands 的 extra，跟 OfflineChecks 一樣）：
// 每張跟官方角色同代號的我的卡一條 warn。bundledCards 算跟著程式的公開卡資料夾（packages.PersonasBundledCards；角色庫不 import packages），
// nil＝只比官方角色資料夾。沒有家、沒有撞到回空。
func SlugClashChecks(bundledCards func(home string) string) func(context.Context, string) []sdk.DoctorCheck {
	return func(_ context.Context, home string) []sdk.DoctorCheck {
		if home == "" {
			return nil
		}
		bundled := ""
		if bundledCards != nil {
			bundled = bundledCards(home)
		}
		var out []sdk.DoctorCheck
		for _, c := range slugClashes(home, bundled) {
			out = append(out, slugClashCheck(c))
		}
		return out
	}
}

func slugClashCheck(c slugClash) sdk.DoctorCheck {
	card := "personas/mine/" + c.slug + ".md"
	mine := personaref.MinePrefix + c.slug
	check := sdk.DoctorCheck{ID: "personas.slug_clash", Group: sdk.DoctorGroupData, Severity: sdk.DoctorSeverityWarn}
	if c.bundled {
		// home tidy 會改這張（官方角色資料夾沒有同代號的卡，PRS-22 的一般規則），連名冊、圖與紀錄一起改。
		check.Detail = "你自己的角色 " + c.slug + "（" + card + "）跟這一版程式帶的公開角色同代號：角色庫與秘書用的是你的這張，公開的那位在這個家看不到"
		check.Fix = "建議改成 my_ 開頭的代號（現在不改也能用；之後官方角色包要是也帶這位，你的這張會被官方的蓋住）：" +
			"先停服務（`evergenie service stop`），跑 `evergenie home tidy` 看預演（會列出所有要改的卡，不只這張），確定再加 `--apply`，做完 `evergenie service start`；" +
			"這張會改成 " + mine + "，名冊、圖與紀錄一起改（想換名字加 `--rename " + c.slug + "=my_<名字>`），改好之後公開的那位也會出現在角色庫。也可以跟任何一位秘書說，請她代做"
		return check
	}
	// home tidy 不改這種（紀錄分不出是哪一位的，PRS-22），所以下一步是手動改名或交給秘書（哪一位都可以）。
	check.Detail = "你自己的角色 " + c.slug + "（" + card + "）跟官方角色同代號：角色庫與秘書用的是官方那張，你的這張沒有作用"
	check.Fix = "要留著你的這張，請改成 my_ 開頭的代號：卡改名成 personas/mine/" + mine + ".md，personas/mine/avatars/（含 full/）裡 " + c.slug +
		" 的圖也照著改名，可以跟任何一位秘書說，請她幫你改；記在 " + c.slug + " 名下的紀錄不跟著改（現在都算官方那位的），要分一部分給你的這張也可以請她幫忙"
	return check
}
