package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCreateFeatureSkill：官方技能 create-feature 的檔頭欄位齊全，寫到的 EverGenie 指令與旗標都真的存在於 CLI 用法，
// 並寫明流程與禁止事項（SEC-302 S8，U-7）。
// Guards: REQ-329, REQ-330, REQ-332, REQ-347
func TestCreateFeatureSkill(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packages", "official", "engine-claude", "skills", "create-feature", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(data)
	head := regexp.MustCompile(`(?s)\A---\nname: create-feature\ndescription: (.+?)\n---\n`).FindStringSubmatch(skill)
	if head == nil || len(head[1]) < 20 {
		t.Fatalf("檔頭要有 name: create-feature 與 description")
	}
	_, top, _ := hg(t, "/nonexistent.sock", "", "help")
	_, pkgHelp, _ := hg(t, "/nonexistent.sock", "", "packages", "--help")
	cmd := regexp.MustCompile("evergenie (packages|agents|data|personas|docs) ([a-z]+)([^`\\n]*)")
	matches := cmd.FindAllStringSubmatch(skill, -1)
	if len(matches) < 5 {
		t.Fatalf("技能裡的指令太少：%d", len(matches))
	}
	for _, m := range matches {
		line := ""
		for _, l := range strings.Split(top, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), m[1]+" ") {
				line = l
			}
		}
		if line == "" || !strings.Contains(line, m[2]) {
			t.Errorf("evergenie %s %s 不在總說明", m[1], m[2])
		}
		if m[1] == "packages" {
			if !strings.Contains(pkgHelp, "  "+m[2]) {
				t.Errorf("packages --help 沒有 %s", m[2])
			}
			for _, flag := range regexp.MustCompile(`--[a-z_-]+`).FindAllString(m[3], -1) {
				if !strings.Contains(pkgHelp, flag) {
					t.Errorf("packages %s 用到的 %s 不在 packages --help", m[2], flag)
				}
			}
		}
	}
	for _, must := range []string{"packages new", "packages sync", "--apply", "[[contributes.data]]", "[[contributes.persona]]", "personas new --to-package",
		"packages/official/", "不能整包複製", "evergenie docs show conventions/user-features.md", "evergenie docs show conventions/terms.md",
		"evergenie docs show schemas/package.md", "設定→選單", "選角色",
		"packages claim-notify", "好，用 X", "換一個", "之後再說", "不再問", "不要重複追問", "不能自己 `--apply`",
		"packages go-status", "packages install-go --confirmed", "要我裝嗎", "提問工具", "沒問過主人不下載 Go"} {
		if !strings.Contains(skill, must) {
			t.Errorf("技能要寫明 %q", must)
		}
	}
}
