package vault

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

type subscription struct {
	ch    chan Change
	kinds map[string]bool
	stop  func() bool
}
type watchState struct {
	fs   *fsnotify.Watcher
	log  *slog.Logger
	done chan struct{}
	subs map[*subscription]bool
	dirs map[string]bool // accessed only by the filesystem worker
}

func (m *Module) Watch(ctx context.Context, kinds ...string) (<-chan Change, error) {
	m.lifeMu.Lock()
	defer m.lifeMu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	m.watchMu.Lock()
	defer m.watchMu.Unlock()
	if m.watcher == nil {
		h, err := platform.LoadHome(m.home)
		if err != nil {
			return nil, err
		}
		h.Clock = m.d.Clock
		f, err := fsnotify.NewWatcher()
		if err != nil {
			return nil, err
		}
		w := &watchState{fs: f, log: platform.NewLogger(h, "vault"), done: make(chan struct{}), subs: map[*subscription]bool{}, dirs: map[string]bool{}}
		err = filepath.WalkDir(filepath.Join(m.home, "vault"), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && m.notOursAbs(path) {
				return filepath.SkipDir // 主人自己的資料夾與備份不監看
			}
			if d.IsDir() {
				if err := f.Add(path); err != nil {
					return err
				}
				w.dirs[path] = true
			}
			return nil
		})
		if err != nil {
			return nil, errors.Join(err, f.Close())
		}
		m.watcher = w
		go m.watchFiles(w)
	}
	w := m.watcher
	s := &subscription{ch: make(chan Change, 256), kinds: map[string]bool{}}
	for _, kind := range kinds {
		s.kinds[kind] = true
	}
	w.subs[s] = true
	s.stop = context.AfterFunc(ctx, func() {
		m.watchMu.Lock()
		defer m.watchMu.Unlock()
		if w.subs[s] {
			delete(w.subs, s)
			close(s.ch)
		}
	})
	return s.ch, nil
}

// stopWatch is called with lifeMu held; worker completion is awaited after releasing it.
func (m *Module) stopWatch() error {
	m.watchMu.Lock()
	defer m.watchMu.Unlock()
	w := m.watcher
	if w == nil {
		return nil
	}
	m.watcher = nil
	for s := range w.subs {
		s.stop()
		close(s.ch)
		delete(w.subs, s)
	}
	return w.fs.Close()
}
func (m *Module) emitChange(r Record, typ sdk.ChangeType, by string) {
	m.watchMu.Lock()
	defer m.watchMu.Unlock()
	w := m.watcher
	if w == nil {
		return
	}
	for s := range w.subs {
		if len(s.kinds) > 0 && !s.kinds[r.Kind] {
			continue
		}
		change := &sdk.VaultWatchResponse{Type: typ, Record: &Record{Kind: r.Kind, ID: r.ID, Revision: r.Revision, Archived: r.Archived, UpdatedAt: r.UpdatedAt}, By: &sdk.Actor{Name: by}, At: m.d.Clock.Now()}
		select {
		case s.ch <- change:
		default:
			w.log.Warn("watch notification dropped", "id", r.ID)
		}
	}
}
func (m *Module) isInvalid(path string) bool {
	m.indexMu.RLock()
	defer m.indexMu.RUnlock()
	return m.invalid[path]
}
func (m *Module) Invalid() []string {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	m.indexMu.RLock()
	defer m.indexMu.RUnlock()
	paths := []string{}
	for path := range m.invalid {
		rel, err := filepath.Rel(m.home, path)
		if err == nil {
			paths = append(paths, filepath.ToSlash(rel))
		}
	}
	sort.Strings(paths)
	return paths
}
func (m *Module) watchFiles(w *watchState) {
	defer close(w.done)
	defer func() {
		if recover() != nil {
			w.log.Error("filesystem watcher stopped unexpectedly")
		}
	}()
	for {
		select {
		case _, ok := <-w.fs.Events:
			if !ok {
				return
			}
			func() {
				m.lifeMu.RLock()
				defer m.lifeMu.RUnlock()
				m.watchMu.Lock()
				active := m.watcher == w
				m.watchMu.Unlock()
				if active && m.lockFile != nil {
					m.reconcile(w)
				}
			}()
		case err, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			if err != nil {
				w.log.Warn("filesystem watcher error")
			}
		}
	}
}

// Reconcile the tree so directory moves and editors' atomic replacements are handled too.
func (m *Module) reconcile(w *watchState) {
	root := filepath.Join(m.home, "vault")
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if m.notOursAbs(path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !w.dirs[path] {
				if err := w.fs.Add(path); err != nil {
					return err
				}
				w.dirs[path] = true
			}
			seen[path] = true
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		seen[path] = true
		m.refreshPath(w, path)
		return nil
	})
	if err != nil {
		w.log.Warn("filesystem scan incomplete")
		return
	}
	for path := range w.dirs {
		if !seen[path] {
			delete(w.dirs, path)
		}
	}
	m.indexMu.RLock()
	entries := sortedEntries(m.index)
	m.indexMu.RUnlock()
	for _, e := range entries {
		if !seen[e.path] {
			m.refreshPath(w, e.path)
		}
	}
	m.indexMu.Lock()
	for path := range m.invalid {
		if !seen[path] {
			delete(m.invalid, path)
		}
	}
	m.indexMu.Unlock()
}
func (m *Module) refreshPath(w *watchState, path string) {
	rel, err := filepath.Rel(filepath.Join(m.home, "vault"), path)
	if err != nil {
		return
	}
	if notOurs(rel) {
		return // 主人自己的檔案與備份：不進索引、不算壞檔
	}
	// Obtain identity before taking its shard, then reread under the same lock as writes.
	var data []byte
	info, statErr := os.Lstat(path)
	if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		m.indexMu.Lock()
		m.invalid[path] = true
		m.indexMu.Unlock()
		return
	}
	if statErr == nil {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			w.log.Warn("external file unreadable")
			return
		}
	}
	r, _, _ := Decode(data)
	kind, id, _ := inferIdentity(rel, r)
	if m.inPackageTree(rel) && !m.packageKindRegistered(kind) {
		// 套件資料夾裡種類沒註冊的檔案（停用、解除安裝、被拒的套件）留在原地，不進索引也不算壞檔（D-28）。
		return
	}
	if kind == "" || id == "" {
		m.indexMu.RLock()
		for _, e := range m.index {
			if e.path == path {
				kind, id = e.record.Kind, e.record.ID
				break
			}
		}
		m.indexMu.RUnlock()
	}
	lock := m.shard(kind, id)
	lock.Lock()
	defer lock.Unlock()
	e, exists := m.lookup(kind, id)
	info, statErr = os.Lstat(path)
	if os.IsNotExist(statErr) {
		if exists && e.path == path {
			m.indexMu.Lock()
			delete(m.index, recordKey(kind, id))
			delete(m.invalid, path)
			if !e.record.Archived && m.givenIDs(kind) {
				m.readmitArchived(path)
			}
			m.indexMu.Unlock()
			w.log.Warn("external file removed", "id", id)
		}
		return
	}
	if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		m.indexMu.Lock()
		m.invalid[path] = true
		m.indexMu.Unlock()
		return
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || statErr != nil {
		w.log.Warn("external file unreadable", "id", id)
		return
	}
	if exists && e.path == path && revision(data) == e.record.Revision && !m.isInvalid(path) {
		return
	}
	r, order, decodeErr := decodeIndexed(rel, path, data)
	if OwnerNoteFile(rel, r, decodeErr) {
		// 主人自己放在 notes/ 的筆記（或主題被改成不是主題的格式）：不進索引、不算壞檔。
		m.indexMu.Lock()
		if exists && e.path == path {
			delete(m.index, recordKey(kind, id))
		}
		delete(m.invalid, path)
		m.indexMu.Unlock()
		return
	}
	r.Kind, r.ID, r.Archived = inferIdentity(rel, r)
	if r.Kind == "" {
		r.Kind = kind
	}
	if r.ID == "" {
		r.ID = id
	}
	if decodeErr == nil {
		if sc, ok := m.spec(r.Kind); ok {
			decodeErr = sc.validate(r, m.prefixes)
		}
	}
	if decodeErr != nil || r.Kind == "" || r.ID == "" || info.Mode()&os.ModeSymlink != 0 {
		m.indexMu.Lock()
		m.invalid[path] = true
		m.indexMu.Unlock()
		return
	}
	if s, ok := r.Fields["updated_at"].(string); ok {
		r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, s)
	}
	if exists && e.path != path {
		keepNew, dup := prefer(e, entry{record: r}, m.givenIDs(r.Kind))
		if !dup && !keepNew {
			// 被正本（或較新的封存）蓋過的封存檔，留在原地、不算壞檔（DEV-151）。
			m.indexMu.Lock()
			delete(m.invalid, path)
			m.indexMu.Unlock()
			return
		}
		if dup {
			m.indexMu.Lock()
			m.invalid[path] = true
			m.indexMu.Unlock()
			return
		}
	}
	wasInvalid := m.isInvalid(path)
	m.indexMu.Lock()
	delete(m.invalid, path)
	m.index[recordKey(r.Kind, r.ID)] = entry{r, order, path}
	m.indexMu.Unlock()
	typ := sdk.ChangeTypeCreated
	if exists || wasInvalid {
		typ = sdk.ChangeTypeUpdated
	}
	m.emitChange(r, typ, "external")
}

// readmitArchived：given 種類的正本被外部刪掉時，把同編號被它蓋過的封存檔接回索引（DEV-151），不用等重開。
// 呼叫者持有 indexMu 寫鎖與這筆的分片鎖。
func (m *Module) readmitArchived(livePath string) {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(livePath), "archive", "*", filepath.Base(livePath)))
	for _, p := range matches {
		_ = m.indexFile(filepath.Join(m.home, "vault"), p)
	}
}
