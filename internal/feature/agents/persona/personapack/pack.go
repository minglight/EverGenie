// Package personapack 是角色包（REQ-283、REQ-131、REQ-135）：發行者把公開角色的卡、頭像、公開版衣櫃、目錄卡打成內容定址的包，
// 使用者升級後照官方套件 `personas` 的 pack.toml 指標下載同版號的包。格式見 ai_docs/schemas/persona-pack.md。
// 只用 platform；檔案在家（`<家>/personas/`、`settings/personas-pack.toml`、`runtime/personas-pack/`）或照佈局在家外（DEV-145、DEV-154）。
package personapack

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
)

const (
	// Kind 與 Schema 是 manifest 的固定欄位。
	Kind   = "evergenie.personas"
	Schema = 1
	// maxManifest、MaxFile 擋掉不合理的大小（壞掉或被竄改的包不該把磁碟填滿；角色匯出也用 MaxFile）。
	maxManifest = 8 << 20
	MaxFile     = 200 << 20
)

// Manifest 是 `m/<版號>.json`。
type Manifest struct {
	Kind    string `json:"kind"`
	Schema  int    `json:"schema"`
	Version string `json:"version"`
	Files   []File `json:"files"`
	// Records 是跟著公開角色走的套件紀錄（PKG-59、D-E：例如衣服目錄），照套件宣告收、套 export_omit；舊的包沒有這一欄，照舊能同步。
	Records []Record `json:"records,omitempty"`
}

// File 是包裡的一個檔案：Path 是邏輯路徑（cards/、avatars/、wardrobe/<slug>/、catalog/），SHA256 同時是 `f/<sha256><副檔名>` 的檔名。
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

var (
	slugPattern    = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	versionPattern = regexp.MustCompile(`^(\d{4})\.(\d{2})\.(\d{2})(?:\.(\d+))?$`)
	segPattern     = regexp.MustCompile(`^[^/\\\x00-\x1f][^/\\\x00-\x1f]*$`)
)

// ParseVersion 把 `YYYY.MM.DD[.N]` 變成可以比較的四個數字。
func ParseVersion(s string) ([4]int, bool) {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return [4]int{}, false
	}
	var v [4]int
	for i := 0; i < 4; i++ {
		if m[i+1] != "" {
			v[i], _ = strconv.Atoi(m[i+1])
		}
	}
	return v, true
}

// CompareVersions 回傳 -1、0、1；任何一邊不是合格的版號回 false。
func CompareVersions(a, b string) (int, bool) {
	va, ok1 := ParseVersion(a)
	vb, ok2 := ParseVersion(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	for i := range va {
		switch {
		case va[i] < vb[i]:
			return -1, true
		case va[i] > vb[i]:
			return 1, true
		}
	}
	return 0, true
}

// CleanPath 檢查邏輯路徑乾淨：不是絕對路徑、已經是最簡形式、沒有 `..`、`.` 開頭的段、空段或控制字元（角色包與角色匯出共用，D-63）。
func CleanPath(logical string) bool {
	if logical == "" || path.IsAbs(logical) || path.Clean(logical) != logical {
		return false
	}
	for _, p := range strings.Split(logical, "/") {
		if p == "" || p == "." || p == ".." || strings.HasPrefix(p, ".") || !segPattern.MatchString(p) {
			return false
		}
	}
	return true
}

// Target 把邏輯路徑換成家裡的相對路徑（`personas/…`，實際位置用 HomePath 換）；不是這四種位置、或路徑不乾淨（CleanPath）回 false。
func Target(logical string) (string, bool) {
	if !CleanPath(logical) {
		return "", false
	}
	parts := strings.Split(logical, "/")
	switch parts[0] {
	case "cards":
		if len(parts) == 2 && strings.HasSuffix(parts[1], ".md") && slugPattern.MatchString(strings.TrimSuffix(parts[1], ".md")) {
			return "personas/" + parts[1], true
		}
	case "avatars":
		if len(parts) >= 2 {
			return "personas/" + logical, true
		}
	case "wardrobe":
		if len(parts) >= 3 && slugPattern.MatchString(parts[1]) {
			return "personas/" + logical, true
		}
	case "catalog":
		if len(parts) == 2 {
			return "personas/" + logical, true
		}
	}
	return "", false
}

// IsPersonaMedia：檔名是這位角色的圖或影片（`<代號>`、`<代號>@<衣服>`、`<代號>~N`、`<代號>+k`（第 k 支動畫，DEV-125）開頭，副檔名 png／jpg／jpeg／webp／mp4；命名見 closet.md）。
func IsPersonaMedia(name, slug string) bool {
	if !strings.HasPrefix(name, slug) || strings.HasPrefix(name, ".") {
		return false
	}
	rest := name[len(slug):]
	return rest != "" && strings.ContainsRune("@~.+", rune(rest[0])) && allowedAvatarExt[strings.ToLower(filepath.Ext(name))]
}

// Validate 檢查 manifest 本身：種類、格式版本、版號、每個檔案的路徑與 sha256、不重複，與紀錄本身（ValidateRecords）。
func (m *Manifest) Validate() error {
	if m.Kind != Kind || m.Schema != Schema {
		return fmt.Errorf("這不是 EverGenie 的角色包（種類或格式版本不對）")
	}
	if _, ok := ParseVersion(m.Version); !ok {
		return fmt.Errorf("角色包的版號「%s」格式不對（要像 2026.10.29 或 2026.10.29.2）", m.Version)
	}
	seen := map[string]bool{}
	for _, f := range m.Files {
		if _, ok := Target(f.Path); !ok {
			return fmt.Errorf("角色包裡有不合規定的路徑「%s」", f.Path)
		}
		if !shaPattern.MatchString(f.SHA256) {
			return fmt.Errorf("角色包裡「%s」的 sha256 格式不對", f.Path)
		}
		if f.Size < 0 || f.Size > MaxFile {
			return fmt.Errorf("角色包裡「%s」的大小不合理", f.Path)
		}
		if seen[f.Path] {
			return fmt.Errorf("角色包裡「%s」重複了", f.Path)
		}
		seen[f.Path] = true
	}
	return ValidateRecords(m.Records, "角色包")
}

// SumBytes 回傳內容的 sha256（十六進位）。
func SumBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// SumFile 回傳檔案的 sha256 與大小。
func SumFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// IsSHA256 檢查是不是 64 個小寫十六進位字元。
func IsSHA256(s string) bool { return shaPattern.MatchString(s) }

// SplitPersonas 把家相對的 `personas/…` 路徑（Target 回的那種）分成實際的根與根底下的相對路徑（用 /）：
// `personas/mine/…` 與 `_` 開頭的（`_style.md`、`_test/` 這類主人的東西）根是 <家>/personas；其他（官方角色的卡、avatars、
// wardrobe、catalog…）根是官方角色資料夾——第 1 版也是 <家>/personas，第 2 版在家外面的狀態資料夾（DEV-145）。
// 不是 `personas/` 開頭的：根是家。
func SplitPersonas(home, rel string) (root, rest string) {
	rel = filepath.ToSlash(rel)
	rest, ok := strings.CutPrefix(rel, "personas/")
	if !ok {
		if rel == "personas" {
			return filepath.Join(home, "personas"), ""
		}
		return home, rel
	}
	if first, _, _ := strings.Cut(rest, "/"); first == "mine" || strings.HasPrefix(first, "_") {
		return filepath.Join(home, "personas"), rest
	}
	return platform.OfficialPersonasDir(home), rest
}

// HomePath 是家相對的 `personas/…` 路徑在這個家的實際位置（照 SplitPersonas）。
func HomePath(home, rel string) string {
	root, rest := SplitPersonas(home, rel)
	return filepath.Join(root, filepath.FromSlash(rest))
}

// 登記檔的位置：官方角色放家外的家，登記檔跟官方角色放一起（同步不寫家裡的檔，DEV-154）；其他的家在 settings/。
func registryPath(home string) string {
	if l := platform.LayoutFor(home); l.V2 && !l.PersonasAtHome {
		return l.OfficialPersonas("pack-registry.toml")
	}
	return homeRegistryPath(home)
}
func homeRegistryPath(home string) string {
	return filepath.Join(home, "settings", "personas-pack.toml")
}
func packDir(home string) string     { return platform.RuntimeDir(home, "personas-pack") }
func pendingPath(home string) string { return filepath.Join(packDir(home), "pending.json") }
func lastPath(home string) string    { return filepath.Join(packDir(home), "last.json") }
func cacheDir(home string) string    { return filepath.Join(packDir(home), "cache") }
func stagingDir(home string) string  { return filepath.Join(packDir(home), "staging") }
