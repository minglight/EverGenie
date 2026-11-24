// evergenie-admin 是官方開發工坊套件（admin）的行程：由 EverGenie 核心啟動（不帶參數），報到後在背景
// 定時整理發版狀態與開發家狀態寫進 vault，並處理頁面寫的重新整理請求（admin.request）。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/minglight/evergenie/packages/official/admin/internal/run"
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
