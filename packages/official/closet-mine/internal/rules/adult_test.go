package rules

import (
	"testing"
)

// 自己的隱藏版只給 photo／full／video；官方衣服也可以（不碰官方圖）。
func TestAdultRequestRules(t *testing.T) {
	r := ParseRequest("U-1", map[string]any{"type": "photo", "upload": "a.png", "persona": "athena", "outfit": "base", "adult": true})
	if !r.Adult {
		t.Fatalf("自己的隱藏版：%+v", r)
	}
	if Validate(r) != "" {
		t.Fatal("自己的隱藏版的照片請求合格")
	}
	for _, typ := range []string{TypeCharacter, TypeOutfit} {
		if got := Validate(Request{ID: "U-1", Type: typ, Upload: "a.png", Name: "x", Persona: "athena", Adult: true}); got != ReasonAdultKind {
			t.Errorf("%s 自己的隱藏版：%q", typ, got)
		}
	}
	if got := CheckFacts(r, Facts{OutfitExists: true, OutfitSource: "official"}); got != "" {
		t.Fatalf("自己的隱藏版可以替官方衣服放圖：%q", got)
	}
	r.Adult = false
	if got := CheckFacts(r, Facts{OutfitExists: true, OutfitSource: "official"}); got != ReasonOfficial {
		t.Fatalf("正常版不能改官方衣服：%q", got)
	}
}

// 上傳的照片登記成隱藏版只看請求與紀錄（DEV-160 S3；作者 2026-11-15：不用名字分類）：寫 adult = true；沒寫 adult 而這套現在的照片全是隱藏版也算；
// 寫了 adult = false 就是一般版。名字長什麼樣（成人禮、Young Adult）都不管——UploadHidden 根本不收名字。
func TestUploadHiddenFromRequestAndRecords(t *testing.T) {
	plain := ParseRequest("U-1", map[string]any{"type": "photo"})
	if plain.UploadHidden(false) {
		t.Fatal("沒寫 adult、這套有一般版的照片：一般版")
	}
	if !plain.UploadHidden(true) {
		t.Fatal("沒寫 adult、這套的照片全是隱藏版：隱藏版")
	}
	if no := ParseRequest("U-2", map[string]any{"type": "photo", "adult": false}); !no.AdultGiven || no.UploadHidden(true) {
		t.Fatal("寫了 adult = false：一般版")
	}
	if !ParseRequest("U-3", map[string]any{"type": "photo", "adult": true}).UploadHidden(false) {
		t.Fatal("寫了 adult = true：隱藏版")
	}
}
