package pub

import "testing"

// 隱藏版只有一組（DEV-160 S2）：公開檔名固定前綴 h-，跟一般版的同一張並存、不互相蓋掉。
func TestHiddenPathsUseFixedPrefix(t *testing.T) {
	if PhotoPath("amy", "sea", true, 2, ".png") != "o/amy/sea/h-2.png" || PhotoPath("amy", "sea", false, 2, ".png") != "o/amy/sea/2.png" {
		t.Error("隱藏版的公開檔名是 h-<n>，不蓋一般版")
	}
	if FullPathOf("amy", "sea", true, ".png") != "f/amy/sea/h-1.png" || FullPathOf("amy", "sea", false, ".png") != "f/amy/sea/1.png" {
		t.Error("隱藏版全身照的公開檔名是 h-1")
	}
	plan := BuildPlan([]OutfitInput{{Persona: "amy", Outfit: "sea", Owned: true,
		Photos:     []Photo{{N: 2, Src: "/s/2.png", Ext: ".png"}, {N: 2, Src: "/h/2.png", Ext: ".png", Hidden: true, Videos: []string{"/h/2.mp4"}}},
		FullHidden: Photo{N: 1, Src: "/h/full.png", Ext: ".png", Hidden: true}}}, nil)
	for rel, src := range map[string]string{"o/amy/sea/2.png": "/s/2.png", "o/amy/sea/h-2.png": "/h/2.png", "o/amy/sea/h-2.mp4": "/h/2.mp4", "f/amy/sea/h-1.png": "/h/full.png"} {
		if plan[rel].Src != src {
			t.Errorf("%s 要是 %s：%+v", rel, src, plan)
		}
	}
	// 沒擁有的剪影只用一般版的第 1 張，不用隱藏版的。
	plan = BuildPlan([]OutfitInput{{Persona: "amy", Outfit: "sea", State: "active",
		Photos: []Photo{{N: 1, Src: "/h/1.png", Ext: ".png", Hidden: true}, {N: 1, Src: "/s/1.png", Ext: ".png"}}}}, nil)
	if plan[SilhouettePath("amy", "sea")].Src != "/s/1.png" {
		t.Errorf("剪影用一般版：%+v", plan)
	}
}
