package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/homebackup"
	"github.com/minglight/evergenie/internal/feature/system/update"
	"github.com/minglight/evergenie/sdk"
)

func serve(args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(errOut)
	home := flags.String("home", "", "home directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *home == "" {
		// 沒給 --home：$EVERGENIE_HOME（main 已經把程式資料夾的指標檔 evergenie.local.toml 補進來）。
		*home = strings.TrimSpace(sdk.Getenv("EVERGENIE_HOME"))
	}
	if *home == "" || flags.NArg() != 0 {
		fmt.Fprintln(errOut, "用法：evergenie serve --home <目錄>（或設 EVERGENIE_HOME、或在程式資料夾放 evergenie.local.toml）")
		return 2
	}

	// 有準備好的還原（整個家換成備份，DEV-153）：趁還沒開任何模組先換上去；換不成會自己換回原樣。
	if !applyPendingRestore(*home, out, errOut) {
		return 1
	}
	// 家的遷移（升級帶來的資料整理）：有沒跑過的就先跑，升級以外的路徑（直接換執行檔、退版後再升）也一致；失敗就不啟動（REQ-280）。
	if ran, err := update.RunHomeMigrations(*home); err != nil {
		fmt.Fprintln(errOut, "serve 拒絕啟動："+err.Error())
		return 1
	} else if len(ran) > 0 {
		fmt.Fprintf(out, "migrated %s\n", strings.Join(ran, ","))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// launchd 接的 serve.err.log／serve.out.log 不照日期分檔：超過上限就留一份、清空（PLT-29）。只動真的接在自己輸出上的檔。
	platform.WatchServiceLogs(ctx, platform.RuntimeDir(*home, "logs"))
	a, err := Build(*home)
	if err == nil {
		err = serveApp(ctx, a, out, 10*time.Second)
	}
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if homebackup.RestartRequested() && !restartForRestore(errOut) {
		return 1
	}
	return 0
}

// serveApp 啟動所有模組（依序，遇錯就把已啟動的反序停掉再回錯），等 ctx 結束再反序停掉；啟停順序與逾時由 app 管（每個模組 Stop 逾時 stopTimeout）。
// 全部啟動後依序印 start <名>，停完後依反序印 stop <名>（錯誤由 app.Stop 一起回）。
func serveApp(ctx context.Context, a *app.App, out io.Writer, stopTimeout time.Duration) error {
	a.WithStopTimeout(stopTimeout)
	if err := a.Start(ctx); err != nil {
		return err
	}
	for _, m := range a.Modules() {
		if _, err := fmt.Fprintf(out, "start %s\n", m.Name()); err != nil {
			return errors.Join(err, a.Stop(ctx))
		}
	}
	<-ctx.Done()
	// 關機要有自己的期限，不能用已經取消的 ctx（app.Stop 也不繼承它的取消）。
	err := a.Stop(ctx)
	mods := a.Modules()
	for i := len(mods) - 1; i >= 0; i-- {
		if _, werr := fmt.Fprintf(out, "stop %s\n", mods[i].Name()); werr != nil {
			return errors.Join(err, werr)
		}
	}
	return err
}
