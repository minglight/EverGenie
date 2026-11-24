package homebackup

import (
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 備份檔的格式（第 1 版）：tar.gz，副檔名 .egbackup。裡面：
//
//	home/home.toml            家的設定
//	home/settings/…           其他設定（通知偏好、選單、推播訂閱、資料整理紀錄…）
//	home/vault/…              筆記與紀錄
//	home/personas/mine/…      主人自己的角色與衣服；官方角色放在家裡的家（[layout] official_personas = "home"，
//	                          官方卡是主人寫的原稿）整個 home/personas/… 都收
//	home/other/<名字>/…       第 2 版佈局的家資料夾裡其他的東西（主人自己放的，例如規矩文件 docs/）
//	home/packages/mine/…      主人自己的套件
//	home/media/…              附件與圖庫（第 1 版佈局的家：runtime/share、runtime/art 收成 media/share、media/art）
//	secrets/push.toml         推播金鑰（只有 [webpush] 那一段）
//	manifest.json             說明檔，寫在最後（每個檔的大小與檢查碼要寫完才知道）
//
// 不收：程式、放在狀態資料夾的官方角色與圖（角色包拿得回來）、別人分享的套件（只在說明檔記名字）、暫存與 log、AI 帳號的登入、符號連結。
const FormatVersion = 1

const manifestName = "manifest.json"

// maxManifestBytes：說明檔最大幾個位元組（一個檔一筆，一百萬個檔也夠）；超過當成壞檔。
const maxManifestBytes = 256 << 20

// 備份檔裡的類別。
const (
	SectionSettings = "settings"
	SectionVault    = "vault"
	SectionPersonas = "personas"
	SectionPackages = "packages"
	SectionMedia    = "media"
	SectionOther    = "other"
	SectionSecrets  = "secrets"
)

// sectionLabels 是類別的白話名稱（照這個順序列）。
var sectionOrder = []string{SectionSettings, SectionVault, SectionPersonas, SectionPackages, SectionMedia, SectionOther, SectionSecrets}

var sectionLabels = map[string]string{
	SectionSettings: "設定",
	SectionVault:    "筆記與紀錄",
	SectionPersonas: "角色與衣服",
	SectionPackages: "自己的套件",
	SectionMedia:    "附件與圖庫",
	SectionOther:    "家資料夾裡的其他東西",
	SectionSecrets:  "推播金鑰",
}

// Manifest 是備份檔的說明檔（manifest.json）。
type Manifest struct {
	Format     int       `json:"format"`
	Product    string    `json:"product"`
	AppVersion string    `json:"app_version"`
	CreatedAt  time.Time `json:"created_at"`
	Kind       string    `json:"kind"`
	HomeName   string    `json:"home_name"`         // 家的資料夾名
	Owner      string    `json:"owner,omitempty"`   // 主人的稱呼（home.toml 的 user.name）
	Machine    string    `json:"machine,omitempty"` // 做備份的電腦（短主機名或 [runtime] machine）
	// HomeDir、UserDir 是做備份那台的家與使用者資料夾的絕對路徑：還原到別台時用來改寫設定裡寫死的路徑。
	HomeDir string `json:"home_dir"`
	UserDir string `json:"user_dir,omitempty"`
	// Migrations 是做備份時這個家已經整理過的資料格式（settings/migrations.toml 的 applied）；比現在的程式認得的多＝備份比程式新。
	Migrations []string `json:"migrations,omitempty"`
	// Installed 是當時裝著的、別人分享的套件（備份不含，還原後要重裝）。
	Installed []string    `json:"installed_packages,omitempty"`
	Sections  []Section   `json:"sections"`
	Files     []FileEntry `json:"files"`
	Skipped   []string    `json:"skipped,omitempty"` // 沒收的（符號連結等），相對備份檔裡的路徑
}

// Section 是一類東西的份量。
type Section struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// FileEntry 是備份檔裡的一個檔。
type FileEntry struct {
	Path   string `json:"path"` // 備份檔裡的路徑（home/vault/…）
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// TotalBytes、TotalFiles：解開後多大、幾個檔。
func (m *Manifest) TotalBytes() (n int64) {
	for _, s := range m.Sections {
		n += s.Bytes
	}
	return n
}

func (m *Manifest) TotalFiles() (n int) {
	for _, s := range m.Sections {
		n += s.Files
	}
	return n
}

func (m *Manifest) sdkSections() []sdk.HomeBackupSection {
	out := make([]sdk.HomeBackupSection, 0, len(m.Sections))
	for _, s := range m.Sections {
		out = append(out, sdk.HomeBackupSection{ID: s.ID, Label: s.Label, Files: int32(s.Files), Bytes: s.Bytes})
	}
	return out
}

// skippedPaths：沒收的東西，寫成家裡的相對路徑（備份檔裡的 home/other/x 就是家裡的 x）。
func (m *Manifest) skippedPaths() []string {
	out := make([]string, 0, len(m.Skipped))
	for _, arc := range m.Skipped {
		p := strings.TrimPrefix(arc, "home/")
		out = append(out, strings.TrimPrefix(p, "other/"))
	}
	return out
}

// sectionOf：備份檔裡的路徑屬於哪一類；不認得回空字串（這種路徑不收）。
func sectionOf(arc string) string {
	switch {
	case arc == "home/home.toml" || hasDirPrefix(arc, "home/settings"):
		return SectionSettings
	case hasDirPrefix(arc, "home/vault"):
		return SectionVault
	case hasDirPrefix(arc, "home/personas"):
		return SectionPersonas
	case hasDirPrefix(arc, "home/packages/mine"):
		return SectionPackages
	case hasDirPrefix(arc, "home/media"):
		return SectionMedia
	case hasDirPrefix(arc, "home/other"):
		return SectionOther
	case arc == "secrets/push.toml":
		return SectionSecrets
	}
	return ""
}

// hasDirPrefix：p 在資料夾 dir 底下（不含 dir 自己）。
func hasDirPrefix(p, dir string) bool {
	return len(p) > len(dir)+1 && p[:len(dir)] == dir && p[len(dir)] == '/'
}
