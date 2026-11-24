package vault

import (
	"math"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

type FieldType string

const (
	String   FieldType = "string"
	Int      FieldType = "int"
	Number   FieldType = "number" // 整數或浮點數都收（套件的資料欄位，D-28）
	Bool     FieldType = "bool"
	Date     FieldType = "date"
	DateTime FieldType = "datetime"
	Enum     FieldType = "enum"
	Link     FieldType = "link"
	List     FieldType = "list"
	Map      FieldType = "map"
)

type Field struct {
	Name       string
	Type       FieldType
	Required   bool
	RequiredIf func(Record) bool
	Enum       []string
	LinkKind   string
	Default    any
}
type Spec struct {
	Kind   string
	Fields []Field
	// IDMode 只有套件的資料種類會用（D-28）：seq＝編號是 IDPrefix 加遞增數字（NextID 發）、given＝編號由呼叫者給；空＝核心種類，不檢查。
	IDMode     string
	IDPrefix   string
	Path       func(id string) string
	ArchiveAt  func(r Record, loc *time.Location) (time.Time, bool)
	ClosedFunc func(Record) (bool, time.Time)
}

func (s Spec) Closed(r Record) (bool, time.Time) {
	if s.ClosedFunc == nil {
		return false, time.Time{}
	}
	return s.ClosedFunc(r)
}

// givenIDPattern 是 given 種類的編號格式（例 2026-10-25、main）；seq 種類的編號是 IDPrefix 加正整數。
var givenIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`) // 收底線：主人自己的角色代號是 my_<名字>，跟著角色走的紀錄編號會帶著它（DEV-154）

// checkID 檢查套件資料種類的編號格式；核心種類（IDMode 空）不檢查。
func (s Spec) checkID(id string) error {
	switch s.IDMode {
	case "given":
		if !givenIDPattern.MatchString(id) {
			return platform.Invalid("id", "編號只能用小寫英文、數字與減號，英文或數字開頭（例：2026-10-25、main）")
		}
	case "seq":
		if !strings.HasPrefix(id, s.IDPrefix) || !positiveID.MatchString(strings.TrimPrefix(id, s.IDPrefix)) {
			return platform.Invalid("id", "編號要是 "+s.IDPrefix+" 加數字（例："+s.IDPrefix+"1）；新編號請用 next_id 取得")
		}
	}
	return nil
}

// Validate 用預設編號字首檢查一筆資料；vault 自己寫入時用 validate 帶家的字首。
func (s Spec) Validate(r Record) error { return s.validate(r, nil) }

func (s Spec) validate(r Record, prefixes map[string]string) error {
	for _, f := range s.Fields {
		v, present := r.Fields[f.Name]
		required := f.Required || (f.RequiredIf != nil && f.RequiredIf(r))
		if !present || v == nil || v == "" {
			if required {
				return platform.Invalid(f.Name, "請填寫必要欄位")
			}
			if !present || v == nil {
				continue
			}
		}
		if !validField(f, v, prefixes) {
			return platform.Invalid(f.Name, "欄位格式或內容不正確，請依資料規格填寫")
		}
	}
	return nil
}

var bracketLink = regexp.MustCompile(`^\[\[([^\[\]\r\n]+)\]\]$`)
var positiveID = regexp.MustCompile(`^[1-9][0-9]*$`)

// defaultIDPrefixes 是核心種類的編號字首；task 與 goal 可以由家的 [ids] 換掉（Module.prefixes，GOAL-2 W3）。
var defaultIDPrefixes = map[string]string{"task": platform.DefaultTaskPrefix, "reminder": "R-", "habit": "H-", "message": "M-", "goal": platform.DefaultGoalPrefix, "policy_request": "P-"}

// idPrefixesFor 套上家的 [ids]：回一份新的對照表（預設那份不動）。
func idPrefixesFor(ids platform.IDPrefixes) map[string]string {
	out := make(map[string]string, len(defaultIDPrefixes))
	for k, v := range defaultIDPrefixes {
		out[k] = v
	}
	out["task"], out["goal"] = ids.Task(), ids.Goal()
	return out
}

func linkID(value, kind string, prefixes map[string]string) (string, bool) {
	match := bracketLink.FindStringSubmatch(value)
	if match == nil || strings.TrimSpace(match[1]) == "" {
		return "", false
	}
	id := match[1]
	if prefixes == nil {
		prefixes = defaultIDPrefixes
	}
	if prefix, known := prefixes[kind]; known {
		if !strings.HasPrefix(id, prefix) || !positiveID.MatchString(strings.TrimPrefix(id, prefix)) {
			return "", false
		}
	}
	return id, true
}
func validField(f Field, v any, prefixes map[string]string) bool {
	str, isString := v.(string)
	switch f.Type {
	case String:
		return isString
	case Int:
		switch reflect.TypeOf(v).Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return true
		case reflect.Float32, reflect.Float64:
			// JSON 來的數字都是浮點數：整數值（3、3.0）算整數，3.5 不算。
			f := reflect.ValueOf(v).Float()
			return f == math.Trunc(f) && !math.IsInf(f, 0)
		}
		return false
	case Number:
		switch reflect.TypeOf(v).Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return true
		case reflect.Float32, reflect.Float64:
			f := reflect.ValueOf(v).Float()
			return !math.IsNaN(f) && !math.IsInf(f, 0)
		}
		return false
	case Bool:
		_, ok := v.(bool)
		return ok
	case Date:
		if !isString || len(str) != 10 {
			return false
		}
		_, err := time.Parse("2006-01-02", str)
		return err == nil
	case DateTime:
		if !isString {
			return false
		}
		_, err := time.Parse(time.RFC3339Nano, str)
		return err == nil
	case Enum:
		return isString && slices.Contains(f.Enum, str)
	case Link:
		if !isString {
			return false
		}
		_, ok := linkID(str, f.LinkKind, prefixes)
		return ok
	case List:
		return reflect.TypeOf(v).Kind() == reflect.Slice || reflect.TypeOf(v).Kind() == reflect.Array
	case Map:
		return reflect.TypeOf(v).Kind() == reflect.Map && reflect.TypeOf(v).Key().Kind() == reflect.String
	default:
		return false
	}
}

// UpgradeLegacyGoal 把舊欄位 `group` 當成 `goal`（作者 2026-10-25：任務群組改名 Goal，REQ-35）。
// 舊系統沒有群組，所以正常不會有 `group`；萬一有，值是合法的 `[[GOAL-<n>]]` 才接成 `goal`
// （已經有 `goal` 就以 `goal` 為準），不合法的值（例如舊設計的 `[[G-<n>]]`）直接丟掉。
// 不管哪種都會拿掉 `group`，所以下一次寫回就清乾淨。傳入的 map 會被改動，呼叫端要自己先複製。
func UpgradeLegacyGoal(fields map[string]any) {
	old, ok := fields["group"]
	if !ok {
		return
	}
	delete(fields, "group")
	if _, has := fields["goal"]; has {
		return
	}
	if s, ok := old.(string); ok {
		if _, ok := linkID(s, "goal", nil); ok {
			fields["goal"] = s
		}
	}
}
