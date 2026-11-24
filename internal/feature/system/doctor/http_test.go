package doctor

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

// POST /api/system/doctor 由 doctor 自己的 Routes() 掛；照實回檢查結果，錯誤轉成 API 錯誤（不認得的群組是 400、欄位 groups）。
// Guards: REQ-117
func TestDoctorRouteServesSystemDoctor(t *testing.T) {
	m := New(Deps{})
	consume(t, m, stubChecks(scheduleCheck(app.CheckResult{Status: app.StatusOK, Message: "好"})))
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	code, raw := postJSON(t, mux, "/api/system/doctor", sdk.SystemDoctorRequest{Groups: []sdk.DoctorGroup{sdk.DoctorGroupSchedule}})
	var res sdk.SystemDoctorResponse
	if code != 200 || json.Unmarshal(raw, &res) != nil || len(res.Checks) == 0 {
		t.Fatalf("要照實回檢查結果：%d %s", code, raw)
	}
	code, raw = postJSON(t, mux, "/api/system/doctor", sdk.SystemDoctorRequest{Groups: []sdk.DoctorGroup{"nope"}})
	var e sdk.Error
	if code != 400 || json.Unmarshal(raw, &e) != nil || e.Field != "groups" {
		t.Fatalf("不認得的群組要 400 並指出欄位：%d %s", code, raw)
	}
}
