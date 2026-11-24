package cmdline

import (
	"context"
	"io"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// 「要動這台電腦」的指令（service、preflight、tailscale、init…）：指令行程不碰系統（launchd、brew、Tailscale），
// 實作由 cmd/evergenie 組裝時指定（HostFunc），指令表裡只放把它包成 Command 的殼。

// HostRequest 是交給「本機指令」實作的東西。
type HostRequest struct {
	Home string // --home 或 $EVERGENIE_HOME；可能是空的
	Args []string
	// Engines 是 preflight 用的：問 serve 各帳號引擎回報的檢查（終端機工具、引擎程式、登入）；false＝serve 沒回應。
	// 用函式不用結果：--fix 裝完要再問一次，才不會印出修之前的舊結果。nil＝沒得問（例如 --no-socket）。
	Engines func() ([]sdk.PreflightCheck, bool)
	In      io.Reader
	Out     io.Writer
	Err     io.Writer
}

// HostFunc 是一個本機指令的實作，回結束碼：0 成功、2 用法錯誤、其他是失敗（訊息實作自己已經印了）。
type HostFunc func(ctx context.Context, r HostRequest) int

// EnvHome 回 $EVERGENIE_HOME（去掉空白）；沒設就是空字串。
func EnvHome() string { return strings.TrimSpace(sdk.Getenv("EVERGENIE_HOME")) }

// HostCommand 把本機指令實作包成 Command；fn 是 nil（例如單元測試沒接）就回「這個環境不支援」。
// prepare 是選用的：在呼叫實作前補 HostRequest（例如 preflight 向 serve 要各帳號引擎的檢查）。
func HostCommand(name string, fn HostFunc, prepare func(e *Env, r *HostRequest)) Command {
	return Command{Run: func(e *Env, args []string) error {
		if fn == nil {
			return Usagef("這個環境不支援 %s", name)
		}
		r := HostRequest{Home: EnvHome(), Args: args, In: e.In, Out: e.Out, Err: e.Err}
		if prepare != nil {
			prepare(e, &r)
		}
		switch code := fn(e.Ctx, r); code {
		case 0:
			return nil
		case 2:
			return Usagef("用法錯誤（evergenie help 看用法）")
		default:
			return ErrReported
		}
	}}
}

// SubHost 把 `service install` 這種「指令＋子指令」交給 fn，子指令放在 Args 的第一個。
func SubHost(name, sub string, fn HostFunc) Command {
	inner := HostCommand(name, fn, nil)
	return Command{Run: func(e *Env, args []string) error { return inner.Run(e, append([]string{sub}, args...)) }}
}
