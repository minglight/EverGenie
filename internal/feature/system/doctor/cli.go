package doctor

import (
	"context"
	"errors"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// OfflineCheck 是 `evergenie doctor` 不需要 serve 就能多做的一項檢查（例如 launchd 有沒有裝）：
// 依賴 host 的事（launchctl、plist）不在 doctor 裡，由 registry 傳給 Commands。home 是要檢查的家（`--home` 或 $EVERGENIE_HOME），可能是空的。
type OfflineCheck func(ctx context.Context, home string) []sdk.DoctorCheck

// Commands 登記 doctor 的指令（套件層級函式，指令行程只是 client，不建 Module）。extra 是額外的離線檢查，接在內建的離線檢查之後。
func Commands(extra ...OfflineCheck) map[string]app.Command {
	return map[string]app.Command{"doctor": {Usage: "doctor [--json] [--home <目錄>]    健康檢查（只報告，不修復；有要處理的項目退出碼 1）", Group: cmdline.GroupSystem, Run: func(e *cmdline.Env, args []string) error { return doctor(e, args, extra) }}}
}

// doctor：evergenie doctor [--json] [--home <家>]。只報告，不修復；有 error 退出碼 1。
func doctor(e *cmdline.Env, args []string, extra []OfflineCheck) error {
	asJSON, home := false, ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			asJSON = true
		case a == "--home" && i+1 < len(args):
			i++
			home = args[i]
		case strings.HasPrefix(a, "--home="):
			home = strings.TrimPrefix(a, "--home=")
		default:
			return cmdline.Usagef("用法：evergenie doctor [--json] [--home <家>]")
		}
	}
	if home == "" {
		home = envHome()
	}
	checks := Collect(e, home, extra)

	if asJSON {
		if err := e.Line(&sdk.SystemDoctorResponse{Checks: checks}); err != nil {
			return err
		}
	} else if err := Print(e, checks); err != nil {
		return err
	}
	for _, c := range checks {
		if c.Severity == sdk.DoctorSeverityError {
			return cmdline.ErrReported
		}
	}
	return nil
}

// Collect 收齊 `evergenie doctor` 的每一條：不用服務就能看的（OfflineChecks＋extra），服務在就再加線上的。
// 回報包（`evergenie report`）也用它，兩邊看到同一份。home 空＝不知道家在哪（只回那一條紅字）。
func Collect(e *cmdline.Env, home string, extra []OfflineCheck) []sdk.DoctorCheck {
	client, noSocket := e.Client, e.NoSocket
	if home != "" && noSocket {
		client = sdk.Dial(platform.RuntimeDir(home, "sockets", "api.sock"))
		noSocket = false
	}
	var checks []sdk.DoctorCheck
	checks = append(checks, OfflineChecks(e.Ctx, home)...)
	for _, f := range extra {
		checks = append(checks, f(e.Ctx, home)...)
	}
	if noSocket {
		return append(checks, serveDown())
	}
	res, err := sdk.NewSystemClient(client).Doctor(e.Ctx, &sdk.SystemDoctorRequest{})
	if err != nil {
		var se *sdk.Error
		if errors.As(err, &se) {
			return append(checks, sdk.DoctorCheck{ID: "serve.doctor", Group: sdk.DoctorGroupConfig, Severity: sdk.DoctorSeverityError,
				Detail: "EverGenie 有在跑，但線上檢查失敗：" + se.Message, Fix: "看 runtime/logs；必要時重啟 evergenie serve"})
		}
		return append(checks, serveDown())
	}
	return append(checks, res.Checks...)
}

func envHome() string { return strings.TrimSpace(sdk.Getenv("EVERGENIE_HOME")) }

func serveDown() sdk.DoctorCheck {
	return sdk.DoctorCheck{ID: "serve.down", Group: sdk.DoctorGroupConfig, Severity: sdk.DoctorSeverityError,
		Detail: "EverGenie 沒在跑", Fix: ServeDownFix}
}

// ServeDownFix 是「EverGenie 沒在跑」的下一步：不懂電腦的人也做得到（DEV-153）。指令留在後半給秘書與開發者。
const ServeDownFix = "先等一分鐘，它當掉會自己重開；還是不行，在這台 Mac 打開「" + platform.RescueName + "」（按 ⌘＋空白鍵搜尋這個名字，點兩下），它會檢查、重開服務。會用指令的人：evergenie service restart"
