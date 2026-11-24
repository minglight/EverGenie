package run

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/sdk"
)

// SDKVault 讓 Service 經 SDK 的 client 讀寫 vault（寫入的操作者是 habits，PKC-N3）。
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

func (v SDKVault) Watch(ctx context.Context, kinds []string, onChange func()) error {
	return v.C.Watch(ctx, &sdk.VaultWatchRequest{Kinds: kinds}, func(*sdk.VaultWatchResponse) error {
		onChange()
		return nil
	})
}

// SDKNotifier 經 SDK 的 client 送通知。
type SDKNotifier struct{ C sdk.NotifyAPI }

func (n SDKNotifier) Send(ctx context.Context, req *sdk.NotifySendRequest) error {
	_, err := n.C.Send(ctx, req)
	return err
}

// Program 是 sdk.PackageProgram：報到成功後跑 Service。
type Program struct {
	Stderr interface{ Write([]byte) (int, error) }
}

// Run 實作 sdk.PackageProgram。
func (p Program) Run(ctx context.Context, host *sdk.PackageHost) error {
	svc := NewService(SDKVault{C: sdk.NewVaultClient(host.Client)}, SDKNotifier{C: sdk.NewNotifyClient(host.Client)}, p.Stderr)
	return svc.Run(ctx)
}
