package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

type fakeUsage struct {
	rows []sdk.AccountUsage
	err  error
	got  string
}

func (f *fakeUsage) Usages(_ context.Context, account string) ([]sdk.AccountUsage, error) {
	f.got = account
	return f.rows, f.err
}

// 起一個測試家，用 app 的方式把 Routes() 掛起來。
func fixture(t *testing.T, usage Usage) (*Module, *homefixture.Home, http.Handler) {
	t.Helper()
	h := homefixture.New(t)
	m := New(Deps{Usage: usage, Clock: h.Clock})
	m.SetHome(h.Dir)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	return m, h, mux
}

func post(t *testing.T, h http.Handler, path string, req any) (int, []byte) {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)))
	return rec.Code, rec.Body.Bytes()
}

// 看板新增帳號的引擎選單照伺服器給的清單（核心不寫死哪一家），測試用的假引擎不列。
func TestAccountsListsSelectableEngines(t *testing.T) {
	_, _, mux := fixture(t, nil)
	code, body := post(t, mux, "/api/system/accounts", &sdk.SystemAccountsRequest{})
	var out sdk.SystemAccountsResponse
	if code != 200 || json.Unmarshal(body, &out) != nil {
		t.Fatalf("accounts：%d %s", code, body)
	}
	if len(out.Engines) == 0 || slices.Contains(out.Engines, "fake") || !slices.IsSorted(out.Engines) {
		t.Fatalf("引擎清單：%v", out.Engines)
	}
}

// 只有主人能改帳號：沒帶 by 是 invalid、秘書是 forbidden，家的設定不變；主人改了有備份、再列得出來。
func TestSaveAccountOnlyOwner(t *testing.T) {
	_, h, mux := fixture(t, nil)
	before, _, _ := platform.GetHomeValue(h.Dir, "")
	req := &sdk.SystemSaveAccountRequest{Account: sdk.Account{Name: "work", Engine: "test-engine", ConfigDir: "/tmp/acct-work"}, MakeDefault: true}

	var e sdk.Error
	code, body := post(t, mux, "/api/system/save_account", req)
	if err := json.Unmarshal(body, &e); err != nil || code != 400 || e.Code != sdk.ErrorCodeInvalid || e.Field != "by" {
		t.Fatalf("沒帶 by：%d %s", code, body)
	}
	req.By = &sdk.Actor{Name: "code"}
	code, body = post(t, mux, "/api/system/save_account", req)
	if err := json.Unmarshal(body, &e); err != nil || code != 403 || !strings.Contains(e.Message, "只有主人") {
		t.Fatalf("秘書改帳號：%d %s", code, body)
	}
	if after, _, _ := platform.GetHomeValue(h.Dir, ""); after != before {
		t.Fatal("被擋時家的設定不可以變")
	}

	req.By = &sdk.Actor{Name: "owner(web)"}
	var saved sdk.SystemSaveAccountResponse
	code, body = post(t, mux, "/api/system/save_account", req)
	if code != 200 || json.Unmarshal(body, &saved) != nil || saved.Backup == "" {
		t.Fatalf("主人改：%d %s", code, body)
	}
	var list sdk.SystemAccountsResponse
	code, body = post(t, mux, "/api/system/accounts", &sdk.SystemAccountsRequest{})
	if code != 200 || json.Unmarshal(body, &list) != nil {
		t.Fatalf("accounts：%d %s", code, body)
	}
	var found bool
	for _, a := range list.Accounts {
		if a.Name == "work" && a.Default {
			found = true
		}
	}
	if !found {
		t.Fatalf("新帳號應列出且是預設：%+v", list.Accounts)
	}
}

// 用量：帳號名去掉空白後轉給引擎；沒接上引擎回 unavailable；引擎的錯誤對外變成白話錯誤。
func TestUsageForwardsToEngine(t *testing.T) {
	f := &fakeUsage{rows: []sdk.AccountUsage{{Account: "main", UsedRatio: 0.5, MeasuredAt: time.Now()}}}
	_, _, mux := fixture(t, f)
	var out sdk.SystemUsageResponse
	code, body := post(t, mux, "/api/system/usage", &sdk.SystemUsageRequest{Account: " main "})
	if code != 200 || json.Unmarshal(body, &out) != nil || len(out.Accounts) != 1 || f.got != "main" {
		t.Fatalf("usage：%d %s（轉給引擎的帳號 %q）", code, body, f.got)
	}

	f.err = platform.NotFound("找不到帳號 nobody")
	if code, _ := post(t, mux, "/api/system/usage", &sdk.SystemUsageRequest{Account: "nobody"}); code != 404 {
		t.Fatalf("引擎說找不到：%d", code)
	}
	f.err = errors.New("內部細節")
	if code, body := post(t, mux, "/api/system/usage", &sdk.SystemUsageRequest{}); code != 500 || strings.Contains(string(body), "內部細節") {
		t.Fatalf("意外的錯誤不能外洩原文：%d %s", code, body)
	}

	_, _, bare := fixture(t, nil)
	if code, _ := post(t, bare, "/api/system/usage", &sdk.SystemUsageRequest{}); code != 503 {
		t.Fatalf("沒接上引擎：%d", code)
	}
}
