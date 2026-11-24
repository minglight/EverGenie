package persona

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personaexport"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

// personas export／import（DEV-116、PRS-8～10、D-63）：檔案直接在家裡讀寫（同 personas pack），
// 套件紀錄經 sdk 的 package 與 vault 服務（跟 evergenie data 同一組 API），寫入一樣有檔頭、驗證與事件。

const personasExportUsage = `用法：evergenie personas export --persona <代號> [--persona <代號>…] --out <檔> [--json]
  把角色（卡、圖、全身照、衣櫃資料夾、目錄卡，加上套件說要跟著角色走的紀錄，例如衣服目錄）打成一個 .hgpersona 檔；
  --persona 可以給好幾次或用逗號分開；--out 沒有副檔名就補 .hgpersona，已經有這個檔不蓋。`

const personasImportUsage = `用法：evergenie personas import <檔> [--dry-run] [--force] [--json]
  匯入別的家匯出的角色：全部先驗（sha256、格式、紀錄欄位）才寫；只新增與替換、不刪。
  你改過的檔與已經有的紀錄不蓋（列在「你改過的，沒換」），--force 才蓋，蓋之前備份到 runtime/backups/persona-import/。
  這個家沒裝或停用的套件，那些紀錄略過並列出。--dry-run 只預覽、什麼都不寫。`

// sdkStore 是指令行程用的 personaexport.Store：種類從 package 服務的清單來，紀錄經 vault 服務。
type sdkStore struct {
	pkgs  sdk.PackageAPI
	vault sdk.VaultAPI
	by    string
}

func newSDKStore(e *cmdline.Env) *sdkStore { return clientStore(e.Client, e.By) }

func clientStore(c *sdk.Client, by string) *sdkStore {
	return &sdkStore{pkgs: sdk.NewPackageClient(c), vault: sdk.NewVaultClient(c), by: by}
}

func (s *sdkStore) Kinds(ctx context.Context) ([]personaexport.KindInfo, error) {
	r, err := s.pkgs.List(ctx, &sdk.PackageListRequest{})
	if err != nil {
		return nil, reachable(err)
	}
	var out []personaexport.KindInfo
	for _, p := range r.Packages {
		if p.State == sdk.PackageStateRejected || !p.Enabled {
			continue
		}
		for _, d := range p.Data {
			out = append(out, personaexport.KindInfo{Kind: p.ID + "." + d.Kind, Decl: d})
		}
	}
	return out, nil
}

// Unreadable 列出停用中與被拒的套件，和它們宣告要跟著角色走的資料（personapack.UnreadableLister；建包與匯出確定沒漏掉紀錄、隱藏版的資料夾與照片紀錄）。
func (s *sdkStore) Unreadable(ctx context.Context) ([]personapack.UnreadablePackage, error) {
	r, err := s.pkgs.List(ctx, &sdk.PackageListRequest{})
	if err != nil {
		return nil, reachable(err)
	}
	return personapack.UnreadableOf(r.Packages), nil
}

func (s *sdkStore) List(ctx context.Context, kind string) ([]sdk.Record, error) {
	r, err := s.vault.List(ctx, &sdk.VaultListRequest{Kind: kind})
	if err != nil {
		return nil, reachable(err)
	}
	return r.Records, nil
}

// ListWithArchived 連封存的一起列（personapack.ArchivedLister；建包找隱藏版資料夾用）。
func (s *sdkStore) ListWithArchived(ctx context.Context, kind string) ([]sdk.Record, error) {
	r, err := s.vault.List(ctx, &sdk.VaultListRequest{Kind: kind, IncludeArchived: true})
	if err != nil {
		return nil, reachable(err)
	}
	return r.Records, nil
}

func (s *sdkStore) Get(ctx context.Context, kind, id string) (*sdk.Record, error) {
	r, err := s.vault.Get(ctx, &sdk.VaultGetRequest{Kind: kind, ID: id})
	if err != nil {
		var se *sdk.Error
		if errors.As(err, &se) && se.Code == sdk.ErrorCodeNotFound {
			return nil, nil
		}
		return nil, reachable(err)
	}
	return r.Record, nil
}

func (s *sdkStore) Put(ctx context.Context, rec sdk.Record, rev string) error {
	_, err := s.vault.Put(ctx, &sdk.VaultPutRequest{Record: &rec, By: &sdk.Actor{Name: s.by}, ExpectedRevision: rev})
	if err != nil {
		return reachable(err)
	}
	return nil
}

// reachable 把「連不到 EverGenie」換成白話：套件紀錄要經這個家的 EverGenie 讀寫。核心回的錯誤（sdk.Error）原樣傳回。
func reachable(err error) error {
	var se *sdk.Error
	if errors.As(err, &se) {
		return err
	}
	return platform.Unavailable("連不到這個家的 EverGenie（serve 沒在跑？）：跟著角色走的套件紀錄要經它讀寫")
}

// splitPersonaFlags 取出可以重複、可以逗號分開的 --persona，其餘參數原樣留下。
func splitPersonaFlags(args []string) (personas, rest []string, ok bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		var v string
		switch {
		case a == "--persona":
			if i+1 >= len(args) {
				return nil, nil, false
			}
			i++
			v = args[i]
		case strings.HasPrefix(a, "--persona="):
			v = strings.TrimPrefix(a, "--persona=")
		default:
			rest = append(rest, a)
			continue
		}
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				personas = append(personas, p)
			}
		}
	}
	return personas, rest, true
}

// bundledFor 是跟著程式的公開卡資料夾（PRS-25）；沒接（nil）回空字串＝只看官方位置。
func bundledFor(bundledCards func(home string) string, home string) string {
	if bundledCards == nil {
		return ""
	}
	return bundledCards(home)
}

func printJSON(e *cmdline.Env, v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintln(e.Out, string(data))
}

func personasExport(e *cmdline.Env, args []string, bundledCards func(home string) string) error {
	personas, rest, ok := splitPersonaFlags(args)
	if !ok {
		return cmdline.Usagef("%s", personasExportUsage)
	}
	vals, on, left, ok := cmdline.LocalFlags(rest, []string{"out", "home"}, []string{"json", "help"})
	if on["help"] {
		_, err := fmt.Fprintln(e.Out, personasExportUsage)
		return err
	}
	if !ok || len(left) > 0 {
		return cmdline.Usagef("%s", personasExportUsage)
	}
	home, err := resolveHome(vals["home"])
	if err != nil {
		return failMsg(e, err)
	}
	res, out, err := personaexport.ExportToFile(e.Ctx, personaexport.ExportOptions{Home: home, Personas: personas, Store: newSDKStore(e), Bundled: bundledFor(bundledCards, home)}, vals["out"])
	if err != nil {
		return failMsg(e, err)
	}
	if on["json"] {
		printJSON(e, struct {
			*personaexport.ExportResult
			Out string `json:"out"`
		}{res, out})
		return nil
	}
	fmt.Fprintln(e.Out, res.Message())
	fmt.Fprintf(e.Out, "檔案：%s（%s）\n", out, humanSize(res.Bytes))
	for _, s := range res.Skipped {
		fmt.Fprintf(e.Out, "沒帶：%s\n", s)
	}
	return nil
}

func personasImport(e *cmdline.Env, args []string, bundledCards func(home string) string) error {
	vals, on, left, ok := cmdline.LocalFlags(args, []string{"home"}, []string{"json", "dry-run", "force", "help"})
	if on["help"] {
		_, err := fmt.Fprintln(e.Out, personasImportUsage)
		return err
	}
	if !ok || len(left) != 1 {
		return cmdline.Usagef("%s", personasImportUsage)
	}
	home, err := resolveHome(vals["home"])
	if err != nil {
		return failMsg(e, err)
	}
	res, err := personaexport.ImportFile(e.Ctx, personaexport.ImportOptions{Home: home, Store: newSDKStore(e), DryRun: on["dry-run"], Force: on["force"], Bundled: bundledFor(bundledCards, home)}, left[0])
	if err != nil {
		return failMsg(e, err)
	}
	if on["json"] {
		printJSON(e, res.SDK()) // 跟 personas.import 的回應同一個型別
		return nil
	}
	fmt.Fprintln(e.Out, res.Message())
	lines := []struct {
		label string
		items []string
	}{
		{"新增", res.Added}, {"換掉", res.Replaced}, {"你改過的，沒換", res.Kept},
		{"新增紀錄", res.RecordsAdded}, {"換掉紀錄", res.RecordsReplaced}, {"已經有、沒換的紀錄", res.RecordsKept}, {"略過的紀錄", res.RecordsSkipped},
	}
	for _, l := range lines {
		for _, it := range l.items {
			fmt.Fprintf(e.Out, "%s：%s\n", l.label, it)
		}
	}
	if res.Backup != "" {
		fmt.Fprintf(e.Out, "蓋掉的東西備份在：%s\n", res.Backup)
	}
	return nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
