package run

import (
	"bytes"
	"context"
	"testing"

	"github.com/minglight/evergenie/packages/official/closet-mine/internal/rules"
)

// 新上傳一律放 photos/<照片編號>.<ext>、照片編號是 <衣服編號>--u<請求編號>，種類、第幾張、一般或隱藏版只記在紀錄上（DEV-160 S3）。
func TestUploadsGoToPhotosFolderNamedByPhotoID(t *testing.T) {
	sc := newScene(t)
	sc.svc.Adult = func(context.Context) bool { return true }
	sc.upload("beach.png", png("beach"))
	sc.upload("a1.png", png("adult-1"))
	sc.upload("full.jpg", jpeg("full"))
	sc.request(map[string]any{"type": "outfit", "upload": "beach.png", "persona": "athena", "name": "海邊"})                // U-1
	sc.request(map[string]any{"type": "photo", "upload": "a1.png", "persona": "athena", "outfit": "base", "adult": true}) // U-2
	sc.request(map[string]any{"type": "full", "upload": "full.jpg", "persona": "athena", "outfit": "my-1"})               // U-3
	sc.round()
	for id, want := range map[string]map[string]any{
		"athena-my-1--u1": {"outfit": "athena-my-1", "type": "half", "order": 1, "source": "mine", "file": "photos/athena-my-1--u1.png", "hidden": false},
		"athena-base--u2": {"outfit": "athena-base", "type": "half", "order": 1, "source": "mine", "file": "photos/athena-base--u2.png", "hidden": true},
		"athena-my-1--u3": {"type": "full", "file": "photos/athena-my-1--u3.jpg", "hidden": false},
	} {
		sc.photoWant(id, want)
	}
	if got := sc.files("photos"); len(got) != 3 {
		t.Fatalf("三個檔都在 photos/：%v", got)
	}
	if sc.files("avatars") != nil || sc.files("adult") != nil {
		t.Fatal("新上傳不寫 avatars/、adult/")
	}
}

// 寫了 photo_id 就是換那一張：同一筆紀錄、新檔 photos/<照片編號>.<ext>、舊檔（舊的檔名也一樣）改名成備份；設計中的換了要重新採用。
func TestReplacePhotoByID(t *testing.T) {
	sc := newScene(t)
	sc.v.seed(kindOutfit, "athena-my-9", map[string]any{"persona": "athena", "outfit": "my-9", "name": "舊的", "cost": 1, "state": "draft", "source": "mine"})
	sc.write("personas/mine/avatars/athena@my-9~2.png", png("old-2"))
	sc.v.seed(kindPhoto, "athena-my-9--half2", map[string]any{"persona": "athena", "outfit": "athena-my-9", "type": "half", "order": 2,
		"hidden": false, "source": "mine", "file": "avatars/athena@my-9~2.png", "adopted": true})
	sc.upload("new.webp", append([]byte("RIFF\x00\x00\x00\x00WEBP"), "new-2"...))
	id := sc.requestAndRound(map[string]any{"type": "photo", "upload": "new.webp", "persona": "athena", "outfit": "my-9", "photo_id": "athena-my-9--half2"})
	sc.done(id)
	sc.photoWant("athena-my-9--half2", map[string]any{"file": "photos/athena-my-9--half2.webp", "order": 2, "adopted": false})
	if sc.read("personas/mine/avatars/athena@my-9~2.png") != nil {
		t.Fatal("舊檔不留在原名")
	}
	if bak := sc.backupOf("avatars/athena@my-9~2.png", "replace-u1"); !bytes.Equal(sc.read("personas/mine/"+bak), png("old-2")) {
		t.Fatalf("舊檔改名成備份：%q", bak)
	}
	if _, ok := sc.v.get(kindPhoto, "athena-my-9--u1"); ok {
		t.Fatal("換圖不另登記")
	}
	// 種類不對、別套的、一般版與隱藏版互換、找不到都拒絕。
	sc.upload("p.png", png("p"))
	sc.v.seed(kindOutfit, "athena-my-8", map[string]any{"persona": "athena", "outfit": "my-8", "name": "別套", "cost": 1, "state": "active", "source": "mine"})
	for want, f := range map[string]map[string]any{
		rules.ReasonPhotoWrongType:   {"type": "full", "upload": "p.png", "persona": "athena", "outfit": "my-9", "photo_id": "athena-my-9--half2"},
		rules.ReasonPhotoOtherOutfit: {"type": "photo", "upload": "p.png", "persona": "athena", "outfit": "my-8", "photo_id": "athena-my-9--half2"},
		rules.ReasonNoPhotoRecord:    {"type": "photo", "upload": "p.png", "persona": "athena", "outfit": "my-9", "photo_id": "athena-my-9--nope"},
	} {
		sc.rejected(sc.requestAndRound(f), want)
	}
	sc.svc.Adult = func(context.Context) bool { return true }
	sc.rejected(sc.requestAndRound(map[string]any{"type": "photo", "upload": "p.png", "persona": "athena", "outfit": "my-9", "photo_id": "athena-my-9--half2", "adult": true}), rules.ReasonPhotoHiddenDiff)
}

// 動畫配哪一張照位置（照片紀錄的 order 排），不看檔名：那張的紀錄被換成別的編號（例如衣櫃補登記的 -r2）也配得到還在的那一筆；
// 寫半身照的 photo_id 就是配那一張。
func TestVideoMatchesLiveHalf(t *testing.T) {
	sc := newScene(t)
	sc.v.seed(kindOutfit, "athena-my-9", map[string]any{"persona": "athena", "outfit": "my-9", "name": "x", "cost": 1, "state": "active", "source": "mine"})
	sc.v.seed(kindPhoto, "athena-my-9--half1-r2", map[string]any{"persona": "athena", "outfit": "athena-my-9", "type": "half", "order": 1,
		"hidden": false, "source": "mine", "file": "avatars/athena@my-9.png", "adopted": true})
	sc.v.seed(kindPhoto, "athena-my-9--u4", map[string]any{"persona": "athena", "outfit": "athena-my-9", "type": "half", "order": 5,
		"hidden": false, "source": "mine", "file": "photos/athena-my-9--u4.png", "adopted": true})
	sc.upload("v.mp4", mp4("v"))
	sc.done(sc.requestAndRound(map[string]any{"type": "video", "upload": "v.mp4", "persona": "athena", "outfit": "my-9", "photo": 1})) // U-1
	sc.photoWant("athena-my-9--u1", map[string]any{"of": "athena-my-9--half1-r2", "clip": 1})
	sc.done(sc.requestAndRound(map[string]any{"type": "video", "upload": "v.mp4", "persona": "athena", "outfit": "my-9", "photo": 2})) // U-2：第 2 張是 order 5 那張
	sc.photoWant("athena-my-9--u2", map[string]any{"of": "athena-my-9--u4", "order": 5})
	sc.upload("v2.mp4", mp4("v2"))
	sc.done(sc.requestAndRound(map[string]any{"type": "video", "upload": "v2.mp4", "persona": "athena", "outfit": "my-9", "photo_id": "athena-my-9--u4", "clip": 2})) // U-3
	sc.photoWant("athena-my-9--u3", map[string]any{"of": "athena-my-9--u4", "clip": 2})
}
