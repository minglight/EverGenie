package photomigrate

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// 舊檔名規則（DEV-160 S2 從 pub 搬來，只有轉換器還認得）。

// Guards: REQ-125
func TestScanPhotosConsecutive(t *testing.T) {
	d := t.TempDir()
	touch(t, d,
		"athena.png", "athena.jpg", // 同一張只認第一個存在的：png
		"athena~2.jpg", "athena~2.mp4", // 第 2 張有動態
		"athena~3.webp",
		"athena~5.png", // 4 缺了，5 不算
		"athena.locked.png", "athena.live.json", "athena.voice.mp4",
		"athena@night.png", "athena@night.mp4", // 只有動態沒關係，第 1 張有圖
		"athena@sea.mp4",   // 只有影片沒有圖：不算
		"athena~2.png.bak", // 不認
		"other/athena.png", // 子資料夾不讀
		"full/athena.png",
	)
	got := ScanPhotos(d, "athena", "base")
	if len(got) != 3 {
		t.Fatalf("base 要 3 張：%+v", got)
	}
	if got[0].Ext != ".png" || filepath.Base(got[0].Src) != "athena.png" || got[0].Video != "" {
		t.Errorf("第 1 張：%+v", got[0])
	}
	if got[1].Ext != ".jpg" || filepath.Base(got[1].Video) != "athena~2.mp4" {
		t.Errorf("第 2 張：%+v", got[1])
	}
	if got[2].Ext != ".webp" || got[2].N != 3 {
		t.Errorf("第 3 張：%+v", got[2])
	}
	night := ScanPhotos(d, "athena", "night")
	if len(night) != 1 || filepath.Base(night[0].Video) != "athena@night.mp4" {
		t.Errorf("night：%+v", night)
	}
	if got := ScanPhotos(d, "athena", "sea"); len(got) != 0 {
		t.Errorf("只有影片沒有圖：%+v", got)
	}
	// 符號連結的圖不算；超過 12 張只取 12 張。
	if err := os.Symlink(filepath.Join(d, "athena.png"), filepath.Join(d, "linked.png")); err != nil {
		t.Fatal(err)
	}
	if got := ScanPhotos(d, "linked", "base"); len(got) != 0 {
		t.Errorf("符號連結：%+v", got)
	}
	for n := 1; n <= 15; n++ {
		touch(t, d, photoName("many", n)+".png")
	}
	if got := ScanPhotos(d, "many", "base"); len(got) != MaxPhotos {
		t.Errorf("最多 12 張：%d", len(got))
	}
}

// Guards: DEV-125（一張照片可以有多支動態：<名>.mp4、<名>+2.mp4…，從 +2 連續、最多 6 支）
func TestScanPhotosExtraVideos(t *testing.T) {
	d := t.TempDir()
	touch(t, d,
		"athena.png", "athena.mp4", "athena+2.mp4", "athena+3.mp4", "athena+5.mp4", // +4 缺了，+5 不算
		"athena~2.png", "athena~2+2.mp4", // 第 2 張沒有第 1 支，只有 +2
		"athena@sea.png", "athena@sea+2.mp4",
	)
	got := ScanPhotos(d, "athena", "base")
	if len(got) != 2 {
		t.Fatalf("base 要 2 張：%+v", got)
	}
	if v := got[0].Videos(); len(v) != 3 || filepath.Base(v[0]) != "athena.mp4" || filepath.Base(v[2]) != "athena+3.mp4" {
		t.Errorf("第 1 張的動態：%v", v)
	}
	if v := got[1].Videos(); len(v) != 1 || filepath.Base(v[0]) != "athena~2+2.mp4" {
		t.Errorf("第 2 張只有 +2：%v", v)
	}
	if v := ScanPhotos(d, "athena", "sea")[0].Videos(); len(v) != 1 {
		t.Errorf("sea 的動態：%v", v)
	}
	e := t.TempDir()
	touch(t, e, "athena.png", "athena.mp4")
	for k := 2; k <= 9; k++ {
		touch(t, e, "athena+"+strconv.Itoa(k)+".mp4")
	}
	if v := ScanPhotos(e, "athena", "base")[0].Videos(); len(v) != MaxVideos {
		t.Errorf("最多 %d 支：%d", MaxVideos, len(v))
	}
}

// 隱藏版資料夾的照片不要求連續（可以只有第 2 張）。
func TestScanSlotsNotConsecutive(t *testing.T) {
	hid := t.TempDir()
	touch(t, hid, "amy@sea~2.jpg", "amy@sea~3.png", "amy@sea~3.mp4", "amy@sea~5.png", "amy@other.png")
	slots := ScanSlots(hid, "amy", "sea")
	if len(slots) != 3 || slots[2].Ext != ".jpg" || slots[3].Video == "" || slots[5].N != 5 {
		t.Fatalf("隱藏版要掃到第 2、3、5 張：%+v", slots)
	}
	if len(ScanSlots(hid, "Amy", "sea")) != 0 {
		t.Error("人設代號不合格回空")
	}
}
