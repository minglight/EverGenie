package recode

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
)

// Kind 是一種會跟著角色代號走的套件資料（組裝點從 package.toml 的宣告轉來，PKG-64）。
type Kind struct {
	Kind     string   // <套件>.<種類>；紀錄在 vault/pkg/<套件>/<種類>/<編號>.md
	Tier     string   // 宣告它的套件在哪一層（official、installed、mine；訊息用）
	Field    string   // 值是角色代號的欄位
	ID       string   // exact（編號就是角色代號）｜prefix（編號是 <角色代號>-<其他>）｜空
	Prefixed []string // 值是 <角色代號>-<其他> 的欄位
	Source   string   // 值是 official／mine（這筆的圖放哪）的欄位
	File     string   // 值是檔案位置的欄位（file_field，DEV-160；相對於擁有者位置：官方位置或 personas/mine/）
}

// PackageIssue 是一個讀不起來的套件（組裝點給的）：它的紀錄沒辦法照宣告改。
type PackageIssue struct {
	Package string
	Message string
}

// Rename 是一位角色要改的代號，與卡和圖怎麼走。路徑都是絕對路徑。
type Rename struct {
	Old, New         string
	CardFrom, CardTo string
	// Media 是這位角色的圖現在放的位置（官方位置或 <家>/personas/mine/）。圖一律不改名（DEV-160：檔名沒有意思，照片紀錄的 file
	// 照舊指著原檔，紀錄的角色、衣服、編號照套件宣告跟著改）。MoveTo 不是空的（收成自己的）：這位角色的紀錄（宣告 file_field 的種類）
	// 指著、放在 Media 的檔搬到 MoveTo 的同一個相對路徑。MoveTo 是空的：Media 底下檔名以舊代號開頭、沒有紀錄指著的檔列出來，不動。
	Media, MoveTo string
	// ListUnder：這些資料夾（含子資料夾，跳過 SkipUnder）裡還有舊代號開頭的檔或資料夾就列出來、不動。
	ListUnder []string
	SkipUnder []string
	// Source 不是空的：宣告了 persona_source 的種類，這位角色的紀錄那個欄位改成這個值（從官方位置收成自己的＝mine）。
	Source string
}

// Note 是列出來、沒有改的地方（給人看）。
type Note struct {
	Path string `json:"path"` // 家底下的相對路徑（家外的是絕對路徑）
	Line int    `json:"line,omitempty"`
	Text string `json:"text,omitempty"`
	Why  string `json:"why"`
}

// Summary 是一位角色改了什麼（數量），預演的白話用。
type Summary struct {
	Old      string   `json:"old"`
	New      string   `json:"new"`
	Card     string   `json:"card"`
	Media    int      `json:"media"`
	Roster   []string `json:"roster,omitempty"`
	Records  int      `json:"records"`
	Settings int      `json:"settings"`
}

// Plan 是預演的結果：照順序的步驟、列出不動的地方、不能做的原因。Problems 不是空的就不能套用。
type Plan struct {
	Home     string    `json:"home"`
	Codes    []Code    `json:"codes,omitempty"`
	Summary  []Summary `json:"summary,omitempty"`
	Steps    []Step    `json:"steps"`
	Notes    []Note    `json:"notes,omitempty"`
	Problems []string  `json:"problems,omitempty"`
}

// pending 是一個檔要改的內容（同一個檔的幾件事合在一起改一次）。
type pending struct {
	path    string // 現在的位置
	what    string
	fm      map[string]change
	tomls   []tomlEdit
	rewrite func(string) (string, int)
}

// builder 組計畫：先收每一件要做的事，Finish 時才讀檔、算改後的內容、排順序、掃一遍還留著舊代號的地方。
type builder struct {
	home     string
	renames  []Rename
	byOld    map[string]*Rename
	kinds    []Kind
	moves    []Step // rename
	movedTo  map[string]string
	dirMoves [][2]string
	writes   map[string]*pending
	order    []string
	notes    []Note
	probs    []string
	sums     map[string]*Summary
	skipScan []string        // 掃描時跳過（紀錄檔、原文資料夾）
	official string          // 官方角色資料夾
	refs     map[string]bool // 有紀錄指著的檔（絕對路徑，DEV-160）
	// pointedBy：每個檔（絕對路徑轉小寫：macOS 的檔名不分大小寫）有哪幾位角色的紀錄指著（宣告 file_field 的種類，全部紀錄）。
	// 收成自己的時，別的角色的紀錄也指著的檔不搬（DEV-160 第二輪審查）。
	pointedBy map[string]map[string]bool
}

func newBuilder(home string, kinds []Kind) *builder {
	return &builder{home: home, official: platform.OfficialPersonasDir(home), kinds: kinds, byOld: map[string]*Rename{}, movedTo: map[string]string{},
		writes: map[string]*pending{}, sums: map[string]*Summary{}, refs: map[string]bool{}, pointedBy: map[string]map[string]bool{}}
}

// rel 是給人看的路徑：家底下的寫相對路徑，家外的寫絕對路徑。
func (b *builder) rel(p string) string {
	if r, err := filepath.Rel(b.home, p); err == nil && r != ".." && !strings.HasPrefix(r, "../") {
		return filepath.ToSlash(r)
	}
	return p
}

func (b *builder) problem(format string, a ...any) {
	b.probs = append(b.probs, fmt.Sprintf(format, a...))
}

func (b *builder) note(path string, line int, text, why string) {
	b.notes = append(b.notes, Note{Path: b.rel(path), Line: line, Text: text, Why: why})
}

// move 加一步改名；目的地已有東西、或兩步搬到同一個地方都是問題（不蓋）。
func (b *builder) move(from, to, what string) {
	if from == to {
		return
	}
	if prev, dup := b.movedTo[to]; dup {
		b.problem("%s 與 %s 都要變成 %s", b.rel(prev), b.rel(from), b.rel(to))
		return
	}
	if exists(to) {
		b.problem("%s 已經有了，不蓋（%s 沒辦法改過去）", b.rel(to), b.rel(from))
		return
	}
	b.movedTo[to] = from
	b.moves = append(b.moves, Step{Op: OpRename, Path: from, To: to, What: what})
}

// moveDir 加一步整個資料夾搬走（官方的東西搬出家）；之後要改這個資料夾裡的檔，改的是搬完的位置。
func (b *builder) moveDir(from, to, what string) {
	n := len(b.moves)
	b.move(from, to, what)
	if len(b.moves) > n {
		b.dirMoves = append(b.dirMoves, [2]string{from, to})
	}
}

// edit 拿一個檔的待改內容（沒有就新開一份）。
func (b *builder) edit(path, what string) *pending {
	if p, ok := b.writes[path]; ok {
		return p
	}
	p := &pending{path: path, what: what}
	b.writes[path] = p
	b.order = append(b.order, path)
	return p
}

// finalPath 是 p 這個檔照這份計畫搬完之後的位置。
func (b *builder) finalPath(p string) string {
	for _, m := range b.moves {
		if m.Path == p {
			return m.To
		}
	}
	for _, d := range b.dirMoves {
		if rel, err := filepath.Rel(d[0], p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepath.Join(d[1], rel)
		}
	}
	return p
}

func (b *builder) sum(old string) *Summary {
	s := b.sums[old]
	if s == nil {
		r := b.byOld[old]
		s = &Summary{Old: old, New: r.New}
		b.sums[old] = s
	}
	return s
}

// boundary：檔名是不是「代號」開頭、後面接 . @ ~ +（找 athena 不會抓到 athena2、athenas）。
func boundary(name, code string) (string, bool) {
	if !strings.HasPrefix(name, code) || len(name) == len(code) {
		return "", false
	}
	rest := name[len(code):]
	return rest, strings.ContainsRune(".@~+", rune(rest[0]))
}

// addRename 收一位角色：檢查代號，排卡、圖的步驟，列出旁邊不動的東西。
func (b *builder) addRename(r Rename) {
	for _, x := range b.renames {
		switch {
		case x.Old == r.Old:
			b.problem("%s 出現兩次（同一個代號兩張卡？紀錄分不出是哪一張，先處理其中一張）", r.Old)
			return
		case x.New == r.New:
			b.problem("%s 與 %s 都要改成 %s", x.Old, r.Old, r.New)
			return
		case x.New == r.Old || x.Old == r.New:
			b.problem("%s → %s 與 %s → %s 互相牽連，請分兩次做", x.Old, x.New, r.Old, r.New)
			return
		}
	}
	b.renames = append(b.renames, r)
	rp := &b.renames[len(b.renames)-1]
	b.byOld[r.Old] = rp
	s := b.sum(r.Old)
	s.Card = b.rel(r.CardFrom) + " → " + b.rel(r.CardTo)
	b.move(r.CardFrom, r.CardTo, "角色卡")
}

// addMedia 在 addRecords 之後：列出這次沒動到的圖（沒有紀錄指著、檔名以舊代號開頭的；收成自己的時官方位置沒搬的東西）。
func (b *builder) addMedia() {
	for i := range b.renames {
		r := &b.renames[i]
		if r.Media != "" && r.MoveTo == "" {
			_ = filepath.WalkDir(r.Media, func(p string, d fs.DirEntry, err error) error {
				if err != nil || p == r.Media {
					return nil
				}
				name := d.Name()
				if strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-") {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if _, ok := boundary(name, r.Old); !ok || d.IsDir() || p == r.CardFrom || b.refs[p] {
					return nil
				}
				if _, moved := b.moving(p); !moved {
					b.note(p, 0, "", "檔名是舊代號 "+r.Old+" 開頭、沒有照片紀錄指著，沒改名也沒搬（衣櫃只照紀錄認圖；要用就先讓衣櫃補登記再改代號）")
				}
				return nil
			})
		}
		b.listUnder(r)
	}
}

// listUnder：ListUnder 這些資料夾（含子資料夾，跳過 SkipUnder）裡還有舊代號開頭、這次沒搬的檔或資料夾就列出來、不動。
func (b *builder) listUnder(r *Rename) {
	for _, root := range r.ListUnder {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || p == root {
				return nil
			}
			for _, skip := range r.SkipUnder {
				if p == skip {
					return filepath.SkipDir
				}
			}
			// _ 開頭（_style.md、_test/ 這類主人的東西）、隱藏的、備份不算官方位置的東西。
			if n := d.Name(); strings.HasPrefix(n, "_") || strings.HasPrefix(n, ".") || strings.Contains(n, ".bak-") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if p == r.CardFrom {
				return nil
			}
			if _, ok := boundary(d.Name(), r.Old); ok || d.Name() == r.Old {
				if _, moved := b.moving(p); !moved {
					b.note(p, 0, "", r.Old+" 的東西放在官方位置，沒搬（要用就自己搬進 personas/mine/）")
				}
				if d.IsDir() {
					return filepath.SkipDir
				}
			}
			return nil
		})
	}
}

// addFile：一筆紀錄（宣告 file_field 的種類）的檔。記下它有紀錄指著；收成自己的（MoveTo）時把放在 Media 的檔搬到 MoveTo 的同一個相對路徑。
func (b *builder) addFile(k Kind, owner *Rename, rec map[string]any) {
	file, _ := rec[k.File].(string)
	if file == "" || !personapack.CleanPath(file) {
		return
	}
	src := ""
	if k.Source != "" {
		src, _ = rec[k.Source].(string)
	}
	root := b.official
	if src == "mine" {
		root = filepath.Join(b.home, "personas", "mine")
	}
	p := filepath.Join(root, filepath.FromSlash(file))
	b.refs[p] = true
	b.sum(owner.Old).Media++
	if owner.MoveTo == "" || src == owner.Source || root != owner.Media {
		return
	}
	if _, moving := b.moving(p); moving {
		return
	}
	for who := range b.pointedBy[strings.ToLower(p)] {
		if who != owner.Old {
			b.note(p, 0, "", "紀錄指著的檔也是別的角色（"+who+"）的紀錄指著的，沒搬")
			return
		}
	}
	if why := b.notPersonaMedia(root, file, owner.Old); why != "" {
		b.note(p, 0, "", "紀錄指著的檔"+why+"，沒搬")
		return
	}
	if !isRegular(p) {
		b.note(p, 0, "", "紀錄指著的檔不在（或不是一般的檔），沒搬")
		return
	}
	b.move(p, filepath.Join(owner.MoveTo, filepath.FromSlash(file)), "圖")
}

// mediaExt 是收成自己的時會跟著搬的檔：圖與影片。
var mediaExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".mp4": true}

// notPersonaMedia：收成自己的時，紀錄指著的檔（root 底下的 file）能不能搬——紀錄的 file 是資料、不是保證（DEV-160 審查 S8）：
// 只搬 avatars/ 或 wardrobe/<舊代號>/ 底下的圖與影片，avatars/ 底下檔名是別的角色（這個家有她的卡）的不搬，路上的資料夾不能是符號連結。
// 回不搬的原因（接在「紀錄指著的檔」後面）；空＝可以搬。
func (b *builder) notPersonaMedia(root, file, old string) string {
	parts := strings.Split(file, "/")
	switch {
	case !(len(parts) >= 2 && parts[0] == "avatars") && !(len(parts) >= 3 && parts[0] == "wardrobe" && parts[1] == old):
		return "不在 avatars/ 或 wardrobe/" + old + "/ 底下"
	case !mediaExt[strings.ToLower(filepath.Ext(file))]:
		return "不是圖或影片"
	}
	if parts[0] == "avatars" {
		base := parts[len(parts)-1]
		// 轉小寫比（macOS 的檔名不分大小寫：avatars/Luna.png 就是 luna 的頭像）
		if i := strings.IndexAny(base, "@~.+"); i > 0 {
			name := strings.ToLower(base[:i])
			if name != old && personaref.ValidSlug(name) &&
				(exists(filepath.Join(b.official, name+".md")) || exists(filepath.Join(b.home, "personas", "mine", name+".md"))) {
				return "檔名是別的角色 " + name + " 的"
			}
		}
	}
	p := root
	for _, seg := range parts[:len(parts)-1] {
		p = filepath.Join(p, seg)
		if !isDir(p) {
			return "路上有符號連結或不是資料夾的段"
		}
	}
	return ""
}

func (b *builder) moving(p string) (string, bool) {
	for _, m := range b.moves {
		if m.Path == p {
			return m.To, true
		}
	}
	return "", false
}

// codeOf 回 s 是不是要改的舊代號（整個值），回那一筆 Rename。
func (b *builder) codeOf(s string) *Rename { return b.byOld[s] }

// prefixOf：s 是 <舊代號>-<其他> 時回那一筆 Rename 與新的值。
// 後面還有「.」的不算：那是套件角色（<套件>.<角色>-<其他>，套件名可以有「-」，例：garden-x.fairy-casual 的 garden 不是角色）。
func (b *builder) prefixOf(s string) (*Rename, string) {
	head, rest, ok := strings.Cut(s, "-")
	if !ok || strings.Contains(rest, ".") {
		return nil, ""
	}
	if r := b.byOld[head]; r != nil {
		return r, r.New + "-" + rest
	}
	return nil, ""
}

// addIssues：讀不起來的套件，vault/pkg/<套件>/ 裡有紀錄就擋下（改了一半，紀錄會對不到角色），沒有紀錄就只列出來。
func (b *builder) addIssues(issues []PackageIssue) {
	if len(b.renames) == 0 {
		return // 沒有要改代號的角色（只搬官方的東西），紀錄不用動
	}
	for _, is := range issues {
		dir := filepath.Join(b.home, "vault", "pkg", is.Package)
		has := false
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
				has = true
				return fs.SkipAll
			}
			return nil
		})
		if has {
			b.problem("%s，而且 vault/pkg/%s/ 裡有紀錄：先修好這個套件（或停用、移走）再做", is.Message, is.Package)
		} else {
			b.note(filepath.Join(b.home, "packages"), 0, "", is.Message+"（它沒有紀錄，不影響）")
		}
	}
}

// addRoster：名冊 vault/agents/<職位>/agent.md 的 persona 是舊代號的改成新代號（只改那一行）。
func (b *builder) addRoster() {
	dirs, _ := os.ReadDir(filepath.Join(b.home, "vault", "agents"))
	for _, d := range dirs {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") || d.Name() == "archive" || strings.Contains(d.Name(), ".bak-") {
			continue
		}
		p := filepath.Join(b.home, "vault", "agents", d.Name(), "agent.md")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		rec, _, err := vault.Decode(data)
		if err != nil {
			continue
		}
		slug, _ := rec.Fields["persona"].(string)
		r := b.codeOf(slug)
		if r == nil {
			continue
		}
		pe := b.edit(p, "名冊")
		if pe.fm == nil {
			pe.fm = map[string]change{}
		}
		pe.fm["persona"] = change{r.Old, r.New}
		s := b.sum(r.Old)
		s.Roster = append(s.Roster, d.Name())
	}
}

// addRecords：照套件的宣告改 vault/pkg/<套件>/<種類>/ 底下的紀錄（不含 archive/）：欄位值、編號（檔名與檔頭的 id）、
// persona_source。目的檔已經有了就整個計畫不做（不蓋）。
func (b *builder) addRecords() {
	type entry struct {
		k     Kind
		p, id string
	}
	var all []entry
	for _, k := range b.kinds {
		pkgID, kind, ok := strings.Cut(k.Kind, ".")
		if !ok {
			continue
		}
		dir := filepath.Join(b.home, "vault", "pkg", pkgID, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			id, isMD := strings.CutSuffix(name, ".md")
			// 捷徑（symlink）也看：只改檔名的可以（搬的是捷徑本身）；要改內容的在 finish 擋下（不把捷徑換成一般檔）。
			if !isMD || e.IsDir() || strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-") {
				continue
			}
			all = append(all, entry{k, filepath.Join(dir, name), id})
		}
	}
	// 先記下每個檔有哪幾位角色的紀錄指著（要搬的檔照這個看有沒有別人的紀錄也指著它），再一筆一筆算。
	for _, e := range all {
		if e.k.File != "" {
			b.notePointer(e.k, e.p, e.id)
		}
	}
	for _, e := range all {
		b.addRecord(e.k, e.p, e.id)
	}
}

// notePointer 記下一筆紀錄指著的檔是哪位角色的紀錄（角色欄位；沒有就照編號）。讀不了、讀不懂的跳過（addRecord 會說）。
func (b *builder) notePointer(k Kind, p, id string) {
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	rec, _, err := vault.Decode(data)
	if err != nil {
		return
	}
	file, _ := rec.Fields[k.File].(string)
	if file == "" || !personapack.CleanPath(file) {
		return
	}
	persona, _ := rec.Fields[k.Field].(string)
	if persona == "" {
		switch k.ID {
		case "exact":
			persona = id
		case "prefix":
			persona, _, _ = strings.Cut(id, "-")
		}
	}
	key := strings.ToLower(b.fileAt(k, rec.Fields, file))
	if b.pointedBy[key] == nil {
		b.pointedBy[key] = map[string]bool{}
	}
	b.pointedBy[key][persona] = true
}

// fileAt 是一筆紀錄的檔的絕對路徑：source 是 mine 的在 personas/mine/，其他在官方角色資料夾。
func (b *builder) fileAt(k Kind, rec map[string]any, file string) string {
	src := ""
	if k.Source != "" {
		src, _ = rec[k.Source].(string)
	}
	if src == "mine" {
		return filepath.Join(b.home, "personas", "mine", filepath.FromSlash(file))
	}
	return filepath.Join(b.official, filepath.FromSlash(file))
}

func (b *builder) addRecord(k Kind, p, id string) {
	newID := id
	var owner *Rename
	switch k.ID {
	case "exact":
		if r := b.codeOf(id); r != nil {
			newID, owner = r.New, r
		}
	case "prefix":
		if r, n := b.prefixOf(id); r != nil {
			newID, owner = n, r
		}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if owner != nil {
			b.problem("%s 讀不了，沒辦法跟著改", b.rel(p))
		}
		return
	}
	rec, _, err := vault.Decode(data)
	if err != nil {
		if owner != nil {
			b.problem("%s 讀不懂（%s），沒辦法跟著改", b.rel(p), err.Error())
		}
		return
	}
	changes := map[string]change{}
	if newID != id {
		// 檔頭沒寫 id 的（舊紀錄）編號就是檔名：只改檔名。寫了但跟檔名對不上的不知道哪個對，擋下。
		if cur, has := rec.Fields["id"]; has {
			if cur != id {
				b.problem("%s 的檔頭 id 是「%v」，跟檔名對不上，沒辦法跟著改", b.rel(p), cur)
				return
			}
			changes["id"] = change{id, newID}
		}
	}
	if k.Field != "" {
		if v, _ := rec.Fields[k.Field].(string); v != "" {
			if r := b.codeOf(v); r != nil {
				changes[k.Field] = change{v, r.New}
				if owner == nil {
					owner = r
				}
			}
		}
	}
	for _, f := range k.Prefixed {
		if v, _ := rec.Fields[f].(string); v != "" {
			if r, n := b.prefixOf(v); r != nil {
				changes[f] = change{v, n}
			}
		}
	}
	if owner != nil && k.File != "" {
		b.addFile(k, owner, rec.Fields)
	}
	if owner != nil && owner.Source != "" && k.Source != "" {
		if v, ok := rec.Fields[k.Source].(string); ok && v != owner.Source {
			changes[k.Source] = change{v, owner.Source}
		}
	}
	if len(changes) == 0 && newID == id {
		return
	}
	if newID != id {
		b.move(p, filepath.Join(filepath.Dir(p), newID+".md"), "套件紀錄 "+k.Kind)
	}
	if len(changes) > 0 {
		pe := b.edit(p, "套件紀錄 "+k.Kind)
		pe.fm = changes
	}
	if owner != nil {
		b.sum(owner.Old).Records++
	} else {
		for _, c := range changes {
			if r, _ := b.prefixOf(c.old); r != nil {
				b.sum(r.Old).Records++
				break
			}
		}
	}
}

// addSettings：核心自己存角色代號的設定：home.toml 的 [web] app_icon、[defaults] persona；settings/packages.toml 的
// [claims.<職位>] persona；settings/persona-import.toml（角色匯入登記）[files] 的路徑與 [records] 的 <種類>/<編號>。
func (b *builder) addSettings() {
	homeTOML := filepath.Join(b.home, "home.toml")
	var h struct {
		Web struct {
			AppIcon string `toml:"app_icon"`
		} `toml:"web"`
		Defaults struct {
			Persona string `toml:"persona"`
		} `toml:"defaults"`
	}
	if data, err := os.ReadFile(homeTOML); err == nil {
		if _, err := toml.Decode(string(data), &h); err == nil {
			if r := b.codeOf(h.Web.AppIcon); r != nil {
				pe := b.edit(homeTOML, "設定")
				pe.tomls = append(pe.tomls, setString([]string{"web"}, "app_icon", r.Old, r.New))
				b.sum(r.Old).Settings++
			}
			if r := b.codeOf(h.Defaults.Persona); r != nil {
				pe := b.edit(homeTOML, "設定")
				pe.tomls = append(pe.tomls, setString([]string{"defaults"}, "persona", r.Old, r.New))
				b.sum(r.Old).Settings++
			}
		}
	}
	pkgs := filepath.Join(b.home, "settings", "packages.toml")
	var st struct {
		Claims map[string]struct {
			Persona string `toml:"persona"`
		} `toml:"claims"`
	}
	if data, err := os.ReadFile(pkgs); err == nil {
		if _, err := toml.Decode(string(data), &st); err == nil {
			ids := make([]string, 0, len(st.Claims))
			for id := range st.Claims {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				if r := b.codeOf(st.Claims[id].Persona); r != nil {
					pe := b.edit(pkgs, "設定")
					pe.tomls = append(pe.tomls, setString([]string{"claims", id}, "persona", r.Old, r.New))
					b.sum(r.Old).Settings++
				}
			}
		}
	}
}

// addImportRegistry：角色匯入登記（settings/persona-import.toml）的鍵跟著改名：[files] 是家底下的相對路徑，
// [records] 是 <種類>/<編號>。新的位置在家外（升成官方的卡）就不改鍵、列出來（那張卡已經不是匯入的自己的角色了）。
// 要在步驟都排好之後呼叫（才知道哪些檔改了名）。
func (b *builder) addImportRegistry() {
	p := filepath.Join(b.home, "settings", "persona-import.toml")
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var reg struct {
		Files   map[string]string `toml:"files"`
		Records map[string]string `toml:"records"`
	}
	if _, err := toml.Decode(string(data), &reg); err != nil {
		b.note(p, 0, "", "讀不懂，裡面登記的路徑沒跟著改")
		return
	}
	var edits []tomlEdit
	for _, m := range b.moves {
		oldRel, newRel := b.rel(m.Path), b.rel(m.To)
		if _, ok := reg.Files[oldRel]; !ok {
			continue
		}
		if filepath.IsAbs(newRel) || !strings.HasPrefix(filepath.ToSlash(newRel), "personas/mine/") {
			// 搬出 personas/mine/（升成官方）：不再是匯入的自己的角色，這一筆拿掉（退回時會放回來）。
			edits = append(edits, deleteKey([]string{"files"}, oldRel))
			continue
		}
		edits = append(edits, renameKey([]string{"files"}, oldRel, newRel))
	}
	for _, m := range b.moves {
		if !strings.HasSuffix(m.Path, ".md") || !strings.Contains(m.Path, string(filepath.Separator)+"pkg"+string(filepath.Separator)) {
			continue
		}
		parts := strings.Split(filepath.ToSlash(b.rel(m.Path)), "/")
		if len(parts) != 5 || parts[0] != "vault" || parts[1] != "pkg" {
			continue
		}
		key := parts[2] + "." + parts[3] + "/" + strings.TrimSuffix(parts[4], ".md")
		if _, ok := reg.Records[key]; ok {
			edits = append(edits, renameKey([]string{"records"}, key, parts[2]+"."+parts[3]+"/"+strings.TrimSuffix(filepath.Base(m.To), ".md")))
		}
	}
	if len(edits) > 0 {
		pe := b.edit(p, "角色匯入登記")
		pe.tomls = append(pe.tomls, edits...)
	}
}

// finish 算每個要改的檔的新內容、補建資料夾的步驟、排順序（建資料夾 → 改名搬位置 → 改內容），掃一遍還寫著舊代號的地方。
func (b *builder) finish() *Plan {
	p := &Plan{Home: b.home}
	for _, r := range b.renames {
		p.Codes = append(p.Codes, Code{Old: r.Old, New: r.New})
		p.Summary = append(p.Summary, *b.sum(r.Old))
	}
	var writes []Step
	after := map[string][]byte{}
	for _, path := range b.order {
		pe := b.writes[path]
		info, err := os.Lstat(path)
		if err == nil && !info.Mode().IsRegular() {
			// 捷徑（symlink）或特殊檔：改內容會把它換成一般的檔（捷徑斷掉、指到的那個檔沒改），不做，請人看。
			b.problem("%s 是捷徑（symlink）或不是一般的檔，裡面的角色代號沒辦法跟著改；請先把它換成一般的檔", b.rel(path))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			b.problem("讀不了 %s", b.rel(path))
			continue
		}
		out := data
		var edits []Edit
		if len(pe.fm) > 0 {
			o, e, err := editFrontmatter(out, pe.fm)
			if err != nil {
				b.problem("%s：%s", b.rel(path), err.Error())
				continue
			}
			out, edits = o, append(edits, e...)
		}
		if len(pe.tomls) > 0 {
			o, e, err := editTOML(out, pe.tomls)
			if err != nil {
				b.problem("%s：%s", b.rel(path), err.Error())
				continue
			}
			out, edits = o, append(edits, e...)
		}
		if pe.rewrite != nil {
			s, n := pe.rewrite(string(out))
			if n > 0 {
				edits = append(edits, lineDiff(string(out), s)...)
				out = []byte(s)
			}
		}
		if string(out) == string(data) {
			continue
		}
		final := b.finalPath(path)
		writes = append(writes, Step{Op: OpWrite, Path: final, What: pe.what, Edits: edits, BeforeSHA: sum(data), AfterSHA: sum(out),
			Mode: uint32(info.Mode().Perm()), before: data, after: out})
		after[final] = out
	}
	// 建資料夾：改名搬位置的目的地上層沒有的，從上往下一層一層建（退回時是空的才拿掉）。
	need := map[string]bool{}
	for _, m := range b.moves {
		for d := filepath.Dir(m.To); !exists(d) && !need[d]; d = filepath.Dir(d) {
			need[d] = true
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	var dirs []string
	for d := range need {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		p.Steps = append(p.Steps, Step{Op: OpMkdir, Path: d, What: "資料夾"})
	}
	p.Steps = append(p.Steps, b.moves...)
	p.Steps = append(p.Steps, writes...)
	b.scan(after)
	p.Notes = b.notes
	p.Problems = b.probs
	return p
}

// lineDiff 列出兩段文字逐行不同的地方（行數一樣時；路徑改寫只換行內的字）。
func lineDiff(a, c string) []Edit {
	la, lc := splitLines(a), splitLines(c)
	if len(la) != len(lc) {
		return []Edit{{Line: 0, Before: "（整個檔）", After: "（整個檔）"}}
	}
	var out []Edit
	for i := range la {
		if la[i] != lc[i] {
			x, _ := trimEOL(la[i])
			y, _ := trimEOL(lc[i])
			out = append(out, Edit{Line: i + 1, Before: x, After: y})
		}
	}
	return out
}

// 掃描還寫著舊代號的地方：vault/、settings/、packages/mine/ 的文字檔與 home.toml，舊代號當獨立字詞（前後不是英數字或底線）出現的。
var scanExts = map[string]bool{".md": true, ".toml": true, ".json": true, ".jsonl": true, ".yaml": true, ".yml": true, ".txt": true}

func (b *builder) scan(after map[string][]byte) {
	if len(b.renames) == 0 {
		return
	}
	var pats []*regexp.Regexp
	var olds []string
	for _, r := range b.renames {
		pats = append(pats, regexp.MustCompile(`(^|[^A-Za-z0-9_])`+regexp.QuoteMeta(r.Old)+`([^A-Za-z0-9_]|$)`))
		olds = append(olds, r.Old)
	}
	check := func(path string, data []byte) {
		field := b.fileFieldOf(path)
		for i, line := range strings.Split(string(data), "\n") {
			if field != "" && strings.HasPrefix(line, field+":") {
				continue // 檔案位置：檔名沒有意思，不跟著改（DEV-160）
			}
			for k, re := range pats {
				if re.MatchString(line) {
					b.notes = append(b.notes, Note{Path: b.rel(path), Line: i + 1, Text: clip(strings.TrimSpace(line)), Why: "還寫著舊代號 " + olds[k] + "（沒有宣告，沒改）"})
					break
				}
			}
		}
	}
	seen := map[string]bool{}
	visit := func(path string) {
		if seen[path] {
			return
		}
		seen[path] = true
		if data, ok := after[path]; ok {
			check(path, data)
			return
		}
		if data, err := os.ReadFile(path); err == nil {
			check(b.finalPath(path), data)
		}
	}
	b.scanRecordNames()
	visit(filepath.Join(b.home, "home.toml"))
	for _, root := range []string{"vault", "settings", filepath.Join("packages", "mine")} {
		_ = filepath.WalkDir(filepath.Join(b.home, root), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-") || b.skipped(p) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() || !d.Type().IsRegular() || !scanExts[strings.ToLower(filepath.Ext(name))] {
				return nil
			}
			// 這次會搬走的檔：掃改後的內容與新位置（after 用新位置當鍵）。
			if final := b.finalPath(p); final != p {
				if _, ok := after[final]; ok {
					visit(final)
					return nil
				}
			}
			visit(p)
			return nil
		})
	}
}

// fileFieldOf：p 是宣告 file_field 的種類的紀錄（vault/pkg/<套件>/<種類>/ 底下，含封存）時回那個欄位名。
func (b *builder) fileFieldOf(p string) string {
	for _, k := range b.kinds {
		pkgID, kind, ok := strings.Cut(k.Kind, ".")
		if !ok || k.File == "" {
			continue
		}
		if inside(filepath.Join(b.home, "vault", "pkg", pkgID, kind), p) {
			return k.File
		}
	}
	return ""
}

// scanRecordNames：vault/pkg/<套件>/<種類>/ 底下檔名就是舊代號、或 <舊代號>-<其他> 的紀錄，這次沒有跟著改名的
// （那個套件沒宣告 persona_id）：內容不一定寫著代號，掃內容掃不到，所以另外列出來請人看。
func (b *builder) scanRecordNames() {
	root := filepath.Join(b.home, "vault", "pkg")
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "archive" || strings.Contains(name, ".bak-")) {
				return filepath.SkipDir
			}
			return nil
		}
		id, ok := strings.CutSuffix(name, ".md")
		if !ok || strings.Contains(name, ".bak-") {
			return nil
		}
		if _, moved := b.moving(p); moved {
			return nil
		}
		r := b.codeOf(id)
		if r == nil {
			r, _ = b.prefixOf(id)
		}
		if r != nil {
			b.note(p, 0, "", "紀錄的編號是舊代號 "+r.Old+" 開頭，套件沒宣告怎麼跟著改（persona_id），沒改名")
		}
		return nil
	})
}

func (b *builder) skipped(p string) bool {
	for _, s := range b.skipScan {
		if p == s || strings.HasPrefix(p, s+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func clip(s string) string {
	r := []rune(s)
	if len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return s
}

// Apply 照計畫做（Problems 不是空的不做）；tool 是紀錄檔裡寫的工具名，journalPath、originals 是紀錄檔與原文資料夾的位置。
func (p *Plan) Apply(tool, journalPath, originals string, now time.Time) error {
	if len(p.Problems) > 0 {
		return conflictf("有問題沒解決，什麼都沒動：\n  %s", joinLines(p.Problems))
	}
	if len(p.Steps) == 0 {
		return nil
	}
	j := &journal{Schema: 1, Tool: tool, Home: p.Home, At: stamp(now), Renames: p.Codes, Originals: originals}
	return apply(p.Steps, j, journalPath)
}
