package board

// 「設定 → 系統健康」多開的兩個方法（DEV-153，WEB-191）：doctor＝完整健檢（只讀、只報告），report＝產生問題回報的內容
// （只產生給主人看，不送出）。寫在這裡不寫進 server.go 的 allowedAPI 那一行，是為了少跟別的分支撞同一行。
func init() {
	allowedAPI["system"]["doctor"] = true
	allowedAPI["system"]["report"] = true
}
