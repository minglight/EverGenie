package run

import "time"

// 寫進 vault 的資料形狀。欄位與 JSON 名稱照舊的 sdk.DevReleaseResponse／sdk.DevHomeStatus（畫面換讀 vault 時不用改解析），
// 但型別放在套件自己這裡：S5 拿掉核心的 dev 程式時 sdk 的那些型別會一起拿掉，套件不能跟著壞。

// Release 是 admin.release/current 的內容（發版狀態）。
type Release struct {
	// Version 是這台核心跑的版本（scripts/version.sh 的格式）；讀不到是空的。
	// 跨台時代的 sites（每台一列：site、role hub／node、online、version）2026-11-01 拿掉（法律第 7 條：每台一個家）；
	// 舊資料帶著 sites 照樣讀得到，下一輪整筆換掉。
	Version     string   `json:"version,omitempty"`
	Main        *Commit  `json:"main,omitempty"`    // origin/main 最新；抓不到是 nil
	Pending     []Commit `json:"pending,omitempty"` // main 有、本機版本還沒有的，新到舊，最多 30
	PendingMore bool     `json:"pending_more"`      // 超過 30 個
	Detail      string   `json:"detail,omitempty"`  // 白話：抓不到 main 的原因、本機版本認不出 commit 等；正常時空
	Latest      *Tag     `json:"latest,omitempty"`  // 上一個正式版（vX.Y.Z）；還沒有是 nil
	Safe        *Tag     `json:"safe,omitempty"`    // 最新的 safe/；還沒有是 nil
	Nightly     *Nightly `json:"nightly,omitempty"` // release_state 底下的 nightly.json
	Next        *Next    `json:"next,omitempty"`    // release_state 底下的 next.json
}

// Commit 是一個 commit。
type Commit struct {
	Commit  string    `json:"commit,omitempty"`
	Short   string    `json:"short,omitempty"`
	Subject string    `json:"subject,omitempty"`
	At      time.Time `json:"at,omitempty"`
}

// Tag 是一個發版 tag。
type Tag struct {
	Name    string    `json:"name,omitempty"`
	Version string    `json:"version,omitempty"` // 正式版的版號（去掉 v）；safe 是空的
	Commit  string    `json:"commit,omitempty"`
	Short   string    `json:"short,omitempty"`
	At      time.Time `json:"at,omitempty"`
	Notes   string    `json:"notes,omitempty"`
}

// Nightly 是上次每晚測試的結果（scripts/release.sh nightly 寫的 nightly.json）。
type Nightly struct {
	Commit  string        `json:"commit,omitempty"`
	Short   string        `json:"short,omitempty"`
	At      time.Time     `json:"at,omitempty"`
	OK      bool          `json:"ok"`
	Steps   []NightlyStep `json:"steps,omitempty"`
	SafeTag string        `json:"safe_tag,omitempty"`
}

// NightlyStep 是每晚測試的一步。
type NightlyStep struct {
	Name    string `json:"name,omitempty"`
	OK      bool   `json:"ok"`
	Seconds int32  `json:"seconds"`
	Log     string `json:"log,omitempty"`
}

// Next 是下一個正式版的草稿（scripts/release.sh prepare 寫的 next.json）。
type Next struct {
	Version string `json:"version,omitempty"`
	Base    string `json:"base,omitempty"`
	SafeTag string `json:"safe_tag,omitempty"`
	Notes   string `json:"notes,omitempty"`
	// Draft：說明還是 prepare 原樣列的 commit 標題（admin 秘書用 release.sh notes 改寫成白話後是 false，DEV-113）
	Draft   bool `json:"draft,omitempty"`
	Commits int  `json:"commits,omitempty"` // 草稿算進去的 commit 數（只重新打包看板的不算）
}

// 開發家的狀態（admin.devhome/current 的 state）。
const (
	DevHomeUnset   = "unset"   // home.toml 沒寫 [admin] dev_home
	DevHomeStopped = "stopped" // 有寫，但 1 秒內沒有回應
	DevHomeRunning = "running"
)

// DevHome 是 admin.devhome/current 的內容。
type DevHome struct {
	State     string `json:"state"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	BoardPort int    `json:"board_port"`
	Detail    string `json:"detail,omitempty"`
	URL       string `json:"url,omitempty"`
}
