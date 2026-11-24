package homebackup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
)

// errBadArchive：這個檔不是備份檔，或內容不對（壞了、被改過、夾了不該有的東西）。細節只進日誌。
type badArchive struct{ why string }

func (e *badArchive) Error() string { return "bad backup archive: " + e.why }

func bad(why string) error { return &badArchive{why: why} }

// errLegacy：改名（DEV-158）前的舊版程式做的備份（副檔名是改名前的，或說明檔寫的產品名不是這一版的）。
// 裡面是改名前的格式，硬還原會得到一個讀不起來的家，所以這一版不還原：讀備份檔時（walk）就停——副檔名是改名前的不打開，
// 產品名不對的讀完說明檔就停、不交出說明檔——預覽與還原都還沒動到任何東西（Stage 先 Inspect 才解開）。
// 列出、下載、清理照樣認得（platform.LegacyHomeBackupExt）。
var errLegacy = errors.New("backup made before the rename")

// legacyNote 是 errLegacy 給人看的話。
const legacyNote = "這是改名前的舊版備份，這一版不能還原；要還原請用舊版的程式"

// userError 把讀備份檔的錯誤換成給人看的話。
func userError(err error) error {
	if errors.Is(err, errLegacy) {
		return platform.Conflict(legacyNote)
	}
	var b *badArchive
	if errors.As(err, &b) {
		return platform.Invalid("backup", "這個檔不是備份檔，或是檔案壞了（沒有動到你的家）")
	}
	var pe *platform.Err
	if errors.As(err, &pe) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		return platform.NotFound("找不到這份備份檔")
	}
	return platform.Unavailable("讀不了這份備份檔（硬碟滿了或沒有權限）")
}

// walk 把備份檔從頭讀到尾：每個一般檔叫一次 onFile（要把 r 讀完或不讀都可以），最後回說明檔。
// 只收資料夾與一般檔；絕對路徑、..、重複的檔、符號連結、不在已知位置的路徑一律當壞檔（解開前的第一道保險）。
// 改名前的舊版備份回 errLegacy：副檔名是改名前的就不打開，說明檔的產品名不是這一版的就不交出說明檔。
func walk(archive string, onFile func(arc string, size, mode int64, r io.Reader) error) (*Manifest, error) {
	if strings.HasSuffix(archive, platform.LegacyHomeBackupExt) {
		return nil, errLegacy
	}
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, bad("not gzip")
	}
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var m *Manifest
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, bad("tar: " + err.Error())
		}
		name := strings.TrimSuffix(hdr.Name, "/")
		if name == "" || name != path.Clean(name) || strings.HasPrefix(name, "/") || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsRune(name, 0) {
			return nil, bad("unsafe path " + hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if name != "home" && name != "secrets" && sectionOf(name+"/x") == "" && !isParentOfKnown(name) {
				return nil, bad("unexpected folder " + name)
			}
			continue
		case tar.TypeReg:
		default:
			return nil, bad("unsupported entry type in " + name)
		}
		if m != nil {
			return nil, bad("entries after the manifest")
		}
		if seen[name] {
			return nil, bad("duplicate entry " + name)
		}
		seen[name] = true
		if name == manifestName {
			data, err := io.ReadAll(io.LimitReader(tr, maxManifestBytes+1))
			if err != nil {
				return nil, bad("manifest: " + err.Error())
			}
			if len(data) > maxManifestBytes {
				return nil, bad("manifest too big")
			}
			m = &Manifest{}
			if err := json.Unmarshal(data, m); err != nil {
				return nil, bad("manifest is not JSON")
			}
			continue
		}
		if sectionOf(name) == "" {
			return nil, bad("unexpected file " + name)
		}
		if err := onFile(name, hdr.Size, hdr.Mode, tr); err != nil {
			return nil, err
		}
	}
	if m == nil {
		return nil, bad("no manifest")
	}
	if m.Product == "" || m.Format < 1 {
		return nil, bad("not a " + platform.ProductName + " backup")
	}
	if m.Product != platform.ProductName {
		return nil, errLegacy
	}
	return m, nil
}

// isParentOfKnown：資料夾是不是已知位置的上層（home/personas、home/packages）。
func isParentOfKnown(name string) bool {
	return name == "home/personas" || name == "home/packages" || name == "home/settings" || name == "home/vault" ||
		name == "home/media" || name == "home/packages/mine" || name == "home/other"
}

// check 對照說明檔：列的每個檔都在、大小與檢查碼都對，也沒有多出來的檔。
func check(m *Manifest, got map[string]FileEntry) error {
	if len(got) != len(m.Files) {
		return bad("file count differs from the manifest")
	}
	for _, want := range m.Files {
		g, ok := got[want.Path]
		if !ok || g.Size != want.Size || !strings.EqualFold(g.SHA256, want.SHA256) {
			return bad("file differs from the manifest: " + want.Path)
		}
	}
	return nil
}

// Inspect 整份讀一遍、驗過每個檔的檢查碼，回說明檔。不寫任何東西。錯誤已經是給人看的話。
func Inspect(archive string) (*Manifest, error) {
	got := map[string]FileEntry{}
	m, err := walk(archive, func(arc string, size, _ int64, r io.Reader) error {
		sum := sha256.New()
		n, err := io.Copy(sum, r)
		if err != nil {
			return bad("read " + arc + ": " + err.Error())
		}
		got[arc] = FileEntry{Path: arc, Size: n, SHA256: hex.EncodeToString(sum.Sum(nil))}
		return nil
	})
	if err == nil {
		err = check(m, got)
	}
	if err != nil {
		return nil, userError(err)
	}
	return m, nil
}

// extract 把備份檔解到 dest（要是空的或不存在）：dest/home/…、dest/secrets/push.toml；邊寫邊算檢查碼，寫完對照說明檔。
// 失敗時 dest 裡解到一半的由呼叫的人收掉。
func extract(archive, dest string) (*Manifest, error) {
	got := map[string]FileEntry{}
	m, err := walk(archive, func(arc string, size, perm int64, r io.Reader) error {
		target := filepath.Join(dest, filepath.FromSlash(arc))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		// 權限只認三種（備份時記的）：只有自己能讀的、可以執行的、一般的；秘密檔一律只有自己能讀。
		mode := os.FileMode(0o644)
		switch {
		case sectionOf(arc) == SectionSecrets:
			mode = 0o600
		case perm&0o077 == 0 && perm&0o100 != 0:
			mode = 0o700
		case perm&0o077 == 0:
			mode = 0o600
		case perm&0o111 != 0:
			mode = 0o755
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		sum := sha256.New()
		n, err := io.Copy(io.MultiWriter(out, sum), r)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		got[arc] = FileEntry{Path: arc, Size: n, SHA256: hex.EncodeToString(sum.Sum(nil))}
		return nil
	})
	if err == nil {
		err = check(m, got)
	}
	if err != nil {
		return nil, userError(err)
	}
	return m, nil
}
