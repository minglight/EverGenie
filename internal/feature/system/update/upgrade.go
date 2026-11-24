package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/feature/system/host"
	"github.com/minglight/evergenie/sdk"
)

// 升級與退版（P8 第二輪 S2、REQ-280）。`evergenie upgrade` 是另一個行程（不是 serve 自己換自己，K3），
// 做法照 scripts/deploy.sh：留舊版 bin.prev/、編譯、跑家的遷移（先備份）、重開服務、檢查，任何一步沒過就自動換回並跳過那一版。
// 對象是 git tag（vX.Y.Z，或 safe/日期），不是 main。這個檔案只做事，不知道 CLI 與通知長什麼樣：
// 重開之後的檢查（Verify）、通知（Notify）、升級成功後的收尾（AfterSuccess）都由呼叫的人（cmd/evergenie）接進來。

// 結束碼：0 成功；1 沒成功（已換回舊版，或根本沒動）；3 沒成功而且換不回去（要人處理）。
const (
	UpgradeExitFailed   = 1
	UpgradeExitStranded = 3
)

// ErrUpgrade 帶結束碼的錯誤；訊息是白話。
type ErrUpgrade struct {
	Code    int
	Message string
}

func (e *ErrUpgrade) Error() string { return e.Message }

func upgradeFail(code int, format string, a ...any) error {
	return &ErrUpgrade{Code: code, Message: fmt.Sprintf(format, a...)}
}

// UpgradeExitCode 從 Apply／Rollback 回傳的錯誤取結束碼（nil＝0）。
func UpgradeExitCode(err error) int {
	if err == nil {
		return 0
	}
	if ue, ok := err.(*ErrUpgrade); ok {
		return ue.Code
	}
	return UpgradeExitFailed
}

// Upgrader 做升級的所有事。欄位裡的函式讓呼叫的人（與測試）接進來。
type Upgrader struct {
	Home    string // 家
	Core    string // EverGenie 程式：原始碼版是 git repo（bin/ 在它底下）；下載版是安裝資料夾（versions/、current，download.go）
	From    string // 現在跑的核心版本（platform.Version()）
	Channel string // release、safe（check 用）
	Out     io.Writer

	// Download＝Core 是下載版的安裝資料夾（IsDownloadInstall）：新版從版本清單 Source 下載、換 current，不用 git 與 make。
	Download bool
	Source   string // 版本清單（manifest.json）的網址；下載版才用（[update] source ＞ 編進執行檔的預設）
	Platform string // 這台在清單 assets 裡的鍵；空＝darwin-<這台的晶片>（測試可以改）

	// Restart 重開服務；預設用 Service（launchd 或 EVERGENIE_SERVICE_MODE=process）重開 <Core>/bin/evergenie。
	Restart func(ctx context.Context) error
	// Verify 重開之後檢查：服務起來了、核心版本是 want、沒有紅字、看板與套件正常。沒過回白話原因。
	Verify func(ctx context.Context, want string) error
	// Notify 發一則通知（title＋一句說明）；可以是 nil。
	Notify func(title, detail string)
	// Baseline 在動任何東西之前呼叫一次：記下升級前就有的健康檢查紅字，Verify 只把「升級後才出現的」算沒過
	// （主人資料本來就有的問題不是這一版的錯，不能因此擋升級，DEV-154 原則 7）。可以是 nil。
	Baseline func(ctx context.Context)
	// Report 升級做完（成功，或沒成功已換回）之後把升級報告交給管電腦的秘書（report.go）；可以是 nil。
	Report func(ctx context.Context, r UpgradeReport)
	// AfterSuccess 升級成功之後的收尾（同步角色包…）；失敗不算升級失敗。可以是 nil。
	AfterSuccess func(ctx context.Context)

	Now          func() time.Time
	FetchTimeout time.Duration // git fetch 的時限（預設 30 秒）
	BuildTimeout time.Duration // make build 的時限（預設 20 分鐘）
	BackupMaxMB  int64         // 備份時略過大於這個大小的檔（預設 30）
	// DownloadTimeout 下載一個發行檔的時限（預設 10 分鐘；下載版才用）
	DownloadTimeout time.Duration
	// HTTP 是下載用的 http.Client（測試換成信任測試憑證的；nil＝預設）。轉址的檢查一律照 platform.DownloadRedirectCheck。
	HTTP *http.Client
}

func (u *Upgrader) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}
func (u *Upgrader) logf(format string, a ...any) {
	if u.Out != nil {
		fmt.Fprintf(u.Out, format+"\n", a...)
	}
}
func (u *Upgrader) notify(title, detail string) {
	if u.Notify != nil {
		u.Notify(title, detail)
	}
}
func (u *Upgrader) git(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	return platform.GitOutput(ctx, timeout, u.Core, args...)
}
func (u *Upgrader) binDir() string { return filepath.Join(u.Core, "bin") }

// exe 是服務跑的執行檔：原始碼版 <Core>/bin/evergenie；下載版 <Core>/current/bin/evergenie（換版只換 current，服務設定不動）。
func (u *Upgrader) exe() string {
	if u.Download {
		return filepath.Join(u.Core, currentLinkName, "bin", platform.CoreExe)
	}
	return filepath.Join(u.Core, "bin", platform.CoreExe)
}

// FindCore 找 EverGenie 程式在哪：[update] core（選填）＞執行檔所在的 repo（bin/ 的上一層，要有 .git）。找不到回空字串。
func FindCore(h *platform.Home, exe string) string {
	isRepo := func(dir string) bool {
		if dir == "" {
			return false
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			return false
		}
		_, err := os.Stat(filepath.Join(dir, "Makefile"))
		return err == nil
	}
	if h != nil && h.Update.Core != "" {
		if isRepo(h.Update.Core) {
			return h.Update.Core
		}
		return ""
	}
	if exe == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir := filepath.Dir(filepath.Dir(exe))
	if filepath.Base(filepath.Dir(exe)) != "bin" || !isRepo(dir) {
		return ""
	}
	return dir
}

// MainRepo 回 dir 所屬 repo 的主工作目錄：dir 本身是主 repo 就回它；dir 是 `git worktree add` 出來的工作目錄
// （開發家從專屬 worktree 跑，執行檔往上找到的是 worktree）就回主 repo。dir 為空、不是 git 資料夾或 git 不能用時原樣回 dir。
// 給秘書看「EverGenie repo 在哪」用：秘書要在主 repo 開分支，不能動開發家正在跑的那份（DEV-93）。
func MainRepo(dir string) string {
	if dir == "" {
		return ""
	}
	out, err := platform.GitOutput(context.Background(), 5*time.Second, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return dir
	}
	common := strings.TrimSpace(out)
	if filepath.Base(common) != ".git" { // bare repo 或怪的排法：不猜
		return dir
	}
	main := filepath.Dir(common)
	if real, err := filepath.EvalSymlinks(main); err == nil {
		main = real
	}
	return main
}

// ---- 檢查 ----------------------------------------------------------------------------------------------

// Check 抓最新的 tag，找出這個頻道的目標版本，結果寫進 check.json 並回傳。抓不到不算失敗：用本機已有的 tag，Detail 說明。
func (u *Upgrader) Check(ctx context.Context) (*UpgradeCheck, error) {
	res := &UpgradeCheck{Current: u.From, Channel: u.Channel, Skipped: ReadUpgradeSkipped(u.Home), CheckedAt: u.now()}
	if res.Skipped == nil {
		res.Skipped = []string{}
	}
	finish := func() (*UpgradeCheck, error) {
		if err := platform.WriteJSONFile(filepath.Join(UpgradeDir(u.Home), UpgradeCheckFile), res); err != nil {
			return res, err
		}
		return res, nil
	}
	if u.Channel == platform.UpdateChannelOff {
		res.Detail = "更新已關閉（[update] channel = off）"
		return finish()
	}
	if u.Download {
		return u.checkDownload(ctx, res, finish)
	}
	timeout := u.FetchTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	var note string
	if _, err := u.git(ctx, timeout, "fetch", "--tags", "--quiet", "origin"); err != nil {
		u.detail("git fetch: %v", err)
		note = "抓不到最新的版本（可能是網路不通），以下是上次抓到的"
	}
	cur, curOK := platform.ParseVersion(u.From)
	tag, version, notes, err := u.pickTarget(ctx)
	if err != nil {
		return res, err
	}
	switch {
	case tag == "":
		res.Detail = "還沒有可以升級的版本"
	case !curOK:
		res.Detail = "這是開發中的版本，不會自動升級"
	default:
		tv, ok := platform.ParseVersion(version)
		switch {
		case !ok:
			res.Detail = "新版 " + version + " 沒有正式版號，沒辦法比較新舊"
		case tv.Compare(cur) <= 0:
			res.Detail = "已經是最新版"
		case contains(res.Skipped, version):
			res.Detail = version + " 這版你跳過了"
		default:
			res.Target, res.Tag, res.Notes = version, tag, notes
		}
	}
	if note != "" {
		if res.Detail != "" {
			res.Detail += "（" + note + "）"
		} else {
			res.Detail = note
		}
	}
	return finish()
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// pickTarget 依頻道找目標：release＝最大的 vX.Y.Z；safe＝最新的 safe/* 指到的 commit（版本是那個 commit 的 describe）。
func (u *Upgrader) pickTarget(ctx context.Context) (tag, version, notes string, err error) {
	if u.Channel == platform.UpdateChannelSafe {
		out, e := u.git(ctx, 10*time.Second, "tag", "--list", "safe/*", "--sort=-version:refname")
		if e != nil {
			u.detail("git tag --list safe/*: %v", e)
			return "", "", "", platform.Unavailable("讀不了版本清單（細節記在升級日誌）")
		}
		first, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
		if first == "" {
			return "", "", "", nil
		}
		ver, e := u.versionOfTag(ctx, first)
		if e != nil {
			return "", "", "", e
		}
		return first, ver, u.tagNotes(ctx, first), nil
	}
	out, e := u.git(ctx, 10*time.Second, "tag", "--list", "v[0-9]*")
	if e != nil {
		u.detail("git tag --list: %v", e)
		return "", "", "", platform.Unavailable("讀不了版本清單（細節記在升級日誌）")
	}
	var best platform.CoreVersion
	for _, t := range strings.Fields(out) {
		v, ok := platform.ParseVersion(t)
		if !ok || !v.Release() {
			continue
		}
		if tag == "" || v.Compare(best) > 0 {
			tag, best = t, v
		}
	}
	if tag == "" {
		return "", "", "", nil
	}
	return tag, strings.TrimPrefix(tag, "v"), u.tagNotes(ctx, tag), nil
}

// versionOrder：a 比 b 舊（<0）、一樣（0）、新（>0）。兩個都要是看得懂的版本號（platform.ParseVersion），有一個看不懂 ok=false：
// 升級遇到看不懂的不猜（fail closed）——不換過去，白話 msgVersionUnknown、下一步 nextAsk，英文細節進升級日誌（UPD-12）。
func versionOrder(a, b string) (order int, ok bool) {
	av, aok := platform.ParseVersion(a)
	bv, bok := platform.ParseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	return av.Compare(bv), true
}

func (u *Upgrader) tagNotes(ctx context.Context, tag string) string {
	out, err := u.git(ctx, 10*time.Second, "tag", "-l", "--format=%(contents)", tag)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// versionOfTag 是這個 tag 編出來的核心版本（跟 scripts/version.sh 一樣）：vX.Y.Z → X.Y.Z；safe/… → 那個 commit 的 git describe。
func (u *Upgrader) versionOfTag(ctx context.Context, tag string) (string, error) {
	if v, ok := platform.ParseVersion(tag); ok && strings.HasPrefix(tag, "v") && v.Release() {
		return strings.TrimPrefix(tag, "v"), nil
	}
	commit, err := u.git(ctx, 10*time.Second, "rev-parse", "--verify", "--quiet", tag+"^{commit}")
	if err != nil {
		u.detail("git rev-parse %s: %v", tag, err)
		return "", upgradeFail(UpgradeExitFailed, "找不到版本 %s", tag)
	}
	out, err := u.git(ctx, 10*time.Second, "describe", "--tags", "--match", "v[0-9]*", "--always", strings.TrimSpace(commit))
	if err != nil {
		u.detail("git describe %s: %v", tag, err)
		return "", upgradeFail(UpgradeExitFailed, "算不出 %s 的版本號", tag)
	}
	return strings.TrimPrefix(strings.TrimSpace(out), "v"), nil
}

// ---- 升級 ----------------------------------------------------------------------------------------------

func (u *Upgrader) setState(st *UpgradeState, phase, reason string) {
	st.Phase, st.Reason = phase, reason
	switch phase {
	case UpgradePhaseDone:
		st.OK, st.FinishedAt = true, u.now()
	case UpgradePhaseRolledBack, UpgradePhaseFailed:
		st.OK, st.FinishedAt = false, u.now()
	}
	if err := platform.WriteJSONFile(filepath.Join(UpgradeDir(u.Home), UpgradeStateFile), st); err != nil {
		u.detail("write %s: %v", UpgradeStateFile, err)
		u.logf("（寫不了進度檔，細節記在升級日誌）")
	}
	// 進度的標籤用中文（UPD-13、UPD-14：原始碼版與下載版一樣）。
	label := phaseLabels[phase]
	if label == "" {
		label = "進度"
	}
	u.logf("【%s】%s", label, reason)
}

// phaseLabels 是印進度時各階段的中文標籤。
var phaseLabels = map[string]string{
	UpgradePhaseFetch: "下載", UpgradePhaseBackup: "備份", UpgradePhaseBuild: "準備", UpgradePhaseMigrate: "整理資料",
	UpgradePhaseRestart: "重開", UpgradePhaseVerify: "檢查", UpgradePhaseDone: "完成",
	UpgradePhaseRolledBack: "用舊版", UpgradePhaseFailed: "沒成功",
}

func (u *Upgrader) lock() (func(), error) {
	dir := UpgradeDir(u.Home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		u.detail("mkdir %s: %v", dir, err)
		return nil, failMsg(UpgradeExitFailed, "建不了升級用的資料夾", nextDisk)
	}
	path := filepath.Join(dir, UpgradeLockFile)
	for try := 0; try < 2; try++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		data, _ := os.ReadFile(path)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 && pid != os.Getpid() && host.PidAlive(pid) {
			u.detail("lock %s held by pid %d", path, pid)
			return nil, failMsg(UpgradeExitFailed, "已經有一個升級在跑了（或正在退回）", nextBusy)
		}
		_ = os.Remove(path) // 上一個升級掛了沒收尾
	}
	u.detail("cannot take lock %s", path)
	return nil, failMsg(UpgradeExitFailed, "拿不到升級用的鎖", nextBusy)
}

// Apply 升級到 to（tag；空＝用 Check 找到的目標；下載版是版本號，可以帶 v）。沒成功時會自動換回舊版。
func (u *Upgrader) Apply(ctx context.Context, to string) error {
	unlock, err := u.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if u.Download {
		return u.applyDownload(ctx, to)
	}

	st := &UpgradeState{Kind: sdk.UpdateKindUpgrade, From: u.From, StartedAt: u.now()}
	u.setState(st, UpgradePhaseFetch, "正在抓最新的版本")
	// 換版之前就停的：舊版照常在跑（outcome untouched），原因白話、英文細節進升級日誌（UPD-14）。
	fail := func(reason, next string) error { return u.untouched(st, reason, next) }
	if out, err := u.git(ctx, 20*time.Second, "status", "--porcelain", "--untracked-files=no"); err != nil {
		u.detail("git status in %s: %v", u.Core, err)
		return fail("讀不了程式資料夾的狀態"+msgUntouched, nextAsk)
	} else if strings.TrimSpace(out) != "" {
		u.detail("uncommitted changes in %s:\n%s", u.Core, strings.TrimSpace(out))
		return fail("程式資料夾裡有沒提交的改動，先不升級；沒有動任何東西", nextDirty)
	}
	timeout := u.FetchTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if _, err := u.git(ctx, timeout, "fetch", "--tags", "--quiet", "origin"); err != nil && to == "" {
		u.detail("git fetch: %v", err)
		return fail("抓不到最新的版本（可能是網路不通）"+msgUntouched, nextNetwork)
	}
	if to == "" {
		chk, err := u.Check(ctx)
		if err != nil {
			return fail(u.explain("檢查新版沒成功", err)+msgUntouched, nextRetry)
		}
		if chk.Target == "" {
			return fail("沒有新版可以升級："+chk.Detail, nextNothing)
		}
		to = chk.Tag
	}
	toVersion, err := u.versionOfTag(ctx, to)
	if err != nil {
		return fail(u.reasonOf(err)+msgUntouched, nextRecheck)
	}
	if order, ok := versionOrder(toVersion, u.From); !ok { // 比不出新舊：不換過去（UPD-12）
		u.detail("cannot compare target version %q with current %q", toVersion, u.From)
		return fail(msgVersionUnknown+msgUntouched, nextAsk)
	} else if order < 0 { // --to 指了比現在舊的版本：降版走「退回上一版」（UPD-12）
		return fail(fmt.Sprintf("%s 比現在的 %s 舊，升級不會換成舊版%s", toVersion, u.From, msgUntouched), nextDowngrade)
	}
	toCommitOut, err := u.git(ctx, 10*time.Second, "rev-parse", "--verify", "--quiet", to+"^{commit}")
	if err != nil {
		u.detail("git rev-parse %s: %v", to, err)
		return fail("找不到版本 "+to+msgUntouched, nextRecheck)
	}
	toCommit := strings.TrimSpace(toCommitOut)
	fromCommitOut, err := u.git(ctx, 10*time.Second, "rev-parse", "HEAD")
	if err != nil {
		u.detail("git rev-parse HEAD: %v", err)
		return fail("讀不到現在的版本"+msgUntouched, nextAsk)
	}
	fromCommit := strings.TrimSpace(fromCommitOut)
	st.To = toVersion
	u.setState(st, UpgradePhaseFetch, fmt.Sprintf("要從 %s 升級到 %s", u.From, toVersion))

	// 備份家（還沒動任何東西）
	u.setState(st, UpgradePhaseBackup, "正在備份你的資料")
	if u.Baseline != nil {
		u.Baseline(ctx)
	}
	backupPath := platform.RuntimeDir(u.Home, "backups", "upgrade-"+safeName(u.From)+"-"+safeName(toVersion)+".tgz")
	if err := u.backupHome(backupPath); err != nil {
		return fail(u.explain("備份失敗", err)+msgUntouched, nextDisk)
	}

	// 留舊版、切到新版、編譯
	u.setState(st, UpgradePhaseBuild, "正在編譯新版（需要幾分鐘）")
	if err := u.keepPrev(); err != nil {
		return fail(u.explain("留不下舊版的執行檔", err)+msgUntouched, nextDisk)
	}
	// migrated：資料被整理過（或整理到一半失敗）才用備份還原。沒整理過就不還原：備份是編譯前做的，
	// 舊版在編譯那幾分鐘照常寫的任務單、記憶不能被靜靜倒回去。重開之後才跑的轉換（套件載入時做的）也算，所以換回前再比一次紀錄。
	var migrated bool
	appliedBefore, _ := AppliedMigrations(u.Home)
	rollback := func(reason string) error {
		if now, _ := AppliedMigrations(u.Home); len(newSince(appliedBefore, now)) > 0 {
			migrated = true
		}
		return u.rollback(ctx, st, fromCommit, toVersion, backupPath, migrated, reason)
	}
	if _, err := u.git(ctx, 60*time.Second, "checkout", "--quiet", "--detach", toCommit); err != nil {
		return rollback(u.explain("切不到新版", err))
	}
	if err := u.build(ctx); err != nil {
		return rollback(u.reasonOf(err))
	}

	// 家的遷移：先在複本上整個試跑一遍（DEV-154：資料格式轉換要先驗證好），過了才動真的家。
	// 新版不認得 --preview（比這個功能還舊的版本）時結束碼是 2，當作沒有這一步。
	u.setState(st, UpgradePhaseMigrate, "正在複本上試跑資料整理（你的資料還沒動）")
	if out, code := u.run(ctx, 10*time.Minute, u.exe(), "migrate-home", "--preview", "--home", u.Home); code != 0 && !(code == 2 && strings.Contains(out, "用法：")) {
		// 結束碼 2 而且印了用法＝那一版不認得 --preview；別的 2（例如程式當掉）照樣算沒過。
		return rollback("資料整理在複本上試跑沒過，你的資料沒有動：" + u.plainTail("migrate-home --preview", out))
	}
	u.setState(st, UpgradePhaseMigrate, "正在整理你的資料（遷移）")
	out, code := u.run(ctx, 10*time.Minute, u.exe(), "migrate-home", "--apply", "--home", u.Home)
	if code != 0 {
		migrated = true // 可能寫到一半
		return rollback("整理資料沒成功：" + u.plainTail("migrate-home --apply", out))
	}
	// 這次跑了哪些：比紀錄檔前後（不靠解析輸出的文字）。
	appliedAfter, _ := AppliedMigrations(u.Home)
	ranMigrations := newSince(appliedBefore, appliedAfter)
	migrated = len(ranMigrations) > 0

	// 重開、檢查
	u.setState(st, UpgradePhaseRestart, "正在重開 EverGenie")
	if err := u.restart(ctx); err != nil {
		return rollback(u.explain("重開失敗", err))
	}
	u.setState(st, UpgradePhaseVerify, "正在檢查新版有沒有正常")
	if u.Verify != nil {
		if err := u.Verify(ctx, toVersion); err != nil {
			return rollback(u.explain("新版檢查沒過", err))
		}
	}

	// 重開時才跑的轉換（主人自己的、分享來的套件在載入時做）也要算進這次升級，退回時資料才會一起回去。
	if again, err := AppliedMigrations(u.Home); err == nil {
		ranMigrations = newSince(appliedBefore, again)
	}
	u.setState(st, UpgradePhaseDone, fmt.Sprintf("已升級：%s → %s", u.From, toVersion))
	_ = AppendUpgradeHistory(u.Home, UpgradeHistory{At: u.now(), Kind: "upgrade", From: u.From, To: toVersion, FromCommit: fromCommit, ToCommit: toCommit, OK: true, Migrations: ranMigrations, Backup: backupPath})
	_ = os.Remove(filepath.Join(UpgradeDir(u.Home), UpgradeDecisionFile))
	removeRescue(u.Home)                       // 之前換不回去的紀錄：這次升級成功了，用不到了
	keepNewestUpdateBackup(u.Home, backupPath) // 更新前的備份只留這次這份（UPD-16）；換不回去的紀錄先清掉才收
	if u.AfterSuccess != nil {
		u.AfterSuccess(ctx)
	}
	if u.Report != nil {
		u.Report(ctx, UpgradeReport{From: u.From, To: toVersion, OK: true, Migrations: ranMigrations, Backup: backupPath})
	}
	u.notify("升級完成："+u.From+" → "+toVersion, "EverGenie 已經升級到 "+toVersion+"，不用做什麼。升級前的備份留著最新一份；用起來有問題就跟秘書說要退回 "+u.From+"，資料也會一起回到升級前。")
	return nil
}

// Accept 記下「主人確認這次升級沒問題」（DEV-154）：之後不再自動退回。升級前的備份不刪（只留最新一份，UPD-16）
// （runtime/backups/ 的整理，作者 2026-11-19：升級成功不再問主人、也不叫他刪備份）。沒有等確認的升級就回白話說明。
func (u *Upgrader) Accept() (*UpgradeHistory, error) {
	unlock, err := u.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	last := PendingAcceptance(u.Home)
	if last == nil {
		return nil, upgradeFail(UpgradeExitFailed, "現在沒有要你同意的升級")
	}
	// 有一次沒換回的升級還沒處理（rescue.json）：確認會刪掉備份與舊版的程式資料夾，換回要用的可能就是它們，先擋住（UPD-14）。
	if r := readRescue(u.Home); r != nil && !r.stale(u.From) {
		return nil, failMsg(UpgradeExitFailed, "還有一次沒換回的升級沒處理好，現在確認會刪掉換回要用的備份或舊版；沒有動任何東西", nextRescueFirst)
	}
	if err := AppendUpgradeHistory(u.Home, UpgradeHistory{At: u.now(), Kind: "accept", From: last.From, To: last.To, FromCommit: last.FromCommit, ToCommit: last.ToCommit, OK: true}); err != nil {
		return nil, err
	}
	if u.Download {
		// 下載版：舊版的程式資料夾也收掉（之後不能再退回），只留 current 指到的那一版。
		if cur := currentVersionDir(u.Core); cur != "" {
			u.pruneVersions(cur)
		}
		u.logf("已確認：%s → %s 沒問題，舊版的程式資料夾收掉了；升級前的備份留著最新一份", last.From, last.To)
		return last, nil
	}
	u.logf("已確認：%s → %s 沒問題；升級前的備份留著最新一份", last.From, last.To)
	return last, nil
}

// rollback 換回舊版：git 回原本的 commit、bin.prev/ 換回 bin/（壞的留 bin.failed/）、跑過遷移就用備份還原、重開、再檢查一次；
// 這一版放進跳過清單。回傳的錯誤帶結束碼：1＝已換回、3＝換不回去（記下 rescue.json，可以再試）。
func (u *Upgrader) rollback(ctx context.Context, st *UpgradeState, fromCommit, toVersion, backupPath string, migrated bool, reason string) error {
	u.logf("沒成功：%s；正在換回 %s", reason, u.From)
	back := &upgradeRescue{Commit: fromCommit, Backup: backupPath, Restore: migrated}
	strand := u.stranded(st, toVersion, reason, back)
	if _, err := u.git(ctx, 60*time.Second, "checkout", "--quiet", "--detach", fromCommit); err != nil {
		return strand(u.explain("程式回不到原本的版本", err))
	}
	if err := u.swapBack(); err != nil {
		return strand(u.explain("換不回舊版的執行檔", err))
	}
	return u.finishRollback(ctx, st, toVersion, backupPath, migrated, true, reason, back)
}

// finishRollback 是自動換回的後半（程式已經換回舊版之後）：整理過資料就用備份還原、這一版放進跳過清單、重開（restart）並檢查舊版、
// 寫進度、交報告、發通知。原始碼版與下載版共用；restart 是 false（什麼都沒換過）時結果是「舊版照常在跑」（outcome untouched）。
// state 的 reason 只寫出了什麼事；給命令列與通知的整句帶結果與下一步（UPD-14）。換不回去時照 back 記 rescue.json：
// 資料已經還原成功才沒過的，記下 Restored（「退回上一版」不再用備份蓋一次）。
func (u *Upgrader) finishRollback(ctx context.Context, st *UpgradeState, toVersion, backupPath string, migrated, restart bool, reason string, back *upgradeRescue) error {
	strand := u.stranded(st, toVersion, reason, back)
	if migrated {
		// 自動換回：只把備份裡的資料寫回去，不拿掉備份之後多出來的檔（那是舊版在編譯那幾分鐘裡照常寫的新紀錄，舊版讀得懂）。
		if err := u.restoreHome(backupPath, false); err != nil {
			return strand(u.explain("還原備份失敗", err))
		}
		back.Restored = true
	}
	_ = AddUpgradeSkipped(u.Home, toVersion)
	if restart {
		if err := u.restart(ctx); err != nil {
			return strand(u.explain("重開舊版失敗", err))
		}
		if u.Verify != nil {
			if err := u.Verify(ctx, u.From); err != nil {
				return strand(u.explain("舊版重開後檢查也沒過", err))
			}
		}
	}
	// 什麼都沒換過（下載版在換 current 之前、試跑資料整理就沒過）：舊版一直在跑，不說「換回」。
	msg := fmt.Sprintf("升級到 %s 沒成功，已換回 %s：%s", toVersion, u.From, reason)
	title := "升級到 " + toVersion + " 沒成功，已換回 " + u.From
	outcome := sdk.UpdateOutcomeRolledBack
	if !restart {
		msg = fmt.Sprintf("升級到 %s 沒成功，舊版 %s 照常在跑：%s", toVersion, u.From, reason)
		title = "升級到 " + toVersion + " 沒成功，舊版照常在跑"
		outcome = sdk.UpdateOutcomeUntouched
	}
	st.Restored = migrated
	err := u.end(st, UpgradeExitFailed, UpgradePhaseRolledBack, outcome, reason, msg, nextSkipped)
	if u.Report != nil {
		u.Report(ctx, UpgradeReport{From: u.From, To: toVersion, Reason: reason, Log: u.logPath()})
	}
	u.notify(title, sentence(reason, nextSkipped)+"。"+u.logNote())
	return err
}

// ---- 退版 ----------------------------------------------------------------------------------------------

// Rollback 先看有沒有換不回去的升級（rescue.json）：有就照它再試一次換回（rescue.go）。
// 沒有就回到 history.jsonl 最後一筆成功升級的「升級前」版本：bin.prev/ 還在而且正好是那一版就直接換回，不然 checkout 那個 commit 重新編。
// 那次升級沒有整理過資料：家不動（舊版照樣讀得懂）。整理過資料（紀錄的 Migrations 不是空的）：用升級前的備份把資料
// 一起還原（還原前先把現在的家留一份 rollback-*.tgz）；備份已經不在（之後又更新成功、被換掉了）或主人確認過升級沒問題就拒絕退回。
// rescue.json 已經不用處理（現在跑的版本跟它對不上：之後手動修好、又換過版）就刪掉，照一般的退回做。
func (u *Upgrader) Rollback(ctx context.Context) error { return u.rollbackOr(ctx, false) }

// Rescue 只照 rescue.json 再試一次換回（看板首頁的「退回上一版」，`upgrade --rollback --rescue`）：沒有這份紀錄、或它已經不用處理，
// 就說明、什麼都不動，不改做一般的退回（主人按的是「換回沒升成的那次」，不是「退回上一次升級」）。
func (u *Upgrader) Rescue(ctx context.Context) error { return u.rollbackOr(ctx, true) }

func (u *Upgrader) rollbackOr(ctx context.Context, rescueOnly bool) error {
	unlock, err := u.lock()
	if err != nil {
		return err
	}
	defer unlock()
	r := readRescue(u.Home)
	if r != nil && r.stale(u.From) {
		u.detail("rescue.json %s -> %s is stale (running %s); removed", r.From, r.To, u.From)
		removeRescue(u.Home)
		r = nil
	}
	if r != nil {
		return u.rescue(ctx, r)
	}
	if rescueOnly {
		return failMsg(UpgradeExitFailed, "現在沒有沒換回的升級要處理（可能已經處理好了）；沒有動任何東西", nextNothing)
	}
	var last *UpgradeHistory
	hist := ReadUpgradeHistory(u.Home)
	// 退得回去的升級：原始碼版要記到升級前的 commit；下載版要是下載版做的升級（舊版資料夾還在就換回去）。
	usable := func(h UpgradeHistory) bool {
		if u.Download {
			return h.Install == InstallDownload
		}
		return h.FromCommit != ""
	}
	for i := len(hist) - 1; i >= 0 && last == nil; i-- {
		switch {
		case !hist[i].OK:
		case hist[i].Kind == "rollback":
			// 最近一次升級已經退回過了：不能對同一次升級再退一次（會把退回之後新增的資料又倒回升級前）。
			return failMsg(UpgradeExitFailed, "最近一次升級已經退回過了（現在是 "+hist[i].To+"）；沒有動任何東西", nextNothing)
		case hist[i].Kind == "accept":
			// 主人確認過沒問題的升級不自動退回（備份還在，要找回個別資料請秘書從備份拿）。
			return failMsg(UpgradeExitFailed, "最近一次升級（"+hist[i].From+" → "+hist[i].To+"）已經確認過沒問題，不自動退回；升級前的備份還留著。沒有動任何東西", nextAsk)
		case hist[i].Kind == "upgrade" && usable(hist[i]):
			last = &hist[i]
		case hist[i].Kind == "upgrade" && len(hist[i].Migrations) > 0:
			// 整理過資料、卻沒記到升級前的 commit（部署工具當時讀不到）：不能跳過它去退更舊的一次。
			return failMsg(UpgradeExitFailed, hist[i].To+" 那次更新整理過資料格式，但沒記到更新前的程式版本，不能自動退回；更新前的備份在 "+hist[i].Backup+"。沒有動任何東西", "請秘書幫忙處理")
		}
	}
	if last == nil {
		return failMsg(UpgradeExitFailed, "沒有可以退回的升級紀錄（這台還沒自動升級過）", nextNothing)
	}
	st := &UpgradeState{Kind: sdk.UpdateKindRollback, From: u.From, To: last.From, StartedAt: u.now()}
	// 結束碼 1：什麼都沒動（outcome untouched）；3：退到一半（outcome stranded，要人處理）。
	fail := func(code int, reason, next string) error {
		outcome := sdk.UpdateOutcomeUntouched
		if code == UpgradeExitStranded {
			outcome = sdk.UpdateOutcomeStranded
		}
		return u.end(st, code, UpgradePhaseFailed, outcome, reason, reason, next)
	}
	// 那次升級整理過資料（格式可能變了，舊版讀不懂）：退回時資料要一起回到升級前（DEV-154）。
	// 更新前的備份只留最新一份（UPD-16）；被換掉了就不能退回，免得舊版對著新格式的資料壞掉。
	restore := len(last.Migrations) > 0
	resume := false
	if restore {
		if _, err := os.Stat(last.Backup); last.Backup == "" || err != nil {
			return fail(UpgradeExitFailed, fmt.Sprintf("%s 那次升級整理過資料格式，升級前的備份已經不在（之後的更新換掉了），不能退回 %s；沒有動任何東西", last.To, last.From), nextAsk)
		}
		// 要還原資料：換版之前先確認備份讀得開（整份讀一遍），不然換了程式才發現資料回不去（UPD-9）。
		if err := readableBackup(last.Backup); err != nil {
			u.detail("backup %s: %v", last.Backup, err)
			return fail(UpgradeExitFailed, fmt.Sprintf("%s 那次升級整理過資料格式，升級前的備份讀不開，不能退回 %s；沒有動任何東西", last.To, last.From), nextAsk)
		}
		// 現在的版本已經是那次升級前的版本、卻沒有退回紀錄：上一次退回換完程式之後失敗了（還原或重開沒成）。
		// 接著做完（不再換程式、不再留一份現在的家：那份上一次已經留了），不能擋死。
		resume = u.From == last.From && u.From != last.To
		// 那次之後又換過版（部署工具沒整理資料的更新不留紀錄）：備份是更早的，還原會把中間這段的紀錄都倒回去。
		if u.From != last.To && !resume {
			return fail(UpgradeExitFailed, fmt.Sprintf("整理過資料的那次更新是 %s → %s，現在已經是 %s（之後又更新過）；用那次的備份還原會連後來的紀錄一起倒回去，所以不自動退回。備份還在 %s。沒有動任何東西", last.From, last.To, u.From, last.Backup), "要找回個別資料請秘書幫忙")
		}
	}
	// 下載版：舊版的資料夾要還在（之後又升級過、或確認過就收掉了，那就不能退回）。
	prevDir := ""
	if u.Download {
		if prevDir = findVersionDir(u.Core, last.From); prevDir == "" {
			return fail(UpgradeExitFailed, fmt.Sprintf("舊版 %s 的程式資料夾已經不在了，不能退回；沒有動任何東西", last.From), nextAsk)
		}
	}
	// 退回後的檢查只看「退回才出現」的紅字：家裡本來就有的問題不算（同升級）。
	if u.Baseline != nil {
		u.Baseline(ctx)
	}
	if u.Download {
		// 沒有 git：不用看有沒有沒提交的改動。
	} else if out, err := u.git(ctx, 20*time.Second, "status", "--porcelain", "--untracked-files=no"); err != nil {
		u.detail("git status in %s: %v", u.Core, err)
		return fail(UpgradeExitFailed, "讀不了程式資料夾的狀態，沒有退回；沒有動任何東西", nextAsk)
	} else if strings.TrimSpace(out) != "" {
		u.detail("uncommitted changes in %s:\n%s", u.Core, strings.TrimSpace(out))
		return fail(UpgradeExitFailed, "程式資料夾裡有沒提交的改動，先不退回；沒有動任何東西", nextDirty)
	}
	kept := "backups/ 裡上一次退回時留的 rollback-*.tgz"
	if restore && !resume {
		// 動任何東西之前，先把現在的家留一份（升級之後新增、改過的東西都在裡面）。
		u.setState(st, UpgradePhaseBackup, "正在把現在的資料留一份")
		kept = platform.RuntimeDir(u.Home, "backups", "rollback-"+safeName(u.From)+"-"+safeName(last.From)+".tgz")
		if err := u.backupHome(kept); err != nil {
			return fail(UpgradeExitFailed, u.explain("留不下退回前的資料備份，沒有動任何東西", err), nextDisk)
		}
	}
	// 下載版的固定位置（UPD-15）也回到舊版那份：內容一樣的不動（主人不用再按一次允許）；中斷後接著做（不再換 current）時也再對一次。
	if resume && u.Download {
		u.syncFixed(filepath.Join(u.Core, versionsDirName, prevDir))
	}
	if !resume && u.Download {
		u.setState(st, UpgradePhaseBuild, fmt.Sprintf("正在退回 %s → %s", u.From, last.From))
		if err := u.switchCurrentFixed(prevDir); err != nil {
			return fail(UpgradeExitFailed, u.explain("換不回 "+last.From+" 的資料夾", err), nextAsk)
		}
	} else if !resume {
		u.setState(st, UpgradePhaseBuild, fmt.Sprintf("正在退回 %s → %s", u.From, last.From))
		if _, err := u.git(ctx, 60*time.Second, "checkout", "--quiet", "--detach", last.FromCommit); err != nil {
			return fail(UpgradeExitFailed, u.explain("切不到 "+last.From, err), nextAsk)
		}
		prevOK := false
		if _, err := os.Stat(filepath.Join(u.Core, "bin.prev")); err == nil {
			if out, code := u.run(ctx, time.Minute, filepath.Join(u.Core, "bin.prev", platform.CoreExe), "version"); code == 0 && strings.TrimSpace(out) == last.From {
				prevOK = true
			}
		}
		if prevOK {
			if err := u.swapBack(); err != nil {
				return fail(UpgradeExitStranded, u.explain("換不回 "+last.From+" 的執行檔", err), nextAsk)
			}
		} else if err := u.build(ctx); err != nil {
			return fail(UpgradeExitStranded, u.explain("重新編譯 "+last.From+" 失敗", err), nextAsk)
		}
	}
	if restore {
		u.setState(st, UpgradePhaseBackup, "正在把資料還原到升級前")
		if err := u.restoreHome(last.Backup, true); err != nil {
			return fail(UpgradeExitStranded, u.explain("還原升級前的資料失敗（退回前的資料在 "+kept+"）", err), nextAsk)
		}
	}
	// 程式與資料都已經回去了：先記下來（跳過清單、退回紀錄），再重開、檢查。重開或檢查沒過也不能讓這次退回「沒發生過」
	// （不然還能對同一次升級再退一次，當晚自動升級又升回去）。
	_ = AddUpgradeSkipped(u.Home, last.To)
	_ = AppendUpgradeHistory(u.Home, UpgradeHistory{At: u.now(), Kind: "rollback", From: last.To, To: last.From, FromCommit: last.ToCommit, ToCommit: last.FromCommit, OK: true, Install: last.Install})
	u.setState(st, UpgradePhaseRestart, "正在重開 EverGenie")
	if err := u.restart(ctx); err != nil {
		return fail(UpgradeExitStranded, u.explain("已經退回 "+last.From+"（資料也回去了），但重開失敗", err), nextAsk)
	}
	u.setState(st, UpgradePhaseVerify, "正在檢查有沒有正常")
	if u.Verify != nil {
		if err := u.Verify(ctx, last.From); err != nil {
			return fail(UpgradeExitStranded, u.explain("已經退回 "+last.From+"，但退回後檢查沒過", err), nextAsk)
		}
	}
	done, note := fmt.Sprintf("已退回：%s → %s（%s 之後不會再自動升級）", u.From, last.From, last.To), "從 "+u.From+" 退回；"+last.To+" 這版先跳過。"
	if restore {
		extra := "資料也回到升級前了；升級之後新增或改過的東西另外留了一份在 " + kept + "，要找回來請秘書幫忙。"
		done, note = done+"。"+extra, note+extra
	}
	u.setState(st, UpgradePhaseDone, done)
	u.notify("已退回到 "+last.From, note)
	return nil
}

// ---- 小工具 --------------------------------------------------------------------------------------------

func (u *Upgrader) restart(ctx context.Context) error {
	if u.Restart != nil {
		return u.Restart(ctx)
	}
	svc := host.Service{Runner: host.ExecRunner{}, UID: os.Getuid(), Spec: host.ServiceSpec{Label: host.HomeServiceLabel(u.Home), Exe: u.exe(), Home: u.Home}}
	_, err := svc.Restart(ctx)
	return err
}

// run 在程式資料夾裡跑一個指令，回輸出（含 stderr）與結束碼；EVERGENIE_HOME 指向這個家，路徑補上常見的 go／make 位置。
func (u *Upgrader) run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, int) {
	call, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(call, name, args...)
	cmd.Dir = u.Core
	cmd.Env = append(os.Environ(), "EVERGENIE_HOME="+u.Home)
	if !u.Download { // 原始碼版要編譯；下載版不用 go 與 make，PATH 照原樣
		cmd.Env = append(cmd.Env, "PATH="+os.Getenv("PATH")+":/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:"+filepath.Join(os.Getenv("HOME"), "go", "bin"))
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		}
		return string(out) + err.Error(), 127
	}
	return string(out), 0
}

func (u *Upgrader) build(ctx context.Context) error {
	timeout := u.BuildTimeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	out, code := u.run(ctx, timeout, "make", "build")
	if code != 0 {
		return plainf(msgBuild, "make build exit %d:\n%s", code, strings.TrimSpace(out))
	}
	if _, err := os.Stat(u.exe()); err != nil {
		return plainf(msgBuild, "make build finished but %s is missing", u.exe())
	}
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == ' ' || r == ':' {
			return '-'
		}
		return r
	}, s)
}

// keepPrev 把現在的 bin/ 複製成 bin.prev/（舊的 bin.prev 刪掉）。用複製不用搬：跑著的服務還在用 bin/ 底下的檔案。
func (u *Upgrader) keepPrev() error {
	prev := filepath.Join(u.Core, "bin.prev")
	if err := os.RemoveAll(prev); err != nil {
		return err
	}
	if _, err := os.Stat(u.binDir()); err != nil {
		return nil // 還沒有 bin/（不會發生在安裝好的電腦上）：沒有舊版可留
	}
	return copyTree(u.binDir(), prev)
}

// swapBack 把 bin.prev/ 換回 bin/（壞的那份留 bin.failed/）。
func (u *Upgrader) swapBack() error {
	prev, failed := filepath.Join(u.Core, "bin.prev"), filepath.Join(u.Core, "bin.failed")
	if _, err := os.Stat(prev); err != nil {
		return plainf(msgLostPrev, "no %s", prev)
	}
	_ = os.RemoveAll(failed)
	if _, err := os.Stat(u.binDir()); err == nil {
		if err := os.Rename(u.binDir(), failed); err != nil {
			return err
		}
	}
	return os.Rename(prev, u.binDir())
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
		return nil
	})
}

// ---- 家的備份與還原 -------------------------------------------------------------------------------------

func (u *Upgrader) maxBackupBytes() int64 {
	mb := u.BackupMaxMB
	if mb == 0 {
		mb = 30
	}
	return mb << 20
}

// backupHome 把家打包成 tgz：不收 runtime/、personas/avatars/（圖很大、可以重新同步），也不收大於上限的檔。
// 第 2 版（DEV-145）只收家裡：狀態資料夾（暫存、密鑰、官方角色快取、別人的套件）不在包裡，退回時也不還原它
// （那些可以重建或本來就不隨版本變）；狀態資料夾若設在家裡面也整個略過。家裡的 media/（附件、圖庫）也不收：
// 升級不會動它，而且很大（第 1 版它在 runtime/ 底下，本來就不收）。
func (u *Upgrader) backupHome(dest string) error {
	layout := platform.LayoutFor(u.Home)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	walkErr := filepath.WalkDir(u.Home, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(u.Home, path)
		if rel == "." {
			return nil
		}
		slash := filepath.ToSlash(rel)
		if layout.V2 && path == layout.State {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if slash == "runtime" || strings.HasPrefix(slash, "runtime/") || (layout.V2 && (slash == "media" || strings.HasPrefix(slash, "media/"))) || slash == "personas/avatars" || strings.HasPrefix(slash, "personas/avatars/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !d.IsDir() {
			return nil
		}
		if info.Mode().IsRegular() && info.Size() > u.maxBackupBytes() {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = slash
		if d.IsDir() {
			hdr.Name += "/"
		}
		var in *os.File
		if !d.IsDir() {
			// 先開檔再寫表頭：備份途中被刪掉的檔直接略過，不留下只有表頭的壞項目。
			in, err = os.Open(path)
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			defer in.Close()
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if in == nil {
			return nil
		}
		return copyExact(tw, in, hdr.Size)
	})
	if err := tw.Close(); walkErr == nil {
		walkErr = err
	}
	if err := gz.Close(); walkErr == nil {
		walkErr = err
	}
	if err := f.Close(); walkErr == nil {
		walkErr = err
	}
	if walkErr != nil {
		_ = os.Remove(tmp)
		return walkErr
	}
	return os.Rename(tmp, dest)
}

// copyExact 從 r 複製剛好 n 個位元組給 w（tar 要求內容長度跟表頭一樣）。備份途中還在寫入的檔（serve 的日誌、佇列）會變長或變短：
// 變長只收表頭記的那麼多（那一刻的內容），變短補零——不然整個備份會 "archive/tar: write too long" 失敗而不能升級（release-e2e 抓到的）。
func copyExact(w io.Writer, r io.Reader, n int64) error {
	written, err := io.CopyN(w, r, n)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if rest := n - written; rest > 0 {
		_, err = io.CopyN(w, zeroReader{}, rest)
		return err
	}
	return nil
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// inRestoreScope：退回時「資料回到升級前」會動到的路徑（相對於家，斜線分隔）：home.toml、遷移紀錄、vault/ 裡核心管的資料夾。
// 其他的（packages/、personas/、settings/ 其他檔、主人自己放在 vault/ 的東西）不碰：那些不是資料整理會改的，
// 而且可能是主人或秘書在升級之後才修好的。所以家的遷移只能改這個範圍裡的東西（HomeMigration 的規矩）。
func inRestoreScope(slash string) bool {
	return slash == "home.toml" || slash == MigrationsFile || vault.CorePath(slash)
}

// restoreHome 把備份裡「資料整理會動到的範圍」（inRestoreScope）寫回家，蓋掉遷移改過的；備份時不存在的
// settings/migrations.toml 拿掉（遷移的紀錄要跟著還原）。prune＝連備份裡沒有的也拿掉：vault/ 核心資料夾裡、備份之後才出現的
// 紀錄（新版用新格式寫的，舊版可能讀不懂、會讓退回後的檢查不過）。只在手動退回用，呼叫前要先另外留一份現在的家。
// 不拿掉的：不是一般檔案的、大到備份本來就不收的（它不在備份裡不代表是新的）。
func (u *Upgrader) restoreHome(src string, prune bool) error {
	before, _ := AppliedMigrations(u.Home)
	if err := u.restoreFiles(src, prune); err != nil {
		return err
	}
	return undoReverted(u.Home, before)
}

// undoReverted 對還原後「從跑過變成沒跑過」的遷移叫它的 Undo（HomeMigration.Undo），照登記的反序。
func undoReverted(home string, before []string) error {
	after, _ := AppliedMigrations(home)
	still := map[string]bool{}
	for _, id := range after {
		still[id] = true
	}
	had := map[string]bool{}
	for _, id := range before {
		had[id] = true
	}
	for i := len(HomeMigrations) - 1; i >= 0; i-- {
		m := HomeMigrations[i]
		if m.Undo == nil || !had[m.ID] || still[m.ID] {
			continue
		}
		if err := m.Undo(home); err != nil {
			return fmt.Errorf("退回遷移 %s 的收尾沒做完：%w", m.ID, err)
		}
	}
	return nil
}

// restoreFiles 是 restoreHome 寫回檔案的部分。
func (u *Upgrader) restoreFiles(src string, prune bool) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	inBackup := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(filepath.FromSlash(hdr.Name))
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		slash := filepath.ToSlash(name)
		if hdr.Typeflag != tar.TypeReg || !inRestoreScope(slash) {
			continue
		}
		inBackup[slash] = true
		target := filepath.Join(u.Home, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		tmp := target + ".restore-tmp"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(hdr.Mode).Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			_ = os.Remove(tmp)
			return err
		}
		if err := out.Close(); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	if !inBackup[MigrationsFile] {
		_ = os.Remove(filepath.Join(u.Home, filepath.FromSlash(MigrationsFile)))
	}
	if !prune {
		return nil
	}
	root := filepath.Join(u.Home, "vault")
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(u.Home, path)
		slash := filepath.ToSlash(rel)
		if d.IsDir() {
			if path != root && !inRestoreScope(slash) {
				return filepath.SkipDir
			}
			return nil
		}
		if inBackup[slash] || !inRestoreScope(slash) || !d.Type().IsRegular() {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > u.maxBackupBytes() {
			return nil
		}
		return os.Remove(path)
	})
	return err
}
