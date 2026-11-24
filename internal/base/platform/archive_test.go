package platform

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

// CopyVerified：長度與 sha256 都對才算成功；失敗分得出寫不進去、斷掉、太長、檢查碼不對。
func TestCopyVerified(t *testing.T) {
	data := []byte("hello, package")
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])
	var buf bytes.Buffer
	if n, err := CopyVerified(&buf, bytes.NewReader(data), int64(len(data)), strings.ToUpper(want)); err != nil || n != int64(len(data)) || buf.String() != string(data) {
		t.Fatalf("成功的：%d %v %q", n, err, buf.String())
	}
	for name, c := range map[string]struct {
		w     io.Writer
		r     io.Reader
		size  int64
		fault CopyFault
	}{
		"寫不進去":   {failWriter{}, bytes.NewReader(data), int64(len(data)), CopyWrite},
		"讀到一半斷掉": {io.Discard, iotest.TimeoutReader(bytes.NewReader(data)), int64(len(data)), CopyCut},
		"比寫的短":   {io.Discard, bytes.NewReader(data[:5]), int64(len(data)), CopyCut},
		"比寫的長":   {io.Discard, bytes.NewReader(append(append([]byte{}, data...), 'x')), int64(len(data)), CopyTooLong},
		"檢查碼不對":  {io.Discard, bytes.NewReader([]byte("HELLO, package")), int64(len(data)), CopyMismatch},
	} {
		_, err := CopyVerified(c.w, c.r, c.size, want)
		var ce *CopyError
		if !errors.As(err, &ce) || ce.Fault != c.fault {
			t.Errorf("%s：%v", name, err)
		}
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func tarGz(t *testing.T, entries ...tar.Header) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		h := h
		body := []byte(nil)
		if h.Typeflag == tar.TypeReg {
			body = []byte("x")
			h.Size = 1
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ExtractTarGz：只收一個最上層資料夾底下的資料夾與一般檔；去掉或留著最上層；其他一律算包的內容不對，不會寫到 dest 外面。
const testMax = 1 << 20 // 測試的包都很小

func TestExtractTarGz(t *testing.T) {
	reg := func(name string) tar.Header { return tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeReg} }
	dir := func(name string) tar.Header { return tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeDir} }
	global := tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "x"}}

	good := tarGz(t, global, dir("go/"), dir("go/bin/"), reg("go/bin/go"), reg("go/VERSION"))
	strip := filepath.Join(t.TempDir(), "strip")
	if err := ExtractTarGz(good, strip, UnpackOptions{MaxBytes: testMax}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(strip, "bin", "go")); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("去掉最上層、保留執行權限：%v %v", info, err)
	}
	keep := filepath.Join(t.TempDir(), "keep")
	if err := ExtractTarGz(good, keep, UnpackOptions{Top: "go", KeepTop: true, MaxBytes: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(keep, "go", "VERSION")); err != nil {
		t.Fatal("KeepTop 要留著 go/")
	}

	for name, c := range map[string]struct {
		archive string
		opt     UnpackOptions
	}{
		"最上層名字不對":      {tarGz(t, reg("other/a")), UnpackOptions{Top: "go", MaxBytes: testMax}},
		"兩個最上層資料夾":     {tarGz(t, reg("top/a"), reg("other/b")), UnpackOptions{MaxBytes: testMax}},
		"最上層是檔":        {tarGz(t, reg("README")), UnpackOptions{MaxBytes: testMax}},
		"..":           {tarGz(t, reg("top/../../evil")), UnpackOptions{MaxBytes: testMax}},
		"絕對路徑":         {tarGz(t, reg("/etc/evil")), UnpackOptions{MaxBytes: testMax}},
		"符號連結":         {tarGz(t, reg("top/a"), tar.Header{Name: "top/b", Linkname: "a", Typeflag: tar.TypeSymlink}), UnpackOptions{MaxBytes: testMax}},
		"硬連結":          {tarGz(t, reg("top/a"), tar.Header{Name: "top/b", Linkname: "top/a", Typeflag: tar.TypeLink}), UnpackOptions{MaxBytes: testMax}},
		"同一個檔兩次":       {tarGz(t, reg("top/a"), reg("top/a")), UnpackOptions{MaxBytes: testMax}},
		"同一個名字是檔也是資料夾": {tarGz(t, reg("top/a"), reg("top/a/b")), UnpackOptions{MaxBytes: testMax}},
		"太大":           {tarGz(t, reg("top/a"), reg("top/b")), UnpackOptions{MaxBytes: 1}},
		"空的":           {tarGz(t), UnpackOptions{MaxBytes: testMax}},
	} {
		parent := t.TempDir()
		err := ExtractTarGz(c.archive, filepath.Join(parent, "out"), c.opt)
		var ue *UnpackError
		if !errors.As(err, &ue) || !ue.Bad {
			t.Errorf("%s：要算包的內容不對：%v", name, err)
		} else if ue.TooBig != (name == "太大") {
			t.Errorf("%s：TooBig 只給「超過 MaxBytes」那一種（現在 %v）", name, ue.TooBig)
		}
		ents, _ := os.ReadDir(parent)
		for _, e := range ents {
			if e.Name() != "out" {
				t.Errorf("%s：資料夾外面多了 %s", name, e.Name())
			}
		}
	}

	// 沒給上限：不解（寫程式的錯，不算包的內容不對），什麼都沒建
	noMax := filepath.Join(t.TempDir(), "nomax")
	if err := ExtractTarGz(good, noMax, UnpackOptions{}); err == nil {
		t.Error("沒給 MaxBytes 要拒絕")
	} else if ue := (*UnpackError)(nil); !errors.As(err, &ue) || ue.Bad {
		t.Errorf("沒給 MaxBytes 不是包的問題：%v", err)
	}
	if _, err := os.Stat(noMax); !os.IsNotExist(err) {
		t.Errorf("沒給 MaxBytes 不能建資料夾：%v", err)
	}

	notGz := filepath.Join(t.TempDir(), "x.tar.gz")
	if err := os.WriteFile(notGz, []byte("<html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	var ue *UnpackError
	if err := ExtractTarGz(notGz, t.TempDir(), UnpackOptions{MaxBytes: testMax}); !errors.As(err, &ue) || !ue.Bad {
		t.Errorf("不是 gzip：%v", err)
	}
	if err := ExtractTarGz(filepath.Join(t.TempDir(), "missing"), t.TempDir(), UnpackOptions{MaxBytes: testMax}); !errors.As(err, &ue) || ue.Bad {
		t.Errorf("打不開檔案是讀寫的問題，不是包的內容：%v", err)
	}
}
