package platform

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/minglight/evergenie/sdk"
)

// 整個家的備份檔（DEV-153）：看板（存上傳、送下載）與 system 塊的 homebackup（做備份、還原、清理）共用的位置與名字格式，
// 放在底層讓兩邊都 import，不各寫一次（同 persona_transfer.go）。

// HomeBackupExt 是備份檔的副檔名（本質是 tar.gz）：新做的備份一律用它。
const HomeBackupExt = ".egbackup"

// LegacyHomeBackupExt 是改名（DEV-158）前的副檔名：舊電腦做的、這台以前做的備份照樣認得（列出、下載、清理），新做的不用它。
// 這一版不還原這種備份（裡面是改名前的格式），還原與預覽時回一句白話（homebackup 的 errLegacy）。
const LegacyHomeBackupExt = ".hgbackup"

// homeBackupExtOf 回 s 結尾的備份副檔名（新的或改名前的），都不是回空字串。
func homeBackupExtOf(s string) string {
	for _, ext := range []string{HomeBackupExt, LegacyHomeBackupExt} {
		if strings.HasSuffix(s, ext) {
			return ext
		}
	}
	return ""
}

// HasHomeBackupExt：s 是不是以備份副檔名（新的或改名前的）結尾。
func HasHomeBackupExt(s string) bool { return homeBackupExtOf(s) != "" }

// HomeBackupKinds 是備份的種類（也是資料夾名），新的種類加在這裡。
var HomeBackupKinds = []string{sdk.HomeBackupKindDaily, sdk.HomeBackupKindManual, sdk.HomeBackupKindBeforeRestore}

// IsHomeBackupKind：是不是認得的種類。
func IsHomeBackupKind(s string) bool {
	for _, k := range HomeBackupKinds {
		if s == k {
			return true
		}
	}
	return false
}

// HomeBackupDir 是這台電腦放某一種備份的資料夾。備份不是暫存：第 2 版放狀態資料夾的 backups/<種類>/（跟 runtime/、secrets/ 並排）；
// 第 1 版的家沒有狀態資料夾，放 <家>/runtime/home-backups/<種類>/（家是 git repo 的話 runtime/ 本來就不進 git）。
func HomeBackupDir(home, kind string) string {
	l := LayoutFor(home)
	if l.V2 {
		return filepath.Join(l.State, "backups", kind)
	}
	return l.Runtime("home-backups", kind)
}

// HomeBackupUploadDir：看板上傳的備份檔放這裡（暫存，不公開伺服；一天後清）。
func HomeBackupUploadDir(home string) string { return RuntimeDir(home, "home-backup", "uploads") }

// HomeBackupStatePath：以前每天自動備份的紀錄（2026-11-21 起不寫了；留著的檔沒人讀）。
func HomeBackupStatePath(home string) string { return RuntimeDir(home, "home-backup", "state.json") }

// ServiceLockName 是「這個家的服務正在跑」的鎖檔（在家的 runtime/ 底下）：vault 開著的整段時間拿著它（flock），
// 同一個家開不了第二個服務；還原要換上去之前也先拿它，確認沒有活著的服務（HBK-12）。
const ServiceLockName = "vault.lock"

// ServiceLockPath 是這個家的 ServiceLockName 的完整位置。
func ServiceLockPath(home string) string { return RuntimeDir(home, ServiceLockName) }

// HomeRestoreResultPath：上一次還原的結果。
func HomeRestoreResultPath(home string) string {
	return RuntimeDir(home, "home-backup", "last-restore.json")
}

// IsHomeBackupName：備份檔名合不合格式（一段檔名、不是隱藏檔、副檔名是新的或改名前的）；不合的一律不碰（擋路徑穿越）。
func IsHomeBackupName(s string) bool {
	ext := homeBackupExtOf(s)
	if len(s) > 200 || ext == "" || len(s) == len(ext) || strings.HasPrefix(s, ".") {
		return false
	}
	return !strings.ContainsAny(s, "/\\\x00") && s != ".." && filepath.Base(s) == s
}

// IsHomeBackupUploadName：看板上傳的暫存檔名（<時間>-<8 碼>.egbackup；暫存檔名是核心自己取的，只會是新的副檔名）。
func IsHomeBackupUploadName(s string) bool {
	n := len(s) - len(HomeBackupExt)
	return n > 0 && s[n:] == HomeBackupExt && IsPersonaTransferToken(s[:n])
}

// HomeBackupStem 把家名整理成能放進檔名的樣子：文字、數字、- _ 留著，其他換成 -；空的用 home。
func HomeBackupStem(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "home"
	}
	if len(out) > 80 {
		out = strings.ToValidUTF8(out[:80], "")
	}
	return out
}
