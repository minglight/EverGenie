package update

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/host"
	"github.com/minglight/evergenie/sdk"
)

// 升級用的極小假環境：origin（bare）裡有一個 Makefile，`make build` 產生一支假的 bin/evergenie（shell 腳本），
// 它會印版本（來自那個 tag 的 ver.txt）、`serve` 時等 SIGTERM、`migrate-home` 時可以改家的檔案並失敗。
// 真的整套（真的編譯、真的 serve）留給 S5 的 make release-e2e。
const fakeMakefile = `build:
	@if [ -f build-fail ]; then echo "編不過：假的錯誤" >&2; exit 2; fi
	@mkdir -p bin
	@printf '#!/bin/sh\ncase "$$1" in\n  version) echo "%s";;\n  serve) trap "exit 0" TERM; while :; do sleep 0.05; done;;\n  migrate-home) if [ "$$2" = "--preview" ]; then if [ -f "$(CURDIR)/preview-fail" ]; then echo "試跑炸了" >&2; exit 1; fi; if [ -f "$(CURDIR)/preview-crash" ]; then echo "panic: 當掉了" >&2; exit 2; fi; if [ -f "$(CURDIR)/preview-old" ]; then echo "用法：evergenie migrate-home [--apply]" >&2; exit 2; fi; exit 0; fi; if [ -f "$(CURDIR)/migrate-fail" ]; then echo migrated > "$$EVERGENIE_HOME/vault/tasks/data.md"; echo "遷移炸了" >&2; exit 1; fi; if [ -f "$(CURDIR)/migrate-ok" ]; then echo migrated > "$$EVERGENIE_HOME/vault/tasks/data.md"; mkdir -p "$$EVERGENIE_HOME/settings"; printf "schema = 1\\napplied = [\\"pkg.demo.one\\"]\\n" > "$$EVERGENIE_HOME/settings/migrations.toml"; fi;;\nesac\n' "$$(cat ver.txt)" > bin/evergenie
	@chmod +x bin/evergenie
`

type upEnv struct {
	t      *testing.T
	root   string
	origin string
	dev    string
	core   string
	home   string
}

func (e *upEnv) git(dir string, args ...string) string {
	e.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("git %v：%v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// release 在 dev 上出一個版本：寫 ver.txt（與可選的失敗開關）、commit、打 annotated tag（說明可帶標題）、推。
func (e *upEnv) release(tag, ver string, files map[string]string) {
	e.t.Helper()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(e.dev, name), []byte(body), 0o644); err != nil {
			e.t.Fatal(err)
		}
	}
	write("ver.txt", ver)
	for _, n := range []string{"build-fail", "migrate-fail", "preview-fail", "preview-crash", "preview-old", "migrate-ok"} {
		_ = os.Remove(filepath.Join(e.dev, n))
	}
	for n, b := range files {
		write(n, b)
	}
	e.git(e.dev, "add", "-A")
	e.git(e.dev, "commit", "-q", "-m", "release "+ver)
	e.git(e.dev, "tag", "-a", "--cleanup=verbatim", tag, "-m", "## 新增\n- "+ver)
	e.git(e.dev, "push", "-q", "origin", "HEAD:main", tag)
}

func newUpEnv(t *testing.T) *upEnv {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("沒有 git")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("沒有 make")
	}
	t.Setenv(host.ServiceModeEnv, "process")
	root := t.TempDir()
	e := &upEnv{t: t, root: root, origin: filepath.Join(root, "origin.git"), dev: filepath.Join(root, "dev"), core: filepath.Join(root, "core"), home: filepath.Join(root, "home")}
	e.git(root, "init", "-q", "--bare", "-b", "main", e.origin)
	e.git(root, "clone", "-q", e.origin, e.dev)
	if err := os.WriteFile(filepath.Join(e.dev, "Makefile"), []byte(fakeMakefile), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dev, ".gitignore"), []byte("bin/\nbin.prev/\nbin.failed/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.release("v0.1.0", "0.1.0", nil)
	e.git(root, "clone", "-q", e.origin, e.core)
	e.git(e.core, "checkout", "-q", "--detach", "v0.1.0")
	if out, err := exec.Command("make", "-C", e.core, "build").CombinedOutput(); err != nil {
		t.Fatalf("make build：%v\n%s", err, out)
	}
	e.home = testHome(t)
	if err := os.MkdirAll(filepath.Join(e.home, "vault", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.home, "vault", "tasks", "data.md"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *upEnv) binVersion(dir string) string {
	out, err := exec.Command(filepath.Join(e.core, dir, "evergenie"), "version").Output()
	if err != nil {
		return "（讀不到）"
	}
	return strings.TrimSpace(string(out))
}

// upgrader 用真的 Service（process 模式）重開，Verify 檢查執行檔印的版本與服務有沒有在跑。
func (e *upEnv) upgrader(channel string) (*Upgrader, *bytes.Buffer, *[]string) {
	var notes []string
	var log bytes.Buffer
	svc := host.Service{Runner: host.ExecRunner{}, Spec: host.ServiceSpec{Exe: filepath.Join(e.core, "bin", "evergenie"), Home: e.home}}
	e.t.Cleanup(func() { _, _ = svc.Stop(context.Background()) })
	u := &Upgrader{Home: e.home, Core: e.core, From: "0.1.0", Channel: channel, Out: &log,
		Notify: func(title, detail string) { notes = append(notes, title) },
		Verify: func(ctx context.Context, want string) error {
			if got := e.binVersion("bin"); got != want {
				return &ErrUpgrade{Message: "執行檔版本是 " + got + "，應該是 " + want}
			}
			if !svc.Status(ctx).Running {
				return &ErrUpgrade{Message: "服務沒有在跑"}
			}
			if want == "0.4.0" {
				return &ErrUpgrade{Message: "假的檢查失敗（0.4.0 是壞版）"}
			}
			return nil
		}}
	if _, err := svc.Start(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	return u, &log, &notes
}

func (e *upEnv) upgradeLog() string {
	data, _ := os.ReadFile(platform.RuntimeDir(e.home, "logs", "upgrade.log"))
	return string(data)
}

func (e *upEnv) headVersion() string {
	return e.git(e.core, "describe", "--tags", "--match", "v[0-9]*", "--always")
}

// 檢查依頻道找目標：release 只看 vX.Y.Z（最大的、不含預發布）；safe 看最新的 safe/*（版本是那個 commit 的 describe）。
// Guards: REQ-280
func TestUpgradeCheckPicksChannel(t *testing.T) {
	e := newUpEnv(t)
	e.release("v0.2.0", "0.2.0", nil)
	e.release("v0.10.0", "0.10.0", nil)
	e.release("v0.11.0-rc1", "0.11.0-rc1", nil)
	e.git(e.dev, "tag", "-a", "safe/2026-10-29", "-m", "n")
	e.git(e.dev, "push", "-q", "origin", "safe/2026-10-29")
	u, _, _ := e.upgrader(platform.UpdateChannelRelease)
	chk, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if chk.Target != "0.10.0" || chk.Tag != "v0.10.0" || !strings.Contains(chk.Notes, "新增") || chk.Current != "0.1.0" || chk.Channel != "release" || chk.Detail != "" {
		t.Fatalf("release：%+v", chk)
	}
	saved, _ := ReadUpgradeCheck(e.home)
	if saved == nil || saved.Target != "0.10.0" || saved.CheckedAt.IsZero() {
		t.Fatalf("check.json：%+v", saved)
	}
	e.release("v0.12.0", "0.12.0", nil) // safe 之後又多了正式版
	u.Channel = platform.UpdateChannelSafe
	chk, _ = u.Check(context.Background())
	if chk.Tag != "safe/2026-10-29" || chk.Target != "0.11.0-rc1" {
		t.Fatalf("safe：%+v", chk)
	}
	u.Channel = platform.UpdateChannelOff
	chk, _ = u.Check(context.Background())
	if chk.Target != "" || !strings.Contains(chk.Detail, "關閉") {
		t.Fatalf("off：%+v", chk)
	}
	// 現在已經是最新、或現在的版本是開發版：沒有新版，原因寫白話。
	u.Channel, u.From = platform.UpdateChannelRelease, "0.12.0"
	if chk, _ = u.Check(context.Background()); chk.Target != "" || !strings.Contains(chk.Detail, "最新") {
		t.Fatalf("最新：%+v", chk)
	}
	u.From = "abc1234"
	if chk, _ = u.Check(context.Background()); chk.Target != "" || !strings.Contains(chk.Detail, "開發中") {
		t.Fatalf("開發版：%+v", chk)
	}
}

// 跳過清單裡的版本不算新版；跳過的是目標時 Target 空、Detail 說跳過了，但清單還是回給畫面。
// Guards: REQ-280
func TestUpgradeSkipsSkipped(t *testing.T) {
	e := newUpEnv(t)
	e.release("v0.2.0", "0.2.0", nil)
	e.release("v0.3.0", "0.3.0", nil)
	u, _, _ := e.upgrader(platform.UpdateChannelRelease)
	if err := AddUpgradeSkipped(e.home, "0.3.0"); err != nil {
		t.Fatal(err)
	}
	chk, _ := u.Check(context.Background())
	if chk.Target != "" || len(chk.Skipped) != 1 || chk.Skipped[0] != "0.3.0" || !strings.Contains(chk.Detail, "跳過") {
		t.Fatalf("%+v", chk)
	}
}

// repo 有沒提交的改動：停，什麼都不動（沒有 bin.prev、HEAD 不變、沒備份）。
// Guards: REQ-280
func TestUpgradeRefusesDirtyRepo(t *testing.T) {
	e := newUpEnv(t)
	e.release("v0.2.0", "0.2.0", nil)
	if err := os.WriteFile(filepath.Join(e.core, "ver.txt"), []byte("改過"), 0o644); err != nil {
		t.Fatal(err)
	}
	u, _, _ := e.upgrader(platform.UpdateChannelRelease)
	head := e.git(e.core, "rev-parse", "HEAD")
	err := u.Apply(context.Background(), "v0.2.0")
	if UpgradeExitCode(err) != 1 || err == nil || !strings.Contains(err.Error(), "沒提交的改動") {
		t.Fatalf("%v", err)
	}
	if e.git(e.core, "rev-parse", "HEAD") != head {
		t.Fatal("HEAD 不能動")
	}
	for _, p := range []string{"bin.prev", filepath.Join(e.home, "runtime", "backups")} {
		if _, err := os.Stat(filepath.Join(e.core, p)); err == nil && p == "bin.prev" {
			t.Fatal("不能留 bin.prev")
		}
	}
	if e.binVersion("bin") != "0.1.0" {
		t.Fatal("bin/ 不能動")
	}
	st, _ := ReadUpgradeState(e.home)
	if st == nil || st.Phase != UpgradePhaseFailed || st.Outcome != sdk.UpdateOutcomeUntouched || st.Next != nextDirty || english(err.Error()) || english(st.Reason) {
		t.Fatalf("state：%+v（%v）", st, err)
	}
	if !strings.Contains(e.upgradeLog(), "ver.txt") {
		t.Fatalf("哪些檔沒提交（英文細節）記在升級日誌：%s", e.upgradeLog())
	}
}

// 升級成功、手動退回（bin.prev 還在＝直接換回；用掉了＝重新編）、壞版（編不過／遷移失敗／檢查沒過）自動換回並跳過那一版、換不回去結束碼 3。
// Guards: REQ-280
func TestUpgradeApplyAndRollback(t *testing.T) {
	ctx := context.Background()
	e := newUpEnv(t)
	e.release("v0.2.0", "0.2.0", nil)
	e.release("v0.3.0", "0.3.0", map[string]string{"build-fail": "x"})
	e.release("v0.3.1", "0.3.1", map[string]string{"migrate-fail": "x"})
	e.release("v0.4.0", "0.4.0", nil)
	u, log, notes := e.upgrader(platform.UpdateChannelRelease)

	// 1. 成功
	if err := u.Apply(ctx, "v0.2.0"); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if e.binVersion("bin") != "0.2.0" || e.binVersion("bin.prev") != "0.1.0" || e.headVersion() != "v0.2.0" {
		t.Fatalf("升級後 bin=%s prev=%s head=%s", e.binVersion("bin"), e.binVersion("bin.prev"), e.headVersion())
	}
	st, _ := ReadUpgradeState(e.home)
	if st == nil || st.Phase != UpgradePhaseDone || !st.OK || st.From != "0.1.0" || st.To != "0.2.0" || st.FinishedAt.IsZero() {
		t.Fatalf("state：%+v", st)
	}
	hist := ReadUpgradeHistory(e.home)
	if len(hist) != 1 || hist[0].Kind != "upgrade" || !hist[0].OK || hist[0].From != "0.1.0" || hist[0].To != "0.2.0" || hist[0].FromCommit == "" {
		t.Fatalf("history：%+v", hist)
	}
	if _, err := os.Stat(filepath.Join(e.home, "runtime", "backups", "upgrade-0.1.0-0.2.0.tgz")); err != nil {
		t.Fatalf("備份：%v", err)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0], "升級完成") {
		t.Fatalf("通知：%v", *notes)
	}
	if _, err := os.Stat(filepath.Join(UpgradeDir(e.home), UpgradeLockFile)); err == nil {
		t.Fatal("鎖要放掉")
	}

	// 2. 手動退回：bin.prev 還在而且是 0.1.0 → 直接換回
	u.From = "0.2.0"
	if err := u.Rollback(ctx); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if e.binVersion("bin") != "0.1.0" || e.headVersion() != "v0.1.0" {
		t.Fatalf("退回後 bin=%s head=%s", e.binVersion("bin"), e.headVersion())
	}
	if !contains(ReadUpgradeSkipped(e.home), "0.2.0") {
		t.Fatal("退回的那一版要跳過")
	}
	hist = ReadUpgradeHistory(e.home)
	if len(hist) != 2 || hist[1].Kind != "rollback" || hist[1].To != "0.1.0" {
		t.Fatalf("history：%+v", hist)
	}

	// 3. 退回但 bin.prev 已經用掉：checkout 那個 commit 重新編
	u.From = "0.1.0"
	if err := u.Apply(ctx, "v0.2.0"); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if err := os.RemoveAll(filepath.Join(e.core, "bin.prev")); err != nil {
		t.Fatal(err)
	}
	u.From = "0.2.0"
	if err := u.Rollback(ctx); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if e.binVersion("bin") != "0.1.0" || e.headVersion() != "v0.1.0" {
		t.Fatalf("重編退回後 bin=%s head=%s", e.binVersion("bin"), e.headVersion())
	}

	// 4. 編不過：自動換回、壞的留 bin.failed、跳過、通知、結束碼 1
	u.From = "0.1.0"
	*notes = nil
	err := u.Apply(ctx, "v0.3.0")
	if UpgradeExitCode(err) != 1 || e.binVersion("bin") != "0.1.0" || e.headVersion() != "v0.1.0" {
		t.Fatalf("編不過：%v bin=%s head=%s\n%s", err, e.binVersion("bin"), e.headVersion(), log.String())
	}
	st, _ = ReadUpgradeState(e.home)
	if st.Phase != UpgradePhaseRolledBack || st.OK || !strings.Contains(st.Reason, "編譯新版失敗") || st.Outcome != sdk.UpdateOutcomeRolledBack || st.Restored ||
		st.Next != nextSkipped || english(st.Reason) || english(err.Error()) || !strings.Contains(err.Error(), nextSkipped) {
		t.Fatalf("state：%+v（%v）", st, err)
	}
	if !strings.Contains(e.upgradeLog(), "make build") {
		t.Fatalf("編譯的英文細節記在升級日誌：%s", e.upgradeLog())
	}
	if !contains(ReadUpgradeSkipped(e.home), "0.3.0") || len(*notes) != 1 || !strings.Contains((*notes)[0], "沒成功，已換回 0.1.0") {
		t.Fatalf("skip=%v notes=%v", ReadUpgradeSkipped(e.home), *notes)
	}
	if !e.svcRunning() {
		t.Fatal("換回後服務要在跑")
	}

	// 5. 遷移失敗：家的檔案被遷移改過 → 用備份還原
	err = u.Apply(ctx, "v0.3.1")
	data, _ := os.ReadFile(filepath.Join(e.home, "vault", "tasks", "data.md"))
	if UpgradeExitCode(err) != 1 || string(data) != "original" || e.binVersion("bin") != "0.1.0" {
		t.Fatalf("遷移失敗：%v data=%q bin=%s\n%s", err, data, e.binVersion("bin"), log.String())
	}
	if _, err := os.Stat(filepath.Join(e.home, "settings", "migrations.toml")); err == nil {
		t.Fatal("備份時沒有的遷移紀錄要拿掉")
	}
	if st, _ := ReadUpgradeState(e.home); st.Outcome != sdk.UpdateOutcomeRolledBack || !st.Restored || english(err.Error()) {
		t.Fatalf("資料用備份還原：%+v（%v）", st, err)
	}

	// 6. 重開後檢查沒過：換回
	err = u.Apply(ctx, "v0.4.0")
	if UpgradeExitCode(err) != 1 || e.binVersion("bin") != "0.1.0" || !strings.Contains(err.Error(), "新版檢查沒過") {
		t.Fatalf("檢查沒過：%v bin=%s", err, e.binVersion("bin"))
	}

	// 7. 換不回去（換回前 bin.prev 被弄丟）：結束碼 3、state failed
	u2 := *u
	u2.Verify = func(ctx context.Context, want string) error {
		_ = os.RemoveAll(filepath.Join(e.core, "bin.prev"))
		return &ErrUpgrade{Message: "假的檢查失敗"}
	}
	err = u2.Apply(ctx, "v0.2.0")
	st, _ = ReadUpgradeState(e.home)
	if UpgradeExitCode(err) != UpgradeExitStranded || st.Phase != UpgradePhaseFailed || !strings.Contains(err.Error(), "換不回") ||
		st.Outcome != sdk.UpdateOutcomeStranded || st.Next != nextRescue || english(err.Error()) || english(st.Reason) {
		t.Fatalf("換不回去：%v %+v", err, st)
	}
	r := readRescue(e.home)
	if r == nil || r.From != "0.1.0" || r.To != "0.2.0" || r.Commit == "" || r.Install != "" {
		t.Fatalf("換不回去要記下換回哪裡：%+v", r)
	}

	// 8. 「退回上一版」（upgrade --rollback 照 rescue.json）：回到升級前的 commit、bin.prev 不在就重新編、重開、檢查；成功就刪掉紀錄。
	*notes = nil
	if err := u.Rollback(ctx); err != nil {
		t.Fatalf("再試一次換回：%v\n%s", err, log.String())
	}
	if e.binVersion("bin") != "0.1.0" || e.headVersion() != "v0.1.0" || readRescue(e.home) != nil || !e.svcRunning() {
		t.Fatalf("換回後 bin=%s head=%s rescue=%+v", e.binVersion("bin"), e.headVersion(), readRescue(e.home))
	}
	if st, _ := ReadUpgradeState(e.home); st.Kind != sdk.UpdateKindRescue || st.Phase != UpgradePhaseRolledBack || st.Outcome != sdk.UpdateOutcomeRolledBack {
		t.Fatalf("state：%+v", st)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0], "已換回 0.1.0") {
		t.Fatalf("通知：%v", *notes)
	}
	// 沒有換不回去的紀錄時，--rollback 照舊退最近一次成功的升級（這裡沒有可以退的，就說白話、不動東西）。
	if err := u.Rollback(ctx); err == nil || english(err.Error()) {
		t.Fatalf("沒有可退的：%v", err)
	}
}

func (e *upEnv) svcRunning() bool {
	svc := host.Service{Runner: host.ExecRunner{}, Spec: host.ServiceSpec{Exe: filepath.Join(e.core, "bin", "evergenie"), Home: e.home}}
	return svc.Status(context.Background()).Running
}

// 同一時間只能有一個升級；掛掉留下的鎖（pid 已經不在）會被接手。
// Guards: REQ-280
func TestUpgradeLock(t *testing.T) {
	e := newUpEnv(t)
	u, _, _ := e.upgrader(platform.UpdateChannelRelease)
	lock := filepath.Join(UpgradeDir(e.home), UpgradeLockFile)
	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte("1\n"), 0o600); err != nil { // pid 1 一定活著
		t.Fatal(err)
	}
	if err := u.Apply(context.Background(), "v0.1.0"); err == nil || !strings.Contains(err.Error(), "已經有一個升級在跑") {
		t.Fatalf("%v", err)
	}
	if err := os.WriteFile(lock, []byte("999999\n"), 0o600); err != nil { // 沒有這個 pid
		t.Fatal(err)
	}
	unlock, err := u.lock()
	if err != nil {
		t.Fatalf("死掉的鎖要接手：%v", err)
	}
	unlock()
}

// FindCore：[update] core 優先；沒寫時從執行檔往上找 bin/ 的上一層，要有 .git 與 Makefile；找不到回空字串。
// Guards: REQ-280
func TestFindCore(t *testing.T) {
	e := newUpEnv(t)
	exe := filepath.Join(e.core, "bin", "evergenie")
	real, _ := filepath.EvalSymlinks(e.core)
	if got := FindCore(nil, exe); got != real {
		t.Fatalf("%q，想要 %q", got, real)
	}
	if got := FindCore(nil, filepath.Join(t.TempDir(), "evergenie")); got != "" {
		t.Fatalf("不在 repo 裡：%q", got)
	}
	h := &platform.Home{}
	h.Update.Core = e.core
	if got := FindCore(h, "/不相干/evergenie"); got != e.core {
		t.Fatalf("core 優先：%q", got)
	}
	h.Update.Core = t.TempDir()
	if got := FindCore(h, exe); got != "" {
		t.Fatalf("core 寫錯就是找不到：%q", got)
	}
}

// MainRepo：主 repo 回自己；從 `git worktree add` 的工作目錄（開發家的 devhome）也回主 repo；不是 git 資料夾原樣回。
// Guards: DEV-93
func TestMainRepoFromWorktree(t *testing.T) {
	e := newUpEnv(t)
	main, _ := filepath.EvalSymlinks(e.core)
	if got := MainRepo(e.core); got != main {
		t.Fatalf("主 repo 回自己：%q，想要 %q", got, main)
	}
	wt := filepath.Join(t.TempDir(), "devhome")
	e.git(e.core, "worktree", "add", "-q", "--detach", wt, "v0.1.0")
	if got := MainRepo(wt); got != main {
		t.Fatalf("從 worktree 要回主 repo：%q，想要 %q", got, main)
	}
	plain := t.TempDir()
	if got := MainRepo(plain); got != plain {
		t.Fatalf("不是 git 資料夾原樣回：%q", got)
	}
	if got := MainRepo(""); got != "" {
		t.Fatalf("空的回空的：%q", got)
	}
}

// 備份途中還在寫入的檔（日誌、佇列）會變長或變短：tar 的長度要照表頭記的，變長只收那麼多、變短補零，不能讓整個備份失敗（release-e2e 抓到 "archive/tar: write too long"）。
// Guards: REQ-280
func TestCopyExactHandlesGrowAndShrink(t *testing.T) {
	var out bytes.Buffer
	if err := copyExact(&out, strings.NewReader("0123456789"), 4); err != nil || out.String() != "0123" {
		t.Fatalf("變長只收表頭記的長度：%q %v", out.String(), err)
	}
	out.Reset()
	if err := copyExact(&out, strings.NewReader("ab"), 5); err != nil || out.String() != "ab\x00\x00\x00" {
		t.Fatalf("變短要補零：%q %v", out.String(), err)
	}
	out.Reset()
	if err := copyExact(&out, strings.NewReader(""), 0); err != nil || out.Len() != 0 {
		t.Fatalf("空檔：%q %v", out.String(), err)
	}
}

// 資料整理先在複本上試跑；整理過資料的升級，主人確認前可以退回而且資料一起回到升級前，確認之後備份刪掉、不能再退（DEV-154）。
func TestUpgradePreviewAcceptAndDataRollback(t *testing.T) {
	ctx := context.Background()
	e := newUpEnv(t)
	e.release("v0.2.0", "0.2.0", map[string]string{"preview-fail": "x", "migrate-ok": "x"})
	e.release("v0.3.0", "0.3.0", map[string]string{"migrate-ok": "x"})
	u, log, notes := e.upgrader(platform.UpdateChannelRelease)
	var reports []UpgradeReport
	u.Report = func(_ context.Context, r UpgradeReport) { reports = append(reports, r) }
	data := func() string {
		b, _ := os.ReadFile(filepath.Join(e.home, "vault", "tasks", "data.md"))
		return string(b)
	}

	// 1. 複本上試跑沒過：真的家沒被整理（不用靠備份還原）、換回舊版
	err := u.Apply(ctx, "v0.2.0")
	if UpgradeExitCode(err) != 1 || !strings.Contains(err.Error(), "試跑沒過") || data() != "original" || e.binVersion("bin") != "0.1.0" {
		t.Fatalf("試跑沒過：%v data=%q bin=%s\n%s", err, data(), e.binVersion("bin"), log.String())
	}
	if PendingAcceptance(e.home) != nil {
		t.Fatal("沒升級成功就沒有要確認的")
	}
	if len(reports) != 1 || reports[0].OK || reports[0].To != "0.2.0" || !strings.Contains(reports[0].Reason, "試跑沒過") {
		t.Fatalf("沒成功也要有升級報告：%+v", reports)
	}
	if _, err := u.Accept(); err == nil {
		t.Fatal("沒有等確認的升級，Accept 要回錯")
	}

	// 2. 升級成功、整理過資料：紀錄記下跑了哪些、備份在哪；等主人確認
	if err := u.Apply(ctx, "v0.3.0"); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	last := PendingAcceptance(e.home)
	if last == nil || last.To != "0.3.0" || len(last.Migrations) != 1 || last.Migrations[0] != "pkg.demo.one" || last.Backup == "" || data() != "migrated\n" {
		t.Fatalf("等確認：%+v data=%q", last, data())
	}
	if r := reports[len(reports)-1]; len(reports) != 2 || !r.OK || len(r.Migrations) != 1 || r.Backup != last.Backup {
		t.Fatalf("成功的升級報告：%+v", reports)
	}
	if n := *notes; !strings.Contains(n[len(n)-1], "升級完成") {
		t.Fatalf("通知：%v", n)
	}

	// 3. 確認前退回：資料回到升級前，升級後的家另外留一份。升級後新增的紀錄（新格式）拿掉；
	// 不是資料整理會動的東西（主人自己放的、秘書修好的套件）不碰。
	after := map[string]string{"vault/tasks/T-new.md": "新格式的紀錄", "vault/我的筆記/n.md": "主人的筆記", "packages/mine/x/fix.txt": "秘書修好的", "after.txt": "別的"}
	for rel, body := range after {
		p := filepath.Join(e.home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	u.From = "0.3.0"
	if err := u.Rollback(ctx); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if data() != "original" || e.binVersion("bin") != "0.1.0" {
		t.Fatalf("退回後 data=%q bin=%s", data(), e.binVersion("bin"))
	}
	if _, err := os.Stat(filepath.Join(e.home, "runtime", "backups", "rollback-0.3.0-0.1.0.tgz")); err != nil {
		t.Fatalf("退回前的家要留一份：%v", err)
	}
	if PendingAcceptance(e.home) != nil {
		t.Fatal("退回之後沒有要確認的")
	}
	for rel := range after {
		_, err := os.Stat(filepath.Join(e.home, filepath.FromSlash(rel)))
		if gone := rel == "vault/tasks/T-new.md"; gone != (err != nil) {
			t.Fatalf("退回後 %s：應該%s", rel, map[bool]string{true: "被拿掉", false: "留著"}[gone])
		}
	}
	if _, err := os.Stat(filepath.Join(e.home, "settings", "migrations.toml")); err == nil {
		t.Fatal("遷移紀錄要跟著回到升級前")
	}
	// 同一次升級不能再退一次
	u.From = "0.1.0"
	if err := u.Rollback(ctx); UpgradeExitCode(err) != 1 || !strings.Contains(err.Error(), "已經退回過了") || e.binVersion("bin") != "0.1.0" {
		t.Fatalf("重複退回要拒絕：%v", err)
	}

	// 4. 再升一次、主人確認：最新那份備份留著（只留最新一份，作者 2026-11-19、11-21），之後不自動退回
	u.From = "0.1.0"
	stale := platform.RuntimeDir(e.home, "backups", "upgrade-0.0.1-0.0.2.tgz")
	if err := os.WriteFile(stale, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := u.Apply(ctx, "v0.3.0"); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("升級成功後，更早的更新前備份要收掉（UPD-16）")
	}
	got, err := u.Accept()
	if err != nil || got.To != "0.3.0" {
		t.Fatalf("確認：%+v %v", got, err)
	}
	if _, err := os.Stat(got.Backup); err != nil {
		t.Fatalf("確認之後升級前的備份要留著（最新一份）：%v", err)
	}
	if PendingAcceptance(e.home) != nil {
		t.Fatal("確認之後沒有要確認的")
	}
	u.From = "0.3.0"
	err = u.Rollback(ctx)
	if UpgradeExitCode(err) != 1 || !strings.Contains(err.Error(), "確認過沒問題") || e.binVersion("bin") != "0.3.0" || data() != "migrated\n" {
		t.Fatalf("確認後退回要拒絕：%v bin=%s data=%q", err, e.binVersion("bin"), data())
	}
}

// 沒整理過資料的升級沒過：換回程式就好，不拿編譯前的備份蓋回資料（舊版在編譯那幾分鐘照常寫的東西不能被倒回去）。
func TestUpgradeSwapBackKeepsDataWhenNothingMigrated(t *testing.T) {
	ctx := context.Background()
	e := newUpEnv(t)
	e.release("v0.4.0", "0.4.0", nil) // 假的檢查會說 0.4.0 是壞版；它沒有資料整理
	u, log, _ := e.upgrader(platform.UpdateChannelRelease)
	data := filepath.Join(e.home, "vault", "tasks", "data.md")
	check := u.Verify
	u.Verify = func(ctx context.Context, want string) error {
		if want == "0.4.0" { // 備份之後才寫的（等於編譯期間舊版寫的新紀錄）
			_ = os.WriteFile(data, []byte("備份之後才寫的"), 0o644)
		}
		return check(ctx, want)
	}
	err := u.Apply(ctx, "v0.4.0")
	if UpgradeExitCode(err) != 1 || e.binVersion("bin") != "0.1.0" {
		t.Fatalf("壞版要換回：%v\n%s", err, log.String())
	}
	if b, _ := os.ReadFile(data); string(b) != "備份之後才寫的" {
		t.Fatalf("沒整理過資料就不能用備份蓋回去：%q", b)
	}
}

// --preview 的結束碼 2：印了用法＝那一版不認得這個參數，照舊往下走；沒印用法（例如當掉）算試跑沒過。
func TestUpgradePreviewExitTwo(t *testing.T) {
	ctx := context.Background()
	e := newUpEnv(t)
	e.release("v0.2.0", "0.2.0", map[string]string{"preview-crash": "x"})
	e.release("v0.3.0", "0.3.0", map[string]string{"preview-old": "x"})
	u, log, _ := e.upgrader(platform.UpdateChannelRelease)
	if err := u.Apply(ctx, "v0.2.0"); UpgradeExitCode(err) != 1 || !strings.Contains(err.Error(), "試跑沒過") || e.binVersion("bin") != "0.1.0" {
		t.Fatalf("當掉要算沒過：%v\n%s", err, log.String())
	}
	if err := u.Apply(ctx, "v0.3.0"); err != nil || e.binVersion("bin") != "0.3.0" {
		t.Fatalf("不認得 --preview 的舊版照樣升：%v\n%s", err, log.String())
	}
}
