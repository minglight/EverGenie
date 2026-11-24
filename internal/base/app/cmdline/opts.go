package cmdline

import (
	"os"
	"path/filepath"
	"strings"
)

// Opts 是手寫指令的旗標：--名稱 值，或只有名稱的開關；位置參數照順序收。
// 欄位跟請求型別對不起來的指令用它（能用 Spec 對進請求型別的指令用 Prepare）。
type Opts struct {
	Vals  map[string]string
	Flags map[string]bool
	Pos   []string
}

// ParseOpts 解析 args：withValue 是要接值的旗標名，switches 是只有名稱的開關；不認得的旗標回用法錯誤，--help 回 HelpText（印用法、結束 0）。
func ParseOpts(args []string, withValue, switches []string, usage string) (Opts, error) {
	o := Opts{Vals: map[string]string{}, Flags: map[string]bool{}}
	isVal := map[string]bool{}
	for _, n := range withValue {
		isVal[n] = true
	}
	isSwitch := map[string]bool{}
	for _, n := range switches {
		isSwitch[n] = true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			o.Pos = append(o.Pos, a)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		switch {
		case name == "help" && !isVal[name] && !isSwitch[name]:
			return o, &HelpText{Text: "用法：" + usage + "\n"} // 手寫的指令也能 --help（DEV-89）
		case isSwitch[name] && !hasValue:
			o.Flags[name] = true
		case isVal[name] && hasValue:
			o.Vals[name] = value
		case isVal[name] && i+1 < len(args):
			i++
			o.Vals[name] = args[i]
		default:
			return o, Usagef("用法：%s", usage)
		}
	}
	return o, nil
}

// ExpandHome 把開頭的 ~ 換成使用者的家目錄。
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
