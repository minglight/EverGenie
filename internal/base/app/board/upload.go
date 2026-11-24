package board

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

const (
	uploadPath     = "/api/web/upload"
	maxUploadBytes = 20 << 20
)

// uploadResponse 是 POST /api/web/upload 的回應（不是 sdk 的方法，不進 SDK 型別）。
type uploadResponse struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int    `json:"size"`
}

// serveUpload 收聊天附件（REQ-82）：body 是檔案原始位元組，檔名在 header X-Filename（encodeURIComponent 過）。
// 必須帶 X-Requested-With: evergenie（逼瀏覽器先 preflight）；跨站檢查跟其他 API 同一套，在進來之前做完（cors.go）。
// 存到家的附件資料夾（第 1 版 <家>/runtime/share/，第 2 版 <家>/media/share/）<時間戳>-<4 碼隨機>-<檔名>，之後由 /share/<名字> 伺服。
func (m *Module) serveUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeStatusError(w, http.StatusMethodNotAllowed, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "請用 POST 上傳"})
		return
	}
	if r.Header.Get("X-Requested-With") != "evergenie" {
		writeStatusError(w, http.StatusForbidden, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "缺少必要的標頭，請從看板上傳"})
		return
	}
	home := m.homeDir()
	if home == "" {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "核心服務尚未啟動"})
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxUploadBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeStatusError(w, http.StatusRequestEntityTooLarge, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "檔案太大（上限 20 MB）"})
			return
		}
		writeStatusError(w, http.StatusBadRequest, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "上傳中斷，請再試一次"})
		return
	}
	if len(data) == 0 {
		writeStatusError(w, http.StatusBadRequest, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "檔案是空的"})
		return
	}
	var salt [2]byte
	if _, err := rand.Read(salt[:]); err != nil {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeInternal, Message: "內部錯誤"})
		return
	}
	name := m.now().Format("20060102-150405") + "-" + hex.EncodeToString(salt[:]) + "-" + uploadBaseName(r.Header.Get("X-Filename"))
	dir := platform.SharesDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "無法建立分享目錄"})
		return
	}
	if err := platform.AtomicWrite(filepath.Join(dir, name), data, 0o600); err != nil {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "無法儲存檔案"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// 寫失敗代表對方已經斷線，沒有別的回應可送。
	_ = json.NewEncoder(w).Encode(uploadResponse{Name: name, URL: "/share/" + name, Size: len(data)})
}

// uploadBaseName 把 X-Filename（encodeURIComponent 過的原檔名）變成可以放進 /share 的檔名部分：
// 去掉資料夾、依 platform.ShareName 的規則清理；連續的 `..` 縮成 `.`（/share 不伺服含 `..` 的名字）；清完是空的就叫 file。
func uploadBaseName(header string) string {
	raw := header
	if dec, err := url.PathUnescape(header); err == nil {
		raw = dec
	}
	raw = raw[strings.LastIndexAny(raw, `/\`)+1:]
	name := platform.ShareName(raw)
	for strings.Contains(name, "..") {
		name = strings.ReplaceAll(name, "..", ".")
	}
	if name == "" {
		return "file"
	}
	return name
}
