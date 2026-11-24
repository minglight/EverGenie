package run

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
)

// 現在穿這套（CLO-30）的測試：指定穿、復原、沒擁有被擋、時間到回到輪流、不動輪流穿的清單。

func (sc *scene) wearReq(id, typ, outfit string) {
	sc.v.seedBy(KindRequest, id, map[string]any{"type": typ, "persona": "builder", "outfit": outfit, "state": "pending"}, "owner(web)")
}

// wearEnd 是 wearScene 那個時間點指定穿的到期時間：當天結束（機器的時區）。
var wearEnd = rules.WearUntil(time.Unix(495000*3600+1800, 0))

// wearScene：night 免費（擁有），sea 要 2 點（沒擁有）；輪流穿只有 base。時間固定在某個小時的第 30 分。
func wearScene(t *testing.T) *scene {
	sc := newScene(t)
	sc.outfit("night", 0, "active", 2)
	sc.v.seed(KindRotation, "builder", map[string]any{"outfits": []any{"base"}})
	sc.now = time.Unix(495000*3600+1800, 0)
	sc.round()
	if p := sc.person(); p["worn"] != "base" || p["picked"] != false {
		sc.t.Fatalf("一開始照輪流穿 base：%v %v", p["worn"], p["picked"])
	}
	return sc
}

func TestWearNowSwitchesAtOnceAndKeepsRotation(t *testing.T) {
	sc := wearScene(t)
	sc.wearReq("Q-1", "wear", "night")
	sc.round()
	if q := sc.fields(KindRequest, "Q-1"); q["state"] != "done" {
		t.Fatalf("請求：%v", q)
	}
	p := sc.person()
	until := wearEnd.Format(time.RFC3339)
	if p["worn"] != "night" || p["picked"] != true || p["worn_until"] != until {
		t.Fatalf("換好後：worn=%v picked=%v until=%v（要 %s）", p["worn"], p["picked"], p["worn_until"], until)
	}
	if !bytes.Equal(sc.public("worn/builder.png"), sc.img["builder@night"]) {
		t.Fatal("頭像要馬上換成 night 的圖")
	}
	// 輪流穿的清單不變：night 不會被加進去。
	r := sc.fields(KindRotation, "builder")
	if l := r["outfits"].([]any); len(l) != 1 || l[0] != "base" {
		t.Fatalf("輪流穿清單被動了：%v", l)
	}
	if rot := p["rotation"].([]any); len(rot) != 1 || rot[0] != "base" {
		t.Fatalf("有效清單被動了：%v", rot)
	}
	if o := sc.outfitView("night"); o["rotating"] != false {
		t.Fatal("night 不該變成輪流穿")
	}
}

func TestWearNowExpiresAtNextRotation(t *testing.T) {
	sc := wearScene(t)
	sc.wearReq("Q-1", "wear", "night")
	sc.round()
	// 當天最後一秒還穿著（中間過了好幾個整點也不輪）。
	sc.now = wearEnd.Add(-time.Second)
	sc.round()
	if p := sc.person(); p["worn"] != "night" {
		t.Fatalf("時間沒到不該換：%v", p["worn"])
	}
	// 過了午夜：回到輪流穿的清單。
	sc.now = wearEnd
	sc.round()
	p := sc.person()
	if p["worn"] != "base" || p["picked"] != false || p["worn_until"] != nil && p["worn_until"] != "" {
		t.Fatalf("時間到要回到輪流：worn=%v picked=%v until=%v", p["worn"], p["picked"], p["worn_until"])
	}
	if !bytes.Equal(sc.public("worn/builder.png"), sc.img["builder"]) {
		t.Fatal("頭像要換回 base")
	}
}

func TestWearNowUndo(t *testing.T) {
	sc := wearScene(t)
	sc.wearReq("Q-1", "wear", "night")
	sc.round()
	// 復原別套（不是現在指定的）：當成功、不做事。
	sc.wearReq("Q-2", "unwear", "base")
	sc.round()
	if q := sc.fields(KindRequest, "Q-2"); q["state"] != "done" {
		t.Fatalf("Q-2：%v", q)
	}
	if p := sc.person(); p["worn"] != "night" {
		t.Fatalf("復原別套不該動到現在穿的：%v", p["worn"])
	}
	sc.wearReq("Q-3", "unwear", "night")
	sc.round()
	if q := sc.fields(KindRequest, "Q-3"); q["state"] != "done" {
		t.Fatalf("Q-3：%v", q)
	}
	if p := sc.person(); p["worn"] != "base" || p["picked"] != false {
		t.Fatalf("復原後回到輪流：%v %v", p["worn"], p["picked"])
	}
	if !bytes.Equal(sc.public("worn/builder.png"), sc.img["builder"]) {
		t.Fatal("頭像要換回 base")
	}
	// 沒有指定時再復原一次也成功。
	sc.wearReq("Q-4", "unwear", "night")
	sc.round()
	if q := sc.fields(KindRequest, "Q-4"); q["state"] != "done" {
		t.Fatalf("Q-4：%v", q)
	}
}

func TestWearNowRejectsNotOwned(t *testing.T) {
	sc := wearScene(t)
	sc.outfit("plan", 0, "draft", 4)
	sc.wearReq("Q-1", "wear", "sea")  // 沒兌換
	sc.wearReq("Q-2", "wear", "plan") // 設計中
	sc.wearReq("Q-3", "wear", "nope") // 沒有這套
	sc.round()
	for id, want := range map[string]string{"Q-1": "要先兌換才能穿", "Q-2": "要先兌換才能穿", "Q-3": "衣櫃裡沒有這套衣服"} {
		q := sc.fields(KindRequest, id)
		if q["state"] != "rejected" || !strings.Contains(q["reason"].(string), want) {
			t.Fatalf("%s：%v", id, q)
		}
	}
	if p := sc.person(); p["worn"] != "base" || p["picked"] != false {
		t.Fatalf("被擋的不該換：%v", p["worn"])
	}
	if r := sc.fields(KindRotation, "builder"); r["wear"] != nil && r["wear"] != "" {
		t.Fatalf("被擋的不該寫 wear：%v", r)
	}
}

// 開發家沒兌換也看得到真圖（CLO-27），但那不是擁有：不能穿。
func TestWearNowRejectsDevPreview(t *testing.T) {
	sc := wearScene(t)
	sc.svc.Dev = func(context.Context) (bool, error) { return true, nil }
	sc.round()
	if o := sc.outfitView("sea"); o["preview"] != true || o["owned"] != false {
		t.Fatalf("開發家的 sea：%v", o)
	}
	sc.wearReq("Q-1", "wear", "sea")
	sc.round()
	if q := sc.fields(KindRequest, "Q-1"); q["state"] != "rejected" || !strings.Contains(q["reason"].(string), "要先兌換才能穿") {
		t.Fatalf("Q-1：%v", q)
	}
	if p := sc.person(); p["worn"] != "base" {
		t.Fatalf("開發家沒兌換的不能穿：%v", p["worn"])
	}
}

// 就算有人直接把 wear 寫進輪播設定（繞過請求），沒擁有的也不會穿上。
func TestWearFieldIgnoredWhenNotOwned(t *testing.T) {
	sc := wearScene(t)
	sc.v.seed(KindRotation, "builder", map[string]any{"outfits": []any{"base"}, "wear": "sea", "wear_until": wearEnd.Format(time.RFC3339)})
	sc.round()
	if p := sc.person(); p["worn"] != "base" || p["picked"] != false {
		t.Fatalf("沒擁有的 wear 不該作用：%v", p["worn"])
	}
	if sc.public("o/builder/sea/1.png") != nil {
		t.Fatal("沒兌換的真圖不該公開")
	}
}

func TestWearNowCreatesRotationRecordWhenMissing(t *testing.T) {
	sc := newScene(t)
	sc.outfit("night", 0, "active", 2)
	sc.now = time.Unix(495000*3600+1800, 0)
	sc.round()
	if _, ok := sc.v.get(KindRotation, "builder"); ok {
		t.Fatal("一開始不該有輪播設定")
	}
	sc.wearReq("Q-1", "wear", "night")
	sc.round()
	r := sc.fields(KindRotation, "builder")
	if l, _ := r["outfits"].([]any); len(l) != 0 || r["wear"] != "night" {
		t.Fatalf("新建的輪播設定：%v", r)
	}
	p := sc.person()
	if p["worn"] != "night" {
		t.Fatalf("worn：%v", p["worn"])
	}
	// 有效清單照舊是第一套擁有的。
	if rot := p["rotation"].([]any); len(rot) != 1 || rot[0] != "base" {
		t.Fatalf("有效清單：%v", rot)
	}
	sc.now = wearEnd
	sc.round()
	if p := sc.person(); p["worn"] != "base" {
		t.Fatalf("時間到回到第一套：%v", p["worn"])
	}
}

// 行程在「寫了 wear、還沒標 done」時被殺：請求還是 pending，重做一次結果相同。
func TestWearNowReplaySafe(t *testing.T) {
	sc := wearScene(t)
	sc.wearReq("Q-1", "wear", "night")
	sc.round()
	before := sc.fields(KindRotation, "builder")["wear_until"]
	sc.wearReq("Q-1", "wear", "night")
	sc.round()
	if q := sc.fields(KindRequest, "Q-1"); q["state"] != "done" {
		t.Fatalf("重播後的請求：%v", q)
	}
	r := sc.fields(KindRotation, "builder")
	if r["wear"] != "night" || r["wear_until"] != before {
		t.Fatalf("重播後：%v", r)
	}
	if p := sc.person(); p["worn"] != "night" {
		t.Fatalf("worn：%v", p["worn"])
	}
	if n := sc.v.count(KindUnlock); n != 0 {
		t.Fatalf("現在穿這套不該寫兌換紀錄：%d", n)
	}
}

// 指定穿之後再指定另一套：換成新的那套；秘書寫的請求也算（不限主人）。
func TestWearNowBySecretaryAndRepick(t *testing.T) {
	sc := wearScene(t)
	sc.wearReq("Q-1", "wear", "night")
	sc.round()
	sc.v.seedBy(KindRequest, "Q-2", map[string]any{"type": "wear", "persona": "builder", "outfit": "base", "state": "pending"}, "code")
	sc.round()
	if q := sc.fields(KindRequest, "Q-2"); q["state"] != "done" {
		t.Fatalf("Q-2：%v", q)
	}
	if p := sc.person(); p["worn"] != "base" || p["picked"] != true {
		t.Fatalf("改指定 base：%v %v", p["worn"], p["picked"])
	}
}

// 請求裡的 persona 是自由字串：寫壞的（讀紀錄會回錯的編號）只拒絕或略過那一筆，不能讓整輪中止——
// 不然那筆永遠 pending，之後每一輪都卡在它，同一輪後面的請求也做不了。
func TestWearRequestWithBadPersonaDoesNotStallRound(t *testing.T) {
	sc := wearScene(t)
	for id, typ := range map[string]string{"Q-1": "wear", "Q-2": "unwear"} {
		sc.v.seedBy(KindRequest, id, map[string]any{"type": typ, "persona": "../x", "outfit": "night", "state": "pending"}, "owner(web)")
	}
	sc.wearReq("Q-3", "wear", "night")
	sc.round()
	if q := sc.fields(KindRequest, "Q-1"); q["state"] != "rejected" || q["reason"] != rules.ReasonNotFound {
		t.Fatalf("寫壞的 wear 要被拒：%v", q)
	}
	if q := sc.fields(KindRequest, "Q-2"); q["state"] != "done" {
		t.Fatalf("寫壞的 unwear 當成功不做事：%v", q)
	}
	if q := sc.fields(KindRequest, "Q-3"); q["state"] != "done" {
		t.Fatalf("同一輪後面的請求照常處理：%v", q)
	}
	if p := sc.person(); p["worn"] != "night" || p["picked"] != true {
		t.Fatalf("後面那筆要換成 night：%v %v", p["worn"], p["picked"])
	}
}
