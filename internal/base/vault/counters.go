package vault

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
)

func (m *Module) loadCounters() error {
	data, err := os.ReadFile(filepath.Join(m.home, "vault", ".counters.toml"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if _, err := toml.Decode(string(data), &m.counters); err != nil {
			return platform.Invalid("counters", "識別碼計數檔格式不正確")
		}
	}
	for _, e := range m.index {
		prefix, ok := m.prefixes[e.record.Kind]
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimPrefix(e.record.ID, prefix), 10, 64)
		if err == nil && n > m.counters[e.record.Kind] {
			m.counters[e.record.Kind] = n
		}
	}
	return nil
}
func (m *Module) NextID(ctx context.Context, kind string) (string, error) {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return "", err
	}
	prefix, ok := m.prefixes[kind]
	if !ok {
		sc, registered := m.spec(kind)
		switch {
		case registered && sc.IDMode == "seq":
			prefix = sc.IDPrefix
		case registered && sc.IDMode == "given":
			return "", platform.Invalid("kind", "這種資料的編號要自己給")
		default:
			return "", platform.Invalid("kind", "這個種類沒有遞增識別碼")
		}
	}
	lock, _ := m.counterLocks.LoadOrStore(kind, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	m.counterMu.Lock()
	defer m.counterMu.Unlock()
	next := m.counters[kind] + 1
	if next <= 0 {
		return "", platform.Internal("識別碼計數已超出範圍")
	}
	copy := map[string]int64{}
	for k, v := range m.counters {
		copy[k] = v
	}
	copy[kind] = next
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(copy); err != nil {
		return "", err
	}
	if err := platform.AtomicWrite(filepath.Join(m.home, "vault", ".counters.toml"), buf.Bytes(), 0600); err != nil {
		return "", err
	}
	m.counters = copy
	return prefix + strconv.FormatInt(next, 10), nil
}
