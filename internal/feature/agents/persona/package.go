package persona

// 角色庫的第三個來源：套件（SEC-302 S2、U-1、U-3）。
// 套件 manifest 的 [[contributes.persona]]（{slug, path}）宣告的卡放在套件資料夾裡；有效代號是 <套件id>.<slug>
// （例 garden.fairy），所以不會跟官方或我的卡撞名（那兩邊的代號不含點）。這裡只讀、不寫；改卡就改套件檔。
// 角色庫不 import packages（業務塊之間不互相 import）：由 packages 實作 PackageSource（與 PackageWriter），registry 用 SetPackagePersonas 注入。

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// PackageSource 由 packages 提供：目前掃到的所有套件角色（含停用的、被拒但 manifest 讀得起來的）。
type PackageSource interface {
	PackagePersonas() []personaref.PackagePersona
}

// PackageWriter 由 packages 提供（跟 PackageSource 同一個物件）：把一張卡寫進 packages/mine/<id>/personas/<slug>.md，
// 並在 package.toml 加 [[contributes.persona]]（寫前備份、原子寫入、寫完驗過，失敗倒回）；回套件資料夾。
// 套件 id 不合格回 Invalid、套件不是 mine 層回 Forbidden、不存在回 NotFound、代號或檔案已存在回 Conflict
// （2026-11-01 從 base 的 personaref 搬來：這是角色庫要 packages 做的事，介面由用的一方定義）。
type PackageWriter interface {
	AddPackagePersona(pkgID, slug string, card []byte) (dir string, err error)
}

// SetPackagePersonas 接上套件角色來源（組裝時呼叫；沒接＝沒有第三個來源，行為跟以前完全一樣）。
func (m *Module) SetPackagePersonas(s PackageSource) {
	m.pkgSrc.mu.Lock()
	m.pkgSrc.src = s
	m.pkgSrc.mu.Unlock()
}

func (m *Module) packageList() []personaref.PackagePersona {
	m.pkgSrc.mu.RLock()
	src := m.pkgSrc.src
	m.pkgSrc.mu.RUnlock()
	if src == nil {
		return nil
	}
	return src.PackagePersonas()
}

func cardOfPackage(pp personaref.PackagePersona) (libCard, bool) {
	c, ok := readCardFile(personaref.SourcePackage, personaref.PackageCode(pp.Package, pp.Slug), pp.Path)
	if !ok {
		return libCard{}, false
	}
	c.pkgID, c.pkgTier = pp.Package, pp.Tier
	if !pp.Active {
		c.inactive = pp.Reason
		if c.inactive == "" {
			c.inactive = "已停用"
		}
	}
	return c, true
}

// packageCards 讀全部套件角色卡（依套件 id、slug 排；檔案不見的略過）。
func (m *Module) packageCards() []libCard {
	list := m.packageList()
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Package != list[j].Package {
			return list[i].Package < list[j].Package
		}
		return list[i].Slug < list[j].Slug
	})
	var out []libCard
	seen := map[string]bool{}
	for _, pp := range list {
		code := personaref.PackageCode(pp.Package, pp.Slug)
		if seen[code] {
			continue
		}
		seen[code] = true
		if c, ok := cardOfPackage(pp); ok {
			out = append(out, c)
		}
	}
	return out
}

// packageCard 依有效代號找一張套件卡；code 不是套件代號或找不到回 false。
func (m *Module) packageCard(code string) (libCard, bool) {
	pkgID, slug, ok := personaref.SplitPackageCode(code)
	if !ok {
		return libCard{}, false
	}
	for _, pp := range m.packageList() {
		if pp.Package == pkgID && pp.Slug == slug {
			return cardOfPackage(pp)
		}
	}
	return libCard{}, false
}

// 角色放進套件（SEC-302 S3、U-2）：新增或複製都是「寫一張新卡進 packages/mine/<id>/personas/」，來源卡不動。

func (m *Module) packageWriter() (PackageWriter, error) {
	m.pkgSrc.mu.RLock()
	src := m.pkgSrc.src
	m.pkgSrc.mu.RUnlock()
	if w, ok := src.(PackageWriter); ok {
		return w, nil
	}
	return nil, platform.Unavailable("套件還沒接上，沒辦法把角色放進套件")
}

// addToPackage 把 data 寫成套件裡的卡，有 avatarFrom（我的角色代號）就把頭像複製過去，回新卡。呼叫前要持有 m.mu。
func (m *Module) addToPackage(ctx context.Context, pkgID, slug string, data []byte, avatarFrom string) (sdk.PersonaCard, error) {
	if !personaref.SlugPattern.MatchString(slug) || strings.EqualFold(slug, "README") {
		return sdk.PersonaCard{}, platform.Invalid("slug", "角色代號用小寫英文加數字（英文開頭），例：fairy")
	}
	w, err := m.packageWriter()
	if err != nil {
		return sdk.PersonaCard{}, err
	}
	if _, ok := m.packageCard(personaref.PackageCode(pkgID, slug)); ok {
		return sdk.PersonaCard{}, platform.Conflict("套件 " + pkgID + " 已經有角色 " + slug)
	}
	dir, err := w.AddPackagePersona(pkgID, slug, data)
	if err != nil {
		return sdk.PersonaCard{}, err
	}
	if avatarFrom != "" {
		m.copyAvatars(avatarFrom, filepath.Join(dir, "personas", "avatars"), slug)
	}
	return m.cardResult(ctx, personaref.SourcePackage, personaref.PackageCode(pkgID, slug))
}

// copyAvatars 把 personas/mine/avatars/<from>.<png|mp4> 複製成 <destDir>/<slug>.<ext>（只複製一般檔案；目的地已有就不蓋）。
// 頭像是附贈的：任何一步失敗都略過，不讓卡已經寫好的事回報成失敗。
func (m *Module) copyAvatars(from, destDir, slug string) {
	for _, ext := range []string{".png", ".mp4"} {
		src := filepath.Join(m.home, "personas", "mine", "avatars", from+ext)
		if info, err := os.Lstat(src); err != nil || !info.Mode().IsRegular() {
			continue
		}
		dst := filepath.Join(destDir, slug+ext)
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil || os.MkdirAll(destDir, 0o755) != nil {
			continue
		}
		_ = platform.AtomicWrite(dst, data, 0o644)
	}
}

// CreatePersonaIn 新增一張套件裡的卡（只限 packages/mine/ 的套件）：設計中，內文是範本；名字沒給就用代號。
func (m *Module) CreatePersonaIn(ctx context.Context, pkgID, slug, name string, aka []string, by string) (sdk.PersonaCard, error) {
	if err := requireOwner(by); err != nil {
		return sdk.PersonaCard{}, err
	}
	name, aka = cleanNames(name, aka)
	if name == "" && slug != "" {
		name = strings.ToUpper(slug[:1]) + slug[1:]
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	data := renderCard(map[string]any{"schema": 1, "name": name, "aka": aka, "status": personaref.StatusDesign}, nil, personaTemplate)
	return m.addToPackage(ctx, pkgID, slug, data, "")
}

// CopyPersonaIn 把一張卡複製進套件（設計中）；來源不動。名字與別名沒給就沿用來源的（套件的卡不佔名字，不會撞名）；
// 來源是我的卡時連同頭像一起複製。
func (m *Module) CopyPersonaIn(ctx context.Context, pkgID, from, slug, name string, aka []string, by string) (sdk.PersonaCard, error) {
	if err := requireOwner(by); err != nil {
		return sdk.PersonaCard{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	src, ok := effective(m.cards(), from)
	if !ok {
		return sdk.PersonaCard{}, platform.NotFound("角色庫裡沒有 " + from)
	}
	if src.parseErr != nil || src.noName {
		return sdk.PersonaCard{}, platform.Invalid("from", "來源卡的格式有問題，先修好再複製")
	}
	name, aka = cleanNames(name, aka)
	if name == "" {
		name = src.name()
	}
	if len(aka) == 0 {
		aka = src.aka()
	}
	if slug == "" {
		slug = strings.TrimPrefix(src.slug, personaref.MinePrefix) // 套件裡的代號不帶 my_（那是主人自己的角色才有的開頭）
		if src.source == personaref.SourcePackage {
			_, slug, _ = personaref.SplitPackageCode(src.slug)
		}
	}
	avatar := ""
	if src.source == personaref.SourceMine {
		avatar = src.slug
	}
	data := renderCard(map[string]any{"schema": 1, "name": name, "aka": aka, "status": personaref.StatusDesign}, nil, src.rec.Body)
	return m.addToPackage(ctx, pkgID, slug, data, avatar)
}
