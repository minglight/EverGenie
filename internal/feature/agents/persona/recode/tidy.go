package recode

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
)

// evergenie home tidy（DEV-154 第 10 件，PRS-22）：搬家前把家整理好，四件事——
//   a. 自己的角色（personas/mine/）代號改成 my_ 開頭（卡、名冊、套件紀錄、設定一起改；圖不改名，照片紀錄照舊指著原檔，DEV-160）；
//   b. 放在官方位置、但判斷不是官方的角色：列出來，主人點名（--adopt）才收進 personas/mine/ 並改成 my_ 開頭（照片紀錄指著的圖搬進
//      personas/mine/ 的同一個相對路徑，紀錄的 source 改成 mine）；
//   c. 官方的卡與圖搬出家（第 2 版佈局、official_personas = "home"、不是發行角色包的家，加 --move-official 才搬）；
//   d. 家裡散著的舊備份：只列出。
// 紀錄檔 settings/home-tidy.json、原文 settings/home-tidy-originals/；一次只能有一份（要再整理先退回，或確定不退了用 --forget 收尾：紀錄改名成備份）。

// TidyJournal、TidyOriginals 是紀錄檔與原文資料夾的位置。
func TidyJournal(home string) string   { return platform.TidyJournalPath(home) }
func TidyOriginals(home string) string { return filepath.Join(home, "settings", "home-tidy-originals") }

// TidyOptions 是 home tidy 的選項。
type TidyOptions struct {
	Rename       map[string]string // --rename 舊=新（自己的角色指定新代號）
	Adopt        []string          // --adopt 代號（官方位置的卡收成自己的）
	MoveOfficial bool              // --move-official
	// Official：--official 代號。主人確認這幾張放在官方位置的卡是官方的（舊的家是部署時抄進來的、沒有角色包登記檔，判斷不出來）；
	// 只影響這一次怎麼分，不寫進任何登記。
	Official []string
}

// Tidy 是 home tidy 的預演：Plan 是要做的步驟；其他是給人看的分段。
type Tidy struct {
	*Plan
	Mine         []Code   `json:"mine,omitempty"`          // a：自己的角色改代號
	Undecided    []Note   `json:"undecided,omitempty"`     // a：要人決定的（名字不合規則、跟官方同代號）
	Misplaced    []string `json:"misplaced,omitempty"`     // b：官方位置、判斷不是官方的卡（代號）
	MisplacedWhy string   `json:"misplaced_why,omitempty"` //
	Adopted      []Code   `json:"adopted,omitempty"`       // b：這次收成自己的
	Official     []string `json:"official,omitempty"`      // c：還在家裡的官方東西
	OfficialWhy  string   `json:"official_why,omitempty"`  //
	OfficialMove bool     `json:"official_move,omitempty"` // c：這次會搬
	Backups      []string `json:"backups,omitempty"`       // d
}

// officialCardName 是官方卡（含它的備份）的檔名：跟 home relayout 搬官方角色時同一條規則。
var officialCardName = regexp.MustCompile(`^([a-z][a-z0-9]*)\.md(\.bak-.+)?$`)

// PlanTidy 算 home tidy 要做的事（不動任何檔）。kinds、issues 由組裝點從各層 package.toml 讀好給。
func PlanTidy(home string, kinds []Kind, issues []PackageIssue, opt TidyOptions) (*Tidy, error) {
	h, err := platform.LoadHome(home)
	if err != nil {
		return nil, err
	}
	l := platform.LayoutFor(home)
	official := platform.OfficialPersonasDir(home)
	mineDir := filepath.Join(home, "personas", "mine")
	publishes := len(h.Admin.PublicPersonas) > 0
	t := &Tidy{}
	b := newBuilder(home, kinds)
	b.skipScan = []string{TidyJournal(home), TidyOriginals(home), platform.PromoteJournalDir(home), filepath.Join(home, "settings", "relayout-rewrites")}

	// b：官方位置頂層的卡，哪些是官方的（角色包登記檔或 public_personas）。
	registered, hasRegistry, err := personapack.RegisteredCards(home)
	if err != nil {
		return nil, err
	}
	for _, c := range h.Admin.PublicPersonas {
		registered[c] = true
	}
	confirmed := map[string]bool{}
	for _, c := range opt.Official {
		confirmed[c] = true
	}
	officialCodes := map[string]bool{} // 官方位置有卡的代號（不管是不是官方）
	undetermined := map[string]bool{}
	for _, name := range readNames(official) {
		m := officialCardName.FindStringSubmatch(name)
		if m == nil || m[2] != "" || !isRegular(filepath.Join(official, name)) {
			continue
		}
		officialCodes[m[1]] = true
		if confirmed[m[1]] {
			confirmed[m[1]] = false // 用到了
			continue
		}
		if !registered[m[1]] {
			undetermined[m[1]] = true
		}
	}
	for _, c := range sortedKeys(confirmed) {
		if confirmed[c] {
			b.problem("--official %s：官方位置沒有 %s.md", c, c)
		}
	}
	t.Misplaced = sortedKeys(undetermined)
	switch {
	case len(t.Misplaced) == 0:
	case !hasRegistry && !publishes:
		t.MisplacedWhy = "這個家沒有角色包登記檔、也不是發行角色包的家，判斷不出哪些是官方的，所以官方位置的卡全部列出來；是自己的角色就加 --adopt <代號> 收進 personas/mine/（改成 my_<代號>）；是官方的，要搬出家時加 --official <代號> 確認"
	default:
		t.MisplacedWhy = "不在角色包登記檔、也不在 home.toml [admin] public_personas，可能是自己的角色放錯位置；是的話加 --adopt <代號> 收進 personas/mine/（改成 my_<代號>）"
	}
	adopting := map[string]bool{}
	for _, code := range opt.Adopt {
		switch {
		case adopting[code]:
			continue
		case !officialCodes[code]:
			b.problem("--adopt %s：官方位置沒有 %s.md", code, code)
			continue
		case !undetermined[code]:
			b.problem("--adopt %s：%s 是官方角色（在角色包登記檔、public_personas 或這次的 --official 裡），不能收成自己的", code, code)
			continue
		}
		newCode := personaref.MinePrefix + code
		if !personaref.MineSlugPattern.MatchString(newCode) {
			b.problem("--adopt %s：%s 太長（my_ 後面最多 24 個字），收不了", code, newCode)
			continue
		}
		adopting[code] = true
		t.Adopted = append(t.Adopted, Code{Old: code, New: newCode})
	}

	// a：自己的角色。
	renamed := map[string]bool{}
	for _, name := range readNames(mineDir) {
		slug, ok := strings.CutSuffix(name, ".md")
		if !ok || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-") || strings.EqualFold(slug, "readme") || !isRegular(filepath.Join(mineDir, name)) {
			continue
		}
		newCode, asked := opt.Rename[slug]
		switch {
		case asked:
			renamed[slug] = true
			if !personaref.MineSlugPattern.MatchString(newCode) {
				b.problem("--rename %s=%s：新代號要是 my_ 開頭、後面 1～24 個小寫英文或數字", slug, newCode)
				continue
			}
			if officialCodes[slug] {
				b.problem("--rename %s：官方位置也有 %s.md，紀錄分不出是哪一位的；先處理官方位置那張", slug, slug)
				continue
			}
		case personaref.MineSlugPattern.MatchString(slug):
			continue
		case !personaref.SlugPattern.MatchString(slug) || !personaref.MineSlugPattern.MatchString(personaref.MinePrefix+slug):
			t.Undecided = append(t.Undecided, Note{Path: b.rel(filepath.Join(mineDir, name)), Why: "名字不合規則（含底線、大寫或太長），沒自動改；要改請加 --rename " + slug + "=my_<新名字>"})
			continue
		case officialCodes[slug]:
			t.Undecided = append(t.Undecided, Note{Path: b.rel(filepath.Join(mineDir, name)), Why: "官方位置也有 " + slug + ".md，紀錄分不出是哪一位的，沒自動改（看板用的是官方那張）；先處理官方位置那張"})
			continue
		default:
			newCode = personaref.MinePrefix + slug
		}
		t.Mine = append(t.Mine, Code{Old: slug, New: newCode})
		b.addRename(Rename{Old: slug, New: newCode, CardFrom: filepath.Join(mineDir, slug+".md"), CardTo: filepath.Join(mineDir, newCode+".md"), Media: mineDir})
	}
	for _, old := range sortedKeys(opt.Rename) {
		if !renamed[old] {
			b.problem("--rename %s：personas/mine/ 沒有 %s.md", old, old)
		}
	}
	if len(t.Adopted) > 0 && !inside(home, official) {
		// 官方位置在家外（狀態資料夾）：收進家裡是跨資料夾搬，要在同一顆磁碟才能一步搬好、退得回來。
		if err := platform.SameDevice(home, l.State); err != nil {
			b.problem("%s", err.Error())
		}
	}
	for _, c := range t.Adopted {
		b.addRename(Rename{
			Old: c.Old, New: c.New,
			CardFrom: filepath.Join(official, c.Old+".md"), CardTo: filepath.Join(mineDir, c.New+".md"),
			Media: official, MoveTo: mineDir, // 紀錄指著的圖搬進 personas/mine/ 的同一個相對路徑、檔名不改（DEV-160）
			ListUnder: []string{official},
			SkipUnder: []string{mineDir},
			Source:    "mine",
		})
	}
	b.addIssues(issues)
	b.addRoster()
	b.addRecords()
	b.addMedia()
	b.addSettings()

	// c：官方的卡與圖搬出家。
	if inside(home, official) {
		var items []string
		for _, name := range readNames(official) {
			p := filepath.Join(official, name)
			switch m := officialCardName.FindStringSubmatch(name); {
			case name == "avatars" || name == "wardrobe" || name == "catalog":
				if isDir(p) {
					items = append(items, name)
				}
			case m != nil && !undetermined[m[1]] && !adopting[m[1]] && isRegular(p):
				items = append(items, name)
			}
		}
		for _, name := range items {
			t.Official = append(t.Official, b.rel(filepath.Join(official, name)))
		}
		outside := filepath.Join(l.State, "personas")
		const stay = "官方衣服的目錄紀錄（vault/pkg/ 底下）留在家裡不搬：資料層讀官方層時家裡的優先，行為不變"
		switch {
		case !l.V2:
			t.OfficialWhy = "這個家是第 1 版佈局，官方角色要搬出家請用 evergenie home relayout --move-personas"
			if opt.MoveOfficial {
				b.problem("--move-official 只用在第 2 版佈局的家；第 1 版請用 evergenie home relayout --move-personas")
			}
		case len(items) == 0:
		case publishes:
			t.OfficialWhy = "這個家發行角色包（home.toml [admin] public_personas 有寫），官方卡是主人的原稿，留在家裡不搬"
			if opt.MoveOfficial {
				b.problem("--move-official：這個家發行角色包，官方卡留在家裡不搬")
			}
		case !opt.MoveOfficial:
			t.OfficialWhy = "加 --move-official 才搬到狀態資料夾（" + outside + "）並拿掉 home.toml 的 official_personas = \"home\"。" + stay
		default:
			t.OfficialMove = true
			t.OfficialWhy = "會搬到狀態資料夾（" + outside + "）並拿掉 home.toml 的 official_personas = \"home\"。" + stay
			if left := remaining(t.Misplaced, adopting); len(left) > 0 {
				b.problem("官方位置還有判斷不出是不是官方的卡（%s）：是自己的就 --adopt 收成自己的，是官方的就加 --official <代號> 確認，再搬", strings.Join(left, "、"))
			}
			if err := platform.SameDevice(home, l.State); err != nil {
				b.problem("%s", err.Error())
			}
			for _, name := range items {
				b.moveDir(filepath.Join(official, name), filepath.Join(outside, name), "官方角色")
			}
			pe := b.edit(filepath.Join(home, "home.toml"), "設定")
			pe.tomls = append(pe.tomls, deleteKey([]string{"layout"}, "official_personas"))
			b.addOfficialRewrites(l)
		}
	} else if opt.MoveOfficial {
		t.OfficialWhy = "官方的卡與圖已經在家外（" + official + "），不用搬"
	}
	b.addImportRegistry()
	if msg := platform.ServiceRunning(home); msg != "" {
		b.problem("%s", strings.Replace(msg, "再搬", "再整理", 1))
	}
	t.Backups = backups(home)
	t.Plan = b.finish()
	if len(t.Steps) > 0 && exists(TidyJournal(home)) {
		t.Problems = append(t.Problems, "上次整理的紀錄還在（settings/home-tidy.json）：要再整理，先用 --revert --apply 退回；確定不退了，先用 --forget --apply 收尾")
	}
	return t, nil
}

// addOfficialRewrites：官方的東西搬出家時，vault/、settings/、personas/mine/ 文字檔裡寫著官方角色在家裡位置的絕對路徑跟著改
// （規則跟 home relayout 同一份，platform.RewriteOfficialPaths）；personas/ 其他地方（要搬走的官方卡、主人放在頂層的 _ 開頭檔）
// 與 packages/mine/ 裡寫著的只列出來（官方卡同步會整份換、主人的套件不動）。
func (b *builder) addOfficialRewrites(l platform.Layout) {
	rewrite := func(s string) (string, int) { return platform.RewriteOfficialPaths(l.Home, l.State, s) }
	mineDir := filepath.Join(l.Home, "personas", "mine")
	for _, root := range []string{"vault", "settings", filepath.Join("personas", "mine"), "personas", filepath.Join("packages", "mine")} {
		change := root != "personas" && root != filepath.Join("packages", "mine")
		_ = filepath.WalkDir(filepath.Join(l.Home, root), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-") || b.skipped(p) || (root == "personas" && p == mineDir) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() || !d.Type().IsRegular() || !scanExts[strings.ToLower(filepath.Ext(name))] {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			if _, n := rewrite(string(data)); n == 0 {
				return nil
			}
			if change {
				b.edit(p, "官方角色的路徑").rewrite = rewrite
				return nil
			}
			for i, line := range strings.Split(string(data), "\n") {
				if _, n := rewrite(line); n > 0 {
					b.note(p, i+1, clip(strings.TrimSpace(line)), "寫著官方角色在家裡的位置，搬出家後會對不上（這裡不改，請自己或那個套件改）")
				}
			}
			return nil
		})
	}
}

// backups 列出家裡散著的舊備份（名字含 .bak- 的檔與資料夾）：vault/、personas/、settings/ 與家的頂層。只列、不搬。
func backups(home string) []string {
	var out []string
	for _, name := range readNames(home) {
		if strings.Contains(name, ".bak-") {
			out = append(out, name)
		}
	}
	for _, root := range []string{"vault", "personas", "settings"} {
		_ = filepath.WalkDir(filepath.Join(home, root), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if strings.Contains(d.Name(), ".bak-") {
				rel, _ := filepath.Rel(home, p)
				out = append(out, filepath.ToSlash(rel))
				if d.IsDir() {
					return filepath.SkipDir
				}
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func readNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func isRegular(p string) bool { i, err := os.Lstat(p); return err == nil && i.Mode().IsRegular() }
func isDir(p string) bool     { i, err := os.Lstat(p); return err == nil && i.IsDir() }

// inside：child 在 parent 底下（或就是 parent）。
func inside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func remaining(all []string, done map[string]bool) []string {
	var out []string
	for _, c := range all {
		if !done[c] {
			out = append(out, c)
		}
	}
	return out
}
