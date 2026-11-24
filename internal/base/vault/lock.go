package vault

import (
	"context"
	"errors"
	"hash/fnv"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/minglight/evergenie/internal/base/platform"
)

func recordKey(kind, id string) string { return kind + "/" + id }
func (m *Module) shard(kind, id string) *sync.Mutex {
	hash := fnv.New32a()
	_, err := hash.Write([]byte(recordKey(kind, id)))
	if err != nil {
		panic(err)
	}
	return &m.shards[hash.Sum32()%64]
}
func (m *Module) Open(home string) error {
	m.lifeMu.Lock()
	defer m.lifeMu.Unlock()
	if m.lockFile != nil {
		return platform.Conflict("資料庫已開啟")
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(h.Paths().Runtime(), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.Paths().Runtime(platform.ServiceLockName), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		closeErr := f.Close()
		if closeErr != nil {
			return errors.Join(platform.Unavailable("已有另一個行程開啟資料庫"), closeErr)
		}
		return platform.Unavailable("已有另一個行程開啟資料庫")
	}
	m.home = home
	m.loc = h.Location()
	m.prefixes = idPrefixesFor(h.IDs)
	m.lockFile = f
	if m.d.Clock == nil {
		m.d.Clock = platform.SystemClock{}
	}
	m.index = make(map[string]entry)
	m.invalid = make(map[string]bool)
	m.counters = make(map[string]int64)
	m.officialRoot = officialRootFor(home)
	if err = os.MkdirAll(filepath.Join(home, "vault"), 0700); err == nil {
		err = m.scan()
	}
	if err == nil {
		m.scanOfficial()
	}
	if err == nil {
		err = m.loadCounters()
	}
	if err != nil {
		unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		closeErr := f.Close()
		m.lockFile = nil
		return errors.Join(err, unlockErr, closeErr)
	}
	return nil
}
func (m *Module) close(ctx context.Context) error {
	m.lifeMu.Lock()
	if m.lockFile == nil {
		m.lifeMu.Unlock()
		return nil
	}
	m.watchMu.Lock()
	w := m.watcher
	m.watchMu.Unlock()
	watchErr := m.stopWatch()
	err := syscall.Flock(int(m.lockFile.Fd()), syscall.LOCK_UN)
	closeErr := m.lockFile.Close()
	m.lockFile = nil
	m.lifeMu.Unlock()
	if w != nil {
		select {
		case <-w.done:
		case <-ctx.Done():
			return errors.Join(watchErr, err, closeErr, ctx.Err())
		}
	}
	return errors.Join(watchErr, err, closeErr)
}
