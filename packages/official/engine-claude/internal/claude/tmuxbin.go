package claude

import (
	"os"
	"path/filepath"

	"github.com/minglight/evergenie/sdk"
)

// 引擎用哪一支 tmux（ECL-63、ECL-65，DEV-155）：發行檔把自己編的 tmux（scripts/build-tmux.sh）跟著程式一起帶，
// 一般人的 Mac 不用另外裝；都沒有（開發機從原始碼編的）才用 PATH 上的（例如 Homebrew 裝的）。

// tmuxExe 是 PATH 上與引擎旁邊（ECL-63，舊的發行檔）那支 tmux 的檔名。
const tmuxExe = "tmux"

// 發行檔裡固定位置的檔案（ECL-65）：包裡的 fixed/ 放附帶的 tmux（檔名是產品名）與開 tmux server 用的小啟動程式；
// 安裝與升級把它們放到程式資料夾的 fixed/（不在帶版本號的資料夾裡，內容沒變就不換；核心 system 卡 UPD-15）。
const (
	fixedDirName = sdk.DownloadFixedDir // 跟核心放檔的位置同一個名字（只定義在 sdk）
	launcherExe  = "disclaim"           // 小啟動程式（scripts/build-tmux.sh 編）：讓 tmux 自己當系統權限視窗的負責程式，再換成 tmux
)

// bundledTmuxName 是附帶的 tmux 在 fixed/ 裡的檔名＝產品名（系統的權限視窗上顯示的就是檔名）。
// scripts/dist.sh 編發行檔時用 -ldflags -X 帶入（來源是 scripts/install.sh 的 PRODUCT，改名只改那一行）；
// 空＝不是發行檔（從原始碼編的），不找 fixed/。
var bundledTmuxName string

// tmuxChoice 是找到的 tmux。
type tmuxChoice struct {
	bin      string // 完整路徑；空＝哪裡都沒有
	launcher string // 開 tmux server 時先經過的小啟動程式（跟 bin 同一個資料夾）；空＝直接開
	bundled  bool   // 跟著程式一起裝的（不是 PATH 上的）
	stable   bool   // 在程式資料夾的固定位置 <app>/fixed/（路徑不帶版本號，升級後系統不重問；ECL-65）
}

// chooseTmux 照順序找：程式資料夾的 fixed/ ＞ 這一版發行檔自己的 fixed/ ＞ 引擎旁邊的 tmux ＞ PATH 上的。
func chooseTmux(self string, lookPath func(string) (string, error)) tmuxChoice {
	if c := fixedTmux(self); c.bin != "" {
		return c
	}
	if p := bundledTmux(self); p != "" {
		return tmuxChoice{bin: p, bundled: true}
	}
	p, err := lookPath(tmuxExe)
	if err != nil || p == "" {
		return tmuxChoice{}
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return tmuxChoice{bin: p}
}

// fixedTmux 找固定位置的 tmux（ECL-65）。引擎執行檔（解開符號連結後）是 <app>/versions/<版本>/bin/<引擎> 時，
// 先找 <app>/fixed/<產品名>：路徑跟內容在升級後都不變，主人按過的允許一直算數。
// 那裡沒有（還沒放好、或從還不會放的舊版升上來）才找這一版自己的 <版本>/fixed/<產品名>：權限視窗一樣顯示產品名，
// 但路徑帶版本號，升級後會再問一次。兩種都跟 ECL-63 一樣：引擎是經過沒解開的路徑（例如 <app>/current/bin）叫起來、
// 那條路徑算出來的位置指到同一個檔時，回那條路徑（給人貼的 attach 指令、記在 session 上的路徑不帶版本號）。
func fixedTmux(self string) tmuxChoice {
	if self == "" || bundledTmuxName == "" {
		return tmuxChoice{}
	}
	real, err := filepath.EvalSymlinks(self)
	if err != nil {
		return tmuxChoice{}
	}
	bin := filepath.Dir(real)
	ver := filepath.Dir(bin)
	nearBin := filepath.Dir(self)
	if versions := filepath.Dir(ver); filepath.Base(bin) == "bin" && filepath.Base(versions) == "versions" {
		if c := tmuxIn(filepath.Join(filepath.Dir(versions), fixedDirName)); c.bin != "" {
			c = nearer(c, filepath.Join(filepath.Dir(filepath.Dir(nearBin)), fixedDirName))
			c.stable = true
			return c
		}
	}
	if c := tmuxIn(filepath.Join(ver, fixedDirName)); c.bin != "" {
		return nearer(c, filepath.Join(filepath.Dir(nearBin), fixedDirName))
	}
	return tmuxChoice{}
}

// nearer：從沒解開連結的引擎路徑算出來的 fixed/ 資料夾 near 裡的那支跟 c 是同一個檔時，改用 near 的路徑；不是就照舊。
func nearer(c tmuxChoice, near string) tmuxChoice {
	near, err := filepath.Abs(near)
	if err != nil || near == filepath.Dir(c.bin) {
		return c
	}
	if nc := tmuxIn(near); nc.bin != "" && sameFile(nc.bin, c.bin) {
		return nc
	}
	return c
}

// tmuxIn 看一個 fixed/ 資料夾：有能執行的 <產品名> 就用它，旁邊有能執行的小啟動程式就一起帶。
func tmuxIn(dir string) tmuxChoice {
	p := filepath.Join(dir, bundledTmuxName)
	if !isExecFile(p) {
		return tmuxChoice{}
	}
	c := tmuxChoice{bin: p, bundled: true}
	if l := filepath.Join(dir, launcherExe); isExecFile(l) {
		c.launcher = l
	}
	return c
}

// bundledTmux 是引擎執行檔旁邊的 tmux（ECL-63）：先解開 self 的符號連結，在真正的資料夾裡找；
// 不在、是資料夾或不能執行都回空字串。
// 沒解開的路徑旁邊（例如下載版的 current/bin/tmux）指到同一個檔就回那個：接上畫面的指令會記在 session 上，
// 帶版本號的路徑在升級、舊版資料夾刪掉後就失效，current/ 底下的不會。
func bundledTmux(self string) string {
	if self == "" {
		return ""
	}
	real, err := filepath.EvalSymlinks(self)
	if err != nil {
		return ""
	}
	p := filepath.Join(filepath.Dir(real), tmuxExe)
	if !isExecFile(p) {
		return ""
	}
	if near, err := filepath.Abs(filepath.Join(filepath.Dir(self), tmuxExe)); err == nil && near != p && sameFile(near, p) {
		return near
	}
	return p
}

// isExecFile：p 是一般檔而且能執行（符號連結照指到的算）。
func isExecFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}
