package requests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func decodeError(t *testing.T, body []byte) sdk.Error {
	t.Helper()
	var e sdk.Error
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("不是錯誤本體：%s", body)
	}
	return e
}

// 模組實作 RouteProvider，登記的服務名是 sdk 的 policy。
func TestRoutesRegisterPolicyService(t *testing.T) {
	m, _, _, _, _ := fixture(t)
	var _ app.RouteProvider = m
	got := m.Routes()
	if len(got) != 1 || got[0].Service != "policy" || got[0].Impl == nil {
		t.Fatalf("Routes = %+v", got)
	}
	serve(t, m) // 簽名不符 sdk 介面的話 Mux 會回錯
}

// 每個寫入都要帶操作者：沒帶 → 400、欄位 by、模組沒被叫到（沒有申請單）。
func TestHTTPWritesRequireActor(t *testing.T) {
	m, _, _, _, _ := fixture(t)
	h := serve(t, m)
	writes := map[string]func(by *sdk.Actor) any{
		"/api/policy/request": func(by *sdk.Actor) any {
			r := ruleReq("~/Workspace/amy/evergenie")
			r.By = by
			return r
		},
		"/api/policy/decide": func(by *sdk.Actor) any { return &sdk.PolicyDecideRequest{ID: "P-1", Approve: true, By: by} },
		"/api/policy/revoke": func(by *sdk.Actor) any { return &sdk.PolicyRevokeRequest{ID: "P-1", By: by} },
	}
	for path, build := range writes {
		for _, missing := range []*sdk.Actor{nil, {}, {Name: "  "}} {
			status, body := post(t, h, path, build(missing))
			if e := decodeError(t, body); status != 400 || e.Code != sdk.ErrorCodeInvalid || e.Field != "by" {
				t.Errorf("%s 沒帶 by：%d %s", path, status, body)
			}
		}
	}
	if list, err := m.List(ctx, nil, ""); err != nil || len(list) != 0 {
		t.Fatalf("被擋的寫入不能留下申請單：%v %v", list, err)
	}
}

// 秘書送申請、不能自己審或撤銷（403）；主人看過指紋核准、再撤銷；取得與列表都通。
func TestHTTPRoundTripOwnerOnlyDecides(t *testing.T) {
	m, _, _, _, _ := fixture(t)
	h := serve(t, m)
	agent, owner := &sdk.Actor{Name: "code"}, &sdk.Actor{Name: "owner(web)"}
	req := ruleReq("~/Workspace/amy/evergenie")
	req.By = agent
	status, body := post(t, h, "/api/policy/request", req)
	var created sdk.PolicyRequestResponse
	if err := json.Unmarshal(body, &created); err != nil || status != 200 || created.Request.ID != "P-1" || created.Request.Status != sdk.PolicyStatusPending {
		t.Fatalf("request：%d %s", status, body)
	}
	q := created.Request

	status, body = post(t, h, "/api/policy/decide", &sdk.PolicyDecideRequest{ID: q.ID, Approve: true, Fingerprint: q.Fingerprint, By: agent})
	if e := decodeError(t, body); status != 403 || e.Code != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書審了自己的申請：%d %s", status, body)
	}
	status, body = post(t, h, "/api/policy/revoke", &sdk.PolicyRevokeRequest{ID: q.ID, By: agent})
	if e := decodeError(t, body); status != 403 || e.Code != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書撤銷：%d %s", status, body)
	}
	if got, err := m.Get(ctx, q.ID); err != nil || got.Status != sdk.PolicyStatusPending {
		t.Fatalf("申請被改了：%+v %v", got, err)
	}

	var decided sdk.PolicyDecideResponse
	status, body = post(t, h, "/api/policy/decide", &sdk.PolicyDecideRequest{ID: q.ID, Approve: true, Fingerprint: q.Fingerprint, By: owner})
	if err := json.Unmarshal(body, &decided); err != nil || status != 200 || decided.Request.Status != sdk.PolicyStatusApplied {
		t.Fatalf("主人核准：%d %s", status, body)
	}
	var got sdk.PolicyGetResponse
	status, body = post(t, h, "/api/policy/get", &sdk.PolicyGetRequest{ID: q.ID})
	if err := json.Unmarshal(body, &got); err != nil || status != 200 || got.Request.Status != sdk.PolicyStatusApplied {
		t.Fatalf("get：%d %s", status, body)
	}
	var listed sdk.PolicyListResponse
	status, body = post(t, h, "/api/policy/list", &sdk.PolicyListRequest{Statuses: []sdk.PolicyStatus{sdk.PolicyStatusApplied}})
	if err := json.Unmarshal(body, &listed); err != nil || status != 200 || len(listed.Requests) != 1 {
		t.Fatalf("list：%d %s", status, body)
	}
	var revoked sdk.PolicyRevokeResponse
	status, body = post(t, h, "/api/policy/revoke", &sdk.PolicyRevokeRequest{ID: q.ID, By: owner})
	if err := json.Unmarshal(body, &revoked); err != nil || status != 200 || revoked.Request.Status != sdk.PolicyStatusRevoked {
		t.Fatalf("主人撤銷：%d %s", status, body)
	}
}

// 找不到的編號回 404 的白話錯誤。
func TestHTTPGetMissingIs404(t *testing.T) {
	m, _, _, _, _ := fixture(t)
	status, body := post(t, serve(t, m), "/api/policy/get", &sdk.PolicyGetRequest{ID: "P-404"})
	if e := decodeError(t, body); status != 404 || e.Code != sdk.ErrorCodeNotFound || e.Message == "" {
		t.Fatalf("%d %s", status, body)
	}
}
