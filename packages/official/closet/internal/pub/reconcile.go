package pub

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry 是公開資料夾裡一個「應該存在的檔案」。
type Entry struct {
	Src        string // 來源檔（完整路徑）
	Silhouette bool   // true＝不複製，從來源產生剪影（輸出是 PNG）
}

// Plan 是完整清單：目的相對路徑（用 /）→ 來源。公開資料夾裡不在清單的檔案會被刪掉。
type Plan map[string]Entry

// Result 是對帳的結果。
type Result struct {
	Present map[string]bool  // 對帳後確實存在的目的路徑
	Errors  map[string]error // 這輪沒做成的（剪影解不開、複製失敗）
}

const stateFile = ".state.json"

// Reconcile 讓 dir（公開資料夾）的內容等於 plan：先刪不在清單的（`.` 開頭的檔不碰），再把缺的複製（先寫暫存檔再 rename，mtime 設成現在好讓瀏覽器重新驗證），
// 用 .state.json 判斷來源有沒有變，沒變就不重寫。
// 來源檔一律先確認是一般檔案（不跟符號連結）、大小 ≤ 30 MB。
func Reconcile(dir string, plan Plan) (Result, error) {
	res := Result{Present: map[string]bool{}, Errors: map[string]error{}}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	// 先刪不在清單的，再寫新的：換衣服時舊的 worn mp4 要在新的 worn 圖出現之前就不見，看板才不會「圖是這套、影片是別套」（DEV-97）。
	prune(dir, plan)
	state := loadState(dir)
	next := map[string]string{}
	dests := make([]string, 0, len(plan))
	for d := range plan {
		dests = append(dests, d)
	}
	sort.Strings(dests)
	for _, dest := range dests {
		if !safeDest(dest) {
			res.Errors[dest] = fmt.Errorf("目的路徑不合格")
			continue
		}
		e := plan[dest]
		info, ok := RegularFile(e.Src)
		if !ok {
			res.Errors[dest] = fmt.Errorf("來源不是一般檔案或太大")
			removeFile(filepath.Join(dir, filepath.FromSlash(dest)))
			continue
		}
		sig := signature(e, info)
		full := filepath.Join(dir, filepath.FromSlash(dest))
		if cur, err := os.Lstat(full); state[dest] == sig && err == nil && cur.Mode().IsRegular() {
			next[dest] = sig
			res.Present[dest] = true
			continue
		}
		if err := produce(full, e); err != nil {
			res.Errors[dest] = err
			removeFile(full)
			continue
		}
		next[dest] = sig
		res.Present[dest] = true
	}
	saveState(dir, state, next)
	return res, nil
}

func signature(e Entry, info os.FileInfo) string {
	s := e.Src + "|" + strconv.FormatInt(info.Size(), 10) + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	if e.Silhouette {
		s += "|sil" + strconv.Itoa(SilhouetteVersion)
	}
	return s
}

func safeDest(d string) bool {
	if d == "" || strings.HasPrefix(d, "/") {
		return false
	}
	for _, p := range strings.Split(d, "/") {
		if !SafeName(p) {
			return false
		}
	}
	return true
}

func produce(full string, e Entry) error {
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	src, err := os.Open(e.Src)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	tmp, err := os.CreateTemp(filepath.Dir(full), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if e.Silhouette {
		data, err := Silhouette(src)
		if err != nil {
			return fail(err)
		}
		if _, err := tmp.Write(data); err != nil {
			return fail(err)
		}
	} else if _, err := io.Copy(tmp, io.LimitReader(src, MaxFileBytes+1)); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	now := time.Now()
	_ = os.Chtimes(tmpName, now, now)
	if err := os.Rename(tmpName, full); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func removeFile(path string) {
	if info, err := os.Lstat(path); err == nil && !info.IsDir() {
		_ = os.Remove(path)
	}
}

// prune 刪掉不在清單裡的檔案（含符號連結本身，不跟過去），再刪空資料夾；`.` 開頭的檔與資料夾不碰。
func prune(dir string, plan Plan) {
	var dirs []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			if strings.HasPrefix(d.Name(), ".tmp-") {
				_ = os.Remove(path) // 上次寫到一半留下的暫存檔
			}
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if _, keep := plan[filepath.ToSlash(rel)]; !keep {
			_ = os.Remove(path)
		}
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, d := range dirs {
		_ = os.Remove(d) // 非空會失敗，正好
	}
}

func loadState(dir string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func saveState(dir string, old, next map[string]string) {
	if len(old) == len(next) {
		same := true
		for k, v := range next {
			if old[k] != v {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	data, err := json.Marshal(next)
	if err != nil {
		return
	}
	tmp := filepath.Join(dir, stateFile+".tmp")
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, filepath.Join(dir, stateFile))
	}
}
