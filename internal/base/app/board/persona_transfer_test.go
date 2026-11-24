package board

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func personaUpload(h http.Handler, body []byte, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, personaUploadPath, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("X-Requested-With", "evergenie")
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i+1] == "" {
			r.Header.Del(headers[i])
		} else {
			r.Header.Set(headers[i], headers[i+1])
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// 角色匯出檔的上傳（WEB-151）：串流存到 runtime/persona-import/uploads/<時間>-<8 碼>.hgpersona（0600）、回 name；
// 標頭、跨站、空檔照聊天上傳的規則；超過上限（跟匯入同一個數字，這裡調小）413 而且不留檔。
func TestPersonaUploadStoresPrivatelyAndRejectsOversize(t *testing.T) {
	old := maxPersonaUploadBytes
	maxPersonaUploadBytes = 1 << 20
	t.Cleanup(func() { maxPersonaUploadBytes = old })
	e := newEnv(t, `public_url="https://hg.example.ts.net"`+"\n")
	dir := filepath.Join(e.home, "runtime", "persona-import", "uploads")

	w := personaUpload(e.h, []byte("PK zip"))
	var got struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("上傳：%d %s", w.Code, w.Body.String())
	}
	if !regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{8}\.hgpersona$`).MatchString(got.Name) || got.Size != 6 {
		t.Fatalf("回應：%+v", got)
	}
	info, err := os.Stat(filepath.Join(dir, got.Name))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("存的檔：%v %v", info, err)
	}
	if strings.Contains(w.Body.String(), "/share/") {
		t.Fatal("匯出檔不放 runtime/share（那裡看板會公開伺服）")
	}
	_ = os.Remove(filepath.Join(dir, got.Name))

	cases := []struct {
		name    string
		w       *httptest.ResponseRecorder
		status  int
		message string
	}{
		{"跨站", personaUpload(e.h, []byte("x"), "Origin", "https://evil.example"), http.StatusForbidden, "不接受從別的網站送來的請求"},
		{"沒帶 X-Requested-With", personaUpload(e.h, []byte("x"), "X-Requested-With", ""), http.StatusForbidden, "缺少必要的標頭，請從看板上傳"},
		{"空檔", personaUpload(e.h, nil), http.StatusBadRequest, "檔案是空的"},
		{"超過上限", personaUpload(e.h, make([]byte, 1<<20+1)), http.StatusRequestEntityTooLarge, "檔案太大（上限 1 MB）"},
	}
	for _, c := range cases {
		if er := decodeErr(t, c.w); c.w.Code != c.status || er.Message != c.message {
			t.Fatalf("%s：%d %+v", c.name, c.w.Code, er)
		}
	}
	// Content-Length 先說太大：不讀就回 413。
	r := httptest.NewRequest(http.MethodPost, personaUploadPath, strings.NewReader("x"))
	r.Header.Set("X-Requested-With", "evergenie")
	r.ContentLength = 1<<20 + 1
	w = httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("Content-Length 太大：%d", w.Code)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("被擋的上傳不該留檔案（含 .part），共 %d 個", len(entries))
	}
	if w := personaUpload(e.h, make([]byte, 1<<20)); w.Code != http.StatusOK {
		t.Fatalf("剛好上限：%d", w.Code)
	}
	if w := get(e.h, personaUploadPath); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET：%d", w.Code)
	}
}

// 下載 personas.export 做好的檔（WEB-151）：attachment、檔名照角色；整個送完就刪那個資料夾；HEAD、Range 不刪；
// 路徑不合格式（`..`、`.` 開頭、不是 .hgpersona、資料夾名不對）一律 404。
func TestPersonaExportDownloadOnceThenGone(t *testing.T) {
	e := newEnv(t, "")
	token := "20261104-120000-0a1b2c3d"
	folder := filepath.Join(e.home, "runtime", "persona-export", token)
	write(t, filepath.Join(folder, "露娜.hgpersona"), "zipdata")
	url := "/persona-export/" + token + "/%E9%9C%B2%E5%A8%9C.hgpersona"

	head := httptest.NewRecorder()
	e.h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, url, nil))
	if head.Code != http.StatusOK {
		t.Fatalf("HEAD：%d", head.Code)
	}
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.Header.Set("Range", "bytes=0-2")
	part := httptest.NewRecorder()
	e.h.ServeHTTP(part, r)
	if part.Code != http.StatusPartialContent {
		t.Fatalf("Range：%d", part.Code)
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatal("HEAD 與 Range 不刪")
	}

	w := get(e.h, url)
	if w.Code != http.StatusOK || w.Body.String() != "zipdata" {
		t.Fatalf("下載：%d %q", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); cd != "attachment; filename*=UTF-8''%E9%9C%B2%E5%A8%9C.hgpersona" {
		t.Fatalf("Content-Disposition：%q", cd)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("標頭：%v", w.Header())
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatal("送完要刪掉")
	}
	if w := get(e.h, url); w.Code != http.StatusNotFound {
		t.Fatalf("第二次：%d", w.Code)
	}

	write(t, filepath.Join(folder, "a.hgpersona"), "x")
	write(t, filepath.Join(folder, ".a.hgpersona"), "x")
	write(t, filepath.Join(folder, "a.txt"), "x")
	for _, bad := range []string{
		"/persona-export/" + token + "/.a.hgpersona",
		"/persona-export/" + token + "/a.txt",
		"/persona-export/" + token,
		"/persona-export/x/a.hgpersona",
		"/persona-export/" + token + "/..%2Fa.hgpersona",
	} {
		if w := get(e.h, bad); w.Code != http.StatusNotFound {
			t.Errorf("%s → %d", bad, w.Code)
		}
	}
	if w := post(e.h, "/persona-export/"+token+"/a.hgpersona", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST → %d", w.Code)
	}
}
