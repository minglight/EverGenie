package claude

import (
	"os"
	"path/filepath"
)

// 官方技能只給秘書用（DEV-42、ECL-36）：本套件資料夾本身就是一個 Claude 外掛（.claude-plugin/plugin.json＋skills/），
// 開秘書 session 時用 --plugin-dir 帶進去，只作用於那個 session；不裝進帳號的 skills/，主人自己開的 Claude 看不到。

// PluginManifest 是外掛說明檔相對套件資料夾的位置。
const PluginManifest = ".claude-plugin/plugin.json"

// FindPluginDir 找本套件的資料夾（要有外掛說明檔與 skills/）：依序 EVERGENIE_OFFICIAL_PACKAGES_DIR/engine-claude、
// 執行檔旁邊的 ../packages/official/engine-claude 與 packages/official/engine-claude（跟核心找官方套件的順序一致）。
// exe 是自己執行檔的路徑（符號連結會先解開）；找不到回空字串，session 照常開，只是沒有官方技能。
func FindPluginDir(getenv func(string) string, exe string) string {
	var cands []string
	if v := getenv("EVERGENIE_OFFICIAL_PACKAGES_DIR"); v != "" {
		cands = append(cands, filepath.Join(v, "engine-claude"))
	}
	if exe != "" {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		dir := filepath.Dir(exe)
		cands = append(cands,
			filepath.Join(dir, "..", "packages", "official", "engine-claude"),
			filepath.Join(dir, "packages", "official", "engine-claude"))
	}
	for _, c := range cands {
		if isPluginDir(c) {
			if abs, err := filepath.Abs(c); err == nil {
				return filepath.Clean(abs)
			}
			return filepath.Clean(c)
		}
	}
	return ""
}

func isPluginDir(dir string) bool {
	if info, err := os.Stat(filepath.Join(dir, PluginManifest)); err != nil || info.IsDir() {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "skills"))
	return err == nil && info.IsDir()
}
