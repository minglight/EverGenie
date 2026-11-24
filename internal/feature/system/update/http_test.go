package update

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

func postJSON(t *testing.T, h http.Handler, path string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)))
	return rec.Code, rec.Body.Bytes()
}

// /api/system/update_* 五條路由由 update 自己的 Routes() 掛：看現況誰都行（200）；檢查、更新、決定只有主人（秘書 403）；
// 找不到 EverGenie 程式時說白話（503）；沒帶身分是 400。
// Guards: REQ-280, REQ-281
func TestUpdateRoutes(t *testing.T) {
	m, up := newFakeModule(testHome(t), true)
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	if code, raw := postJSON(t, mux, "/api/system/update_status", sdk.UpdateStatusRequest{}); code != 200 {
		t.Fatalf("status：%d %s", code, raw)
	}
	secretary := &sdk.Actor{Name: "code"}
	for path, body := range map[string]any{
		"/api/system/update_check":    sdk.UpdateCheckRequest{By: secretary},
		"/api/system/update_apply":    sdk.UpdateApplyRequest{By: secretary, Version: "0.3.0"},
		"/api/system/update_decide":   sdk.UpdateDecideRequest{By: secretary, Version: "0.3.0", Decision: "skip"},
		"/api/system/update_rollback": sdk.UpdateRollbackRequest{By: secretary},
	} {
		if code, raw := postJSON(t, mux, path, body); code != http.StatusForbidden {
			t.Errorf("%s：秘書要 403：%d %s", path, code, raw)
		}
	}
	if code, _ := postJSON(t, mux, "/api/system/update_check", sdk.UpdateCheckRequest{}); code != http.StatusBadRequest {
		t.Errorf("沒帶身分要 400：%d", code)
	}
	if up.checked != 0 || len(up.applied) != 0 || up.rescued != 0 {
		t.Fatalf("被擋的呼叫不能動手：%+v", up)
	}
	if code, raw := postJSON(t, mux, "/api/system/update_rollback", sdk.UpdateRollbackRequest{By: &sdk.Actor{Name: "owner(web)"}}); code != http.StatusBadRequest {
		t.Errorf("沒有換不回去的升級要 400：%d %s", code, raw)
	}
	if code, raw := postJSON(t, mux, "/api/system/update_check", sdk.UpdateCheckRequest{By: &sdk.Actor{Name: "owner(web)"}}); code != 200 || up.checked != 1 {
		t.Fatalf("主人檢查：%d %s %+v", code, raw, up)
	}

	none, _ := newFakeModule(testHome(t), false)
	mux, err = app.New(none).Mux()
	if err != nil {
		t.Fatal(err)
	}
	if code, raw := postJSON(t, mux, "/api/system/update_check", sdk.UpdateCheckRequest{By: &sdk.Actor{Name: "owner(web)"}}); code != http.StatusServiceUnavailable {
		t.Fatalf("找不到程式要 503：%d %s", code, raw)
	}
}
