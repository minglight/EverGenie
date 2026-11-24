package host

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 手機連線（DEV-153，system 卡 HST-7、SET-10～13）：看板「設定 → 手機連線」與 `evergenie tailscale` 共用的那一半——
// 找 Tailscale 的找法（檢查與設定同一套）、現在做到哪一步（只讀）、一顆按鈕的設定、偵測與關掉 Funnel。

// TailscaleDownload 是 Tailscale 官方的 Mac 下載頁：沒裝時一律請主人到這裡下載 App（不建議 brew：那支常比 App 舊，新手也不會用）。
const TailscaleDownload = "https://tailscale.com/download/mac"

// tailscaleAdminDNS 是 Tailscale 後台開 HTTPS 的那一頁（讀不到 Tailscale 自己給的開啟連結時用）。
const tailscaleAdminDNS = "https://login.tailscale.com/admin/dns"

// TailscaleBinEnv：有設就只用這個路徑的 Tailscale 程式（找不到＝當成沒裝）。給測試與 e2e 換成假的執行檔，不碰這台真的 Tailscale。
const TailscaleBinEnv = "EVERGENIE_TAILSCALE_BIN"

// TailscaleBin 找 Tailscale 的程式：先 App 內附那支（跟常駐程式同版），找不到才找 PATH 上的 `tailscale`；都沒有回空字串。
// 安裝前檢查（Preflight）、`evergenie tailscale`、看板的手機連線、健檢都用這一個，不會一邊說沒裝、另一邊找得到（DEV-153）。
func TailscaleBin(r Runner) string {
	if p := strings.TrimSpace(sdk.Getenv(TailscaleBinEnv)); p != "" {
		return r.LookPath(p)
	}
	if p := r.LookPath(AppCLI); p != "" {
		return p
	}
	return r.LookPath("tailscale")
}

// 手機連線做到哪（PhoneState.State）。
const (
	PhoneMissing = "missing" // 這台沒裝 Tailscale
	PhoneStopped = "stopped" // 裝了，但沒開（沒有回應）
	PhoneLogin   = "login"   // 開了，還沒登入或還沒連上
	PhoneReady   = "ready"   // 裝好、登入好了
)

// 設定沒成功的原因（PhoneSetup.Reason；前三個跟 State 同字）。
const (
	PhoneReasonHTTPS = "https" // Tailscale 後台還沒開 HTTPS（全新的帳號第一次要先開）
	PhoneReasonOther = "other"
)

// PhoneState 是手機連線現在的樣子（只讀）。
type PhoneState struct {
	State     string // PhoneMissing／PhoneStopped／PhoneLogin／PhoneReady
	Account   string // 這台 Tailscale 登入的帳號（手機要用同一個）；tag 身分或讀不到是空的
	Mounted   bool   // 看板已經掛到 Tailscale，而且 home.toml 的 web.public_url 就是那個網址
	URL       string // 手機要開的網址（Mounted 才有）
	Funnel    bool   // 看板的埠被 Funnel 開到整個網路
	FunnelURL string // 被公開的網址（Funnel 才有）
	Problem   string // 讀不到現況的白話原因（home.toml 讀不懂、serve 設定讀不到、服務還沒核准…）

	viaService bool // 掛法是 Tailscale 服務、而 home.toml 沒寫 web.service（照 serve 設定認出來的）
	funnels    []funnelEntry
}

type funnelEntry struct {
	hostPort string // AllowFunnel 的 key：<這台的名字>:<埠>
	tcp      bool   // 不是網頁代理，是整個 TCP 轉送
}

// serveStatus 是 `tailscale serve status --json` 的輸出（ipn.ServeConfig；2026-11-16 用 App CLI 1.104.5 對過格式）。
type serveStatus struct {
	TCP map[string]struct {
		TCPForward string
	}
	Web map[string]struct {
		Handlers map[string]struct {
			Proxy string
		}
	}
	AllowFunnel map[string]bool
	// Tailscale 服務（svc:<名字>）各自的網頁代理；用服務掛看板的家（D-43）在這裡，不在上面的 Web。
	Services map[string]struct {
		Web map[string]struct {
			Handlers map[string]struct {
				Proxy string
			}
		}
	}
}

// serviceToBoard：有沒有哪個 Tailscale 服務把 publicBase 那個網址代理到這個家的看板。
// 家沒在 home.toml 寫 web.service、但早就用服務掛好的（2026-11-17 實測：當時的家都是這樣），也算掛好了，不叫主人重做。
func (cfg *serveStatus) serviceToBoard(port int, publicBase string) bool {
	if publicBase == "" {
		return false
	}
	for _, svc := range cfg.Services {
		for hp, w := range svc.Web {
			if httpsURL(hp) != publicBase {
				continue
			}
			for _, h := range w.Handlers {
				if pointsToBoard(h.Proxy, port) {
					return true
				}
			}
		}
	}
	return false
}

// readServe 讀這台的 serve 設定（只讀）。沒設過任何 serve 時輸出是 `{}`。
func (t Tailscale) readServe(ctx context.Context, bin string) (*serveStatus, string) {
	out, code := t.Runner.Run(ctx, bin, "serve", "status", "--json")
	if code != 0 {
		return nil, "讀不到 Tailscale 的 serve 設定：" + oneLine(out)
	}
	var cfg serveStatus
	if strings.TrimSpace(out) != "" && jsonAfterBrace(out, &cfg) != nil {
		return nil, "Tailscale 的 serve 設定看不懂：" + oneLine(out)
	}
	return &cfg, ""
}

// pointsToBoard：serve 的目標是不是這個家的看板（本機的同一個 port；`http://127.0.0.1:8850`、`localhost:8850`、`tcp://…` 都算）。
func pointsToBoard(target string, port int) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if !strings.Contains(target, "://") {
		target = "tcp://" + target
	}
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
	default:
		return false
	}
	return u.Port() == strconv.Itoa(port)
}

func (cfg *serveStatus) webToBoard(hostPort string, port int) bool {
	for _, h := range cfg.Web[hostPort].Handlers {
		if pointsToBoard(h.Proxy, port) {
			return true
		}
	}
	return false
}

// boardFunnels 列出「Funnel 開著、而且指到這個家的看板」的項目（別的程式自己開的 Funnel 不算）。
func (cfg *serveStatus) boardFunnels(port int) []funnelEntry {
	var out []funnelEntry
	for hp, on := range cfg.AllowFunnel {
		if !on {
			continue
		}
		_, p, err := net.SplitHostPort(hp)
		if err != nil {
			continue
		}
		switch {
		case cfg.webToBoard(hp, port):
			out = append(out, funnelEntry{hostPort: hp})
		case pointsToBoard(cfg.TCP[p].TCPForward, port):
			out = append(out, funnelEntry{hostPort: hp, tcp: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].hostPort < out[j].hostPort })
	return out
}

func httpsURL(hostPort string) string {
	return "https://" + strings.TrimSuffix(hostPort, ":443")
}

// account 是這台登入的帳號（status 的 User[Self.UserID].LoginName）；tag 身分沒有個人帳號，回空的。
func (st *tsStatus) account() string {
	if len(st.Self.Tags) > 0 || st.Self.UserID == 0 {
		return ""
	}
	u, ok := st.User[strconv.FormatInt(st.Self.UserID, 10)]
	if !ok || !strings.Contains(u.LoginName, "@") {
		return ""
	}
	return u.LoginName
}

// Phone 回手機連線現在做到哪（只讀：只跑 status 與 serve status）。
func (t Tailscale) Phone(ctx context.Context) PhoneState {
	bin := t.bin()
	if bin == "" {
		return PhoneState{State: PhoneMissing}
	}
	st := t.status(ctx, bin)
	switch {
	case st == nil || st.BackendState == "":
		return PhoneState{State: PhoneStopped}
	case st.BackendState != "Running" || st.dns() == "":
		return PhoneState{State: PhoneLogin}
	}
	s := PhoneState{State: PhoneReady, Account: st.account()}
	h, err := platform.LoadHome(t.Home)
	if err != nil {
		s.Problem = "這個家的設定（home.toml）讀不懂：" + platformMessage(err)
		return s
	}
	cfg, problem := t.readServe(ctx, bin)
	if problem != "" {
		s.Problem = problem
		return s
	}
	if s.funnels = cfg.boardFunnels(h.Web.Port); len(s.funnels) > 0 {
		s.Funnel, s.FunnelURL = true, httpsURL(s.funnels[0].hostPort)
	}
	mounted := ""
	if h.Web.Service != "" {
		// 進階掛法（Tailscale 服務，D-43）：沿用現有的檢查。
		svc := t.CheckService(ctx)
		switch {
		case svc.Problem != "":
			s.Problem = svc.Problem
		case svc.Mounted && !svc.Approved:
			s.Problem = "看板掛到 Tailscale 服務 svc:" + svc.Name + " 了，但 Tailscale 後台還沒核准這台，網址還打不開"
		case svc.Mounted:
			mounted = svc.URL
		}
	} else if hp := net.JoinHostPort(st.dns(), strconv.Itoa(t.https())); cfg.webToBoard(hp, h.Web.Port) {
		mounted = t.url(st.dns())
	}
	if mounted != "" && h.PublicBase() == mounted {
		s.Mounted, s.URL = true, mounted
	} else if s.Problem == "" && cfg.serviceToBoard(h.Web.Port, h.PublicBase()) {
		s.Mounted, s.URL, s.viaService = true, h.PublicBase(), true
	}
	return s
}

// PhoneSetup 是「讓手機連得到」那顆按鈕的結果。OK＝看板掛好了；不行時 Reason 說卡在哪一步、Message 是白話、
// HelpURL 是要主人去按的網頁（沒裝＝下載頁；後台沒開 HTTPS＝開啟的那一頁）。
type PhoneSetup struct {
	OK      bool
	Reason  string
	Message string
	HelpURL string
	Lines   []string
}

// SetupPhone 做跟 `evergenie tailscale setup` 一樣的事（同一個 Setup），把每一種不通整理成看板能帶著主人處理的樣子。
// 只跑 `tailscale serve`，絕不開 Funnel；同一個埠原本開著 Funnel 的話，serve 會把它收回成只有自己的 Tailscale 網路連得到。
func (t Tailscale) SetupPhone(ctx context.Context) PhoneSetup {
	bin := t.bin()
	if bin == "" {
		return PhoneSetup{Reason: PhoneMissing, Message: "這台 Mac 還沒裝 Tailscale。先到官網下載 App、裝好並登入，再回來按一次。", HelpURL: TailscaleDownload}
	}
	st := t.status(ctx, bin)
	switch {
	case st == nil || st.BackendState == "":
		return PhoneSetup{Reason: PhoneStopped, Message: "Tailscale 沒有開。請在這台 Mac 打開 Tailscale App，再回來按一次。"}
	case st.BackendState != "Running" || st.dns() == "":
		return PhoneSetup{Reason: PhoneLogin, Message: "Tailscale 還沒登入。請在這台 Mac 打開 Tailscale App 登入，再回來按一次。"}
	}
	if p := t.Phone(ctx); p.Mounted && p.viaService {
		// 早就用 Tailscale 服務掛好的家：不在這台再掛一次、不改網址。
		return PhoneSetup{OK: true, Lines: []string{"看板已經掛在 " + p.URL + "，不用再設定"}}
	}
	lines, err := t.Setup(ctx)
	if err == nil {
		return PhoneSetup{OK: true, Lines: lines}
	}
	out := PhoneSetup{Reason: PhoneReasonOther, Message: platformMessage(err), Lines: lines}
	var pe *platform.Err
	if errors.As(err, &pe) && pe.Details["reason"] == PhoneReasonHTTPS {
		out.Reason, out.HelpURL = PhoneReasonHTTPS, pe.Details["help_url"]
	}
	return out
}

var (
	tailscaleEnableLink = regexp.MustCompile(`https://login\.tailscale\.com/[^\s"'<>]+`)
	httpsOffWords       = regexp.MustCompile(`(?i)not enabled on your tailnet|https (is )?(not|disabled)|enable https|https certificates`)
)

// serveFailed 把 `tailscale serve` 的失敗整理成錯誤（Setup 用）。全新的 Tailscale 帳號第一次 serve 要先到後台開 HTTPS：
// Tailscale 會印「Serve is not enabled on your tailnet. To enable, visit: <連結>」然後等人去按（指令被時限切斷，結束碼不是 0）。
// 認得出這種情況（它給了開啟連結、或說 HTTPS 沒開、或這台根本還沒有憑證網域）就回帶 `reason = https` 與連結的錯誤，
// 看板與指令都白話說要去哪裡按什麼；其他失敗照舊附上 Tailscale 的原話。沒有新帳號可以實測，認法是照 Tailscale CLI 的輸出寫的。
func serveFailed(out string, st *tsStatus) error {
	link := tailscaleEnableLink.FindString(out)
	if link == "" && !httpsOffWords.MatchString(out) && (st == nil || len(st.CertDomains) > 0) {
		return platform.Unavailable("Tailscale 沒有把看板掛上去。Tailscale 說：" + oneLine(out))
	}
	if link == "" {
		link = tailscaleAdminDNS
	}
	msg := "Tailscale 後台還沒打開 HTTPS（新帳號第一次都要開）。用瀏覽器打開 " + link + " ，按「Enable」（在 DNS 那一頁是「Enable HTTPS」），開好再做一次。"
	if detail := oneLine(strings.TrimSuffix(strings.TrimSpace(out), "逾時")); detail != "" {
		msg += "（Tailscale 說：" + detail + "）"
	}
	return &platform.Err{Code: sdk.ErrorCodeUnavailable, Message: msg, Details: map[string]string{"reason": PhoneReasonHTTPS, "help_url": link}}
}

// CloseFunnel 把看板的 Funnel 關掉（別的程式自己開的 Funnel 不動；Tailscale 本身與登入不動）。沒有開著就什麼都不做。
// 這個家用機器 serve、而且被公開的就是手機連的那個網址：改成一般的 serve（只有自己的 Tailscale 網路連得到，手機照樣能用）；
// 其他情況（用 Tailscale 服務的家、別的埠、整個 TCP 轉送）直接把那個 Funnel 拿掉。做完重讀一次，還開著就回錯誤。
func (t Tailscale) CloseFunnel(ctx context.Context) ([]string, error) {
	bin := t.bin()
	if bin == "" {
		return nil, platform.NotFound("這台沒裝 Tailscale，沒有東西要關")
	}
	h, err := platform.LoadHome(t.Home)
	if err != nil {
		return nil, err
	}
	cfg, problem := t.readServe(ctx, bin)
	if problem != "" {
		return nil, platform.Unavailable(problem)
	}
	entries := cfg.boardFunnels(h.Web.Port)
	if len(entries) == 0 {
		return []string{"看板沒有對整個網路公開，沒有東西要關"}, nil
	}
	var lines []string
	for _, e := range entries {
		_, port, _ := net.SplitHostPort(e.hostPort)
		off := []string{"funnel", "--https=" + port, "off"}
		if e.tcp {
			off = []string{"funnel", "--tcp=" + port, "off"}
		}
		keep := !e.tcp && h.Web.Service == "" && port == strconv.Itoa(t.https()) && h.PublicBase() == httpsURL(e.hostPort)
		if keep {
			// 同一個埠再 serve 一次：Tailscale 會把 Funnel 收回（「Removing Funnel for …」），看板照樣掛在自己的網路裡。
			t.Runner.Run(ctx, bin, "serve", "--bg", "--https="+port, localTarget(h.Web.Port))
			if again, _ := t.readServe(ctx, bin); again != nil && !again.AllowFunnel[e.hostPort] {
				lines = append(lines, "已關掉 "+httpsURL(e.hostPort)+" 的對外公開；手機照樣用這個網址連（只有你的 Tailscale 網路連得到）")
				continue
			}
		}
		if out, code := t.Runner.Run(ctx, bin, off...); code != 0 {
			return lines, platform.Unavailable("關不掉對外公開。Tailscale 說：" + oneLine(out))
		}
		lines = append(lines, "已關掉 "+httpsURL(e.hostPort)+" 的對外公開")
		if keep {
			// 整個拿掉之後手機也連不到了：重新掛回只有自己網路連得到的 serve。
			if out, code := t.Runner.Run(ctx, bin, "serve", "--bg", "--https="+port, localTarget(h.Web.Port)); code != 0 {
				lines = append(lines, "手機連線也一起關掉了，要再按一次「讓手機連得到」（Tailscale 說："+oneLine(out)+"）")
			}
		}
	}
	again, problem := t.readServe(ctx, bin)
	if problem != "" {
		return lines, platform.Unavailable("關了，但確認不了結果：" + problem)
	}
	if left := again.boardFunnels(h.Web.Port); len(left) > 0 {
		return lines, platform.Unavailable(fmt.Sprintf("關不掉：%s 還是對整個網路公開。請打開 Tailscale App 檢查，或跟任何一位秘書說「看板還是對外公開，幫我關掉」", httpsURL(left[0].hostPort)))
	}
	return lines, nil
}

// funnelWarning 是 Funnel 開著時給 `evergenie tailscale status` 的那一行；沒開回空字串。
func (s PhoneState) funnelWarning() string {
	if !s.Funnel {
		return ""
	}
	return "⚠ 你的看板現在整個網路都連得到（Tailscale Funnel 開著：" + s.FunnelURL + "）。要關掉：evergenie tailscale funnel-off，或到看板「設定 → 手機連線」按「關掉對外公開」"
}
