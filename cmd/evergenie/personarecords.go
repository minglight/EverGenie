package main

import (
	"context"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/feature/agents/persona"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/internal/feature/packages/pkg"
	"github.com/minglight/evergenie/sdk"
)

// personaRecords 是 serve 裡角色包同步（personas.pack_retry）用的套件資料（PKG-59）：種類宣告從 packages、紀錄經 vault 模組的正常寫入。
// 指令行程那一份是角色庫的 sdkStore（經 API）；兩份都只看套件的宣告，角色庫不 import packages 與 vault 的寫入。
// by 是寫入的署名：角色包的定期再試是 system；看板的匯入（personas.import）經 As 換成主人（PRS-18）。
type personaRecords struct {
	pkgs  *pkg.Module
	vault *vault.Module
	by    string
	// official：只讀寫資料層的官方層（家外；DEV-154）。角色包同步在「官方角色放家外」的家用這一份，家裡的檔一個都不寫。
	official bool
}

// OfficialStore 回只讀寫官方層的同一份 Store；這個家沒有官方層回 nil（personapack.OfficialProvider）。
func (p personaRecords) OfficialStore() personapack.Store {
	if !p.vault.HasOfficialLayer() {
		return nil
	}
	p.official = true
	return p
}

var (
	_ personapack.Store            = personaRecords{}
	_ persona.ActorStore           = personaRecords{}
	_ personapack.ArchivedLister   = personaRecords{}
	_ personapack.UnreadableLister = personaRecords{}
)

// As 回署名換成 by 的同一份 Store。
func (p personaRecords) As(by string) personapack.Store { p.by = by; return p }

func (p personaRecords) Kinds(context.Context) ([]personapack.KindInfo, error) {
	// 套件服務還沒準備好時 Packages() 是空的：回 UNAVAILABLE，免得同步把每一筆都當成「套件沒裝」略過、還登記成已同步。
	if err := p.pkgs.Ready(); err != nil {
		return nil, err
	}
	var out []personapack.KindInfo
	for _, pk := range p.pkgs.Packages() {
		if pk.State == sdk.PackageStateRejected || !pk.Enabled {
			continue
		}
		for _, d := range pk.Data {
			out = append(out, personapack.KindInfo{Kind: pk.ID + "." + d.Kind, Decl: d})
		}
	}
	return out, nil
}

func (p personaRecords) List(ctx context.Context, kind string) ([]sdk.Record, error) {
	if p.official {
		return p.vault.OfficialList(kind), nil
	}
	return p.vault.List(ctx, kind, vault.Query{})
}

// ListWithArchived 連封存的一起列（personapack.ArchivedLister）：看板的匯出照全家的照片紀錄挑檔，封存了的隱藏版也要擋（DEV-160 審查 M1）。
func (p personaRecords) ListWithArchived(ctx context.Context, kind string) ([]sdk.Record, error) {
	if p.official {
		return p.vault.OfficialList(kind), nil
	}
	return p.vault.List(ctx, kind, vault.Query{IncludeArchived: true})
}

// Unreadable 列出停用中與被拒的套件（personapack.UnreadableLister；跟指令行程的 sdk store 同一份做法）：
// 照紀錄帶圖的套件讀不到時，看板的匯出也不做。
func (p personaRecords) Unreadable(context.Context) ([]personapack.UnreadablePackage, error) {
	if err := p.pkgs.Ready(); err != nil {
		return nil, err
	}
	return personapack.UnreadableOf(p.pkgs.Packages()), nil
}

func (p personaRecords) Get(ctx context.Context, kind, id string) (*sdk.Record, error) {
	if p.official {
		if r, ok := p.vault.OfficialGet(kind, id); ok {
			return &r, nil
		}
		return nil, nil
	}
	r, err := p.vault.Get(ctx, kind, id)
	if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (p personaRecords) Put(ctx context.Context, rec sdk.Record, rev string) error {
	if p.official {
		_, err := p.vault.OfficialPut(ctx, rec)
		return err
	}
	by := p.by
	if by == "" {
		by = "system"
	}
	_, err := p.vault.Put(ctx, rec, by, rev)
	return err
}
