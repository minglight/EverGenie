package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
)

// ownHomeFlag 是自己處理 --home 的指令（serve 開哪個家、init 建在哪、service／tailscale 對哪個家動作）：
// 全域的 --home 不碰它們，免得 init 吃到 $EVERGENIE_HOME（init 一定要明講，才不會蓋到現有的家）。
var ownHomeFlag = map[string]bool{"serve": true, "init": true, "service": true, "tailscale": true}

// extractHomeFlag 從資料指令的參數裡拿掉 `--home <家>`／`--home=<家>`（DEV-88）：回傳家的絕對路徑與剩下的參數。
// 沒寫 --home 回空字串、參數原樣；寫了沒給值回錯。呼叫的人把家設成 $EVERGENIE_HOME，這次指令就連那個家
// （$EVERGENIE_API_SOCKET 屬於別的家時改連這個家，見 cmdline.SocketPath）。
func extractHomeFlag(args []string) (string, []string, error) {
	if len(args) == 0 || ownHomeFlag[args[0]] {
		return "", args, nil
	}
	home, rest := "", make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--home":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return "", nil, platform.Invalid("home", "--home 後面要接家的資料夾")
			}
			i++
			home = args[i]
		case strings.HasPrefix(a, "--home="):
			home = strings.TrimPrefix(a, "--home=")
		default:
			rest = append(rest, a)
		}
	}
	if home == "" {
		if len(rest) != len(args) { // 寫了 --home= 但是空的
			return "", nil, platform.Invalid("home", "--home 後面要接家的資料夾")
		}
		return "", args, nil
	}
	home = platform.ExpandHome(home)
	abs, err := filepath.Abs(home)
	if err != nil {
		return "", nil, platform.Invalid("home", "家的路徑不對")
	}
	return abs, rest, nil
}

// useHomeFlag 套用 --home：設成 $EVERGENIE_HOME，回剩下的參數。
func useHomeFlag(args []string, setenv func(k, v string) error) ([]string, error) {
	home, rest, err := extractHomeFlag(args)
	if err != nil {
		return nil, fmt.Errorf("%s", errMessage(err))
	}
	if home != "" {
		if err := setenv("EVERGENIE_HOME", home); err != nil {
			return nil, err
		}
	}
	return rest, nil
}

func errMessage(err error) string {
	if pe, ok := err.(*platform.Err); ok && pe.Message != "" {
		return pe.Message
	}
	return err.Error()
}
