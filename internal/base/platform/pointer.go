package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// PointerFile 是程式資料夾（EverGenie repo 的 checkout）根目錄的指標檔：寫著這份程式屬於哪個家（`home = "<家的資料夾>"`）。
// 不進 git。找家的順序：--home 旗標 > $EVERGENIE_HOME > 執行檔所在 checkout（bin/ 的上一層）的指標檔 > 各指令原本的行為。
// 連 API 時再加一條（DEV-88，cmdline.SocketPath）：資料指令的 --home 會先換成 $EVERGENIE_HOME；$EVERGENIE_API_SOCKET（秘書 session 預設有）
// 若是標準位置（<家>/runtime/sockets/api.sock）卻屬於別的家，代表有人明講了要連另一個家，就照 $EVERGENIE_HOME 那個家的 socket，
// 並在 stderr 提醒「這次連的是另一個家：<標籤>」；socket 屬於同一個家、或不是標準位置，照 $EVERGENIE_API_SOCKET。
// serve／init／service／tailscale 的 --home 是各指令自己的意思（開哪個家、建在哪、對哪個家動作），全域的 --home 不碰它們。
const PointerFile = ProductName + ".local.toml"

// ReadPointer 讀 <checkout>/evergenie.local.toml 的 home：開頭的 ~ 展開成使用者家目錄，相對路徑以 checkout 為準。
// 沒有這個檔回空字串、沒有錯誤；檔案壞了或沒寫 home 回 Invalid。
func ReadPointer(checkout string) (string, error) {
	data, err := os.ReadFile(filepath.Join(checkout, PointerFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", Unavailable("讀不到 " + PointerFile + "，請檢查存取權限")
	}
	var p struct {
		Home string `toml:"home"`
	}
	if _, err := toml.Decode(string(data), &p); err != nil {
		return "", Invalid("home", PointerFile+" 格式不對：要寫成 home = \"<家的資料夾>\"")
	}
	home := strings.TrimSpace(p.Home)
	if home == "" {
		return "", Invalid("home", PointerFile+" 沒有寫 home = \"<家的資料夾>\"")
	}
	home = ExpandHome(home)
	if !filepath.IsAbs(home) {
		home = filepath.Join(checkout, home)
	}
	return filepath.Clean(home), nil
}

// PointerHome 從執行檔的位置找指標檔（pointerDir 那個資料夾裡的 evergenie.local.toml）。
func PointerHome(exe string) (string, error) {
	dir := pointerDir(exe)
	if dir == "" {
		return "", nil
	}
	return ReadPointer(dir)
}

// pointerDir 是指標檔該放的資料夾：exe 是下載版某一版的執行檔時是安裝資料夾（DownloadInstallOf；指標檔才不會跟著版本換掉，
// 升級換 current 之後還找得到）；其他是 exe（符號連結先解開）所在資料夾的上一層（原始碼版就是 checkout）。exe 空的回空字串。
func pointerDir(exe string) string {
	if exe == "" {
		return ""
	}
	if app := DownloadInstallOf(exe); app != "" {
		return app
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(filepath.Dir(exe))
}

// ExecutableDirs 是「核心執行檔旁邊」的候選資料夾：先是執行檔被叫的那個路徑所在的資料夾（照原樣，不解開符號連結），
// 解開符號連結之後不一樣的話再加那一個。下載版的服務跑 <安裝資料夾>/current/bin/evergenie（current 是符號連結）：
// 照原樣的路徑就找得到 current/bin、current/packages/official；從 ~/.local/bin/evergenie 這類連結叫起來時，解開後的那個才對。
// 找引擎與官方套件的執行檔、官方套件資料夾都用它（順序不變，只是多一個退路）。找不到執行檔回 nil。
func ExecutableDirs() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	return executableDirs(exe)
}

func executableDirs(exe string) []string {
	if exe == "" {
		return nil
	}
	dirs := []string{filepath.Dir(exe)}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		if d := filepath.Dir(real); d != dirs[0] {
			dirs = append(dirs, d)
		}
	}
	return dirs
}
