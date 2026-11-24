package run

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// 問不到是不是開發家（Dev 回錯）：不改寫能力紀錄、照上一輪的能力算這一輪（開發家照樣給真圖），也不當成一般家（DEV-160 審查 S5，憲法第 10 條）。
// 能力紀錄被封存：下一輪用同一個編號重寫一筆沒封存的（writeIfChanged）。
func TestCapabilitiesKeepLastWhenDevUnknownAndRewriteArchived(t *testing.T) {
	sc := newScene(t)
	sc.svc.Dev = devOn
	sc.round()
	puts := sc.v.puts[KindCapabilities]
	sc.svc.Dev = func(context.Context) (bool, error) { return false, errors.New("核心沒回應") }
	sc.round()
	if got := sc.fields(KindCapabilities, "main")["supports"]; !reflect.DeepEqual(got, []any{"mine", "edit", "edit-tab"}) || sc.v.puts[KindCapabilities] != puts {
		t.Fatalf("問不到時不改寫能力紀錄：%v（寫了 %d 次）", got, sc.v.puts[KindCapabilities]-puts)
	}
	if sc.outfitView("night")["preview"] != true {
		t.Fatal("照上一輪的能力：開發家照樣給真圖")
	}

	sc.svc.Dev = devOn
	if err := sc.v.Archive(context.Background(), KindCapabilities, "main"); err != nil {
		t.Fatal(err)
	}
	sc.round()
	if r, _ := sc.v.get(KindCapabilities, "main"); r.Archived || !reflect.DeepEqual(r.Fields["supports"], []any{"mine", "edit", "edit-tab"}) {
		t.Fatalf("封存的能力紀錄要重寫回來：archived=%v %v", r.Archived, r.Fields)
	}
	// 封存了、這一輪又問不到：沒有可以照的上一輪，當沒有 edit（寧可遮），也不寫。
	if err := sc.v.Archive(context.Background(), KindCapabilities, "main"); err != nil {
		t.Fatal(err)
	}
	sc.svc.Dev = func(context.Context) (bool, error) { return false, errors.New("核心沒回應") }
	sc.round()
	if r, _ := sc.v.get(KindCapabilities, "main"); !r.Archived {
		t.Fatal("問不到時不寫")
	}
	if _, has := sc.outfitView("night")["preview"]; has {
		t.Fatal("沒有上一輪可照：當沒有 edit")
	}
}

// 一般家沒有上架／下架（作者 2026-11-15，CLO-38）：能力問得到、確定沒有 edit 時，自己做的設計中衣服每一輪自動上架、照片標採用；
// 官方的設計中不動；開發家、問不到、沒接 Dev 都不動；做過的再跑一輪不寫任何東西（冪等）。
func TestOrdinaryHomePublishesMineDrafts(t *testing.T) {
	seed := func(sc *scene) {
		pngFile(t, sc.home+"/personas/mine/photos/builder-my-5--u5.png", 70)
		pngFile(t, sc.home+"/personas/mine/photos/builder-my-5--u6.png", 71)
		sc.v.seed(KindOutfit, "builder-my-5", map[string]any{"persona": "builder", "outfit": "my-5", "name": "草稿", "cost": 0, "state": "draft", "source": "mine", "order": 20})
		for i, id := range []string{"builder-my-5--u5", "builder-my-5--u6"} {
			sc.v.seed(KindPhoto, id, map[string]any{"persona": "builder", "outfit": "builder-my-5", "type": "half", "order": i + 1,
				"hidden": false, "source": "mine", "file": "photos/" + id + ".png", "adopted": i == 0})
		}
		sc.outfit("wip", 0, "draft", 9) // 官方的設計中
	}
	for name, dev := range map[string]func(context.Context) (bool, error){
		"開發家": devOn,
		"問不到": func(context.Context) (bool, error) { return false, errors.New("核心沒回應") },
		"沒接":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			sc := newScene(t)
			seed(sc)
			sc.svc.Dev = dev
			sc.round()
			if sc.fields(KindOutfit, "builder-my-5")["state"] != "draft" || sc.fields(KindPhoto, "builder-my-5--u6")["adopted"] != false {
				t.Fatal("不是確定的一般家：設計中不動")
			}
		})
	}
	sc := newScene(t)
	seed(sc)
	sc.svc.Dev = func(context.Context) (bool, error) { return false, nil }
	sc.round()
	if sc.fields(KindOutfit, "builder-my-5")["state"] != "active" {
		t.Fatalf("一般家：我的設計中自動上架：%v", sc.fields(KindOutfit, "builder-my-5"))
	}
	for _, id := range []string{"builder-my-5--u5", "builder-my-5--u6"} {
		if sc.fields(KindPhoto, id)["adopted"] != true {
			t.Fatalf("%s 要標採用", id)
		}
	}
	if sc.fields(KindOutfit, "builder-wip")["state"] != "draft" {
		t.Fatal("官方的設計中不動")
	}
	if o := sc.outfitView("my-5"); o == nil || o["state"] != "active" || o["owned"] != true {
		t.Fatalf("這一輪的檢視就是上架的樣子：%v", o)
	}
	before := sc.v.rev
	sc.round()
	if sc.v.rev != before {
		t.Fatal("做過的再跑一輪不寫任何東西")
	}
}

// 隱藏版開關關著時，「修改」分頁不列隱藏版的照片、不留框（作者 2026-11-15，CLO-35）：衣櫃在 closet.person 寫 hidden_off，
// 開著時是空的；自己的隱藏版另看「自己的隱藏版」開關。
func TestPersonViewListsHiddenOff(t *testing.T) {
	sc, _ := variantScene(t)
	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": false})
	sc.v.seed(KindPhoto, "builder-base--mine-h", map[string]any{"persona": "builder", "outfit": "builder-base", "type": "half", "order": 9,
		"hidden": true, "source": "mine", "file": "photos/builder-base--mine-h.png", "adopted": true})
	sc.round()
	off, _ := sc.person()["hidden_off"].([]any)
	if len(off) < 2 || !contains(off, "builder-base--mine-h") || !contains(off, "builder-base--half1-h") {
		t.Fatalf("關著時列出隱藏版的編號：%v", off)
	}
	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": true})
	sc.round()
	off, _ = sc.person()["hidden_off"].([]any)
	if !reflect.DeepEqual(off, []any{"builder-base--mine-h"}) {
		t.Fatalf("開著時只剩自己的隱藏版（「自己的隱藏版」開關沒開）：%v", off)
	}
	sc.svc.Adult = func(context.Context) bool { return true }
	sc.round()
	if off, _ = sc.person()["hidden_off"].([]any); len(off) != 0 {
		t.Fatalf("都開著時是空的：%v", off)
	}
}

func contains(list []any, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
