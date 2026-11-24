package vault

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
)

func (m *Module) Snapshot(ctx context.Context, dest string) (path string, err error) {
	m.lifeMu.Lock()
	defer m.lifeMu.Unlock()
	if err := m.ready(ctx); err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(m.home, absolute)
	if err != nil {
		return "", err
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return "", platform.Invalid("dest", "快照請存放在家目錄之外")
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return "", err
	}
	path = filepath.Join(absolute, m.d.Clock.Now().In(m.loc).Format("2006-01-02")+".tar.gz")
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return "", platform.Invalid("dest", "快照路徑不是檔案")
		}
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	temp, err := os.CreateTemp(absolute, ".snapshot-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if removeErr := os.Remove(temp.Name()); removeErr != nil && !os.IsNotExist(removeErr) {
			err = errors.Join(err, removeErr)
		}
	}()
	gz := gzip.NewWriter(temp)
	archive := tar.NewWriter(gz)
	walkErr := filepath.WalkDir(m.home, func(source string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if source == absolute {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(m.home, source)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSocket != 0 {
			return nil
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(source)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(source)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(archive, f)
			return errors.Join(copyErr, f.Close())
		}
		return nil
	})
	err = errors.Join(walkErr, archive.Close(), gz.Close(), temp.Sync(), temp.Close())
	if err != nil {
		return "", err
	}
	if err := platform.Move(temp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}
