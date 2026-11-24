package notify

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// push init：替這個家產生手機推播要用的金鑰（VAPID），寫進秘密檔並接到 home.toml 的 [secrets]。
// 不用開 serve；不印金鑰的值（REQ-120）。已經有就不動，要重做加 --force。

const pushUsage = "evergenie push init [--home <家>] [--subject mailto:你的信箱] [--force]"

// defaultPushSubject 是沒給 --subject 時的聯絡方式；推播服務只看格式，之後想換直接改秘密檔的 vapid_subject。
const defaultPushSubject = "mailto:evergenie@example.com"

func pushCommand(r *cmdline.Env, args []string) error {
	fs := flag.NewFlagSet("push init", flag.ContinueOnError)
	fs.SetOutput(r.Err)
	homeFlag := fs.String("home", strings.TrimSpace(sdk.Getenv("EVERGENIE_HOME")), "家")
	subjectFlag := fs.String("subject", "", "聯絡方式")
	force := fs.Bool("force", false, "重新產生")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return cmdline.Usagef("用法：" + pushUsage)
	}
	if *homeFlag == "" {
		return platform.Invalid("home", "不知道家在哪裡：用 --home 指定家的資料夾")
	}
	home, err := filepath.Abs(*homeFlag)
	if err != nil {
		return err
	}
	subject := strings.TrimSpace(*subjectFlag)
	if subject == "" {
		subject = defaultPushSubject
	}
	if !strings.HasPrefix(subject, "mailto:") && !strings.HasPrefix(subject, "https://") {
		fmt.Fprintln(r.Err, "--subject 要以 mailto: 或 https:// 開頭，例如 mailto:you@example.com")
		return cmdline.ExitCodeError{Code: 2}
	}
	path, made, err := EnsurePushKeys(home, subject, *force)
	if err != nil {
		var pe *platform.Err
		if !errors.As(err, &pe) || pe.Code == sdk.ErrorCodeInternal || pe.Message == "" {
			return err // 家的設定讀不到這類：照舊交給外層印
		}
		fmt.Fprintln(r.Err, pe.Message)
		return cmdline.ErrReported
	}
	if !made {
		fmt.Fprintln(r.Out, "這個家已經有推播金鑰了，沒有動它。要重做請加 --force（重做後，所有裝置都要重新開啟推播）。")
		return nil
	}
	fmt.Fprintln(r.Out, "推播金鑰做好了："+path)
	fmt.Fprintln(r.Out, "接下來：到手機的看板「設定 → 裝置推播」按「在這台裝置開啟推播」（不用重開服務；手機還連不到看板的話，先在這台 Mac 的看板「設定 → 手機連線」照步驟做）。")
	if subject == defaultPushSubject {
		fmt.Fprintln(r.Out, "聯絡信箱用的是預設值；想換成自己的，改秘密檔的 vapid_subject，或重做時加 --subject mailto:你的信箱 --force。")
	}
	return nil
}
