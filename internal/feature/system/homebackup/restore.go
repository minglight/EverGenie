package homebackup

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 還原（HBK-5、HBK-6）分兩段，中間隔一次服務重開：
//
//  1. Stage（服務開著做）：把備份解到 <家>/.restore/<代號>/new/、驗過檢查碼、改寫綁機器的設定、確認解出來的家設定讀得懂，
//     再留一份現況（before-restore），最後寫 <家>/.restore/pending.json。到這裡家裡一個檔都沒動。
//  2. ApplyPending（服務剛啟動、還沒開任何模組時做）：一類一類用改名換上去（現在的 → old/，new/ 的 → 家裡），
//     換完跑一次家的檢查（設定讀得懂、資料整理跑得完）。任何一步失敗，就把已經換的全部改名換回去：家跟還原前一模一樣。
//     換到一半被關掉（斷電）：下次啟動看到 pending.json 寫著「正在換」，一樣全部換回去。
//
// 放在家裡的 .restore/ 是為了跟家在同一顆硬碟（改名才是瞬間的）。

const (
	restoreDirName = ".restore"
	pendingName    = "pending.json"
	phaseReady     = "ready"
	phaseSwapping  = "swapping"
	phaseDone      = "done"
)

// Unit 是還原時整個換掉的一塊（一個檔或一個資料夾）。
type Unit struct {
	Name    string `json:"name"`     // 白話用不到，日誌用：home.toml、vault…
	Live    string `json:"live"`     // 家裡的位置
	New     string `json:"new"`      // 備份解出來的位置
	Old     string `json:"old"`      // 換下來的放哪
	HasNew  bool   `json:"has_new"`  // 備份裡有這一塊
	HadLive bool   `json:"had_live"` // 換之前家裡有這一塊（開始換的時候才填）
}

// Pending 是「等重開時換上去」的一份還原。
type Pending struct {
	Token            string    `json:"token"`
	Phase            string    `json:"phase"`
	StagedAt         time.Time `json:"staged_at"`
	BackupCreatedAt  time.Time `json:"backup_created_at"`
	BackupHome       string    `json:"backup_home,omitempty"`
	Safety           string    `json:"safety,omitempty"`
	FromOtherMachine bool      `json:"from_other_machine,omitempty"`
	NewHome          bool      `json:"new_home,omitempty"`
	Installed        []string  `json:"installed,omitempty"`
	Units            []Unit    `json:"units"`
	// SecretsFile 是這個家的秘密檔；SecretsNew 是備份解出來的推播金鑰（沒有＝空）。SecretsBefore 是換之前的內容存在哪（沒有檔＝空）。
	SecretsFile    string `json:"secrets_file,omitempty"`
	SecretsNew     string `json:"secrets_new,omitempty"`
	SecretsBefore  string `json:"secrets_before,omitempty"`
	SecretsExisted bool   `json:"secrets_existed,omitempty"`
	// LostLinks 是準備還原時家裡要換掉的那幾塊裡現有的捷徑（符號連結）：換完就不見了，結果裡請主人自己重建（HBK-11）。
	LostLinks []string `json:"lost_links,omitempty"`
	// ServiceLock 是這個家的服務跑著時一直拿著的鎖檔（platform.ServiceLockPath）；換上去之前要拿得到它（HBK-12）。
	// 記在這裡是因為換到一半時家的設定檔可能不在原位，那時算不出位置。
	ServiceLock string `json:"service_lock,omitempty"`
}

func restoreDir(home string) string  { return filepath.Join(home, restoreDirName) }
func pendingPath(home string) string { return filepath.Join(home, restoreDirName, pendingName) }

// HasPending：有一份還原在等重開。
func HasPending(home string) bool {
	_, err := os.Stat(pendingPath(home))
	return err == nil
}

func loadPending(home string) (*Pending, error) {
	data, err := os.ReadFile(pendingPath(home))
	if err != nil {
		return nil, err
	}
	p := &Pending{}
	if err := json.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return p, nil
}

func savePending(home string, p *Pending) error {
	data, err := json.MarshalIndent(p, "", " ")
	if err != nil {
		return err
	}
	return platform.AtomicWrite(pendingPath(home), data, 0o600)
}

// StageOptions：準備一份還原。
type StageOptions struct {
	Home    string
	Archive string
	Now     time.Time
	// NewHome：這是剛建好的空家（從備份建家）：不用留現況。
	NewHome bool
	// KnownMigrations 是現在的程式認得的資料整理（核心的）；備份裡有不認得的核心整理＝備份比程式新，不還原。nil＝不檢查。
	KnownMigrations []string
	// Inspected 是呼叫的人剛用 Inspect 讀過這個檔拿到的說明檔（省一次整份重讀）；nil＝這裡自己讀。
	Inspected *Manifest
}

// Problem 回「這份備份現在不能還原到這個程式」的白話原因；空字串＝可以。
func Problem(m *Manifest, known []string) string {
	if m.Format > FormatVersion {
		return "這份備份是比較新的程式做的，這台的程式讀不懂；先把程式更新到最新再還原"
	}
	if known == nil {
		return ""
	}
	have := map[string]bool{}
	for _, id := range known {
		have[id] = true
	}
	for _, id := range m.Migrations {
		if !strings.HasPrefix(id, "pkg.") && !have[id] {
			return "這份備份是比較新的程式做的，這台的程式讀不懂；先把程式更新到最新再還原"
		}
	}
	return ""
}

// planUnits 列出這個家要整個換掉的幾塊（照這個家的佈局與解出來的備份）。work 是這次還原的工作資料夾。
// 會動到解出來的那份（把自己的角色從 personas/ 分出來），不動家。
func planUnits(home string, l platform.Layout, work string) ([]Unit, error) {
	n := filepath.Join(work, "new", "home")
	o := filepath.Join(work, "old")
	out := []Unit{
		{Name: "home.toml", Live: filepath.Join(home, "home.toml"), New: filepath.Join(n, "home.toml"), Old: filepath.Join(o, "home.toml")},
		{Name: "settings", Live: filepath.Join(home, "settings"), New: filepath.Join(n, "settings"), Old: filepath.Join(o, "settings")},
		{Name: "vault", Live: filepath.Join(home, "vault"), New: filepath.Join(n, "vault"), Old: filepath.Join(o, "vault")},
	}
	// 角色：備份只有自己的角色（personas/mine）就只換那一塊，這個家的官方角色不動。
	// 備份帶著官方角色（做備份的家把官方角色放在家裡）：這個家的官方角色也在家裡就整個 personas/ 一起換；
	// 在狀態資料夾就分成兩塊，各換到各的位置。
	staged := filepath.Join(n, "personas")
	mine := Unit{Name: "personas/mine", Live: filepath.Join(home, "personas", "mine"), New: filepath.Join(staged, "mine"), Old: filepath.Join(o, "personas-mine")}
	official := filepath.Clean(l.OfficialPersonas())
	switch {
	case !hasOtherThan(staged, "mine"):
		out = append(out, mine)
	case official == filepath.Join(home, "personas"):
		out = append(out, Unit{Name: "personas", Live: official, New: staged, Old: filepath.Join(o, "personas")})
	default:
		if exists(mine.New) {
			split := filepath.Join(n, "personas-mine")
			if err := os.Rename(mine.New, split); err != nil {
				return nil, err
			}
			mine.New = split
		}
		out = append(out, mine, Unit{Name: "personas（官方）", Live: official, New: staged, Old: filepath.Join(o, "personas-official")})
	}
	out = append(out, Unit{Name: "packages/mine", Live: filepath.Join(home, "packages", "mine"), New: filepath.Join(n, "packages", "mine"), Old: filepath.Join(o, "packages-mine")})
	if l.V2 {
		out = append(out, Unit{Name: "media", Live: l.Media(), New: filepath.Join(n, "media"), Old: filepath.Join(o, "media")})
	} else {
		out = append(out,
			Unit{Name: "media/share", Live: l.Media("share"), New: filepath.Join(n, "media", "share"), Old: filepath.Join(o, "media-share")},
			Unit{Name: "media/art", Live: l.Media("art"), New: filepath.Join(n, "media", "art"), Old: filepath.Join(o, "media-art")})
	}
	// 家資料夾裡其他的東西：備份有的、這個家現在有的（第 2 版佈局才算）都是一塊，跟其他類別一樣整個換。
	names := map[string]bool{}
	for _, name := range dirNames(filepath.Join(n, "other")) {
		names[name] = true
	}
	if entries, err := os.ReadDir(filepath.Join(n, "other")); err == nil {
		for _, e := range entries {
			names[e.Name()] = true
		}
	}
	if l.V2 {
		live, _ := otherNames(home)
		for _, name := range live {
			names[name] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		if !knownTop[name] {
			sorted = append(sorted, name)
		}
	}
	sort.Strings(sorted)
	for _, name := range sorted {
		out = append(out, Unit{Name: "other/" + name, Live: filepath.Join(home, name), New: filepath.Join(n, "other", name), Old: filepath.Join(o, "other", name)})
	}
	return out, nil
}

// hasOtherThan：資料夾裡有沒有 name 以外的東西（資料夾不在算沒有）。
func hasOtherThan(dir, name string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Name() != name {
			return true
		}
	}
	return false
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Stage 準備一份還原（見檔頭）；成功後家裡多了 .restore/pending.json，等 ApplyPending。失敗時不留東西，家沒有動。
func Stage(o StageOptions) (*Pending, error) {
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	home := filepath.Clean(o.Home)
	if HasPending(home) {
		return nil, platform.Conflict("已經有一份還原在等服務重開；等它做完再試")
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return nil, platform.Conflict("這個家現在的設定檔（home.toml）讀不懂，沒辦法自動還原；跟任何一位秘書說「家的設定檔讀不懂，幫我看」")
	}
	layout := h.Paths()
	// 先看空間（HBK-10）：解開要多大從說明檔讀（先整份讀一遍、順便驗檢查碼），加上還原前要留的那份現況；不夠就不開始。
	pre := o.Inspected
	if pre == nil {
		if pre, err = Inspect(o.Archive); err != nil {
			return nil, err
		}
	}
	if why := Problem(pre, o.KnownMigrations); why != "" {
		return nil, platform.Conflict(why)
	}
	if err := stageSpace(home, pre.TotalBytes(), o.NewHome); err != nil {
		return nil, err
	}
	token, err := platform.NewPersonaTransferToken(o.Now)
	if err != nil {
		return nil, platform.Internal("準備還原時出錯")
	}
	work := filepath.Join(restoreDir(home), token)
	if err := os.MkdirAll(work, 0o700); err != nil {
		return nil, platform.Unavailable("家裡建不了還原用的暫存資料夾")
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(work) // 沒準備好就收掉；收不掉下次清
			removeIfEmpty(restoreDir(home))
		}
	}()
	m, err := extract(o.Archive, filepath.Join(work, "new"))
	if err != nil {
		return nil, err
	}
	if why := Problem(m, o.KnownMigrations); why != "" {
		return nil, platform.Conflict(why)
	}
	list, err := planUnits(home, layout, work)
	if err != nil {
		return nil, platform.Unavailable("整理解出來的備份時出錯，沒有還原（你的家沒有動）")
	}
	for i := range list {
		list[i].HasNew = exists(list[i].New)
		if info, err := os.Lstat(list[i].Live); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return nil, platform.Conflict("這個家的「" + list[i].Name + "」是捷徑（符號連結），自動還原會把它換掉，所以沒有還原；把它換成真的資料夾再試，或跟任何一位秘書說")
		}
	}
	if !list[0].HasNew {
		return nil, platform.Invalid("backup", "這份備份裡沒有家的設定，不能還原")
	}
	// 綁機器的設定改成這個家的。
	userDir, _ := os.UserHomeDir() // 拿不到就不改使用者資料夾那一段
	paths := rebindPaths{OldHome: m.HomeDir, NewHome: home, OldUser: m.UserDir, NewUser: userDir}
	backupToml, err := os.ReadFile(list[0].New)
	if err != nil {
		return nil, platform.Unavailable("讀不了解出來的家設定")
	}
	liveToml, err := os.ReadFile(list[0].Live)
	if err != nil {
		return nil, platform.Unavailable("讀不了這個家現在的設定檔")
	}
	if exists(filepath.Join(work, "new", "secrets", "push.toml")) {
		paths.DefaultSecrets = "secrets/push.toml"
	}
	rebound, _, err := rebindHomeToml(backupToml, liveToml, paths)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(list[0].New, rebound, 0o644); err != nil {
		return nil, platform.Unavailable("寫不了還原用的家設定")
	}
	if _, err := platform.LoadHome(filepath.Join(work, "new", "home")); err != nil {
		return nil, platform.Invalid("backup", "備份裡的家設定在這台電腦上讀不懂，不能還原（沒有動到你的家）")
	}
	p := &Pending{
		Token: token, Phase: phaseReady, StagedAt: o.Now.UTC(), BackupCreatedAt: m.CreatedAt, BackupHome: m.HomeName,
		NewHome: o.NewHome, Installed: m.Installed, Units: list,
		ServiceLock:      platform.ServiceLockPath(home),
		FromOtherMachine: o.NewHome || filepath.Clean(m.HomeDir) != home || (m.Machine != "" && m.Machine != h.Runtime.Machine),
	}
	// 秘密檔的位置照換上去之後的家設定算（這個家本來沒設的話，上面補的是 secrets/push.toml）。
	if staged, err := platform.LoadHome(filepath.Join(work, "new", "home")); err == nil && staged.Secrets.File != "" {
		staged.Dir = home
		p.SecretsFile = staged.SecretsPath()
	}
	if p.SecretsFile == "" {
		p.SecretsFile = layout.Secrets("push.toml")
	}
	if s := filepath.Join(work, "new", "secrets", "push.toml"); exists(s) {
		p.SecretsNew = s
	}
	p.SecretsBefore = filepath.Join(work, "old", "secrets-before.toml")
	if !o.NewHome {
		p.LostLinks = LostLinks(home)
		safety, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindBeforeRestore, Now: o.Now})
		if err != nil {
			return nil, platform.Unavailable("還原前要先留一份現在的樣子，但沒留成，所以沒有還原（你的家沒有動）")
		}
		p.Safety = safety.Name
	}
	if err := savePending(home, p); err != nil {
		return nil, platform.Unavailable("還原準備到最後一步寫不進去，沒有還原（你的家沒有動）")
	}
	ok = true
	return p, nil
}

// stageSpace：準備還原要的空間夠不夠。解開的備份放在家的那顆磁碟（.restore/），還原前留的現況放在備份的資料夾；
// 兩個在同一顆磁碟就加起來算。
func stageSpace(home string, unpacked int64, newHome bool) error {
	var safety int64
	safetyDir := platform.HomeBackupDir(home, sdk.HomeBackupKindBeforeRestore)
	if !newHome {
		safety = estimateHome(home)
	}
	short := func(need, free int64) error {
		text := "這台電腦剩下的空間不夠還原：解開這份備份"
		if safety > 0 {
			text += "、再留一份現在的樣子"
		}
		return platform.Unavailable(text + "大約要 " + sizeText(need) + "，只剩 " + sizeText(free) + "。清出一些空間再試一次（你的家沒有動）")
	}
	if sameVolume(home, safetyDir) {
		if free, ok := enoughSpace(home, unpacked+safety); !ok {
			return short(unpacked+safety, free)
		}
		return nil
	}
	if free, ok := enoughSpace(home, unpacked); !ok {
		return short(unpacked, free)
	}
	if free, ok := enoughSpace(safetyDir, safety); !ok {
		return short(safety, free)
	}
	return nil
}

func removeIfEmpty(dir string) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir) // 空的才刪；刪不掉無妨
	}
}

// ApplyOptions：把準備好的還原換上去。
type ApplyOptions struct {
	Home string
	Now  time.Time
	// Verify 在換上去之後跑（家的資料整理、讀設定）；回錯＝這份備份在這台跑不起來，全部換回去。nil＝只檢查家設定讀得懂。
	Verify func(home string) error
	// Rename 是測試用的接縫（nil＝os.Rename）。
	Rename func(from, to string) error
}

// ApplyPending 把等著的還原換上去（見檔頭）。沒有等著的還原回 (nil, nil)。回的結果已經寫進 last-restore.json；
// 結果的 OK=false 表示沒換成、家維持原樣。只有連「換回去」都做不到才回 error（這時 pending.json 留著，下次啟動再換回去）。
func ApplyPending(o ApplyOptions) (*sdk.HomeRestoreResult, error) {
	home := filepath.Clean(o.Home)
	if !exists(restoreDir(home)) {
		return nil, nil // 沒有還原在等，也沒有留下的暫存
	}
	// 這個家有活著的服務就不能動（HBK-12）：第二個 serve 在服務跑著時被開起來，不能把正在用的家換掉、也不能收掉人家正在準備的暫存。
	unlock, err := lockService(home)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !HasPending(home) {
		// 沒有等著的還原：上次準備到一半被關掉留下的暫存（可能好幾 GB）在這裡收掉。這時候服務還沒開，不會有人正在準備。
		_ = os.RemoveAll(restoreDir(home)) // 收不掉：下次啟動再收
		return nil, nil
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	rename := o.Rename
	if rename == nil {
		rename = os.Rename
	}
	p, err := loadPending(home)
	if err != nil {
		// 讀不懂的 pending：不知道換到哪。暫存裡沒有「換下來的舊東西」＝還沒開始換（Stage 寫它之前家沒動過），當成沒開始、收掉。
		// 有舊東西就不能刪（那是原本的家）：不開服務，留給人處理。
		if hasSwappedOut(home) {
			return nil, platform.Unavailable("還原卡住了：上次還原的紀錄讀不到，而且有東西已經換下來了。請不要動家裡的資料夾，打開「" + platform.RescueName + "」照畫面做")
		}
		_ = os.RemoveAll(restoreDir(home)) // 收不掉下次再收
		return nil, nil
	}
	if p.Phase == phaseDone {
		finish(home, p) // 上次已經做完、結果也寫了，只差沒收乾淨
		return nil, nil
	}
	token := p.Token
	writeResult := func(home string, res *sdk.HomeRestoreResult) {
		markNotified(home, &restoreRecord{HomeRestoreResult: *res, Token: token})
	}
	res := &sdk.HomeRestoreResult{
		At: o.Now.UTC(), BackupCreatedAt: p.BackupCreatedAt, BackupHome: p.BackupHome, Safety: p.Safety, FromOtherMachine: p.FromOtherMachine,
	}
	fail := func(why string) (*sdk.HomeRestoreResult, error) {
		if err := rollback(p, rename); err != nil {
			return nil, platform.Unavailable("還原卡住了：還原沒做成，而且有東西換不回去。請不要動家裡的資料夾，打開「" + platform.RescueName + "」照畫面做")
		}
		finish(home, p)
		res.OK, res.Message = false, why
		writeResult(home, res)
		return res, nil
	}
	if p.Phase == phaseSwapping {
		// 換完、驗過、結果也寫了，只差沒記「做完了」就被關掉：那次還原是成功的，不能退回去又說沒做成。
		// 認法：結果檔記的是這一次（代號一樣）而且是成功。
		if r := readResult(home); r != nil && r.OK && r.Token != "" && r.Token == p.Token {
			finish(home, p)
			return nil, nil
		}
		return fail("上次還原換到一半電腦或服務被關掉了，已經全部換回還原前的樣子；要還原請再做一次")
	}
	for i := range p.Units {
		p.Units[i].HadLive = exists(p.Units[i].Live)
	}
	_, statErr := os.Stat(p.SecretsFile)
	p.SecretsExisted = statErr == nil
	p.Phase = phaseSwapping
	if err := savePending(home, p); err != nil {
		p.Phase = phaseReady
		finish(home, p)
		res.Message = "還原沒做成：硬碟寫不進去。你的家沒有動"
		writeResult(home, res)
		return res, nil
	}
	if err := swap(p, rename); err != nil {
		return fail("還原沒做成：檔案換不上去（硬碟滿了或沒有權限）。已經換回還原前的樣子")
	}
	if err := mergeSecrets(p); err != nil {
		return fail("還原沒做成：推播金鑰寫不進去。已經換回還原前的樣子")
	}
	verify := o.Verify
	if verify == nil {
		verify = func(home string) error { _, err := platform.LoadHome(home); return err }
	}
	if err := verify(home); err != nil {
		return fail("還原沒做成：這份備份在這台電腦上跑不起來。已經換回還原前的樣子")
	}
	res.OK = true
	res.Message = "已經換成 " + p.BackupCreatedAt.Local().Format("2006-01-02 15:04") + " 的備份"
	// 同一台、同一個家還原：帳號、手機連線、別人分享的套件都還在，不用列。換了電腦（或換了位置的家）才列。
	if p.FromOtherMachine {
		res.Todo = Todo(p.Installed)
	}
	if len(p.LostLinks) > 0 {
		res.Todo = append(res.Todo, lostLinksTodo(p.LostLinks))
	}
	writeResult(home, res)
	// 先記「做完了」再收：pending.json 萬一刪不掉，下次啟動看到 done 只會再收一次，不會當成換到一半去換回來。
	p.Phase = phaseDone
	_ = savePending(home, p) // 寫不進去：下面 finish 刪掉 pending 就好
	finish(home, p)
	return res, nil
}

// swap 一塊一塊換：現在的改名到 old/，備份的改名進家裡。
func swap(p *Pending, rename func(from, to string) error) error {
	for _, u := range p.Units {
		if u.HadLive {
			if err := os.MkdirAll(filepath.Dir(u.Old), 0o700); err != nil {
				return err
			}
			if err := rename(u.Live, u.Old); err != nil {
				return err
			}
		}
		if u.HasNew {
			if err := os.MkdirAll(filepath.Dir(u.Live), 0o755); err != nil {
				return err
			}
			if err := rename(u.New, u.Live); err != nil {
				return err
			}
		}
	}
	return nil
}

// rollback 把換過的全部換回去；每一塊不管換到哪一步都處理得了，重跑也沒關係。
func rollback(p *Pending, rename func(from, to string) error) error {
	var errs []error
	for i := len(p.Units) - 1; i >= 0; i-- {
		u := p.Units[i]
		// 備份的已經進家裡了：搬回 new/。判斷方式：new/ 那邊空了，而且（家裡本來沒有這塊，或原本那塊已經在 old/）。
		if u.HasNew && !exists(u.New) && exists(u.Live) && (!u.HadLive || exists(u.Old)) {
			if err := rename(u.Live, u.New); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if u.HadLive && exists(u.Old) && !exists(u.Live) {
			if err := rename(u.Old, u.Live); err != nil {
				errs = append(errs, err)
			}
		}
	}
	// 秘密檔：換之前的內容有存下來就寫回去；本來沒有這個檔、這次才可能長出來的就刪掉。
	if before, err := os.ReadFile(p.SecretsBefore); err == nil {
		if err := platform.AtomicWrite(p.SecretsFile, before, 0o600); err != nil {
			errs = append(errs, err)
		}
	} else if !p.SecretsExisted && p.SecretsNew != "" && p.Phase == phaseSwapping {
		if err := os.Remove(p.SecretsFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// mergeSecrets 把備份的推播金鑰放進這個家的秘密檔（只換 [webpush]，檔裡別的東西留著）。備份沒有金鑰就不動。
func mergeSecrets(p *Pending) error {
	if p.SecretsNew == "" || p.SecretsFile == "" {
		return nil
	}
	in := map[string]any{}
	if _, err := toml.DecodeFile(p.SecretsNew, &in); err != nil {
		return nil // 備份的金鑰讀不懂：當作沒有，手機重開一次通知就好
	}
	wp, ok := in["webpush"].(map[string]any)
	if !ok {
		return nil
	}
	cur := map[string]any{}
	if before, err := os.ReadFile(p.SecretsFile); err == nil {
		if err := os.MkdirAll(filepath.Dir(p.SecretsBefore), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(p.SecretsBefore, before, 0o600); err != nil {
			return err
		}
		if _, err := toml.Decode(string(before), &cur); err != nil {
			return nil // 這個家的秘密檔讀不懂：不動它（寫回去會丟掉裡面別的東西），手機重開一次通知就好
		}
	}
	cur["webpush"] = wp
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cur); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.SecretsFile), 0o700); err != nil {
		return err
	}
	return platform.AtomicWrite(p.SecretsFile, buf.Bytes(), 0o600)
}

// hasSwappedOut：還原的暫存裡有沒有「換下來的舊東西」（.restore/<代號>/old 不是空的）。
func hasSwappedOut(home string) bool {
	for _, token := range dirNames(restoreDir(home)) {
		if entries, err := os.ReadDir(filepath.Join(restoreDir(home), token, "old")); err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}

// Stuck 只讀地看這個家是不是「還原卡住了」（服務會因此拒絕啟動，HBK-7）：還原換到一半、東西已經換下來，而且不是
// 「其實已經成功、只差沒收乾淨」。給修復程式用（HST-10）：它重開服務後看板還是不起來，靠這個分辨是不是還原卡住——
// 是的話叫主人先不要動家裡的資料夾。什麼都不改；服務還在自己換回去的途中也會回 true，所以只在服務開不起來之後才問。
func Stuck(home string) bool {
	home = filepath.Clean(home)
	if !HasPending(home) {
		return false
	}
	p, err := loadPending(home)
	if err != nil {
		return hasSwappedOut(home)
	}
	if p.Phase != phaseSwapping {
		return false
	}
	if r := readResult(home); r != nil && r.OK && r.Token != "" && r.Token == p.Token {
		return false
	}
	return true
}

// finish 收掉這次還原的工作資料夾與 pending.json（先刪 pending：它在就表示還沒做完）。
func finish(home string, p *Pending) {
	_ = os.Remove(pendingPath(home))                           // 刪不掉：下次啟動會再走一次換回去，結果一樣
	_ = os.RemoveAll(filepath.Join(restoreDir(home), p.Token)) // 換下來的舊東西；還原前那份現況在備份資料夾
	removeIfEmpty(restoreDir(home))
}

// Todo 是換了電腦之後還要主人自己做的事（白話，一項一句；不叫主人打指令、不假設家裡有哪一位秘書）。
func Todo(installed []string) []string {
	out := []string{
		"AI 帳號要在這台電腦重新登入：按 ⌘＋空白鍵搜尋「" + platform.RescueName + "」打開它，照畫面選重新登入",
		"手機要重新連回家：到看板「設定 → 手機連線」照步驟做，手機上的看板也要重新加到主畫面",
		"手機的通知要重開一次（看板「設定 → 通知」）",
		"舊電腦上的家不要再開著用，兩邊同時用資料會對不起來",
	}
	if len(installed) > 0 {
		out = append(out, installedTodo(installed))
	}
	return out
}

func installedTodo(installed []string) string {
	return "別人分享給你的套件不在備份裡，要重新安裝：" + strings.Join(installed, "、")
}

// lostLinksTodo：還原前家裡有、換完就不見的捷徑，請主人自己重建（HBK-11；不自動補回）。
func lostLinksTodo(links []string) string {
	return "家裡原本的 " + strconv.Itoa(len(links)) + " 個捷徑（符號連結）還原後不見了，要用的話請自己重建：" + strings.Join(links, "、")
}

// restoreRecord 是 last-restore.json：上一次還原的結果，加上「通知過主人沒」。
type restoreRecord struct {
	sdk.HomeRestoreResult
	Notified bool `json:"notified,omitempty"`
	// Token 是那次還原的代號（pending.json 的 token）：啟動時靠它認出「結果已經寫了、只差沒收尾」的那一次。
	Token string `json:"token,omitempty"`
}

// markNotified 把結果（連同通知過沒）寫回 last-restore.json。
func markNotified(home string, r *restoreRecord) {
	path := platform.HomeRestoreResultPath(home)
	data, err := json.MarshalIndent(r, "", " ")
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	_ = platform.AtomicWrite(path, data, 0o600) // 結果寫不進去不影響還原本身；看板只是看不到那一行
}

func readResult(home string) *restoreRecord {
	data, err := os.ReadFile(platform.HomeRestoreResultPath(home))
	if err != nil {
		return nil
	}
	r := &restoreRecord{}
	if json.Unmarshal(data, r) != nil {
		return nil
	}
	return r
}

// lockService 拿這個家「服務正在跑」的鎖（服務跑著時由 vault 一直拿著，platform.ServiceLockPath）。拿不到＝這個家有活著的服務：
// 回白話的錯，什麼都不動。拿到了回放掉它的函式（換完就放，接著啟動的服務自己再拿）。
func lockService(home string) (unlock func(), err error) {
	path := platform.ServiceLockPath(home)
	if p, perr := loadPending(home); perr == nil && p.ServiceLock != "" {
		path = p.ServiceLock
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, platform.Unavailable("還原卡住了：這個家的暫存資料夾建不起來，沒有換上備份")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, platform.Unavailable("還原卡住了：這個家的暫存資料夾寫不進去，沒有換上備份")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close() // 只是關掉沒拿到鎖的檔
		return nil, platform.Conflict("這個家已經有一個服務在跑，這次啟動不換上備份、也不開第二個服務")
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) // 關檔也會放
		_ = f.Close()
	}, nil
}
