package platform

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// 版本與更新（P8 第二輪 S2、REQ-280、REQ-281）：home.toml 的 [update]，以及升級過程放在 <家>/runtime/upgrade/ 的狀態檔。
// 升級本身（抓、編、換、重開、檢查、退回）、狀態檔的格式與家的遷移在業務模組 internal/feature/system/update；這裡只放 home.toml 的頻道設定
// 與通用的 JSON 檔讀寫（2026-11-01 家的遷移搬去 update）。

// 更新頻道。
const (
	UpdateChannelRelease = "release"
	UpdateChannelSafe    = "safe"
	UpdateChannelOff     = "off"
)

// DefaultUpdateSource 是下載版沒寫 [update] source 時找新版的版本清單網址：發行時用
// -ldflags "-X github.com/minglight/evergenie/internal/base/platform.DefaultUpdateSource=…" 編進去（scripts/dist.sh --source）。
// 空字串＝沒有內建的下載來源（從原始碼編的；dist.sh 沒給 --source 時編的是 scripts/install.sh 的 DEFAULT_SOURCE，佔位的 .invalid 網址才是空的）。
var DefaultUpdateSource string

// UpdateSource 是下載版找新版用的版本清單網址：[update] source ＞ 編進執行檔的 DefaultUpdateSource；都沒有回空字串。
func (h *Home) UpdateSource() string {
	if h.Update.Source != "" {
		return h.Update.Source
	}
	return DefaultUpdateSource
}

// UpdateChannel 是這個家實際用的頻道：有寫就照寫的，沒寫是 release。用部署工具更新的家（例：發行者的兩台，K4）自己寫 off。
func (h *Home) UpdateChannel() string {
	if h.Update.Channel != "" {
		return h.Update.Channel
	}
	return UpdateChannelRelease
}

// ReadJSONFile 讀 JSON 檔到 v；檔案不存在回 false、nil。
func ReadJSONFile(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, Unavailable("讀不了 " + path)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, Invalid("json", path+" 的格式不對")
	}
	return true, nil
}

// WriteJSONFile 原子地寫 JSON 檔（建好資料夾）。
func WriteJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return Internal("寫不出 JSON")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Unavailable("建不了資料夾 " + filepath.Dir(path))
	}
	return AtomicWrite(path, append(data, '\n'), 0o600)
}
