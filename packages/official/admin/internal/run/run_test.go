package run

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// ---- 假 vault ----

type memVault struct {
	mu   sync.Mutex
	recs map[string]sdk.Record // kind/id
	n    int
}

func newMemVault() *memVault { return &memVault{recs: map[string]sdk.Record{}} }

func (m *memVault) List(_ context.Context, kind string, _ bool) ([]sdk.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []sdk.Record
	for _, r := range m.recs {
		if r.Kind == kind && !r.Archived {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memVault) Get(_ context.Context, kind, id string) (*sdk.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.recs[kind+"/"+id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (m *memVault) Put(_ context.Context, rec sdk.Record, rev string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.recs[rec.Kind+"/"+rec.ID]
	if ok && old.Revision != rev || !ok && rev != "" {
		return errors.New("revision 不對")
	}
	m.n++
	rec.Revision = strings.Repeat("r", m.n)
	m.recs[rec.Kind+"/"+rec.ID] = rec
	return nil
}

func (m *memVault) Patch(_ context.Context, kind, id string, set map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.recs[kind+"/"+id]
	if !ok {
		return errors.New("找不到")
	}
	for k, v := range set {
		r.Fields[k] = v
	}
	m.recs[kind+"/"+id] = r
	return nil
}

func (m *memVault) Archive(_ context.Context, kind, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.recs[kind+"/"+id]
	r.Archived = true
	m.recs[kind+"/"+id] = r
	return nil
}

func (m *memVault) Watch(ctx context.Context, _ []string, _ func()) error {
	<-ctx.Done()
	return nil
}

// ---- 假 git repo ----

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@example.invalid", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "f.txt")
	git(t, dir, "commit", "-q", "-m", msg)
	return git(t, dir, "rev-parse", "HEAD")
}

// fakeRepos 建 origin（bare）、開發用 clone（推 commit）、admin 讀的 repo（另一個 clone，靠 fetch 看到新的）。
// 回 admin 的 repo 路徑與三個 commit（c1 推上去之後才 clone，c2、c3 是 clone 之後才推的）。
func fakeRepos(t *testing.T) (repo string, c1, c2, c3 string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("這台沒有 git")
	}
	root := t.TempDir()
	origin, work := filepath.Join(root, "origin.git"), filepath.Join(root, "work")
	repo = filepath.Join(root, "repo")
	git(t, root, "init", "-q", "--bare", "-b", "main", origin)
	git(t, root, "init", "-q", "-b", "main", work)
	git(t, work, "remote", "add", "origin", origin)
	c1 = commit(t, work, "第一版")
	git(t, work, "tag", "-a", "v0.1.0", "-m", "第一個正式版")
	git(t, work, "push", "-q", "origin", "main", "v0.1.0")
	git(t, root, "clone", "-q", origin, repo)
	c2 = commit(t, work, "加功能")
	git(t, work, "tag", "safe/2026-10-30", c2)
	c3 = commit(t, work, "修 bug")
	git(t, work, "push", "-q", "origin", "main", "safe/2026-10-30")
	git(t, repo, "fetch", "-q", "--tags", "origin")
	return repo, c1, c2, c3
}

// 發版狀態：main 是 origin 最新、pending 是本機版本之後的 commit（新到舊）、正式版與 safe tag、release_state 的 nightly／next。
func TestComputeReleaseFromFakeRepo(t *testing.T) {
	repo, c1, c2, c3 := fakeRepos(t)
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "nightly.json"), []byte(`{"commit":"`+c2+`","ok":true,"safe_tag":"safe/2026-10-30"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "next.json"), []byte(`{"version":"0.2.0","base":"0.1.0","notes":"- 加功能"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Repo: repo, ReleaseState: state}
	sv := "0.1.0"
	r := ComputeRelease(context.Background(), cfg, sv, DefaultTimeouts)
	if r.Detail != "" {
		t.Fatalf("detail 應該是空的：%q", r.Detail)
	}
	if r.Main == nil || r.Main.Commit != c3 || r.Main.Short != c3[:7] || r.Main.Subject != "修 bug" || r.Main.At.IsZero() {
		t.Fatalf("main 不對：%+v", r.Main)
	}
	if len(r.Pending) != 2 || r.Pending[0].Commit != c3 || r.Pending[1].Commit != c2 || r.PendingMore {
		t.Fatalf("pending 不對：%+v", r.Pending)
	}
	if r.Latest == nil || r.Latest.Name != "v0.1.0" || r.Latest.Version != "0.1.0" || r.Latest.Commit != c1 || r.Latest.Notes != "第一個正式版" {
		t.Fatalf("latest 不對：%+v", r.Latest)
	}
	if r.Safe == nil || r.Safe.Name != "safe/2026-10-30" || r.Safe.Commit != c2 {
		t.Fatalf("safe 不對：%+v", r.Safe)
	}
	if r.Nightly == nil || !r.Nightly.OK || r.Next == nil || r.Next.Version != "0.2.0" {
		t.Fatalf("nightly／next 不對：%+v %+v", r.Nightly, r.Next)
	}
	if r.Version != sv {
		t.Fatalf("version 應該是本機版本 %q：%q", sv, r.Version)
	}
}

// 本機版本是 commit 短碼也認得；版本照原樣寫進 version。
func TestComputeReleaseShortVersion(t *testing.T) {
	repo, _, c2, c3 := fakeRepos(t)
	sv := c2[:7] + "-dirty"
	r := ComputeRelease(context.Background(), Config{Repo: repo}, sv, DefaultTimeouts)
	if len(r.Pending) != 1 || r.Pending[0].Commit != c3 {
		t.Fatalf("pending 不對：%+v（detail %q）", r.Pending, r.Detail)
	}
	if r.Version != sv {
		t.Fatalf("version 不對：%q", r.Version)
	}
}

// 讀不到本機版本（Version 回錯）：detail 白話說讀不到版本，不寫成「本機的版本  在 repo 裡找不到」（ADM-31）。
func TestComputeReleaseEmptyVersionSaysUnknown(t *testing.T) {
	repo, _, _, c3 := fakeRepos(t)
	r := ComputeRelease(context.Background(), Config{Repo: repo}, "", DefaultTimeouts)
	if r.Main == nil || r.Main.Commit != c3 {
		t.Fatalf("main 還是要讀得到：%+v", r.Main)
	}
	if !strings.Contains(r.Detail, "讀不到這台跑的版本") || strings.Contains(r.Detail, "版本  在") || len(r.Pending) != 0 {
		t.Fatalf("detail 不對：%q（pending %d）", r.Detail, len(r.Pending))
	}
}

// 找不到 repo：白話說要填 [admin] repo，不出錯。
func TestComputeReleaseMissingRepo(t *testing.T) {
	r := ComputeRelease(context.Background(), Config{Repo: filepath.Join(t.TempDir(), "沒有")}, "", DefaultTimeouts)
	if !strings.Contains(r.Detail, "[admin] repo") || r.Main != nil {
		t.Fatalf("detail 不對：%+v", r)
	}
}

// ---- 開發家 ----

func noDoctor(context.Context, string) (*sdk.SystemDoctorResponse, error) {
	return nil, errors.New("connection refused")
}

func okDoctor(context.Context, string) (*sdk.SystemDoctorResponse, error) {
	return &sdk.SystemDoctorResponse{Checks: []sdk.DoctorCheck{{ID: "config.version", Detail: versionDetailPrefix + "0.2.0-1-gabc1234"}}}, nil
}

func devHomeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	toml := "[web]\nport = 1\n[runtime]\nboard_port = 18870 # 看板\nurl = \"https://office.example.invalid:18871\"\n"
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// 開發家三種狀態：沒設定、沒開、開著（版本、port、網址都讀得到）。
func TestDevHomeUnsetStoppedRunning(t *testing.T) {
	ctx := context.Background()
	st, write := ComputeDevHome(ctx, Config{}, okDoctor, time.Second)
	if !write || st.State != DevHomeUnset || !strings.Contains(st.Detail, "dev_home") {
		t.Fatalf("沒設定：%+v", st)
	}
	dir := devHomeDir(t)
	st, _ = ComputeDevHome(ctx, Config{DevHome: dir}, noDoctor, time.Second)
	if st.State != DevHomeStopped || st.Path != dir || st.BoardPort != 18870 || st.Version != "" {
		t.Fatalf("沒開：%+v", st)
	}
	st, _ = ComputeDevHome(ctx, Config{DevHome: dir}, okDoctor, time.Second)
	if st.State != DevHomeRunning || st.Version != "0.2.0-1-gabc1234" || st.BoardPort != 18870 || st.URL != "https://office.example.invalid:18871" {
		t.Fatalf("開著：%+v", st)
	}
	st, _ = ComputeDevHome(ctx, Config{DevHome: "relative/dir"}, okDoctor, time.Second)
	if st.State != DevHomeStopped || !strings.Contains(st.Detail, "完整路徑") {
		t.Fatalf("相對路徑：%+v", st)
	}
}

// 開發家網址只收 http／https。
func TestDevHomeURLOnlyHTTP(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte("[runtime]\nurl = 'javascript:alert(1)'\n[web]\nport = 18870\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := ProbeDevHome(context.Background(), dir, okDoctor, time.Second)
	if st.URL != "" || st.BoardPort != 18870 {
		t.Fatalf("網址應該被丟掉、port 退回 [web]：%+v", st)
	}
}

// ---- 設定與服務 ----

// [admin] 的路徑從家的 home.toml 讀，~ 換成家目錄；沒寫用預設。
func TestLoadConfig(t *testing.T) {
	home := t.TempDir()
	toml := "owner = \"amy\"\n[admin]\nagents = [\n  \"admin\",\n  \"code\",\n]\nrepo = \"~/src/hg\" # 註解\ndev_home = '/tmp/dev'\ndev_site = \"office\" # 舊的跨台設定，不再讀\n[runtime]\nrepo = \"不是這個\"\n"
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := os.UserHomeDir()
	if c.Repo != filepath.Join(u, "src/hg") || c.DevHome != "/tmp/dev" || c.ReleaseState != filepath.Join(u, ".evergenie-release") {
		t.Fatalf("設定不對：%+v", c)
	}
}

// 一輪寫 release/current 與 devhome/current；pending 的請求處理完標 done，看不懂的標 rejected。
func TestServiceRoundAndRequests(t *testing.T) {
	repo, _, _, c3 := fakeRepos(t)
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte("[admin]\nrepo = \""+repo+"\"\nrelease_state = \""+t.TempDir()+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := newMemVault()
	sv := "0.1.0"
	s := NewService(v, home, func(context.Context) (string, error) { return sv, nil }, os.Stderr)
	s.Doctor = noDoctor
	now := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	ctx := context.Background()

	v.recs[KindRequest+"/R-1"] = sdk.Record{Kind: KindRequest, ID: "R-1", Fields: map[string]any{"type": "refresh", "target": "release", "state": "pending"}}
	v.recs[KindRequest+"/R-2"] = sdk.Record{Kind: KindRequest, ID: "R-2", Fields: map[string]any{"type": "rebuild", "state": "pending"}}
	if err := s.HandleRequests(ctx); err != nil {
		t.Fatal(err)
	}
	rel, _ := v.Get(ctx, KindRelease, currentID)
	if rel == nil {
		t.Fatal("沒寫 release/current")
	}
	main, _ := rel.Fields["main"].(map[string]any)
	if main["commit"] != c3 || rel.Fields["computed_at"] != "2026-10-31T09:00:00Z" {
		t.Fatalf("release 內容不對：%+v", rel.Fields)
	}
	if p, _ := rel.Fields["pending"].([]any); len(p) != 2 {
		t.Fatalf("pending 應該有 2 個：%+v", rel.Fields["pending"])
	}
	if r, _ := v.Get(ctx, KindRequest, "R-1"); r.Fields["state"] != "done" {
		t.Fatalf("R-1 應該是 done：%+v", r.Fields)
	}
	if r, _ := v.Get(ctx, KindRequest, "R-2"); r.Fields["state"] != "rejected" || r.Fields["reason"] == "" {
		t.Fatalf("R-2 應該被拒：%+v", r.Fields)
	}
	if dh, _ := v.Get(ctx, KindDevHome, currentID); dh != nil {
		t.Fatal("只要 release 時不該寫 devhome")
	}

	// 定時的一輪：兩份都寫（第二次寫 release 要帶上一版的 revision）
	if err := s.Round(ctx, TargetAll); err != nil {
		t.Fatal(err)
	}
	dh, _ := v.Get(ctx, KindDevHome, currentID)
	if dh == nil || dh.Fields["state"] != DevHomeUnset {
		t.Fatalf("devhome 不對：%+v", dh)
	}

	// 處理完超過 7 天的請求封存
	for id, r := range v.recs {
		if r.Kind == KindRequest {
			r.UpdatedAt = now.Add(-8 * 24 * time.Hour)
			v.recs[id] = r
		}
	}
	if err := s.archiveOld(ctx); err != nil {
		t.Fatal(err)
	}
	if left, _ := v.List(ctx, KindRequest, false); len(left) != 0 {
		t.Fatalf("舊請求應該封存：%+v", left)
	}
}

// 跨台時代寫的 release/current 帶 sites（每台一列：site、role、online、version）：照樣讀得到、算得了，
// 下一輪整筆換成只有本機 version，舊的 sites 不留。
func TestServiceRoundReplacesLegacySites(t *testing.T) {
	repo, _, _, _ := fakeRepos(t)
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte("[admin]\nrepo = \""+repo+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := newMemVault()
	ctx := context.Background()
	legacy := sdk.Record{Kind: KindRelease, ID: currentID, Fields: map[string]any{
		"sites": []any{
			map[string]any{"site": "mini", "role": "hub", "online": true, "version": "0.0.9"},
			map[string]any{"site": "office", "role": "node", "online": false},
		},
		"pending_more": false,
	}}
	if err := v.Put(ctx, legacy, ""); err != nil {
		t.Fatal(err)
	}
	s := NewService(v, home, func(context.Context) (string, error) { return "0.1.0", nil }, os.Stderr)
	s.Doctor = noDoctor
	if err := s.Round(ctx, TargetRelease); err != nil {
		t.Fatalf("舊資料帶 sites 時算不了：%v", err)
	}
	rel, _ := v.Get(ctx, KindRelease, currentID)
	if rel == nil || rel.Fields["version"] != "0.1.0" {
		t.Fatalf("version 應該是本機版本：%+v", rel)
	}
	if _, ok := rel.Fields["sites"]; ok {
		t.Fatalf("舊的 sites 應該被整筆換掉：%+v", rel.Fields)
	}
}
