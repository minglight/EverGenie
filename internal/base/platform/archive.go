package platform

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// 下載的包：照寫好的大小與 sha256 收下、安全地解開（PLT-27）。system/update 換版與 packages 下載 Go 共用這一套；
// 這裡只回錯誤的種類與英文細節（給日誌），給人看的白話由各模組自己定。

// CopyFault 是 CopyVerified 失敗的種類。
type CopyFault int

const (
	CopyWrite    CopyFault = iota + 1 // 寫不進目的地（硬碟的問題）
	CopyCut                           // 來源讀到一半斷掉，或比寫好的大小短
	CopyTooLong                       // 來源比寫好的大小長
	CopyMismatch                      // 大小對、sha256 不對
)

// CopyError：CopyVerified 沒成功。Got 是讀到的位元組數。
type CopyError struct {
	Fault CopyFault
	Got   int64
	Err   error
}

func (e *CopyError) Error() string { return e.Err.Error() }
func (e *CopyError) Unwrap() error { return e.Err }

// CopyVerified 從 src 最多讀 size+1 位元組寫進 dst，邊寫邊算 sha256；長度與檢查碼（16 進位，大小寫不拘）都要對。
// 失敗回 *CopyError（dst 裡可能已經寫了一部分，呼叫的人要收掉）。
func CopyVerified(dst io.Writer, src io.Reader, size int64, wantSHA256 string) (int64, error) {
	want := strings.ToLower(strings.TrimSpace(wantSHA256))
	sum := sha256.New()
	w := &faultWriter{w: dst}
	n, err := io.Copy(io.MultiWriter(w, sum), io.LimitReader(src, size+1))
	switch {
	case w.err != nil:
		return n, &CopyError{CopyWrite, n, fmt.Errorf("write: %w", w.err)}
	case err != nil:
		return n, &CopyError{CopyCut, n, fmt.Errorf("read: %w after %d of %d bytes", err, n, size)}
	case n < size:
		return n, &CopyError{CopyCut, n, fmt.Errorf("got %d of %d bytes", n, size)}
	case n > size:
		return n, &CopyError{CopyTooLong, n, fmt.Errorf("more than the %d bytes expected", size)}
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != want {
		return n, &CopyError{CopyMismatch, n, fmt.Errorf("sha256 %s, want %s", got, want)}
	}
	return n, nil
}

// faultWriter 記下寫入的錯（跟讀來源的錯分開：前者是硬碟的問題，後者是下載斷掉）。
type faultWriter struct {
	w   io.Writer
	err error
}

func (f *faultWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if err != nil && f.err == nil {
		f.err = err
	}
	return n, err
}

// UnpackOptions 是 ExtractTarGz 的選項。
type UnpackOptions struct {
	Top      string // 包裡唯一的最上層資料夾要叫這個名字；空＝名字不拘（但只能有一個）
	KeepTop  bool   // true：放成 dest/<最上層>/…；false：去掉最上層，內容直接放進 dest
	MaxBytes int64  // 一般檔加起來最多幾個位元組；一定要給（> 0），沒給就不解（PLT-27：每個用的地方都要有上限）
}

// UnpackError：ExtractTarGz 沒成功。Bad＝包的內容不對（不是 gzip／tar、跑出資料夾、連結、重複、其他種類、太大）；
// TooBig＝其中「一般檔加起來超過 MaxBytes」那一種（也是 Bad；用的模組可以另外講白話）；不是 Bad＝寫不進去（硬碟的問題）。
type UnpackError struct {
	Bad    bool
	TooBig bool
	Err    error
}

func (e *UnpackError) Error() string { return e.Err.Error() }
func (e *UnpackError) Unwrap() error { return e.Err }

func badPack(format string, a ...any) error {
	return &UnpackError{Bad: true, Err: fmt.Errorf(format, a...)}
}
func diskPack(err error) error { return &UnpackError{Err: err} }

// ExtractTarGz 解開 archive（tar.gz）到 dest（不在就建）。包裡只能有一個最上層資料夾；只收資料夾與一般檔：
// 絕對路徑、..、重複的檔、符號連結、硬連結與其他種類一律拒絕。符號連結整個不收，因為一串連結可以拼出指到外面的路徑，
// 逐一檢查擋不乾淨；包本來是照檢查碼驗過的，這是第二道保險。失敗回 *UnpackError，dest 裡解到一半的由呼叫的人收掉。
func ExtractTarGz(archive, dest string, opt UnpackOptions) error {
	if opt.MaxBytes <= 0 { // 寫程式的錯（不是包的內容不對）：什麼都不碰
		return diskPack(fmt.Errorf("UnpackOptions.MaxBytes must be set (got %d)", opt.MaxBytes))
	}
	f, err := os.Open(archive)
	if err != nil {
		return diskPack(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return badPack("gzip: %v", err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return diskPack(err)
	}
	tr := tar.NewReader(gz)
	top := opt.Top
	seenTop := false
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return badPack("tar: %v", err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		raw := strings.TrimPrefix(hdr.Name, "./")
		if strings.HasPrefix(raw, "/") {
			return badPack("tar entry %q is an absolute path", hdr.Name)
		}
		name := path.Clean(raw)
		if name == "." || name == ".." || strings.HasPrefix(name, "../") {
			return badPack("tar entry %q escapes the folder", hdr.Name)
		}
		first, rest, _ := strings.Cut(name, "/")
		switch {
		case top == "":
			top = first
		case first != top && opt.Top != "":
			return badPack("tar entry %q is not under %s/", hdr.Name, opt.Top)
		case first != top:
			return badPack("tar has more than one top folder (%q, %q)", top, first)
		}
		seenTop = true
		rel := rest
		if opt.KeepTop {
			rel = name
		}
		target := dest
		if rel != "" {
			target = filepath.Join(dest, filepath.FromSlash(rel))
		}
		perm := fs.FileMode(hdr.Mode).Perm()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := mkdirIn(target, perm&0o755|0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if rel == "" {
				return badPack("tar entry %q is a file at the top", hdr.Name)
			}
			if total += hdr.Size; hdr.Size < 0 || (total > opt.MaxBytes) {
				return &UnpackError{Bad: true, TooBig: true, Err: fmt.Errorf("unpacked size over %d bytes", opt.MaxBytes)}
			}
			if err := mkdirIn(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, perm&0o755|0o600)
			if errors.Is(err, fs.ErrExist) {
				return badPack("tar entry %q appears twice", hdr.Name)
			}
			if err != nil {
				return diskPack(err)
			}
			w := &faultWriter{w: out}
			_, err = io.Copy(w, tr)
			cerr := out.Close()
			switch {
			case w.err != nil:
				return diskPack(w.err)
			case err != nil:
				return badPack("unpack %s: %v", hdr.Name, err)
			case cerr != nil:
				return diskPack(cerr)
			}
		default:
			return badPack("tar entry %q has unsupported type %c", hdr.Name, hdr.Typeflag)
		}
	}
	if !seenTop {
		return badPack("empty tar")
	}
	return nil
}

// mkdirIn 建資料夾；路上有一般檔擋著（包裡同一個名字既是檔又是資料夾）算包的內容不對。
func mkdirIn(p string, perm fs.FileMode) error {
	err := os.MkdirAll(p, perm)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ENOTDIR) || errors.Is(err, fs.ErrExist) {
		return badPack("tar has both a file and a folder at %s", filepath.Base(p))
	}
	return diskPack(err)
}
