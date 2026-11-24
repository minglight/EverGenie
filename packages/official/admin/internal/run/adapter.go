package run

import (
	"context"
	"errors"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// SDKVault 讓 Service 經 SDK 的 client 讀寫 vault（寫入的操作者是 admin，PKC-N3）。
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

// Program 是 sdk.PackageProgram：報到成功後跑 Service。
type Program struct {
	Stderr interface{ Write([]byte) (int, error) }
}

// Run 實作 sdk.PackageProgram。家的位置由核心用 EVERGENIE_HOME 給（[admin] 設定在家的 home.toml）。
func (p Program) Run(ctx context.Context, host *sdk.PackageHost) error {
	home, err := homeFromEnv()
	if err != nil {
		return err
	}
	sys := sdk.NewSystemClient(host.Client)
	version := func(ctx context.Context) (string, error) {
		res, err := sys.Doctor(ctx, &sdk.SystemDoctorRequest{Groups: []sdk.DoctorGroup{sdk.DoctorGroupConfig}})
		if err != nil {
			return "", err
		}
		for _, c := range res.Checks {
			if c.ID == "config.version" {
				return strings.TrimPrefix(c.Detail, versionDetailPrefix), nil
			}
		}
		return "", nil
	}
	return NewService(SDKVault{C: sdk.NewVaultClient(host.Client)}, home, version, p.Stderr).Run(ctx)
}
