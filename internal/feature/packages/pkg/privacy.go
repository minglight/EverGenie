package pkg

import (
	"bytes"
	"fmt"
	"math"
	"path"
	"regexp"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// 匯出前的個資掃描（SEC-302 S5、REQ-336）：只找「看起來像」金鑰／token、家的絕對路徑、email、放密碼的檔名，
// 找到就停下列出（不自動改）；寧可多報讓主人看一眼，也不讓金鑰跟著套件被分享出去。
// 白話說明只露出開頭幾個字，不把整段金鑰寫進回應、log 或對話。

const (
	findSecret     = "secret"
	findSecretFile = "secret_file"
	findHomePath   = "home_path"
	findEmail      = "email"

	scanMaxBytes   = 2 << 20 // 每個檔只掃前 2MB
	entropyMinLen  = 32
	entropyMinBits = 4.3
)

var (
	// 常見服務的金鑰前綴與私鑰檔頭。
	secretPrefixes = []struct {
		re   *regexp.Regexp
		what string
	}{
		{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`), "私鑰"},
		{regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{10,}`), "AI 服務的金鑰（sk-ant-）"},
		{regexp.MustCompile(`\bsk-(?:proj-|live-|test-)?[A-Za-z0-9_-]{20,}`), "API 金鑰（sk-）"},
		{regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`), "GitHub token"},
		{regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`), "GitLab token"},
		{regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`), "Slack token"},
		{regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), "AWS 金鑰"},
		{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`), "Google API 金鑰"},
		{regexp.MustCompile(`\bya29\.[A-Za-z0-9_-]{20,}`), "Google OAuth token"},
		{regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`), "npm token"},
		{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), "JWT token"},
	}
	// key = "值"：名字像密碼的欄位，引號裡有 8 個字以上的值。
	secretAssign = regexp.MustCompile(`(?i)\b[a-z0-9_.-]*(?:api[_-]?key|secret|token|passw(?:or)?d|private[_-]?key|access[_-]?key)[a-z0-9_]*["']?\s*[:=]\s*["']([^"'\s]{8,})["']`)
	highEntropy  = regexp.MustCompile(`[A-Za-z0-9+_=-]{32,}`)
	// 前面是行首、空白、引號、等號、括號或斜線（file:///Users/…）才算；網址中間的 example.com/home/… 不算。
	homeDirRe = regexp.MustCompile("(?:^|[\\s\"'=(:,\\[{`/])((?:/Users|/home)/([A-Za-z0-9._-]+))")
	emailRe   = regexp.MustCompile(`[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.([A-Za-z]{2,}))`)
)

// 一看就是佔位字的值不算。
var placeholderWords = []string{"example", "placeholder", "changeme", "your", "xxxx", "****", "<", "${", "{{", "process.env", "env.", "dummy", "redacted"}

// 不算個人 email 的網域（文件範例、保留網域）與「其實是檔名」的結尾（icon@2x.png）。
var (
	exampleDomains = []string{"example.com", "example.org", "example.net", "users.noreply.github.com"}
	reservedTLDs   = map[string]bool{"test": true, "invalid": true, "local": true, "localhost": true, "example": true}
	fileExtTLDs    = map[string]bool{"png": true, "jpg": true, "jpeg": true, "gif": true, "svg": true, "webp": true, "mp4": true, "webm": true, "js": true, "ts": true, "tsx": true, "css": true, "md": true, "json": true, "toml": true}
)

// 鎖檔、相依清單裡全是雜湊值，不做高亂度檢查（前綴型的金鑰照樣找）。
var hashListFiles = map[string]bool{"go.sum": true, "package-lock.json": true, "pnpm-lock.yaml": true, "yarn.lock": true}

// secretFileName：檔名看起來就是放密碼的（.env、secrets.*、credentials.*、私鑰檔）。
func secretFileName(rel string) (string, bool) {
	base := strings.ToLower(path.Base(rel))
	stem := strings.TrimSuffix(base, path.Ext(base))
	switch {
	case base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env"):
		return "環境變數檔（.env，常放金鑰）", true
	case stem == "secret" || stem == "secrets" || strings.HasPrefix(base, "secrets.") || strings.HasPrefix(base, "secret."):
		return "檔名是 secrets（常放金鑰）", true
	case strings.HasPrefix(base, "credentials") || base == ".netrc" || base == ".npmrc" || base == ".pgpass":
		return "帳號密碼檔", true
	case strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") || strings.HasPrefix(base, "id_ecdsa"):
		return "ssh 私鑰檔", true
	}
	switch path.Ext(base) {
	case ".pem", ".key", ".p12", ".pfx", ".keystore", ".jks":
		return "金鑰或憑證檔", true
	}
	// 資料夾名字是 secrets 的，裡面每個檔都算。
	for _, seg := range strings.Split(strings.ToLower(path.Dir(rel)), "/") {
		if seg == "secrets" || seg == ".secrets" {
			return "放在 secrets 資料夾", true
		}
	}
	return "", false
}

// scanPrivacy 掃一個檔：rel 是相對套件資料夾的路徑（斜線），homes 是要當成「家的路徑」的完整路徑（$HOME、EverGenie 的家）。
// 二進位檔（前 8KB 有 NUL）只看檔名。
func scanPrivacy(rel string, data []byte, homes []string) []sdk.PackageExportFinding {
	var out []sdk.PackageExportFinding
	if what, ok := secretFileName(rel); ok {
		out = append(out, sdk.PackageExportFinding{Path: rel, Line: 0, Kind: findSecretFile, What: what})
	}
	head := data
	if len(head) > 8192 {
		head = head[:8192]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return out
	}
	if len(data) > scanMaxBytes {
		data = data[:scanMaxBytes]
	}
	entropy := !hashListFiles[path.Base(rel)]
	for i, line := range strings.Split(string(data), "\n") {
		n := int32(i + 1)
		add := func(kind, what string) {
			out = append(out, sdk.PackageExportFinding{Path: rel, Line: n, Kind: kind, What: what})
		}
		if what, ok := findSecretInLine(line, entropy); ok {
			add(findSecret, what)
		}
		if what, ok := findHomeInLine(line, homes); ok {
			add(findHomePath, what)
		}
		if what, ok := findEmailInLine(line); ok {
			add(findEmail, what)
		}
	}
	return out
}

func findSecretInLine(line string, entropy bool) (string, bool) {
	for _, p := range secretPrefixes {
		if m := p.re.FindString(line); m != "" {
			return fmt.Sprintf("看起來像%s（%s）", p.what, mask(m)), true
		}
	}
	for _, m := range secretAssign.FindAllStringSubmatch(line, -1) {
		if v := m[1]; !isPlaceholder(v) {
			return fmt.Sprintf("像密碼的欄位寫了值（%s）", mask(v)), true
		}
	}
	if !entropy || strings.Contains(line, "base64,") || strings.Contains(line, "integrity") {
		return "", false
	}
	for _, m := range highEntropy.FindAllString(line, -1) {
		if looksRandom(m) {
			return fmt.Sprintf("一長串看起來像金鑰的亂碼（%s）", mask(m)), true
		}
	}
	return "", false
}

func isPlaceholder(v string) bool {
	low := strings.ToLower(v)
	for _, w := range placeholderWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// looksRandom：32 字以上、大小寫與數字都有、每個字的亂度（Shannon entropy）夠高。十六進位（commit、雜湊）最多 4 bits，不算。
func looksRandom(s string) bool {
	s = strings.TrimRight(s, "=")
	if len(s) < entropyMinLen {
		return false
	}
	var upper, lower, digit bool
	freq := map[rune]int{}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		}
		freq[r]++
	}
	if !upper || !lower || !digit {
		return false
	}
	var h float64
	for _, c := range freq {
		p := float64(c) / float64(len(s))
		h -= p * math.Log2(p)
	}
	return h >= entropyMinBits
}

func findHomeInLine(line string, homes []string) (string, bool) {
	for _, h := range homes {
		if len(h) > 1 && strings.Contains(line, h) {
			return "寫了這台電腦的家路徑 " + h, true
		}
	}
	for _, m := range homeDirRe.FindAllStringSubmatch(line, -1) {
		if m[2] != "Shared" {
			return "寫了家的絕對路徑 " + m[1], true
		}
	}
	return "", false
}

func findEmailInLine(line string) (string, bool) {
	for _, m := range emailRe.FindAllStringSubmatchIndex(line, -1) {
		full, domain, tld := line[m[0]:m[1]], strings.ToLower(line[m[2]:m[3]]), strings.ToLower(line[m[4]:m[5]])
		local := full[:strings.IndexByte(full, '@')]
		if local == "git" || reservedTLDs[tld] || fileExtTLDs[tld] {
			continue
		}
		skip := false
		for _, d := range exampleDomains {
			if domain == d || strings.HasSuffix(domain, "."+d) {
				skip = true
			}
		}
		if skip {
			continue
		}
		return fmt.Sprintf("寫了 email（%s***@%s）", local[:1], domain), true
	}
	return "", false
}

// mask 只露出開頭 4 個字與總長度。
func mask(s string) string {
	r := []rune(s)
	if len(r) <= 4 {
		return "共 " + fmt.Sprint(len(r)) + " 字"
	}
	return fmt.Sprintf("%s…，共 %d 字", string(r[:4]), len(r))
}
