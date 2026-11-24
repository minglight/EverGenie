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

	"github.com/minglight/evergenie/internal/base/platform"
)

func homeBackupUpload(h http.Handler, body []byte, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, homeBackupUploadPath, bytes.NewReader(body))
	r.Header.Set("X-Requested-With", "evergenie")
	r.RemoteAddr = loopback // 這台 Mac 本機開的看板（上傳只准本機，WEB-190）
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

// 備份檔的上傳（WEB-190）：存到暫存的上傳資料夾（0600、不在公開伺服的位置）、回 name；跨站、沒帶標頭、空檔、太大都擋而且不留檔。
func TestHomeBackupUploadStoresPrivatelyAndRejectsBadRequests(t *testing.T) {
	old := maxHomeBackupUploadBytes
	maxHomeBackupUploadBytes = 1 << 20
	t.Cleanup(func() { maxHomeBackupUploadBytes = old })
	e := newEnv(t, `public_url="https://hg.example.ts.net"`+"\n")
	dir := platform.HomeBackupUploadDir(e.home)

	w := homeBackupUpload(e.h, []byte("backup"))
	var got struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	if !regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{8}\.egbackup$`).MatchString(got.Name) || got.Size != 6 || !platform.IsHomeBackupUploadName(got.Name) {
		t.Fatalf("response: %+v", got)
	}
	if info, err := os.Stat(filepath.Join(dir, got.Name)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stored file: %v %v", info, err)
	}
	if err := os.Remove(filepath.Join(dir, got.Name)); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		w      *httptest.ResponseRecorder
		status int
	}{
		"cross-site":     {homeBackupUpload(e.h, []byte("x"), "Origin", "https://evil.example"), http.StatusForbidden},
		"missing header": {homeBackupUpload(e.h, []byte("x"), "X-Requested-With", ""), http.StatusForbidden},
		"empty":          {homeBackupUpload(e.h, nil), http.StatusBadRequest},
		"too big":        {homeBackupUpload(e.h, make([]byte, 1<<20+1)), http.StatusRequestEntityTooLarge},
	} {
		if er := decodeErr(t, c.w); c.w.Code != c.status || er.Message == "" {
			t.Fatalf("%s: %d %+v", name, c.w.Code, er)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("rejected uploads left %d files", len(entries))
	}
	if w := get(e.h, homeBackupUploadPath); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", w.Code)
	}
}

// 下載這台電腦上的一份備份（WEB-190）：attachment、不快取，下載完檔還在；種類或檔名不合格式（.. 、別的副檔名、不認得的種類）一律 404。
func TestHomeBackupDownloadKeepsFileAndRejectsBadPaths(t *testing.T) {
	e := newEnv(t, "")
	file := filepath.Join(platform.HomeBackupDir(e.home, "manual"), "我的家-20261116-103000.egbackup")
	write(t, file, "archive")
	write(t, filepath.Join(platform.HomeBackupDir(e.home, "manual"), "notes.txt"), "not a backup")
	url := "/home-backup/manual/%E6%88%91%E7%9A%84%E5%AE%B6-20261116-103000.egbackup"
	for i := 0; i < 2; i++ {
		w := get(e.h, url)
		if w.Code != http.StatusOK || w.Body.String() != "archive" {
			t.Fatalf("download %d: %d %q", i, w.Code, w.Body.String())
		}
		if cd := w.Header().Get("Content-Disposition"); cd != "attachment; filename*=UTF-8''%E6%88%91%E7%9A%84%E5%AE%B6-20261116-103000.egbackup" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("headers: %v", w.Header())
		}
	}
	for _, bad := range []string{
		"/home-backup/manual/notes.txt", "/home-backup/other/x.egbackup", "/home-backup/manual/..%2F..%2Fhome.toml",
		"/home-backup/manual/.hidden.egbackup", "/home-backup/manual/", "/home-backup/x.egbackup", "/home-backup/manual/a/b.egbackup",
	} {
		if w := get(e.h, bad); w.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", bad, w.Code)
		}
	}
}

// 看板來的 backup 請求：身分一律是主人，而且不能指定這台電腦上的任意路徑（path 只給指令）。
func TestBoardBackupRequestsCannotNamePaths(t *testing.T) {
	out, serr := forceIdentity("backup", "restore", []byte(`{"Path":"/etc/passwd","upload":"20261116-103000-0a1b2c3d.egbackup","by":{"name":"life"}}`))
	if serr != nil {
		t.Fatal(serr)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if _, has := got["Path"]; has || string(got["by"]) != `{"name":"`+boardActor+`"}` || len(got["upload"]) == 0 {
		t.Fatalf("forwarded body: %s", out)
	}
	for _, method := range []string{"get_status", "create", "preview", "restore"} {
		if !allowedAPI["backup"][method] {
			t.Fatalf("board should offer backup.%s", method)
		}
	}
}

// 從看板上傳備份檔再還原只准這台 Mac 本機開的看板（上傳、預覽、還原三步伺服器端都擋）；挑這台現有的備份還原不限本機，
// 但從公網（Funnel、別的代理）轉進來的一律擋。看狀態誰都可以，local 是伺服器算的。
// Guards: WEB-190
func TestBackupUploadRestoreOnlyFromThisMac(t *testing.T) {
	e := newEnv(t, crossWeb)
	dir := platform.HomeBackupUploadDir(e.home)
	public := func(name string) bool {
		return strings.Contains(name, "Funnel") || strings.Contains(name, "別的代理")
	}
	for name, headers := range notThisMac {
		if strings.Contains(name, "別台看板跨站") || strings.Contains(name, "別的網站") {
			continue // 這兩種由跨站檢查另外處理
		}
		// 上傳：不是本機一律不收、不留檔。
		r := httptest.NewRequest(http.MethodPost, homeBackupUploadPath, bytes.NewReader([]byte("backup")))
		r.Header.Set("X-Requested-With", "evergenie")
		r.RemoteAddr, r.Host = loopback, "127.0.0.1:8850"
		for i := 0; i+1 < len(headers); i += 2 {
			if headers[i] == "Host" {
				r.Host = headers[i+1]
				continue
			}
			r.Header.Add(headers[i], headers[i+1])
		}
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, r)
		if er := decodeErr(t, w); w.Code != http.StatusForbidden || er.Message != uploadThisMacMessage {
			t.Errorf("上傳／%s：要 403「要在那台 Mac 上做」，得到 %d %+v", name, w.Code, er)
		}
		for _, method := range []string{"preview", "restore"} {
			// 指到上傳的檔（key 大小寫不同也算）：不是本機一律擋。
			for _, body := range []string{`{"upload":"20261116-103000-0a1b2c3d.egbackup"}`, `{"Upload":"20261116-103000-0a1b2c3d.egbackup"}`} {
				w := phonePost(e.h, "/api/backup/"+method, body, loopback, headers...)
				want := uploadThisMacMessage
				if public(name) {
					want = viaPublicMessage
				}
				if er := decodeErr(t, w); w.Code != http.StatusForbidden || er.Message != want {
					t.Errorf("%s 上傳的檔／%s：要 403 %q，得到 %d %+v", method, name, want, w.Code, er)
				}
			}
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("被擋的上傳留了 %d 個檔", len(entries))
	}
	if e.api.calls != 0 {
		t.Fatalf("被擋的請求不能交給核心：%d 次", e.api.calls)
	}

	// 挑這台現有的備份：手機經 Tailscale 可以（交給核心）；公網轉進來的不行。
	existing := `{"name":"我的家-20261116-103000.egbackup","kind":"manual"}`
	for name, headers := range notThisMac {
		if strings.Contains(name, "別台看板跨站") || strings.Contains(name, "別的網站") {
			continue
		}
		before := e.api.calls
		w := phonePost(e.h, "/api/backup/restore", existing, loopback, headers...)
		if public(name) {
			if er := decodeErr(t, w); w.Code != http.StatusForbidden || er.Message != viaPublicMessage || e.api.calls != before {
				t.Errorf("現有的備份／%s：公網轉進來的要 403，得到 %d %+v", name, w.Code, er)
			}
			continue
		}
		if w.Code == http.StatusForbidden || e.api.calls != before+1 {
			t.Errorf("現有的備份／%s：不限本機，要交給核心，得到 %d %s", name, w.Code, w.Body.String())
		}
	}

	// 這台 Mac 本機：上傳收、指到上傳檔的還原交給核心。
	if w := homeBackupUpload(e.h, []byte("backup"), "Origin", "http://127.0.0.1"); w.Code != http.StatusOK {
		t.Fatalf("本機上傳：%d %s", w.Code, w.Body.String())
	}
	before := e.api.calls
	if w := phonePost(e.h, "/api/backup/restore", `{"upload":"20261116-103000-0a1b2c3d.egbackup"}`, loopback, "Origin", "http://127.0.0.1:8850"); w.Code == http.StatusForbidden || e.api.calls != before+1 {
		t.Fatalf("本機用上傳的檔還原要交給核心：%d %s", w.Code, w.Body.String())
	}

	// 狀態的 local 由伺服器算：手機帶 local=true（含大小寫不同的 key）不算數，本機是 true。
	status := func(remoteHeaders []string, body string) string {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/backup/get_status", nil)
		r.RemoteAddr, r.Host = loopback, "127.0.0.1:8850"
		for i := 0; i+1 < len(remoteHeaders); i += 2 {
			if remoteHeaders[i] == "Host" {
				r.Host = remoteHeaders[i+1]
				continue
			}
			r.Header.Add(remoteHeaders[i], remoteHeaders[i+1])
		}
		out, ok := gateThisMac(httptest.NewRecorder(), r, "backup", "get_status", []byte(body))
		if !ok {
			t.Fatal("看狀態不該被擋")
		}
		return string(out)
	}
	if got := status(notThisMac["手機或別台經 Tailscale（個人帳號）"], `{"LOCAL":true,"local":true}`); got != `{"local":false}` {
		t.Errorf("手機上的看板不能被當成本機：%s", got)
	}
	if got := status([]string{"Origin", "http://127.0.0.1:8850"}, `{}`); got != `{"local":true}` {
		t.Errorf("本機的看板 local 要是 true：%s", got)
	}
}

// 看板的「備份掃描」（設定 → 電腦 → 備份）讀報告、按「現在掃一次」都走 system.backup_sweep：允許清單要有它，身分一律換成主人
// （2026-11-20 DEV-163 發版檢查：漏了這一項，畫面上那一段永遠是「設定暫時載不進來」）。
// Guards: BSW-6
func TestBoardOffersBackupSweep(t *testing.T) {
	if !allowedAPI["system"]["backup_sweep"] {
		t.Fatal("board should offer system.backup_sweep")
	}
	out, serr := forceIdentity("system", "backup_sweep", []byte(`{"run":true,"by":{"name":"life"}}`))
	if serr != nil {
		t.Fatal(serr)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["by"]) != `{"name":"`+boardActor+`"}` || string(got["run"]) != "true" {
		t.Fatalf("forwarded body: %s", out)
	}
}
