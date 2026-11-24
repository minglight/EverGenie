package migrate

// 工具不受限：可以 import 任何業務塊與底層。
import (
	_ "github.com/minglight/evergenie/internal/base/vault"
	_ "github.com/minglight/evergenie/internal/feature/reminders"
	_ "github.com/minglight/evergenie/internal/feature/tasks"
)
