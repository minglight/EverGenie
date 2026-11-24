package platform

import (
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const relayoutHomeToml = "# 主人的註解\nname = \"測試\"\n\n[user]\nname = \"Amy\" # 行尾註解\ntimezone = \"Asia/Taipei\"\n"

// makeV1Home 在 dir 造一個第 1 版的家。
func makeV1Home(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"home.toml":                         relayoutHomeToml,
		"runtime/logs/serve.log":            "log",
		"runtime/share/a.png":               "png-a",
		"runtime/art/x.png":                 "png-x",
		"runtime/queue/1.json":              "{}",
		"secrets/push.toml":                 "key = 1",
		"personas/luna.md":                  "# 露娜",
		"personas/luna.md.bak-20261105-x":   "# 舊露娜",
		"personas/avatars/luna.png":         "avatar",
		"personas/wardrobe/luna/w.toml":     "w = 1",
		"personas/catalog/c.md":             "catalog",
		"personas/mine/my.md":               "mine",
		"personas/_style.md":                "style",
		"personas/wardrobe-mine/x.toml":     "x = 1",
		"personas/README.md":                "readme",
		"packages/mine/p/package.toml":      "name = \"p\"",
		"packages/installed/q/package.toml": "name = \"q\"",
		"vault/agents/x.md":                 "agent",
		"settings/web.toml":                 "port = 1",
	}
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "runtime", "sockets"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// snapshotTree 記下整棵樹的路徑（資料夾以 / 結尾）與檔案內容；搬家留下的備份（-relayout、-reverted）不算。
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if _, err := os.Stat(root); err != nil {
		return out
	}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		if strings.HasSuffix(d.Name(), "-relayout") || strings.HasSuffix(d.Name(), "-reverted") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func diffTrees(t *testing.T, want, got map[string]string) {
	t.Helper()
	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		w, wok := want[k]
		g, gok := got[k]
		switch {
		case !gok:
			t.Errorf("少了 %s", k)
		case !wok:
			t.Errorf("多了 %s", k)
		case w != g:
			t.Errorf("%s 內容不同：%q ≠ %q", k, g, w)
		}
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func newRelayoutDirs(t *testing.T) (home, state string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	state = filepath.Join(root, "state", "home")
	makeV1Home(t, home)
	writeFile(t, filepath.Join(home, "vault", "art", "run.md"), vaultRecord(home))
	writeFile(t, filepath.Join(home, "vault", "art", "other.md"), "# 不相干\n"+home+"/runtime/artwork/x.png\n")
	return home, state
}

// vaultRecord 是一筆寫了絕對路徑的紀錄（像真的家裡的生圖紀錄）。
func vaultRecord(home string) string {
	return "---\nvideo: " + home + "/runtime/art/runs/r1/snow.mp4\n---\n附件 " + home + "/runtime/share/a.png、" + home + "/runtime/art\n"
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRelayoutApplyAndRevert(t *testing.T) {
	home, state := newRelayoutDirs(t)
	before := snapshotTree(t, home)

	p, err := PlanRelayout(home, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) > 0 {
		t.Fatalf("預檢不該有問題：%v", p.Problems)
	}
	if !exists(filepath.Join(home, "runtime")) || exists(state) {
		t.Fatal("預演不該改任何東西")
	}
	stay := map[string]bool{}
	for _, s := range p.Stay {
		stay[filepath.Base(s.From)] = true
	}
	for _, n := range []string{"mine", "_style.md", "wardrobe-mine", "README.md"} {
		if !stay[n] {
			t.Errorf("預演要把 personas/%s 列成留在家裡", n)
		}
	}
	if len(p.Rewrites) != 1 || p.Rewrites[0].Path != filepath.Join("vault", "art", "run.md") || p.Rewrites[0].Count != 3 {
		t.Fatalf("預演要列出 vault/art/run.md 改 3 處（runtime/artwork 不算）：%+v", p.Rewrites)
	}
	if err := ApplyRelayout(p, time.Date(2026, 11, 8, 0, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	rec := filepath.Join(home, "vault", "art", "run.md")
	got, _ := os.ReadFile(rec)
	want := "---\nvideo: " + home + "/media/art/runs/r1/snow.mp4\n---\n附件 " + home + "/media/share/a.png、" + home + "/media/art\n"
	if string(got) != want {
		t.Errorf("vault 紀錄的路徑要改成 media/：\n%s", got)
	}
	if st, _ := os.Stat(rec); st.Mode().Perm() != 0o600 {
		t.Errorf("改寫後權限要照原檔：%v", st.Mode().Perm())
	}
	if orig, _ := os.ReadFile(filepath.Join(home, "settings", "relayout-rewrites", "vault", "art", "run.md")); string(orig) != vaultRecord(home) {
		t.Errorf("原文要存在 settings/relayout-rewrites/：%q", orig)
	}

	for _, gone := range []string{"runtime", "secrets", "packages/installed", "personas/luna.md", "personas/luna.md.bak-20261105-x", "personas/avatars", "personas/wardrobe", "personas/catalog"} {
		if exists(filepath.Join(home, gone)) {
			t.Errorf("家裡不該還有 %s", gone)
		}
	}
	for _, keep := range []string{"media/share/a.png", "media/art/x.png", "personas/mine/my.md", "personas/_style.md", "personas/wardrobe-mine/x.toml", "personas/README.md", "packages/mine/p/package.toml", "vault/agents/x.md", "home.toml.bak-20261108-relayout", "settings/relayout.json"} {
		if !exists(filepath.Join(home, keep)) {
			t.Errorf("家裡要有 %s", keep)
		}
	}
	for _, s := range []string{"runtime/logs/serve.log", "runtime/queue/1.json", "runtime/sockets", "secrets/push.toml", "personas/luna.md", "personas/luna.md.bak-20261105-x", "personas/avatars/luna.png", "personas/wardrobe/luna/w.toml", "personas/catalog/c.md", "packages/installed/q/package.toml"} {
		if !exists(filepath.Join(state, s)) {
			t.Errorf("狀態資料夾要有 %s", s)
		}
	}
	if exists(filepath.Join(state, "runtime", "share")) {
		t.Error("share 要搬到 media/，不該跟著 runtime 走")
	}
	data, _ := os.ReadFile(filepath.Join(home, "home.toml"))
	if !strings.HasPrefix(string(data), "schema = 2\n"+relayoutHomeToml) {
		t.Errorf("home.toml 檔頭加 schema = 2，原本的內容原樣保留：%q", data)
	}
	if v, err := homeLayoutVersion(home); err != nil || v != 2 {
		t.Errorf("搬完 home.toml 要讀得到、有 [layout]：%v %v", v, err)
	}
	l := LayoutFor(home)
	if !l.V2 || l.State != state {
		t.Fatalf("搬完要是第 2 版、狀態資料夾 %s：%+v", state, l)
	}
	if l.Runtime("logs") != filepath.Join(state, "runtime", "logs") || l.Shares() != filepath.Join(home, "media", "share") ||
		l.Secrets("push.toml") != filepath.Join(state, "secrets", "push.toml") || l.OfficialPersonas("luna.md") != filepath.Join(state, "personas", "luna.md") ||
		l.InstalledPackages("q") != filepath.Join(state, "packages", "installed", "q") {
		t.Errorf("第 2 版路徑不對：%+v", l)
	}

	// 已是第 2 版：拒絕再搬。
	p2, err := PlanRelayout(home, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Problems) == 0 || ApplyRelayout(p2, time.Now()) == nil {
		t.Fatal("已是第 2 版要拒絕")
	}

	if err := RevertRelayout(home, time.Date(2026, 11, 8, 0, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	diffTrees(t, before, snapshotTree(t, home))
	if data, _ := os.ReadFile(filepath.Join(home, "home.toml")); string(data) != relayoutHomeToml {
		t.Errorf("退回後 home.toml 要跟原本一樣：%q", data)
	}
	if exists(state) {
		t.Error("退回後搬家時新建的狀態資料夾要刪掉")
	}
	if !exists(filepath.Join(home, "settings", "relayout.json.bak-20261108-reverted")) || exists(RelayoutRecordPath(home)) {
		t.Error("退回後紀錄要改名成 .bak-<日期>-reverted")
	}
	if LayoutFor(home).V2 {
		t.Error("退回後要是第 1 版")
	}
}

func TestRelayoutDestinationOccupied(t *testing.T) {
	home, state := newRelayoutDirs(t)
	if err := os.MkdirAll(filepath.Join(state, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "secrets", "old.toml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, home)
	p, err := PlanRelayout(home, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) == 0 {
		t.Fatal("目的地有東西要列成問題")
	}
	if err := ApplyRelayout(p, time.Now()); err == nil {
		t.Fatal("目的地有東西要拒絕")
	}
	diffTrees(t, before, snapshotTree(t, home))
	if exists(filepath.Join(state, "runtime")) {
		t.Error("不該搬任何東西")
	}
}

func TestRelayoutEmptyDestinationIsFine(t *testing.T) {
	home, state := newRelayoutDirs(t)
	if err := os.MkdirAll(filepath.Join(state, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := PlanRelayout(home, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) > 0 {
		t.Fatalf("空資料夾的目的地可以搬：%v", p.Problems)
	}
	if err := ApplyRelayout(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(state, "secrets", "push.toml")) {
		t.Fatal("密鑰要搬到狀態資料夾")
	}
	if err := RevertRelayout(home, time.Now()); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(state, "secrets")); err != nil || !st.IsDir() {
		t.Error("退回後原本的空資料夾要建回來")
	}
}

func TestRelayoutRejectsStateInsideHome(t *testing.T) {
	home, _ := newRelayoutDirs(t)
	p, err := PlanRelayout(home, filepath.Join(home, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) == 0 {
		t.Fatal("狀態資料夾在家裡面要拒絕")
	}
	if _, err := ResolveStateDir(home, "relative/dir"); err == nil {
		t.Error("相對路徑的 --state 要拒絕")
	}
}

func TestRelayoutRejectsRunningService(t *testing.T) {
	// unix socket 路徑有長度上限：家放在短的暫存資料夾。
	root, err := os.MkdirTemp("", "rl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	home := filepath.Join(root, "h")
	makeV1Home(t, home)
	ln, err := net.Listen("unix", filepath.Join(home, "runtime", "sockets", "api.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	p, err := PlanRelayout(home, filepath.Join(root, "s"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) == 0 || !strings.Contains(strings.Join(p.Problems, ""), "請先停服務") {
		t.Fatalf("服務在跑要拒絕：%v", p.Problems)
	}
	if err := ApplyRelayout(p, time.Now()); err == nil || !exists(filepath.Join(home, "runtime")) {
		t.Fatal("服務在跑不該搬")
	}
}

func TestRelayoutRejectsLivePid(t *testing.T) {
	home, state := newRelayoutDirs(t)
	if err := os.WriteFile(filepath.Join(home, "runtime", "serve.pid"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := PlanRelayout(home, state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(p.Problems, ""), "請先停服務") {
		t.Fatalf("serve.pid 的行程活著要拒絕：%v", p.Problems)
	}
}

func TestRelayoutFailureRollsBack(t *testing.T) {
	home, state := newRelayoutDirs(t)
	before := snapshotTree(t, home)
	// 官方角色的目的地上層是唯讀的：runtime、密鑰等已搬，搬到官方角色時失敗。
	ro := filepath.Join(state, "personas")
	if err := os.MkdirAll(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	p, err := PlanRelayout(home, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) > 0 {
		t.Fatalf("預檢不該有問題：%v", p.Problems)
	}
	if err := ApplyRelayout(p, time.Now()); err == nil {
		t.Fatal("搬不進唯讀資料夾要回錯")
	}
	diffTrees(t, before, snapshotTree(t, home))
	if exists(RelayoutRecordPath(home)) {
		t.Error("自動退回後要拿掉紀錄")
	}
	for _, n := range []string{"runtime", "secrets", "packages"} {
		if exists(filepath.Join(state, n)) {
			t.Errorf("自動退回後狀態資料夾不該有 %s", n)
		}
	}
	if LayoutFor(home).V2 {
		t.Error("失敗後要維持第 1 版")
	}
}

func TestRelayoutRevertWithoutRecord(t *testing.T) {
	home, _ := newRelayoutDirs(t)
	if err := RevertRelayout(home, time.Now()); err == nil {
		t.Fatal("沒有紀錄要回錯")
	}
}

func TestRemoveLayoutTableKeepsOtherLines(t *testing.T) {
	in := "a = 1\n\n[layout] # 手改過\nversion = 2\nstate = \"/x\"\n\n[user]\nname = \"h\"\n"
	got := string(removeLayoutTable([]byte(in), "不相符"))
	want := "a = 1\n\n[user]\nname = \"h\"\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRelayoutRevertEditedRewrite(t *testing.T) {
	home, state := newRelayoutDirs(t)
	p, err := PlanRelayout(home, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyRelayout(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	rec := filepath.Join(home, "vault", "art", "run.md")
	cur, _ := os.ReadFile(rec)
	writeFile(t, rec, string(cur)+"搬家後加的一行\n")
	if err := RevertRelayout(home, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(rec)
	if want := vaultRecord(home) + "搬家後加的一行\n"; string(got) != want {
		t.Errorf("搬家後改過的檔要反向替換、保留新內容：\n%s", got)
	}
}

func TestRewriteTextTildeAndBoundary(t *testing.T) {
	u, err := os.UserHomeDir()
	if err != nil {
		t.Skip("沒有使用者家目錄")
	}
	home := filepath.Join(u, "Workspace", "h")
	in := "a ~/Workspace/h/runtime/art/x.png b " + home + "/runtime/share c ~/Workspace/h/runtime/artwork d ~/Workspace/h2/runtime/art"
	rules := rewriteRules(home, filepath.Join(u, ".evergenie", "state", "h"), false)
	got, n := rewriteText(in, rules, false)
	want := "a ~/Workspace/h/media/art/x.png b " + home + "/media/share c ~/Workspace/h/runtime/artwork d ~/Workspace/h2/runtime/art"
	if got != want || n != 2 {
		t.Errorf("got %q (%d)", got, n)
	}
	back, _ := rewriteText(got, rules, true)
	if back != in {
		t.Errorf("反向要回到原文：%q", back)
	}
}

func TestRelayoutRewriteFailureRollsBack(t *testing.T) {
	home, state := newRelayoutDirs(t)
	ro := filepath.Join(home, "vault", "zz")
	writeFile(t, filepath.Join(ro, "r.md"), vaultRecord(home))
	before := snapshotTree(t, home)
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	p, err := PlanRelayout(home, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rewrites) != 2 {
		t.Fatalf("要改寫 2 個檔：%+v", p.Rewrites)
	}
	if err := ApplyRelayout(p, time.Now()); err == nil {
		t.Fatal("改寫失敗要回錯")
	}
	_ = os.Chmod(ro, 0o755)
	diffTrees(t, before, snapshotTree(t, home))
	if exists(rewritesDir(home)) || exists(RelayoutRecordPath(home)) {
		t.Error("自動退回後要拿掉原文資料夾與紀錄")
	}
}

func TestRewritePersonasAndRuntimeRefs(t *testing.T) {
	home, state := "/h/home", "/s/state"
	in := "a /h/home/personas/avatars/luna.png b /h/home/personas/luna.md c /h/home/personas/luna.md.bak-20261105-x d /h/home/personas/mine/m.md e /h/home/personas/README.md f /h/home/personas/avatarsx/1"
	got, n := rewriteText(in, rewriteRules(home, state, false), false)
	want := "a /s/state/personas/avatars/luna.png b /s/state/personas/luna.md c /s/state/personas/luna.md.bak-20261105-x d /h/home/personas/mine/m.md e /h/home/personas/README.md f /h/home/personas/avatarsx/1"
	if got != want || n != 3 {
		t.Errorf("got %q (%d)", got, n)
	}
	if back, _ := rewriteText(got, rewriteRules(home, state, false), true); back != in {
		t.Errorf("反向要回到原文：%q", back)
	}
	if _, n := rewriteText(in, rewriteRules(home, state, true), false); n != 0 {
		t.Errorf("發行角色包的家不改官方角色的路徑：%d", n)
	}
	refs := "x /h/home/runtime/logs/a.log y runtime/share/b.png z /h/home/media/art/q w /other/runtime/share/k v myruntime/art"
	if c := runtimeRefs(refs, home); c != 2 {
		t.Errorf("runtime 的絕對路徑與相對寫法各一處：%d", c)
	}
}

func TestRelayoutSchemaLineReplacedAndRestored(t *testing.T) {
	home, state := newRelayoutDirs(t)
	orig := "# 頭\nschema = 1 # 版本\nname = \"x\"\n[user]\nschema = 1\n"
	writeFile(t, filepath.Join(home, "home.toml"), orig)
	p, err := PlanRelayout(home, state)
	if err != nil || len(p.Problems) > 0 {
		t.Fatal(err, p.Problems)
	}
	if err := ApplyRelayout(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(home, "home.toml"))
	if !strings.HasPrefix(string(data), "# 頭\nschema = 2\nname = \"x\"\n[user]\nschema = 1\n") {
		t.Errorf("頂層的 schema 那一行改成 2，表裡的不動：%q", data)
	}
	if err := RevertRelayout(home, time.Now()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(home, "home.toml")); string(data) != orig {
		t.Errorf("退回後 home.toml 要跟原本一樣：%q", data)
	}
}

func TestRelayoutPublishingHomeKeepsPersonas(t *testing.T) {
	home, state := newRelayoutDirs(t)
	writeFile(t, filepath.Join(home, "home.toml"), relayoutHomeToml+"\n[admin]\npublic_personas = [\"luna\"]\n")
	writeFile(t, filepath.Join(home, "vault", "p.md"), home+"/personas/avatars/luna.png\n")
	p, err := PlanRelayout(home, state)
	if err != nil || len(p.Problems) > 0 {
		t.Fatal(err, p.Problems)
	}
	if !p.PersonasAtHome {
		t.Fatal("有 public_personas 的家要標成發行角色包的家")
	}
	for _, m := range p.Moves {
		if m.Kind == RelayoutPersonas {
			t.Errorf("發行角色包的家不搬官方角色：%s", m.From)
		}
	}
	if err := ApplyRelayout(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"personas/luna.md", "personas/avatars/luna.png", "personas/catalog/c.md"} {
		if !exists(filepath.Join(home, keep)) {
			t.Errorf("官方角色要留在家裡：%s", keep)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(home, "vault", "p.md")); string(data) != home+"/personas/avatars/luna.png\n" {
		t.Errorf("發行角色包的家不改官方角色的路徑：%q", data)
	}
	l := LayoutFor(home)
	if !l.V2 || !l.PersonasAtHome || l.OfficialPersonas("luna.md") != filepath.Join(home, "personas", "luna.md") {
		t.Errorf("[layout] official_personas = \"home\"：%+v", l)
	}
	if l.Runtime() != filepath.Join(state, "runtime") {
		t.Errorf("暫存照樣在狀態資料夾：%s", l.Runtime())
	}
}

func TestRelayoutPlanListsNotTempAndRuntimeRefs(t *testing.T) {
	home, state := newRelayoutDirs(t)
	writeFile(t, filepath.Join(home, "runtime", "backups", "b.tar"), "b")
	writeFile(t, filepath.Join(home, "runtime", "persona-x", "a"), "a")
	writeFile(t, filepath.Join(home, "vault", "log.md"), home+"/runtime/logs/serve.log\n")
	p, err := PlanRelayout(home, state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.NotTemp, ",") != "backups" {
		t.Errorf("runtime 底下不是暫存的只有 backups：%v", p.NotTemp)
	}
	found := false
	for _, r := range p.RuntimeRefs {
		if r.Path == filepath.Join("vault", "log.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("寫了 runtime/logs 絕對路徑的檔要列出來：%+v", p.RuntimeRefs)
	}
}

func TestRelayoutBackupNameNoClobber(t *testing.T) {
	home, state := newRelayoutDirs(t)
	now := time.Date(2026, 11, 8, 9, 30, 0, 0, time.Local)
	first := filepath.Join(home, "home.toml.bak-20261108-relayout")
	writeFile(t, first, "舊的備份")
	p, err := PlanRelayout(home, state)
	if err != nil || len(p.Problems) > 0 {
		t.Fatal(err, p.Problems)
	}
	if err := ApplyRelayout(p, now); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(first); string(data) != "舊的備份" {
		t.Error("同名的舊備份不能蓋掉")
	}
	if p.Backup != filepath.Join(home, "home.toml.bak-20261108-0930-relayout") || !exists(p.Backup) {
		t.Errorf("撞名要加 -HHMM：%s", p.Backup)
	}
}

// 紀錄停在 moving：一項已 rename、一項還沒 rename，revert 都要照實際位置處理。
func TestRelayoutRevertMovingSteps(t *testing.T) {
	home, state := newRelayoutDirs(t)
	before := snapshotTree(t, home)
	p, err := PlanRelayout(home, state)
	if err != nil || len(p.Problems) > 0 {
		t.Fatal(err, p.Problems)
	}
	if err := ApplyRelayout(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	recPath := RelayoutRecordPath(home)
	rec, err := loadRecord(recPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := range rec.Steps {
		s := &rec.Steps[i]
		switch s.Kind {
		case RelayoutSecrets: // rename 做了、紀錄沒來得及寫 done
			s.Status = "moving"
		case RelayoutInstalled: // 還沒 rename：搬回原位，紀錄是 moving
			if err := os.Rename(s.To, s.From); err != nil {
				t.Fatal(err)
			}
			s.Status = "moving"
		}
	}
	if err := saveRecord(recPath, rec); err != nil {
		t.Fatal(err)
	}
	if err := RevertRelayout(home, time.Now()); err != nil {
		t.Fatal(err)
	}
	diffTrees(t, before, snapshotTree(t, home))
}

// revert 搬到一半失敗：已搬回的再搬去第 2 版，home.toml 與文字檔維持第 2 版。
func TestRelayoutRevertFailureKeepsV2(t *testing.T) {
	home, state := newRelayoutDirs(t)
	p, err := PlanRelayout(home, state)
	if err != nil || len(p.Problems) > 0 {
		t.Fatal(err, p.Problems)
	}
	if err := ApplyRelayout(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	after := snapshotTree(t, home)
	afterState := snapshotTree(t, state)
	calls := 0
	relayoutRename = func(from, to string) error {
		calls++
		if calls == 3 {
			return os.ErrPermission
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { relayoutRename = os.Rename })
	if err := RevertRelayout(home, time.Now()); err == nil {
		t.Fatal("搬回失敗要回錯")
	}
	relayoutRename = os.Rename
	// 紀錄檔的內容（狀態、時間）會變，其他都要跟搬完時一樣。
	got := snapshotTree(t, home)
	delete(got, filepath.Join("settings", "relayout.json"))
	delete(after, filepath.Join("settings", "relayout.json"))
	diffTrees(t, after, got)
	diffTrees(t, afterState, snapshotTree(t, state))
	if !LayoutFor(home).V2 {
		t.Error("失敗後要維持第 2 版")
	}
	// 修好後再退回一次就成功。
	if err := RevertRelayout(home, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// 尾巴對不上（搬完後檔尾又加了別的表）時，退回也要把搬家加的那行註解一起拿掉。
func TestRemoveLayoutTableDropsMarkerWhenTailChanged(t *testing.T) {
	orig := "schema = 1\n[web]\nport = 1\n"
	added := layoutTable([]byte(orig), "/s", false)
	data := orig + added + "\n[sessions]\nautostart = false\n"
	got := string(removeLayoutTable([]byte(data), added))
	if strings.Contains(got, layoutMarker) || strings.Contains(got, "[layout]") {
		t.Fatalf("註解或 [layout] 沒拿掉：\n%s", got)
	}
	if !strings.Contains(got, "[sessions]\nautostart = false") || !strings.HasPrefix(got, orig) {
		t.Fatalf("其他內容被動到：\n%s", got)
	}
}

// 第 2 版期間狀態資料夾的 runtime/ 又長出 art/：退回搬回 runtime 後 art 那步兩邊都有，要停下、維持第 2 版，不能回報成功。
func TestRelayoutRevertLateConflictKeepsV2(t *testing.T) {
	home, state := newRelayoutDirs(t)
	p, err := PlanRelayout(home, state)
	if err != nil || len(p.Problems) > 0 {
		t.Fatal(err, p.Problems)
	}
	if err := ApplyRelayout(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(state, "runtime", "art", "late.png"), "late")
	after := snapshotTree(t, home)
	afterState := snapshotTree(t, state)
	if err := RevertRelayout(home, time.Now()); err == nil {
		t.Fatal("中途撞到兩邊都有，要回錯")
	}
	got := snapshotTree(t, home)
	delete(got, filepath.Join("settings", "relayout.json"))
	delete(after, filepath.Join("settings", "relayout.json"))
	diffTrees(t, after, got)
	diffTrees(t, afterState, snapshotTree(t, state))
	if !LayoutFor(home).V2 || !exists(filepath.Join(home, "media", "art", "x.png")) {
		t.Error("失敗後要維持第 2 版，圖要還在 media/art")
	}
}
