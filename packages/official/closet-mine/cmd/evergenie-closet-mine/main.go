// evergenie-closet-mine 是官方套件「上傳衣服與角色」的行程（P6-4c）：由 EverGenie 核心啟動（不帶參數），報到後在背景處理
// closet-mine.request：把使用者上傳的圖驗證後複製進 personas/mine/，並登記到衣櫃的目錄。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/minglight/evergenie/packages/official/closet-mine/internal/run"
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
