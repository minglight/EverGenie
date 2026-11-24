package run

import (
	"bytes"
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// adultScene：自己的隱藏版資料夾 personas/mine/adult 裡有 builder（擁有的 base）的圖，並寫好一筆 dir = mine/adult 的隱藏版（開著）。
func adultScene(t *testing.T) (*scene, []byte) {
	sc := newScene(t)
	img := pngFile(t, filepath.Join(sc.home, "personas", "mine", "adult", "builder.png"), 90)
	sc.v.seed(KindVariant, "adult", map[string]any{"name": "自己的隱藏版", "dir": "mine/adult", "button": true, "on": true})
	return sc, img
}

// 家設定沒打開（沒接開關、或值不是 on）：自己的隱藏版整筆不作用、不列在 state.variants，公開資料夾沒有它的圖。
func TestAdultVariantIgnoredWhenSwitchOff(t *testing.T) {
	for name, adult := range map[string]func(context.Context) bool{
		"沒接開關": nil,
		"開關關著": func(context.Context) bool { return false },
	} {
		t.Run(name, func(t *testing.T) {
			sc, img := adultScene(t)
			sc.svc.Adult = adult
			sc.round()
			if sc.publicHas(img) || !bytes.Equal(sc.public("o/builder/base/1.png"), sc.img["builder"]) {
				t.Fatal("沒打開時自己的隱藏版的圖不能進公開資料夾")
			}
			if vs := sc.fields(KindState, "main")["variants"].([]any); len(vs) != 0 {
				t.Fatalf("沒打開時不顯示自己的隱藏版的開關：%v", vs)
			}
		})
	}
}

// 打開：自己的隱藏版跟一般隱藏版一樣只加在擁有的衣服上（跟一般版並列）；再關掉，下一輪換回、公開資料夾不留它的圖。
func TestAdultVariantAppliesOnlyWhenSwitchOn(t *testing.T) {
	sc, img := adultScene(t)
	on := true
	sc.svc.Adult = func(context.Context) bool { return on }
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/h-1.png"), img) || !bytes.Equal(sc.public("o/builder/base/1.png"), sc.img["builder"]) {
		t.Fatal("打開後擁有的衣服多列自己的隱藏版的圖（一般版照樣在）")
	}
	if vs := sc.fields(KindState, "main")["variants"].([]any); len(vs) != 1 {
		t.Fatalf("打開後列出自己的隱藏版的開關：%v", vs)
	}
	on = false
	sc.round()
	if sc.publicHas(img) || !bytes.Equal(sc.public("o/builder/base/1.png"), sc.img["builder"]) {
		t.Fatal("關掉後換回正常版、不留自己的隱藏版的圖")
	}
}

// 舊家把自己的隱藏版放在官方位置（avatars/level/…，名字看起來是自己的隱藏版）：登記成官方的隱藏版照片，
// 開關關著時照樣不列（跟以前一樣，DEV-154 搬進 personas/mine/ 之前寧可多擋）；它的設定開著也不讓那一組開起來。
// 一般的隱藏版（avatars/hidden）不受開關影響。
func TestLegacyOfficialAdultFolderStaysBehindSwitch(t *testing.T) {
	sc := newScene(t)
	night := pngFile(t, filepath.Join(sc.home, "personas", "avatars", "hidden", "builder.png"), 91)
	legacy := pngFile(t, filepath.Join(sc.home, "personas", "avatars", "level", "hidden", "builder~2.png"), 92)
	sc.v.seed(KindVariant, "lv", map[string]any{"name": "舊的", "dir": "avatars/level/hidden", "button": true, "on": true})
	on := false
	sc.svc.Adult = func(context.Context) bool { return on }
	sc.round()
	if sc.publicHas(legacy) {
		t.Fatal("開關關著：舊家放在官方位置的自己的隱藏版不能進公開資料夾")
	}
	if vs := sc.fields(KindState, "main")["variants"].([]any); len(vs) != 0 {
		t.Fatalf("開關關著：只有自己的隱藏版的設定，不顯示那一組：%v", vs)
	}
	if f := sc.fields(KindPhoto, "builder-base--half2-h"); f["source"] != "official" || f["hidden"] != true {
		t.Fatalf("照樣登記成官方的隱藏版照片：%v", f)
	}
	sc.v.seed(KindVariant, "night", map[string]any{"name": "夜晚", "dir": "avatars/hidden", "button": true, "on": true})
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/h-1.png"), night) || sc.publicHas(legacy) {
		t.Fatal("一般的隱藏版開著時列出，舊家的自己的隱藏版照樣不列")
	}
	on = true
	sc.round()
	if !bytes.Equal(sc.public("o/builder/base/h-2.png"), legacy) {
		t.Fatal("開關打開後才列")
	}
}

// 主人自己的衣服（source = mine）也能設價錢：沒兌換只有剪影，兌換後才公開真圖。
func TestMineOutfitWithCostShowsMaskUntilRedeemed(t *testing.T) {
	sc := newScene(t)
	real := pngFile(t, filepath.Join(sc.home, "personas", "mine", "avatars", "builder@my-3.png"), 33)
	sc.v.seed(KindOutfit, "builder-my-3", map[string]any{"persona": "builder", "outfit": "my-3", "name": "我的", "cost": 2, "state": "active", "source": "mine"})
	sc.wallet(2)
	sc.round()
	if sc.publicHas(real) || sc.public("s/builder/my-3/1.png") == nil {
		t.Fatal("沒兌換的我的衣服只能有剪影")
	}
	if o := sc.outfitView("my-3"); o["owned"] != false || o["cost"] != 2 {
		t.Fatalf("兌換前：%v", o)
	}
	sc.request("Q-1", "owner(web)", "builder", "my-3")
	sc.round()
	if !bytes.Equal(sc.public("o/builder/my-3/1.png"), real) || sc.public("s/builder/my-3/1.png") != nil {
		t.Fatal("兌換後真圖取代剪影")
	}
	if st := sc.fields(KindState, "main"); st["spent"] != 2 || st["points"] != 0 {
		t.Fatalf("扣了 2 點：%v", st)
	}
}

// 升成官方後：新編號帶 from（舊編號已封存），舊編號兌換過的照樣擁有、不再扣點。
func TestPromotedOutfitKeepsRedeemed(t *testing.T) {
	sc := newScene(t)
	sc.v.seedBy(KindUnlock, "builder-my-3", map[string]any{"persona": "builder", "outfit": "my-3", "cost": 1, "at": "2026-11-01T10:00:00+08:00"}, "closet")
	sc.v.seedBy(KindOutfit, "builder-gala", map[string]any{"persona": "builder", "outfit": "gala", "name": "晚宴", "cost": 1, "state": "active", "source": "official", "from": "builder-my-3"}, "closet-mine")
	// 別人寫的 from 不認（不是升成官方產生的）。
	sc.v.seedBy(KindUnlock, "builder-my-4", map[string]any{"persona": "builder", "outfit": "my-4", "cost": 1, "at": "2026-11-01T10:00:00+08:00"}, "closet")
	sc.v.seed(KindOutfit, "builder-ball", map[string]any{"persona": "builder", "outfit": "ball", "name": "舞會", "cost": 1, "state": "active", "source": "official", "from": "builder-my-4"})
	pngFile(t, filepath.Join(sc.home, "personas", "avatars", "builder@gala.png"), 34)
	pngFile(t, filepath.Join(sc.home, "personas", "avatars", "builder@ball.png"), 35)
	sc.round()
	if o := sc.outfitView("gala"); o["owned"] != true {
		t.Fatalf("升官方前兌換過的要照樣擁有：%v", o)
	}
	if o := sc.outfitView("ball"); o["owned"] == true {
		t.Fatalf("不是 closet-mine 寫的 from 不認：%v", o)
	}
	moved, ok := sc.v.get(KindUnlock, "builder-gala")
	if !ok || moved.Fields["by"] != "closet" || moved.Fields["request"] != "carry:builder-my-3" || moved.Fields["cost"] != 1 {
		t.Fatalf("兌換要搬成 closet 寫的新紀錄：%+v", moved)
	}
	if old, _ := sc.v.get(KindUnlock, "builder-my-3"); !old.Archived {
		t.Fatal("舊的兌換要封存（點數不重複算）")
	}
	// 之後有人改了那筆 outfit（by 變了），擁有不會掉。
	sc.v.seed(KindOutfit, "builder-gala", map[string]any{"persona": "builder", "outfit": "gala", "name": "晚宴（改）", "cost": 1, "state": "active", "source": "official", "from": "builder-my-3"})
	sc.round()
	if o := sc.outfitView("gala"); o["owned"] != true {
		t.Fatalf("改過之後照樣擁有：%v", o)
	}
}

// 跑著的時候把開關關掉（沒有任何 vault 變動）：幾秒內公開資料夾就換回正常版、沒有自己的隱藏版的圖。
func TestAdultSwitchOffTakesEffectWithoutVaultChange(t *testing.T) {
	sc, img := adultScene(t)
	var on atomic.Bool
	on.Store(true)
	sc.svc.Adult = func(context.Context) bool { return on.Load() }
	sc.svc.AdultPoll, sc.svc.Debounce = 20*time.Millisecond, 10*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sc.svc.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	wait := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatalf("等不到：%s", what)
	}
	wait("打開時多列自己的隱藏版", func() bool { return bytes.Equal(sc.public("o/builder/base/h-1.png"), img) })
	on.Store(false)
	wait("關掉後換回正常版", func() bool {
		return !sc.publicHas(img) && bytes.Equal(sc.public("o/builder/base/1.png"), sc.img["builder"])
	})
}

// 「修改」分頁開關（closet.edit-tab）改了不經 vault：輪詢看到就做一輪，edit-tab 馬上跟著拿掉或加回（SEC-12 審查）。
func TestEditTabSwitchTakesEffectWithoutVaultChange(t *testing.T) {
	sc := newScene(t)
	var on atomic.Bool
	on.Store(true)
	sc.svc.Dev = devOn
	sc.svc.EditTab = func(context.Context) bool { return on.Load() }
	sc.svc.AdultPoll, sc.svc.Debounce = 20*time.Millisecond, 10*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sc.svc.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	has := func() bool {
		r, ok := sc.v.get(KindCapabilities, "main")
		if !ok {
			return false
		}
		for _, c := range anyStrings(r.Fields["supports"]) {
			if c == "edit-tab" {
				return true
			}
		}
		return false
	}
	wait := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatalf("等不到：%s", what)
	}
	wait("開著：有 edit-tab", has)
	on.Store(false)
	wait("關掉：edit-tab 拿掉", func() bool { return !has() })
	on.Store(true)
	wait("再打開：edit-tab 回來", has)
}

func anyStrings(v any) []string {
	var out []string
	if list, ok := v.([]any); ok {
		for _, x := range list {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
