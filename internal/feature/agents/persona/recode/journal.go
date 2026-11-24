// Package recode 是離線改角色代號（DEV-154 第 10、13 件，PRS-22～PRS-24）：evergenie home tidy（自己的角色改成 my_ 開頭、
// 放錯位置的角色收成自己的、官方的卡與圖搬出家）與 evergenie personas promote（自己的角色升成官方）共用。
//
// 只在服務停著時做；預演是預設，套用時整份計畫先寫進紀錄檔、要改內容的檔原文另存一份，一步一步做，中途失敗照紀錄反向退回；
// 之後也能照紀錄逐檔退回到跟之前一模一樣（位元組相同、檔名相同）。
//
// 套件紀錄怎麼跟著代號走只看套件自己的宣告（Kind，來自 package.toml 的 persona_field、persona_id、persona_prefixed、
// persona_source，PKG-64，由組裝點 cmd/evergenie 讀好傳進來）：這個 package 不 import 任何套件、也不認得任何套件的名字（法律第 3 條）。
package recode

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 一步的種類。
const (
	OpMkdir  = "mkdir"  // 建一個資料夾（退回時是空的就拿掉）
	OpRename = "rename" // 檔案或資料夾改名、搬位置（同一顆磁碟，os.Rename）
	OpWrite  = "write"  // 改一個文字檔的內容（原文另存一份）
)

// Step 是計畫裡的一步。路徑都是絕對路徑。
// 順序固定：先建資料夾、再改名搬位置、最後改內容（改的是搬完的位置）——退回時反過來，每一步只看自己的檔案現在的樣子就知道要不要退。
type Step struct {
	Op   string `json:"op"`
	Path string `json:"path"`         // mkdir、write 的位置；rename 的來源
	To   string `json:"to,omitempty"` // rename 的目的地
	What string `json:"what"`         // 白話：這一步是什麼（卡、圖、名冊、套件紀錄…）
	// write：改了哪幾行（預演給人看），改前改後的雜湊（退回時確認沒人再改過），原文存在原文資料夾裡的檔名。
	Edits     []Edit `json:"edits,omitempty"`
	BeforeSHA string `json:"before_sha256,omitempty"`
	AfterSHA  string `json:"after_sha256,omitempty"`
	Mode      uint32 `json:"mode,omitempty"`
	Saved     string `json:"saved,omitempty"`

	before, after []byte
}

// Edit 是 write 那一步改的一行（行號從 1 算）。
type Edit struct {
	Line   int    `json:"line"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// journal 是紀錄檔（JSON）：整份計畫、原文資料夾、做完沒有。套用前就寫好；退回靠它。
type journal struct {
	Schema    int    `json:"schema"`
	Tool      string `json:"tool"`
	Home      string `json:"home"`
	At        string `json:"at"`
	Renames   []Code `json:"renames,omitempty"`
	Originals string `json:"originals"`
	// MadeDir：紀錄檔所在的資料夾是這次才建的（最上面那一層，例如 settings/persona-promote）；退回完是空的就拿掉，家才跟之前一樣。
	MadeDir string `json:"made_dir,omitempty"`
	Steps   []Step `json:"steps"`
	Done    bool   `json:"done"`
}

// Code 是一組代號對照（舊 → 新），紀錄檔與輸出用。
type Code struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// 測試掛勾：renameFile 換掉 os.Rename；failAt 是第幾步（從 1 算）做之前故意失敗，0＝不用。
var (
	renameFile = os.Rename
	failAt     = 0
)

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// apply 照計畫做：先存原文與紀錄檔，再一步一步做；任何一步失敗就把做過的反向退回、拿掉紀錄檔與原文，家跟之前一樣。
func apply(steps []Step, j *journal, journalPath string) (err error) {
	if exists(journalPath) {
		return platform.Conflict("已經有紀錄檔 " + journalPath + "（上次做的還沒收尾：要退回用 --revert --apply；確定不退了用 --forget --apply）")
	}
	if exists(j.Originals) {
		return platform.Conflict("已經有原文資料夾 " + j.Originals + "（上次留下的？先確認內容，再照備份規則改名成 .bak-<日期>-<原因>）")
	}
	if err := preflight(steps); err != nil {
		return err
	}
	j.MadeDir = firstMissing(filepath.Dir(journalPath))
	if err := os.MkdirAll(j.Originals, 0o700); err != nil {
		return platform.Unavailable("建不了原文資料夾 " + j.Originals + "：" + err.Error())
	}
	cleanup := func() {
		_ = os.RemoveAll(j.Originals)
		_ = os.Remove(journalPath)
		removeEmpty(j.MadeDir, filepath.Dir(journalPath))
	}
	for i := range steps {
		if steps[i].Op != OpWrite {
			continue
		}
		steps[i].Saved = fmt.Sprintf("%04d.orig", i+1)
		if err := platform.AtomicWrite(filepath.Join(j.Originals, steps[i].Saved), steps[i].before, 0o600); err != nil {
			cleanup()
			return platform.Unavailable("存不了原文：" + err.Error())
		}
	}
	j.Steps = steps
	if err := saveJournal(journalPath, j); err != nil {
		cleanup()
		return err
	}
	started := 0
	defer func() {
		if err == nil {
			return
		}
		probs, uerr := undo(j.Steps[:started], j.Originals, true)
		if uerr != nil || len(probs) > 0 {
			msg := fmt.Sprintf("%s；自動退回沒做完，紀錄留在 %s，可以用 --revert 再試", err.Error(), journalPath)
			for _, p := range probs {
				msg += "\n  " + p
			}
			if uerr != nil {
				msg += "\n  " + uerr.Error()
			}
			err = platform.Unavailable(msg)
			return
		}
		cleanup()
		err = platform.Unavailable(err.Error() + "；做過的已經全部退回，家跟之前一樣")
	}()
	for i := range j.Steps {
		if failAt == i+1 {
			started = i
			return fmt.Errorf("第 %d 步做之前測試故意失敗", i+1)
		}
		// 每一步都是一次做完或沒做（建資料夾、同一顆磁碟的改名、先寫暫存檔再換上），失敗的那一步不用退。
		if err := doStep(&j.Steps[i]); err != nil {
			started = i
			return fmt.Errorf("第 %d 步（%s %s）沒做成：%s", i+1, j.Steps[i].What, j.Steps[i].Path, err.Error())
		}
		started = i + 1
	}
	j.Done = true
	return saveJournal(journalPath, j)
}

// preflight：計畫做好之後到現在沒人動過：改名的來源在、目的地沒有；要改的檔內容跟預演時一樣；要建的資料夾還沒有。
func preflight(steps []Step) error {
	var probs []string
	for _, s := range steps {
		switch s.Op {
		case OpMkdir:
			if exists(s.Path) {
				probs = append(probs, "資料夾已經有了："+s.Path)
			}
		case OpRename:
			if !exists(s.Path) {
				probs = append(probs, "找不到："+s.Path)
			}
			if exists(s.To) {
				probs = append(probs, "目的地已經有東西："+s.To)
			}
		case OpWrite:
			src := sourceOf(steps, s.Path)
			if cur, err := os.ReadFile(src); err != nil || sum(cur) != s.BeforeSHA {
				probs = append(probs, "預演之後被改過："+src)
			}
		}
	}
	if len(probs) > 0 {
		return platform.Conflict("預演之後家裡有東西變了，什麼都沒動，請再預演一次：\n  " + joinLines(probs))
	}
	return nil
}

// sourceOf 是 write 那一步的檔在做之前的位置（步驟裡寫的是改名搬完之後的位置）。
func sourceOf(steps []Step, p string) string {
	for i := len(steps) - 1; i >= 0; i-- {
		s := steps[i]
		if s.Op != OpRename {
			continue
		}
		if p == s.To {
			p = s.Path
		} else if rest, ok := strings.CutPrefix(p, s.To+string(filepath.Separator)); ok {
			p = filepath.Join(s.Path, rest)
		}
	}
	return p
}

func doStep(s *Step) error {
	switch s.Op {
	case OpMkdir:
		return os.Mkdir(s.Path, 0o755)
	case OpRename:
		if exists(s.To) {
			return errors.New("目的地已經有東西")
		}
		return renameFile(s.Path, s.To)
	case OpWrite:
		cur, err := os.ReadFile(s.Path)
		if err != nil {
			return err
		}
		if sum(cur) != s.BeforeSHA {
			return errors.New("這個檔在預演之後被改過")
		}
		return platform.AtomicWrite(s.Path, s.after, os.FileMode(s.Mode))
	}
	return fmt.Errorf("不認得的步驟 %s", s.Op)
}

// undo 反向退回 steps。每一步只看檔案現在的樣子：改名的目的地在、來源不在才搬回；內容是改後的才換回原文；
// 已經是原樣的跳過（沒做到的步驟、上次退到一半的都適用）。check＝先全部檢查一次，有問題就一步都不動（回問題清單）。
func undo(steps []Step, originals string, check bool) ([]string, error) {
	if check {
		if probs := undoProblems(steps, originals); len(probs) > 0 {
			return probs, nil
		}
	}
	for i := len(steps) - 1; i >= 0; i-- {
		s := steps[i]
		switch s.Op {
		case OpWrite:
			state, err := writeState(steps, s)
			if err != nil {
				return nil, err
			}
			if state == writeNotDone {
				continue
			}
			orig, err := readOriginal(s, originals)
			if err != nil {
				return nil, err
			}
			if err := platform.AtomicWrite(s.Path, orig, os.FileMode(s.Mode)); err != nil {
				return nil, fmt.Errorf("換不回 %s：%w", s.Path, err)
			}
		case OpRename:
			if exists(s.To) && !exists(s.Path) {
				if err := renameFile(s.To, s.Path); err != nil {
					return nil, fmt.Errorf("搬不回 %s：%w", s.Path, err)
				}
			}
		case OpMkdir:
			if entries, err := os.ReadDir(s.Path); err == nil && len(entries) == 0 {
				_ = os.Remove(s.Path)
			}
		}
	}
	return nil, nil
}

// undoProblems 是退回前的檢查（不動任何檔）。
func undoProblems(steps []Step, originals string) []string {
	var probs []string
	for i := len(steps) - 1; i >= 0; i-- {
		s := steps[i]
		switch s.Op {
		case OpWrite:
			state, err := writeState(steps, s)
			switch {
			case err != nil:
				probs = append(probs, err.Error())
			case state == writeNotDone:
			default:
				if _, err := readOriginal(s, originals); err != nil {
					probs = append(probs, err.Error())
				}
			}
		case OpRename:
			to, from := exists(s.To), exists(s.Path)
			switch {
			case to && !from, !to && from:
			case to && from:
				probs = append(probs, "兩邊都有東西，不知道哪個是對的："+s.Path+"、"+s.To)
			default:
				probs = append(probs, "兩邊都找不到："+s.Path+"、"+s.To)
			}
		}
	}
	return probs
}

const (
	writeNotDone = iota // 還是改之前的內容（沒做到這一步）
	writeDone           // 是改後的內容，要換回原文
)

// writeState 看 write 那一步現在的樣子。做到一半當掉（程式被關、電腦斷電）時，要改的檔可能還在改名之前的位置
// （它跟著的改名那一步沒做到）：那裡的內容是改之前的就算沒做到。內容既不是改前也不是改後＝之後被改過，不能換回。
func writeState(steps []Step, s Step) (int, error) {
	cur, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		src := sourceOf(steps, s.Path)
		if src != s.Path {
			if old, serr := os.ReadFile(src); serr == nil && sum(old) == s.BeforeSHA {
				return writeNotDone, nil
			}
		}
		return 0, fmt.Errorf("找不到要換回原文的檔：%s", s.Path)
	}
	if err != nil {
		return 0, fmt.Errorf("讀不了 %s：%w", s.Path, err)
	}
	switch sum(cur) {
	case s.BeforeSHA:
		return writeNotDone, nil
	case s.AfterSHA:
		return writeDone, nil
	}
	return 0, fmt.Errorf("這個檔在整理之後又被改過，換回原文會蓋掉新的內容：%s", s.Path)
}

func readOriginal(s Step, originals string) ([]byte, error) {
	p := filepath.Join(originals, s.Saved)
	orig, err := os.ReadFile(p)
	if err != nil || s.Saved == "" {
		return nil, fmt.Errorf("找不到 %s 的原文（%s）", s.Path, p)
	}
	if sum(orig) != s.BeforeSHA {
		return nil, fmt.Errorf("%s 的原文（%s）跟記下的雜湊對不上", s.Path, p)
	}
	return orig, nil
}

func saveJournal(path string, j *journal) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return platform.Internal("寫不了紀錄檔")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return platform.Unavailable("寫不了紀錄檔 " + path + "：" + err.Error())
	}
	if err := platform.AtomicWrite(path, append(data, '\n'), 0o644); err != nil {
		return platform.Unavailable("寫不了紀錄檔 " + path + "：" + err.Error())
	}
	return nil
}

func loadJournal(path string) (*journal, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, platform.NotFound("找不到紀錄檔 " + path + "（沒做過，或已經退回了）")
	}
	if err != nil {
		return nil, platform.Unavailable("讀不了紀錄檔 " + path)
	}
	var j journal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, platform.Invalid("journal", "紀錄檔格式壞了："+path)
	}
	return &j, nil
}

// RevertPlan 是照紀錄退回的預演。
type RevertPlan struct {
	Journal  string   `json:"journal"`
	Tool     string   `json:"tool"`
	At       string   `json:"at"`
	Done     bool     `json:"done"` // 上次有沒有做完（沒做完＝做到一半中斷）
	Renames  []Code   `json:"renames,omitempty"`
	Steps    int      `json:"steps"`
	Problems []string `json:"problems,omitempty"`
}

// PlanRevert 讀紀錄檔、檢查每一步能不能退回（不動任何檔）。
func PlanRevert(journalPath string) (*RevertPlan, error) {
	j, err := loadJournal(journalPath)
	if err != nil {
		return nil, err
	}
	rp := &RevertPlan{Journal: journalPath, Tool: j.Tool, At: j.At, Done: j.Done, Renames: j.Renames, Steps: len(j.Steps)}
	rp.Problems = undoProblems(j.Steps, j.Originals)
	return rp, nil
}

// Revert 照紀錄逐步退回；全部退回後拿掉紀錄檔與原文資料夾（家跟做之前逐檔相同，原文已經放回原位，不用再留一份）。
func Revert(journalPath string) error {
	j, err := loadJournal(journalPath)
	if err != nil {
		return err
	}
	probs, err := undo(j.Steps, j.Originals, true)
	if len(probs) > 0 {
		return platform.Conflict("有地方對不上，什麼都沒動：\n  " + joinLines(probs))
	}
	if err != nil {
		return platform.Unavailable(err.Error() + "（紀錄檔留著，修好再用 --revert 接著退）")
	}
	_ = os.RemoveAll(j.Originals)
	if err := os.Remove(journalPath); err != nil {
		return platform.Unavailable("都退回了，但拿不掉紀錄檔 " + journalPath + "：" + err.Error())
	}
	removeEmpty(j.MadeDir, filepath.Dir(journalPath))
	return nil
}

// ForgetPlan 是收尾的預演：確定不退了，紀錄檔與原文資料夾改名成備份（照備份規則 <名>.bak-<日期>-done，放原位旁邊，
// 30 天後可以刪）。收尾後就不能 --revert 了；之後才能再整理、再升、再做 home relayout。
type ForgetPlan struct {
	Journal  string      `json:"journal"`
	Tool     string      `json:"tool"`
	At       string      `json:"at"`
	Renames  []Code      `json:"renames,omitempty"`
	Moves    [][2]string `json:"moves"` // 改名成備份：[現在, 之後]
	Problems []string    `json:"problems,omitempty"`
}

// PlanForget 讀紀錄檔、算收尾要改的名（不動任何檔）。上次沒做完（做到一半當掉）的不能收尾：家是一半改過的樣子，要先退回。
func PlanForget(journalPath string, now time.Time) (*ForgetPlan, error) {
	j, err := loadJournal(journalPath)
	if err != nil {
		return nil, err
	}
	fp := &ForgetPlan{Journal: journalPath, Tool: j.Tool, At: j.At, Renames: j.Renames}
	if !j.Done {
		fp.Problems = append(fp.Problems, "上次沒做完（中途停了），家裡是一半改過的樣子：先用 --revert 退回，不能收尾")
	}
	fp.Moves = append(fp.Moves, [2]string{journalPath, platform.BackupName(journalPath, now, "done")})
	if exists(j.Originals) {
		fp.Moves = append(fp.Moves, [2]string{j.Originals, platform.BackupName(j.Originals, now, "done")})
	}
	return fp, nil
}

// Forget 照 PlanForget 收尾：先改原文資料夾、再改紀錄檔（紀錄檔還在＝還沒收尾，中途失敗可以再做一次）。
func Forget(journalPath string, now time.Time) (*ForgetPlan, error) {
	fp, err := PlanForget(journalPath, now)
	if err != nil {
		return nil, err
	}
	if len(fp.Problems) > 0 {
		return fp, platform.Conflict(joinLines(fp.Problems))
	}
	for i := len(fp.Moves) - 1; i >= 0; i-- {
		m := fp.Moves[i]
		if err := os.Rename(m[0], m[1]); err != nil {
			return fp, platform.Unavailable("改不了名 " + m[0] + "：" + err.Error())
		}
	}
	return fp, nil
}

// firstMissing 回 dir 往上第一層還沒有的資料夾裡最上面那一層（都有了回空字串）。
func firstMissing(dir string) string {
	top := ""
	for d := dir; !exists(d); d = filepath.Dir(d) {
		top = d
		if filepath.Dir(d) == d {
			break
		}
	}
	return top
}

// removeEmpty 從 dir 往上拿掉空的資料夾，到 top 為止（top 是空字串＝都不是這次建的，不拿）。
func removeEmpty(top, dir string) {
	if top == "" {
		return
	}
	for d := dir; ; d = filepath.Dir(d) {
		if entries, err := os.ReadDir(d); err != nil || len(entries) > 0 || os.Remove(d) != nil {
			return
		}
		if d == top || filepath.Dir(d) == d {
			return
		}
	}
}

func joinLines(lines []string) string {
	var b bytes.Buffer
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\n  ")
		}
		b.WriteString(l)
	}
	return b.String()
}

func stamp(now time.Time) string { return now.Format(time.RFC3339) }

func conflictf(format string, a ...any) error { return platform.Conflict(fmt.Sprintf(format, a...)) }
