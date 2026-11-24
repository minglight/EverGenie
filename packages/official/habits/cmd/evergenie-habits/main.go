// evergenie-habits 是官方習慣套件的行程（P6-4a）：由 EverGenie 核心啟動（不帶參數），報到後在背景算習慣的點數。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/minglight/evergenie/packages/official/habits/internal/run"
	"github.com/minglight/evergenie/sdk"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := sdk.ServePackage(ctx, run.Program{Stderr: os.Stderr}); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
