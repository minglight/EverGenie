package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

type entry struct {
	id       string
	tier     sdk.Tier
	dir      string
	man      *Manifest
	err      error // 非 nil＝rejected（manifest 不合格、相依缺少、同名衝突…）
	enabled  bool
	external bool
	source   *sdk.PackageSource // 從網址裝的：.source.toml（REQ-282）

	// 附程式套件的執行期狀態
	state    sdk.PackageState
	errText  string
	wantRun  bool // 還需要它在跑（乾淨結束且政策不重啟時變 false）
	rebuild  bool // 檔案有改動或 Reload，需要重編並重啟（開始編就清掉：編譯中又要求的，編完再編一次）
	building bool
	buildID  uint64 // 正在跑的那一次編譯（重掃換了一份 entry 時照抄，編完認得出結果算在哪一份上）
	gaveUp   bool   // 連續失敗太多次或編不過：等檔案改動或手動 Reload 才再試
	buildErr error  // 新版編不過、舊版還在跑時的那個編譯錯誤（舊版之後結束了，錯誤與通知改說這個；編好就清掉）
	proc     *process
	token    string
	hello    bool
	launched time.Time
	helloAt  time.Time
	restarts int
	next     time.Time
	remedy   sdk.PackageRemedy // 失敗時可以怎麼補救（例：沒有 Go）
}

// settings 是 settings/packages.toml：哪些套件被停用、使用者設的擴充點值。
type settings struct {
	Schema     int               `toml:"schema"`
	Enabled    map[string]bool   `toml:"enabled"`
	Extensions map[string]string `toml:"extensions"`
	// Claims：認領職位的通知走到哪裡（SEC-302 S9），key 是職位 id；同一個職位只問一次。
	Claims map[string]claimRecord `toml:"claims,omitempty"`
}

func (m *Module) settingsPath() string { return filepath.Join(m.home, "settings", "packages.toml") }

// loadSettings 讀不到檔案是空設定；格式壞掉回錯誤（讀取路徑用空設定繼續，寫入路徑則拒絕，避免蓋掉使用者的檔案）。
func (m *Module) loadSettings() (settings, error) {
	s := settings{Schema: 1, Enabled: map[string]bool{}, Extensions: map[string]string{}}
	data, err := os.ReadFile(m.settingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, platform.Unavailable("讀不了 settings/packages.toml")
	}
	if _, err := toml.Decode(string(data), &s); err != nil {
		return settings{Schema: 1, Enabled: map[string]bool{}, Extensions: map[string]string{}}, platform.Invalid("settings/packages.toml", "settings/packages.toml 的格式不對，請先修正或刪掉它")
	}
	if s.Enabled == nil {
		s.Enabled = map[string]bool{}
	}
	if s.Extensions == nil {
		s.Extensions = map[string]string{}
	}
	return s, nil
}

func (m *Module) saveSettings(s settings) error {
	var b strings.Builder
	s.Schema = 1
	if err := toml.NewEncoder(&b).Encode(s); err != nil {
		return platform.Internal("無法寫入套件設定")
	}
	if err := os.MkdirAll(filepath.Dir(m.settingsPath()), 0o755); err != nil {
		return platform.Unavailable("無法寫入套件設定")
	}
	if err := platform.AtomicWrite(m.settingsPath(), []byte(b.String()), 0o644); err != nil {
		return platform.Unavailable("無法寫入套件設定")
	}
	return nil
}

// rescanLocked 重新掃描三層、合成有效清單、做相依檢查，並盡量保留既有的行程狀態。
func (m *Module) rescanLocked(now time.Time) {
	var all []*loaded
	for _, tier := range []sdk.Tier{sdk.TierOfficial, sdk.TierInstalled, sdk.TierMine} {
		all = append(all, scanRoot(m.tierDir(tier), tier)...)
	}
	active, shadowed, conflicts := resolveTiers(all)
	st, err := m.loadSettings()
	if err != nil && m.log != nil {
		m.log.Warn("套件設定檔讀不了，先當作沒有設定")
	}
	old := m.entries
	m.entries, m.shadowed, m.rejected = map[string]*entry{}, shadowed, nil
	for id, l := range active {
		e := &entry{id: id, tier: l.tier, dir: l.dir, man: l.manifest, err: l.err, wantRun: true}
		if l.tier != sdk.TierOfficial {
			e.source = readSource(l.dir)
		}
		e.enabled = l.manifest == nil || !l.manifest.Optional // 選用套件沒有記錄＝預設不啟用（REQ-176）
		if v, ok := st.Enabled[id]; ok {
			e.enabled = v
		}
		if o := old[id]; o != nil && o.dir == e.dir && o.tier == e.tier {
			// 保留執行期狀態；manifest 以新讀到的為準。
			e.state, e.errText, e.wantRun, e.rebuild, e.building, e.buildID, e.gaveUp = o.state, o.errText, o.wantRun, o.rebuild, o.building, o.buildID, o.gaveUp
			e.buildErr = o.buildErr
			e.proc, e.token, e.hello, e.launched, e.helloAt, e.restarts, e.next, e.remedy = o.proc, o.token, o.hello, o.launched, o.helloAt, o.restarts, o.next, o.remedy
		} else if o != nil && o.proc != nil {
			o.proc.kill() // 換了位置或層級（例如 fork 之後換成 mine）：舊的行程收掉
		}
		if e.man != nil && e.man.Kind == string(sdk.PackageKindProgram) && m.external != nil {
			if _, _, ok := m.external.Claim(e.man.Binary()); ok {
				e.external = true
			}
		}
		m.entries[id] = e
	}
	for _, l := range conflicts {
		m.rejected = append(m.rejected, &entry{id: l.id, tier: l.tier, dir: l.dir, man: l.manifest, err: l.err, enabled: true})
	}
	for id, o := range old {
		if _, ok := m.entries[id]; !ok && o.proc != nil {
			o.proc.kill() // 套件被移除
		}
	}
	m.migrateOwnerPackagesLocked()
	m.resolveDepsLocked()
	m.refreshWarningsLocked(st)
	m.syncDataKindsLocked()
}

// migrateOwnerPackagesLocked：主人自己的、別人分享的套件（啟用中、manifest 合格）有還沒跑過的資料轉換就在這裡做
// （DEV-154）。官方套件的轉換跟著核心升級跑、失敗會擋升級；這兩層不一樣：轉不過去只把這個套件標成被拒
// （資料種類不註冊，它的紀錄留在原地、不算壞檔），原因寫白話，其他套件與整個家照常；下次重新掃描會再試
// （主人或秘書把紀錄修好就會過）。成功才記成跑過。法律第 3 條：壞了只壞它自己。
func (m *Module) migrateOwnerPackagesLocked() {
	if m.migrations == nil || m.home == "" {
		return
	}
	var applied map[string]bool
	for _, id := range sortedKeys(m.entries) {
		e := m.entries[id]
		if e.tier == sdk.TierOfficial || e.err != nil || !e.enabled || e.man == nil || len(e.man.Migration) == 0 {
			continue
		}
		if applied == nil {
			list, err := m.migrations.Applied(m.home)
			if err != nil {
				if m.log != nil {
					m.log.Warn("讀不了資料轉換的紀錄，這次先不做套件的資料轉換")
				}
				return
			}
			applied = map[string]bool{}
			for _, a := range list {
				applied[a] = true
			}
		}
		var pending []DataMigration
		var ids []string
		for _, dm := range manifestMigrations(e.man, e.tier) {
			if !applied[dm.ID] {
				pending, ids = append(pending, dm), append(ids, dm.ID)
			}
		}
		if len(pending) == 0 {
			continue
		}
		if err := RunDataMigrations(m.home, pending); err != nil {
			e.err = platform.Conflict("這個套件的資料格式轉換沒過，先不載入（紀錄都沒動）：" + plain(err))
			continue
		}
		if err := m.migrations.Mark(m.home, ids); err != nil {
			e.err = platform.Unavailable("這個套件的資料轉好了，但記不下「轉過了」，先不載入：" + plain(err))
		}
	}
}

// syncDataKindsLocked 把「看板看得到的套件」（沒被拒、沒停用；純資料或附程式都算）宣告的資料種類註冊進 vault，
// 其他套件（停用、解除安裝、被拒）整批拿掉：資料留在原地、暫時看不到，重新啟用就回來（D-28、REQ-173）。內容沒變的套件不重建。
func (m *Module) syncDataKindsLocked() {
	if m.d.Vault == nil {
		return
	}
	if m.dataSigs == nil {
		m.dataSigs = map[string]string{}
	}
	want := map[string][]vault.Spec{}
	sigs := map[string]string{}
	for id, e := range m.entries {
		if e.err != nil || !e.enabled || e.man == nil || len(e.man.dataKinds) == 0 {
			continue
		}
		var specs []vault.Spec
		var sig strings.Builder
		for _, k := range e.man.dataKinds {
			specs = append(specs, k.spec(id))
			fmt.Fprintf(&sig, "%+v\n", k)
		}
		want[id], sigs[id] = specs, sig.String()
	}
	for id := range m.dataSigs {
		if _, ok := want[id]; !ok {
			m.d.Vault.SetPackageSchemas(id, nil)
			delete(m.dataSigs, id)
		}
	}
	for id, specs := range want {
		if m.dataSigs[id] != sigs[id] {
			m.d.Vault.SetPackageSchemas(id, specs)
			m.dataSigs[id] = sigs[id]
		}
	}
}

// resolveDepsLocked 做相依檢查（D-19 只是邊界檢查）：缺少、版本不符、被停用、成環的套件 rejected，其餘不受影響。
func (m *Module) resolveDepsLocked() {
	// 核心版本限制（REQ-282）：不符的拒載；核心是開發版、認不出版本號就不擋（refreshWarningsLocked 會警告）。
	for _, id := range m.sortedIDsLocked() {
		if e := m.entries[id]; e.err == nil && e.man != nil && e.man.Core != "" {
			if ok, known := satisfiesCore(e.man.Core, m.coreVer()); known && !ok {
				e.err = fmt.Errorf("%s", coreMismatch(e.man.Core, m.coreVer()))
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, id := range m.sortedIDsLocked() {
			e := m.entries[id]
			if e.err != nil || e.man == nil {
				continue
			}
			for _, r := range e.man.Requires {
				dep := m.entries[r.ID]
				var why string
				switch {
				case dep == nil || dep.err != nil:
					why = fmt.Sprintf("需要套件「%s」，但它沒有安裝或沒有載入", r.ID)
				case !dep.enabled:
					why = fmt.Sprintf("需要套件「%s」，但它已停用", r.ID)
				default:
					c, _ := parseConstraint(r.Version)
					if !c.allows(dep.man.Version) {
						why = fmt.Sprintf("需要套件「%s」%s，目前裝的是 %s", r.ID, r.Version, dep.man.Version)
					}
				}
				if why != "" {
					e.err, changed = fmt.Errorf("%s", why), true
					break
				}
			}
		}
	}
	// 成環：從每個還有效的套件沿著 requires 走，走回自己就是環。
	for _, id := range m.sortedIDsLocked() {
		if e := m.entries[id]; e.err == nil && m.inCycleLocked(id) {
			e.err = fmt.Errorf("相依成環（%s 直接或間接依賴自己）", id)
		}
	}
	m.order = m.order[:0]
	seen := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		if e := m.entries[id]; e != nil && e.man != nil {
			for _, r := range e.man.Requires {
				visit(r.ID)
			}
		}
		if m.entries[id] != nil {
			m.order = append(m.order, id)
		}
	}
	for _, id := range m.sortedIDsLocked() {
		visit(id)
	}
}

func (m *Module) inCycleLocked(start string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(id string) bool {
		e := m.entries[id]
		if e == nil || e.man == nil {
			return false
		}
		for _, r := range e.man.Requires {
			if r.ID == start {
				return true
			}
			if !seen[r.ID] {
				seen[r.ID] = true
				if walk(r.ID) {
					return true
				}
			}
		}
		return false
	}
	return walk(start)
}

func (m *Module) sortedIDsLocked() []string {
	ids := make([]string, 0, len(m.entries))
	for id := range m.entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// refreshWarningsLocked：設定裡引用了不存在的擴充點（被移除或套件不在了）→ 警告，不壞（REQ-94）。
func (m *Module) refreshWarningsLocked(st settings) {
	var w []string
	keys := make([]string, 0, len(st.Extensions))
	for k := range st.Extensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pkgID, extID, _ := strings.Cut(k, ".")
		e := m.entries[pkgID]
		found := false
		if e != nil && e.man != nil {
			for _, x := range e.man.Extension {
				found = found || x.ID == extID
			}
		}
		if !found {
			w = append(w, fmt.Sprintf("設定裡的擴充點「%s」已經不存在（套件拿掉了它），這個設定先保留、沒有作用", k))
		}
	}
	if _, ok := coreSemver(m.coreVer()); !ok {
		for _, id := range m.sortedIDsLocked() {
			if e := m.entries[id]; e.man != nil && e.man.Core != "" {
				w = append(w, fmt.Sprintf("套件「%s」要 EverGenie %s，但這份 EverGenie 是開發版，認不出版本號，先不擋", e.man.Name, e.man.Core))
			}
		}
	}
	// fork 的官方出了新版：提醒一句（不自動合併，PKG-5）。
	for _, id := range m.sortedIDsLocked() {
		e := m.entries[id]
		if e.tier != sdk.TierMine || e.man == nil || e.man.Fork == nil {
			continue
		}
		off, err := ParseManifest(filepath.Join(m.officialDir(), e.man.Fork.Of), sdk.TierOfficial)
		if err != nil {
			continue
		}
		nv, ok1 := parseSemver(off.Version)
		bv, ok2 := parseSemver(e.man.Fork.BaseVersion)
		if ok1 && ok2 && nv.compare(bv) > 0 {
			w = append(w, fmt.Sprintf("你 fork 的套件「%s」官方出了新版 %s → %s；可以請秘書看差異清單（不會自動合併）", e.man.Name, e.man.Fork.BaseVersion, off.Version))
		}
	}
	if len(w) > len(m.warnings) && m.log != nil {
		m.log.Warn("套件設定引用了不存在的擴充點", "count", len(w))
	}
	m.warnings = w
}

// view 把 entry 變成 API 上的 Package。
func (m *Module) viewLocked(e *entry) sdk.Package {
	var p sdk.Package
	if e.man != nil {
		p = e.man.Public(e.tier)
	} else {
		p = sdk.Package{ID: e.id, Name: e.id, Tier: e.tier}
	}
	p.Enabled = e.enabled
	p.Source = e.source
	switch {
	case e.err != nil:
		p.State, p.Error = sdk.PackageStateRejected, e.err.Error()
	case !e.enabled:
		p.State = sdk.PackageStateStopped

	case e.man.Kind == string(sdk.PackageKindData):
		p.State = sdk.PackageStateRunning // 純資料：載入了就是在作用中
	case e.external && m.external != nil:
		state, detail, _ := m.external.Claim(e.man.Binary())
		p.State = state
		if state == sdk.PackageStateFailed {
			p.Error = detail
		}
	default:
		p.State, p.Error = e.state, e.errText
		if p.State == sdk.PackageStateFailed {
			p.Remedy = e.remedy
		}
		if p.State == "" {
			p.State = sdk.PackageStateStopped
		}
	}
	return p
}
