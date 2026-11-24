package pkg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

// EditedOfficial 回官方套件資料夾裡被改過的套件 id（排序）：官方資料夾在 git 工作樹裡時，
// 看 git 有沒有未提交的修改、刪除或新增（SEC-302 S8，REQ-333）。
// 沒有發版指紋可比，所以不是 git 工作樹（發版安裝的複製品）、沒有 git 或讀不出來時回空，寧可不報也不誤報。
// 要不要報（沙盒家不報）由呼叫端決定。
// 下載版的官方資料夾（<app>/versions/<版本>/packages/official）、或往上找不到 .git 的，都不叫 git（PKG-21）：
// 乾淨的 Mac 上 /usr/bin/git 是「要先裝開發者工具」的空殼，一叫就跳安裝視窗（doctor 每跑一次跳一次；DEV-155 S9 乾淨環境的整體驗收抓到的）；
// 下載版就算剛好放在別的 git repo 底下（例如整個家目錄是放設定檔的 repo），也不把整包官方套件當成「新增的檔」誤報。
func EditedOfficial(ctx context.Context, home string) []string {
	dir := OfficialDir(home)
	if dir == "" || !isDir(dir) || inDownloadInstall(dir) || !inGitTree(dir) {
		return nil
	}
	gctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	git := func(args ...string) (string, bool) {
		out, err := exec.CommandContext(gctx, "git", append([]string{"-C", dir}, args...)...).Output()
		return string(out), err == nil
	}
	if out, ok := git("rev-parse", "--is-inside-work-tree"); !ok || strings.TrimSpace(out) != "true" {
		return nil
	}
	ids := map[string]bool{}
	collect := func(out string) {
		for _, line := range strings.Split(out, "\n") {
			line = filepath.ToSlash(strings.TrimSpace(line))
			if line == "" || strings.HasPrefix(line, "../") {
				continue
			}
			if id := strings.SplitN(line, "/", 2)[0]; id != "" && id != "." {
				ids[id] = true
			}
		}
	}
	// 路徑相對於官方資料夾：已追蹤的修改（含已 add 的）、刪除，與沒被忽略的新檔。
	if out, ok := git("diff", "--relative", "--name-only", "HEAD", "--", "."); ok {
		collect(out)
	}
	if out, ok := git("ls-files", "-m", "-d", "-o", "--exclude-standard", "--", "."); ok {
		collect(out)
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	sort.Strings(list)
	return list
}

// realAbs 回 dir 解開符號連結後的絕對路徑（解不開就用原本的絕對路徑）。
func realAbs(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Clean(dir)
}

// inDownloadInstall：dir（解開符號連結後）是下載版某一版帶的官方資料夾 <app>/versions/<版本>/packages/official（platform.IsDownloadInstall）。
func inDownloadInstall(dir string) bool {
	dir = realAbs(dir)
	ver := filepath.Dir(filepath.Dir(dir))
	if filepath.Base(dir) != "official" || filepath.Base(filepath.Dir(dir)) != "packages" || filepath.Base(filepath.Dir(ver)) != platform.DownloadVersionsDir {
		return false
	}
	return platform.IsDownloadInstall(filepath.Dir(filepath.Dir(ver)))
}

// inGitTree 回 dir（解開符號連結後）或它上面哪一層有沒有 .git（資料夾，或 worktree 的 .git 檔）；只看檔案系統、不叫 git。
func inGitTree(dir string) bool {
	for d := realAbs(dir); ; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return false
		}
		d = parent
	}
}
