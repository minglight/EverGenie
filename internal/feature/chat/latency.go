package chat

import (
	"time"

	"github.com/minglight/evergenie/sdk"
)

// latencyStream 是一條 session.watch 串流自己的記錄：每個 session 最後推出去的事件時間。第一次看到的那筆（連上時的現況）
// 只記下來、不算；之後事件時間往前走了才算一筆（同一個事件的後續變化不重複算）。樣本交給 m.latency（REQ-390）。
type latencyStream struct {
	l    observer
	now  func() time.Time
	last map[string]time.Time
}

func (m *Module) latencyStream() *latencyStream {
	return &latencyStream{l: m.latency, now: m.now, last: map[string]time.Time{}}
}

func (s *latencyStream) sent(v *sdk.Session) {
	if s == nil || s.l == nil || v == nil || v.LastEventAt.IsZero() {
		return
	}
	prev, seen := s.last[v.Name]
	if seen && !v.LastEventAt.After(prev) {
		return
	}
	s.last[v.Name] = v.LastEventAt
	if !seen {
		return
	}
	now := s.now()
	s.l.Observe(now, now.Sub(v.LastEventAt))
}
