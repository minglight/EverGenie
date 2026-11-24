package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyOfficialPackage 把 repo 裡的官方套件 id 複製進家的 packages/official/（家裡有這個資料夾時核心就用它，
// 所以測試只載入這一個套件，不會碰到 engine-claude 之類要起行程的官方套件）。
func copyOfficialPackage(t *testing.T, home, id string) {
	t.Helper()
	src := filepath.Join("..", "..", "packages", "official", id)
	dst := filepath.Join(home, "packages", "official", id)
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
}

// TestNutritionKindsRegistered：官方 nutrition 套件放進 fixture 家，三種資料都註冊成功；
// 缺必填被擋、items（list of map）收得下、目標只有 main 這一筆。秘書樣板在 health 套件（health_test.go）。
// Guards: REQ-128
func TestNutritionKindsRegistered(t *testing.T) {
	_, sock := startFixtureAPIWith(t, func(dir string) { copyOfficialPackage(t, dir, "nutrition") })
	t.Setenv("EVERGENIE_AGENT", "health")

	kinds := hgOK(t, sock, "data", "kinds")
	for _, want := range []string{"nutrition.meal", "餐點", "M-<數字>", "nutrition.goal", "飲食目標", "nutrition.fav", "常吃餐點", "F-<數字>", "enum(早餐|午餐|晚餐|點心|宵夜)"} {
		if !strings.Contains(kinds, want) {
			t.Errorf("data kinds 少了「%s」：\n%s", want, kinds)
		}
	}

	// 缺 date 被擋（沒有存進去）。
	noDate := `{"at":"2026-10-25 12:30","meal":"午餐","kcal":600,"protein":30,"carb":70,"fat":20}`
	if code, _, errOut := hg(t, sock, "", "data", "add", "nutrition.meal", "--json", noDate); code == 0 || !strings.Contains(errOut, "date") {
		t.Fatalf("缺 date 要被擋並提到 date：%d %q", code, errOut)
	}
	// 餐別不在列舉裡也擋。
	badMeal := `{"date":"2026-10-25","at":"2026-10-25 12:30","meal":"下午茶","kcal":600,"protein":30,"carb":70,"fat":20}`
	if code, _, errOut := hg(t, sock, "", "data", "add", "nutrition.meal", "--json", badMeal); code == 0 || !strings.Contains(errOut, "meal") {
		t.Fatalf("餐別不合要被擋：%d %q", code, errOut)
	}

	// 完整的一餐：items 是 list of map、photos 是 list of string。
	full := `{"date":"2026-10-25","at":"2026-10-25 12:30","meal":"午餐","kcal":680,"protein":42,"carb":70,"fat":22.5,` +
		`"items":[{"name":"雞腿便當","qty":"1 份","kcal":680,"protein":42,"carb":70,"fat":22.5}],` +
		`"photos":["/share/20261025-123012-ab12-lunch.jpg"],"conf":"中","note":"估算"}`
	// 編號不一定從 1 開始：欄位型別 vault 才檢查得到的錯（例如餐別不在列舉裡）已經領了號才被擋。
	id := strings.TrimSpace(hgOK(t, sock, "data", "add", "nutrition.meal", "--json", full))
	if !strings.HasPrefix(id, "M-") {
		t.Fatalf("第一餐：%q", id)
	}
	rec := jsonLine(t, hgOK(t, sock, "data", "get", "nutrition.meal", id, "--json"))
	fields := rec["fields"].(map[string]any)
	items, _ := fields["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["name"] != "雞腿便當" || fields["fat"] != 22.5 {
		t.Fatalf("items 沒存好：%v", fields)
	}
	if photos, _ := fields["photos"].([]any); len(photos) != 1 || photos[0] != "/share/20261025-123012-ab12-lunch.jpg" {
		t.Fatalf("photos 沒存好：%v", fields["photos"])
	}
	list := hgOK(t, sock, "data", "list", "nutrition.meal", "--since", "2026-10-25")
	if !strings.HasPrefix(list, id+"  2026-10-25") {
		t.Fatalf("list：%q", list)
	}
	if out := hgOK(t, sock, "data", "list", "nutrition.meal", "--since", "2026-10-26"); strings.Contains(out, "M-") {
		t.Fatalf("--since 隔天不該有東西：%q", out)
	}

	// 目標：id 自己給（main）；不給 id 被擋；改欄位用 set。
	goal := `{"mode":"減脂","kcal":1800,"protein":120,"carb":0,"fat":0}`
	if code, _, errOut := hg(t, sock, "", "data", "add", "nutrition.goal", "--json", goal); code != 2 || !strings.Contains(errOut, "編號要自己給") {
		t.Fatalf("goal 不給 id 要被擋：%d %q", code, errOut)
	}
	if id := strings.TrimSpace(hgOK(t, sock, "data", "add", "nutrition.goal", "--id", "main", "--json", goal)); id != "main" {
		t.Fatalf("goal：%q", id)
	}
	hgOK(t, sock, "data", "set", "nutrition.goal", "main", "--json", `{"kcal":1900}`)
	g := jsonLine(t, hgOK(t, sock, "data", "get", "nutrition.goal", "main", "--json"))["fields"].(map[string]any)
	if g["kcal"] != float64(1900) || g["mode"] != "減脂" {
		t.Fatalf("goal set 之後：%v", g)
	}

	// 常吃餐點。
	fav := `{"name":"雞腿便當","kcal":680,"protein":42,"carb":70,"fat":22,"items":[{"name":"雞腿","qty":"1 隻"}]}`
	if id := strings.TrimSpace(hgOK(t, sock, "data", "add", "nutrition.fav", "--json", fav)); id != "F-1" {
		t.Fatalf("fav：%q", id)
	}

	// 刪除＝封存。
	hgOK(t, sock, "data", "rm", "nutrition.meal", id)
	if out := strings.TrimSpace(hgOK(t, sock, "data", "list", "nutrition.meal")); strings.Contains(out, "M-") {
		t.Fatalf("封存後列表：%q", out)
	}
}
