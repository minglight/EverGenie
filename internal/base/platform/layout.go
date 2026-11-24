package platform

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// 家的佈局（DEV-145，ai_docs/schemas/home.md「佈局」）。
//
// 第 1 版（沒寫 [layout]）：全部在家裡，暫存在 <家>/runtime/、密鑰在 <家>/secrets/、附件在 <家>/runtime/share/、
// 官方角色複本在 <家>/personas/。
// 第 2 版（[layout] version = 2）：家裡只放主人的東西。暫存、密鑰、官方角色包快取、別人的套件放家外面的狀態資料夾
// （[layout] state，沒寫＝~/.evergenie/state/<家的資料夾名>）；主人的附件與圖庫放 <家>/media/。
//
// 核心一律用 LayoutFor(home) 的方法拿路徑，不要自己 Join(home, "runtime", …)；開套件、引擎、session 時把 Env() 加進環境，
// 子行程用 sdk.RuntimeDir 等函式讀。
type LayoutConfig struct {
	Version int    `toml:"version"`
	State   string `toml:"state"`
	// OfficialPersonas 是官方角色放哪：沒寫＝狀態資料夾；"home"＝家裡的 personas/（relayout 預設寫這個；官方卡可能是主人的原稿或拿不回來）。
	OfficialPersonas string `toml:"official_personas"`
}

// Layout 是算好的位置。Home 是家；State 只有第 2 版才有。PersonasAtHome：第 2 版但官方角色留在家裡（official_personas = "home"）。
type Layout struct {
	Home           string
	State          string
	V2             bool
	PersonasAtHome bool
}

// LayoutVersion 回傳設定的版本（沒寫＝1）。
func (c LayoutConfig) LayoutVersion() int {
	if c.Version == 0 {
		return 1
	}
	return c.Version
}

// DefaultStateDir 是第 2 版沒寫 state 時的狀態資料夾。
func DefaultStateDir(home string) string {
	return filepath.Join(ExpandHome("~/."+ProductName+"/state"), filepath.Base(filepath.Clean(home)))
}

// NewLayout 照設定算出位置；版本不是 2 的一律當第 1 版（驗證在 LoadHome）。
func NewLayout(home string, c LayoutConfig) Layout {
	if c.LayoutVersion() != 2 {
		return Layout{Home: home}
	}
	state := ExpandHome(c.State)
	if state == "" {
		state = DefaultStateDir(home)
	} else if !filepath.IsAbs(state) {
		state = filepath.Join(home, state)
	}
	return Layout{Home: home, State: state, V2: true, PersonasAtHome: c.OfficialPersonas == "home"}
}

func join(base string, parts []string) string {
	return filepath.Join(append([]string{base}, parts...)...)
}

// Runtime 是暫存資料夾（法律第 2 條：可以刪）。
func (l Layout) Runtime(parts ...string) string {
	if l.V2 {
		return join(filepath.Join(l.State, "runtime"), parts)
	}
	return join(filepath.Join(l.Home, "runtime"), parts)
}

// Secrets 是密鑰資料夾（推播金鑰等；不進 git、不上雲）。
func (l Layout) Secrets(parts ...string) string {
	if l.V2 {
		return join(filepath.Join(l.State, "secrets"), parts)
	}
	return join(filepath.Join(l.Home, "secrets"), parts)
}

// Media 是主人的檔案（附件 share/、圖庫 art/…）；第 1 版在 runtime/ 底下。
func (l Layout) Media(parts ...string) string {
	if l.V2 {
		return join(filepath.Join(l.Home, "media"), parts)
	}
	return join(filepath.Join(l.Home, "runtime"), parts)
}

// Shares 是 /share/<名字> 附件的資料夾。
func (l Layout) Shares() string { return l.Media("share") }

// OfficialPersonas 是官方角色（卡、avatars/、wardrobe/、catalog/）的位置；主人的角色永遠在 <家>/personas/mine/。
func (l Layout) OfficialPersonas(parts ...string) string {
	if l.V2 && !l.PersonasAtHome {
		return join(filepath.Join(l.State, "personas"), parts)
	}
	return join(filepath.Join(l.Home, "personas"), parts)
}

// InstalledPackages 是別人的套件（packages/installed/，可重裝）。
func (l Layout) InstalledPackages(parts ...string) string {
	if l.V2 {
		return join(filepath.Join(l.State, "packages", "installed"), parts)
	}
	return join(filepath.Join(l.Home, "packages", "installed"), parts)
}

// Env 是交給子行程（套件、引擎、session）的環境變數。第 1 版回空的：子行程沒有這幾個變數時會自己讀 home.toml 的 [layout]
// （sdk/layout.go），所以搬家前就開著的行程，搬完也會照新位置走；第 1 版若明寫 EVERGENIE_STATE=<家>，那些行程搬完反而會寫回家裡。
// 呼叫的地方要先拿掉外面帶進來的同名變數再加上這些。
func (l Layout) Env() []string {
	if !l.V2 {
		return nil
	}
	return []string{
		"EVERGENIE_STATE=" + l.State,
		"EVERGENIE_MEDIA=" + l.Media(),
		"EVERGENIE_OFFICIAL_PERSONAS=" + l.OfficialPersonas(),
	}
}

type layoutCacheEntry struct {
	mod    time.Time
	size   int64
	layout Layout
}

var layoutCache sync.Map // home → layoutCacheEntry

// LayoutFor 讀 <家>/home.toml 的 [layout] 算出位置；home.toml 沒變就用快取（每次只 stat）。
// 讀不到或格式錯就當第 1 版（LoadHome 會報錯，這裡不擋路徑計算）。
func LayoutFor(home string) Layout {
	path := filepath.Join(home, "home.toml")
	st, err := os.Stat(path)
	if err != nil {
		return Layout{Home: home}
	}
	if v, ok := layoutCache.Load(home); ok {
		e := v.(layoutCacheEntry)
		if e.mod.Equal(st.ModTime()) && e.size == st.Size() {
			return e.layout
		}
	}
	var doc struct {
		Layout LayoutConfig `toml:"layout"`
	}
	l := Layout{Home: home}
	if data, err := os.ReadFile(path); err == nil {
		if _, err := toml.Decode(string(data), &doc); err == nil {
			l = NewLayout(home, doc.Layout)
		}
	}
	layoutCache.Store(home, layoutCacheEntry{mod: st.ModTime(), size: st.Size(), layout: l})
	return l
}

// RuntimeDir 是 LayoutFor(home).Runtime(parts...) 的捷徑。
func RuntimeDir(home string, parts ...string) string { return LayoutFor(home).Runtime(parts...) }

// SharesDir 是 LayoutFor(home).Shares() 的捷徑。
func SharesDir(home string) string { return LayoutFor(home).Shares() }

// OfficialPersonasDir 是 LayoutFor(home).OfficialPersonas(parts...) 的捷徑。
func OfficialPersonasDir(home string, parts ...string) string {
	return LayoutFor(home).OfficialPersonas(parts...)
}
