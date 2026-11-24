package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// 發版狀態（從核心 internal/api/dev.go 的 dev.release 搬過來，REQ-272、REQ-277、REQ-278）：
// 看本機跑的版本、repo 的 origin/main 最新、還沒部署到本機的 commit、正式版與 safe tag、release_state 的 nightly／next。
// git 一律 exec.CommandContext、不經 shell、不叫 release.sh。

const (
	releasePendingMax = 30
	releaseLogFormat  = "--format=%H%x1f%s%x1f%cI"
)

// Timeouts 是 git 指令的時限：fetch 最多 15 秒，其他本機 git 10 秒（測試可以縮短）。
type Timeouts struct {
	Fetch time.Duration
	Git   time.Duration
}

// DefaultTimeouts 跟核心原本一樣。
var DefaultTimeouts = Timeouts{Fetch: 15 * time.Second, Git: 10 * time.Second}

// ComputeRelease 用本機核心版本計算發版狀態。
func ComputeRelease(ctx context.Context, cfg Config, version string, to Timeouts) *Release {
	res := &Release{Version: version}
	var notes []string
	repo := cfg.Repo
	if fi, err := os.Stat(repo); err != nil || !fi.IsDir() {
		res.Detail = "找不到 repo：" + repo + "（home.toml 的 [admin] repo）"
		return res
	}
	if _, err := exec.LookPath("git"); err != nil {
		res.Detail = "這台沒有 git，沒辦法看發版狀態"
		return res
	}
	if _, err := gitOutput(ctx, to.Fetch, repo, "fetch", "--quiet", "origin", "main"); err != nil {
		notes = append(notes, "抓不到最新的 main，以下是上次抓到的："+err.Error())
	}
	res.Latest = releaseTag(ctx, to.Git, repo, "refs/tags/v[0-9]*")
	res.Safe = releaseTag(ctx, to.Git, repo, "refs/tags/safe/*")
	if res.Latest != nil {
		res.Latest.Version = strings.TrimPrefix(res.Latest.Name, "v")
	}
	if dir := cfg.ReleaseState; dir != "" {
		var n Nightly
		if readJSON(filepath.Join(dir, "nightly.json"), &n) && n.Commit != "" {
			res.Nightly = &n
		}
		var x Next
		if readJSON(filepath.Join(dir, "next.json"), &x) && x.Version != "" {
			res.Next = &x
		}
	}
	out, err := gitOutput(ctx, to.Git, repo, "log", "-1", releaseLogFormat, "origin/main")
	if err != nil || len(parseCommits(out)) == 0 {
		notes = append(notes, "repo 裡找不到 origin/main，沒辦法算還沒部署的 commit")
		res.Detail = strings.Join(notes, "\n")
		return res
	}
	main := parseCommits(out)[0]
	res.Main = &main

	if strings.TrimSpace(version) == "" {
		notes = append(notes, "讀不到這台跑的版本（問不到本機的健康檢查），沒辦法算還沒部署的 commit")
		res.Detail = strings.Join(notes, "\n")
		return res
	}
	host := commitOf(ctx, to.Git, repo, version)
	if host == "" {
		notes = append(notes, "這台跑的版本 "+version+" 在 repo 裡找不到，沒辦法算還沒部署的 commit")
		res.Detail = strings.Join(notes, "\n")
		return res
	}
	out, err = gitOutput(ctx, to.Git, repo, "log", releaseLogFormat, "-n", fmt.Sprint(releasePendingMax+1), host+"..origin/main")
	if err != nil {
		notes = append(notes, "算不出還沒部署的 commit："+err.Error())
	}
	pending := parseCommits(out)
	if len(pending) > releasePendingMax {
		pending, res.PendingMore = pending[:releasePendingMax], true
	}
	res.Pending = pending
	res.Detail = strings.Join(notes, "\n")
	return res
}

func readJSON(path string, v any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

// parseCommits 解析 `--format=%H%x1f%s%x1f%cI` 的輸出。
func parseCommits(out string) []Commit {
	var list []Commit
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(line), "\x1f")
		if len(f) != 3 || f[0] == "" {
			continue
		}
		c := Commit{Commit: f[0], Short: short(f[0]), Subject: f[1]}
		c.At, _ = time.Parse(time.RFC3339, f[2])
		list = append(list, c)
	}
	return list
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// 核心版本字串（scripts/version.sh）：0.2.0、0.2.0-3-gabc1234、加 -dirty；還沒有 v tag 時是 commit 短碼。
var (
	versionCore  = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)(?:-(.+))?$`)
	versionAhead = regexp.MustCompile(`^(?:.*-)?\d+-g([0-9a-f]{4,40})$`)
)

// commitOf 把核心版本字串換成 repo 裡的 commit（找不到回空字串）：短碼（可能帶 -dirty）、X.Y.Z（tag vX.Y.Z）
// 或 X.Y.Z-N-g<短碼>（tag 之後的開發版）。
func commitOf(ctx context.Context, timeout time.Duration, repo, version string) string {
	version = strings.TrimSuffix(strings.TrimSpace(version), "-dirty")
	candidate := version
	if m := versionCore.FindStringSubmatch(version); m != nil {
		candidate = "v" + m[1]
		if m[2] != "" {
			if a := versionAhead.FindStringSubmatch(m[2]); a != nil {
				candidate = a[1]
			} else {
				candidate = "v" + m[1] + "-" + m[2] // 有預發布標記的 tag
			}
		}
	}
	if candidate == "" || strings.HasPrefix(candidate, "-") {
		return ""
	}
	sha, err := gitOutput(ctx, timeout, repo, "rev-parse", "--verify", "--quiet", candidate+"^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(sha)
}

// releaseTag 找符合 pattern 的最新一個 tag（版本排序）；沒有回 nil。
func releaseTag(ctx context.Context, timeout time.Duration, repo, pattern string) *Tag {
	const sep, end = "\x1f", "\x1e"
	out, err := gitOutput(ctx, timeout, repo, "for-each-ref", "--sort=-version:refname", "--count=1",
		"--format=%(refname:short)%1f%(objectname)%1f%(*objectname)%1f%(creatordate:iso-strict)%1f%(contents)%1e", pattern)
	if err != nil {
		return nil
	}
	rec, _, _ := strings.Cut(out, end)
	f := strings.SplitN(strings.TrimLeft(rec, "\n"), sep, 5)
	if len(f) != 5 || f[0] == "" {
		return nil
	}
	t := &Tag{Name: f[0], Commit: f[2], Notes: strings.TrimSpace(f[4])}
	if t.Commit == "" {
		t.Commit = f[1] // 輕量 tag：tag 本身就指到 commit
	}
	t.Short = short(t.Commit)
	t.At, _ = time.Parse(time.RFC3339, f[3])
	return t
}

// gitOutput 跑 `git -C <repo> <args…>`，timeout 內沒做完就中止；失敗回一行白話的錯。
func gitOutput(ctx context.Context, timeout time.Duration, repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("等了 %d 秒沒有回應", int(timeout.Seconds()))
		}
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		first, _, _ := strings.Cut(msg, "\n")
		return "", errors.New(strings.TrimSpace(first))
	}
	return out.String(), nil
}
