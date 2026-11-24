package run

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
	"github.com/minglight/evergenie/sdk"
)

// SDKVault 讓 Service 經 SDK 的 client 讀寫 vault（寫入的操作者是 closet，PKC-N3）。
type SDKVault struct{ C sdk.VaultAPI }

func (v SDKVault) List(ctx context.Context, kind string, includeArchived bool) ([]sdk.Record, error) {
	r, err := v.C.List(ctx, &sdk.VaultListRequest{Kind: kind, IncludeArchived: includeArchived})
	if err != nil {
		return nil, err
	}
	return r.Records, nil
}

func (v SDKVault) Get(ctx context.Context, kind, id string) (*sdk.Record, error) {
	r, err := v.C.Get(ctx, &sdk.VaultGetRequest{Kind: kind, ID: id})
	if err != nil {
		var e *sdk.Error
		if errors.As(err, &e) && e.Code == sdk.ErrorCodeNotFound {
			return nil, nil
		}
		return nil, err
	}
	return r.Record, nil
}

func (v SDKVault) Put(ctx context.Context, rec sdk.Record, expectedRevision string) error {
	_, err := v.C.Put(ctx, &sdk.VaultPutRequest{Record: &rec, By: &sdk.Actor{Name: actor}, ExpectedRevision: expectedRevision})
	return err
}

func (v SDKVault) Patch(ctx context.Context, kind, id string, set map[string]any) error {
	_, err := v.C.Patch(ctx, &sdk.VaultPatchRequest{Kind: kind, ID: id, Set: set, By: &sdk.Actor{Name: actor}})
	return err
}

func (v SDKVault) Archive(ctx context.Context, kind, id string) error {
	_, err := v.C.Archive(ctx, &sdk.VaultArchiveRequest{Kind: kind, ID: id, By: &sdk.Actor{Name: actor}})
	return err
}

func (v SDKVault) Watch(ctx context.Context, kinds []string, onChange func()) error {
	return v.C.Watch(ctx, &sdk.VaultWatchRequest{Kinds: kinds}, func(*sdk.VaultWatchResponse) error {
		onChange()
		return nil
	})
}

// AdultSetting 讀衣櫃的擴充點 closet.adult（看板「設定 → 套件」設的值）：值是 on 才算開；讀不到（核心沒回應）當關。
func AdultSetting(c sdk.PackageAPI) func(ctx context.Context) bool {
	return func(ctx context.Context) bool {
		r, err := c.ListExtensions(ctx, &sdk.PackageListExtensionsRequest{Package: "closet"})
		if err != nil || r == nil {
			return false
		}
		return rules.AdultEnabled(r.Values["closet."+rules.AdultExtension])
	}
}

// EditTabSetting 讀擴充點 closet.edit-tab：值是 off 才關；沒設、讀不到都當開（只管分頁掛不掛，不是權限）。
func EditTabSetting(c sdk.PackageAPI) func(ctx context.Context) bool {
	return func(ctx context.Context) bool {
		r, err := c.ListExtensions(ctx, &sdk.PackageListExtensionsRequest{Package: "closet"})
		if err != nil || r == nil {
			return true
		}
		return rules.EditTabEnabled(r.Values["closet."+rules.EditTabExtension])
	}
}

// DevHome 問核心這個家是不是開發家（system.get_mode 的 env，公開的 SDK 介面；跟 closet-mine 的升成官方同一個問法）。
// 只有 env = dev 才是；讀不到回錯（不是「不是開發家」：caps.go 照上一輪的能力，也不自動上架，憲法第 10 條）。
func DevHome(c sdk.SystemAPI) func(ctx context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		r, err := c.GetMode(ctx, &sdk.SystemGetModeRequest{})
		if err != nil {
			return false, err
		}
		if r == nil {
			return false, errors.New("核心沒有回這個家的模式")
		}
		return r.Env == sdk.HomeEnvDev, nil
	}
}

// PackageList 列家裡的套件（核心的 package.list，公開的 SDK 介面）：驗登記的動作時看登記的套件在不在、宣告了哪些種類（CLO-33）。
func PackageList(c sdk.PackageAPI) func(ctx context.Context) ([]sdk.Package, error) {
	return func(ctx context.Context) ([]sdk.Package, error) {
		r, err := c.List(ctx, &sdk.PackageListRequest{})
		if err != nil {
			return nil, err
		}
		if r == nil {
			return nil, errors.New("package.list 沒有回應")
		}
		return r.Packages, nil
	}
}

// Program 是 sdk.PackageProgram：報到成功後跑 Service。
type Program struct {
	Stderr interface{ Write([]byte) (int, error) }
}

// Run 實作 sdk.PackageProgram。家的位置由核心用 EVERGENIE_HOME 給（公開資料夾與人設圖都在家裡）。
func (p Program) Run(ctx context.Context, host *sdk.PackageHost) error {
	home, err := homeFromEnv()
	if err != nil {
		return err
	}
	svc := NewService(SDKVault{C: sdk.NewVaultClient(host.Client)}, home, p.Stderr)
	pc := sdk.NewPackageClient(host.Client)
	svc.Adult = AdultSetting(pc)
	svc.Dev = DevHome(sdk.NewSystemClient(host.Client))
	svc.EditTab = EditTabSetting(pc)
	svc.Packages = PackageList(pc)
	return svc.Run(ctx)
}
