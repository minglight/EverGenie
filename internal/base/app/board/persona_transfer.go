package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 角色匯出檔的上傳與下載（DEV-116 S3、WEB-151）：檔案可以很大（影片），不走 1 MB 上限的 JSON API，
// 由看板直接讀寫家的 runtime/；真正的匯出、匯入是 sdk 的 personas.export／personas.import（角色庫做，只有主人）。
// 位置與名字格式跟角色庫共用 platform 的 PersonaUploadDir、PersonaExportDir、PersonaTransferToken。

const (
	personaUploadPath   = "/api/web/persona-upload"
	personaExportPrefix = "/persona-export/"
)

// maxPersonaUploadBytes 跟匯入的上限同一個數字（sdk.PersonaImportMaxBytes）；測試會調小。
var maxPersonaUploadBytes = sdk.PersonaImportMaxBytes

var errPersonaTooBig = errors.New("too big")

func tooBigMessage() string {
	return fmt.Sprintf("檔案太大（上限 %s）", humanBytes(maxPersonaUploadBytes))
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30 && n%(1<<30) == 0:
		return fmt.Sprintf("%d GB", n>>30)
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	}
	return fmt.Sprintf("%d bytes", n)
}

// servePersonaUpload 收一個角色匯出檔（body 是原始位元組），串流寫到 <家>/runtime/persona-import/uploads/<時間>-<8 碼>.hgpersona（0600），
// 回 {"name","size"}；之後看板拿 name 呼叫 personas.import。標頭與跨站規則同聊天上傳（WEB-24、WEB-78）。
func (m *Module) servePersonaUpload(w http.ResponseWriter, r *http.Request) {
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
	if r.ContentLength > maxPersonaUploadBytes {
		writeStatusError(w, http.StatusRequestEntityTooLarge, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: tooBigMessage()})
		return
	}
	token, err := platform.NewPersonaTransferToken(m.now())
	if err != nil {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeInternal, Message: "內部錯誤"})
		return
	}
	dir := platform.PersonaUploadDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "無法建立上傳的資料夾"})
		return
	}
	name := token + platform.PersonaTransferExt
	n, err := writeLimited(filepath.Join(dir, name), r.Body, maxPersonaUploadBytes)
	switch {
	case errors.Is(err, errPersonaTooBig):
		writeStatusError(w, http.StatusRequestEntityTooLarge, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: tooBigMessage()})
		return
	case err != nil:
		writeStatusError(w, http.StatusBadRequest, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "上傳中斷，請再試一次"})
		return
	case n == 0:
		_ = os.Remove(filepath.Join(dir, name))
		writeStatusError(w, http.StatusBadRequest, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "檔案是空的"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}{name, n})
}

// writeLimited 把 r 串流寫進 dst（先寫 .part，完整才改名）；超過 limit 回 errPersonaTooBig，失敗都不留檔。
func writeLimited(dst string, r io.Reader, limit int64) (int64, error) {
	part := dst + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, io.LimitReader(r, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > limit {
		err = errPersonaTooBig
	}
	if err == nil {
		err = os.Rename(part, dst)
	}
	if err != nil {
		_ = os.Remove(part)
		return 0, err
	}
	return n, nil
}

// servePersonaExport 送 personas.export 做好的檔：GET /persona-export/<資料夾>/<檔名>（資料夾是 <時間>-<8 碼>）。
// 下載用（attachment、檔名照角色），完整送完一次（不是 Range、不是 HEAD）就刪掉那個資料夾；沒拿的由角色庫一小時後清。
func (m *Module) servePersonaExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "不支援這個方法", http.StatusMethodNotAllowed)
		return
	}
	home := m.homeDir()
	dir, name, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, personaExportPrefix), "/")
	if !ok || home == "" || !platform.IsPersonaTransferToken(dir) || name == "" || strings.ContainsAny(name, "/\\\x00") ||
		strings.HasPrefix(name, ".") || !strings.HasSuffix(name, platform.PersonaTransferExt) {
		http.NotFound(w, r)
		return
	}
	folder := filepath.Join(platform.PersonaExportDir(home), dir)
	file := filepath.Join(folder, name)
	if info, err := os.Lstat(folder); err != nil || !info.IsDir() {
		http.NotFound(w, r)
		return
	}
	sw := &statusWriter{ResponseWriter: w}
	serveRegular(sw, r, file, "application/octet-stream", "attachment", "no-store")
	if r.Method == http.MethodGet && r.Header.Get("Range") == "" && sw.status == http.StatusOK && sw.complete(file) {
		_ = os.RemoveAll(folder)
	}
}

// statusWriter 記下回了什麼狀態、寫了多少位元組（判斷檔案有沒有整個送完）。
type statusWriter struct {
	http.ResponseWriter
	status int
	n      int64
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(p []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(p)
	s.n += int64(n)
	return n, err
}

// ReadFrom 讓 http.ServeContent 照樣用得到底下的 sendfile（大檔不在程式裡多複製一輪）。
func (s *statusWriter) ReadFrom(r io.Reader) (int64, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	var n int64
	var err error
	if rf, ok := s.ResponseWriter.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(r)
	} else {
		n, err = io.Copy(struct{ io.Writer }{s.ResponseWriter}, r)
	}
	s.n += n
	return n, err
}

func (s *statusWriter) complete(file string) bool {
	info, err := os.Lstat(file)
	return err == nil && info.Size() == s.n
}
