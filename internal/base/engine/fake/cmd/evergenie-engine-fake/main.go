// evergenie-engine-fake 是給瀏覽器 e2e 用的假引擎執行檔：`serve` 在引擎 socket 上提供腳本引擎。
// 正式安裝不會有這個檔案；home.toml 的帳號沒用到 "fake" 引擎就不會被啟動。
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "serve" {
		fmt.Fprintln(stderr, "用法：evergenie-engine-fake serve")
		return 2
	}
	home := sdk.Getenv("EVERGENIE_HOME")
	if home == "" {
		fmt.Fprintln(stderr, "找不到家的位置（環境變數 EVERGENIE_HOME）")
		return 1
	}
	apiSocket := sdk.Getenv("EVERGENIE_API_SOCKET")
	if apiSocket == "" {
		apiSocket = sdk.RuntimeDir(home, "sockets", "api.sock")
	}
	engineSocket := sdk.Getenv("EVERGENIE_ENGINE_SOCKET")
	if engineSocket == "" {
		engineSocket = sdk.RuntimeDir(home, "sockets", "engine-fake.sock")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	engine := fake.NewScriptedContext(ctx, sdk.NewEngineHostClient(sdk.Dial(apiSocket)))
	if err := sdk.Serve(ctx, engineSocket, map[string]any{"engine": engine}); err != nil && ctx.Err() == nil {
		fmt.Fprintf(stderr, "serve 結束：%v\n", err)
		return 1
	}
	return 0
}
