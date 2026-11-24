// Package pub 管衣櫃的公開資料夾（<暫存>/packages/closet/public/，sdk.RuntimeDir；第 1 版是 <家>/runtime/）：照照片紀錄出清單、剪影、對帳。
// 這個資料夾是可重建的衍生檔（不是資料），只有 closet 行程寫（PKC-10）；核心唯讀送出（D-29）。
// DEV-160 S2 起不掃檔名：要放哪些檔由 closet.photo 紀錄決定（run 讀好給 Photo），舊檔名的規則只剩 photomigrate 認得。
package pub

import (
	"os"
	"strings"
)

const (
	// MaxPhotos：一套（一般版、隱藏版各自）最多列 12 張半身照。
	MaxPhotos = 12
	// MaxVideos：同一張照片最多 6 支動態（DEV-125）。
	MaxVideos = 6
	// MaxFileBytes：來源檔一律先確認是一般檔案、大小 ≤ 30 MB。
	MaxFileBytes = 30 << 20
)

// Photo 是一套衣服的一張照片（半身或全身）：照片紀錄讀好、找得到檔的樣子。
type Photo struct {
	N        int      // 公開的第幾張（＝紀錄的 order；一般版、隱藏版各自編）
	Src      string   // 靜態圖的完整路徑
	Ext      string   // 靜態圖的副檔名（小寫，含點）
	Videos   []string // 這張的影片（完整路徑），照 clip 排；公開的編號是它在清單裡的位置（1 起算），一定連續
	Hidden   bool     // 隱藏版的圖
	ID       string   // closet.photo 編號
	VideoIDs []string // 跟 Videos 一一對應的 closet.photo 編號
}

// RegularFile：一般檔案（不跟符號連結）而且大小在上限內。
func RegularFile(path string) (os.FileInfo, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return nil, false
	}
	return info, true
}

// SafeName：公開資料夾裡的路徑每一段都只能是小寫英文、數字、減號、底線、加號（第 2 支起的動畫 `<n>+k.mp4`，DEV-125），和一個副檔名，不含 . 開頭。
func SafeName(s string) bool {
	if s == "" || strings.HasPrefix(s, ".") || len(s) > 80 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '+':
		default:
			return false
		}
	}
	return !strings.Contains(s, "..")
}
