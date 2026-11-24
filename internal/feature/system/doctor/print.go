package doctor

import (
	"fmt"
	"strings"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// doctorGroupTitles 是輸出時每個 group 的標題，也決定輸出順序。
var doctorGroupTitles = []struct {
	group sdk.DoctorGroup
	title string
}{
	{sdk.DoctorGroupConfig, "設定"},
	{sdk.DoctorGroupSessions, "秘書的 session"},
	{sdk.DoctorGroupPackages, "套件"},
	{sdk.DoctorGroupPermissions, "權限"},
	{sdk.DoctorGroupSchedule, "排程"},
	{sdk.DoctorGroupData, "資料"},
	{sdk.DoctorGroupPush, "推播"},
}

// Print 印健檢結果：每個 group 一段，每條一行（✓／!／✗＋說明），有下一步的下一行縮排寫「→ …」，最後一行摘要。
// `doctor` 指令用它；`config set`（settings）、`trust diff`（trust）也印同樣的格式，業務模組之間不互相 import，
// 由 registry 把 Print 傳進它們的 Commands（2026-11-01 從 base 的 cmdline 搬回來）。
func Print(e *cmdline.Env, checks []sdk.DoctorCheck) error {
	var b strings.Builder
	ok, warn, bad := 0, 0, 0
	for _, g := range doctorGroupTitles {
		first := true
		for _, c := range checks {
			if c.Group != g.group {
				continue
			}
			if first {
				fmt.Fprintf(&b, "【%s】\n", g.title)
				first = false
			}
			mark := "✓"
			switch c.Severity {
			case sdk.DoctorSeverityWarn:
				mark, warn = "!", warn+1
			case sdk.DoctorSeverityError:
				mark, bad = "✗", bad+1
			default:
				ok++
			}
			fmt.Fprintf(&b, "  %s %s\n", mark, c.Detail)
			if c.Fix != "" {
				fmt.Fprintf(&b, "    → %s\n", c.Fix)
			}
		}
		if !first {
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "%d 項正常、%d 項注意、%d 項要處理\n", ok, warn, bad)
	_, err := e.Out.Write([]byte(b.String()))
	return err
}
