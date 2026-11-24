package agent

import (
	"context"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 兼任的職位（DEV-161，decisions 第 20 條，作者：「放一起就是想讓她融會貫通」）：同一位角色擔任好幾個職位時，
// 每個職位的開場另外帶其他職位的專業背景（她是誰、基本知識、勾了的可選知識、聊到什麼先看什麼、回答時怎麼用）。
// 記憶、日誌、你主動照顧的事、第一天上班、界線都不帶：那些仍在那個職位自己的對話。規則 ai_docs/modules/agent.md AGT-43。

// TopicCuesSection 是職責裡「聊到什麼先看什麼」那一節的標題（SCR-5）；主人在附加規則裡加的同名一段，兼任的職位也一起帶。
const TopicCuesSection = "聊到什麼先看什麼"

// TopicCuesClosing 是官方職責「聊到什麼先看什麼」一節的最後一句（SCR-5，六份一字不差，cmd/evergenie 的職責格式測試守著）。
// 兼任的職位帶那一節時拿掉這句：主人說「以後聊到 X 先看 Y」記在現在這個職位自己的附加規則，自己的職責已經有這句。
const TopicCuesClosing = "主人說「以後聊到 X 先看 Y」時，用 `evergenie customize add <你的職位> --section " + TopicCuesSection + " <那一條>` 加進你附加規則裡同名的一段，不改官方檔；這裡寫的和主人加的兩邊都照做，衝突時聽主人加的。"

// sideSections 是兼任的職位帶進開場的職責段落，照這個順序。
var sideSections = []string{"她是誰", "基本知識", KnowledgeSection, TopicCuesSection, "回答時怎麼用"}

// sidePositions 組「兼任的職位」那一節：rows 是名冊（沒封存的），self 是這次開場的職位。
// 其他啟用（enabled 不是 false）、persona 跟 self 一樣的職位各一段；帶不出任何段落的職位（職責沒照 SCR-5 寫）略過，
// 全部略過時回空字串（不出這一節，所以沒兼職時開場跟以前逐字相同）。
func (m *Module) sidePositions(ctx context.Context, self vault.Record, rows []Agent) (string, error) {
	slug, _ := self.Fields["persona"].(string)
	if strings.TrimSpace(slug) == "" {
		return "", nil
	}
	var parts []string
	for _, a := range rows {
		if a.ID == self.ID || a.Archived {
			continue
		}
		if on, ok := a.Fields["enabled"].(bool); ok && !on {
			continue
		}
		if p, _ := a.Fields["persona"].(string); p != slug {
			continue
		}
		addendum, err := m.d.Vault.Get(ctx, "addendum", a.ID)
		if err != nil && platform.CodeOf(err) != sdk.ErrorCodeNotFound {
			return "", err
		}
		body := sideKnowledge(m.DutiesOf(*a), addendum.Body)
		if body == "" {
			continue
		}
		who := a.ID
		if duty, _ := a.Fields["duty"].(string); strings.TrimSpace(duty) != "" {
			who += "（" + strings.TrimSpace(duty) + "）"
		}
		parts = append(parts, "### "+a.ID+"\n\n你同時擔任 "+who+"，下面是那個職位的知識，回答時一起用；那個職位的記憶與日常照顧仍在它自己的對話。"+
			"下面提到那個職位的記憶或沒帶過來的段落時，用 `evergenie memory show "+a.ID+"`、`evergenie customize show "+a.ID+"` 看。\n\n"+body)
	}
	return strings.Join(parts, "\n\n"), nil
}

// sideKnowledge 從一個職位的職責（已篩過可選知識）取兼任要帶的段落，標題降兩級（放在「兼任的職位」→「### <職位>」底下）；
// 「聊到什麼先看什麼」拿掉節尾那句（TopicCuesClosing），後面接那個職位附加規則裡同名的一段。一段都沒有回空字串。
func sideKnowledge(duties, addendum string) string {
	sections := h2Sections(duties)
	if cues, ok := sections[TopicCuesSection]; ok {
		var keep []string
		for _, l := range strings.Split(cues, "\n") {
			if strings.TrimSpace(l) != TopicCuesClosing {
				keep = append(keep, l)
			}
		}
		sections[TopicCuesSection] = strings.Join(keep, "\n")
	}
	if extra := strings.TrimSpace(h2Sections(addendum)[TopicCuesSection]); extra != "" {
		cues := strings.TrimSpace(sections[TopicCuesSection])
		if cues != "" {
			cues += "\n\n"
		}
		sections[TopicCuesSection] = cues + "主人加的（跟上面衝突時照這幾條）：\n" + extra
	}
	var out []string
	for _, name := range sideSections {
		if body := strings.TrimSpace(sections[name]); body != "" {
			out = append(out, "#### "+name+"\n\n"+demoteBy(body, 2))
		}
	}
	return strings.Join(out, "\n\n")
}

// demoteBy 把程式碼區塊以外、開頭是 # 的標題行多加 n 個 #（放進更深一層的段落裡）。
func demoteBy(text string, n int) string {
	lines := strings.Split(text, "\n")
	fence := false
	for i, l := range lines {
		t := strings.TrimLeft(l, " \t")
		if isFence(t) {
			fence = !fence
			continue
		}
		if !fence && isHeading(t) {
			lines[i] = l[:len(l)-len(t)] + strings.Repeat("#", n) + t
		}
	}
	return strings.Join(lines, "\n")
}

// isHeading：一個以上的 # 之後是空白或行尾（`#標籤` 這種不算標題）。
func isHeading(t string) bool {
	rest := strings.TrimLeft(t, "#")
	return len(rest) < len(t) && (rest == "" || rest[0] == ' ' || rest[0] == '\t')
}
