package board

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// 「這台 Mac 本機開的看板」（DEV-153，WEB-187）：手機連線的設定會動這台電腦的 Tailscale，只准坐在這台 Mac 前面、
// 用它自己的瀏覽器開 http://127.0.0.1:<port>（或 localhost）的看板按；從手機、別台電腦、別台看板連進來的只能看狀態。
// 伺服器端擋，不靠畫面把按鈕藏起來。

// localRule 是一個方法對「是不是這台 Mac 本機」的要求。
type localRule int

const (
	// localInfo：照樣給，但請求的 `local` 一律換成這裡算出來的（用戶端自己帶的不算）。
	localInfo localRule = iota
	// localOnly：不是本機就 403、不交給核心；過了的也換 `local`。
	localOnly
	// localForUpload：請求指的是看板上傳的檔（`upload`）時才要求本機；另外帶公網轉送標頭的一律擋（backup.preview、backup.restore）。
	localForUpload
)

// localAware：要知道「是不是本機」的方法（服務 → 方法 → 規則）。
//   - 手機連線（WEB-187）：`phone_setup` 會動這台電腦的 Tailscale，只准本機。`phone_close_funnel`（關掉對外公開）只換 `local`、不擋：
//     關掉是變安全的動作，主人從哪裡都能關（SET-12）。
//   - 整個家的備份（WEB-190）：從看板**上傳**備份檔再還原只准本機（上傳的檔可以是任何人做的家，換上去等於整個家交給那個檔）；
//     挑這台**現有的**備份還原不限本機（主人在手機上也能退回昨天），但從公網轉送進來的請求一律擋。
var localAware = map[string]map[string]localRule{
	"web":    {"get_phone": localInfo, "phone_setup": localOnly, "phone_close_funnel": localInfo},
	"backup": {"get_status": localInfo, "preview": localForUpload, "restore": localForUpload},
}

const (
	thisMacOnlyMessage   = "這個設定只能在跑看板的那台 Mac 上按：請到那台 Mac，用它自己的瀏覽器打開看板"
	uploadThisMacMessage = "用上傳的備份檔還原，要在跑看板的那台 Mac 上做：請到那台 Mac，用它自己的瀏覽器打開看板的「設定 → 電腦 → 備份」"
	viaPublicMessage     = "這個請求是從整個網路（公開的網址）轉進來的，不能還原；請從家裡的網路或那台 Mac 上的看板操作"
)

// onThisMac：這個請求是不是這台 Mac 自己的瀏覽器直接打本機看板送來的。四個條件都要：
//   - 連線來自 loopback（看板本來就只聽 127.0.0.1；經 Tailscale 進來的也是，所以光這條不夠）；
//   - Host 是 127.0.0.1／localhost／::1（經 Tailscale 進來的是 *.ts.net）；
//   - 沒有任何「被轉送過」的標頭：`X-Forwarded-*`、`Forwarded`、`Via`、`X-Real-Ip`、`Tailscale-*`。
//     Tailscale serve／Funnel 轉進來時一定補 `X-Forwarded-For`（用戶端自己帶的會被整個換掉、也拿不掉；
//     2026-11-03 實測過覆蓋，cors.go 的 fromThisMachine 同一個依據），所以就算有人在 tailnet 裡把 Host 偽造成 127.0.0.1 也過不了；
//   - 瀏覽器有說來源時（Origin、Sec-Fetch-Site）要是本機同源：Origin 要是 http、而且跟請求的 Host 一模一樣（含埠；
//     這台 Mac 上別的埠的網頁不算）。
//
// 擋不了的（跟 D-24 同一個已知限制）：這台 Mac 上的別的程式直接打 127.0.0.1——它本來就能自己跑 tailscale；
// 以及有人另外架了不帶轉送標頭的通道（例如 ssh -L 把埠轉到別台）——那個人已經能登入這台 Mac。
func onThisMac(r *http.Request) bool {
	if !loopbackHostPort(r.RemoteAddr) || !loopbackHostPort(r.Host) {
		return false
	}
	for name := range r.Header {
		n := strings.ToLower(name)
		if n == "forwarded" || n == "via" || n == "x-real-ip" || strings.HasPrefix(n, "x-forwarded-") || strings.HasPrefix(n, "tailscale-") {
			return false
		}
	}
	origins := r.Header.Values("Origin")
	if len(origins) > 1 {
		return false
	}
	if len(origins) == 1 {
		u, err := url.Parse(origins[0])
		if err != nil || u.Scheme != "http" || !loopbackHostPort(u.Host) || !strings.EqualFold(u.Host, r.Host) {
			return false
		}
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	return true
}

// loopbackHostPort：host[:port] 的 host 是 localhost 或 loopback 位址。
func loopbackHostPort(hostPort string) bool {
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// viaPublic：這個請求是不是從公網轉送進來的——Tailscale Funnel（一定帶 `Tailscale-Funnel-Request`），或別的反向代理
// （`Forwarded`、`Via`、`X-Real-Ip`）。tailnet 裡經 Tailscale serve 進來的只有 `X-Forwarded-*` 與 `Tailscale-User-*`，不算。
func viaPublic(r *http.Request) bool {
	for name := range r.Header {
		switch strings.ToLower(name) {
		case "tailscale-funnel-request", "forwarded", "via", "x-real-ip":
			return true
		}
	}
	return false
}

// namesUpload：請求有沒有指到看板上傳的檔（`upload` 不是空字串；key 不分大小寫，跟 Go 的 JSON 解碼一致）。
func namesUpload(obj map[string]json.RawMessage) bool {
	for k, v := range obj {
		if !strings.EqualFold(k, "upload") {
			continue
		}
		var s string
		if json.Unmarshal(v, &s) != nil || s != "" {
			return true // 不是字串的也當成有（交給核心前就擋）
		}
	}
	return false
}

// gateThisMac 在看板把請求交給核心之前跑：回 ok=false 表示已經回 403。
// 過了的回換過 `local` 的 body（大小寫不同的重複 key 一起拿掉）。
func gateThisMac(w http.ResponseWriter, r *http.Request, service, method string, body []byte) ([]byte, bool) {
	rule, aware := localAware[service][method]
	if !aware {
		return body, true
	}
	forbid := func(msg string) ([]byte, bool) {
		writeStatusError(w, http.StatusForbidden, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: msg})
		return nil, false
	}
	local := onThisMac(r)
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil || obj == nil {
		obj = map[string]json.RawMessage{}
	}
	switch rule {
	case localOnly:
		if !local {
			return forbid(thisMacOnlyMessage)
		}
	case localForUpload:
		if viaPublic(r) {
			return forbid(viaPublicMessage)
		}
		if namesUpload(obj) && !local {
			return forbid(uploadThisMacMessage)
		}
		return body, true // 這兩個方法沒有 `local` 欄位
	}
	dropFold(obj, "local")
	obj["local"] = json.RawMessage("false")
	if local {
		obj["local"] = json.RawMessage("true")
	}
	out, err := json.Marshal(obj)
	if err != nil {
		writeStatusError(w, http.StatusBadRequest, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "請傳入有效的 JSON"})
		return nil, false
	}
	return out, true
}
