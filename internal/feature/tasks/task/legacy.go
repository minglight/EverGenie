package task

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
)

// 舊狀態怎麼轉（2026-11-01，九種收成六種，設計 design-goal-task.md）：
//
//	review（等你驗收）→ waiting_user（等你），proposed 留著；沒有 ask 就補 reviewAsk
//	failed（無法完成）→ cancelled（不做了），result 前面加「無法完成：」
//	shelved（擱置）   → cancelled（不做了），result 前面加「擱置：」；補 closed_at（取最後更新時間），一般任務補 accepted_by／accepted_at
//	proposed 的 failed → cancelled
//
// idea 不用轉：「待做」的值沿用 idea。轉完的單舊版照樣讀得懂（只用舊版也有的值、補齊舊版要的欄位）。
// 讀取（decode）、每次寫入前（load）、家的遷移（MigrateLegacyStatuses）都用同一個 upgradeLegacy。

// reviewAsk 是交付等驗收的「要主人做什麼」：Submit 與遷移都補這句（舊版的 waiting_user 一定要有 ask）。
const reviewAsk = "請驗收"

// LegacyAcceptedBy 是遷移時替擱置的一般任務補的 accepted_by（跟舊系統匯入的舊單一樣是例外，不是 owner(…)）。
const LegacyAcceptedBy = "migrate"

const legacyFailedPrefix, legacyShelvedPrefix = "無法完成：", "擱置："

// upgradeLegacy 把一張單的舊狀態轉成新狀態（就地改 fields），有改回 true。
// fallback 是 shelved 沒有 closed_at 時用的時間（RFC3339，通常是單的 updated_at）。
func upgradeLegacy(f map[string]any, fallback string) bool {
	str := func(k string) string { s, _ := f[k].(string); return s }
	changed := false
	if str("proposed") == "failed" {
		f["proposed"] = "cancelled"
		changed = true
	}
	prefix := func(p string) {
		if r := str("result"); !strings.HasPrefix(r, p) {
			f["result"] = p + r
		}
	}
	switch str("status") {
	case "review":
		f["status"] = "waiting_user"
		if strings.TrimSpace(str("ask")) == "" {
			f["ask"] = reviewAsk
		}
		changed = true
	case "failed":
		f["status"] = "cancelled"
		prefix(legacyFailedPrefix)
		changed = true
	case "shelved":
		f["status"] = "cancelled"
		prefix(legacyShelvedPrefix)
		if str("closed_at") == "" {
			at := fallback
			if at == "" {
				at = str("updated_at")
			}
			f["closed_at"] = at
		}
		if str("type") != "ask" {
			if str("accepted_by") == "" {
				f["accepted_by"] = LegacyAcceptedBy
			}
			if str("accepted_at") == "" {
				f["accepted_at"] = f["closed_at"]
			}
		}
		changed = true
	}
	if _, ok := f["site"]; ok { // Site 欄位拿掉（法律第 7 條，每台一個家）
		delete(f, "site")
		changed = true
	}
	return changed
}

// MigrateLegacyStatuses 是家的遷移（update 的 HomeMigration，由 cmd/evergenie 組裝）：
// vault/tasks/ 底下（含 archive）每張單的舊狀態轉成新狀態、拿掉 site 欄位。能重跑：轉過的不會再動。
// 在 serve 開起來之前跑，直接改檔（原子寫入），不經 vault 模組。
func MigrateLegacyStatuses(home string) error {
	root := filepath.Join(home, "vault", "tasks")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == root {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() {
			if d.Name() == "_files" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		r, order, err := vault.Decode(data)
		if err != nil {
			return nil // 壞檔不是這次的事：照舊留給 doctor 報
		}
		if !upgradeLegacy(r.Fields, "") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return platform.AtomicWrite(path, vault.Encode(r, order, vault.Spec{}), info.Mode().Perm())
	})
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
