package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
)

// 搬家：把第 1 版的家搬成第 2 版（DEV-145，evergenie home relayout），以及照紀錄退回。
//
// 一律用 os.Rename（同一顆磁碟，瞬間完成、不會掉資料）；跨磁碟在預檢就擋下，什麼都不搬。
// 每搬一項就更新 <家>/settings/relayout.json，中途失敗自動反向搬回並拿掉紀錄；--revert 也靠這份紀錄。

// 搬家項目的類別（預演時給主人看）。
const (
	RelayoutMedia     = "主人的檔案"
	RelayoutRuntime   = "暫存"
	RelayoutSecrets   = "密鑰"
	RelayoutInstalled = "別人的套件"
	RelayoutPersonas  = "官方角色"
	RelayoutStay      = "留在家裡"
)

// RelayoutItem 是一項：從哪、到哪、大小、類別。留在家裡的項目 To 是空的。
type RelayoutItem struct {
	From string
	To   string
	Size int64
	Kind string
}

// RelayoutPlan 是預演的結果：要搬的（照順序）、留在家裡的、預檢發現的問題。
type RelayoutPlan struct {
	Home     string
	State    string
	Moves    []RelayoutItem
	Stay     []RelayoutItem
	Rewrites []RelayoutRewrite // 搬完要改寫路徑的文字檔
	// RuntimeRefs 是寫了家裡 runtime 路徑、但不會改的地方（<家>/runtime/<其他> 的絕對路徑、相對寫法 runtime/share/…）：只提醒。
	RuntimeRefs []RelayoutRewrite
	// NotTemp 是 runtime/ 底下不在已知暫存清單的東西（例如 backups/）：搬到狀態資料夾後不要刪。
	NotTemp []string
	// PersonasAtHome：官方角色留在家裡（預設；沒加 --move-personas，或發行角色包的家，官方卡是主人寫的原稿）。
	PersonasAtHome bool
	Publishes      bool     // [admin] public_personas 有寫
	Backup         string   // --apply 後：home.toml 的備份位置
	Problems       []string // 空的才能 --apply
}

// knownRuntime 是 runtime/ 底下已知的暫存（可以刪）；share、art 會先搬去 media/。persona-、engine- 開頭的（各引擎自己的暫存）另外認。
var knownRuntime = map[string]bool{
	"sockets": true, "locks": true, "logs": true, "sessions": true, "queue": true, "jobs": true,
	"replay": true, "sweep": true, "migrate": true, "upgrade": true, "web": true, "packages": true, "policy": true,
	"trust": true, "personas-pack": true, "voice": true, "exports": true, "engine": true, "now.md": true,
	"serve.pid": true, "vault.lock": true, "share": true, "art": true,
}

// notTempRuntime 列出 runtime/ 底下不是暫存的東西（backups、backup 一定算）。
func notTempRuntime(home string) []string {
	entries, err := os.ReadDir(filepath.Join(home, "runtime"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if n == "backups" || n == "backup" || (!knownRuntime[n] && !strings.HasPrefix(n, "persona-") && !strings.HasPrefix(n, "engine-")) {
			out = append(out, n)
		}
	}
	return out
}

// RelayoutRewrite 是一個要改寫的文字檔：家裡的相對路徑、會改幾處。
type RelayoutRewrite struct {
	Path  string
	Count int
}

// relayoutRecord 是 <家>/settings/relayout.json：搬到哪一步、怎麼退回。
type relayoutRecord struct {
	Version        int            `json:"version"`
	Home           string         `json:"home"`
	State          string         `json:"state"`
	StartedAt      time.Time      `json:"started_at"`
	FinishedAt     *time.Time     `json:"finished_at,omitempty"`
	Backup         string         `json:"home_toml_backup,omitempty"`
	Appended       string         `json:"home_toml_appended,omitempty"`        // 附加在 home.toml 尾巴的原文，退回時照原文拿掉
	SchemaLine     string         `json:"home_toml_schema_line,omitempty"`     // 改成 schema = 2 之前那一行的原文（空的＝原本沒寫，是加在檔頭的）
	PersonasAtHome bool           `json:"official_personas_at_home,omitempty"` // 官方角色沒搬（預設）
	Created        []string       `json:"created_dirs,omitempty"`              // 搬家時新建的資料夾（退回時空了就刪）
	Steps          []relayoutStep `json:"steps"`
	Rewrites       []rewriteStep  `json:"rewrites,omitempty"`
}

// rewriteStep 是一個改寫過路徑的文字檔：原文存在 <家>/settings/relayout-rewrites/<相對路徑>。
type rewriteStep struct {
	Path   string `json:"path"`   // 家裡的相對路徑
	Backup string `json:"backup"` // 原文的位置（絕對路徑）
	Count  int    `json:"count"`
	Sum    string `json:"sha256"` // 改寫後內容的 sha256：退回時一樣就整份還原原文，不一樣（之後又被改過）就反向替換字串
	Status string `json:"status"` // done、reverted
}

type relayoutStep struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Kind      string    `json:"kind"`
	EmptyDest bool      `json:"empty_dest,omitempty"` // 目的地原本是空資料夾（搬之前先刪掉，退回時建回來）
	Status    string    `json:"status"`               // pending、moving、done、reverted
	At        time.Time `json:"at"`
}

// officialCard 是官方角色卡（含它的 .bak-* 備份）：<代號>.md，代號是小寫英文開頭、小寫英數。
var officialCard = regexp.MustCompile(`^[a-z][a-z0-9]*\.md(\.bak-.+)?$`)

// relayoutRename 是搬一項的方法；測試換掉它來模擬中途失敗。
var relayoutRename = os.Rename

// RelayoutRecordPath 是搬家紀錄的位置。
func RelayoutRecordPath(home string) string {
	return filepath.Join(home, "settings", "relayout.json")
}

// ResolveStateDir 檢查並展開 --state：要絕對路徑或 ~ 開頭；空的＝DefaultStateDir(home)。
func ResolveStateDir(home, state string) (string, error) {
	if strings.TrimSpace(state) == "" {
		return DefaultStateDir(home), nil
	}
	s := ExpandHome(state)
	if !filepath.IsAbs(s) {
		return "", Invalid("state", "狀態資料夾要寫絕對路徑或 ~ 開頭："+state)
	}
	return filepath.Clean(s), nil
}

// PlanRelayout 算出第 1 版→第 2 版要搬哪些，並做完全部預檢（不改任何東西）。
func PlanRelayout(home, state string) (*RelayoutPlan, error) {
	return PlanRelayoutWith(home, state, true)
}

// PlanRelayoutWith 同 PlanRelayout；movePersonas = false 時官方角色一律留在家裡（指令的預設）：
// 角色包網址還沒設的家，官方角色（含主人自己做的圖、隱藏版）沒辦法重新下載，搬去狀態資料夾就備份不到。
// 發行角色包的家不管怎麼設都留在家裡。
func PlanRelayoutWith(home, state string, movePersonas bool) (*RelayoutPlan, error) {
	if !filepath.IsAbs(home) {
		return nil, Invalid("home", "家要是絕對路徑："+home)
	}
	home = filepath.Clean(home)
	s, err := ResolveStateDir(home, state)
	if err != nil {
		return nil, err
	}
	p := &RelayoutPlan{Home: home, State: s}
	if cfg, err := readRelayoutTOML(home); err == nil {
		p.PersonasAtHome = cfg.publishes
		p.Publishes = cfg.publishes
	}
	if !movePersonas {
		p.PersonasAtHome = true
	}
	add := func(from, to, kind string) {
		if _, err := os.Lstat(from); err != nil {
			return // 不存在就跳過
		}
		p.Moves = append(p.Moves, RelayoutItem{From: from, To: to, Size: diskSize(from), Kind: kind})
	}
	add(filepath.Join(home, "runtime", "share"), filepath.Join(home, "media", "share"), RelayoutMedia)
	add(filepath.Join(home, "runtime", "art"), filepath.Join(home, "media", "art"), RelayoutMedia)
	add(filepath.Join(home, "runtime"), filepath.Join(s, "runtime"), RelayoutRuntime)
	add(filepath.Join(home, "secrets"), filepath.Join(s, "secrets"), RelayoutSecrets)
	add(filepath.Join(home, "packages", "installed"), filepath.Join(s, "packages", "installed"), RelayoutInstalled)
	for _, d := range []string{"avatars", "wardrobe", "catalog"} {
		if !p.PersonasAtHome {
			add(filepath.Join(home, "personas", d), filepath.Join(s, "personas", d), RelayoutPersonas)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(home, "personas")); err == nil {
		for _, e := range entries {
			name := e.Name()
			from := filepath.Join(home, "personas", name)
			switch {
			case p.PersonasAtHome:
				p.Stay = append(p.Stay, RelayoutItem{From: from, Size: diskSize(from), Kind: RelayoutStay})
			case name == "avatars" || name == "wardrobe" || name == "catalog":
			case !e.IsDir() && officialCard.MatchString(name):
				add(from, filepath.Join(s, "personas", name), RelayoutPersonas)
			default:
				p.Stay = append(p.Stay, RelayoutItem{From: from, Size: diskSize(from), Kind: RelayoutStay})
			}
		}
	}
	// runtime 整個搬走前，share、art 已先搬到 media/：大小扣掉，免得重複算。
	for i := range p.Moves {
		if p.Moves[i].Kind == RelayoutRuntime {
			for _, m := range p.Moves {
				if m.Kind == RelayoutMedia {
					p.Moves[i].Size -= m.Size
				}
			}
		}
	}
	p.Rewrites, p.RuntimeRefs = planRewrites(home, s, p.PersonasAtHome, p.Moves)
	p.NotTemp = notTempRuntime(home)
	p.Problems = relayoutPreflight(p)
	return p, nil
}

// relayoutPreflight 回傳不能搬的原因（空的＝可以搬）。
func relayoutPreflight(p *RelayoutPlan) []string {
	var probs []string
	if cfg, err := readRelayoutTOML(p.Home); err != nil {
		probs = append(probs, errMsg(err))
	} else if cfg.hasLayout {
		probs = append(probs, "這個家已經是第 2 版（home.toml 已有 [layout]），不用再搬")
	} else if cfg.schema != 1 {
		probs = append(probs, "home.toml 的 schema 不是 1，不是能搬的第 1 版家")
	}
	if _, err := os.Stat(RelayoutRecordPath(p.Home)); err == nil {
		probs = append(probs, "已經有搬家紀錄 "+RelayoutRecordPath(p.Home)+"（上次搬過還沒退回？要退回請用 --revert）")
	}
	if _, err := os.Lstat(rewritesDir(p.Home)); err == nil {
		probs = append(probs, "已經有 "+rewritesDir(p.Home)+"（上次搬家留下的原文？先確認再刪掉）")
	}
	if inside(p.Home, p.State) {
		probs = append(probs, "狀態資料夾不能放在家裡面："+p.State)
	}
	if msg := serviceRunning(LayoutFor(p.Home)); msg != "" {
		probs = append(probs, msg)
	}
	if msg := pendingRecodeProblem(p.Home); msg != "" {
		probs = append(probs, msg)
	}
	if err := sameDevice(p.Home, p.State); err != nil {
		probs = append(probs, err.Error())
	}
	for _, m := range p.Moves {
		if !destFree(m.To) {
			probs = append(probs, "目的地已經有東西："+m.To)
		}
	}
	return probs
}

type relayoutTOML struct {
	hasLayout bool
	schema    int
	publishes bool // [admin] public_personas 有寫：發行角色包的家
}

// readRelayoutTOML 讀 home.toml 裡搬家要看的欄位：有沒有 [layout]、schema、是不是發行角色包的家。
func readRelayoutTOML(home string) (relayoutTOML, error) {
	data, err := os.ReadFile(filepath.Join(home, "home.toml"))
	if err != nil {
		return relayoutTOML{}, Unavailable("讀不到 " + filepath.Join(home, "home.toml") + "（這裡是家嗎？）")
	}
	doc := struct {
		Schema int          `toml:"schema"`
		Layout LayoutConfig `toml:"layout"`
		Admin  struct {
			PublicPersonas []string `toml:"public_personas"`
		} `toml:"admin"`
	}{Schema: 1}
	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		return relayoutTOML{}, Invalid("home", "home.toml 格式不對，先修好再搬")
	}
	return relayoutTOML{hasLayout: md.IsDefined("layout"), schema: doc.Schema, publishes: len(doc.Admin.PublicPersonas) > 0}, nil
}

// homeLayoutVersion：home.toml 有寫 [layout] 就算第 2 版（不管 version 寫多少，都不再搬）。
func homeLayoutVersion(home string) (int, error) {
	cfg, err := readRelayoutTOML(home)
	if err != nil {
		return 0, err
	}
	if cfg.hasLayout {
		return 2, nil
	}
	return 1, nil
}

// inside：child 是 parent 本身或在它裡面。
func inside(parent, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// destFree：目的地不存在，或是空資料夾。
func destFree(p string) bool {
	st, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil || !st.IsDir() {
		return false
	}
	entries, err := os.ReadDir(p)
	return err == nil && len(entries) == 0
}

// ServiceRunning 是給其他離線工具（evergenie home tidy、personas promote）用的同一個判斷：服務在跑回白話原因，沒在跑回空字串。
func ServiceRunning(home string) string { return serviceRunning(LayoutFor(home)) }

// serviceRunning：這個家的服務在跑就回白話原因（API socket 連得上，或 serve.pid 的行程還活著）。
func serviceRunning(l Layout) string {
	sock := l.Runtime("sockets", "api.sock")
	if c, err := net.DialTimeout("unix", sock, 500*time.Millisecond); err == nil {
		_ = c.Close()
		return "服務還在跑（" + sock + " 連得上），請先停服務（evergenie service stop）再搬"
	}
	if data, err := os.ReadFile(l.Runtime("serve.pid")); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
			if err := syscall.Kill(pid, 0); err == nil || errors.Is(err, syscall.EPERM) {
				return fmt.Sprintf("服務還在跑（serve.pid 的行程 %d 還活著），請先停服務（evergenie service stop）再搬", pid)
			}
		}
	}
	return ""
}

// SameDevice 是 sameDevice 給其他離線工具（evergenie home tidy --move-official）用的版本：不在同一顆磁碟回白話錯誤。
func SameDevice(home, state string) error { return sameDevice(home, state) }

// sameDevice：狀態資料夾（還沒建就看最近一層存在的上層）跟家在同一顆磁碟。
func sameDevice(home, state string) error {
	hd, err := deviceOf(home)
	if err != nil {
		return Unavailable("讀不到家的資料夾：" + home)
	}
	dir := state
	for {
		if _, err := os.Stat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Unavailable("找不到狀態資料夾的上層：" + state)
		}
		dir = parent
	}
	sd, err := deviceOf(dir)
	if err != nil {
		return Unavailable("讀不到狀態資料夾的上層：" + dir)
	}
	if hd != sd {
		return Invalid("state", "狀態資料夾跟家不在同一顆磁碟（"+dir+"），搬家只做同一顆磁碟的改名，請換一個位置")
	}
	return nil
}

func deviceOf(p string) (uint64, error) {
	st, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("拿不到裝置編號")
	}
	return uint64(sys.Dev), nil //nolint:unconvert // 各平台 Dev 的型別不同
}

// diskSize 是檔案大小總和（像 du；不跟著 symlink 走）。
func diskSize(p string) int64 {
	var total int64
	_ = filepath.WalkDir(p, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil && !d.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// HumanSize 把位元組數寫成好讀的樣子（1.2 MB）。
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ApplyRelayout 照預演真的搬。預檢有問題就什麼都不做；中途失敗自動退回已搬的並拿掉紀錄。
func ApplyRelayout(p *RelayoutPlan, now time.Time) (err error) {
	if len(p.Problems) > 0 {
		return Conflict("不能搬：" + strings.Join(p.Problems, "；"))
	}
	// 再做一次預檢：預演到真的搬之間，情況可能變了。
	if probs := relayoutPreflight(p); len(probs) > 0 {
		return Conflict("不能搬：" + strings.Join(probs, "；"))
	}
	tomlPath := filepath.Join(p.Home, "home.toml")
	orig, err := os.ReadFile(tomlPath)
	if err != nil {
		return Unavailable("讀不到 " + tomlPath)
	}
	st, err := os.Stat(tomlPath)
	if err != nil {
		return Unavailable("讀不到 " + tomlPath)
	}
	backup := backupName(tomlPath, now, "relayout")
	if err := AtomicWrite(backup, orig, st.Mode().Perm()); err != nil {
		return Unavailable("備份 home.toml 失敗：" + err.Error())
	}
	p.Backup = backup
	rec := &relayoutRecord{Version: 1, Home: p.Home, State: p.State, StartedAt: now, Backup: backup, PersonasAtHome: p.PersonasAtHome}
	for _, m := range p.Moves {
		rec.Steps = append(rec.Steps, relayoutStep{From: m.From, To: m.To, Kind: m.Kind, Status: "pending", At: now})
	}
	recPath := RelayoutRecordPath(p.Home)
	if err := os.MkdirAll(filepath.Dir(recPath), 0o755); err != nil {
		return Unavailable("建不了 " + filepath.Dir(recPath))
	}
	if err := saveRecord(recPath, rec); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if rerr := undoSteps(rec, recPath); rerr != nil {
				err = Unavailable(errMsg(err) + "；自動退回也失敗了：" + rerr.Error() + "（紀錄留在 " + recPath + "，可以再用 --revert 試）")
				return
			}
			_ = os.Remove(recPath)
		}
	}()
	for i := range rec.Steps {
		if err := moveStep(rec, i, recPath); err != nil {
			return err
		}
	}
	if err := applyRewrites(rec, p.Rewrites, recPath); err != nil {
		return err
	}
	appended := layoutTable(orig, p.State, p.PersonasAtHome)
	withSchema, schemaLine := setSchema2(orig)
	// 先把要加的原文記進紀錄再改 home.toml：中間當掉時，退回照原文還原（schema 那行改回原樣，不是整行刪掉）。
	rec.Appended, rec.SchemaLine = appended, schemaLine
	if err := saveRecord(recPath, rec); err != nil {
		return err
	}
	if err := AtomicWrite(tomlPath, append(withSchema, appended...), st.Mode().Perm()); err != nil {
		return Unavailable("寫不了 home.toml：" + err.Error())
	}
	t := time.Now()
	rec.FinishedAt = &t
	if err := saveRecord(recPath, rec); err != nil {
		// home.toml 已改好：退回時先把 [layout] 拿掉。
		_ = AtomicWrite(tomlPath, orig, st.Mode().Perm())
		return err
	}
	return nil
}

// moveStep 搬第 i 項：建好目的地的上層（記下新建的資料夾）、空資料夾目的地先刪、rename、更新紀錄。
func moveStep(rec *relayoutRecord, i int, recPath string) error {
	s := &rec.Steps[i]
	created, err := mkdirAllTracked(filepath.Dir(s.To))
	rec.Created = append(rec.Created, created...)
	if err != nil {
		_ = saveRecord(recPath, rec)
		return Unavailable("建不了 " + filepath.Dir(s.To) + "：" + err.Error())
	}
	if st, err := os.Lstat(s.To); err == nil {
		if !st.IsDir() || os.Remove(s.To) != nil { // 只有空資料夾刪得掉
			return Conflict("目的地已經有東西：" + s.To)
		}
		s.EmptyDest = true
	}
	s.Status, s.At = "moving", time.Now()
	if err := saveRecord(recPath, rec); err != nil {
		return err
	}
	if err := relayoutRename(s.From, s.To); err != nil {
		if s.EmptyDest {
			_ = os.Mkdir(s.To, 0o755)
			s.EmptyDest = false
		}
		s.Status = "pending"
		_ = saveRecord(recPath, rec)
		if errors.Is(err, syscall.EXDEV) {
			return Invalid("state", "跨磁碟了（"+s.From+" → "+s.To+"），搬家只做同一顆磁碟的改名")
		}
		return Unavailable("搬不了 " + s.From + " → " + s.To + "：" + err.Error())
	}
	s.Status, s.At = "done", time.Now()
	return saveRecord(recPath, rec)
}

// undoSteps 把已搬（done）的反向搬回，再刪掉搬家時新建、現在空了的資料夾。
func undoSteps(rec *relayoutRecord, recPath string) error {
	var errs []string
	if err := revertRewrites(rec, recPath); err != nil {
		errs = append(errs, err.Error())
	} else {
		_ = os.RemoveAll(rewritesDir(rec.Home))
	}
	for i := len(rec.Steps) - 1; i >= 0; i-- {
		s := &rec.Steps[i]
		need, problem := stepNeedsRevert(*s)
		if problem != "" {
			errs = append(errs, problem)
			continue
		}
		if !need {
			if s.Status == "moving" {
				s.Status = "reverted"
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(s.From), 0o755); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if err := relayoutRename(s.To, s.From); err != nil {
			errs = append(errs, s.To+" → "+s.From+"："+err.Error())
			continue
		}
		if s.EmptyDest {
			_ = os.Mkdir(s.To, 0o755)
		}
		s.Status, s.At = "reverted", time.Now()
		_ = saveRecord(recPath, rec)
	}
	removeCreated(rec.Created)
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "；"))
	}
	return nil
}

// removeCreated 由深到淺刪掉新建的資料夾（不是空的就留著）。
func removeCreated(dirs []string) {
	sorted := append([]string{}, dirs...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, d := range sorted {
		_ = os.Remove(d)
	}
}

// mkdirAllTracked 像 os.MkdirAll，回傳這次新建的資料夾（由淺到深）。
func mkdirAllTracked(dir string) ([]string, error) {
	var missing []string
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			break
		}
		missing = append(missing, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	var created []string
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return created, err
		}
		created = append(created, missing[i])
	}
	return created, nil
}

// layoutMarker 是搬家時加在 [layout] 前面那行註解的開頭，退回時認它一起拿掉。
const layoutMarker = "# 家的佈局第 2 版"

// layoutTable 是附加在 home.toml 尾巴的 [layout] 表（原檔沒有換行結尾就先補一個）。
func layoutTable(orig []byte, state string, personasAtHome bool) string {
	var b strings.Builder
	if len(orig) > 0 && orig[len(orig)-1] != '\n' {
		b.WriteString("\n")
	}
	b.WriteString("\n" + layoutMarker + "（evergenie home relayout 加的；退回用 evergenie home relayout --revert）\n")
	b.WriteString("[layout]\nversion = 2\nstate = " + tomlString(state) + "\n")
	if personasAtHome {
		b.WriteString("official_personas = \"home\" # 官方角色留在家裡（主人的原稿或沒辦法重新下載），跟家一起備份\n")
	}
	return b.String()
}

var (
	tableHeader = regexp.MustCompile(`^\s*\[`)
	schemaLine  = regexp.MustCompile(`^\s*schema\s*=\s*(\d+)\s*(#.*)?$`)
)

// setSchema2 把頂層（第一個表之前）的 schema 那一行改成 schema = 2；沒有那行就加在檔頭。
// 回傳新內容與原本那一行（空的＝原本沒寫）。第 2 版的家寫 schema = 2，舊程式只收 1，會拒絕啟動，不會把家當第 1 版亂寫。
func setSchema2(data []byte) ([]byte, string) {
	lines := strings.SplitAfter(string(data), "\n")
	for i, ln := range lines {
		body := strings.TrimRight(ln, "\r\n")
		if tableHeader.MatchString(body) {
			break
		}
		if schemaLine.MatchString(body) {
			lines[i] = "schema = 2" + ln[len(body):]
			return []byte(strings.Join(lines, "")), body
		}
	}
	return append([]byte("schema = 2\n"), data...), ""
}

// restoreSchema 把 setSchema2 改的那一行改回原文（原本沒寫就拿掉加在檔頭的那行）；沒有頂層的 schema = 2 就不動。
func restoreSchema(data []byte, orig string) []byte {
	lines := strings.SplitAfter(string(data), "\n")
	for i, ln := range lines {
		body := strings.TrimRight(ln, "\r\n")
		if tableHeader.MatchString(body) {
			break
		}
		if m := schemaLine.FindStringSubmatch(body); m != nil && m[1] == "2" {
			if orig == "" {
				lines[i] = ""
			} else {
				lines[i] = orig + ln[len(body):]
			}
			return []byte(strings.Join(lines, ""))
		}
	}
	return data
}

// backupName 是 <檔>.bak-<YYYYMMDD>-<原因>；同一天已有就加 -HHMM，再撞就加序號（備份規則）。
func backupName(path string, now time.Time, reason string) string {
	p := path + ".bak-" + now.Format("20060102") + "-" + reason
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	return uniquePath(path + ".bak-" + now.Format("20060102-1504") + "-" + reason)
}

// stepNeedsRevert 看實際的位置判斷一步要不要搬回（紀錄可能停在 moving：rename 做了沒有要看檔案在哪）。
// 回 (要搬回, 問題)；pending、reverted 不用搬回。
func stepNeedsRevert(s relayoutStep) (bool, string) {
	if s.Status != "done" && s.Status != "moving" {
		return false, ""
	}
	_, toErr := os.Lstat(s.To)
	_, fromErr := os.Lstat(s.From)
	toOK, fromOK := toErr == nil, fromErr == nil
	switch {
	case toOK && !fromOK:
		return true, ""
	case fromOK && (!toOK || (s.EmptyDest && destFree(s.To))):
		return false, "" // 還沒搬（或已搬回）
	case fromOK && toOK:
		return false, "兩邊都有東西，不知道哪個是對的：" + s.From + "、" + s.To
	default:
		return false, "兩邊都找不到：" + s.From + "、" + s.To
	}
}

// tomlString 是 TOML 的基本字串（雙引號，跳脫反斜線、雙引號與控制字元）。
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04X", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// removeLayoutTable 從 home.toml 拿掉 [layout] 表：尾巴是當初附加的原文就整段拿掉；
// 否則只拿掉 [layout] 那一行到下一個表之前的幾行，其他不動。
func removeLayoutTable(data []byte, appended string) []byte {
	if appended != "" && strings.HasSuffix(string(data), appended) {
		return []byte(strings.TrimSuffix(string(data), appended))
	}
	header := regexp.MustCompile(`^\s*\[\s*layout\s*\]\s*(#.*)?$`)
	anyTable := regexp.MustCompile(`^\s*\[`)
	lines := strings.SplitAfter(string(data), "\n")
	var out []string
	skipping := false
	for _, ln := range lines {
		trimmed := strings.TrimRight(ln, "\r\n")
		switch {
		case header.MatchString(trimmed):
			skipping = true
			// 搬家時加在 [layout] 前面的註解與空行一起拿掉（使用者之後在檔尾又加了東西，尾巴對不上時走到這裡）。
			if n := len(out); n > 0 && strings.HasPrefix(strings.TrimSpace(out[n-1]), layoutMarker) {
				out = out[:n-1]
				if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) == "" {
					out = out[:n-1]
				}
			}
			continue
		case skipping && anyTable.MatchString(trimmed):
			skipping = false
		}
		if !skipping {
			out = append(out, ln)
		}
	}
	return []byte(strings.Join(out, ""))
}

// RevertPlan 是 --revert 要做的：照紀錄反向搬回的項目（從第 2 版的位置回到第 1 版）。
type RevertPlan struct {
	Home     string
	State    string
	Moves    []RelayoutItem    // From＝現在的位置（第 2 版），To＝要搬回的位置（第 1 版）
	Rewrites []RelayoutRewrite // 要還原路徑的文字檔
	Problems []string
}

// PlanRevert 讀搬家紀錄，算出要搬回哪些並做預檢（不改任何東西）。
// 每一步看 From、To 實際在哪判斷（紀錄停在 moving 的也算），不只看紀錄的狀態。
func PlanRevert(home string) (*RevertPlan, error) {
	home = filepath.Clean(home)
	rec, err := loadRecord(RelayoutRecordPath(home))
	if err != nil {
		return nil, err
	}
	p := &RevertPlan{Home: home, State: rec.State}
	for i := len(rec.Steps) - 1; i >= 0; i-- {
		s := rec.Steps[i]
		need, problem := stepNeedsRevert(s)
		if problem != "" {
			p.Problems = append(p.Problems, problem)
		}
		if need {
			p.Moves = append(p.Moves, RelayoutItem{From: s.To, To: s.From, Size: diskSize(s.To), Kind: s.Kind})
		}
	}
	for _, r := range rec.Rewrites {
		if r.Status == "done" || r.Status == "pending" {
			p.Rewrites = append(p.Rewrites, RelayoutRewrite{Path: r.Path, Count: r.Count})
		}
	}
	if msg := serviceRunning(LayoutFor(home)); msg != "" {
		p.Problems = append(p.Problems, msg)
	}
	if msg := pendingRecodeProblem(home); msg != "" {
		p.Problems = append(p.Problems, msg)
	}
	return p, nil
}

// RevertRelayout 照紀錄搬回第 1 版：先把資料夾全部反向 rename；全部成功才改回 home.toml（拿掉 [layout]、schema 改回）、
// 還原改寫過路徑的文字檔，最後紀錄改名成 .bak-<日期>-reverted。
// 搬回途中失敗：已搬回的再搬去第 2 版的位置，home.toml 與文字檔都不動，家維持第 2 版。
func RevertRelayout(home string, now time.Time) (err error) {
	home = filepath.Clean(home)
	p, err := PlanRevert(home)
	if err != nil {
		return err
	}
	if len(p.Problems) > 0 {
		return Conflict("不能退回：" + strings.Join(p.Problems, "；"))
	}
	recPath := RelayoutRecordPath(home)
	rec, err := loadRecord(recPath)
	if err != nil {
		return err
	}
	var undone []int // 這次搬回的步驟
	moving := true
	defer func() {
		if err == nil || !moving {
			return
		}
		for j := len(undone) - 1; j >= 0; j-- {
			s := &rec.Steps[undone[j]]
			if rerr := relayoutRename(s.From, s.To); rerr == nil {
				s.Status = "done"
			}
		}
		_ = saveRecord(recPath, rec)
	}()
	for i := len(rec.Steps) - 1; i >= 0; i-- {
		s := &rec.Steps[i]
		need, problem := stepNeedsRevert(*s)
		if problem != "" {
			// 搬回前面幾步後才冒出來的衝突（例：第 2 版期間狀態資料夾的 runtime/ 又長出 art/）：停下，已搬回的會再搬去第 2 版。
			return Conflict(problem + "（家維持第 2 版，沒有搬回）")
		}
		if !need {
			if s.Status == "done" || s.Status == "moving" {
				s.Status = "reverted"
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(s.From), 0o755); err != nil {
			return Unavailable("建不了 " + filepath.Dir(s.From) + "：" + err.Error())
		}
		if err := relayoutRename(s.To, s.From); err != nil {
			return Unavailable("搬不回 " + s.To + " → " + s.From + "：" + err.Error() + "（已搬回的會再搬去第 2 版的位置，家維持第 2 版）")
		}
		if s.EmptyDest {
			_ = os.Mkdir(s.To, 0o755)
		}
		s.Status, s.At = "reverted", time.Now()
		undone = append(undone, i)
		if err := saveRecord(recPath, rec); err != nil {
			return err
		}
	}
	moving = false // 資料夾都搬回了：之後失敗不再搬去第 2 版，照訊息處理後再跑一次 --revert
	tomlPath := filepath.Join(home, "home.toml")
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		return Unavailable("讀不到 " + tomlPath)
	}
	st, err := os.Stat(tomlPath)
	if err != nil {
		return Unavailable("讀不到 " + tomlPath)
	}
	if err := AtomicWrite(tomlPath, restoreSchema(removeLayoutTable(data, rec.Appended), rec.SchemaLine), st.Mode().Perm()); err != nil {
		return Unavailable("資料夾都搬回了，但寫不了 home.toml：" + err.Error() + "（修好後再跑一次 --revert）")
	}
	if err := revertRewrites(rec, recPath); err != nil {
		return Unavailable("家已退回第 1 版，但有些改寫過路徑的檔還原失敗：" + err.Error() + "（原文在 " + rewritesDir(home) + "，修好後再跑一次 --revert 會重試）")
	}
	removeCreated(rec.Created)
	if len(rec.Rewrites) > 0 {
		if err := os.Rename(rewritesDir(home), uniquePath(rewritesDir(home)+".bak-"+now.Format("20060102")+"-reverted")); err != nil && !os.IsNotExist(err) {
			return Unavailable("搬原文資料夾失敗（家已退回第 1 版）：" + err.Error())
		}
	}
	if err := os.Rename(recPath, uniquePath(recPath+".bak-"+now.Format("20060102")+"-reverted")); err != nil {
		return Unavailable("搬紀錄檔失敗（家已退回第 1 版）：" + err.Error())
	}
	return nil
}

// uniquePath：同名已存在（同一天退回兩次）就在後面加序號。
func uniquePath(p string) string {
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	for i := 2; ; i++ {
		c := p + "-" + strconv.Itoa(i)
		if _, err := os.Lstat(c); err != nil {
			return c
		}
	}
}

func saveRecord(path string, rec *relayoutRecord) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return Unavailable("寫不了搬家紀錄")
	}
	if err := AtomicWrite(path, append(data, '\n'), 0o644); err != nil {
		return Unavailable("寫不了搬家紀錄 " + path + "：" + err.Error())
	}
	return nil
}

func loadRecord(path string) (*relayoutRecord, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, NotFound("找不到搬家紀錄 " + path + "（這個家沒用 evergenie home relayout 搬過，沒辦法自動退回）")
	}
	if err != nil {
		return nil, Unavailable("讀不了搬家紀錄 " + path)
	}
	var rec relayoutRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, Invalid("relayout", "搬家紀錄格式壞了："+path)
	}
	return &rec, nil
}

func errMsg(err error) string {
	var pe *Err
	if errors.As(err, &pe) && pe.Message != "" {
		return pe.Message
	}
	return err.Error()
}
