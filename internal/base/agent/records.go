package agent

// 名冊的單筆紀錄操作：讀、列、寫、改、搬、封存，給業務模組 agents 的名冊管理（建、封存、改名、換角色…）用。
// 這裡只做資料操作，不判斷誰能做、id 格式、白名單這些規則（規則在 internal/feature/agents/roster 的 manage.go）。
// 只碰 agent 擁有的種類（ownedKinds）；秘書名下的排程（job）擁有者是 jobs，由業務模組 agents 經 jobs 的介面處理。

import (
	"context"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// ownKinds 是 id 就是職位 id 的資料種類（agent 自己排第一）。
var ownKinds = []string{"agent", "addendum", "memory"}

// childKinds 是掛在一位秘書底下、id 是「<職位>/<…>」、擁有者是 agent 的資料種類。
var childKinds = []string{"note", "journal", "changelog"}

// owned 回這個種類是不是 agent 擁有的（profile 也是，但不掛在秘書底下）。
func owned(kind string) error {
	switch kind {
	case "agent", "addendum", "memory", "note", "journal", "changelog", "profile":
		return nil
	}
	return platform.Invalid("kind", "名冊只管 agent 自己的資料種類："+kind)
}

// Record 讀一筆 agent 擁有的紀錄（含封存的；找不到回 NotFound）。
func (m *Module) Record(ctx context.Context, kind, id string) (vault.Record, error) {
	if err := owned(kind); err != nil {
		return vault.Record{}, err
	}
	return m.d.Vault.Get(ctx, kind, id)
}

// Records 列出一位秘書名下還沒封存、agent 擁有的資料：先 agent、addendum、memory，再筆記、日誌、變更紀錄。
func (m *Module) Records(ctx context.Context, id string) ([]vault.Record, error) {
	var out []vault.Record
	for _, kind := range ownKinds {
		r, err := m.d.Vault.Get(ctx, kind, id)
		if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !r.Archived {
			out = append(out, r)
		}
	}
	for _, kind := range childKinds {
		rows, err := m.d.Vault.List(ctx, kind, vault.Query{})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if strings.HasPrefix(r.ID, id+"/") {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// PutAgent 寫一筆新的名冊紀錄（內文空白）。id 已經有了由 vault 回錯。
func (m *Module) PutAgent(ctx context.Context, id string, fields map[string]any, by string) (vault.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.d.Vault.Put(ctx, vault.Record{Kind: "agent", ID: id, Fields: fields}, by, "")
}

// PatchAgent 改名冊紀錄的欄位（set 寫入、unset 拿掉）。
func (m *Module) PatchAgent(ctx context.Context, id string, set map[string]any, unset []string, by string) (vault.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.d.Vault.Patch(ctx, "agent", id, set, unset, by)
}

// SetBody 整份改寫一筆 agent 擁有的紀錄的內文（沒有就建，名下的種類帶 agent 欄位）；不判斷權限。
func (m *Module) SetBody(ctx context.Context, kind, id, body, by string) error {
	if err := owned(kind); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBody(ctx, kind, id, body, by)
}

// ArchiveRecord 立刻封存一筆 agent 擁有的紀錄（搬進 archive/，不刪除）。
func (m *Module) ArchiveRecord(ctx context.Context, kind, id, by string) error {
	if err := owned(kind); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.d.Vault.ArchiveNow(ctx, kind, id, by)
}

// MoveRecord 把一筆 agent 擁有的紀錄改成新的 id，set 的欄位一起改。
func (m *Module) MoveRecord(ctx context.Context, kind, from, to string, set map[string]any, by string) (vault.Record, error) {
	if err := owned(kind); err != nil {
		return vault.Record{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.d.Vault.Move(ctx, kind, from, to, set, by)
}

// Changed 發 agent.changed（type：created、archived、renamed、assigned、updated）。
func (m *Module) Changed(ctx context.Context, id, what string) error { return m.changed(ctx, id, what) }

// Persona 讀人設卡（不擋設計中；套件不在時回暫代卡），給名冊管理算名字與別名。
func (m *Module) Persona(slug string) (vault.Record, error) { return m.persona(slug) }

// LivePersona 讀一張能指派的人設卡：設計中、套件停用的回 Invalid（白話原因）。
func (m *Module) LivePersona(slug string) (vault.Record, error) { return m.livePersona(slug) }
