package homebackup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

const cliUsage = "backup create|status|show <檔>|restore <檔> --yes   整個家的備份與還原（<檔> 是備份檔的位置，或 backup status 列的檔名；備份只放在這台電腦）"

// Commands 登記整個家的備份指令（套件層級；指令行程只是 client）。頂層 backup 的用法說明寫在 settings 的 backup list 那一筆。
func Commands() map[string]app.Command {
	return map[string]app.Command{
		"backup create":  {Run: cliCreate},
		"backup status":  {Run: cliStatus},
		"backup show":    {Run: cliShow},
		"backup restore": {Run: cliRestore},
	}
}

func client(e *cmdline.Env) sdk.BackupAPI { return sdk.NewBackupClient(e.Client) }

func cliActor(e *cmdline.Env) *sdk.Actor { return &sdk.Actor{Name: e.By} }

func sizeText(n int64) string {
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

func timeText(t time.Time) string {
	if t.IsZero() {
		return "還沒有"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// KindLabel 是備份種類的白話。
func KindLabel(kind string) string {
	switch kind {
	case sdk.HomeBackupKindDaily:
		return "以前每天自動的"
	case sdk.HomeBackupKindManual:
		return "手動"
	case sdk.HomeBackupKindBeforeRestore:
		return "還原前留的"
	}
	return kind
}

func cliCreate(e *cmdline.Env, args []string) error {
	if len(args) != 0 {
		return cmdline.Usagef("用法：evergenie %s", cliUsage)
	}
	r, err := client(e).Create(e.Ctx, &sdk.BackupCreateRequest{By: cliActor(e)})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(e.Out, "備份好了：%s（%s）\n", r.File.Name, sizeText(r.File.Size)); err != nil {
		return err
	}
	for _, s := range r.Sections {
		if _, err := fmt.Fprintf(e.Out, "  %s：%d 個檔，%s\n", s.Label, s.Files, sizeText(s.Bytes)); err != nil {
			return err
		}
	}
	if len(r.Skipped) > 0 {
		if _, err := fmt.Fprintf(e.Out, "沒收進去的（捷徑類的檔）%d 個：%s\n", len(r.Skipped), strings.Join(r.Skipped, "、")); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(e.Out, "備份只放在這台電腦；電腦壞了備份也會不見，重要的話請自己另外存一份（備份檔沒有密碼，拿到的人看得到全部內容）")
	return err
}

func cliStatus(e *cmdline.Env, args []string) error {
	if len(args) != 0 {
		return cmdline.Usagef("用法：evergenie %s", cliUsage)
	}
	r, err := client(e).GetStatus(e.Ctx, &sdk.BackupGetStatusRequest{})
	if err != nil {
		return err
	}
	lines := []string{"上次備份：" + timeText(r.LastAt)}
	if r.Restoring {
		lines = append(lines, "有一份還原在等服務重開")
	}
	if lr := r.LastRestore; lr != nil {
		lines = append(lines, "上次還原（"+timeText(lr.At)+"）："+lr.Message)
		for _, t := range lr.Todo {
			lines = append(lines, "  還要做："+t)
		}
	}
	lines = append(lines, fmt.Sprintf("保留規則：手動做的、還原前留的最新 %d 份一定留，其餘留 %d 天；每次更新前另外自動留一份，只留最新的", r.KeepCount, r.KeepDays))
	for _, f := range r.Files {
		lines = append(lines, fmt.Sprintf("%s  %-8s  %8s  %s", timeText(f.CreatedAt), KindLabel(f.Kind), sizeText(f.Size), f.Name))
	}
	_, err = fmt.Fprintln(e.Out, strings.Join(lines, "\n"))
	return err
}

// target 把指令的 <檔> 變成請求裡的欄位：這台電腦上有這個檔就用路徑，不然當成 backup status 列的檔名。
func target(e *cmdline.Env, arg string) (kind, name, path string, err error) {
	if info, serr := os.Stat(arg); serr == nil && info.Mode().IsRegular() {
		abs, aerr := filepath.Abs(arg)
		if aerr != nil {
			return "", "", "", cmdline.Usagef("讀不到這個檔：%s", arg)
		}
		return "", "", abs, nil
	}
	r, err := client(e).GetStatus(e.Ctx, &sdk.BackupGetStatusRequest{})
	if err != nil {
		return "", "", "", err
	}
	for _, f := range r.Files {
		if f.Name == arg {
			return f.Kind, f.Name, "", nil
		}
	}
	return "", "", "", cmdline.Usagef("找不到這份備份：%s（backup status 看有哪些）", arg)
}

func printPreview(e *cmdline.Env, r *sdk.BackupPreviewResponse) error {
	lines := []string{
		"備份時間：" + timeText(r.CreatedAt),
		"家：" + r.HomeName + "（" + r.Owner + "）",
		fmt.Sprintf("大小：%s（解開 %s，%d 個檔）", sizeText(r.Size), sizeText(r.Bytes), r.Files),
	}
	for _, s := range r.Sections {
		lines = append(lines, fmt.Sprintf("  %s：%d 個檔，%s", s.Label, s.Files, sizeText(s.Bytes)))
	}
	if len(r.Skipped) > 0 {
		lines = append(lines, fmt.Sprintf("做備份時沒收進去的（捷徑類的檔）%d 個：%s", len(r.Skipped), strings.Join(r.Skipped, "、")))
	}
	if r.OtherMachine {
		lines = append(lines, "這份是別台電腦（或別的位置的家）做的：還原後 AI 帳號登入、手機連回家、手機通知要重做")
	}
	if r.OtherMachine && len(r.Installed) > 0 {
		lines = append(lines, "備份不含別人分享的套件，還原後要重裝："+strings.Join(r.Installed, "、"))
	}
	if len(r.LostLinks) > 0 {
		lines = append(lines, fmt.Sprintf("這個家現在有 %d 個捷徑（符號連結），還原後會不見，要自己重建：%s", len(r.LostLinks), strings.Join(r.LostLinks, "、")))
	}
	if r.Problem != "" {
		lines = append(lines, "不能還原："+r.Problem)
	}
	_, err := fmt.Fprintln(e.Out, strings.Join(lines, "\n"))
	return err
}

func cliShow(e *cmdline.Env, args []string) error {
	if len(args) != 1 {
		return cmdline.Usagef("用法：evergenie backup show <檔>")
	}
	kind, name, path, err := target(e, args[0])
	if err != nil {
		return err
	}
	r, err := client(e).Preview(e.Ctx, &sdk.BackupPreviewRequest{Kind: kind, Name: name, Path: path})
	if err != nil {
		return err
	}
	return printPreview(e, r)
}

func cliRestore(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, nil, []string{"yes"}, cliUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 {
		return cmdline.Usagef("用法：evergenie backup restore <檔> --yes")
	}
	kind, name, path, err := target(e, o.Pos[0])
	if err != nil {
		return err
	}
	pre, err := client(e).Preview(e.Ctx, &sdk.BackupPreviewRequest{Kind: kind, Name: name, Path: path})
	if err != nil {
		return err
	}
	if err := printPreview(e, pre); err != nil {
		return err
	}
	if pre.Problem != "" {
		return cmdline.Usagef("這份備份現在不能還原")
	}
	if !o.Flags["yes"] {
		_, err := fmt.Fprintln(e.Out, "還原會把整個家換成這份備份（換之前會自動留一份現在的樣子）。確定要做：加 --yes")
		return err
	}
	r, err := client(e).Restore(e.Ctx, &sdk.BackupRestoreRequest{Kind: kind, Name: name, Path: path, By: cliActor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "準備好了，服務正在重開並換上備份（大約一分鐘）。現在的樣子留在：%s\n之後用 backup status 看結果\n", r.Safety)
	return err
}
