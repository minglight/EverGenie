package platform

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 下載來源的規則（PLT-25）：https 要有主機；http 只收本機；file 要絕對路徑；帳號密碼、看不到的字、大寫的 scheme 都不收。
func TestDownloadURLAllowed(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://example.com/m.json":            true,
		"https://example.com":                   true,
		"https://example.com:8443/a/b.json?x=1": true,
		"http://127.0.0.1:8000/manifest.json":   true,
		"http://localhost/m.json":               true,
		"http://[::1]:9/m.json":                 true,
		"file:///tmp/r/manifest.json":           true,
		"http://example.com/m.json":             false,
		"http://192.0.2.5/m.json":               false,
		"http://127.0.0.2/m.json":               false,
		"http://localhost.example.com/m.json":   false,
		"http://127.0.0.1.nip.io/m.json":        false,
		"https://user:pw@example.com/m.json":    false,
		"https://":                              false,
		"HTTPS://example.com/m.json":            false,
		"ftp://example.com/m.json":              false,
		"file://host/m.json":                    false,
		"/tmp/m.json":                           false,
		"https://例子.com/m.json":                 false,
		"https://example.com/有空白 m.json":        false,
		"":                                      false,
	} {
		if got := DownloadURLAllowed(raw); got != want {
			t.Errorf("DownloadURLAllowed(%q) = %v，要 %v", raw, got, want)
		}
	}
}

// 下載時的轉址（PLT-25）：https 被轉成 http（連本機也一樣）一律失敗；轉到規則以外的網址失敗；https 轉 https 照常。
func TestDownloadClientRefusesRedirectAwayFromHTTPS(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("plain")) }))
	defer plain.Close()
	var tlsURL string
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-http":
			http.Redirect(w, r, plain.URL+"/x", http.StatusFound)
		case "/to-https":
			http.Redirect(w, r, tlsURL+"/ok", http.StatusFound)
		case "/to-outside":
			http.Redirect(w, r, "https://user:pw@example.invalid/x", http.StatusFound)
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer tls.Close()
	tlsURL = tls.URL
	client := DownloadClient(tls.Client(), 0)
	get := func(path string) error {
		resp, err := client.Get(tls.URL + path)
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	if err := get("/to-https"); err != nil {
		t.Fatalf("https 轉到 https 要照常：%v", err)
	}
	for _, path := range []string{"/to-http", "/to-outside"} {
		err := get(path)
		var ue *url.Error
		if err == nil || !errors.As(err, &ue) || !errors.Is(err, ErrUnsafeRedirect) {
			t.Fatalf("%s 要被擋（ErrUnsafeRedirect）：%v", path, err)
		}
	}
	// 本機 http（測試用的來源）轉到本機 http 照常；轉到別台的 http 擋
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/away" {
			http.Redirect(w, r, "http://example.invalid/x", http.StatusFound)
			return
		}
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, "/done", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer local.Close()
	lc := DownloadClient(nil, 0)
	if resp, err := lc.Get(local.URL + "/hop"); err != nil {
		t.Fatalf("本機 http 轉到本機 http 要照常：%v", err)
	} else {
		resp.Body.Close()
	}
	if _, err := lc.Get(local.URL + "/away"); !errors.Is(err, ErrUnsafeRedirect) {
		t.Fatalf("本機 http 轉到別台的 http 要被擋：%v", err)
	}
}

// OpenDownload（PLT-25）：照規則才連；file:// 只收一般檔；回的碼不是 200、https 被轉成 http 各有自己的種類；錯誤裡沒有查詢字串。
func TestOpenDownload(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	fault := func(err error) DownloadFault {
		var de *DownloadError
		if !errors.As(err, &de) {
			t.Fatalf("要回 *DownloadError：%v", err)
		}
		return de.Fault
	}
	rc, n, err := OpenDownload(t.Context(), nil, "file://"+f, 0)
	if err != nil || n != 5 {
		t.Fatalf("file:// 的一般檔要打得開：%d %v", n, err)
	}
	if b, _ := io.ReadAll(rc); string(b) != "hello" {
		t.Fatalf("內容不對：%q", b)
	}
	rc.Close()
	if _, _, err := OpenDownload(t.Context(), nil, "file://"+dir, 0); fault(err) != DownloadUnreachable {
		t.Fatalf("資料夾不是一般檔：%v", err)
	}
	if _, _, err := OpenDownload(t.Context(), nil, "file://"+filepath.Join(dir, "none"), 0); fault(err) != DownloadUnreachable {
		t.Fatalf("沒有這個檔：%v", err)
	}
	for _, raw := range []string{"http://example.com/x", "ftp://example.com/x", "relative/x", "https://u:p@example.com/x"} {
		if _, _, err := OpenDownload(t.Context(), nil, raw, 0); fault(err) != DownloadNotAllowed {
			t.Fatalf("%s 不照規則，要 DownloadNotAllowed：%v", raw, err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	rc, _, err = OpenDownload(t.Context(), srv.Client(), srv.URL+"/x", 0)
	if err != nil {
		t.Fatalf("本機 http 要照常：%v", err)
	}
	rc.Close()
	_, _, err = OpenDownload(t.Context(), srv.Client(), srv.URL+"/missing?token=secret", 0)
	var de *DownloadError
	if !errors.As(err, &de) || de.Fault != DownloadStatus || de.Status != 404 || err.Error() != "HTTP 404" {
		t.Fatalf("404 要是 DownloadStatus：%v", err)
	}
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/x", http.StatusFound)
	}))
	defer tls.Close()
	_, _, err = OpenDownload(t.Context(), tls.Client(), tls.URL+"/to-http?token=secret", 0)
	if fault(err) != DownloadUnsafeRedirect || !errors.Is(err, ErrUnsafeRedirect) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("https 轉成 http 要是 DownloadUnsafeRedirect、錯誤裡沒有查詢字串：%v", err)
	}
	srv.Close()
	if _, _, err := OpenDownload(t.Context(), nil, srv.URL+"/x?token=secret", 0); fault(err) != DownloadUnreachable || strings.Contains(err.Error(), "secret") {
		t.Fatalf("連不上要是 DownloadUnreachable、錯誤裡沒有查詢字串：%v", err)
	}
}
