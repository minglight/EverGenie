package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

const spikeDir = "../../testdata/spike"

var fixedNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(spikeDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureField(t *testing.T, rel, field string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readFixture(t, rel), &m); err != nil {
		t.Fatal(err)
	}
	s, _ := m[field].(string)
	return s
}

// fakeHost 記錄呼叫、回預先設好的結果。
type fakeHost struct {
	// during：每次 RequestApproval 進來時叫（第幾次），測試用來模擬核心等了一陣子才斷。
	during    func(n int)
	mu        sync.Mutex
	events    []*sdk.EngineEvent
	approvals []*sdk.EngineHostRequestApprovalRequest
	checks    []*sdk.EngineHostCheckActionRequest
	contexts  []*sdk.EngineHostGetContextRequest

	approval *sdk.EngineHostRequestApprovalResponse
	check    *sdk.EngineHostCheckActionResponse
	context  string
	err      error
	// approvalErrs：RequestApproval 前幾次依序回這些錯（用完才看 err／approval），模擬核心重開中連不上。
	approvalErrs []error
}

func (f *fakeHost) CheckAction(_ context.Context, r *sdk.EngineHostCheckActionRequest) (*sdk.EngineHostCheckActionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, r)
	if f.err != nil {
		return nil, f.err
	}
	if f.check == nil {
		return &sdk.EngineHostCheckActionResponse{Verdict: sdk.ActionVerdictAllow}, nil
	}
	return f.check, nil
}

func (f *fakeHost) ReportEvent(_ context.Context, r *sdk.EngineHostReportEventRequest) (*sdk.EngineHostReportEventResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, r.Event)
	if f.err != nil {
		return nil, f.err
	}
	return &sdk.EngineHostReportEventResponse{}, nil
}

func (f *fakeHost) RequestApproval(_ context.Context, r *sdk.EngineHostRequestApprovalRequest) (*sdk.EngineHostRequestApprovalResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *r
	f.approvals = append(f.approvals, &cp)
	if f.during != nil {
		f.during(len(f.approvals))
	}
	if len(f.approvalErrs) > 0 {
		err := f.approvalErrs[0]
		f.approvalErrs = f.approvalErrs[1:]
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.approval == nil {
		return &sdk.EngineHostRequestApprovalResponse{}, nil
	}
	return f.approval, nil
}

func (f *fakeHost) GetContext(_ context.Context, r *sdk.EngineHostGetContextRequest) (*sdk.EngineHostGetContextResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contexts = append(f.contexts, r)
	if f.err != nil {
		return nil, f.err
	}
	return &sdk.EngineHostGetContextResponse{Text: f.context}, nil
}

type hookEnv struct {
	home string
	host *fakeHost
	// moving：true 時 Sleep 會讓時鐘往前走（測重試的剩下時間與上限）；預設時鐘停在 fixedNow。
	moving bool
	now    time.Time
}

func newHookEnv(t *testing.T) *hookEnv {
	t.Helper()
	return &hookEnv{home: t.TempDir(), host: &fakeHost{}}
}

func (e *hookEnv) deps() HookDeps {
	vars := map[string]string{
		"EVERGENIE_HOME": e.home, "EVERGENIE_API_SOCKET": filepath.Join(e.home, "api.sock"), "EVERGENIE_SESSION": "code",
		"HOME": "/Users/tester", "CLAUDE_CONFIG_DIR": "/Users/tester/.claude-acct",
	}
	if !e.moving {
		return HookDeps{Host: e.host, Getenv: func(k string) string { return vars[k] }, Now: func() time.Time { return fixedNow }, Sleep: func(time.Duration) {}}
	}
	if e.now.IsZero() {
		e.now = fixedNow
	}
	return HookDeps{Host: e.host, Getenv: func(k string) string { return vars[k] }, Now: func() time.Time { return e.now }, Sleep: func(d time.Duration) { e.now = e.now.Add(d) }}
}

// run 執行一個 hook，回傳 stdout 與結束碼。
func (e *hookEnv) run(event string, payload []byte) (string, int) {
	var out bytes.Buffer
	code := RunHook(context.Background(), event, bytes.NewReader(payload), &out, e.deps())
	return out.String(), code
}

func (e *hookEnv) runFixture(t *testing.T, event, rel string) (string, int) {
	t.Helper()
	return e.run(event, readFixture(t, rel))
}

func jsonEqual(t *testing.T, got, want []byte) bool {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("輸出不是 JSON：%q（%v）", got, err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(g, w)
}

var errCoreDown = errors.New("core down")

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
