package pkg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 套件從 git 網址安裝（REQ-282）：先預覽再確認，記下來源，之後有新的 semver tag 就提示更新。
// git 一律用真的 git（不經 Runner，Runner 是給 go build 用的假替身）；測試用 file:// 指到 t.TempDir() 裡的 repo。

const (
	cloneTimeout    = 120 * time.Second
	lsRemoteTimeout = 20 * time.Second
	outdatedWorkers = 4
	sourceFile      = ".source.toml"
)

var (
	refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@+-]*$`)
	tagPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)
)

// isGitURL：是不是 git 網址（https://、ssh://、git@、file:// 開頭）。其餘當本機路徑。
func isGitURL(s string) bool {
	for _, p := range []string{"https://", "ssh://", "git@", "file://"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// checkSourceURL：網址裡不能帶帳號密碼（會被記進 .source.toml 和 log）。ssh 的使用者名稱（git@）不算。
func checkSourceURL(raw string) error {
	if strings.HasPrefix(raw, "git@") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return platform.Invalid("source", "網址看不懂，請確認有沒有打錯")
	}
	if u.User != nil {
		if _, hasPass := u.User.Password(); hasPass || u.Scheme == "https" {
			return platform.Invalid("source", "網址裡不能帶帳號或密碼；私有的 repo 請先在這台電腦設好 ssh 金鑰，再用 git@ 或 ssh:// 的網址")
		}
	}
	if u.Scheme == "file" && u.Path == "" {
		return platform.Invalid("source", "file:// 網址後面要接資料夾的完整路徑")
	}
	return nil
}

// runGit 跑 git：不問密碼（GIT_TERMINAL_PROMPT=0、ssh BatchMode）、不跑 ext 協定。找不到 git 回 UNAVAILABLE。
func (m *Module) runGit(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
	if m.git != nil {
		return m.git(ctx, timeout, dir, args...)
	}
	return execGit(ctx, timeout, dir, args...)
}

func execGit(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, platform.Unavailable("這台沒有 git，裝好 git 才能從網址安裝套件")
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, append([]string{"-c", "protocol.ext.allow=never"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=true", "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=15")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return out.Bytes(), fmt.Errorf("逾時（超過 %d 秒）", int(timeout.Seconds()))
		}
		return out.Bytes(), err
	}
	return out.Bytes(), nil
}

// gitFailure 把 git 的錯誤換成白話（不把整段輸出丟給使用者）。
func gitFailure(what string, out []byte, err error) error {
	var pe *platform.Err
	if errors.As(err, &pe) {
		return err
	}
	return platform.Unavailable(fmt.Sprintf("%s沒有成功：%s", what, lastLine(out, err)))
}

func lastLine(out []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}

// tagVersion 把 tag（v1.2.0 或 1.2.0）變成 semver；預發行（1.2.0-rc1）與其他寫法不算。
func tagVersion(tag string) (semver, bool) {
	if !tagPattern.MatchString(tag) {
		return semver{}, false
	}
	return parseSemver(strings.TrimPrefix(tag, "v"))
}

// latestTag 從 tag 清單挑最大的 semver tag；沒有回空字串。
func latestTag(tags []string) string {
	best, bestV := "", semver{}
	for _, t := range tags {
		v, ok := tagVersion(strings.TrimSpace(t))
		if !ok {
			continue
		}
		if best == "" || v.compare(bestV) > 0 {
			best, bestV = strings.TrimSpace(t), v
		}
	}
	return best
}

// fetched 是抓下來的來源：src 是工作目錄（可以刪掉 .git 之後複製），root 是要在結束時刪掉的暫存資料夾。
type fetched struct {
	root   string
	src    string
	stage  string             // <root>/<id>：驗過、可以直接 rename 的資料夾（stageSource 填）
	source *sdk.PackageSource // 本機資料夾沒有
	man    *Manifest
	manErr error // manifest 讀不起來或不合格（預覽把它當 Problems，安裝直接回錯誤）
}

func (f *fetched) cleanup() {
	if f != nil && f.root != "" {
		_ = os.RemoveAll(f.root)
	}
}

// fetchGit 把網址抓到 parent/<暫存>/src：clone → 選版本（ref ＞ 最大的 semver tag ＞ 預設分支）→ 記 commit。
// 不持有 m.mu（clone 最長 120 秒，不能卡住監督）。
func (m *Module) fetchGit(ctx context.Context, parent, rawURL, ref, prefix string) (*fetched, error) {
	if err := checkSourceURL(rawURL); err != nil {
		return nil, err
	}
	if ref != "" && !refPattern.MatchString(ref) {
		return nil, platform.Invalid("ref", "版本（ref）只能是 tag 或分支的名稱")
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, platform.Unavailable("無法建立暫存資料夾")
	}
	root := filepath.Join(parent, prefix+shortRand())
	f := &fetched{root: root, src: filepath.Join(root, "src")}
	fail := func(err error) (*fetched, error) { f.cleanup(); return nil, err }
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fail(platform.Unavailable("無法建立暫存資料夾"))
	}
	if out, err := m.runGit(ctx, cloneTimeout, root, "clone", "--quiet", "--", rawURL, f.src); err != nil {
		return fail(gitFailure("從網址抓套件", out, err))
	}
	if ref == "" {
		out, err := m.runGit(ctx, cloneTimeout, f.src, "tag", "--list")
		if err != nil {
			return fail(gitFailure("讀版本清單", out, err))
		}
		ref = latestTag(strings.Fields(string(out)))
	}
	if ref != "" {
		if out, err := m.runGit(ctx, cloneTimeout, f.src, "checkout", "--quiet", "--detach", ref); err != nil {
			return fail(platform.Invalid("ref", fmt.Sprintf("這個網址裡沒有版本「%s」：%s", ref, lastLine(out, err))))
		}
	} else if out, err := m.runGit(ctx, cloneTimeout, f.src, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		ref = strings.TrimSpace(string(out)) // 沒有 tag：預設分支
	}
	out, err := m.runGit(ctx, cloneTimeout, f.src, "rev-parse", "HEAD")
	if err != nil {
		return fail(gitFailure("讀 commit", out, err))
	}
	f.source = &sdk.PackageSource{URL: rawURL, Ref: ref, Commit: strings.TrimSpace(string(out)), InstalledAt: m.d.Clock.Now().UTC()}
	return f, nil
}

// writeSource 把來源記進套件資料夾的 .source.toml。
func writeSource(dir string, s *sdk.PackageSource) error {
	var b strings.Builder
	rec := struct {
		URL         string `toml:"url"`
		Ref         string `toml:"ref"`
		Commit      string `toml:"commit"`
		InstalledAt string `toml:"installed_at"`
	}{s.URL, s.Ref, s.Commit, s.InstalledAt.UTC().Format(time.RFC3339)}
	if err := toml.NewEncoder(&b).Encode(rec); err != nil {
		return platform.Internal("無法記錄來源")
	}
	if err := os.WriteFile(filepath.Join(dir, sourceFile), []byte(b.String()), 0o644); err != nil {
		return platform.Unavailable("無法記錄來源")
	}
	return nil
}

// readSource 讀 .source.toml；沒有或壞掉回 nil（當作本機資料夾裝的）。
func readSource(dir string) *sdk.PackageSource {
	data, err := os.ReadFile(filepath.Join(dir, sourceFile))
	if err != nil {
		return nil
	}
	var rec struct {
		URL         string `toml:"url"`
		Ref         string `toml:"ref"`
		Commit      string `toml:"commit"`
		InstalledAt string `toml:"installed_at"`
	}
	if _, err := toml.Decode(string(data), &rec); err != nil || rec.URL == "" {
		return nil
	}
	s := &sdk.PackageSource{URL: rec.URL, Ref: rec.Ref, Commit: rec.Commit}
	if t, err := parseTime(rec.InstalledAt); err == nil {
		s.InstalledAt = t
	}
	return s
}

// problemsFor 把一份抓下來的套件跟目前這個家比對，列出擋住安裝的問題（白話）和警告。
// 需要鎖：讀 m.entries。
func (m *Module) problemsForLocked(man *Manifest, replacing bool) (problems, warnings []string, exists bool) {
	if man.Core != "" {
		ok, known := satisfiesCore(man.Core, m.coreVer())
		switch {
		case !known:
			warnings = append(warnings, fmt.Sprintf("這個套件要 EverGenie %s，但這份 EverGenie 是開發版，認不出版本號，先不擋", man.Core))
		case !ok:
			problems = append(problems, coreMismatch(man.Core, m.coreVer()))
		}
	}
	if isDir(filepath.Join(m.officialDir(), man.ID)) {
		problems = append(problems, "跟官方套件同名；要調整官方的請用它的擴充點，或換個名字做自己的套件")
	}
	if e := m.entries[man.ID]; e != nil && e.tier != sdk.TierOfficial {
		exists = true
		if !replacing {
			v := ""
			if e.man != nil {
				v = e.man.Version
			}
			warnings = append(warnings, fmt.Sprintf("已經裝了「%s」(版本 %s)；要換新版請用更新", man.ID, v))
		}
	} else if e != nil {
		exists = true
	}
	for _, r := range man.Requires {
		dep := m.entries[r.ID]
		switch {
		case dep == nil || dep.err != nil || dep.man == nil:
			problems = append(problems, fmt.Sprintf("需要套件「%s」，但這台沒有安裝或沒有載入", r.ID))
		default:
			if c, _ := parseConstraint(r.Version); !c.allows(dep.man.Version) {
				problems = append(problems, fmt.Sprintf("需要套件「%s」%s，目前裝的是 %s", r.ID, r.Version, dep.man.Version))
			}
		}
	}
	return problems, warnings, exists
}

func coreMismatch(limit, current string) string {
	return fmt.Sprintf("這個套件要 EverGenie %s，你現在是 %s", limit, current)
}

// coreVer 是這個核心的版本字串（測試可以換）。
func (m *Module) coreVer() string {
	if m.coreVersion != nil {
		return m.coreVersion()
	}
	return platform.Version()
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// Preview 先看一個套件（git 網址或本機資料夾）：抓下來、驗過，回內容摘要，不安裝（REQ-282）。
// manifest 不合格不是錯誤而是 Problems（畫面要顯示給使用者看）；網址連不上、版本不存在才是錯誤。
func (m *Module) Preview(ctx context.Context, req *sdk.PackagePreviewRequest) (*sdk.PackagePreviewResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	err := m.readyLocked()
	home := m.home
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	f, err := m.stageSource(ctx, strings.TrimSpace(req.Source), strings.TrimSpace(req.Ref), sdk.TierInstalled, platform.RuntimeDir(home, "packages"), ".preview-")
	if err != nil {
		return nil, err
	}
	defer f.cleanup()
	out := &sdk.PackagePreviewResponse{}
	if f.source != nil {
		out.Ref, out.Commit = f.source.Ref, f.source.Commit
	}
	if f.manErr != nil {
		out.Problems = append(out.Problems, errMessage(f.manErr))
		return out, nil
	}
	man := f.man
	out.ID, out.Name, out.Version, out.Description, out.Kind, out.Core = man.ID, man.Name, man.Version, man.Description, man.Kind, man.Core
	out.RunsProgram = man.Kind == string(sdk.PackageKindProgram)
	for _, a := range man.agents {
		out.Agents = append(out.Agents, a.ID)
	}
	for _, p := range man.Contributes.Page {
		out.Pages = append(out.Pages, p.ID)
	}
	for _, j := range man.Contributes.Job {
		out.Jobs = append(out.Jobs, j.Agent+"/"+j.ID)
	}
	for _, k := range man.dataKinds {
		out.DataKinds = append(out.DataKinds, man.ID+"."+k.Kind)
	}
	out.Reads, out.Writes = sortedCopy(man.Capabilities.Read), sortedCopy(man.Capabilities.Write)
	out.Personas = previewPersonas(man, f.stage)
	m.mu.Lock()
	out.Problems, out.Warnings, out.Exists = m.problemsForLocked(man, false)
	m.mu.Unlock()
	return out, nil
}

// remoteTags 問來源有哪些 tag（git ls-remote，不抓內容）。
func (m *Module) remoteTags(ctx context.Context, rawURL string) ([]string, error) {
	if err := checkSourceURL(rawURL); err != nil {
		return nil, err
	}
	out, err := m.runGit(ctx, lsRemoteTimeout, "", "ls-remote", "--tags", "--refs", "--", rawURL)
	if err != nil {
		return nil, gitFailure("問來源有哪些版本", out, err)
	}
	var tags []string
	for _, line := range strings.Split(string(out), "\n") {
		if _, ref, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok {
			if t := strings.TrimPrefix(ref, "refs/tags/"); t != ref {
				tags = append(tags, t)
			}
		}
	}
	return tags, nil
}

// Outdated 列出從網址裝的套件有沒有新版（看來源最大的 semver tag）。同時最多問 4 個，每個逾時 20 秒；抓不到的放 Failed。
func (m *Module) Outdated(ctx context.Context, req *sdk.PackageOutdatedRequest) (*sdk.PackageOutdatedResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type target struct{ id, name, current, url string }
	m.mu.Lock()
	if err := m.readyLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	var targets []target
	for _, id := range m.sortedIDsLocked() {
		if e := m.entries[id]; e.source != nil && e.tier != sdk.TierOfficial && e.man != nil {
			targets = append(targets, target{id, e.man.Name, e.man.Version, e.source.URL})
		}
	}
	m.mu.Unlock()
	type result struct {
		latest string
		err    error
	}
	results := make([]result, len(targets))
	sem := make(chan struct{}, outdatedWorkers)
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			tags, err := m.remoteTags(ctx, t.url)
			results[i] = result{latestTag(tags), err}
		}()
	}
	wg.Wait()
	out := &sdk.PackageOutdatedResponse{}
	for i, t := range targets {
		r := results[i]
		if r.err != nil {
			out.Failed = append(out.Failed, fmt.Sprintf("「%s」的來源抓不到（%s）", t.name, t.url))
			continue
		}
		latest, ok1 := tagVersion(r.latest)
		cur, ok2 := parseSemver(t.current)
		if r.latest != "" && ok1 && ok2 && latest.compare(cur) > 0 {
			out.Packages = append(out.Packages, sdk.PackageOutdated{ID: t.id, Name: t.name, Current: t.current, Latest: strings.TrimPrefix(r.latest, "v"), URL: t.url})
		}
	}
	return out, nil
}

// Upgrade 把一個從網址裝的套件換成新版：舊資料夾先改名留著，新版 rename 進來、重掃；新版載入失敗（rejected／failed）就換回舊的。
// 資料（vault/pkg/<id>/）不動。附程式的新版編譯是背景進行的，編不過會在之後顯示 failed（不會自動換回，限制寫在 pkg 卡）。
func (m *Module) Upgrade(ctx context.Context, req *sdk.PackageUpgradeRequest) (*sdk.PackageUpgradeResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if err := m.readyLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	e := m.entries[req.ID]
	switch {
	case e == nil || e.man == nil:
		m.mu.Unlock()
		return nil, platform.NotFound(fmt.Sprintf("沒有套件「%s」", req.ID))
	case e.tier == sdk.TierOfficial:
		m.mu.Unlock()
		return nil, platform.Forbidden("官方套件跟著發版更新，不能用網址更新")
	case e.source == nil:
		m.mu.Unlock()
		return nil, platform.Invalid("id", fmt.Sprintf("「%s」不是從網址裝的，沒辦法用網址更新", req.ID))
	}
	dir, tier, src, from := e.dir, e.tier, *e.source, e.man.Version
	root := m.tierDir(tier)
	m.mu.Unlock()

	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		tags, err := m.remoteTags(ctx, src.URL)
		if err != nil {
			return nil, err
		}
		latest := latestTag(tags)
		if latest == "" {
			return nil, platform.Conflict("來源沒有版本標籤（tag），找不到新版")
		}
		lv, _ := tagVersion(latest)
		if cv, ok := parseSemver(from); ok && lv.compare(cv) <= 0 {
			return nil, platform.Conflict(fmt.Sprintf("已經是最新版 %s", from))
		}
		ref = latest
	}
	f, err := m.stageSource(ctx, src.URL, ref, tier, root, ".upgrade-")
	if err != nil {
		return nil, err
	}
	defer f.cleanup()
	if f.manErr != nil {
		return nil, f.manErr
	}
	if f.man.ID != req.ID {
		return nil, platform.Invalid("id", fmt.Sprintf("來源這個版本的套件 id 是「%s」，跟「%s」對不上，沒有更新", f.man.ID, req.ID))
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return nil, err
	}
	if cur := m.entries[req.ID]; cur == nil || cur.dir != dir {
		return nil, platform.Conflict("更新的時候這個套件被改動了，請再試一次")
	}
	if problems, _, _ := m.problemsForLocked(f.man, true); len(problems) > 0 {
		return nil, platform.Invalid("source", strings.Join(problems, "；"))
	}
	now := m.d.Clock.Now()
	backup := filepath.Join(root, ".old-"+req.ID+"-"+now.Format("20060102-150405"))
	if err := os.Rename(dir, backup); err != nil {
		return nil, platform.Unavailable("無法備份舊版，沒有更新")
	}
	if err := os.Rename(f.stage, dir); err != nil {
		_ = os.Rename(backup, dir)
		return nil, platform.Unavailable("無法換成新版，沒有更新")
	}
	m.rescanLocked(now)
	m.markChangedLocked(dir)
	p, kerr := m.kickLocked(req.ID)
	if kerr == nil && p.State != sdk.PackageStateRejected && p.State != sdk.PackageStateFailed {
		_ = os.RemoveAll(backup)
		if m.log != nil {
			m.log.Info("更新套件", "id", req.ID, "from", from, "to", f.man.Version, "by", actor(req.By))
		}
		return &sdk.PackageUpgradeResponse{Package: p, From: from, To: f.man.Version}, nil
	}
	reason := "新版載入失敗"
	if p != nil && p.Error != "" {
		reason = p.Error
	}
	// 換回舊的。
	_ = os.RemoveAll(dir)
	if err := os.Rename(backup, dir); err != nil {
		return nil, platform.Unavailable(fmt.Sprintf("新版載入失敗（%s），而且換回舊版也失敗；舊版在 %s", reason, backup))
	}
	m.rescanLocked(m.d.Clock.Now())
	m.markChangedLocked(dir)
	_, _ = m.kickLocked(req.ID)
	if m.log != nil {
		m.log.Warn("更新套件失敗，已換回舊版", "id", req.ID, "to", f.man.Version, "reason", reason)
	}
	return nil, platform.Conflict(fmt.Sprintf("「%s」更新到 %s 沒成功，已換回舊版 %s：%s", req.ID, f.man.Version, from, reason))
}
