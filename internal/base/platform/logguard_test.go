package platform

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type logClock struct{ at time.Time }

func (c *logClock) now() time.Time { return c.at }

func guardedLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("不是 JSON：%q", line)
		}
		out = append(out, m)
	}
	return out
}

// 同一句警告洗版（開發家一天半 5.8 萬行「接回對話失敗」，每 2 秒一行）：收斂成第一行＋之後每個視窗一行帶次數，
// 視窗加倍到一小時；不同的句子、不同的欄位、不同的模組各算各的；一般紀錄（INFO）不收斂。
// Guards: PLT-29
func TestRepeatGuardCollapsesFlood(t *testing.T) {
	var buf bytes.Buffer
	c := &logClock{at: time.Date(2026, 11, 5, 3, 42, 0, 0, time.UTC)}
	g := newRepeatGuard(slog.NewJSONHandler(&buf, nil), c.now)
	log := slog.New(g).With("module", "session")
	other := slog.New(g).With("module", "chat")

	// 一天半、每 2 秒一行
	for i := 0; i < 36*60*30; i++ {
		log.Warn("接回對話失敗", "error", "這個 session 已經在執行")
		c.at = c.at.Add(2 * time.Second)
	}
	lines := guardedLines(t, &buf)
	if len(lines) > 50 {
		t.Fatalf("6.4 萬次同一句要收斂到幾十行以內，寫了 %d 行", len(lines))
	}
	total := 0
	for i, m := range lines {
		total++
		if n, ok := m[RepeatedKey].(float64); ok {
			total += int(n)
		} else if i != 0 {
			t.Fatalf("第一行以後每一行都要帶次數：%v", m)
		}
	}
	if total < 36*60*30-1800 { // 最後一個視窗裡還沒寫出來的不算
		t.Fatalf("寫出來的次數要對得上：%d", total)
	}

	buf.Reset()
	log.Warn("接回對話失敗", "error", "別的原因")
	log.Warn("讀取對話記錄失敗", "error", "這個 session 已經在執行")
	other.Warn("接回對話失敗", "error", "這個 session 已經在執行")
	if got := guardedLines(t, &buf); len(got) != 3 {
		t.Fatalf("不同的欄位、標題、模組各算各的：%v", got)
	}
	buf.Reset()
	for i := 0; i < 5; i++ {
		log.Info("做完一輪", "session", "life")
	}
	if got := guardedLines(t, &buf); len(got) != 5 {
		t.Fatalf("一般紀錄不收斂：%d 行", len(got))
	}
	// 安靜超過一小時再發生：當成新的事，馬上寫、不帶次數
	buf.Reset()
	c.at = c.at.Add(2 * time.Hour)
	log.Warn("接回對話失敗", "error", "這個 session 已經在執行")
	if got := guardedLines(t, &buf); len(got) != 1 || got[0][RepeatedKey] != nil {
		t.Fatalf("安靜很久之後重新算：%v", got)
	}
}

// 種類很多也不會把記憶體吃光：超過上限就重新算。
func TestRepeatGuardBoundsKeys(t *testing.T) {
	c := &logClock{at: time.Unix(0, 0)}
	g := newRepeatGuard(slog.NewJSONHandler(&bytes.Buffer{}, nil), c.now)
	log := slog.New(g)
	for i := 0; i < repeatKeysMax*3; i++ {
		log.Warn("x", "n", i)
	}
	if n := len(g.state.seen); n > repeatKeysMax {
		t.Fatalf("記住的種類不能超過 %d：%d", repeatKeysMax, n)
	}
}

// 每日日誌的大小上限：超過就把當天那份改名成 <日期>.1.jsonl、開新的；兩份加起來不超過上限的兩倍。
// Guards: PLT-29
func TestDailyLogRotatesAtSizeCap(t *testing.T) {
	old := LogMaxBytes
	LogMaxBytes = 2000
	t.Cleanup(func() { LogMaxBytes = old })
	dir, _ := v2HomeDir(t)
	h, err := LoadHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := &dailyWriter{home: h, clock: SystemClock{}}
	line := []byte(strings.Repeat("x", 99) + "\n")
	for i := 0; i < 100; i++ { // 一萬位元組
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(h.Paths().Runtime("logs"), "*.jsonl"))
	if len(files) != 2 {
		t.Fatalf("要剛好兩份（當天的與輪替出來的）：%v", files)
	}
	var sum int64
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil || st.Size() > LogMaxBytes {
			t.Fatalf("%s 超過上限：%v %v", f, st.Size(), err)
		}
		sum += st.Size()
	}
	if !strings.HasSuffix(files[0], ".1.jsonl") && !strings.HasSuffix(files[1], ".1.jsonl") {
		t.Fatalf("輪替出來的要叫 <日期>.1.jsonl：%v", files)
	}
	if sum > 2*LogMaxBytes {
		t.Fatalf("兩份加起來不超過上限的兩倍：%d", sum)
	}
}

// 服務的輸出檔（launchd 接的那種：附加模式開著）：超過上限留一份到 .1、清空，之後接著從頭寫；
// 不是自己輸出的檔、沒超過上限的都不動。
// Guards: PLT-29
func TestGuardOwnLog(t *testing.T) {
	old := LogMaxBytes
	LogMaxBytes = 1000
	t.Cleanup(func() { LogMaxBytes = old })
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.err.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Repeat("a", 500)); err != nil {
		t.Fatal(err)
	}
	if guardOwnLog(f, path) {
		t.Fatal("沒超過上限不動")
	}
	if _, err := f.WriteString(strings.Repeat("b", 1500)); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(dir, "other.log")
	if err := os.WriteFile(elsewhere, []byte(strings.Repeat("c", 3000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if guardOwnLog(f, elsewhere) {
		t.Fatal("不是自己輸出的檔不動")
	}
	if !guardOwnLog(f, path) {
		t.Fatal("超過上限要輪替")
	}
	if _, err := f.WriteString("after\n"); err != nil {
		t.Fatal(err)
	}
	now, _ := os.ReadFile(path)
	prev, _ := os.ReadFile(path + ".1")
	// 上一份只留最後 LogMaxBytes（這裡是後面那 1000 個 b），不整份複製。
	if string(now) != "after\n" || string(prev) != strings.Repeat("b", 1000) {
		t.Fatalf("清空後從頭寫、上一份留最後一段：now=%q prev=%d", now, len(prev))
	}
	if got, _ := os.ReadFile(elsewhere); len(got) != 3000 {
		t.Fatal("別的檔被動到了")
	}
}

// 上一份留不成（硬碟滿了、.1 寫不進去）：不能因為留不了就不清——原地截斷、只留最後一小段（從完整的一行開始），之後接著寫。
// Guards: PLT-29
func TestGuardOwnLogTruncatesInPlaceWhenPreviousCopyCannotBeKept(t *testing.T) {
	old := LogMaxBytes
	LogMaxBytes = 1000
	t.Cleanup(func() { LogMaxBytes = old })
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.err.log")
	// <檔名>.1 是一個資料夾：上一份一定留不成（跟硬碟滿了寫不進去同一條路）。
	if err := os.Mkdir(path+".1", 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Repeat("old line\n", 600) + "last line 1\nlast line 2\n"); err != nil {
		t.Fatal(err)
	}
	if !guardOwnLog(f, path) {
		t.Fatal("上一份留不成也要清")
	}
	if _, err := f.WriteString("after\n"); err != nil {
		t.Fatal(err)
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(now)) > logTailKeep()+int64(len("after\n")) {
		t.Fatalf("截斷後只留一小段尾巴，還有 %d 位元組", len(now))
	}
	if !strings.HasSuffix(string(now), "last line 1\nlast line 2\nafter\n") || !strings.HasPrefix(string(now), "old line\n") {
		t.Fatalf("要留最後幾行（從完整的一行開始）再接著寫：%q", now)
	}
}
