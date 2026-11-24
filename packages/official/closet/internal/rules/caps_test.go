package rules

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestCapabilities(t *testing.T) {
	if got := Capabilities(false); !reflect.DeepEqual(got, []string{CapMine}) {
		t.Fatalf("一般家：%v", got)
	}
	if got := Capabilities(true); !reflect.DeepEqual(got, []string{CapMine, CapEdit}) {
		t.Fatalf("開發家：%v", got)
	}
	if HasCap(Capabilities(false), CapEdit) || !HasCap(Capabilities(true), CapEdit) {
		t.Fatal("edit 只有開發家有")
	}
}

func validAction() map[string]any {
	return map[string]any{
		"package": "studio", "label": "搬到別處", "scope": "photo", "needs": "edit",
		"request_kind": "studio.request", "request_fields": []any{"persona", "outfit", "photo"},
		"status_kind": "studio.review", "status_field": "state", "status_labels": map[string]any{"done": "搬好了", "stale": "改過要再搬"},
		"filter_label": "搬過的", "filter_values": []any{"done"},
	}
}

// 每一種不合格各有白話原因（衣櫃記 log 用）；合格的原樣整理成發給頁面的樣子。
func TestParseAction(t *testing.T) {
	fields := func(names ...string) map[string]bool {
		m := map[string]bool{}
		for _, n := range names {
			m[n] = true
		}
		return m
	}
	regs := map[string]Registrant{
		"studio": {ID: "studio", Kinds: map[string]map[string]bool{
			"request": fields("persona", "outfit", "photo", "type"),
			"review":  fields("persona", "outfit", "photo", "state"),
			"log":     fields("state"),
		}},
		"closet": {ID: "closet", Official: true, Kinds: map[string]map[string]bool{"request": fields("persona", "outfit", "type")}},
	}
	a, err := ParseAction("studio-move", validAction(), regs)
	if err != nil {
		t.Fatal(err)
	}
	if a.Scope != ScopePhoto || a.Needs != CapEdit || a.Fields["id"] != "studio-move" || !reflect.DeepEqual(a.Fields["filter_values"], []string{"done"}) {
		t.Fatalf("合格的動作：%+v", a)
	}
	// 沒有狀態的動作：只有鈕，沒有狀態與篩選欄位。
	plain := validAction()
	for _, k := range []string{"status_kind", "status_field", "status_labels", "filter_label", "filter_values"} {
		delete(plain, k)
	}
	if a, err := ParseAction("studio-plain", plain, regs); err != nil || a.Fields["status_kind"] != nil || a.Fields["filter_label"] != nil {
		t.Fatalf("沒有狀態的動作：%+v %v", a, err)
	}

	for name, c := range map[string]struct {
		id   string
		edit func(map[string]any)
		want string
	}{
		"no package":       {"studio-x", func(f map[string]any) { delete(f, "package") }, "沒寫 package"},
		"id prefix":        {"other-x", func(map[string]any) {}, "studio-"},
		"id only prefix":   {"studio-", func(map[string]any) {}, "studio-"},
		"not installed":    {"ghost-x", func(f map[string]any) { f["package"] = "ghost" }, "沒裝、沒啟用或被拒"},
		"no label":         {"studio-x", func(f map[string]any) { f["label"] = " " }, "label"},
		"long label":       {"studio-x", func(f map[string]any) { f["label"] = strings.Repeat("字", 21) }, "label"},
		"bad scope":        {"studio-x", func(f map[string]any) { f["scope"] = "all" }, "scope"},
		"bad needs":        {"studio-x", func(f map[string]any) { f["needs"] = "admin" }, "needs"},
		"foreign kind":     {"studio-x", func(f map[string]any) { f["request_kind"] = "closet.request" }, "request_kind"},
		"undeclared kind":  {"studio-x", func(f map[string]any) { f["request_kind"] = "studio.nothing" }, "request_kind"},
		"no fields":        {"studio-x", func(f map[string]any) { f["request_fields"] = []any{} }, "request_fields"},
		"unknown field":    {"studio-x", func(f map[string]any) { f["request_fields"] = []any{"persona", "shot"} }, "shot"},
		"dup field":        {"studio-x", func(f map[string]any) { f["request_fields"] = []any{"persona", "persona"} }, "重複"},
		"photo on outfit":  {"studio-x", func(f map[string]any) { f["scope"] = "outfit" }, "photo 只能用在 scope = photo"},
		"set overrides":    {"studio-x", func(f map[string]any) { f["request_set"] = map[string]any{"persona": "x"} }, "request_set"},
		"set by":           {"studio-x", func(f map[string]any) { f["request_set"] = map[string]any{"by": "x"} }, "request_set"},
		"set nested":       {"studio-x", func(f map[string]any) { f["request_set"] = map[string]any{"type": []any{"a"}} }, "request_set"},
		"status foreign":   {"studio-x", func(f map[string]any) { f["status_kind"] = "closet.state" }, "status_kind"},
		"status no field":  {"studio-x", func(f map[string]any) { delete(f, "status_field") }, "status_field"},
		"status no labels": {"studio-x", func(f map[string]any) { delete(f, "status_labels") }, "status_labels"},
		"filter no status": {"studio-x", func(f map[string]any) { delete(f, "status_kind") }, "要配 status_kind"},
		"filter values":    {"studio-x", func(f map[string]any) { f["filter_values"] = []any{"gone"} }, "gone"},
		"values no label":  {"studio-x", func(f map[string]any) { delete(f, "filter_label") }, "filter_values 要配 filter_label"},
		// 官方套件（含衣櫃自己）不能登記：不然秘書可以做出替主人兌換、升成官方的鈕。
		"official": {"closet-x", func(f map[string]any) {
			f["package"], f["request_kind"], f["request_set"] = "closet", "closet.request", map[string]any{"type": "unlock"}
		}, "官方套件"},
		// 請求與狀態用到的欄位都要在那個種類的規格裡（核心自己寫的 id、created_at 這類不會在規格裡）。
		"set undeclared":    {"studio-x", func(f map[string]any) { f["request_set"] = map[string]any{"color": "red"} }, "沒有欄位「color」"},
		"set reserved":      {"studio-x", func(f map[string]any) { f["request_set"] = map[string]any{"created_at": "x"} }, "沒有欄位「created_at」"},
		"status undeclared": {"studio-x", func(f map[string]any) { f["status_field"] = "mood" }, "沒有欄位「mood」"},
		"status no target":  {"studio-x", func(f map[string]any) { f["status_kind"] = "studio.log" }, "studio.log"},
	} {
		t.Run(name, func(t *testing.T) {
			f := validAction()
			c.edit(f)
			_, err := ParseAction(c.id, f, regs)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("要不合格而且說到「%s」，實際 %v", c.want, err)
			}
		})
	}
}

func TestSupportedActionsFilterAndOrder(t *testing.T) {
	in := []Action{
		{ID: "s-c", Scope: ScopePhoto, Needs: CapMine},
		{ID: "s-b", Scope: ScopeOutfit, Needs: CapEdit, Order: 2},
		{ID: "s-a", Scope: ScopeOutfit, Needs: CapMine, Order: 2},
		{ID: "s-z", Scope: ScopeOutfit, Needs: CapMine, Order: 1},
	}
	ids := func(list []Action) []string {
		var out []string
		for _, a := range list {
			out = append(out, a.ID)
		}
		return out
	}
	if got := ids(SupportedActions(in, Capabilities(true))); !reflect.DeepEqual(got, []string{"s-z", "s-a", "s-b", "s-c"}) {
		t.Fatalf("開發家：%v", got)
	}
	if got := ids(SupportedActions(in, Capabilities(false))); !reflect.DeepEqual(got, []string{"s-z", "s-a", "s-c"}) {
		t.Fatalf("一般家沒有 needs = edit 的：%v", got)
	}
}

// 能力與 scope 的名字有三份（這裡的常數、schemas/action.toml 的 enum、頁面 pages/lib/caps.ts），這支測試守住三份一樣。
func TestCapNamesMatchSchemaAndPages(t *testing.T) {
	schema, err := os.ReadFile("../../schemas/action.toml")
	if err != nil {
		t.Fatal(err)
	}
	enum := func(field string) []string {
		m := regexp.MustCompile(`(?s)name = "` + field + `"\s*\ntype = "enum"\s*\nenum = \[([^\]]*)\]`).FindSubmatch(schema)
		if m == nil {
			t.Fatalf("action.toml 找不到 %s 的 enum", field)
		}
		var out []string
		for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(m[1], -1) {
			out = append(out, string(q[1]))
		}
		return out
	}
	if got := enum("needs"); !reflect.DeepEqual(got, KnownCaps) {
		t.Fatalf("action.toml 的 needs %v，程式 %v", got, KnownCaps)
	}
	if got := enum("scope"); !reflect.DeepEqual(got, []string{ScopeOutfit, ScopePhoto}) {
		t.Fatalf("action.toml 的 scope %v", got)
	}
	page, err := os.ReadFile("../../pages/lib/caps.ts")
	if err != nil {
		t.Fatal(err)
	}
	var consts []string
	for _, m := range regexp.MustCompile(`export const CAP_[A-Z_]+ = "([^"]+)"`).FindAllSubmatch(page, -1) {
		consts = append(consts, string(m[1]))
	}
	if !reflect.DeepEqual(consts, KnownCaps) {
		t.Fatalf("pages/lib/caps.ts 的 CAP_* %v，程式 %v", consts, KnownCaps)
	}
	// closet-mine 的頁面抄了用得到的一小段（套件不能互相 import，CMI-24）：紀錄名與 edit 要跟這裡一致。
	mine, err := os.ReadFile("../../../closet-mine/pages/lib/caps.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`export const CAPABILITIES = "closet\.capabilities"`).Match(mine) {
		t.Fatal(`closet-mine pages/lib/caps.ts 的 CAPABILITIES 不是 "closet.capabilities"`)
	}
	if !regexp.MustCompile(`export const CAP_EDIT = "` + CapEdit + `"`).Match(mine) {
		t.Fatalf("closet-mine pages/lib/caps.ts 的 CAP_EDIT 不是 %q", CapEdit)
	}
	// closet-mine 的程式也讀它（換圖要不要回到還沒採用、能不能換官方的照片，CMI-22、CMI-25）。
	svc, err := os.ReadFile("../../../closet-mine/internal/run/service.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`kindCaps\s+= "closet\.capabilities"`).Match(svc) || !regexp.MustCompile(`capEdit\s+= "`+CapEdit+`"`).Match(svc) {
		t.Fatalf("closet-mine internal/run/service.go 的 kindCaps／capEdit 要是 closet.capabilities／%q", CapEdit)
	}
}

// 「修改」分頁開關（SEC-12）：有 edit 而且沒關才加 edit-tab；一般家怎樣都沒有；沒設、怪值都當開，只有 off 關。
func TestWithEditTab(t *testing.T) {
	if got := WithEditTab(Capabilities(true), true); !reflect.DeepEqual(got, []string{CapMine, CapEdit, CapEditTab}) {
		t.Fatalf("開發家、沒關：%v", got)
	}
	if got := WithEditTab(Capabilities(true), false); !reflect.DeepEqual(got, []string{CapMine, CapEdit}) {
		t.Fatalf("開發家、關掉：%v", got)
	}
	if got := WithEditTab(Capabilities(false), true); !reflect.DeepEqual(got, []string{CapMine}) {
		t.Fatalf("一般家：%v", got)
	}
	for v, want := range map[string]bool{"": true, "on": true, " OFF ": false, "off": false, "xx": true} {
		if EditTabEnabled(v) != want {
			t.Fatalf("EditTabEnabled(%q) 應該是 %v", v, want)
		}
	}
}
