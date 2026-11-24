package platform

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"time"
)

// 看板的角色匯出／匯入暫存（DEV-116 S3、WEB-151、PRS-17～18）：看板（存上傳、送下載）與角色庫（做匯出、讀上傳、清理）
// 共用的位置與名字格式，放在底層讓兩邊都 import，不各寫一次。

// PersonaTransferExt 是角色匯出檔的副檔名。
const PersonaTransferExt = ".hgpersona"

// personaTransferToken：<YYYYMMDD-HHMMSS>-<8 碼十六進位>（32 位元亂數，猜不到）。
var personaTransferToken = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{8}$`)

// PersonaUploadDir：看板上傳的匯出檔放這裡（暫存資料夾的 runtime/persona-import/uploads/，不公開伺服）。
func PersonaUploadDir(home string) string {
	return RuntimeDir(home, "persona-import", "uploads")
}

// PersonaExportDir：personas.export 做好的檔放 <這裡>/<token>/<檔名>，看板下載一次就刪。
func PersonaExportDir(home string) string {
	return RuntimeDir(home, "persona-export")
}

// NewPersonaTransferToken 產一個新的暫存名字（上傳檔名去掉副檔名、匯出的資料夾名）。
func NewPersonaTransferToken(now time.Time) (string, error) {
	var salt [4]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	return now.Format("20060102-150405") + "-" + hex.EncodeToString(salt[:]), nil
}

// IsPersonaTransferToken：匯出的資料夾名合不合格式。
func IsPersonaTransferToken(s string) bool { return personaTransferToken.MatchString(s) }

// IsPersonaUploadName：上傳檔名（token＋.hgpersona）合不合格式；不合的一律不碰（擋路徑穿越）。
func IsPersonaUploadName(s string) bool {
	n := len(s) - len(PersonaTransferExt)
	return n > 0 && s[n:] == PersonaTransferExt && IsPersonaTransferToken(s[:n])
}
