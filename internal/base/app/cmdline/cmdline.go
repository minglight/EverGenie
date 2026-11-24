package cmdline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// DefaultBy 是沒有 $EVERGENIE_AGENT 時的操作者身分（人在 shell 下指令）。
const DefaultBy = "owner(cli)"

// IO 是指令的標準輸入輸出。
type IO struct {
	In       io.Reader
	Out, Err io.Writer
}

// Env 是一次指令執行的環境。
type Env struct {
	Ctx      context.Context
	Client   *sdk.Client
	NoSocket bool   // 沒有任何線索知道 API socket 在哪
	By       string // 操作者：$EVERGENIE_AGENT，沒有就是 DefaultBy
	Cmd      string // 這次執行的指令名稱（例 "reminders add"）；子指令的 --help 用
	In       io.Reader
	Out, Err io.Writer
}

// Command 是一個指令：Run 是實作（args 是指令名稱之後的參數）；Usage 是 help 裡這個頂層指令的用法
// （第一行接在指令名稱位置，多行時後面幾行是說明），同一個頂層指令（例如 tasks add、tasks list 都算 tasks）
// 只在其中一筆寫；Group 決定 help 裡排在哪一段。沒有寫 Usage 的頂層指令，help 自動列「名稱 子指令|子指令」。
type Command struct {
	Run   func(e *Env, args []string) error
	Usage string
	Group Group
}

// SocketPath 找 API socket。找家的順序（跟 platform/pointer.go 的註解一致）：明講的家（--home 會換成 $EVERGENIE_HOME）＞ $EVERGENIE_API_SOCKET ＞ 指標檔（開機時已換成 $EVERGENIE_HOME）。
//   - $EVERGENIE_API_SOCKET 沒設：用 $EVERGENIE_HOME 的 API socket（照家的佈局，第 1 版是 <家>/runtime/sockets/api.sock）；都沒有回空字串。
//   - $EVERGENIE_API_SOCKET 有設，也有 $EVERGENIE_HOME：兩個指向同一個家（秘書 session 的預設）就用 socket；
//     socket 是標準位置（<家>/runtime/sockets/api.sock）但屬於別的家，代表有人明講要用 $EVERGENIE_HOME 那個家，照 $EVERGENIE_HOME 的 socket。
//     socket 不是標準位置（測試、特別的排法）就照 $EVERGENIE_API_SOCKET，不猜（DEV-88）。
func SocketPath() string {
	sock, home := sdk.Getenv("EVERGENIE_API_SOCKET"), sdk.Getenv("EVERGENIE_HOME")
	if sock != "" {
		if _, other := OtherHome(); other {
			return homeSocket(home)
		}
		// 搬家前就開著的 session 帶著第 1 版的 socket（<家>/runtime/sockets/api.sock）：家已是第 2 版就改連新位置（DEV-145）。
		if owner := socketHome(sock); owner != "" && platform.LayoutFor(owner).V2 {
			return homeSocket(owner)
		}
		return sock
	}
	if home != "" {
		return homeSocket(home)
	}
	return ""
}

// homeSocket 是家的 API socket（照 home.toml 的 [layout]；第 2 版在狀態資料夾的 runtime/ 底下）。
func homeSocket(home string) string { return platform.RuntimeDir(home, "sockets", "api.sock") }

// socketHome 是標準位置的 socket（<家>/runtime/sockets/api.sock）所屬的家；不是標準位置回空字串。
func socketHome(sock string) string {
	tail := string(filepath.Separator) + filepath.Join("runtime", "sockets", "api.sock")
	if !strings.HasSuffix(sock, tail) {
		return ""
	}
	return strings.TrimSuffix(sock, tail)
}

func samePath(a, b string) bool {
	clean := func(p string) string {
		p = filepath.Clean(p)
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return real
		}
		return p
	}
	return clean(a) == clean(b)
}

// OtherHome：這次要連的家（$EVERGENIE_HOME）跟 $EVERGENIE_API_SOCKET 所屬的家不同（socket 是標準位置才比）。
// 回傳要連的家與是否不同；呼叫的人拿來提醒「這次用的是另一個家」。
func OtherHome() (string, bool) {
	sock, home := sdk.Getenv("EVERGENIE_API_SOCKET"), sdk.Getenv("EVERGENIE_HOME")
	if sock == "" || home == "" {
		return "", false
	}
	if samePath(sock, homeSocket(home)) {
		return "", false
	}
	owner := socketHome(sock)
	if owner == "" || samePath(owner, home) {
		return "", false
	}
	return filepath.Clean(home), true
}

// Dial 依環境變數連 API；找不到 socket 的線索時回 nil（Run 會在需要連線時印白話訊息）。
func Dial() *sdk.Client {
	if p := SocketPath(); p != "" {
		return sdk.Dial(p)
	}
	return nil
}

// Run 執行一個指令，回結束碼：0 成功、1 一般錯誤、2 用法錯誤，或指令用 ExitCodeError 指定的碼。
// cmds 是合併後的指令表；help 與不認得指令時印的用法由 Help(cmds) 組出來，footer 接在最後（可以是空的）。
// 錯誤訊息直接顯示 API 回的白話 message，沒有堆疊（CLI-1）。
func Run(ctx context.Context, args []string, client *sdk.Client, stdio IO, cmds map[string]Command, footer string) int {
	e := &Env{Ctx: ctx, Client: client, By: sdk.Getenv("EVERGENIE_AGENT"), In: stdio.In, Out: stdio.Out, Err: stdio.Err}
	if e.By == "" {
		e.By = DefaultBy
	}
	if e.In == nil {
		e.In = strings.NewReader("")
	}
	if e.Client == nil {
		e.NoSocket = true
		e.Client = sdk.Dial("")
	}
	usage := Help(cmds) + footer
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(e.Out, usage)
		return 0
	}
	fn, rest := Lookup(cmds, args)
	e.Cmd = strings.Join(args[:len(args)-len(rest)], " ")
	if fn == nil {
		fmt.Fprintf(e.Err, "不認得的指令：%s\n", strings.Join(args[:min(len(args), 2)], " "))
		fmt.Fprint(e.Err, usage)
		return 2
	}
	err := fn(e, rest)
	var ue *UsageError
	var xe ExitCodeError
	var he *HelpText
	switch {
	case err == nil:
		return 0
	case errors.As(err, &he):
		fmt.Fprint(e.Out, he.Text)
		return 0
	case errors.Is(err, ErrReported):
		return 1
	case errors.As(err, &xe):
		return xe.Code
	case errors.As(err, &ue):
		fmt.Fprintf(e.Err, "%s\n（evergenie help 看用法）\n", ue.Msg)
		return 2
	}
	fmt.Fprintln(e.Err, e.Message(err))
	return 1
}

// Lookup 在指令表裡找指令的實作。先比對「指令＋子指令」：quota 本身是指令，quota check 也是。
// 找不到回 nil。
func Lookup(cmds map[string]Command, args []string) (func(e *Env, args []string) error, []string) {
	if len(args) == 0 {
		return nil, nil
	}
	if len(args) >= 2 {
		if c, ok := cmds[args[0]+" "+args[1]]; ok && c.Run != nil {
			return c.Run, args[2:]
		}
	}
	if c, ok := cmds[args[0]]; ok && c.Run != nil {
		return c.Run, args[1:]
	}
	return nil, nil
}

// Merge 合併多個指令表；名稱重複、沒有實作、同一個頂層指令寫了兩份用法就回錯（兩個業務模組不能搶同一個指令）。
func Merge(sets ...map[string]Command) (map[string]Command, error) {
	all := map[string]Command{}
	usageBy := map[string]string{} // 頂層指令 → 寫了用法的那一筆
	for i, set := range sets {
		for name, c := range set {
			if name == "" || c.Run == nil {
				return nil, fmt.Errorf("第 %d 組指令裡有沒有名稱或沒有實作的指令", i+1)
			}
			if _, dup := all[name]; dup {
				return nil, fmt.Errorf("指令 %q 重複登記", name)
			}
			if c.Usage != "" {
				top := topWord(name)
				if prev, dup := usageBy[top]; dup {
					return nil, fmt.Errorf("指令 %q 的用法寫了兩份（%q、%q）", top, prev, name)
				}
				usageBy[top] = name
			}
			all[name] = c
		}
	}
	return all, nil
}

// Message 把錯誤轉成給人看的一行白話。
func (e *Env) Message(err error) string {
	var se *sdk.Error
	if errors.As(err, &se) {
		// 帶點的欄位（web.sites.url 這類 home.toml 的設定名）不是請求的欄位，沒有對應的命令列旗標，不提示。
		if se.Field != "" && !strings.Contains(se.Field, ".") && !strings.Contains(se.Message, se.Field) {
			return se.Message + "（欄位：" + se.Field + "，命令列是 --" + se.Field + "）" // 缺哪個欄位要說（DEV-89）
		}
		return se.Message
	}
	if e.Ctx.Err() != nil {
		return "已中斷"
	}
	var ue *url.Error
	var ne net.Error
	if errors.As(err, &ue) || errors.As(err, &ne) {
		if e.NoSocket {
			return "連不到 EverGenie：不知道它的位置（沒有設定 EVERGENIE_API_SOCKET 或 EVERGENIE_HOME），請確認 serve 在跑"
		}
		return "連不到 EverGenie，請確認 serve 在跑"
	}
	return err.Error()
}

// Line 印一行 JSON。
func (e *Env) Line(v any) error {
	enc := json.NewEncoder(e.Out)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
