package platform

import "github.com/minglight/evergenie/sdk"

// ProductName 是產品名（PLT-26；正本在 sdk.ProductName，sdk 讀家的佈局也要用）。執行檔、指標檔、預設的狀態資料夾、服務名稱都從它來，
// 不要在別處另寫一次 "evergenie"；scripts/install.sh 的 PRODUCT、Makefile 與 scripts/dist.sh 的執行檔名要跟它一樣
// （cmd/evergenie 的 TestProductNameSameEverywhere）。
const ProductName = sdk.ProductName

// CoreExe 是核心執行檔在 bin/ 底下的檔名（原始碼版 <repo>/bin/<名>，下載版 <app>/versions/<版本>/bin/<名>）。
const CoreExe = ProductName
