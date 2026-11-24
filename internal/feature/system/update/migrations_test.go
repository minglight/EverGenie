package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
)

func migHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	toml := "[user]\nname = \"測試\"\ntimezone = \"Asia/Taipei\"\n[slots]\ndefault = \"life\"\n[slots.life]\nlabel = \"日常\"\nremind = \"08:30\"\ncheck = \"18:00\"\n[accounts]\ndefault = \"main\"\n[accounts.main]\nengine = \"x\"\n[web]\nport = 18850\n"
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// 遷移依序跑、每個只跑一次（記在 settings/migrations.toml）；再跑一次什麼都不做；之後新加的只跑新的。
// Guards: REQ-280
func TestHomeMigrationsRunOnceInOrder(t *testing.T) {
	home := migHome(t)
	var order []string
	mk := func(id string) HomeMigration {
		return HomeMigration{ID: id, Description: "整理 " + id, Run: func(string) error { order = append(order, id); return nil }}
	}
	list := []HomeMigration{mk("a"), mk("b")}
	pending, err := PendingMigrations(home, list)
	if err != nil || len(pending) != 2 {
		t.Fatalf("%v %v", pending, err)
	}
	ran, err := RunMigrations(home, list)
	if err != nil || strings.Join(ran, ",") != "a,b" || strings.Join(order, ",") != "a,b" {
		t.Fatalf("ran=%v order=%v err=%v", ran, order, err)
	}
	applied, _ := AppliedMigrations(home)
	if strings.Join(applied, ",") != "a,b" {
		t.Fatalf("applied=%v", applied)
	}
	if ran, err = RunMigrations(home, list); err != nil || len(ran) != 0 || len(order) != 2 {
		t.Fatalf("第二次不該再跑：%v %v %v", ran, order, err)
	}
	list = append(list, mk("c"))
	if ran, err = RunMigrations(home, list); err != nil || strings.Join(ran, ",") != "c" || strings.Join(order, ",") != "a,b,c" {
		t.Fatalf("新加的才跑：%v %v %v", ran, order, err)
	}
	// 記錄檔是合法 TOML、家照樣讀得起來（含 settings 檢查）
	if _, err := platform.LoadHome(home); err != nil {
		t.Fatal(err)
	}
	if ran, err = RunMigrations(home, nil); err != nil || len(ran) != 0 {
		t.Fatalf("核心現在沒有遷移：%v %v", ran, err)
	}
	if _, err := RunHomeMigrations(home); err != nil {
		t.Fatal(err)
	}
}

// 一個失敗就停：前面成功的有記下來、失敗的與後面的沒有；錯誤白話指出是哪一個；修好再跑會從失敗的那個接著做。
// Guards: REQ-280
func TestHomeMigrationFailureStops(t *testing.T) {
	home := migHome(t)
	boom := true
	var order []string
	list := []HomeMigration{
		{ID: "a", Description: "第一個", Run: func(string) error { order = append(order, "a"); return nil }},
		{ID: "b", Description: "會失敗的", Run: func(string) error {
			order = append(order, "b")
			if boom {
				return errors.New("磁碟滿了")
			}
			return nil
		}},
		{ID: "c", Description: "第三個", Run: func(string) error { order = append(order, "c"); return nil }},
	}
	ran, err := RunMigrations(home, list)
	if err == nil || strings.Join(ran, ",") != "a" || strings.Join(order, ",") != "a,b" || !strings.Contains(err.Error(), "會失敗的") || !strings.Contains(err.Error(), "磁碟滿了") {
		t.Fatalf("ran=%v order=%v err=%v", ran, order, err)
	}
	if applied, _ := AppliedMigrations(home); strings.Join(applied, ",") != "a" {
		t.Fatalf("applied=%v", applied)
	}
	boom = false
	ran, err = RunMigrations(home, list)
	if err != nil || strings.Join(ran, ",") != "b,c" || strings.Join(order, ",") != "a,b,b,c" {
		t.Fatalf("ran=%v order=%v err=%v", ran, order, err)
	}
	if _, err := RunMigrations(home, []HomeMigration{{ID: "x"}}); err == nil {
		t.Fatal("沒有 Run 的遷移要被擋")
	}
	if _, err := RunMigrations(home, []HomeMigration{{ID: "x", Run: func(string) error { return nil }}, {ID: "x", Run: func(string) error { return nil }}}); err == nil {
		t.Fatal("重複 id 要被擋")
	}
}

// 套件自己帶的資料轉換（DEV-154）排在核心的遷移後面跑、記在同一份紀錄；套件的轉換沒過就停在那裡、回白話原因，下次再試。
func TestPackageMigrationsRunAfterCore(t *testing.T) {
	home := migHome(t)
	var order []string
	fail := true
	oldCore, oldPkg := HomeMigrations, PackageMigrations
	t.Cleanup(func() { HomeMigrations, PackageMigrations = oldCore, oldPkg })
	HomeMigrations = []HomeMigration{{ID: "core-1", Description: "核心的", Run: func(string) error { order = append(order, "core-1"); return nil }}}
	PackageMigrations = func(h string) []HomeMigration {
		if h != home {
			t.Fatalf("要拿到這個家：%s", h)
		}
		return []HomeMigration{{ID: "pkg.health.v2", Description: "套件「health」：體重改名", Run: func(string) error {
			order = append(order, "pkg.health.v2")
			if fail {
				return platform.Conflict("有 1 筆轉不過去，一筆都沒動")
			}
			return nil
		}}}
	}
	ran, err := RunHomeMigrations(home)
	if err == nil || strings.Join(ran, ",") != "core-1" || !strings.Contains(err.Error(), "體重改名") || !strings.Contains(err.Error(), "一筆都沒動") {
		t.Fatalf("套件的轉換沒過要停下來說明：ran=%v err=%v", ran, err)
	}
	fail = false
	ran, err = RunHomeMigrations(home)
	if err != nil || strings.Join(ran, ",") != "pkg.health.v2" || strings.Join(order, ",") != "core-1,pkg.health.v2,pkg.health.v2" {
		t.Fatalf("修好後只補跑套件的：ran=%v order=%v err=%v", ran, order, err)
	}
	if applied, _ := AppliedMigrations(home); strings.Join(applied, ",") != "core-1,pkg.health.v2" {
		t.Fatalf("applied = %v", applied)
	}
}

// 複本上試跑：會改資料的遷移只改複本，真的家一個字都不動；沒過就回原因。
func TestPreviewMigrationsLeavesHomeAlone(t *testing.T) {
	home := t.TempDir()
	for rel, body := range map[string]string{"home.toml": "", "vault/tasks/T-1.md": "old", "vault/tasks.bak-20261112-x/T-1.md": "備份", "settings/a.toml": "x = 1\n"} {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	saved, savedPkg := HomeMigrations, PackageMigrations
	t.Cleanup(func() { HomeMigrations, PackageMigrations = saved, savedPkg })
	PackageMigrations = nil
	var sawBackup bool
	HomeMigrations = []HomeMigration{{ID: "t.rewrite", Description: "改寫", Run: func(h string) error {
		if h == home {
			t.Error("試跑不能拿真的家")
		}
		if _, err := os.Stat(filepath.Join(h, "vault", "tasks.bak-20261112-x")); err == nil {
			sawBackup = true
		}
		return os.WriteFile(filepath.Join(h, "vault", "tasks", "T-1.md"), []byte("new"), 0o600)
	}}}
	ran, err := PreviewMigrations(home)
	if err != nil || len(ran) != 1 || ran[0] != "t.rewrite" {
		t.Fatalf("試跑：%v %v", ran, err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, "vault", "tasks", "T-1.md")); string(b) != "old" {
		t.Fatalf("真的家被改了：%q", b)
	}
	if _, err := os.Stat(filepath.Join(home, "settings", "migrations.toml")); err == nil {
		t.Fatal("試跑不能在真的家記「跑過了」")
	}
	if sawBackup {
		t.Fatal("備份資料夾不用抄進複本")
	}
	HomeMigrations = append(HomeMigrations, HomeMigration{ID: "t.bad", Description: "會失敗", Run: func(string) error { return platform.Conflict("有 3 筆轉不過去") }})
	ran, err = PreviewMigrations(home)
	if err == nil || len(ran) != 1 {
		t.Fatalf("第二個要失敗：%v %v", ran, err)
	}
}

// 同一組的遷移：還沒跑過的一次交給 RunGroup，全部成功才一起記；沒過就一個都不記，下次整組再來。
func TestGroupedMigrationsRunTogether(t *testing.T) {
	home := migHome(t)
	var got [][]string
	fail := true
	group := func(_ string, ids []string) error {
		got = append(got, ids)
		if fail {
			return platform.Conflict("有 1 筆轉不過去，一筆都沒動")
		}
		return nil
	}
	never := func(string) error { t.Fatal("同組的不能一個一個跑"); return nil }
	list := []HomeMigration{
		{ID: "core-1", Description: "核心的", Run: func(string) error { return nil }},
		{ID: "pkg.a.m1", Description: "改名", Run: never, Group: "pkg", RunGroup: group},
		{ID: "pkg.a.m2", Description: "補必填", Run: never, Group: "pkg", RunGroup: group},
	}
	ran, err := RunMigrations(home, list)
	if err == nil || strings.Join(ran, ",") != "core-1" {
		t.Fatalf("整組沒過：只記核心的：ran=%v err=%v", ran, err)
	}
	if applied, _ := AppliedMigrations(home); strings.Join(applied, ",") != "core-1" {
		t.Fatalf("applied = %v", applied)
	}
	fail = false
	ran, err = RunMigrations(home, list)
	if err != nil || strings.Join(ran, ",") != "pkg.a.m1,pkg.a.m2" || len(got) != 2 || strings.Join(got[1], ",") != "pkg.a.m1,pkg.a.m2" {
		t.Fatalf("整組一次跑完：ran=%v got=%v err=%v", ran, got, err)
	}
	if err := MarkMigrationsApplied(home, []string{"pkg.a.m1", "pkg.mine.x"}); err != nil {
		t.Fatal(err)
	}
	if applied, _ := AppliedMigrations(home); strings.Join(applied, ",") != "core-1,pkg.a.m1,pkg.a.m2,pkg.mine.x" {
		t.Fatalf("MarkMigrationsApplied 不重複記：%v", applied)
	}
}

// 複本的狀態資料夾指到複本自己底下（真的家寫絕對路徑也一樣），編號檔有帶過去。
func TestPreviewIsolatesStateAndKeepsCounters(t *testing.T) {
	home := migHome(t)
	state := t.TempDir()
	q, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte("[layout]\nversion = 2\nstate = "+string(q)+"\n\n[web]\nport = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "vault"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "vault", ".counters.toml"), []byte("task = 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldCore, oldPkg := HomeMigrations, PackageMigrations
	t.Cleanup(func() { HomeMigrations, PackageMigrations = oldCore, oldPkg })
	PackageMigrations = nil
	var gotState, counters string
	HomeMigrations = []HomeMigration{{ID: "look", Description: "看一下", Run: func(h string) error {
		gotState = platform.LayoutFor(h).State
		b, _ := os.ReadFile(filepath.Join(h, "vault", ".counters.toml"))
		counters = string(b)
		return os.WriteFile(filepath.Join(platform.LayoutFor(h).State, "touched"), []byte("x"), 0o600)
	}}}
	if _, err := PreviewMigrations(home); err != nil {
		t.Fatal(err)
	}
	if gotState == state || gotState == "" || counters != "task = 7\n" {
		t.Fatalf("複本的狀態資料夾不能是真的那個、編號檔要帶：state=%q counters=%q", gotState, counters)
	}
	if _, err := os.Stat(filepath.Join(state, "touched")); err == nil {
		t.Fatal("試跑寫到真的狀態資料夾了")
	}
}
