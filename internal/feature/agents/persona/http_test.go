package persona

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

// stubTester 記下轉給 session 的測口吻請求。
type stubTester struct{ got *sdk.PersonasTestRequest }

func (s *stubTester) TestPersona(_ context.Context, req *sdk.PersonasTestRequest) (*sdk.PersonasTestResponse, error) {
	s.got = req
	return &sdk.PersonasTestResponse{Run: "2026-10-26-100000.md", Scenarios: 3}, nil
}
func (s *stubTester) PersonaTestResults(_ context.Context, req *sdk.PersonasTestResultsRequest) (*sdk.PersonasTestResultsResponse, error) {
	return &sdk.PersonasTestResultsResponse{Runs: []sdk.PersonaTestRun{{Run: "2026-10-26-100000.md", Status: "done"}}}, nil
}

// personas.test、test_results 轉給 session 的測口吻；誰都可以叫（不用 by）；session 沒接上回 unavailable。
// Guards: REQ-130
func TestPersonasTestRoutes(t *testing.T) {
	m := New(Deps{})
	svc := &service{m: m}
	ctx := context.Background()
	_, err := svc.Test(ctx, &sdk.PersonasTestRequest{Slug: "athena"})
	var se *sdk.Error
	if !errors.As(err, &se) || se.Code != sdk.ErrorCodeUnavailable {
		t.Fatalf("沒接上 → %v", err)
	}
	ts := &stubTester{}
	m.SetTester(ts)
	got, err := svc.Test(ctx, &sdk.PersonasTestRequest{Slug: "athena", Seat: "code", Only: []string{"report"}, Card: "草稿", Style: "d"})
	if err != nil || got.Run != "2026-10-26-100000.md" || got.Scenarios != 3 {
		t.Fatalf("test → %v %+v", err, got)
	}
	if ts.got == nil || ts.got.Slug != "athena" || ts.got.Seat != "code" || ts.got.Card != "草稿" || ts.got.Style != "d" || len(ts.got.Only) != 1 {
		t.Fatalf("請求要原樣轉給 session：%+v", ts.got)
	}
	res, err := svc.TestResults(ctx, &sdk.PersonasTestResultsRequest{Slug: "athena"})
	if err != nil || len(res.Runs) != 1 || res.Runs[0].Status != "done" {
		t.Fatalf("test_results → %v %+v", err, res)
	}
}

// TestPersonasPackStatus：personas.pack_status 讀登記檔、指標與待補，不連網路；誰都可以看，路由有掛。
// Guards: REQ-283
func TestPersonasPackStatus(t *testing.T) {
	h := homefixture.New(t)
	ptr := filepath.Join(t.TempDir(), "pack.toml")
	if err := os.WriteFile(ptr, []byte("version = \"2026.10.30\"\nurl = \"file:///x/m/2026.10.30.json\"\nsha256 = \""+strings.Repeat("a", 64)+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVERGENIE_PERSONAS_PACK_POINTER", ptr)
	if err := os.MkdirAll(filepath.Join(h.Dir, "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.Dir, "settings", "personas-pack.toml"), []byte("schema = 1\nversion = \"2026.10.29\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.Dir, "runtime", "personas-pack"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.Dir, "runtime", "personas-pack", "pending.json"), []byte(`{"version":"2026.10.30","reason":"抓不到","at":"2026-10-29T10:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(Deps{})
	m.SetHome(h.Dir)
	m.SetPackPointer(func(string) string { return ptr })
	svc := &service{m: m}
	got, err := svc.PackStatus(context.Background(), &sdk.PersonasPackStatusRequest{})
	if err != nil || got.Registered != "2026.10.29" || got.Latest != "2026.10.30" || got.Pending == nil || got.Pending.Reason != "抓不到" || got.Source {
		t.Fatalf("現況：%v %+v", err, got)
	}
	// 路由真的掛得起來：整個 personas 服務由這個業務塊實作（app.Mux 會檢查簽名與重複）。
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/personas/pack_status", strings.NewReader("{}")))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "2026.10.30") {
		t.Fatalf("pack_status → %d %s", rec.Code, rec.Body)
	}
}

// TestPersonasPackSync：personas.pack_sync 請服務同步（PRS-28）：誰都可以叫，結果跟指令 --json 同一份欄位、多一行白話；
// 強制蓋掉（force）、指定跟服務不一樣的指標只有主人；同步沒成功也回結果（status failed），路由有掛。上次結果帶「紀錄還沒寫」與白話給頁面用。
// Guards: PRS-28, PRS-7
func TestPersonasPackSync(t *testing.T) {
	h := homefixture.New(t)
	// 下載版：服務算出 <程式>/current/…，經 ~/.local/bin 的連結叫起來的指令算出 <程式>/versions/<版>/…（current 是連到那一版的連結），是同一個檔。
	appDir := t.TempDir()
	ver := filepath.Join(appDir, "versions", "0.0.1")
	if err := os.MkdirAll(ver, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ver, "pack.toml"), []byte("version = \"\"\nurl = \"\"\nsha256 = \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(ver, filepath.Join(appDir, "current")); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(appDir, "current", "pack.toml")
	viaLink := filepath.Join(ver, "pack.toml")
	m := New(Deps{})
	m.SetHome(h.Dir)
	m.SetPackPointer(func(string) string { return own })
	calls := fakePackSync(t, &personapack.SyncResult{Status: personapack.StatusSynced, Version: "2026.11.14", Installed: []string{"cards/athena.md"}, RecordsAdded: []string{"closet.outfit/athena-base"}})
	svc := &service{m: m}
	ctx := context.Background()
	if _, err := svc.PackSync(ctx, &sdk.PersonasPackSyncRequest{}); err == nil {
		t.Fatal("要有操作者")
	}
	got, err := svc.PackSync(ctx, &sdk.PersonasPackSyncRequest{By: &sdk.Actor{Name: "code"}, Pointer: viaLink})
	if err != nil || got.Status != "synced" || got.Version != "2026.11.14" || len(got.RecordsAdded) != 1 || !strings.Contains(got.Message, "角色包已同步到 2026.11.14") {
		t.Fatalf("秘書請服務同步（指標經符號連結跟服務的是同一個檔）：%v %+v", err, got)
	}
	if (*calls)[0].PointerPath != viaLink || (*calls)[0].Force {
		t.Fatalf("同步參數：%+v", (*calls)[0])
	}
	for _, req := range []*sdk.PersonasPackSyncRequest{
		{By: &sdk.Actor{Name: "code"}, Force: true},
		{By: &sdk.Actor{Name: "code"}, Pointer: filepath.Join(t.TempDir(), "other.toml")},
	} {
		_, err := svc.PackSync(ctx, req)
		var se *sdk.Error
		if !errors.As(err, &se) || se.Code != sdk.ErrorCodeForbidden {
			t.Fatalf("秘書不能 %+v：%v", req, err)
		}
	}
	if len(*calls) != 1 {
		t.Fatalf("被擋下的不能同步：%d", len(*calls))
	}
	if _, err := svc.PackSync(ctx, &sdk.PersonasPackSyncRequest{By: &sdk.Actor{Name: "owner(cli)"}, Force: true, Pointer: filepath.Join(t.TempDir(), "other.toml")}); err != nil || !(*calls)[1].Force {
		t.Fatalf("主人可以強制、指定別的指標：%v", err)
	}
	// 同步沒成功：照樣回結果（failed＋原因），紀錄還沒寫的標出來。
	fakePackSync(t, &personapack.SyncResult{Status: personapack.StatusFailed, RecordsLater: true, Installed: []string{"cards/athena.md"}, Reason: "包裡的 1 筆紀錄這次沒寫：套件服務還沒啟動"})
	got, err = svc.PackSync(ctx, &sdk.PersonasPackSyncRequest{By: &sdk.Actor{Name: "owner(web)"}})
	if err != nil || got.Status != "failed" || !got.RecordsLater || !strings.Contains(got.Message, "還沒同步完") {
		t.Fatalf("沒成功也回結果：%v %+v", err, got)
	}
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/personas/pack_sync", strings.NewReader(`{"by":{"name":"life"}}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"records_later":true`) {
		t.Fatalf("pack_sync → %d %s", rec.Code, rec.Body)
	}
}
