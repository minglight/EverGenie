package app

import _ "github.com/minglight/evergenie/internal/base/platform"

// app 是登記介面的定義與合併處，例外。
type App struct{}

func (a *App) Commands() map[string]string { return nil }
func (a *App) Routes() []string            { return nil }
