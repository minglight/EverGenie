package sdk

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// 家的佈局（DEV-145）：第 2 版的家只放主人的東西，暫存（runtime）、密鑰（secrets）、官方角色包快取放家外面的狀態資料夾；
// 主人的附件與圖庫放家裡的 media/。核心照 home.toml 的 [layout] 算好位置，開套件、引擎時用下面的環境變數傳下來；
// 套件與引擎一律用這幾個函式拿路徑，不要自己 Join(home, "runtime", …)。環境變數沒有（舊核心開的行程、搬家前就開著的 session）就自己讀 <家>/home.toml 的 [layout]，
// 沒寫才是第 1 版的位置；這樣搬完家，還沒重開的舊行程也不會把暫存寫回家裡。
const (
	EnvState            = "EVERGENIE_STATE"             // 狀態資料夾：底下有 runtime/、secrets/、personas/（官方角色快取）
	EnvMedia            = "EVERGENIE_MEDIA"             // 主人的檔案：share/（附件）、art/（圖庫）…
	EnvOfficialPersonas = "EVERGENIE_OFFICIAL_PERSONAS" // 官方角色卡、頭像、衣櫃、目錄的位置
	EnvBundledPersonas  = "EVERGENIE_BUNDLED_PERSONAS"  // 跟著程式的公開角色卡（那一版程式資料夾裡官方套件 personas 的 cards/，唯讀；PRS-25）
)

// DownloadFixedDir 是下載版的「固定位置」資料夾名（DEV-155）：每一版發行檔的 fixed/、程式資料夾的 <app>/fixed/。
// 放要一直待在同一個路徑、內容沒變就不能換的檔；核心升級時把新版的放過去（system 卡 UPD-15），引擎從這裡找它附帶的程式。
// 核心與引擎（另一個 Go 模組，只 import sdk）都用這個名字，不自己寫字串。
const DownloadFixedDir = "fixed"

// RuntimeDir 回傳家的暫存資料夾（可以刪的：sockets、logs、sessions、queue…）並接上 parts。
func RuntimeDir(home string, parts ...string) string {
	base := filepath.Join(home, "runtime")
	if s := Getenv(EnvState); s != "" {
		base = filepath.Join(s, "runtime")
	} else if st, ok := fileLayoutState(home); ok {
		base = filepath.Join(st, "runtime")
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

// MediaDir 回傳主人檔案的資料夾（第 1 版在 runtime/ 底下）並接上 parts，例：MediaDir(home, "share")。
func MediaDir(home string, parts ...string) string {
	base := filepath.Join(home, "runtime")
	if m := Getenv(EnvMedia); m != "" {
		base = m
	} else if _, ok := fileLayoutState(home); ok {
		base = filepath.Join(home, "media")
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

// SharesDir 是 /share/<名字> 附件的資料夾。
func SharesDir(home string) string { return MediaDir(home, "share") }

// OfficialPersonasDir 回傳官方角色（卡片 <代號>.md、avatars/、wardrobe/、catalog/）的資料夾並接上 parts；第 1 版是家裡的 personas/。
// 主人自己的角色永遠在家裡的 personas/mine/，不走這裡。
func OfficialPersonasDir(home string, parts ...string) string {
	base := filepath.Join(home, "personas")
	if p := Getenv(EnvOfficialPersonas); p != "" {
		base = p
	} else if st, atHome, ok := fileLayout(home); ok && !atHome {
		base = filepath.Join(st, "personas")
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

// BundledPersonasDir 回跟著程式的公開角色卡資料夾（PRS-25）：核心開套件時用 EVERGENIE_BUNDLED_PERSONAS 傳下來；
// 沒有（舊核心開的行程、自己跑的測試）回空字串＝只看官方角色資料夾。
func BundledPersonasDir() string { return Getenv(EnvBundledPersonas) }

// OfficialCardPath 是找官方角色卡 <代號>.md 的共用入口（PRS-25）。同代號時的順序：官方角色資料夾（角色包同步來的、舊家原本就有的）
// ＞主人自己的卡（<家>/personas/mine/<代號>.md）＞跟著程式的那張——主人有同代號的卡時，跟著程式的那張不算官方卡（被主人的蓋過）。
// 找到回 (位置, true)，沒有官方卡回 (官方角色資料夾裡該在的位置, false)，代號不合格回 ("", false)。只讀。
func OfficialCardPath(home, slug string) (string, bool) {
	mine := ""
	if home != "" {
		mine = filepath.Join(home, "personas", "mine") // 主人自己的角色永遠在家裡
	}
	return OfficialCardIn(OfficialPersonasDir(home), BundledPersonasDir(), mine, slug)
}

// OfficialCardIn 是 OfficialCardPath 的同一條規則，三個資料夾由呼叫的人給（核心的角色庫用組裝傳進來的位置）；
// bundledDir 空＝不看跟著程式的卡，mineDir 空＝不看主人的卡（跟著程式的那張不會被蓋過）。
// 官方角色資料夾裡有這個名字（不管是什麼）就用它、不往下找，讀不了也一樣（不悄悄換成另一張）；
// 主人的資料夾裡有這張卡（HasMineCard）就不用跟著程式的那張；跟著程式的那張要是一般檔。
// 代號是空的、有路徑分隔或 . 開頭的一律當找不到，回空的位置（不回拼好的路徑，免得不看 ok 的人照著讀到資料夾外面）。
func OfficialCardIn(officialDir, bundledDir, mineDir, slug string) (string, bool) {
	if slug == "" || strings.ContainsAny(slug, `/\`) || strings.HasPrefix(slug, ".") {
		return "", false
	}
	p := filepath.Join(officialDir, slug+".md")
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		return p, err == nil
	}
	if bundledDir != "" && !HasMineCard(mineDir, slug) {
		b := filepath.Join(bundledDir, slug+".md")
		if info, err := os.Stat(b); err == nil && info.Mode().IsRegular() {
			return b, true
		}
	}
	return p, false
}

// HasMineCard：主人自己的卡 <mineDir>/<代號>.md 在不在（PRS-27 的「我的有」；核心的匯入檢查也用這個）。
// 資料夾裡要有這個確切的檔名：macOS 檔名不分大小寫，Venus.md 不算 venus 的卡（角色庫也不列它）；同名的是資料夾不算（角色庫不當它是卡）；
// 其他（一般檔、捷徑、壞掉的捷徑）都算，mineDir 讀不了也算有——主人的東西寧可當作有，不悄悄換成跟著程式的那張。mineDir 空＝不看。
func HasMineCard(mineDir, slug string) bool {
	if mineDir == "" || slug == "" {
		return false
	}
	entries, err := os.ReadDir(mineDir)
	if err != nil {
		return !os.IsNotExist(err)
	}
	name := slug + ".md"
	for _, e := range entries {
		if e.Name() == name {
			return !e.IsDir()
		}
	}
	return false
}

// ResolveAPISocket：sock 是某個家第 1 版的標準位置（<家>/runtime/sockets/api.sock），而那個家已搬成第 2 版時，
// 回第 2 版的位置（<狀態資料夾>/runtime/sockets/api.sock）；其他情況原樣回傳。搬家前就開著的 session，
// 引擎回報用的指令裡寫死了舊的 socket，靠這個在搬完後還連得到核心（DEV-145）。
func ResolveAPISocket(sock string) string {
	tail := string(filepath.Separator) + filepath.Join("runtime", "sockets", "api.sock")
	if !strings.HasSuffix(sock, tail) {
		return sock
	}
	if st, ok := fileLayoutState(strings.TrimSuffix(sock, tail)); ok {
		return filepath.Join(st, "runtime", "sockets", "api.sock")
	}
	return sock
}

// LayoutStateOf 回傳 dir 這個家的狀態資料夾（只看它自己的 home.toml，不看環境變數）；第 1 版回 ("", false)。
// 用在算「別的家」的路徑（例：admin 套件找開發家的 socket）。
func LayoutStateOf(dir string) (string, bool) { return fileLayoutState(dir) }

// fileLayoutState 讀 <家>/home.toml 的 [layout]：version = 2 才回狀態資料夾（state 沒寫＝~/.evergenie/state/<家的資料夾名>，
// 相對路徑從家算，~ 展開）。sdk 不帶 TOML 函式庫，這裡只認 [layout] 表裡 `鍵 = 值` 的單行寫法（relayout 寫的就是這樣），
// 跟核心 platform.NewLayout 的規則一致。讀不到或格式看不懂就當第 1 版。
func fileLayoutState(home string) (string, bool) {
	st, _, ok := fileLayout(home)
	return st, ok
}

// fileLayout 同 fileLayoutState，另外回官方角色是不是留在家裡（[layout] official_personas = "home"，relayout 預設）。
func fileLayout(home string) (state string, personasAtHome, ok bool) {
	if home == "" {
		return "", false, false
	}
	f, err := os.Open(filepath.Join(home, "home.toml"))
	if err != nil {
		return "", false, false
	}
	defer f.Close()
	in, version, official := false, 0, ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			in = strings.TrimSpace(strings.Trim(line, "[]")) == "layout"
			continue
		}
		if !in {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if i := strings.Index(v, " #"); i >= 0 && !strings.HasPrefix(v, "\"") {
			v = strings.TrimSpace(v[:i])
		}
		switch k {
		case "version":
			version, _ = strconv.Atoi(v)
		case "state":
			state = unquoteTOML(v)
		case "official_personas":
			official = unquoteTOML(v)
		}
	}
	if version != 2 {
		return "", false, false
	}
	if state == "~" || strings.HasPrefix(state, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			state = filepath.Join(h, strings.TrimPrefix(state, "~"))
		}
	}
	switch {
	case state == "":
		h, err := os.UserHomeDir()
		if err != nil {
			return "", false, false
		}
		state = filepath.Join(h, "."+ProductName, "state", filepath.Base(filepath.Clean(home)))
	case !filepath.IsAbs(state):
		state = filepath.Join(home, state)
	}
	return state, official == "home", true
}

// unquoteTOML 拿掉字串的引號（後面接著 # 註解也認得）。
func unquoteTOML(v string) string {
	if u, err := strconv.Unquote(v); err == nil {
		return u
	}
	if strings.HasPrefix(v, "\"") {
		if prefix, err := strconv.QuotedPrefix(v); err == nil {
			if u, err := strconv.Unquote(prefix); err == nil {
				return u
			}
		}
	}
	if strings.HasPrefix(v, "'") {
		if end := strings.Index(v[1:], "'"); end >= 0 {
			return v[1 : end+1]
		}
	}
	return strings.Trim(v, "'")
}
