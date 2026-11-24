package pkg

// 把角色放進套件（SEC-302 S3、U-1、U-2）：agent 的角色庫新增／複製「去套件」時，經角色庫的 PackageWriter 介面（registry 接）呼叫到這裡。
// 只動 packages/mine/<id>/：personas/<slug>.md＋package.toml 尾端多一段 [[contributes.persona]]；
// 官方與 installed 層一律 FORBIDDEN（PKG-9、PKG-N2）。寫 manifest 前先備份，原子寫入，寫完用 ParseManifest 驗過，失敗倒回。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func (m *Module) writeFile(path string, data []byte, mode os.FileMode) error {
	if m.atomicWrite != nil {
		return m.atomicWrite(path, data, mode)
	}
	return platform.AtomicWrite(path, data, mode)
}

// AddPackagePersona 在 packages/mine/<pkgID>/ 新增 personas/<slug>.md（內容 card）並宣告進 package.toml；回套件資料夾。
func (m *Module) AddPackagePersona(pkgID, slug string, card []byte) (string, error) {
	if !idPattern.MatchString(pkgID) {
		return "", platform.Invalid("package", "職位 id 不正確")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return "", err
	}
	e := m.entries[pkgID]
	if e == nil {
		for _, r := range m.rejected {
			if r.id == pkgID && r.tier == sdk.TierMine {
				e = r
			}
		}
	}
	if e == nil || e.dir == "" {
		return "", platform.NotFound(fmt.Sprintf("沒有套件「%s」", pkgID))
	}
	if e.tier != sdk.TierMine {
		return "", platform.Forbidden(fmt.Sprintf("套件「%s」是%s的，不能直接改它的角色（只有 packages/mine/ 裡你自己的套件能放角色；官方的角色請用角色庫複製成自己的）", pkgID, tierLabel(e.tier)))
	}
	dir := e.dir
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return "", platform.NotFound(fmt.Sprintf("套件「%s」的資料夾不在", pkgID))
	}
	manPath := filepath.Join(dir, "package.toml")
	old, err := os.ReadFile(manPath)
	if err != nil {
		return "", platform.Unavailable("讀不了 package.toml")
	}
	if _, err := ParseManifest(dir, sdk.TierMine); err != nil {
		return "", platform.Invalid("package.toml", fmt.Sprintf("套件「%s」現在的 package.toml 就有問題（%s），先修好再放角色", pkgID, err))
	}
	rel := "personas/" + slug + ".md"
	cardPath := filepath.Join(dir, filepath.FromSlash(rel))
	for _, p := range []string{filepath.Join(dir, "personas"), cardPath} {
		if info, err := os.Lstat(p); err == nil && (info.Mode()&os.ModeSymlink != 0 || (p == cardPath && !info.Mode().IsRegular())) {
			return "", platform.Invalid("slug", "personas 資料夾或角色檔是符號連結，不寫")
		}
	}
	if _, err := os.Lstat(cardPath); err == nil {
		return "", platform.Conflict(fmt.Sprintf("套件「%s」的 personas/%s.md 已經存在", pkgID, slug))
	}
	man, _ := ParseManifest(dir, sdk.TierMine)
	for _, p := range man.Contributes.Persona {
		if p.Slug == slug {
			return "", platform.Conflict(fmt.Sprintf("套件「%s」已經有角色 %s", pkgID, slug))
		}
	}

	backup := platform.RuntimeDir(m.home, "backups", "packages", fmt.Sprintf("%s-package.toml-%s", pkgID, m.d.Clock.Now().Format("20060102-150405.000000000")))
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		return "", platform.Unavailable("無法備份 package.toml")
	}
	if err := platform.AtomicWrite(backup, old, 0o600); err != nil {
		return "", platform.Unavailable("無法備份 package.toml")
	}
	if err := os.MkdirAll(filepath.Dir(cardPath), 0o755); err != nil {
		return "", platform.Unavailable("無法建立 personas 資料夾")
	}
	if err := m.writeFile(cardPath, card, 0o644); err != nil {
		return "", platform.Unavailable("無法寫入角色卡")
	}
	undo := func() {
		_ = os.Remove(cardPath)
		_ = platform.AtomicWrite(manPath, old, 0o644)
	}
	text := string(old)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += fmt.Sprintf("\n[[contributes.persona]]\nslug = %q\npath = %q\n", slug, rel)
	if err := m.writeFile(manPath, []byte(text), 0o644); err != nil {
		undo()
		return "", platform.Unavailable("無法寫入 package.toml，已倒回")
	}
	verify := func() error {
		nm, err := ParseManifest(dir, sdk.TierMine)
		if err != nil {
			return err
		}
		for _, p := range nm.Contributes.Persona {
			if p.Slug == slug && p.Path == rel {
				return nil
			}
		}
		return platform.Internal("寫完讀不到新角色的宣告")
	}
	if err := verify(); err != nil {
		undo()
		return "", platform.Invalid("package.toml", fmt.Sprintf("寫入後驗證沒過，已倒回：%s", err))
	}
	now := m.d.Clock.Now()
	m.rescanLocked(now)
	m.seen[dir] = fingerprint(dir) // 這是核心自己改的，不要當成使用者改了程式而重編（資料套件本來就不編）
	delete(m.pend, dir)
	if m.log != nil {
		m.log.Info("套件新增角色", "id", pkgID, "slug", slug)
	}
	return dir, nil
}

func tierLabel(t sdk.Tier) string {
	switch t {
	case sdk.TierOfficial:
		return "官方"
	case sdk.TierInstalled:
		return "別人分享（installed）"
	}
	return string(t)
}
