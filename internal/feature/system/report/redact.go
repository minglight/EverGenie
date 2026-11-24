package report

import (
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
)

// Private 是這台電腦上「不能出現在回報包裡」的字（RPT-2）。Build 的每一個輸出字串都經過 Redactor。
type Private struct {
	UserHome string   // 登入者的家目錄（/Users/<登入名>）
	Username string   // 登入名
	Hosts    []string // 電腦名（含 .local 之類的全名、home.toml 的 machine、家的標籤）
	Home     string   // EverGenie 的家（資料夾完整路徑）
	State    string   // 狀態資料夾
	Accounts []string // AI 帳號名、它們的登入資料夾
	Names    []string // 其他私人的字：主人的名字（home.toml user.name）、owner、看板登入名、別台的名字與網址
	// ConfigUnread：家的設定讀不起來（home.toml 壞了，正是最需要回報的時候）。這時遮蔽用的是從設定檔逐行撈到的字，
	// 回報包只出一般版（不帶日誌尾段與錯誤原文），並寫明這件事（RPT-2：判斷不了就少給，不多給）。
	ConfigUnread bool
}

// PrivateOf 讀這台與這個家的私人字。home.toml 不管讀不讀得起來，都另外逐行撈一次（looseWords）：
// 設定壞掉時也遮得到帳號名、登入資料夾、電腦名、主人的名字；路徑另收一份 symlink 解開後的樣子。
func PrivateOf(home string) Private {
	p := Private{Home: home}
	if h, err := os.UserHomeDir(); err == nil {
		p.UserHome = h
		if real, err := filepath.EvalSymlinks(h); err == nil && real != h {
			p.Accounts = append(p.Accounts, real+"/") // 帶斜線：當路徑遮
		}
	}
	if u, err := user.Current(); err == nil {
		p.Username = u.Username
		if u.Name != "" {
			p.Names = append(p.Names, u.Name)
		}
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		p.Hosts = append(p.Hosts, host)
		if short, _, ok := strings.Cut(host, "."); ok {
			p.Hosts = append(p.Hosts, short)
		}
	}
	if home == "" {
		return p
	}
	if state := filepath.Dir(platform.LayoutFor(home).Runtime()); state != filepath.Clean(home) {
		p.State = state
	}
	if real, err := filepath.EvalSymlinks(home); err == nil && real != filepath.Clean(home) {
		p.Accounts = append(p.Accounts, real+"/")
	}
	names, hosts, accounts := looseWords(filepath.Join(home, "home.toml"))
	p.Names, p.Hosts, p.Accounts = append(p.Names, names...), append(p.Hosts, hosts...), append(p.Accounts, accounts...)
	h, err := platform.LoadHome(home)
	if err != nil {
		p.ConfigUnread = true
		return p
	}
	p.Hosts = append(p.Hosts, h.Runtime.Machine, h.Label())
	p.Names = append(p.Names, h.User.Name, h.Runtime.Owner, h.Web.OwnerLogin, h.Web.PublicURL, h.Web.Service)
	p.Names = append(p.Names, h.Web.AllowBoards...)
	for _, s := range h.Web.Sites {
		p.Names = append(p.Names, s.URL)
	}
	for name, v := range h.Accounts {
		if name != "default" {
			p.Accounts = append(p.Accounts, name)
		}
		if table, ok := v.(map[string]any); ok {
			if cfg, ok := table["engine_config"].(map[string]any); ok {
				if dir, _ := cfg["config_dir"].(string); dir != "" {
					p.Accounts = append(p.Accounts, dir, platform.ExpandHome(dir))
				}
			}
		}
	}
	return p
}

var (
	reTomlTable = regexp.MustCompile(`^\s*\[+\s*accounts\.("[^"]+"|[A-Za-z0-9_-]+)`)
	reTomlValue = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s*=\s*["']([^"']+)["']`)
)

// looseWords 不靠設定格式對不對，逐行撈 home.toml 裡的私人字：`[accounts.<名>]` 的帳號名，
// 以及 name／owner／登入名／網址（名字）、machine／label（電腦）、config_dir／env_file（帳號的資料夾）這幾個鍵的字串值。
func looseWords(path string) (names, hosts, accounts []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		if m := reTomlTable.FindStringSubmatch(line); m != nil {
			if name := strings.Trim(m[1], `"`); name != "default" {
				accounts = append(accounts, name)
			}
			continue
		}
		m := reTomlValue.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		switch m[1] {
		case "name", "owner", "owner_login", "public_url", "service", "url", "default":
			names = append(names, m[2])
		case "machine", "label":
			hosts = append(hosts, m[2])
		case "config_dir", "env_file":
			accounts = append(accounts, m[2], platform.ExpandHome(m[2]))
		}
	}
	return names, hosts, accounts
}

type swap struct {
	re   *regexp.Regexp
	with string
}

// Redactor 把私人字換成記號。路徑照字面換（長的先）；名字只在「前後不是英數字」時換——登入名是 ever 時不會把 evergenie 也換掉。
type Redactor struct{ swaps []swap }

var (
	reTailnet = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9.-]*\.ts\.net`)
	reEmail   = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+`)
	reURL     = regexp.MustCompile(`https?://[^\s"'）)，。]+`)
	reUsers   = regexp.MustCompile(`/(Users|home)/[^/\s"']+`)
	// 網址編碼過的家目錄（%2FUsers%2F<登入名>）
	reUsersEnc = regexp.MustCompile(`(?i)%2F(Users|home)%2F[^%\s"'&]+`)
	// vault 裡的檔名常是人名、主題名：只留到 vault/ 為止
	reVault = regexp.MustCompile(`(vault/)[^\s"'：:，。）)]+`)
	// 同樣常是人名、主題名的幾個資料夾（媒體檔、角色、主人自己的套件）：只留到資料夾為止。程式的堆疊行（….go:行號）不動。
	reOwnDirs = regexp.MustCompile(`(^|[/\s"'=（(])(media/|personas/|packages/mine/)[^\s"'：:，。）)]+(:\d+)?`)
	// 外接硬碟：磁碟名常是人名，後面的路徑也是主人自己取的；整段只留 /Volumes/<磁碟>（磁碟名可以有空白，所以一路收到標點為止）。
	reVolumes = regexp.MustCompile(`/Volumes/[^\n"'：:，。）)]+`)
	reIP      = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	// IPv6 的候選：一串十六進位數字與冒號（可以夾 IPv4 的尾巴）。是不是真的位址交給 netip 判斷（時間 20:30:05、行號 :42 都不是）。
	reIP6 = regexp.MustCompile(`[0-9A-Fa-f]*:[0-9A-Fa-f:.]*[0-9A-Fa-f:]`)
)

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// cleanIP6 把 IPv6 位址換成 <IP>（本機 ::1 與 :: 留著）。前後緊鄰英數字的不算（`std::foo` 這種不是位址）。
func cleanIP6(s string) string {
	locs := reIP6.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		start, end := loc[0], loc[1]
		// 句子裡位址後面常接冒號或句點（「dial fe80::1: refused」）：從尾巴往回收到是位址為止。
		for end > start {
			if addr, err := netip.ParseAddr(s[start:end]); err == nil {
				if !addr.Is6() || addr.IsLoopback() || addr.IsUnspecified() {
					break
				}
				if (start > 0 && isWordByte(s[start-1])) || (end < len(s) && isWordByte(s[end])) {
					break
				}
				b.WriteString(s[last:start])
				b.WriteString("<IP>")
				last = end
				break
			}
			if c := s[end-1]; c != ':' && c != '.' {
				break
			}
			end--
		}
	}
	b.WriteString(s[last:])
	return b.String()
}

// NewRedactor 照 Private 建好替換表。
func NewRedactor(p Private) *Redactor {
	r := &Redactor{}
	literal := func(s, with string) {
		if s = strings.TrimRight(strings.TrimSpace(s), "/"); len(s) > 1 {
			r.swaps = append(r.swaps, swap{regexp.MustCompile(`(?i)` + regexp.QuoteMeta(s)), with})
		}
	}
	word := func(s, with string) {
		// 太短的字（一個字元）換了會把整份弄花，也遮不到什麼。
		if s = strings.TrimSpace(s); len([]rune(s)) >= 2 {
			r.swaps = append(r.swaps, swap{regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(s) + `($|[^A-Za-z0-9_])`), "${1}" + with + "${2}"})
		}
	}
	// 路徑：長的先換（狀態資料夾、家通常在家目錄底下）。
	paths := []struct{ s, with string }{{p.State, "<狀態資料夾>"}, {p.Home, "<家>"}}
	for _, a := range p.Accounts {
		if strings.Contains(a, "/") {
			paths = append(paths, struct{ s, with string }{a, "<帳號的登入資料夾>"})
		}
	}
	paths = append(paths, struct{ s, with string }{p.UserHome, "~"})
	sort.SliceStable(paths, func(i, j int) bool { return len(paths[i].s) > len(paths[j].s) })
	for _, x := range paths {
		literal(x.s, x.with)
	}
	for _, h := range p.Hosts {
		word(h, "<電腦>")
	}
	for _, a := range p.Accounts {
		if !strings.Contains(a, "/") {
			word(a, "<帳號>")
		}
	}
	for _, n := range p.Names {
		word(n, "<名字>")
	}
	word(p.Username, "<使用者>")
	return r
}

// Clean 回遮過的字串。先換信箱（登入名常是信箱的前半，先換名字信箱就認不出來、網域會留著），再換這台的私人字
// （不分大小寫：電腦名在網路上常是小寫的樣子），最後換看得出樣子的（別人的家目錄、vault／media／personas／packages/mine 裡的檔名、
// 外接硬碟的路徑、網址、Tailscale、IPv4 與 IPv6）。
func (r *Redactor) Clean(s string) string {
	s = reEmail.ReplaceAllString(s, "<信箱>")
	for _, x := range r.swaps {
		// 名字的規則會吃掉前後各一個字元，緊鄰的兩個要換兩輪才換得完（例「amy amy」）。
		for i := 0; i < 2; i++ {
			s = x.re.ReplaceAllString(s, x.with)
		}
	}
	s = reUsers.ReplaceAllString(s, "/$1/<使用者>")
	s = reUsersEnc.ReplaceAllString(s, "%2F$1%2F<使用者>")
	s = reVault.ReplaceAllString(s, "${1}<檔>")
	s = reOwnDirs.ReplaceAllStringFunc(s, func(m string) string {
		sub := reOwnDirs.FindStringSubmatch(m)
		if sub[3] != "" && strings.HasSuffix(strings.TrimSuffix(m, sub[3]), ".go") {
			return m // 堆疊行：程式自己的檔名與行號
		}
		return sub[1] + sub[2] + "<檔>"
	})
	s = reVolumes.ReplaceAllString(s, "/Volumes/<磁碟>")
	s = reURL.ReplaceAllStringFunc(s, func(u string) string {
		if strings.HasPrefix(u, "http://127.0.0.1") || strings.HasPrefix(u, "http://localhost") {
			return u
		}
		return "<網址>"
	})
	s = reTailnet.ReplaceAllString(s, "<網址>")
	s = cleanIP6(s)
	s = reIP.ReplaceAllStringFunc(s, func(ip string) string {
		if ip == "127.0.0.1" || ip == "0.0.0.0" {
			return ip
		}
		return "<IP>"
	})
	return s
}
