package charter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

func post(t *testing.T, h http.Handler, path string, body any) (int, sdk.Error) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)))
	var e sdk.Error
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	return rec.Code, e
}

// 經 HTTP：寫入都要帶操作者；秘書存檔 403、送提案可以。
func TestHTTPWritesNeedActorAndOwner(t *testing.T) {
	r := defaultRig(t)
	mux, err := app.New(r.m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/rules/save", "/api/rules/restore", "/api/rules/propose", "/api/rules/decide"} {
		if status, e := post(t, mux, path, map[string]any{"doc": "law"}); status != 400 || e.Field != "by" {
			t.Errorf("%s 沒帶 by：%d %+v", path, status, e)
		}
	}
	cur := r.get(t, "law")
	if status, e := post(t, mux, "/api/rules/save", &sdk.RulesSaveRequest{Doc: "law", Text: "x", Revision: cur.Doc.Revision, By: &sdk.Actor{Name: "code"}}); status != 403 {
		t.Fatalf("秘書存檔：%d %+v", status, e)
	}
	if status, e := post(t, mux, "/api/rules/propose", &sdk.RulesProposeRequest{Doc: "law", Text: "x", Why: "y", By: &sdk.Actor{Name: "code"}}); status != 200 {
		t.Fatalf("秘書提案：%d %+v", status, e)
	}
}
