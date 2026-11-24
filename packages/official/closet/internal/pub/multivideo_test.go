package pub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildPlanNumbersVideosConsecutively(t *testing.T) {
	ph := Photo{N: 1, Src: "/s/a.png", Ext: ".png", Videos: []string{"/s/a+2.mp4", "/s/a+3.mp4"}} // 照片紀錄的 clip 2、3：公開編號照清單位置
	plan := BuildPlan(
		[]OutfitInput{{Persona: "athena", Outfit: "base", Owned: true, Photos: []Photo{ph}}},
		[]Wearing{{Persona: "athena", Outfit: "base", Photo: ph}},
	)
	for _, want := range []string{"o/athena/base/1.mp4", "o/athena/base/1+2.mp4", "worn/athena.mp4", "worn/athena+2.mp4"} {
		if _, ok := plan[want]; !ok {
			t.Errorf("計畫缺 %s：%v", want, plan)
		}
	}
	if _, ok := plan["worn/athena+3.mp4"]; ok {
		t.Error("只有 2 支，不該有 +3")
	}
	if plan["worn/athena+2.mp4"].Src != "/s/a+3.mp4" {
		t.Errorf("公開編號照清單位置：%+v", plan["worn/athena+2.mp4"])
	}
}

// 含 + 的目的路徑要能走過 Reconcile（以前 SafeName 不收 +，第 2 支起的動畫全被擋掉，review 抓到）；
// 換衣服後舊的 worn/<人設>+k.mp4 要被清掉（圖是這套、影片不能是別套，DEV-97）。
func TestReconcileExtraVideosAndPrune(t *testing.T) {
	src := t.TempDir()
	for _, f := range []string{"a.mp4", "a+2.mp4", "b.mp4"} {
		write(t, filepath.Join(src, f), []byte("mp4-"+f))
	}
	dir := filepath.Join(t.TempDir(), "public")
	res, err := Reconcile(dir, Plan{
		"o/athena/base/1.mp4":   {Src: filepath.Join(src, "a.mp4")},
		"o/athena/base/1+2.mp4": {Src: filepath.Join(src, "a+2.mp4")},
		"worn/athena.mp4":       {Src: filepath.Join(src, "a.mp4")},
		"worn/athena+2.mp4":     {Src: filepath.Join(src, "a+2.mp4")},
	})
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("對帳：%v %+v", err, res.Errors)
	}
	for _, rel := range []string{"o/athena/base/1+2.mp4", "worn/athena+2.mp4"} {
		if !res.Present[rel] {
			t.Errorf("%s 沒放出來", rel)
		}
	}
	// 換成只有一支的衣服：+2 要消失。
	res, err = Reconcile(dir, Plan{"worn/athena.mp4": {Src: filepath.Join(src, "b.mp4")}})
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("換衣服：%v %+v", err, res.Errors)
	}
	if _, err := os.Stat(filepath.Join(dir, "worn", "athena+2.mp4")); err == nil {
		t.Fatal("換衣服後不該留著舊的 worn/athena+2.mp4")
	}
}
