package platform

import (
	"errors"
	"os"
	"path/filepath"
)

// Move preserves file contents while relocating a canonical record.
func Move(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(from), filepath.Dir(to)} {
		f, err := os.Open(dir)
		if err != nil {
			return err
		}
		syncErr := f.Sync()
		closeErr := f.Close()
		if err := errors.Join(syncErr, closeErr); err != nil {
			return err
		}
	}
	return nil
}
