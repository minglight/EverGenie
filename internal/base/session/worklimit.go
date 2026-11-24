package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
)

// 工作 session 的上限（settings/sessions.toml 的 work_max，SES-30、SES-73）：預設值、合理範圍、讀與寫都在這裡，
// 開工作 session 前的檢查（workMax）和看板的設定（system.get_work_max｜set_work_max）用同一份。

const (
	// DefaultWorkMax 是每台同時 doing 的工作 session 上限（settings/sessions.toml 的 work_max 沒寫時）。
	DefaultWorkMax = 3
	// MinWorkMax、MaxWorkMax 是從看板改時接受的範圍（手改檔案寫更大的照樣生效，只是看板不讓改到那麼大）。
	MinWorkMax = 1
	MaxWorkMax = 10
)

// WorkSettingsPath 是 settings/sessions.toml 的位置。
func WorkSettingsPath(home string) string { return filepath.Join(home, "settings", "sessions.toml") }

// ReadWorkMax 讀 work_max：set＝檔裡有寫、而且是合理的值（≥ 1）；沒有檔、沒寫、不合理都回預設值、set＝false。
// 檔案讀不懂時另外回 err（呼叫的人決定要不要記下來），值一樣是預設。
func ReadWorkMax(home string) (n int, set bool, err error) {
	data, err := os.ReadFile(WorkSettingsPath(home))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultWorkMax, false, nil
		}
		return DefaultWorkMax, false, err
	}
	var f struct {
		WorkMax int `toml:"work_max"`
	}
	if _, err := toml.Decode(string(data), &f); err != nil {
		return DefaultWorkMax, false, err
	}
	if f.WorkMax < 1 {
		return DefaultWorkMax, false, nil
	}
	return f.WorkMax, true, nil
}

// workMaxMu 讓 WriteWorkMax 一次一個。
var workMaxMu sync.Mutex

// workMaxLine 認最上層的 `work_max = …`（可以有前後空白與行尾註解）：1 縮排與等號、2 值、3 行尾的空白與註解。
var workMaxLine = regexp.MustCompile(`^(\s*work_max\s*=\s*)([^#\r\n]*?)(\s*(?:#.*)?)$`)

// WriteWorkMax 把 work_max 寫成 n（MinWorkMax～MaxWorkMax）：只改那一行的值，其他內容、註解、空行一個字都不動；
// 沒寫就加在最上層最後（第一個 [表格] 之前）；沒有檔就新建。原檔先備份到暫存資料夾的 backups/sessions/，
// 寫完再讀一次確認是 n，不對就把原檔寫回。檔案本來就讀不懂時不改（免得越改越壞）。回傳備份的路徑（沒有原檔就是空）。
// 錯誤訊息會直接顯示給主人：白話、不寫檔名（UXC-8）；檔名留給呼叫的人記 log。
func WriteWorkMax(home string, n int, now time.Time) (backup string, err error) {
	if n < MinWorkMax || n > MaxWorkMax {
		return "", platform.Invalid("work_max", fmt.Sprintf("同時進行的對話只能設 %d 到 %d 個", MinWorkMax, MaxWorkMax))
	}
	workMaxMu.Lock() // 看板連按時兩個請求不要互相看到對方寫一半的值
	defer workMaxMu.Unlock()
	path := WorkSettingsPath(home)
	old, err := os.ReadFile(path)
	mode := os.FileMode(0o600)
	switch {
	case errors.Is(err, os.ErrNotExist):
		old = nil
	case err != nil:
		return "", platform.Unavailable("讀不到同時進行的對話設定")
	default:
		if info, serr := os.Stat(path); serr == nil {
			mode = info.Mode().Perm()
		}
		var probe map[string]any
		if _, derr := toml.Decode(string(old), &probe); derr != nil {
			return "", platform.Invalid("work_max", "同時進行的對話設定檔讀不懂，先不改；跟任何一位秘書說，請她看一下")
		}
	}
	next := setWorkMaxText(string(old), n)
	if old != nil {
		abs := platform.RuntimeDir(home, "backups", "sessions", now.Format("20060102-150405.000000000")+".toml")
		backup = abs
		if rel, rerr := filepath.Rel(home, abs); rerr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			backup = rel
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			return "", platform.Unavailable("沒辦法先備份原本的設定，先不改")
		}
		if err := platform.AtomicWrite(abs, old, 0o600); err != nil {
			return "", platform.Unavailable("沒辦法先備份原本的設定，先不改")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return backup, platform.Unavailable("沒辦法存設定；跟任何一位秘書說，請她看一下")
	}
	if err := platform.AtomicWrite(path, []byte(next), mode); err != nil {
		return backup, platform.Unavailable("沒辦法存設定；跟任何一位秘書說，請她看一下")
	}
	if got, set, rerr := ReadWorkMax(home); rerr != nil || !set || got != n {
		if old != nil {
			_ = platform.AtomicWrite(path, old, mode)
		}
		return backup, platform.Internal("存進去讀回來不對，已經改回原本的值")
	}
	return backup, nil
}

// setWorkMaxText 回改好的檔案內容（見 WriteWorkMax）。空內容＝新檔。
func setWorkMaxText(old string, n int) string {
	value := strconv.Itoa(n)
	if old == "" {
		return "schema = 1\nwork_max = " + value + "\n"
	}
	lines := strings.SplitAfter(old, "\n")
	top := len(lines) // 最上層到哪一行為止（第一個 [表格] 之前）
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "[") {
			top = i
			break
		}
	}
	for i := 0; i < top; i++ {
		body, eol := strings.TrimRight(lines[i], "\r\n"), lines[i][len(strings.TrimRight(lines[i], "\r\n")):]
		if m := workMaxLine.FindStringSubmatch(body); m != nil {
			lines[i] = m[1] + value + m[3] + eol
			return strings.Join(lines, "")
		}
	}
	// 沒寫過：加在最上層最後；前一行沒換行先補上。
	add := "work_max = " + value + "\n"
	head := strings.Join(lines[:top], "")
	if head != "" && !strings.HasSuffix(head, "\n") {
		head += "\n"
	}
	tail := strings.Join(lines[top:], "")
	if tail != "" && head != "" && !strings.HasSuffix(head, "\n\n") {
		add += "\n"
	}
	return head + add + tail
}
