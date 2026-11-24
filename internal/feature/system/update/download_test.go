package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/host"
	"github.com/minglight/evergenie/sdk"
)

// 下載版的極小假環境：版本清單與發行檔由 httptest 提供（也測 file://）；每一版的 bin/evergenie 是 shell 腳本：
// version 印 VERSION、serve 把自己的 pid 寫進 <版本>/serving 再等 SIGTERM（版本資料夾裡有 serve-fail 就印一行英文的當掉訊息、結束 3）、
// migrate-home 照開關檔試跑失敗或改家的檔案。不需要 git、go、make。真的整套走 make upgrade-download-test。
const fakeDownloadBin = `#!/bin/sh
d="$(cd "$(dirname "$0")/.." && pwd)"
case "$1" in
  version) cat "$d/VERSION";;
  serve) if [ -f "$d/serve-fail" ]; then echo "panic: runtime error: index out of range" >&2; exit 3; fi; trap "exit 0" TERM; echo $$ > "$d/serving"; while :; do sleep 0.05; done;;
  migrate-home)
    if [ "$2" = "--preview" ]; then
      if [ -f "$d/preview-fail" ]; then echo "panic: goroutine 1 [running]" >&2; exit 1; fi
      exit 0
    fi
    if [ -f "$d/migrate-ok" ]; then echo migrated > "$EVERGENIE_HOME/vault/tasks/data.md"; mkdir -p "$EVERGENIE_HOME/settings"; printf 'schema = 1\napplied = ["pkg.demo.one"]\n' > "$EVERGENIE_HOME/settings/migrations.toml"; fi;;
esac
`

// releaseTar 做一個發行檔：最上層 evergenie-<版本>/，裡面 bin/evergenie、VERSION、packages/official/secretaries/package.toml，與開關檔。
// flags 是包裡多放的檔：只寫路徑＝內容 "x"、權限 0644 的旗標檔（假的執行檔看它決定怎麼做）；寫成「路徑=內容」＝那個內容、權限 0755。
func releaseTar(t *testing.T, version string, flags ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	top := "evergenie-" + version + "/"
	add := func(name string, mode int64, body string) {
		if err := tw.WriteHeader(&tar.Header{Name: top + name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"", "bin/", "packages/", "packages/official/", "packages/official/secretaries/"} {
		if err := tw.WriteHeader(&tar.Header{Name: top + d, Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
			t.Fatal(err)
		}
	}
	add("bin/evergenie", 0o755, fakeDownloadBin)
	add("VERSION", 0o644, version+"\n")
	add("packages/official/secretaries/package.toml", 0o644, "id = \"secretaries\"\n")
	for _, f := range flags {
		if name, body, ok := strings.Cut(f, "="); ok { // 「路徑=內容」：可以執行的檔（例如 fixed/ 裡的）
			add(name, 0o755, body)
			continue
		}
		add(f, 0o644, "x")
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type dlEnv struct {
	t    *testing.T
	app  string
	home string
	srv  *httptest.Server

	mu       sync.Mutex
	files    map[string][]byte // 網址路徑 → 內容
	cut      map[string]bool   // 這些路徑只送一半就斷線
	requests []string
}

func newDLEnv(t *testing.T) *dlEnv {
	t.Helper()
	t.Setenv(host.ServiceModeEnv, "process")
	root := t.TempDir()
	e := &dlEnv{t: t, app: filepath.Join(root, "app"), files: map[string][]byte{}, cut: map[string]bool{}}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		body, ok := e.files[r.URL.Path]
		cut := e.cut[r.URL.Path]
		e.requests = append(e.requests, r.URL.Path)
		e.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		if cut {
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", len(body))
			_, _ = buf.Write(body[:len(body)/2])
			_ = buf.Flush()
			_ = conn.Close()
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(e.srv.Close)
	// 裝好 0.1.0：解開到 versions/0.1.0，current 指過去。
	if err := os.MkdirAll(filepath.Join(e.app, versionsDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "first.tar.gz")
	if err := os.WriteFile(archive, releaseTar(t, "0.1.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractRelease(archive, filepath.Join(e.app, versionsDirName, "0.1.0")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(versionsDirName, "0.1.0"), filepath.Join(e.app, currentLinkName)); err != nil {
		t.Fatal(err)
	}
	e.home = testHome(t)
	if err := os.MkdirAll(filepath.Join(e.home, "vault", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.home, "vault", "tasks", "data.md"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

// publish 把 version 的發行檔放上網站並寫版本清單（url 相對於清單）；mutate 可以在寫出去之前改清單。
func (e *dlEnv) publish(version, channel string, mutate func(m *ReleaseManifest), flags ...string) {
	e.t.Helper()
	data := releaseTar(e.t, version, flags...)
	sum := sha256.Sum256(data)
	name := "evergenie-" + version + "-darwin-arm64.tar.gz"
	m := &ReleaseManifest{Schema: 1, Version: version, Channel: channel, ReleasedAt: "2026-11-13T00:00:00Z", Notes: "## 新增\n- " + version,
		Assets: map[string]ReleaseAsset{"darwin-arm64": {URL: "rel/" + name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}}}
	if mutate != nil {
		mutate(m)
	}
	mj, _ := json.Marshal(m)
	e.mu.Lock()
	e.files["/rel/"+name] = data
	e.files["/manifest.json"] = mj
	e.mu.Unlock()
}

func (e *dlEnv) current() string { return currentVersionDir(e.app) }

func (e *dlEnv) versions() []string {
	entries, _ := os.ReadDir(filepath.Join(e.app, versionsDirName))
	var out []string
	for _, x := range entries {
		out = append(out, x.Name())
	}
	return out
}

func (e *dlEnv) exe() string { return filepath.Join(e.app, currentLinkName, "bin", "evergenie") }

func (e *dlEnv) binVersion() string {
	out, err := exec.Command(e.exe(), "version").Output()
	if err != nil {
		return "（讀不到）"
	}
	return strings.TrimSpace(string(out))
}

func (e *dlEnv) upgradeLog() string {
	data, _ := os.ReadFile(platform.RuntimeDir(e.home, "logs", "upgrade.log"))
	return string(data)
}

type dlNote struct{ title, detail string }

// upgrader：真的 Service（process 模式，跑 current/bin/evergenie）；Verify 看 current 的版本，再等到「服務記的 pid 寫進了 serving」
// （開好了）或服務已經結束（開不起來的版本一下子就結束了）。
func (e *dlEnv) upgrader() (*Upgrader, *bytes.Buffer, *[]dlNote) {
	var notes []dlNote
	var log bytes.Buffer
	svc := host.Service{Runner: host.ExecRunner{}, Spec: host.ServiceSpec{Exe: e.exe(), Home: e.home}}
	e.t.Cleanup(func() { _, _ = svc.Stop(context.Background()) })
	u := &Upgrader{Home: e.home, Core: e.app, From: "0.1.0", Channel: platform.UpdateChannelRelease, Out: &log,
		Download: true, Source: e.srv.URL + "/manifest.json", Platform: "darwin-arm64",
		Notify: func(title, detail string) { notes = append(notes, dlNote{title, detail}) },
		Verify: func(ctx context.Context, want string) error {
			if got := e.binVersion(); got != want {
				return &ErrUpgrade{Message: "執行檔版本是 " + got + "，應該是 " + want}
			}
			for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
				if !svc.Status(ctx).Running {
					return &ErrUpgrade{Message: "重開之後服務沒有在跑"}
				}
				ready, _ := os.ReadFile(filepath.Join(e.app, currentLinkName, "serving"))
				if pid := servePID(e.t, e.home); pid != "" && strings.TrimSpace(string(ready)) == pid {
					return nil
				}
				if time.Now().After(deadline) {
					return &ErrUpgrade{Message: "等了十秒服務還沒開好"}
				}
			}
		}}
	if _, err := svc.Start(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	return u, &log, &notes
}

func servePID(t *testing.T, home string) string {
	t.Helper()
	data, _ := os.ReadFile(platform.RuntimeDir(home, "serve.pid"))
	return strings.TrimSpace(string(data))
}

var asciiWord = regexp.MustCompile(`[A-Za-z]{2,}`)

// pathLike 是訊息裡的檔案路徑（通知最後一句「細節記在升級日誌：<路徑>」給秘書查）；路徑不算夾英文。
var pathLike = regexp.MustCompile(`/[^\s（）：，。「」]+`)

// english：拿掉路徑之後還有英文字（給主人的話不能有）。
func english(s string) bool { return asciiWord.MatchString(pathLike.ReplaceAllString(s, "")) }

// 找安裝：current 指到 versions/<版本>、那一版有 VERSION、不是 git repo；經過 current 或別的符號連結叫起來都找得到同一個安裝資料夾。
// [update] core 指到安裝資料夾也算。原始碼版（有 .git 的 repo）照舊由 FindCore 找，FindCore 不認下載版。
func TestFindInstallDownloadLayout(t *testing.T) {
	e := newDLEnv(t)
	app, _ := filepath.EvalSymlinks(e.app)
	if got := FindInstall(nil, e.exe()); got != app {
		t.Fatalf("經過 current：%q，應該是 %q", got, app)
	}
	link := filepath.Join(t.TempDir(), "evergenie")
	if err := os.Symlink(e.exe(), link); err != nil {
		t.Fatal(err)
	}
	if got := FindInstall(nil, link); got != app {
		t.Fatalf("經過另一個符號連結：%q", got)
	}
	if got := FindCore(nil, e.exe()); got != "" {
		t.Fatalf("FindCore 只認原始碼版：%q", got)
	}
	h, err := platform.LoadHome(e.home)
	if err != nil {
		t.Fatal(err)
	}
	h.Update.Core = e.app
	if got := FindInstall(h, ""); got != e.app {
		t.Fatalf("[update] core 指到安裝資料夾：%q", got)
	}
	h.Update.Core = t.TempDir()
	if got := FindInstall(h, e.exe()); got != "" {
		t.Fatalf("[update] core 指到別的地方就不照執行檔找：%q", got)
	}
	if !IsDownloadInstall(e.app) {
		t.Fatal("應該是下載版")
	}
	if err := os.Mkdir(filepath.Join(e.app, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if IsDownloadInstall(e.app) {
		t.Fatal("有 .git 就不是下載版")
	}
}

// 下載版的檢查讀版本清單：有新版填 Target（Tag 就是版本號）；測試過的版本只給 safe 頻道；跳過的、沒有這台晶片的包都寫白話原因；
// 沒設定來源寫在 Detail；清單讀不到回白話錯誤、不蓋掉上一次的結果，英文細節只進 upgrade.log。
func TestDownloadCheck(t *testing.T) {
	ctx := context.Background()
	e := newDLEnv(t)
	u, _, _ := e.upgrader()
	e.publish("0.2.0", "release", nil)
	chk, err := u.Check(ctx)
	if err != nil || chk.Target != "0.2.0" || chk.Tag != "0.2.0" || !strings.Contains(chk.Notes, "新增") || chk.Detail != "" {
		t.Fatalf("有新版：%+v %v", chk, err)
	}
	e.publish("0.2.1", "safe", nil)
	if chk, _ = u.Check(ctx); chk.Target != "" || !strings.Contains(chk.Detail, "只升正式版") {
		t.Fatalf("release 頻道不升測試版：%+v", chk)
	}
	u.Channel = platform.UpdateChannelSafe
	if chk, _ = u.Check(ctx); chk.Target != "0.2.1" {
		t.Fatalf("safe 頻道升測試版：%+v", chk)
	}
	u.Channel = platform.UpdateChannelRelease
	_ = AddUpgradeSkipped(e.home, "0.2.2")
	e.publish("0.2.2", "release", nil)
	if chk, _ = u.Check(ctx); chk.Target != "" || !strings.Contains(chk.Detail, "跳過") {
		t.Fatalf("跳過：%+v", chk)
	}
	e.publish("0.3.0", "release", func(m *ReleaseManifest) { delete(m.Assets, "darwin-arm64") })
	if chk, _ = u.Check(ctx); chk.Target != "" || !strings.Contains(chk.Detail, "晶片") {
		t.Fatalf("沒有這台晶片的包：%+v", chk)
	}
	e.publish("0.4.0", "release", nil)
	if chk, _ = u.Check(ctx); chk.Target != "0.4.0" {
		t.Fatalf("%+v", chk)
	}
	e.mu.Lock()
	delete(e.files, "/manifest.json")
	e.mu.Unlock()
	if _, err := u.Check(ctx); err == nil || !strings.Contains(err.Error(), "讀不到版本清單") || asciiWord.MatchString(err.Error()) {
		t.Fatalf("清單讀不到：%v", err)
	}
	if saved, _ := ReadUpgradeCheck(e.home); saved == nil || saved.Target != "0.4.0" {
		t.Fatalf("讀不到清單不能蓋掉上一次的結果：%+v", saved)
	}
	if !strings.Contains(e.upgradeLog(), "404") {
		t.Fatalf("英文細節要進 upgrade.log：%s", e.upgradeLog())
	}
	u.Source = ""
	if chk, err = u.Check(ctx); err != nil || chk.Target != "" || !strings.HasPrefix(chk.Detail, msgNoSource) || !strings.Contains(chk.Detail, nextSource) || asciiWord.MatchString(chk.Detail) {
		t.Fatalf("沒設定來源（白話、不寫設定名稱）：%+v %v", chk, err)
	}
	// file:// 的清單也行，相對的 url 照清單所在的資料夾找。
	dir := t.TempDir()
	e.publish("0.5.0", "release", nil)
	e.mu.Lock()
	for p, b := range e.files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		_ = os.WriteFile(filepath.Join(dir, p), b, 0o644)
	}
	e.mu.Unlock()
	u.Source = "file://" + filepath.ToSlash(filepath.Join(dir, "manifest.json"))
	if chk, err = u.Check(ctx); err != nil || chk.Target != "0.5.0" {
		t.Fatalf("file://：%+v %v", chk, err)
	}
	if err := u.Apply(ctx, "v0.5.0"); err != nil || e.current() != "0.5.0" {
		t.Fatalf("file:// 升級（--to 帶 v 也認）：%v current=%s", err, e.current())
	}
}

// 下載版升級：current 換到新版、舊版資料夾留著、服務跑 current/bin/evergenie；手動退回換回舊版資料夾；
// 主人確認之後只留現在這一版、不能再退回。退回、確認都沿用 history、通知。
func TestDownloadApplyRollbackAccept(t *testing.T) {
	ctx := context.Background()
	e := newDLEnv(t)
	u, log, notes := e.upgrader()
	e.publish("0.2.0", "release", nil, "migrate-ok")
	pid := servePID(t, e.home)
	if err := u.Apply(ctx, ""); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if e.current() != "0.2.0" || e.binVersion() != "0.2.0" || strings.Join(e.versions(), " ") != "0.1.0 0.2.0" {
		t.Fatalf("升級後 current=%s bin=%s versions=%v", e.current(), e.binVersion(), e.versions())
	}
	if servePID(t, e.home) == pid {
		t.Fatal("服務要重開")
	}
	link, _ := os.Readlink(filepath.Join(e.app, currentLinkName))
	if link != filepath.Join(versionsDirName, "0.2.0") {
		t.Fatalf("current 要是相對的連結：%s", link)
	}
	hist := ReadUpgradeHistory(e.home)
	if len(hist) != 1 || hist[0].Install != InstallDownload || hist[0].From != "0.1.0" || hist[0].To != "0.2.0" || hist[0].FromCommit != "" || len(hist[0].Migrations) != 1 {
		t.Fatalf("history：%+v", hist)
	}
	if st, _ := ReadUpgradeState(e.home); st == nil || st.Phase != UpgradePhaseDone {
		t.Fatalf("state：%+v", st)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0].title, "升級完成") {
		t.Fatalf("通知：%+v", *notes)
	}
	if data, _ := os.ReadFile(filepath.Join(e.home, "vault", "tasks", "data.md")); string(data) != "migrated\n" {
		t.Fatalf("新版整理過資料：%q", data)
	}

	// 手動退回：換回舊版資料夾，資料一起回到升級前。
	u.From = "0.2.0"
	if err := u.Rollback(ctx); err != nil {
		t.Fatalf("退回：%v\n%s", err, log.String())
	}
	if e.current() != "0.1.0" || e.binVersion() != "0.1.0" {
		t.Fatalf("退回後 current=%s", e.current())
	}
	if data, _ := os.ReadFile(filepath.Join(e.home, "vault", "tasks", "data.md")); string(data) != "original" {
		t.Fatalf("資料要回到升級前：%q", data)
	}
	if hist = ReadUpgradeHistory(e.home); hist[len(hist)-1].Kind != "rollback" || hist[len(hist)-1].Install != InstallDownload {
		t.Fatalf("history：%+v", hist)
	}
	if !contains(ReadUpgradeSkipped(e.home), "0.2.0") {
		t.Fatal("退回的版本要跳過")
	}

	// 明講要升（跳過的也可以；這次不整理資料）→ 確認 → 舊版資料夾刪掉、不能再退回。
	u.From = "0.1.0"
	e.publish("0.2.0", "release", nil)
	if err := u.Apply(ctx, "0.2.0"); err != nil {
		t.Fatalf("再升一次：%v\n%s", err, log.String())
	}
	if _, err := u.Accept(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(e.versions(), " ") != "0.2.0" {
		t.Fatalf("確認之後只留現在這一版：%v", e.versions())
	}
	u.From = "0.2.0"
	err := u.Rollback(ctx)
	if UpgradeExitCode(err) != 1 || err == nil || !strings.Contains(err.Error(), "確認過沒問題") || asciiWord.MatchString(err.Error()) || e.current() != "0.2.0" {
		t.Fatalf("確認過就不能退回：%v current=%s", err, e.current())
	}
}

// 下載出問題（檢查碼不符、下載中斷、清單讀不到、沒有這台晶片的包、檔案下載不到、清單寫的大小超過上限、包的內容不對）：結束碼 1、
// 舊版完全沒動（沒重開、current 不變、versions/ 沒有多東西、沒有跳過這一版）；給主人的話是白話（不夾英文），英文細節進 upgrade.log
// （網址的查詢字串不進去）。
func TestDownloadRefusesBadDownloads(t *testing.T) {
	cases := []struct {
		name, want, log string
		setup           func(e *dlEnv)
		nolog           string // upgrade.log 裡不能出現的字
	}{
		{"檢查碼不符", "下載的檔案不完整", "sha256", func(e *dlEnv) {
			e.publish("0.2.0", "release", func(m *ReleaseManifest) {
				a := m.Assets["darwin-arm64"]
				a.SHA256 = strings.Repeat("0", 64)
				m.Assets["darwin-arm64"] = a
			})
		}, ""},
		{"下載中斷", "下載到一半斷掉了", "bytes", func(e *dlEnv) {
			e.publish("0.2.0", "release", nil)
			e.mu.Lock()
			e.cut["/rel/evergenie-0.2.0-darwin-arm64.tar.gz"] = true
			e.mu.Unlock()
		}, ""},
		{"清單讀不到", "讀不到版本清單", "parse", func(e *dlEnv) {
			e.mu.Lock()
			e.files["/manifest.json"] = []byte("<html>not json</html>")
			e.mu.Unlock()
		}, ""},
		{"沒有這台晶片的包", "晶片", "no asset", func(e *dlEnv) {
			e.publish("0.2.0", "release", func(m *ReleaseManifest) {
				m.Assets = map[string]ReleaseAsset{"darwin-amd64": m.Assets["darwin-arm64"]}
			})
		}, ""},
		{"檔案下載不到（網址的查詢字串不進日誌）", "下載不到新版的檔案", "404", func(e *dlEnv) {
			e.publish("0.2.0", "release", func(m *ReleaseManifest) {
				a := m.Assets["darwin-arm64"]
				a.URL += "?token=s3cr3t"
				m.Assets["darwin-arm64"] = a
			})
			e.mu.Lock()
			delete(e.files, "/rel/evergenie-0.2.0-darwin-arm64.tar.gz")
			e.mu.Unlock()
		}, "s3cr3t"},
		{"清單寫的大小超過上限", "讀不到版本清單", "max", func(e *dlEnv) {
			e.publish("0.2.0", "release", func(m *ReleaseManifest) {
				a := m.Assets["darwin-arm64"]
				a.Size = assetMaxSize + 1
				m.Assets["darwin-arm64"] = a
			})
		}, ""},
		// 清單上的版本號要看得懂、能當資料夾名稱（不然比不出新舊、放不進 versions/）：一律當清單讀不到（UPD-12）
		{"清單的版本號看不懂（dev）", "讀不到版本清單", "bad version", func(e *dlEnv) {
			e.publish("0.2.0", "release", func(m *ReleaseManifest) { m.Version = "dev" })
		}, ""},
		{"清單的版本號只有兩段", "讀不到版本清單", "bad version", func(e *dlEnv) {
			e.publish("0.2.0", "release", func(m *ReleaseManifest) { m.Version = "0.2" })
		}, ""},
		{"清單的版本號帶路徑", "讀不到版本清單", "bad version", func(e *dlEnv) {
			e.publish("0.2.0", "release", func(m *ReleaseManifest) { m.Version = "../0.2.0" })
		}, ""},
		{"包的內容不對", "內容不對", "VERSION", func(e *dlEnv) {
			e.publish("0.2.0", "release", nil)
			wrong := releaseTar(e.t, "0.9.9")
			sum := sha256.Sum256(wrong)
			e.mu.Lock()
			defer e.mu.Unlock()
			e.files["/rel/evergenie-0.2.0-darwin-arm64.tar.gz"] = wrong
			var m ReleaseManifest
			_ = json.Unmarshal(e.files["/manifest.json"], &m)
			m.Assets["darwin-arm64"] = ReleaseAsset{URL: m.Assets["darwin-arm64"].URL, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(wrong))}
			e.files["/manifest.json"], _ = json.Marshal(m)
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newDLEnv(t)
			u, out, notes := e.upgrader()
			c.setup(e)
			pid := servePID(t, e.home)
			err := u.Apply(context.Background(), "")
			if UpgradeExitCode(err) != 1 || err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "舊版照常在跑") {
				t.Fatalf("錯誤：%v", err)
			}
			if asciiWord.MatchString(err.Error()) {
				t.Fatalf("給主人的話不能夾英文：%s", err.Error())
			}
			if asciiWord.MatchString(out.String()) || !strings.Contains(out.String(), "【沒成功】") {
				t.Fatalf("印出來的進度也不能夾英文：%s", out.String())
			}
			if !strings.Contains(e.upgradeLog(), c.log) || (c.nolog != "" && strings.Contains(e.upgradeLog(), c.nolog)) {
				t.Fatalf("英文細節要進 upgrade.log（要有 %q、不能有 %q）：%s", c.log, c.nolog, e.upgradeLog())
			}
			if e.current() != "0.1.0" || strings.Join(e.versions(), " ") != "0.1.0" || servePID(t, e.home) != pid {
				t.Fatalf("舊版不能動：current=%s versions=%v pid %s→%s", e.current(), e.versions(), pid, servePID(t, e.home))
			}
			if len(ReadUpgradeSkipped(e.home)) != 0 || len(ReadUpgradeHistory(e.home)) != 0 || len(*notes) != 0 {
				t.Fatalf("下載沒成功不跳過、不記升級、不發通知：%v %+v", ReadUpgradeSkipped(e.home), *notes)
			}
			// 結果：舊版照常（untouched），原因與下一步都是白話，日誌位置記著（UPD-14）。
			if st, _ := ReadUpgradeState(e.home); st == nil || st.Phase != UpgradePhaseFailed || st.Kind != sdk.UpdateKindUpgrade || st.Outcome != sdk.UpdateOutcomeUntouched ||
				st.Next == "" || english(st.Reason+st.Next) || st.Log != u.logPath() || !strings.Contains(err.Error(), st.Next) {
				t.Fatalf("state：%+v", st)
			}
			if readRescue(e.home) != nil {
				t.Fatal("舊版照常：不用記換回")
			}
		})
	}
}

// 下載來源的規則（PLT-25）：https 的清單或包被轉址到 http（連本機也一樣）一律不跟、不下載；https 的清單裡寫 http 的包、
// 家裡設成別台電腦的 http 都當清單不對。每一種都是「沒升級、舊版照常在跑」，http 那台一個請求都沒收到。
func TestDownloadRefusesRedirectAwayFromHTTPS(t *testing.T) {
	cases := []struct {
		name, want, log string
		source          func(e *dlEnv, tls string) string
	}{
		{"包被轉到 http", "下載不到新版的檔案", "not allowed", func(e *dlEnv, tls string) string { return tls + "/manifest-abs.json" }},
		{"清單被轉到 http", "讀不到版本清單", "not allowed", func(e *dlEnv, tls string) string { return tls + "/manifest-redirect.json" }},
		{"https 的清單寫 http 的包", "讀不到版本清單", "is not allowed", func(e *dlEnv, tls string) string { return tls + "/manifest-http-asset.json" }},
		{"家裡設成別台電腦的 http", "讀不到版本清單", "is not allowed", func(e *dlEnv, tls string) string { return "http://example.invalid/manifest.json" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newDLEnv(t)
			u, _, notes := e.upgrader()
			e.publish("0.2.0", "release", nil)
			e.mu.Lock()
			var m ReleaseManifest
			_ = json.Unmarshal(e.files["/manifest.json"], &m)
			asset := m.Assets["darwin-arm64"]
			e.requests = nil
			e.mu.Unlock()
			plain := e.srv.URL
			var tlsURL string
			tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mm := m
				switch r.URL.Path {
				case "/manifest-abs.json": // 包的網址是 https，那裡再轉到 http
					a := asset
					a.URL = tlsURL + "/" + asset.URL
					mm.Assets = map[string]ReleaseAsset{"darwin-arm64": a}
				case "/manifest-http-asset.json":
					a := asset
					a.URL = plain + "/" + asset.URL
					mm.Assets = map[string]ReleaseAsset{"darwin-arm64": a}
				case "/manifest-redirect.json":
					http.Redirect(w, r, plain+"/manifest.json", http.StatusFound)
					return
				default:
					http.Redirect(w, r, plain+r.URL.Path, http.StatusFound)
					return
				}
				data, _ := json.Marshal(mm)
				_, _ = w.Write(data)
			}))
			t.Cleanup(tls.Close)
			tlsURL = tls.URL
			u.Source, u.HTTP = c.source(e, tls.URL), tls.Client()
			pid := servePID(t, e.home)
			err := u.Apply(context.Background(), "")
			if UpgradeExitCode(err) != 1 || err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "舊版照常在跑") || asciiWord.MatchString(err.Error()) {
				t.Fatalf("錯誤：%v", err)
			}
			if !strings.Contains(e.upgradeLog(), c.log) {
				t.Fatalf("細節要進 upgrade.log（要有 %q）：%s", c.log, e.upgradeLog())
			}
			e.mu.Lock()
			reqs := append([]string{}, e.requests...)
			e.mu.Unlock()
			if len(reqs) != 0 {
				t.Fatalf("http 那台不能收到任何請求：%v", reqs)
			}
			if e.current() != "0.1.0" || strings.Join(e.versions(), " ") != "0.1.0" || servePID(t, e.home) != pid || len(*notes) != 0 {
				t.Fatalf("舊版不能動：current=%s versions=%v", e.current(), e.versions())
			}
		})
	}
}

// 新版開不起來：自動換回舊版資料夾（整理過的資料用備份還原）、跳過這一版、壞的資料夾留著查原因；通知是白話，沒有新版印的英文。
// 試跑資料整理沒過：跟原始碼版一樣當這一版有問題（跳過、請秘書查、資料夾留著），但還沒換版，舊版不用重開，訊息與通知說
// 「舊版照常在跑」、不說「已換回」；新版印的英文只進 upgrade.log。
func TestDownloadBrokenVersionSwitchesBack(t *testing.T) {
	ctx := context.Background()
	e := newDLEnv(t)
	u, log, notes := e.upgrader()
	e.publish("0.2.0", "release", nil, "serve-fail", "migrate-ok")
	err := u.Apply(ctx, "")
	if UpgradeExitCode(err) != 1 || err == nil || !strings.Contains(err.Error(), "已換回 0.1.0") {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if e.current() != "0.1.0" || e.binVersion() != "0.1.0" || !contains(ReadUpgradeSkipped(e.home), "0.2.0") {
		t.Fatalf("current=%s skipped=%v", e.current(), ReadUpgradeSkipped(e.home))
	}
	if data, _ := os.ReadFile(filepath.Join(e.home, "vault", "tasks", "data.md")); string(data) != "original" {
		t.Fatalf("整理過的資料要還原：%q", data)
	}
	if _, err := os.Stat(filepath.Join(e.app, versionsDirName, "0.2.0")); err != nil {
		t.Fatal("壞的那一版留著查原因")
	}
	if st, _ := ReadUpgradeState(e.home); st == nil || st.Phase != UpgradePhaseRolledBack || st.Outcome != sdk.UpdateOutcomeRolledBack || !st.Restored ||
		st.Next != nextSkipped || english(st.Reason) || st.Log == "" {
		t.Fatalf("state：%+v", st)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0].title, "沒成功") || english((*notes)[0].title+(*notes)[0].detail) ||
		!strings.Contains((*notes)[0].detail, nextSkipped) || !strings.Contains((*notes)[0].detail, u.logPath()) || strings.Contains((*notes)[0].detail, "serve.log") {
		t.Fatalf("通知（白話、帶下一步、日誌位置）：%+v", *notes)
	}

	e.publish("0.3.0", "release", nil, "preview-fail")
	pid := servePID(t, e.home)
	*notes = nil
	err = u.Apply(ctx, "")
	if UpgradeExitCode(err) != 1 || err == nil || !strings.Contains(err.Error(), "試跑沒過") || !strings.Contains(err.Error(), "舊版 0.1.0 照常在跑") ||
		strings.Contains(err.Error(), "換回") || asciiWord.MatchString(err.Error()) {
		t.Fatalf("試跑沒過：%v", err)
	}
	if !strings.Contains(e.upgradeLog(), "panic: goroutine") {
		t.Fatalf("新版印的英文要進 upgrade.log：%s", e.upgradeLog())
	}
	if e.current() != "0.1.0" || servePID(t, e.home) != pid {
		t.Fatal("還沒換版：舊版不用重開")
	}
	if !contains(ReadUpgradeSkipped(e.home), "0.3.0") {
		t.Fatalf("試跑沒過的版本要跳過：%v", ReadUpgradeSkipped(e.home))
	}
	if st, _ := ReadUpgradeState(e.home); st == nil || st.Phase != UpgradePhaseRolledBack || st.To != "0.3.0" || st.Outcome != sdk.UpdateOutcomeUntouched ||
		st.Restored || strings.Contains(st.Reason, "換回") || english(st.Reason) {
		t.Fatalf("state：%+v", st)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0].title, "舊版照常在跑") || strings.Contains((*notes)[0].title+(*notes)[0].detail, "換回") ||
		english((*notes)[0].title+(*notes)[0].detail) {
		t.Fatalf("通知：%+v", *notes)
	}
	if _, err := os.Stat(filepath.Join(e.app, versionsDirName, "0.3.0", "bin", "evergenie")); err != nil {
		t.Fatal("試跑沒過的那一版資料夾留著查原因")
	}
	if data, _ := os.ReadFile(filepath.Join(e.home, "vault", "tasks", "data.md")); string(data) != "original" {
		t.Fatalf("資料沒動：%q", data)
	}
}

// 換了 current 之後新版沒過、舊版的資料夾卻不見了（換不回去）：結束碼 3、通知「換不回去」、state 是 failed，訊息白話。
func TestDownloadStrandedWhenOldFolderGone(t *testing.T) {
	ctx := context.Background()
	e := newDLEnv(t)
	u, log, notes := e.upgrader()
	e.publish("0.2.0", "release", nil)
	u.Verify = func(ctx context.Context, want string) error {
		_ = os.RemoveAll(filepath.Join(e.app, versionsDirName, "0.1.0"))
		return &ErrUpgrade{Message: "新版檢查沒過"}
	}
	err := u.Apply(ctx, "")
	if UpgradeExitCode(err) != UpgradeExitStranded || err == nil || !strings.Contains(err.Error(), "換不回") || asciiWord.MatchString(err.Error()) {
		t.Fatalf("換不回去：%v\n%s", err, log.String())
	}
	if e.current() != "0.2.0" {
		t.Fatalf("換不回去時 current 留在新版：%s", e.current())
	}
	if st, _ := ReadUpgradeState(e.home); st == nil || st.Phase != UpgradePhaseFailed || st.Outcome != sdk.UpdateOutcomeStranded || st.Next != nextRescue || english(st.Reason) {
		t.Fatalf("state：%+v", st)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0].title, "換不回去") || english((*notes)[0].title+(*notes)[0].detail) || !strings.Contains((*notes)[0].detail, "退回上一版") {
		t.Fatalf("通知：%+v", *notes)
	}
	r := readRescue(e.home)
	if r == nil || r.From != "0.1.0" || r.To != "0.2.0" || r.Install != InstallDownload || r.Dir != "0.1.0" {
		t.Fatalf("換不回去要記下換回哪裡：%+v", r)
	}

	// 「退回上一版」：舊版的資料夾回來了（例如秘書從備份放回去）→ 照 rescue.json 再試一次換回；成功就刪掉紀錄，結果是「已換回」。
	u.Verify = nil
	archive := filepath.Join(t.TempDir(), "old.tar.gz")
	if err := os.WriteFile(archive, releaseTar(t, "0.1.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractRelease(archive, filepath.Join(e.app, versionsDirName, "0.1.0")); err != nil {
		t.Fatal(err)
	}
	*notes = nil
	if err := u.Rollback(ctx); err != nil {
		t.Fatalf("再試一次換回：%v\n%s", err, log.String())
	}
	if e.current() != "0.1.0" || readRescue(e.home) != nil {
		t.Fatalf("換回之後 current=%s rescue=%+v", e.current(), readRescue(e.home))
	}
	if st, _ := ReadUpgradeState(e.home); st == nil || st.Kind != sdk.UpdateKindRescue || st.Phase != UpgradePhaseRolledBack || st.Outcome != sdk.UpdateOutcomeRolledBack || st.From != "0.1.0" || st.To != "0.2.0" {
		t.Fatalf("state：%+v", st)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0].title, "已換回 0.1.0") || english((*notes)[0].title+(*notes)[0].detail) {
		t.Fatalf("通知：%+v", *notes)
	}
}

// 自動換回時資料已經還原、舊版卻重開失敗（launchctl 的錯誤是「中文：英文原文」）：結束碼 3，給主人看的只有中文那段、英文進升級日誌；
// rescue.json 記下「資料已經還原過」。之後「退回上一版」不再用升級前的備份蓋一次（中間新寫的資料留著）、也不用另外留一份。
func TestDownloadStrandedAfterRestoreDoesNotRestoreTwice(t *testing.T) {
	ctx := context.Background()
	e := newDLEnv(t)
	u, log, notes := e.upgrader()
	e.publish("0.2.0", "release", nil, "serve-fail", "migrate-ok")
	restarts := 0
	svc := host.Service{Runner: host.ExecRunner{}, UID: os.Getuid(), Spec: host.ServiceSpec{Label: host.HomeServiceLabel(e.home), Exe: e.exe(), Home: e.home}}
	u.Restart = func(ctx context.Context) error {
		restarts++
		if restarts == 2 { // 換回舊版之後那次重開
			return platform.Unavailable("載入服務失敗：Bootstrap failed: 5: Input/output error")
		}
		_, err := svc.Restart(ctx)
		return err
	}
	err := u.Apply(ctx, "")
	if UpgradeExitCode(err) != UpgradeExitStranded || err == nil || asciiWord.MatchString(err.Error()) || !strings.Contains(err.Error(), "重開舊版失敗：載入服務失敗") {
		t.Fatalf("換不回去：%v\n%s", err, log.String())
	}
	if st, _ := ReadUpgradeState(e.home); st == nil || st.Outcome != sdk.UpdateOutcomeStranded || english(st.Reason) || strings.Contains(st.Reason, "Bootstrap") {
		t.Fatalf("state：%+v", st)
	}
	if len(*notes) != 1 || english((*notes)[0].title+(*notes)[0].detail) {
		t.Fatalf("通知不夾英文：%+v", *notes)
	}
	if !strings.Contains(e.upgradeLog(), "Bootstrap failed: 5") {
		t.Fatalf("英文原文要進升級日誌：%s", e.upgradeLog())
	}
	r := readRescue(e.home)
	if r == nil || !r.Restore || !r.Restored {
		t.Fatalf("資料已經還原過要記下來：%+v", r)
	}
	if data, _ := os.ReadFile(filepath.Join(e.home, "vault", "tasks", "data.md")); string(data) != "original" {
		t.Fatalf("自動換回時資料已經還原：%q", data)
	}

	// 沒換回的這段時間又寫了資料；「退回上一版」重開成功：不再還原、不另外留一份，資料留著。
	data := filepath.Join(e.home, "vault", "tasks", "data.md")
	if err := os.WriteFile(data, []byte("written after"), 0o644); err != nil {
		t.Fatal(err)
	}
	*notes = nil
	if err := u.Rescue(ctx); err != nil {
		t.Fatalf("再試一次換回：%v\n%s", err, log.String())
	}
	if got, _ := os.ReadFile(data); string(got) != "written after" {
		t.Fatalf("不能又用升級前的備份蓋一次：%q", got)
	}
	if kept, _ := filepath.Glob(platform.RuntimeDir(e.home, "backups", "rollback-*.tgz")); len(kept) != 0 {
		t.Fatalf("不用還原就不用另外留一份：%v", kept)
	}
	if readRescue(e.home) != nil || e.current() != "0.1.0" {
		t.Fatalf("換回之後：rescue=%+v current=%s", readRescue(e.home), e.current())
	}
	if st, _ := ReadUpgradeState(e.home); st == nil || st.Kind != sdk.UpdateKindRescue || st.Outcome != sdk.UpdateOutcomeRolledBack || !st.Restored {
		t.Fatalf("state：%+v", st)
	}
}

// 「退回上一版」要還原資料（還沒還原過）：動東西之前先把現在的家留一份 rollback-*.tgz（記在 rescue.json，再試不會蓋掉那份），再用升級前的備份還原。
// 紀錄對不上現在跑的版本（之後手動修好、又換過版）：Rescue 刪掉紀錄、什麼都不動；不改做一般的退回。
func TestRescueKeepsCurrentHomeBeforeRestoreAndDropsStale(t *testing.T) {
	ctx := context.Background()
	e := newDLEnv(t)
	u, log, notes := e.upgrader()
	before := platform.RuntimeDir(e.home, "backups", "upgrade-0.1.0-0.2.0.tgz")
	if err := os.MkdirAll(filepath.Dir(before), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := u.backupHome(before); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(e.home, "vault", "tasks", "data.md")
	if err := os.WriteFile(data, []byte("newer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeRescue(e.home, upgradeRescue{From: "0.1.0", To: "0.2.0", Install: InstallDownload, Dir: "0.1.0", Backup: before, Restore: true, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := u.Rescue(ctx); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if got, _ := os.ReadFile(data); string(got) != "original" {
		t.Fatalf("資料要回到升級前：%q", got)
	}
	kept, _ := filepath.Glob(platform.RuntimeDir(e.home, "backups", "rollback-0.2.0-0.1.0-*.tgz"))
	if len(kept) != 1 {
		t.Fatalf("還原前要留一份現在的家：%v", kept)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0].detail, kept[0]) || english((*notes)[0].title+(*notes)[0].detail) {
		t.Fatalf("通知要說留了一份在哪：%+v", *notes)
	}

	// 對不上現在的版本（0.1.0）的紀錄：刪掉、不動、結束碼 1。
	if err := writeRescue(e.home, upgradeRescue{From: "0.0.5", To: "0.0.6", Install: InstallDownload, Dir: "0.0.5", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	stBefore, _ := ReadUpgradeState(e.home)
	err := u.Rescue(ctx)
	if UpgradeExitCode(err) != UpgradeExitFailed || err == nil || asciiWord.MatchString(err.Error()) || !strings.Contains(err.Error(), "沒有動任何東西") {
		t.Fatalf("過期的紀錄：%v", err)
	}
	if readRescue(e.home) != nil || e.current() != "0.1.0" {
		t.Fatalf("過期的紀錄要刪掉、不動：rescue=%+v current=%s", readRescue(e.home), e.current())
	}
	if stAfter, _ := ReadUpgradeState(e.home); stAfter == nil || stBefore == nil || stAfter.StartedAt != stBefore.StartedAt {
		t.Fatalf("不改做一般的退回（state 不變）：%+v → %+v", stBefore, stAfter)
	}
}

// 手動退回時舊版的資料夾已經不在（不是確認過、是被刪掉了）：拒絕、什麼都不動（current 不變、不記退回、不跳過）。
func TestDownloadRollbackRefusedWhenOldFolderGone(t *testing.T) {
	ctx := context.Background()
	e := newDLEnv(t)
	u, log, _ := e.upgrader()
	e.publish("0.2.0", "release", nil)
	if err := u.Apply(ctx, ""); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if err := os.RemoveAll(filepath.Join(e.app, versionsDirName, "0.1.0")); err != nil {
		t.Fatal(err)
	}
	u.From = "0.2.0"
	err := u.Rollback(ctx)
	if UpgradeExitCode(err) != 1 || err == nil || !strings.Contains(err.Error(), "程式資料夾已經不在") || asciiWord.MatchString(err.Error()) {
		t.Fatalf("舊版資料夾不在就不能退回：%v", err)
	}
	if e.current() != "0.2.0" || e.binVersion() != "0.2.0" {
		t.Fatalf("什麼都不動：current=%s", e.current())
	}
	if hist := ReadUpgradeHistory(e.home); len(hist) != 1 || hist[0].Kind != "upgrade" {
		t.Fatalf("不記退回：%+v", hist)
	}
	if len(ReadUpgradeSkipped(e.home)) != 0 {
		t.Fatalf("不跳過：%v", ReadUpgradeSkipped(e.home))
	}
}

// current 的替換是原子的：先建暫時的連結再 rename，換完旁邊不留暫時的連結；不存在的版本不換。
func TestSwitchCurrent(t *testing.T) {
	e := newDLEnv(t)
	for _, v := range []string{"0.2.0", "0.3.0"} {
		archive := filepath.Join(t.TempDir(), "r.tgz")
		_ = os.WriteFile(archive, releaseTar(t, v), 0o644)
		if err := extractRelease(archive, filepath.Join(e.app, versionsDirName, v)); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"0.2.0", "0.3.0", "0.1.0"} {
		if err := switchCurrent(e.app, v); err != nil || e.current() != v || e.binVersion() != v {
			t.Fatalf("換到 %s：%v current=%s", v, err, e.current())
		}
	}
	if err := switchCurrent(e.app, "9.9.9"); err == nil || e.current() != "0.1.0" {
		t.Fatalf("不存在的版本不換：%v", err)
	}
	if err := switchCurrent(e.app, "../x"); err == nil {
		t.Fatal("不是版本名稱的不換")
	}
	entries, _ := os.ReadDir(e.app)
	var names []string
	for _, x := range entries {
		names = append(names, x.Name())
	}
	if strings.Join(names, " ") != "current versions" {
		t.Fatalf("旁邊不留暫時的連結：%v", names)
	}
}

// 解開發行檔：去掉最上層資料夾；擋掉跑到外面的路徑、任何符號連結（包括一串連結拼出指到外面的路徑）、不只一個最上層資料夾、
// 硬連結、重複的檔；被擋的包不會在資料夾外面留下任何東西。
func TestExtractReleaseRejectsEscapes(t *testing.T) {
	build := func(entries ...tar.Header) string {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for _, h := range entries {
			h := h
			if h.Typeflag == tar.TypeReg {
				h.Size = 1
			}
			if err := tw.WriteHeader(&h); err != nil {
				t.Fatal(err)
			}
			if h.Typeflag == tar.TypeReg {
				_, _ = tw.Write([]byte("x"))
			}
		}
		_ = tw.Close()
		_ = gz.Close()
		p := filepath.Join(t.TempDir(), "a.tgz")
		_ = os.WriteFile(p, buf.Bytes(), 0o644)
		return p
	}
	reg := func(name string) tar.Header { return tar.Header{Name: name, Mode: 0o644, Typeflag: tar.TypeReg} }
	dir := func(name string) tar.Header { return tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeDir} }
	link := func(name, to string) tar.Header {
		return tar.Header{Name: name, Linkname: to, Typeflag: tar.TypeSymlink}
	}
	good := build(dir("top/"), dir("top/bin/"), reg("top/bin/a"), reg("top/VERSION"))
	dest := filepath.Join(t.TempDir(), "out")
	if err := extractRelease(good, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "a")); err != nil {
		t.Fatal("一般檔要解開到去掉最上層資料夾的位置")
	}
	for name, archive := range map[string]string{
		"..":        build(reg("top/../../evil")),
		"絕對路徑":      build(reg("/etc/evil")),
		"連結指到外面":    build(link("top/x", "../../evil")),
		"連結指到包裡也不收": build(reg("top/bin/a"), link("top/bin/b", "a")),
		"一串連結拼出外面":  build(dir("top/sub/"), link("top/sub/l", ".."), link("top/x", "sub/l/.."), reg("top/x/escaped.txt")),
		"兩個最上層資料夾":  build(reg("top/a"), reg("other/b")),
		"硬連結":       build(reg("top/a"), tar.Header{Name: "top/b", Linkname: "top/a", Typeflag: tar.TypeLink}),
		"同一個檔出現兩次":  build(reg("top/a"), reg("top/a")),
	} {
		parent := t.TempDir()
		if err := extractRelease(archive, filepath.Join(parent, "out")); err == nil || !strings.Contains(err.Error(), "內容不對") {
			t.Fatalf("%s：%v", name, err)
		}
		entries, _ := os.ReadDir(parent)
		for _, x := range entries {
			if x.Name() != "out" {
				t.Fatalf("%s：資料夾外面多了 %s", name, x.Name())
			}
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(parent), "escaped.txt")); err == nil {
			t.Fatalf("%s：跑到更外面去了", name)
		}
	}
}

// 解開後太大（壓縮得特別小、解開特別大）：一般檔加起來超過 unpackMaxSize（2 GiB）就不解完、白話說包不對＋下一步（DEV-155 最後審查）。
// 包裡只寫標頭說這個檔有多大（不真的放 2 GiB 的內容）：上限在讀內容之前就檢查。
func TestExtractReleaseRefusesTooBig(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range []tar.Header{
		{Name: "top/", Mode: 0o755, Typeflag: tar.TypeDir},
		{Name: "top/a", Mode: 0o644, Typeflag: tar.TypeReg, Size: 1},
	} {
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			_, _ = tw.Write([]byte("x"))
		}
	}
	if err := tw.WriteHeader(&tar.Header{Name: "top/big", Mode: 0o644, Typeflag: tar.TypeReg, Size: unpackMaxSize}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Flush() // 內容沒寫完，Flush／Close 會抱怨；標頭已經寫進去了
	_ = gz.Close()
	archive := filepath.Join(t.TempDir(), "big.tgz")
	if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out")
	err := extractRelease(archive, dest)
	var pe *plainErr
	if !errors.As(err, &pe) || pe.plain != msgTooBig || nextOf(err) != nextWait || asciiWord.MatchString(pe.plain+nextOf(err)) {
		t.Fatalf("太大的包要白話拒絕（%q＋%q）：%v", msgTooBig, nextWait, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "big")); err == nil {
		t.Fatal("太大的檔不該開始寫")
	}
}

// fileID 是檔案本身的身分（inode）與修改時間：內容沒變不換時兩個都不能變。
func fileID(t *testing.T, p string) (uint64, time.Time) {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t).Ino, info.ModTime()
}

// UPD-15：發行檔 fixed/ 裡的檔放到程式資料夾的 fixed/。第一次放；內容沒變完全不動（同一個 inode、修改時間不變）；
// 內容變了才換（新的 inode），換的時候已經開著舊檔的人讀到的還是舊的（先寫暫存檔再 rename）；不留暫存檔；包裡沒有 fixed/ 什麼都不做。
func TestSyncFixedFilesReplacesOnlyChangedContent(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	write := func(name, body string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(src, name), mode); err != nil {
			t.Fatal(err)
		}
	}
	if changed, err := syncFixedFiles(src, dst); err != nil || changed != nil {
		t.Fatalf("包裡沒有 fixed/：什麼都不做：%v %v", changed, err)
	}
	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("包裡沒有 fixed/ 不建資料夾：%v", err)
	}
	write("prog", "v1", 0o755)
	write("helper", "h1", 0o755)
	if changed, err := syncFixedFiles(src, dst); err != nil || strings.Join(changed, " ") != "helper prog" {
		t.Fatalf("第一次要放：%v %v", changed, err)
	}
	if info, err := os.Stat(filepath.Join(dst, "prog")); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("權限照包裡的：%v %v", info, err)
	}
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(filepath.Join(dst, "prog"), old, old); err != nil {
		t.Fatal(err)
	}
	ino, mtime := fileID(t, filepath.Join(dst, "prog"))

	// 同樣內容的新版（解開的時間不一樣）：不動。
	write("prog", "v1", 0o755)
	if changed, err := syncFixedFiles(src, dst); err != nil || len(changed) != 0 {
		t.Fatalf("內容沒變不換：%v %v", changed, err)
	}
	if ino2, mtime2 := fileID(t, filepath.Join(dst, "prog")); ino2 != ino || !mtime2.Equal(mtime) {
		t.Fatalf("內容沒變要完全不動：inode %d→%d，修改時間 %v→%v", ino, ino2, mtime, mtime2)
	}

	// 內容變了：換成新的；已經開著舊檔的照樣讀到舊內容。
	f, err := os.Open(filepath.Join(dst, "prog"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	write("prog", "v2", 0o755)
	if changed, err := syncFixedFiles(src, dst); err != nil || strings.Join(changed, " ") != "prog" {
		t.Fatalf("內容變了才換、只換變了的：%v %v", changed, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "prog")); string(data) != "v2" {
		t.Fatalf("換成新內容：%q", data)
	}
	if ino2, _ := fileID(t, filepath.Join(dst, "prog")); ino2 == ino {
		t.Fatal("換的時候要是新的檔（rename 蓋過去），不是改寫舊檔")
	}
	if data, _ := io.ReadAll(f); string(data) != "v1" {
		t.Fatalf("開著舊檔的讀到的要還是舊的：%q", data)
	}
	entries, _ := os.ReadDir(dst)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "helper prog" {
		t.Fatalf("不留暫存檔：%v", names)
	}
}

// UPD-15 接在升級上：新版的 fixed/ 在換版時放到程式資料夾的 fixed/；下一版內容一樣就不動、變了才換；舊的包（沒有 fixed/）不動已經放好的。
func TestDownloadApplySyncsFixedFiles(t *testing.T) {
	ctx := context.Background()
	e := newDLEnv(t)
	u, log, _ := e.upgrader()
	prog := filepath.Join(e.app, fixedDirName, "prog")
	e.publish("0.2.0", "release", nil, "fixed/prog=#!/bin/sh\necho v1\n", "fixed/helper=#!/bin/sh\n")
	if err := u.Apply(ctx, ""); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if data, err := os.ReadFile(prog); err != nil || string(data) != "#!/bin/sh\necho v1\n" {
		t.Fatalf("升級後固定位置要有新版 fixed/ 的檔：%q %v", data, err)
	}
	if !strings.Contains(e.upgradeLog(), "fixed files replaced (content changed): helper, prog") {
		t.Fatalf("換了哪些要寫進 upgrade.log：%s", e.upgradeLog())
	}
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(prog, old, old); err != nil {
		t.Fatal(err)
	}
	ino, mtime := fileID(t, prog)

	u.From = "0.2.0"
	e.publish("0.3.0", "release", nil, "fixed/prog=#!/bin/sh\necho v1\n", "fixed/helper=#!/bin/sh\n")
	if err := u.Apply(ctx, ""); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if ino2, mtime2 := fileID(t, prog); ino2 != ino || !mtime2.Equal(mtime) {
		t.Fatalf("內容一樣的新版不能動到固定位置的檔：inode %d→%d，修改時間 %v→%v", ino, ino2, mtime, mtime2)
	}

	u.From = "0.3.0"
	e.publish("0.4.0", "release", nil, "fixed/prog=#!/bin/sh\necho v2\n", "fixed/helper=#!/bin/sh\n")
	if err := u.Apply(ctx, ""); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if data, _ := os.ReadFile(prog); string(data) != "#!/bin/sh\necho v2\n" {
		t.Fatalf("內容變了要換：%q", data)
	}
	if ino2, _ := fileID(t, prog); ino2 == ino {
		t.Fatal("內容變了要換成新的檔")
	}
	ino, _ = fileID(t, prog)

	u.From = "0.4.0"
	e.publish("0.5.0", "release", nil)
	if err := u.Apply(ctx, ""); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if ino2, _ := fileID(t, prog); ino2 != ino {
		t.Fatal("舊的包（沒有 fixed/）不動已經放好的")
	}
	if e.current() != "0.5.0" {
		t.Fatalf("current=%s", e.current())
	}
}

// installFixed 模擬 install.sh 裝好 0.1.0 時放的固定位置：versions/0.1.0/fixed/ 與 <app>/fixed/ 都有 prog（內容 body）。
func (e *dlEnv) installFixed(body string) string {
	e.t.Helper()
	for _, dir := range []string{filepath.Join(e.app, versionsDirName, "0.1.0", fixedDirName), filepath.Join(e.app, fixedDirName)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			e.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prog"), []byte(body), 0o755); err != nil {
			e.t.Fatal(err)
		}
	}
	return filepath.Join(e.app, fixedDirName, "prog")
}

// UPD-15 跟退回：新版帶來的 fixed/ 換過固定位置之後，自動換回（新版重開沒過）與手動退回都把固定位置換回舊版那份，
// 新版帶來壞掉的程式不會留下來給舊版用；在放固定位置之前就沒過（試跑資料整理沒過）的，固定位置完全不動。
func TestDownloadRollbackRestoresFixedFiles(t *testing.T) {
	ctx := context.Background()
	const v1, v2 = "#!/bin/sh\necho v1\n", "#!/bin/sh\necho v2\n"

	// 新版重開沒過 → 自動換回
	e := newDLEnv(t)
	u, log, _ := e.upgrader()
	prog := e.installFixed(v1)
	e.publish("0.2.0", "release", nil, "serve-fail", "fixed/prog="+v2)
	if err := u.Apply(ctx, ""); UpgradeExitCode(err) != 1 || e.current() != "0.1.0" {
		t.Fatalf("要自動換回：%v current=%s\n%s", err, e.current(), log.String())
	}
	if data, _ := os.ReadFile(prog); string(data) != v1 {
		t.Fatalf("換回後固定位置要是舊版那份：%q", data)
	}

	// 試跑資料整理沒過（還沒放固定位置）→ 固定位置連修改時間都不變
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(prog, old, old); err != nil {
		t.Fatal(err)
	}
	ino, mtime := fileID(t, prog)
	e.publish("0.3.0", "release", nil, "preview-fail", "fixed/prog="+v2)
	if err := u.Apply(ctx, ""); UpgradeExitCode(err) != 1 || e.current() != "0.1.0" {
		t.Fatalf("試跑沒過：%v current=%s", err, e.current())
	}
	if ino2, mtime2 := fileID(t, prog); ino2 != ino || !mtime2.Equal(mtime) {
		t.Fatalf("還沒換版就沒過，固定位置不能動：inode %d→%d，修改時間 %v→%v", ino, ino2, mtime, mtime2)
	}

	// 升級成功（固定位置換成新版的）→ 手動退回 → 換回舊版那份
	e2 := newDLEnv(t)
	u2, log2, _ := e2.upgrader()
	prog2 := e2.installFixed(v1)
	e2.publish("0.2.0", "release", nil, "fixed/prog="+v2)
	if err := u2.Apply(ctx, ""); err != nil {
		t.Fatalf("%v\n%s", err, log2.String())
	}
	if data, _ := os.ReadFile(prog2); string(data) != v2 {
		t.Fatalf("升級後要是新版那份：%q", data)
	}
	u2.From = "0.2.0"
	if err := u2.Rollback(ctx); err != nil || e2.current() != "0.1.0" {
		t.Fatalf("退回：%v current=%s\n%s", err, e2.current(), log2.String())
	}
	if data, _ := os.ReadFile(prog2); string(data) != v1 {
		t.Fatalf("手動退回後固定位置要是舊版那份：%q", data)
	}
}

// UPD-15 放不了不擋升級：<app>/fixed 是一個檔（放不進去）時照樣升級成功、原因（英文）寫進 upgrade.log；
// 上一次被中斷留下的暫存檔（.<名>.new-*）下一次放的時候收掉。
func TestDownloadFixedSyncFailureDoesNotBlockUpgrade(t *testing.T) {
	ctx := context.Background()
	e := newDLEnv(t)
	u, log, _ := e.upgrader()
	if err := os.WriteFile(filepath.Join(e.app, fixedDirName), []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.publish("0.2.0", "release", nil, "fixed/prog=#!/bin/sh\n")
	if err := u.Apply(ctx, ""); err != nil || e.current() != "0.2.0" {
		t.Fatalf("放不了固定位置也要升級成功：%v current=%s\n%s", err, e.current(), log.String())
	}
	if !strings.Contains(e.upgradeLog(), "fixed files: ") {
		t.Fatalf("放不了的原因要寫進 upgrade.log：%s", e.upgradeLog())
	}
	if strings.Contains(log.String(), "fixed") {
		t.Fatalf("給主人看的輸出不提英文細節：%s", log.String())
	}

	src, dst := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "prog"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, ".prog.new-123"), []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := syncFixedFiles(src, dst); err != nil {
		t.Fatal(err)
	}
	if names, _ := os.ReadDir(dst); len(names) != 1 || names[0].Name() != "prog" {
		t.Fatalf("中斷留下的暫存檔要收掉：%v", names)
	}
}

// UPD-15 跟「退回上一版」（UPD-14 的救援）：升級沒成功、又換不回去（舊版資料夾不見）時固定位置留著新版那份；
// 舊版資料夾回來之後按「退回上一版」，固定位置先對回舊版那份再換版。放不了（<app>/fixed 是一個檔）也照樣換回，原因只進升級日誌。
func TestRescueRestoresFixedFiles(t *testing.T) {
	ctx := context.Background()
	const v1, v2 = "#!/bin/sh\necho v1\n", "#!/bin/sh\necho v2\n"
	e := newDLEnv(t)
	u, log, notes := e.upgrader()
	prog := e.installFixed(v1)
	e.publish("0.2.0", "release", nil, "fixed/prog="+v2)
	u.Verify = func(ctx context.Context, want string) error {
		_ = os.RemoveAll(filepath.Join(e.app, versionsDirName, "0.1.0"))
		return &ErrUpgrade{Message: "新版檢查沒過"}
	}
	if err := u.Apply(ctx, ""); UpgradeExitCode(err) != UpgradeExitStranded {
		t.Fatalf("換不回去：%v\n%s", err, log.String())
	}
	if data, _ := os.ReadFile(prog); string(data) != v2 {
		t.Fatalf("換不回去時舊版那份也不在，固定位置留著新版的：%q", data)
	}
	// 舊版資料夾回來了（帶著它的 fixed/）
	u.Verify = nil
	archive := filepath.Join(t.TempDir(), "old.tar.gz")
	if err := os.WriteFile(archive, releaseTar(t, "0.1.0", "fixed/prog="+v1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractRelease(archive, filepath.Join(e.app, versionsDirName, "0.1.0")); err != nil {
		t.Fatal(err)
	}
	*notes = nil
	logged := len(e.upgradeLog()) // 升級日誌一直往後加：只看「退回上一版」寫的
	if err := u.Rescue(ctx); err != nil || e.current() != "0.1.0" || readRescue(e.home) != nil {
		t.Fatalf("再試一次換回：%v current=%s\n%s", err, e.current(), log.String())
	}
	if data, _ := os.ReadFile(prog); string(data) != v1 {
		t.Fatalf("換回後固定位置要是舊版那份：%q", data)
	}
	if got := e.upgradeLog()[logged:]; !strings.Contains(got, "fixed files replaced (content changed): prog") {
		t.Fatalf("換了哪些要寫進升級日誌：%s", got)
	}

	// 放不了：<app>/fixed 是一個檔。照樣換回、刪掉紀錄；原因只進升級日誌，給主人看的不夾英文。
	e2 := newDLEnv(t)
	u2, log2, notes2 := e2.upgrader()
	e2.installFixed(v1)
	if err := os.RemoveAll(filepath.Join(e2.app, fixedDirName)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e2.app, fixedDirName), []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeRescue(e2.home, upgradeRescue{From: "0.1.0", To: "0.2.0", Install: InstallDownload, Dir: "0.1.0", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := u2.Rescue(ctx); err != nil || e2.current() != "0.1.0" || readRescue(e2.home) != nil {
		t.Fatalf("放不了固定位置也要換回：%v current=%s\n%s", err, e2.current(), log2.String())
	}
	if !strings.Contains(e2.upgradeLog(), "fixed files: ") {
		t.Fatalf("放不了的原因要寫進升級日誌：%s", e2.upgradeLog())
	}
	if st, _ := ReadUpgradeState(e2.home); st == nil || st.Outcome != sdk.UpdateOutcomeRolledBack || english(st.Reason) {
		t.Fatalf("state：%+v", st)
	}
	if len(*notes2) != 1 || english((*notes2)[0].title+(*notes2)[0].detail) || strings.Contains(log2.String(), "fixed") {
		t.Fatalf("給主人看的不夾英文：%+v\n%s", *notes2, log2.String())
	}

	// 換不過去（紀錄指到的那一版有 fixed/、沒有執行檔）：固定位置對回 current 還指著的那一版；紀錄裡不合法的資料夾名不拿來放檔。
	e3 := newDLEnv(t)
	u3, log3, _ := e3.upgrader()
	prog3 := e3.installFixed(v1)
	for dir, body := range map[string]string{filepath.Join(e3.app, versionsDirName, "0.0.9", fixedDirName): v2, filepath.Join(e3.app, "evil", fixedDirName): v2} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prog"), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"0.0.9", "../evil"} {
		if err := writeRescue(e3.home, upgradeRescue{From: "0.1.0", To: "0.2.0", Install: InstallDownload, Dir: dir, At: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if err := u3.Rescue(ctx); UpgradeExitCode(err) != UpgradeExitStranded || e3.current() != "0.1.0" {
			t.Fatalf("%s：換不過去要是換不回去：%v current=%s\n%s", dir, err, e3.current(), log3.String())
		}
		if data, _ := os.ReadFile(prog3); string(data) != v1 {
			t.Fatalf("%s：換不過去，固定位置要對回現在這一版：%q", dir, data)
		}
	}
}
