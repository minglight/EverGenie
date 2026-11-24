package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/minglight/evergenie/packages/official/closet-mine/internal/rules"
	"github.com/minglight/evergenie/packages/official/closet-mine/internal/store"
	"github.com/minglight/evergenie/sdk"
)

// foxWithPhotos：我的角色 myfox（U-1 基本款第 1 張）、第 2 張（U-2）、配第 2 張的兩支動畫（U-3、U-4）、全身照（U-5）。
func foxWithPhotos(t *testing.T) *scene {
	sc := newScene(t)
	sc.upload("b.png", png("base"))
	sc.upload("p2.png", png("p2"))
	sc.upload("v1.mp4", mp4("v1"))
	sc.upload("v2.mp4", mp4("v2"))
	sc.upload("f.png", png("full"))
	for _, f := range []map[string]any{
		{"type": "character", "upload": "b.png", "name": "狐", "persona": "myfox"},
		{"type": "photo", "upload": "p2.png", "persona": "myfox", "outfit": "base"},
		{"type": "video", "upload": "v1.mp4", "persona": "myfox", "outfit": "base", "photo": 2},
		{"type": "video", "upload": "v2.mp4", "persona": "myfox", "outfit": "base", "photo": 2, "clip": 2},
		{"type": "full", "upload": "f.png", "persona": "myfox", "outfit": "base"},
	} {
		sc.done(sc.requestAndRound(f))
	}
	return sc
}

// 刪單張：封存那筆、檔改名成備份，配它的動畫一起；拿回：紀錄寫回來、檔改回原名，動畫一起回來。
func TestRemoveAndRestorePhoto(t *testing.T) {
	sc := foxWithPhotos(t)
	del := sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "myfox-base--u2"}) // U-6
	if f := sc.done(del); f["result"] != "myfox-base" {
		t.Fatalf("result：%v", f)
	}
	for _, pid := range []string{"myfox-base--u2", "myfox-base--u3", "myfox-base--u4"} {
		if r, _ := sc.v.get(kindPhoto, pid); !r.Archived {
			t.Fatalf("%s 要封存", pid)
		}
	}
	for _, rel := range []string{"photos/myfox-base--u2.png", "photos/myfox-base--u3.mp4", "photos/myfox-base--u4.mp4"} {
		if sc.read("personas/mine/"+rel) != nil || sc.backupOf(rel, "delete-u6") == "" {
			t.Fatalf("%s 要改名成備份：%v", rel, sc.files("photos"))
		}
	}
	if r, _ := sc.v.get(kindPhoto, "myfox-base--u5"); r.Archived {
		t.Fatal("別張不動")
	}
	// 重播：已經刪了就算做完，不多做。
	snap := sc.snapshot()
	if err := sc.v.Patch(context.Background(), KindRequest, del, map[string]any{"state": "pending"}); err != nil {
		t.Fatal(err)
	}
	sc.round()
	if sc.done(del); sc.snapshot() != snap {
		t.Fatal("重播不能再動檔")
	}

	sc.done(sc.requestAndRound(map[string]any{"type": "restore_photo", "photo_id": "myfox-base--u2", "persona": "myfox"})) // U-7
	sc.photoWant("myfox-base--u2", map[string]any{"file": "photos/myfox-base--u2.png", "order": 2})
	sc.photoWant("myfox-base--u3", map[string]any{"file": "photos/myfox-base--u3.mp4", "of": "myfox-base--u2", "clip": 1})
	sc.photoWant("myfox-base--u4", map[string]any{"file": "photos/myfox-base--u4.mp4", "clip": 2})
	if !bytes.Equal(sc.photoFile("myfox-base--u2"), png("p2")) || !bytes.Equal(sc.photoFile("myfox-base--u4"), mp4("v2")) {
		t.Fatal("檔要改回原名")
	}
	if got := sc.files("photos"); len(got) != 4 { // 第 1 張是角色的頭像 avatars/myfox.png
		t.Fatalf("備份都改回去了：%v", got)
	}
	// 拿回本來就在的：算做完。
	sc.done(sc.requestAndRound(map[string]any{"type": "restore_photo", "photo_id": "myfox-base--u2"}))
}

// 單獨刪掉的一支動畫，之後刪、拿回它那張半身照時不跟著回來（只拿回同一個請求一起刪的）；原本的位置被用了就排到最後。
func TestRestorePhotoOnlyBringsBackItsOwnBatch(t *testing.T) {
	sc := foxWithPhotos(t)
	sc.done(sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "myfox-base--u4"})) // U-6：單獨刪第 2 支
	sc.done(sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "myfox-base--u2"})) // U-7：刪半身照（帶第 1 支）
	sc.upload("p3.png", png("p3"))
	sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "p3.png", "persona": "myfox", "outfit": "base"})) // U-8：排在現在還在的最後（order 2）
	sc.photoWant("myfox-base--u8", map[string]any{"order": 2})
	sc.done(sc.requestAndRound(map[string]any{"type": "restore_photo", "photo_id": "myfox-base--u2"})) // U-9
	if r, _ := sc.v.get(kindPhoto, "myfox-base--u3"); r.Archived || r.Fields["order"] != 3 {
		t.Fatalf("一起刪的第 1 支要回來、order 跟著半身照：%v", r)
	}
	if r, _ := sc.v.get(kindPhoto, "myfox-base--u4"); !r.Archived {
		t.Fatal("單獨刪的第 2 支不跟著回來")
	}
	sc.photoWant("myfox-base--u2", map[string]any{"order": 3}) // 原本的 order 2 被用了，排到最後
	// 再拿回單獨刪的那支：配的那張在了，可以。
	sc.done(sc.requestAndRound(map[string]any{"type": "restore_photo", "photo_id": "myfox-base--u4"}))
	sc.photoWant("myfox-base--u4", map[string]any{"file": "photos/myfox-base--u4.mp4", "clip": 2})
}

// 刪單張的限制：最後一張一般版的半身照不能刪、官方的照片不能刪、拿回動畫要先拿回它那張、拿回全身照時不能已經有一張。
func TestRemovePhotoRules(t *testing.T) {
	sc := foxWithPhotos(t)
	sc.done(sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "myfox-base--u2"}))
	sc.rejected(sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "myfox-base--u1"}), rules.ReasonLastHalf)
	sc.rejected(sc.requestAndRound(map[string]any{"type": "restore_photo", "photo_id": "myfox-base--u3"}), rules.ReasonRestoreHalf)
	sc.done(sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "myfox-base--u5"})) // U-9 刪全身照
	sc.upload("f2.png", png("full-2"))
	sc.done(sc.requestAndRound(map[string]any{"type": "full", "upload": "f2.png", "persona": "myfox", "outfit": "base"}))
	sc.rejected(sc.requestAndRound(map[string]any{"type": "restore_photo", "photo_id": "myfox-base--u5"}), rules.ReasonFullTaken)
	sc.v.seed(kindPhoto, "athena-base--half1", map[string]any{"persona": "athena", "outfit": "athena-base", "type": "half", "order": 1,
		"hidden": false, "source": "official", "file": "athena.png", "adopted": true})
	sc.rejected(sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "athena-base--half1"}), rules.ReasonOfficialPhoto)
	sc.rejected(sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "nope--u1"}), rules.ReasonNoPhotoRecord)
	sc.rejected(sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "myfox-base--u1", "persona": "athena"}), rules.ReasonNoPhotoRecord)
	sc.officialUntouched()
}

// 刪單張做到一半（檔改名了、紀錄還沒改）被殺：重開後找回那個備份接著做；拿回做到一半（紀錄寫回來了、檔還沒改回原名）也一樣。
func TestRemoveRestorePhotoResume(t *testing.T) {
	sc := foxWithPhotos(t)
	bak := "photos/myfox-base--u5.png.bak-20261101-delete-u6"
	if err := sc.svc.Store.Move("photos/myfox-base--u5.png", bak); err != nil {
		t.Fatal(err)
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "myfox-base--u5"})) // U-6
	if r, _ := sc.v.get(kindPhoto, "myfox-base--u5"); !r.Archived || r.Fields["file"] != bak {
		t.Fatalf("要接著做完：%v", r)
	}
	// 拿回到一半：紀錄寫回來了（file 是原名），檔還是備份。
	r, _ := sc.v.get(kindPhoto, "myfox-base--u5")
	if err := sc.svc.putBack(context.Background(), r, map[string]any{"file": "photos/myfox-base--u5.png"}); err != nil {
		t.Fatal(err)
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "restore_photo", "photo_id": "myfox-base--u5"}))
	if !bytes.Equal(sc.photoFile("myfox-base--u5"), png("full")) {
		t.Fatalf("檔要改回原名：%v", sc.files("photos"))
	}
}

// 刪一套：封存衣服與它所有的照片紀錄（檔不動），請求記下照片編號；拿回：照片紀錄先寫回來、衣服再回來。基本款不能刪。
func TestRemoveAndRestoreOutfit(t *testing.T) {
	sc := newScene(t)
	sc.upload("a.png", png("a"))
	sc.upload("b.png", png("b"))
	sc.done(sc.requestAndRound(map[string]any{"type": "outfit", "upload": "a.png", "persona": "athena", "name": "海邊"}))    // U-1
	sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "b.png", "persona": "athena", "outfit": "my-1"})) // U-2
	del := sc.requestAndRound(map[string]any{"type": "remove_outfit", "persona": "athena", "outfit": "my-1"})              // U-3
	f := sc.done(del)
	if got := rules.Strings(f["photos"]); len(got) != 2 {
		t.Fatalf("請求記下兩張的編號：%v", f)
	}
	for kind, id := range map[string]string{kindOutfit: "athena-my-1", kindPhoto: "athena-my-1--u2"} {
		if r, _ := sc.v.get(kind, id); !r.Archived {
			t.Fatalf("%s/%s 要封存", kind, id)
		}
	}
	if r, _ := sc.v.get(kindPhoto, "athena-my-1--u1"); !r.Archived {
		t.Fatal("照片紀錄一起封存")
	}
	if got := sc.files("photos"); len(got) != 2 {
		t.Fatalf("整套刪檔不動：%v", got)
	}
	// 整套刪掉的照片不能一張一張拿回：要先拿回整套。
	sc.rejected(sc.requestAndRound(map[string]any{"type": "restore_photo", "photo_id": "athena-my-1--u2"}), rules.ReasonRestoreOutfit)
	sc.done(sc.requestAndRound(map[string]any{"type": "restore_outfit", "persona": "athena", "outfit": "my-1"}))
	for kind, id := range map[string]string{kindOutfit: "athena-my-1", kindPhoto: "athena-my-1--u1"} {
		if r, _ := sc.v.get(kind, id); r.Archived {
			t.Fatalf("%s/%s 要拿回來", kind, id)
		}
	}
	sc.photoWant("athena-my-1--u2", map[string]any{"order": 2, "file": "photos/athena-my-1--u2.png"})
	if o := sc.fields(kindOutfit, "athena-my-1"); o["name"] != "海邊" || o["source"] != "mine" {
		t.Fatalf("衣服照舊：%v", o)
	}
	// 基本款、官方的衣服不能刪。
	sc.rejected(sc.requestAndRound(map[string]any{"type": "remove_outfit", "persona": "athena", "outfit": "base"}), rules.ReasonBaseOutfit)
	sc.v.seed(kindOutfit, "athena-sea", map[string]any{"persona": "athena", "outfit": "sea", "name": "官方", "cost": 1, "state": "active", "source": "official"})
	sc.rejected(sc.requestAndRound(map[string]any{"type": "remove_outfit", "persona": "athena", "outfit": "sea"}), rules.ReasonOfficial)
	sc.officialUntouched()
}

// 展示間「不要了」只封存衣服（照片紀錄還在）：刪一套補封存照片；升成官方換了編號的拿不回來。
func TestRemoveOutfitAfterDismissAndPromoted(t *testing.T) {
	sc := newScene(t)
	sc.upload("a.png", png("a"))
	sc.done(sc.requestAndRound(map[string]any{"type": "outfit", "upload": "a.png", "persona": "athena", "name": "海邊", "draft": true})) // U-1
	if err := sc.v.Archive(context.Background(), kindOutfit, "athena-my-1"); err != nil {
		t.Fatal(err)
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "remove_outfit", "persona": "athena", "outfit": "my-1"}))
	if r, _ := sc.v.get(kindPhoto, "athena-my-1--u1"); !r.Archived {
		t.Fatal("補封存照片紀錄")
	}
	sc.v.seed(kindOutfit, "athena-seaside", map[string]any{"persona": "athena", "outfit": "seaside", "name": "海邊", "cost": 1, "state": "active", "source": "official", "from": "athena-my-1"})
	sc.rejected(sc.requestAndRound(map[string]any{"type": "restore_outfit", "persona": "athena", "outfit": "my-1"}), rules.ReasonOutfitPromoted)
}

// 要用點數換：新增時帶 cost（沒寫照 free、預設 1 點）；之後用 set_cost 改，0＝一上傳就擁有。基本款一定免費。
func TestSetCost(t *testing.T) {
	sc := newScene(t)
	sc.upload("a.png", png("a"))
	sc.done(sc.requestAndRound(map[string]any{"type": "outfit", "upload": "a.png", "persona": "athena", "name": "海邊"})) // U-1：預設 1 點
	if o := sc.fields(kindOutfit, "athena-my-1"); o["cost"] != 1 {
		t.Fatalf("預設要 1 點：%v", o)
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "set_cost", "persona": "athena", "outfit": "my-1", "cost": 0}))
	if o := sc.fields(kindOutfit, "athena-my-1"); o["cost"] != 0 {
		t.Fatalf("改成免費：%v", o)
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "set_cost", "persona": "athena", "outfit": "my-1", "cost": 5}))
	if o := sc.fields(kindOutfit, "athena-my-1"); o["cost"] != 5 {
		t.Fatalf("改成 5 點：%v", o)
	}
	sc.rejected(sc.requestAndRound(map[string]any{"type": "set_cost", "persona": "athena", "outfit": "base", "cost": 2}), rules.ReasonBaseCost)
	sc.rejected(sc.requestAndRound(map[string]any{"type": "set_cost", "persona": "athena", "outfit": "my-9", "cost": 2}), rules.ReasonNoOutfit)
	sc.officialUntouched()
}

// 上傳直接生效是預設（衣服 active、照片 adopted = true）；帶 draft = true 才走「設計中」（照片 adopted = false），之後加的照片跟著衣服。
func TestDirectPathIsDefault(t *testing.T) {
	sc := newScene(t)
	sc.upload("a.png", png("a"))
	sc.upload("b.png", png("b"))
	sc.done(sc.requestAndRound(map[string]any{"type": "outfit", "upload": "a.png", "persona": "athena", "name": "直接"}))                // U-1
	sc.done(sc.requestAndRound(map[string]any{"type": "outfit", "upload": "a.png", "persona": "athena", "name": "設計", "draft": true})) // U-2
	sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "b.png", "persona": "athena", "outfit": "my-1"}))             // U-3
	sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "b.png", "persona": "athena", "outfit": "my-2"}))             // U-4
	if o := sc.fields(kindOutfit, "athena-my-1"); o["state"] != "active" {
		t.Fatalf("直接生效：%v", o)
	}
	if o := sc.fields(kindOutfit, "athena-my-2"); o["state"] != "draft" {
		t.Fatalf("設計中：%v", o)
	}
	sc.photoWant("athena-my-1--u1", map[string]any{"adopted": true})
	sc.photoWant("athena-my-1--u3", map[string]any{"adopted": true})
	sc.photoWant("athena-my-2--u2", map[string]any{"adopted": false})
	sc.photoWant("athena-my-2--u4", map[string]any{"adopted": false})
}

// 換圖（CMI-22，作者 2026-11-15「秘書真的換了新圖，那張自動回到還沒採用」）：支援 edit 的家（開發家）換了就是 adopted = false，
// 設計中與已上架的都一樣；一般家（能力讀得到、確定沒有 edit）照衣服的狀態直接生效；能力紀錄還沒有、封存了不當一般家（回到還沒採用）；
// 讀不到能力就不做（請求維持 pending，下一輪再試）。
func TestReplaceGoesBackToUnadopted(t *testing.T) {
	sc := newScene(t)
	ctx := context.Background()
	sc.upload("a.png", png("a"))
	sc.done(sc.requestAndRound(map[string]any{"type": "outfit", "upload": "a.png", "persona": "athena", "name": "直接"}))                // U-1
	sc.done(sc.requestAndRound(map[string]any{"type": "outfit", "upload": "a.png", "persona": "athena", "name": "設計", "draft": true})) // U-2
	adopt := func(pid string) {
		t.Helper()
		if err := sc.v.Patch(ctx, kindPhoto, pid, map[string]any{"adopted": true}); err != nil { // 「修改」分頁按了採用
			t.Fatal(err)
		}
	}
	replace := func(outfit, pid, payload string) string {
		t.Helper()
		sc.upload(payload+".png", png(payload))
		id := sc.requestAndRound(map[string]any{"type": "photo", "upload": payload + ".png", "persona": "athena", "outfit": outfit, "photo_id": pid})
		return id
	}

	// 一般家：已上架的直接生效、設計中的照舊要採用。
	sc.v.seed(kindCaps, "main", map[string]any{"supports": []any{"mine"}})
	sc.done(replace("my-1", "athena-my-1--u1", "b"))
	sc.photoWant("athena-my-1--u1", map[string]any{"adopted": true})
	adopt("athena-my-2--u2")
	sc.done(replace("my-2", "athena-my-2--u2", "c"))
	sc.photoWant("athena-my-2--u2", map[string]any{"adopted": false})

	// 開發家：已上架的換了也回到還沒採用。
	sc.v.seed(kindCaps, "main", map[string]any{"supports": []any{"mine", "edit"}})
	sc.done(replace("my-1", "athena-my-1--u1", "d"))
	sc.photoWant("athena-my-1--u1", map[string]any{"adopted": false})
	if !bytes.Equal(sc.photoFile("athena-my-1--u1"), png("d")) {
		t.Fatal("換成新圖")
	}
	// 跟現在一模一樣的圖再送一次：檔不動，採用照樣回到還沒採用。
	adopt("athena-my-1--u1")
	sc.done(replace("my-1", "athena-my-1--u1", "d"))
	sc.photoWant("athena-my-1--u1", map[string]any{"adopted": false})

	// 能力紀錄封存了、還沒有：不知道是哪種家，不當一般家。
	adopt("athena-my-1--u1")
	if err := sc.v.Archive(ctx, kindCaps, "main"); err != nil {
		t.Fatal(err)
	}
	sc.done(replace("my-1", "athena-my-1--u1", "e"))
	sc.photoWant("athena-my-1--u1", map[string]any{"adopted": false})
	adopt("athena-my-1--u1")
	sc.v.drop(kindCaps, "main")
	sc.done(replace("my-1", "athena-my-1--u1", "f"))
	sc.photoWant("athena-my-1--u1", map[string]any{"adopted": false})

	// 讀不到能力：不做，請求維持 pending、檔不動。
	adopt("athena-my-1--u1")
	sc.v.getErr = func(kind, _ string) error {
		if kind == kindCaps {
			return &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "暫時讀不到"}
		}
		return nil
	}
	id := replace("my-1", "athena-my-1--u1", "g")
	if f := sc.fields(KindRequest, id); f["state"] != "pending" {
		t.Fatalf("讀不到能力要維持 pending：%v", f)
	}
	sc.photoWant("athena-my-1--u1", map[string]any{"adopted": true})
	if !bytes.Equal(sc.photoFile("athena-my-1--u1"), png("f")) {
		t.Fatal("讀不到能力時不能換檔")
	}
	sc.v.getErr = nil
	sc.v.seed(kindCaps, "main", map[string]any{"supports": []any{"mine"}})
	sc.round()
	sc.done(id)
	sc.photoWant("athena-my-1--u1", map[string]any{"adopted": true})
	sc.officialUntouched()
}

// 換官方的照片（CMI-25）：只有開發家（核心說是、衣櫃的能力也有 edit）、官方角色的位置就在家裡時才換——新檔寫在官方位置
// avatars/photos/<照片編號>.<ext>（角色本身的頭像還是 avatars/<角色>.<ext>）、舊檔改名成備份留在原處、紀錄 adopted = false；
// 一般家（含被寫了含 edit 的能力紀錄的一般家）、能力還沒有都拒，問不到核心維持 pending；官方位置在家外面（跟著角色包）時拒並說原因；
// 不能從這裡替官方的照片多配一支動畫、也不能換錯種類；隱藏版的、檔不在 avatars/ 底下的拒並說原因（不會卡在 pending）。
func TestReplaceOfficialPhoto(t *testing.T) {
	sc := newScene(t)
	sc.write("personas/avatars/athena@base~2.png", png("official-2"))
	sc.write("personas/adult/athena@base~3.png", png("official-3"))
	sc.write("personas/extra/athena@base~4.png", png("official-4"))
	sc.v.seed(kindPhoto, "athena-base--half1", map[string]any{"persona": "athena", "outfit": "athena-base", "type": "half", "order": 1,
		"hidden": false, "source": "official", "file": "avatars/athena.png", "adopted": true})
	sc.v.seed(kindPhoto, "athena-base--half2", map[string]any{"persona": "athena", "outfit": "athena-base", "type": "half", "order": 2,
		"hidden": false, "source": "official", "file": "avatars/athena@base~2.png", "adopted": true})
	sc.v.seed(kindPhoto, "athena-base--half3-h", map[string]any{"persona": "athena", "outfit": "athena-base", "type": "half", "order": 1,
		"hidden": true, "source": "official", "file": "adult/athena@base~3.png", "adopted": true})
	sc.v.seed(kindPhoto, "athena-base--half4", map[string]any{"persona": "athena", "outfit": "athena-base", "type": "half", "order": 4,
		"hidden": false, "source": "official", "file": "extra/athena@base~4.png", "adopted": true})
	sc.upload("n.png", png("new-2"))
	req := func(f map[string]any) string {
		g := map[string]any{"type": "photo", "upload": "n.png", "persona": "athena", "outfit": "base"}
		for k, v := range f {
			g[k] = v
		}
		return sc.requestAndRound(g)
	}
	untouched := func() {
		t.Helper()
		if !bytes.Equal(sc.read("personas/avatars/athena@base~2.png"), png("official-2")) {
			t.Fatal("被拒時官方的圖不能動")
		}
		sc.photoWant("athena-base--half2", map[string]any{"file": "avatars/athena@base~2.png", "adopted": true})
	}

	sc.rejected(req(map[string]any{"photo_id": "athena-base--half2"}), rules.ReasonOfficialReplace) // 沒接核心＝不是開發家
	untouched()
	dev, devErr := false, error(nil)
	sc.svc.Dev = func(context.Context) (bool, error) { return dev, devErr }
	sc.v.seed(kindCaps, "main", map[string]any{"supports": []any{"mine", "edit"}})
	sc.rejected(req(map[string]any{"photo_id": "athena-base--half2"}), rules.ReasonOfficialReplace) // 一般家被寫了含 edit 的能力紀錄
	untouched()
	devErr = errors.New("核心沒回")
	waiting := req(map[string]any{"photo_id": "athena-base--half2"})
	if f := sc.fields(KindRequest, waiting); f["state"] != "pending" {
		t.Fatalf("問不到核心要維持 pending：%v", f)
	}
	untouched()
	sc.v.drop(KindRequest, waiting)
	dev, devErr = true, nil
	sc.v.drop(kindCaps, "main")
	sc.rejected(req(map[string]any{"photo_id": "athena-base--half2"}), rules.ReasonOfficialReplace) // 還不知道是哪種家
	untouched()
	sc.v.seed(kindCaps, "main", map[string]any{"supports": []any{"mine"}})
	sc.rejected(req(map[string]any{"photo_id": "athena-base--half2"}), rules.ReasonOfficialReplace) // 能力說一般家
	untouched()
	sc.v.seed(kindCaps, "main", map[string]any{"supports": []any{"mine", "edit"}})
	// 隱藏版的不換（換到 avatars/photos/ 會分不出是不是自己的隱藏版）；檔不在 avatars/ 底下的換不了：都直接拒、說原因。
	sc.rejected(req(map[string]any{"photo_id": "athena-base--half3-h"}), rules.ReasonOfficialHidden)
	sc.rejected(req(map[string]any{"photo_id": "athena-base--half3-h", "adult": false}), rules.ReasonPhotoHiddenDiff)
	sc.rejected(req(map[string]any{"photo_id": "athena-base--half4"}), rules.ReasonOfficialFile)
	if !bytes.Equal(sc.read("personas/adult/athena@base~3.png"), png("official-3")) || !bytes.Equal(sc.read("personas/extra/athena@base~4.png"), png("official-4")) {
		t.Fatal("被拒時官方的圖不能動")
	}
	sc.rejected(req(map[string]any{"type": "full", "photo_id": "athena-base--half2"}), rules.ReasonPhotoWrongType)
	sc.rejected(req(map[string]any{"type": "video", "upload": "v.mp4", "photo_id": "athena-base--half2"}), rules.ReasonNoUpload)
	sc.upload("v.mp4", mp4("v"))
	sc.rejected(req(map[string]any{"type": "video", "upload": "v.mp4", "photo_id": "athena-base--half2"}), rules.ReasonOfficial)
	sc.rejected(req(map[string]any{"photo_id": "athena-base--half2", "adult": true}), rules.ReasonAdultOff)
	untouched()

	// 官方角色的位置在家外面（第 2 版、跟著角色包）：寫不進去，說原因。
	t.Setenv(sdk.EnvOfficialPersonas, t.TempDir())
	sc.rejected(req(map[string]any{"photo_id": "athena-base--half2"}), rules.ReasonOfficialLayer)
	untouched()
	t.Setenv(sdk.EnvOfficialPersonas, "")

	id := req(map[string]any{"photo_id": "athena-base--half2"})
	sc.done(id)
	n, _ := rules.SeqOf(id)
	sc.photoWant("athena-base--half2", map[string]any{"file": "avatars/photos/athena-base--half2.png", "adopted": false, "source": "official"})
	if !bytes.Equal(sc.read("personas/avatars/photos/athena-base--half2.png"), png("new-2")) {
		t.Fatal("新檔寫在官方位置的 avatars/photos/")
	}
	bak := store.BackupName("avatars/athena@base~2.png", sc.svc.Now(), fmt.Sprintf("replace-u%d", n))
	if !bytes.Equal(sc.read("personas/"+bak), png("official-2")) || sc.read("personas/avatars/athena@base~2.png") != nil {
		t.Fatalf("舊檔改名成備份留在原處：%s", bak)
	}
	// 角色本身的頭像：換了還是放 avatars/<角色>.<ext>（頭像不能因為換圖不見）。
	sc.upload("a2.png", png("new-avatar"))
	sc.done(req(map[string]any{"upload": "a2.png", "photo_id": "athena-base--half1"}))
	sc.photoWant("athena-base--half1", map[string]any{"file": "avatars/athena.png", "adopted": false})
	if !bytes.Equal(sc.read("personas/avatars/athena.png"), png("new-avatar")) {
		t.Fatal("頭像換成新圖")
	}
	if got := sc.read("personas/athena.md"); string(got) != "---\nname: Athena\n---\n" {
		t.Fatalf("官方角色卡不能動：%q", got)
	}
	if len(sc.files("photos")) != 0 {
		t.Fatalf("官方的照片不寫進 personas/mine/：%v", sc.files("photos"))
	}
}

// 拿回整套（restore_outfit）也收下架的我的衣服（沒封存、state = retired）：一般家沒有上架／下架，「衣服」分頁把它列在「刪掉的」，
// 拿回＝改回上架（CMI-24）；照片不動。已經上架的再送一次算做完。（S5c 的 publish 2026-11-15 拿掉，CMI-23 作廢。）
func TestRestoreRetiredOutfit(t *testing.T) {
	sc := newScene(t)
	sc.upload("a.png", png("a"))
	sc.done(sc.requestAndRound(map[string]any{"type": "outfit", "upload": "a.png", "persona": "athena", "name": "海邊"})) // U-1
	ctx := context.Background()
	if err := sc.v.Patch(ctx, kindOutfit, "athena-my-1", map[string]any{"state": "retired"}); err != nil {
		t.Fatal(err)
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "restore_outfit", "persona": "athena", "outfit": "my-1"}))
	if o := sc.fields(kindOutfit, "athena-my-1"); o["state"] != "active" {
		t.Fatalf("改回上架：%v", o)
	}
	sc.photoWant("athena-my-1--u1", map[string]any{"adopted": true})
	sc.done(sc.requestAndRound(map[string]any{"type": "restore_outfit", "persona": "athena", "outfit": "my-1"}))
	if f := sc.fields(KindRequest, sc.requestAndRound(map[string]any{"type": "publish", "persona": "athena", "outfit": "my-1"})); f["state"] != "rejected" {
		t.Fatalf("publish 拿掉了，要被拒：%v", f)
	}
	sc.officialUntouched()
}

// 衣櫃的轉換器每一輪替已經有的衣服補登記「沒有紀錄指著」的 avatars/<角色>.<ext>：新角色要先寫照片紀錄、再寫衣服，
// 換頭像要先讓紀錄指到新檔、再寫新檔，轉換器才不會多登記一筆；換了副檔名而新名字已經有別的檔時，什麼都不動就拒。
func TestAvatarNeverUnreferencedForConverter(t *testing.T) {
	sc := newScene(t)
	sc.upload("b.png", png("base"))
	sc.v.beforePut = func(rec sdk.Record) {
		if rec.Kind == kindOutfit && rec.ID == "myfox-base" {
			if _, ok := sc.v.recs[kindPhoto]["myfox-base--u1"]; !ok {
				t.Error("衣服寫出來之前，頭像要已經有照片紀錄指著")
			}
		}
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "character", "upload": "b.png", "name": "狐", "persona": "myfox"})) // U-1
	sc.v.beforePut = nil

	sc.v.beforePatch = func(kind, id string, set map[string]any) {
		if kind == kindPhoto && id == "myfox-base--u1" && set["file"] != nil {
			if sc.read("personas/mine/avatars/myfox.jpg") != nil || sc.read("personas/mine/avatars/myfox.png") != nil {
				t.Error("紀錄改指新檔時，舊檔要已經改名成備份、新檔還沒寫")
			}
		}
	}
	sc.upload("n.jpg", jpeg("new-face"))
	sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "n.jpg", "persona": "myfox", "outfit": "base", "photo_id": "myfox-base--u1"})) // U-2
	sc.v.beforePatch = nil
	sc.photoWant("myfox-base--u1", map[string]any{"file": "avatars/myfox.jpg"})

	// 換回 png，但 avatars/myfox.png 已經有別的檔：拒，舊檔、紀錄都不動。
	sc.write("personas/mine/avatars/myfox.png", png("someone-else"))
	sc.upload("m.png", png("mine-again"))
	sc.rejected(sc.requestAndRound(map[string]any{"type": "photo", "upload": "m.png", "persona": "myfox", "outfit": "base", "photo_id": "myfox-base--u1"}), rules.ReasonFileTaken) // U-3
	sc.photoWant("myfox-base--u1", map[string]any{"file": "avatars/myfox.jpg"})
	if !bytes.Equal(sc.read("personas/mine/avatars/myfox.jpg"), jpeg("new-face")) || !bytes.Equal(sc.read("personas/mine/avatars/myfox.png"), png("someone-else")) {
		t.Fatalf("被拒時檔都不動：%v", sc.files("avatars"))
	}
}

// 角色本身的頭像（新角色的第一張，avatars/<代號>.<ext>）換圖、刪掉、拿回都留在頭像的路徑：複製角色進套件只認那裡（DEV-160 S3）。
// S3 以前的角色第一支動畫是 avatars/<代號>.mp4，換了也一樣留在那裡；其他張照舊放 photos/。
func TestCharacterAvatarStaysAtAvatarPath(t *testing.T) {
	sc := foxWithPhotos(t)
	sc.upload("n.jpg", jpeg("new-face"))
	sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "n.jpg", "persona": "myfox", "outfit": "base", "photo_id": "myfox-base--u1"})) // U-6
	sc.photoWant("myfox-base--u1", map[string]any{"file": "avatars/myfox.jpg", "order": 1})
	if !bytes.Equal(sc.read("personas/mine/avatars/myfox.jpg"), jpeg("new-face")) || sc.read("personas/mine/avatars/myfox.png") != nil {
		t.Fatalf("新頭像在 avatars/myfox.jpg、舊的改名成備份：%v", sc.files("avatars"))
	}
	if bak := sc.backupOf("avatars/myfox.png", "replace-u6"); !bytes.Equal(sc.read("personas/mine/"+bak), png("base")) {
		t.Fatal("舊頭像留在備份")
	}
	if got := sc.files("photos"); len(got) != 4 {
		t.Fatalf("其他張不動：%v", got)
	}

	sc.done(sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "myfox-base--u1"})) // U-7
	if sc.read("personas/mine/avatars/myfox.jpg") != nil || sc.backupOf("avatars/myfox.jpg", "delete-u7") == "" {
		t.Fatalf("刪掉：頭像改名成備份：%v", sc.files("avatars"))
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "restore_photo", "photo_id": "myfox-base--u1"})) // U-8
	sc.photoWant("myfox-base--u1", map[string]any{"file": "avatars/myfox.jpg"})
	if !bytes.Equal(sc.read("personas/mine/avatars/myfox.jpg"), jpeg("new-face")) {
		t.Fatal("拿回：頭像改回原名")
	}

	// S3 以前的角色：第一張 avatars/myold.png、第一支動畫 avatars/myold.mp4（轉換器登記的紀錄）。
	sc.write("personas/mine/myold.md", []byte(rules.CharacterCard("舊", "")))
	sc.write("personas/mine/avatars/myold.png", png("old-face"))
	sc.write("personas/mine/avatars/myold.mp4", mp4("old-clip"))
	sc.v.seed(kindOutfit, "myold-base", map[string]any{"persona": "myold", "outfit": "base", "name": "基本款", "cost": 0, "state": "active", "source": "mine"})
	sc.v.seed(kindPhoto, "myold-base--half1", map[string]any{"persona": "myold", "outfit": "myold-base", "type": "half", "order": 1,
		"hidden": false, "source": "mine", "file": "avatars/myold.png", "adopted": true})
	sc.v.seed(kindPhoto, "myold-base--half1-v1", map[string]any{"persona": "myold", "outfit": "myold-base", "type": "video", "of": "myold-base--half1", "clip": 1,
		"hidden": false, "source": "mine", "file": "avatars/myold.mp4", "adopted": true})
	sc.upload("n.mp4", mp4("new-clip"))
	sc.done(sc.requestAndRound(map[string]any{"type": "video", "upload": "n.mp4", "persona": "myold", "outfit": "base", "photo_id": "myold-base--half1-v1"})) // U-9
	sc.photoWant("myold-base--half1-v1", map[string]any{"file": "avatars/myold.mp4"})
	if !bytes.Equal(sc.read("personas/mine/avatars/myold.mp4"), mp4("new-clip")) || sc.backupOf("avatars/myold.mp4", "replace-u9") == "" {
		t.Fatal("舊角色的動畫換了還在 avatars/myold.mp4、舊的留在備份")
	}
}

// 拿回單獨刪的一支動畫時，那張已經放滿 6 支就拒（不排成第 7 支）。
func TestRestoreVideoWhenClipsFull(t *testing.T) {
	sc := foxWithPhotos(t)
	sc.done(sc.requestAndRound(map[string]any{"type": "remove_photo", "photo_id": "myfox-base--u4"})) // U-6：刪第 2 支
	for clip := 2; clip <= rules.MaxClips; clip++ {                                                   // U-7～U-11：補滿 6 支
		sc.done(sc.requestAndRound(map[string]any{"type": "video", "upload": "v1.mp4", "persona": "myfox", "outfit": "base", "photo": 2, "clip": clip}))
	}
	sc.rejected(sc.requestAndRound(map[string]any{"type": "restore_photo", "photo_id": "myfox-base--u4"}), rules.ReasonNoClip(rules.MaxClips+1))
	if r, _ := sc.v.get(kindPhoto, "myfox-base--u4"); !r.Archived {
		t.Fatal("拒絕時不動")
	}
}

// 換圖做到一半被殺也接得上（CMI-7）：舊檔改名成備份了、新檔還沒寫；換了副檔名、新檔寫了、紀錄還沒改。
func TestReplacePhotoResume(t *testing.T) {
	sc := foxWithPhotos(t)
	sc.upload("n2.png", png("new-2"))
	bak := store.BackupName("photos/myfox-base--u2.png", sc.svc.Now(), "replace-u6")
	if err := sc.svc.Store.Move("photos/myfox-base--u2.png", bak); err != nil {
		t.Fatal(err)
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "n2.png", "persona": "myfox", "outfit": "base", "photo_id": "myfox-base--u2"})) // U-6
	sc.photoWant("myfox-base--u2", map[string]any{"file": "photos/myfox-base--u2.png"})
	if !bytes.Equal(sc.photoFile("myfox-base--u2"), png("new-2")) || !bytes.Equal(sc.read("personas/mine/"+bak), png("p2")) {
		t.Fatalf("新檔寫好、舊檔留在備份：%v", sc.files("photos"))
	}

	webp := append([]byte("RIFF\x00\x00\x00\x00WEBP"), "full-2"...)
	sc.upload("f2.webp", webp)
	if err := sc.svc.Store.Move("photos/myfox-base--u5.png", store.BackupName("photos/myfox-base--u5.png", sc.svc.Now(), "replace-u7")); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.svc.Store.PutNew("photos/myfox-base--u5.webp", webp); err != nil {
		t.Fatal(err)
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "full", "upload": "f2.webp", "persona": "myfox", "outfit": "base", "photo_id": "myfox-base--u5"})) // U-7
	sc.photoWant("myfox-base--u5", map[string]any{"file": "photos/myfox-base--u5.webp"})
	if sc.backupOf("photos/myfox-base--u5.png", "replace-u7") == "" || !bytes.Equal(sc.photoFile("myfox-base--u5"), webp) {
		t.Fatalf("換副檔名接著做完：%v", sc.files("photos"))
	}
}

// 刪一套、拿回一套做到一半被殺也接得上（CMI-7）：刪——請求記了照片、衣服封存了、照片只封存了一張；拿回——照片寫回了一張、衣服還沒回來。
func TestRemoveRestoreOutfitResume(t *testing.T) {
	sc := newScene(t)
	ctx := context.Background()
	sc.upload("a.png", png("a"))
	sc.upload("b.png", png("b"))
	sc.done(sc.requestAndRound(map[string]any{"type": "outfit", "upload": "a.png", "persona": "athena", "name": "海邊"}))    // U-1
	sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "b.png", "persona": "athena", "outfit": "my-1"})) // U-2
	del := sc.request(map[string]any{"type": "remove_outfit", "persona": "athena", "outfit": "my-1",
		"photos": []any{"athena-my-1--u1", "athena-my-1--u2"}}) // U-3：上次記好照片了
	if err := sc.v.Archive(ctx, kindOutfit, "athena-my-1"); err != nil {
		t.Fatal(err)
	}
	if err := sc.v.Archive(ctx, kindPhoto, "athena-my-1--u1"); err != nil {
		t.Fatal(err)
	}
	sc.round()
	sc.done(del)
	if r, _ := sc.v.get(kindPhoto, "athena-my-1--u2"); !r.Archived {
		t.Fatal("剩下的照片要接著封存")
	}
	r, _ := sc.v.get(kindPhoto, "athena-my-1--u2")
	if err := sc.svc.putBack(ctx, r, nil); err != nil {
		t.Fatal(err)
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "restore_outfit", "persona": "athena", "outfit": "my-1"}))
	for kind, ids := range map[string][]string{kindOutfit: {"athena-my-1"}, kindPhoto: {"athena-my-1--u1", "athena-my-1--u2"}} {
		for _, id := range ids {
			if r, _ := sc.v.get(kind, id); r.Archived {
				t.Fatalf("%s/%s 要拿回來", kind, id)
			}
		}
	}
}
