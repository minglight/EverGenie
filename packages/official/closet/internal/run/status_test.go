package run

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/minglight/evergenie/packages/official/closet/internal/pub"
)

// 每張照片的狀態（CLO-21）：擁有的衣服列出第 1..N 張在正常版與每個隱藏版有沒有圖、幾支動畫，以及全身照；
// 隱藏版關著（不管開不開）也照列，好讓主人知道缺哪張；沒擁有的衣服沒有 status；自己的隱藏版開關關著時不列自己的隱藏版。
func TestPersonViewListsPhotoStatus(t *testing.T) {
	sc, _ := variantScene(t)
	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": false})
	sc.round()

	hidden := sc.fields(KindPerson, "builder")["hidden"].([]any)
	if len(hidden) != 1 || hidden[0].(map[string]any)["id"] != "h" || hidden[0].(map[string]any)["name"] != "隱藏版" {
		t.Fatalf("hidden：%v", hidden)
	}
	st, ok := sc.outfitView("base")["status"].(map[string]any)
	if !ok {
		t.Fatal("擁有的衣服要有 status")
	}
	photos := st["photos"].([]any)
	if len(photos) != 2 {
		t.Fatalf("隱藏版多一張第 2 張，要列到第 2 張：%v", photos)
	}
	p1 := photos[0].(map[string]any)
	if p1["std"].(map[string]any)["img"] != true || p1["hidden"].(map[string]any)["h"].(map[string]any)["videos"] != 1 {
		t.Fatalf("第 1 張：%v", p1)
	}
	p2 := photos[1].(map[string]any)
	if p2["std"].(map[string]any)["img"] != false || p2["hidden"].(map[string]any)["h"].(map[string]any)["img"] != true {
		t.Fatalf("第 2 張正常版缺、隱藏版有：%v", p2)
	}
	full := st["full"].(map[string]any)
	if full["std"] != false || full["hidden"].(map[string]any)["h"] != true {
		t.Fatalf("全身照：%v", full)
	}
	if _, has := sc.outfitView("night")["status"]; has {
		t.Fatal("沒擁有的衣服不能有 status（不透露張數）")
	}
}

func TestPersonViewStatusSkipsAdultWhenSwitchOff(t *testing.T) {
	sc, _ := adultScene(t)
	sc.svc.Adult = func(context.Context) bool { return false }
	sc.round()
	if hidden := sc.fields(KindPerson, "builder")["hidden"].([]any); len(hidden) != 0 {
		t.Fatalf("自己的隱藏版關著不列：%v", hidden)
	}
	st := sc.outfitView("base")["status"].(map[string]any)
	if h := st["photos"].([]any)[0].(map[string]any)["hidden"].(map[string]any); len(h) != 0 {
		t.Fatalf("自己的隱藏版關著，照片狀態也不列它：%v", h)
	}
}

// 設計中每張圖各自採用（SEC-10）：draft 的衣服把每張照片紀錄的 adopted 帶進 person（DEV-160 S2 起讀 closet.photo；
// 舊草稿 closet.outfit 的 adopted 由轉換器搬到照片紀錄）；沒有就是空 list；每張照片多 n（第幾張）讓頁面組出「哪一張」；
// 正式的衣服不帶 adopted。
func TestPersonViewListsAdoptedPiecesOfDraft(t *testing.T) {
	sc := newScene(t)
	avatars := filepath.Join(sc.home, "personas", "avatars")
	for _, f := range []string{"builder@wip.png", "builder@wip~2.png", "full/builder@wip.png", "builder@old.png"} {
		pngFile(t, filepath.Join(avatars, filepath.FromSlash(f)), 90)
	}
	sc.v.seed(KindOutfit, "builder-wip", map[string]any{"persona": "builder", "outfit": "wip", "name": "WIP", "cost": 1, "state": "draft", "source": "official", "order": 4, "adopted": []any{"half-1", "full"}})
	sc.v.seed(KindOutfit, "builder-old", map[string]any{"persona": "builder", "outfit": "old", "name": "OLD", "cost": 1, "state": "draft", "source": "official", "order": 5})
	sc.round()

	wip := sc.outfitView("wip")
	adopted, ok := wip["adopted"].([]any)
	if !ok || len(adopted) != 2 || adopted[0] != "half-1" || adopted[1] != "full" {
		t.Fatalf("設計中要帶 adopted（舊草稿的 adopted 轉到照片紀錄）：%v", wip["adopted"])
	}
	if old := sc.outfitView("old"); old["adopted"] == nil || len(old["adopted"].([]any)) != 0 {
		t.Fatalf("舊草稿（沒寫 adopted）要是空 list：%v", old["adopted"])
	}
	if _, has := sc.outfitView("base")["adopted"]; has {
		t.Fatal("正式的衣服不帶 adopted")
	}
	// 頁面採用第 2 張：寫照片紀錄的 adopted，不寫 closet.outfit 的。
	if err := sc.v.Patch(context.Background(), KindPhoto, "builder-wip--half2", map[string]any{"adopted": true}); err != nil {
		t.Fatal(err)
	}
	sc.round()
	if got := sc.outfitView("wip")["adopted"].([]any); len(got) != 3 || got[1] != "half-2" {
		t.Fatalf("照片紀錄採用了第 2 張：%v", got)
	}
}

// 草稿的圖在 person 裡帶 n（第幾張）：擁有的與設計中的照片才有。
func TestPersonViewPhotosCarryNumber(t *testing.T) {
	sc, _ := variantScene(t)
	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": true})
	sc.round()
	photos := sc.outfitView("base")["photos"].([]any)
	// 一般版 1 張＋隱藏版 2 張都列（2026-11-13 作者：兩版都顯示）；n 是各自那一版的第幾張，variant 分辨是哪一版。
	if len(photos) != 3 {
		t.Fatalf("隱藏版開著有 3 張：%v", photos)
	}
	want := []struct {
		n       int
		variant string
	}{{1, ""}, {1, "h"}, {2, "h"}}
	for i, p := range photos {
		m := p.(map[string]any)
		if m["n"] != want[i].n || m["variant"] != want[i].variant {
			t.Fatalf("第 %d 項：%v", i+1, m)
		}
	}
	// 每張帶照片紀錄的編號（頁面的採用、修改、留言寫那一筆，DEV-160 S2）。
	if m := photos[1].(map[string]any); m["id"] != "builder-base--half1-h" || len(m["video_ids"].([]any)) != 1 || m["video_ids"].([]any)[0] != "builder-base--video1-c1-h" {
		t.Fatalf("隱藏版第 1 張的編號：%v", m)
	}
	if base := sc.outfitView("base"); base["full_hidden_id"] != "builder-base--full1-h" {
		t.Fatalf("隱藏版全身照的編號：%v", base)
	}
}

// 照片編號 → 公開網址（closet.person 的 photo_urls，DEV-160 CLO-35）：「修改」分頁照編號找圖，不照位置猜。
// 列這一輪真的公開的每張半身、每支影片、全身照（隱藏版是 h-<n>、帶 ?v=h）；沒擁有的衣服（只有剪影）不列。
// 改了一般／隱藏版或類型，下一輪同一個編號就對到新的公開檔，不會變成對不到。
func TestPersonViewMapsPhotoIDsToPublicURLs(t *testing.T) {
	sc, _ := variantScene(t)
	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": true})
	sc.round()
	urls := func() map[string]any {
		t.Helper()
		m, ok := sc.person()["photo_urls"].(map[string]any)
		if !ok {
			t.Fatalf("closet.person 要有 photo_urls：%v", sc.person())
		}
		return m
	}
	got := urls()
	base := sc.outfitView("base")
	photos := base["photos"].([]any)
	for _, x := range photos {
		m := x.(map[string]any)
		if got[m["id"].(string)] != m["url"] {
			t.Fatalf("%v 的網址要跟 photos 那張一樣：%v", m["id"], got)
		}
	}
	if got["builder-base--half1-h"] != pub.URLPrefix+"o/builder/base/h-1.png?v=h" {
		t.Fatalf("隱藏版第 1 張照 h-<n>：%v", got)
	}
	if got["builder-base--video1-c1-h"] != pub.URLPrefix+"o/builder/base/h-1.mp4?v=h" {
		t.Fatalf("隱藏版影片：%v", got)
	}
	if got["builder-base--full1-h"] != base["full_hidden"] || base["full_hidden"] == "" {
		t.Fatalf("隱藏版全身照：%v／%v", got, base["full_hidden"])
	}
	for id := range got {
		if strings.HasPrefix(id, "builder-night--") {
			t.Fatalf("沒擁有的衣服只有剪影，不列照片編號：%v", got)
		}
	}

	// 隱藏版第 2 張改成一般版：下一輪同一個編號對到一般版的第 2 張。
	ctx := context.Background()
	if err := sc.v.Patch(ctx, KindPhoto, "builder-base--half2-h", map[string]any{"hidden": false}); err != nil {
		t.Fatal(err)
	}
	sc.round()
	if got := urls()["builder-base--half2-h"]; got != pub.URLPrefix+"o/builder/base/2.png" {
		t.Fatalf("改成一般版之後：%v", got)
	}
	// 再改成全身照（一般版還沒有全身照）：對到一般版的全身照。
	if err := sc.v.Patch(ctx, KindPhoto, "builder-base--half2-h", map[string]any{"type": "full", "order": 1}); err != nil {
		t.Fatal(err)
	}
	sc.round()
	if got := urls()["builder-base--half2-h"]; got != pub.URLPrefix+"f/builder/base/1.png" || sc.outfitView("base")["full"] != got {
		t.Fatalf("改成全身照之後：%v", got)
	}
}

// owner_hidden：自己的隱藏版（放在 mine 的，或舊家放在官方位置、路徑看起來是自己的隱藏版的）不管開關開沒開都列，
// 「修改」分頁照它不給改成一般版（CLO-18、CLO-35）；官方的隱藏版與一般版不列。
func TestPersonViewListsOwnerHidden(t *testing.T) {
	sc := newScene(t)
	put := func(id string, f map[string]any) {
		base := map[string]any{"persona": "builder", "outfit": "builder-base", "type": "half", "order": 9, "adopted": true}
		for k, v := range f {
			base[k] = v
		}
		sc.v.seed(KindPhoto, id, base)
	}
	put("builder-base--x-mine-h", map[string]any{"hidden": true, "source": "mine", "file": "avatars/builder@x.png"})
	put("builder-base--x-adult-h", map[string]any{"hidden": true, "source": "official", "file": "avatars/level/builder@x.png"})
	put("builder-base--x-official-h", map[string]any{"hidden": true, "source": "official", "file": "extra/set/builder@x.png"})
	put("builder-base--x-mine", map[string]any{"hidden": false, "source": "mine", "file": "avatars/builder@y.png"})
	sc.round()
	got, _ := sc.person()["owner_hidden"].([]any)
	if want := []any{"builder-base--x-adult-h", "builder-base--x-mine-h"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("owner_hidden 要是 %v：%v", want, got)
	}
}
