package settings

import (
	"context"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/host"
	"github.com/minglight/evergenie/sdk"
)

// 手機連線（DEV-153，system 卡 SET-10～SET-13）：看板「設定 → 手機連線」背後的三個方法（web.get_phone、phone_setup、
// phone_close_funnel）與一項健檢（config.web_funnel）。動 Tailscale 的那一半在同一塊的 host（phone.go），這裡只管誰能按、
// 順便把推播金鑰做好、把結果整理成畫面要的樣子。

// PushKeys 是 inbox 塊的推播金鑰（registry 注入，這一塊不 import inbox）：Ready 看這個家有沒有金鑰，
// Ensure 沒有才做一組（回這次有沒有新做）。兩個都可以是 nil：沒接時不管金鑰。
type PushKeys struct {
	Ready  func(home string) bool
	Ensure func(home string) (made bool, err error)
}

const (
	phoneReadTimeout  = 5 * time.Second  // 讀現況的每支 tailscale 指令最多等多久（看板的讀取時限是 8 秒）
	phoneSetupTimeout = 25 * time.Second // 設定時的每支指令：新帳號還沒開 HTTPS 時 tailscale serve 會停下來等人去按網頁，不能一直等
	phoneOnlyHere     = "這個設定只能在跑看板的那台 Mac 上按：請到那台 Mac，用它自己的瀏覽器打開看板的「設定 → 手機連線」"
)

// phoneMu：設定與關 Funnel 一次只做一個（連按兩下、兩個分頁同時按不會同時改 Tailscale 的設定）。
var phoneMu sync.Mutex

func (m *Module) tailscale(home string, timeout time.Duration) host.Tailscale {
	var r host.Runner = host.ExecRunner{Timeout: timeout}
	if m.d.Runner != nil {
		r = m.d.Runner
	}
	return host.Tailscale{Runner: r, Home: home}
}

// phone 把現況整理成 sdk 的樣子；local 是看板伺服器照請求算的「是不是這台 Mac 本機開的看板」。
func (s *service) phone(ctx context.Context, home string, local bool) sdk.WebPhone {
	st := s.m.tailscale(home, phoneReadTimeout).Phone(ctx)
	out := sdk.WebPhone{
		Local: local, Tailscale: st.State, Account: st.Account, Mounted: st.Mounted, URL: st.URL,
		Funnel: st.Funnel, FunnelURL: st.FunnelURL, Problem: st.Problem, DownloadURL: host.TailscaleDownload,
	}
	if ready := s.m.d.Push.Ready; ready != nil {
		out.PushReady = ready(home)
	}
	return out
}

// GetPhone 回手機連線現在做到哪。只讀，誰都可以看。
func (s *service) GetPhone(ctx context.Context, req *sdk.WebGetPhoneRequest) (*sdk.WebGetPhoneResponse, error) {
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.WebGetPhoneResponse{Phone: s.phone(ctx, home, req.Local)}, nil
}

// phoneActor：「讓手機連得到」只有主人、而且只能在這台 Mac 本機開的看板上按（伺服器端擋，不靠畫面）。
func phoneActor(by *sdk.Actor, local bool) (string, error) {
	who, err := ownerOnly(by, "只有主人能設定手機連線")
	if err != nil {
		return "", err
	}
	if !local {
		return "", platform.Forbidden(phoneOnlyHere)
	}
	return who, nil
}

// PhoneSetup 是「讓手機連得到」：跑跟 `evergenie tailscale setup` 一樣的設定（只 serve、絕不開 Funnel），成功後順便把推播金鑰做好。
// 預料中的不通（沒裝、沒登入、後台還沒開 HTTPS…）不算錯誤：回 ok=false 加上卡在哪與白話，畫面帶著主人處理完再按一次。
func (s *service) PhoneSetup(ctx context.Context, req *sdk.WebPhoneSetupRequest) (*sdk.WebPhoneSetupResponse, error) {
	who, err := phoneActor(req.By, req.Local)
	if err != nil {
		return nil, s.fail(err)
	}
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	phoneMu.Lock()
	defer phoneMu.Unlock()
	got := s.m.tailscale(home, phoneSetupTimeout).SetupPhone(ctx)
	res := &sdk.WebPhoneSetupResponse{OK: got.OK, Reason: got.Reason, Message: got.Message, HelpURL: got.HelpURL}
	if ensure := s.m.d.Push.Ensure; got.OK && ensure != nil {
		if _, err := ensure(home); err != nil {
			res.Message = "看板掛好了，手機連得到；但通知用的金鑰沒做成（" + platform.SDKError(err).Message + "）。再按一次試試；還是不行，跟任何一位秘書說「通知的金鑰做不出來，幫我看」。"
		}
	}
	if log := s.m.logger(); log != nil {
		log.Info("手機連線：設定", "by", who, "ok", got.OK, "reason", got.Reason)
	}
	res.Phone = s.phone(ctx, home, req.Local)
	return res, nil
}

// PhoneCloseFunnel 把看板對整個網路的公開（Funnel）關掉。只有主人，但不限這台 Mac 本機（SET-12）：關掉是變安全的動作，
// 主人在手機上看到紅色警告就要能馬上關，不用先走到那台 Mac 前面。
func (s *service) PhoneCloseFunnel(ctx context.Context, req *sdk.WebPhoneCloseFunnelRequest) (*sdk.WebPhoneCloseFunnelResponse, error) {
	who, err := ownerOnly(req.By, "只有主人能關掉對外公開")
	if err != nil {
		return nil, s.fail(err)
	}
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	phoneMu.Lock()
	defer phoneMu.Unlock()
	_, err = s.m.tailscale(home, phoneSetupTimeout).CloseFunnel(ctx)
	if log := s.m.logger(); log != nil {
		log.Info("手機連線：關掉對外公開", "by", who, "error", err)
	}
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.WebPhoneCloseFunnelResponse{Phone: s.phone(ctx, home, req.Local)}, nil
}

// Checks：看板被 Tailscale Funnel 開到整個網路時，健檢紅字（沒開、沒裝 Tailscale、讀不到都不出現這一條）。
func (m *Module) Checks() []app.Check {
	return []app.Check{{Name: "config.web_funnel", RunAll: m.checkFunnel}}
}

func (m *Module) checkFunnel(ctx context.Context) []app.CheckResult {
	home := m.homeDir()
	if home == "" {
		return nil
	}
	st := m.tailscale(home, phoneReadTimeout).Phone(ctx)
	if !st.Funnel {
		return nil
	}
	return []app.CheckResult{{
		Status:  app.StatusError,
		Message: "你的看板現在整個網路都連得到（Tailscale Funnel 開著：" + st.FunnelURL + "）。看板沒有登入，任何人拿到網址都能用",
		Fix:     "到看板「設定 → 手機連線」按「關掉對外公開」（手機上的看板也可以按）",
	}}
}

var _ app.CheckProvider = (*Module)(nil)
