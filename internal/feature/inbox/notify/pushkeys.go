package notify

import (
	"bytes"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/SherClockHolmes/webpush-go"
	"github.com/minglight/evergenie/internal/base/platform"
)

// EnsurePushKeys 替這個家準備手機推播要用的金鑰（VAPID）：已經有就不動（force 才重做），沒有就產生一組、寫進秘密檔（0600）、
// 必要時把 home.toml 的 [secrets] 接上。回秘密檔的路徑與這次有沒有新做。不印、不回金鑰的值（REQ-120）。
// `evergenie push init` 與看板「手機連線」那顆按鈕（DEV-153，經 inbox.EnsurePushKeys）共用這一個；
// 金鑰每次用到才重讀（PSH-2），做好不用重開服務。subject 空的用預設值。
func EnsurePushKeys(home, subject string, force bool) (path string, made bool, err error) {
	if subject == "" {
		subject = defaultPushSubject
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return "", false, err
	}
	path, needConfig := h.SecretsPath(), false
	if path == "" {
		path, needConfig = h.Paths().Secrets("push.toml"), true // 第 2 版在狀態資料夾的 secrets/
	}
	raw := map[string]any{}
	if data, rerr := os.ReadFile(path); rerr == nil {
		if _, derr := toml.Decode(string(data), &raw); derr != nil {
			return path, false, platform.Invalid("secrets", "秘密檔已經存在但讀不懂，沒有動它："+path)
		}
		if wp, ok := raw["webpush"].(map[string]any); ok && wp["vapid_public_key"] != nil && !force {
			return path, false, nil
		}
	}
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return path, false, platform.Unavailable("產生金鑰失敗")
	}
	raw["webpush"] = map[string]any{"vapid_public_key": public, "vapid_private_key": private, "vapid_subject": subject}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(raw); err != nil {
		return path, false, platform.Unavailable("無法寫出秘密檔")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return path, false, platform.Unavailable("無法建立秘密檔的資料夾")
	}
	if err := platform.AtomicWrite(path, buf.Bytes(), 0o600); err != nil {
		return path, false, platform.Unavailable("無法寫入秘密檔")
	}
	if needConfig {
		// 一律寫 secrets/push.toml：第 2 版由 SecretsPath 對到狀態資料夾，搬家不用改設定。
		if _, _, err := platform.SetHomeValue(home, "secrets.file", "secrets/push.toml", time.Now()); err != nil {
			return path, false, err
		}
	}
	return path, true, nil
}

// PushKeysReady：這個家有沒有推播金鑰（秘密檔的 [webpush] 有 vapid_public_key）。只看有沒有，不讀出值。
func PushKeysReady(home string) bool {
	h, err := platform.LoadHome(home)
	if err != nil || h.SecretsPath() == "" {
		return false
	}
	data, err := os.ReadFile(h.SecretsPath())
	if err != nil {
		return false
	}
	raw := map[string]any{}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return false
	}
	wp, ok := raw["webpush"].(map[string]any)
	return ok && wp["vapid_public_key"] != nil
}
