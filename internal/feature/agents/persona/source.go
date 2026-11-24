package persona

// 名冊讀角色庫的入口：Card 與 Style 合起來就是 agent.PersonaSource（agent 定義這個小 interface，registry 用 SetPersonaSource 接進去）。

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
)

// Card 讀一張人設卡（同代號：官方資料夾＞我的＞跟著程式的公開卡，PRS-27——官方這一輪經 cardPath，跟著程式的那張被我的卡蓋過時讀不到、
// 就輪到我的；代號是 <套件id>.<slug> 時找套件）；不管有沒有上線。
// 代號無效、卡不存在或讀不了回 invalid；缺 name 回 invalid（此時 info 仍帶讀到的內容）。
func (m *Module) Card(slug string) (personaref.CardInfo, error) {
	if slug == "" || filepath.Base(slug) != slug || strings.ContainsAny(slug, "/\\") || slug == "." || slug == ".." {
		return personaref.CardInfo{}, platform.Invalid("persona", "請指定有效的人設")
	}
	if personaref.IsPackageCode(slug) {
		c, ok := m.packageCard(slug)
		if !ok || c.parseErr != nil {
			return personaref.CardInfo{}, platform.Invalid("persona", "人設卡不存在或無法讀取")
		}
		info := personaref.CardInfo{Record: c.rec, Source: personaref.SourcePackage, PackageID: c.pkgID, Inactive: c.inactive, Status: cardStatus(personaref.SourcePackage, c.rec)}
		if c.noName {
			return info, platform.Invalid("name", "人設缺少名稱")
		}
		return info, nil
	}
	var data []byte
	var err error
	source := personaref.SourceOfficial
	for _, source = range []string{personaref.SourceOfficial, personaref.SourceMine} {
		data, err = os.ReadFile(m.cardPath(source, slug))
		if err == nil || !os.IsNotExist(err) {
			break
		}
	}
	if err != nil {
		return personaref.CardInfo{}, platform.Invalid("persona", "人設卡不存在或無法讀取")
	}
	r, err := parseCard(data)
	info := personaref.CardInfo{Record: r, Source: source, Status: cardStatus(source, r)}
	if err != nil {
		return info, err
	}
	if name, ok := r.Fields["name"].(string); !ok || name == "" {
		return info, platform.Invalid("name", "人設缺少名稱")
	}
	return info, nil
}
