package main

import (
	"strings"
	"testing"
)

// TestBodyKindRegistered：官方 body 套件放進 fixture 家，身體數據註冊成 body.entry；
// 編號要自己給（日期）、不合格的日期擋、同一天不能記兩次、誰都可以改同一份、刪除＝封存。
// Guards: REQ-129, REQ-236
func TestBodyKindRegistered(t *testing.T) {
	_, sock := startFixtureAPIWith(t, func(dir string) { copyOfficialPackage(t, dir, "body") })
	t.Setenv("EVERGENIE_AGENT", "health")

	if kinds := hgOK(t, sock, "data", "kinds"); !strings.Contains(kinds, "body.entry") || !strings.Contains(kinds, "身體數據") {
		t.Fatalf("data kinds 少了 body.entry：\n%s", kinds)
	}
	body := `{"date":"2026-10-25","weight":72.4,"fat":18.5}`
	if code, _, errOut := hg(t, sock, "", "data", "add", "body.entry", "--json", body); code != 2 || !strings.Contains(errOut, "編號要自己給") {
		t.Fatalf("body 不給 id 要被擋：%d %q", code, errOut)
	}
	if id := strings.TrimSpace(hgOK(t, sock, "data", "add", "body.entry", "--id", "2026-10-25", "--json", body)); id != "2026-10-25" {
		t.Fatalf("body：%q", id)
	}
	if code, _, errOut := hg(t, sock, "", "data", "add", "body.entry", "--id", "Oct 25", "--json", body); code == 0 {
		t.Fatalf("不合格的日期 id 要被擋：%d %q", code, errOut)
	}
	if code, _, errOut := hg(t, sock, "", "data", "add", "body.entry", "--id", "2026-10-25", "--json", body); code == 0 {
		t.Fatalf("同一天不能再 add：%d %q", code, errOut)
	}
	t.Setenv("EVERGENIE_AGENT", "life")
	hgOK(t, sock, "data", "set", "body.entry", "2026-10-25", "--json", `{"weight":72.1}`)
	b := jsonLine(t, hgOK(t, sock, "data", "get", "body.entry", "2026-10-25", "--json"))["fields"].(map[string]any)
	if b["weight"] != 72.1 || b["fat"] != 18.5 {
		t.Fatalf("body set 之後：%v", b)
	}
	hgOK(t, sock, "data", "rm", "body.entry", "2026-10-25")
	if out := hgOK(t, sock, "data", "list", "body.entry"); strings.Contains(out, "2026-10-25") {
		t.Fatalf("封存後列表：%q", out)
	}
}

// TestHealthOneAgentEachPartCanBeOff：飲食、運動、身體由同一位 health 秘書負責——只有 health 套件帶秘書樣板；
// 三項是各自的套件，停用其中一項，那項的資料就寫不進去（秘書照樣板先看哪些開著），其他項照常；重新啟用就回來。
// Guards: REQ-236
func TestHealthOneAgentEachPartCanBeOff(t *testing.T) {
	_, sock := startFixtureAPIWith(t, func(dir string) {
		for _, id := range []string{"health", "nutrition", "fitness", "body"} {
			copyOfficialPackage(t, dir, id)
		}
	})
	t.Setenv("EVERGENIE_AGENT", "health")

	if out := hgOK(t, sock, "packages", "sync", "health"); !strings.Contains(out, "health") {
		t.Fatalf("health 的秘書樣板要能預演：%q", out)
	}
	for _, id := range []string{"nutrition", "fitness", "body"} {
		pkg := jsonLine(t, hgOK(t, sock, "packages", "get", id))
		if pkg["enabled"] != true {
			t.Fatalf("%s 預設要啟用：%v", id, pkg)
		}
	}

	out := hgOK(t, sock, "packages", "disable", "body")
	if !strings.Contains(out, "停用") {
		t.Fatalf("停用 body：%q", out)
	}
	if code, _, errOut := hg(t, sock, "", "data", "add", "body.entry", "--id", "2026-10-25", "--json", `{"date":"2026-10-25","weight":72}`); code == 0 {
		t.Fatalf("身體關掉後不能再記：%d %q", code, errOut)
	}
	meal := `{"date":"2026-10-25","at":"2026-10-25 12:30","meal":"午餐","kcal":600,"protein":30,"carb":70,"fat":20}`
	if id := strings.TrimSpace(hgOK(t, sock, "data", "add", "nutrition.meal", "--json", meal)); !strings.HasPrefix(id, "M-") {
		t.Fatalf("身體關掉時飲食照常：%q", id)
	}

	hgOK(t, sock, "packages", "enable", "body")
	if id := strings.TrimSpace(hgOK(t, sock, "data", "add", "body.entry", "--id", "2026-10-25", "--json", `{"date":"2026-10-25","weight":72}`)); id != "2026-10-25" {
		t.Fatalf("重新啟用後要能記：%q", id)
	}
}
