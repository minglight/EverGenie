package platform

import (
	"testing"
	"time"
)

// 看板與角色庫共用的暫存名字（WEB-151、PRS-18）：新產的一定合格式；不合格式的（路徑穿越、別的副檔名、少位數）一律不認。
func TestPersonaTransferNames(t *testing.T) {
	tok, err := NewPersonaTransferToken(time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC))
	if err != nil || !IsPersonaTransferToken(tok) || !IsPersonaUploadName(tok+PersonaTransferExt) || tok[:15] != "20261104-120000" {
		t.Fatalf("%q %v", tok, err)
	}
	for _, bad := range []string{"", ".hgpersona", "../home.toml", "20261104-120000-0a1b2c3d", "20261104-120000-0a1b2c3d.zip",
		"20261104-120000-0A1B2C3D.hgpersona", "x/20261104-120000-0a1b2c3d.hgpersona", "20261104-120000-0a1b2c3.hgpersona"} {
		if IsPersonaUploadName(bad) {
			t.Errorf("不該認 %q", bad)
		}
	}
}

// 備份檔名（HBK-1）：新做的用 .egbackup；改名前做的 .hgbackup 照樣認得（列出、下載、清理；這一版不還原，homebackup 的 errLegacy）。看板上傳的暫存檔名是核心自己取的，只認新的。
func TestHomeBackupNamesAcceptLegacyExt(t *testing.T) {
	for _, ok := range []string{"我的家-20261116-103000" + HomeBackupExt, "我的家-20261116-103000" + LegacyHomeBackupExt} {
		if !IsHomeBackupName(ok) || !HasHomeBackupExt(ok) {
			t.Errorf("要認 %q", ok)
		}
	}
	for _, bad := range []string{"", HomeBackupExt, LegacyHomeBackupExt, "." + "a" + LegacyHomeBackupExt, "a.tar.gz", "../a" + LegacyHomeBackupExt, "x/a" + HomeBackupExt} {
		if IsHomeBackupName(bad) {
			t.Errorf("不該認 %q", bad)
		}
	}
	tok, err := NewPersonaTransferToken(time.Date(2026, 11, 16, 10, 30, 0, 0, time.UTC))
	if err != nil || !IsHomeBackupUploadName(tok+HomeBackupExt) || IsHomeBackupUploadName(tok+LegacyHomeBackupExt) {
		t.Fatalf("上傳暫存檔名只認新的副檔名：%q %v", tok, err)
	}
}
