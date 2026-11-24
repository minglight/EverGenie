package main

import (
	"strings"
	"testing"
)

// TestFitnessKindsRegistered：官方 fitness 套件放進 fixture 家，三種資料都註冊成功；
// 缺必填被擋、sets（list of map）收得下。身體數據在 body 套件（health_test.go）。
// Guards: REQ-127
func TestFitnessKindsRegistered(t *testing.T) {
	_, sock := startFixtureAPIWith(t, func(dir string) { copyOfficialPackage(t, dir, "fitness") })
	t.Setenv("EVERGENIE_AGENT", "health")

	kinds := hgOK(t, sock, "data", "kinds")
	for _, want := range []string{
		"fitness.workout", "訓練", "W-<數字>",
		"fitness.equipment", "器材", "E-<數字>",
		"fitness.note", "訓練筆記", "N-<數字>",
		"enum(重訓|跑步|划船|游泳|騎車)", "enum(進行中|完成)", "enum(自由式|蛙式|仰式|蝶式|混合)",
	} {
		if !strings.Contains(kinds, want) {
			t.Errorf("data kinds 少了「%s」：\n%s", want, kinds)
		}
	}

	// 缺 type（重訓或跑步）被擋；種類不在列舉裡也擋。
	noType := `{"date":"2026-10-25","status":"完成"}`
	if code, _, errOut := hg(t, sock, "", "data", "add", "fitness.workout", "--json", noType); code == 0 || !strings.Contains(errOut, "type") {
		t.Fatalf("缺 type 要被擋並提到 type：%d %q", code, errOut)
	}
	badType := `{"type":"瑜珈","date":"2026-10-25","status":"完成"}`
	if code, _, errOut := hg(t, sock, "", "data", "add", "fitness.workout", "--json", badType); code == 0 || !strings.Contains(errOut, "type") {
		t.Fatalf("種類不合要被擋：%d %q", code, errOut)
	}

	// 有氧（DEV-101）：游泳帶心率、池長、趟數、泳姿；泳姿不在列舉裡擋。
	swim := `{"type":"游泳","date":"2026-10-24","status":"完成","distance_km":1,"duration_min":25,"pace":"2:30",` +
		`"avg_hr":140,"max_hr":160,"pool_m":25,"laps":40,"swolf":38,"stroke_style":"自由式"}`
	swimID := strings.TrimSpace(hgOK(t, sock, "data", "add", "fitness.workout", "--json", swim))
	sf := jsonLine(t, hgOK(t, sock, "data", "get", "fitness.workout", swimID, "--json"))["fields"].(map[string]any)
	if sf["avg_hr"] != 140.0 || sf["laps"] != 40.0 || sf["stroke_style"] != "自由式" {
		t.Fatalf("游泳欄位沒存好：%v", sf)
	}
	badStroke := `{"type":"游泳","date":"2026-10-24","status":"完成","stroke_style":"狗爬式"}`
	if code, _, errOut := hg(t, sock, "", "data", "add", "fitness.workout", "--json", badStroke); code == 0 || !strings.Contains(errOut, "stroke_style") {
		t.Fatalf("泳姿不合要被擋：%d %q", code, errOut)
	}

	// 一次重訓：sets 是 list of map（weight 有小數）。
	full := `{"type":"重訓","date":"2026-10-25","started":"2026-10-25 18:00","status":"進行中",` +
		`"sets":[{"equipment":"啞鈴彎舉","weight":12.5,"reps":10,"feel":"剛好"},{"equipment":"棒式","weight":0,"reps":30,"feel":"吃力"}]}`
	id := strings.TrimSpace(hgOK(t, sock, "data", "add", "fitness.workout", "--json", full))
	if !strings.HasPrefix(id, "W-") {
		t.Fatalf("workout 編號：%q", id)
	}
	rec := jsonLine(t, hgOK(t, sock, "data", "get", "fitness.workout", id, "--json"))
	fields := rec["fields"].(map[string]any)
	sets, _ := fields["sets"].([]any)
	if len(sets) != 2 || sets[0].(map[string]any)["weight"] != 12.5 || fields["type"] != "重訓" {
		t.Fatalf("sets 沒存好：%v", fields)
	}
	// 收操：補欄位、review_by 填自己的 id。
	hgOK(t, sock, "data", "set", "fitness.workout", id, "--json", `{"status":"完成","feeling":"剛好","review":"不錯","review_by":"health"}`)
	fields = jsonLine(t, hgOK(t, sock, "data", "get", "fitness.workout", id, "--json"))["fields"].(map[string]any)
	if fields["status"] != "完成" || fields["review_by"] != "health" {
		t.Fatalf("set 之後：%v", fields)
	}
	if out := hgOK(t, sock, "data", "list", "fitness.workout", "--since", "2026-10-25"); !strings.HasPrefix(out, id+"  2026-10-25") {
		t.Fatalf("list：%q", out)
	}

	// 器材、筆記。
	eq := `{"name":"棒式","type":"徒手","unit":"秒","primary":["核心"],"secondary":[]}`
	if id := strings.TrimSpace(hgOK(t, sock, "data", "add", "fitness.equipment", "--json", eq)); !strings.HasPrefix(id, "E-") {
		t.Fatalf("equipment：%q", id)
	}
	badUnit := `{"name":"棒式","unit":"分"}`
	if code, _, errOut := hg(t, sock, "", "data", "add", "fitness.equipment", "--json", badUnit); code == 0 || !strings.Contains(errOut, "unit") {
		t.Fatalf("unit 不合要被擋：%d %q", code, errOut)
	}
	note := `{"title":"十月訓練計畫","date":"2026-10-25","pinned":true}`
	if id := strings.TrimSpace(hgOK(t, sock, "data", "add", "fitness.note", "--json", note)); !strings.HasPrefix(id, "N-") {
		t.Fatalf("note：%q", id)
	}
	if strings.Contains(kinds, "fitness.body") {
		t.Errorf("身體數據已經搬到 body 套件，fitness 不該再有 fitness.body：\n%s", kinds)
	}
}
