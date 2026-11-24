package timer

// 底層：沒有 HTTP、沒有指令；照「底層模組」表 import 比自己低的底層。
import (
	_ "github.com/minglight/evergenie/internal/base/app"
	_ "github.com/minglight/evergenie/internal/base/platform"
)

func Now() int { return 0 }
