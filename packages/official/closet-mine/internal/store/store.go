// Package store 是「上傳衣服與角色」唯一碰磁碟的地方（D-30）：讀家的附件資料夾（sdk.SharesDir：第 1 版 <家>/runtime/share/，
// 第 2 版 <家>/media/share/，DEV-145）的上傳暫存，
// 寫 <家>/personas/mine/ 底下的角色卡與圖（新上傳的照片在 photos/<照片編號>.<副檔名>，DEV-160 S3），
// 換圖、刪單張時把照片紀錄指著的檔改名成備份、拿回時改回來。目的路徑只由驗證過的代號與編號組出、一定在 personas/mine/ 底下；
// 從不寫 personas/<代號>.md（官方角色卡）；官方圖 personas/avatars/ 只有兩個例外：開發家的「升成官方」（internal/promote），
// 與開發家換官方的照片（Official 那個 Store，只在官方角色位置就在家裡時、只碰 avatars/ 底下，CMI-25）。
package store

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// MaxUploadBytes：上傳檔大小上限。
const MaxUploadBytes = 20 << 20

// 錯誤。
var (
	ErrNoUpload  = errors.New("找不到上傳的檔案")
	ErrExists    = errors.New("目的檔已存在而且內容不同")
	ErrUnsafe    = errors.New("目的路徑不安全")
	errBadTarget = errors.New("目的檔名不合格")
)

// Store 管一個家。
type Store struct {
	Home string
	// official：根目錄換成官方角色的位置 <家>/personas/，只碰 avatars/ 底下的圖（開發家換官方照片，CMI-25；Official）。
	official bool
}

// New 組一個 Store。
func New(home string) *Store { return &Store{Home: home} }

// Official 是同一個家的官方角色位置 <家>/personas/：只給開發家換官方的照片用（CMI-25），先確定 OfficialInHome。
// 讀、改名、寫新檔都只在 avatars/ 底下（官方角色卡 <代號>.md、personas/mine/ 都碰不到）；路上的資料夾一樣不能是符號連結。
func (s *Store) Official() *Store { return &Store{Home: s.Home, official: true} }

// OfficialInHome：官方角色的位置就是 <家>/personas/（第 1 版的家、official_personas = "home" 的開發家）。
// 不是（第 2 版在家外的官方層）時，官方的圖與照片紀錄跟著角色包走，這個家不能改（VLT-19）。
func (s *Store) OfficialInHome() bool {
	return filepath.Clean(sdk.OfficialPersonasDir(s.Home)) == filepath.Join(s.Home, "personas")
}

func (s *Store) mineDir() string {
	if s.official {
		return filepath.Join(s.Home, "personas")
	}
	return filepath.Join(s.Home, "personas", "mine")
}
func (s *Store) shareDir() string { return sdk.SharesDir(s.Home) }

// ReadUpload 讀附件資料夾的 <name>：name 只能是單一檔名；用 Lstat 確認是一般檔案（不是符號連結、資料夾）、大小 ≤ 20 MB。
func (s *Store) ReadUpload(name string) ([]byte, error) {
	if name == "" || strings.ContainsAny(name, "/\\\x00") || strings.HasPrefix(name, ".") || strings.Contains(name, "..") {
		return nil, ErrNoUpload
	}
	path := filepath.Join(s.shareDir(), name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxUploadBytes {
		return nil, ErrNoUpload
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrNoUpload
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxUploadBytes+1))
	if err != nil || len(data) > MaxUploadBytes {
		return nil, ErrNoUpload
	}
	return data, nil
}

// relPattern 是能寫新檔的 rel：`<代號>.md`（角色卡）、`avatars/<代號>.<副檔名>`（角色本身的頭像）、`photos/<照片編號>.<副檔名>`
// （DEV-160 S3：名字沒有意思，是誰的、哪一種只看 closet.photo）。檔名只用小寫英數與 _ -，副檔名限定。
// 以前的 `avatars/<代號>@…`、`avatars/full/…`、`adult/…` 只照 ValidFile 改名成備份或改回來。
var relPattern = regexp.MustCompile(`^([a-z0-9][a-z0-9_-]*\.md|avatars/[a-z0-9][a-z0-9_]*\.(png|jpg|webp|mp4)|photos/[a-z0-9][a-z0-9_-]*\.(png|jpg|webp|mp4))$`)

// ValidRel：rel 是不是 personas/mine/ 底下允許寫新檔的相對路徑——角色卡 <代號>.md、角色本身的頭像 avatars/<代號>.<圖或 mp4>
// 與 photos/<照片編號>.<副檔名>（DEV-160 S3 起新的檔只寫這三種；舊的 avatars/<代號>@…、full/、adult/ 只會被改名成備份或改回來，照 ValidFile）。
func ValidRel(rel string) bool { return relPattern.MatchString(rel) && !strings.Contains(rel, "..") }

// officialRelPattern 是官方位置能寫的新檔（只有換官方照片會寫，CMI-25）：角色本身的頭像 avatars/<代號>.<副檔名>、
// 其他照片 avatars/photos/<照片編號>.<副檔名>（官方的圖都在 avatars/ 底下，建包、匯出只收那裡；升成官方也放這裡）。
var officialRelPattern = regexp.MustCompile(`^avatars/([a-z0-9][a-z0-9_]*|photos/[a-z0-9][a-z0-9_-]*)\.(png|jpg|webp|mp4)$`)

// validNew：這個 Store 能不能寫 rel 這個新檔（personas/mine/ 照 ValidRel；官方位置照 officialRelPattern）。
func (s *Store) validNew(rel string) bool {
	if s.official {
		return officialRelPattern.MatchString(rel) && !strings.Contains(rel, "..")
	}
	return ValidRel(rel)
}

// validFile：照片紀錄指著的檔能不能讀、改名（ValidFile；官方位置另外只認 avatars/ 底下的，ValidOfficialFile）。
func (s *Store) validFile(rel string) bool {
	if s.official {
		return ValidOfficialFile(rel)
	}
	return ValidFile(rel)
}

// ValidOfficialFile：官方照片紀錄指著的檔，官方位置（Official）讀得到、改得了名嗎——要在 avatars/ 底下而且 ValidFile。
// 換官方照片（CMI-25）先照這個判斷，不合的直接拒絕說原因，不寫到一半才發現寫不了。
func ValidOfficialFile(rel string) bool { return strings.HasPrefix(rel, "avatars/") && ValidFile(rel) }

// 照片紀錄指著的檔（closet.photo 的 file，相對於 personas/mine/）：新上傳的在 photos/，舊的在 avatars/、adult/ 或別的自己的隱藏版資料夾，
// 名字不一定照現在寫檔的規則（舊檔、macOS 上大寫的副檔名）。結尾是圖或影片的副檔名，或它的備份
// （<原名>.bak-<日期>[-<時分>]-<原因>，照備份規則（system 卡 BSW-4）；換圖、刪單張時舊檔改名成這樣）。
var (
	mediaFile  = regexp.MustCompile(`(?i)\.(png|jpg|jpeg|webp|mp4)$`)
	backupFile = regexp.MustCompile(`(?i)\.(png|jpg|jpeg|webp|mp4)\.bak-[0-9]{8}(-[0-9]{4})?-[a-z0-9-]+$`)
)

// ValidFile：rel 是不是照片紀錄可以指的檔——相對路徑、用 / 分段、2 到 4 段、沒有空段或 . 開頭的段（含 ..）、沒有反斜線與控制字元，
// 是圖、影片或它們的備份。只用來讀、改名（換圖、刪單張、拿回），新檔一律照 ValidRel 寫。
func ValidFile(rel string) bool {
	if rel == "" || len(rel) > 300 || strings.ContainsAny(rel, "\\\x00") {
		return false
	}
	segs := strings.Split(rel, "/")
	if len(segs) < 2 || len(segs) > 4 {
		return false
	}
	for _, seg := range segs {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return false
		}
		for _, r := range seg {
			if r < 0x20 || r == 0x7f {
				return false
			}
		}
	}
	return mediaFile.MatchString(rel) || backupFile.MatchString(rel)
}

// backupOf 拆備份的名字：<原名>.bak-<日期>[-<時分>]-<原因>。
var backupOf = regexp.MustCompile(`^(.+)\.bak-[0-9]{8}(?:-[0-9]{4})?-([a-z0-9-]+)$`)

// SplitBackup：file 是 BackupName 寫的備份嗎；是的話回原來的名字與原因（例 delete-u12）。
func SplitBackup(file string) (orig, reason string, ok bool) {
	m := backupOf.FindStringSubmatch(file)
	if m == nil || !mediaFile.MatchString(m[1]) {
		return "", "", false
	}
	return m[1], m[2], true
}

var backupWhen = regexp.MustCompile(`\.bak-([0-9]{8})(?:-([0-9]{4}))?-[a-z0-9-]*?(?:u([0-9]+))?$`)

// backupLess：備份 a 比 b 舊嗎——先比日期、時分（沒有時分算 0000），再比原因裡的請求編號（delete-u9 比 delete-u12 舊），最後照名字。
func backupLess(a, b string) bool {
	ma, mb := backupWhen.FindStringSubmatch(a), backupWhen.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return a < b
	}
	key := func(m []string) (string, int) {
		hm := m[2]
		if hm == "" {
			hm = "0000"
		}
		n, _ := strconv.Atoi(m[3])
		return m[1] + hm, n
	}
	wa, na := key(ma)
	wb, nb := key(mb)
	switch {
	case wa != wb:
		return wa < wb
	case na != nb:
		return na < nb
	}
	return a < b
}

// BackupName 是 file 的備份名字：<原名>.bak-<日期>-<原因>（備份規則（system 卡 BSW-4）；放在原檔旁邊，預設留 30 天）。
func BackupName(file string, day time.Time, reason string) string {
	return file + ".bak-" + day.Format("20060102") + "-" + reason
}

// resolve 檢查 rel（照 ValidRel），確認 personas/mine（與路上的資料夾）不是符號連結，回完整路徑。
// create 為 true 時，不存在的資料夾會建起來。
func (s *Store) resolve(rel string, create bool) (string, error) {
	return s.resolveIf(rel, create, s.validNew)
}

// resolveIf 同 resolve，rel 照 valid 檢查（寫新檔照 ValidRel；照片紀錄指著的檔照 ValidFile）。
func (s *Store) resolveIf(rel string, create bool, valid func(string) bool) (string, error) {
	if !valid(rel) {
		return "", errBadTarget
	}
	dir := s.mineDir()
	parts := strings.Split(rel, "/")
	chain := []string{dir}
	for _, p := range parts[:len(parts)-1] {
		dir = filepath.Join(dir, p)
		chain = append(chain, dir)
	}
	for _, d := range chain {
		info, err := os.Lstat(d)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return "", ErrUnsafe
			}
		case os.IsNotExist(err):
			if !create {
				return "", err
			}
			if err := os.Mkdir(d, 0o755); err != nil && !os.IsExist(err) {
				return "", err
			}
			// 建完再確認一次（避免剛好被換成符號連結）。
			if info, err := os.Lstat(d); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return "", ErrUnsafe
			}
		default:
			return "", err
		}
	}
	full := filepath.Join(chain[len(chain)-1], parts[len(parts)-1])
	if r, err := filepath.Rel(s.mineDir(), full); err != nil || strings.HasPrefix(r, "..") {
		return "", ErrUnsafe
	}
	return full, nil
}

// Read 讀 personas/mine/<rel>；不存在回 nil, nil；不是一般檔案回 ErrUnsafe。
func (s *Store) Read(rel string) ([]byte, error) {
	full, err := s.resolve(rel, false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	info, err := os.Lstat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrUnsafe
	}
	return os.ReadFile(full)
}

// PutNew 寫新檔，不覆蓋：目的檔已存在而且位元組相同＝成功（written = false，重播）；不同回 ErrExists。
func (s *Store) PutNew(rel string, data []byte) (written bool, err error) {
	full, err := s.resolve(rel, true)
	if err != nil {
		return false, err
	}
	if info, err := os.Lstat(full); err == nil {
		if !info.Mode().IsRegular() {
			return false, ErrUnsafe
		}
		cur, err := os.ReadFile(full)
		if err != nil {
			return false, err
		}
		if bytes.Equal(cur, data) {
			return false, nil
		}
		return false, ErrExists
	} else if !os.IsNotExist(err) {
		return false, err
	}
	return true, writeAtomic(full, data)
}

// Remove 刪 personas/mine/<rel>（不存在不算錯；不是一般檔案不刪）。
func (s *Store) Remove(rel string) error {
	full, err := s.resolve(rel, false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	info, err := os.Lstat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrUnsafe
	}
	return os.Remove(full)
}

// Exists：personas/mine/<rel> 是否存在（任何型態都算存在）。
func (s *Store) Exists(rel string) bool {
	full, err := s.resolve(rel, false)
	if err != nil {
		return errors.Is(err, ErrUnsafe)
	}
	_, err = os.Lstat(full)
	return err == nil
}

// OfficialCardExists：官方角色卡 <代號>.md 是否存在（只讀；代號要先驗過）。走找官方卡的共用入口 sdk.OfficialCardPath（PRS-25）：
// 官方角色資料夾（第 1 版 <家>/personas/，第 2 版在狀態資料夾）先，沒有才看跟著程式的公開卡（新裝的家還沒同步角色包時只有那張）；
// 主人自己有同代號的卡（personas/mine/<代號>.md，舊的不是 my_ 開頭的）時跟著程式的那張不算——這個代號在這個家是主人的，不是官方角色（PRS-27）。
func (s *Store) OfficialCardExists(code string) bool {
	if !regexp.MustCompile(`^[a-z][a-z0-9]*$`).MatchString(code) {
		return false
	}
	_, ok := sdk.OfficialCardPath(s.Home, code)
	return ok
}

// PhotoExts 是頭像 avatars/<代號>.<ext> 認的圖的副檔名（jpeg 是人工放進來的，我們自己只寫 png／jpg／webp）：新角色看代號有沒有人用、
// 認哪個檔是角色本身的頭像（isAvatar）時用。
var PhotoExts = []string{"png", "jpg", "jpeg", "webp"}

// ReadFile 讀照片紀錄指著的檔 personas/mine/<rel>（照 ValidFile）；不存在回 nil, nil；不是一般檔案回 ErrUnsafe。
func (s *Store) ReadFile(rel string) ([]byte, error) {
	full, err := s.resolveIf(rel, false, s.validFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	info, err := os.Lstat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrUnsafe
	}
	return os.ReadFile(full)
}

// HasFile：照片紀錄指著的檔 personas/mine/<rel> 在不在（一般檔案才算；是符號連結或資料夾回 ErrUnsafe）。
func (s *Store) HasFile(rel string) (bool, error) {
	full, err := s.resolveIf(rel, false, s.validFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	info, err := os.Lstat(full)
	switch {
	case os.IsNotExist(err):
		return false, nil
	case err != nil:
		return false, err
	case !info.Mode().IsRegular():
		return false, ErrUnsafe
	}
	return true, nil
}

// Move 把 personas/mine/<from> 改名成 <to>（兩個都照 ValidFile；換圖、刪單張時把檔改名成備份，拿回時改回來）：
// from 要是一般檔案；to 已經有東西回 ErrExists（不蓋）。路上的資料夾不能是符號連結。
func (s *Store) Move(from, to string) error {
	src, err := s.resolveIf(from, false, s.validFile)
	if err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrUnsafe
	}
	dst, err := s.resolveIf(to, true, s.validFile)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		return ErrExists
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(src, dst)
}

// FindBackup 找 <file>.bak-<日期>[-<時分>]-<原因> 這種備份、原因合 match 的（同一個請求上次做到一半、或要拿回刪掉的那張時用）；
// 有好幾個回最新的（照日期、時分，再照原因裡的請求編號 u<n> 的數字比；backupLess），沒有回空字串。回的是相對於 personas/mine/ 的路徑。
func (s *Store) FindBackup(file string, match func(reason string) bool) (string, error) {
	full, err := s.resolveIf(file, false, s.validFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	entries, err := os.ReadDir(filepath.Dir(full))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	base := filepath.Base(full)
	var found []string
	for _, e := range entries {
		n := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(n, base+".bak-") {
			continue
		}
		if orig, reason, ok := SplitBackup(n); ok && orig == base && match(reason) {
			found = append(found, n)
		}
	}
	if len(found) == 0 {
		return "", nil
	}
	sort.Slice(found, func(i, j int) bool { return backupLess(found[i], found[j]) })
	return path.Join(path.Dir(file), found[len(found)-1]), nil
}

func writeAtomic(full string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(full), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, full); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("寫入失敗：%w", err)
	}
	return nil
}
