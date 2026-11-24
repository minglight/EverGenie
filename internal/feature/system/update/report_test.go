package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 升級報告交給管電腦的秘書；沒有 system 職位就給名冊上第一位沒停用的。
func TestReportAgent(t *testing.T) {
	home := t.TempDir()
	add := func(id, body string) {
		dir := filepath.Join(home, "vault", "agents", id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agent.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := ReportAgent(home); got != "" {
		t.Fatalf("沒有秘書要回空的，拿到 %q", got)
	}
	add("art", "---\nid: art\nenabled: false\n---\n")
	add("life", "---\nid: life\nenabled: true\n---\n")
	add("code", "---\nid: code\nenabled: true\n---\n")
	add("code.bak-20261112-x", "---\nid: code\n---\n")
	if got := ReportAgent(home); got != "code" {
		t.Fatalf("沒有 system、都沒寫時間：照 id 排第一位沒停用的，拿到 %q", got)
	}
	// 沒有電腦秘書的家：名冊上第一位＝最早請的那一位（不是代號字母排第一位的；隨選的也算，傳話會叫醒她）。
	add("health", "---\nid: health\ncreated_at: \"2026-11-10T09:00:00+08:00\"\nenabled: true\nmode: resident\n---\n")
	add("life", "---\r\nid: life\r\ncreated_at: 2026-11-01T09:00:00+08:00\r\nenabled: true # 註解\r\nbudget:\r\n  enabled: false\r\n  mode: none\r\n---\r\n內文裡寫 enabled: false 不算\r\n")
	add("code", "---\nid: code\ncreated_at: \"2026-11-05T09:00:00+08:00\"\nenabled: true\nmode: ondemand\n---\n")
	if got := ReportAgent(home); got != "life" {
		t.Fatalf("沒有 system：要給最早請的 life（health 字母在前但比較晚；縮排的同名鍵、行尾註解、CRLF 不影響），拿到 %q", got)
	}
	// 叫不醒的（mode: none）、停用的（含行尾註解）不收；時間看不懂的排在看得懂的後面。
	add("art", "---\nid: art\ncreated_at: \"2026-01-01T09:00:00+08:00\"\nenabled: true\nmode: none\n---\n")
	add("aaa", "---\nid: aaa\ncreated_at: 去年\nenabled: true\n---\n")
	add("life", "---\nid: life\ncreated_at: \"2026-11-01T09:00:00+08:00\"\nenabled: false # 先停\n---\n")
	if got := ReportAgent(home); got != "code" {
		t.Fatalf("art 叫不醒、life 停用、aaa 的時間看不懂：要給 code，拿到 %q", got)
	}
	add("system", "---\nid: system\nenabled: false\n---\n")
	if got := ReportAgent(home); got != "code" {
		t.Fatalf("system 停用中不能收，拿到 %q", got)
	}
	add("system", "---\nid: system\nenabled: true\n---\n")
	if got := ReportAgent(home); got != "system" {
		t.Fatalf("有 system 就給 system，拿到 %q", got)
	}
}

// 報告的內容：成功時列出整理了什麼、主人的東西哪些要處理、怎麼確認與退回；沒成功時說原因與不能刪主人的東西。
func TestUpgradeReportText(t *testing.T) {
	ok := UpgradeReport{From: "0.2.0", To: "0.3.0", OK: true, Migrations: []string{"pkg.habits.rename"}, Backup: "/b/x.tgz",
		Packages: []sdk.Package{{ID: "my-thing", Tier: "mine", Enabled: true, Error: "讀不懂 package.toml"}},
		Forks:    []string{"closet"},
		Checks:   []sdk.DoctorCheck{{Detail: "有 2 筆紀錄格式不對"}}}.Text()
	for _, want := range []string{"系統自動產生", "0.2.0 升級到 0.3.0", "pkg.habits.rename", "my-thing（主人自己的）：讀不懂 package.toml", "closet", "有 2 筆紀錄格式不對", "不要問他要不要刪備份", "upgrade --rollback", "一律留著", "/b/x.tgz"} {
		if !strings.Contains(ok, want) {
			t.Errorf("成功的報告少了「%s」：\n%s", want, ok)
		}
	}
	clean := UpgradeReport{From: "0.2.0", To: "0.3.0", OK: true}.Text()
	if !strings.Contains(clean, "沒有發現要整理的東西") || !strings.Contains(clean, "沒有整理資料格式") || !strings.Contains(clean, "留著最新一份") {
		t.Errorf("沒事的報告：\n%s", clean)
	}
	bad := UpgradeReport{From: "0.2.0", To: "0.3.0", Reason: "資料整理在複本上試跑沒過：habits 有 3 筆轉不過去"}.Text()
	for _, want := range []string{"沒成功", "已經換回 0.2.0", "habits 有 3 筆轉不過去", "不要為了讓升級通過而刪", "--apply --to"} {
		if !strings.Contains(bad, want) {
			t.Errorf("沒成功的報告少了「%s」：\n%s", want, bad)
		}
	}
}

func TestOldForks(t *testing.T) {
	home := t.TempDir()
	for _, p := range []string{"packages/mine/closet/.fork-base/package.toml", "packages/mine/own/package.toml", "packages/mine/closet.bak-20261112-x/.fork-base/x"} {
		full := filepath.Join(home, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := oldForks(home); len(got) != 1 || got[0] != "closet" {
		t.Fatalf("只有 closet 是舊複製品：%v", got)
	}
}

// 職責內文的客製被搬到附加規則的職位要列進報告，請秘書看過（DEV-154）。
func TestUpgradeReportListsMovedDuties(t *testing.T) {
	text := UpgradeReport{From: "1", To: "2", OK: true, Duties: []string{"life", "system"}}.Text()
	if !strings.Contains(text, "life、system") || !strings.Contains(text, "附加規則") || strings.Contains(text, "沒有發現要整理的東西") {
		t.Fatalf("報告要列出職位：%s", text)
	}
}
