package report

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// 假的私人字：故意挑會出現在路徑、帳號、電腦名、訊息內文裡的各一種。
const (
	fakeUser    = "wangxiaoming"
	fakeHost    = "Xiaomings-MacBook-Pro"
	fakeAccount = "mingpersonal"
	fakeSecret  = "下週三要跟陳醫師複診的事不要告訴我媽"
	fakeMail    = "xiaoming@example.com"
	fakeTailnet = "genie-main.example.ts.net"
)

// fakeHome 建一個假的家：home.toml 有帳號、機器名、看板網址；日誌裡每一種私人字都放一次。回家的路徑與 Private。
func fakeHome(t *testing.T, now time.Time) (string, *Private) {
	t.Helper()
	userHome := filepath.Join(t.TempDir(), "Users", fakeUser)
	home := filepath.Join(userHome, "evergenie-"+fakeUser)
	logs := filepath.Join(home, "runtime", "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	day := now.Format("2006-01-02")
	at := now.Format("2006-01-02T15:04:05") + "+08:00"
	lines := []string{
		`{"time":"` + at + `","level":"INFO","msg":"收到傳話但找不到對應的封包","module":"session","session":"life","from":"` + fakeAccount + `","input":"` + fakeSecret + `"}`,
		`{"time":"` + at + `","level":"WARN","msg":"接回對話失敗","module":"session","error":"open ` + home + `/vault/x.md: no such file","body":"` + fakeSecret + `","title":"` + fakeSecret + `"}`,
		`{"time":"` + at + `","level":"WARN","msg":"備份送不出去","module":"backup","peer":"` + fakeHost + `","error":"dial ` + fakeTailnet + `:443 as ` + fakeMail + ` from ` + fakeHost + `.local account ` + fakeAccount + ` at ` + userHome + `/.ai-` + fakeAccount + `"}`,
		`{"time":"` + at + `","level":"ERROR","msg":"request failed","module":"chat","error":"/Users/` + fakeUser + `/secret.txt 讀不到（` + fakeUser + `）"}`,
	}
	jsonl := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(logs, day+".jsonl"), []byte(jsonl), 0o600); err != nil {
		t.Fatal(err)
	}
	// serve.err.log：同一批日誌再一份（核心兩邊都寫），加上不是日誌格式的行（引擎的輸出、當掉的堆疊）。
	errLog := jsonl + "life：" + fakeSecret + "\n" + "panic: runtime error: index out of range\n" + "goroutine 12 [running]:\n" +
		"main.run(0x1400, 0x2)\n\t" + userHome + "/src/evergenie/main.go:42 +0x1c\n"
	if err := os.WriteFile(filepath.Join(logs, "serve.err.log"), []byte(errLog), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &Private{
		UserHome: userHome, Username: fakeUser, Hosts: []string{fakeHost, fakeHost + ".local"}, Home: home,
		Accounts: []string{fakeAccount, userHome + "/.ai-" + fakeAccount}, Names: []string{"王小明", "https://" + fakeTailnet},
	}
	return home, p
}

func fakeChecks(home string) []sdk.DoctorCheck {
	return []sdk.DoctorCheck{
		{ID: "config.home", Group: sdk.DoctorGroupConfig, Ok: true, Severity: sdk.DoctorSeverityOk, Detail: "home.toml 讀得起來：" + home},
		{ID: "config.launchd", Group: sdk.DoctorGroupConfig, Severity: sdk.DoctorSeverityWarn, Detail: "serve 的 launchd 服務管的是另一個家：" + home + "-old", Fix: "evergenie service install --home " + home},
		{ID: "sessions.resident", Group: sdk.DoctorGroupSessions, Severity: sdk.DoctorSeverityError, Detail: "王小明 的帳號 " + fakeAccount + " 在 " + fakeHost + " 沒登入"},
	}
}

func privateWords(home string) []string {
	return []string{fakeUser, fakeHost, fakeAccount, fakeSecret, "複診", "陳醫師", fakeMail, fakeTailnet, "ts.net", "王小明", home, "/Users/" + fakeUser}
}

// 回報包遮得乾淨（RPT-2、RPT-3）：假日誌與健檢裡放了登入名、家目錄路徑、電腦名、帳號名、信箱、Tailscale 網址、訊息內文，
// 一般版與詳細版的標題、內文、回報頁網址裡都找不到。
func TestBundleHasNoPrivateStrings(t *testing.T) {
	now := time.Date(2026, 11, 16, 20, 30, 0, 0, time.FixedZone("TST", 8*3600))
	home, p := fakeHome(t, now)
	t.Setenv("EVERGENIE_REPORT_ISSUE_URL", "https://example.invalid/issues/new")
	for _, detail := range []bool{false, true} {
		b := Build(context.Background(), Options{Home: home, Detail: detail, Checks: fakeChecks(home), Now: now, Private: p, OS: "macOS 15.1（arm64）"})
		link, _ := urlDecode(b.Link)
		all := b.Title + "\n" + b.Body + "\n" + link
		for _, w := range privateWords(home) {
			if strings.Contains(all, w) {
				t.Errorf("detail=%v：回報包裡出現私人字 %q：\n%s", detail, w, all)
			}
		}
	}
}

// 一般版只有版本、系統、健檢結果、錯誤的標題與次數（RPT-1）：沒有日誌原文行；同一行在每日日誌與 serve.err.log 各一份只算一次。
func TestBundleDefaultContents(t *testing.T) {
	now := time.Date(2026, 11, 16, 20, 30, 0, 0, time.FixedZone("TST", 8*3600))
	home, p := fakeHome(t, now)
	b := Build(context.Background(), Options{Home: home, Checks: fakeChecks(home), Now: now, Private: p, OS: "macOS 15.1（arm64）"})
	for _, want := range []string{
		"- 系統：macOS 15.1（arm64）", "要處理 1 項、注意 1 項、正常 1 項",
		"- ! config.launchd\n", "- ✗ sessions.resident\n",
		"- 1 次 session：接回對話失敗（最後一次 11-16 20:30）", "- 1 次 chat：request failed", "- 1 次 backup：備份送不出去",
		"另有 5 行不是日誌格式（沒有帶內容）",
	} {
		if !strings.Contains(b.Body, want) {
			t.Errorf("一般版少了 %q：\n%s", want, b.Body)
		}
	}
	// 健檢只帶代號與嚴重度，說明文字（各檢查自己組的句子）一般版不帶。
	for _, not := range []string{"詳細日誌", "level=", "收到傳話", "evergenie service install", "管的是另一個家", "沒登入", "都不在裡面"} {
		if strings.Contains(b.Body, not) {
			t.Errorf("一般版不該有 %q：\n%s", not, b.Body)
		}
	}
	if !strings.Contains(b.Body, "已盡量拿掉私人內容，送出前請自己看一遍") {
		t.Errorf("包尾要請主人送出前自己看一遍：\n%s", b.Body)
	}
	if !strings.HasPrefix(b.Title, "問題回報：") || !strings.Contains(b.Title, "健檢 1 項要處理") {
		t.Errorf("標題不對：%q", b.Title)
	}
	if b.Link != "" {
		t.Errorf("沒設定回報頁時不該有網址：%q", b.Link)
	}
}

// 詳細版多帶日誌尾段，但只留狀況欄位（RPT-3）：內文欄位（input、body、title）、對話名、對象整欄丟掉；
// 不是日誌格式的行只帶認得出是當掉的（panic、堆疊），其他不帶。
func TestBundleDetailKeepsOnlySafeFields(t *testing.T) {
	now := time.Date(2026, 11, 16, 20, 30, 0, 0, time.FixedZone("TST", 8*3600))
	home, p := fakeHome(t, now)
	b := Build(context.Background(), Options{Home: home, Detail: true, Checks: fakeChecks(home), Now: now, Private: p, OS: "macOS"})
	for _, want := range []string{
		"level=INFO module=session msg=收到傳話但找不到對應的封包",
		"msg=接回對話失敗 error=open <家>/vault/<檔>: no such file",
		"- ! serve 的 launchd 服務管的是另一個家：<家>-old（config.launchd）", // 詳細版才帶健檢的說明（遮過的）
		"panic: runtime error: index out of range", "goroutine 12 [running]:", "main.run(0x1400, 0x2)", "~/src/evergenie/main.go:42",
	} {
		if !strings.Contains(b.Body, want) {
			t.Errorf("詳細版少了 %q：\n%s", want, b.Body)
		}
	}
	for _, not := range []string{"input=", "body=", "title=", "session=", "from=", "peer=", "life："} {
		if strings.Contains(b.Body, not) {
			t.Errorf("詳細版不該有 %q：\n%s", not, b.Body)
		}
	}
}

// 回報頁網址（RPT-5）：設了才有，帶標題與內文；內文太長只帶前面並說明；不是 https 的當沒設。
func TestIssueLink(t *testing.T) {
	if got := issueLink("", "t", "b"); got != "" {
		t.Errorf("沒設定時要空的：%q", got)
	}
	got := issueLink("https://example.invalid/issues/new", "標題 1", "內文\n第二行")
	if got != "https://example.invalid/issues/new?title=%E6%A8%99%E9%A1%8C+1&body=%E5%85%A7%E6%96%87%0A%E7%AC%AC%E4%BA%8C%E8%A1%8C" {
		t.Errorf("網址不對：%q", got)
	}
	long, _ := urlDecode(issueLink("https://example.invalid/new?labels=bug", "t", strings.Repeat("長", maxLinkBody+10)))
	if !strings.HasPrefix(long, "https://example.invalid/new?labels=bug&title=t&body=") || !strings.Contains(long, "內容太長") || strings.Count(long, "長") != maxLinkBody+1 {
		t.Errorf("太長的內文沒有截斷並說明：%d 個字", strings.Count(long, "長"))
	}
	t.Setenv("EVERGENIE_REPORT_ISSUE_URL", "http://example.invalid/new")
	if issueURL() != "" {
		t.Error("不是 https 的回報頁要當沒設")
	}
	t.Setenv("EVERGENIE_REPORT_ISSUE_URL", "")
	if issueURL() != IssueURL {
		t.Error("沒有環境變數時用 IssueURL")
	}
}

// 登入名是產品名的一部分時（ever ↔ EverGenie）不會把產品名也換掉；緊鄰的兩個都換得到。
func TestRedactorWordBoundary(t *testing.T) {
	r := NewRedactor(Private{Username: "ever", UserHome: "/Users/ever", Hosts: []string{"ever-mac"}})
	got := r.Clean("EverGenie 在 /Users/ever/evergenie-ever 跑，ever ever 用 ever-mac；別人的 /Users/amy/x、192.0.2.5、127.0.0.1")
	want := "EverGenie 在 ~/evergenie-<使用者> 跑，<使用者> <使用者> 用 <電腦>；別人的 /Users/<使用者>/x、<IP>、127.0.0.1"
	if got != want {
		t.Errorf("\n得到 %s\n要是 %s", got, want)
	}
}

// PrivateOf 真的從家的設定抓得到帳號名（RPT-2）：鍵名改了、抓不到，這條會失敗。
func TestPrivateOfReadsAccountsFromHome(t *testing.T) {
	h := homefixture.New(t)
	data, err := os.ReadFile(filepath.Join(h.Dir, "home.toml"))
	if err != nil {
		t.Fatal(err)
	}
	p := PrivateOf(h.Dir)
	if p.ConfigUnread {
		t.Fatal("測試用的家設定讀得起來")
	}
	found := 0
	for _, line := range strings.Split(string(data), "\n") {
		if m := reTomlTable.FindStringSubmatch(line); m != nil && m[1] != "default" {
			found++
			if !contains(p.Accounts, m[1]) {
				t.Errorf("帳號 %s 沒進遮蔽表：%v", m[1], p.Accounts)
			}
		}
	}
	if found == 0 {
		t.Fatalf("測試用的家要有帳號：\n%s", data)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// 設定壞掉（最需要回報的時候）也遮得住（RPT-2）：帳號名、登入資料夾、電腦名、主人的名字從設定檔逐行撈；
// 回報包只出一般版——健檢只列代號、不帶日誌尾段——並講明。
func TestBrokenHomeStillRedactsAndGivesLess(t *testing.T) {
	now := time.Date(2026, 11, 16, 20, 30, 0, 0, time.FixedZone("TST", 8*3600))
	home, _ := fakeHome(t, now)
	toml := "[user]\nname = \"王小明\"\n[runtime]\nmachine = \"" + fakeHost + "\"\n[accounts." + fakeAccount + "]\nengine = \"x\"\n[accounts." + fakeAccount + ".engine_config]\nconfig_dir = \"/opt/ai-store/" + fakeAccount + "-dir\"\n這一行壞了 = = =\n"
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	p := PrivateOf(home)
	if !p.ConfigUnread || !contains(p.Accounts, fakeAccount) || !contains(p.Hosts, fakeHost) || !contains(p.Names, "王小明") {
		t.Fatalf("壞掉的設定也要撈得到：%+v", p)
	}
	b := Build(context.Background(), Options{Home: home, Detail: true, Checks: fakeChecks(home), Now: now, OS: "macOS 15.1（arm64）"})
	for _, w := range []string{fakeHost, fakeAccount, "王小明", fakeSecret, "/opt/ai-store", "## 詳細日誌", "沒登入"} {
		if strings.Contains(b.Title+b.Body, w) {
			t.Errorf("設定讀不起來的回報包不該有 %q：\n%s", w, b.Body)
		}
	}
	for _, w := range []string{"設定讀不起來", "sessions.resident", "要處理 1 項"} {
		if !strings.Contains(b.Body, w) {
			t.Errorf("少了 %q：\n%s", w, b.Body)
		}
	}
}

// 遮蔽的幾種變體（RPT-2）：大小寫不同的電腦名與登入名、登入名開頭的信箱（網域也不留）、網址編碼過的家目錄、vault 裡的檔名。
func TestRedactorVariants(t *testing.T) {
	r := NewRedactor(Private{UserHome: "/Users/" + fakeUser, Username: fakeUser, Hosts: []string{fakeHost}, Home: "/Users/" + fakeUser + "/genie"})
	in := "dial " + strings.ToLower(fakeHost) + " as " + strings.ToUpper(fakeUser) + " mail " + fakeUser + "@corp.example.com " +
		"q=%2FUsers%2F" + fakeUser + "%2Fx open /Users/" + fakeUser + "/genie/vault/notes/人/陳醫師.md: no such file " + fakeUser + "genie"
	out := r.Clean(in)
	for _, w := range []string{strings.ToLower(fakeHost), strings.ToUpper(fakeUser), "corp.example.com", "%2F" + fakeUser, "陳醫師", "notes"} {
		if strings.Contains(out, w) {
			t.Errorf("還看得到 %q：%s", w, out)
		}
	}
	// 名字只在前後不是英數字時換：產品名這種「登入名＋別的字」不會被換壞。
	if !strings.Contains(out, fakeUser+"genie") || !strings.Contains(out, "<家>/vault/<檔>: no such file") {
		t.Errorf("換過頭或沒換到：%s", out)
	}
}

// 遮蔽補強（RPT-2）：media／personas／packages/mine 裡的檔名與外接硬碟的路徑只留到資料夾為止（堆疊行的程式檔名不動）；
// IPv6 位址遮掉（本機 ::1 留著；時間、行號、`a::b` 這種不是位址的不動）。
func TestRedactorOwnFoldersVolumesAndIPv6(t *testing.T) {
	r := NewRedactor(Private{UserHome: "/Users/" + fakeUser, Username: fakeUser, Home: "/Users/" + fakeUser + "/genie"})
	for in, want := range map[string]string{
		"open /Users/" + fakeUser + "/genie/media/陳醫師的照片.jpg: no such file": "open <家>/media/<檔>: no such file",
		"讀不到 personas/陳醫師/card.md":                                          "讀不到 personas/<檔>",
		"error=open packages/mine/複診提醒/package.toml: permission denied":     "error=open packages/mine/<檔>: permission denied",
		"copy to /Volumes/陳醫師的 硬碟/備份/我的家.egbackup: input/output error":      "copy to /Volumes/<磁碟>: input/output error",
		"\t/src/internal/feature/agents/personas/clash.go:42 +0x1c":         "\t/src/internal/feature/agents/personas/clash.go:42 +0x1c",
		"dial tcp [2001:db8::1234]:443: refused":                            "dial tcp [<IP>]:443: refused",
		"from fe80::1c2d:3e4f:5a6b:7c8d: timeout，本機 ::1 與 [::1]:8850":       "from <IP>: timeout，本機 ::1 與 [::1]:8850",
		"2001:db8:0:0:0:0:2:1 與 ::ffff:192.0.2.5":                           "<IP> 與 <IP>",
		"time=2026-11-16T20:30:05+08:00 at 20:30:05 main.go:42 std::foo":    "time=2026-11-16T20:30:05+08:00 at 20:30:05 main.go:42 std::foo",
	} {
		if got := r.Clean(in); got != want {
			t.Errorf("\n輸入 %s\n得到 %s\n要是 %s", in, got, want)
		}
	}
}

// panic 那一行只留型別與位置（RPT-3）：訊息是程式自己組的句子（可能有檔名、內容），不帶；執行環境的固定句子整行留；堆疊行照帶。
func TestCrashLineKeepsOnlyTheType(t *testing.T) {
	for in, want := range map[string]string{
		"panic: runtime error: index out of range [5] with length 3": "panic: runtime error: index out of range [5] with length 3",
		"panic: 讀不到 " + fakeSecret:                                   "panic:（內容沒有帶）",
		"panic: (main.noteError) {" + fakeSecret + "}":               "panic: main.noteError（內容沒有帶）",
		"panic: &errors.errorString{s:\"" + fakeSecret + "\"}":       "panic: errors.errorString（內容沒有帶）",
		"panic: open /tmp/x: no such file [recovered]":               "panic:（內容沒有帶）",
		"goroutine 12 [running]:":                                    "goroutine 12 [running]:",
		"fatal error: concurrent map writes":                         "fatal error: concurrent map writes",
	} {
		if got := crashLine(in); got != want {
			t.Errorf("%q → %q，要是 %q", in, got, want)
		}
	}
	// 整包：panic 帶著私人內容時，詳細版只看得到型別與下面的位置。
	now := time.Date(2026, 11, 16, 20, 30, 0, 0, time.FixedZone("TST", 8*3600))
	home, p := fakeHome(t, now)
	log := "panic: 寫不進 " + fakeSecret + "\ngoroutine 7 [running]:\nmain.save(0x1400)\n\t/src/main.go:88 +0x1c\n"
	if err := os.WriteFile(filepath.Join(home, "runtime", "logs", "serve.err.log"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	b := Build(context.Background(), Options{Home: home, Detail: true, Checks: fakeChecks(home), Now: now, Private: p, OS: "macOS"})
	if strings.Contains(b.Body, "複診") || !strings.Contains(b.Body, "panic:（內容沒有帶）") || !strings.Contains(b.Body, "/src/main.go:88") {
		t.Errorf("panic 行只留型別與位置：\n%s", b.Body)
	}
}
