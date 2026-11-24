package pkg

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

// 用 app 的方式把 Routes() 掛起來，從 HTTP 打進真的模組。
func serve(t *testing.T, m *Module) http.Handler {
	t.Helper()
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	return mux
}

func post(t *testing.T, h http.Handler, path string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)))
	return rec.Code, rec.Body.Bytes()
}

// 模組實作 RouteProvider，登記的服務名是 sdk 的 package；20 個方法全部接得上（簽名不符 Mux 會回錯）。
// Guards: REQ-91
func TestRoutesRegisterPackageService(t *testing.T) {
	h := newHarness(t)
	var _ app.RouteProvider = h.m
	got := h.m.Routes()
	if len(got) != 1 || got[0].Service != "package" || got[0].Impl == nil {
		t.Fatalf("Routes = %+v", got)
	}
	serve(t, h.m)
	n := 0
	for _, r := range sdk.Routes {
		if r.Service == "package" {
			n++
		}
	}
	if n != 20 {
		t.Fatalf("package 服務要有 20 個方法，實際 %d", n)
	}
}

// 寫入都要帶操作者（含 export，REQ-334）：沒帶 → 400、欄位 by；唯讀的方法與 sync 預演不用。
// Guards: REQ-91, REQ-334
func TestHTTPWritesRequireActor(t *testing.T) {
	h := newHarness(t)
	h.start()
	srv := serve(t, h.m)
	writes := map[string]bool{"install": true, "uninstall": true, "set_enabled": true, "new": true, "set_extension": true, "install_go": true, "upgrade": true, "export": true, "claim_notify": true}
	for _, r := range sdk.Routes {
		if r.Service != "package" || !writes[r.Method] {
			continue
		}
		code, raw := post(t, srv, r.Path, map[string]any{"id": "notes"})
		var e sdk.Error
		_ = json.Unmarshal(raw, &e)
		if code != http.StatusBadRequest || !strings.Contains(e.Message, "操作者") {
			t.Errorf("%s 沒帶操作者要 400：%d %s", r.Path, code, raw)
		}
	}
	// sync：預演不用操作者，套用要帶。
	if code, raw := post(t, srv, "/api/package/sync", map[string]any{"id": "notes", "apply": true}); code != http.StatusBadRequest || !strings.Contains(string(raw), "操作者") {
		t.Errorf("sync 套用沒帶操作者要 400：%d %s", code, raw)
	}
}

// 整包複製官方套件的入口已關（DEV-154）：帶了操作者也一樣回 403 與白話說明，packages/mine/ 不多東西。
func TestHTTPForkClosed(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierOfficial, "notes", dataPkg("notes", "1.0.0", ""), nil)
	h.start()
	srv := serve(t, h.m)
	code, raw := post(t, srv, "/api/package/fork", map[string]any{"id": "notes", "by": map[string]any{"name": "owner(web)"}})
	if code != http.StatusForbidden || !strings.Contains(string(raw), "不能整包複製") || !strings.Contains(string(raw), "擴充點") {
		t.Fatalf("fork 要 403 並說明改用擴充點：%d %s", code, raw)
	}
	if got := h.get("notes"); got.Tier != sdk.TierOfficial {
		t.Fatalf("官方套件不該被複製成自己的：%+v", got)
	}
}

// 唯讀的方法不用操作者，直接走到真的模組：list 回得出清單、get 找不到回 404。
func TestHTTPReadRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.start()
	srv := serve(t, h.m)
	code, raw := post(t, srv, "/api/package/list", &sdk.PackageListRequest{})
	var res sdk.PackageListResponse
	if code != http.StatusOK || json.Unmarshal(raw, &res) != nil || len(res.Packages) != len(h.list()) {
		t.Fatalf("list：%d %s", code, raw)
	}
	if code, raw := post(t, srv, "/api/package/get", &sdk.PackageGetRequest{ID: "no-such-package"}); code != http.StatusNotFound {
		t.Fatalf("get 不存在的套件要 404：%d %s", code, raw)
	}
}
