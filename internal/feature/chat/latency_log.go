package chat

import (
	"sort"
	"sync"
	"time"
)

const stateLatencyCap = 5000

type latencySample struct {
	at   time.Time // 推出去的時間
	took time.Duration
}

// latencyLog 收「狀態延遲」的樣本（REQ-390）：session.watch 串流推出去的每個事件，從引擎事件發生到推出去花了多久。
// 串流用 Observe 記下，健檢 sessions.state_latency（checks.go）用 Since 讀；2026-11-01 從 doctor 搬來（讀寫都在 chat，不再經 registry 轉交）。
// 只在記憶體（serve 重開就從頭算）。
type latencyLog struct {
	mu      sync.Mutex
	samples []latencySample
}

// Observe 記一筆：at 是推出去的時間，took 是花的時間（負的當 0：兩台的時鐘差一點點）。
func (l *latencyLog) Observe(at time.Time, took time.Duration) {
	if l == nil {
		return
	}
	if took < 0 {
		took = 0 // 兩台的時鐘差一點點：當成 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.samples = append(l.samples, latencySample{at: at, took: took})
	if n := len(l.samples); n > stateLatencyCap {
		l.samples = append([]latencySample(nil), l.samples[n-stateLatencyCap:]...)
	}
}

// Since：從 from 之後推出去的那些花了多久，排好序。
func (l *latencyLog) Since(from time.Time) []time.Duration {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []time.Duration
	for _, s := range l.samples {
		if !s.at.Before(from) {
			out = append(out, s.took)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
