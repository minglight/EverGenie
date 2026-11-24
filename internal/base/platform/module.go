// internal/base/platform/module.go — ai_docs/modules/platform.md（REQ-118, REQ-119, REQ-120, REQ-3, REQ-32, REQ-62）。
package platform

import "context"

type Deps struct {
}
type Module struct{ d Deps }

func New(d Deps) *Module                          { return &Module{d: d} }
func (m *Module) Name() string                    { return "platform" }
func (m *Module) Start(ctx context.Context) error { return nil }
func (m *Module) Stop(ctx context.Context) error  { return nil }
