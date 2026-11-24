package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
)

// OfficialDutyTemplate 是家的遷移要知道的官方職位樣板（由組裝點從套件模組轉過來；名冊不 import 套件）。
type OfficialDutyTemplate struct {
	Ref   string          // 要記進 duties_from 的值：<套件>/<樣板的職位>
	Known map[string]bool // 樣板現在與以前各版出現過的行（去掉行尾空白，不含空白行）
}

// OfficialDutiesMigration 是遷移的 id；搬到附加規則的那一段前面的註記也帶著它（重跑時靠它知道搬過了）。
const OfficialDutiesMigration = "2026-11-12-official-duties"

// MovedDutiesMark 是「從職責內文搬到附加規則」那一段開頭的註記行。升級報告靠它找出哪些職位被搬過。
const MovedDutiesMark = "<!-- " + OfficialDutiesMigration + "：以下原本寫在職責內文裡，升級時原樣搬來（官方職責改成跟著程式走，家裡只留附加規則） -->"

// MigrateOfficialDuties 是家的遷移（update 的 HomeMigration，由 cmd/evergenie 組裝；DEV-154，作者 2026-11-12）：
// 官方套件帶來的職位，職責全文以前抄在家裡 vault/agents/<職位>/agent.md 的內文，官方改版要靠「同步」整份覆蓋，
// 主人寫在內文裡的客製會被洗掉。改成職責跟著程式走：agent 紀錄記 duties_from＝<套件>/<樣板的職位>，內文清空，開場時讀樣板。
//
// 對每個 id 對得上官方樣板、還沒有 duties_from 的職位：內文裡不是樣板任何一版出現過的行，就是主人自己加的，
// 照原順序、一個字不改地接到 addendum.md（附加規則）最後面，前面加一行 MovedDutiesMark；然後記 duties_from、清空內文。
// 對不上樣板的職位（主人自己建的）不動。只改 vault/agents/ 裡的檔（退回會還原的範圍）；不經 vault 模組，原子寫入。
// 能重跑：有 duties_from 的跳過；附加規則裡已經有註記就不再接一次。
func MigrateOfficialDuties(home string, official map[string]OfficialDutyTemplate) error {
	root := filepath.Join(home, "vault", "agents")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return platform.Unavailable("讀不了名冊資料夾 vault/agents")
	}
	for _, e := range entries {
		id := e.Name()
		t, ok := official[id]
		if !ok || !e.IsDir() || t.Ref == "" {
			continue
		}
		dir := filepath.Join(root, id)
		path := filepath.Join(dir, "agent.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue // 沒有 agent.md 的資料夾不是職位
		}
		head, body, ok := splitFrontMatter(data)
		if !ok {
			continue // 舊格式或壞檔：不是這次的事，照舊留給健康檢查報
		}
		r, _, err := vault.Decode(data)
		if err != nil || DutiesFrom(r.Fields) != "" {
			continue
		}
		if own := ownDutyLines(body, t.Known); own != "" {
			if err := appendMovedDuties(filepath.Join(dir, "addendum.md"), id, own); err != nil {
				return err
			}
		}
		info, err := os.Stat(path)
		if err != nil {
			return platform.Unavailable("讀不了職位「" + id + "」的紀錄")
		}
		out := head + "duties_from: " + t.Ref + "\n---\n"
		if err := platform.AtomicWrite(path, []byte(out), info.Mode().Perm()); err != nil {
			return platform.Unavailable("寫不了職位「" + id + "」的紀錄")
		}
	}
	return nil
}

// splitFrontMatter 把紀錄檔拆成標頭（第一行 --- 到結尾的 --- 之前，含換行）與內文；不是這個格式回 false。
func splitFrontMatter(data []byte) (head, body string, ok bool) {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return "", "", false
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---\n")
	switch {
	case end >= 0:
		return s[:4+end+1], rest[end+5:], true
	case strings.HasSuffix(rest, "\n---"):
		return s[:len(s)-3], "", true
	}
	return "", "", false
}

// ownDutyLines 回職責內文裡主人自己加的行（不在 known 裡的非空白行），照原順序、原樣；原本中間隔著別的行的，中間留一個空行。
// 開頭只有一行的大標題（# …）是以前建職位時自動加的，不算主人的。
func ownDutyLines(body string, known map[string]bool) string {
	var out []string
	gap := false
	for i, line := range strings.Split(body, "\n") {
		l := strings.TrimRight(line, " \t\r")
		if l == "" || known[l] || (strings.HasPrefix(l, "# ") && firstNonBlank(body, i)) {
			gap = true
			continue
		}
		if gap && len(out) > 0 {
			out = append(out, "")
		}
		out, gap = append(out, line), false
	}
	return strings.Join(out, "\n")
}

func firstNonBlank(body string, index int) bool {
	for i, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" {
			return i == index
		}
	}
	return false
}

// appendMovedDuties 把主人自己加的職責行接到附加規則最後面（沒有 addendum.md 就建一份）。已經有註記就不動。
func appendMovedDuties(path, id, own string) error {
	fail := platform.Unavailable("寫不了職位「" + id + "」的附加規則")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		now := time.Now().Format(time.RFC3339)
		data = []byte("---\nschema: 1\nupdated_at: \"" + now + "\"\nby: system\nagent: " + id + "\ncreated_at: \"" + now + "\"\n---\n")
	} else if err != nil {
		return fail
	}
	if bytes.Contains(data, []byte(MovedDutiesMark)) {
		return nil
	}
	if _, _, ok := splitFrontMatter(data); !ok {
		return platform.Invalid("addendum", "職位「"+id+"」的附加規則（addendum.md）格式讀不懂，沒辦法把職責裡的客製接上去；先把那個檔修好")
	}
	s := strings.TrimRight(string(data), "\n") + "\n"
	if _, body, _ := splitFrontMatter(data); strings.TrimSpace(body) != "" {
		s += "\n"
	}
	s += MovedDutiesMark + "\n" + own + "\n"
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := platform.AtomicWrite(path, []byte(s), mode); err != nil {
		return fail
	}
	return nil
}

// MovedDuties 列出附加規則裡帶著 MovedDutiesMark 的職位（升級報告用：請電腦秘書看過搬過去的那一段）。
func MovedDuties(home string) []string {
	root := filepath.Join(home, "vault", "agents")
	entries, _ := os.ReadDir(root)
	var out []string
	for _, e := range entries {
		if data, err := os.ReadFile(filepath.Join(root, e.Name(), "addendum.md")); err == nil && bytes.Contains(data, []byte(MovedDutiesMark)) {
			out = append(out, e.Name())
		}
	}
	return out
}
