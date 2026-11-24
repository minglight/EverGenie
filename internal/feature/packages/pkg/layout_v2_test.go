package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// useLayoutV2 把測試的家改成佈局第 2 版（DEV-145），回傳狀態資料夾。
func useLayoutV2(t *testing.T, home string) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(home, "home.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 第 2 版的家頂層是 schema = 2（舊程式才會拒絕啟動）。
	cfg := regexp.MustCompile(`(?m)^schema\s*=\s*1\s*$`).ReplaceAllString(string(data), "schema = 2")
	if !strings.Contains(cfg, "schema = 2") {
		cfg = "schema = 2\n" + cfg
	}
	cfg += fmt.Sprintf("\n[layout]\nversion = 2\nstate = %q\n", state)
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return state
}

// TestLayoutV2PackageEnvAndInstalled：第 2 版的家，套件子行程拿到佈局的環境變數（EVERGENIE_STATE、EVERGENIE_MEDIA、EVERGENIE_OFFICIAL_PERSONAS），
// socket 在狀態資料夾；別人的套件從狀態資料夾的 packages/installed/ 找，主人的套件留在家裡的 packages/mine/。
func TestLayoutV2PackageEnvAndInstalled(t *testing.T) {
	h := newHarness(t)
	state := useLayoutV2(t, h.home.Dir)
	mine := h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	if want := filepath.Join(h.home.Dir, "packages", "mine", "watcher"); mine != want {
		t.Fatalf("主人的套件要在家裡：%s", mine)
	}
	inst := h.write(sdk.TierInstalled, "notes", dataPkg("notes", "1.0.0", ""), nil)
	if want := filepath.Join(state, "packages", "installed", "notes"); inst != want {
		t.Fatalf("別人的套件要在狀態資料夾：%s", inst)
	}
	h.start()
	p := h.lastProc("watcher")
	for k, want := range map[string]string{
		"EVERGENIE_HOME":              h.home.Dir,
		"EVERGENIE_STATE":             state,
		"EVERGENIE_MEDIA":             filepath.Join(h.home.Dir, "media"),
		"EVERGENIE_OFFICIAL_PERSONAS": filepath.Join(state, "personas"),
		"EVERGENIE_API_SOCKET":        filepath.Join(state, "runtime", "sockets", "api.sock"),
	} {
		if got := envValue(p.env, k); got != want {
			t.Errorf("%s = %q，要 %q", k, got, want)
		}
	}
	if got := h.get("notes"); got.Tier != sdk.TierInstalled {
		t.Fatalf("狀態資料夾裡的套件要找得到、是 installed：%+v", got)
	}
}

// TestLayoutV1PackageEnv：第 1 版不帶佈局的環境變數（外面帶進來的也拿掉），套件自己讀 home.toml 退回家裡的位置；
// 這樣搬家前就開著的行程，搬完也會照新位置走。
func TestLayoutV1PackageEnv(t *testing.T) {
	t.Setenv("EVERGENIE_STATE", "/別的家")
	h := newHarness(t)
	h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.start()
	p := h.lastProc("watcher")
	for _, k := range []string{"EVERGENIE_STATE", "EVERGENIE_MEDIA", "EVERGENIE_OFFICIAL_PERSONAS"} {
		if got := envValue(p.env, k); got != "" {
			t.Errorf("%s = %q，第 1 版不該帶", k, got)
		}
	}
}

// TestPackageEnvBundledPersonas：套件子行程拿到跟著程式的公開卡資料夾 EVERGENIE_BUNDLED_PERSONAS（官方套件資料夾的 personas/cards/，PRS-25），
// 外面帶進來的同名變數換成這個家的；套件用 sdk.OfficialCardPath 找官方卡（官方資料夾先、沒有才用這裡的）。
func TestPackageEnvBundledPersonas(t *testing.T) {
	t.Setenv(sdk.EnvBundledPersonas, "/別的程式/cards")
	h := newHarness(t)
	h.write(sdk.TierMine, "watcher", programPkg("watcher", "", ""), programFiles("watcher"))
	h.start()
	p := h.lastProc("watcher")
	if got, want := envValue(p.env, sdk.EnvBundledPersonas), filepath.Join(h.official, "personas", "cards"); got != want {
		t.Fatalf("%s = %q，要 %q", sdk.EnvBundledPersonas, got, want)
	}
	n := 0
	for _, kv := range p.env {
		if strings.HasPrefix(kv, sdk.EnvBundledPersonas+"=") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%s 只能有一個（外面帶進來的要拿掉）：%d", sdk.EnvBundledPersonas, n)
	}
}
