// evergenie-closet 是官方衣櫃套件的行程（P6-4b）：由 EverGenie 核心啟動（不帶參數），報到後在背景處理兌換請求、
// 輪播換衣，並維護公開資料夾（沒兌換的衣服不放真圖）。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/minglight/evergenie/packages/official/closet/internal/run"
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
