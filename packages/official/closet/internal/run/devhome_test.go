package run

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 開發家不遮（CLO-27）：開發家是做角色與衣服的地方，沒兌換的衣服也給真圖；擁有、點數、兌換、輪播都不變。
// 正式家（沒接 Dev、或 Dev 回 false）一律照舊只給剪影——這幾個測試兩邊都守。

func devOn(context.Context) (bool, error) { return true, nil }

// 開發家：沒兌換的 night 給真圖（半身、輪播、全身、動畫），不做剪影；仍然標成沒擁有、多一個 preview，點數與輪播照舊。
func TestDevHomeShowsRealImagesOfUnredeemed(t *testing.T) {
	sc := newScene(t)
	avatars := filepath.Join(sc.home, "personas", "avatars")
	second := pngFile(t, filepath.Join(avatars, "builder@night~2.png"), 12)
	full := pngFile(t, filepath.Join(avatars, "full", "builder@night.png"), 13)
	if err := os.WriteFile(filepath.Join(avatars, "builder@night.mp4"), []byte("night-mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	sc.svc.Dev = devOn
	sc.round()

	if !bytes.Equal(sc.public("o/builder/night/1.png"), sc.img["builder@night"]) || !bytes.Equal(sc.public("o/builder/night/2.png"), second) {
		t.Fatal("開發家：沒兌換的衣服也要有真圖（含輪播的第 2 張）")
	}
	if !bytes.Equal(sc.public("f/builder/night/1.png"), full) || string(sc.public("o/builder/night/1.mp4")) != "night-mp4" {
		t.Fatal("開發家：沒兌換的衣服的全身照與動畫也給")
	}
	if sc.public("s/builder/night/1.png") != nil {
		t.Fatal("給了真圖就不再做剪影")
	}
	night := sc.outfitView("night")
	if night["owned"] != false || night["preview"] != true || night["rotating"] != false {
		t.Fatalf("仍然是沒兌換，只多 preview：%v", night)
	}
	photos := night["photos"].([]any)
	if len(photos) != 2 || photos[0].(map[string]any)["real"] != true || photos[0].(map[string]any)["url"] != "/pkg-files/closet/o/builder/night/1.png" ||
		photos[0].(map[string]any)["video"] != "/pkg-files/closet/o/builder/night/1.mp4" || night["full"] != "/pkg-files/closet/f/builder/night/1.png" {
		t.Fatalf("照片照擁有的同一套給法：%v", night)
	}
	if _, ok := night["status"].(map[string]any); !ok {
		t.Fatal("開發家：沒兌換的衣服也列每張照片的狀態（角色頁「衣服」分頁用）")
	}
	// 擁有的衣服不標 preview。
	if _, has := sc.outfitView("base")["preview"]; has {
		t.Fatal("擁有的衣服不該有 preview")
	}
	// 擁有與輪播不變：只穿擁有的 base，輪播清單沒有 night。
	p := sc.person()
	if rot := p["rotation"].([]any); p["worn"] != "base" || len(rot) != 1 || rot[0] != "base" {
		t.Fatalf("輪播與現穿只算真的擁有的：worn=%v rotation=%v", p["worn"], p["rotation"])
	}
	// 兌換規則不變：沒有點數照樣被拒，被拒後還是沒擁有。
	sc.request("Q-1", "owner(web)", "builder", "night")
	sc.round()
	if r := sc.fields(KindRequest, "Q-1"); r["state"] != "rejected" {
		t.Fatalf("沒有點數的兌換要被拒：%v", r)
	}
	if sc.outfitView("night")["owned"] != false || sc.v.count(KindUnlock) != 0 {
		t.Fatal("看得到不等於擁有：沒有兌換紀錄")
	}
	// 有點數就照常兌換：扣點、變成擁有、不再標 preview。
	sc.wallet(1)
	sc.request("Q-2", "owner(web)", "builder", "night")
	sc.round()
	night = sc.outfitView("night")
	if _, has := night["preview"]; has || night["owned"] != true {
		t.Fatalf("兌換後是擁有的、不標 preview：%v", night)
	}
	if st := sc.fields(KindState, "main"); st["spent"] != 1 || st["points"] != 0 {
		t.Fatalf("兌換照樣扣點：%v", st)
	}
}

// 正式家（沒接、或核心說不是開發家）：沒兌換的只有剪影，真圖、全身照、動畫都不在公開資料夾，也沒有 preview、status。
func TestProdHomeStillMasksUnredeemed(t *testing.T) {
	for name, dev := range map[string]func(context.Context) (bool, error){
		"沒接":    nil,
		"不是開發家": func(context.Context) (bool, error) { return false, nil },
	} {
		t.Run(name, func(t *testing.T) {
			sc := newScene(t)
			avatars := filepath.Join(sc.home, "personas", "avatars")
			full := pngFile(t, filepath.Join(avatars, "full", "builder@night.png"), 13)
			if err := os.WriteFile(filepath.Join(avatars, "builder@night.mp4"), []byte("night-mp4"), 0o644); err != nil {
				t.Fatal(err)
			}
			sc.svc.Dev = dev
			sc.round()
			if sc.publicHas(sc.img["builder@night"]) || sc.publicHas(full) || sc.publicHas([]byte("night-mp4")) || sc.public("o/builder/night/1.png") != nil {
				t.Fatal("正式家：沒兌換的真圖、全身照、動畫不能進公開資料夾")
			}
			if len(sc.public("s/builder/night/1.png")) == 0 {
				t.Fatal("正式家：沒兌換的只有剪影")
			}
			night := sc.outfitView("night")
			photos := night["photos"].([]any)
			if len(photos) != 1 || photos[0].(map[string]any)["real"] != false || night["full"] != "" {
				t.Fatalf("正式家：只有一項剪影：%v", night)
			}
			if _, has := night["preview"]; has {
				t.Fatal("正式家不該有 preview")
			}
			if _, has := night["status"]; has {
				t.Fatal("正式家：沒兌換的不透露張數")
			}
		})
	}
}

// 從開發家變回不是（或核心一時讀不到）：下一輪真圖拿掉、換回剪影。
func TestDevHomeOffRestoresMask(t *testing.T) {
	sc := newScene(t)
	dev := true
	sc.svc.Dev = func(context.Context) (bool, error) { return dev, nil }
	sc.round()
	if !bytes.Equal(sc.public("o/builder/night/1.png"), sc.img["builder@night"]) {
		t.Fatal("開發家要有真圖")
	}
	dev = false
	sc.round()
	if sc.publicHas(sc.img["builder@night"]) || len(sc.public("s/builder/night/1.png")) == 0 {
		t.Fatal("不是開發家之後：真圖拿掉、換回剪影")
	}
	if _, has := sc.outfitView("night")["preview"]; has {
		t.Fatal("不是開發家之後不再標 preview")
	}
}

// 下架而且沒擁有的衣服：展示間照舊不列（CLO-4 不變）；開發家（支援 edit）的「修改」分頁要看得到每一張（CLO-27「全開不遮」，
// DEV-160 審查 S3b）——真圖放進公開資料夾、照片網址列在 photo_urls，但不在 outfits 裡。一般家什麼都不放。
func TestDevHomeRetiredUnownedOnlyForEditTab(t *testing.T) {
	sc := newScene(t)
	old := pngFile(t, filepath.Join(sc.home, "personas", "avatars", "builder@old.png"), 50)
	sc.outfit("old", 1, "retired", 9)
	sc.svc.Dev = func(context.Context) (bool, error) { return false, nil }
	sc.round()
	if sc.outfitView("old") != nil || sc.publicHas(old) || sc.public("s/builder/old/1.png") != nil {
		t.Fatal("一般家：下架而且沒擁有的衣服看不到")
	}
	if _, has := sc.fields(KindPerson, "builder")["photo_urls"].(map[string]any)["builder-old--half1"]; has {
		t.Fatal("一般家：不給網址")
	}
	sc.svc.Dev = devOn
	sc.round()
	if sc.outfitView("old") != nil || sc.public("s/builder/old/1.png") != nil {
		t.Fatal("開發家：展示間照舊不列、不做剪影")
	}
	if !bytes.Equal(sc.public("o/builder/old/1.png"), old) {
		t.Fatal("開發家：真圖放進公開資料夾給「修改」分頁")
	}
	if u, _ := sc.fields(KindPerson, "builder")["photo_urls"].(map[string]any)["builder-old--half1"].(string); u == "" {
		t.Fatalf("開發家：photo_urls 有這張：%v", sc.fields(KindPerson, "builder")["photo_urls"])
	}
}

// 隱藏版的界線不因開發家放寬：隱藏版關著（不在時段、開關沒開）時，沒兌換的衣服只多了正常版的真圖，隱藏版的圖不在公開資料夾；
// 開著時才跟擁有的衣服一樣接在後面。
func TestDevHomeHiddenVariantStillNeedsSwitch(t *testing.T) {
	sc, hidden := variantScene(t)
	sc.svc.Dev = devOn
	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": false})
	sc.round()
	if !bytes.Equal(sc.public("o/builder/night/1.png"), sc.img["builder@night"]) {
		t.Fatal("開發家：正常版的真圖要有")
	}
	for name, h := range hidden {
		if sc.publicHas(h) {
			t.Fatalf("隱藏版關著：%s 的隱藏版圖不能進公開資料夾", name)
		}
	}
	if photos := sc.outfitView("night")["photos"].([]any); len(photos) != 1 || photos[0].(map[string]any)["variant"] != "" {
		t.Fatalf("隱藏版關著只列正常版：%v", photos)
	}

	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": true})
	sc.round()
	if !bytes.Equal(sc.public("o/builder/night/h-1.png"), hidden["builder@night"]) {
		t.Fatal("隱藏版開著：開發家沒兌換的衣服跟擁有的一樣接上隱藏版的圖")
	}

	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": false})
	sc.round()
	if sc.publicHas(hidden["builder@night"]) {
		t.Fatal("關掉後隱藏版的圖要拿掉")
	}
}

// 自己的隱藏版開關（closet.adult）關著時，開發家也完全不讀、不顯示自己的隱藏版；打開而且那筆隱藏版開著才顯示。
func TestDevHomeAdultStillNeedsHomeSwitch(t *testing.T) {
	sc, _ := adultScene(t)
	img := pngFile(t, filepath.Join(sc.home, "personas", "mine", "adult", "builder@night.png"), 91)
	sc.svc.Dev = devOn
	for name, adult := range map[string]func(context.Context) bool{
		"沒接開關": nil,
		"開關關著": func(context.Context) bool { return false },
	} {
		sc.svc.Adult = adult
		sc.round()
		if sc.publicHas(img) {
			t.Fatalf("%s：自己的隱藏版的圖不能進公開資料夾", name)
		}
		if hidden := sc.person()["hidden"].([]any); len(hidden) != 0 {
			t.Fatalf("%s：不列自己的隱藏版：%v", name, hidden)
		}
		if !bytes.Equal(sc.public("o/builder/night/1.png"), sc.img["builder@night"]) {
			t.Fatalf("%s：正常版的真圖照給", name)
		}
	}
	sc.svc.Adult = func(context.Context) bool { return true }
	sc.round()
	if !bytes.Equal(sc.public("o/builder/night/h-1.png"), img) {
		t.Fatal("自己的隱藏版開關開著、那筆隱藏版也開著：開發家沒兌換的衣服跟擁有的一樣顯示")
	}
}

// 假的 system 介面：只回 get_mode。
type fakeMode struct {
	sdk.SystemAPI
	env sdk.HomeEnv
	err error
}

func (f fakeMode) GetMode(context.Context, *sdk.SystemGetModeRequest) (*sdk.SystemGetModeResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &sdk.SystemGetModeResponse{Env: f.env}, nil
}

// 是不是開發家只認核心回的 env = dev；正式家、沒寫是「不是」，讀不到（核心沒回應、沒回內容）回錯——不是「不是開發家」：
// 衣櫃照上一輪的能力、也不自動上架（憲法第 10 條，caps.go）。
func TestDevHomeOnlyWhenCoreSaysDev(t *testing.T) {
	ctx := context.Background()
	if dev, err := DevHome(fakeMode{env: sdk.HomeEnvDev})(ctx); !dev || err != nil {
		t.Fatalf("env = dev 是開發家：%v %v", dev, err)
	}
	for name, f := range map[string]fakeMode{
		"正式家": {env: sdk.HomeEnvProd},
		"沒寫":  {},
	} {
		if dev, err := DevHome(f)(ctx); dev || err != nil {
			t.Fatalf("%s：不是開發家、也不是讀不到：%v %v", name, dev, err)
		}
	}
	if dev, err := DevHome(fakeMode{err: errors.New("核心沒回應")})(ctx); dev || err == nil {
		t.Fatalf("讀不到要回錯：%v %v", dev, err)
	}
}

// 可以升成官方（CLO-29）：只有開發家、官方角色、主人自己加的、已上架、照片不是全部隱藏版，才標 promotable。
// DEV-160 S2 起看照片紀錄的 hidden，不看名字；編號、名字看起來像隱藏版的舊圖是轉換器登記成 hidden 的。
// 正式家永遠沒有這個欄位。
func TestPromotableOnlyOnDevHomeOwnActiveOutfitOfOfficialPersona(t *testing.T) {
	seed := func(sc *scene) {
		mine := filepath.Join(sc.home, "personas", "mine", "avatars")
		for _, code := range []string{"builder@my-1", "builder@my-2", "builder@adult-1", "builder@my-4", "pet", "pet@my-1"} {
			pngFile(t, filepath.Join(mine, code+".png"), 60)
		}
		put := func(persona, id, name, state string) {
			sc.v.seed(KindOutfit, persona+"-"+id, map[string]any{"persona": persona, "outfit": id, "name": name, "cost": 0, "state": state, "source": "mine", "order": 20})
		}
		put("builder", "my-1", "海邊", "active")
		put("builder", "my-2", "還在設計", "draft")
		put("builder", "adult-1", "夜晚", "active")
		put("builder", "my-4", "R18 泳裝", "active")
		// 主人自己的角色（沒有官方角色卡）。
		put("pet", "base", "原樣", "active")
		put("pet", "my-1", "散步", "active")
	}
	flag := func(sc *scene, persona, outfit string) bool {
		for _, o := range sc.fields(KindPerson, persona)["outfits"].([]any) {
			if m := o.(map[string]any); m["outfit"] == outfit {
				v, has := m["promotable"]
				if has && v != true {
					t.Fatalf("promotable 只會是 true 或沒有：%v", m)
				}
				return has
			}
		}
		t.Fatalf("找不到 %s 的 %s", persona, outfit)
		return false
	}

	dev := newScene(t)
	seed(dev)
	dev.svc.Dev = devOn
	dev.round()
	for _, c := range []struct {
		persona, outfit string
		want            bool
		why             string
	}{
		{"builder", "my-1", true, "官方角色、自己加的、已上架"},
		{"builder", "night", false, "已經是官方的"},
		{"builder", "base", false, "已經是官方的"},
		{"builder", "my-2", false, "還在設計中"},
		{"builder", "adult-1", false, "照片全是隱藏版（舊圖的編號看起來像，轉換器登記成 hidden）"},
		{"builder", "my-4", false, "照片全是隱藏版（舊圖的名字看起來像）"},
		{"pet", "my-1", false, "主人自己的角色"},
		{"pet", "base", false, "主人自己的角色"},
	} {
		if got := flag(dev, c.persona, c.outfit); got != c.want {
			t.Fatalf("%s 的 %s（%s）：promotable=%v，要 %v", c.persona, c.outfit, c.why, got, c.want)
		}
	}

	prod := newScene(t)
	seed(prod)
	prod.round()
	for _, c := range [][2]string{{"builder", "my-1"}, {"builder", "night"}, {"pet", "my-1"}} {
		if flag(prod, c[0], c[1]) {
			t.Fatalf("正式家不該有 promotable：%s 的 %s", c[0], c[1])
		}
	}
}
