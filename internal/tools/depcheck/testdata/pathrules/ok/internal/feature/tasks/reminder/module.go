package reminder

// 業務塊可以 import 任何底層、自己的子套件與同一塊的子套件。
import (
	_ "github.com/minglight/evergenie/internal/base/app"
	_ "github.com/minglight/evergenie/internal/base/timer"
	_ "github.com/minglight/evergenie/internal/feature/tasks"
	_ "github.com/minglight/evergenie/internal/feature/tasks/reminder/store"
)

type Module struct{}

func (Module) Routes() []string   { return nil }
func Commands() map[string]string { return nil }
