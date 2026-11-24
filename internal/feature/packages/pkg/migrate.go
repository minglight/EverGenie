package pkg

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 套件自己帶的資料轉換（DEV-154，作者 2026-11-12：「官方套件的紀錄格式只能加欄位；真的要改就必須附自動轉換，而且要驗證好」）。
//
// 套件在 package.toml 用 [[migration]] 宣告「舊紀錄怎麼變成新格式」，只有四種動作，都能重跑：
//
//	rename   = { 舊欄位 = "新欄位" }                 欄位改名（新欄位已經有一樣的值就只拿掉舊的；兩邊值不同＝衝突，停下來）
//	values   = { 欄位 = { 舊值 = "新值" } }           某個欄位的值對照（字串，或字串清單裡的每一項）
//	defaults = { 欄位 = 值 }                         缺這個欄位就補上
//	remove   = ["欄位"]                              拿掉欄位
//
// 順序固定：rename → values → defaults → remove。核心升級時（serve 開起來之前、`migrate-home --apply`）照宣告順序跑，
// 跑過的記在 settings/migrations.toml（id 是 pkg.<套件>.<轉換 id>）。跑之前先整批預演：每一筆轉完都要符合套件現在的
// 資料規格，有一筆不合就整個不動（一個檔都不寫）。

var migrationIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// MigrationDecl 是 package.toml 的一個 [[migration]]。
type MigrationDecl struct {
	ID          string                       `toml:"id"`
	Description string                       `toml:"description"`
	Kind        string                       `toml:"kind"` // 套件自己的資料種類（不含套件 id）
	Rename      map[string]string            `toml:"rename"`
	Values      map[string]map[string]string `toml:"values"`
	Defaults    map[string]any               `toml:"defaults"`
	Remove      []string                     `toml:"remove"`
}

// touches：這個轉換有沒有處理到欄位 name（改名的來源、值對照、拿掉）。發版前的格式檢查用它判斷「改格式有沒有附轉換」。
func (d MigrationDecl) touches(name string) bool {
	if _, ok := d.Rename[name]; ok {
		return true
	}
	if _, ok := d.Values[name]; ok {
		return true
	}
	for _, r := range d.Remove {
		if r == name {
			return true
		}
	}
	return false
}

// fills：這個轉換會不會替舊紀錄補上欄位 name（補預設，或從別的欄位改名過來）。
func (d MigrationDecl) fills(name string) bool {
	if _, ok := d.Defaults[name]; ok {
		return true
	}
	for _, to := range d.Rename {
		if to == name {
			return true
		}
	}
	return false
}

// coreFields 是每一筆紀錄都有、由核心管的欄位：套件的轉換不能改名、補值或拿掉它們。
var coreFields = map[string]bool{"id": true, "kind": true, "schema": true, "created_at": true, "updated_at": true, "by": true}

// fieldNames 回這個轉換提到的所有欄位名（改名的兩邊、對照、補值、拿掉）。
func (d MigrationDecl) fieldNames() []string {
	var out []string
	for from, to := range d.Rename {
		out = append(out, from, to)
	}
	out = append(out, sortedKeys(d.Values)...)
	out = append(out, sortedKeys(d.Defaults)...)
	return append(out, d.Remove...)
}

// drops：這個轉換會不會把舊欄位 name 整個拿走（拿掉，或改名成別的）。型別換了只有這樣才算處理到（values 只會對照字串）。
func (d MigrationDecl) drops(name string) bool {
	if _, ok := d.Rename[name]; ok {
		return true
	}
	for _, n := range d.Remove {
		if n == name {
			return true
		}
	}
	return false
}

func validateMigrations(decls []MigrationDecl, data []ContribData) error {
	bad := func(format string, a ...any) error { return platform.Invalid("migration", fmt.Sprintf(format, a...)) }
	kinds := map[string]bool{}
	for _, d := range data {
		kinds[d.Kind] = true
	}
	seen := map[string]bool{}
	for _, d := range decls {
		if !migrationIDPattern.MatchString(d.ID) || seen[d.ID] {
			return bad("資料轉換的 id「%s」要是小寫英文、數字與 -（可以用日期開頭），而且不能重複", cutUTF8(strings.Join(strings.Fields(d.ID), " "), 60))
		}
		seen[d.ID] = true
		if strings.TrimSpace(d.Description) == "" {
			return bad("資料轉換「%s」要寫 description（白話說明改了什麼，主人看得懂）", d.ID)
		}
		// 說明會進錯誤訊息與給秘書的升級報告：只能一行、有長度上限，別人分享的套件不能靠它往報告裡塞一段指示（憲法第 10 條）。
		if strings.ContainsAny(d.Description, "\r\n") || len(d.Description) > extensionDescMax {
			return bad("資料轉換「%s」的 description 只能寫一行，而且不超過 %d 個位元組", d.ID, extensionDescMax)
		}
		if !kinds[d.Kind] {
			return bad("資料轉換「%s」的 kind 不是這個套件宣告的資料種類", d.ID)
		}
		if len(d.Rename)+len(d.Values)+len(d.Defaults)+len(d.Remove) == 0 {
			return bad("資料轉換「%s」沒有任何動作（rename、values、defaults、remove 至少寫一種）", d.ID)
		}
		for from, to := range d.Rename {
			if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" || from == to {
				return bad("資料轉換「%s」的 rename 要寫成 { 舊欄位 = \"新欄位\" }，兩邊不能一樣或空白", d.ID)
			}
		}
		for _, name := range d.fieldNames() {
			if coreFields[name] {
				return bad("資料轉換「%s」不能動「%s」：這是每一筆紀錄都有的基本欄位（編號、時間、誰寫的），由系統管", d.ID, name)
			}
		}
		for _, name := range d.Remove {
			if strings.TrimSpace(name) == "" {
				return bad("資料轉換「%s」的 remove 不能有空白的欄位名", d.ID)
			}
		}
	}
	return nil
}

// apply 把轉換套在一筆紀錄的欄位上（就地改），有改回 true；改名撞到「新舊欄位都有值而且不同」回錯誤。
func (d MigrationDecl) apply(f map[string]any) (bool, error) {
	changed := false
	for _, from := range sortedKeys(d.Rename) {
		to := d.Rename[from]
		old, has := f[from]
		if !has {
			continue
		}
		if cur, exists := f[to]; exists && !reflect.DeepEqual(cur, old) {
			return false, fmt.Errorf("欄位「%s」要改名成「%s」，但兩個欄位都有值而且不一樣", from, to)
		}
		f[to] = old
		delete(f, from)
		changed = true
	}
	for _, name := range sortedKeys(d.Values) {
		table := d.Values[name]
		switch v := f[name].(type) {
		case string:
			if nv, ok := table[v]; ok && nv != v {
				f[name] = nv
				changed = true
			}
		case []any:
			for i, item := range v {
				if s, ok := item.(string); ok {
					if nv, ok := table[s]; ok && nv != s {
						v[i] = nv
						changed = true
					}
				}
			}
		}
	}
	for _, name := range sortedKeys(d.Defaults) {
		if _, has := f[name]; !has {
			f[name] = d.Defaults[name]
			changed = true
		}
	}
	for _, name := range d.Remove {
		if _, has := f[name]; has {
			delete(f, name)
			changed = true
		}
	}
	return changed, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// DataMigration 是一個套件的一個資料轉換，接好了「對哪個家的哪個資料夾做」。
type DataMigration struct {
	ID          string // pkg.<套件>.<轉換 id>：記在 settings/migrations.toml 的名字
	Package     string
	Tier        sdk.Tier // 這個套件是哪一層的（官方的跟著核心升級跑；主人自己的、分享來的在套件載入時跑）
	Description string
	decl        MigrationDecl
	spec        vault.Spec
	hasSpec     bool
}

// Run 在家 home 上只做這一個轉換（同 RunDataMigrations 一批只有一個）。
func (d DataMigration) Run(home string) error { return RunDataMigrations(home, []DataMigration{d}) }

// Check 只預演不寫檔：回會改幾筆；有問題回錯誤（同 Run）。
func (d DataMigration) Check(home string) (int, error) {
	return runDataMigrations(home, []DataMigration{d}, false)
}

// RunDataMigrations 把一批還沒跑過的轉換一起做（照給的順序）。同一個套件同一種資料的轉換在記憶體裡串起來套完，
// 才拿套件現在的資料規格驗證一次（一個一個驗會卡住：前一個改名、後一個補必填欄位時，前一個單獨看一定不合格；
// 跳了好幾版才升級的家就是這種情況）。整批先預演，全部沒問題才寫檔；有一筆不合就一個檔都不寫，
// 錯誤裡列出是哪幾個檔、什麼問題（最多 5 個）。能重跑。
func RunDataMigrations(home string, ms []DataMigration) error {
	_, err := runDataMigrations(home, ms, true)
	return err
}

type pendingWrite struct {
	path string
	data []byte
	mode fs.FileMode
	bak  string // 原檔要留一份的位置（寫之前抄過去）
}

// migrateNow 給測試換掉日期。
var migrateNow = time.Now

// backupBeforeMigrate 把要改寫的檔原樣抄到旁邊的備份資料夾（`vault/pkg/<套件>/<種類>.bak-<日期>-migrate-<轉換 id>/`，
// 名字照備份的規矩，vault 與下一次轉換都不會把它當紀錄）。已經有同名的備份就不蓋（同一天重跑時，留最早那份才是原樣）。
// 有一個檔留不下來就回錯，呼叫的人一個檔都不寫。
func backupBeforeMigrate(writes []pendingWrite) error {
	for _, w := range writes {
		if w.bak == "" {
			continue
		}
		if _, err := os.Lstat(w.bak); err == nil {
			continue
		}
		raw, err := os.ReadFile(w.path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(w.bak), 0o755); err != nil {
			return err
		}
		if err := platform.AtomicWrite(w.bak, raw, w.mode); err != nil {
			return err
		}
	}
	return nil
}

func runDataMigrations(home string, ms []DataMigration, write bool) (int, error) {
	var order []string
	groups := map[string][]DataMigration{}
	for _, m := range ms {
		key := m.Package + "\x00" + m.decl.Kind
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], m)
	}
	var writes []pendingWrite
	for _, key := range order {
		w, err := planGroup(home, groups[key])
		if err != nil {
			return 0, err
		}
		writes = append(writes, w...)
	}
	if !write {
		return len(writes), nil
	}
	// 轉換會改寫（remove 還會拿掉欄位）主人的紀錄：動手前每個檔先留一份原樣在旁邊，轉錯了找得回來（憲法第 8 條）。
	if err := backupBeforeMigrate(writes); err != nil {
		return 0, platform.Unavailable("資料轉換前留不下原檔的備份，一個檔都沒動")
	}
	for _, w := range writes {
		if err := platform.AtomicWrite(w.path, w.data, w.mode); err != nil {
			return 0, platform.Unavailable(fmt.Sprintf("寫不進 %s", w.path))
		}
	}
	return len(writes), nil
}

// planGroup 預演同一個套件同一種資料的一串轉換：回要寫的檔；有紀錄轉不過去回 CONFLICT（什麼都還沒寫）。
func planGroup(home string, g []DataMigration) ([]pendingWrite, error) {
	first := g[0]
	root := filepath.Join(home, "vault", "pkg", first.Package, first.decl.Kind)
	bakRoot := root + ".bak-" + migrateNow().Format("20060102") + "-migrate-" + g[len(g)-1].decl.ID
	var writes []pendingWrite
	var problems []string
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == root {
				return filepath.SkipDir
			}
			return err
		}
		name := e.Name()
		if e.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-") || name == "_files") {
				return filepath.SkipDir // 備份、隱藏資料夾、附件：不是紀錄
			}
			return nil
		}
		if !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-") || e.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(home, path)
		rel = filepath.ToSlash(rel)
		r, order, err := vault.Decode(raw)
		if err != nil {
			problems = append(problems, rel+"：檔案讀不懂（格式壞了）")
			return nil
		}
		changed := false
		for _, m := range g {
			c, err := m.decl.apply(r.Fields)
			if err != nil {
				problems = append(problems, rel+"："+err.Error())
				return nil
			}
			changed = changed || c
		}
		if first.hasSpec {
			if err := first.spec.Validate(r); err != nil {
				problems = append(problems, rel+"：轉完還是不符合新的資料規格（"+plain(err)+"）")
				return nil
			}
		}
		if !changed {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		inKind, _ := filepath.Rel(root, path)
		writes = append(writes, pendingWrite{path: path, data: vault.Encode(r, order, vault.Spec{}), mode: info.Mode().Perm(), bak: filepath.Join(bakRoot, inKind)})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, platform.Unavailable(fmt.Sprintf("讀不了套件「%s」的資料（%s）", first.Package, first.decl.Kind))
	}
	if len(problems) > 0 {
		more := ""
		if len(problems) > 5 {
			more = fmt.Sprintf("……等 %d 個檔", len(problems))
			problems = problems[:5]
		}
		var what []string
		for _, m := range g {
			what = append(what, m.Description)
		}
		return nil, platform.Conflict(fmt.Sprintf("套件「%s」的資料轉換「%s」有 %s 筆轉不過去，一筆都沒動：%s%s",
			first.Package, strings.Join(what, "、"), countWord(len(problems), more), strings.Join(problems, "；"), more))
	}
	return writes, nil
}

func countWord(shown int, more string) string {
	if more != "" {
		return "好幾"
	}
	return fmt.Sprint(shown)
}

func plain(err error) string {
	if e, ok := err.(*platform.Err); ok && e.Message != "" {
		if e.Field != "" {
			return e.Field + "：" + e.Message
		}
		return e.Message
	}
	return err.Error()
}

// DataMigrations 列出家 home 裡每個套件宣告的資料轉換（不看套件有沒有啟用：停用的套件資料還在，格式一樣要跟上）。
// 同一個 id 出現在多層時用實際會載入的那一份（自己的 > 官方 > 分享的，同 scanRoot 的規則）；manifest 讀不起來的套件略過
// （它本來就不會載入，另外有健檢報）。順序：套件 id，再照宣告順序。
func DataMigrations(home string) []DataMigration {
	roots := []struct {
		tier sdk.Tier
		dir  string
	}{
		{sdk.TierMine, filepath.Join(home, "packages", string(sdk.TierMine))},
		{sdk.TierOfficial, OfficialDir(home)},
		{sdk.TierInstalled, platform.LayoutFor(home).InstalledPackages()},
	}
	chosen := map[string]*Manifest{}
	tiers := map[string]sdk.Tier{}
	for _, root := range roots {
		entries, err := os.ReadDir(root.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			dir := filepath.Join(root.dir, name)
			if !isDir(dir) || strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-") || notAPackage(dir, root.tier) {
				continue
			}
			man, err := ParseManifest(dir, root.tier)
			if err != nil || man == nil || man.ID != name {
				continue
			}
			if _, taken := chosen[man.ID]; !taken {
				chosen[man.ID], tiers[man.ID] = man, root.tier
			}
		}
	}
	var out []DataMigration
	for _, id := range sortedKeys(chosen) {
		out = append(out, manifestMigrations(chosen[id], tiers[id])...)
	}
	return out
}

// manifestMigrations 把一個套件宣告的轉換接上它現在的資料規格。
func manifestMigrations(man *Manifest, tier sdk.Tier) []DataMigration {
	var out []DataMigration
	for _, decl := range man.Migration {
		m := DataMigration{ID: "pkg." + man.ID + "." + decl.ID, Package: man.ID, Tier: tier, Description: decl.Description, decl: decl}
		for _, k := range man.dataKinds {
			if k.Kind == decl.Kind {
				m.spec, m.hasSpec = k.spec(man.ID), true
			}
		}
		out = append(out, m)
	}
	return out
}

// SchemaBreaks 比較同一個套件上一個正式版（old）與現在（cur）的資料規格，列出「舊紀錄會讀不懂、又沒有附轉換」的地方
// （發版前的檢查，DEV-154）。規則：加欄位（不必填）隨便加；下面這些一定要有對應的 [[migration]]——
//
//	欄位不見了或型別換了      → 同種類的轉換要處理到它（rename 的來源、values、或 remove）
//	列舉少了一個值            → 同種類的轉換要有這個欄位的 values 對照到那個值
//	欄位變成必填（或新增必填） → 同種類的轉換要補上它（defaults，或從別的欄位 rename 過來）
//	整個種類不見了            → 不行（使用者家裡的紀錄會沒人認）
//
// 每一項的開頭是 <套件>.<種類>[.<欄位>]，刻意這樣改的可以登記例外（工具 schemacheck 讀 schema.removed）。
func SchemaBreaks(old, cur *Manifest) []string {
	var out []string
	// 上一版附的轉換這一版要留著：跳版升級的家還沒跑過它。
	have := map[string]bool{}
	for _, m := range cur.Migration {
		have[m.ID] = true
	}
	for _, m := range old.Migration {
		if !have[m.ID] {
			out = append(out, fmt.Sprintf("%s.%s 上一版的資料轉換「%s」被拿掉了（還沒升到上一版的家會沒得轉）", old.ID, m.Kind, m.ID))
		}
	}
	curKinds := map[string]DataKind{}
	for _, k := range cur.dataKinds {
		curKinds[k.Kind] = k
	}
	for _, ok := range old.dataKinds {
		name := old.ID + "." + ok.Kind
		ck, exists := curKinds[ok.Kind]
		if !exists {
			out = append(out, name+" 整個資料種類不見了（使用者家裡的紀錄會沒人認）")
			continue
		}
		if ok.IDs != ck.IDs || ok.Prefix != ck.Prefix {
			out = append(out, fmt.Sprintf("%s 編號方式變了（%s %s → %s %s）：舊紀錄的編號會對不上，資料轉換改不了編號", name, ok.IDs, ok.Prefix, ck.IDs, ck.Prefix))
		}
		var migs []MigrationDecl
		for _, m := range cur.Migration {
			if m.Kind == ok.Kind {
				migs = append(migs, m)
			}
		}
		some := func(f func(MigrationDecl) bool) bool {
			for _, m := range migs {
				if f(m) {
					return true
				}
			}
			return false
		}
		curFields := map[string]dataFieldFile{}
		for _, f := range ck.Fields {
			curFields[f.Name] = f
		}
		oldFields := map[string]dataFieldFile{}
		for _, of := range ok.Fields {
			oldFields[of.Name] = of
			field := name + "." + of.Name
			cf, still := curFields[of.Name]
			touched := some(func(m MigrationDecl) bool { return m.touches(of.Name) })
			switch {
			case !still && !touched:
				out = append(out, field+" 欄位不見了，沒有附資料轉換（[[migration]] 的 rename 或 remove）")
			case still && cf.Type != of.Type && !some(func(m MigrationDecl) bool { return m.drops(of.Name) }):
				out = append(out, fmt.Sprintf("%s 型別從 %s 換成 %s：資料轉換不會換型別，要先把舊欄位拿掉或改名（[[migration]] 的 remove 或 rename），新欄位用新名字或補預設", field, of.Type, cf.Type))
			}
			if !still || len(cf.Enum) == 0 {
				continue
			}
			keep := map[string]bool{}
			for _, v := range cf.Enum {
				keep[v] = true
			}
			for _, v := range of.Enum {
				if keep[v] {
					continue
				}
				mapped := some(func(m MigrationDecl) bool { nv, ok := m.Values[of.Name][v]; return ok && keep[nv] })
				if !mapped {
					out = append(out, fmt.Sprintf("%s 的選項「%s」不見了，沒有附資料轉換（[[migration]] 的 values 把它對照到還在的選項）", field, v))
				}
			}
		}
		for _, cf := range ck.Fields {
			of, had := oldFields[cf.Name]
			if !cf.Required || (had && of.Required) {
				continue
			}
			if !some(func(m MigrationDecl) bool { return m.fills(cf.Name) }) {
				out = append(out, name+"."+cf.Name+" 變成必填，舊紀錄沒有這個欄位，沒有附資料轉換（[[migration]] 的 defaults 補上）")
			}
		}
	}
	sort.Strings(out)
	return out
}
