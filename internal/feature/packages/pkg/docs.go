package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
)

// 跟著程式的說明文件（PKG-67，DEV-155）：秘書替主人做套件、解釋套件怎麼運作時讀的那幾份。
// 找法只有一套（platform PLT-28）：程式資料夾（執行檔解開符號連結的上上層）的 ai_docs/，清單 ai_docs/bundled.txt；
// 下載版是發行檔照清單帶的那幾份，原始碼版是 repo 的 ai_docs/。兩種版本都只給清單上的（原始碼版也不給沒跟著發行的內部文件）。
// 不連 serve、不需要家；只讀。

const docsUsage = `用法：evergenie docs [list] | docs show <文件>
  list            程式資料夾在哪（sdk/、官方套件也在那裡）、跟著程式的說明文件有哪些（每份一句話說明）
  show <文件>     印出一份的全文。<文件> 照 list 的寫法（例：conventions/user-features.md），也可以只寫檔名（例：user-features）
這幾份是做套件、解釋套件怎麼運作時要讀的：資料夾地圖與命名、package.toml 的欄位、名詞。下載版與原始碼版的找法一樣，不要自己猜路徑。
`

// docsExecutable 回現在跑的執行檔（測試換掉）。
var docsExecutable = os.Executable

func docsCommands() map[string]app.Command {
	return map[string]app.Command{
		"docs":      {Usage: "docs list|show <文件>\n跟著程式的說明文件：做套件、解釋套件怎麼運作時讀（資料夾地圖與命名、package.toml 的欄位、名詞）；show 印一份的全文，例：evergenie docs show conventions/user-features.md", Group: cmdline.GroupFeatures, Run: docsList},
		"docs list": {Run: docsList},
		"docs show": {Run: docsShow},
	}
}

// programDocs 找程式資料夾的說明文件：回文件資料夾與清單；找不到回白話的錯。
func programDocs() (string, []platform.ProgramDoc, error) {
	exe, _ := docsExecutable()
	program := platform.ProgramDir(exe)
	if program == "" {
		return "", nil, fmt.Errorf("找不到程式資料夾，讀不到跟著程式的說明文件")
	}
	dir := filepath.Join(program, platform.ProgramDocsDir)
	docs, err := platform.ReadProgramDocs(program)
	if err != nil {
		return dir, nil, fmt.Errorf("讀不到跟著程式的說明文件清單（%s）：程式資料夾可能不完整，跟任何一位秘書說「重新安裝這一版」", filepath.Join(dir, platform.ProgramDocsList))
	}
	return dir, docs, nil
}

func docsList(e *cmdline.Env, args []string) error {
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "help" {
			return &cmdline.HelpText{Text: docsUsage}
		}
		return cmdline.Usagef("docs list 不用參數（看一份的內容用 evergenie docs show <文件>）")
	}
	dir, docs, err := programDocs()
	if err != nil {
		return err
	}
	width := 0
	for _, d := range docs {
		width = max(width, len(d.Path))
	}
	fmt.Fprintf(e.Out, "程式資料夾：%s（sdk/ 是寫附程式套件用的工具組原始碼，packages/official/ 是官方套件，可以當範例）\n", filepath.Dir(dir))
	fmt.Fprintf(e.Out, "跟著程式的說明文件（在 %s；evergenie docs show <文件> 看全文）：\n", dir)
	for _, d := range docs {
		fmt.Fprintf(e.Out, "  %-*s  %s\n", width, d.Path, d.About)
	}
	return nil
}

func docsShow(e *cmdline.Env, args []string) error {
	var names []string
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return &cmdline.HelpText{Text: docsUsage}
		}
		names = append(names, a)
	}
	if len(names) != 1 {
		return cmdline.Usagef("要給一份文件，例：evergenie docs show conventions/user-features.md（evergenie docs list 看有哪些）")
	}
	dir, docs, err := programDocs()
	if err != nil {
		return err
	}
	doc, ok := platform.FindProgramDoc(docs, names[0])
	if !ok {
		return fmt.Errorf("沒有這份說明文件：%s（evergenie docs list 看有哪些）", names[0])
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(doc.Path)))
	if err != nil {
		return fmt.Errorf("清單上有 %s，但讀不到這個檔：程式資料夾可能不完整，跟任何一位秘書說「重新安裝這一版」", doc.Path)
	}
	text := string(data)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	_, err = fmt.Fprint(e.Out, text)
	return err
}
