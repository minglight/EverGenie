package backupsweep

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 掃描（BSW-2）：只走 Config.Roots 這幾處，不跟捷徑、不進 .git 與 node_modules；名字像備份的檔案或資料夾整個算一項（不再往裡面走）。
// git 管的檔（程式碼、文件裡名字像備份的）不是備份：同一個 git 工作目錄併成一筆「略過」。

// noDescend 是不往裡面走的資料夾名：git 的內容、套件管理器裝的東西（都不是主人留的備份）。
var noDescend = map[string]bool{".git": true, "node_modules": true}

type repo struct {
	root    string
	tracked map[string]bool // git 管的檔與它們的上層資料夾（相對 root，/ 分隔）
	err     error
	loaded  bool
	hits    int
}

type scanner struct {
	ctx     context.Context
	cfg     Config
	loc     *time.Location
	now     time.Time
	repos   map[string]*repo
	order   []*repo
	found   []found
	skipped []sdk.BackupSweepItem
	errs    []string
	notes   []string
}

// found 是名字像備份、不是例外的一項（還沒分類）。
type found struct {
	path string
	info fs.FileInfo
}

// Scan 照設定掃一次、分類，回報告（還沒寫檔）。now 是這次的時間，loc 是這個家的時區。
func Scan(ctx context.Context, cfg Config, now time.Time, loc *time.Location) *sdk.BackupSweepReport {
	if loc == nil {
		loc = time.Local
	}
	s := &scanner{ctx: ctx, cfg: cfg, loc: loc, now: now, repos: map[string]*repo{}}
	rep := &sdk.BackupSweepReport{At: now}
	roots := s.roots()
	for _, r := range roots {
		rep.Roots = append(rep.Roots, s.display(r))
	}
	for _, r := range roots {
		if ctx.Err() != nil {
			s.errs = append(s.errs, "掃描被中斷了，結果不完整")
			break
		}
		s.walk(r, nil)
	}
	s.classify(rep)
	for _, r := range s.order {
		if r.hits > 0 {
			s.skipped = append(s.skipped, sdk.BackupSweepItem{Path: s.display(r.root), Dir: true, State: sdk.BackupSweepSkipped,
				Reason: plural(r.hits) + "名字像備份的檔是 git 管的（程式碼、文件；靠 git 退回，不算備份）"})
		}
		if r.err != nil {
			s.notes = append(s.notes, s.display(r.root)+"：讀不到 git（"+r.err.Error()+"），裡面名字像備份的都當成不是 git 管的列出來")
		}
	}
	sort.Slice(s.skipped, func(i, j int) bool { return s.skipped[i].Path < s.skipped[j].Path })
	rep.Skipped = s.skipped
	rep.Errors = append(rep.Errors, s.errs...)
	rep.Notes = append(rep.Notes, s.notes...)
	return rep
}

func plural(n int) string {
	if n == 1 {
		return "1 個"
	}
	return strconv.Itoa(n) + " 個"
}

// display 把使用者家目錄底下的路徑寫成 ~/…（報告給人看，也不露出帳號名）。
func (s *scanner) display(p string) string {
	if rel, err := filepath.Rel(s.cfg.UserHome, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return "~/" + filepath.ToSlash(rel)
	}
	return p
}

// roots 展開掃描範圍（萬用字元、去重複、包在別的範圍裡的不重掃）。整個使用者家目錄、根目錄、使用者家目錄的上層一律不掃（BSW-N1）。
func (s *scanner) roots() []string {
	home := filepath.Clean(s.cfg.UserHome)
	seen := map[string]bool{}
	var out []string
	for _, pattern := range s.cfg.Roots {
		matches := []string{pattern}
		if strings.ContainsAny(pattern, "*?[") {
			m, err := filepath.Glob(pattern)
			if err != nil {
				s.errs = append(s.errs, "掃描範圍寫錯了："+s.display(pattern))
				continue
			}
			matches = m
		}
		for _, p := range matches {
			p = filepath.Clean(p)
			if p == home || p == "/" || strings.HasPrefix(home+string(filepath.Separator), p+string(filepath.Separator)) {
				s.errs = append(s.errs, "掃描範圍不能是整個使用者家目錄或它的上層："+p+"（略過）")
				continue
			}
			info, err := os.Lstat(p)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				s.errs = append(s.errs, "讀不到 "+s.display(p))
				continue
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				s.notes = append(s.notes, s.display(p)+" 是捷徑，沒有跟進去")
				continue
			}
			if !info.IsDir() || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	// 包在別的範圍裡的（例：這個家在 ~/Workspace 底下）不重掃。
	var kept []string
	for _, p := range out {
		inside := false
		for _, k := range kept {
			if strings.HasPrefix(p, k+string(filepath.Separator)) {
				inside = true
				break
			}
		}
		if !inside {
			kept = append(kept, p)
		}
	}
	return kept
}

// skip 看這個路徑是不是例外；是的話回那一條的原因。
func (s *scanner) skip(p string) (string, bool) {
	for _, sk := range s.cfg.Skips {
		if sk.Path == p {
			return sk.Reason, true
		}
		if ok, err := filepath.Match(sk.Path, p); err == nil && ok {
			return sk.Reason, true
		}
	}
	return "", false
}

func (s *scanner) walk(dir string, in *repo) {
	if s.ctx.Err() != nil {
		return
	}
	if s.cfg.Visit != nil {
		s.cfg.Visit(dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.errs = append(s.errs, "讀不到資料夾 "+s.display(dir))
		return
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			in = s.repoAt(dir)
			break
		}
	}
	for _, e := range entries {
		name := e.Name()
		p := filepath.Join(dir, name)
		if reason, ok := s.skip(p); ok {
			s.skipItem(p, reason)
			continue
		}
		if looksLikeBackup(name) {
			if in != nil && s.tracked(in, p) {
				in.hits++
				continue
			}
			info, err := os.Lstat(p)
			if err != nil {
				s.errs = append(s.errs, "讀不到 "+s.display(p))
				continue
			}
			s.found = append(s.found, found{path: p, info: info})
			continue
		}
		if e.IsDir() && e.Type()&fs.ModeSymlink == 0 && !noDescend[name] {
			s.walk(p, in)
		}
	}
}

func (s *scanner) skipItem(p, reason string) {
	item := sdk.BackupSweepItem{Path: s.display(p), State: sdk.BackupSweepSkipped, Reason: reason}
	if info, err := os.Lstat(p); err == nil {
		// 例外不算大小（vault、附件這類大資料夾每週整個走一遍太浪費）。
		item.Dir, item.ModTime = info.IsDir(), info.ModTime()
		item.Date = info.ModTime().In(s.loc).Format("2006-01-02")
	}
	s.skipped = append(s.skipped, item)
}

func (s *scanner) repoAt(dir string) *repo {
	if r, ok := s.repos[dir]; ok {
		return r
	}
	r := &repo{root: dir}
	s.repos[dir] = r
	s.order = append(s.order, r)
	return r
}

// tracked：p 是 git 管的檔，或底下有 git 管的檔的資料夾。第一次問到這個工作目錄時才跑 git。
func (s *scanner) tracked(r *repo, p string) bool {
	if !r.loaded {
		r.loaded = true
		files, err := s.gitFiles(r.root)
		if err != nil {
			r.err = err
		} else {
			r.tracked = map[string]bool{}
			for _, f := range files {
				for f != "." && f != "" && !r.tracked[f] {
					r.tracked[f] = true
					f = filepath.ToSlash(filepath.Dir(f))
				}
			}
		}
	}
	if r.err != nil {
		return false
	}
	rel, err := filepath.Rel(r.root, p)
	return err == nil && r.tracked[filepath.ToSlash(rel)]
}

func (s *scanner) gitFiles(root string) ([]string, error) {
	if s.cfg.GitFiles != nil {
		return s.cfg.GitFiles(root)
	}
	return gitLsFiles(s.ctx, root)
}

// gitLsFiles 跑 git ls-files（只讀；服務的 PATH 可能沒有 git，找不到就試常見的位置）。
func gitLsFiles(ctx context.Context, root string) ([]string, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		for _, c := range []string{"/usr/bin/git", "/opt/homebrew/bin/git", "/usr/local/bin/git"} {
			if _, e := os.Stat(c); e == nil {
				bin, err = c, nil
				break
			}
		}
	}
	if err != nil {
		return nil, errors.New("找不到 git")
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-C", root, "ls-files", "-z")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	var files []string
	for _, f := range strings.Split(out.String(), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// size：檔案的大小；資料夾是裡面所有一般檔加起來（不跟捷徑）。
func size(ctx context.Context, p string, info fs.FileInfo) int64 {
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && d.Type().IsRegular() {
			if i, e := d.Info(); e == nil {
				total += i.Size()
			}
		}
		return nil
	})
	return total
}

// classify 把找到的分成已到期、不合規、期限內；已到期與不合規的再查有沒有在用。
func (s *scanner) classify(rep *sdk.BackupSweepReport) {
	var results []checkResult
	var names []string
	for _, c := range s.cfg.Checks {
		names = append(names, c.label())
	}
	rep.Checks = names
	if len(s.found) > 0 {
		for _, c := range s.cfg.Checks {
			probe := s.cfg.Probe
			if probe == nil {
				probe = func(c Check) ([]Ref, error) { return runCheck(s.ctx, c) }
			}
			refs, err := probe(c)
			results = append(results, checkResult{name: c.label(), refs: refs, err: err})
			if err != nil {
				s.errs = append(s.errs, "「在用」檢查（"+c.label()+"）沒做成："+err.Error())
			}
		}
	}
	var failed []string
	for _, r := range results {
		if r.err != nil {
			failed = append(failed, r.name)
		}
	}
	checked := "查過：沒有" + strings.Join(names, "、") + "用到它"
	for _, f := range s.found {
		item := sdk.BackupSweepItem{Path: s.display(f.path), Dir: f.info.IsDir(), Size: size(s.ctx, f.path, f.info), ModTime: f.info.ModTime()}
		parsed, problem := parseName(f.info.Name(), s.loc)
		if problem == "" && !parsed.expired(s.now, s.loc) {
			rep.Current++
			rep.CurrentBytes += item.Size
			continue
		}
		used := usedBy(f.path, s.cfg.UserHome, results)
		switch {
		case used != "":
			item.UsedBy = used
		case len(failed) > 0:
			item.Checked = "沒查完：" + strings.Join(failed, "、") + "沒查成，不知道有沒有在用"
		default:
			item.Checked = checked
		}
		if problem != "" {
			item.State, item.Problem = sdk.BackupSweepMisnamed, problem
			item.Date = f.info.ModTime().In(s.loc).Format("2006-01-02")
			item.Suggest = "改名成 " + suggestName(f.info.Name(), f.info.ModTime(), s.loc) + "（補日期與原因）；確定不用了就刪掉"
			if used != "" {
				item.Suggest = "在用，先問過再動；" + item.Suggest
			}
			rep.Misnamed = append(rep.Misnamed, item)
			continue
		}
		item.Date = parsed.date.Format("2006-01-02")
		item.Expires = parsed.expires.Format("2006-01-02")
		switch {
		case used != "":
			item.State = sdk.BackupSweepInUse
		case len(failed) > 0:
			item.State = sdk.BackupSweepUnchecked
		default:
			item.State = sdk.BackupSweepExpired
		}
		rep.Expired = append(rep.Expired, item)
	}
	sort.Slice(rep.Expired, func(i, j int) bool { return rep.Expired[i].Path < rep.Expired[j].Path })
	sort.Slice(rep.Misnamed, func(i, j int) bool { return rep.Misnamed[i].Path < rep.Misnamed[j].Path })
}
