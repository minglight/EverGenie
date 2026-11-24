// schemacheck 是發版前的檢查（DEV-154）：官方套件的紀錄格式只能加欄位；真的要改（拿掉欄位、換型別、拿掉選項、變必填）
// 就要在 package.toml 附 [[migration]]，升級時核心替使用者把舊紀錄轉過去。
//
//	schemacheck <上一個正式版的 packages/official> <現在的 packages/official>
//
// 刻意不轉的登記在 <現在的 packages/official>/schema.removed：一行一個「<套件>.<種類>[.<欄位>]<tab>原因」，# 開頭是註解。
// 結束碼 0＝沒有問題；1＝有沒附轉換的格式變更（一行一項）；2＝用法錯。
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/feature/packages/pkg"
	"github.com/minglight/evergenie/sdk"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "用法：schemacheck <上一個正式版的 packages/official> <現在的 packages/official>")
		os.Exit(2)
	}
	os.Exit(check(os.Args[1], os.Args[2], os.Stdout))
}

func check(oldDir, curDir string, out io.Writer) int {
	allowed := map[string]bool{}
	if raw, err := os.ReadFile(filepath.Join(curDir, "schema.removed")); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			key, reason, ok := strings.Cut(line, "\t")
			if key = strings.TrimSpace(key); key == "" || strings.HasPrefix(key, "#") {
				continue
			}
			if !ok || strings.TrimSpace(reason) == "" {
				fmt.Fprintf(out, "schema.removed 的「%s」沒有寫原因（格式：<套件>.<種類>[.<欄位>]<tab>原因）\n", key)
				return 1
			}
			allowed[key] = true
		}
	}
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		fmt.Fprintf(out, "讀不了 %s\n", oldDir)
		return 1
	}
	var breaks []string
	compared := 0
	for _, e := range entries {
		id := e.Name()
		if !e.IsDir() || strings.HasPrefix(id, ".") {
			continue
		}
		old, err := pkg.ParseManifest(filepath.Join(oldDir, id), sdk.TierOfficial)
		if err != nil || old == nil {
			if _, statErr := os.Stat(filepath.Join(oldDir, id, "package.toml")); statErr == nil {
				// 讀不起來不能當作沒事（那等於不檢查）：列出來，確定沒問題再登記例外。
				breaks = append(breaks, id+" 上一版的 package.toml 用現在的規則讀不起來，沒辦法比紀錄格式")
			}
			continue
		}
		if len(old.Contributes.Data) == 0 {
			continue // 上一版沒有自己的資料：沒有東西可以比
		}
		compared++
		cur, err := pkg.ParseManifest(filepath.Join(curDir, id), sdk.TierOfficial)
		if err != nil || cur == nil {
			if _, statErr := os.Stat(filepath.Join(curDir, id)); os.IsNotExist(statErr) {
				for _, d := range old.Contributes.Data {
					breaks = append(breaks, id+"."+d.Kind+" 整個套件不見了（使用者家裡的紀錄會沒人認）")
				}
			} else {
				breaks = append(breaks, id+" 現在的 package.toml 讀不起來")
			}
			continue
		}
		breaks = append(breaks, pkg.SchemaBreaks(old, cur)...)
	}
	sort.Strings(breaks)
	bad := 0
	for _, b := range breaks {
		key, _, _ := strings.Cut(b, " ")
		if allowed[key] {
			continue
		}
		fmt.Fprintln(out, b)
		bad++
	}
	if bad > 0 {
		return 1
	}
	fmt.Fprintf(out, "比了 %d 個有資料的官方套件，紀錄格式都相容（只加欄位，或有附資料轉換）\n", compared)
	return 0
}
