package platform

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

type dailyWriter struct {
	mu    sync.Mutex
	home  *Home
	clock Clock
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	dir := w.home.Paths().Runtime("logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return 0, err
	}
	day := w.clock.Now().In(w.home.Location()).Format("2006-01-02")
	path := filepath.Join(dir, day+".jsonl")
	rotateIfLarge(path, filepath.Join(dir, day+".1.jsonl"), len(p)) // 大小上限（PLT-29）
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	n, writeErr := f.Write(p)
	closeErr := f.Close()
	return n, errors.Join(writeErr, closeErr)
}
func NewLogger(home *Home, module string) *slog.Logger {
	clock := home.Clock
	if clock == nil {
		clock = SystemClock{}
	}
	writer := &dailyWriter{home: home, clock: clock}
	handler := slog.NewJSONHandler(io.MultiWriter(writer, os.Stderr), &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Time(slog.TimeKey, clock.Now().In(home.Location()))
			}
			return a
		},
	})
	// 重複的警告與錯誤收斂（PLT-29，logguard.go）
	return slog.New(newRepeatGuard(handler, clock.Now)).With("module", module)
}

// LogValue prevents accidental logging of opaque settings or secret locations.
func (h *Home) LogValue() slog.Value {
	return slog.GroupValue(slog.String("mode", h.Mode), slog.String("timezone", h.User.Timezone))
}
