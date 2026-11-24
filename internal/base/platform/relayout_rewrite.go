package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// 搬家時改寫家裡文字檔裡的絕對路徑（DEV-145）：紀錄裡寫了 <家>/runtime/art/…、<家>/runtime/share/… 的，
// 搬完改成 <家>/media/art/…、<家>/media/share/…；官方角色有搬時，<家>/personas/avatars/… 等改成狀態資料夾的位置；
// 也認 ~/ 開頭的寫法。<家>/runtime/<其他> 與相對寫法不改，只在預演提醒。原文先存進 settings/relayout-rewrites/，
// 退回時還原。只動那段字串，其他內容與檔案權限不變。

// rewriteRoots 是要掃的資料夾（家裡的相對路徑）。
var rewriteRoots = []string{"vault", "personas", filepath.Join("packages", "mine"), "settings"}

// rewriteExts 是要掃的文字檔副檔名。
var rewriteExts = map[string]bool{".md": true, ".toml": true, ".json": true, ".jsonl": true, ".yaml": true, ".txt": true}

func rewritesDir(home string) string { return filepath.Join(home, "settings", "relayout-rewrites") }

// rewriteRule：<from>/<段名> → <to>/<段名>，段名要讓 match 說好（段名是一整段路徑名字，後面不能再接名字字元）。
type rewriteRule struct {
	from, to string
	match    func(seg string) bool
}

func segIn(names ...string) func(string) bool {
	return func(seg string) bool {
		for _, n := range names {
			if seg == n {
				return true
			}
		}
		return false
	}
}

// tildeForm 是 p 的 ~/ 寫法（p 在使用者家目錄底下才有）。
func tildeForm(p string) (string, bool) {
	u, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(u, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return "~/" + filepath.ToSlash(rel), true
}

// rewriteRules 是搬家的路徑改寫規則（絕對路徑與 ~/ 寫法各一組）：
// <家>/runtime/{art,share} → <家>/media/…；官方角色有搬時，<家>/personas/{avatars,wardrobe,catalog,<代號>.md} → <狀態>/personas/…。
func rewriteRules(home, state string, personasAtHome bool) []rewriteRule {
	type form struct{ home, state string }
	forms := []form{{home, state}}
	if th, ok := tildeForm(home); ok {
		ts := state
		if t, ok := tildeForm(state); ok {
			ts = t
		}
		forms = append(forms, form{th, ts})
	}
	media := segIn("art", "share")
	official := func(seg string) bool {
		return segIn("avatars", "wardrobe", "catalog")(seg) || officialCard.MatchString(seg)
	}
	var rules []rewriteRule
	for _, f := range forms {
		rules = append(rules, rewriteRule{f.home + "/runtime", f.home + "/media", media})
		if !personasAtHome && f.state != "" {
			rules = append(rules, rewriteRule{f.home + "/personas", f.state + "/personas", official})
		}
	}
	return rules
}

// RewriteOfficialPaths 把 s 裡官方角色在家裡的絕對路徑（<家>/personas/{avatars,wardrobe,catalog,<代號>.md}，含 ~/ 寫法）
// 換成狀態資料夾的位置（<狀態>/personas/…），回新字串與次數；evergenie home tidy --move-official 把官方角色搬出家時用，
// 規則跟搬家（home relayout --move-personas）同一份。
func RewriteOfficialPaths(home, state, s string) (string, int) {
	var rules []rewriteRule
	for _, r := range rewriteRules(home, state, false) {
		if strings.HasSuffix(r.from, "/personas") {
			rules = append(rules, r)
		}
	}
	return rewriteText(s, rules, false)
}

// pathChar：路徑名字裡會出現的字元；前綴後面接著這種字元（例如 runtime/artwork）就不算同一個路徑。
func pathChar(c byte) bool {
	return c == '-' || c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// segmentAt 回傳 s[i:] 開頭那一段路徑名字。
func segmentAt(s string, i int) string {
	j := i
	for j < len(s) && pathChar(s[j]) {
		j++
	}
	return s[i:j]
}

// applyRule 把 s 裡 from/<段名> 換成 to/<段名>，回新字串與次數。
func applyRule(s, from, to string, match func(string) bool) (string, int) {
	var b strings.Builder
	n, i := 0, 0
	prefix := from + "/"
	for {
		j := strings.Index(s[i:], prefix)
		if j < 0 {
			break
		}
		at := i + j
		seg := segmentAt(s, at+len(prefix))
		if seg == "" || !match(seg) {
			b.WriteString(s[i : at+len(prefix)])
			i = at + len(prefix)
			continue
		}
		b.WriteString(s[i:at])
		b.WriteString(to + "/")
		i = at + len(prefix)
		n++
	}
	if n == 0 {
		return s, 0
	}
	b.WriteString(s[i:])
	return b.String(), n
}

// rewriteText 套用全部規則；reverse＝反方向（搬回第 1 版）。
func rewriteText(s string, rules []rewriteRule, reverse bool) (string, int) {
	total := 0
	for _, r := range rules {
		from, to := r.from, r.to
		if reverse {
			from, to = to, from
		}
		var n int
		s, n = applyRule(s, from, to, r.match)
		total += n
	}
	return s, total
}

// runtimeRefs 數還寫著家裡 runtime 的地方（改寫後剩下的）：<家>/runtime/<段名> 的絕對路徑，以及相對寫法 runtime/share、runtime/art。
func runtimeRefs(s, home string) int {
	n := 0
	bases := []string{home}
	if th, ok := tildeForm(home); ok {
		bases = append(bases, th)
	}
	for _, b := range bases {
		_, c := applyRule(s, b+"/runtime", b+"/runtime", func(seg string) bool { return true })
		n += c
	}
	for _, rel := range []string{"runtime/share", "runtime/art"} {
		for i := 0; ; {
			j := strings.Index(s[i:], rel)
			if j < 0 {
				break
			}
			at := i + j
			end := at + len(rel)
			i = end
			if at > 0 && (s[at-1] == '/' || pathChar(s[at-1])) {
				continue // 絕對路徑的一部分（上面算過）或別的名字
			}
			if end < len(s) && pathChar(s[end]) {
				continue
			}
			n++
		}
	}
	return n
}

// planRewrites 找出要改寫的檔案（不在搬家項目裡的；不跟著 symlink 走），以及改寫後還寫著 runtime 路徑、不會改的檔。
func planRewrites(home, state string, personasAtHome bool, moves []RelayoutItem) (rewrites, refs []RelayoutRewrite) {
	rules := rewriteRules(home, state, personasAtHome)
	skip := map[string]bool{RelayoutRecordPath(home): true, rewritesDir(home): true}
	for _, m := range moves {
		skip[m.From] = true
	}
	for _, root := range rewriteRoots {
		_ = filepath.WalkDir(filepath.Join(home, root), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if skip[p] {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || !rewriteExts[strings.ToLower(filepath.Ext(p))] {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(home, p)
			text, n := rewriteText(string(data), rules, false)
			if n > 0 {
				rewrites = append(rewrites, RelayoutRewrite{Path: rel, Count: n})
			}
			if c := runtimeRefs(text, home); c > 0 {
				refs = append(refs, RelayoutRewrite{Path: rel, Count: c})
			}
			return nil
		})
	}
	return rewrites, refs
}

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// applyRewrites 搬完後改寫：每個檔先存原文、寫進紀錄，再換字串（權限照原檔）。
func applyRewrites(rec *relayoutRecord, list []RelayoutRewrite, recPath string) error {
	pairs := rewriteRules(rec.Home, rec.State, rec.PersonasAtHome)
	for _, r := range list {
		path := filepath.Join(rec.Home, r.Path)
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() {
			continue // 預演到現在之間被刪了就跳過
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return Unavailable("讀不了 " + path + "：" + err.Error())
		}
		newText, n := rewriteText(string(data), pairs, false)
		if n == 0 {
			continue
		}
		backup := filepath.Join(rewritesDir(rec.Home), r.Path)
		if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
			return Unavailable("建不了 " + filepath.Dir(backup) + "：" + err.Error())
		}
		if err := AtomicWrite(backup, data, st.Mode().Perm()); err != nil {
			return Unavailable("存不了原文 " + backup + "：" + err.Error())
		}
		rec.Rewrites = append(rec.Rewrites, rewriteStep{Path: r.Path, Backup: backup, Count: n, Sum: sum([]byte(newText)), Status: "pending"})
		if err := saveRecord(recPath, rec); err != nil {
			return err
		}
		if err := AtomicWrite(path, []byte(newText), st.Mode().Perm()); err != nil {
			return Unavailable("改寫不了 " + path + "：" + err.Error())
		}
		rec.Rewrites[len(rec.Rewrites)-1].Status = "done"
		if err := saveRecord(recPath, rec); err != nil {
			return err
		}
	}
	return nil
}

// revertRewrites 還原改寫過的檔：內容跟改寫後一樣就整份還原原文；之後又被改過就只反向替換字串。
func revertRewrites(rec *relayoutRecord, recPath string) error {
	pairs := rewriteRules(rec.Home, rec.State, rec.PersonasAtHome)
	var errs []string
	for i := len(rec.Rewrites) - 1; i >= 0; i-- {
		r := &rec.Rewrites[i]
		if r.Status == "reverted" {
			continue
		}
		path := filepath.Join(rec.Home, r.Path)
		cur, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			r.Status = "reverted" // 主人搬家後刪了這個檔：不建回來
			continue
		}
		if err != nil {
			errs = append(errs, "讀不了 "+path+"："+err.Error())
			continue
		}
		st, err := os.Stat(path)
		if err != nil {
			errs = append(errs, "讀不了 "+path+"："+err.Error())
			continue
		}
		var restored []byte
		switch {
		case sum(cur) != r.Sum && r.Status == "pending":
			// 原文已存、檔案還沒改寫（寫入是整份換掉，不會只寫一半）：不用動。
			r.Status = "reverted"
			continue
		case sum(cur) != r.Sum:
			// 搬家後又被改過：不整份蓋回，只把 media 前綴換回 runtime。
			s, _ := rewriteText(string(cur), pairs, true)
			restored = []byte(s)
		default:
			orig, err := os.ReadFile(r.Backup)
			if err != nil {
				errs = append(errs, "讀不到原文 "+r.Backup+"："+err.Error())
				continue
			}
			restored = orig
		}
		if err := AtomicWrite(path, restored, st.Mode().Perm()); err != nil {
			errs = append(errs, "還原不了 "+path+"："+err.Error())
			continue
		}
		r.Status = "reverted"
		_ = saveRecord(recPath, rec)
	}
	_ = saveRecord(recPath, rec)
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "；"))
	}
	return nil
}
