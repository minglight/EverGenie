package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/agent/personaref"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// personas：角色庫（P6-5a、D-31、REQ-130，等價舊 `agentctl persona`）。都經 `personas.*` API；
// 官方卡唯讀，新增與複製出來的卡放在 personas/mine/，一開始是設計中（不能指派），檢查過了才上線。
// 編輯內文不做指令：直接改 personas/mine/<代號>.md 檔案即可（跟舊系統一樣）。

func api(e *cmdline.Env) sdk.PersonasAPI { return sdk.NewPersonasClient(e.Client) }

func actor(e *cmdline.Env) *sdk.Actor { return &sdk.Actor{Name: e.By} }

// opts／parseOpts 是 cmdline.Opts／ParseOpts 的小殼（小寫欄位，指令內用）。
type opts struct {
	vals  map[string]string
	flags map[string]bool
	pos   []string
}

func parseOpts(args []string, withValue, switches []string, usage string) (opts, error) {
	o, err := cmdline.ParseOpts(args, withValue, switches, usage)
	return opts{vals: o.Vals, flags: o.Flags, pos: o.Pos}, err
}

const personaUsage = `用法：evergenie personas <子指令>
  list [--json]                      角色庫（代號、名字、別名、來源、狀態、坐在哪）
  show <角色>                        印一張卡的整份檔案（<角色> 可以是代號、名字或別名）
  new <代號> --name <名字> [--aka a,b]    新增一張我的卡（設計中，內文是範本，放在 personas/mine/<代號>.md）
  new --to-package <套件> <代號> [--name <名字>] [--aka a,b]   新增一張套件裡的卡（packages/mine/<套件>/personas/<代號>.md，設計中；有效代號 <套件>.<代號>；只限自己的套件）
  copy <來源> <代號> --name <名字> [--aka a,b]   把一張卡（官方的也行）複製成新的我的卡，名字要改
  copy <來源> --to-package <套件> [--slug <代號>] [--name <名字>]   把一張卡複製進套件（來源不動；設計中）
  move <我的角色> --to-package <套件> [--slug <代號>]   把我的角色放進套件：複製（來源不動，我的角色永遠是個人的）連頭像；放進去後要 check、live 才能指派
  check [<角色>]                     檢查卡片（撞名、範本空格、寫了職位 id…）；有問題結束碼 1
  live <角色>                        上線（先檢查；只限我的卡與自己套件裡的卡，例 garden.fairy）
  design <角色>                      移回設計中（有秘書坐著不行；只限我的卡）
  style [a|b|c|d|custom]             看或設說話層強度（a 現況、b 頭尾一句、c 用說的、d 全開、custom 自己的 personas/_style.md）
  test <角色> [--seat <職位>] [--only a,b] [--model <模型>] [--card <檔案>] [--style a|b|c|d|custom] [--no-wait]
                                     測口吻：用固定情境、跟常駐一樣的指令各說一次（不開工具、不留對話，花座位那個帳號的額度）；
                                     預設等跑完（每 2 秒查一次），印結果檔路徑與上一次的路徑；--no-wait 只印路徑。
                                     情境放 personas/_test/scenarios.md，沒有就用內建的；--card 用草稿卡檔案測
                                     情境可以自己帶梗本（[梗本]）與提梗次數的檢查（[檢查] 提梗 1），跑完自動判、印「檢查：」
  results <角色> [--limit n] [--json]  列最近幾次的測試結果（時間、狀態、座位、模型、路徑）
  pack build|cards|sync|status       角色包（發行者建包、把公開卡寫進 repo；使用者升級後下載同版號的包；evergenie personas pack 看細節）
  export --persona <代號>… --out <檔>    把角色連圖、衣櫃、衣服目錄打成一個 .hgpersona 檔（給別的家；--persona 可重複）
  import <檔> [--dry-run] [--force]   匯入別的家匯出的角色：先驗再寫、只新增與替換、不蓋你改過的（--force 才蓋，先備份）
  promote <my_名字> [--as <官方代號>] [--revert|--forget] [--apply] [--json]
                                     開發家把自己的角色升成官方：卡搬到官方位置、代號去掉 my_，圖留在 personas/mine/ 只改檔名，
                                     名冊與套件紀錄跟著改（預設只預演；服務要先停；--apply 只認主人；evergenie personas promote --help 看細節）
編輯卡片內文：直接改 personas/mine/<代號>.md（官方卡唯讀，要改先 copy）；改完跑 check 再 live。
`

func personasHelp(e *cmdline.Env, a []string) error {
	for _, x := range a {
		if x != "--help" && x != "-h" && x != "help" {
			return cmdline.Usagef("不認得的子指令：personas %s（evergenie personas --help 看用法）", x)
		}
	}
	_, err := fmt.Fprint(e.Out, personaUsage)
	return err
}

// findPersona 把代號、名字或別名（不分大小寫）對到角色庫裡的一張卡（同代號照角色庫的順序，官方角色資料夾＞我的＞跟著程式的，PRS-27；同舊 find_persona）；
// 都對不到時，再試主人自己的 my_<key>。
func findPersona(cards []sdk.PersonaCard, key string) (sdk.PersonaCard, bool) {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, c := range cards {
		if c.Shadowed {
			continue
		}
		if k == c.Slug || k == strings.ToLower(c.Name) {
			return c, true
		}
		for _, a := range c.Aka {
			if k == strings.ToLower(a) {
				return c, true
			}
		}
	}
	for _, c := range cards { // 主人自己的角色代號是 my_<名字>：只打名字那一段也找得到（官方同代號的在上面已經先對到）
		if !c.Shadowed && c.Slug == personaref.MinePrefix+k {
			return c, true
		}
	}
	return sdk.PersonaCard{}, false
}

func resolvePersona(e *cmdline.Env, key string) (sdk.PersonaCard, error) {
	r, err := api(e).List(e.Ctx, &sdk.PersonasListRequest{})
	if err != nil {
		return sdk.PersonaCard{}, err
	}
	c, ok := findPersona(r.Personas, key)
	if !ok {
		return sdk.PersonaCard{}, fmt.Errorf("角色庫裡沒有 %s（evergenie personas list）", key)
	}
	return c, nil
}

func splitAka(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func personasList(e *cmdline.Env, args []string) error {
	o, err := parseOpts(args, nil, []string{"json"}, "evergenie personas list [--json]")
	if err != nil || len(o.pos) > 0 {
		return cmdline.Usagef("用法：evergenie personas list [--json]")
	}
	r, err := api(e).List(e.Ctx, &sdk.PersonasListRequest{})
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, c := range r.Personas {
		if o.flags["json"] {
			if err := e.Line(c); err != nil {
				return err
			}
			continue
		}
		seat := "（還沒指派）"
		if len(c.Seats) > 0 {
			seat = strings.Join(c.Seats, "、")
		}
		aka := "—"
		if len(c.Aka) > 0 {
			aka = strings.Join(c.Aka, "、")
		}
		source := map[string]string{"official": "官方", "mine": "我的", "package": "套件 " + c.Package}[c.Source]
		state := map[string]string{"live": "上線", "design": "設計中"}[c.Status]
		note := ""
		if c.Shadowed {
			note = "　（被官方同代號的卡蓋住）"
		}
		fmt.Fprintf(&b, "%-12s %s　別名 %s　%s　%s　坐在 %s%s\n", c.Slug, c.Name, aka, source, state, seat, note)
	}
	if o.flags["json"] {
		return nil
	}
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

func personasShow(e *cmdline.Env, args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return cmdline.Usagef("用法：evergenie personas show <角色>")
	}
	c, err := resolvePersona(e, args[0])
	if err != nil {
		return err
	}
	r, err := api(e).Get(e.Ctx, &sdk.PersonasGetRequest{Slug: c.Slug})
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(e.Out, r.Raw)
	return err
}

func personasNew(e *cmdline.Env, args []string) error {
	const usage = "evergenie personas new <代號> --name <名字> [--aka a,b]　或　evergenie personas new --to-package <套件> <代號> [--name <名字>] [--aka a,b]"
	o, err := parseOpts(args, []string{"name", "aka", "to-package"}, nil, usage)
	if err != nil {
		return err
	}
	if pkgID := o.vals["to-package"]; pkgID != "" {
		if len(o.pos) != 1 {
			return cmdline.Usagef("用法：%s", usage)
		}
		r, err := api(e).Create(e.Ctx, &sdk.PersonasCreateRequest{Slug: o.pos[0], Name: o.vals["name"], Aka: splitAka(o.vals["aka"]), Package: pkgID, By: actor(e)})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(e.Out, "已建立 packages/mine/%s/personas/%s.md（設計中，有效代號 %s）。填好個性後跑 evergenie personas check %s，再 evergenie personas live %s\n", pkgID, o.pos[0], r.Persona.Slug, r.Persona.Slug, r.Persona.Slug)
		return err
	}
	if len(o.pos) != 1 || o.vals["name"] == "" {
		return cmdline.Usagef("用法：%s", usage)
	}
	r, err := api(e).Create(e.Ctx, &sdk.PersonasCreateRequest{Slug: o.pos[0], Name: o.vals["name"], Aka: splitAka(o.vals["aka"]), By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已建立 personas/mine/%s.md（設計中，還沒指派到任何職位）。填好個性後跑 evergenie personas check %s，再 evergenie personas live %s\n", r.Persona.Slug, r.Persona.Slug, r.Persona.Slug)
	return err
}

func personasCopy(e *cmdline.Env, args []string) error {
	const usage = "evergenie personas copy <來源> <代號> --name <名字> [--aka a,b]　或　evergenie personas copy <來源> --to-package <套件> [--slug <代號>] [--name <名字>]"
	o, err := parseOpts(args, []string{"name", "aka", "to-package", "slug"}, nil, usage)
	if err != nil {
		return err
	}
	if o.vals["to-package"] != "" {
		if len(o.pos) != 1 {
			return cmdline.Usagef("用法：%s", usage)
		}
		src, err := resolvePersona(e, o.pos[0])
		if err != nil {
			return err
		}
		return copyToPackage(e, src, o.vals["to-package"], o.vals["slug"], o.vals["name"], splitAka(o.vals["aka"]))
	}
	if o.vals["slug"] != "" || len(o.pos) != 2 || o.vals["name"] == "" {
		return cmdline.Usagef("用法：%s", usage)
	}
	src, err := resolvePersona(e, o.pos[0])
	if err != nil {
		return err
	}
	r, err := api(e).Copy(e.Ctx, &sdk.PersonasCopyRequest{From: src.Slug, Slug: o.pos[1], Name: o.vals["name"], Aka: splitAka(o.vals["aka"]), By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已把 %s 複製成 personas/mine/%s.md（設計中）。改好後跑 evergenie personas check %s，再 evergenie personas live %s\n", src.Slug, r.Persona.Slug, r.Persona.Slug, r.Persona.Slug)
	return err
}

func copyToPackage(e *cmdline.Env, src sdk.PersonaCard, pkgID, slug, name string, aka []string) error {
	r, err := api(e).Copy(e.Ctx, &sdk.PersonasCopyRequest{From: src.Slug, Slug: slug, Name: name, Aka: aka, Package: pkgID, By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已把 %s 複製進套件 %s：%s（設計中；來源不動）。看過內容後跑 evergenie personas check %s，再 evergenie personas live %s\n", src.Slug, pkgID, r.Persona.Slug, r.Persona.Slug, r.Persona.Slug)
	return err
}

// personas move <我的角色> --to-package <套件> [--slug <代號>]：把我的角色放進套件（U-2：複製，來源不動）。
func personasMove(e *cmdline.Env, args []string) error {
	const usage = "evergenie personas move <我的角色> --to-package <套件> [--slug <代號>]"
	o, err := parseOpts(args, []string{"to-package", "slug"}, nil, usage)
	if err != nil || len(o.pos) != 1 || o.vals["to-package"] == "" {
		return cmdline.Usagef("用法：%s", usage)
	}
	src, err := resolvePersona(e, o.pos[0])
	if err != nil {
		return err
	}
	if src.Source != "mine" {
		return fmt.Errorf("%s 不是我的角色（來源 %s）；只有我的角色能放進套件，官方的請用 evergenie personas copy %s --to-package %s", src.Slug, src.Source, src.Slug, o.vals["to-package"])
	}
	return copyToPackage(e, src, o.vals["to-package"], o.vals["slug"], "", nil)
}

func personasCheck(e *cmdline.Env, args []string) error {
	if len(args) > 1 || (len(args) == 1 && strings.HasPrefix(args[0], "-")) {
		return cmdline.Usagef("用法：evergenie personas check [<角色>]")
	}
	slug, label := "", "全部角色卡"
	if len(args) == 1 {
		c, err := resolvePersona(e, args[0])
		if err != nil {
			return err
		}
		slug, label = c.Slug, c.Slug
	}
	r, err := api(e).Check(e.Ctx, &sdk.PersonasCheckRequest{Slug: slug})
	if err != nil {
		return err
	}
	if len(r.Problems) == 0 {
		_, err = fmt.Fprintf(e.Out, "角色卡沒問題：%s\n", label)
		return err
	}
	if _, err := fmt.Fprintln(e.Out, strings.Join(r.Problems, "\n")); err != nil {
		return err
	}
	return cmdline.ExitCodeError{Code: 1}
}

func personasSetStatus(status string) func(e *cmdline.Env, args []string) error {
	return func(e *cmdline.Env, args []string) error {
		verb := map[string]string{"live": "live", "design": "design"}[status]
		if len(args) != 1 || strings.HasPrefix(args[0], "-") {
			return cmdline.Usagef("用法：evergenie personas %s <角色>", verb)
		}
		c, err := resolvePersona(e, args[0])
		if err != nil {
			return err
		}
		if _, err := api(e).SetStatus(e.Ctx, &sdk.PersonasSetStatusRequest{Slug: c.Slug, Status: status, By: actor(e)}); err != nil {
			return err
		}
		if status == "live" {
			_, err = fmt.Fprintf(e.Out, "%s 已上線，可以指派了（evergenie agents assign <職位> %s）\n", c.Slug, c.Slug)
		} else {
			_, err = fmt.Fprintf(e.Out, "%s 已移回設計中（不能指派）\n", c.Slug)
		}
		return err
	}
}

func personasStyle(e *cmdline.Env, args []string) error {
	if len(args) > 1 || (len(args) == 1 && strings.HasPrefix(args[0], "-")) {
		return cmdline.Usagef("用法：evergenie personas style [a|b|c|d|custom]")
	}
	if len(args) == 1 {
		r, err := api(e).SetStyle(e.Ctx, &sdk.PersonasSetStyleRequest{Level: args[0], By: actor(e)})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(e.Out, "說話層強度已設成 %s（新開的對話才生效；常駐對話重開後換）\n", r.Level)
		return err
	}
	r, err := api(e).GetStyle(e.Ctx, &sdk.PersonasGetStyleRequest{})
	if err != nil {
		return err
	}
	custom := "（沒有 personas/_style.md）"
	if r.CustomExists {
		custom = "（有 personas/_style.md）"
	}
	_, err = fmt.Fprintf(e.Out, "目前的說話層強度：%s　custom %s\n可以選 a、b、c、d、custom：evergenie personas style <強度>\n", r.Level, custom)
	return err
}

// personaTestPoll 是等測口吻跑完時，查結果的間隔（契約：每 2 秒）。測試可以改短。
var personaTestPoll = 2 * time.Second

// runtimeDir 盡量找出家的暫存資料夾，只用來把結果檔名印成完整路徑：EVERGENIE_HOME（照家的佈局），
// 其次從 EVERGENIE_API_SOCKET（<暫存>/sockets/api.sock）推；都沒有就是相對家的 runtime。
func runtimeDir() string {
	if h := sdk.Getenv("EVERGENIE_HOME"); h != "" {
		return platform.RuntimeDir(h)
	}
	if sock := sdk.Getenv("EVERGENIE_API_SOCKET"); strings.HasSuffix(sock, filepath.Join("runtime", "sockets", "api.sock")) {
		return filepath.Dir(filepath.Dir(sock))
	}
	return "runtime"
}

// testResultPath 是一次測試結果檔的位置；找不到家就印相對家的路徑。
func testResultPath(slug, run string) string {
	return filepath.Join(runtimeDir(), "persona-tests", slug, run)
}

func personasTest(e *cmdline.Env, args []string) error {
	const usage = "evergenie personas test <角色> [--seat <職位>] [--only a,b] [--model <模型>] [--card <檔案>] [--style a|b|c|d|custom] [--no-wait]"
	o, err := parseOpts(args, []string{"seat", "only", "model", "card", "style"}, []string{"no-wait"}, usage)
	if err != nil || len(o.pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	c, err := resolvePersona(e, o.pos[0])
	if err != nil {
		return err
	}
	req := &sdk.PersonasTestRequest{Slug: c.Slug, Seat: o.vals["seat"], Only: splitAka(o.vals["only"]), Model: o.vals["model"], Style: o.vals["style"]}
	if file := o.vals["card"]; file != "" {
		data, err := os.ReadFile(expandTilde(file))
		if err != nil {
			return fmt.Errorf("讀不到草稿卡 %s", file)
		}
		req.Card = string(data)
	}
	prev, err := api(e).TestResults(e.Ctx, &sdk.PersonasTestResultsRequest{Slug: c.Slug, Limit: 1})
	if err != nil {
		return err
	}
	started, err := api(e).Test(e.Ctx, req)
	if err != nil {
		return err
	}
	path := testResultPath(c.Slug, started.Run)
	if o.flags["no-wait"] {
		fmt.Fprintf(e.Out, "開始測 %s 的口吻（%d 個情境，背景跑）\n結果：%s\n", c.Name, started.Scenarios, path)
		return printPrevious(e, c.Slug, prev)
	}
	fmt.Fprintf(e.Err, "測 %s 的口吻：%d 個情境，等跑完……\n", c.Name, started.Scenarios)
	for {
		select {
		case <-e.Ctx.Done():
			return e.Ctx.Err()
		case <-time.After(personaTestPoll):
		}
		r, err := api(e).TestResults(e.Ctx, &sdk.PersonasTestResultsRequest{Slug: c.Slug, Limit: 3})
		if err != nil {
			return err
		}
		for _, run := range r.Runs {
			if run.Run != started.Run || run.Status == "running" {
				continue
			}
			if run.Status == "failed" {
				fmt.Fprintf(e.Err, "這次測試失敗：%s\n結果：%s\n", run.Reason, path)
				return cmdline.ErrReported
			}
			fmt.Fprintf(e.Out, "結果：%s\n", path)
			if run.Checks != "" {
				fmt.Fprintf(e.Out, "檢查：%s\n", run.Checks) // 情境自己帶的檢查（例：提梗次數，DEV-164）
			}
			return printPrevious(e, c.Slug, prev)
		}
	}
}

func printPrevious(e *cmdline.Env, slug string, prev *sdk.PersonasTestResultsResponse) error {
	if prev != nil && len(prev.Runs) > 0 {
		_, err := fmt.Fprintf(e.Out, "上一次：%s\n", testResultPath(slug, prev.Runs[0].Run))
		return err
	}
	return nil
}

func expandTilde(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

var testStatusNames = map[string]string{"running": "測試中", "done": "完成", "failed": "失敗"}

func personasResults(e *cmdline.Env, args []string) error {
	const usage = "evergenie personas results <角色> [--limit n] [--json]"
	o, err := parseOpts(args, []string{"limit"}, []string{"json"}, usage)
	if err != nil || len(o.pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	limit := 0
	if v := o.vals["limit"]; v != "" {
		if _, err := fmt.Sscanf(v, "%d", &limit); err != nil || limit <= 0 {
			return cmdline.Usagef("用法：%s（--limit 要是正整數）", usage)
		}
	}
	c, err := resolvePersona(e, o.pos[0])
	if err != nil {
		return err
	}
	r, err := api(e).TestResults(e.Ctx, &sdk.PersonasTestResultsRequest{Slug: c.Slug, Limit: int32(limit)})
	if err != nil {
		return err
	}
	if o.flags["json"] {
		for _, run := range r.Runs {
			run.Markdown = ""
			if err := e.Line(run); err != nil {
				return err
			}
		}
		return nil
	}
	if len(r.Runs) == 0 {
		_, err = fmt.Fprintf(e.Out, "%s 還沒測過口吻（evergenie personas test %s）\n", c.Name, c.Slug)
		return err
	}
	for _, run := range r.Runs {
		reason := ""
		if run.Reason != "" {
			reason = "　（" + run.Reason + "）"
		}
		if run.Checks != "" {
			reason += "　檢查 " + run.Checks
		}
		fmt.Fprintf(e.Out, "%s　%s　座位 %s　模型 %s　說話層 %s　%s%s\n", run.At.Format("2006-01-02 15:04"), testStatusNames[run.Status], run.Seat, run.Model, strings.ToUpper(run.Style), testResultPath(c.Slug, run.Run), reason)
	}
	return nil
}
