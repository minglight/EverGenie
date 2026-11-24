package sdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type testHost struct {
	EngineHostAPI
	fail error
}

func (h *testHost) GetContext(ctx context.Context, req *EngineHostGetContextRequest) (*EngineHostGetContextResponse, error) {
	if h.fail != nil {
		return nil, h.fail
	}
	return &EngineHostGetContextResponse{}, nil
}

type testBus struct {
	BusAPI
	watch func(context.Context, *BusWatchRequest, func(*BusWatchResponse) error) error
}

func (b *testBus) Watch(ctx context.Context, r *BusWatchRequest, send func(*BusWatchResponse) error) error {
	return b.watch(ctx, r, send)
}

type wrongHost struct{}

func (*wrongHost) ReportEvent(context.Context, *EngineHostReportEventRequest) error { return nil }
func shortSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return filepath.Join(dir, "api.sock")
}
func startServer(t *testing.T, mounts map[string]any) (*Client, string, func()) {
	t.Helper()
	socket := shortSocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, socket, mounts) }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	}
	t.Cleanup(stop)
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		conn, err := net.Dial("unix", socket)
		if err == nil {
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			break
		}
		select {
		case err := <-done:
			stopped = true
			cancel()
			t.Fatalf("server exited: %v", err)
		case <-timeout.C:
			t.Fatal("server not ready")
		default:
			runtime.Gosched()
		}
	}
	return Dial(socket), socket, stop
}

// Guards: REQ-116, REQ-144
func TestMountRejectsWrongSignature(t *testing.T) {
	for _, impl := range []any{nil, (*testHost)(nil), &wrongHost{}, struct{}{}} {
		err := Mount(http.NewServeMux(), "engine_host", impl)
		if err == nil || !strings.Contains(err.Error(), "engine_host.report_event 期望的簽名") {
			t.Fatalf("wrong signature accepted or unclear: %v", err)
		}
	}
}

// Guards: REQ-116, REQ-144
func TestInvokeRoundTripOverUnixSocket(t *testing.T) {
	c, _, _ := startServer(t, map[string]any{"engine_host": &testHost{}})
	out, err := NewEngineHostClient(c).GetContext(context.Background(), &EngineHostGetContextRequest{})
	if err != nil || out == nil {
		t.Fatalf("round trip: %v %v", out, err)
	}
	// Invalid JSON must be rejected before dispatch, including trailing objects.
	for _, body := range []string{"{", "{} {}"} {
		req, err := http.NewRequest(http.MethodPost, "http://localhost/api/engine_host/get_context", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 400 {
			t.Errorf("invalid JSON status=%d", res.StatusCode)
		}
		if err := res.Body.Close(); err != nil {
			t.Error(err)
		}
	}
}

// Guards: REQ-116
func TestErrorMapsToHTTPStatus(t *testing.T) {
	for code, status := range map[ErrorCode]int{ErrorCodeInvalid: 400, ErrorCodeNotFound: 404, ErrorCodeConflict: 409, ErrorCodeForbidden: 403, ErrorCodeUnavailable: 503, ErrorCodeInternal: 500} {
		t.Run(string(code), func(t *testing.T) {
			mux := http.NewServeMux()
			if err := Mount(mux, "engine_host", &testHost{fail: &Error{Code: code, Message: "測試錯誤", Field: "name"}}); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/engine_host/get_context", strings.NewReader("{}")))
			if w.Code != status {
				t.Fatalf("status=%d want=%d", w.Code, status)
			}
			err := responseError(w.Result())
			var e *Error
			if !errors.As(err, &e) || e.Code != code || e.Field != "name" || e.Message != "測試錯誤" {
				t.Fatalf("error=%v", err)
			}
		})
	}
	mux := http.NewServeMux()
	if err := Mount(mux, "engine_host", &testHost{fail: errors.New("private internal detail")}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/engine_host/get_context", strings.NewReader("{}")))
	if w.Code != 500 || strings.Contains(w.Body.String(), "private") || !strings.Contains(w.Body.String(), "內部錯誤") {
		t.Fatal(w.Body.String())
	}
}

// Guards: REQ-116
func TestUnknownPathIsSDKError(t *testing.T) {
	c, _, _ := startServer(t, nil)
	_, err := Invoke[struct{}, struct{}](context.Background(), c, "/unknown", &struct{}{})
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrorCodeNotFound || e.Message != "沒有這個路徑" {
		t.Fatalf("unknown path: %v", err)
	}
}

// Guards: REQ-116, REQ-144
func TestStreamSSE(t *testing.T) {
	b := &testBus{watch: func(ctx context.Context, r *BusWatchRequest, send func(*BusWatchResponse) error) error {
		for i := 0; i < 3; i++ {
			if err := send(&BusWatchResponse{}); err != nil {
				return err
			}
		}
		return nil
	}}
	c, _, _ := startServer(t, map[string]any{"bus": b})
	count := 0
	err := NewBusClient(c).Watch(context.Background(), &BusWatchRequest{}, func(*BusWatchResponse) error { count++; return nil })
	if err != nil || count != 3 {
		t.Fatalf("stream count=%d err=%v", count, err)
	}
}

// Guards: REQ-116, REQ-144
func TestStreamClientCancel(t *testing.T) {
	stopped := make(chan struct{})
	b := &testBus{watch: func(ctx context.Context, r *BusWatchRequest, send func(*BusWatchResponse) error) error {
		defer close(stopped)
		if err := send(&BusWatchResponse{}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	c, _, _ := startServer(t, map[string]any{"bus": b})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := NewBusClient(c).Watch(ctx, &BusWatchRequest{}, func(*BusWatchResponse) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not observe cancellation")
	}
}

// 服務關機時請求的 context 先被取消，連線還沒斷：不能寫成 event: error，看板會當成永久失敗、不再重連。
func TestStreamCancelledRequestWritesNoErrorEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &testBus{watch: func(_ context.Context, _ *BusWatchRequest, send func(*BusWatchResponse) error) error {
		if err := send(&BusWatchResponse{}); err != nil {
			return err
		}
		cancel()
		return send(&BusWatchResponse{})
	}}
	mux := http.NewServeMux()
	if err := Mount(mux, "bus", b); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/bus/watch", strings.NewReader("{}")).WithContext(ctx))
	if strings.Contains(w.Body.String(), "event: error") {
		t.Fatalf("關機時不該送 error 事件：%q", w.Body.String())
	}
}

// 請求還活著時的錯誤照舊要讓對方知道。
func TestStreamErrorEventWhileRequestAlive(t *testing.T) {
	b := &testBus{watch: func(_ context.Context, _ *BusWatchRequest, send func(*BusWatchResponse) error) error {
		if err := send(&BusWatchResponse{}); err != nil {
			return err
		}
		return &Error{Code: ErrorCodeNotFound, Message: "沒有這個討論串"}
	}}
	mux := http.NewServeMux()
	if err := Mount(mux, "bus", b); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/bus/watch", strings.NewReader("{}")))
	if !strings.Contains(w.Body.String(), "event: error") {
		t.Fatalf("錯誤沒有送出：%q", w.Body.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Guards: REQ-116, REQ-144
func TestEveryRouteHasClientMethod(t *testing.T) {
	c := &Client{}
	clients := map[string]any{"bus": NewBusClient(c), "engine": NewEngineClient(c), "engine_host": NewEngineHostClient(c), "session": NewSessionClient(c), "notify": NewNotifyClient(c), "tasks": NewTasksClient(c), "reminders": NewRemindersClient(c), "vault": NewVaultClient(c)}
	for _, route := range Routes {
		client, ok := clients[route.Service]
		if !ok {
			continue
		}
		t.Run(route.Service+"."+route.Method, func(t *testing.T) {
			called := false
			c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				called = true
				if r.Method != http.MethodPost || r.URL.Path != route.Path {
					t.Errorf("wrong request: %s %s", r.Method, r.URL.Path)
				}
				data := "{}"
				content := "application/json"
				if route.Stream {
					data = "data: {}\n\n"
					content = "text/event-stream"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{content}}, Body: io.NopCloser(strings.NewReader(data))}, nil
			})}
			method := reflect.ValueOf(client).MethodByName(methodName(route.Method))
			if !method.IsValid() {
				t.Fatal("missing client method")
			}
			args := []reflect.Value{reflect.ValueOf(context.Background()), reflect.New(method.Type().In(1).Elem())}
			count := 0
			if route.Stream {
				args = append(args, reflect.MakeFunc(method.Type().In(2), func([]reflect.Value) []reflect.Value {
					count++
					return []reflect.Value{reflect.Zero(reflect.TypeFor[error]())}
				}))
			}
			out := method.Call(args)
			if !out[len(out)-1].IsNil() {
				t.Fatal(out[len(out)-1].Interface())
			}
			if !called || (route.Stream && count != 1) {
				t.Fatal("client did not dispatch or forward response")
			}
		})
	}
}

// Guards: REQ-116, REQ-144
func TestServeRemovesSocket(t *testing.T) {
	_, socket, stop := startServer(t, nil)
	stop()
	if _, err := os.Lstat(socket); !os.IsNotExist(err) {
		t.Fatalf("socket remains: %v", err)
	}
	// Restart with a residual socket left by an interrupted process.
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, socket, map[string]any{"engine_host": &testHost{}}) }()
	c := Dial(socket)
	ready := make(chan error, 1)
	go func() {
		for {
			_, err := NewEngineHostClient(c).GetContext(ctx, &EngineHostGetContextRequest{})
			if err == nil || ctx.Err() != nil {
				ready <- err
				return
			}
			runtime.Gosched()
		}
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Error("restart not ready")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restart did not stop")
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatal(fmt.Errorf("restart socket remains: %v", err))
	}
}

// 串流沒有資料時每 StreamHeartbeat 送一行 SSE 註解（看板靠它判斷連線還活著）；Go 的 client 照常只收到資料。
// Guards: REQ-305
func TestStreamHeartbeat(t *testing.T) {
	old := StreamHeartbeat
	StreamHeartbeat = 20 * time.Millisecond
	t.Cleanup(func() { StreamHeartbeat = old })
	b := &testBus{watch: func(ctx context.Context, r *BusWatchRequest, send func(*BusWatchResponse) error) error {
		time.Sleep(100 * time.Millisecond)
		return send(&BusWatchResponse{})
	}}
	c, socket, _ := startServer(t, map[string]any{"bus": b})
	count := 0
	if err := NewBusClient(c).Watch(context.Background(), &BusWatchRequest{}, func(*BusWatchResponse) error { count++; return nil }); err != nil || count != 1 {
		t.Fatalf("client 只收到資料：count=%d err=%v", count, err)
	}
	hc := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	resp, err := hc.Post("http://x/api/bus/watch", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), ": ping\n\n") || !strings.Contains(string(raw), "data: ") {
		t.Fatalf("要有心跳也要有資料：%q", raw)
	}
}

// 心跳 1 秒：看板與 hub↔節點的推送 3 秒沒收到東西就重連，正常的閒置串流不能被誤判成斷線（REQ-388）。
// 用預設值跑：2.5 秒內至少收到兩次心跳。
// Guards: REQ-388
func TestStreamHeartbeatEverySecond(t *testing.T) {
	if StreamHeartbeat != time.Second {
		t.Fatalf("心跳要 1 秒一次：%s", StreamHeartbeat)
	}
	b := &testBus{watch: func(ctx context.Context, r *BusWatchRequest, send func(*BusWatchResponse) error) error {
		<-ctx.Done()
		return nil
	}}
	_, socket, _ := startServer(t, map[string]any{"bus": b})
	hc := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://x/api/bus/watch", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body) // 讀到 2.5 秒逾時為止
	if n := strings.Count(string(raw), ": ping\n\n"); n < 2 {
		t.Fatalf("2.5 秒內要有至少兩次心跳：%d（%q）", n, raw)
	}
}

// 一個服務可以只掛幾個方法（system 服務由好幾個業務模組分著實作）：只有被列到的方法要有簽名、也只有它們會被掛上。
func TestMountMethodsMountsOnlyListedMethods(t *testing.T) {
	mux := http.NewServeMux()
	if err := MountMethods(mux, "system", &onlyTrust{}, []string{"trust"}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{"/api/system/trust": true, "/api/system/doctor": false} {
		if _, pattern := mux.Handler(httptest.NewRequest(http.MethodPost, path, nil)); (pattern == path) != want {
			t.Errorf("%s 掛載=%v，要 %v", path, pattern == path, want)
		}
	}
	// 沒有簽名的實作掛列到的方法會失敗；列了服務沒有的方法也會失敗。
	if err := MountMethods(http.NewServeMux(), "system", struct{}{}, []string{"trust"}); err == nil || !strings.Contains(err.Error(), "system.trust 期望的簽名") {
		t.Fatalf("簽名不符要回錯：%v", err)
	}
	if err := MountMethods(http.NewServeMux(), "system", &onlyTrust{}, []string{"no_such"}); err == nil || !strings.Contains(err.Error(), "system.no_such 不存在") {
		t.Fatalf("不存在的方法要回錯：%v", err)
	}
	if got := ServiceMethods("system"); len(got) < 5 || ServiceMethods("no_such") != nil {
		t.Fatalf("ServiceMethods：%v", got)
	}
}

// onlyTrust 只有 system 服務的 trust 一個方法。
type onlyTrust struct{}

func (*onlyTrust) Trust(context.Context, *SystemTrustRequest) (*SystemTrustResponse, error) {
	return &SystemTrustResponse{}, nil
}
