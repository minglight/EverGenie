package board

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 整個家的備份檔的上傳與下載（DEV-153、WEB-190）：檔案可以很大，不走 1 MB 上限的 JSON API，由看板直接讀寫；
// 做備份、預覽、還原是 sdk 的 backup 服務（system 塊的 homebackup）。位置與名字格式共用 platform 的 HomeBackup*。

const (
	homeBackupUploadPath = "/api/web/home-backup-upload"
	homeBackupPrefix     = "/home-backup/"
)

// maxHomeBackupUploadBytes 是上傳一個備份檔的上限（sdk.HomeBackupUploadMaxBytes）；測試會調小。
var maxHomeBackupUploadBytes = sdk.HomeBackupUploadMaxBytes

func homeBackupTooBig() *sdk.Error {
	return &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "備份檔太大（上限 " + humanBytes(maxHomeBackupUploadBytes) + "），沒辦法從看板上傳；跟任何一位秘書說「用這台電腦上的備份檔還原」，告訴她檔案放在哪裡"}
}

// serveHomeBackupUpload 收一個備份檔（body 是原始位元組），串流寫到暫存的上傳資料夾（<時間>-<8 碼>.egbackup，0600），
// 回 {"name","size"}；之後看板拿 name 呼叫 backup.preview、backup.restore。標頭與跨站規則同其他上傳（WEB-24、WEB-78）。
func (m *Module) serveHomeBackupUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeStatusError(w, http.StatusMethodNotAllowed, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "請用 POST 上傳"})
		return
	}
	if r.Header.Get("X-Requested-With") != "evergenie" {
		writeStatusError(w, http.StatusForbidden, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "缺少必要的標頭，請從看板上傳"})
		return
	}
	// 上傳備份檔只准這台 Mac 本機開的看板（WEB-190，thismac.go）：上傳的檔可以是任何人做的家，從別處連進來的不收。
	if !onThisMac(r) {
		writeStatusError(w, http.StatusForbidden, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: uploadThisMacMessage})
		return
	}
	home := m.homeDir()
	if home == "" {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "核心服務尚未啟動"})
		return
	}
	if r.ContentLength > maxHomeBackupUploadBytes {
		writeStatusError(w, http.StatusRequestEntityTooLarge, homeBackupTooBig())
		return
	}
	token, err := platform.NewPersonaTransferToken(m.now())
	if err != nil {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeInternal, Message: "內部錯誤"})
		return
	}
	dir := platform.HomeBackupUploadDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "無法建立上傳的資料夾"})
		return
	}
	name := token + platform.HomeBackupExt
	n, err := writeLimited(filepath.Join(dir, name), r.Body, maxHomeBackupUploadBytes)
	switch {
	case errors.Is(err, errPersonaTooBig):
		writeStatusError(w, http.StatusRequestEntityTooLarge, homeBackupTooBig())
		return
	case err != nil:
		writeStatusError(w, http.StatusBadRequest, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "上傳中斷，請再試一次"})
		return
	case n == 0:
		_ = os.Remove(filepath.Join(dir, name)) // 空檔不留
		writeStatusError(w, http.StatusBadRequest, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "檔案是空的"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct { // 回應寫不出去（對方斷線）沒有可以補救的
		Name string `json:"name"`
		Size int64  `json:"size"`
	}{name, n})
}

// serveHomeBackup 送這台電腦上的一份備份：GET /home-backup/<種類>/<檔名>（下載用，attachment）。備份留著，不因為下載過就刪。
func (m *Module) serveHomeBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "不支援這個方法", http.StatusMethodNotAllowed)
		return
	}
	home := m.homeDir()
	kind, name, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, homeBackupPrefix), "/")
	if !ok || home == "" || !platform.IsHomeBackupKind(kind) || !platform.IsHomeBackupName(name) {
		http.NotFound(w, r)
		return
	}
	serveRegular(w, r, filepath.Join(platform.HomeBackupDir(home, kind), name), "application/octet-stream", "attachment", "no-store")
}
