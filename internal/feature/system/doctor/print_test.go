package doctor

import (
	"bytes"
	"testing"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// 健檢輸出：每個群組一段（照固定順序）、✓／!／✗＋說明、有下一步的下一行寫「→ …」、最後一行摘要。
// Guards: REQ-117
func TestPrintGroupsAndSummary(t *testing.T) {
	ok := func(g sdk.DoctorGroup, detail string) sdk.DoctorCheck {
		return sdk.DoctorCheck{ID: string(g) + ".x", Group: g, Ok: true, Severity: sdk.DoctorSeverityOk, Detail: detail}
	}
	var out bytes.Buffer
	err := Print(&cmdline.Env{Out: &out}, []sdk.DoctorCheck{
		ok(sdk.DoctorGroupSchedule, "排程都載入了"),
		{ID: "data.invalid", Group: sdk.DoctorGroupData, Severity: sdk.DoctorSeverityError, Detail: "task SEC-9 壞了", Fix: "打開那個檔案修正"},
		{ID: "push.status", Group: sdk.DoctorGroupPush, Severity: sdk.DoctorSeverityWarn, Detail: "還沒有裝置開啟推播"},
		ok(sdk.DoctorGroupConfig, "秘書都對得上"),
	})
	want := "【設定】\n  ✓ 秘書都對得上\n\n" +
		"【排程】\n  ✓ 排程都載入了\n\n" +
		"【資料】\n  ✗ task SEC-9 壞了\n    → 打開那個檔案修正\n\n" +
		"【推播】\n  ! 還沒有裝置開啟推播\n\n" +
		"2 項正常、1 項注意、1 項要處理\n"
	if err != nil || out.String() != want {
		t.Fatalf("輸出不對：\n%s\n--- 要的是 ---\n%s", out.String(), want)
	}
}
