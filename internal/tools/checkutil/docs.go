// Package checkutil parses the documentation shared by repository checks.
package checkutil

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func Section(text, name string) string {
	lines := strings.Split(text, "\n")
	var result []string
	active := false
	for _, line := range lines {
		if line == "## "+name {
			active = true
			continue
		}
		if active && strings.HasPrefix(line, "## ") {
			break
		}
		if active {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

// Cells respects escaped pipes and pipes inside inline code.
func Cells(line string) []string {
	if !strings.HasPrefix(strings.TrimSpace(line), "|") {
		return nil
	}
	var cells []string
	var cell strings.Builder
	code, escaped := false, false
	for _, r := range strings.TrimSpace(line) {
		if escaped {
			cell.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '`' {
			code = !code
		}
		if r == '|' && !code {
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
		} else {
			cell.WriteRune(r)
		}
	}
	cells = append(cells, strings.TrimSpace(cell.String()))
	if len(cells) > 0 && cells[0] == "" {
		cells = cells[1:]
	}
	if len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return cells
}

// BaseOrder 讀 architecture.md「底層模組」表：| `模組` | 可以 import 的底層（反引號模組名，可以空） | 做什麼 |。
// 回 模組 → 它可以 import 的底層模組。表上沒有的底層模組不准存在（depcheck 擋）。
func BaseOrder(root string) (map[string]map[string]bool, error) {
	data, err := os.ReadFile(filepath.Join(root, "ai_docs", "architecture.md"))
	if err != nil {
		return nil, err
	}
	one := regexp.MustCompile("^`([a-z][a-z-]*)`$")
	names := regexp.MustCompile("`([a-z][a-z-]*)`")
	order := map[string]map[string]bool{}
	for _, line := range strings.Split(Section(string(data), "底層模組"), "\n") {
		c := Cells(line)
		if len(c) < 2 {
			continue
		}
		mod := one.FindStringSubmatch(c[0])
		if mod == nil {
			continue
		}
		if _, dup := order[mod[1]]; dup {
			return nil, fmt.Errorf("architecture.md 底層模組：%s 重複", mod[1])
		}
		order[mod[1]] = map[string]bool{}
		for _, m := range names.FindAllStringSubmatch(c[1], -1) {
			order[mod[1]][m[1]] = true
		}
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("architecture.md 缺少底層模組表")
	}
	return order, nil
}

// FeatureBlocks 讀 architecture.md「業務塊」表：| `塊` | 子套件 | 做什麼 |，一塊一列。
// 回登記了的塊名。表上沒有的 internal/feature/<塊> 不准存在（depcheck 擋，計畫 2026-11-02-module-merge 設計決定 11）。
func FeatureBlocks(root string) (map[string]bool, error) {
	data, err := os.ReadFile(filepath.Join(root, "ai_docs", "architecture.md"))
	if err != nil {
		return nil, err
	}
	one := regexp.MustCompile("^`([a-z][a-z0-9-]*)`$")
	blocks := map[string]bool{}
	for _, line := range strings.Split(Section(string(data), "業務塊"), "\n") {
		c := Cells(line)
		if len(c) < 2 {
			continue
		}
		b := one.FindStringSubmatch(c[0])
		if b == nil {
			continue
		}
		if blocks[b[1]] {
			return nil, fmt.Errorf("architecture.md 業務塊：%s 重複（一塊一列）", b[1])
		}
		blocks[b[1]] = true
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("architecture.md 缺少業務塊表")
	}
	return blocks, nil
}

// KindOwner 是 architecture.md「資料種類的擁有者」表的一列：擁有的模組（可以讀寫），與另外可以讀的模組。
// 模組名是底層模組、業務塊，或塊裡的子套件（`<塊>/<子套件>`）。
type KindOwner struct {
	Owner   string
	Readers map[string]bool
}

// KindOwners 讀「資料種類的擁有者」表：| `種類` | `擁有者` | 也可以讀（反引號模組名，可以空） |。
func KindOwners(root string) (map[string]KindOwner, error) {
	data, err := os.ReadFile(filepath.Join(root, "ai_docs", "architecture.md"))
	if err != nil {
		return nil, err
	}
	one := regexp.MustCompile("^`([a-z][a-z0-9_]*)`$")
	// 擁有者與讀者可以是一塊（`inbox`）或塊裡的一個子套件（`inbox/notify`，只有那個子套件算）。
	who := regexp.MustCompile("^`([a-z][a-z0-9_]*(?:/[a-z][a-z0-9_]*)?)`$")
	names := regexp.MustCompile("`([a-z][a-z0-9_-]*(?:/[a-z][a-z0-9_]*)?)`")
	owners := map[string]KindOwner{}
	for _, line := range strings.Split(Section(string(data), "資料種類的擁有者"), "\n") {
		c := Cells(line)
		if len(c) != 3 {
			continue
		}
		kind, owner := one.FindStringSubmatch(c[0]), who.FindStringSubmatch(c[1])
		if kind == nil || owner == nil {
			continue
		}
		if _, dup := owners[kind[1]]; dup {
			return nil, fmt.Errorf("architecture.md 資料種類的擁有者：%s 重複", kind[1])
		}
		o := KindOwner{Owner: owner[1], Readers: map[string]bool{}}
		for _, m := range names.FindAllStringSubmatch(c[2], -1) {
			o.Readers[m[1]] = true
		}
		owners[kind[1]] = o
	}
	if len(owners) == 0 {
		return nil, fmt.Errorf("architecture.md 缺少資料種類的擁有者表")
	}
	return owners, nil
}
