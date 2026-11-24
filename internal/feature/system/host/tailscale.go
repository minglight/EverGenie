package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
)

// AppCLI 是 macOS Tailscale App 內附的 CLI：跟 App 常駐程式同一版。Homebrew 裝的 `tailscale` 常常比常駐程式舊
// （不認得 `--service`、`get-config`），從 ssh 進來的 PATH 也可能找不到它，所以先找這支（D-43）。
const AppCLI = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"

// Tailscale 管這台的 Tailscale。
type Tailscale struct {
	Runner Runner
	Home   string
	Port   int // https 埠；0＝443
	// Service 是 --service：有給就用這個 Tailscale 服務（不含 `svc:`），setup 會把它寫進 home.toml 的 web.service；
	// 空的就讀 web.service；兩個都沒有＝照舊用這台機器自己的 serve。
	Service string
}

// bin 找 Tailscale CLI：先 App 內那支（只有 macOS 有），找不到才找 PATH 上的 `tailscale`（TailscaleBin，安裝前檢查也用同一個）。
func (t Tailscale) bin() string { return TailscaleBin(t.Runner) }

func (t Tailscale) https() int {
	if t.Port == 0 {
		return 443
	}
	return t.Port
}

// serviceName 回要用的服務名稱（不含 `svc:`）與這個家的看板 port：--service ＞ home.toml 的 web.service；名稱空＝不用服務。
// 讀不懂 home.toml 回錯誤（不能當成「沒寫服務」，不然 down 會去關這台機器的 serve，那可能是別的家在用）；名稱格式不對也回錯。
func (t Tailscale) serviceName() (name string, port int, err error) {
	h, err := platform.LoadHome(t.Home)
	if err != nil {
		return "", 0, err
	}
	name, field := h.Web.Service, "web.service"
	if s := strings.TrimPrefix(strings.TrimSpace(t.Service), "svc:"); s != "" {
		name, field = s, "service"
	}
	if name != "" && !platform.ValidTailscaleService(name) {
		return "", 0, platform.Invalid(field, "Tailscale 服務名稱只能用小寫英文、數字與連字號（例：genie-mini）："+name)
	}
	return name, h.Web.Port, nil
}

// checkServicePort：Tailscale 服務一律掛 443（D-43），--https-port 給別的埠就擋。
func (t Tailscale) checkServicePort() error {
	if t.https() != 443 {
		return platform.Invalid("https-port", "Tailscale 服務一律用 HTTPS 443，不要加 --https-port")
	}
	return nil
}

type tsStatus struct {
	BackendState   string
	MagicDNSSuffix string
	CurrentTailnet *struct{ MagicDNSSuffix string }
	CertDomains    []string // 後台開了 HTTPS 才有（手機連線認「還沒開 HTTPS」用，phone.go）
	Self           struct {
		DNSName string
		Tags    []string
		CapMap  map[string]json.RawMessage
		UserID  int64
	}
	User map[string]struct{ LoginName string } // key 是 UserID（手機連線顯示「用哪個帳號登入」，phone.go）
}

// status 讀 `tailscale status --json`；沒裝、沒回應或讀不懂回 nil。
func (t Tailscale) status(ctx context.Context, bin string) *tsStatus {
	if bin == "" {
		return nil
	}
	out, code := t.Runner.Run(ctx, bin, "status", "--json")
	if code != 0 {
		return nil
	}
	var st tsStatus
	if jsonAfterBrace(out, &st) != nil {
		return nil
	}
	return &st
}

func (st *tsStatus) dns() string { return strings.TrimSuffix(st.Self.DNSName, ".") }

// suffix 是 tailnet 的網域（例 example.ts.net）：MagicDNSSuffix，沒有就從這台的 DNSName 拿掉第一段。
func (st *tsStatus) suffix() string {
	if s := strings.TrimSuffix(st.MagicDNSSuffix, "."); s != "" {
		return s
	}
	if st.CurrentTailnet != nil {
		if s := strings.TrimSuffix(st.CurrentTailnet.MagicDNSSuffix, "."); s != "" {
			return s
		}
	}
	if _, rest, ok := strings.Cut(st.dns(), "."); ok {
		return rest
	}
	return ""
}

// approved：後台核准這台當 svc:<名> 的主機後，`Self.CapMap["service-host"]` 會列出它（陣列，元素是 {服務: [位址…]}）。
func (st *tsStatus) approved(name string) bool {
	raw, ok := st.Self.CapMap["service-host"]
	if !ok {
		return false
	}
	var hosts []map[string]json.RawMessage
	if json.Unmarshal(raw, &hosts) != nil {
		return false
	}
	for _, m := range hosts {
		if _, ok := m["svc:"+name]; ok {
			return true
		}
	}
	return false
}

func jsonAfterBrace(out string, v any) error {
	if i := strings.Index(out, "{"); i >= 0 {
		out = out[i:]
	}
	return json.Unmarshal([]byte(out), v)
}

// serveServices 讀 `serve get-config --all`，回每個服務 tcp:443 指到哪（例 "svc:genie-amy-mini" → "http://127.0.0.1:8850"；
// 有登記但沒有 443 的是空字串）。讀不到或看不懂回 problem（CLI 太舊之類）。
func (t Tailscale) serveServices(ctx context.Context, bin string) (targets map[string]string, problem string) {
	out, code := t.Runner.Run(ctx, bin, "serve", "get-config", "--all")
	if code != 0 {
		return nil, "讀不到 serve 設定：" + oneLine(out)
	}
	var cfg struct {
		Services map[string]struct {
			Endpoints map[string]string `json:"endpoints"`
		} `json:"services"`
	}
	if jsonAfterBrace(out, &cfg) != nil {
		return nil, "serve 設定看不懂：" + oneLine(out)
	}
	targets = map[string]string{}
	for name, svc := range cfg.Services {
		targets[name] = svc.Endpoints["tcp:443"]
	}
	return targets, ""
}

func localTarget(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

// sameTarget：服務的 443 指到的是不是這個家的看板（127.0.0.1 或 localhost 的同一個 port，結尾的 / 不算）。
func sameTarget(got string, port int) bool {
	got = strings.TrimSuffix(strings.TrimSpace(got), "/")
	return got == localTarget(port) || got == fmt.Sprintf("http://localhost:%d", port)
}

// Backend 回 Tailscale 的狀態（Running、NeedsLogin…）與這台在 tailnet 的網域名稱；沒回應回空字串。
func (t Tailscale) Backend(ctx context.Context) (state, dns string) {
	st := t.status(ctx, t.bin())
	if st == nil {
		return "", ""
	}
	return st.BackendState, st.dns()
}

func (t Tailscale) url(host string) string {
	if p := t.https(); p != 443 {
		return fmt.Sprintf("https://%s:%d", host, p)
	}
	return "https://" + host
}

// ServiceState 是這台跟一個 Tailscale 服務的關係（只讀；status 與 doctor 用）。
type ServiceState struct {
	Name      string // 不含 svc:
	Bin       string // 空＝沒裝 Tailscale
	Backend   string // Running、NeedsLogin…；空＝沒回應
	URL       string // https://<名>.<tailnet 網域>；Tailscale 沒回應時空
	Tagged    bool   // 這台是 tag 身分（服務主機一定要是）
	Tags      []string
	Port      int    // 這個家的看板 port
	Target    string // 這台 serve 設定裡這個服務的 443 指到哪；空＝沒登記
	Mounted   bool   // 這個服務的 443 指到這個家的看板（名稱與目標都對上）
	Elsewhere bool   // 這個服務在這台有掛，但指到別的地方（別的家或別的程式）
	Approved  bool   // 後台核准了這台當服務主機
	Problem   string // home.toml 讀不懂、名稱不對、讀 get-config 失敗的原因
}

// CheckService 看這台有沒有把看板掛到服務、後台核准了沒；沒有服務名稱（web.service 沒寫）回 Name 空的值。
func (t Tailscale) CheckService(ctx context.Context) ServiceState {
	name, port, err := t.serviceName()
	s := ServiceState{Name: name, Port: port, Bin: t.bin()}
	if err != nil {
		s.Problem = platformMessage(err)
		return s
	}
	if s.Name == "" || s.Bin == "" {
		return s
	}
	st := t.status(ctx, s.Bin)
	if st == nil {
		return s
	}
	s.Backend, s.Tags, s.Tagged = st.BackendState, st.Self.Tags, len(st.Self.Tags) > 0
	if sfx := st.suffix(); sfx != "" {
		s.URL = t.url(s.Name + "." + sfx)
	}
	s.Approved = st.approved(s.Name)
	targets, problem := t.serveServices(ctx, s.Bin)
	if problem != "" {
		s.Problem = problem
		return s
	}
	if target, ok := targets["svc:"+s.Name]; ok {
		s.Target = target
		s.Mounted = sameTarget(target, port)
		s.Elsewhere = target != "" && !s.Mounted // 登記了但沒有 443 的算沒掛上
	}
	return s
}

const (
	tagFix     = "到 Tailscale 後台把這台改成 tag 身分（例 tag:hub），或 tailscale up --advertise-tags=tag:<名稱> 重新登入"
	approveFix = "到 Tailscale 後台 Services 頁核准這台當服務主機，或在 ACL 的 autoApprovers.services 讓這台的 tag 自動核准"
)

// Status 回人話的現況，一行一項（只讀）。
func (t Tailscale) Status(ctx context.Context) []string {
	bin := t.bin()
	if bin == "" {
		return []string{"Tailscale：沒裝（到 " + TailscaleDownload + " 下載 App，裝好並登入）"}
	}
	lines := []string{"Tailscale 程式：" + bin}
	state, dns := t.Backend(ctx)
	switch {
	case state == "":
		return append(lines, "節點：沒回應（Tailscale 沒開，或還沒登入）")
	case state == "NeedsLogin":
		return append(lines, "節點：還沒登入（打開 Tailscale App 登入，或 tailscale up）")
	}
	lines = append(lines, "節點："+state+"，"+dns)
	name, _, nameErr := t.serviceName()
	if nameErr != nil {
		return append(lines, "home.toml 讀不懂："+platformMessage(nameErr))
	}
	if name != "" {
		s := t.CheckService(ctx)
		if s.Tagged {
			lines = append(lines, "身分：tag（"+strings.Join(s.Tags, "、")+"）")
		} else {
			lines = append(lines, "身分：個人帳號——服務主機要用 tag 身分（"+tagFix+"）")
		}
		url := s.URL
		if url == "" {
			url = "（拿不到 tailnet 網域）"
		}
		lines = append(lines, "服務：svc:"+name+"，"+url)
		switch {
		case s.Mounted:
			lines = append(lines, "掛上：有（指到這個家的看板 "+localTarget(s.Port)+"）")
		case s.Elsewhere:
			lines = append(lines, "掛上：指到別的地方（"+s.Target+"，這個家是 "+localTarget(s.Port)+"）")
		case s.Problem != "":
			lines = append(lines, "掛上：不知道（"+s.Problem+"）")
		default:
			lines = append(lines, "掛上：還沒（evergenie tailscale setup）")
		}
		if s.Approved {
			lines = append(lines, "核准：有")
		} else {
			lines = append(lines, "核准：還沒（"+approveFix+"）")
		}
	} else if out, code := t.Runner.Run(ctx, bin, "serve", "status"); code == 0 && strings.TrimSpace(out) != "" {
		lines = append(lines, "serve："+strings.SplitN(strings.TrimSpace(out), "\n", 2)[0])
	} else {
		lines = append(lines, "serve：還沒露出看板（evergenie tailscale setup）")
	}
	if h, err := platform.LoadHome(t.Home); err == nil {
		url := h.Web.PublicURL
		if url == "" {
			url = "（沒設）"
		}
		lines = append(lines, "web.public_url："+url)
	}
	if warn := t.Phone(ctx).funnelWarning(); warn != "" {
		lines = append(lines, warn)
	}
	return lines
}

// Setup 一步一步做：程式在不在 → 登入了沒 → 只把看板 serve 出去 → 把網址寫進 home.toml 的 web.public_url。
// 有服務名稱（--service 或 web.service）時改掛到 Tailscale 服務 svc:<名>（這台要是 tag 身分），網址是 https://<名>.<tailnet 網域>。
// 任何一步不通就停在那一步說原因；修好可以重跑。
func (t Tailscale) Setup(ctx context.Context) ([]string, error) {
	name, port, err := t.serviceName()
	if err != nil {
		return nil, err
	}
	if name != "" {
		if err := t.checkServicePort(); err != nil {
			return nil, err
		}
	}
	bin := t.bin()
	if bin == "" {
		return nil, platform.Unavailable("Tailscale 沒裝：先到 " + TailscaleDownload + " 下載 App，裝好並登入，再執行一次")
	}
	st := t.status(ctx, bin)
	switch {
	case st == nil || st.BackendState == "":
		return nil, platform.Unavailable("Tailscale 沒有回應：請先打開 Tailscale App")
	case st.BackendState != "Running" || st.dns() == "":
		return nil, platform.Unavailable("Tailscale 還沒登入或還沒連上（目前：" + st.BackendState + "）：打開 Tailscale App 登入後再執行一次")
	}
	if name != "" {
		return t.setupService(ctx, bin, st, name, port)
	}
	dns := st.dns()
	lines := []string{"Tailscale 已連上：" + dns}
	if svc := t.publicURLService(ctx, bin); svc != "" {
		return lines, platform.Invalid("service", "web.public_url 是 Tailscale 服務 svc:"+svc+" 的網址，這個家的看板掛在服務上；請改用 evergenie tailscale setup --service "+svc+"（不要重開這台機器的 serve）")
	}
	out, code := t.Runner.Run(ctx, bin, "serve", "--bg", fmt.Sprintf("--https=%d", t.https()), localTarget(port))
	if code != 0 {
		return lines, serveFailed(out, st)
	}
	lines = append(lines, fmt.Sprintf("已把看板（127.0.0.1:%d）露出到 tailnet 的 HTTPS %d", port, t.https()))
	return t.writePublicURL(lines, t.url(dns))
}

// publicURLService：沒寫 web.service 的家，web.public_url 的第一段剛好是這台 serve 設定裡的某個服務 → 回那個服務名（不含 svc:）。
// 這種家其實掛在服務上（只是 home.toml 還沒補 web.service），用機器 serve 會把 443 重新打開、public_url 改回機器網址。讀不到 serve 設定就不擋。
func (t Tailscale) publicURLService(ctx context.Context, bin string) string {
	h, err := platform.LoadHome(t.Home)
	if err != nil || h.Web.PublicURL == "" {
		return ""
	}
	u, err := url.Parse(h.Web.PublicURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	first, _, _ := strings.Cut(u.Hostname(), ".")
	targets, problem := t.serveServices(ctx, bin)
	if problem != "" {
		return ""
	}
	if _, ok := targets["svc:"+first]; ok {
		return first
	}
	return ""
}

func (t Tailscale) setupService(ctx context.Context, bin string, st *tsStatus, name string, port int) ([]string, error) {
	lines := []string{"Tailscale 已連上：" + st.dns()}
	if len(st.Self.Tags) == 0 {
		return lines, platform.Unavailable("這台的 Tailscale 是用個人帳號登入的；服務主機要用 tag 身分：" + tagFix + "，再執行一次")
	}
	sfx := st.suffix()
	if sfx == "" {
		return lines, platform.Unavailable("讀不到 tailnet 的網域（MagicDNS 沒開？）：到 Tailscale 後台開啟 MagicDNS 與 HTTPS 再執行一次")
	}
	svc := "svc:" + name
	targets, problem := t.serveServices(ctx, bin)
	if problem != "" {
		return lines, platform.Unavailable(problem + "（看不到 " + svc + " 現在指到哪，不敢動；Tailscale 程式太舊時換用 App 內附的 CLI）")
	}
	target, exists := targets[svc]
	switch {
	case exists && target != "" && !sameTarget(target, port):
		return lines, platform.Conflict(svc + " 在這台已經指到 " + target + "，不是這個家的看板 " + localTarget(port) + "；可能是別的家或別的程式在用，請換一個服務名稱（--service）")
	case exists && target != "":
		lines = append(lines, svc+" 已經指到這個家的看板（"+localTarget(port)+"），不用再掛")
	default:
		out, code := t.Runner.Run(ctx, bin, "serve", "--service="+svc, fmt.Sprintf("--https=%d", t.https()), localTarget(port))
		if code != 0 {
			return lines, platform.Unavailable("serve 沒設成功：" + oneLine(out) + "（後台 Services 要先有 " + svc + "；Tailscale 程式太舊也會失敗）")
		}
		lines = append(lines, fmt.Sprintf("已把看板（127.0.0.1:%d）掛到服務 %s 的 HTTPS %d", port, svc, t.https()))
	}
	if again := t.status(ctx, bin); again != nil {
		st = again
	}
	if st.approved(name) {
		lines = append(lines, "後台已核准這台當 "+svc+" 的主機")
	} else {
		lines = append(lines, "還沒核准："+approveFix+"；核准前網址打不開")
	}
	if h, err := platform.LoadHome(t.Home); err == nil && h.Web.Service != name {
		if _, _, err := platform.SetHomeValue(t.Home, "web.service", `"`+name+`"`, timeNow()); err != nil {
			return lines, err
		}
		lines = append(lines, "已把 web.service 設成 "+name)
	}
	return t.writePublicURL(lines, t.url(name+"."+sfx))
}

// tailnetReminder：掛好之後一定說一次誰連得到（DEV-153：看板沒有登入，界線是主人的 Tailscale 網路）。
const tailnetReminder = "注意：跟你在同一個 Tailscale 網路裡的人，都能打開你的看板；不要把別人加進你的 Tailscale 網路。這個指令只開給你的 Tailscale 網路，不會對整個網路公開（不開 Funnel）。"

// writePublicURL 把網址寫進 web.public_url（一樣就不寫）。
func (t Tailscale) writePublicURL(lines []string, url string) ([]string, error) {
	if h, err := platform.LoadHome(t.Home); err == nil && h.Web.PublicURL == url {
		return append(lines, "web.public_url 已經是 "+url, tailnetReminder), nil
	}
	if _, _, err := platform.SetHomeValue(t.Home, "web.public_url", `"`+url+`"`, timeNow()); err != nil {
		return lines, err
	}
	return append(lines, "已把 web.public_url 設成 "+url+"（舊的 home.toml 備份在 runtime/backups/home/）", tailnetReminder), nil
}

// Down 關掉看板的 serve（Tailscale 本身與登入不動）。有服務名稱時，只有那個服務的 443 指到這個家的看板才 `serve clear`：
// 沒掛在這台就說沒東西要關，指到別處（別的家或別的程式）就拒絕。
func (t Tailscale) Down(ctx context.Context) ([]string, error) {
	name, port, err := t.serviceName()
	if err != nil {
		return nil, err
	}
	if name != "" {
		if err := t.checkServicePort(); err != nil {
			return nil, err
		}
	}
	bin := t.bin()
	if bin == "" {
		return nil, platform.NotFound("Tailscale 沒裝，沒有東西要關")
	}
	if name != "" {
		svc := "svc:" + name
		targets, problem := t.serveServices(ctx, bin)
		if problem != "" {
			return nil, platform.Unavailable(problem + "（看不到 " + svc + " 現在指到哪，不敢關）")
		}
		target, ok := targets[svc]
		switch {
		case !ok:
			return []string{svc + " 沒有掛在這台，沒有東西要關"}, nil
		case !sameTarget(target, port):
			return nil, platform.Conflict(svc + " 指到 " + target + "，不是這個家的看板 " + localTarget(port) + "；不是這個家掛的，不關")
		}
		out, code := t.Runner.Run(ctx, bin, "serve", "clear", svc)
		if code != 0 {
			return nil, platform.Unavailable("關不掉服務 " + svc + "：" + oneLine(out))
		}
		return []string{"已把這台從服務 " + svc + " 拿掉（後台的服務與核准沒動；web.service、web.public_url 沒改）"}, nil
	}
	out, code := t.Runner.Run(ctx, bin, "serve", fmt.Sprintf("--https=%d", t.https()), "off")
	if code != 0 {
		return nil, platform.Unavailable("關不掉 serve：" + oneLine(out))
	}
	return []string{fmt.Sprintf("已關掉 HTTPS %d 的 serve（Tailscale 本身沒動；web.public_url 沒改）", t.https())}, nil
}

// platformMessage 取 platform 錯誤的白話訊息（沒有就用 Error()）。
func platformMessage(err error) string {
	var pe *platform.Err
	if errors.As(err, &pe) && pe.Message != "" {
		return pe.Message
	}
	return err.Error()
}
