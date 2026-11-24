package backupsweep

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 報告（BSW-6）：每次掃完寫兩份到狀態資料夾的 backup-sweep/：report.json（看板與指令讀）、report.md（給人與秘書看）。只留最新一次。

func reportDir(home string) string { return platform.RuntimeDir(home, "backup-sweep") }

// ReportPath 是 Markdown 報告的位置。
func ReportPath(home string) string { return filepath.Join(reportDir(home), "report.md") }

func jsonPath(home string) string { return filepath.Join(reportDir(home), "report.json") }

// saveReport 寫兩份報告（原子寫入）。
func saveReport(home string, rep *sdk.BackupSweepReport, loc *time.Location) error {
	if err := os.MkdirAll(reportDir(home), 0o755); err != nil {
		return platform.Unavailable("建不了放報告的資料夾")
	}
	rep.File = ReportPath(home)
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := platform.AtomicWrite(jsonPath(home), data, 0o644); err != nil {
		return platform.Unavailable("寫不了報告")
	}
	if err := platform.AtomicWrite(ReportPath(home), []byte(Markdown(rep, loc)), 0o644); err != nil {
		return platform.Unavailable("寫不了報告")
	}
	return nil
}

// loadReport 讀上一次的報告；還沒掃過回 nil。
func loadReport(home string) (*sdk.BackupSweepReport, error) {
	data, err := os.ReadFile(jsonPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, platform.Unavailable("讀不了上一次的備份掃描報告")
	}
	var rep sdk.BackupSweepReport
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, platform.Unavailable("上一次的備份掃描報告壞了，再掃一次就好")
	}
	return &rep, nil
}

// SizeText 是大小的白話（報告與指令共用）。
func SizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(s, "\n", " ")
}

func pathCell(it sdk.BackupSweepItem) string {
	p := "`" + strings.ReplaceAll(it.Path, "`", "'") + "`"
	if it.Dir {
		p += "（資料夾）"
	}
	return cell(p)
}

// Counts 是報告的數字摘要：已到期（可刪、在用、沒查完）。
func Counts(rep *sdk.BackupSweepReport) (deletable, inUse, unchecked int) {
	for _, it := range rep.Expired {
		switch it.State {
		case sdk.BackupSweepExpired:
			deletable++
		case sdk.BackupSweepInUse:
			inUse++
		default:
			unchecked++
		}
	}
	return
}

// Markdown 把報告寫成給人看的 Markdown。
func Markdown(rep *sdk.BackupSweepReport, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	var b strings.Builder
	del, used, unchecked := Counts(rep)
	b.WriteString("# 備份掃描報告\n\n")
	fmt.Fprintf(&b, "- 時間：%s（花了 %s）\n", rep.At.In(loc).Format("2006-01-02 15:04"), (time.Duration(rep.TookMs) * time.Millisecond).Round(time.Second))
	if rep.By != "" {
		fmt.Fprintf(&b, "- 誰叫的：%s\n", rep.By)
	}
	fmt.Fprintf(&b, "- 掃描範圍：%s\n", strings.Join(rep.Roots, "、"))
	fmt.Fprintf(&b, "- 「在用」查了：%s\n", strings.Join(rep.Checks, "、"))
	fmt.Fprintf(&b, "- 結果：已到期 %d 個（可刪 %d、在用 %d、沒查完 %d）、名字不合規 %d 個、期限內 %d 個（%s）、略過的例外 %d 筆\n",
		len(rep.Expired), del, used, unchecked, len(rep.Misnamed), rep.Current, SizeText(rep.CurrentBytes), len(rep.Skipped))
	if rep.Pruned > 0 {
		fmt.Fprintf(&b, "- 這個家的 runtime/backups/ 照保留規則清掉 %d 個舊備份（%s）\n", rep.Pruned, SizeText(rep.PrunedBytes))
	}
	b.WriteString("\n核心只出報告、不刪。主人或秘書看過再刪；刪之前照備份規則再用指令查一次有沒有在用。\n")

	section := func(title string, items []sdk.BackupSweepItem, keep func(sdk.BackupSweepItem) bool, header string, row func(sdk.BackupSweepItem) string) {
		var rows []string
		for _, it := range items {
			if keep(it) {
				rows = append(rows, row(it))
			}
		}
		fmt.Fprintf(&b, "\n### %s（%d）\n\n", title, len(rows))
		if len(rows) == 0 {
			b.WriteString("沒有。\n")
			return
		}
		b.WriteString(header)
		for _, r := range rows {
			b.WriteString(r)
		}
	}
	state := func(s string) func(sdk.BackupSweepItem) bool {
		return func(it sdk.BackupSweepItem) bool { return it.State == s }
	}
	b.WriteString("\n## 已到期\n")
	section("可刪", rep.Expired, state(sdk.BackupSweepExpired), "| 路徑 | 大小 | 日期 | 留到 | 查過 |\n|---|---|---|---|---|\n", func(it sdk.BackupSweepItem) string {
		return fmt.Sprintf("| %s | %s | %s | %s | %s |\n", pathCell(it), SizeText(it.Size), it.Date, it.Expires, cell(it.Checked))
	})
	section("在用（不要刪，先問）", rep.Expired, state(sdk.BackupSweepInUse), "| 路徑 | 大小 | 日期 | 留到 | 誰指到 |\n|---|---|---|---|---|\n", func(it sdk.BackupSweepItem) string {
		return fmt.Sprintf("| %s | %s | %s | %s | %s |\n", pathCell(it), SizeText(it.Size), it.Date, it.Expires, cell(it.UsedBy))
	})
	section("沒查完（不要刪）", rep.Expired, state(sdk.BackupSweepUnchecked), "| 路徑 | 大小 | 日期 | 留到 | 原因 |\n|---|---|---|---|---|\n", func(it sdk.BackupSweepItem) string {
		return fmt.Sprintf("| %s | %s | %s | %s | %s |\n", pathCell(it), SizeText(it.Size), it.Date, it.Expires, cell(it.Checked))
	})
	b.WriteString("\n## 名字不合規\n")
	section("要改名或刪", rep.Misnamed, func(sdk.BackupSweepItem) bool { return true }, "| 路徑 | 大小 | 最後修改 | 哪裡不對 | 建議 |\n|---|---|---|---|---|\n", func(it sdk.BackupSweepItem) string {
		return fmt.Sprintf("| %s | %s | %s | %s | %s |\n", pathCell(it), SizeText(it.Size), it.Date, cell(it.Problem), cell(it.Suggest))
	})
	b.WriteString("\n## 期限內\n\n")
	fmt.Fprintf(&b, "%d 個，共 %s（名字合規、還沒到期，不用動）。\n", rep.Current, SizeText(rep.CurrentBytes))
	b.WriteString("\n## 略過的例外\n")
	section("不算備份", rep.Skipped, func(sdk.BackupSweepItem) bool { return true }, "| 路徑 | 為什麼略過 |\n|---|---|\n", func(it sdk.BackupSweepItem) string {
		return fmt.Sprintf("| %s | %s |\n", pathCell(it), cell(it.Reason))
	})
	if len(rep.Errors) > 0 {
		b.WriteString("\n## 出錯\n\n")
		for _, e := range rep.Errors {
			b.WriteString("- " + e + "\n")
		}
	}
	if len(rep.Notes) > 0 {
		b.WriteString("\n## 提醒\n\n")
		for _, n := range rep.Notes {
			b.WriteString("- " + n + "\n")
		}
	}
	return b.String()
}
