package main

import (
	"fmt"
	"io"
	"os"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// applyPointerHome：沒設 $EVERGENIE_HOME 時，從執行檔所在 checkout 的指標檔（evergenie.local.toml）找家，設成 $EVERGENIE_HOME，
// 之後各指令照原本的順序（--home 旗標 > $EVERGENIE_HOME）就會用它；連 API socket 也跟著走這個家。
// 找家的順序因此是：--home > $EVERGENIE_HOME > 指標檔 > 各指令原本的行為。指標檔寫壞了只警告，不擋指令。
func applyPointerHome(exe string, errOut io.Writer) {
	if sdk.Getenv("EVERGENIE_HOME") != "" {
		return
	}
	home, err := platform.PointerHome(exe)
	if err != nil {
		fmt.Fprintln(errOut, "注意：", err)
		return
	}
	if home != "" {
		os.Setenv("EVERGENIE_HOME", home)
	}
}
