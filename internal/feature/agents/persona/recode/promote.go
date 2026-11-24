package recode

import (
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
)

// evergenie personas promote（DEV-154 第 13 件，PRS-23）：開發家把自己的角色 my_<名字> 升成官方。
// 卡搬到官方角色資料夾、代號去掉 my_（或 --as 指定），內容原樣；圖留在 personas/mine/、檔名不改（DEV-160：照片紀錄照舊指著原檔，
// 角色、衣服、編號跟著新代號；衣服之後用展示間的衣服「升成官方」一套一套升，自己的隱藏版永遠不搬）；名冊、套件紀錄、設定跟著新代號。
// 家裡不留重複的那張卡。
// 紀錄檔 settings/persona-promote/<舊代號>.json、原文 settings/persona-promote/<舊代號>-originals/（一位角色一份，可以各自退回）。

// PromoteJournal、PromoteOriginals 是紀錄檔與原文資料夾的位置（舊代號就是 my_ 開頭那個）。
// 位置在 platform 定（home relayout 搬家與退回前要看還有沒有沒收尾的紀錄）。
func PromoteJournal(home, mine string) string {
	return filepath.Join(platform.PromoteJournalDir(home), mine+".json")
}
func PromoteOriginals(home, mine string) string {
	return filepath.Join(platform.PromoteJournalDir(home), mine+"-originals")
}

// Promote 是 personas promote 的預演。
type Promote struct {
	*Plan
	Mine     string `json:"mine"`
	Official string `json:"official"`
	Card     string `json:"card"` // 官方那張卡的位置
}

// PlanPromote 算把自己的角色 mine 升成官方 as（空的＝去掉 my_）要做的事（不動任何檔）。
// 不是開發家回 FORBIDDEN；代號不對、找不到卡回錯；其他不能做的原因（官方已有同名、服務在跑…）放在 Problems。
func PlanPromote(home, mine, as string, kinds []Kind, issues []PackageIssue) (*Promote, error) {
	h, err := platform.LoadHome(home)
	if err != nil {
		return nil, err
	}
	if !h.IsDev() {
		return nil, platform.Forbidden("升成官方只能在開發家做（home.toml [runtime] env = \"dev\"）；這個家不是開發家，什麼都沒動")
	}
	if !personaref.MineSlugPattern.MatchString(mine) {
		return nil, platform.Invalid("persona", "要升的是自己的角色，代號是 my_ 開頭（例：my_mika）")
	}
	mineDir := filepath.Join(home, "personas", "mine")
	card := filepath.Join(mineDir, mine+".md")
	if !isRegular(card) {
		return nil, platform.NotFound("personas/mine/ 沒有 " + mine + ".md")
	}
	code := as
	if code == "" {
		code = strings.TrimPrefix(mine, personaref.MinePrefix)
	}
	if !personaref.SlugPattern.MatchString(code) {
		return nil, platform.Invalid("as", "官方代號「"+code+"」不合規則：要小寫英文開頭、只有小寫英文與數字（用 --as 指定一個）")
	}
	official := platform.OfficialPersonasDir(home)
	to := filepath.Join(official, code+".md")
	p := &Promote{Mine: mine, Official: code}
	b := newBuilder(home, kinds)
	b.skipScan = []string{TidyJournal(home), TidyOriginals(home), platform.PromoteJournalDir(home), filepath.Join(home, "settings", "relayout-rewrites")}
	if exists(filepath.Join(mineDir, code+".md")) {
		b.problem("personas/mine/ 已經有 %s.md（舊的、沒有 my_ 的自己的角色）：先用 evergenie home tidy 把它改成 my_ 開頭", code)
	}
	if exists(to) {
		b.problem("官方位置已經有 %s.md，不蓋；換一個代號（--as）", code)
	}
	if !inside(home, official) {
		// 卡用改名搬過去（一次做完或沒做），要同一顆磁碟；官方角色資料夾在狀態資料夾時先確認。
		if err := platform.SameDevice(home, official); err != nil {
			b.problem("官方角色資料夾跟家不在同一顆磁碟（%s），卡搬不過去", official)
		}
	}
	b.addRename(Rename{Old: mine, New: code, CardFrom: card, CardTo: to, Media: mineDir})
	b.addIssues(issues)
	b.addRoster()
	b.addRecords()
	b.addMedia()
	b.addSettings()
	b.addImportRegistry()
	if msg := platform.ServiceRunning(home); msg != "" {
		b.problem("%s", strings.Replace(msg, "再搬", "再升", 1))
	}
	if exists(PromoteJournal(home, mine)) {
		b.problem("已經有 %s 的升級紀錄（settings/persona-promote/%s.json）：上次升過還沒收尾；要退回用 --revert --apply，確定不退了用 --forget --apply", mine, mine)
	}
	p.Plan = b.finish()
	p.Card = b.rel(to)
	return p, nil
}
