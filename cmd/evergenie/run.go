package main

import (
	"context"
	"fmt"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// laterCommands 是還沒有的指令（舊系統的搬家工具已刪）。
var laterCommands = map[string]bool{"migrate": true, "cutover": true, "rollback": true, "delegate": true}

// runCLI 執行一個指令，回退出碼：0 成功、1 一般錯誤、2 用法錯誤。
// 錯誤訊息直接顯示 API 回的白話 message，沒有堆疊（CLI-1）。
//
// cmds 是合併後的指令表（featureCommands：各業務模組的 Commands()、角色庫指令、要動這台電腦的本機指令），
// 同一個指令名稱兩邊都有會在合併時就回錯。
func runCLI(ctx context.Context, args []string, client *sdk.Client, stdio cmdline.IO, cmds map[string]cmdline.Command) int {
	if len(args) > 0 && laterCommands[args[0]] {
		fmt.Fprintf(stdio.Err, "%s：還沒有這個指令（舊系統的搬家工具已刪）\n", args[0])
		return 2
	}
	code := cmdline.Run(ctx, args, client, stdio, cmds, helpFooter)
	if home, other := cmdline.OtherHome(); other && len(args) > 0 && args[0] != "help" && args[0] != "--help" && args[0] != "-h" && stdio.Err != nil {
		fmt.Fprintln(stdio.Err, otherHomeNote(home))
	}
	return code
}

// otherHomeNote 提醒這次的資料指令連的是另一個家（秘書 session 指定了別的家，DEV-88）：
// 寫進去的是 <標籤>，不是這個 session 自己的家。標籤讀不到就只印路徑。
func otherHomeNote(home string) string {
	label := home
	if h, err := platform.LoadHome(home); err == nil && h.Label() != "" {
		label = h.Label() + "，" + home
	}
	return "（這次連的是另一個家：" + label + "；不是這個對話自己的家）"
}

// helpFooter 接在各指令自己登記的用法後面（help 本身由 cmdline.Help 從指令表組出來）。
const helpFooter = "\nmigrate、cutover、rollback、delegate：還沒有這幾個指令（舊系統的搬家工具已刪）。\n"
