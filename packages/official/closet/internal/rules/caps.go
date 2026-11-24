package rules

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// 能力（CLO-32，DEV-160 S4）：這個家支援哪幾組動作。頁面與核心都不判斷「我在哪種家」，只看這份清單；
// 「是不是開發家」只在這裡換算一次（Capabilities 的 dev），別的地方一律問能力。
const (
	// CapMine：每個家都有——自己的衣服：新增／刪一套、上傳／刪單張、要不要用點數換（第 2 層）。
	CapMine = "mine"
	// CapEdit：只有開發家——整套與單張的狀態、評論、篩選，沒兌換的也看得到真圖（第 3 層，「修改」分頁）。
	CapEdit = "edit"
	// CapEditTab：有 edit、而且擴充點 closet.edit-tab 沒關，角色頁才掛「修改」分頁（衣櫃頁面連到它的入口也跟著）。
	// 開發家的主人改用別的套件管設計（例：我的角色工坊）時可以關掉，edit 的其他功能不變（SEC-12，2026-11-18）。
	CapEditTab = "edit-tab"
)

// EditTabExtension 是「修改」分頁開關的擴充點 id（完整設定鍵 closet.edit-tab；預設 on，寫 off 才關）。
const EditTabExtension = "edit-tab"

// KnownCaps 是衣櫃認得的能力（動作的 needs 只能寫這些）。
var KnownCaps = []string{CapMine, CapEdit, CapEditTab}

// Capabilities 是這個家支援的能力，照 KnownCaps 的順序。dev＝這個家是開發家（怎麼判斷只在 run/caps.go 一處）。
func Capabilities(dev bool) []string {
	if dev {
		return []string{CapMine, CapEdit}
	}
	return []string{CapMine}
}

// WithEditTab：caps 有 edit 而且「修改」分頁沒關（tabOn）就加上 edit-tab；其他照原樣。
func WithEditTab(caps []string, tabOn bool) []string {
	if !tabOn || !slices.Contains(caps, CapEdit) || slices.Contains(caps, CapEditTab) {
		return caps
	}
	return append(slices.Clone(caps), CapEditTab)
}

// EditTabEnabled：開關的值不是 off（不分大小寫、去頭尾空白）就算開；沒設是開。
func EditTabEnabled(value string) bool { return !strings.EqualFold(strings.TrimSpace(value), "off") }

// HasCap：caps 裡有沒有 name。
func HasCap(caps []string, name string) bool { return slices.Contains(caps, name) }

// ---- 動作插座 closet.action（CLO-33） ----

// 動作對誰：整套衣服或單張照片。
const (
	ScopeOutfit = "outfit"
	ScopePhoto  = "photo"
)

// 請求可以帶的欄位：頁面按下去時照目標填進請求（persona＝角色代號、outfit＝closet.outfit 的編號、photo＝照片編號）。
const (
	FieldPersona = "persona"
	FieldOutfit  = "outfit"
	FieldPhoto   = "photo"
)

// Registrant 是登記動作的套件（看得到＝啟用而且沒被拒）：Kinds 是它宣告的資料種類（短名，不含套件 id）→ 那個種類規格裡的欄位名。
// Official＝官方套件：不能登記（插座給使用者自己的套件用；官方套件有自己的頁面，衣櫃自己的種類也不能拿來做鈕，免得做出替主人兌換的鈕）。
type Registrant struct {
	ID       string
	Official bool
	Kinds    map[string]map[string]bool
}

// Action 是驗過的一個動作；Fields 是要發給頁面的樣子（跟 closet.action 的欄位同名，多 id）。
type Action struct {
	ID    string
	Scope string
	Needs string
	Order int
	// Fields 照 closet.capabilities 的 actions 一項的格式（見 schemas/capabilities.toml）。
	Fields map[string]any
}

var (
	kindNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	fieldPattern    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

const maxLabelRunes = 20

// ParseAction 驗一筆 closet.action：不合格回白話原因（程式記 log、不列這個動作，CLO-33）。
// regs 是看得到的套件；登記的套件不在裡面（沒裝、關掉、被拒）也不列。
func ParseAction(id string, f map[string]any, regs map[string]Registrant) (Action, error) {
	pkg := Text(f["package"])
	reg, ok := regs[pkg]
	switch {
	case pkg == "":
		return Action{}, fmt.Errorf("沒寫 package（哪個套件登記的）")
	case !strings.HasPrefix(id, pkg+"-") || len(id) == len(pkg)+1:
		return Action{}, fmt.Errorf("編號要以「%s-」開頭（<套件 id>-<動作名>），免得兩個套件撞名", pkg)
	case !ok:
		return Action{}, fmt.Errorf("登記的套件「%s」沒裝、沒啟用或被拒", pkg)
	case reg.Official:
		return Action{}, fmt.Errorf("官方套件「%s」不能登記動作（插座給使用者自己的套件用）", pkg)
	}
	label := Text(f["label"])
	if label == "" || strings.ContainsAny(label, "\r\n") || len([]rune(label)) > maxLabelRunes {
		return Action{}, fmt.Errorf("label 要有字、只能一行、不超過 %d 個字", maxLabelRunes)
	}
	scope := Text(f["scope"])
	if scope != ScopeOutfit && scope != ScopePhoto {
		return Action{}, fmt.Errorf("scope 要是 outfit（整套）或 photo（單張）")
	}
	needs := Text(f["needs"])
	if !HasCap(KnownCaps, needs) {
		return Action{}, fmt.Errorf("needs 要是衣櫃認得的能力（%s）", strings.Join(KnownCaps, "、"))
	}
	reqKind, err := ownKind(Text(f["request_kind"]), reg)
	if err != nil {
		return Action{}, fmt.Errorf("request_kind %v", err)
	}
	reqFields, err := requestFields(f["request_fields"], scope)
	if err != nil {
		return Action{}, err
	}
	out := map[string]any{
		"id": id, "package": pkg, "label": label, "scope": scope, "needs": needs,
		"request_kind": reqKind, "request_fields": reqFields,
	}
	set, err := requestSet(f["request_set"], reqFields)
	if err != nil {
		return Action{}, err
	}
	if len(set) > 0 {
		out["request_set"] = set
	}
	// 請求帶的欄位都要是 request_kind 規格裡的欄位（核心自己寫的 id、by、created_at 這類不會在規格裡），免得主人按了才寫不進去。
	if err := declared(reg, reqKind, "request_kind", append(slices.Clone(reqFields), sortedKeys(set)...)); err != nil {
		return Action{}, err
	}
	order, _ := Int(f["order"])
	out["order"] = order

	statusKind := Text(f["status_kind"])
	if statusKind == "" {
		for _, k := range []string{"status_field", "status_labels", "filter_label", "filter_values"} {
			if f[k] != nil && f[k] != "" {
				return Action{}, fmt.Errorf("%s 要配 status_kind（狀態看哪個種類）", k)
			}
		}
		return Action{ID: id, Scope: scope, Needs: needs, Order: order, Fields: out}, nil
	}
	if out["status_kind"], err = ownKind(statusKind, reg); err != nil {
		return Action{}, fmt.Errorf("status_kind %v", err)
	}
	field := Text(f["status_field"])
	if !fieldPattern.MatchString(field) {
		return Action{}, fmt.Errorf("有 status_kind 就要寫 status_field（狀態看那個種類的哪個欄位）")
	}
	out["status_field"] = field
	// 狀態照 request_fields 對目標，所以那幾個欄位與 status_field 都要在 status_kind 的規格裡。
	if err := declared(reg, statusKind, "status_kind", append(slices.Clone(reqFields), field)); err != nil {
		return Action{}, err
	}
	labels, err := statusLabels(f["status_labels"])
	if err != nil {
		return Action{}, err
	}
	out["status_labels"] = labels
	if fl := Text(f["filter_label"]); fl != "" {
		if strings.ContainsAny(fl, "\r\n") || len([]rune(fl)) > maxLabelRunes {
			return Action{}, fmt.Errorf("filter_label 只能一行、不超過 %d 個字", maxLabelRunes)
		}
		out["filter_label"] = fl
		values := Strings(f["filter_values"])
		if len(values) == 0 {
			for v := range labels {
				values = append(values, v)
			}
			sort.Strings(values)
		}
		for _, v := range values {
			if _, ok := labels[v]; !ok {
				return Action{}, fmt.Errorf("filter_values 的「%s」不在 status_labels 裡", v)
			}
		}
		out["filter_values"] = values
	} else if f["filter_values"] != nil {
		return Action{}, fmt.Errorf("filter_values 要配 filter_label（篩選鈕的名字）")
	}
	return Action{ID: id, Scope: scope, Needs: needs, Order: order, Fields: out}, nil
}

// declared：fields 都要是 full（<套件>.<種類>）規格裡的欄位。
func declared(reg Registrant, full, what string, fields []string) error {
	_, kind, _ := strings.Cut(full, ".")
	have := reg.Kinds[kind]
	for _, k := range fields {
		if !have[k] {
			return fmt.Errorf("%s「%s」的規格裡沒有欄位「%s」", what, full, k)
		}
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ownKind：寫成 <登記的套件>.<種類>，而且是那個套件自己宣告的種類；回完整名稱。
func ownKind(full string, reg Registrant) (string, error) {
	pkg, kind, ok := strings.Cut(full, ".")
	if !ok || pkg != reg.ID || !kindNamePattern.MatchString(kind) || reg.Kinds[kind] == nil {
		return "", fmt.Errorf("要寫成「%s.<種類>」，而且是 %s 自己宣告的資料種類", reg.ID, reg.ID)
	}
	return full, nil
}

func requestFields(v any, scope string) ([]string, error) {
	list := Strings(v)
	if len(list) == 0 {
		return nil, fmt.Errorf("request_fields 要列請求帶哪些欄位（persona、outfit、photo）")
	}
	seen := map[string]bool{}
	for _, k := range list {
		switch {
		case seen[k]:
			return nil, fmt.Errorf("request_fields 的「%s」重複了", k)
		case k == FieldPhoto && scope != ScopePhoto:
			return nil, fmt.Errorf("request_fields 的 photo 只能用在 scope = photo")
		case k != FieldPersona && k != FieldOutfit && k != FieldPhoto:
			return nil, fmt.Errorf("request_fields 只能有 persona、outfit、photo，不認得「%s」", k)
		}
		seen[k] = true
	}
	return list, nil
}

// requestSet：請求另外固定帶的欄位（例 {"type": "move"}）；值只能是字串、數字、true/false，不能蓋 request_fields 與 by。
func requestSet(v any, reqFields []string) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("request_set 要是 {欄位: 值}")
	}
	out := map[string]any{}
	for k, x := range m {
		if !fieldPattern.MatchString(k) || k == "by" || slices.Contains(reqFields, k) {
			return nil, fmt.Errorf("request_set 的欄位「%s」不行（不能是 by 或 request_fields 裡的）", k)
		}
		switch x.(type) {
		case string, bool, int, int64, float64:
			out[k] = x
		default:
			return nil, fmt.Errorf("request_set 的「%s」只能是字串、數字或 true/false", k)
		}
	}
	return out, nil
}

func statusLabels(v any) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil, fmt.Errorf("有 status_kind 就要寫 status_labels（狀態值 → 白話，例 {\"done\": \"已搬\"}）")
	}
	out := map[string]any{}
	for k, x := range m {
		s := Text(x)
		if k == "" || s == "" || strings.ContainsAny(s, "\r\n") || len([]rune(s)) > maxLabelRunes {
			return nil, fmt.Errorf("status_labels 的「%s」要對到一行、不超過 %d 個字的白話", k, maxLabelRunes)
		}
		out[k] = s
	}
	return out, nil
}

// SupportedActions 只留這個家的能力撐得起的動作（needs 在 caps 裡），依 scope、order、編號排。
func SupportedActions(actions []Action, caps []string) []Action {
	var out []Action
	for _, a := range actions {
		if HasCap(caps, a.Needs) {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
}
