package agent

// 秘書自己的做事筆記（DEV-161；規格 ai_docs/schemas/memory.md）：vault/agents/<職位>/notes/<名字>.md，只有這個職位用的長文件
// （計畫、清單、草稿）。跟全家共用的主題筆記（notes.go）不同：這份只有本人（和 system）能寫、內文自由。
// 寫入一律經這裡（evergenie memory note write）：檔頭（schema、updated_at、by、agent、created_at）由核心補，秘書只給內文；
// 新檔不用版本，改舊檔要帶 show 看到的版本，兩個對話才不會互蓋。沒有檔頭的舊檔（直接用編輯器寫的）vault 讀得到
// （vault.headerlessOwnNote），經這裡寫回時才補上檔頭。

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

const maxOwnNoteName = 80

// CheckOwnNoteName 檢查做事筆記的名字：名字就是檔名（中文可），不含路徑符號、不是隱藏檔或備份檔名；回去掉前後空白與 .md 的名字。
func CheckOwnNoteName(name string) (string, error) {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".md")
	switch {
	case name == "":
		return "", platform.Invalid("name", "筆記名字不能空白")
	case utf8.RuneCountInString(name) > maxOwnNoteName:
		return "", platform.Invalid("name", "筆記名字太長（最多 80 個字）")
	case strings.ContainsAny(name, "/\\"):
		return "", platform.Invalid("name", "筆記名字就是檔名，不能有 / 或 \\（不開子資料夾）")
	case strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-") || name == "archive" || name == "索引":
		return "", platform.Invalid("name", "這個名字核心保留（隱藏檔、備份、archive、索引），請換一個")
	case strings.IndexFunc(name, unicode.IsControl) >= 0 || strings.TrimSpace(name) != name:
		return "", platform.Invalid("name", "筆記名字不能有控制字元，前後也不能有空白")
	}
	return name, nil
}

// WriteNote 寫（新增或整份取代）這位秘書自己的做事筆記；只有本人（by 等於 id）或 system 能寫。
// revision 空白＝新檔（已經有就回 conflict，請先看版本）；改舊檔要帶現在的版本，對不上回 conflict。
// 回實際用的名字、是不是新建的與寫完的版本。
func (m *Module) WriteNote(ctx context.Context, id, name, body, revision, by string) (final string, created bool, rev string, err error) {
	if by != id && by != "system" {
		return "", false, "", platform.Forbidden("只能寫自己的做事筆記")
	}
	if name, err = CheckOwnNoteName(name); err != nil {
		return "", false, "", err
	}
	if strings.TrimSpace(body) == "" {
		return "", false, "", platform.Invalid("body", "筆記內容是空的，沒有寫入")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.d.Vault.Get(ctx, "agent", id); err != nil {
		return "", false, "", err
	}
	key := id + "/" + name
	old, err := m.d.Vault.Get(ctx, "note", key)
	switch {
	case platform.CodeOf(err) == sdk.ErrorCodeNotFound:
		if revision != "" {
			return "", false, "", platform.Conflict("這份筆記「" + name + "」還不存在；新檔不用帶版本")
		}
		old = vault.Record{Kind: "note", ID: key, Fields: map[string]any{}}
	case err != nil:
		return "", false, "", err
	case old.Archived:
		return "", false, "", platform.Conflict("這份筆記「" + name + "」已封存，請換一個名字")
	case revision == "":
		return "", false, "", platform.Conflict("已經有這份筆記「" + name + "」：先用 evergenie memory note show 看版本，再帶 --revision 整份取代")
	}
	old.Fields["agent"] = id
	old.Body = body
	r, err := m.d.Vault.Put(ctx, old, by, revision)
	if err != nil {
		return "", false, "", err
	}
	return name, revision == "", r.Revision, nil
}
