package rules

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Variant 是一筆隱藏版設定（closet.variant，CLO-17）：開關與時段。DEV-160 S2 起所有設定當同一組（HiddenGroup），
// 照片歸不歸隱藏版看照片紀錄的 hidden；dir 只剩舊圖轉換（photomigrate）找檔與角色包排除資料夾用。
type Variant struct {
	ID       string
	Name     string
	Dir      string   // 相對於 <家>/personas/ 的資料夾（用 /）
	Windows  []Window // 每天自動開著的時段（本機時間）
	Button   bool     // 衣櫃頁要不要畫開關
	On       bool     // 開關（頁面、秘書或使用者自己的程式寫）
	Order    int
	HasOrder bool
}

// Window 是每天的一個時段，單位是從 00:00 起算的分鐘；End < Start 表示跨過午夜（例 22:00-02:00）。
type Window struct{ Start, End int }

const dayMinutes = 24 * 60

// ParseWindow 解析 "HH:MM-HH:MM"（結尾可以寫 24:00）；頭尾一樣不合格（要整天開請用開關）。
func ParseWindow(s string) (Window, error) {
	a, b, ok := strings.Cut(strings.TrimSpace(s), "-")
	if !ok {
		return Window{}, fmt.Errorf("時段「%s」要寫成 HH:MM-HH:MM", s)
	}
	start, err1 := parseClock(a, false)
	end, err2 := parseClock(b, true)
	if err1 != nil || err2 != nil {
		return Window{}, fmt.Errorf("時段「%s」要寫成 HH:MM-HH:MM（00:00～24:00）", s)
	}
	if start == end%dayMinutes {
		return Window{}, fmt.Errorf("時段「%s」頭尾一樣", s)
	}
	return Window{Start: start, End: end}, nil
}

func parseClock(s string, allow24 bool) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, fmt.Errorf("格式不對")
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || mm < 0 || mm > 59 || hh > 24 || (hh == 24 && (!allow24 || mm != 0)) {
		return 0, fmt.Errorf("超出範圍")
	}
	return hh*60 + mm, nil
}

// Contains：這一分鐘（0～1439）在時段裡嗎？開頭算、結尾不算。
func (w Window) Contains(minute int) bool {
	end := w.End % dayMinutes
	if w.Start < end {
		return minute >= w.Start && minute < end
	}
	return minute >= w.Start || minute < end
}

// ValidVariantDir：相對路徑、用 / 分段、沒有空段、沒有 . 開頭的段（含 ..）、沒有反斜線。
// 只能指到 <家>/personas/ 底下，免得一筆資料就把家裡別的檔案公開出去。
func ValidVariantDir(dir string) bool {
	if dir == "" || strings.HasPrefix(dir, "/") || strings.Contains(dir, `\`) || len(dir) > 200 {
		return false
	}
	for _, seg := range strings.Split(dir, "/") {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}

// VariantFromFields 把 closet.variant 的欄位轉成 Variant；不合格回錯誤（呼叫的人記 log，這個隱藏版不作用）。
func VariantFromFields(id string, f map[string]any) (Variant, error) {
	v := Variant{ID: id, Name: Text(f["name"]), Dir: Text(f["dir"])}
	if !ValidOutfit(id) {
		return Variant{}, fmt.Errorf("編號「%s」不合格（小寫英文、數字與減號）", id)
	}
	if v.Name == "" {
		return Variant{}, fmt.Errorf("沒有名稱")
	}
	if !ValidVariantDir(v.Dir) {
		return Variant{}, fmt.Errorf("dir「%s」要是 personas/ 底下的相對路徑（用 /，不能有 . 開頭的段）", v.Dir)
	}
	for _, s := range Strings(f["windows"]) {
		w, err := ParseWindow(s)
		if err != nil {
			return Variant{}, err
		}
		v.Windows = append(v.Windows, w)
	}
	v.Button, _ = f["button"].(bool)
	v.On, _ = f["on"].(bool)
	if n, ok := Int(f["order"]); ok {
		v.Order, v.HasOrder = n, true
	}
	return v, nil
}

// InWindow：now（本機時間）在任何一個時段裡嗎？
func (v Variant) InWindow(now time.Time) bool {
	m := now.Hour()*60 + now.Minute()
	for _, w := range v.Windows {
		if w.Contains(m) {
			return true
		}
	}
	return false
}

// Active：開關開著，或現在在時段裡。
func (v Variant) Active(now time.Time) bool { return v.On || v.InWindow(now) }

// SortVariants 照 order 再照編號排（沒寫 order 的排最後）；同一套衣服有好幾個隱藏版都開著時，排前面的先用。
func SortVariants(list []Variant) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.HasOrder != b.HasOrder {
			return a.HasOrder
		}
		if a.HasOrder && a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.ID < b.ID
	})
}

// NextBoundary 是 now 之後最近的一個時段開頭或結尾（本機時間）；沒有任何時段回零值。
func NextBoundary(list []Variant, now time.Time) time.Time {
	var best time.Time
	for _, v := range list {
		for _, w := range v.Windows {
			for _, m := range []int{w.Start, w.End % dayMinutes} {
				for d := 0; d <= 1; d++ {
					t := time.Date(now.Year(), now.Month(), now.Day()+d, m/60, m%60, 0, 0, now.Location())
					if t.After(now) && (best.IsZero() || t.Before(best)) {
						best = t
					}
				}
			}
		}
	}
	return best
}

// 隱藏版只留一組（DEV-160 S2，作者 2026-11-15「保留時段＋手動開關，多組拿掉」）：所有 closet.variant 當同一組，
// 任一筆開關開著或在任一筆的時段裡就是開著；照片紀錄 hidden = true 的在開著時列。紀錄本身這步不合併、不刪（S6 收）。
const (
	// HiddenID 是唯一那組隱藏版在畫面、公開網址（o/…/h-<n>）、狀態裡的固定代號。
	HiddenID = "h"
	// HiddenName 是它給人看的名字。
	HiddenName = "隱藏版"
)

// 自己的隱藏版（作者 2026-11-07，DEV-141）：主人自己放的隱藏版，圖放 <家>/personas/mine/adult/。
// 家層級的開關是衣櫃的擴充點 closet.adult（看板「設定 → 套件」，值寫 on 才算開；預設 off）。
// 沒打開時主人自己的隱藏版照片（source = mine 而且 hidden）一律不列、公開資料夾也不會有它的圖；
// 主人自己的隱藏版設定（dir 在 mine/ 底下的 closet.variant）也不算進那一組的開關。
const (
	// AdultExtension 是開關的擴充點 id（完整設定鍵 closet.adult）。
	AdultExtension = "adult"
)

// MineVariant：這筆隱藏版設定的 dir 在 personas/mine/ 底下（存放位置只表示歸誰）。
func MineVariant(v Variant) bool {
	first, _, _ := strings.Cut(v.Dir, "/")
	return first == "mine"
}

// HiddenGroup 是算「那一組開不開」要看的設定：自己的隱藏版開關關著時，owner 說是自己的隱藏版的設定不算
// （衣櫃傳 photomigrate.OwnerVariant；nil＝MineVariant）；回傳新的 slice、照順序排。
func HiddenGroup(list []Variant, adultOn bool, owner func(Variant) bool) []Variant {
	if owner == nil {
		owner = MineVariant
	}
	out := make([]Variant, 0, len(list))
	for _, v := range list {
		if adultOn || !owner(v) {
			out = append(out, v)
		}
	}
	SortVariants(out)
	return out
}

// HiddenOpen：那一組隱藏版現在開著嗎——任一筆開關開著，或現在在任一筆的時段裡。
func HiddenOpen(group []Variant, now time.Time) bool {
	for _, v := range group {
		if v.Active(now) {
			return true
		}
	}
	return false
}

// Promotable：這套衣服「可以升成官方」嗎（CLO-29）——開發家、官方角色、主人自己加的（source = mine）、已上架（active）、
// 不是整套都是隱藏版的照片（allHidden：有照片紀錄而且每張 hidden 都是 true；DEV-160 S2 起看紀錄，不再看名字）。
// 這是畫面上「可以升成官方」的唯一定義：衣櫃的程式把結果寫進 closet.person（promotable），展示間的篩選與 closet-mine 的按鈕讀它。
// 真的升的時候擋不擋仍由 closet-mine 的程式把關（主人本人、編號），隱藏版的照片不搬。
func Promotable(dev, officialPersona bool, o Outfit, allHidden bool) bool {
	return dev && officialPersona && o.Source == SourceMine && o.State == StateActive && !allHidden
}

// AdultEnabled：開關的值是 on（不分大小寫、去頭尾空白）才算開；其他值、沒設都是關。
func AdultEnabled(value string) bool { return strings.EqualFold(strings.TrimSpace(value), "on") }
