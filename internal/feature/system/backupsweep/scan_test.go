package backupsweep

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/engine/registry"
	"github.com/minglight/evergenie/sdk"
)

// TestLooksLikeBackupOnlyMatchesCopies：只認「某個東西的複本」（.bak、.backup 結尾、<原名>-backup-<日期>）；
// 名字裡剛好有 backup 的程式、紀錄、放備份的資料夾不認（2026-11-21 實測：28 筆不合規大多是這種誤報）。
func TestLooksLikeBackupOnlyMatchesCopies(t *testing.T) {
	yes := []string{"x.bak", "x.BAK", "x.bak2", "x.bak-20261101-why", "x.bak.tgz", "settings.json.backup", "db.backup2", "x.json.backup.1", "foo.backup.tgz", "foo.backup-20260101",
		"data-backup-20261020", "data_backup_2026-10-20.tgz", "site.backups-20261020"}
	no := []string{"backup-list.txt", "icloud-backup", "icloud-backup.log", "backups", "backup", "upgrade-backup", "migrate-backups",
		"950-0339-rtc-rv3028-Add-backup-switchover-mode-support.patch", "FeedbackUploadParams.json", "backups-2026-10-30.md",
		"2026-11-09-owner-backup", "x.bakery.js", "backup.go", "foo.bakup", ".backup", "backup-20261020", "-backup-20261020",
		"icloud-backup-2026-10-30.log", "daily-backup-20261030.log"}
	for _, n := range yes {
		if !looksLikeBackup(n) {
			t.Errorf("%s 應該算備份", n)
		}
	}
	for _, n := range no {
		if looksLikeBackup(n) {
			t.Errorf("%s 不該算備份", n)
		}
	}
}

// TestParseNameFollowsBackupNamingRule：照備份規則（system 卡 BSW-4）的命名拆日期、-HHMM、原因、-until；不合規的說得出哪裡不對。
func TestParseNameFollowsBackupNamingRule(t *testing.T) {
	cases := []struct {
		name, expires, problem string
	}{
		{"home.toml.bak-20261101-tailscale-services", "2026-12-01", ""},
		{"tasks.bak-20261020-1530-cleanup", "2026-11-19", ""},
		{"a.toml.bak-20261001-keep-until20270101", "2027-01-01", ""},
		{"x.bak", "", "沒有日期"},
		{"x.bak-20261101", "", "沒寫原因"},
		{"x.bak-20261101-1200", "", "沒寫原因"},
		{"x.bak-20261340-oops", "", "日期不對"},
		{"data-backup-20261020", "", "名字沒照"},
		{"data.bak-20261119-vd-upgrade.tgz", "2026-12-19", ""},
		{"data.bak-20261106-merge-main.tar.gz", "2026-12-06", ""},
		{"home.bak-20261101-v0.1.0", "2026-12-01", ""},
		{"settings.json.bak-20260527-000918", "", "沒寫原因"},
		{"x.bak-20261101-000918.json", "", "沒寫原因"},
		{"x.bak-20261101-1200.tgz", "", "沒寫原因"},
		{"x.bak-20261101-1200-why.tgz", "2026-12-01", ""},
		{"x.bak-20261101-why-until20270101.tgz", "2027-01-01", ""},
		{"x.bak-20261101-v0.1.0-until20261215", "2026-12-15", ""},
	}
	for _, c := range cases {
		p, problem := parseName(c.name, taipei)
		if c.problem != "" {
			if !strings.Contains(problem, c.problem) {
				t.Errorf("%s：要說「%s」，得到「%s」", c.name, c.problem, problem)
			}
			continue
		}
		if problem != "" || p.expires.Format("2006-01-02") != c.expires {
			t.Errorf("%s：留到 %s（%s），要 %s", c.name, p.expires.Format("2006-01-02"), problem, c.expires)
		}
	}
}

// TestMentionsNeedsPathBoundary：/a/bc 不算寫到 /a/b；寫到裡面的東西（/a/b/x）算。
func TestMentionsNeedsPathBoundary(t *testing.T) {
	if mentions("<string>/w/amy/foo-old</string>", "/w/amy/foo") {
		t.Error("/w/amy/foo-old 不是 /w/amy/foo")
	}
	if !mentions("<string>/w/amy/foo/run.sh</string>", "/w/amy/foo") || !mentions("cd /w/amy/foo && x", "/w/amy/foo") {
		t.Error("寫到資料夾裡的東西要算")
	}
}

// TestSweepReportClassifiesExpiredMisnamedCurrentAndSkipped：一次掃描分得出已到期（查過沒人指到＝可刪）、名字不合規（附建議）、
// 期限內（只算數量）、略過的例外（內建的與主人設定檔加的）。
func TestSweepReportClassifiesExpiredMisnamedCurrentAndSkipped(t *testing.T) {
	user := fakeUser(t)
	home := filepath.Join(user, "Workspace", "amy", "home")
	ws := filepath.Join(user, "Workspace")
	put(t, filepath.Join(home, "home.toml"), testNow)
	put(t, filepath.Join(ws, "proj", "conf", "a.toml.bak-20261001-before-x"), testNow) // 過了 30 天
	put(t, filepath.Join(ws, "proj", "conf", "b.toml.bak-20261115-before-y"), testNow) // 期限內
	put(t, filepath.Join(ws, "proj", "notes.bak"), testNow.AddDate(0, -1, 0))          // 沒日期
	put(t, filepath.Join(ws, "proj", "old-backup-20261020", "x.txt"), testNow)         // 資料夾整個算一項
	put(t, filepath.Join(ws, "proj", "keep-backup-20261020", "x.txt"), testNow)        // 主人加的例外
	put(t, filepath.Join(home, "runtime", "backups", "trust", "1.toml"), testNow)      // 這個家自己的備份資料夾
	put(t, filepath.Join(ws, "proj", "node_modules", "pkg", "x.bak"), testNow)         // 不往套件管理器的資料夾裡走
	if err := os.MkdirAll(filepath.Join(home, "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "settings", "backup-sweep.toml"), []byte("[[skip]]\npath = \"~/Workspace/proj/keep-*\"\nreason = \"要留著的範例\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 內建的例外：平台帳號資料夾裡工具自己輪替的備份（名字照 engine/registry 的寫法造，測試裡不寫平台名稱）。
	account := strings.Replace(strings.TrimPrefix(registry.SweepSkips()[0].Path, "~/"), "*", "-test", 1)
	put(t, filepath.Join(user, account, "x.json.backup.1"), testNow)
	// 這個家自己的資料與附件（家不是 git 工作目錄時）：名字有 backup 也不是備份。
	put(t, filepath.Join(home, "vault", "jobs", "backup-sweep.md"), testNow)
	put(t, filepath.Join(home, "runtime", "share", "x-backup-sweep-規格.md"), testNow)

	rep := Scan(context.Background(), testConfig(t, user, home, noRefs), testNow, taipei)
	if len(rep.Errors) > 0 {
		t.Fatalf("不該出錯：%v", rep.Errors)
	}
	exp, ok := byPath(rep.Expired, "~/Workspace/proj/conf/a.toml.bak-20261001-before-x")
	if !ok || exp.State != sdk.BackupSweepExpired || exp.Checked != "查過：沒有開機服務、執行中的程式、終端機視窗用到它" || exp.Date != "2026-10-01" || exp.Expires != "2026-10-31" {
		t.Fatalf("已到期可刪：%+v（全部 %+v）", exp, rep.Expired)
	}
	if len(rep.Expired) != 1 {
		t.Fatalf("已到期只有一個：%+v", rep.Expired)
	}
	mis, ok := byPath(rep.Misnamed, "~/Workspace/proj/notes.bak")
	if !ok || !strings.Contains(mis.Problem, "沒有日期") || !strings.Contains(mis.Suggest, "notes.bak-20261022-<原因>") {
		t.Fatalf("不合規：%+v", rep.Misnamed)
	}
	if dir, ok := byPath(rep.Misnamed, "~/Workspace/proj/old-backup-20261020"); !ok || !dir.Dir {
		t.Fatalf("名字像備份的資料夾整個算一項：%+v", rep.Misnamed)
	}
	if len(rep.Misnamed) != 2 {
		t.Fatalf("不合規只有兩個（node_modules 裡的不算）：%+v", rep.Misnamed)
	}
	if rep.Current != 1 {
		t.Fatalf("期限內 %d 個，要 1", rep.Current)
	}
	for _, want := range []string{"~/Workspace/proj/keep-backup-20261020", "~/Workspace/amy/home/runtime/backups", "~/" + filepath.ToSlash(account)} {
		if _, ok := byPath(rep.Skipped, want); !ok {
			t.Errorf("略過的例外少了 %s：%+v", want, rep.Skipped)
		}
	}
	if keep, _ := byPath(rep.Skipped, "~/Workspace/proj/keep-backup-20261020"); !strings.HasPrefix(keep.Reason, "要留著的範例") {
		t.Errorf("主人加的例外要帶他寫的原因：%+v", keep)
	}
	md := Markdown(rep, taipei)
	for _, want := range []string{"## 已到期", "### 可刪（1）", "## 名字不合規", "## 期限內", "## 略過的例外", "a.toml.bak-20261001-before-x", "查過：沒有開機服務、執行中的程式、終端機視窗用到它"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown 少了「%s」", want)
		}
	}
}

// TestSweepMarksBackupInUseWhenLaunchAgentPointsToParent：開機服務的設定指到備份的上層資料夾，報告標「在用」、不列可刪。
func TestSweepMarksBackupInUseWhenLaunchAgentPointsToParent(t *testing.T) {
	user := fakeUser(t)
	home := filepath.Join(user, "Workspace", "home")
	put(t, filepath.Join(home, "home.toml"), testNow)
	svc := filepath.Join(user, "Workspace", "svc")
	put(t, filepath.Join(svc, "snap.bak-20260901-before-move"), testNow)
	agents := filepath.Join(user, "Library", "LaunchAgents")
	t.Setenv("EVERGENIE_LAUNCH_AGENTS_DIR", agents)
	put(t, filepath.Join(agents, "com.example.svc.plist"), testNow)
	plist := "<plist><dict><key>WorkingDirectory</key><string>" + svc + "</string><key>ProgramArguments</key><array><string>" + svc + "/run.sh</string></array></dict></plist>"
	if err := os.WriteFile(filepath.Join(agents, "com.example.svc.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	// 開機服務設定那一種照真的讀檔；跑指令的兩種換成找不到東西。
	probe := func(c Check) ([]Ref, error) {
		if c.Files != "" {
			return runCheck(context.Background(), c)
		}
		return nil, nil
	}
	rep := Scan(context.Background(), testConfig(t, user, home, probe), testNow, taipei)
	it, ok := byPath(rep.Expired, "~/Workspace/svc/snap.bak-20260901-before-move")
	if !ok || it.State != sdk.BackupSweepInUse || !strings.HasPrefix(it.UsedBy, "開機服務：com.example.svc.plist") || !strings.Contains(it.UsedBy, "上層資料夾") || it.Checked != "" {
		t.Fatalf("要標在用：%+v", rep.Expired)
	}
	del, used, _ := Counts(rep)
	if del != 0 || used != 1 {
		t.Fatalf("可刪 %d、在用 %d", del, used)
	}
	md := Markdown(rep, taipei)
	if !strings.Contains(md, "### 可刪（0）") || !strings.Contains(md, "### 在用（不要刪，先問）（1）") {
		t.Fatalf("報告要把它放在「在用」：\n%s", md)
	}
}

// TestSweepNeverReadsOutsideScanRoots：只走規格的四處（工作資料夾、這個家、帳號資料夾）；使用者家目錄裡的桌面、文件、照片一個都不讀。
func TestSweepNeverReadsOutsideScanRoots(t *testing.T) {
	user := fakeUser(t)
	home := filepath.Join(user, "Workspace", "home")
	put(t, filepath.Join(home, "home.toml"), testNow)
	for _, private := range []string{"Desktop", "Documents", "Pictures", "Downloads", "Library/Mobile Documents"} {
		put(t, filepath.Join(user, private, "photos.bak-20250101-old"), testNow)
	}
	// 範圍裡指到範圍外的捷徑（資料夾與檔案）不跟過去。
	if err := os.Symlink(filepath.Join(user, "Desktop"), filepath.Join(user, "Workspace", "desk")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(user, "Workspace", "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(user, "Desktop", "photos.bak-20250101-old"), filepath.Join(user, "Workspace", "p", "link.bak")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(user, "Workspace", "p", "a.bak"), testNow)
	cfg := testConfig(t, user, home, noRefs)
	allowed := []string{filepath.Join(user, "Workspace")}
	for _, r := range cfg.Roots {
		if r != filepath.Join(user, "Workspace") {
			allowed = append(allowed, r) // 有萬用字元的照樣比對
		}
	}
	var visited []string
	cfg.Visit = func(dir string) { visited = append(visited, dir) }
	rep := Scan(context.Background(), cfg, testNow, taipei)
	if len(visited) == 0 {
		t.Fatal("一個資料夾都沒讀")
	}
	for _, dir := range visited {
		if dir == user {
			t.Fatalf("讀了整個使用者家目錄")
		}
		inside := false
		for _, root := range allowed {
			if strings.ContainsAny(root, "*") {
				top := filepath.Join(user, strings.SplitN(strings.TrimPrefix(root, user+"/"), "/", 2)[0])
				if ok, _ := filepath.Match(root, top); ok && (dir == top || strings.HasPrefix(dir, top+"/")) {
					inside = true
				}
				continue
			}
			if dir == root || strings.HasPrefix(dir, root+"/") {
				inside = true
			}
		}
		if !inside {
			t.Errorf("讀了範圍外的資料夾 %s", dir)
		}
	}
	for _, it := range append(rep.Expired, rep.Misnamed...) {
		if !strings.HasPrefix(it.Path, "~/Workspace/") {
			t.Errorf("報告列了範圍外的東西 %s", it.Path)
		}
	}
}

// TestSweepRefusesWholeUserHomeAsRoot：範圍就算被寫成整個使用者家目錄（或它的上層）也不掃，記成出錯。
func TestSweepRefusesWholeUserHomeAsRoot(t *testing.T) {
	user := fakeUser(t)
	put(t, filepath.Join(user, "Desktop", "x.bak"), testNow)
	var visited []string
	cfg := Config{UserHome: user, Home: filepath.Join(user, "Workspace", "home"), Roots: []string{user, filepath.Dir(user)}, Probe: noRefs,
		Visit: func(dir string) { visited = append(visited, dir) }}
	rep := Scan(context.Background(), cfg, testNow, taipei)
	if len(visited) != 0 || len(rep.Errors) != 2 || len(rep.Misnamed) != 0 {
		t.Fatalf("不能掃整個家目錄：讀了 %v，出錯 %v", visited, rep.Errors)
	}
}

// TestSweepSkipsGitTrackedFiles：git 管的檔（程式碼、文件裡名字像備份的）不算備份，同一個工作目錄併成一筆略過；
// 同一個工作目錄裡 git 不管的 .bak 照樣列出來。
func TestSweepSkipsGitTrackedFiles(t *testing.T) {
	user := fakeUser(t)
	home := filepath.Join(user, "Workspace", "home")
	put(t, filepath.Join(home, "home.toml"), testNow)
	repo := filepath.Join(user, "Workspace", "repo")
	put(t, filepath.Join(repo, ".git", "HEAD"), testNow)
	put(t, filepath.Join(repo, "conf", "app.toml.bak"), testNow)
	put(t, filepath.Join(repo, "docs", "old.backup"), testNow)
	put(t, filepath.Join(repo, "NOTES.local.md.bak"), testNow)
	cfg := testConfig(t, user, home, noRefs)
	cfg.GitFiles = func(root string) ([]string, error) {
		if root != repo {
			return nil, errors.New("不是這個")
		}
		return []string{"conf/app.toml.bak", "docs/old.backup"}, nil
	}
	rep := Scan(context.Background(), cfg, testNow, taipei)
	if len(rep.Misnamed) != 1 || rep.Misnamed[0].Path != "~/Workspace/repo/NOTES.local.md.bak" {
		t.Fatalf("只有 git 不管的要列：%+v", rep.Misnamed)
	}
	skip, ok := byPath(rep.Skipped, "~/Workspace/repo")
	if !ok || !strings.Contains(skip.Reason, "2 個") || !strings.Contains(skip.Reason, "git") {
		t.Fatalf("git 管的併成一筆略過：%+v", rep.Skipped)
	}
}

// TestSweepMarksUncheckedWhenACheckFails：有一種「在用」檢查沒做成，已到期的標「沒查完」、不列可刪，並記成出錯。
func TestSweepMarksUncheckedWhenACheckFails(t *testing.T) {
	user := fakeUser(t)
	home := filepath.Join(user, "Workspace", "home")
	put(t, filepath.Join(home, "home.toml"), testNow)
	put(t, filepath.Join(user, "Workspace", "p", "a.bak-20260101-old"), testNow)
	probe := func(c Check) ([]Ref, error) {
		if c.Name == "ps" {
			return nil, errors.New("跑不起來")
		}
		return nil, nil
	}
	rep := Scan(context.Background(), testConfig(t, user, home, probe), testNow, taipei)
	if len(rep.Expired) != 1 || rep.Expired[0].State != sdk.BackupSweepUnchecked || rep.Expired[0].Checked != "沒查完：執行中的程式沒查成，不知道有沒有在用" {
		t.Fatalf("要標沒查完：%+v", rep.Expired)
	}
	if len(rep.Errors) != 1 || !strings.HasPrefix(rep.Errors[0], "「在用」檢查（執行中的程式）沒做成") {
		t.Fatalf("要記成出錯：%v", rep.Errors)
	}
}

// TestSweepReadsBinaryLaunchAgentPlist：開機服務的設定是二進位 plist 時先轉成 XML 再比對，指到上層資料夾一樣標「在用」。
func TestSweepReadsBinaryLaunchAgentPlist(t *testing.T) {
	if _, err := os.Stat(plistBin); err != nil {
		t.Skip("這台沒有 plutil")
	}
	user := fakeUser(t)
	home := filepath.Join(user, "Workspace", "home")
	put(t, filepath.Join(home, "home.toml"), testNow)
	svc := filepath.Join(user, "Workspace", "svc")
	put(t, filepath.Join(svc, "snap.bak-20260901-before-move"), testNow)
	agents := filepath.Join(user, "Library", "LaunchAgents")
	t.Setenv("EVERGENIE_LAUNCH_AGENTS_DIR", agents)
	plist := filepath.Join(agents, "com.example.svc.plist")
	put(t, plist, testNow)
	xml := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>WorkingDirectory</key><string>` + svc + `</string></dict></plist>`
	if err := os.WriteFile(plist, []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(plistBin, "-convert", "binary1", plist).CombinedOutput(); err != nil {
		t.Fatalf("轉不成二進位：%v %s", err, out)
	}
	if data, _ := os.ReadFile(plist); !strings.HasPrefix(string(data), "bplist") {
		t.Fatal("測試的 plist 要是二進位的")
	}
	probe := func(c Check) ([]Ref, error) {
		if c.Files != "" {
			return runCheck(context.Background(), c)
		}
		return nil, nil
	}
	rep := Scan(context.Background(), testConfig(t, user, home, probe), testNow, taipei)
	if len(rep.Expired) != 1 || rep.Expired[0].State != sdk.BackupSweepInUse || !strings.Contains(rep.Expired[0].UsedBy, "上層資料夾") {
		t.Fatalf("二進位 plist 指到上層資料夾也要標在用：%+v %v", rep.Expired, rep.Errors)
	}
}

// TestSweepOwnerTextHasNoToolNames：報告（Markdown 與看板讀的 JSON）、通知帶的原因只寫白話的檢查名稱，不寫工具名稱（MOD-N7、BSW-N3）。
// 涵蓋可刪的「查過」、在用的出處、沒查完的原因、出錯訊息與查了哪幾種。
func TestSweepOwnerTextHasNoToolNames(t *testing.T) {
	user := fakeUser(t)
	home := filepath.Join(user, "Workspace", "home")
	put(t, filepath.Join(home, "home.toml"), testNow)
	used := filepath.Join(user, "Workspace", "svc")
	put(t, filepath.Join(used, "a.bak-20260101-old"), testNow)
	put(t, filepath.Join(user, "Workspace", "p", "b.bak-20260101-old"), testNow)
	cfg := testConfig(t, user, home, nil)
	var names []string
	for _, c := range cfg.Checks {
		names = append(names, regexp.QuoteMeta(c.Name))
	}
	tool := regexp.MustCompile(`(?i)\b(` + strings.Join(names, "|") + `)\b`)
	check := func(rep *sdk.BackupSweepReport) {
		t.Helper()
		data, err := json.Marshal(rep)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{Markdown(rep, taipei), string(data)} {
			if m := tool.FindString(text); m != "" {
				t.Fatalf("給主人看的字不寫工具名稱（找到 %q）：\n%s", m, text)
			}
		}
	}
	// 都查過：一個可刪。
	cfg.Probe = noRefs
	ok := Scan(context.Background(), cfg, testNow, taipei)
	if d, _, _ := Counts(ok); d != 2 {
		t.Fatalf("兩個都可刪：%+v", ok.Expired)
	}
	check(ok)
	// 開機服務指到一個、終端機視窗那一種沒查成：一個在用、一個沒查完、記成出錯。
	cfg.Probe = func(c Check) ([]Ref, error) {
		switch {
		case c.Files != "":
			return []Ref{{Label: "x.plist", Text: used}}, nil
		case len(c.Command) > 0 && c.Command[0] == "/bin/sh":
			return nil, &checkError{msg: "有一個終端機視窗查不了：連不上"}
		}
		return nil, nil
	}
	bad := Scan(context.Background(), cfg, testNow, taipei)
	if _, u, n := Counts(bad); u != 1 || n != 1 || len(bad.Errors) != 1 {
		t.Fatalf("一個在用、一個沒查完、一筆出錯：%+v %v", bad.Expired, bad.Errors)
	}
	check(bad)
}
