// Package personafixture 是測試用的角色庫：直接讀測試家裡的 personas/ 檔案，實作名冊的 agent.PersonaSource
// 與 session 測口吻要的 ShowPersona／StyleInfo。
//
// 正式的角色庫是業務模組 internal/feature/agents/persona（registry 用 SetPersonaSource 接）；底層 agent、session 的測試不能 import 業務模組
// （depcheck 擋），所以用這份只有讀卡、說話層的最小版本。套件角色、設計中狀態的規則、角色庫的編輯都不在這裡，那些在 personas 自己的測試。
package personafixture

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// Library 讀官方角色資料夾（照家的佈局，第 1 版 personas/）的 <slug>.md（官方）與家裡的 personas/mine/<slug>.md（我的）；說話層讀 personas/_style.md。
// Bundled 是跟著程式的公開卡資料夾（PRS-25，測試自己指定；空＝沒有）：官方卡跟正式角色庫一樣走 sdk.OfficialCardIn
// （同代號：官方資料夾＞我的＞跟著程式的，PRS-27）。
type Library struct {
	Home    string
	Bundled string
}

// New 回一個讀 home 的測試角色庫。
func New(home string) *Library { return &Library{Home: home} }

// Card 讀一張人設卡：官方優先，再我的（跟著程式的卡被我的卡蓋過時不算官方，PRS-27）；不存在或缺 name 回 invalid（跟正式角色庫同樣的白話）。
func (l *Library) Card(slug string) (personaref.CardInfo, error) {
	if slug == "" || filepath.Base(slug) != slug || strings.ContainsAny(slug, "/\\") {
		return personaref.CardInfo{}, platform.Invalid("persona", "請指定有效的人設")
	}
	official, _ := l.OfficialCardFile(slug)
	for _, where := range []struct{ file, source string }{{official, personaref.SourceOfficial}, {filepath.Join(l.Home, "personas", "mine", slug+".md"), personaref.SourceMine}} {
		data, err := os.ReadFile(where.file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			break
		}
		r, err := personaref.ParseCard(data)
		info := personaref.CardInfo{Record: r, Source: where.source, Status: status(where.source, r)}
		if err != nil {
			return info, err
		}
		if name, ok := r.Fields["name"].(string); !ok || name == "" {
			return info, platform.Invalid("name", "人設缺少名稱")
		}
		return info, nil
	}
	return personaref.CardInfo{}, platform.Invalid("persona", "人設卡不存在或無法讀取")
}

// presets 是 a–d 四級的測試文字（正式的全文在 personas 業務模組內嵌的 styles/*.md）。
var presets = map[string]string{"a": "說話層 A", "b": "說話層 B", "c": "說話層 C", "d": "說話層 D"}

// Style 回現在生效的說話層：personas/_style.md 在就是 custom，不在就是 c。
func (l *Library) Style() (level, text string, customExists bool, err error) {
	custom, exists, err := l.custom()
	if err != nil {
		return "", "", false, err
	}
	if exists {
		return "custom", custom, true, nil
	}
	return "c", presets["c"], false, nil
}

// StyleInfo 回現在生效的說話層與五個選項的全文（session 測口吻用）。
func (l *Library) StyleInfo(context.Context) (level, text string, customExists bool, levels []sdk.PersonaStyleLevel, err error) {
	level, text, customExists, err = l.Style()
	if err != nil {
		return "", "", false, nil, err
	}
	custom, _, _ := l.custom()
	for _, lv := range []string{"a", "b", "c", "d"} {
		levels = append(levels, sdk.PersonaStyleLevel{Level: lv, Text: presets[lv], Exists: true})
	}
	levels = append(levels, sdk.PersonaStyleLevel{Level: "custom", Text: custom, Exists: customExists})
	return level, text, customExists, levels, nil
}

// ShowPersona 回一張卡的看板資訊、內文與原始全文；沒有這張卡回 not found（session 測口吻用）。
func (l *Library) ShowPersona(_ context.Context, slug string) (sdk.PersonaCard, string, string, error) {
	if !l.exists(slug) {
		return sdk.PersonaCard{}, "", "", platform.NotFound("角色庫裡沒有 " + slug)
	}
	info, err := l.Card(slug)
	if err != nil {
		return sdk.PersonaCard{}, "", "", err
	}
	name, _ := info.Record.Fields["name"].(string)
	return sdk.PersonaCard{Slug: slug, Name: name, Source: info.Source, Status: info.Status}, info.Record.Body, "", nil
}

// OfficialCardFile 回官方卡在哪（找官方卡的共用入口 sdk.OfficialCardIn：官方資料夾先，沒有才用 Bundled；我的卡同代號時 Bundled 不算）。
func (l *Library) OfficialCardFile(slug string) (string, bool) {
	return sdk.OfficialCardIn(platform.OfficialPersonasDir(l.Home), l.Bundled, filepath.Join(l.Home, "personas", "mine"), slug)
}

func (l *Library) exists(slug string) bool {
	official, _ := l.OfficialCardFile(slug)
	for _, p := range []string{official, filepath.Join(l.Home, "personas", "mine", slug+".md")} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func (l *Library) custom() (string, bool, error) {
	data, err := os.ReadFile(filepath.Join(l.Home, "personas", "_style.md"))
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, platform.Unavailable("無法讀取說話層")
	}
	return strings.TrimSpace(string(data)), true, nil
}

// status 照角色庫的規則（feature/agents/persona 的 cardStatus）：只有我的卡能是設計中（測試角色庫沒有套件）。
func status(source string, r vault.Record) string {
	if s, _ := r.Fields["status"].(string); source == personaref.SourceMine && strings.TrimSpace(s) == personaref.StatusDesign {
		return personaref.StatusDesign
	}
	return personaref.StatusLive
}
