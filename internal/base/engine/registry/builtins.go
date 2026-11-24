// Package registry contains the built-in executable names, without behavior branches.
package registry

import (
	"sort"

	"github.com/minglight/evergenie/internal/base/platform"
)

// Default 是新家第一個帳號用的引擎（`evergenie init`）；核心其他地方不寫死引擎 id。
const Default = "claude"

// testOnly 是只給測試用的引擎，不列給使用者選。
const testOnly = "fake"

// Executables 的 "fake" 是給瀏覽器 e2e 用的假引擎；正式安裝沒有這個執行檔，帳號沒用到就不會啟動。
func Executables() map[string]string {
	return map[string]string{"claude": platform.ProductName + "-engine-claude", testOnly: platform.ProductName + "-engine-fake"}
}

// Engines 是使用者可以選的引擎 id（排好序，不含測試用的）。
func Engines() []string {
	var out []string
	for id := range Executables() {
		if id != testOnly {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
