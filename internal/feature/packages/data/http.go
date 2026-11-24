package data

import (
	"context"
	"errors"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// vaultService：讀（Get／List／Watch）什麼種類都開；寫只開給套件的資料種類（種類名稱含 `.`，D-28、REQ-173）。
// 核心的種類（任務、待辦、秘書…）的寫入一律走對應模組（API-1），這裡只說明該去哪裡。
type vaultService struct{ m *Module }

var _ sdk.VaultAPI = (*vaultService)(nil)

// Routes 登記要掛的路由（app.RouteProvider）：整個 sdk `vault` 服務。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "vault", Impl: &vaultService{m: m}}}
}

// store 回 vault；沒接上回 unavailable。
func (s *vaultService) store() (VaultStore, error) {
	if s.m.d.Vault == nil {
		return nil, platform.Unavailable("資料服務還沒接上")
	}
	return s.m.d.Vault, nil
}

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *vaultService) fail(err error) error {
	out := platform.SDKError(err)
	if log := s.m.logger(); out.Code == sdk.ErrorCodeInternal && log != nil && !errors.Is(err, context.Canceled) {
		log.Error("request failed", "error", err)
	}
	return out
}

// by 回每個寫入都要帶的操作者名稱。
func by(a *sdk.Actor) (string, error) { return platform.ActorName(a) }

// pump 把模組的 channel 轉給串流的 send，直到任一邊結束。
func pump[T any](ctx context.Context, ch <-chan T, send func(T) error) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case v, ok := <-ch:
			if !ok {
				return nil
			}
			if err := send(v); err != nil {
				return err
			}
		}
	}
}

var errVaultWrite = platform.Forbidden("請用對應模組的指令")

// packageKind：套件的資料種類叫 `<套件 id>.<種類>`；核心的種類沒有 `.`。
func packageKind(kind string) bool { return strings.Contains(kind, ".") }

// NightQueueID 是夜班清單的 note 編號（vault/agents/system/notes/night-queue.md）。
// 它是唯一一筆開放經 API 整筆寫入（put）的核心種類資料：夜班清單由主人在看板、秘書用 `evergenie night` 改（P6-6），
// 沒有自己的模組，內容是純文字，所以直接走 vault 的 get／put（有 by、變更紀錄、樂觀鎖）。
const NightQueueID = "system/night-queue"

func nightQueue(kind, id string) bool { return kind == "note" && id == NightQueueID }

func (s *vaultService) Get(ctx context.Context, req *sdk.VaultGetRequest) (*sdk.VaultGetResponse, error) {
	st, err := s.store()
	if err != nil {
		return nil, s.fail(err)
	}
	r, err := st.Get(ctx, req.Kind, req.ID)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.VaultGetResponse{Record: &r}, nil
}
func (s *vaultService) List(ctx context.Context, req *sdk.VaultListRequest) (*sdk.VaultListResponse, error) {
	st, err := s.store()
	if err != nil {
		return nil, s.fail(err)
	}
	list, err := st.List(ctx, req.Kind, vault.Query{Where: req.Where, IncludeArchived: req.IncludeArchived, Page: req.Page})
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.VaultListResponse{Records: list}, nil
}
func (s *vaultService) Put(ctx context.Context, req *sdk.VaultPutRequest) (*sdk.VaultPutResponse, error) {
	st, err := s.store()
	if err != nil {
		return nil, s.fail(err)
	}
	if req.Record == nil || !packageKind(req.Record.Kind) && !nightQueue(req.Record.Kind, req.Record.ID) {
		return nil, s.fail(errVaultWrite)
	}
	who, err := by(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	r, err := st.Put(ctx, *req.Record, who, req.ExpectedRevision)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.VaultPutResponse{Record: &r}, nil
}
func (s *vaultService) Patch(ctx context.Context, req *sdk.VaultPatchRequest) (*sdk.VaultPatchResponse, error) {
	st, err := s.store()
	if err != nil {
		return nil, s.fail(err)
	}
	if !packageKind(req.Kind) {
		return nil, s.fail(errVaultWrite)
	}
	who, err := by(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if req.ExpectedRevision != "" {
		cur, err := st.Get(ctx, req.Kind, req.ID)
		if err != nil {
			return nil, s.fail(err)
		}
		if cur.Revision != req.ExpectedRevision {
			return nil, s.fail(platform.Conflict("資料已更新，請重新讀取後再試"))
		}
	}
	r, err := st.Patch(ctx, req.Kind, req.ID, req.Set, req.Unset, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.VaultPatchResponse{Record: &r}, nil
}

// Changes 讀變更紀錄（設定→紀錄）；只讀，什麼種類都看得到。
func (s *vaultService) Changes(ctx context.Context, req *sdk.VaultChangesRequest) (*sdk.VaultChangesResponse, error) {
	st, err := s.store()
	if err != nil {
		return nil, s.fail(err)
	}
	items, next, err := st.Changes(ctx, vault.ChangesQuery{Since: req.Since, Until: req.Until, By: req.By, Kind: req.Kind, Limit: int(req.Limit), Before: req.Before})
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.VaultChangesResponse{Items: items, NextBefore: next}, nil
}
func (s *vaultService) Append(ctx context.Context, req *sdk.VaultAppendRequest) (*sdk.VaultAppendResponse, error) {
	return nil, s.fail(errVaultWrite)
}

// Archive 搬進 archive/（不刪除）。套件的資料沒有自動封存規則，所以這裡是使用者明確要求的封存（ArchiveNow）。
func (s *vaultService) Archive(ctx context.Context, req *sdk.VaultArchiveRequest) (*sdk.VaultArchiveResponse, error) {
	st, err := s.store()
	if err != nil {
		return nil, s.fail(err)
	}
	if !packageKind(req.Kind) {
		return nil, s.fail(errVaultWrite)
	}
	who, err := by(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if err := st.ArchiveNow(ctx, req.Kind, req.ID, who); err != nil {
		return nil, s.fail(err)
	}
	return &sdk.VaultArchiveResponse{}, nil
}
func (s *vaultService) NextId(ctx context.Context, req *sdk.VaultNextIdRequest) (*sdk.VaultNextIdResponse, error) {
	st, err := s.store()
	if err != nil {
		return nil, s.fail(err)
	}
	if !packageKind(req.Kind) {
		return nil, s.fail(errVaultWrite)
	}
	id, err := st.NextID(ctx, req.Kind)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.VaultNextIdResponse{ID: id}, nil
}
func (s *vaultService) Watch(ctx context.Context, req *sdk.VaultWatchRequest, send func(*sdk.VaultWatchResponse) error) error {
	st, err := s.store()
	if err != nil {
		return s.fail(err)
	}
	ch, err := st.Watch(ctx, req.Kinds...)
	if err != nil {
		return s.fail(err)
	}
	return pump(ctx, ch, send)
}
