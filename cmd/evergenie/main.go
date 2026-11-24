package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/update"
)

func main() {
	update.HomeMigrations = homeMigrations()
	update.PackageMigrations = packageMigrations
	update.MovedDuties = movedDuties
	if len(os.Args) == 1 {
		fmt.Println("evergenie dev")
		return
	}
	if os.Args[1] == "version" {
		// 印出這個執行檔的核心版本（升級退版時用來確認 bin/ 與 bin.prev/ 是哪一版，REQ-277）。
		fmt.Println(platform.Version())
		return
	}
	// 資料指令的 --home（DEV-88）：明講要連哪個家，比 $EVERGENIE_HOME 與 $EVERGENIE_API_SOCKET 都優先；serve／init／service／tailscale 自己處理。
	args, err := useHomeFlag(os.Args[1:], os.Setenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if exe, err := os.Executable(); err == nil {
		applyPointerHome(exe, os.Stderr)
	}
	if len(args) > 0 && args[0] == "serve" {
		os.Exit(serve(args[1:], os.Stdout, os.Stderr))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	featureCmds, err := featureCommands()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		stop()
		os.Exit(1)
	}
	code := runCLI(ctx, args, cmdline.Dial(), cmdline.IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}, featureCmds)
	stop()
	os.Exit(code)
}
