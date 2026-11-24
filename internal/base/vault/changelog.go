package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

func (m *Module) changeLog(r Record, by, op string) error {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	now := m.d.Clock.Now().In(m.loc)
	data, err := json.Marshal(map[string]string{"at": now.Format(time.RFC3339Nano), "by": by, "kind": r.Kind, "id": r.ID, "op": op})
	if err != nil {
		return err
	}
	dir := platform.RuntimeDir(m.home, "logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "changes-"+now.Format("2006-01-02")+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return errors.Join(err, f.Close())
}
