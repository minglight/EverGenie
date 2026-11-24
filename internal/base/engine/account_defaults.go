package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/registry"
)

// accountDefaultsKey：引擎給的帳號設定只收小寫英數與底線的鍵。
var accountDefaultsKey = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// accountDefaultsTimeout 是等引擎回答的上限；引擎的子行程抓著輸出不放時，再多等 accountDefaultsWaitDelay 就不管它。
const (
	accountDefaultsTimeout   = 10 * time.Second
	accountDefaultsWaitDelay = 2 * time.Second
)

// AccountDefaults 問引擎套件「新家第一個帳號的 engine_config」（ENG-19）：執行 `<引擎執行檔> account-defaults`，
// 讀它印的一個 JSON 物件。核心不解讀內容（ENG-4、法律第 3 條），只收值是字串或布林、鍵是小寫英數與底線的物件。
// 問不到（找不到執行檔、結束碼不是 0、逾時、印的不是這種物件）回 nil 與一句給主人看的白話原因（不用「引擎」這種內部用語）。
func AccountDefaults(ctx context.Context, engineID string) (map[string]any, string) {
	bin := FindBinary(registry.Executables()[engineID])
	if bin == "" {
		return nil, "程式裡少了連 AI 用的那一支"
	}
	ctx, cancel := context.WithTimeout(ctx, accountDefaultsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "account-defaults")
	cmd.WaitDelay = accountDefaultsWaitDelay
	out, err := cmd.Output()
	if err != nil {
		return nil, "沒問到這台電腦登入的是哪個 AI 帳號"
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out), &m); err != nil || len(m) == 0 {
		return nil, "問到的帳號設定看不懂"
	}
	for k, v := range m {
		switch v.(type) {
		case string, bool:
		default:
			return nil, "問到的帳號設定看不懂"
		}
		if !accountDefaultsKey.MatchString(k) {
			return nil, "問到的帳號設定看不懂"
		}
	}
	return m, ""
}
