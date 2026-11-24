package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
)

// TestInitBuildsNewLayout（DEV-154 第 1 件）：新家一開始就是佈局第 2 版、官方角色放家外：家裡只有主人的東西
// （home.toml、settings/、vault/、personas/mine/），沒有 runtime/、secrets/，官方角色資料夾在狀態資料夾。
func TestInitBuildsNewLayout(t *testing.T) {
	root := t.TempDir()
	home, state := filepath.Join(root, "home"), filepath.Join(root, "state")
	if _, err := initHome(initOptions{Home: home, Name: "測試", Timezone: "Asia/Taipei", Port: 18998, State: state}); err != nil {
		t.Fatal(err)
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		t.Fatal(err)
	}
	l := h.Paths()
	if !l.V2 || l.PersonasAtHome || l.State != state {
		t.Fatalf("新家要是第 2 版、官方角色放家外：%+v", l)
	}
	entries, _ := os.ReadDir(home)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if got := strings.Join(names, ","); got != "home.toml,personas,settings,vault" {
		t.Errorf("家裡只放主人的東西：%s", got)
	}
	if sub, _ := os.ReadDir(filepath.Join(home, "personas")); len(sub) != 1 || sub[0].Name() != "mine" {
		t.Errorf("家裡的 personas/ 只有 mine/：%v", sub)
	}
	for _, d := range []string{l.Runtime("logs"), l.Runtime("sockets"), l.OfficialPersonas()} {
		if !strings.HasPrefix(d, state+string(filepath.Separator)) {
			t.Errorf("%s 要在狀態資料夾", d)
		}
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Errorf("沒建 %s", d)
		}
	}
}

// TestInitRefusesSharedState：狀態資料夾已經有別的家在用（有東西）就拒絕，家與狀態資料夾都不動；狀態資料夾在家裡面也拒絕。
func TestInitRefusesSharedState(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(state, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	_, err := initHome(initOptions{Home: home, Name: "測試", Timezone: "Asia/Taipei", Port: 18997, State: state})
	if err == nil || !strings.Contains(err.Error(), "狀態資料夾已經有東西") {
		t.Fatalf("要拒絕共用狀態資料夾：%v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Error("拒絕時不能留下半個家")
	}
	if _, err := os.Stat(filepath.Join(state, "runtime")); err != nil {
		t.Error("別人的狀態資料夾不能動")
	}
	if _, err := initHome(initOptions{Home: home, Name: "測試", Timezone: "Asia/Taipei", Port: 18997, State: filepath.Join(home, "state")}); err == nil {
		t.Error("狀態資料夾不能在家裡面")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Error("拒絕時不能留下半個家")
	}
}
