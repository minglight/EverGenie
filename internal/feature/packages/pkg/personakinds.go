package pkg

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// PersonaKind 是一種會跟著角色代號走的套件資料（PKG-64）：完整種類名 <套件>.<種類>、哪一層的套件、它的宣告。
type PersonaKind struct {
	Kind    string
	Package string
	Tier    sdk.Tier
	Decl    sdk.DataKindDecl
}

// PersonaKindIssue 是一個讀不起來的套件：id 與白話原因。
type PersonaKindIssue struct {
	Package string
	Message string
}

// PersonaKinds 離線讀這個家每一層套件的 package.toml（官方、別人分享的、主人自己的；同一個 id 用會載入的那一份，
// 跟 discover 的 resolveTiers 同一條規則），回有宣告角色代號（persona_field、persona_id、persona_prefixed）的資料種類。
// 不看套件有沒有啟用：停用的套件紀錄還在 vault/pkg/，重新啟用就會讀到，代號也要跟著改。
// 第二個回傳值是讀不起來的套件：它們的紀錄沒辦法照宣告改（搬家工具看那個套件有沒有紀錄，決定擋下或列出來）。
// 給 evergenie home tidy、personas promote 用（組裝點 cmd/evergenie 轉給角色庫的改代號工具，角色庫不 import packages）。
func PersonaKinds(home string) ([]PersonaKind, []PersonaKindIssue) {
	var all []*loaded
	all = append(all, scanRoot(OfficialDir(home), sdk.TierOfficial)...)
	all = append(all, scanRoot(platform.LayoutFor(home).InstalledPackages(), sdk.TierInstalled)...)
	all = append(all, scanRoot(filepath.Join(home, "packages", string(sdk.TierMine)), sdk.TierMine)...)
	active, _, _ := resolveTiers(all)
	ids := make([]string, 0, len(active))
	for id := range active {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []PersonaKind
	var problems []PersonaKindIssue
	for _, id := range ids {
		l := active[id]
		if l.manifest == nil {
			msg := "套件 " + id + " 讀不起來，它的紀錄沒辦法跟著角色代號改"
			if l.err != nil {
				msg += "（" + l.err.Error() + "）"
			}
			problems = append(problems, PersonaKindIssue{Package: id, Message: msg})
			continue
		}
		for _, k := range l.manifest.dataKinds {
			d := k.decl()
			if d.PersonaField == "" && d.PersonaID == "" && len(d.PersonaPrefixed) == 0 {
				continue
			}
			d.Fields = nil
			out = append(out, PersonaKind{Kind: id + "." + k.Kind, Package: id, Tier: l.tier, Decl: d})
		}
	}
	return out, problems
}

// PersonasFileKinds 是這個家的套件（不管有沒有啟用）宣告「照紀錄帶圖」的種類（export = by_persona 而且有 file_field，例如衣櫃的照片紀錄），
// 完整名稱、排序過。建官方角色包連不到服務時用（`personas pack build --no-records`，DEV-160 審查 M2）：有這種種類就不建——哪些圖能公開只標在紀錄上。
// 有套件讀不起來（不知道它宣告了什麼）回錯誤。只讀 package.toml，不用服務在跑。
func PersonasFileKinds(home string) ([]string, error) {
	kinds, problems := PersonaKinds(home)
	if len(problems) > 0 {
		msgs := make([]string, 0, len(problems))
		for _, p := range problems {
			msgs = append(msgs, p.Message)
		}
		return nil, errors.New(strings.Join(msgs, "；"))
	}
	var out []string
	for _, k := range kinds {
		if k.Decl.Export == sdk.DataExportByPersona && k.Decl.FileField != "" {
			out = append(out, k.Kind)
		}
	}
	sort.Strings(out)
	return out, nil
}
