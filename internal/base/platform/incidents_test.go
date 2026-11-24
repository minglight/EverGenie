package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

func addAgent(t *testing.T, home, id string) {
	t.Helper()
	dir := filepath.Join(home, "vault", "agents", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.md"), []byte("---\nid: "+id+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readHomeToml(t *testing.T, home string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "home.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// 舊版的家只寫 [incidents] dispatcher（收單的秘書）：升級遷移把它抄成 owner，新版讀得到事故負責人；dispatcher、reviewer 原樣留著，
// 舊版（只認 dispatcher／reviewer）照樣讀得懂；再跑一次不會再改。
func TestMigrateIncidentsOwnerCopiesDispatcher(t *testing.T) {
	home := configDir(t, homeConfig+"\n[incidents]\n  dispatcher = \"system\"\n  reviewer = \"architect\"\n\n[[extra]]\n  dispatcher = \"nope\"\n")
	addAgent(t, home, "system")
	if err := MigrateIncidentsOwner(home); err != nil {
		t.Fatal(err)
	}
	h, err := LoadHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if h.Incidents.Owner != "system" {
		t.Fatalf("owner = %q，應該是 system", h.Incidents.Owner)
	}
	var old struct {
		Incidents struct {
			Dispatcher string `toml:"dispatcher"`
			Reviewer   string `toml:"reviewer"`
		} `toml:"incidents"`
	}
	after := readHomeToml(t, home)
	if _, err := toml.Decode(after, &old); err != nil || old.Incidents.Dispatcher != "system" || old.Incidents.Reviewer != "architect" {
		t.Fatalf("舊版讀不懂遷移後的設定：%v %+v", err, old)
	}
	if err := MigrateIncidentsOwner(home); err != nil {
		t.Fatal(err)
	}
	if again := readHomeToml(t, home); again != after {
		t.Fatalf("重跑又改了設定：\n%s", again)
	}
}

// 不該動的情況：已經有 owner、dispatcher 那位不在名冊、沒有 [incidents]、dispatcher 不是秘書代號。檔案一個位元都不變（doctor 照舊提醒主人自己定）。
func TestMigrateIncidentsOwnerLeavesOthersAlone(t *testing.T) {
	for name, tail := range map[string]string{
		"已有 owner":     "\n[incidents]\nowner = \"code\"\ndispatcher = \"system\"\n",
		"秘書不在名冊":       "\n[incidents]\ndispatcher = \"ghost\"\n",
		"沒有 incidents": "",
		"代號不合法":        "\n[incidents]\ndispatcher = \"../system\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			home := configDir(t, homeConfig+tail)
			addAgent(t, home, "system")
			before := readHomeToml(t, home)
			if err := MigrateIncidentsOwner(home); err != nil {
				t.Fatal(err)
			}
			if after := readHomeToml(t, home); after != before {
				t.Fatalf("不該改卻改了：\n%s", after)
			}
		})
	}
}
