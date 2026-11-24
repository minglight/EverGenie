package main

import (
	"fmt"

	"github.com/minglight/evergenie/internal/base/engine"
	"github.com/minglight/evergenie/internal/base/engine/registry"
	"github.com/minglight/evergenie/sdk"
)

// engineClaims 讓 pkg 知道哪些附程式的套件已經由 engine 監督（內建的引擎執行檔），pkg 只列出它們的狀態（PKG-N3）。
type engineClaims struct{ engines *engine.Module }

func (c engineClaims) Claim(binary string) (sdk.PackageState, string, bool) {
	id := ""
	for engineID, exe := range registry.Executables() {
		if exe == binary {
			id = engineID
		}
	}
	if id == "" {
		return "", "", false
	}
	for _, s := range c.engines.Statuses() {
		if s.ID != id {
			continue
		}
		switch {
		case s.Ready:
			return sdk.PackageStateRunning, "", true
		case !s.Found:
			return sdk.PackageStateFailed, fmt.Sprintf("找不到引擎「%s」的執行檔", id), true
		default:
			return sdk.PackageStateBuilding, "", true
		}
	}
	return sdk.PackageStateStopped, "", true // 沒有帳號用這個引擎
}
