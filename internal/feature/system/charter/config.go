package charter

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
)

// docConf 是 home.toml [[rules.docs]] 的一筆（D-44）：清單的順序就是位階，最上面最高。
type docConf struct {
	ID      string
	Title   string
	Path    string // 設定寫的樣子（相對家的資料夾，或 ~／絕對路徑）
	Section string // 只取這個標題底下的一段；空＝整個檔
	Rank    int    // 1 起算
}

// config 是讀到的設定：explicit＝home.toml 有寫 [rules]（沒寫就用預設，檔案不在也不算問題）；problem 非空時 docs 是空的。
type config struct {
	docs     []docConf
	explicit bool
	problem  string
}

// defaultDocs 是沒寫 [rules] 時的預設：跟作者自己的正式家現在的放法一致。
func defaultDocs() []docConf {
	return []docConf{
		{ID: "constitution", Title: "憲法", Path: "docs/constitution.md", Rank: 1},
		{ID: "purpose", Title: "宗旨", Path: "docs/projects/evergenie.md", Section: "宗旨", Rank: 2},
		{ID: "law", Title: "法律", Path: "docs/projects/evergenie.md", Section: "法律", Rank: 3},
	}
}

var docIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// loadConfig 讀 home.toml 的 [rules]；讀不到家的設定也回預設（讓頁面照樣能開，doctor 由別的檢查報設定壞掉）。
func loadConfig(home string) config {
	raw, err := platform.ReadHomeTable(home)
	if err != nil {
		return config{docs: defaultDocs()}
	}
	table, ok := raw["rules"]
	if !ok {
		return config{docs: defaultDocs()}
	}
	section, ok := table.(map[string]any)
	if !ok {
		return config{explicit: true, problem: "home.toml 的 rules 要是一個表格（[rules]）"}
	}
	list, ok := section["docs"]
	if !ok {
		return config{explicit: true}
	}
	items, ok := list.([]map[string]any)
	if !ok {
		if empty, isList := list.([]any); isList && len(empty) == 0 {
			return config{explicit: true}
		}
		return config{explicit: true, problem: "home.toml 的 rules.docs 要寫成 [[rules.docs]] 一筆一筆"}
	}
	out := config{explicit: true}
	seen := map[string]bool{}
	places := map[string]bool{}
	for i, item := range items {
		d, problem := parseDoc(item)
		if problem != "" {
			return config{explicit: true, problem: fmt.Sprintf("home.toml 的第 %d 份規矩文件：%s", i+1, problem)}
		}
		if seen[d.ID] {
			return config{explicit: true, problem: fmt.Sprintf("home.toml 的規矩文件 id「%s」重複了", d.ID)}
		}
		place := d.Path + "#" + d.Section
		if places[place] {
			return config{explicit: true, problem: fmt.Sprintf("home.toml 的規矩文件「%s」跟前面一份指到同一個地方", d.ID)}
		}
		seen[d.ID], places[place] = true, true
		d.Rank = i + 1
		out.docs = append(out.docs, d)
	}
	return out
}

func parseDoc(item map[string]any) (docConf, string) {
	text := func(k string) (string, bool) {
		v, present := item[k]
		if !present {
			return "", true
		}
		s, ok := v.(string)
		return strings.TrimSpace(s), ok
	}
	var d docConf
	var ok bool
	for _, k := range []string{"id", "title", "path", "section"} {
		v, good := text(k)
		if !good {
			return d, k + " 要是文字"
		}
		switch k {
		case "id":
			d.ID = v
		case "title":
			d.Title = v
		case "path":
			d.Path = v
		case "section":
			d.Section = v
		}
	}
	for k := range item {
		switch k {
		case "id", "title", "path", "section":
		default:
			return d, "不認得的欄位 " + k + "（只有 id、title、path、section）"
		}
	}
	if !docIDPattern.MatchString(d.ID) {
		return d, "id 要是小寫英文開頭，只用小寫英文、數字、減號（例：law）"
	}
	if d.Title == "" {
		return d, "要寫 title（頁面上顯示的名字，例：法律）"
	}
	if d.Path == "" {
		return d, "要寫 path（相對家的資料夾，例：docs/constitution.md）"
	}
	if ok = filepath.Ext(d.Path) == ".md"; !ok {
		return d, "path 要是 .md 檔"
	}
	switch {
	case strings.HasPrefix(d.Path, "~/"), filepath.IsAbs(d.Path):
	case strings.HasPrefix(d.Path, "~"):
		return d, "~ 只能接 /（例：~/Documents/rules.md）"
	default:
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(d.Path)))
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return d, "相對路徑不能跑出家的資料夾（..）；要放別處請寫絕對路徑或 ~/ 開頭"
		}
		if first, _, _ := strings.Cut(clean, "/"); first == "vault" || first == "runtime" {
			return d, "規矩文件不能放在家的 vault/ 或 runtime/ 裡（那是系統的資料）"
		}
	}
	if strings.ContainsAny(d.Section, "\r\n") {
		return d, "section 只能是一行標題文字"
	}
	return d, ""
}

// resolve 回這份文件在磁碟上的位置。
func (d docConf) resolve(home string) string {
	p := d.Path
	if strings.HasPrefix(p, "~/") {
		return platform.ExpandHome(p)
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(home, filepath.FromSlash(p))
}
