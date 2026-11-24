package run

import (
	"context"
	"errors"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// SDKVault 讓 Service 經 SDK 的 client 讀寫 vault（寫入的操作者是 closet-mine，PKC-N3）。
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

// AdultSetting 讀衣櫃的擴充點 closet.adult（看板「設定 → 套件」設的值，跟衣櫃同一個開關）：值是 on 才算開；讀不到當關。
func AdultSetting(c sdk.PackageAPI) func(ctx context.Context) bool {
	return func(ctx context.Context) bool {
		r, err := c.ListExtensions(ctx, &sdk.PackageListExtensionsRequest{Package: "closet"})
		if err != nil || r == nil {
			return false
		}
		return strings.EqualFold(strings.TrimSpace(r.Values["closet.adult"]), "on")
	}
}

// DevHome 問核心這個家是不是開發家（system.get_mode 的 env，公開的 SDK 介面）。
func DevHome(c sdk.SystemAPI) func(ctx context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		r, err := c.GetMode(ctx, &sdk.SystemGetModeRequest{})
		if err != nil {
			return false, err
		}
		return r.Env == sdk.HomeEnvDev, nil
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
	svc.Adult = AdultSetting(sdk.NewPackageClient(host.Client))
	svc.Dev = DevHome(sdk.NewSystemClient(host.Client))
	return svc.Run(ctx)
}
