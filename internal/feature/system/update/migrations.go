package update

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
)

// 家的遷移（2026-11-01 從 platform/update.go 搬來）：升級時對家的一次性整理，serve 啟動時先跑（cmd/evergenie/serve.go），
// `evergenie migrate-home` 列出或手動跑；升級失敗退回時，記錄檔 MigrationsFile 跟著還原（upgrade.go）。

// ---- 家的遷移（HomeMigration）---------------------------------------------------------------------------

// HomeMigration 是升級時對「家」的一次性整理（欄位改名、檔案搬位置…）。
// 規矩：能重跑（跑到一半掛了再跑一次要得到同樣結果）。核心自己的遷移要讓舊版照樣讀得懂這個家；官方套件的資料轉換
// （Group "pkg"）可能不相容，所以整理過資料的升級退回時靠升級前的備份把資料一起還原（upgrade.go，UPD-9）。
// 升級會先在家的複本上試跑（PreviewMigrations）：複本只有 home.toml、settings/、vault/，而且狀態資料夾指到暫存的空資料夾。
// 退回時「資料回到升級前」只還原 home.toml、遷移紀錄與 vault/ 裡核心管的資料夾（upgrade.go 的 inRestoreScope）：
// 遷移只能改這個範圍裡的東西，要動別的地方（packages/、personas/、media/、settings/ 其他檔、狀態資料夾）就不適合放這裡。
type HomeMigration struct {
	ID          string
	Description string
	Run         func(home string) error
	// Group 不是空的：同一組裡還沒跑過的遷移要一起做（RunGroup 一次收到它們的 id，全部成功才一起記成跑過）。
	// 給「分開驗證會卡住」的用：同一種資料連續兩個格式轉換，要串起來套完才驗證。
	Group    string
	RunGroup func(home string, ids []string) error
	// Undo 不是 nil：退回到這個遷移跑之前的版本時（restoreHome 還原了遷移紀錄、這個 id 從「跑過」變成「沒跑過」），
	// 還原之後叫一次，收掉新版自己產生、備份裡沒有、舊版讀不懂的檔（例：核心產生的索引）。只能刪新版產生的東西，不碰主人的資料。
	Undo func(home string) error
}

// HomeMigrations 是核心要跑的遷移，照順序。由組裝點 cmd/evergenie 在 main 一開始填（homeMigrations()，各業務模組的整理函式在業務模組自己那裡）。
var HomeMigrations []HomeMigration

// MigrationsFile 是記錄「已經跑過的遷移」的檔案（相對家）。
const MigrationsFile = "settings/migrations.toml"

// AppliedMigrations 讀 settings/migrations.toml 的 applied（沒有檔案＝都沒跑過）。
func AppliedMigrations(home string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(MigrationsFile)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, platform.Unavailable("讀不了 " + MigrationsFile)
	}
	var d struct {
		Applied []string `toml:"applied"`
	}
	if _, err := toml.Decode(string(data), &d); err != nil {
		return nil, platform.Invalid("migrations", MigrationsFile+" 格式不對")
	}
	return d.Applied, nil
}

func writeApplied(home string, applied []string) error {
	var b strings.Builder
	b.WriteString("schema = 1\napplied = [")
	for i, id := range applied {
		if i > 0 {
			b.WriteString(", ")
		}
		q, _ := json.Marshal(id)
		b.Write(q)
	}
	b.WriteString("]\n")
	path := filepath.Join(home, filepath.FromSlash(MigrationsFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return platform.Unavailable("建不了 settings 資料夾")
	}
	return platform.AtomicWrite(path, []byte(b.String()), 0o644)
}

// MarkMigrationsApplied 把幾個 id 記成跑過（已經在的不重複）。給不經 RunMigrations 的路用：主人自己的、分享來的套件
// 在套件載入時自己做資料轉換（pkg 模組，組裝點接過來），紀錄還是同一份。
func MarkMigrationsApplied(home string, ids []string) error {
	applied, err := AppliedMigrations(home)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, id := range applied {
		have[id] = true
	}
	n := len(applied)
	for _, id := range ids {
		if !have[id] {
			applied, have[id] = append(applied, id), true
		}
	}
	if len(applied) == n {
		return nil
	}
	return writeApplied(home, applied)
}

// PendingMigrations 回 list 裡還沒跑過的遷移（保持順序）。
func PendingMigrations(home string, list []HomeMigration) ([]HomeMigration, error) {
	applied, err := AppliedMigrations(home)
	if err != nil {
		return nil, err
	}
	done := map[string]bool{}
	for _, id := range applied {
		done[id] = true
	}
	var out []HomeMigration
	for _, m := range list {
		if !done[m.ID] {
			out = append(out, m)
		}
	}
	return out, nil
}

// RunMigrations 依序跑 list 裡還沒跑過的遷移，每跑完一個就記下來；任何一個失敗就停（後面的不跑），回已經跑完的 id 與錯誤。
// 重複 id、沒有 Run 的遷移回 Invalid（開發時的錯誤）。
func RunMigrations(home string, list []HomeMigration) (ran []string, err error) {
	seen := map[string]bool{}
	for _, m := range list {
		if m.ID == "" || m.Run == nil || seen[m.ID] {
			return nil, platform.Invalid("migration", "遷移的 id 不可重複或空白，也一定要有 Run："+m.ID)
		}
		seen[m.ID] = true
	}
	pending, err := PendingMigrations(home, list)
	if err != nil {
		return nil, err
	}
	applied, _ := AppliedMigrations(home)
	fail := func(m HomeMigration, err error) error {
		msg := "家的整理「" + m.Description + "」（" + m.ID + "）沒做成"
		var pe *platform.Err
		if errors.As(err, &pe) && pe.Message != "" {
			msg += "：" + pe.Message
		} else if err != nil {
			msg += "：" + err.Error()
		}
		return platform.Unavailable(msg)
	}
	done := map[string]bool{}
	for _, m := range pending {
		if done[m.ID] {
			continue
		}
		ids := []string{m.ID}
		if m.Group != "" && m.RunGroup != nil {
			ids = nil
			for _, o := range pending {
				if o.Group == m.Group {
					ids = append(ids, o.ID)
				}
			}
			if err := m.RunGroup(home, ids); err != nil {
				return ran, fail(m, err)
			}
		} else if err := m.Run(home); err != nil {
			return ran, fail(m, err)
		}
		applied = append(applied, ids...)
		if err := writeApplied(home, applied); err != nil {
			return ran, err
		}
		for _, id := range ids {
			done[id] = true
		}
		ran = append(ran, ids...)
	}
	return ran, nil
}

// PackageMigrations 回家 home 裡各套件自己宣告的資料轉換（package.toml 的 [[migration]]，DEV-154）。由組裝點填
// （update 不 import 套件模組）；nil＝沒有。排在核心的遷移後面跑，紀錄同一份 settings/migrations.toml（id 是 pkg.<套件>.<轉換 id>）。
var PackageMigrations func(home string) []HomeMigration

// AllMigrations 是這個家要看的全部遷移：核心的在前，再來是各套件自己帶的。
func AllMigrations(home string) []HomeMigration {
	out := append([]HomeMigration(nil), HomeMigrations...)
	if PackageMigrations != nil {
		out = append(out, PackageMigrations(home)...)
	}
	return out
}

// RunHomeMigrations 跑這個家還沒跑過的遷移（核心的與套件的；serve 啟動時與 `migrate-home --apply`）。
func RunHomeMigrations(home string) ([]string, error) {
	return RunMigrations(home, AllMigrations(home))
}

// SortedCopy 回排序過的複本（跳過清單等顯示用）。
func SortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// PreviewMigrations 在家的複本上把還沒跑過的遷移整個走一遍（DEV-154，作者 2026-11-12：資料格式轉換要先驗證好）：
// 把 home.toml、settings/、vault/（備份、隱藏的、附件 _files 不帶）抄到暫存資料夾，在那裡依序跑；真的家一個檔都不動。
// 回會跑的 id；有一個沒過就回它的白話原因（已經過的 id 也回）。遷移清單照真的家算（套件的轉換要看家裡裝了哪些套件），
// 資料用複本的。暫存資料夾結束就刪。
func PreviewMigrations(home string) (ran []string, err error) {
	list := AllMigrations(home)
	pending, err := PendingMigrations(home, list)
	if err != nil || len(pending) == 0 {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "hg-migrate-preview-")
	if err != nil {
		return nil, platform.Unavailable("建不了預演用的暫存資料夾")
	}
	defer os.RemoveAll(tmp)
	for _, name := range []string{"home.toml", "settings", "vault"} {
		if err := copyForPreview(filepath.Join(home, name), filepath.Join(tmp, name)); err != nil {
			return nil, platform.Unavailable("抄不出預演用的複本（" + name + "）")
		}
	}
	if err := isolatePreviewState(tmp); err != nil {
		return nil, platform.Unavailable("預演用的複本設定不了自己的狀態資料夾")
	}
	return RunMigrations(tmp, list)
}

// isolatePreviewState 把複本 home.toml 的 [layout] state 改指到複本自己底下的空資料夾：真的家常把它寫成絕對路徑，
// 不改的話在複本上試跑的遷移會讀寫到真的狀態資料夾。沒有 [layout]（第 1 版佈局，狀態在家裡的 runtime/）不用改。
func isolatePreviewState(tmp string) error {
	path := filepath.Join(tmp, "home.toml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	q, _ := json.Marshal(filepath.Join(tmp, ".state"))
	want := "state = " + string(q)
	lines := strings.Split(string(data), "\n")
	in, found, header := false, false, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			in = t == "[layout]"
			if in {
				header = i
			}
			continue
		}
		if key, _, ok := strings.Cut(t, "="); in && ok && strings.TrimSpace(key) == "state" {
			lines[i], found = want, true
		}
	}
	if header < 0 {
		return nil
	}
	if !found {
		lines = append(lines[:header+1], append([]string{want}, lines[header+1:]...)...)
	}
	if err := os.MkdirAll(filepath.Join(tmp, ".state"), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600)
}

// copyForPreview 抄一個檔或資料夾；不帶備份（名字含 .bak-）、隱藏的（編號檔 .counters.toml 例外，遷移要新增紀錄時用得到）、
// 附件資料夾 _files、符號連結。來源不存在不算錯。
func copyForPreview(src, dst string) error {
	info, err := os.Lstat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o600)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		name := d.Name()
		if rel != "." && name != ".counters.toml" && (strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-") || name == "_files") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
}
