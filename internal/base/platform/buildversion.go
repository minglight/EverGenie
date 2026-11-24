package platform

import (
	"runtime/debug"
)

// PersonaPackSource：這個家是角色包的來源（[admin] public_personas 有寫）：角色庫以這裡為準，不同步別人發的角色包。
func (h *Home) PersonaPackSource() bool { return len(h.Admin.PublicPersonas) > 0 }

// Version 是這個執行檔的版本：發版時用 -ldflags "-X github.com/minglight/evergenie/internal/base/platform.BuildVersion=…" 指定；
// 沒指定就用 Go 內嵌的 git 版本資訊（commit 前 7 碼，有未提交的改動加 -dirty），再不然是 "dev"。
func Version() string {
	if BuildVersion != "" {
		return BuildVersion
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}

// BuildVersion 由 -ldflags 填；空字串表示沒指定。
var BuildVersion string
