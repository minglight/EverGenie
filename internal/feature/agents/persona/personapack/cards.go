package personapack

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 跟著程式的公開卡（PRS-25、PRS-26）：公開角色的卡片文字放在 repo 的 packages/official/personas/cards/<代號>.md，
// 發行檔照官方套件整包帶著（裝在那一版的程式資料夾裡，唯讀，換版跟著換）；圖還是走角色包。
// 正本在角色包的來源家（[admin] public_personas 列的、官方位置的卡），repo 裡的是 `personas pack cards --repo` 產生的副本；
// `personas pack build --set-pointer` 也一起寫，讓同一次發版的角色包與程式帶的卡是同一份。

// CardsResult 是把公開卡寫進 repo 的結果（代號依字母排）。
type CardsResult struct {
	Dir       string   `json:"dir"`
	Written   []string `json:"written,omitempty"`   // 新增或內容換了的
	Unchanged []string `json:"unchanged,omitempty"` // 跟 repo 裡的一樣
	Removed   []string `json:"removed,omitempty"`   // 不再公開、從 repo 拿掉的
	Skipped   []string `json:"skipped,omitempty"`   // 刻意不收的（白話一行一個）
}

// BundledCardsDir 是 repo（或解開的發行檔）裡跟著程式的公開卡資料夾。
func BundledCardsDir(repo string) string {
	return filepath.Join(repo, "packages", "official", "personas", "cards")
}

// publicSlugs 是來源家 [admin] public_personas 去掉重複、驗過格式的代號；沒寫回 Invalid（說明怎麼設）。
func publicSlugs(h *platform.Home, what string) ([]string, error) {
	slugs := dedupe(h.Admin.PublicPersonas)
	if len(slugs) == 0 {
		return nil, platform.Invalid("admin.public_personas", "home.toml 的 [admin] public_personas 還沒寫要公開哪些角色（例：public_personas = [\"kaguya\", \"athena\"]），所以不"+what)
	}
	for _, s := range slugs {
		if !slugPattern.MatchString(s) {
			return nil, platform.Invalid("admin.public_personas", fmt.Sprintf("公開角色的代號「%s」格式不對（小寫英文與數字）", s))
		}
	}
	return slugs, nil
}

// ownerNameIn：公開卡會給所有人，卡裡寫了這個家主人的名字（home.toml [user] name）就不收。
// 名字少於兩個字、或本來就叫「主人」不查（卡裡稱呼主人一律寫「主人」）；英文名字照整個字比，不分大小寫。
func ownerNameIn(h *platform.Home, slug string, data []byte) error {
	name := strings.TrimSpace(h.User.Name)
	if utf8.RuneCountInString(name) < 2 || name == "主人" {
		return nil
	}
	var hit bool
	if isASCII(name) {
		hit = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(name) + `($|[^A-Za-z0-9_])`).Match(data)
	} else {
		hit = bytes.Contains(data, []byte(name))
	}
	if hit {
		return platform.Invalid("admin.public_personas", fmt.Sprintf("公開角色 %s 的卡寫了主人的名字「%s」；公開的卡會給所有人，先在卡裡改成「主人」再來", slug, name))
	}
	return nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// CardsOptions 是 `evergenie personas pack cards` 的參數。
type CardsOptions struct {
	Home string // 角色包的來源家
	Repo string // EverGenie repo 的根目錄
}

// ExportCards 把來源家的公開卡寫進 repo 的 packages/official/personas/cards/（不 commit）：
// 只收 public_personas 列的、官方位置的卡；標了隱藏版的不收（列在 skipped）；卡裡寫了主人的名字整個不寫。
// 全部先讀、先驗，才寫；資料夾裡不再公開的 <代號>.md 拿掉，其他檔不碰。
func ExportCards(o CardsOptions) (*CardsResult, error) {
	h, err := platform.LoadHome(o.Home)
	if err != nil {
		return nil, err
	}
	slugs, err := publicSlugs(h, "寫公開卡")
	if err != nil {
		return nil, err
	}
	if o.Repo == "" {
		return nil, platform.Invalid("repo", "要用 --repo 指定 EverGenie 的 repo")
	}
	if !isFile(filepath.Join(o.Repo, "packages", "official", "personas", "package.toml")) {
		return nil, platform.Invalid("repo", "「"+o.Repo+"」不是 EverGenie 的 repo（找不到 packages/official/personas/package.toml）")
	}
	cards, skipped, err := readPublicCards(h, o.Home, slugs)
	if err != nil {
		return nil, err
	}
	res, err := writeBundledCards(BundledCardsDir(o.Repo), cards)
	if err != nil {
		return nil, err
	}
	res.Skipped = skipped
	return res, nil
}

// readPublicCards 讀要公開的卡（代號 → 內容）；標了隱藏版的列在 skipped。卡不在、不是一般檔、太大、寫了主人的名字都回錯。
func readPublicCards(h *platform.Home, home string, slugs []string) (map[string][]byte, []string, error) {
	dir := platform.OfficialPersonasDir(home)
	cards := map[string][]byte{}
	var skipped []string
	for _, slug := range slugs {
		p := filepath.Join(dir, slug+".md")
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			return nil, nil, platform.Invalid("admin.public_personas", fmt.Sprintf("公開角色「%s」沒有角色卡 personas/%s.md", slug, slug))
		}
		if info.Size() > MaxFile {
			return nil, nil, platform.Invalid("admin.public_personas", fmt.Sprintf("公開角色「%s」的卡太大", slug))
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, nil, platform.Unavailable("讀不了 personas/" + slug + ".md")
		}
		if why := Excluded(slug+".md", data); why != "" {
			skipped = append(skipped, "cards/"+slug+".md："+why)
			continue
		}
		if err := ownerNameIn(h, slug, data); err != nil {
			return nil, nil, err
		}
		cards[slug] = data
	}
	return cards, skipped, nil
}

// writeBundledCards 把卡寫進 dir（內容一樣的不動），dir 裡不在 cards 的 <代號>.md 拿掉。
func writeBundledCards(dir string, cards map[string][]byte) (*CardsResult, error) {
	res := &CardsResult{Dir: dir}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, platform.Unavailable("無法建立 " + dir)
	}
	slugs := make([]string, 0, len(cards))
	for s := range cards {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		p := filepath.Join(dir, slug+".md")
		if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, cards[slug]) {
			res.Unchanged = append(res.Unchanged, slug)
			continue
		}
		if err := platform.AtomicWrite(p, cards[slug], 0o644); err != nil {
			return nil, platform.Unavailable("無法寫入 " + p)
		}
		res.Written = append(res.Written, slug)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, platform.Unavailable("讀不了 " + dir)
	}
	for _, e := range entries {
		slug, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || e.IsDir() || !slugPattern.MatchString(slug) || cards[slug] != nil {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return nil, platform.Unavailable("無法拿掉 " + filepath.Join(dir, e.Name()))
		}
		res.Removed = append(res.Removed, slug)
	}
	return res, nil
}
