package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// DeniedLogPath 是核心擋下（或改成問主人）的動作紀錄，一行一筆 JSON（runtime，不進 git）。
func DeniedLogPath(home string) string {
	return platform.RuntimeDir(home, "trust", "denied.jsonl")
}

// AppendDenied 追加一筆被擋的動作；寫不進去只回錯誤，呼叫端記 log 就好，不影響判斷本身。
func AppendDenied(home string, d sdk.DeniedAction) error {
	path := DeniedLogPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadDenied 讀 since 之後的被擋紀錄（舊的在前）；檔案不存在回空，壞掉的行略過。
func ReadDenied(home string, since time.Time) ([]sdk.DeniedAction, error) {
	data, err := os.ReadFile(DeniedLogPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, platform.Unavailable("無法讀取被擋紀錄")
	}
	var out []sdk.DeniedAction
	for _, line := range bytes.Split(data, []byte("\n")) {
		var d sdk.DeniedAction
		if len(bytes.TrimSpace(line)) == 0 || json.Unmarshal(line, &d) != nil || d.At.Before(since) {
			continue
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}
