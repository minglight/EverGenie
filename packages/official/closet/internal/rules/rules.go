// Package rules 是衣櫃的規則（純函式，沒有 I/O）：誰擁有哪套衣服、可以花的點數、輪播穿哪套哪張、兌換請求能不能過。
// 來源：舊系統 closet.py 的兌換與輪替；新規則見說明卡 closet。
package rules

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// 衣服的狀態。
const (
	StateActive  = "active"
	StateDraft   = "draft"
	StateRetired = "retired"
)

// 衣服的來源。
const (
	SourceOfficial = "official"
	SourceMine     = "mine"
)

// BaseOutfit 是每位角色的第一套。
const BaseOutfit = "base"

// 能寫 closet.unlock 的操作者：closet 自己，和搬家當天的轉資料腳本。
const (
	ActorCloset  = "closet"
	ActorMigrate = "migrate"
)

var (
	personaPattern = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	// 主人自己的角色：my_<名字>（核心 personaref.MineSlugPattern，DEV-154）
	minePersonaPattern = regexp.MustCompile(`^my_[a-z0-9]{1,24}$`)
	outfitPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// ValidPersona：人設代號 ^[a-z][a-z0-9]*$。
func ValidPersona(s string) bool {
	return personaPattern.MatchString(s) || minePersonaPattern.MatchString(s)
}

// ValidOutfit：衣服 id ^[a-z0-9][a-z0-9-]*$。
func ValidOutfit(s string) bool { return outfitPattern.MatchString(s) }

// RecordID 是一套衣服的編號：<人設>-<衣服 id>。
func RecordID(persona, outfit string) string { return persona + "-" + outfit }

// Outfit 是目錄裡的一套衣服。
type Outfit struct {
	Record   string // 編號 <人設>-<衣服>
	Persona  string
	Outfit   string
	Name     string
	Desc     string
	Cost     int
	State    string
	Source   string
	Order    int
	HasOrder bool
	// From：升成官方前的編號（closet-mine 的升成官方請求寫的，例 athena-my-12）；只認 closet-mine 寫的，舊編號的兌換會搬過來（CarryPlan）。
	From string
	// Adopted：設計中（draft）的衣服已經單張採用的那幾張（SEC-10）：`half-<n>`、`full`、`video-<n>-<k>`；隱藏版的前面加 `<隱藏版編號>:`（例 `h1:half-2`）。沒寫＝一張都還沒單張採用（舊的草稿照舊整套採用）。
	Adopted []string
	By      string // 最後寫這筆的人（vault 的 by）
}

// OutfitFromFields 把 closet.outfit 的欄位轉成 Outfit；不合格回錯誤（呼叫的人記 log 略過這一筆）。
func OutfitFromFields(id string, f map[string]any) (Outfit, error) {
	o := Outfit{Record: id, Persona: Text(f["persona"]), Outfit: Text(f["outfit"]), Name: Text(f["name"]), Desc: Text(f["desc"]), State: Text(f["state"]), Source: Text(f["source"]), From: Text(f["from"]), Adopted: Strings(f["adopted"]), By: Text(f["by"])}
	if !ValidPersona(o.Persona) {
		return Outfit{}, fmt.Errorf("人設代號「%s」不合格（小寫英文開頭，只用小寫英文與數字）", o.Persona)
	}
	if !ValidOutfit(o.Outfit) {
		return Outfit{}, fmt.Errorf("衣服 id「%s」不合格（小寫英文、數字與減號）", o.Outfit)
	}
	if id != RecordID(o.Persona, o.Outfit) {
		return Outfit{}, fmt.Errorf("編號「%s」要等於 %s", id, RecordID(o.Persona, o.Outfit))
	}
	if o.Name == "" {
		return Outfit{}, fmt.Errorf("沒有名稱")
	}
	cost, ok := Int(f["cost"])
	if !ok || cost < 0 {
		return Outfit{}, fmt.Errorf("cost 要是 0 以上的整數")
	}
	o.Cost = cost
	switch o.State {
	case StateActive, StateDraft, StateRetired:
	default:
		return Outfit{}, fmt.Errorf("state「%s」不認得（active、draft、retired）", o.State)
	}
	switch o.Source {
	case SourceOfficial, SourceMine:
	default:
		return Outfit{}, fmt.Errorf("source「%s」不認得（official、mine）", o.Source)
	}
	if n, ok := Int(f["order"]); ok {
		o.Order, o.HasOrder = n, true
	}
	return o, nil
}

// SortOutfits 同一位照 order 再照 id 排，沒寫 order 的排最後。
func SortOutfits(list []Outfit) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.HasOrder != b.HasOrder {
			return a.HasOrder
		}
		if a.HasOrder && a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Record < b.Record
	})
}

// Unlock 是一筆認得的兌換紀錄。
type Unlock struct {
	Record  string // 同一套衣服的 outfit 編號
	Persona string
	Outfit  string
	Cost    int
	Request string
}

// TrustedActor：只有 closet 或 migrate 寫的兌換才認（別的來源會被封存，免得繞過點數）。
func TrustedActor(by string) bool { return by == ActorCloset || by == ActorMigrate }

// UnlockFromFields 轉 closet.unlock；cost 沒寫（或不是正整數）當 1，跟 habits 算 spent 一致。
func UnlockFromFields(id string, f map[string]any) Unlock {
	u := Unlock{Record: id, Persona: Text(f["persona"]), Outfit: Text(f["outfit"]), Request: Text(f["request"])}
	u.Cost = 1
	if n, ok := Int(f["cost"]); ok && n > 0 {
		u.Cost = n
	}
	return u
}

// Owned：cost = 0 或有認得的 unlock 就擁有；draft 不算擁有。
func Owned(o Outfit, unlocked map[string]bool) bool {
	if o.State == StateDraft {
		return false
	}
	return o.Cost == 0 || unlocked[o.Record]
}

// ActorMine 是 closet-mine 套件程式寫紀錄時的署名；升成官方的那筆 outfit 由它寫（from 只認它寫的）。
const ActorMine = "closet-mine"

// CarryPrefix 是搬過來的兌換紀錄的 request 欄位開頭（carry:<舊編號>），用來認出「這筆是從舊編號搬來的」。
const CarryPrefix = "carry:"

// Carry 是一筆要搬的兌換：舊編號 From 的兌換紀錄搬到 To（升成官方後的新編號）。
type Carry struct {
	From, To string
	Unlock   Unlock // 舊的那筆（照它的 cost 寫新的，花掉的點數不變）
}

// CarryPlan 找出升成官方前兌換過、要搬到新編號的兌換紀錄。只認這些：outfit 的 from 指向同一位角色的舊編號、
// 那筆 outfit 是 closet-mine 寫的（by = closet-mine，升成官方產生的）、舊編號已不在目錄裡、舊編號有認得的兌換、新編號還沒有；
// 而且同一個舊編號只有一筆 outfit 指著它（好幾筆都指著就一筆都不搬，免得一筆兌換接到好幾套）。
// 搬是真的寫一筆新的兌換（by = closet）並封存舊的，所以之後誰改了那筆 outfit 都不會掉；照順序回傳。
func CarryPlan(catalog map[string]Outfit, unlocks []Unlock) []Carry {
	have := map[string]Unlock{}
	for _, u := range unlocks {
		have[u.Record] = u
	}
	claims := map[string]int{}
	for _, o := range catalog {
		if o.From != "" {
			claims[o.From]++
		}
	}
	var out []Carry
	for _, o := range catalog {
		if o.From == "" || o.From == o.Record || !strings.HasPrefix(o.From, o.Persona+"-") || o.By != ActorMine || claims[o.From] != 1 {
			continue
		}
		if _, live := catalog[o.From]; live {
			continue
		}
		old, ok := have[o.From]
		if _, done := have[o.Record]; !ok || done {
			continue
		}
		out = append(out, Carry{From: o.From, To: o.Record, Unlock: old})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].To < out[j].To })
	return out
}

// CarriedLeftovers 是已經搬到新編號（新的那筆 request = carry:<舊編號>）、舊的卻還在的兌換編號——上次搬到一半，要封存舊的，
// 不然 habits 會把同一次兌換算兩次。
func CarriedLeftovers(unlocks []Unlock) []string {
	have := map[string]bool{}
	for _, u := range unlocks {
		have[u.Record] = true
	}
	var out []string
	for _, u := range unlocks {
		if from, ok := strings.CutPrefix(u.Request, CarryPrefix); ok && from != u.Record && have[from] {
			out = append(out, from)
		}
	}
	sort.Strings(out)
	return out
}

// SpentPoints 是認得的兌換花掉的點數合計。
func SpentPoints(unlocks []Unlock) int {
	total := 0
	for _, u := range unlocks {
		total += u.Cost
	}
	return total
}

// Available 是可以花的點數：habits 錢包的 earned（只增不減）減所有認得的兌換，最少 0。
// 不直接用 wallet.points：habits 要幾秒後才把剛花掉的算進 spent，用 earned 自己減才不會在這段空檔重複花。
func Available(earned int, unlocks []Unlock) int {
	if p := earned - SpentPoints(unlocks); p > 0 {
		return p
	}
	return 0
}

// Hour 是輪播用的小時序號（Unix 秒 / 3600）。
func Hour(unix int64) int64 { return unix / 3600 }

// EffectiveRotation 是有效輪播清單：勾的裡面還擁有的（照目錄順序）；沒有就是第一套擁有的；一套都沒有就空。
// owned 已照目錄順序排好。
func EffectiveRotation(chosen []string, owned []Outfit) []string {
	pick := map[string]bool{}
	for _, c := range chosen {
		pick[c] = true
	}
	var out []string
	for _, o := range owned {
		if pick[o.Outfit] {
			out = append(out, o.Outfit)
		}
	}
	if len(out) == 0 && len(owned) > 0 {
		out = []string{owned[0].Outfit}
	}
	return out
}

// WearingIndex：穿有效清單的第 hour % len 套；清單空回 -1。
func WearingIndex(hour int64, n int) int {
	if n <= 0 {
		return -1
	}
	return int(hour % int64(n))
}

// PhotoIndex：同一套有 photos 張照片時穿第 (hour / outfits) % photos 張（0 起算，跟舊系統同一個算式）；沒有照片回 -1。
func PhotoIndex(hour int64, outfits, photos int) int {
	if outfits <= 0 || photos <= 0 {
		return -1
	}
	return int((hour / int64(outfits)) % int64(photos))
}

// WearUntil 是「現在穿這套」（CLO-30）穿到什麼時候：當天結束（now 所在時區的隔天 00:00；now 由衣櫃的程式給，
// 是服務所在機器的時間與時區，不看瀏覽器）。指定穿多久只由這裡決定。
// 算出來的時間一定在 now 之後：時區那天午夜不存在或往回撥（夏令時間）而算出不在 now 之後的時間時，退成 now 加 24 小時。
func WearUntil(now time.Time) time.Time {
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	if !end.After(now) {
		return now.Add(24 * time.Hour)
	}
	return end
}

// Picked：指定穿的那套（wear）現在還算不算。要三個都成立：有指定、時間還沒到（now < until）、而且真的擁有
// （owned 是真的擁有的清單；開發家看得到真圖的不算擁有）。不成立就照輪流穿的清單。
func Picked(wear string, until, now int64, owned []Outfit) bool {
	if wear == "" || now >= until {
		return false
	}
	for _, o := range owned {
		if o.Outfit == wear {
			return true
		}
	}
	return false
}

// 「現在穿這套」請求被拒絕的白話原因。
const ReasonNotHers = "還沒擁有這套，要先兌換才能穿"

// CheckWear 判斷一筆「現在穿這套」的請求：找不到 → ReasonNotFound；不是真的擁有（沒兌換、設計中）→ ReasonNotHers；通過回空字串。
// 不看是誰寫的（輪流穿本來秘書也能設；只有花點數的兌換才限主人）。
func CheckWear(persona, outfit string, catalog map[string]Outfit, unlocks map[string]bool) (Outfit, string) {
	o, ok := catalog[RecordID(persona, outfit)]
	if !ok || o.Persona != persona || o.Outfit != outfit {
		return Outfit{}, ReasonNotFound
	}
	if !Owned(o, unlocks) {
		return o, ReasonNotHers
	}
	return o, ""
}

// RequestInput 是判斷一筆兌換請求要用的東西。
type RequestInput struct {
	By      string
	Persona string
	Outfit  string
	Catalog map[string]Outfit // 編號 → 衣服
	Unlocks map[string]bool   // 已擁有兌換的衣服編號
	Wallet  bool              // 找得到 habits 的錢包
	Points  int               // 現在可以花的點數
}

// 兌換請求被拒絕的白話原因。
const (
	ReasonNotOwner = "只有主人本人可以花點數兌換"
	ReasonNotFound = "衣櫃裡沒有這套衣服"
	ReasonNotYet   = "這套還不能兌換"
	ReasonHaveIt   = "已經擁有這套了"
	// ReasonUnlockBroken：這套的兌換編號被別的（被封存的）紀錄佔住了。
	ReasonUnlockBroken = "這套的兌換紀錄有問題，請跟任何一位秘書說"
	ReasonNoWallet     = "還沒有點數：先安裝「習慣」套件、達標賺到點數才能兌換"
	reasonNoPointsF    = "點數不夠：要 %d 點，現在 %d 點"
)

// CheckUnlock 判斷一筆 pending 的兌換請求；通過回 outfit 與空字串，拒絕回白話原因。
func CheckUnlock(in RequestInput) (Outfit, string) {
	if !strings.HasPrefix(in.By, "owner(") || !strings.HasSuffix(in.By, ")") {
		return Outfit{}, ReasonNotOwner
	}
	o, ok := in.Catalog[RecordID(in.Persona, in.Outfit)]
	if !ok || o.Persona != in.Persona || o.Outfit != in.Outfit {
		return Outfit{}, ReasonNotFound
	}
	if o.State == StateDraft {
		return o, ReasonNotYet
	}
	if Owned(o, in.Unlocks) {
		return o, ReasonHaveIt
	}
	if o.State == StateRetired {
		return o, ReasonNotYet
	}
	if !in.Wallet {
		return o, ReasonNoWallet
	}
	if in.Points < o.Cost {
		return o, fmt.Sprintf(reasonNoPointsF, o.Cost, in.Points)
	}
	return o, ""
}

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

// Strings 取字串清單欄位。
func Strings(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, x := range list {
		if s := Text(x); s != "" {
			out = append(out, s)
		}
	}
	if ss, ok := v.([]string); ok {
		return ss
	}
	return out
}
