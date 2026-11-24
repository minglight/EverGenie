package platform

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Home struct {
	// Incidents 已廢（2026-11-02 S5：doctor 不再檢查；S5-2 刪掉 session 的事故單後就沒有人讀）：照讀，舊家的 home.toml 不會因此壞（PLT-14）。
	Incidents struct {
		Owner string `toml:"owner"`
	} `toml:"incidents"`
	Sessions struct {
		Autostart bool   `toml:"autostart"`
		Workdir   string `toml:"workdir"`
	} `toml:"sessions"`
	Dir    string `toml:"-"`
	Clock  Clock  `toml:"-"`
	Schema int    `toml:"schema"`
	Mode   string `toml:"mode"`
	// ChatDisplay 是看板對話的顯示方式：default 不露工具細節，engineer 顯示（只影響看板怎麼顯示）。
	ChatDisplay string `toml:"chat_display"`
	User        struct {
		Name         string `toml:"name"`
		Language     string `toml:"language"`
		Timezone     string `toml:"timezone"`
		QuietHours   string `toml:"quiet_hours"`
		QuietDeferTo string `toml:"quiet_defer_to"`
	} `toml:"user"`
	// Voice 是語音入口（web.voice_send，DEV-144）的設定：猜給誰用的模型與思考力度（空＝sonnet、low），
	// 猜不出來時交給誰（fallback，職位 id；空＝admin，再沒有就名冊第一位）。
	Voice struct {
		Model    string `toml:"model"`
		Effort   string `toml:"effort"`
		Fallback string `toml:"fallback"`
	} `toml:"voice"`
	Slots    map[string]any `toml:"slots"`
	Accounts map[string]any `toml:"accounts"`
	Web      struct {
		Port      int    `toml:"port"`
		PublicURL string `toml:"public_url"`
		AppIcon   string `toml:"app_icon"`
		// Service 是看板用的 Tailscale 服務名稱（不含 `svc:`，例 genie-amy-mini；寫了 svc: 讀進來會拿掉）；沒寫＝用這台機器自己的 serve（D-43）。
		// 格式不對不擋 LoadHome（ValidTailscaleService 由 doctor 與 tailscale 指令檢查）。
		Service string `toml:"service"`
		// 看板連多台（S3）：owner_login＝主人的 tailnet 登入，跨站請求的身分要等於它（沒寫＝不開放跨站）；
		// allow_boards＝哪些看板（https origin）可以從瀏覽器呼叫這個家；sites＝這個看板還要看哪些家。檢查與正規化在 web.go。
		OwnerLogin  string    `toml:"owner_login"`
		AllowBoards []string  `toml:"allow_boards"`
		Sites       []WebSite `toml:"sites"`
		// TrustSelf（預設關）：這台是 tag 身分的機器時，Tailscale 不帶 Tailscale-User-Login，從這台自己的瀏覽器開別台看板、
		// 再連回這個家的請求會一律被擋。開了之後，沒帶登入、而且來源位址（serve 覆寫的 X-Forwarded-For）是這台自己的 tailnet 位址的請求，
		// 視為主人（WEB-130）。只在 home.toml 改，沒有看板或 CLI 的設定入口。
		TrustSelf bool `toml:"trust_self"`
	} `toml:"web"`
	Secrets struct {
		File string `toml:"file"`
	} `toml:"secrets"`
	// Layout 是家的佈局版本與狀態資料夾（[layout]，DEV-145）；路徑一律用 h.Paths() 或 LayoutFor(home)。
	Layout LayoutConfig `toml:"layout"`
	Sense  struct {
		IntervalMin  int      `toml:"interval_min"`
		MaxLines     int      `toml:"max_lines"`
		Disabled     []string `toml:"disabled"`
		ExcludeRepos []string `toml:"exclude_repos"`
	} `toml:"sense"`
	// Defaults 是新建東西時的預設值：persona＝套件帶來新秘書時用哪個人設（packages sync，D-28）；沒寫就要主人先指定。
	Defaults struct {
		Persona string `toml:"persona"`
	} `toml:"defaults"`
	// Persona.Style 是說話層強度（P6-5a、D-31）：a、b、c、d 用內建文字，custom 讀 personas/_style.md；沒寫＝_style.md 在就 custom、不在就 c。
	Persona struct {
		Style string `toml:"style"`
	} `toml:"persona"`
	Integrations map[string]any `toml:"integrations"`
	Content      map[string]any `toml:"content"`
	Upgrade      struct {
		Channel string `toml:"channel"`
	} `toml:"upgrade"`
	// Update 是「版本與更新」的設定（P8 第二輪 S2、REQ-280）：channel＝release（只升正式版，預設）｜safe（也升每晚測過的版本）｜off（不自動檢查）；
	// 沒寫 channel 當 release；用 scripts/deploy.sh 部署 main 的家（發行者的兩台，K4）要自己寫 off。
	// check_at＝每天幾點檢查一次（預設 10:00）、tonight_at＝「今晚更新」在幾點動手（預設 03:30）；
	// core＝EverGenie 程式在哪裡（選填；原始碼版是 git repo，下載版是有 versions/ 與 current 的安裝資料夾；預設照執行檔的位置找）；
	// source＝下載版找新版用的版本清單網址（manifest.json；選填，沒寫用編進執行檔的預設，見 UpdateSource）。
	Update struct {
		Channel   string `toml:"channel"`
		CheckAt   string `toml:"check_at"`
		TonightAt string `toml:"tonight_at"`
		Core      string `toml:"core"`
		Source    string `toml:"source"`
	} `toml:"update"`
	// Runtime 是這個家自己的執行設定（[runtime]）。家的身分（env、owner、machine）主要拿來標記這是哪個家（看板、對話名、通知；依 env 改變行為的少數例外列在 PLT-20），
	// sandbox 才管「不推播、不自動升級、套件檢查放寬」；舊的 dev = true 讀成 sandbox = true 加 env = "dev"（見 identity.go）。
	// remote_suffix 2026-11-01 退休：讀到忽略，也不交給引擎。
	Runtime struct {
		BoardPort    int            `toml:"board_port"`
		Env          string         `toml:"env"`
		Owner        string         `toml:"owner"`
		Machine      string         `toml:"machine"`
		Sandbox      bool           `toml:"sandbox"`
		LegacyDev    bool           `toml:"dev"` // 舊寫法，只在 LoadHome 換算成 Sandbox＋Env，其他地方不要讀
		ServiceLabel string         `toml:"service_label"`
		Overrides    map[string]any `toml:"-"`
	} `toml:"runtime"`
	// IDs 是編號字首（[ids]，GOAL-2 W3）：tasks 預設 SEC-、goals 預設 GOAL-。
	IDs IDPrefixes `toml:"ids"`
	// Tasks 是任務單的家設定（[tasks]）：keep_review_open＝等你驗收（review）時不關工作對話、被關掉的自動接回、
	// 接回不占進行中的名額，驗收或取消才關；沒寫＝false（照舊 review 就關）。工坊設 true。
	Tasks struct {
		KeepReviewOpen bool `toml:"keep_review_open"`
	} `toml:"tasks"`
	// Admin 是建角色包的設定：核心只讀 public_personas（`personas pack build` 只收這些人的卡、頭像、公開版衣櫃、目錄卡，REQ-283；
	// 沒寫＝不建角色包，也表示這個家不是角色包的來源、會去同步別人發的包）。其他欄位（repo、dev_home、release_state）
	// 是 admin 套件自己讀的，核心不認得（2026-10-31 搬出核心，GOAL-2）。
	Admin struct {
		PublicPersonas []string `toml:"public_personas"`
	} `toml:"admin"`
	Trust Trust `toml:"-"`
	loc   *time.Location
}

type Trust struct {
	Schema       int                   `toml:"schema"`
	DefaultLevel string                `toml:"default_level,omitempty"` // 沒有單獨設定的職位用這個等級；空＝內建預設（見 LevelFor）
	Agents       map[string]TrustAgent `toml:"agent,omitempty"`         // 每個職位 id 的等級（Q5）
	Repos        []TrustedRepo         `toml:"repo"`
	Approve      map[string]any        `toml:"approve,omitempty"`
	Deny         map[string]any        `toml:"deny,omitempty"`
}
type TrustAgent struct {
	Level string `toml:"level"`
}
type TrustedRepo struct {
	Path    string   `toml:"path"`
	Remote  string   `toml:"remote,omitempty"`
	Push    *bool    `toml:"push"`
	Writers []string `toml:"writers"`
}

func (h *Home) Location() *time.Location { return h.loc }

// ExpandHome 把開頭的 ~ 或 ~/ 換成使用者家目錄；其他路徑原樣回傳（DEV-4）。
func ExpandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(u, strings.TrimPrefix(p, "~"))
}

// Paths 是這個家算好的位置（佈局，DEV-145）。
func (h *Home) Paths() Layout { return NewLayout(h.Dir, h.Layout) }

// SecretsPath：相對路徑接在家上；第 2 版的家 secrets/ 開頭的接在狀態資料夾的 secrets/（密鑰不放家裡）。
func (h *Home) SecretsPath() string {
	if h.Secrets.File == "" || filepath.IsAbs(h.Secrets.File) {
		return h.Secrets.File
	}
	if p := h.Paths(); p.V2 {
		if rest, ok := strings.CutPrefix(filepath.ToSlash(filepath.Clean(h.Secrets.File)), "secrets/"); ok {
			return p.Secrets(filepath.FromSlash(rest))
		}
	}
	return filepath.Join(h.Dir, h.Secrets.File)
}
func LoadHome(dir string) (*Home, error) {
	h := &Home{Dir: dir, Clock: SystemClock{}, Schema: 1, Mode: "simple", ChatDisplay: "default"}
	h.Sessions.Autostart = true
	h.Sessions.Workdir = dir
	h.User.Language = "zh-TW"
	h.User.QuietHours = "22:00-07:01"
	h.User.QuietDeferTo = "08:30"
	h.Sense.IntervalMin = 30
	h.Sense.MaxLines = 40
	h.Sense.Disabled = []string{}
	h.Sense.ExcludeRepos = []string{}
	h.Upgrade.Channel = "stable"
	h.Update.CheckAt = "10:00"
	h.Update.TonightAt = "03:30"
	data, err := os.ReadFile(filepath.Join(dir, "home.toml"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, NotFound("找不到家的設定，請先建立設定檔")
	}
	if err != nil {
		return nil, Unavailable("無法讀取家的設定，請檢查存取權限")
	}
	if _, err = toml.Decode(string(data), h); err != nil {
		return nil, Invalid("home", "設定格式或欄位型別不正確，請檢查設定檔")
	}
	if err = h.loadRuntime(data); err != nil {
		return nil, err
	}
	if err = h.resolveIdentity(); err != nil {
		return nil, err
	}
	// 設定裡的路徑可以用 ~ 開頭（DEV-4）：在這裡展開成使用者家目錄，不要當成家裡一個叫「~」的資料夾。
	if v := h.Layout.LayoutVersion(); v != 1 && v != 2 {
		return nil, Invalid("layout.version", "家的佈局版本只能是 1 或 2")
	}
	if o := h.Layout.OfficialPersonas; o != "" && o != "home" {
		return nil, Invalid("layout.official_personas", "官方角色的位置只能不寫（狀態資料夾）或寫 \"home\"（家裡）")
	}
	for _, p := range []*string{&h.Sessions.Workdir, &h.Secrets.File, &h.Update.Core, &h.Layout.State} {
		*p = ExpandHome(*p)
	}
	if h.Runtime.BoardPort != 0 {
		h.Web.Port = h.Runtime.BoardPort
	}
	// web.service 寫成 svc:<名> 也認得；其他格式錯不擋整個家（不然 serve 起不來、部署會退回），由 doctor 給黃字、tailscale 指令回錯。
	h.Web.Service = strings.TrimPrefix(strings.TrimSpace(h.Web.Service), "svc:")
	if err = h.validate(); err != nil {
		return nil, err
	}
	for name, v := range h.Accounts {
		if table, ok := v.(map[string]any); ok && name != "default" {
			if ec, ok := table["engine_config"].(map[string]any); ok {
				for k, v := range h.Runtime.Overrides {
					ec[k] = v
				}
			}
		}
	}
	h.Trust.Schema = 1
	data, err = os.ReadFile(filepath.Join(dir, "settings", "trust.toml"))
	if errors.Is(err, os.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return nil, Unavailable("無法讀取權限設定，請檢查存取權限")
	}
	if _, err = toml.Decode(string(data), &h.Trust); err != nil {
		return nil, Invalid("trust", "權限設定格式不正確")
	}
	if h.Trust.Schema != 1 {
		return nil, Invalid("trust.schema", "設定版本不支援")
	}
	unknown := []string{}
	for section, table := range map[string]map[string]any{"approve": h.Trust.Approve, "deny": h.Trust.Deny} {
		for field, value := range table {
			if field != "actions" {
				unknown = append(unknown, section+"."+field)
				continue
			}
			switch values := value.(type) {
			case []string:
			case []any:
				for _, value := range values {
					if _, ok := value.(string); !ok {
						return nil, Invalid(section+".actions", "權限動作必須是字串陣列")
					}
				}
			default:
				return nil, Invalid(section+".actions", "權限動作必須是字串陣列")
			}
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		NewLogger(h, "platform").Warn("忽略未知權限欄位", "fields", unknown)
	}
	if err = h.Trust.validateLevels(); err != nil {
		return nil, err
	}
	for _, repo := range h.Trust.Repos {
		if repo.Path == "" {
			return nil, Invalid("repo.path", "請填寫允許的路徑")
		}
		if repo.Push == nil {
			return nil, Invalid("repo.push", "請指定是否允許推送")
		}
		if repo.Writers == nil {
			return nil, Invalid("repo.writers", "請指定可寫入的職位清單")
		}
		for _, writer := range repo.Writers {
			if strings.TrimSpace(writer) == "" {
				return nil, Invalid("repo.writers", "職位名稱不可空白")
			}
		}
	}
	return h, nil
}

// coreRuntimeKeys 是核心自己解讀的 [runtime] 欄位；其餘原樣交給引擎（Overrides）。
// remote_suffix 已退休（2026-11-01）：還列在這裡，舊檔寫了也不會變成引擎設定。
var coreRuntimeKeys = map[string]bool{"board_port": true, "dev": true, "sandbox": true, "env": true, "owner": true, "machine": true, "service_label": true, "backup_dir": true, "remote_suffix": true}

var tailscaleServicePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidTailscaleService 判斷 web.service 合不合格：一段 DNS 名稱（小寫英數字與連字號、最長 63 字），不含 `svc:`。
func ValidTailscaleService(name string) bool { return tailscaleServicePattern.MatchString(name) }

var serviceLabelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// loadRuntime 讀 [runtime]：board_port 檢查範圍；其餘欄位原樣留在 Overrides。
func (h *Home) loadRuntime(data []byte) error {
	if h.Runtime.BoardPort < 0 || h.Runtime.BoardPort > 65535 {
		return Invalid("runtime.board_port", "看板連接埠請填 1–65535，或不寫")
	}
	if l := h.Runtime.ServiceLabel; l != "" && !serviceLabelPattern.MatchString(l) {
		return Invalid("runtime.service_label", "服務名稱只能用英數字、點、底線與連字號（例：com.evergenie.workshop）")
	}
	var raw struct {
		Runtime map[string]any `toml:"runtime"`
	}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return Invalid("home", "設定格式或欄位型別不正確，請檢查設定檔")
	}
	h.Runtime.Overrides = map[string]any{}
	for k, v := range raw.Runtime {
		if !coreRuntimeKeys[k] {
			h.Runtime.Overrides[k] = v
		}
	}
	return nil
}
func (h *Home) validate() error {
	// schema 跟著佈局走（DEV-145）：第 2 版的家寫 schema = 2，舊程式只收 1，會拒絕啟動，不會把家當第 1 版亂寫。
	switch v := h.Layout.LayoutVersion(); {
	case h.Schema != 1 && h.Schema != 2:
		return Invalid("schema", "設定版本不支援")
	case h.Schema == 2 && v != 2:
		return Invalid("schema", "schema = 2 只用在佈局第 2 版的家（[layout] version = 2）")
	case h.Schema == 1 && v == 2:
		return Invalid("schema", "佈局第 2 版的家要寫 schema = 2（用 evergenie home relayout 搬家會自動改）")
	}
	if h.Mode != "simple" && h.Mode != "full" {
		return Invalid("mode", "模式必須是 simple 或 full")
	}
	if h.ChatDisplay != "default" && h.ChatDisplay != "engineer" {
		return Invalid("chat_display", "對話的顯示方式必須是 default 或 engineer")
	}
	if strings.TrimSpace(h.User.Name) == "" {
		return Invalid("user.name", "請填寫使用者稱呼")
	}
	switch h.Persona.Style {
	case "", "a", "b", "c", "d", "custom":
	default:
		return Invalid("persona.style", "說話層強度只能是 a、b、c、d、custom")
	}
	if h.User.Language == "" {
		return Invalid("user.language", "請填寫語言")
	}
	if h.User.Timezone == "" {
		return Invalid("user.timezone", "請填寫時區")
	}
	loc, err := time.LoadLocation(h.User.Timezone)
	if err != nil {
		return Invalid("user.timezone", "時區無法辨識，請使用有效時區名稱")
	}
	h.loc = loc
	if !validateQuiet(h.User.QuietHours, h.User.QuietDeferTo) {
		return Invalid("user.quiet_hours", "請使用正確的安靜時段與延後時間")
	}
	if err := validateNamedTables(h.Slots, "slots", true); err != nil {
		return err
	}
	if err := validateNamedTables(h.Accounts, "accounts", false); err != nil {
		return err
	}
	if h.Web.Port < 1 || h.Web.Port > 65535 {
		return Invalid("web.port", "請指定有效連接埠")
	}
	if h.Sense.IntervalMin <= 0 || h.Sense.MaxLines <= 0 {
		return Invalid("sense", "時間間隔與行數必須大於零")
	}
	if h.Upgrade.Channel != "stable" && h.Upgrade.Channel != "nightly" {
		return Invalid("upgrade.channel", "更新頻道必須是 stable 或 nightly")
	}
	switch h.Update.Channel {
	case "", UpdateChannelRelease, UpdateChannelSafe, UpdateChannelOff:
	default:
		return Invalid("update.channel", "更新頻道只能是 release（只升正式版）、safe（也升測試過的版本）或 off（不自動檢查）")
	}
	for field, v := range map[string]string{"update.check_at": h.Update.CheckAt, "update.tonight_at": h.Update.TonightAt} {
		if _, ok := clockMinutes(v); !ok {
			return Invalid(field, "時間請用 HH:MM（例如 10:00）")
		}
	}
	if h.Update.Source != "" && !DownloadURLAllowed(h.Update.Source) {
		return Invalid("update.source", "下載新版的地方要寫 https:// 開頭的網址")
	}
	if err := h.IDs.validate(); err != nil {
		return err
	}
	if err := h.validateWeb(); err != nil {
		return err
	}

	return nil
}
func validateNamedTables(tables map[string]any, field string, slots bool) error {
	def, ok := tables["default"].(string)
	if !ok || def == "" {
		return Invalid(field+".default", "請指定預設名稱")
	}
	if _, ok := tables[def].(map[string]any); !ok {
		return Invalid(field+".default", "預設名稱沒有對應設定")
	}
	for name, value := range tables {
		if name == "default" {
			continue
		}
		table, ok := value.(map[string]any)
		if !ok {
			return Invalid(field+"."+name, "設定必須是表格")
		}
		if slots {
			for _, key := range []string{"label", "remind", "check"} {
				v, ok := table[key].(string)
				if !ok || v == "" {
					return Invalid(field+"."+name+"."+key, "請填寫必要設定")
				}
				if key != "label" {
					if _, ok := clockMinutes(v); !ok {
						return Invalid(field+"."+name+"."+key, "時間必須使用 HH:MM")
					}
				}
			}
		} else {
			if v, ok := table["engine"].(string); !ok || v == "" {
				return Invalid(field+"."+name+".engine", "請指定引擎")
			}
			for _, key := range []string{"remote_access", "usage_warmup"} {
				if v, exists := table[key]; exists {
					if _, ok := v.(bool); !ok {
						return Invalid(field+"."+name+"."+key, "設定必須是布林值")
					}
				} else {
					table[key] = false
				}
			}
			if v, exists := table["engine_config"]; exists {
				if _, ok := v.(map[string]any); !ok {
					return Invalid(field+"."+name+".engine_config", "設定必須是表格")
				}
			} else {
				table["engine_config"] = map[string]any{}
			}
		}
	}
	return nil
}
