package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// 套件（P6-1）：看清單與狀態、重新載入、看 fork 的官方更新差異；沒有能用的 Go 時，秘書先問主人，同意了才 install-go（PKG-65、PKG-N5）。

func api(e *cmdline.Env) sdk.PackageAPI { return sdk.NewPackageClient(e.Client) }

const packagesUsage = `用法：evergenie packages <子指令>
  list [--tier official|installed|mine]   套件清單與狀態（rejected／failed 的 error 是白話原因；remedy 是可以怎麼補救）
  get <套件>                              一個套件的詳細狀態
  enable <套件>                           啟用套件。選用套件（list 裡 optional 是 true，預設不啟用）要先在對話裡問過使用者，他說要才開
  disable <套件>                          停用套件（資料留著，重新啟用就回來）；依賴它的套件會被拒
  reload [--id <套件>]                    重新掃描；指定的（空＝全部）附程式套件重新編譯並啟動
  updates <套件>                          fork 的套件：官方新版跟基準的差異，並寫一份差異清單檔（report_path）給你逐項問使用者要不要合併
  extensions [<套件>]                     套件開放可以調整的地方（擴充點）與現在的值：setting 是設定值、prompt_append 是接在秘書指令後面的一段、
                                          page_slot 是頁面上可以放東西的位置。想調整官方套件就用這個，不要去改或複製官方套件
  set-extension <套件> <擴充點> (--value <字> | --file <檔> | --clear)
                                          改一個擴充點的值（--clear 拿掉、回到預設）。prompt_append 的值下次開對話才會帶進去；標了 owner_only 的只有主人能改
  sync <套件> [--agent <職位>] [--persona <人設>] [--apply] [--json]
                                          套件帶來的秘書職責與排程樣板：預設只預演（列出會新增的秘書、職責差異的行數、會新增的排程），
                                          主人同意了才加 --apply 套用（只有主人能套用；已存在的排程不改，addendum 不動）；
                                          名冊上還沒有的選用職位（樣板 optional: true）不跟著整包加，主人想要再用 --agent <職位> 指名
  new <id> [--name <顯示名稱>] [--json]   建一個自己的新套件：在 packages/mine/<id>/ 放好 package.toml、一位秘書的樣板、最小頁面（/p/<id>/index）、personas/、schemas/、README.md。
                                          id 要小寫英文開頭、小寫英文數字與 -、共 2～31 字；不能跟官方或已裝的套件同名、不能是保留字（清單：evergenie docs show conventions/user-features.md 的「保留字」）。
                                          只寫自己的層，不碰官方；建好後要讓秘書上班用 packages sync <id>（預演，主人同意才 --apply）
  install <網址|資料夾> [--ref <tag>] [--yes]   安裝套件。網址（https://、ssh://、git@、file://）沒有 --yes 時只印預覽（名字、版本、會不會在這台電腦跑程式、
                                          用到哪些資料、問題與警告），最後一行告訴你確定要裝就加 --yes 再跑一次；本機資料夾直接裝
  preview <網址|資料夾> [--ref <tag>] [--json]   只看預覽，不安裝
  outdated [--json]                       從網址裝的套件有沒有新版（看來源最新的版本標籤）
  upgrade <套件> [--ref <tag>]            把從網址裝的套件換成新版（資料保留；新版載入失敗會換回舊的）
  export <套件> [--out <資料夾|檔名.tar.gz>] [--git] [--allow-findings] [--json]
                                          把自己的（或分享來的）套件打包出去分享：只帶套件資料夾（不帶你的資料 vault/pkg/<id>/、runtime/、
                                          .git、node_modules、編譯產物）；--out 不給就放 <家>/runtime/exports/<id>-<版本>/。
                                          --git 產生可以直接 git init、git push 的資料夾（含 .gitignore；不會幫你 init 或 push）。
                                          匯出前會掃疑似個資（金鑰、家的路徑、email、.env 之類的檔），有就停下列出檔案與行號、什麼都不寫；
                                          先跟主人一起看過、確定沒問題才加 --allow-findings。官方套件不用匯出（別人升級就有）
  go-status                               編譯附程式的套件要用的 Go 能不能用（先看程式自己的工具資料夾，再看 PATH；版本要夠新）。
                                          唯讀、不下載；能用結束碼 0，不能用結束碼 3（JSON 照樣印：detail 是原因，ask 是問主人的那一句）
  install-go --confirmed                  沒有能用的 Go（套件的 remedy 是 install_go，或 go-status 結束碼 3）時：先用提問工具問主人
                                          （照 go-status 的 ask 原文問：會說大約多大、裝在程式自己的資料夾裡），他同意了才加 --confirmed 執行；
                                          下載寫死的那一版、對過檢查碼才放進去（不用 Homebrew、不用密碼、不改 PATH），裝好自動重新編譯等著 Go 的套件
`

func packagesHelp(e *cmdline.Env, a []string) error {
	for _, x := range a {
		if x != "--help" && x != "-h" && x != "help" {
			return cmdline.Usagef("不認得的子指令：packages %s（evergenie packages --help 看用法）", x)
		}
	}
	_, err := fmt.Fprint(e.Out, packagesUsage+packagesClaimUsage)
	return err
}

func packagesList(e *cmdline.Env, a []string) error {
	return cmdline.Each(e, a, cmdline.Spec{}, api(e).List, nil,
		func(r *sdk.PackageListResponse) ([]sdk.Package, string) { return r.Packages, "" })
}

func packagesGet(e *cmdline.Env, a []string) error {
	return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Get, func(r *sdk.PackageGetResponse) any { return r.Package })
}

func packagesReload(e *cmdline.Env, a []string) error {
	return cmdline.Each(e, a, cmdline.Spec{}, api(e).Reload, nil,
		func(r *sdk.PackageReloadResponse) ([]sdk.Package, string) { return r.Packages, "" })
}

// packages enable|disable <套件>（REQ-176）：轉 PackageSetEnabled。選用套件先問過使用者再開。
func packagesEnable(e *cmdline.Env, a []string) error  { return packagesSetEnabled(e, a, true) }
func packagesDisable(e *cmdline.Env, a []string) error { return packagesSetEnabled(e, a, false) }

func packagesSetEnabled(e *cmdline.Env, a []string, on bool) error {
	verb := map[bool]string{true: "enable", false: "disable"}[on]
	usage := "evergenie packages " + verb + " <套件>"
	o, err := parseOpts(a, nil, nil, usage)
	if err != nil || len(o.pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	r, err := api(e).SetEnabled(e.Ctx, &sdk.PackageSetEnabledRequest{ID: o.pos[0], Enabled: on, By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	p := r.Package
	if p == nil {
		return fmt.Errorf("沒有收到套件「%s」的狀態", o.pos[0])
	}
	if on && p.State == sdk.PackageStateRejected {
		return fmt.Errorf("套件「%s」已設為啟用，但載入被拒：%s", p.ID, p.Error)
	}
	word := map[bool]string{true: "啟用", false: "停用"}[on]
	if _, err := fmt.Fprintf(e.Out, "套件「%s」已%s（狀態 %s）\n", p.ID, word, p.State); err != nil {
		return err
	}
	if p.Error != "" {
		_, err = fmt.Fprintf(e.Out, "說明：%s\n", p.Error)
	}
	return err
}

// packages extensions [<套件>]：列擴充點與現在的值（DEV-154：調整官方套件的正路）。
func packagesExtensions(e *cmdline.Env, a []string) error {
	usage := "evergenie packages extensions [<套件>]"
	o, err := parseOpts(a, nil, nil, usage)
	if err != nil || len(o.pos) > 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	req := &sdk.PackageListExtensionsRequest{}
	if len(o.pos) == 1 {
		req.Package = o.pos[0]
	}
	r, err := api(e).ListExtensions(e.Ctx, req)
	if err != nil {
		return err
	}
	return e.Line(r)
}

// packages set-extension <套件> <擴充點> (--value <字> | --file <檔> | --clear)
func packagesSetExtension(e *cmdline.Env, a []string) error {
	usage := "evergenie packages set-extension <套件> <擴充點> (--value <字> | --file <檔> | --clear)"
	o, err := parseOpts(a, []string{"value", "file"}, []string{"clear"}, usage)
	_, hasValue := o.vals["value"]
	_, hasFile := o.vals["file"]
	given := 0
	for _, on := range []bool{hasValue, hasFile, o.flags["clear"]} {
		if on {
			given++
		}
	}
	if err != nil || len(o.pos) != 2 || given != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	req := &sdk.PackageSetExtensionRequest{Package: o.pos[0], Extension: o.pos[1], By: &sdk.Actor{Name: e.By}}
	switch {
	case hasValue:
		req.Value = map[string]any{"value": o.vals["value"]}
	case hasFile:
		raw, err := os.ReadFile(o.vals["file"])
		if err != nil {
			return fmt.Errorf("讀不到檔案 %s", o.vals["file"])
		}
		req.Value = map[string]any{"value": strings.TrimRight(string(raw), "\n")}
	}
	if _, err := api(e).SetExtension(e.Ctx, req); err != nil {
		return err
	}
	word := "已改好"
	if o.flags["clear"] {
		word = "已拿掉（回到預設）"
	}
	_, err = fmt.Fprintf(e.Out, "擴充點「%s.%s」%s\n", o.pos[0], o.pos[1], word)
	return err
}

func packagesUpdates(e *cmdline.Env, a []string) error {
	return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).ListUpdates, func(r *sdk.PackageListUpdatesResponse) any { return r })
}

func packagesInstallGo(e *cmdline.Env, a []string) error {
	p, err := cmdline.Prepare[sdk.PackageInstallGoRequest](e, a, cmdline.Spec{})
	if err != nil {
		return err
	}
	if !p.Req.Confirmed {
		return cmdline.Usagef("裝 Go 之前要先問主人；他同意了再加 --confirmed")
	}
	return cmdline.Send(e, p, api(e).InstallGo, func(r *sdk.PackageInstallGoResponse) any { return r })
}

// goStatusUnusable：go-status 說「沒有能用的 Go」的結束碼（固定，秘書照它決定要不要問主人）。
const goStatusUnusable = 3

// packages go-status（PKG-65）：印 GoStatus 的 JSON；不能用時結束碼 3。
func packagesGoStatus(e *cmdline.Env, a []string) error {
	p, err := cmdline.Prepare[sdk.PackageGoStatusRequest](e, a, cmdline.Spec{})
	if err != nil {
		return err
	}
	r, err := api(e).GoStatus(e.Ctx, p.Req)
	if err != nil {
		return err
	}
	if err := e.Line(r); err != nil {
		return err
	}
	if !r.Usable {
		return cmdline.ExitCodeError{Code: goStatusUnusable}
	}
	return nil
}

// packages sync <套件> [--agent <職位>] [--persona <人設>] [--apply] [--json]（D-28、REQ-174、238）：預設只預演；--apply 才套用，只有主人能套用。
// --agent 只同步那一個職位；--persona 是新增秘書時用的人設（不給就用預設人設）。
func packagesSync(e *cmdline.Env, a []string) error {
	const usage = "evergenie packages sync <套件> [--agent <職位>] [--persona <人設>] [--apply] [--json]"
	o, err := parseOpts(a, []string{"agent", "persona"}, []string{"apply", "json"}, usage)
	if err != nil || len(o.pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	r, err := api(e).Sync(e.Ctx, &sdk.PackageSyncRequest{ID: o.pos[0], Agent: o.vals["agent"], Persona: o.vals["persona"], Apply: o.flags["apply"], By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	if o.flags["json"] {
		return e.Line(r)
	}
	var b strings.Builder
	if r.Applied {
		fmt.Fprintf(&b, "已套用套件「%s」的職責與排程：\n", o.pos[0])
	} else {
		fmt.Fprintf(&b, "套件「%s」同步預演（沒有改任何東西；主人同意後加 --apply 套用）：\n", o.pos[0])
	}
	if len(r.Agents)+len(r.Jobs)+len(r.Optional) == 0 {
		b.WriteString("這個套件沒有帶秘書職責或排程樣板\n")
	}
	if len(r.Agents) > 0 {
		b.WriteString("秘書：\n")
	}
	for _, x := range r.Agents {
		switch x.Action {
		case "create":
			fmt.Fprintf(&b, "  %s　新增（職責 %d 行）%s\n", x.ID, x.Added, personaNote(x))
		case "update":
			fmt.Fprintf(&b, "  %s　更新職責（新增 %d 行、移除 %d 行；附加規則不動）\n", x.ID, x.Added, x.Removed)
		default:
			fmt.Fprintf(&b, "  %s　職責相同，不動\n", x.ID)
		}
	}
	if len(r.Jobs) > 0 {
		b.WriteString("排程：\n")
	}
	for _, x := range r.Jobs {
		if x.Action == "create" {
			fmt.Fprintf(&b, "  %s/%s　新增\n", x.Agent, x.ID)
		} else {
			fmt.Fprintf(&b, "  %s/%s　已存在，不改\n", x.Agent, x.ID)
		}
	}
	for _, id := range r.Optional {
		fmt.Fprintf(&b, "選用職位 %s 沒有一起加；主人想要再用 --agent %s 指名\n", id, id)
	}
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

// personaNote 是 create 的預演裡「預設角色」那一段（來源：這次指定的、職位樣板預設的、家的預設人設）。
func personaNote(x sdk.SyncAgent) string {
	if x.Persona == "" {
		return "　預設角色：（沒有，套用前要指定 --persona）"
	}
	from := map[string]string{"request": "這次指定", "template": "職位樣板預設", "home": "家的預設人設"}[x.PersonaFrom]
	return fmt.Sprintf("　預設角色：%s（%s）", x.Persona, from)
}

func printPreview(e *cmdline.Env, r *sdk.PackagePreviewResponse) error {
	var b strings.Builder
	if r.ID != "" {
		fmt.Fprintf(&b, "套件：%s（%s）　版本 %s\n", r.Name, r.ID, r.Version)
		if r.Description != "" {
			fmt.Fprintf(&b, "說明：%s\n", r.Description)
		}
		if r.Ref != "" {
			fmt.Fprintf(&b, "來源版本：%s　commit %s\n", r.Ref, shortSHA(r.Commit))
		}
		if r.RunsProgram {
			b.WriteString("注意：這個套件會在你的電腦上編譯並執行它附的程式\n")
		}
		if r.Core != "" {
			fmt.Fprintf(&b, "要求 EverGenie：%s\n", r.Core)
		}
		for _, row := range []struct {
			label string
			items []string
		}{{"帶來的秘書", r.Agents}, {"帶來的頁面", r.Pages}, {"帶來的排程", r.Jobs}, {"資料種類", r.DataKinds}, {"會讀", r.Reads}, {"會寫", r.Writes}} {
			if len(row.items) > 0 {
				fmt.Fprintf(&b, "%s：%s\n", row.label, strings.Join(row.items, "、"))
			}
		}
		// 帶來的角色（REQ-338）：裝好之後角色庫就有，代號是 <id>.<slug>。
		var personas []string
		for _, p := range r.Personas {
			label := p.ID
			if p.Name != "" {
				label = p.Name + "（" + p.ID + "）"
			}
			if p.Avatar {
				label += "，有頭像"
			}
			personas = append(personas, label)
		}
		if len(personas) > 0 {
			fmt.Fprintf(&b, "帶來的角色：%s\n", strings.Join(personas, "、"))
		}
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "提醒：%s\n", w)
	}
	for _, p := range r.Problems {
		fmt.Fprintf(&b, "不能安裝：%s\n", p)
	}
	_, err := fmt.Fprint(e.Out, b.String())
	return err
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func isPackageURL(s string) bool {
	for _, p := range []string{"https://", "ssh://", "git@", "file://"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// packages new <id> [--name <顯示名稱>] [--json]（REQ-306）：轉 PackageNew，建 packages/mine/<id>/ 的鷹架。
func packagesNew(e *cmdline.Env, a []string) error {
	const usage = "evergenie packages new <id> [--name <顯示名稱>] [--json]"
	o, err := parseOpts(a, []string{"name"}, []string{"json"}, usage)
	if err != nil || len(o.pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	r, err := api(e).New(e.Ctx, &sdk.PackageNewRequest{ID: o.pos[0], Name: o.vals["name"], By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	if o.flags["json"] {
		return e.Line(r)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已建立你的新套件「%s」：%s\n", o.pos[0], r.Dir)
	for _, f := range r.Files {
		fmt.Fprintf(&b, "  %s\n", f)
	}
	if r.Package != nil {
		fmt.Fprintf(&b, "狀態 %s；頁面網址 /p/%s/index\n", r.Package.State, o.pos[0])
		if r.Package.Error != "" {
			fmt.Fprintf(&b, "說明：%s\n", r.Package.Error)
		}
	}
	fmt.Fprintf(&b, "下一步：改 package.toml、agents/%s/agent.md 與 pages/index.tsx；要讓秘書負責它，先 evergenie packages sync %s 預演，主人同意再加 --apply\n", o.pos[0], o.pos[0])
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

// packages preview <網址|資料夾> [--ref <tag>] [--json]（REQ-282）：只看，不裝。
func packagesPreview(e *cmdline.Env, a []string) error {
	const usage = "evergenie packages preview <網址|資料夾> [--ref <tag>] [--json]"
	o, err := parseOpts(a, []string{"ref"}, []string{"json"}, usage)
	if err != nil || len(o.pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	r, err := api(e).Preview(e.Ctx, &sdk.PackagePreviewRequest{Source: o.pos[0], Ref: o.vals["ref"]})
	if err != nil {
		return err
	}
	if o.flags["json"] {
		return e.Line(r)
	}
	return printPreview(e, r)
}

// packages install <網址|資料夾> [--ref <tag>] [--yes]（REQ-282）：網址沒有 --yes 只印預覽；有問題的預覽不能裝。
func packagesInstall(e *cmdline.Env, a []string) error {
	const usage = "evergenie packages install <網址|資料夾> [--ref <tag>] [--yes]"
	o, err := parseOpts(a, []string{"ref"}, []string{"yes"}, usage)
	if err != nil || len(o.pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	src, ref := o.pos[0], o.vals["ref"]
	if isPackageURL(src) && !o.flags["yes"] {
		r, err := api(e).Preview(e.Ctx, &sdk.PackagePreviewRequest{Source: src, Ref: ref})
		if err != nil {
			return err
		}
		if err := printPreview(e, r); err != nil {
			return err
		}
		if len(r.Problems) > 0 {
			return cmdline.ErrReported
		}
		_, err = fmt.Fprintln(e.Out, "確定要裝就加 --yes 再跑一次")
		return err
	}
	r, err := api(e).Install(e.Ctx, &sdk.PackageInstallRequest{Source: src, Ref: ref, Confirmed: o.flags["yes"], By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	if p := r.Package; p != nil {
		_, err = fmt.Fprintf(e.Out, "已安裝「%s」%s（狀態 %s）\n", p.Name, p.Version, p.State)
		if err == nil && p.Error != "" {
			_, err = fmt.Fprintf(e.Out, "說明：%s\n", p.Error)
		}
	}
	return err
}

func packagesOutdated(e *cmdline.Env, a []string) error {
	const usage = "evergenie packages outdated [--json]"
	o, err := parseOpts(a, nil, []string{"json"}, usage)
	if err != nil || len(o.pos) != 0 {
		return cmdline.Usagef("用法：%s", usage)
	}
	r, err := api(e).Outdated(e.Ctx, &sdk.PackageOutdatedRequest{})
	if err != nil {
		return err
	}
	if o.flags["json"] {
		return e.Line(r)
	}
	var b strings.Builder
	for _, p := range r.Packages {
		fmt.Fprintf(&b, "%s　%s → %s　（packages upgrade %s）\n", p.ID, p.Current, p.Latest, p.ID)
	}
	for _, f := range r.Failed {
		fmt.Fprintf(&b, "沒查到：%s\n", f)
	}
	if len(r.Packages) == 0 && len(r.Failed) == 0 {
		b.WriteString("從網址裝的套件都是最新版\n")
	}
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

func packagesUpgrade(e *cmdline.Env, a []string) error {
	const usage = "evergenie packages upgrade <套件> [--ref <tag>]"
	o, err := parseOpts(a, []string{"ref"}, nil, usage)
	if err != nil || len(o.pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	r, err := api(e).Upgrade(e.Ctx, &sdk.PackageUpgradeRequest{ID: o.pos[0], Ref: o.vals["ref"], By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "「%s」已從 %s 更新到 %s（資料保留）\n", o.pos[0], r.From, r.To)
	return err
}

// packages export <套件> [--out <資料夾|.tar.gz>] [--git] [--allow-findings] [--json]（SEC-302 S5、REQ-340）：
// 轉 PackageExport；--out 相對路徑以目前資料夾解開。掃到疑似個資（沒有 --allow-findings）印清單、結束碼 1。
func packagesExport(e *cmdline.Env, a []string) error {
	const usage = "evergenie packages export <套件> [--out <資料夾|檔名.tar.gz>] [--git] [--allow-findings] [--json]"
	o, err := parseOpts(a, []string{"out"}, []string{"git", "allow-findings", "json"}, usage)
	if err != nil || len(o.pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	out := o.vals["out"]
	if out != "" && !filepath.IsAbs(out) {
		if out, err = filepath.Abs(out); err != nil {
			return fmt.Errorf("看不懂輸出位置：%v", err)
		}
	}
	r, err := api(e).Export(e.Ctx, &sdk.PackageExportRequest{ID: o.pos[0], Out: out, Git: o.flags["git"], AllowFindings: o.flags["allow-findings"], By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	if o.flags["json"] {
		if err := e.Line(r); err != nil {
			return err
		}
		if r.Blocked {
			return cmdline.ErrReported
		}
		return nil
	}
	var b strings.Builder
	if r.Blocked {
		fmt.Fprintf(&b, "沒有匯出「%s」：找到 %d 處疑似個資（金鑰、家的路徑、email 之類），什麼都沒寫：\n", o.pos[0], len(r.Findings))
	} else {
		fmt.Fprintf(&b, "已匯出「%s」：%s（%d 個檔）\n", o.pos[0], r.Path, len(r.Files))
		if len(r.Findings) > 0 {
			fmt.Fprintf(&b, "注意：照你的意思（--allow-findings）帶著 %d 處疑似個資匯出了：\n", len(r.Findings))
		}
	}
	for _, f := range r.Findings {
		if f.Line > 0 {
			fmt.Fprintf(&b, "  %s:%d　%s\n", f.Path, f.Line, f.What)
		} else {
			fmt.Fprintf(&b, "  %s　%s\n", f.Path, f.What)
		}
	}
	if r.Blocked {
		b.WriteString("請改掉這些地方（金鑰放設定或擴充點、路徑改成相對的）；確定沒問題、主人也同意，再加 --allow-findings 重跑\n")
	}
	if len(r.Skipped) > 0 {
		b.WriteString("沒有帶的：\n")
		for _, s := range r.Skipped {
			fmt.Fprintf(&b, "  %s\n", s)
		}
	}
	if !r.Blocked && o.flags["git"] {
		fmt.Fprintf(&b, "下一步（要放上 git 的話）：cd %s && git init && git add -A && git commit -m \"%s\" && git tag v<版本> && git remote add origin <網址> && git push -u origin HEAD --tags\n", r.Path, o.pos[0])
		b.WriteString("別人用 evergenie packages install <網址> 安裝（會先看預覽、加 --yes 才裝）；之後你打新的版本標籤，他們用 packages upgrade 更新\n")
	} else if !r.Blocked {
		b.WriteString("別人可以用 evergenie packages install <這個資料夾的完整路徑> 安裝（壓縮檔先解開）\n")
	}
	if _, err := fmt.Fprint(e.Out, b.String()); err != nil {
		return err
	}
	if r.Blocked {
		return cmdline.ErrReported
	}
	return nil
}

// packagesClaimUsage 接在 packages --help 後面（SEC-302 S9）。
const packagesClaimUsage = `  claim-notify <套件> [--agent <職位>] [--persona <角色>] [--json]
                                          套件建好、預演過之後，請核心發一則「要你同意」給主人：「這個職位還沒有人負責，建議用 X」，
                                          選項「好，用 X｜換一個｜之後再說」。不帶 --persona 由核心挑（套件自己帶的上線角色優先）。
                                          不帶 --agent 時不挑選用職位（樣板 optional: true）；主人說要那個職位才用 --agent 指名
                                          主人按「好」由核心以主人身分建好秘書並讓她上班（你不用、也不能自己 --apply）；
                                          同一個職位只問一次：之前問過、主人說「之後再說」或「換一個」都不再發，印出當時的狀態
`

// packages claim-notify <套件>（SEC-302 S9、REQ-341）：轉 PackageClaimNotify。
func packagesClaimNotify(e *cmdline.Env, a []string) error {
	const usage = "evergenie packages claim-notify <套件> [--agent <職位>] [--persona <角色>] [--json]"
	o, err := parseOpts(a, []string{"agent", "persona"}, []string{"json"}, usage)
	if err != nil || len(o.pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	r, err := api(e).ClaimNotify(e.Ctx, &sdk.PackageClaimNotifyRequest{ID: o.pos[0], Agent: o.vals["agent"], Persona: o.vals["persona"], By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	if o.flags["json"] {
		return e.Line(r)
	}
	var b strings.Builder
	if r.Sent {
		fmt.Fprintf(&b, "已發認領通知 %s：職位「%s」建議用 %s（%s）—%s。\n", r.Notice, r.Agent, r.PersonaName, r.Persona, r.Reason)
		b.WriteString("主人按「好」：核心建好秘書並讓她上班；「換一個」：他到設定→職位與秘書那張卡自己挑；「之後再說」：不再問。\n")
	} else {
		fmt.Fprintf(&b, "沒有發新的通知：職位「%s」%s（狀態 %s", r.Agent, r.Detail, r.State)
		if r.Notice != "" {
			fmt.Fprintf(&b, "，通知 %s", r.Notice)
		}
		b.WriteString("）\n")
	}
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

// opts 是子指令的旗標：--名稱 值，或只有名稱的開關；位置參數照順序收。
type opts struct {
	vals  map[string]string
	flags map[string]bool
	pos   []string
}

// parseOpts 用 cmdline.LocalFlags 拆旗標；認不得的旗標或缺值回用法錯誤。
func parseOpts(args []string, withValue, switches []string, usage string) (opts, error) {
	vals, on, rest, ok := cmdline.LocalFlags(args, withValue, switches)
	if !ok {
		return opts{vals: map[string]string{}, flags: map[string]bool{}}, cmdline.Usagef("用法：%s", usage)
	}
	return opts{vals: vals, flags: on, pos: rest}, nil
}

// Commands 登記 `packages …`、`data …` 與 `docs …`（跟著程式的說明文件，docs.go）三組指令（套件層級函式，不是 Module 的方法）。
func Commands() map[string]app.Command {
	cmds := map[string]app.Command{
		"packages":               {Usage: "packages list|get|enable|disable|reload|extensions|set-extension|updates|sync|new|install|preview|outdated|upgrade|export|claim-notify|go-status|install-go\n套件清單與狀態、重新載入、fork 的官方更新差異、套件的職責與排程預演／套用、建自己的新套件、請主人選職位的負責角色（通知，按「好」由核心設好）、從網址或資料夾裝套件（先預覽再 --yes）與更新、匯出自己的套件分享（先掃個資）、看編譯套件要用的 Go 能不能用、主人同意後下載 Go 到程式自己的資料夾（evergenie packages --help 看細節）", Group: cmdline.GroupFeatures, Run: packagesHelp},
		"packages list":          {Run: packagesList},
		"packages get":           {Run: packagesGet},
		"packages reload":        {Run: packagesReload},
		"packages enable":        {Run: packagesEnable},
		"packages disable":       {Run: packagesDisable},
		"packages updates":       {Run: packagesUpdates},
		"packages extensions":    {Run: packagesExtensions},
		"packages set-extension": {Run: packagesSetExtension},
		"packages install-go":    {Run: packagesInstallGo},
		"packages go-status":     {Run: packagesGoStatus},
		"packages sync":          {Run: packagesSync},
		"packages new":           {Run: packagesNew},
		"packages install":       {Run: packagesInstall},
		"packages preview":       {Run: packagesPreview},
		"packages outdated":      {Run: packagesOutdated},
		"packages upgrade":       {Run: packagesUpgrade},
		"packages export":        {Run: packagesExport},
		"packages claim-notify":  {Run: packagesClaimNotify},
		"data":                   {Run: dataHelp},
		"data kinds":             {Usage: "data kinds|list|get|add|set|rm     套件的資料（種類名稱是 <套件>.<種類>；evergenie data --help 看細節）", Group: cmdline.GroupFeatures, Run: dataKindsCmd},
		"data list":              {Run: dataList},
		"data get":               {Run: dataGet},
		"data add":               {Run: dataAdd},
		"data set":               {Run: dataSet},
		"data rm":                {Run: dataRm},
	}
	for name, c := range docsCommands() {
		cmds[name] = c
	}
	return cmds
}
