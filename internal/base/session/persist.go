package session

import (
	"context"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

// Coalesce bursts without holding the state mutex during disk I/O.
func (m *Module) persistLoop(ctx context.Context) {
	defer m.wg.Done()
	defer m.recoverWorker()
	retry := time.NewTicker(time.Second)
	defer retry.Stop()
	for {
		select {
		case <-ctx.Done():
			return // Stop flushes after all workers have stopped.
		case <-m.persist:
			timer := time.NewTimer(25 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		case <-retry.C:
		}
		if err := m.flushDirty(); err != nil {
			m.warn("背景寫入對話狀態失敗，稍後重試", err)
		}
	}
}
func (m *Module) flushDirty() error {
	m.mu.Lock()
	names := make([]string, 0, len(m.dirty))
	for name := range m.dirty {
		names = append(names, name)
	}
	m.mu.Unlock()
	for _, name := range names {
		m.mu.Lock()
		if !m.dirty[name] {
			m.mu.Unlock()
			continue
		}
		// Never wait for a synchronous save while holding the state mutex.
		if !m.writeMu.TryLock() {
			m.mu.Unlock()
			continue
		}
		e := m.entries[name]
		if e == nil {
			// A panic here would end persistLoop and silently stop all state writes.
			delete(m.dirty, name)
			m.writeMu.Unlock()
			m.mu.Unlock()
			continue
		}
		data, err := m.encodeState(e)
		if err != nil {
			m.writeMu.Unlock()
			m.mu.Unlock()
			return err
		}
		delete(m.dirty, name)
		path := platform.RuntimeDir(m.home, "sessions", name+".md")
		write := m.writeState
		m.mu.Unlock()
		err = write(path, data)
		// Lock order matters: synchronous save already owns mu while waiting for writeMu.
		m.writeMu.Unlock()
		if err != nil {
			m.mu.Lock()
			m.dirty[name] = true
			m.mu.Unlock()
			return platform.Unavailable("無法儲存對話狀態，請檢查磁碟")
		}
	}
	return nil
}
