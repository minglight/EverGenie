package agent

import (
	"context"
	"regexp"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// slot 的資料格式（Q1，取代舊家的 overrides/slots/<名>.md 整檔覆寫）：
//   - addendum.md 裡用 `## slot: <名稱>` 開一段，到下一個 `## ` 標題為止是那個 slot 的內容。
//   - 排程內文用 `{{slot <名稱>}}預設內容{{/slot}}` 標出可以換掉的地方；addendum 有那個 slot 就換成它，沒有就用預設內容。

var slotHeading = regexp.MustCompile(`^##\s+slot:\s*([a-z0-9][a-z0-9-]*)\s*$`)
var slotBlock = regexp.MustCompile(`(?s)\{\{slot ([a-z0-9][a-z0-9-]*)\}\}(.*?)\{\{/slot\}\}`)

// ParseSlots 從 addendum 內文找出每個 `## slot: <名稱>` 段落（去掉頭尾空行）。
func ParseSlots(addendum string) map[string]string {
	out := map[string]string{}
	name := ""
	var buf []string
	flush := func() {
		if name != "" {
			out[name] = strings.Trim(strings.Join(buf, "\n"), "\n")
		}
	}
	for _, line := range strings.Split(addendum, "\n") {
		if m := slotHeading.FindStringSubmatch(strings.TrimRight(line, " \t\r")); m != nil {
			flush()
			name, buf = m[1], nil
			continue
		}
		if strings.HasPrefix(line, "## ") {
			flush()
			name, buf = "", nil
			continue
		}
		if name != "" {
			buf = append(buf, line)
		}
	}
	flush()
	return out
}

// RenderSlots 把內文裡的 `{{slot 名稱}}…{{/slot}}` 換成 slots 裡的內容，沒有就留預設內容。
func RenderSlots(body string, slots map[string]string) string {
	return slotBlock.ReplaceAllStringFunc(body, func(block string) string {
		m := slotBlock.FindStringSubmatch(block)
		if v, ok := slots[m[1]]; ok {
			return v
		}
		return strings.Trim(m[2], "\n")
	})
}

// RenderJob 用這位秘書的 addendum 套好排程內文的 slot（jobs 送排程封包時呼叫）。
func (m *Module) RenderJob(ctx context.Context, agentID, body string) (string, error) {
	r, err := m.d.Vault.Get(ctx, "addendum", agentID)
	if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
		return RenderSlots(body, nil), nil
	}
	if err != nil {
		return RenderSlots(body, nil), err
	}
	return RenderSlots(body, ParseSlots(r.Body)), nil
}
