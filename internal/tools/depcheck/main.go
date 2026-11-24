// depcheck 照路徑檢查 internal/ 的依賴方向（architecture.md「依賴規則」），以及 vault 資料種類的擁有者（kinds.go）。
package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/tools/checkutil"
)

const modulePath = "github.com/minglight/evergenie"

func check(root string, out io.Writer) int {
	order, err := checkutil.BaseOrder(root)
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	// 業務塊表只在真的有 internal/feature/<塊> 時才需要（表壞了就跟著那個資料夾報出來）。
	blocks, blockErr := checkutil.FeatureBlocks(root)
	internal := filepath.Join(root, "internal")
	bad := false
	err = filepath.WalkDir(internal, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(internal, path)
		if err != nil || rel == "." {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if d.IsDir() {
			switch {
			case d.Name() == "testdata" || parts[0] == layerTools:
				return filepath.SkipDir
			case len(parts) == 1 && parts[0] != layerBase && parts[0] != layerFeature:
				fmt.Fprintf(out, "%s: internal/ 底下只能有 base、feature、tools（底層模組放 internal/base/<名>，業務塊放 internal/feature/<塊>/<名>）\n", path)
				bad = true
				return filepath.SkipDir
			case len(parts) == 2 && parts[0] == layerBase && order[parts[1]] == nil:
				fmt.Fprintf(out, "%s: 底層模組 %s 沒有登記在 architecture.md 的「底層模組」表（要寫它可以 import 哪些底層）\n", path, parts[1])
				bad = true
				return filepath.SkipDir
			case len(parts) == 2 && parts[0] == layerFeature && blockErr != nil:
				fmt.Fprintf(out, "%s: 讀不到業務塊的登記：%v\n", path, blockErr)
				bad = true
				return filepath.SkipDir
			case len(parts) == 2 && parts[0] == layerFeature && !blocks[parts[1]]:
				fmt.Fprintf(out, "%s: 業務塊 %s 沒有登記在 architecture.md 的「業務塊」表（新的業務放進現有的塊；真的要新塊先在表上加一列）\n", path, parts[1])
				bad = true
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if checkPathRules(path, parts, order, out) {
			bad = true
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(out, err)
		bad = true
	}
	// 反過來：表上登記的塊都要有資料夾（合併或刪掉塊時表跟著改）。沒有 internal/feature 就不查。
	if info, statErr := os.Stat(filepath.Join(internal, layerFeature)); statErr == nil && info.IsDir() && blockErr == nil {
		var names []string
		for name := range blocks {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if info, err := os.Stat(filepath.Join(internal, layerFeature, name)); err != nil || !info.IsDir() {
				fmt.Fprintf(out, "architecture.md: 「業務塊」表登記了業務塊 %s，但沒有 internal/feature/%s（塊合併或刪掉時把那一列拿掉）\n", name, name)
				bad = true
			}
		}
	}
	if checkKinds(root, out) {
		bad = true
	}
	if bad {
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "用法：depcheck <專案根目錄>")
		os.Exit(2)
	}
	os.Exit(check(os.Args[1], os.Stdout))
}
