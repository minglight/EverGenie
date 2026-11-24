package platform

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/minglight/evergenie/sdk"
)

// 下載版的安裝資料夾（system 卡 UPD-11）：<app>/versions/<版本>/（每版一份解開的發行檔，有 VERSION）、
// <app>/current → versions/<版本>（相對符號連結），<app> 本身不是 git repo。
// <app>/fixed/ 放發行檔 fixed/ 裡、要一直待在同一個路徑的檔（UPD-15：安裝與升級時內容變了才換）。
// <app>/tools/ 放主人同意後才下載的編譯工具（packages 卡 PKG-65：tools/go）；升級、退回只動 versions/、current、fixed/，不碰它。
// 「這是不是下載版」只在這裡判斷一次：指標檔的位置（PLT-21）與升級（update 模組）共用。
const (
	DownloadVersionsDir = "versions"
	DownloadCurrentLink = "current"
	DownloadVersionFile = "VERSION"
	DownloadFixedDir    = sdk.DownloadFixedDir // 名字只定義在 sdk：引擎找附帶的程式也用它
	DownloadToolsDir    = "tools"
)

// IsDownloadInstall：dir 是下載版的安裝資料夾（current 是符號連結、指到 versions/ 底下有 VERSION 的一版，dir 本身不是 git repo）。
func IsDownloadInstall(dir string) bool {
	if dir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return false
	}
	info, err := os.Lstat(filepath.Join(dir, DownloadCurrentLink))
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return false
	}
	name := DownloadCurrentVersion(dir)
	if name == "" {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, DownloadVersionsDir, name, DownloadVersionFile))
	return err == nil
}

// DownloadCurrentVersion 回 current 指到的版本資料夾名稱（versions/ 底下那一層）；不是指到 versions/ 底下就回空字串。
func DownloadCurrentVersion(app string) string {
	link, err := os.Readlink(filepath.Join(app, DownloadCurrentLink))
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(app, link)
	}
	link = filepath.Clean(link)
	if filepath.Dir(link) != filepath.Join(filepath.Clean(app), DownloadVersionsDir) {
		return ""
	}
	return filepath.Base(link)
}

// DownloadInstallOf：exe（符號連結先解開）是下載版某一版的執行檔（<app>/versions/<版本>/bin/<執行檔>，那一版有 VERSION、
// 不是 git checkout、<app> 是下載版的安裝資料夾）時回 <app>；不是回空字串。升級換了 current 之後，舊版的執行檔也還認得出來。
func DownloadInstallOf(exe string) string {
	if exe == "" {
		return ""
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	bin := filepath.Dir(real)
	versions := filepath.Dir(filepath.Dir(bin))
	if filepath.Base(bin) != "bin" || filepath.Base(versions) != DownloadVersionsDir {
		return ""
	}
	ver := filepath.Dir(bin)
	if _, err := os.Stat(filepath.Join(ver, DownloadVersionFile)); err != nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(ver, ".git")); err == nil {
		return "" // 那一層是 git checkout：原始碼版
	}
	app := filepath.Dir(versions)
	if !IsDownloadInstall(app) {
		return ""
	}
	return app
}
