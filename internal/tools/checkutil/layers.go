package checkutil

import (
	"regexp"
	"sort"
	"strings"
)

// 兩層結構（法律第 8 條，2026-11-01 舊的 L0–L6 依賴表退休）的共同規則：depcheck 查 import、
// doccheck 查說明卡的「依賴」。

// 說明卡「種類」行開頭寫的種類。
const (
	KindBase    = "base"    // 「底層模組」：internal/base/<名>
	KindFeature = "feature" // 「業務塊」：internal/feature/<名>
	KindTool    = "tool"    // 「工具…」：internal/tools/<名>，不受依賴規則限制
	KindPackage = "package" // 「官方…套件」：packages/official/<id>，只用 sdk
)

// CardKind 讀說明卡的「種類」行：開頭是「底層模組」「業務塊」「工具」「官方」分別回 KindBase、KindFeature、KindTool、KindPackage；
// 其他（或沒有種類行）回空字串。
func CardKind(text string) string {
	for _, line := range strings.Split(text, "\n") {
		rest, ok := strings.CutPrefix(line, "**種類：**")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		switch {
		case strings.HasPrefix(rest, "底層模組"):
			return KindBase
		case strings.HasPrefix(rest, "業務塊"):
			return KindFeature
		case strings.HasPrefix(rest, "工具"):
			return KindTool
		case strings.HasPrefix(rest, "官方"):
			return KindPackage
		}
		return ""
	}
	return ""
}

var depToken = regexp.MustCompile("`([a-z/-]+)`")

// CardDeps 檢查一張說明卡（種類 kind、名稱 name）的「依賴」段落，回傳違規說明。
// 依賴寫成 `名稱`、`base/名稱`、`feature/名稱` 或 `internal/…` 都認得；
// kinds 是所有說明卡的 名稱 → 種類，用來認出沒寫路徑的名稱。
// 規則照路徑：底層不能依賴業務塊；業務塊不能依賴別的業務塊；套件只能依賴 sdk；工具不受限。
// 底層之間的順序不在這裡查：卡的依賴要跟實際 import 一致（doccheck），import 的順序由 depcheck 照「底層模組」表擋。
func CardDeps(kind, name, depSection string, kinds map[string]string) []string {
	var problems []string
	seen := map[string]bool{}
	for _, m := range depToken.FindAllStringSubmatch(depSection, -1) {
		token := m[1]
		if seen[token] {
			continue
		}
		seen[token] = true
		if kind == KindPackage {
			if token != "sdk" {
				problems = append(problems, name+": 依賴 "+token+" 不允許（套件只用 `sdk`，不 import 任何 internal/）")
			}
			continue
		}
		path := strings.TrimPrefix(token, "internal/")
		toKind, to := "", path
		switch {
		case strings.HasPrefix(path, KindBase+"/"):
			toKind, to = KindBase, strings.Split(strings.TrimPrefix(path, KindBase+"/"), "/")[0]
		case strings.HasPrefix(path, KindFeature+"/"):
			toKind, to = KindFeature, strings.Split(strings.TrimPrefix(path, KindFeature+"/"), "/")[0]
		default:
			to = strings.Split(path, "/")[0]
			toKind = kinds[to]
		}
		switch {
		case kind == KindTool:
		case toKind == KindFeature && kind == KindBase:
			problems = append(problems, name+": 依賴 "+token+" 不允許（底層模組不能依賴業務塊）")
		case toKind == KindFeature && to != name:
			problems = append(problems, name+": 依賴 "+token+" 不允許（業務塊不能依賴別的業務塊，要對方的能力就在自己的 Deps 定義小 interface）")
		}
	}
	sort.Strings(problems)
	return problems
}
