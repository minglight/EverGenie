package persona

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie personas …` 的指令表（套件層級函式，指令行程只是 client，不建 Module）。
// 角色庫的子指令都經 `personas.*` API；`personas pack` 的 build、cards、status 直接讀家裡的檔案（REQ-283），
// sync 交給這個家的服務做（personas.pack_sync），服務沒在跑才自己換檔案（PRS-5）。
// packPointer 算角色包指標檔 pack.toml 的路徑（packages.PersonasPackPointer），bundledCards 算跟著程式的公開卡資料夾
// （packages.PersonasBundledCards，PRS-25；匯出、匯入找官方卡用），fileKinds 從套件設定讀這個家照紀錄帶圖的種類
// （packages.PersonasFileKinds；連不到服務的 pack build --no-records 用，DEV-160 審查 M2）；都由 registry 傳進來（業務塊之間不互相 import），nil＝沒有。
func Commands(packPointer, bundledCards func(home string) string, fileKinds func(home string) ([]string, error)) map[string]app.Command {
	return map[string]app.Command{
		"personas":         {Usage: "personas list|show|new|copy|move|check|live|design|style|test|results|pack|export|import|promote\n角色庫（new <代號> --name …、copy 官方卡成我的；new／copy／move 加 --to-package <套件> 放進自己的套件；新卡是設計中，check 過 live 才能指派；style 設說話層強度 a–d／custom；test 測口吻、results 看結果；export／import 把角色連衣服打包給別的家；promote 開發家把自己的角色升成官方（離線、服務要停）；evergenie personas --help 看細節）", Group: cmdline.GroupFeatures, Run: personasHelp},
		"personas list":    {Run: personasList},
		"personas show":    {Run: personasShow},
		"personas new":     {Run: personasNew},
		"personas copy":    {Run: personasCopy},
		"personas move":    {Run: personasMove},
		"personas check":   {Run: personasCheck},
		"personas live":    {Run: personasSetStatus("live")},
		"personas design":  {Run: personasSetStatus("design")},
		"personas style":   {Run: personasStyle},
		"personas test":    {Run: personasTest},
		"personas results": {Run: personasResults},
		"personas pack":    {Run: func(e *cmdline.Env, args []string) error { return personasPack(e, args, packPointer, fileKinds) }},
		"personas export":  {Run: func(e *cmdline.Env, args []string) error { return personasExport(e, args, bundledCards) }},
		"personas import":  {Run: func(e *cmdline.Env, args []string) error { return personasImport(e, args, bundledCards) }},
	}
}

const personasPackUsage = `用法：evergenie personas pack <子指令>
  build --out <資料夾> [--version <版號>] [--set-pointer <repo> --url-base <網址>] [--no-records] [--json]
                                      發行者建包：只收 home.toml [admin] public_personas 列的角色，不收隱藏版；印 manifest 的 sha256。
                                      要這個家的服務在跑（讀衣服等跟著角色走的紀錄、找出隱藏版的資料夾）；--no-records 只收圖與卡
                                      （連不到服務時衣服資料夾整個不收）。--set-pointer 把這份包寫進那個 repo 的
                                      packages/official/personas/pack.toml，公開卡也一起寫（不 commit）
  cards --repo <repo> [--json]        發行者把公開角色的卡片文字寫進 repo 的 packages/official/personas/cards/（跟著程式發行；不 commit）：
                                      只收 public_personas、不收隱藏版、卡裡寫了主人的名字就不寫；不再公開的拿掉
  sync [--force] [--json]             請這個家的服務照這一版核心的指標下載同版號的角色包：只換有變的檔、驗 sha256、不刪、不蓋你改過的
                                      （--force 才蓋，先備份）；沒網路或驗證失敗什麼都不換、記下待補（結束碼 1）。
                                      服務沒在跑時指令自己換檔案，衣服等紀錄等服務啟動後自動補上
  status [--json]                     登記的版號、這一版的版號、有沒有待補、上次結果`

// failMsg 印一行白話原因再回「已印過」（結束碼 1）。
func failMsg(e *cmdline.Env, err error) error {
	var pe *platform.Err
	if errors.As(err, &pe) && pe.Message != "" {
		fmt.Fprintln(e.Err, pe.Message)
	} else {
		fmt.Fprintln(e.Err, err.Error())
	}
	return cmdline.ErrReported
}

func resolveHome(flag string) (string, error) {
	home := flag
	if home == "" {
		home = strings.TrimSpace(sdk.Getenv("EVERGENIE_HOME"))
	}
	if home == "" {
		return "", platform.Invalid("home", "不知道家在哪裡：用 --home <家的資料夾>，或設定環境變數 EVERGENIE_HOME")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return "", platform.Invalid("home", "家的路徑不對")
	}
	return abs, nil
}

// homeClient 連 home 這個家自己的服務（它的 API socket），不看 EVERGENIE_HOME／EVERGENIE_API_SOCKET：`--home` 指哪個家就讀寫哪個家，不會讀寫到別的家。
func homeClient(home string) *sdk.Client {
	return sdk.Dial(platform.RuntimeDir(home, "sockets", "api.sock"))
}

// serviceDown：連不上這個家的 socket（服務沒在跑）。服務回的錯誤、連上之後斷掉、指令自己被取消（Ctrl-C）都不算：
// 服務那邊可能還在同步，指令不能再自己同步一次。
func serviceDown(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial" && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// syncViaService 請這個家的服務同步（personas.pack_sync，PRS-5、PRS-28）。服務沒在跑、或是還不認得這個指令的舊服務時回 ok=false，呼叫的人自己同步。
func syncViaService(e *cmdline.Env, home, pointer string, force bool) (res *personapack.SyncResult, ok bool, err error) {
	resp, err := sdk.NewPersonasClient(homeClient(home)).PackSync(e.Ctx, &sdk.PersonasPackSyncRequest{Force: force, Pointer: pointer, By: &sdk.Actor{Name: e.By}})
	if serviceDown(err) {
		return nil, false, nil
	}
	var se *sdk.Error
	if errors.As(err, &se) && se.Code == sdk.ErrorCodeNotFound {
		return nil, false, nil // 舊的服務沒有 pack_sync
	}
	if err != nil && se == nil && e.Ctx.Err() == nil {
		// 連上了、途中斷掉：服務那邊可能還在做（它不跟著這條連線走），不自己再同步一次。
		return nil, true, platform.Unavailable("這個家的服務同步到一半沒有回應：它可能還在做，等一下用 evergenie personas pack status 看結果")
	}
	if err != nil {
		return nil, true, err
	}
	res = syncResultFrom(resp)
	if res.Status == personapack.StatusFailed {
		return res, true, errors.New(resp.Message)
	}
	return res, true, nil
}

func orNone(s, none string) string {
	if s == "" {
		return none
	}
	return s
}

func personasPack(e *cmdline.Env, args []string, pointerOf func(string) string, fileKinds func(string) ([]string, error)) error {
	if len(args) == 0 {
		fmt.Fprintln(e.Err, personasPackUsage)
		return cmdline.Usagef("用法錯誤（evergenie personas pack 看用法）")
	}
	sub, rest := args[0], args[1:]
	var withValue []string
	switches := []string{"json"}
	switch sub {
	case "build":
		withValue, switches = []string{"out", "version", "set-pointer", "url-base", "home"}, []string{"json", "no-records"}
	case "cards":
		withValue = []string{"repo", "home"}
	case "sync":
		withValue, switches = []string{"home"}, []string{"json", "force"}
	case "status":
		withValue = []string{"home"}
	default:
		fmt.Fprintln(e.Err, personasPackUsage)
		return cmdline.Usagef("用法錯誤（evergenie personas pack 看用法）")
	}
	vals, on, left, ok := cmdline.LocalFlags(rest, withValue, switches)
	if !ok || len(left) > 0 {
		fmt.Fprintln(e.Err, personasPackUsage)
		return cmdline.Usagef("用法錯誤（evergenie personas pack 看用法）")
	}
	home, err := resolveHome(vals["home"])
	if err != nil {
		return failMsg(e, err)
	}
	pointer := ""
	if pointerOf != nil {
		pointer = pointerOf(home)
	}
	emit := func(v any) {
		data, _ := json.Marshal(v)
		fmt.Fprintln(e.Out, string(data))
	}
	switch sub {
	case "build":
		store := clientStore(homeClient(home), e.By) // 經這個家自己的服務（PRS-4）；連不到時 Build 說要先啟動服務
		o := personapack.BuildOptions{Home: home, Out: vals["out"], Version: vals["version"], PointerRepo: vals["set-pointer"], URLBase: vals["url-base"], Store: store, NoRecords: on["no-records"], Ctx: e.Ctx}
		if fileKinds != nil {
			o.OfflineFileKinds = func() ([]string, error) { return fileKinds(home) }
		}
		res, err := personapack.Build(o)
		if err != nil {
			return failMsg(e, err)
		}
		if on["json"] {
			emit(res)
			return nil
		}
		fmt.Fprintf(e.Out, "manifest：%s\nmanifest sha256：%s\n", res.ManifestPath, res.ManifestSHA256)
		if len(res.RecordKinds) > 0 {
			fmt.Fprintf(e.Out, "紀錄的種類：%s\n", strings.Join(res.RecordKinds, "、"))
		}
		if res.RecordsNote != "" {
			fmt.Fprintf(e.Out, "注意：%s\n", res.RecordsNote)
		}
		for _, s := range res.Skipped {
			fmt.Fprintf(e.Out, "沒收：%s\n", s)
		}
		if res.Cards != nil {
			printCards(e, res.Cards)
		}
		if res.PointerFile != "" {
			fmt.Fprintf(e.Out, "已寫入指標：%s（沒有 commit）\n", res.PointerFile)
		}
		fmt.Fprintln(e.Out, res.Summary)
		// 收進來的子資料夾一個一行：發行的人一眼看出有沒有不該帶的資料夾（DEV-157）。
		for _, d := range res.IncludedDirs {
			fmt.Fprintf(e.Out, "收進來的子資料夾：%s/（%d 個檔）\n", d.Dir, d.Files)
		}
		return nil
	case "cards":
		res, err := personapack.ExportCards(personapack.CardsOptions{Home: home, Repo: vals["repo"]})
		if err != nil {
			return failMsg(e, err)
		}
		if on["json"] {
			emit(res)
			return nil
		}
		printCards(e, res)
		return nil
	case "sync":
		res, delegated, err := syncViaService(e, home, pointer, on["force"])
		if !delegated {
			// 服務沒在跑（或是還不認得 pack_sync 的舊服務）：指令自己同步。紀錄要經服務寫——連得上的舊服務照舊經它寫，
			// 連不上就只換檔案、紀錄留給服務啟動後補（PRS-5、PRS-6），並說清楚。
			var store personapack.Store
			running := platform.ServiceRunning(home) != ""
			if running {
				store = clientStore(homeClient(home), e.By)
			}
			res, err = syncPack(e.Ctx, personapack.SyncOptions{Home: home, PointerPath: pointer, Force: on["force"], Store: store})
			if !running && res != nil {
				note := "這個家的服務沒在跑，這次由指令自己同步"
				if res.RecordsLater {
					note += "：角色包的檔案換好了，跟著角色走的紀錄（例如官方衣服的目錄）要經服務寫，服務啟動後會自動補上"
				}
				if on["json"] {
					fmt.Fprintln(e.Err, note)
				} else {
					fmt.Fprintln(e.Out, note)
				}
			}
		}
		if on["json"] && res != nil {
			emit(res)
		} else if res != nil {
			fmt.Fprintln(e.Out, res.Message())
			for _, p := range res.UserEdited {
				fmt.Fprintf(e.Out, "你改過、沒換：%s\n", p)
			}
			for _, k := range res.RecordsKept {
				fmt.Fprintf(e.Out, "你改過、沒換的紀錄：%s\n", k)
			}
			for _, k := range res.RecordsSkipped {
				fmt.Fprintf(e.Out, "略過的紀錄：%s\n", k)
			}
			if len(res.NewCards) > 0 {
				fmt.Fprintf(e.Out, "新角色：%s（會在白天各挑一個時間來跟你打招呼；晚上 8 點以後同步的排到明天）\n", strings.Join(res.NewCards, "、"))
			}
			if res.Backup != "" {
				fmt.Fprintf(e.Out, "蓋掉的檔案備份在：%s\n", res.Backup)
			}
		}
		if err != nil {
			if res == nil {
				return failMsg(e, err)
			}
			return cmdline.ErrReported
		}
		return nil
	}
	st, err := personapack.Status(home, pointer)
	if err != nil {
		return failMsg(e, err)
	}
	if on["json"] {
		emit(st)
		return nil
	}
	fmt.Fprintf(e.Out, "登記的版號：%s\n這一版的版號：%s\n", orNone(st.Registered, "還沒同步過"), orNone(st.Latest, "還沒有公開的角色包"))
	if st.Source {
		fmt.Fprintln(e.Out, "這個家是角色包的來源，不同步角色包")
	}
	if st.Pending != nil {
		fmt.Fprintf(e.Out, "待補：%s（%s）\n", st.Pending.Reason, st.Pending.Version)
	}
	if st.Last != nil {
		fmt.Fprintf(e.Out, "上次：%s（%s）\n", st.Last.Message(), st.Last.At)
	}
	return nil
}

// printCards 印寫進 repo 的公開卡（PRS-26）。
func printCards(e *cmdline.Env, r *personapack.CardsResult) {
	fmt.Fprintf(e.Out, "跟著程式的公開卡：%s（沒有 commit）\n", r.Dir)
	for _, row := range []struct {
		label string
		slugs []string
	}{{"寫入", r.Written}, {"沒變", r.Unchanged}, {"拿掉（不再公開）", r.Removed}} {
		if len(row.slugs) > 0 {
			fmt.Fprintf(e.Out, "%s：%s\n", row.label, strings.Join(row.slugs, "、"))
		}
	}
	for _, s := range r.Skipped {
		fmt.Fprintf(e.Out, "沒收：%s\n", s)
	}
}

// OfflineChecks 是 `evergenie doctor` 的離線檢查之一（doctor.Commands 的 extra，registry 傳進去）：
// 角色包沒補上（沒網路、下載或驗證失敗）時黃字；有網路時 serve 會自動再試（REQ-283）；沒有待補就不報。
func OfflineChecks(_ context.Context, home string) []sdk.DoctorCheck {
	note := personapack.PendingNote(home)
	if note == "" {
		return nil
	}
	return []sdk.DoctorCheck{{ID: "packages.personas_pack", Group: sdk.DoctorGroupPackages, Severity: sdk.DoctorSeverityWarn, Detail: note, Fix: "有網路時服務每 6 小時會自動再試（紀錄還沒寫時幾十秒內就再試）；想馬上補可以跑 `evergenie personas pack sync`"}}
}
