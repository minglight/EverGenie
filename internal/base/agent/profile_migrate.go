package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
)

// OwnerTopicMigration 是「profile.md 搬進主題『主人』」這個家的遷移的 id（DEV-161）。
const OwnerTopicMigration = "2026-11-15-profile-to-owner-topic"

// profileBackupReason 是備份檔名的原因（備份規則，system 卡 BSW-4：<原名>.bak-<YYYYMMDD>-<原因>）。
const profileBackupReason = "dev161"

// MigrateProfileToOwnerTopic 是家的遷移（update 的 HomeMigration，由 cmd/evergenie 組裝；DEV-161，作者 2026-11-15）：
// 共用的使用者 profile（vault/profile.md，以前只有生活秘書寫）退休，改成固定主題 vault/notes/人/主人.md，所有職位開場都讀。
//
//  1. 先備份：vault/profile.md 原樣複製成 vault/profile.md.bak-<今天>-dev161（已經有同名備份就不蓋）。
//  2. 內文搬進「主人」：每一行一件事的行尾補「（life MM-DD）」（以前只有 life 能寫；日期取 profile 的 updated_at，
//     讀不到用檔案修改時間）；空行、標題原樣。「主人」還沒有就建一份；已經有了（重跑、或秘書先寫過）只接上還沒有的行。
//  3. 舊檔留在原處不刪（封存）：核心不再讀它，開場也不帶。
//
// 只改 vault/ 裡的檔（退回會還原的範圍）；不經 vault 模組，原子寫入。能重跑：同一行不會接兩次。沒有 profile.md 的家什麼都不做。
func MigrateProfileToOwnerTopic(home string) error {
	src := filepath.Join(home, "vault", "profile.md")
	data, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return platform.Unavailable("讀不了 vault/profile.md")
	}
	info, err := os.Stat(src)
	if err != nil {
		return platform.Unavailable("讀不了 vault/profile.md")
	}
	now := time.Now()
	backup := src + ".bak-" + now.Format("20060102") + "-" + profileBackupReason
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		if err := platform.AtomicWrite(backup, data, info.Mode().Perm()); err != nil {
			return platform.Unavailable("備份不了 vault/profile.md")
		}
	}
	r, _, err := vault.Decode(data)
	if err != nil {
		// 讀不懂不擋升級：備份已經做了、舊檔不動，之後照備份手動搬。
		return nil
	}
	day := info.ModTime()
	if s, _ := r.Fields["updated_at"].(string); s != "" {
		if at, err := time.Parse(time.RFC3339Nano, s); err == nil {
			day = at
		}
	}
	lines := profileLines(r.Body, day.Format("01-02"))
	if len(lines) == 0 {
		return nil
	}
	dst := filepath.Join(home, "vault", "notes", filepath.FromSlash(OwnerTopicID)+".md")
	var topic vault.Record
	var order []string
	if old, err := os.ReadFile(dst); err == nil {
		if topic, order, err = vault.Decode(old); err != nil {
			return nil // 主人改壞的「主人」不蓋、不擋升級；doctor 會報檔頭不合，profile 留著之後再搬
		}
	} else if os.IsNotExist(err) {
		topic = vault.Record{Fields: map[string]any{"schema": 1, "name": OwnerTopicName, "aliases": []string{}, "summary": ownerTopicSummary, "follow": []string{}, "links": []string{}}}
	} else {
		return platform.Unavailable("讀不了 vault/notes/人/主人.md")
	}
	have := map[string]bool{}
	for _, l := range strings.Split(topic.Body, "\n") {
		have[unmark(l)] = true
	}
	fresh := strings.TrimSpace(topic.Body) == ""
	body := strings.TrimRight(topic.Body, "\n")
	added := false
	for _, l := range lines {
		switch {
		case contentLine(l) && have[unmark(l)]:
			continue // 搬過了（重跑）或「主人」裡已經有
		case !contentLine(l) && !fresh:
			continue // 接到已經有的「主人」後面：只接還沒有的事，空行與標題不接
		}
		body += "\n" + l
		added = true
	}
	if !added {
		return nil
	}
	topic.Body = strings.TrimLeft(body, "\n") + "\n"
	topic.Fields["updated_at"] = now.Format(time.RFC3339)
	topic.Fields["by"] = "migrate"
	var spec vault.Spec
	for _, sc := range noteSpecs() {
		if sc.Kind == TopicKind {
			spec = sc
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return platform.Unavailable("建不了 vault/notes/人/")
	}
	if err := platform.AtomicWrite(dst, vault.Encode(topic, order, spec), 0600); err != nil {
		return platform.Unavailable("寫不了 vault/notes/人/主人.md")
	}
	return nil
}

// profileLines 把 profile 內文轉成主題的行：一件事的行補「（life MM-DD）」，空行與標題原樣；前後的空行拿掉。
func profileLines(body, monthDay string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n")), "\n") {
		if contentLine(l) {
			l = MarkLine(l, "life", monthDay)
		}
		out = append(out, strings.TrimRight(l, " \t"))
	}
	if len(out) == 1 && out[0] == "" {
		return nil
	}
	return out
}

// unmark 去掉行尾標記與前後空白（比對「這一行搬過了沒」用）。
func unmark(l string) string { return strings.TrimSpace(markPattern.ReplaceAllString(l, "")) }
