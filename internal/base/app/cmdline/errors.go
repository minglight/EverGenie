package cmdline

import (
	"errors"
	"fmt"
	"strconv"
)

// ErrReported 表示指令已經把原因印完了，Run 只需要回結束碼 1，不要再印一行。
var ErrReported = errors.New("已回報")

// UsageError 是用法錯誤（結束碼 2），訊息直接給人看。
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// Usagef 建立用法錯誤。
func Usagef(format string, a ...any) error { return &UsageError{Msg: fmt.Sprintf(format, a...)} }

// ExitCodeError 讓指令回 0–2 以外的結束碼（例如 quota check 的「該停」「沒資料」「不在範圍」）。訊息指令自己已經印完了。
type ExitCodeError struct{ Code int }

func (e ExitCodeError) Error() string { return "exit " + strconv.Itoa(e.Code) }

// HelpText 是子指令的 --help：Run 把 Text 印到標準輸出，結束碼 0。
type HelpText struct{ Text string }

func (h *HelpText) Error() string { return h.Text }
