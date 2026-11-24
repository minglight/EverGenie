package backupsweep

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// 「在用」的檢查（備份規則的「清理」，system 卡 BSW-4）：開機服務的設定、正在跑的程式、終端機 session 有沒有指到這個路徑或它的上層資料夾。
// 有指到就是在用，報告標「在用」、不列可刪；有一種沒查成就標「沒查完」，也不列可刪。

// Ref 是一筆拿來比對的文字：Label 是給人看的出處（設定檔名、那一行指令），Text 是要找路徑的內容。
type Ref struct {
	Label string
	Text  string
}

// checkTimeout：一種檢查最多跑這麼久。
const checkTimeout = 30 * time.Second

// runCheck 照 Check 讀檔或跑指令，回要比對的文字。ctx 取消（服務收工）就停。
func runCheck(ctx context.Context, c Check) ([]Ref, error) {
	if c.Files != "" {
		matches, err := filepath.Glob(c.Files)
		if err != nil {
			return nil, err
		}
		var out []Ref
		for _, f := range matches {
			data, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			if bytes.HasPrefix(data, []byte("bplist")) {
				// 二進位的 plist：字串前面是長度位元組，直接找路徑會斷錯邊界，先轉成 XML 再比對。
				if data, err = plistXML(ctx, f); err != nil {
					return nil, err
				}
			}
			out = append(out, Ref{Label: filepath.Base(f), Text: string(data)})
		}
		return out, nil
	}
	if len(c.Command) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Command[0], c.Command[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, &checkError{msg: msg}
		}
		return nil, err
	}
	var out []Ref
	for _, line := range strings.Split(stdout.String(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, Ref{Label: shorten(line, 120), Text: line})
		}
	}
	return out, nil
}

// plistBin 是 macOS 內建的 plist 轉換工具（測試可以換）。
var plistBin = "/usr/bin/plutil"

// plistXML 把二進位 plist 轉成 XML（plutil，只讀、輸出到 stdout）；轉不了就是這種檢查沒做成（不猜）。
func plistXML(ctx context.Context, f string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, plistBin, "-convert", "xml1", "-o", "-", f)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, &checkError{msg: "讀不懂 " + filepath.Base(f) + "（二進位 plist 轉不了）"}
	}
	return stdout.Bytes(), nil
}

type checkError struct{ msg string }

func (e *checkError) Error() string { return e.msg }

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// checkResult 是一種檢查跑完的結果。
type checkResult struct {
	name string
	refs []Ref
	err  error
}

// pathChar：路徑名字裡會出現、接在後面就代表是另一個名字的字元。
func pathChar(c byte) bool {
	return c == '.' || c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// mentions：text 裡有寫到 target 這個路徑（或它底下的東西）。前後要斷在路徑的邊界：/a/bc 不算寫到 /a/b。
func mentions(text, target string) bool {
	if target == "" || target == "/" {
		return false
	}
	for i := 0; ; {
		j := strings.Index(text[i:], target)
		if j < 0 {
			return false
		}
		at := i + j
		end := at + len(target)
		before := at == 0 || !pathChar(text[at-1])
		after := end == len(text) || !pathChar(text[end])
		if before && after {
			return true
		}
		i = at + 1
	}
}

// targets 是一個路徑在設定裡可能的寫法：絕對路徑，在使用者家目錄底下的再加 ~/… 的寫法。
func targets(p, userHome string) []string {
	out := []string{p}
	if rel, err := filepath.Rel(userHome, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		out = append(out, "~/"+filepath.ToSlash(rel))
	}
	return out
}

// usedBy 看這個備份有沒有被指到：備份自己、或它的上層資料夾。回「哪一種：出處（指到哪裡）」，沒有是空字串。
func usedBy(p, userHome string, results []checkResult) string {
	kinds := []struct {
		what    string
		targets []string
	}{{"這個路徑", targets(p, userHome)}, {"上層資料夾", targets(filepath.Dir(p), userHome)}}
	for _, r := range results {
		if r.err != nil {
			continue
		}
		for _, ref := range r.refs {
			for _, k := range kinds {
				for _, t := range k.targets {
					if mentions(ref.Text, t) {
						return r.name + "：" + ref.Label + "（指到" + k.what + "）"
					}
				}
			}
		}
	}
	return ""
}
