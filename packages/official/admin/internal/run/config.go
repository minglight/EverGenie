package run

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// Config 是程式要的設定，來自家的 home.toml 的 [admin]（核心今天也是讀這裡；sdk 沒有讀家設定的呼叫，所以直接讀檔，
// 檔案位置是核心給的 EVERGENIE_HOME）。路徑的 ~ 換成這台的家目錄。
type Config struct {
	Repo         string // [admin] repo；沒寫＝~/Workspace/<登入名>/evergenie（defaultRepo）
	ReleaseState string // [admin] release_state；沒寫＝~/.evergenie-release
	DevHome      string // [admin] dev_home；沒寫＝空（開發家沒設定）
}

const defaultReleaseState = "~/.evergenie-release"

// defaultRepo 是沒寫 [admin] repo 時的位置：~/Workspace/<這台的登入名>/evergenie（發行者開發機的放法）。
func defaultRepo() string {
	name := "owner"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	return "~/Workspace/" + name + "/" + sdk.ProductName
}

// LoadConfig 讀 <home>/home.toml。讀不到或格式認不出時回預設值與錯誤（呼叫端記 log、照預設繼續）。
func LoadConfig(home string) (Config, error) {
	c := Config{Repo: expandHome(defaultRepo()), ReleaseState: expandHome(defaultReleaseState)}
	data, err := os.ReadFile(filepath.Join(home, "home.toml"))
	if err != nil {
		return c, fmt.Errorf("讀不到家的 home.toml：%w", err)
	}
	t := parseTOML(string(data))
	if v := t.str("admin", "repo"); v != "" {
		c.Repo = expandHome(v)
	}
	if v := t.str("admin", "release_state"); v != "" {
		c.ReleaseState = expandHome(v)
	}
	c.DevHome = expandHome(t.str("admin", "dev_home"))
	return c, nil
}

// HomePeek 是從開發家的 home.toml 讀出來的最少資訊（跟核心 platform.PeekHome 一樣）。
type HomePeek struct {
	BoardPort int    // [runtime] board_port；沒寫退回 [web] port
	URL       string // [runtime] url
	Runtime   string // 那個家的暫存資料夾（照它的 [layout] 算，RuntimeDirOf）
}

// RuntimeDirOf 算別的家（dir）的暫存資料夾：看它自己 home.toml 的 [layout]（DEV-145），不能用 sdk.RuntimeDir——
// 那是照「這個家」的環境變數算的。規則跟核心 platform.NewLayout 一樣：version = 2 才在狀態資料夾
// （[layout] state；相對路徑從那個家算；沒寫＝~/.evergenie/state/<家的資料夾名>），其他一律 <家>/runtime。
func RuntimeDirOf(dir string, t tomlTable) string {
	if t.int("layout", "version") != 2 {
		return filepath.Join(dir, "runtime")
	}
	state := expandHome(t.str("layout", "state"))
	switch {
	case state == "":
		state = filepath.Join(expandHome("~/."+sdk.ProductName+"/state"), filepath.Base(filepath.Clean(dir)))
	case !filepath.IsAbs(state):
		state = filepath.Join(dir, state)
	}
	return filepath.Join(state, "runtime")
}

// PeekHome 讀 dir/home.toml 的看板 port 與網址；檔案不存在或讀不到回錯誤。
func PeekHome(dir string) (HomePeek, error) {
	data, err := os.ReadFile(filepath.Join(dir, "home.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return HomePeek{}, fmt.Errorf("開發家的資料夾裡沒有 home.toml")
		}
		return HomePeek{}, fmt.Errorf("讀不到開發家的 home.toml，請檢查存取權限")
	}
	t := parseTOML(string(data))
	p := HomePeek{BoardPort: t.int("runtime", "board_port"), URL: strings.TrimSpace(t.str("runtime", "url")), Runtime: RuntimeDirOf(dir, t)}
	if p.BoardPort == 0 {
		p.BoardPort = t.int("web", "port")
	}
	return p, nil
}

func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// tomlTable 是「段 → 鍵 → 值」；值只收字串、整數、布林（套件只用得到這些；sdk 不帶 TOML 函式庫，套件也只 import sdk）。
type tomlTable map[string]map[string]any

func (t tomlTable) str(section, key string) string {
	s, _ := t[section][key].(string)
	return s
}

func (t tomlTable) int(section, key string) int {
	n, _ := t[section][key].(int)
	return n
}

// parseTOML 是很小的 TOML 讀法：認 [段]、key = "字串"／'字串'／整數／true／false 與行尾註解；
// 陣列、行內表格、多行字串整段略過（跨行的也略過到結束），[[陣列表]] 的內容不收。
func parseTOML(src string) tomlTable {
	t := tomlTable{}
	section := ""
	depth := 0    // 跨行陣列／行內表格還沒關的層數
	inMulti := "" // 多行字串的結尾記號（""" 或 '''）
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if inMulti != "" {
			if strings.Contains(line, inMulti) {
				inMulti = ""
			}
			continue
		}
		if depth > 0 {
			depth += bracketDelta(line)
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[[") {
			section = "\x00" // 陣列表：不收
			continue
		}
		if strings.HasPrefix(line, "[") {
			end := strings.Index(line, "]")
			if end < 0 {
				continue
			}
			section = strings.TrimSpace(line[1:end])
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.Trim(strings.TrimSpace(key), `"'`)
		val = strings.TrimSpace(val)
		switch {
		case strings.HasPrefix(val, `"""`), strings.HasPrefix(val, `'''`):
			mark := val[:3]
			if !strings.Contains(val[3:], mark) {
				inMulti = mark
			}
			continue
		case strings.HasPrefix(val, "["), strings.HasPrefix(val, "{"):
			depth = bracketDelta(val)
			continue
		}
		v, ok := tomlValue(val)
		if !ok || section == "\x00" {
			continue
		}
		if t[section] == nil {
			t[section] = map[string]any{}
		}
		t[section][key] = v
	}
	return t
}

// tomlValue 解一個純量值（含行尾註解）。
func tomlValue(val string) (any, bool) {
	switch {
	case strings.HasPrefix(val, `"`):
		// 找結尾的雙引號（跳過跳脫的）
		for i := 1; i < len(val); i++ {
			if val[i] == '\\' {
				i++
				continue
			}
			if val[i] == '"' {
				s, err := strconv.Unquote(val[:i+1])
				return s, err == nil
			}
		}
		return nil, false
	case strings.HasPrefix(val, "'"):
		end := strings.Index(val[1:], "'")
		if end < 0 {
			return nil, false
		}
		return val[1 : end+1], true
	}
	if i := strings.Index(val, "#"); i >= 0 {
		val = strings.TrimSpace(val[:i])
	}
	switch val {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	n, err := strconv.Atoi(strings.ReplaceAll(val, "_", ""))
	return n, err == nil
}

// bracketDelta 算一行裡 [ { 比 ] } 多幾個（略過字串裡的）。
func bracketDelta(s string) int {
	d := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '#':
			return d
		case '[', '{':
			d++
		case ']', '}':
			d--
		}
	}
	return d
}
