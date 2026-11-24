// internal/feature/inbox/overview/module.go — ai_docs/modules/inbox.md（REQ-109）。
package overview

import (
	"context"
	"sync"

	"github.com/minglight/evergenie/internal/base/app"
)

// Deps 是空的：首頁的每一段資料都由各業務模組的 Board() 登記，app 組裝時經 ConsumeBoard 交進來（2026-11-01，
// 原本的 Threads、Tasks 兩個來源改由 chat、tasks 自己登記 waiting_threads、waiting_tasks）。
type Deps struct{}

// Module 是首頁彙整：把各業務模組登記的首頁資料段（Board）合成一份總覽，算等你處理的總數。
// HTTP 在 http.go：sdk 的 web 服務裡只有 get_overview 由這裡登記（Mount.Methods）；選單徽章的數字也從 Overview 來（menu 業務模組）。
type Module struct {
	d     Deps
	mu    sync.Mutex
	slots []slot
	ready bool
}

var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
	_ app.BoardConsumer = (*Module)(nil)
)

// New 不做任何 I/O（MOD-N4）。
func New(d Deps) *Module { return &Module{d: d} }

func (m *Module) Name() string                { return "overview" }
func (m *Module) Start(context.Context) error { return nil }
func (m *Module) Stop(context.Context) error  { return nil }
