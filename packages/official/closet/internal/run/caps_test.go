package run

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 能力（CLO-32）：衣櫃每一輪算好「這個家支援什麼」寫成 closet.capabilities/main；一般家只有 mine，開發家多 edit。
// 「是不是開發家」只在衣櫃這裡換算一次；核心的 show_when（cap:closet.edit）與頁面都只讀這一筆。
func TestCapabilitiesFollowHome(t *testing.T) {
	sc := newScene(t)
	sc.round()
	caps := sc.fields(KindCapabilities, "main")
	if !reflect.DeepEqual(caps["supports"], []any{"mine"}) {
		t.Fatalf("一般家（沒接 Dev）只有 mine：%v", caps["supports"])
	}
	if a, ok := caps["actions"].([]any); !ok || len(a) != 0 {
		t.Fatalf("沒有登記的動作：actions 是空的清單，實際 %#v", caps["actions"])
	}
	sc.round()
	if n := sc.v.puts[KindCapabilities]; n != 1 {
		t.Fatalf("沒變不該再寫：寫了 %d 次", n)
	}

	sc.svc.Dev = devOn
	sc.round()
	if got := sc.fields(KindCapabilities, "main")["supports"]; !reflect.DeepEqual(got, []any{"mine", "edit", "edit-tab"}) {
		t.Fatalf("開發家：mine、edit、edit-tab（「修改」分頁開關沒接＝開），實際 %v", got)
	}
	// 「修改」分頁開關關掉（SEC-12）：edit 還在，edit-tab 拿掉；再打開就回來。
	sc.svc.EditTab = func(context.Context) bool { return false }
	sc.round()
	if got := sc.fields(KindCapabilities, "main")["supports"]; !reflect.DeepEqual(got, []any{"mine", "edit"}) {
		t.Fatalf("開發家、分頁關掉：mine、edit，實際 %v", got)
	}
	sc.svc.EditTab = func(context.Context) bool { return true }
	sc.round()
	if got := sc.fields(KindCapabilities, "main")["supports"]; !reflect.DeepEqual(got, []any{"mine", "edit", "edit-tab"}) {
		t.Fatalf("開發家、分頁打開：要回到有 edit-tab，實際 %v", got)
	}
	// 讀不到是不是開發家（Dev 回 false）：當一般家，edit 拿掉。
	sc.svc.Dev = func(context.Context) (bool, error) { return false, nil }
	sc.round()
	if got := sc.fields(KindCapabilities, "main")["supports"]; !reflect.DeepEqual(got, []any{"mine"}) {
		t.Fatalf("不是開發家：只剩 mine，實際 %v", got)
	}
}

func kind(name string, fields ...string) sdk.DataKindDecl {
	d := sdk.DataKindDecl{Kind: name}
	for _, f := range fields {
		d.Fields = append(d.Fields, sdk.DataFieldDecl{Name: f, Type: "string"})
	}
	return d
}

// studio 是測試用的登記套件（使用者自己的）：宣告了 request（請求）與 review（狀態）兩種資料。
// 衣櫃自己（官方）也在清單裡，用來驗官方套件不能登記。
func studioPackages(enabled bool) func(context.Context) ([]sdk.Package, error) {
	return func(context.Context) ([]sdk.Package, error) {
		return []sdk.Package{
			{ID: "studio", Tier: sdk.TierMine, Enabled: enabled, State: sdk.PackageStateRunning, Data: []sdk.DataKindDecl{
				kind("request", "persona", "outfit", "photo", "type"), kind("review", "persona", "outfit", "photo", "state"),
			}},
			{ID: "closet", Tier: sdk.TierOfficial, Enabled: true, State: sdk.PackageStateRunning, Data: []sdk.DataKindDecl{
				kind("request", "persona", "outfit", "type"),
			}},
		}, nil
	}
}

func moveAction(needs string) map[string]any {
	return map[string]any{
		"package": "studio", "label": "搬到別處", "scope": "outfit", "needs": needs,
		"request_kind": "studio.request", "request_fields": []any{"persona", "outfit"}, "request_set": map[string]any{"type": "move"},
		"status_kind": "studio.review", "status_field": "state", "status_labels": map[string]any{"done": "搬好了", "stale": "改過要再搬"},
		"filter_label": "搬過的",
	}
}

func actionsOf(sc *scene) []any {
	sc.t.Helper()
	list, ok := sc.fields(KindCapabilities, "main")["actions"].([]any)
	if !ok {
		sc.t.Fatalf("actions 要是清單：%#v", sc.fields(KindCapabilities, "main")["actions"])
	}
	return list
}

// 動作插座（CLO-33）：別的套件登記一個動作（closet.action），衣櫃驗過、這個家撐得起（needs 在 supports 裡）才放進 capabilities 的 actions；
// 頁面照這份清單畫鈕與篩選。沒登記＝空；封存、登記的套件關掉、讀不到套件清單都不列。
func TestRegisteredActionListed(t *testing.T) {
	sc := newScene(t)
	sc.svc.Dev = devOn
	sc.svc.Packages = studioPackages(true)
	sc.round()
	if a := actionsOf(sc); len(a) != 0 {
		t.Fatalf("還沒登記：%v", a)
	}

	sc.v.seedBy(KindAction, "studio-move", moveAction("edit"), "studio-agent")
	sc.round()
	got := actionsOf(sc)
	if len(got) != 1 {
		t.Fatalf("登記了一個：%v", got)
	}
	want := map[string]any{
		"id": "studio-move", "package": "studio", "label": "搬到別處", "scope": "outfit", "needs": "edit", "order": 0,
		"request_kind": "studio.request", "request_fields": []string{"persona", "outfit"}, "request_set": map[string]any{"type": "move"},
		"status_kind": "studio.review", "status_field": "state", "status_labels": map[string]any{"done": "搬好了", "stale": "改過要再搬"},
		"filter_label": "搬過的", "filter_values": []string{"done", "stale"}, // 沒寫 filter_values＝status_labels 的全部值
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("發給頁面的動作：\n got %#v\nwant %#v", got[0], want)
	}

	// 一般家：needs = edit 的不列，needs = mine 的列。
	sc.svc.Dev = nil
	sc.v.seedBy(KindAction, "studio-keep", map[string]any{"package": "studio", "label": "留一份", "scope": "photo", "needs": "mine",
		"request_kind": "studio.request", "request_fields": []any{"persona", "photo"}, "order": 2}, "studio-agent")
	sc.round()
	if got := actionsOf(sc); len(got) != 1 || got[0].(map[string]any)["id"] != "studio-keep" {
		t.Fatalf("一般家只列 needs = mine 的：%v", got)
	}

	// 登記的套件關掉：一個都不列。
	sc.svc.Dev = devOn
	sc.svc.Packages = studioPackages(false)
	sc.round()
	if got := actionsOf(sc); len(got) != 0 {
		t.Fatalf("studio 關掉了：%v", got)
	}
	// 讀不到套件清單：不知道登記的套件還在不在，不列。
	sc.svc.Packages = func(context.Context) ([]sdk.Package, error) { return nil, errors.New("核心沒回應") }
	sc.round()
	if got := actionsOf(sc); len(got) != 0 {
		t.Fatalf("讀不到套件清單：%v", got)
	}
	// 回來了；開發家兩個都列，照 scope（outfit 在 photo 前）再照 order 排。
	sc.svc.Packages = studioPackages(true)
	sc.round()
	if got := actionsOf(sc); len(got) != 2 || got[0].(map[string]any)["id"] != "studio-move" || got[1].(map[string]any)["id"] != "studio-keep" {
		t.Fatalf("開發家兩個都列：%v", got)
	}
	// 封存＝不要了。
	if err := sc.v.Archive(context.Background(), KindAction, "studio-move"); err != nil {
		t.Fatal(err)
	}
	sc.round()
	if got := actionsOf(sc); len(got) != 1 || got[0].(map[string]any)["id"] != "studio-keep" {
		t.Fatalf("封存的不列：%v", got)
	}
}

// 不合格的登記不列（記一次 log），不影響合格的那個。
func TestInvalidActionsNotListed(t *testing.T) {
	sc := newScene(t)
	sc.svc.Dev = devOn
	sc.svc.Packages = studioPackages(true)
	bad := map[string]func(map[string]any){
		"other-move":       func(map[string]any) {},                                                      // 編號不是 studio- 開頭
		"studio-nokind":    func(f map[string]any) { f["request_kind"] = "studio.nothing" },              // 沒宣告的種類
		"studio-foreign":   func(f map[string]any) { f["request_kind"] = "closet.request" },              // 別人的種類
		"studio-photo":     func(f map[string]any) { f["request_fields"] = []any{"persona", "photo"} },   // 整套不能帶 photo
		"studio-nostatus":  func(f map[string]any) { delete(f, "status_kind") },                          // 有篩選沒狀態
		"studio-badfilter": func(f map[string]any) { f["filter_values"] = []any{"gone"} },                // 篩選值不在 labels 裡
		"studio-needs":     func(f map[string]any) { f["needs"] = "admin" },                              // 不認得的能力
		"ghost-move":       func(f map[string]any) { f["package"] = "ghost" },                            // 沒裝的套件
		"studio-color":     func(f map[string]any) { f["request_set"] = map[string]any{"color": "red"} }, // 規格裡沒有的欄位
		// 官方套件（衣櫃自己）登記：不列，免得做出替主人兌換的鈕。
		"closet-unlock": func(f map[string]any) {
			f["package"], f["request_kind"], f["request_set"] = "closet", "closet.request", map[string]any{"type": "unlock"}
			delete(f, "status_kind")
			delete(f, "status_field")
			delete(f, "status_labels")
			delete(f, "filter_label")
		},
	}
	for id, edit := range bad {
		f := moveAction("edit")
		edit(f)
		sc.v.seedBy(KindAction, id, f, "studio-agent")
	}
	sc.v.seedBy(KindAction, "studio-move", moveAction("edit"), "studio-agent")
	sc.round()
	if got := actionsOf(sc); len(got) != 1 || got[0].(map[string]any)["id"] != "studio-move" {
		t.Fatalf("只列合格的 studio-move：%v", got)
	}
}
