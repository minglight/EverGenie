package homebackup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/minglight/evergenie/internal/base/platform"
)

// CreateOptions：做一份備份。Dir 空＝這台電腦放這種備份的資料夾（platform.HomeBackupDir）。
type CreateOptions struct {
	Home string
	Kind string
	Now  time.Time
	Dir  string
}

// Created 是做好的備份。
type Created struct {
	Path     string
	Name     string
	Size     int64
	Manifest *Manifest
}

// source 是要收進備份的一個檔或資料夾：arc 是備份檔裡的路徑，path 是它在這台電腦上的位置。
type source struct {
	arc  string
	path string
	file bool
}

// sources 列出這個家要收的東西（照佈局算位置）；skipped 是家資料夾最上層沒收的（符號連結）。
func sources(home string, l platform.Layout) (out []source, skipped []string) {
	out = []source{
		{arc: "home/home.toml", path: filepath.Join(home, "home.toml"), file: true},
		{arc: "home/settings", path: filepath.Join(home, "settings")},
		{arc: "home/vault", path: filepath.Join(home, "vault")},
	}
	if l.V2 && l.PersonasAtHome {
		// 官方角色放在家裡的家：官方卡與圖是主人自己的原稿（角色包拿不回來），整個 personas/ 都收。
		out = append(out, source{arc: "home/personas", path: filepath.Join(home, "personas")})
	} else {
		out = append(out, source{arc: "home/personas/mine", path: filepath.Join(home, "personas", "mine")})
	}
	out = append(out, source{arc: "home/packages/mine", path: filepath.Join(home, "packages", "mine")})
	if !l.V2 {
		// 第 1 版的「附件與圖庫」散在 runtime/ 底下，只收這兩個（跟搬佈局時搬進 media/ 的是同一組）；
		// 家資料夾裡混著程式自己的東西，其他的不收。
		return append(out,
			source{arc: "home/media/share", path: l.Media("share")},
			source{arc: "home/media/art", path: l.Media("art")}), nil
	}
	out = append(out, source{arc: "home/media", path: l.Media()})
	// 第 2 版的家裡只有主人的東西：最上層其他的檔與資料夾（例如規矩文件 docs/）也收。
	names, links := otherNames(home)
	for _, name := range names {
		path := filepath.Join(home, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue // 剛被刪掉的
		}
		out = append(out, source{arc: "home/other/" + name, path: path, file: info.Mode().IsRegular()})
	}
	for _, name := range links {
		skipped = append(skipped, "home/other/"+name)
	}
	return out, skipped
}

// knownTop 是家資料夾最上層「不算其他東西」的名字：各有自己的類別，或是不收的（暫存、秘密、還原的工作資料夾、git 的紀錄）。
var knownTop = map[string]bool{
	"home.toml": true, "settings": true, "vault": true, "personas": true, "packages": true, "media": true,
	"runtime": true, "secrets": true, restoreDirName: true, ".git": true,
}

// otherNames 列家資料夾最上層「其他的東西」（照名字排）；links 是其中的符號連結（不收、還原時也不碰）。
// 不列：knownTop、系統長出來的檔、照備份規則改名留著的舊東西（名字裡有 .bak-）。
func otherNames(home string) (names, links []string) {
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		name := e.Name()
		if knownTop[name] || skipName(name) || strings.Contains(name, ".bak-") {
			continue
		}
		switch {
		case e.Type()&fs.ModeSymlink != 0:
			links = append(links, name)
		case e.IsDir() || e.Type().IsRegular():
			names = append(names, name)
		}
	}
	sort.Strings(names)
	sort.Strings(links)
	return names, links
}

// skipName：不收的檔名（系統自己長出來的、寫到一半的暫存檔）。
func skipName(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, ".atomic-")
}

// Create 把家裡主人的東西打成一個 .egbackup 檔（服務開著也可以做），做完整份讀一遍、檢查碼都對才算成功。
// 失敗不留半個檔。碰到備份當下被改短的檔會整份重做一次。
func Create(o CreateOptions) (*Created, error) {
	c, err := create(o)
	if errors.Is(err, errChanged) {
		c, err = create(o)
	}
	if errors.Is(err, errChanged) {
		return nil, platform.Unavailable("家裡的檔案一直在變，備份沒做成；等一下再試一次")
	}
	return c, err
}

var errChanged = errors.New("file changed while being backed up")

func create(o CreateOptions) (*Created, error) {
	if o.Home == "" {
		return nil, platform.Unavailable("還不知道家在哪裡")
	}
	if !platform.IsHomeBackupKind(o.Kind) {
		return nil, platform.Invalid("kind", "不認得的備份種類")
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	layout := platform.LayoutFor(o.Home)
	m := &Manifest{
		Format: FormatVersion, Product: platform.ProductName, AppVersion: platform.Version(), CreatedAt: o.Now.UTC().Truncate(time.Second),
		Kind: o.Kind, HomeName: filepath.Base(filepath.Clean(o.Home)), HomeDir: filepath.Clean(o.Home),
	}
	if u, err := os.UserHomeDir(); err == nil {
		m.UserDir = u
	}
	secrets := ""
	if h, err := platform.LoadHome(o.Home); err == nil {
		m.Owner, m.Machine = h.User.Name, h.Runtime.Machine
		layout = h.Paths()
		if secrets = h.SecretsPath(); secrets == "" {
			secrets = layout.Secrets("push.toml")
		}
	} else if _, serr := os.Stat(filepath.Join(o.Home, "home.toml")); serr != nil {
		return nil, platform.NotFound("這個資料夾不是一個家，沒辦法備份")
	}
	m.Migrations = appliedMigrations(o.Home)
	m.Installed = dirNames(layout.InstalledPackages())

	dir := o.Dir
	if dir == "" {
		dir = platform.HomeBackupDir(o.Home, o.Kind)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, platform.Unavailable("建不了放備份的資料夾")
	}
	// 先看空間（HBK-10）：估這次多大，放備份的那顆磁碟剩的不夠就不做（不把硬碟寫滿、不留半個檔）。
	list, skipped := sources(o.Home, layout)
	est := estimate(list)
	if free, ok := enoughSpace(dir, est); !ok {
		return nil, platform.Unavailable("這台電腦剩下的空間不夠做備份：這份大約要 " + sizeText(est) + "，只剩 " + sizeText(free) + "。清出一些空間再試一次")
	}
	name := uniqueName(dir, platform.HomeBackupStem(m.HomeName)+"-"+o.Now.Format("20060102-150405"))
	final := filepath.Join(dir, name)
	part := filepath.Join(dir, "."+name+".part")
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, platform.Unavailable("寫不了備份檔")
	}
	ok := false
	defer func() {
		if !ok {
			_ = f.Close() // 可能已經關過；失敗就是要收掉半個檔
			_ = os.Remove(part)
		}
	}()

	gz, _ := gzip.NewWriterLevel(f, gzip.BestSpeed) // 圖與影片本來就壓過，求快
	tw := tar.NewWriter(gz)
	w := &packer{tw: tw, m: m, counts: map[string]*Section{}, now: m.CreatedAt}
	m.Skipped = append(m.Skipped, skipped...)
	for _, s := range list {
		if err := w.add(s); err != nil {
			return nil, packError(err)
		}
	}
	if err := w.addSecrets(secrets); err != nil {
		return nil, packError(err)
	}
	for _, id := range sectionOrder {
		if c := w.counts[id]; c != nil {
			m.Sections = append(m.Sections, *c)
		}
	}
	sort.Strings(m.Skipped)
	data, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return nil, platform.Internal("寫不出備份的說明檔")
	}
	if err := w.bytes(manifestName, data, 0o644); err != nil {
		return nil, packError(err)
	}
	if err := errors.Join(tw.Close(), gz.Close(), f.Sync(), f.Close()); err != nil {
		return nil, packError(err)
	}
	// 整份讀一遍：讀得開、每個檔的檢查碼都對，才算做好。
	if _, err := Inspect(part); err != nil {
		return nil, platform.Unavailable("備份檔寫完後檢查沒過，沒有留下；請再試一次")
	}
	if err := os.Rename(part, final); err != nil {
		return nil, platform.Unavailable("備份檔放不進資料夾")
	}
	ok = true
	info, err := os.Stat(final)
	if err != nil {
		return nil, platform.Unavailable("讀不到剛做好的備份檔")
	}
	_ = os.Chtimes(final, o.Now, o.Now) // 清單照做的時間排；改不了不影響備份
	return &Created{Path: final, Name: name, Size: info.Size(), Manifest: m}, nil
}

func packError(err error) error {
	if errors.Is(err, errChanged) {
		return err
	}
	var pe *platform.Err
	if errors.As(err, &pe) {
		return err
	}
	return platform.Unavailable("備份沒做成（硬碟滿了或檔案讀不到）")
}

// uniqueName：同一秒做兩份時在後面加 -2、-3…。
func uniqueName(dir, stem string) string {
	name := stem + platform.HomeBackupExt
	for n := 2; ; n++ {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			return name
		}
		name = fmt.Sprintf("%s-%d%s", stem, n, platform.HomeBackupExt)
	}
}

type packer struct {
	tw     *tar.Writer
	m      *Manifest
	counts map[string]*Section
	now    time.Time
}

func (p *packer) count(arc string, size int64) {
	id := sectionOf(arc)
	c := p.counts[id]
	if c == nil {
		c = &Section{ID: id, Label: sectionLabels[id]}
		p.counts[id] = c
	}
	c.Files++
	c.Bytes += size
}

// add 收一個檔或一整個資料夾；不存在就跳過（這個家沒有那一類東西）。
// 整類本身是符號連結（或不是該有的種類）就不做這份備份：收不到裡面的東西，靜靜略過會讓主人以為有備份到。
// 資料夾裡面的符號連結、其他種類的檔不收，記在 Skipped（畫面會說有幾個沒收）。
func (p *packer) add(s source) error {
	info, err := os.Lstat(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.file {
		if !info.Mode().IsRegular() {
			return platform.Conflict("家裡的「" + filepath.Base(s.path) + "」是捷徑（符號連結），備份收不到它的內容，所以沒有備份；把它換成真的檔案再試，或跟任何一位秘書說")
		}
		return p.file(s.arc, s.path)
	}
	if !info.IsDir() {
		return platform.Conflict("家裡的「" + filepath.Base(s.path) + "」資料夾是捷徑（符號連結），備份收不到裡面的東西，所以沒有備份；把它換成真的資料夾再試，或跟任何一位秘書說")
	}
	return filepath.WalkDir(s.path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // 走到一半被刪掉的
			}
			return err
		}
		rel, _ := filepath.Rel(s.path, path)
		arc := s.arc
		if rel != "." {
			arc += "/" + filepath.ToSlash(rel)
		}
		switch {
		case d.IsDir():
			return p.tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: arc + "/", Mode: 0o755, ModTime: p.now})
		case skipName(d.Name()):
			return nil
		case !d.Type().IsRegular():
			p.m.Skipped = append(p.m.Skipped, arc)
			return nil
		}
		return p.file(arc, path)
	})
}

// file 收一個一般檔：照打開當下的大小寫進去，邊寫邊算檢查碼。寫的時候檔變短了回 errChanged（整份重做）。
func (p *packer) file(arc, path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	// 權限只記三種：只有自己能讀的（設定裡可能有不想給別人看的）、可以執行的（套件的腳本）、一般的。
	mode := int64(0o644)
	switch perm := info.Mode().Perm(); {
	case perm&0o077 == 0 && perm&0o100 != 0:
		mode = 0o700
	case perm&0o077 == 0:
		mode = 0o600
	case perm&0o111 != 0:
		mode = 0o755
	}
	if err := p.tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: arc, Size: info.Size(), Mode: mode, ModTime: info.ModTime()}); err != nil {
		return err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(p.tw, sum), io.LimitReader(f, info.Size()))
	if err != nil {
		return err
	}
	if n != info.Size() {
		return errChanged
	}
	p.m.Files = append(p.m.Files, FileEntry{Path: arc, Size: n, SHA256: hex.EncodeToString(sum.Sum(nil))})
	p.count(arc, n)
	return nil
}

// bytes 把一段內容寫成備份檔裡的一個檔（說明檔不列進 Files）。
func (p *packer) bytes(arc string, data []byte, mode int64) error {
	if err := p.tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: arc, Size: int64(len(data)), Mode: mode, ModTime: p.now}); err != nil {
		return err
	}
	if _, err := p.tw.Write(data); err != nil {
		return err
	}
	if arc != manifestName {
		sum := sha256.Sum256(data)
		p.m.Files = append(p.m.Files, FileEntry{Path: arc, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])})
		p.count(arc, int64(len(data)))
	}
	return nil
}

// addSecrets 只收秘密檔的 [webpush]（推播金鑰）：秘密檔裡別的東西不進備份。沒有秘密檔、沒有金鑰、讀不懂都跳過。
func (p *packer) addSecrets(path string) error {
	data, ok := webpushOnly(path)
	if !ok {
		return nil
	}
	return p.bytes("secrets/push.toml", data, 0o600)
}

// webpushOnly 讀秘密檔，回只有 [webpush] 那一段的 TOML。
func webpushOnly(path string) ([]byte, bool) {
	if path == "" {
		return nil, false
	}
	raw := map[string]any{}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, false
	}
	wp, ok := raw["webpush"].(map[string]any)
	if !ok || len(wp) == 0 {
		return nil, false
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(map[string]any{"webpush": wp}); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// appliedMigrations 讀 settings/migrations.toml 的 applied（沒有檔案或讀不懂＝空的）。
func appliedMigrations(home string) []string {
	var d struct {
		Applied []string `toml:"applied"`
	}
	if _, err := toml.DecodeFile(filepath.Join(home, "settings", "migrations.toml"), &d); err != nil {
		return nil
	}
	return d.Applied
}

// dirNames 列出資料夾底下的子資料夾名（排序過；不存在＝空的）。
func dirNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}
