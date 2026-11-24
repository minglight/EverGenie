package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/feature/packages/pkg"
)

// 官方樣板後來加了 art（DEV-157；2026-11-15 起在開發工坊 admin，DEV-160）：還沒跑過 2026-11-12-official-duties 的家，
// 主人自己建的 art 職位一個位元都不動（不記 duties_from、不搬到附加規則）；那次遷移認得的職位（life）照舊轉。
// 用 repo 裡真的官方套件（遷移讀全部官方套件的樣板、不看有沒有啟用，所以 art 在不在一般家開著的套件裡都一樣）。
// Guards: AGT-38, ADM-39
func TestOfficialDutiesMigrationLeavesOwnArt(t *testing.T) {
	official, err := filepath.Abs(filepath.Join("..", "..", "packages", "official"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", official)
	home := t.TempDir()
	if _, ok := pkg.OfficialDuties(home)["art"]; !ok {
		t.Fatal("官方套件要帶 art 職位樣板（這個測試才有意義）")
	}
	write := func(id, text string) string {
		dir := filepath.Join(home, "vault", "agents", id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "agent.md")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	head := func(id string) string {
		return "---\nschema: 1\nupdated_at: \"2026-11-01T00:00:00+08:00\"\nby: system\nid: " + id +
			"\ncreated_at: \"2026-10-01T00:00:00+08:00\"\nduty: 測試\npersona: athena\nenabled: true\n---\n"
	}
	artPath := write("art", head("art")+"# art 的職責\n\n## 你主動照顧的事\n- 主人客製：用他自己的生圖流程\n")
	artBefore, _ := os.ReadFile(artPath)
	lifePath := write("life", head("life")+"# life 的職責\n")

	if err := migrateOfficialDuties(home); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(artPath); string(got) != string(artBefore) {
		t.Fatalf("主人自己建的 art 被改了：\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(home, "vault", "agents", "art", "addendum.md")); !os.IsNotExist(err) {
		t.Fatal("主人自己建的 art 不該多附加規則")
	}
	if got, _ := os.ReadFile(lifePath); !strings.Contains(string(got), "duties_from: secretaries/life\n") {
		t.Fatalf("那次遷移認得的 life 要照舊轉：\n%s", got)
	}
}

// 那次遷移認得的職位都是旁邊有 history.txt 的樣板；之後才加的（art）沒有，也不在清單裡（新增樣板不要加進清單）。
// Guards: AGT-38
func TestOfficialDutiesMigratedMatchesHistory(t *testing.T) {
	official, err := filepath.Abs(filepath.Join("..", "..", "packages", "official"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", official)
	for id, d := range pkg.OfficialDuties(t.TempDir()) {
		_, err := os.Stat(filepath.Join(official, d.Package, "agents", id, "history.txt"))
		if hasHistory := err == nil; hasHistory != officialDutiesMigrated[id] {
			t.Errorf("職位 %s：有 history.txt＝%v，在遷移清單＝%v，兩邊要一致", id, hasHistory, officialDutiesMigrated[id])
		}
	}
	if officialDutiesMigrated["art"] {
		t.Error("art 是 2026-11-14 才加的樣板，不能在遷移清單裡")
	}
}
