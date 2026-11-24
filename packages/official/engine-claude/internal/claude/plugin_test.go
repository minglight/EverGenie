package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writePlugin(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "skills", "create-feature"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, PluginManifest), []byte(`{"name":"evergenie"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestShippedPluginManifest：本套件資料夾本身就是外掛：有 .claude-plugin/plugin.json（name = evergenie），
// 兩份官方技能在 skills/ 底下，FindPluginDir 從 EVERGENIE_OFFICIAL_PACKAGES_DIR 找得到它。
// Guards: REQ-354
func TestShippedPluginManifest(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, PluginManifest))
	if err != nil {
		t.Fatalf("套件沒有外掛說明檔：%v", err)
	}
	var m struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &m); err != nil || m.Name != "evergenie" {
		t.Fatalf("plugin.json 讀不出 name = evergenie：%v %+v", err, m)
	}
	for _, s := range []string{"create-feature", "request-permission"} {
		if _, err := os.Stat(filepath.Join(root, "skills", s, "SKILL.md")); err != nil {
			t.Fatalf("外掛裡少了技能 %s：%v", s, err)
		}
	}
	env := map[string]string{"EVERGENIE_OFFICIAL_PACKAGES_DIR": filepath.Dir(root)}
	if got := FindPluginDir(func(k string) string { return env[k] }, ""); got != root {
		t.Fatalf("FindPluginDir = %q，要 %q", got, root)
	}
}

// TestFindPluginDir：先看 EVERGENIE_OFFICIAL_PACKAGES_DIR，再看執行檔旁邊的 ../packages/official（符號連結會解開）；
// 資料夾少了外掛說明檔或 skills/ 不算；都找不到回空字串。
// Guards: REQ-354
func TestFindPluginDir(t *testing.T) {
	none := func(string) string { return "" }
	repo := t.TempDir()
	pluginDir := filepath.Join(repo, "packages", "official", "engine-claude")
	writePlugin(t, pluginDir)
	exe := filepath.Join(repo, "bin", "evergenie-engine-claude")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(pluginDir)
	got, _ := filepath.EvalSymlinks(FindPluginDir(none, exe))
	if got != want {
		t.Fatalf("執行檔旁邊：%q，要 %q", got, want)
	}
	// 符號連結（例：~/.local/bin 指到 repo 的 bin/）先解開。
	link := filepath.Join(t.TempDir(), "evergenie-engine-claude")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	got, _ = filepath.EvalSymlinks(FindPluginDir(none, link))
	if got != want {
		t.Fatalf("符號連結：%q，要 %q", got, want)
	}
	// 環境變數優先。
	other := filepath.Join(t.TempDir(), "official")
	writePlugin(t, filepath.Join(other, "engine-claude"))
	env := func(k string) string {
		if k == "EVERGENIE_OFFICIAL_PACKAGES_DIR" {
			return other
		}
		return ""
	}
	if got := FindPluginDir(env, exe); got != filepath.Join(other, "engine-claude") {
		t.Fatalf("EVERGENIE_OFFICIAL_PACKAGES_DIR 要優先：%q", got)
	}
	// 少了說明檔不算。
	bare := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bare, "engine-claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindPluginDir(func(string) string { return bare }, ""); got != "" {
		t.Fatalf("沒有 plugin.json 不算：%q", got)
	}
	if got := FindPluginDir(none, ""); got != "" {
		t.Fatalf("都沒有要回空字串：%q", got)
	}
}

// TestStartArgvPluginDir：有外掛資料夾時開 session 帶 --plugin-dir（在 --add-dir 前面，--add-dir 吃後面所有的值）；
// 沒有就不帶；帳號的設定資料夾（config_dir）不會多出 skills/。
// Guards: REQ-354
func TestStartArgvPluginDir(t *testing.T) {
	v := newEngEnv(t)
	c := v.start("code", nil)
	if slices.Contains(c.Argv, "--plugin-dir") {
		t.Fatalf("沒有外掛資料夾就不帶 --plugin-dir：%q", c.Argv)
	}

	v = newEngEnv(t)
	v.e.d.PluginDir = "/opt/hg/packages/official/engine-claude"
	c = v.start("code", nil)
	got, ok := argvValue(c.Argv, "--plugin-dir")
	if !ok || got != "/opt/hg/packages/official/engine-claude" {
		t.Fatalf("--plugin-dir = %q（%v）：%q", got, ok, c.Argv)
	}
	if slices.Index(c.Argv, "--plugin-dir") > slices.Index(c.Argv, "--add-dir") {
		t.Fatalf("--plugin-dir 要在 --add-dir 前面：%q", c.Argv)
	}
	if _, err := os.Stat(filepath.Join(v.cfgDir, "skills")); !os.IsNotExist(err) {
		t.Fatalf("不能寫進帳號的 skills/：%v", err)
	}
}
