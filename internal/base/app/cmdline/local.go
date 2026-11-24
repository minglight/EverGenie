package cmdline

import "strings"

// LocalFlags 取出 --name 值（或 --name=值）與開關；其餘留在 rest。
// 給「本機指令」用（不經 API、不用 Spec 填請求欄位的指令，例如 upgrade）；認不得的旗標或缺值回 ok=false。
func LocalFlags(args []string, withValue, switches []string) (vals map[string]string, on map[string]bool, rest []string, ok bool) {
	vals, on = map[string]string{}, map[string]bool{}
	isVal, isSwitch := map[string]bool{}, map[string]bool{}
	for _, n := range withValue {
		isVal[n] = true
	}
	for _, n := range switches {
		isSwitch[n] = true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			rest = append(rest, a)
			continue
		}
		name, value, has := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		switch {
		case isSwitch[name] && !has:
			on[name] = true
		case isVal[name] && has:
			vals[name] = value
		case isVal[name] && i+1 < len(args):
			i++
			vals[name] = args[i]
		default:
			return nil, nil, nil, false
		}
	}
	return vals, on, rest, true
}
