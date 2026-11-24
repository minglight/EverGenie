package data

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// recorder 記下假 vault 收到的呼叫（依序），和它該回的錯誤，測試只看轉手的內容。
type recorder struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (r *recorder) rec(format string, args ...any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, fmt.Sprintf(format, args...))
	return r.err
}
func (r *recorder) log() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}
func (r *recorder) has(prefix string) bool {
	for _, c := range r.log() {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

type fakeVault struct{ *recorder }

func (f fakeVault) Get(ctx context.Context, kind, id string) (vault.Record, error) {
	return vault.Record{Kind: kind, ID: id}, f.rec("vault.get %s %s", kind, id)
}
func (f fakeVault) List(ctx context.Context, kind string, q vault.Query) ([]vault.Record, error) {
	return nil, f.rec("vault.list %s", kind)
}
func (f fakeVault) Watch(ctx context.Context, kinds ...string) (<-chan vault.Change, error) {
	ch := make(chan vault.Change)
	close(ch)
	return ch, f.rec("vault.watch")
}
func (f fakeVault) Put(ctx context.Context, r vault.Record, by, expectedRev string) (vault.Record, error) {
	r.Revision = "rev-2"
	return r, f.rec("vault.put %s %s by=%s rev=%s", r.Kind, r.ID, by, expectedRev)
}
func (f fakeVault) Patch(ctx context.Context, kind, id string, set map[string]any, unset []string, by string) (vault.Record, error) {
	return vault.Record{Kind: kind, ID: id}, f.rec("vault.patch %s %s by=%s", kind, id, by)
}
func (f fakeVault) ArchiveNow(ctx context.Context, kind, id, by string) error {
	return f.rec("vault.archive %s %s by=%s", kind, id, by)
}
func (f fakeVault) NextID(ctx context.Context, kind string) (string, error) {
	return "N-7", f.rec("vault.next_id %s", kind)
}
func (f fakeVault) Changes(ctx context.Context, q vault.ChangesQuery) ([]sdk.ChangeEntry, string, error) {
	return nil, "", f.rec("vault.changes %s", q.Kind)
}

// rig 起一個接假 vault 的 data 模組，用 app 的方式把 Routes() 掛起來。
func rig(t *testing.T) (*recorder, http.Handler) {
	t.Helper()
	r := &recorder{}
	mux, err := app.New(New(Deps{Vault: fakeVault{r}})).Mux()
	if err != nil {
		t.Fatal(err)
	}
	return r, mux
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

func decodeError(t *testing.T, raw []byte) sdk.Error {
	t.Helper()
	var e sdk.Error
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("not an error body: %s", raw)
	}
	return e
}

// vault 回的錯誤依種類轉成對應的狀態碼；沒預料到的錯誤只說「內部錯誤」，不外洩原文。
// Guards: REQ-116
func TestErrorsMapToStatusWithoutLeaking(t *testing.T) {
	r, h := rig(t)
	for _, c := range []struct {
		err    error
		status int
		code   sdk.ErrorCode
	}{
		{platform.Invalid("title", "請填寫標題"), 400, sdk.ErrorCodeInvalid},
		{platform.NotFound("找不到這張單"), 404, sdk.ErrorCodeNotFound},
		{platform.Conflict("狀態不允許"), 409, sdk.ErrorCodeConflict},
		{platform.Forbidden("不可以"), 403, sdk.ErrorCodeForbidden},
		{platform.Unavailable("稍後再試"), 503, sdk.ErrorCodeUnavailable},
		{errors.New("panic: nil map at /internal/x.go:12"), 500, sdk.ErrorCodeInternal},
	} {
		r.mu.Lock()
		r.err = c.err
		r.mu.Unlock()
		status, body := post(t, h, "/api/vault/get", &sdk.VaultGetRequest{Kind: "task", ID: "SEC-1"})
		got := decodeError(t, body)
		if status != c.status || got.Code != c.code || got.Message == "" {
			t.Fatalf("%v: status %d body %s", c.err, status, body)
		}
		if strings.Contains(string(body), "/internal/") || strings.Contains(string(body), "panic") {
			t.Fatalf("internal detail leaked: %s", body)
		}
	}
}

// 沒接 vault 時每個請求回 503（不是 panic）。
func TestWithoutVaultIsUnavailable(t *testing.T) {
	mux, err := app.New(New(Deps{})).Mux()
	if err != nil {
		t.Fatal(err)
	}
	if status, body := post(t, mux, "/api/vault/get", &sdk.VaultGetRequest{Kind: "task", ID: "SEC-1"}); status != http.StatusServiceUnavailable {
		t.Fatalf("要 503：%d %s", status, body)
	}
}

// 核心種類的寫入一律擋（請用對應模組的指令），讀可以。
// Guards: REQ-2
func TestVaultWritesForbidden(t *testing.T) {
	r, h := rig(t)
	record := &sdk.Record{Kind: "task", ID: "SEC-1"}
	writes := map[string]any{
		"/api/vault/put":     &sdk.VaultPutRequest{Record: record, By: &sdk.Actor{Name: "code"}},
		"/api/vault/patch":   &sdk.VaultPatchRequest{Kind: "task", ID: "SEC-1", Set: map[string]any{"status": "done"}, By: &sdk.Actor{Name: "code"}},
		"/api/vault/append":  &sdk.VaultAppendRequest{Kind: "journal", ID: "2026-10", Line: "x", By: &sdk.Actor{Name: "code"}},
		"/api/vault/archive": &sdk.VaultArchiveRequest{Kind: "task", ID: "SEC-1", By: &sdk.Actor{Name: "code"}},
		"/api/vault/next_id": &sdk.VaultNextIdRequest{Kind: "task"},
	}
	for path, req := range writes {
		status, body := post(t, h, path, req)
		e := decodeError(t, body)
		if status != 403 || e.Code != sdk.ErrorCodeForbidden || e.Message != "請用對應模組的指令" {
			t.Errorf("%s: %d %s", path, status, body)
		}
	}
	if len(r.log()) != 0 {
		t.Fatalf("a write reached the store: %v", r.log())
	}
	if status, _ := post(t, h, "/api/vault/get", &sdk.VaultGetRequest{Kind: "task", ID: "SEC-1"}); status != 200 {
		t.Fatal(status)
	}
	if status, _ := post(t, h, "/api/vault/list", &sdk.VaultListRequest{Kind: "task"}); status != 200 {
		t.Fatal(status)
	}
	if !r.has("vault.get task SEC-1") || !r.has("vault.list task") {
		t.Fatalf("reads not wired: %v", r.log())
	}
}

// 套件的資料種類（種類名稱含 `.`）可以經 API 寫（put／patch／archive／next_id，要帶操作者）；append 與核心種類照舊擋。
// Guards: REQ-173, REQ-2
func TestVaultPackageKindWrites(t *testing.T) {
	r, h := rig(t)
	owner := &sdk.Actor{Name: "owner(web)"}
	rec := &sdk.Record{Kind: "demo.note", ID: "N-7", Fields: map[string]any{"text": "x"}}
	for path, req := range map[string]any{
		"/api/vault/put":     &sdk.VaultPutRequest{Record: rec, By: owner, ExpectedRevision: "rev-1"},
		"/api/vault/patch":   &sdk.VaultPatchRequest{Kind: "demo.note", ID: "N-7", Set: map[string]any{"text": "y"}, By: owner},
		"/api/vault/archive": &sdk.VaultArchiveRequest{Kind: "demo.note", ID: "N-7", By: owner},
		"/api/vault/next_id": &sdk.VaultNextIdRequest{Kind: "demo.note"},
	} {
		if status, body := post(t, h, path, req); status != 200 {
			t.Errorf("%s: %d %s", path, status, body)
		}
	}
	for _, want := range []string{"vault.put demo.note N-7 by=owner(web) rev=rev-1", "vault.patch demo.note N-7 by=owner(web)", "vault.archive demo.note N-7 by=owner(web)", "vault.next_id demo.note"} {
		if !r.has(want) {
			t.Errorf("沒有送到 vault：%s（%v）", want, r.log())
		}
	}
	// 沒帶操作者的寫入被擋；append 一律擋。
	for path, req := range map[string]any{
		"/api/vault/put":     &sdk.VaultPutRequest{Record: rec},
		"/api/vault/patch":   &sdk.VaultPatchRequest{Kind: "demo.note", ID: "N-7"},
		"/api/vault/archive": &sdk.VaultArchiveRequest{Kind: "demo.note", ID: "N-7"},
	} {
		if status, body := post(t, h, path, req); status != 400 || !strings.Contains(decodeError(t, body).Message, "操作者") {
			t.Errorf("%s 沒帶操作者要 400：%d %s", path, status, body)
		}
	}
	if status, _ := post(t, h, "/api/vault/append", &sdk.VaultAppendRequest{Kind: "demo.note", ID: "N-7", Line: "x", By: owner}); status != 403 {
		t.Errorf("append 一律擋：%d", status)
	}
}

// 夜班清單那一筆 note 可以經 API 整筆寫（P6-6）；別的 note 與核心種類照舊擋，變更紀錄可以讀。
// Guards: REQ-111, REQ-2
func TestVaultNightQueueWrite(t *testing.T) {
	r, h := rig(t)
	owner := &sdk.Actor{Name: "owner(web)"}
	rec := &sdk.Record{Kind: "note", ID: NightQueueID, Body: "x"}
	if status, body := post(t, h, "/api/vault/put", &sdk.VaultPutRequest{Record: rec, By: owner, ExpectedRevision: "rev-1"}); status != 200 {
		t.Fatalf("夜班清單要能寫：%d %s", status, body)
	}
	if !r.has("vault.put note system/night-queue by=owner(web) rev=rev-1") {
		t.Fatalf("沒有送到 vault：%v", r.log())
	}
	before := len(r.log())
	for _, rc := range []*sdk.Record{{Kind: "note", ID: "code/other"}, {Kind: "task", ID: NightQueueID}} {
		if status, body := post(t, h, "/api/vault/put", &sdk.VaultPutRequest{Record: rc, By: owner}); status != 403 {
			t.Errorf("%s/%s 要擋：%d %s", rc.Kind, rc.ID, status, body)
		}
	}
	if status, _ := post(t, h, "/api/vault/archive", &sdk.VaultArchiveRequest{Kind: "note", ID: NightQueueID, By: owner}); status != 403 {
		t.Errorf("夜班清單只開 put，不開封存：%d", status)
	}
	if len(r.log()) != before {
		t.Fatalf("被擋的寫入不能到 vault：%v", r.log()[before:])
	}
	if status, body := post(t, h, "/api/vault/changes", &sdk.VaultChangesRequest{Kind: "task", Limit: 5}); status != 200 || !r.has("vault.changes task") {
		t.Fatalf("changes 要通：%d %s %v", status, body, r.log())
	}
}

// vault.watch：模組的 channel 關了，串流就結束。
// Guards: REQ-116
func TestVaultWatchEndsWhenChannelCloses(t *testing.T) {
	_, h := rig(t)
	if status, body := post(t, h, "/api/vault/watch", &sdk.VaultWatchRequest{}); status != 200 {
		t.Errorf("/api/vault/watch: %d %s", status, body)
	}
}
