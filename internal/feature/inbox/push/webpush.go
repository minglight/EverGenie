package push

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/minglight/evergenie/internal/base/platform"
)

// pushTTL matches the legacy board: a phone offline for hours still gets the notice.
const pushTTL = 12 * 60 * 60

// vapid 是秘密檔 [webpush] 的三個欄位；只在函式內短暫存在，不快取、不寫進 log（REQ-120）。
type vapid struct{ public, private, subject string }

// readVAPID 讀秘密檔並檢查三個欄位與 subject。錯誤訊息只帶欄位名，不帶值或檔案內容。
func (s *Service) readVAPID() (vapid, error) {
	path := s.secretsPath()
	if path == "" {
		return vapid{}, platform.Unavailable("尚未設定推播秘密檔")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return vapid{}, platform.Unavailable("無法讀取推播秘密檔")
	}
	var cfg struct {
		WebPush struct {
			Public  string `toml:"vapid_public_key"`
			Private string `toml:"vapid_private_key"`
			Subject string `toml:"vapid_subject"`
		} `toml:"webpush"`
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return vapid{}, platform.Invalid("webpush", "推播秘密檔格式不正確")
	}
	for _, field := range []struct{ name, value string }{
		{"vapid_public_key", cfg.WebPush.Public}, {"vapid_private_key", cfg.WebPush.Private}, {"vapid_subject", cfg.WebPush.Subject},
	} {
		if strings.TrimSpace(field.value) == "" {
			return vapid{}, platform.Invalid(field.name, "推播金鑰缺 "+field.name)
		}
	}
	if !strings.HasPrefix(cfg.WebPush.Subject, "mailto:") && !strings.HasPrefix(cfg.WebPush.Subject, "https://") {
		return vapid{}, platform.Invalid("vapid_subject", "推播聯絡方式必須以 mailto: 或 https:// 開頭")
	}
	return vapid{public: cfg.WebPush.Public, private: cfg.WebPush.Private, subject: cfg.WebPush.Subject}, nil
}

// PublicKey 回 VAPID 公開金鑰（手機訂閱時要用）。沒設定或壞了回錯誤；錯誤只帶欄位名，不帶值。
func (s *Service) PublicKey() (string, error) {
	key, err := s.readVAPID()
	if err != nil {
		return "", err
	}
	return key.public, nil
}

// Send 把 payload 加密後送到 sub 這個端點。只在真的要送時才讀金鑰。
// 回的錯誤一律是整理過的 platform 錯誤（白話、不帶解碼器、檔案或加密的原始訊息；推播服務拒絕時只帶狀態碼與過濾後的原因代碼，見 rejected），呼叫者可以放心寫進 log。
func (s *Service) Send(ctx context.Context, sub Subscription, payload []byte) error {
	key, err := s.readVAPID()
	if err != nil {
		return err
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{Subscriber: librarySubscriber(key.subject), VAPIDPublicKey: key.public,
		VAPIDPrivateKey: key.private, TTL: pushTTL, HTTPClient: &http.Client{Timeout: 15 * time.Second}})
	if err != nil {
		return platform.Unavailable("推播送出失敗，請檢查訂閱與金鑰")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return platform.Unavailable(rejected(resp))
	}
	return nil
}

// librarySubscriber 把設定的聯絡方式轉成 webpush-go 要的樣子：它遇到不是 https: 開頭的會自己在前面加 mailto:，
// 設定寫的「mailto:x@y」交給它就變成「mailto:mailto:x@y」，Apple 的推播服務回 403 BadJwtToken
// （Google 的不檢查，所以只有 iPhone 收不到；2026-11-03 實機查到）。mailto: 開頭的先拿掉，讓它加回一次。
func librarySubscriber(subject string) string {
	return strings.TrimPrefix(subject, "mailto:")
}

// rejected 把推播服務的拒絕整理成白話：狀態碼＋服務給的原因代碼（Apple 回 {"reason":"BadJwtToken"} 這類，
// FCM 回一行文字）。只留英數與少數符號、最多 60 字，不帶端點網址、金鑰或回應原文，可以放心寫進 log 與畫面。
// 沒有這兩個資訊時看不出是訂閱失效、金鑰對不上還是服務端問題（2026-11-03 實機推播查不出原因）。
func rejected(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	reason := ""
	var apple struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal(body, &apple) == nil && apple.Reason != "" {
		reason = apple.Reason
	} else {
		reason, _, _ = strings.Cut(strings.TrimSpace(string(body)), "\n")
	}
	reason = safeReason(reason)
	msg := fmt.Sprintf("推播服務未接受通知（HTTP %d", resp.StatusCode)
	if reason != "" {
		msg += " " + reason
	}
	msg += "）"
	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusGone:
		msg += "：收件裝置的訂閱已失效，在那台裝置關閉推播再重新開啟"
	case http.StatusForbidden, http.StatusUnauthorized:
		msg += "：推播服務不認這把金鑰或聯絡方式"
	}
	return msg
}

// safeReason 只留英數、空白與 _ . : - ，最多 60 字；網址（含 // ）整段不要。
func safeReason(s string) string {
	if strings.Contains(s, "//") {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= 60 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == ' ', r == '_', r == '.', r == ':', r == '-':
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
