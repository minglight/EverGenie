package run

import (
	"bytes"
	"context"
	"testing"

	"github.com/minglight/evergenie/packages/official/closet-mine/internal/rules"
)

// 自己的隱藏版的家設定沒打開：放自己的隱藏版圖的請求一律拒絕並說原因，不會有檔、也不建隱藏版。
func TestAdultUploadRejectedWhenSwitchOff(t *testing.T) {
	for name, adult := range map[string]func(context.Context) bool{
		"沒接開關": nil,
		"開關關著": func(context.Context) bool { return false },
	} {
		t.Run(name, func(t *testing.T) {
			sc := newScene(t)
			sc.svc.Adult = adult
			sc.upload("a.png", png("adult-1"))
			id := sc.request(map[string]any{"type": "photo", "upload": "/share/a.png", "persona": "athena", "outfit": "base", "adult": true})
			sc.round()
			if f := sc.fields(KindRequest, id); f["state"] != "rejected" || f["reason"] != rules.ReasonAdultOff {
				t.Fatalf("請求：%v", f)
			}
			if sc.files("photos") != nil || sc.files("adult") != nil {
				t.Fatal("沒打開不能寫自己的隱藏版的圖")
			}
			if _, ok := sc.v.get(kindVariant, adultVariantID); ok {
				t.Fatal("沒打開不建自己的隱藏版")
			}
		})
	}
}

// 打開：官方衣服也能放自己的隱藏版（檔一樣放 personas/mine/photos/、紀錄 hidden = true、source = mine，不碰官方圖）；
// 第幾張、第幾支照隱藏版自己的照片算；第一次放時建 closet.variant/adult（dir = mine/adult、有開關、預設關著）。
func TestAdultUploadWritesHiddenRecordsAndVariant(t *testing.T) {
	sc := newScene(t)
	sc.svc.Adult = func(context.Context) bool { return true }
	sc.upload("a1.png", png("adult-1"))
	sc.upload("a2.png", png("adult-2"))
	sc.upload("af.png", png("adult-full"))
	sc.upload("av.mp4", mp4("adult-video"))
	ids := []string{
		sc.request(map[string]any{"type": "photo", "upload": "a1.png", "persona": "athena", "outfit": "base", "adult": true}),
		sc.request(map[string]any{"type": "photo", "upload": "a2.png", "persona": "athena", "outfit": "base", "adult": true}),
		sc.request(map[string]any{"type": "full", "upload": "af.png", "persona": "athena", "outfit": "base", "adult": true}),
		sc.request(map[string]any{"type": "video", "upload": "av.mp4", "persona": "athena", "outfit": "base", "photo": 2, "adult": true}),
	}
	sc.round()
	for _, id := range ids {
		if f := sc.fields(KindRequest, id); f["state"] != "done" {
			t.Fatalf("%s：%v", id, f)
		}
	}
	for pid, want := range map[string]map[string]any{
		"athena-base--u1": {"type": "half", "order": 1, "hidden": true, "source": "mine", "file": "photos/athena-base--u1.png"},
		"athena-base--u2": {"type": "half", "order": 2, "hidden": true},
		"athena-base--u3": {"type": "full", "hidden": true, "file": "photos/athena-base--u3.png"},
		"athena-base--u4": {"type": "video", "of": "athena-base--u2", "clip": 1, "hidden": true, "file": "photos/athena-base--u4.mp4"},
	} {
		sc.photoWant(pid, want)
	}
	if !bytes.Equal(sc.photoFile("athena-base--u4"), mp4("adult-video")) || !bytes.Equal(sc.read("personas/avatars/athena.png"), png("official-athena")) {
		t.Fatal("檔的內容不對，或官方圖被動了")
	}
	if sc.files("avatars") != nil || sc.files("adult") != nil {
		t.Fatal("新上傳不寫 avatars/、adult/")
	}
	v := sc.fields(kindVariant, adultVariantID)
	if v["dir"] != "mine/adult" || v["button"] != true || v["on"] != false || v["name"] != "自己的隱藏版" {
		t.Fatalf("隱藏版：%v", v)
	}
}

// 隱藏版只看請求與紀錄、不看名字（DEV-160 S3；作者 2026-11-15）：名字含「成人」「Adult」的新角色、新衣服、加照片都是一般版、開關關著也放得進去；
// 這套現在的照片全是隱藏版時，沒寫 adult 的上傳算隱藏版（開關沒開就拒、說清楚），寫 adult = false 才是一般版。
func TestHiddenFromRecordsNotNames(t *testing.T) {
	sc := newScene(t)
	sc.upload("p.png", png("p"))
	sc.upload("q.png", png("q"))
	sc.done(sc.requestAndRound(map[string]any{"type": "outfit", "upload": "p.png", "persona": "athena", "name": "成人禮"}))              // U-1
	sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "q.png", "persona": "athena", "outfit": "my-1"}))            // U-2
	sc.done(sc.requestAndRound(map[string]any{"type": "character", "upload": "p.png", "name": "Young Adult", "persona": "my_adult"})) // U-3
	for _, pid := range []string{"athena-my-1--u1", "athena-my-1--u2", "my_adult-base--u3"} {
		sc.photoWant(pid, map[string]any{"hidden": false})
	}
	if _, ok := sc.v.get(kindVariant, adultVariantID); ok {
		t.Fatal("沒有隱藏版的照片就不建自己的隱藏版設定")
	}

	// 整套都是隱藏版（例如轉換器登記的舊衣服）：沒寫 adult 的上傳也算隱藏版。
	sc.v.seed(kindOutfit, "athena-my-9", map[string]any{"persona": "athena", "outfit": "my-9", "name": "海邊", "cost": 1, "state": "active", "source": "mine"})
	sc.v.seed(kindPhoto, "athena-my-9--half1-h", map[string]any{"persona": "athena", "outfit": "athena-my-9", "type": "half", "order": 1,
		"hidden": true, "source": "mine", "file": "adult/athena@my-9.png", "adopted": true})
	sc.rejected(sc.requestAndRound(map[string]any{"type": "photo", "upload": "q.png", "persona": "athena", "outfit": "my-9"}), rules.ReasonAllHidden) // U-4
	if _, ok := sc.v.get(kindPhoto, "athena-my-9--u4"); ok {
		t.Fatal("拒絕的不登記")
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "q.png", "persona": "athena", "outfit": "my-9", "adult": false})) // U-5
	sc.photoWant("athena-my-9--u5", map[string]any{"hidden": false})
	sc.svc.Adult = func(context.Context) bool { return true }
	sc.v.seed(kindOutfit, "athena-my-8", map[string]any{"persona": "athena", "outfit": "my-8", "name": "夜", "cost": 1, "state": "active", "source": "mine"})
	sc.v.seed(kindPhoto, "athena-my-8--half1-h", map[string]any{"persona": "athena", "outfit": "athena-my-8", "type": "half", "order": 1,
		"hidden": true, "source": "mine", "file": "adult/athena@my-8.png", "adopted": true})
	sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "p.png", "persona": "athena", "outfit": "my-8"})) // U-6
	sc.photoWant("athena-my-8--u6", map[string]any{"hidden": true, "order": 2})
}

// 自己的隱藏版不另做角色或衣服；不是自己的隱藏版的請求照舊不能改官方衣服。
func TestAdultOnlyForExistingOutfits(t *testing.T) {
	sc := newScene(t)
	sc.svc.Adult = func(context.Context) bool { return true }
	sc.upload("a.png", png("x"))
	c := sc.request(map[string]any{"type": "character", "upload": "a.png", "name": "新", "adult": true})
	o := sc.request(map[string]any{"type": "outfit", "upload": "a.png", "persona": "athena", "name": "新", "adult": true})
	p := sc.request(map[string]any{"type": "photo", "upload": "a.png", "persona": "athena", "outfit": "base"})
	sc.round()
	for _, id := range []string{c, o} {
		if f := sc.fields(KindRequest, id); f["reason"] != rules.ReasonAdultKind {
			t.Fatalf("%s：%v", id, f)
		}
	}
	if f := sc.fields(KindRequest, p); f["reason"] != rules.ReasonOfficial {
		t.Fatalf("正常版改官方衣服要拒絕：%v", f)
	}
}
