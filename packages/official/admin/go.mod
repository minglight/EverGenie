module github.com/minglight/evergenie/packages/official/admin

go 1.24

require github.com/minglight/evergenie/sdk v0.0.0

// 開發時指到同一個 repo 的 sdk（go.work 也會用本機的 sdk）；只 import sdk，不 import internal/（PKC-N1）。
replace github.com/minglight/evergenie/sdk => ../../../sdk
