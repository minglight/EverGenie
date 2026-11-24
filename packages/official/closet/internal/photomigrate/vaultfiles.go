package photomigrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// ReadKindFiles 直接讀一個家（或它的複本）的 vault/pkg/closet/<kind>/ 檔案，只給預演用（不經核心、只讀）：
// 最上層的 *.md 是現有的，archive/<年-月>/*.md 是封存的。真的轉換走衣櫃程式的 vault API，不用這個。
// 套件不能 import 核心的 YAML 解析（PKC-N1），這裡只認核心寫出來的那種簡單檔頭：key: 值、key: 後面接「- 值」的清單、
// [a, b] 的單行清單、| 與 > 的多行文字（略過內容）；認不得的檔回在 bad（檔名＋原因），不猜。
//
// officialRoot 不是空的時也讀官方層 <官方角色資料夾>/records/pkg/closet/<kind>/（vault 卡 VLT-19：官方角色放家外的家，
// 官方衣服的目錄在那裡）；同一個編號家裡有就用家裡的（同 vault 的合併規則）。
func ReadKindFiles(home, officialRoot, kind string) (recs []sdk.Record, bad []string, err error) {
	local, bad, err := readLayer(filepath.Join(home, "vault", "pkg", "closet", kind), kind)
	if err != nil || officialRoot == "" {
		return local, bad, err
	}
	layer := filepath.Join(officialRoot, "records", "pkg", "closet", kind)
	if same(layer, filepath.Join(home, "vault", "pkg", "closet", kind)) {
		return local, bad, nil
	}
	off, offBad, err := readLayer(layer, kind)
	if err != nil {
		return nil, nil, err
	}
	have := map[string]bool{}
	for _, r := range local {
		have[r.ID] = true
	}
	recs = local
	for _, r := range off {
		if !have[r.ID] {
			recs = append(recs, r)
		}
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
	return recs, append(bad, offBad...), nil
}

func same(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// readLayer 讀一層（家裡的 vault 或官方層）的一個種類；資料夾不存在＝沒有紀錄。
func readLayer(dir, kind string) (recs []sdk.Record, bad []string, err error) {
	read := func(path string, archived bool) {
		data, err := os.ReadFile(path)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s：讀不到（%v）", path, err))
			return
		}
		fields, err := parseFrontmatter(string(data))
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s：%v", path, err))
			return
		}
		id, _ := fields["id"].(string)
		if id == "" {
			id = strings.TrimSuffix(filepath.Base(path), ".md")
		}
		recs = append(recs, sdk.Record{Kind: "closet." + kind, ID: id, Fields: fields, Archived: archived})
	}
	top, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, nil, err
	}
	for _, p := range top {
		read(p, false)
	}
	old, _ := filepath.Glob(filepath.Join(dir, "archive", "*", "*.md"))
	for _, p := range old {
		read(p, true)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
	return recs, bad, nil
}

// parseFrontmatter 解 --- 包起來的檔頭（只認上面說的那幾種寫法）。
func parseFrontmatter(text string) (map[string]any, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) < 2 || lines[0] != "---" {
		return nil, fmt.Errorf("缺少檔頭")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("檔頭沒有結束標記")
	}
	head := lines[1:end]
	fields := map[string]any{}
	for i := 0; i < len(head); i++ {
		line := head[i]
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			return nil, fmt.Errorf("第 %d 行的縮排看不懂", i+2)
		}
		key, rest, ok := strings.Cut(line, ":")
		if !ok || key == "" {
			return nil, fmt.Errorf("第 %d 行不是「欄位: 值」", i+2)
		}
		rest = strings.TrimSpace(rest)
		switch {
		case rest == "":
			// 下面接縮排的「- 值」清單（或縮排的表格：不認得，整段略過）。
			var list []any
			nested := false
			for i+1 < len(head) && (strings.HasPrefix(head[i+1], " ") || strings.HasPrefix(head[i+1], "\t")) {
				i++
				item := strings.TrimSpace(head[i])
				if v, ok := strings.CutPrefix(item, "- "); ok && !nested {
					x, err := scalar(v)
					if err != nil {
						return nil, fmt.Errorf("第 %d 行：%v", i+2, err)
					}
					list = append(list, x)
				} else {
					nested = true
				}
			}
			if nested {
				fields[key] = nil // 表格或巢狀清單：這個工具用不到，不猜內容
			} else {
				fields[key] = list
			}
		case rest[0] == '|' || rest[0] == '>':
			for i+1 < len(head) && (strings.HasPrefix(head[i+1], " ") || strings.HasPrefix(head[i+1], "\t") || strings.TrimSpace(head[i+1]) == "") {
				i++
			}
			fields[key] = "" // 多行文字：這個工具用不到
		case rest == "[]":
			fields[key] = []any{}
		case rest[0] == '[':
			if !strings.HasSuffix(rest, "]") {
				return nil, fmt.Errorf("第 %d 行的清單沒有結尾", i+2)
			}
			var list []any
			for _, part := range strings.Split(rest[1:len(rest)-1], ",") {
				x, err := scalar(strings.TrimSpace(part))
				if err != nil {
					return nil, fmt.Errorf("第 %d 行：%v", i+2, err)
				}
				list = append(list, x)
			}
			fields[key] = list
		case rest[0] == '{':
			fields[key] = nil // 單行表格：用不到
		default:
			x, err := scalar(rest)
			if err != nil {
				return nil, fmt.Errorf("第 %d 行：%v", i+2, err)
			}
			fields[key] = x
		}
	}
	return fields, nil
}

// scalar 解一個值：雙引號（跳脫同 JSON）、單引號（” 是一個 '）、true／false、整數、其他當文字。
func scalar(s string) (any, error) {
	switch {
	case strings.HasPrefix(s, `"`):
		var out string
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, fmt.Errorf("雙引號的文字看不懂")
		}
		return out, nil
	case strings.HasPrefix(s, "'"):
		if len(s) < 2 || !strings.HasSuffix(s, "'") {
			return nil, fmt.Errorf("單引號的文字沒有結尾")
		}
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	case s == "true":
		return true, nil
	case s == "false":
		return false, nil
	case s == "null" || s == "~":
		return nil, nil
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, nil
	}
	return s, nil
}
