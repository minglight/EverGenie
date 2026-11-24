package pkg

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 分享套件（SEC-302 S5、U-6）：export 只打包套件資料夾本身——使用者的資料（vault/pkg/<id>/）與 runtime/ 不在資料夾裡，
// 本來就帶不到；資料夾裡的本機狀態（.git、fork 基準、安裝來源、暫存）、相依與編譯產物也不帶。符號連結一律不跟：
// 指到資料夾外面的整個拒絕並列出，指在裡面的略過（安裝時本來也不帶）。匯出前先掃疑似個資（privacy.go）。

const (
	exportFormatDir = "dir"
	exportFormatTar = "tar.gz"
)

// 資料夾裡屬於這台電腦的狀態：不分享。
var exportSkipNames = map[string]string{
	".git":         "git 紀錄（分享 git 版請用 --git 產生乾淨的資料夾再自己 init）",
	forkBaseDir:    "fork 的基準內容",
	sourceFile:     "安裝來源紀錄",
	"node_modules": "相依套件（可重建）",
	"__pycache__":  "快取（可重建）",
}

// 隱藏的暫存資料夾前綴（安裝、更新、預覽、新建、備份、匯出）。
var exportTempPrefixes = []string{".install-", ".upgrade-", ".preview-", ".new-", ".old-", ".export-"}

// 編譯產物的副檔名。
var exportBuildExts = map[string]bool{".o": true, ".a": true, ".so": true, ".dylib": true, ".dll": true, ".exe": true, ".test": true, ".pyc": true, ".class": true}

// --git 產生的 .gitignore 內容（已經有 .gitignore 就補上沒有的行）。
var exportGitignore = []string{".DS_Store", "node_modules/", ".env", ".env.*", sourceFile, forkBaseDir + "/"}

type exportItem struct {
	rel  string // 斜線
	dir  bool
	mode fs.FileMode
}

// Export 把 mine 或 installed 的套件資料夾匯出成資料夾或 .tar.gz（REQ-334～337）。
func (m *Module) Export(ctx context.Context, req *sdk.PackageExportRequest) (*sdk.PackageExportResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(req.ID)
	if id == "" || strings.ContainsAny(id, `/\`) || strings.HasPrefix(id, ".") {
		return nil, platform.Invalid("id", "要給套件的 id")
	}
	m.mu.Lock()
	if err := m.readyLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	e := m.entries[id]
	home := m.home
	if e == nil {
		for _, r := range m.rejected {
			if r.id == id {
				e = r
			}
		}
	}
	var dir, version string
	switch {
	case e == nil:
		m.mu.Unlock()
		return nil, platform.NotFound(fmt.Sprintf("沒有套件「%s」", id))
	case e.tier == sdk.TierOfficial:
		m.mu.Unlock()
		return nil, platform.Forbidden("官方套件不用匯出，別人升級 EverGenie 就有")
	case e.man == nil:
		m.mu.Unlock()
		return nil, platform.Invalid("id", fmt.Sprintf("「%s」的 package.toml 有問題（%s），先修好再匯出", id, errMessage(e.err)))
	case e.man.Fork != nil:
		m.mu.Unlock()
		return nil, platform.Forbidden(fmt.Sprintf("「%s」是官方套件的修改版（fork），別人那邊有同名的官方套件、裝不起來；要分享請用 packages new 做成自己的套件", id))
	}
	dir, version = e.dir, e.man.Version
	m.mu.Unlock()

	out, format, err := exportTarget(req, home, id, version)
	if err != nil {
		return nil, err
	}
	if err := checkExportOut(out, dir, home); err != nil {
		return nil, err
	}
	items, skipped, err := collectExport(dir)
	if err != nil {
		return nil, err
	}
	resp := &sdk.PackageExportResponse{Format: format, Skipped: skipped}
	homes := []string{home}
	if h, err := os.UserHomeDir(); err == nil {
		homes = append(homes, h)
	}
	for _, it := range items {
		if it.dir {
			continue
		}
		resp.Files = append(resp.Files, it.rel)
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(it.rel)))
		if err != nil {
			return nil, platform.Unavailable(fmt.Sprintf("讀不到 %s，沒有匯出", it.rel))
		}
		resp.Findings = append(resp.Findings, scanPrivacy(it.rel, data, homes)...)
	}
	if len(resp.Findings) > 0 && !req.AllowFindings {
		resp.Blocked = true
		return resp, nil
	}
	if format == exportFormatTar {
		err = writeExportTar(out, dir, id, items)
	} else {
		err = writeExportDir(out, dir, items, req.Git)
	}
	if err != nil {
		return nil, err
	}
	resp.Path = out
	if m.log != nil {
		m.log.Info("匯出套件", "id", id, "format", format, "files", len(resp.Files), "findings", len(resp.Findings), "by", actor(req.By))
	}
	return resp, nil
}

// exportTarget 決定輸出位置與格式：空＝<家>/runtime/exports/<id>-<版本>（已存在就加 -2、-3…）。
func exportTarget(req *sdk.PackageExportRequest, home, id, version string) (string, string, error) {
	out := strings.TrimSpace(req.Out)
	if out == "" {
		base := platform.RuntimeDir(home, "exports", id+"-"+version)
		out = base
		for n := 2; exists(out); n++ {
			out = fmt.Sprintf("%s-%d", base, n)
		}
	} else if !filepath.IsAbs(out) {
		return "", "", platform.Invalid("out", "輸出位置要給完整路徑")
	}
	out = filepath.Clean(out)
	format := exportFormatDir
	if low := strings.ToLower(out); strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".tgz") {
		format = exportFormatTar
	}
	if req.Git && format == exportFormatTar {
		return "", "", platform.Invalid("git", "--git 要輸出成資料夾（不要用 .tar.gz 結尾）")
	}
	return out, format, nil
}

// checkExportOut：輸出不能在套件資料夾裡、不能在家的 packages/（會被當成套件掃到）或 vault/（使用者的資料）裡；
// 資料夾只能是不存在或空的，壓縮檔不能已存在。
func checkExportOut(out, pkgDir, home string) error {
	real := resolveExisting(out)
	for _, bad := range []struct{ dir, why string }{
		{pkgDir, "不能匯出到套件自己的資料夾裡"},
		{filepath.Join(home, "packages"), "不能匯出到家裡的 packages/（會被當成另一個套件）"},
		{filepath.Join(home, "vault"), "不能匯出到家裡的 vault/（那是你的資料）"},
	} {
		if within(real, resolveExisting(bad.dir)) {
			return platform.Invalid("out", bad.why)
		}
	}
	info, err := os.Lstat(out)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return platform.Unavailable("無法讀輸出位置")
	case info.IsDir() && !strings.HasSuffix(strings.ToLower(out), ".tar.gz") && !strings.HasSuffix(strings.ToLower(out), ".tgz"):
		if ents, err := os.ReadDir(out); err == nil && len(ents) == 0 {
			return nil
		}
	}
	return platform.Conflict(fmt.Sprintf("%s 已經存在；換一個位置，或先把它移走", out))
}

// resolveExisting 把路徑最長的已存在部分解開符號連結（macOS 的 /var → /private/var），其餘照接。
func resolveExisting(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; cur = filepath.Dir(cur) {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		if filepath.Dir(cur) == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
	}
}

func within(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// collectExport 走一遍套件資料夾（不跟符號連結），回要匯出的檔案與資料夾（排序）、略過的東西與原因。
// 指到資料夾外面（或指不到東西）的符號連結回 INVALID 並全部列出。
func collectExport(dir string) ([]exportItem, []string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, nil, platform.Unavailable("讀不到套件資料夾")
	}
	var items []exportItem
	var skipped, escaping []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		name := d.Name()
		skipDir := func(why string) error {
			skipped = append(skipped, fmt.Sprintf("%s：%s", rel, why))
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if why, ok := exportSkipNames[name]; ok {
			return skipDir(why)
		}
		if name == ".DS_Store" {
			return nil
		}
		for _, pre := range exportTempPrefixes {
			if strings.HasPrefix(name, pre) {
				return skipDir("暫存")
			}
		}
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(p)
			if err != nil || !within(target, root) {
				escaping = append(escaping, rel)
				return nil
			}
			skipped = append(skipped, rel+"：符號連結不匯出（安裝時也不會帶）")
			return nil
		}
		if d.IsDir() {
			items = append(items, exportItem{rel: rel, dir: true, mode: 0o755})
			return nil
		}
		if !d.Type().IsRegular() {
			skipped = append(skipped, rel+"：不是一般檔案")
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if exportBuildExts[strings.ToLower(filepath.Ext(name))] || isExecutableBinary(p) {
			skipped = append(skipped, rel+"：編譯產物（對方那邊會自己編）")
			return nil
		}
		items = append(items, exportItem{rel: rel, mode: info.Mode().Perm()})
		return nil
	})
	if err != nil {
		return nil, nil, platform.Unavailable("讀套件資料夾失敗：" + err.Error())
	}
	if len(escaping) > 0 {
		sort.Strings(escaping)
		return nil, nil, platform.Invalid("source", "這些符號連結指到套件資料夾外面（或指不到東西），不能匯出："+strings.Join(escaping, "、")+"；請換成真的檔案或刪掉")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].rel < items[j].rel })
	sort.Strings(skipped)
	return items, skipped, nil
}

// isExecutableBinary：開頭是 Mach-O 或 ELF 的執行檔（go build 留在資料夾裡的）。
func isExecutableBinary(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	var b [4]byte
	if _, err := io.ReadFull(f, b[:]); err != nil {
		return false
	}
	switch string(b[:]) {
	case "\x7fELF", "\xfe\xed\xfa\xce", "\xfe\xed\xfa\xcf", "\xce\xfa\xed\xfe", "\xcf\xfa\xed\xfe":
		return true
	}
	return false
}

// writeExportDir 先寫在旁邊隱藏的暫存資料夾，全部寫完才 rename 成 out；失敗不留東西。
func writeExportDir(out, src string, items []exportItem, git bool) error {
	parent := filepath.Dir(out)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return platform.Unavailable("無法建立輸出的上層資料夾")
	}
	stage := filepath.Join(parent, ".export-"+shortRand())
	fail := func(msg string) error { _ = os.RemoveAll(stage); return platform.Unavailable(msg) }
	if err := os.Mkdir(stage, 0o755); err != nil {
		return platform.Unavailable("無法建立暫存資料夾")
	}
	hasIgnore := false
	for _, it := range items {
		target := filepath.Join(stage, filepath.FromSlash(it.rel))
		if it.dir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fail("無法建立資料夾 " + it.rel)
			}
			continue
		}
		if it.rel == ".gitignore" {
			hasIgnore = true
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fail("無法建立資料夾 " + it.rel)
		}
		if err := copyFile(filepath.Join(src, filepath.FromSlash(it.rel)), target, it.mode); err != nil {
			return fail("無法複製 " + it.rel)
		}
	}
	if git {
		if err := writeGitignore(filepath.Join(stage, ".gitignore"), hasIgnore); err != nil {
			return fail("無法寫 .gitignore")
		}
	}
	if isDir(out) {
		if err := os.Remove(out); err != nil { // 只會是空資料夾（checkExportOut 擋過）
			return fail("無法使用輸出資料夾 " + out)
		}
	}
	if err := os.Rename(stage, out); err != nil {
		return fail("無法放到 " + out)
	}
	return nil
}

// writeGitignore：沒有就寫一份；已經有就補上沒有的行。
func writeGitignore(p string, has bool) error {
	var cur string
	if has {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		cur = string(b)
	}
	present := map[string]bool{}
	for _, l := range strings.Split(cur, "\n") {
		present[strings.TrimSpace(l)] = true
	}
	var b strings.Builder
	b.WriteString(cur)
	if cur != "" && !strings.HasSuffix(cur, "\n") {
		b.WriteString("\n")
	}
	if !has {
		b.WriteString("# evergenie packages export --git 產生：這些是每台電腦自己的東西，不放進 git\n")
	}
	for _, l := range exportGitignore {
		if !present[l] {
			b.WriteString(l + "\n")
		}
	}
	return os.WriteFile(p, []byte(b.String()), 0o644)
}

// writeExportTar 打成 .tar.gz，裡面的路徑都在 <id>/ 底下（解開就是可以 packages install 的資料夾）；先寫暫存檔再 rename。
func writeExportTar(out, src, id string, items []exportItem) error {
	parent := filepath.Dir(out)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return platform.Unavailable("無法建立輸出的上層資料夾")
	}
	tmp, err := os.CreateTemp(parent, ".export-*.tar.gz")
	if err != nil {
		return platform.Unavailable("無法建立暫存檔")
	}
	fail := func(msg string) error { tmp.Close(); _ = os.Remove(tmp.Name()); return platform.Unavailable(msg) }
	gz := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: id + "/", Mode: 0o755}); err != nil {
		return fail("無法寫壓縮檔")
	}
	for _, it := range items {
		name := id + "/" + it.rel
		if it.dir {
			if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: name + "/", Mode: 0o755}); err != nil {
				return fail("無法寫壓縮檔")
			}
			continue
		}
		p := filepath.Join(src, filepath.FromSlash(it.rel))
		info, err := os.Stat(p)
		if err != nil {
			return fail("讀不到 " + it.rel)
		}
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: int64(it.mode), Size: info.Size(), ModTime: info.ModTime()}); err != nil {
			return fail("無法寫壓縮檔")
		}
		f, err := os.Open(p)
		if err != nil {
			return fail("讀不到 " + it.rel)
		}
		_, err = io.Copy(tw, f)
		f.Close()
		if err != nil {
			return fail("無法寫壓縮檔")
		}
	}
	if err := tw.Close(); err != nil {
		return fail("無法寫壓縮檔")
	}
	if err := gz.Close(); err != nil {
		return fail("無法寫壓縮檔")
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return platform.Unavailable("無法寫壓縮檔")
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		_ = os.Remove(tmp.Name())
		return platform.Unavailable("無法寫壓縮檔")
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		_ = os.Remove(tmp.Name())
		return platform.Unavailable("無法放到 " + out)
	}
	return nil
}

// previewPersonas 列出套件帶來的角色（REQ-338）：有效代號 <id>.<slug>、卡上的名字（讀不出來空白）、有沒有頭像
// （personas/avatars/<slug>.png 或 .mp4，一般檔案才算）。dir 是驗過的暫存資料夾。
func previewPersonas(man *Manifest, dir string) []sdk.PackagePreviewPersona {
	var out []sdk.PackagePreviewPersona
	for _, p := range man.Contributes.Persona {
		pp := sdk.PackagePreviewPersona{ID: man.ID + "." + p.Slug, Slug: p.Slug}
		if data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p.Path))); err == nil {
			decode := vault.Decode
			if strings.HasPrefix(string(data), "+++") {
				decode = vault.DecodeLegacy
			}
			if r, _, err := decode(data); err == nil {
				pp.Name, _ = r.Fields["name"].(string)
			}
		}
		for _, ext := range []string{".png", ".mp4"} {
			if info, err := os.Lstat(filepath.Join(dir, "personas", "avatars", p.Slug+ext)); err == nil && info.Mode().IsRegular() {
				pp.Avatar = true
			}
		}
		out = append(out, pp)
	}
	return out
}
