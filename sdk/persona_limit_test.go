package sdk

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// 角色匯出檔的上限只有一個數字（PRS-18、WEB-151）：看板前端擋大檔用的 MAX_PERSONA_UPLOAD_BYTES 要跟這裡的 PersonaImportMaxBytes 一樣。
func TestPersonaUploadLimitMatchesWeb(t *testing.T) {
	data, err := os.ReadFile("../web/packages/sdk-web/src/upload.ts")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("export const MAX_PERSONA_UPLOAD_BYTES = %d;", PersonaImportMaxBytes)
	if !strings.Contains(string(data), want) {
		t.Fatalf("web/packages/sdk-web/src/upload.ts 要有 %q", want)
	}
}

// 備份檔從看板上傳的上限也只有一個數字（DEV-153）：前端的 MAX_HOME_BACKUP_UPLOAD_BYTES 要跟 HomeBackupUploadMaxBytes 一樣。
func TestHomeBackupUploadLimitMatchesWeb(t *testing.T) {
	data, err := os.ReadFile("../web/packages/sdk-web/src/upload.ts")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("export const MAX_HOME_BACKUP_UPLOAD_BYTES = %d;", HomeBackupUploadMaxBytes)
	if !strings.Contains(string(data), want) {
		t.Fatalf("web/packages/sdk-web/src/upload.ts 要有 %q", want)
	}
}
