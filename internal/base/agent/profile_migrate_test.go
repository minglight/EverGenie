package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/vault"
)

// profile.md 搬進「主人」：先備份、每行補（life MM-DD）（日期取 profile 的 updated_at）、舊檔留著不刪；重跑不會多接。
func TestMigrateProfileToOwnerTopic(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(home, "vault", "profile.md")
	must(t, os.MkdirAll(filepath.Dir(src), 0700))
	old := "---\nschema: 1\nupdated_at: \"2026-09-03T10:00:00+08:00\"\nby: life\n---\n# 基本\n\n住台北\n有兩個小孩（life 08-01）\n"
	must(t, os.WriteFile(src, []byte(old), 0600))
	must(t, MigrateProfileToOwnerTopic(home))
	backup := src + ".bak-" + time.Now().Format("20060102") + "-dev161"
	if data, err := os.ReadFile(backup); err != nil || string(data) != old {
		t.Fatalf("要先原樣備份 profile.md：%v %q", err, data)
	}
	if data, err := os.ReadFile(src); err != nil || string(data) != old {
		t.Fatal("舊的 profile.md 留著不刪不改")
	}
	dst := filepath.Join(home, "vault", "notes", "人", "主人.md")
	data, err := os.ReadFile(dst)
	must(t, err)
	r, _, err := vault.Decode(data)
	must(t, err)
	if want := "# 基本\n\n住台北（life 09-03）\n有兩個小孩（life 08-01）"; strings.TrimSpace(r.Body) != want {
		t.Fatalf("主人的內文：%q", r.Body)
	}
	for _, sc := range noteSpecs() {
		if sc.Kind == TopicKind {
			must(t, sc.Validate(r))
		}
	}
	if r.Fields["name"] != "主人" || r.Fields["by"] != "migrate" {
		t.Fatalf("檔頭：%v", r.Fields)
	}
	must(t, MigrateProfileToOwnerTopic(home))
	again, err := os.ReadFile(dst)
	must(t, err)
	if string(again) != string(data) {
		t.Fatalf("重跑不能多接：\n%s", again)
	}
	// 「主人」已經有東西（秘書先寫過）：只接上還沒有的事。
	must(t, os.WriteFile(src, []byte(strings.Replace(old, "住台北\n", "住台北\n喜歡爬山\n", 1)), 0600))
	must(t, MigrateProfileToOwnerTopic(home))
	data, err = os.ReadFile(dst)
	must(t, err)
	r, _, err = vault.Decode(data)
	must(t, err)
	if !strings.HasSuffix(strings.TrimSpace(r.Body), "有兩個小孩（life 08-01）\n喜歡爬山（life 09-03）") || strings.Count(r.Body, "# 基本") != 1 {
		t.Fatalf("接上還沒有的行：%q", r.Body)
	}
}

// 沒有 profile.md 的家什麼都不做。
func TestMigrateProfileWithoutProfile(t *testing.T) {
	home := t.TempDir()
	must(t, MigrateProfileToOwnerTopic(home))
	if _, err := os.Stat(filepath.Join(home, "vault", "notes")); !os.IsNotExist(err) {
		t.Fatal("沒有 profile 不該建任何東西")
	}
}
