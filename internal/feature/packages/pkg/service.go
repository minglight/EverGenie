package pkg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

var _ sdk.PackageAPI = (*Module)(nil)

// Ready：套件服務啟動了沒（沒有＝UNAVAILABLE）。Packages() 在還沒啟動時回空清單，要分辨「沒有套件」與「還沒準備好」的用這個。
func (m *Module) Ready() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readyLocked()
}

func (m *Module) readyLocked() error {
	if m.run == nil || m.home == "" {
		return platform.Unavailable("套件服務還沒啟動")
	}
	return nil
}

// Hello：只接受「核心剛啟動、還沒報到」的那個附程式套件（REQ-91、REQ-96）。資料路徑一律是 vault/pkg/<id>（REQ-172）。
func (m *Module) Hello(ctx context.Context, req *sdk.PackageHelloRequest) (*sdk.PackageHelloResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return nil, err
	}
	e := m.entries[req.ID]
	if e == nil || e.err != nil || e.man == nil || e.man.Kind != string(sdk.PackageKindProgram) || e.external || e.proc == nil || e.hello {
		return nil, platform.Conflict(fmt.Sprintf("「%s」不是核心剛啟動、等著報到的套件", req.ID))
	}
	if req.API != e.man.API {
		return nil, platform.Invalid("api", fmt.Sprintf("報到的協定版本「%s」跟 package.toml 寫的「%s」不一樣", req.API, e.man.API))
	}
	if req.Version != "" && req.Version != e.man.Version {
		return nil, platform.Invalid("version", fmt.Sprintf("報到的版本 %s 跟 package.toml 寫的 %s 不一樣", req.Version, e.man.Version))
	}
	now := m.d.Clock.Now()
	e.hello, e.helloAt = true, now
	e.state, e.errText, e.remedy = sdk.PackageStateRunning, "", ""
	var granted []string
	for _, c := range e.man.Capabilities.Read {
		granted = append(granted, "read:"+c)
	}
	for _, c := range e.man.Capabilities.Write {
		granted = append(granted, "write:"+c)
	}
	for _, c := range e.man.Capabilities.Events {
		granted = append(granted, "event:"+c)
	}
	return &sdk.PackageHelloResponse{Token: e.token, DataDir: packageDataDir(e.id), Granted: granted}, nil
}

// packageDataDir 是套件資料的位置（相對於家，PKC-7、D-27）：vault/pkg/<id>。
func packageDataDir(id string) string { return "vault/pkg/" + id }

func (m *Module) List(ctx context.Context, req *sdk.PackageListRequest) (*sdk.PackageListResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return nil, err
	}
	out := &sdk.PackageListResponse{Packages: m.listLocked(req.Tier), Warnings: append([]string(nil), m.warnings...)}
	return out, nil
}

func (m *Module) listLocked(tier sdk.Tier) []sdk.Package {
	var out []sdk.Package
	for _, id := range m.sortedIDsLocked() {
		if e := m.entries[id]; tier == "" || e.tier == tier {
			out = append(out, m.viewLocked(e))
		}
	}
	for _, e := range m.rejected {
		if tier == "" || e.tier == tier {
			out = append(out, m.viewLocked(e))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return tierOrder[out[i].Tier] < tierOrder[out[j].Tier]
	})
	return out
}

func (m *Module) Get(ctx context.Context, req *sdk.PackageGetRequest) (*sdk.PackageGetResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return nil, err
	}
	if e := m.entries[req.ID]; e != nil {
		p := m.viewLocked(e)
		return &sdk.PackageGetResponse{Package: &p}, nil
	}
	for _, e := range m.rejected {
		if e.id == req.ID {
			p := m.viewLocked(e)
			return &sdk.PackageGetResponse{Package: &p}, nil
		}
	}
	return nil, platform.NotFound(fmt.Sprintf("沒有套件「%s」", req.ID))
}

// Install 安裝一個套件：本機資料夾（完整路徑）或 git 網址（REQ-282；一定要 confirmed）。先驗 manifest，
// 抓到（或複製到）暫存資料夾驗過再 rename，失敗不留暫存；同層同 id 或跟官方同 id 回 CONFLICT。
func (m *Module) Install(ctx context.Context, req *sdk.PackageInstallRequest) (*sdk.PackageInstallResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tier := req.Tier
	switch tier {
	case "":
		tier = sdk.TierInstalled
	case sdk.TierInstalled, sdk.TierMine:
	case sdk.TierOfficial:
		return nil, platform.Forbidden("官方套件跟著發版，不能自己安裝到官方層；要調整官方套件請用它的擴充點，或自己做新套件")
	default:
		return nil, platform.Invalid("tier", "tier 要是 installed 或 mine")
	}
	src := strings.TrimSpace(req.Source)
	fromURL := isGitURL(src)
	if fromURL && !req.Confirmed {
		return nil, platform.Invalid("confirmed", "從網址安裝要先看過預覽再確認（confirmed 要是 true）")
	}
	m.mu.Lock()
	err := m.readyLocked()
	root := m.tierDir(tier)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	f, err := m.stageSource(ctx, src, strings.TrimSpace(req.Ref), tier, root, ".install-")
	if err != nil {
		return nil, err
	}
	defer f.cleanup()
	if f.manErr != nil {
		return nil, f.manErr
	}
	head := f.man
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return nil, err
	}
	dest := filepath.Join(root, head.ID)
	if exists(dest) {
		return nil, platform.Conflict(fmt.Sprintf("packages/%s/%s 已經存在", tier, head.ID))
	}
	if isDir(filepath.Join(m.officialDir(), head.ID)) {
		return nil, platform.Conflict(fmt.Sprintf("官方已經有套件「%s」；要調整官方套件請用它的擴充點，或換個名字做自己的套件", head.ID))
	}
	problems, _, _ := m.problemsForLocked(head, false)
	if fromURL && len(problems) > 0 {
		return nil, platform.Invalid("source", strings.Join(problems, "；"))
	}
	if head.Core != "" {
		if ok, known := satisfiesCore(head.Core, m.coreVer()); known && !ok {
			return nil, platform.Invalid("core", coreMismatch(head.Core, m.coreVer()))
		}
	}
	if err := os.Rename(f.stage, dest); err != nil {
		return nil, platform.Unavailable("無法放進 packages/" + string(tier))
	}
	now := m.d.Clock.Now()
	m.rescanLocked(now)
	m.markChangedLocked(dest)
	if m.log != nil {
		m.log.Info("安裝套件", "id", head.ID, "tier", string(tier), "by", actor(req.By), "from_url", fromURL)
	}
	p, err := m.kickLocked(head.ID)
	if err != nil {
		return nil, err
	}
	return &sdk.PackageInstallResponse{Package: p}, nil
}

// stageSource 把來源（git 網址或本機資料夾）準備成「驗過、可以直接 rename 進去」的資料夾 f.stage（<暫存>/<id>）：
// 網址先 clone 並選版本；兩種都整包複製（不帶 .git）、網址的寫上 .source.toml、最後用 ParseManifest 完整驗一次。
// manifest 不合格不算 error，放在 f.manErr（預覽把它當 Problems，安裝直接回）。呼叫者要 f.cleanup()。不持有 m.mu。
func (m *Module) stageSource(ctx context.Context, src, ref string, tier sdk.Tier, parent, prefix string) (*fetched, error) {
	var f *fetched
	switch {
	case isGitURL(src):
		var err error
		if f, err = m.fetchGit(ctx, parent, src, ref, prefix); err != nil {
			return nil, err
		}
	case src != "" && filepath.IsAbs(src) && isDir(src):
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return nil, platform.Unavailable("無法建立暫存資料夾")
		}
		f = &fetched{root: filepath.Join(parent, prefix+shortRand()), src: src}
		if err := os.MkdirAll(f.root, 0o755); err != nil {
			return nil, platform.Unavailable("無法建立暫存資料夾")
		}
	default:
		return nil, platform.Invalid("source", "只能從 git 網址（https://、ssh://、git@、file://）或這台電腦上的資料夾（請給完整路徑）安裝")
	}
	data, err := os.ReadFile(filepath.Join(f.src, "package.toml"))
	if err != nil {
		f.manErr = platform.Invalid("source", "這個來源的最上層沒有 package.toml")
		return f, nil
	}
	head, err := parseManifestBytes(data, "", tier)
	if err != nil {
		f.manErr = err
		return f, nil
	}
	f.stage = filepath.Join(f.root, head.ID)
	if err := copyTree(f.src, f.stage); err != nil {
		f.cleanup()
		return nil, platform.Unavailable("無法複製套件資料夾")
	}
	_ = os.Remove(filepath.Join(f.stage, sourceFile)) // 來源只認這次抓的，不認來源資料夾自己帶的
	if f.source != nil {
		if err := writeSource(f.stage, f.source); err != nil {
			f.cleanup()
			return nil, err
		}
	}
	man, err := ParseManifest(f.stage, tier)
	if err != nil {
		f.manErr = err
		return f, nil
	}
	f.man = man
	return f, nil
}

// kickLocked 在改完套件之後馬上跑一拍（編譯、啟動不用等下一拍），再回這個套件現在的樣子。進出都持有鎖。
func (m *Module) kickLocked(id string) (*sdk.Package, error) {
	run := m.run
	m.mu.Unlock()
	m.Tick(run, m.d.Clock.Now())
	m.mu.Lock()
	e := m.entries[id]
	if e == nil {
		return nil, platform.Internal("找不到剛處理的套件「" + id + "」")
	}
	p := m.viewLocked(e)
	return &p, nil
}

// Uninstall 只移除分享的套件（installed）：資料夾刪掉，vault/pkg/<id>/ 的資料保留（PKC-7）。
func (m *Module) Uninstall(ctx context.Context, req *sdk.PackageUninstallRequest) (*sdk.PackageUninstallResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return nil, err
	}
	if req.ID == "" || strings.ContainsAny(req.ID, `/\`) || strings.HasPrefix(req.ID, ".") {
		return nil, platform.Invalid("id", "要給套件 id")
	}
	dir := filepath.Join(m.tierDir(sdk.TierInstalled), req.ID)
	if !isDir(dir) {
		if e := m.entries[req.ID]; e != nil {
			return nil, platform.Forbidden(fmt.Sprintf("「%s」是%s套件，不能從這裡解除安裝", req.ID, tierName(e.tier)))
		}
		return nil, platform.NotFound(fmt.Sprintf("沒有分享的套件「%s」", req.ID))
	}
	if e := m.entries[req.ID]; e != nil && e.dir == dir {
		m.stopProcLocked(e)
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, platform.Unavailable("無法移除套件資料夾")
	}
	delete(m.seen, dir)
	delete(m.pend, dir)
	m.rescanLocked(m.d.Clock.Now())
	if m.log != nil {
		m.log.Info("解除安裝套件", "id", req.ID, "by", actor(req.By))
	}
	return &sdk.PackageUninstallResponse{}, nil
}

func tierName(t sdk.Tier) string {
	switch t {
	case sdk.TierOfficial:
		return "官方"
	case sdk.TierMine:
		return "你自己的"
	}
	return "分享的"
}

// Reload 重掃三層；指定的（空＝全部）附程式套件清掉停手狀態並重編。
func (m *Module) Reload(ctx context.Context, req *sdk.PackageReloadRequest) (*sdk.PackageReloadResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if err := m.readyLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	m.rescanLocked(m.d.Clock.Now())
	if req.ID != "" && m.entries[req.ID] == nil {
		m.mu.Unlock()
		return nil, platform.NotFound(fmt.Sprintf("沒有套件「%s」", req.ID))
	}
	for id, e := range m.entries {
		if (req.ID == "" || id == req.ID) && e.man != nil && e.man.Kind == string(sdk.PackageKindProgram) && !e.external {
			e.rebuild, e.gaveUp, e.wantRun, e.restarts = true, false, true, 0
		}
	}
	run := m.run
	m.mu.Unlock()
	m.Tick(run, m.d.Clock.Now())
	m.mu.Lock()
	defer m.mu.Unlock()
	return &sdk.PackageReloadResponse{Packages: m.listLocked("")}, nil
}

// SetEnabled 寫 settings/packages.toml 的 enabled；停用的行程收掉，依賴它的套件被拒。設定檔壞了就拒絕寫（不蓋掉使用者的檔）。
func (m *Module) SetEnabled(ctx context.Context, req *sdk.PackageSetEnabledRequest) (*sdk.PackageSetEnabledResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if err := m.readyLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if m.entries[req.ID] == nil {
		m.mu.Unlock()
		return nil, platform.NotFound(fmt.Sprintf("沒有套件「%s」", req.ID))
	}
	st, err := m.loadSettings()
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if cur := m.entries[req.ID]; cur != nil && cur.man != nil && cur.man.Optional {
		st.Enabled[req.ID] = req.Enabled // 選用套件一律寫明確值：true 也寫，不能靠「刪掉＝預設啟用」（REQ-176）
	} else if req.Enabled {
		delete(st.Enabled, req.ID) // 預設就是啟用
	} else {
		st.Enabled[req.ID] = false
	}
	if err := m.saveSettings(st); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	m.rescanLocked(m.d.Clock.Now())
	if e := m.entries[req.ID]; e != nil && req.Enabled {
		e.gaveUp, e.wantRun, e.restarts = false, true, 0
	}
	run := m.run
	m.mu.Unlock()
	m.Tick(run, m.d.Clock.Now())
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[req.ID]
	if e == nil {
		return nil, platform.NotFound(fmt.Sprintf("沒有套件「%s」", req.ID))
	}
	p := m.viewLocked(e)
	return &sdk.PackageSetEnabledResponse{Package: &p}, nil
}

// ListExtensions 回每個套件的擴充點、使用者設的值，以及「設定引用的擴充點不見了」的警告（REQ-94）。
func (m *Module) ListExtensions(ctx context.Context, req *sdk.PackageListExtensionsRequest) (*sdk.PackageListExtensionsResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return nil, err
	}
	if req.Package != "" && m.entries[req.Package] == nil {
		return nil, platform.NotFound(fmt.Sprintf("沒有套件「%s」", req.Package))
	}
	st, _ := m.loadSettings()
	out := &sdk.PackageListExtensionsResponse{Values: map[string]string{}}
	for _, id := range m.sortedIDsLocked() {
		e := m.entries[id]
		if (req.Package != "" && id != req.Package) || e.man == nil {
			continue
		}
		for _, x := range e.man.Extension {
			out.Extensions = append(out.Extensions, sdk.ExtensionPoint{ID: x.ID, Kind: sdk.ExtensionKind(x.Kind), Description: x.Description, DefaultValue: x.Default, OwnerOnly: x.OwnerOnly, Agent: x.Agent, Package: id})
		}
	}
	for k, v := range st.Extensions {
		if req.Package == "" || strings.HasPrefix(k, req.Package+".") {
			out.Values[k] = v
		}
	}
	for _, w := range m.warnings {
		if strings.Contains(w, "擴充點") {
			out.Warnings = append(out.Warnings, w)
		}
	}
	return out, nil
}

// SetExtension 寫一個擴充點的值：value["value"] 是字串；空的 map 表示拿掉設定。
func (m *Module) SetExtension(ctx context.Context, req *sdk.PackageSetExtensionRequest) (*sdk.PackageSetExtensionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var value string
	remove := len(req.Value) == 0
	if !remove {
		v, ok := req.Value["value"].(string)
		if !ok || len(req.Value) != 1 {
			return nil, platform.Invalid("value", `value 要是 {"value": "<字串>"}；空的表示拿掉設定`)
		}
		value = v
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return nil, err
	}
	e := m.entries[req.Package]
	if e == nil || e.man == nil {
		return nil, platform.NotFound(fmt.Sprintf("沒有套件「%s」", req.Package))
	}
	var decl *ExtensionDecl
	for i := range e.man.Extension {
		if e.man.Extension[i].ID == req.Extension {
			decl = &e.man.Extension[i]
		}
	}
	if decl == nil {
		return nil, platform.NotFound(fmt.Sprintf("套件「%s」沒有擴充點「%s」", req.Package, req.Extension))
	}
	if decl.Kind == string(sdk.ExtensionKindPageSlot) && !remove {
		return nil, platform.Invalid("extension", fmt.Sprintf("「%s」是頁面上的位置，沒有值可以設；要在那裡放東西，做一個自己的套件、頁面寫 slot = \"%s.%s\"", req.Extension, req.Package, req.Extension))
	}
	if decl.Kind == string(sdk.ExtensionKindPromptAppend) && len(value) > promptAppendMax {
		return nil, platform.Invalid("value", fmt.Sprintf("這一段太長了（%d 個位元組，上限 %d）；它每次開對話都會帶進去，請寫短一點", len(value), promptAppendMax))
	}
	if decl.Kind == string(sdk.ExtensionKindPromptAppend) && e.tier == sdk.TierInstalled && !remove && !platform.IsOwner(actor(req.By)) {
		// 這段字每次開對話都會進秘書的指令；別人分享的套件要用它，得主人本人點頭（秘書不能替主人設，免得被套件的說明帶著走）。
		return nil, platform.Forbidden(fmt.Sprintf("「%s」是別人分享的套件，要讓它的這一段進秘書的指令，只有主人本人能設（在看板「設定 → 套件」）", e.man.Name))
	}
	if decl.OwnerOnly && !platform.IsOwner(actor(req.By)) {
		return nil, platform.Forbidden(fmt.Sprintf("「%s」的這個設定只有主人本人能改（在看板「設定 → 套件」）", e.man.Name))
	}
	st, err := m.loadSettings()
	if err != nil {
		return nil, err
	}
	key := req.Package + "." + req.Extension
	if remove {
		delete(st.Extensions, key)
	} else {
		st.Extensions[key] = value
	}
	if err := m.saveSettings(st); err != nil {
		return nil, err
	}
	m.refreshWarningsLocked(st)
	if m.log != nil {
		m.log.Info("設定擴充點", "key", key, "by", actor(req.By))
	}
	return &sdk.PackageSetExtensionResponse{}, nil
}

// InstallGo：主人同意之後才裝（confirmed 必須是 true）。已經有能用的 Go 就不下載；沒有就把寫死的那一版下載到
// 程式自己的工具資料夾（toolchain.go，PKG-65）。有能用的 Go 之後，因為沒有 Go 而停下來的套件全部排進重編。
func (m *Module) InstallGo(ctx context.Context, req *sdk.PackageInstallGoRequest) (*sdk.PackageInstallGoResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !req.Confirmed {
		return nil, platform.Invalid("confirmed", "裝 Go 要先問過主人、他同意了才裝（confirmed 要是 true）")
	}
	m.mu.Lock()
	if err := m.readyLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	tools, look := m.tools, m.lookGo
	if m.installing {
		m.mu.Unlock()
		return nil, platform.Conflict("正在下載 Go，請等它跑完")
	}
	m.installing = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.installing = false; m.mu.Unlock() }()

	var command string
	if _, err := look(); err != nil {
		// 下載不跟著這次請求取消：看板關掉、秘書的指令逾時，下載照樣跑完（同時再按會回「正在下載」）。
		version, err := tools.install(context.WithoutCancel(ctx))
		if err != nil {
			var fail *goFail
			if errors.As(err, &fail) {
				if m.log != nil && fail.detail != nil {
					m.log.Warn("下載 Go 沒成功", "by", actor(req.By), "detail", fail.detail.Error())
				}
				return nil, fail.apiError()
			}
			return nil, err
		}
		command = fmt.Sprintf("下載 Go %s 到程式自己的資料夾", version)
		if _, err := look(); err != nil {
			if m.log != nil {
				m.log.Warn("下載 Go 之後還是找不到能用的 go", "detail", err.Error())
			}
			return nil, platform.Unavailable(goMsgStill + "，跟任何一位秘書說，請她幫忙看看")
		}
	}
	version := ""
	if f := tools.find(); f.problem == "" {
		version = f.version
	}
	m.mu.Lock()
	var waiting []string
	for id, e := range m.entries {
		if e.remedy == sdk.PackageRemedyInstallGo {
			e.remedy, e.rebuild, e.gaveUp, e.wantRun, e.restarts = "", true, false, true, 0
			waiting = append(waiting, id)
		}
	}
	sort.Strings(waiting)
	run := m.run
	if m.log != nil {
		m.log.Info("安裝 Go", "command", command, "version", version, "by", actor(req.By), "packages", len(waiting))
	}
	m.mu.Unlock()
	m.Tick(run, m.d.Clock.Now())
	m.mu.Lock()
	defer m.mu.Unlock()
	out := &sdk.PackageInstallGoResponse{Command: command, Version: version}
	for _, id := range waiting {
		if e := m.entries[id]; e != nil {
			out.Packages = append(out.Packages, m.viewLocked(e))
		}
	}
	return out, nil
}

// GoStatus：編譯套件要用的 Go 能不能用（唯讀；不下載，PKG-65）。
func (m *Module) GoStatus(ctx context.Context, _ *sdk.PackageGoStatusRequest) (*sdk.PackageGoStatusResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	tools := m.tools
	m.mu.Unlock()
	return tools.status(), nil
}

func actor(a *sdk.Actor) string {
	if a == nil {
		return ""
	}
	return a.Name
}

// Packages 回全部有效的套件（含 rejected），內容同 PackageAPI.List；web 的 PackageCatalog 用（P6-2a）。
func (m *Module) Packages() []sdk.Package {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readyLocked() != nil {
		return nil
	}
	return m.listLocked("")
}

// TemplateDuty 是啟用中套件帶來的職位樣板的一句話職責（agents 建秘書沒給職責時用，DEV-91）；沒有這個職位回空字串。
func (m *Module) TemplateDuty(id string) string {
	for _, p := range m.Packages() {
		if !p.Enabled || p.State == sdk.PackageStateRejected {
			continue
		}
		for _, a := range p.Agents {
			if a.ID == id && a.Duty != "" {
				return a.Duty
			}
		}
	}
	return ""
}

// PackageDir 回有效（沒被拒）套件的資料夾絕對路徑；web 的 PackageCatalog 用（P6-2a）。
func (m *Module) PackageDir(id string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readyLocked() != nil {
		return "", false
	}
	if e := m.entries[id]; e != nil && e.err == nil && e.dir != "" {
		return e.dir, true
	}
	return "", false
}

// promptAppendMax 是一個 prompt_append 擴充點的值最多幾個位元組（每次開對話都會帶進指令，不能無限長）。
const promptAppendMax = 4000

// extensionDescMax 是擴充點說明最多幾個位元組（只能一行）。
const extensionDescMax = 200

// PromptAppend 是一段要接在秘書指令後面的補充：某個套件 prompt_append 擴充點現在的值（DEV-154）。
type PromptAppend struct {
	Package, Extension, Description, Text string
}

// PromptAppends 回要接在職位 agentID 指令後面的補充：啟用中、載入沒問題的套件宣告的 prompt_append 擴充點，
// 主人設了值就用設的、沒設用宣告的預設；空的不列。**別人分享的套件（installed）預設值不算**：裝一個套件不該讓它寫的字
// 自動進到秘書的指令（憲法第 10 條），要主人自己在擴充點設了值才帶。設定檔裡超過上限的值（手改的）只帶前面那一段，健檢會提醒。宣告了 agent 的只給那個職位，沒宣告的每一位都接。
// 宣告了 until_record 的，那筆紀錄還不在（NOT_FOUND）才帶；讀不到（其他錯、沒接 vault）就不帶，只講一次的話寧可不講（憲法第 10 條 fail closed）。
// 順序照套件 id、再照宣告順序。套件停用或被拒，它的補充就不再帶（值留在設定裡）。
func (m *Module) PromptAppends(agentID string) []PromptAppend {
	cands := m.promptAppendCandidates(agentID)
	out := make([]PromptAppend, 0, len(cands))
	for _, c := range cands {
		if c.untilKind != "" && !m.recordMissing(c.untilKind, c.untilID) {
			continue
		}
		out = append(out, c.PromptAppend)
	}
	return out
}

// promptAppendCandidate 是一段補充與它的 until_record（完整的種類名稱與編號；空＝一直帶）。
type promptAppendCandidate struct {
	PromptAppend
	untilKind, untilID string
}

// promptAppendCandidates 在鎖裡把補充收齊；讀 vault 留到鎖外（vault 不該卡住套件模組）。
func (m *Module) promptAppendCandidates(agentID string) []promptAppendCandidate {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readyLocked() != nil {
		return nil
	}
	st, err := m.loadSettings()
	if err != nil {
		return nil
	}
	var out []promptAppendCandidate
	for _, id := range m.sortedIDsLocked() {
		e := m.entries[id]
		if e.man == nil || e.err != nil || !e.enabled {
			continue
		}
		for _, x := range e.man.Extension {
			if x.Kind != string(sdk.ExtensionKindPromptAppend) || (x.Agent != "" && x.Agent != agentID) {
				continue
			}
			text, set := st.Extensions[id+"."+x.ID]
			if !set {
				if e.tier == sdk.TierInstalled {
					continue
				}
				text = x.Default
			}
			text = cutUTF8(text, promptAppendMax)
			if text = strings.TrimSpace(text); text == "" {
				continue
			}
			c := promptAppendCandidate{PromptAppend: PromptAppend{Package: id, Extension: x.ID, Description: x.Description, Text: text}}
			if kind, rec, ok := strings.Cut(x.UntilRecord, "/"); ok {
				c.untilKind, c.untilID = id+"."+kind, rec
			}
			out = append(out, c)
		}
	}
	return out
}

// recordMissing：那筆紀錄確定不在（NOT_FOUND）才回 true。
func (m *Module) recordMissing(kind, id string) bool {
	if m.d.Vault == nil {
		return false
	}
	_, err := m.d.Vault.Get(context.Background(), kind, id)
	return err != nil && platform.CodeOf(err) == sdk.ErrorCodeNotFound
}

// PackagePersonas 回各套件 [[contributes.persona]] 宣告的角色卡（agent 的第三個角色來源，SEC-302 S2）。
// 有效（沒被拒、啟用中）的 Active=true；停用或被拒但 manifest 讀得起來的也列出（Active=false），讓已經指派的秘書讀得到原卡。
// manifest 讀不起來的被拒套件沒有宣告可讀，不列。卡檔經符號連結跑到套件資料夾外的略過。
func (m *Module) PackagePersonas() []personaref.PackagePersona {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readyLocked() != nil {
		return nil
	}
	var out []personaref.PackagePersona
	add := func(e *entry) {
		if e.man == nil || e.dir == "" || len(e.man.Contributes.Persona) == 0 {
			return
		}
		active, reason := e.err == nil && e.enabled, ""
		switch {
		case e.err != nil:
			reason = "被拒"
		case !e.enabled:
			reason = "已停用"
		}
		root, err := filepath.EvalSymlinks(e.dir)
		if err != nil {
			return
		}
		for _, p := range e.man.Contributes.Persona {
			file, err := filepath.EvalSymlinks(filepath.Join(e.dir, filepath.FromSlash(p.Path)))
			if err != nil || !strings.HasPrefix(file, root+string(filepath.Separator)) {
				continue
			}
			out = append(out, personaref.PackagePersona{Package: e.id, Slug: p.Slug, Path: file, Tier: string(e.tier), Active: active, Reason: reason})
		}
	}
	for _, id := range m.sortedIDsLocked() {
		add(m.entries[id])
	}
	for _, e := range m.rejected {
		add(e)
	}
	return out
}

// cutUTF8 把 s 截到最多 max 個位元組，不切在一個字的中間。
func cutUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// checkPromptAppends：設定檔裡有 prompt_append 的值超過上限（只可能是手改的；SetExtension 會擋）就提醒，秘書只會收到前面那一段。
func (m *Module) checkPromptAppends(context.Context) []app.CheckResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readyLocked() != nil {
		return nil
	}
	st, err := m.loadSettings()
	if err != nil {
		return nil
	}
	var out []app.CheckResult
	for _, id := range m.sortedIDsLocked() {
		e := m.entries[id]
		if e.man == nil {
			continue
		}
		for _, x := range e.man.Extension {
			if v := st.Extensions[id+"."+x.ID]; x.Kind == string(sdk.ExtensionKindPromptAppend) && len(v) > promptAppendMax {
				out = append(out, app.CheckResult{Status: app.StatusWarn,
					Message: fmt.Sprintf("套件「%s」的補充「%s」太長（%d 個位元組，上限 %d），秘書只會收到前面那一段", id, x.ID, len(v), promptAppendMax),
					Fix:     fmt.Sprintf("evergenie packages set-extension %s %s --value <短一點的內容>", id, x.ID)})
			}
		}
	}
	return out
}
