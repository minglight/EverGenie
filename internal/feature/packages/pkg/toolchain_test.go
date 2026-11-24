package pkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fakeGoVersion = "go1.27.4"

var realGoDownload = flag.Bool("real-go-download", false, "真的從 Go 官方下載寫死的那一版（TestOfficialGoDownload）")

// fakeGoScript 是假的 go：env GOVERSION 印版本，build -buildvcs=false -o <檔> 寫一個小檔（測試不碰真的 go）。
func fakeGoScript(version string) string {
	return "#!/bin/sh\nif [ \"$1\" = env ]; then echo " + version + "; exit 0; fi\n" +
		"if [ \"$1\" = build ] && [ \"$2\" = -buildvcs=false ] && [ \"$3\" = -o ]; then echo built > \"$4\"; chmod +x \"$4\"; exit 0; fi\nexit 1\n"
}

type tarFile struct {
	name string
	body string
	mode int64
	typ  byte
	link string
}

func goTarball(t *testing.T, files []tarFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		typ := f.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: f.name, Mode: f.mode, Typeflag: typ, Linkname: f.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(f.body))
		}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(f.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// goodGoFiles 是一個像官方包的假 Go：go/VERSION、go/bin/go（假的 go）。
func goodGoFiles(version string) []tarFile {
	return []tarFile{
		{name: "go/", typ: tar.TypeDir, mode: 0o755},
		{name: "go/VERSION", body: version + "\ntime 2026-11-01T00:00:00Z\n"},
		{name: "go/bin/", typ: tar.TypeDir, mode: 0o755},
		{name: "go/bin/go", body: fakeGoScript(version), mode: 0o755},
	}
}

// fakeGo 是本機 http 上的假下載點＋指到它的 goTools（工具資料夾在暫存資料夾，PATH 裡沒有 go）。
type fakeGo struct {
	tools *goTools
	body  []byte
	hits  atomic.Int32
	pin   goPin
}

const fakeGoFile = "go-fake.darwin-test.tar.gz"

func newFakeGo(t *testing.T, body []byte) *fakeGo {
	t.Helper()
	f := &fakeGo{body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if r.URL.Path != "/dl/"+fakeGoFile {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(f.body)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(body)
	f.pin = goPin{Version: fakeGoVersion, Base: srv.URL + "/dl/", Files: map[string]goFile{"test-arch": {Name: fakeGoFile, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}}}
	f.tools = &goTools{
		dir:      filepath.Join(t.TempDir(), "tools"),
		sdk:      fakeSDK(t, "1.24"),
		platform: "test-arch",
		pin:      func() (goPin, error) { return f.pin, nil },
		lookPath: func(string) (string, error) { return "", exec.ErrNotFound },
		version:  goVersionOf,
		free:     func(string) (uint64, error) { return 1 << 40, nil },
		client:   srv.Client(),
	}
	return f
}

// fakeSDK 做一份「程式旁邊附的 sdk」：只有 go.mod（模組名是 sdk 的，go 那一行是 goLine）與一個檔。
func fakeSDK(t *testing.T, goLine string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sdk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"go.mod": "module " + sdkModulePath + "\n\ngo " + goLine + "\n",
		"doc.go": "package sdk\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// goLeftovers 列出工具資料夾裡除了 go/ 之外的東西（下載或解到一半的暫存不能留）。
func goLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if e.Name() != "go" {
			out = append(out, e.Name())
		}
	}
	return out
}

var englishWord = regexp.MustCompile(`[A-Za-z]{3,}`)

// plainOnly：給人看的話不能夾英文（Go 與 MB 這種名字除外）。
func plainOnly(t *testing.T, s string) {
	t.Helper()
	for _, w := range englishWord.FindAllString(s, -1) {
		t.Errorf("給人看的話夾了英文 %q：%s", w, s)
	}
}

// TestGoToolsInstall：工具資料夾與 PATH 都沒有 go → status 說沒有、可以幫忙下載、問主人的那一句；install 對過檢查碼才放進 tools/go，
// 之後 status 用的是工具資料夾那支；不留暫存。原本有一支太舊的會被換掉。
func TestGoToolsInstall(t *testing.T) {
	f := newFakeGo(t, goTarball(t, goodGoFiles(fakeGoVersion)))
	st := f.tools.status()
	if st.Usable || !st.Installable || st.Detail != goMsgNone || st.Offer != fakeGoVersion || st.Need != minGoVersion ||
		!strings.Contains(st.Ask, "要先裝編譯工具（Go，約 1MB，裝在程式自己的資料夾裡），要我裝嗎？") {
		t.Fatalf("沒有 go 的 status：%+v", st)
	}
	if f.hits.Load() != 0 {
		t.Fatal("status 不能下載")
	}
	// 原本有一支太舊的：不能用、會被換掉。
	old := filepath.Join(f.tools.dir, "go", "bin", "go")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte(fakeGoScript("go1.20.3")), 0o755); err != nil {
		t.Fatal(err)
	}
	if st := f.tools.status(); st.Usable || !strings.Contains(st.Detail, "太舊") || !strings.Contains(st.Detail, "go1.20.3") {
		t.Fatalf("太舊的 status：%+v", st)
	}
	v, err := f.tools.install(t.Context())
	if err != nil || v != fakeGoVersion {
		t.Fatalf("install = %q %v", v, err)
	}
	st = f.tools.status()
	if !st.Usable || st.Source != "tools" || st.Path != old || st.Version != fakeGoVersion || st.Ask != "" {
		t.Fatalf("裝好之後的 status：%+v", st)
	}
	if got, err := f.tools.look(); err != nil || got != old {
		t.Fatalf("look = %q %v", got, err)
	}
	if l := goLeftovers(t, f.tools.dir); len(l) != 0 {
		t.Fatalf("留下暫存：%v", l)
	}
}

// TestGoToolsInstallFailuresLeaveNothing：下載不到、檢查碼或大小對不上、內容不對（連結、跑出 go/ 外面、版本不對）、空間不夠、
// 沒有這台晶片的包：白話＋「沒有裝」＋下一步，不夾英文；不留暫存，原本的 tools/go 一個位元都不動。
func TestGoToolsInstallFailuresLeaveNothing(t *testing.T) {
	good := goTarball(t, goodGoFiles(fakeGoVersion))
	cases := []struct {
		name  string
		setup func(f *fakeGo)
		want  string
	}{
		{"檢查碼不符", func(f *fakeGo) {
			file := f.pin.Files["test-arch"]
			file.SHA256 = strings.Repeat("0", 64)
			f.pin.Files = map[string]goFile{"test-arch": file}
		}, "檢查碼對不上"},
		{"比寫的長", func(f *fakeGo) { f.body = append(append([]byte{}, good...), 0) }, "檢查碼對不上"},
		{"比寫的短", func(f *fakeGo) { f.body = good[:len(good)-10] }, "斷掉了"},
		{"下載不到", func(f *fakeGo) {
			file := f.pin.Files["test-arch"]
			file.Name = "missing.tar.gz"
			f.pin.Files = map[string]goFile{"test-arch": file}
		}, "下載不到 Go"},
		{"有連結", func(f *fakeGo) {
			f.setBody(t, goTarball(t, append(goodGoFiles(fakeGoVersion), tarFile{name: "go/lib", typ: tar.TypeSymlink, link: "/etc"})))
		}, "內容不對"},
		{"跑出 go 外面", func(f *fakeGo) {
			f.setBody(t, goTarball(t, append(goodGoFiles(fakeGoVersion), tarFile{name: "go/../escape", body: "x"})))
		}, "內容不對"},
		{"兩個最上層", func(f *fakeGo) {
			f.setBody(t, goTarball(t, append(goodGoFiles(fakeGoVersion), tarFile{name: "other/x", body: "x"})))
		}, "內容不對"},
		{"重複的檔", func(f *fakeGo) {
			f.setBody(t, goTarball(t, append(goodGoFiles(fakeGoVersion), tarFile{name: "go/VERSION", body: "x"})))
		}, "內容不對"},
		{"不是 gzip", func(f *fakeGo) { f.setBody(t, []byte("not a tarball")) }, "內容不對"},
		{"沒有 go 執行檔", func(f *fakeGo) {
			f.setBody(t, goTarball(t, []tarFile{{name: "go/VERSION", body: fakeGoVersion}}))
		}, "內容不對"},
		{"版本不對", func(f *fakeGo) { f.setBody(t, goTarball(t, goodGoFiles("go1.26.0"))) }, "內容不對"},
		{"空間不夠", func(f *fakeGo) { f.tools.free = func(string) (uint64, error) { return 10, nil } }, "空間不夠"},
		{"沒有這台晶片的包", func(f *fakeGo) { f.tools.platform = "plan9-mips" }, "晶片沒有可以下載的 Go"},
		{"設定讀不到", func(f *fakeGo) { f.tools.pin = func() (goPin, error) { return goPin{}, errors.New("bad json") } }, "設定讀不到"},
		// 下載來源的規則（platform PLT-25）：別台電腦的 http、帳號密碼都不收，連都不連
		{"下載點不照規則", func(f *fakeGo) { f.pin.Base = "http://example.com/dl/" }, "設定讀不到"},
		{"https 被轉成 http", func(f *fakeGo) {
			plain := f.pin.Base + fakeGoFile // 本機 http 上真的有這個包：只差在是被 https 轉過去的
			tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, plain, http.StatusFound)
			}))
			t.Cleanup(tls.Close)
			f.tools.client = tls.Client()
			f.pin.Base = tls.URL + "/dl/"
		}, "轉到不安全的地方"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeGo(t, good)
			c.setup(f)
			// 原本的 tools/go（壞的也一樣）不能被動到。
			keep := filepath.Join(f.tools.dir, "go", "bin", "go")
			if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(keep, []byte("keep me"), 0o755); err != nil {
				t.Fatal(err)
			}
			v, err := f.tools.install(t.Context())
			var fail *goFail
			if err == nil || v != "" || !errors.As(err, &fail) {
				t.Fatalf("要失敗：%q %v", v, err)
			}
			if !strings.Contains(fail.plain, c.want) || !strings.Contains(fail.plain, "沒有裝；") {
				t.Fatalf("訊息：%q", fail.plain)
			}
			// 重試也一樣的是 CONFLICT（never），可以重試的是 UNAVAILABLE
			never := map[string]bool{"沒有這台晶片的包": true, "設定讀不到": true, "下載點不照規則": true, "https 被轉成 http": true}[c.name]
			if fail.never != never {
				t.Fatalf("重試也一樣要是 %v：%+v", never, fail)
			}
			plainOnly(t, fail.plain)
			if b, err := os.ReadFile(keep); err != nil || string(b) != "keep me" {
				t.Fatalf("原本的 tools/go 被動到：%q %v", b, err)
			}
			if l := goLeftovers(t, f.tools.dir); len(l) != 0 {
				t.Fatalf("留下暫存：%v", l)
			}
		})
	}
}

func (f *fakeGo) setBody(t *testing.T, body []byte) {
	t.Helper()
	f.body = body
	sum := sha256.Sum256(body)
	file := f.pin.Files["test-arch"]
	file.SHA256, file.Size = hex.EncodeToString(sum[:]), int64(len(body))
	f.pin.Files = map[string]goFile{"test-arch": file}
}

// TestGoToolsInterruptedDownload：下載到一半斷掉（連線被關）＝白話「斷掉了」、不留暫存。
func TestGoToolsInterruptedDownload(t *testing.T) {
	f := newFakeGo(t, goTarball(t, goodGoFiles(fakeGoVersion)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write([]byte("partial"))
	}))
	t.Cleanup(srv.Close)
	f.pin.Base = srv.URL
	file := f.pin.Files["test-arch"]
	file.Size = 100000
	f.pin.Files = map[string]goFile{"test-arch": file}
	_, err := f.tools.install(t.Context())
	var fail *goFail
	if !errors.As(err, &fail) || !strings.Contains(fail.plain, "下載到一半斷掉了") {
		t.Fatalf("要說斷掉：%v", err)
	}
	if l := goLeftovers(t, f.tools.dir); len(l) != 0 {
		t.Fatalf("留下暫存：%v", l)
	}
}

// TestGoToolsFindOrder：工具資料夾 ＞ PATH；工具資料夾那支壞了就用 PATH 的；都太舊就說太舊；原始碼版不幫忙下載。
func TestGoToolsFindOrder(t *testing.T) {
	dir := t.TempDir()
	write := func(p, version string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		body := fakeGoScript(version)
		if version == "" {
			body = "#!/bin/sh\nexit 1\n"
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	pathGo := write(filepath.Join(dir, "path", "go"), "go1.25.1")
	toolsGo := write(filepath.Join(dir, "tools", "go", "bin", "go"), "go1.27.4")
	tl := &goTools{dir: filepath.Join(dir, "tools"), platform: "x", pin: func() (goPin, error) { return officialGo, nil },
		lookPath: func(string) (string, error) { return pathGo, nil }, version: goVersionOf}
	if f := tl.find(); f.path != toolsGo || f.source != "tools" || f.version != "go1.27.4" {
		t.Fatalf("工具資料夾要先：%+v", f)
	}
	write(toolsGo, "")
	if f := tl.find(); f.path != pathGo || f.source != "path" || f.version != "go1.25.1" {
		t.Fatalf("工具資料夾那支壞了要用 PATH 的：%+v", f)
	}
	write(pathGo, "go1.21.0")
	if f := tl.find(); f.problem != goMsgBroken {
		t.Fatalf("第一個問題是工具資料夾那支跑不起來：%+v", f)
	}
	if err := os.Remove(toolsGo); err != nil {
		t.Fatal(err)
	}
	if f := tl.find(); !strings.Contains(f.problem, "太舊") {
		t.Fatalf("PATH 的太舊：%+v", f)
	}
	// 原始碼版：不幫忙下載，status 與編譯的訊息都說要自己裝。
	src := &goTools{platform: "x", pin: func() (goPin, error) { return officialGo, nil }, lookPath: func(string) (string, error) { return "", exec.ErrNotFound }, version: goVersionOf}
	st := src.status()
	if st.Usable || st.Installable || st.Ask != "" || !strings.Contains(st.Detail, "原始碼版") {
		t.Fatalf("原始碼版的 status：%+v", st)
	}
	if _, err := src.install(context.Background()); err == nil || !strings.Contains(err.Error(), "原始碼版") {
		t.Fatalf("原始碼版不下載：%v", err)
	}
	if txt := src.missingText(goMsgNone); !strings.Contains(txt, "原始碼版") || strings.Contains(txt, "要我裝嗎") {
		t.Fatalf("原始碼版的編譯訊息：%q", txt)
	}
}

// TestCleanGoLeftovers：上次被中斷、很久沒動的暫存收掉；剛寫的（可能正在下載）與 go/ 不動。
func TestCleanGoLeftovers(t *testing.T) {
	dir := t.TempDir()
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	mine := fmt.Sprintf(".go-download-%d-2", os.Getpid())
	gone := fmt.Sprintf(".go-unpack-%d-3", dead.Process.Pid)
	for _, n := range []string{".go-download-1-1", ".go-unpack-1-1", ".go-old-1-1", mine, gone, "go", "other"} {
		if err := os.MkdirAll(filepath.Join(dir, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stale := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, n := range []string{".go-download-1-1", ".go-unpack-1-1", ".go-old-1-1", "go", "other"} {
		if err := os.Chtimes(filepath.Join(dir, n), stale, stale); err != nil {
			t.Fatal(err)
		}
	}
	cleanGoLeftovers(dir)
	ents, _ := os.ReadDir(dir)
	var got []string
	for _, e := range ents {
		got = append(got, e.Name())
	}
	if strings.Join(got, ",") != mine+",go,other" {
		t.Fatalf("剩下：%v", got)
	}
}

// TestGoVersionHelpers：版本比較與給人看的大小。
func TestGoVersionHelpers(t *testing.T) {
	for v, want := range map[string]bool{"go1.24": true, "go1.24.0": true, "go1.27.4": true, "go1.25rc1": true, "go2.0": true, "go1.23.9": false, "go1": false, "devel": false, "": false} {
		if got := goAtLeast(v, minGoVersion); got != want {
			t.Errorf("goAtLeast(%q) = %v", v, got)
		}
	}
	// 連修訂版都比（sdk 寫 1.24.3 時 1.24.0 不夠）
	for v, want := range map[string]bool{"go1.24.0": false, "go1.24.3": true, "go1.24.5": true, "go1.25.0": true, "go1.24rc1": false} {
		if got := goAtLeast(v, "go1.24.3"); got != want {
			t.Errorf("goAtLeast(%q, go1.24.3) = %v", v, got)
		}
	}
	for n, want := range map[int64]int{68231011: 70, 71756705: 70, 341155055: 340, 1500: 1, 3_400_000: 3} {
		if got := sizeMB(n); got != want {
			t.Errorf("sizeMB(%d) = %d，要 %d", n, got, want)
		}
	}
}

// TestMinGoVersionMatchesSDK：最低版本跟 sdk/go.mod 的 go 那一行一樣（套件只 import sdk）。
func TestMinGoVersionMatchesSDK(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "sdk", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^go (\S+)`).FindStringSubmatch(string(b)) // 整行一樣（含修訂版）：找 Go 時照它比
	if m == nil || "go"+m[1] != minGoVersion {
		t.Fatalf("sdk/go.mod 的 go 是 %v，minGoVersion 是 %s", m, minGoVersion)
	}
}

// TestOfficialGoPin：寫死的那一版有兩種 Mac 晶片的包，檔名跟版本對得上，檢查碼是 sha256，下載點是 Go 官方；版本 ≥ 最低版本。
func TestOfficialGoPin(t *testing.T) {
	if officialGo.Base != "https://go.dev/dl/" || !goAtLeast(officialGo.Version, minGoVersion) {
		t.Fatalf("pin：%+v", officialGo)
	}
	hexsum := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, arch := range []string{"arm64", "amd64"} {
		f, ok := officialGo.Files["darwin-"+arch]
		if !ok || f.Name != officialGo.Version+".darwin-"+arch+".tar.gz" || !hexsum.MatchString(f.SHA256) || f.Size < 10_000_000 {
			t.Errorf("darwin-%s：%+v", arch, f)
		}
	}
}

// TestOfficialGoDownload：真的從 Go 官方下載寫死的那一版（兩種晶片都對大小與 sha256，這台的晶片再完整裝一次、問版本）。
// 要網路、約 140MB，平常跳過：換版本時跑一次（runbooks/release.md）：
// go test ./internal/feature/packages/pkg/ -run TestOfficialGoDownload -count=1 -args -real-go-download
func TestOfficialGoDownload(t *testing.T) {
	if !*realGoDownload {
		t.Skip("真的下載 Go：加 -args -real-go-download 才跑")
	}
	tl := &goTools{dir: filepath.Join(t.TempDir(), "tools"), platform: runtime.GOOS + "-" + runtime.GOARCH,
		pin: func() (goPin, error) { return officialGo, nil }, lookPath: func(string) (string, error) { return "", exec.ErrNotFound },
		version: goVersionOf, free: freeSpace, client: http.DefaultClient}
	for key, f := range officialGo.Files {
		if key == tl.platform {
			continue
		}
		if err := os.MkdirAll(tl.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(tl.dir, ".go-download-check")
		if err := tl.download(t.Context(), officialGo.Base, f, dst); err != nil {
			t.Fatalf("%s：%v（%v）", key, err, err.(*goFail).detail)
		}
		_ = os.Remove(dst)
	}
	v, err := tl.install(t.Context())
	if err != nil {
		var fail *goFail
		if errors.As(err, &fail) {
			t.Fatalf("%s（%v）", fail.plain, fail.detail)
		}
		t.Fatal(err)
	}
	st := tl.status()
	if v != officialGo.Version || !st.Usable || st.Source != "tools" || st.Version != officialGo.Version {
		t.Fatalf("install = %q，status %+v", v, st)
	}
	t.Logf("真的下載、對過檢查碼、裝好：%s（%s）", st.Version, st.Path)
}

// 正式程式不讀測試開關（DEV-155）：環境變數換不掉寫死的 Go；只有測試編的版本（-ldflags -X testGoPinFile）讀得到換掉的那份。
func TestGoPinOnlyFromTestBuild(t *testing.T) {
	if testGoPinFile != "" {
		t.Fatalf("testGoPinFile 平常要是空的：%q", testGoPinFile)
	}
	t.Setenv("EVERGENIE_GO_DOWNLOAD_PIN", filepath.Join(t.TempDir(), "沒有這個檔.json"))
	got, err := buildPin()
	if err != nil || got.Version != officialGo.Version || got.Base != officialGo.Base {
		t.Fatalf("正式的版本要用寫死的 Go：%+v %v", got, err)
	}
	f := filepath.Join(t.TempDir(), "pin.json")
	if err := os.WriteFile(f, []byte(`{"version":"go9.9.9","base":"http://127.0.0.1:1/godl/","files":{"darwin-arm64":{"name":"go.tgz","sha256":"`+strings.Repeat("0", 64)+`","size":3}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old := testGoPinFile
	testGoPinFile = f
	t.Cleanup(func() { testGoPinFile = old })
	got, err = buildPin()
	if err != nil || got.Version != "go9.9.9" || got.Base != "http://127.0.0.1:1/godl/" || got.Files["darwin-arm64"].Size != 3 {
		t.Fatalf("測試編的版本要讀那份 JSON：%+v %v", got, err)
	}
	testGoPinFile = filepath.Join(t.TempDir(), "沒有.json")
	if _, err := buildPin(); err == nil {
		t.Fatal("讀不到那份 JSON 要回錯")
	}
}
