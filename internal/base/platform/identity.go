package platform

import (
	"os"
	"regexp"
	"strings"
)

// 家的身分（2026-11-01 作者定）：一個家＝一份程式＋一份資料；每台電腦有一個正式家（env = prod）與一個開發家（env = dev）。
// 身分只拿來讓人一眼分出是哪個家（看板標籤、引擎的遠端存取名稱、通知標題、手機主畫面圖示），不改任何行為；
// 行為上的「沙盒」（不推播、不自動升級、套件檢查放寬）看 Runtime.Sandbox。

const (
	EnvProd = "prod"
	EnvDev  = "dev"
)

// hostname 是這台的主機名稱（測試可換）。
var hostname = os.Hostname

var identityPart = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}._-]*$`)

// resolveIdentity 補 [runtime] 的 env、owner、machine 預設值並檢查格式；舊的 dev = true 換算成 sandbox＋env = dev。
func (h *Home) resolveIdentity() error {
	r := &h.Runtime
	if r.LegacyDev {
		r.Sandbox = true
		if r.Env == "" {
			r.Env = EnvDev
		}
	}
	switch r.Env {
	case "":
		r.Env = EnvProd
	case EnvProd, EnvDev:
	default:
		return Invalid("runtime.env", `家的環境只能是 "prod"（正式）或 "dev"（開發）`)
	}
	if r.Owner == "" {
		r.Owner = identitySlug(h.User.Name)
	} else if !identityPart.MatchString(r.Owner) {
		return Invalid("runtime.owner", "主人代號只能用字母、數字、點、底線與連字號（例：amy）")
	}
	if r.Machine == "" {
		r.Machine = shortHostname()
	} else if !identityPart.MatchString(r.Machine) {
		return Invalid("runtime.machine", "電腦代號只能用字母、數字、點、底線與連字號（例：home、office）")
	}
	return nil
}

// identitySlug 把名稱轉成標籤用的一段：小寫、空白換成 -、其他符號拿掉；什麼都不剩就回空字串。
func identitySlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r == ' ' || r == '\t':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		case r == '-' || r == '_' || r == '.' || identityPart.MatchString(string(r)):
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-_.")
}

// shortHostname 是這台的短主機名（第一個點之前、小寫）；拿不到就是 local。
func shortHostname() string {
	n, err := hostname()
	if err != nil {
		return "local"
	}
	n, _, _ = strings.Cut(n, ".")
	if s := identitySlug(n); s != "" {
		return s
	}
	return "local"
}

// IsDev：這個家是開發家（[runtime] env = "dev"）。主要拿來標記，不管推播、升級（那是 Runtime.Sandbox）；依它改變行為的少數例外列在 PLT-20。
func (h *Home) IsDev() bool { return h.Runtime.Env == EnvDev }

// Label 是家的標籤：<owner>-<machine>，開發家加 @dev（例 amy-home、amy-home@dev）。
func (h *Home) Label() string {
	parts := []string{}
	for _, p := range []string{h.Runtime.Owner, h.Runtime.Machine} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	label := strings.Join(parts, "-")
	if h.IsDev() {
		label += "@dev"
	}
	return label
}
