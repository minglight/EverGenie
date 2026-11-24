package report

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// IssueURL 是「回報問題」要開的新 issue 頁（例 https://github.com/<owner>/<repo>/issues/new）。
// 還沒開放，先留空：空的時候看板只給「複製內容」，不給開網頁的按鈕（RPT-5）。要開放時只改這一行。
// 環境變數 EVERGENIE_REPORT_ISSUE_URL 可以蓋過（測試、開放前先試用）。
const IssueURL = ""

// issueURL 回現在生效的回報頁網址；只收 https（別的一律當沒設）。
func issueURL() string {
	u := strings.TrimSpace(sdk.Getenv("EVERGENIE_REPORT_ISSUE_URL"))
	if u == "" {
		u = IssueURL
	}
	if !strings.HasPrefix(u, "https://") {
		return ""
	}
	return u
}

const (
	logDays     = 3    // 錯誤標題看最近幾天
	maxTitles   = 30   // 最多列幾種錯誤
	detailLines = 40   // 詳細版每個檔最多帶幾行（一樣的連續幾行先收成一行）
	maxLinkBody = 5000 // 帶進網址的內文上限（字元）；太長瀏覽器與 GitHub 會拒收
	maxScan     = 8 << 20
)

// detailKeys 是詳細版日誌行留下的欄位（RPT-3）：只有說「哪裡、什麼狀況」的；沒列的（對話名、訊息內文 input／body／title、
// 誰傳給誰、編號…）整個欄位丟掉，不靠事後遮。
var detailKeys = []string{"time", "level", "module", "msg", "error", "reason", "kind", "state", "code", "status", "try", "repeated"}

// Options 是產生一份回報包要的東西。Checks 由呼叫的人先跑好（指令行程走 doctor.Collect、服務裡走 doctor 模組）。
type Options struct {
	Home    string
	Detail  bool
	Checks  []sdk.DoctorCheck
	Now     time.Time
	Private *Private // nil＝照這台與這個家讀（PrivateOf）；測試給假的
	OS      string   // 空＝問這台（sw_vers）
}

// Bundle 是一份回報包：標題一行、內文一段 Markdown。Link 是帶好標題與內文的回報頁網址（沒設定回報頁就是空的）。
type Bundle struct {
	Title string
	Body  string
	Link  string
}

// Build 產生回報包。只讀（日誌、家的設定），不寫任何東西、不連網。
func Build(ctx context.Context, o Options) Bundle {
	p := o.Private
	if p == nil {
		got := PrivateOf(o.Home)
		p = &got
	}
	red := NewRedactor(*p)
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	osName := o.OS
	if osName == "" {
		osName = osVersion(ctx)
	}
	var b strings.Builder
	b.WriteString("## 環境\n")
	fmt.Fprintf(&b, "- 版本：%s\n- 系統：%s\n- 產生時間：%s\n", platform.Version(), osName, now.Format("2006-01-02 15:04"))
	// 設定讀不起來：不確定遮得全，就少給（不帶日誌尾段與說明文字），並講明。
	detail := o.Detail && !p.ConfigUnread
	if p.ConfigUnread {
		b.WriteString("- 這個家的設定讀不起來：這份只帶檢查項目的代號與錯誤的標題，不帶說明文字與日誌\n")
	}

	bad, warn, ok := 0, 0, 0
	var lines []string
	for _, sev := range []sdk.DoctorSeverity{sdk.DoctorSeverityError, sdk.DoctorSeverityWarn} {
		for _, c := range o.Checks {
			if c.Severity != sev {
				continue
			}
			mark := "✗"
			if sev == sdk.DoctorSeverityWarn {
				mark = "!"
			}
			// 一般版只帶代號與嚴重度（RPT-1）：說明文字是各檢查自己組的句子，裡面可能有路徑、名字，只有詳細版才帶（遮過的）。
			if !detail {
				lines = append(lines, fmt.Sprintf("- %s %s", mark, c.ID))
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s %s（%s）", mark, oneLine(c.Detail), c.ID))
		}
	}
	for _, c := range o.Checks {
		switch c.Severity {
		case sdk.DoctorSeverityError:
			bad++
		case sdk.DoctorSeverityWarn:
			warn++
		default:
			ok++
		}
	}
	b.WriteString("\n## 健康檢查\n")
	fmt.Fprintf(&b, "要處理 %d 項、注意 %d 項、正常 %d 項\n", bad, warn, ok)
	for _, l := range lines {
		b.WriteString(l + "\n")
	}

	dir := ""
	if o.Home != "" {
		dir = platform.RuntimeDir(o.Home, "logs")
	}
	files := logFiles(dir, now)
	fmt.Fprintf(&b, "\n## 最近 %d 天的錯誤（只有標題與次數）\n", logDays)
	since := now.AddDate(0, 0, -(logDays - 1)).Format("2006-01-02")
	titles, other := countTitles(files, since)
	if len(titles) == 0 {
		b.WriteString("沒有錯誤紀錄。\n")
	}
	for i, t := range titles {
		if i == maxTitles {
			fmt.Fprintf(&b, "- 另外還有 %d 種，沒有列出\n", len(titles)-maxTitles)
			break
		}
		fmt.Fprintf(&b, "- %d 次 %s：%s（最後一次 %s）\n", t.n, t.module, oneLine(t.msg), t.last)
	}
	if other > 0 {
		fmt.Fprintf(&b, "- 另有 %d 行不是日誌格式（沒有帶內容）\n", other)
	}

	if detail {
		b.WriteString("\n## 詳細日誌（最後幾行；只留狀況欄位，私人內容已拿掉）\n")
		any := false
		for _, f := range files {
			// serve.err.log 裡日誌格式的行跟每日日誌是同一批，不重複帶；它只帶當掉的堆疊。
			tail := detailTail(f, since, strings.HasPrefix(filepath.Base(f), "serve."))
			if len(tail) == 0 {
				continue
			}
			any = true
			fmt.Fprintf(&b, "\n%s\n```\n%s\n```\n", filepath.Base(f), strings.Join(tail, "\n"))
		}
		if !any {
			b.WriteString("沒有可以帶的日誌。\n")
		}
	}
	b.WriteString("\n（這份是 " + platform.ProductName + " 產生的問題回報：已盡量拿掉私人內容，送出前請自己看一遍。）\n")

	title := fmt.Sprintf("問題回報：%s，%s", platform.Version(), osName)
	if bad > 0 {
		title += fmt.Sprintf("，健檢 %d 項要處理", bad)
	}
	out := Bundle{Title: red.Clean(title), Body: red.Clean(b.String())}
	out.Link = issueLink(issueURL(), out.Title, out.Body)
	return out
}

// issueLink 把標題與內文帶進回報頁的網址；內文太長就只帶前面一段並說明（完整的請用「複製內容」貼上）。
func issueLink(base, title, body string) string {
	if base == "" {
		return ""
	}
	if r := []rune(body); len(r) > maxLinkBody {
		body = string(r[:maxLinkBody]) + "\n\n（內容太長，後面沒帶進來：請回看板按「複製內容」，把完整的貼在這裡。）\n"
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "title=" + url.QueryEscape(title) + "&body=" + url.QueryEscape(body)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// osVersion 回「macOS 15.1（arm64）」；問不到版本就只寫系統與晶片。
func osVersion(ctx context.Context) string {
	name := runtime.GOOS
	if runtime.GOOS == "darwin" {
		name = "macOS"
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(c, "/usr/bin/sw_vers", "-productVersion").Output(); err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				name += " " + v
			}
		}
	}
	return name + "（" + runtime.GOARCH + "）"
}

var reDay = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(\.\d+)?\.jsonl$`)

// logFiles 回要看的日誌：最近 logDays 天的每日日誌（含輪替出來的那一份），加上服務的 serve.err.log 與它輪替的上一份。舊的在前。
func logFiles(dir string, now time.Time) []string {
	if dir == "" {
		return nil
	}
	since := now.AddDate(0, 0, -(logDays - 1)).Format("2006-01-02")
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if name := e.Name(); !e.IsDir() && reDay.MatchString(name) && name[:10] >= since {
			out = append(out, filepath.Join(dir, name))
		}
	}
	sort.Strings(out)
	for _, name := range []string{"serve.err.log.1", "serve.err.log"} {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out
}

type titleCount struct {
	module, msg, last string
	n                 int
}

// eachLine 一行一行讀檔；檔太大只讀最後 maxScan（回報包不該因為日誌洗版就卡住）。
func eachLine(path string, fn func(line []byte)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	skipFirst := false
	if st, err := f.Stat(); err == nil && st.Size() > maxScan {
		if _, err := f.Seek(st.Size()-maxScan, 0); err == nil {
			skipFirst = true // 第一行多半是從中間切開的
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if skipFirst {
			skipFirst = false
			continue
		}
		fn(sc.Bytes())
	}
}

// countTitles 數每一種警告與錯誤（模組＋標題）出現幾次。每日日誌與 serve.err.log 是同一批（核心兩邊都寫），
// 同一行（時間、模組、標題都一樣）只算一次。other 是 serve.err.log 裡不是日誌格式的行數。
// since（YYYY-MM-DD）之前的行不算：serve.err.log 不照日期分檔，裡面有更早的。
func countTitles(files []string, since string) (out []titleCount, other int) {
	counts := map[string]*titleCount{}
	seen := map[string]bool{}
	for _, f := range files {
		eachLine(f, func(line []byte) {
			if len(strings.TrimSpace(string(line))) == 0 {
				return
			}
			var r map[string]any
			if json.Unmarshal(line, &r) != nil {
				other++
				return
			}
			level, _ := r["level"].(string)
			if level != "WARN" && level != "ERROR" {
				return
			}
			module, _ := r["module"].(string)
			msg, _ := r["msg"].(string)
			at, _ := r["time"].(string)
			if at < since {
				return
			}
			id := at + "|" + module + "|" + msg
			if seen[id] {
				return
			}
			seen[id] = true
			n := 1
			if rep, ok := r["repeated"].(float64); ok && rep > 0 {
				n += int(rep) // 收斂掉的重複（platform 的 dedupe）也算進次數
			}
			key := module + "|" + msg
			c := counts[key]
			if c == nil {
				c = &titleCount{module: module, msg: msg}
				counts[key] = c
			}
			c.n += n
			if len(at) >= 16 && at > c.last {
				c.last = at
			}
		})
	}
	for _, c := range counts {
		if len(c.last) >= 16 {
			c.last = strings.Replace(c.last[5:16], "T", " ", 1)
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].module+out[i].msg < out[j].module+out[j].msg
	})
	return out, other
}

// reCrash 認得出是程式當掉的行（panic、堆疊）：不是日誌格式的行只帶這種，其他的不知道裡面是什麼，不帶。
var reCrash = regexp.MustCompile(`^(panic:|fatal error:|goroutine \d+ \[|\s+\S+\.go:\d+|created by [A-Za-z0-9_./*()-]+|[A-Za-z0-9_./*()\[\]{}-]+\.[A-Za-z0-9_.()*\[\]{}-]+\([0-9a-fx?,. {}\[\]]*\)$)`)

// rePanicType 是 panic 行裡的型別（`panic: (pkg.Type) …`、`panic: main.T{…}`、`panic: &errors.errorString{…}`）。
var rePanicType = regexp.MustCompile(`^\(?[&*]*([A-Za-z0-9_./\[\]*]+\.[A-Za-z0-9_\[\]*]+)\)?`)

// crashLine：`panic:` 那一行只留型別（RPT-3）——panic 的訊息是程式自己組的句子，裡面可能有檔名、名字、內容。
// 執行環境自己丟的（`runtime error: …`，句子是固定的）整行留；別的只留認得出的型別，認不出就只寫「內容沒有帶」。
// 位置在下面的堆疊行（函式名、檔名與行號），照樣帶。
func crashLine(s string) string {
	rest, ok := strings.CutPrefix(s, "panic:")
	if !ok {
		return s
	}
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "runtime error:") {
		return s
	}
	if m := rePanicType.FindStringSubmatch(rest); m != nil {
		return "panic: " + m[1] + "（內容沒有帶）"
	}
	return "panic:（內容沒有帶）"
}

// detailTail 回一個日誌檔最後 detailLines 行、只留 detailKeys 的版本（RPT-3）。since 之前的日誌行不帶；
// 連續一樣的行（不看時間與第幾次）收成一行加「同樣的又 N 行」，洗版的那一句才不會把別的擠掉。
func detailTail(path, since string, crashOnly bool) []string {
	type row struct {
		text, same string
		more       int
	}
	var keep []row
	add := func(text, same string) {
		if n := len(keep); n > 0 && keep[n-1].same == same {
			keep[n-1].more++
			return
		}
		keep = append(keep, row{text: text, same: same})
		if len(keep) > 2*detailLines {
			keep = append(keep[:0], keep[len(keep)-detailLines:]...)
		}
	}
	eachLine(path, func(line []byte) {
		var r map[string]any
		if json.Unmarshal(line, &r) != nil {
			if s := strings.TrimRight(string(line), " \t\r"); reCrash.MatchString(s) {
				s = crashLine(s)
				add(s, s)
			}
			return
		}
		if at, _ := r["time"].(string); crashOnly || at < since {
			return
		}
		var parts, same []string
		for _, k := range detailKeys {
			if v, ok := r[k]; ok {
				part := fmt.Sprintf("%s=%s", k, oneLine(fmt.Sprint(v)))
				parts = append(parts, part)
				if k != "time" && k != "try" && k != "repeated" {
					same = append(same, part)
				}
			}
		}
		if len(parts) > 0 {
			add(strings.Join(parts, " "), strings.Join(same, " "))
		}
	})
	if len(keep) > detailLines {
		keep = keep[len(keep)-detailLines:]
	}
	out := make([]string, 0, len(keep))
	for _, k := range keep {
		if k.more > 0 {
			k.text += fmt.Sprintf("（同樣的又 %d 行）", k.more)
		}
		out = append(out, k.text)
	}
	return out
}
