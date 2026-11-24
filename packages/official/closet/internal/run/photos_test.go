package run

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 舊角色包、舊匯出（沒帶照片紀錄）同步或匯入後：衣櫃自己補登記，衣服照樣有圖（DEV-160 S2）。
// 補登記只新增紀錄、不動檔、冪等；退路是封存補錯的紀錄，下一輪用新編號重補。
func TestOldPackWithoutPhotoRecordsBackfills(t *testing.T) {
	sc := newScene(t)
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/1.png"), sc.img["builder"]) {
		t.Fatal("沒有照片紀錄的舊資料，補登記後要有圖")
	}
	half := sc.fields(KindPhoto, "builder-base--half1")
	if half["source"] != "official" || half["file"] != "avatars/builder.png" || half["hidden"] != false || half["adopted"] != true {
		t.Fatalf("補登記的紀錄：%v", half)
	}
	if _, err := os.Stat(filepath.Join(sc.home, "personas", "avatars", "builder.png")); err != nil {
		t.Fatal("補登記不動檔")
	}
	n := sc.v.puts[KindPhoto]
	if n != 3 {
		t.Fatalf("三套各一張：%d", n)
	}
	sc.round()
	if sc.v.puts[KindPhoto] != n {
		t.Fatal("再跑一次不重補（冪等）")
	}
	// 後來才複製進來的圖：下一輪補上（跟以前定期重掃一樣）。
	second := pngFile(t, filepath.Join(sc.home, "personas", "avatars", "builder~2.png"), 9)
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/2.png"), second) || sc.fields(KindPhoto, "builder-base--half2")["order"] != 2 {
		t.Fatal("新複製進來的圖下一輪補登記")
	}
	// 退路：封存補錯的紀錄，下一輪換新編號重補。
	if err := sc.v.Archive(context.Background(), KindPhoto, "builder-base--half2"); err != nil {
		t.Fatal(err)
	}
	sc.round()
	if again := sc.fields(KindPhoto, "builder-base--half2-r2"); again["file"] != "avatars/builder~2.png" {
		t.Fatalf("封存後重補用 -r2：%v", again)
	}
}

// 官方層在家外，角色包帶了照片紀錄：用紀錄指的檔，不再照檔名猜；家裡不寫官方照片紀錄。
func TestOfficialRecordsFromPackWin(t *testing.T) {
	sc, state := newSceneV2(t)
	renamed := pngFile(t, filepath.Join(state, "personas", "avatars", "pack", "b1.png"), 33)
	sc.v.seed(KindPhoto, "builder-base--half1", map[string]any{"persona": "builder", "outfit": "builder-base", "type": "half", "order": 1,
		"hidden": false, "source": "official", "file": "avatars/pack/b1.png", "adopted": true})
	puts := sc.v.puts[KindPhoto]
	sc.round()
	b, _ := os.ReadFile(filepath.Join(state, "runtime", "packages", "closet", "public", "o", "builder", "base", "1.png"))
	if !bytes.Equal(b, renamed) {
		t.Fatal("有官方照片紀錄就用紀錄指的檔")
	}
	if sc.v.puts[KindPhoto] != puts {
		t.Fatal("官方層在家外：家裡不寫官方照片紀錄")
	}
}

// 改編號（recode、home tidy、personas promote）之後：照片紀錄的 outfit、編號換了，檔名不改；頁面照樣有圖。
func TestRecodedRecordsKeepFileNames(t *testing.T) {
	sc := newScene(t)
	sc.round()
	// 模擬改編號：builder-sea → builder-shore，照片紀錄跟著換，檔還是 builder@sea.png。
	sc.v.Archive(context.Background(), KindOutfit, "builder-sea")
	sc.v.seed(KindOutfit, "builder-shore", map[string]any{"persona": "builder", "outfit": "shore", "name": "SHORE", "cost": 0, "state": "active", "source": "official", "order": 3})
	sc.v.Archive(context.Background(), KindPhoto, "builder-sea--half1")
	sc.v.seed(KindPhoto, "builder-shore--half1", map[string]any{"persona": "builder", "outfit": "builder-shore", "type": "half", "order": 1,
		"hidden": false, "source": "official", "file": "avatars/builder@sea.png", "adopted": true})
	sc.round()
	if !bytes.Equal(sc.public("o/builder/shore/1.png"), sc.img["builder@sea"]) {
		t.Fatal("改編號後照紀錄找圖，檔名沒改也有圖")
	}
	if _, err := os.Stat(filepath.Join(sc.home, "personas", "avatars", "builder@sea.png")); err != nil {
		t.Fatal("檔名不改")
	}
	for _, r := range sc.v.records(KindPhoto) {
		if strings.HasPrefix(r.ID, "builder-sea") || (r.ID != "builder-shore--half1" && r.Fields["file"] == "avatars/builder@sea.png") {
			t.Fatalf("同一個檔不重複登記：%v", r)
		}
	}
}

// 隱藏版的時段開、關：公開資料夾的 h- 檔跟著出現、消失。
func TestHiddenWindowAddsAndRemovesPublicFiles(t *testing.T) {
	sc, hidden := variantScene(t)
	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "windows": []any{"12:00-13:00"}})
	sc.now = time.Date(2026, 11, 15, 11, 59, 0, 0, time.Local)
	sc.round()
	if sc.public("o/builder/base/h-1.png") != nil || sc.public("f/builder/base/h-1.png") != nil {
		t.Fatal("時段外沒有 h- 檔")
	}
	if photos := sc.outfitView("base")["photos"].([]any); len(photos) != 1 {
		t.Fatalf("時段外只有一般版：%v", photos)
	}
	sc.now = time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/h-1.png"), hidden["builder"]) || !bytes.Equal(sc.public("o/builder/base/h-2.png"), hidden["builder~2"]) ||
		string(sc.public("o/builder/base/h-1.mp4")) != "hidden-mp4" || !bytes.Equal(sc.public("f/builder/base/h-1.png"), hidden["full/builder"]) {
		t.Fatal("時段內 h- 檔要出現（照片、影片、全身照）")
	}
	sc.now = time.Date(2026, 11, 15, 13, 0, 0, 0, time.Local)
	sc.round()
	for _, rel := range []string{"o/builder/base/h-1.png", "o/builder/base/h-2.png", "o/builder/base/h-1.mp4", "f/builder/base/h-1.png"} {
		if sc.public(rel) != nil {
			t.Fatalf("時段結束 %s 要消失", rel)
		}
	}
}

// 照片紀錄的 file 不合格或經過連結跑出擁有者位置：不用。
func TestPhotoRecordFileMustStayInside(t *testing.T) {
	sc := newScene(t)
	outside := t.TempDir()
	leak := pngFile(t, filepath.Join(outside, "x.png"), 77)
	if err := os.Symlink(outside, filepath.Join(sc.home, "personas", "link")); err != nil {
		t.Fatal(err)
	}
	sc.outfit("wip", 1, "draft", 4)
	for id, file := range map[string]string{"builder-wip--half1": "link/x.png", "builder-wip--half2": "../vault/x.png", "builder-wip--half3": "/abs.png"} {
		sc.v.seed(KindPhoto, id, map[string]any{"persona": "builder", "outfit": "builder-wip", "type": "half", "order": int(id[len(id)-1] - '0'),
			"hidden": false, "source": "official", "file": file, "adopted": false})
	}
	sc.round()
	if sc.publicHas(leak) || len(sc.outfitView("wip")["photos"].([]any)) != 0 {
		t.Fatal("跑出擁有者位置的檔不能用")
	}
}
