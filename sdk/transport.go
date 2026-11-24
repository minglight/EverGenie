package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
)

var serviceTypes = map[string]reflect.Type{
	"bus": reflect.TypeFor[BusAPI](), "engine": reflect.TypeFor[EngineAPI](),
	"engine_host": reflect.TypeFor[EngineHostAPI](), "session": reflect.TypeFor[SessionAPI](),
	"notify": reflect.TypeFor[NotifyAPI](), "tasks": reflect.TypeFor[TasksAPI](),
	"reminders": reflect.TypeFor[RemindersAPI](), "vault": reflect.TypeFor[VaultAPI](),
	"web": reflect.TypeFor[WebAPI](), "package": reflect.TypeFor[PackageAPI](),
	"goals": reflect.TypeFor[GoalsAPI](), "policy": reflect.TypeFor[PolicyAPI](), "system": reflect.TypeFor[SystemAPI](),
	"scheduler": reflect.TypeFor[SchedulerAPI](),
	"agents":    reflect.TypeFor[AgentsAPI](),
	"personas":  reflect.TypeFor[PersonasAPI](),
	"rules":     reflect.TypeFor[RulesAPI](),
	"notes":     reflect.TypeFor[NotesAPI](),
	"backup":    reflect.TypeFor[BackupAPI](),
}

var StreamHeartbeat = time.Second

func methodName(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}
func publicError(err error) *Error {
	var e *Error
	if errors.As(err, &e) && e != nil {
		return e
	}
	return &Error{Code: ErrorCodeInternal, Message: "內部錯誤"}
}
func writeError(w http.ResponseWriter, err error) {
	e := publicError(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(HTTPStatus(e.Code))
	// A failed response write means the caller disconnected; no further response is possible.
	if err := json.NewEncoder(w).Encode(e); err != nil {
		return
	}
}
func NotFoundHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, &Error{Code: ErrorCodeNotFound, Message: "沒有這個路徑"})
	})
}

// Mount validates every method before registering any handlers.
func Mount(mux *http.ServeMux, service string, impl any) error {
	return MountMethods(mux, service, impl, nil)
}

// ServiceMethods 回傳某個服務在 Routes 裡的所有方法（網址上的名稱，例 "set_permission_levels"）；不認得的服務回 nil。
func ServiceMethods(service string) []string {
	var out []string
	for _, route := range Routes {
		if route.Service == service {
			out = append(out, route.Method)
		}
	}
	return out
}

// MountMethods 同 Mount，但只掛 methods 列的方法（網址上的名稱）；methods 是空的就掛整個服務。
// 用在一個 sdk 服務被好幾個業務模組分著實作的時候（例：system 服務的 trust、accounts 各自由業務模組實作）：
// impl 只需要有被列到的方法。列了服務沒有的方法回錯。
func MountMethods(mux *http.ServeMux, service string, impl any, methods []string) error {
	api, ok := serviceTypes[service]
	if !ok {
		return fmt.Errorf("unknown service: %s", service)
	}
	want, unseen := map[string]bool{}, map[string]bool{}
	for _, m := range methods {
		want[m], unseen[m] = true, true
	}
	value := reflect.ValueOf(impl)
	handlers := map[string]http.Handler{}
	for _, route := range Routes {
		if route.Service != service || (len(want) > 0 && !want[route.Method]) {
			continue
		}
		delete(unseen, route.Method)
		name := methodName(route.Method)
		expected, ok := api.MethodByName(name)
		if !ok {
			return fmt.Errorf("%s.%s 期望的簽名不存在", service, route.Method)
		}
		var method reflect.Value
		if value.IsValid() && !(value.Kind() == reflect.Pointer && value.IsNil()) {
			method = value.MethodByName(name)
		}
		if !method.IsValid() || method.Type() != expected.Type {
			return fmt.Errorf("%s.%s 期望的簽名 %s", service, route.Method, expected.Type)
		}
		handlers[route.Path] = methodHandler(method, route.Stream)
	}
	for m := range unseen {
		return fmt.Errorf("%s.%s 不存在", service, m)
	}
	for path, h := range handlers {
		mux.Handle(path, h)
	}
	return nil
}
func methodHandler(method reflect.Value, stream bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, &Error{Code: ErrorCodeInvalid, Message: "請使用 POST 請求"})
			return
		}
		req := reflect.New(method.Type().In(1).Elem())
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(req.Interface()); err != nil {
			writeError(w, &Error{Code: ErrorCodeInvalid, Message: "請傳入有效的 JSON"})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeError(w, &Error{Code: ErrorCodeInvalid, Message: "請只傳入一份 JSON"})
			return
		}
		args := []reflect.Value{reflect.ValueOf(r.Context()), req}
		if !stream {
			out := method.Call(args)
			if !out[1].IsNil() {
				writeError(w, out[1].Interface().(error))
				return
			}
			data, err := json.Marshal(out[0].Interface())
			if err != nil {
				writeError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(data); err != nil {
				return
			}
			return
		}
		var mu sync.Mutex
		started := false
		send := reflect.MakeFunc(method.Type().In(2), func(values []reflect.Value) []reflect.Value {
			mu.Lock()
			defer mu.Unlock()
			err := r.Context().Err()
			if err == nil {
				var data []byte
				data, err = json.Marshal(values[0].Interface())
				if err == nil {
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("Cache-Control", "no-cache")
					started = true
					_, err = fmt.Fprintf(w, "data: %s\n\n", data)
					if err == nil {
						err = http.NewResponseController(w).Flush()
					}
				}
			}
			if err != nil {
				return []reflect.Value{reflect.ValueOf(&err).Elem()}
			}
			return []reflect.Value{reflect.Zero(reflect.TypeFor[error]())}
		})
		// 心跳：每 StreamHeartbeat 送一行 SSE 註解。手機切到背景、換網路時連線會悄悄斷掉，
		// 看板靠「多久沒收到東西」判斷要重連（REQ-305）；註解不是事件，看板不會當成資料。
		stop := make(chan struct{})
		pinged := make(chan struct{})
		go func() {
			defer close(pinged)
			t := time.NewTicker(StreamHeartbeat)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-r.Context().Done():
					return
				case <-t.C:
					mu.Lock()
					if !started {
						w.Header().Set("Content-Type", "text/event-stream")
						w.Header().Set("Cache-Control", "no-cache")
						started = true
					}
					if _, err := fmt.Fprint(w, ": ping\n\n"); err == nil {
						_ = http.NewResponseController(w).Flush()
					}
					mu.Unlock()
				}
			}
		}()
		out := method.Call(append(args, send))
		close(stop)
		<-pinged // 心跳停了才能結束這個請求（之後再寫 w 會出錯）
		mu.Lock()
		defer mu.Unlock()
		// 請求本身已經結束（服務正在關、對方走了）就不算錯誤：寫成 event: error 的話，對方會當成永久失敗、不再重連（DEV-132）。
		if !out[0].IsNil() && r.Context().Err() == nil {
			err := out[0].Interface().(error)
			if !started {
				writeError(w, err)
				return
			}
			// HTTP status is committed after the first item; signal subsequent errors as SSE events.
			data, marshalErr := json.Marshal(publicError(err))
			if marshalErr != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "event: error\ndata: %s\n\n", data); err != nil {
				return
			}
			if err := http.NewResponseController(w).Flush(); err != nil {
				return
			}
		} else if !started {
			w.Header().Set("Content-Type", "text/event-stream")
		}
	})
}

// Serve runs until cancellation and removes the socket on exit.
func Serve(ctx context.Context, socketPath string, mounts map[string]any) error {
	mux := http.NewServeMux()
	for name, impl := range mounts {
		if err := Mount(mux, name, impl); err != nil {
			return err
		}
	}
	mux.Handle("/", NotFoundHandler())
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("socket path is not a socket")
		}
		if err := os.Remove(socketPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	// UnixListener removes its socket when closed.
	server := &http.Server{Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		select {
		case <-ctx.Done():
			// Closing cancels active streams as well as idle connections.
			if err := server.Close(); err != nil {
				return
			}
		case <-done:
		}
	}()
	err = server.Serve(listener)
	close(done)
	<-closed
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
