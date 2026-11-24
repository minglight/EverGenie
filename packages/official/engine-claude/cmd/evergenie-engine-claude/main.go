// evergenie-engine-claude 是 Claude 引擎套件的入口：`serve`、平台呼叫的 `hook <事件>`、`evergenie init` 問的 `account-defaults`（ENG-19）。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/minglight/evergenie/packages/official/engine-claude/internal/claude"
	"github.com/minglight/evergenie/sdk"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "用法：evergenie-engine-claude serve | hook <事件> | account-defaults")
		return 2
	}
	switch args[0] {
	case "hook":
		// hook 永遠 exit 0，連參數不對也一樣：平台不該因為我們出錯而卡住。
		if len(args) < 2 {
			return 0
		}
		return claude.RunHook(context.Background(), args[1], stdin, stdout, claude.HookDeps{})
	case "serve":
		return serve(stderr)
	case "account-defaults":
		// 新家第一個帳號的 engine_config（JSON 物件）：用現在這個環境登入的那個帳號（ECL-64）。
		m, err := claude.AccountDefaults(os.Getenv)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		data, err := json.Marshal(m)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintf(stderr, "不認得的子指令：%s\n", args[0])
	return 2
}

func serve(stderr io.Writer) int {
	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "找不到自己的執行檔：%v\n", err)
		return 1
	}
	cfg, err := claude.ServeConfigFromEnv(sdk.Getenv, bin)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := claude.Serve(ctx, cfg); err != nil && ctx.Err() == nil {
		fmt.Fprintf(stderr, "serve 結束：%v\n", err)
		return 1
	}
	return 0
}
