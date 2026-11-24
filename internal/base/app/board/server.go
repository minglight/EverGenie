package board

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

const (
	boardActor      = "owner(web)"
	nightQueueID    = "system/night-queue" // 夜班清單 note 的編號（跟 internal/feature/packages/data 的 NightQueueID 相同；底層不 import 業務模組）
	maxRequestBytes = 1 << 20
)

// allowedAPI 是看板用得到的服務與方法（P4 共通定案 6）。
// 表上沒有的（含 engine、engine_host、vault）一律回 404。
var allowedAPI = map[string]map[string]bool{
	"session":   set("list", "get", "open", "chat", "watch", "list_threads", "watch_threads", "mark_seen", "resolve", "command"),
	"notify":    set("list", "get", "mark_read", "archive", "answer", "get_prefs", "set_prefs", "watch", "push_key", "subscribe_push", "unsubscribe_push", "push_test", "presence", "resolve_nudge"), // presence、resolve_nudge：不吵的通知（P7-6）；送提醒（nudge）不從看板來
	"tasks":     set("list", "get", "waiting", "stale", "accept", "archive", "reject", "update", "close", "create"),
	"reminders": set("create", "get", "list", "update", "snooze", "done", "doing", "cancel"),
	"goals":     set("list", "get", "withdraw", "create"), // create：主人在看板直接開 Goal（D-39，REQ-200）；by 一律換成看板身分，秘書提案不從看板來
	"policy":    set("list", "get", "decide", "revoke"),   // 權限申請頁（Q7）：by 一律換成看板身分（主人），送申請不從看板來
	"bus":       set("send", "get", "subscribe"),          // subscribe：只限 boardTopics（看板的即時更新，REQ-389）
	// 規矩頁（S7）：主人改、還原、採用或退回；送提案（propose）只給秘書，不從看板來。
	"rules": set("list", "get", "save", "history", "version", "restore", "proposals", "decide"),
	// 設定頁補齊（P6-2b）：套件管理（不含 hello，那是附程式套件行程的呼叫）、秘書啟用與帳號、權限等級、電腦。
	// 套件的資料（D-28、REQ-173）：套件頁面用同一組 vault API 讀寫，只限套件的資料種類（見 vaultPackageOnly）；watch 不開。
	"vault":   set("list", "get", "put", "patch", "archive", "next_id", "changes"),
	"package": set("list", "get", "install", "uninstall", "set_enabled", "reload", "list_extensions", "set_extension", "install_go", "fork", "list_updates", "sync", "preview", "outdated", "upgrade"),
	"agents":  set("set_field", "set_account", "assign", "knowledge", "set_knowledge"), // knowledge、set_knowledge：「職位與秘書」頁勾可選知識（DEV-161）
	// 角色庫（P6-5a、D-31、REQ-130）：角色頁的介紹（DEV-117 從設定搬來）；寫入的 by 一律換成看板身分（主人）。
	// export／import（DEV-116 S3、WEB-151）：檔案經 /api/web/persona-upload 上傳、/persona-export/ 下載（persona_transfer.go）。
	// pack_sync：衣櫃與展示間的「現在再試」（DEV-157）；pointer、force 由 forceIdentity 拿掉（一律用服務自己的指標、不蓋掉主人改過的）。
	"personas": set("list", "get", "create", "copy", "update", "set_status", "check", "get_style", "set_style", "test", "test_results", "pack_status", "pack_sync", "export", "import"),
	"system":   set("trust", "set_permission_levels", "preflight", "usage", "backups", "accounts", "save_account", "get_mode", "set_mode", "set_chat_display", "get_work_max", "set_work_max", "update_status", "update_check", "update_apply", "update_decide", "update_rollback", "backup_sweep"), // backup_sweep：「設定 → 電腦 → 備份」的備份掃描（DEV-163，BSW-6）；by 一律換成看板身分
	// 整個家的備份與還原（DEV-153，設定 → 這台電腦 → 備份）：by 一律換成看板身分（主人）；檔案經 home_backup.go 上傳、下載。
	"backup": set("get_status", "create", "preview", "restore"),
	// 排程頁（任務單 → 排程，REQ-18、REQ-159）：列表、新增、改、移除、立刻跑；by 一律換成看板身分（主人）。
	"scheduler": set("list_jobs", "save_job", "set_job", "remove_job", "run_job"),
	// 看板外殼自己要的資料（選單、頁面、名冊、總覽、健康、權限、設定頁的秘書）：sdk 的 web 服務整個開放；各方法由 menu、overview、agents、doctor、trust 業務模組登記。
	"web": set(sdk.ServiceMethods("web")...),
}

func set(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

var (
	agentID     = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	personaSlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	imageTypes  = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}
	// 聊天裡分享的影片也直接播（inline）；其他副檔名照舊下載。
	videoTypes = map[string]string{".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime"}
	// 頭像只有這兩種：預設圖與預設影片。
	avatarTypes  = map[string]string{".png": "image/png", ".mp4": "video/mp4"}
	oldTabNotice = `<!doctype html><html lang="zh-TW"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>找不到這個連結</title></head>` +
		`<body style="font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;line-height:1.6">` +
		`<h1 style="font-size:1.25rem">找不到這個連結</h1><p>這是舊看板的連結，新看板不支援；請從選單進入。</p><p><a href="/">回到首頁</a></p></body></html>`
)

// buildHandler 組出整個看板：不用最外層的 ServeMux，才不會被它把 `..` 的路徑轉址成別的網址。
// /api/* 與上傳先過跨站檢查、其他路由先過 Host 檢查（cors.go），過了才交給 handler。
func (m *Module) buildHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == uploadPath || strings.HasPrefix(p, "/api/") {
			if !m.gateCrossSite(w, r) {
				return
			}
			switch p {
			case uploadPath:
				m.serveUpload(w, r)
			case personaUploadPath:
				m.servePersonaUpload(w, r)
			case homeBackupUploadPath:
				m.serveHomeBackupUpload(w, r)
			default:
				m.serveAPI(w, r)
			}
			return
		}
		// 其他路由先看 Host（cors.go，擋 DNS rebinding）；不過就已經回 403。
		if !m.gateHost(w, r) {
			return
		}
		switch {
		case strings.HasPrefix(p, "/avatars/"):
			m.serveAvatar(w, r)
		case strings.HasPrefix(p, "/share/"):
			m.serveShare(w, r)
		case p == voiceShortcutPath:
			m.serveVoiceShortcut(w, r)
		case strings.HasPrefix(p, personaExportPrefix):
			m.servePersonaExport(w, r)
		case strings.HasPrefix(p, homeBackupPrefix):
			m.serveHomeBackup(w, r)
		case strings.HasPrefix(p, "/pkg-bundles/"):
			m.d.Assets.ServeBundle(w, r)
		case strings.HasPrefix(p, pkgFilesPrefix):
			m.d.Assets.ServePkgFile(w, r)
		case p == "/manifest.webmanifest":
			m.serveManifest(w, r)
		case strings.HasPrefix(p, "/icons/") && m.serveAppIcon(w, r):
		default:
			m.serveStatic(w, r)
		}
	})
}

func writeJSONError(w http.ResponseWriter, e *sdk.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(sdk.HTTPStatus(e.Code))
	// 寫失敗代表對方已經斷線，沒有別的回應可送。
	_ = json.NewEncoder(w).Encode(e)
}

func writeStatusError(w http.ResponseWriter, status int, e *sdk.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(e)
}

func isJSON(contentType string) bool {
	main, _, _ := strings.Cut(contentType, ";")
	return strings.EqualFold(strings.TrimSpace(main), "application/json")
}

func (m *Module) serveAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !isJSON(r.Header.Get("Content-Type")) {
		writeStatusError(w, http.StatusUnsupportedMediaType, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "請用 POST 並把 Content-Type 設成 application/json"})
		return
	}
	service, method, ok := splitAPIPath(r.URL.Path)
	if !ok {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "沒有這個路徑"})
		return
	}
	if !allowedAPI[service][method] {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "看板不提供這個服務"})
		return
	}
	target := m.api()
	if target == nil {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "核心服務尚未啟動"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		writeStatusError(w, http.StatusRequestEntityTooLarge, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "請求太大"})
		return
	}
	if service == "vault" && method != "changes" && !vaultPackageOnly(method, body) {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "看板不提供這個服務"})
		return
	}
	if service == "system" && method == "backups" && backupsApply(body) {
		// 看板只能看備份；要清跟任何一位秘書說（不轉給 api）。
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "看板不能刪備份；要清理，跟任何一位秘書說「幫我清理舊的設定備份」", Field: "apply"})
		return
	}
	body, serr := forceIdentity(service, method, body)
	if serr != nil {
		writeJSONError(w, serr)
		return
	}
	// 手機連線（DEV-153）：會動這台電腦的方法只准這台 Mac 本機開的看板，`local` 由這裡算（thismac.go）。
	if body, ok = gateThisMac(w, r, service, method, body); !ok {
		return
	}
	// 看的是換過身分、真的要交給 api 的那一份（大小寫不同的重複 key 在這裡已經合併，檢查與執行看到的一樣）。
	if service == "bus" && method == "subscribe" && !boardTopicsOnly(body) {
		writeJSONError(w, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "看板不提供這個服務"})
		return
	}
	r2 := r.Clone(r.Context())
	r2.Body = io.NopCloser(bytes.NewReader(body))
	r2.ContentLength = int64(len(body))
	r2.Header.Set("Content-Length", strconv.Itoa(len(body)))
	if !quickRead(service, method) {
		target.ServeHTTP(w, r2)
		return
	}
	// 讀取有時限（WEB-175）：後面的服務卡住（等鎖、等引擎）時回白話的 503，看板那一塊顯示「讀不到、可以再試」，不會一直轉圈。
	limit := m.readTimeout()
	started := time.Now()
	w.Header().Set("Content-Type", "application/json") // 逾時的回應用；正常回應會被服務自己的標頭蓋掉
	http.TimeoutHandler(target, limit, readTimeoutBody).ServeHTTP(w, r2)
	if time.Since(started) >= limit {
		m.logger().Warn("讀取等太久，已回逾時", "service", service, "method", method, "limit", limit.String())
	}
}

// apiReadTimeout 是看板讀取（quickRead）最多等多久；比看板前端的時限（transport.ts 的 READ_TIMEOUT_MS，10 秒）短，
// 前端先收到這裡的白話錯誤。
const apiReadTimeout = 8 * time.Second

var readTimeoutBody = func() string {
	b, _ := json.Marshal(&sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "看板服務忙不過來，這份資料等太久沒讀到；稍後再試一次"})
	return string(b)
}()

// slowReads 是本來就可能跑很久的讀取（健康檢查要逐項跑、查更新要連外網）：不設時限。跟 transport.ts 的 SLOW_READS 同一份。
var slowReads = map[string]bool{"web/get_health": true, "package/list_updates": true}

// quickRead：這個方法是不是「該很快回來的讀取」（方法名是 get、get_*、list、list_*）。
// 串流（watch、chat、subscribe）與會改東西的方法不算：中途切斷不知道做了沒，不設時限。
func quickRead(service, method string) bool {
	if slowReads[service+"/"+method] {
		return false
	}
	for _, verb := range []string{"get", "list"} {
		if method == verb || strings.HasPrefix(method, verb+"_") {
			return true
		}
	}
	return false
}

func (m *Module) readTimeout() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.apiReadLimit > 0 {
		return m.apiReadLimit
	}
	return apiReadTimeout
}

// vaultPackageOnly：看板的 vault 服務只給套件的資料種類（種類名稱含 `.`，例 meal 套件的 nutrition.meal）。
// put 看 record.kind，其他方法看 kind；沒帶種類、沒有 `.`（核心的種類，例如任務、待辦）或解不開都不給。
// 兩個例外（P6-6）：`changes`（變更紀錄，只讀，不看種類，在呼叫前就放行）；夜班清單那一筆 note（種類 `note`、編號 `system/night-queue`）的 get 與 put。
func vaultPackageOnly(method string, body []byte) bool {
	var req struct {
		Kind   string `json:"kind"`
		ID     string `json:"id"`
		Record *struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"record"`
	}
	if len(bytes.TrimSpace(body)) == 0 || json.Unmarshal(body, &req) != nil {
		return false
	}
	kind, id := req.Kind, req.ID
	if method == "put" {
		if req.Record == nil {
			return false
		}
		kind, id = req.Record.Kind, req.Record.ID
	}
	if (method == "get" || method == "put") && kind == "note" && id == nightQueueID {
		return true
	}
	return strings.Contains(kind, ".")
}

var boardTopics = map[string]bool{"vault.changed": true}

// boardTopicsOnly：bus.subscribe 的 topics 至少一個、而且全部在 boardTopics（不接受空的＝全部、也不接受結尾 *）。
func boardTopicsOnly(body []byte) bool {
	var req struct {
		Topics []string `json:"topics"`
	}
	if json.Unmarshal(body, &req) != nil || len(req.Topics) == 0 {
		return false
	}
	for _, t := range req.Topics {
		if !boardTopics[t] {
			return false
		}
	}
	return true
}

// backupsApply：system.backups 的請求有沒有要真的刪（apply 為真）。解不開的 body 不算，交給後面的檢查擋。
func backupsApply(body []byte) bool {
	var req struct {
		Apply bool `json:"apply"`
	}
	return json.Unmarshal(body, &req) == nil && req.Apply
}

func splitAPIPath(p string) (service, method string, ok bool) {
	parts := strings.Split(strings.TrimPrefix(p, "/api/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// api 回 api 模組的 handler（第一次用到才取，那時 api 已經啟動）。
func (m *Module) api() http.Handler {
	m.apiOnce.Do(func() {
		if m.d.API != nil {
			m.apiHandler = m.d.API.Handler()
		}
	})
	return m.apiHandler
}

// forceIdentity 讓看板來的請求一律算主人：
//   - 請求的 `by` 一律換成 owner(web)（沒有也補上；沒有 `by` 欄位的請求會忽略它）；
//   - bus.send 另外把 `from` 換成 owner(web)，而且只准 kind: chat。
func forceIdentity(service, method string, body []byte) ([]byte, *sdk.Error) {
	if len(bytes.TrimSpace(body)) == 0 {
		body = []byte("{}")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil || obj == nil {
		// 不是 JSON 物件（含 null、陣列）就沒辦法換身分，直接擋下，不交給服務。
		return nil, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "請傳入 JSON 物件"}
	}
	if service == "vault" && method == "changes" {
		// 只讀，沒有操作者；它的 `by` 是「篩選誰做的」（字串），不能換成身分。
		return body, nil
	}
	dropFold(obj, "by")
	obj["by"] = json.RawMessage(`{"name":"` + boardActor + `"}`)
	if service == "personas" && method == "pack_sync" {
		dropFold(obj, "pointer") // 指標檔是本機的路徑、只給指令用；看板的「現在再試」一律用服務自己的指標（DEV-157）
		dropFold(obj, "force")   // 看板只給「現在再試」，不給蓋掉主人改過的（強制同步只留給指令）
	}
	if service == "backup" {
		dropFold(obj, "path") // 這台電腦上的檔案路徑只給指令用；看板只能用上傳的檔或這台現有的備份
	}
	if service == "bus" && method == "send" {
		for k, v := range obj {
			if !strings.EqualFold(k, "kind") {
				continue
			}
			var kind string
			if json.Unmarshal(v, &kind) != nil || kind != string(sdk.EnvelopeKindChat) {
				return nil, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "看板只能送聊天訊息", Field: "kind"}
			}
		}
		dropFold(obj, "kind")
		dropFold(obj, "from")
		obj["kind"] = json.RawMessage(strconv.Quote(string(sdk.EnvelopeKindChat)))
		obj["from"] = json.RawMessage(strconv.Quote(boardActor))
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "請傳入有效的 JSON"}
	}
	return out, nil
}

// dropFold 刪掉大小寫不同但 Go 的 JSON 解碼器會當成同一個欄位的 key。
func dropFold(obj map[string]json.RawMessage, name string) {
	for k := range obj {
		if strings.EqualFold(k, name) {
			delete(obj, k)
		}
	}
}

// ---- 靜態檔 ----

func (m *Module) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "不支援這個方法", http.StatusMethodNotAllowed)
		return
	}
	if m.static == nil {
		http.Error(w, "看板的畫面檔不存在", http.StatusNotFound)
		return
	}
	p := r.URL.Path
	if p == "/" && r.URL.Query().Has("tab") {
		// 舊看板的 /?tab=chat&room=… 連結不支援（WEB-N4、REQ-108）。
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, oldTabNotice)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+p), "/")
	if strings.HasPrefix(name, "assets/") {
		if !m.serveFile(w, r, name, "public, max-age=31536000, immutable") {
			http.NotFound(w, r)
		}
		return
	}
	if name != "" && name != "index.html" && name != "manifest.webmanifest" {
		cache := "no-cache"
		if m.serveFile(w, r, name, cache) {
			return
		}
	}
	// SPA：其他路徑都回 index.html，由前端的路由決定畫什麼。依家改過（apple-touch-icon 的版本、開發家的顏色與標題，appicon.go）。
	raw, err := fs.ReadFile(m.static, "index.html")
	if err != nil {
		http.Error(w, "看板的畫面檔不存在", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", zeroTime, bytes.NewReader(m.indexHTML(r.Context(), raw)))
}

var zeroTime time.Time

// serveFile 回 static 裡的檔案；沒有（或是資料夾）回 false，什麼都沒寫。
func (m *Module) serveFile(w http.ResponseWriter, r *http.Request, name, cache string) bool {
	return serveFS(w, r, m.static, name, cache)
}

func serveFS(w http.ResponseWriter, r *http.Request, fsys fs.FS, name, cache string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	info, err := fs.Stat(fsys, name)
	if err != nil || info.IsDir() {
		return false
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return false
	}
	h := w.Header()
	h.Set("Cache-Control", cache)
	switch path.Base(name) {
	case "sw.js":
		h.Set("Service-Worker-Allowed", "/")
		h.Set("Content-Type", "text/javascript; charset=utf-8")
	case "manifest.webmanifest":
		h.Set("Content-Type", "application/manifest+json")
	}
	http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(data))
	return true
}

// ---- 頭像與分享檔 ----

func (m *Module) serveAvatar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "不支援這個方法", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/avatars/")
	ext := path.Ext(rest)
	_, known := avatarTypes[ext]
	// /avatars/persona/<角色代號>.png|.mp4（WEB-150，DEV-115）：照角色代號給頭像，不必有秘書坐著（新角色來打招呼的訊息用）；
	// 解析跟秘書頭像同一條鏈（Assets.Avatar），代號不合格或檔案不在一律 404。
	if slug, ok := strings.CutPrefix(rest, "persona/"); ok {
		slug, clip := splitClip(strings.TrimSuffix(slug, ext), ext)
		if !known || m.homeDir() == "" || strings.ContainsAny(slug, "/\\") {
			http.NotFound(w, r)
			return
		}
		if file, contentType, ok := m.d.Assets.AvatarClip(slug, ext, clip); ok {
			serveRegular(w, r, file, contentType, "inline", "no-cache")
			return
		}
		http.NotFound(w, r)
		return
	}
	id, clip := splitClip(strings.TrimSuffix(rest, ext), ext)
	if !known || !agentID.MatchString(id) || m.d.Agents == nil || m.homeDir() == "" {
		http.NotFound(w, r)
		return
	}
	agents, err := m.d.Agents.List(r.Context())
	if err != nil {
		http.NotFound(w, r)
		return
	}
	for _, a := range agents {
		if a == nil || a.ID != id {
			continue
		}
		slug, _ := a.Fields["persona"].(string)
		if file, contentType, ok := m.d.Assets.AvatarClip(slug, ext, clip); ok {
			serveRegular(w, r, file, contentType, "inline", "no-cache")
			return
		}
		break
	}
	http.NotFound(w, r)
}

// splitClip 把 "<名>+k"（只有 .mp4，k 是 2 到 maxClips）拆成 名 與 第幾支；沒有 +k 就是第 1 支。
// 格式不對的 +（例 "+1"、"+07"、"+9"）原樣留在名字裡，之後名字驗證會擋掉。
func splitClip(name, ext string) (string, int) {
	if ext != ".mp4" {
		return name, 1
	}
	i := strings.LastIndexByte(name, '+')
	if i < 0 {
		return name, 1
	}
	k, err := strconv.Atoi(name[i+1:])
	if err != nil || k < 2 || k > maxClips || strconv.Itoa(k) != name[i+1:] {
		return name, 1
	}
	return name[:i], k
}

func (m *Module) serveShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "不支援這個方法", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/share/")
	if name == "" || strings.ContainsAny(name, "/\\\x00") || strings.Contains(name, "..") || strings.HasPrefix(name, ".") || m.homeDir() == "" {
		http.NotFound(w, r)
		return
	}
	contentType, disposition := "application/octet-stream", "attachment"
	ext := strings.ToLower(filepath.Ext(name))
	if t, ok := imageTypes[ext]; ok {
		contentType, disposition = t, "inline"
	} else if t, ok := videoTypes[ext]; ok {
		contentType, disposition = t, "inline"
	}
	serveRegular(w, r, filepath.Join(platform.SharesDir(m.homeDir()), name), contentType, disposition, "no-cache")
}

// serveRegular 只送一般檔案（符號連結、資料夾都當不存在），並擋掉瀏覽器自己猜類型。
func serveRegular(w http.ResponseWriter, r *http.Request, file, contentType, disposition, cache string) {
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", cache)
	if disposition == "attachment" {
		h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filepath.Base(file)))
	} else {
		h.Set("Content-Disposition", "inline")
	}
	http.ServeContent(w, r, filepath.Base(file), info.ModTime(), f)
}
