package board

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 語音捷徑下載（WEB-170）：設定 → 裝置推播的「iPhone 與 Apple Watch 語音傳話」按這個網址下載「跟秘書說」捷徑。
// 捷徑裡的看板網址是這個家的 web.public_url（沒設就用請求的 *.ts.net 主機），所以每個家下載到的都送回自己。
// iOS 只匯入簽過名的捷徑：用 macOS 的 `shortcuts sign --mode anyone` 簽，第一次要等 Apple 的伺服器（實測幾秒到兩分鐘），
// 簽好的檔依內容（網址＋捷徑版本）快取在 runtime/voice/，之後直接送。
const (
	voiceShortcutPath = "/voice-shortcut"
	voiceShortcutName = "跟秘書說"
)

// signShortcut 把 in（XML plist）簽成 out；測試換掉它。
// shortcuts sign 只認副檔名 .shortcut 的輸入（別的副檔名回「isn't in the correct format」），內容是 XML 也可以。
var signShortcut = func(ctx context.Context, in, out string) error {
	cmd := exec.CommandContext(ctx, "shortcuts", "sign", "--mode", "anyone", "--input", in, "--output", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v：%s", err, strings.TrimSpace(string(b)))
	}
	return nil
}

var voiceSignTimeout = 3 * time.Minute

// voiceBoardURL 是要寫進捷徑的看板網址；空字串＝不知道手機該連哪裡。
func (m *Module) voiceBoardURL(r *http.Request) string {
	a := m.access()
	if a.publicHost != "" {
		scheme := a.publicScheme
		if scheme == "" {
			scheme = "https"
		}
		return scheme + "://" + a.publicHost
	}
	host := strings.ToLower(r.Host)
	if strings.HasSuffix(host, ".ts.net") && !strings.Contains(host, ":") {
		return "https://" + host
	}
	return ""
}

func (m *Module) serveVoiceShortcut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "不支援這個方法", http.StatusMethodNotAllowed)
		return
	}
	home := m.homeDir()
	base := m.voiceBoardURL(r)
	if home == "" || base == "" {
		writeStatusError(w, http.StatusConflict, &sdk.Error{Code: sdk.ErrorCodeConflict, Message: "這個家還沒設定看板網址（home.toml 的 web.public_url），手機不知道要連哪裡"})
		return
	}
	plist := voiceShortcutPlist(base + "/api/web/voice_send")
	sum := sha256.Sum256([]byte(plist))
	// 一種內容（網址＋捷徑版本）一個資料夾、檔名固定「跟秘書說」：下載名就是 iOS 匯入後的捷徑名字（用 /share/ 時前面帶編號，名字一長串）。
	dir := platform.RuntimeDir(home, "voice", hex.EncodeToString(sum[:6]))
	out := filepath.Join(dir, voiceShortcutName+".shortcut")
	if _, err := os.Stat(out); err != nil {
		if err := m.makeVoiceShortcut(r.Context(), dir, out, plist); err != nil {
			if log := m.logger(); log != nil {
				log.Warn("voice shortcut sign failed", "err", err)
			}
			writeStatusError(w, http.StatusServiceUnavailable, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "捷徑還沒簽好（要這台 Mac 的「捷徑」幫忙簽名），過一下再按一次"})
			return
		}
	}
	serveRegular(w, r, out, "application/octet-stream", "attachment", "no-cache")
}

func (m *Module) makeVoiceShortcut(ctx context.Context, dir, out, plist string) error {
	m.voiceMu.Lock()
	defer m.voiceMu.Unlock()
	if _, err := os.Stat(out); err == nil {
		return nil // 等鎖的時候別人簽好了
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	in := filepath.Join(dir, "unsigned.shortcut")
	if err := os.WriteFile(in, []byte(plist), 0o644); err != nil {
		return err
	}
	defer func() { _ = os.Remove(in) }()
	tmp := filepath.Join(dir, "signing.shortcut")
	_ = os.Remove(tmp)
	// 主人關掉頁面也簽完：下一次就直接有檔。
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), voiceSignTimeout)
	defer cancel()
	if err := signShortcut(sctx, in, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if info, err := os.Stat(tmp); err != nil || info.Size() == 0 {
		_ = os.Remove(tmp)
		return fmt.Errorf("簽名沒有產生檔案")
	}
	return os.Rename(tmp, out)
}

// voiceShortcutPlist：聽寫（中文台灣，停頓就結束）→ POST {text} 到語音入口 → 取 reply 跳通知 → 取 speak 唸出來
// （問問題時 speak 是秘書的簡短回答，CHT-10；交代事情是「交給某某了」）。
// 顯示結果不用 showresult：iOS 26 把它認成「播放音樂」（2026-11-08 作者實測「轉換錯誤」）。
func voiceShortcutPlist(endpoint string) string {
	const dictate, post, reply, speak = "6B1F2A0E-6E3D-4C59-9C7A-1D7A0B5E0001", "6B1F2A0E-6E3D-4C59-9C7A-1D7A0B5E0002", "6B1F2A0E-6E3D-4C59-9C7A-1D7A0B5E0003", "6B1F2A0E-6E3D-4C59-9C7A-1D7A0B5E0004"
	result := map[string]any{"Value": map[string]any{"OutputUUID": post, "OutputName": "URL 的內容", "Type": "ActionOutput"}, "WFSerializationType": "WFTextTokenAttachment"}
	token := func(uuid, name string) map[string]any {
		return map[string]any{
			"Value": map[string]any{
				"string":             "￼",
				"attachmentsByRange": map[string]any{"{0, 1}": map[string]any{"OutputUUID": uuid, "OutputName": name, "Type": "ActionOutput"}},
			},
			"WFSerializationType": "WFTextTokenString",
		}
	}
	action := func(id string, params map[string]any) map[string]any {
		return map[string]any{"WFWorkflowActionIdentifier": id, "WFWorkflowActionParameters": params}
	}
	wf := map[string]any{
		"WFWorkflowActions": []any{
			action("is.workflow.actions.dictatetext", map[string]any{"UUID": dictate, "WFSpeechLanguage": "zh-TW", "WFDictateTextStopListening": "After Pause"}),
			action("is.workflow.actions.downloadurl", map[string]any{
				"UUID": post, "WFURL": endpoint, "WFHTTPMethod": "POST", "WFHTTPBodyType": "JSON", "ShowHeaders": false,
				"WFJSONValues": map[string]any{
					"Value": map[string]any{"WFDictionaryFieldValueItems": []any{map[string]any{
						"WFItemType": 0,
						"WFKey":      map[string]any{"Value": map[string]any{"string": "text"}, "WFSerializationType": "WFTextTokenString"},
						"WFValue":    token(dictate, "已聽寫的文字"),
					}}},
					"WFSerializationType": "WFDictionaryFieldValue",
				},
			}),
			action("is.workflow.actions.getvalueforkey", map[string]any{"UUID": reply, "WFDictionaryKey": "reply", "WFInput": result}),
			action("is.workflow.actions.notification", map[string]any{"WFNotificationActionTitle": "秘書", "WFNotificationActionBody": token(reply, "字典值"), "WFNotificationActionSound": false}),
			action("is.workflow.actions.getvalueforkey", map[string]any{"UUID": speak, "WFDictionaryKey": "speak", "WFInput": result}),
			action("is.workflow.actions.speaktext", map[string]any{"WFText": token(speak, "字典值"), "WFSpeakTextLanguage": "zh-TW", "WFSpeakTextWait": true}),
		},
		"WFWorkflowClientVersion":              "2607.0.2",
		"WFWorkflowMinimumClientVersion":       900,
		"WFWorkflowMinimumClientVersionString": "900",
		"WFWorkflowIcon":                       map[string]any{"WFWorkflowIconGlyphNumber": 59511, "WFWorkflowIconStartColor": 4282601983},
		"WFWorkflowImportQuestions":            []any{},
		"WFWorkflowInputContentItemClasses":    []any{},
		"WFWorkflowOutputContentItemClasses":   []any{},
		"WFWorkflowTypes":                      []any{"Watch", "NCWidget"},
		"WFWorkflowHasShortcutInputVariables":  false,
		"WFQuickActionSurfaces":                []any{},
		"WFWorkflowName":                       voiceShortcutName,
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">`)
	writePlist(&b, wf)
	b.WriteString("</plist>\n")
	return b.String()
}

func writePlist(b *strings.Builder, v any) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("<dict>")
		for _, k := range keys {
			b.WriteString("<key>" + plistEscape(k) + "</key>")
			writePlist(b, x[k])
		}
		b.WriteString("</dict>")
	case []any:
		b.WriteString("<array>")
		for _, e := range x {
			writePlist(b, e)
		}
		b.WriteString("</array>")
	case string:
		b.WriteString("<string>" + plistEscape(x) + "</string>")
	case bool:
		if x {
			b.WriteString("<true/>")
		} else {
			b.WriteString("<false/>")
		}
	case int:
		fmt.Fprintf(b, "<integer>%d</integer>", x)
	default:
		panic(fmt.Sprintf("plist 不支援 %T", v))
	}
}

func plistEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
