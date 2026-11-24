package platform

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 日誌不會把硬碟寫滿（DEV-153，PLT-29）。三道：
//  1. 重複的警告與錯誤收斂（repeatGuard）：同一句（等級、標題、欄位都一樣）在視窗內只寫第一次，之後的只數次數；
//     視窗過了再出現時寫一行、帶 repeated=<這段時間沒寫的次數>，視窗加倍（1 分鐘起、最多 1 小時）。安靜超過 1 小時就重新算。
//  2. 每日日誌有大小上限（LogMaxBytes）：超過就把當天那份改名成 <日期>.1.jsonl（只留一份，舊的蓋掉），再開新的。
//  3. 服務的 serve.err.log／serve.out.log（launchd 接的 stderr／stdout，不照日期分檔）：超過上限時把內容留一份到 <檔名>.1 再清空
//     （GuardServiceLogs，啟動時與之後每小時）。上一份留不成（硬碟滿了）就原地截斷、只留最後一小段，不會因為留不了就不清。
// 不寫主人說的話是另一條（ERR-8）：日誌欄位只放「哪裡、什麼狀況」，不放訊息內文。

// LogMaxBytes 是一個日誌檔的大小上限。測試可以改小。
var LogMaxBytes int64 = 20 << 20

const (
	repeatWindow    = time.Minute
	repeatWindowMax = time.Hour
	repeatKeysMax   = 512
	// RepeatedKey 是收斂後那一行帶的欄位：上次寫出來之後，同一句又發生了幾次沒寫。
	RepeatedKey = "repeated"
)

type repeatEntry struct {
	last, lastSeen time.Time // last：上次寫出來的時間；lastSeen：上次發生（寫不寫都算）
	window         time.Duration
	skipped        int
}

type repeatState struct {
	mu   sync.Mutex
	seen map[string]*repeatEntry
	now  func() time.Time
}

// repeatGuard 包在真正的 handler 外面；WithAttrs／WithGroup 出來的共用同一份 state，key 帶著各自的前綴。
type repeatGuard struct {
	inner  slog.Handler
	state  *repeatState
	prefix string
}

func newRepeatGuard(inner slog.Handler, now func() time.Time) *repeatGuard {
	return &repeatGuard{inner: inner, state: &repeatState{seen: map[string]*repeatEntry{}, now: now}}
}

func (g *repeatGuard) Enabled(ctx context.Context, l slog.Level) bool {
	return g.inner.Enabled(ctx, l)
}

func (g *repeatGuard) WithAttrs(attrs []slog.Attr) slog.Handler {
	var b strings.Builder
	b.WriteString(g.prefix)
	for _, a := range attrs {
		b.WriteString(a.Key + "=" + a.Value.String() + "\x00")
	}
	return &repeatGuard{inner: g.inner.WithAttrs(attrs), state: g.state, prefix: b.String()}
}

func (g *repeatGuard) WithGroup(name string) slog.Handler {
	return &repeatGuard{inner: g.inner.WithGroup(name), state: g.state, prefix: g.prefix + name + "\x01"}
}

func (g *repeatGuard) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < slog.LevelWarn { // 一般紀錄不收斂：它們本來就不該重複，重複了也要看得到順序
		return g.inner.Handle(ctx, r)
	}
	var b strings.Builder
	b.WriteString(g.prefix + r.Level.String() + "\x00" + r.Message + "\x00")
	r.Attrs(func(a slog.Attr) bool {
		b.WriteString(a.Key + "=" + a.Value.String() + "\x00")
		return true
	})
	key, now := b.String(), g.state.now()

	g.state.mu.Lock()
	e := g.state.seen[key]
	if e != nil && now.Sub(e.lastSeen) > repeatWindowMax {
		e = nil // 安靜很久了：當成新的事
	}
	if e == nil {
		if len(g.state.seen) >= repeatKeysMax {
			for k, old := range g.state.seen {
				if now.Sub(old.lastSeen) > repeatWindowMax {
					delete(g.state.seen, k)
				}
			}
			if len(g.state.seen) >= repeatKeysMax {
				g.state.seen = map[string]*repeatEntry{} // 還是滿的（很多種不同的錯）：全部重新算，寧可多寫幾行
			}
		}
		g.state.seen[key] = &repeatEntry{last: now, lastSeen: now, window: repeatWindow}
		g.state.mu.Unlock()
		return g.inner.Handle(ctx, r)
	}
	e.lastSeen = now
	if now.Sub(e.last) < e.window {
		e.skipped++
		g.state.mu.Unlock()
		return nil
	}
	skipped := e.skipped
	e.skipped, e.last = 0, now
	if e.window *= 2; e.window > repeatWindowMax {
		e.window = repeatWindowMax
	}
	g.state.mu.Unlock()
	if skipped > 0 {
		r = r.Clone()
		r.AddAttrs(slog.Int(RepeatedKey, skipped))
	}
	return g.inner.Handle(ctx, r)
}

// rotateMu：一個行程裡每個模組各有一個 dailyWriter，改名只能一個一個來。
var rotateMu sync.Mutex

// rotateIfLarge：path 加上要寫的 add 位元組會超過 LogMaxBytes 時，把它改名成 rotated（蓋掉舊的那份）。
func rotateIfLarge(path, rotated string, add int) {
	st, err := os.Stat(path)
	if err != nil || st.Size()+int64(add) <= LogMaxBytes {
		return
	}
	rotateMu.Lock()
	defer rotateMu.Unlock()
	if st, err = os.Stat(path); err != nil || st.Size()+int64(add) <= LogMaxBytes {
		return // 別的 writer 剛換過
	}
	_ = os.Rename(path, rotated)
}

// GuardServiceLogs 看服務的 stdout／stderr 是不是接在 dir 裡的 serve.out.log／serve.err.log（launchd 接的）；是而且超過上限，
// 就把內容複製到 <檔名>.1（蓋掉舊的）再清空原檔。只動「真的是我自己的輸出」的檔（比對 inode），在終端機跑、輸出接到別處時什麼都不做。
// launchd 用附加模式開這兩個檔，清空之後接著從頭寫。回清空了哪幾個檔。
func GuardServiceLogs(dir string) []string {
	var done []string
	for _, x := range []struct {
		f    *os.File
		name string
	}{{os.Stdout, "serve.out.log"}, {os.Stderr, "serve.err.log"}} {
		path := filepath.Join(dir, x.name)
		if guardOwnLog(x.f, path) {
			done = append(done, path)
		}
	}
	return done
}

func guardOwnLog(f *os.File, path string) bool {
	if f == nil {
		return false
	}
	mine, err := f.Stat()
	if err != nil || !mine.Mode().IsRegular() || mine.Size() <= LogMaxBytes {
		return false
	}
	onDisk, err := os.Stat(path)
	if err != nil || !os.SameFile(mine, onDisk) {
		return false
	}
	if !keepPreviousLog(path, mine.Size()) {
		// 上一份留不成（最常見：硬碟滿了，正是最需要清的時候）：不能因為留不了就不清。改成原地截斷，只留最後一小段。
		return truncateToTail(f, path, mine.Size())
	}
	if err := f.Truncate(0); err != nil {
		return false
	}
	_, _ = f.Seek(0, io.SeekStart) // 不是附加模式開的也從頭寫（附加模式下這行沒有作用）
	return true
}

// keepPreviousLog 把 path 的最後 LogMaxBytes 複製到 <path>.1（蓋掉舊的）。只留最後一段：舊版洗到幾 GB 的檔不整份複製
// （拖慢啟動、硬碟快滿時複製不完）。沒留成回 false，寫到一半的 .1 收掉（不佔空間）。
func keepPreviousLog(path string, size int64) bool {
	src, err := os.Open(path)
	if err != nil {
		return false
	}
	defer src.Close()
	dst, err := os.OpenFile(path+".1", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	if _, err := src.Seek(size-LogMaxBytes, io.SeekStart); err != nil {
		_ = dst.Close()
		_ = os.Remove(path + ".1") // 沒留成的半份不留
		return false
	}
	_, copyErr := io.Copy(dst, io.LimitReader(src, LogMaxBytes))
	if closeErr := dst.Close(); copyErr != nil || closeErr != nil {
		_ = os.Remove(path + ".1") // 沒留成的半份不留（硬碟滿時它佔的就是最缺的空間）
		return false
	}
	return true
}

// logTailKeep 是原地截斷時留在檔尾的大小上限（最多 256 KB，而且不超過上限的四分之一）：夠看最後發生什麼事，又一定放得下。
func logTailKeep() int64 {
	keep := int64(256 << 10)
	if q := LogMaxBytes / 4; q < keep {
		keep = q
	}
	return keep
}

// truncateToTail 把自己的輸出檔原地截斷、只留最後一小段（先讀進記憶體、清空、再寫回去）。尾段讀不到或寫不回去
// 也照樣清空：寧可少一段舊日誌，不要讓檔一直長。
func truncateToTail(f *os.File, path string, size int64) bool {
	var tail []byte
	if keep := logTailKeep(); keep > 0 {
		if src, err := os.Open(path); err == nil {
			buf := make([]byte, keep)
			if n, rerr := src.ReadAt(buf, size-keep); n > 0 && (rerr == nil || rerr == io.EOF) {
				tail = buf[:n]
				// 從完整的一行開始（第一個換行之後）；整段沒有換行就原樣留。
				if i := bytes.IndexByte(tail, '\n'); i >= 0 && i+1 < len(tail) {
					tail = tail[i+1:]
				}
			}
			_ = src.Close() // 只讀
		}
	}
	if err := f.Truncate(0); err != nil {
		return false
	}
	_, _ = f.Seek(0, io.SeekStart) // 同 guardOwnLog：不是附加模式開的也從頭寫
	if len(tail) > 0 {
		_, _ = f.Write(tail) // 寫不回去就算了，檔已經清空
	}
	return true
}

// WatchServiceLogs 啟動時看一次，之後每小時看一次，到 ctx 結束。serve 啟動時叫。
func WatchServiceLogs(ctx context.Context, dir string) {
	GuardServiceLogs(dir)
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				GuardServiceLogs(dir)
			}
		}
	}()
}
