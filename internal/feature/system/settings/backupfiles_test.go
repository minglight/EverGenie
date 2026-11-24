package settings

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func putBackup(t *testing.T, home, rel string, age time.Duration, now time.Time) {
	t.Helper()
	p := filepath.Join(home, "runtime", "backups", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("備份"), 0o600); err != nil {
		t.Fatal(err)
	}
	at := now.Add(-age)
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

// TestBackupPruneDryRun：依保留規則（每組最新 keep 個一定留、其餘只留 days 天內）標出要刪的；預演不刪任何東西，真的刪只動 runtime/backups/ 底下。
// Guards: REQ-166
func TestBackupPruneDryRun(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	for i, age := range []time.Duration{1 * day, 2 * day, 40 * day, 50 * day, 60 * day} {
		putBackup(t, home, filepath.Join("trust", string(rune('a'+i))+".toml"), age, now)
	}
	putBackup(t, home, "home/x.toml", 90*day, now) // 這一組只有一個：最新的一個一定留
	putBackup(t, home, "legacy-20260101.tar.gz", 100*day, now)
	putBackup(t, home, "new-20260101.tar.gz", 100*day, now)

	list, err := ListBackups(home)
	if err != nil || len(list) != 8 {
		t.Fatalf("列出：%d %v", len(list), err)
	}
	if list[0].ModTime.Before(list[len(list)-1].ModTime) {
		t.Fatal("新的在前")
	}
	groups := map[string]int{}
	for _, b := range list {
		groups[b.Group]++
	}
	if groups["trust"] != 5 || groups["home"] != 1 || groups["legacy"] != 1 || groups["new"] != 1 {
		t.Fatalf("分組：%v", groups)
	}

	plan := PlanPrune(list, 2, 30, now)
	if len(plan) != 3 || !plan["runtime/backups/trust/c.toml"] || !plan["runtime/backups/trust/d.toml"] || !plan["runtime/backups/trust/e.toml"] {
		t.Fatalf("trust 組留最新 2 個與 30 天內的，其餘 3 個要刪：%v", plan)
	}
	if after, _ := ListBackups(home); len(after) != 8 {
		t.Fatal("PlanPrune 不能刪任何東西")
	}
	// 預設值（0）＝每組留 10 個、30 天：trust 組才 5 個，全部留。
	if len(PlanPrune(list, 0, 0, now)) != 0 {
		t.Fatal("預設規則下每組最新 10 個一定留")
	}

	var paths []string
	for p := range plan {
		paths = append(paths, p)
	}
	if err := DeleteBackups(home, paths); err != nil {
		t.Fatal(err)
	}
	if after, _ := ListBackups(home); len(after) != 5 {
		t.Fatalf("刪完剩 5 個：%d", len(after))
	}
	if err := DeleteBackups(home, []string{"home.toml"}); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("不能刪 runtime/backups/ 以外的東西：%v", err)
	}
	if err := DeleteBackups(home, []string{"runtime/backups/../../home.toml"}); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("不能用 .. 跳出去：%v", err)
	}
	empty := t.TempDir()
	if l, err := ListBackups(empty); err != nil || len(l) != 0 {
		t.Fatalf("沒有備份資料夾：%v %v", l, err)
	}
}
