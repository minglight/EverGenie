package sdk

// ProductName 是產品名：指令與核心執行檔 bin/<名>、官方套件的執行檔 <名>-<套件>、指標檔 <名>.local.toml、
// 預設的狀態資料夾 ~/.<名>/state、服務名稱 com.<名>.serve 都從它來（核心用 platform.ProductName，就是這一個）。
// 改名（DEV-158）照 scripts/install.sh 開頭「名稱與網址」那段的清單改；cmd/evergenie 的 TestProductNameSameEverywhere 比對每一處。
const ProductName = "evergenie"
