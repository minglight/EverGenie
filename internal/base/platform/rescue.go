package platform

// RescueName 是「修復」入口的名字（DEV-153）：安裝時放在主人的「應用程式」資料夾（~/Applications/<RescueName>.command），
// 點兩下就檢查、重開服務、把結果開給他看。看板打不開時的提示（離線頁、健檢的下一步、秘書的急救規則）都叫他找這個名字，
// 所以名字只在這裡定；scripts/install.sh 與離線頁（web/apps/board/public/sw.js）寫死同一個字，由測試對齊。
// 用小寫的產品名（不是畫面上的 EverGenie）：它同時是「應用程式」裡那個可以點兩下執行的檔名，跟指令、執行檔同一套寫法（DEV-158）。
const RescueName = ProductName + " 修復"

// RescueFile 是修復入口的檔名。
const RescueFile = RescueName + ".command"
