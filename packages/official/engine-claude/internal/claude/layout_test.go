package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// TestMain 先清掉家的佈局變數：測試常在秘書的 session 裡跑（核心會帶 EVERGENIE_STATE 等），其他測試都假設第 1 版的位置。
func TestMain(m *testing.M) {
	for _, k := range sdk.EnvPrefixes(sdk.EnvState, sdk.EnvMedia, sdk.EnvOfficialPersonas, sdk.EnvBundledPersonas) {
		os.Unsetenv(k)
	}
	os.Exit(m.Run())
}

// 第 2 版的家（DEV-145）：sockets、帳號設定、hook 的紀錄、session 紀錄在狀態資料夾的 runtime/，附件在 <家>/media/share/。
func TestLayoutV2Paths(t *testing.T) {
	home, state := t.TempDir(), t.TempDir()
	t.Setenv(sdk.EnvState, state)
	t.Setenv(sdk.EnvMedia, filepath.Join(home, "media"))
	rt := filepath.Join(state, "runtime")
	c, err := ServeConfigFromEnv(func(k string) string {
		if k == "EVERGENIE_HOME" {
			return home
		}
		return ""
	}, "/opt/hg/bin")
	if err != nil {
		t.Fatal(err)
	}
	if c.APISocket != filepath.Join(rt, "sockets", "api.sock") || c.EngineSocket != filepath.Join(rt, "sockets", "engine-claude.sock") {
		t.Fatalf("sockets 要在狀態資料夾：%+v", c)
	}
	if dir, _ := (Generator{Home: home}).Dir("acct"); dir != filepath.Join(rt, "engine-claude", "acct") {
		t.Fatalf("帳號設定資料夾：%s", dir)
	}
	if p := turnFilePath(home, "code"); p != filepath.Join(rt, "engine-claude", "turns", "code.json") {
		t.Fatalf("hook 的紀錄：%s", p)
	}
}

// 第 2 版：開 session 時附件資料夾（--add-dir）是 <家>/media/share/；家的佈局變數原樣傳給 session（秘書跑的工具要用）。
func TestLayoutV2StartSession(t *testing.T) {
	v := newEngEnv(t)
	state := t.TempDir()
	layout := map[string]string{sdk.EnvState: state, sdk.EnvMedia: filepath.Join(v.home, "media"), sdk.EnvOfficialPersonas: filepath.Join(state, "personas")}
	for k, val := range layout {
		t.Setenv(k, val)
	}
	host := append([]string(nil), hostEnviron...)
	for k, val := range layout {
		host = append(host, k+"="+val)
	}
	v.e.d.Environ = func() []string { return append([]string(nil), host...) }
	c := v.start("code", nil)
	share := filepath.Join(v.home, "media", "share")
	if d, _ := argvValue(c.Argv, "--add-dir"); d != share {
		t.Fatalf("附件資料夾要是 media/share：%q", c.Argv)
	}
	if info, err := os.Stat(share); err != nil || !info.IsDir() {
		t.Fatalf("附件資料夾要建好：%v", err)
	}
	if _, err := os.Stat(filepath.Join(v.home, "runtime", "share")); !os.IsNotExist(err) {
		t.Fatal("第 2 版不該在家裡建 runtime/share")
	}
	env := envMap(c.Env)
	for k, val := range layout {
		if env[k] != val {
			t.Errorf("session 的 %s = %q，要 %q", k, env[k], val)
		}
	}
}
