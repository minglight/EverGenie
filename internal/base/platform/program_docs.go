package platform

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// 程式資料夾與跟著程式帶的說明文件（PLT-28，DEV-155）。
// 程式資料夾＝執行檔（符號連結先解開）的上上層：下載版是 <app>/versions/<版本>（發行檔解開的那一版），原始碼版是 repo
// （執行檔在 <repo>/bin/）。跟著程式的東西都從這裡找，兩種版本同一套：附的 sdk 原始碼（packages PKG-66）、說明文件（PKG-67）。
// 說明文件放 <程式資料夾>/ai_docs/；帶哪幾份只看清單 ai_docs/bundled.txt（scripts/dist.sh 照它抄進發行檔，`evergenie docs` 只給清單上的）。
const (
	ProgramDocsDir  = "ai_docs"     // 程式資料夾裡放說明文件的資料夾
	ProgramDocsList = "bundled.txt" // 清單：在 ProgramDocsDir 裡，一行一份（相對 ProgramDocsDir 的路徑，`#` 後面是一句話說明）
)

// ProgramDir：exe（符號連結先解開）的上上層；exe 空的或解不開回空字串。
func ProgramDir(exe string) string {
	if exe == "" {
		return ""
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(real))
}

// ProgramDoc 是清單上的一份說明文件。
type ProgramDoc struct {
	Path  string // 相對 ProgramDocsDir 的路徑（斜線分隔，例 conventions/user-features.md）
	About string // 一句話：這份講什麼（清單那一行 `#` 後面的字；`evergenie docs` 列給秘書看）
}

// ParseProgramDocs 解析清單的文字：空行與 `#` 開頭的行略過；其他行是「路徑 [# 說明]」。
// 路徑要是相對、乾淨（沒有 .、..、多的斜線）、.md 結尾、不重複；不合的回錯（第幾行、為什麼）。
func ParseProgramDocs(text string) ([]ProgramDoc, error) {
	var docs []ProgramDoc
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, about, _ := strings.Cut(line, "#")
		p, about = strings.TrimSpace(p), strings.TrimSpace(about)
		switch {
		case strings.ContainsAny(p, " \t\\"):
			return nil, fmt.Errorf("第 %d 行：路徑不能有空白或反斜線（%s）", i+1, p)
		case !strings.HasSuffix(p, ".md"):
			return nil, fmt.Errorf("第 %d 行：只收 .md 檔（%s）", i+1, p)
		case path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../"):
			return nil, fmt.Errorf("第 %d 行：路徑要是 %s 裡面的相對路徑（%s）", i+1, ProgramDocsDir, p)
		case seen[p]:
			return nil, fmt.Errorf("第 %d 行：%s 列了兩次", i+1, p)
		}
		seen[p] = true
		docs = append(docs, ProgramDoc{Path: p, About: about})
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("清單是空的")
	}
	return docs, nil
}

// ReadProgramDocs 讀 <program>/ai_docs/bundled.txt。
func ReadProgramDocs(program string) ([]ProgramDoc, error) {
	data, err := os.ReadFile(filepath.Join(program, ProgramDocsDir, ProgramDocsList))
	if err != nil {
		return nil, err
	}
	return ParseProgramDocs(string(data))
}

// FindProgramDoc 照名字找清單上的一份：接受清單上的寫法（conventions/user-features.md），前面多 ai_docs/、少 .md 也行；
// 只寫檔名（user-features、user-features.md）時，清單上只有一份叫這個名字才算找到。
func FindProgramDoc(docs []ProgramDoc, name string) (ProgramDoc, bool) {
	name = strings.TrimPrefix(strings.TrimSpace(name), ProgramDocsDir+"/")
	if name == "" {
		return ProgramDoc{}, false
	}
	if !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	var hit []ProgramDoc
	for _, d := range docs {
		if d.Path == name {
			return d, true
		}
		if !strings.Contains(name, "/") && path.Base(d.Path) == name {
			hit = append(hit, d)
		}
	}
	if len(hit) == 1 {
		return hit[0], true
	}
	return ProgramDoc{}, false
}
