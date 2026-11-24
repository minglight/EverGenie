package agent

import (
	"regexp"
	"strings"

	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 可選知識（DEV-161）：職責文字裡 `## 可選知識` 底下每段 `### <標題> {#<id>}` 是一塊可以勾掉的專業背景，
// 標題下一行 `> 預設：勾`（或 `> 預設：不勾`）、再一行 `> 一句說明`。主人勾了哪幾塊記在名冊紀錄的 knowledge（id 清單）；
// 沒有這個欄位＝照每塊的預設。開場（DutiesOf）只留勾了的那幾塊，全部沒勾整段拿掉。格式見 ai_docs/modules/secretaries.md。

// KnowledgeSection 是可選知識那一節的標題文字（`## 可選知識`）。
const KnowledgeSection = "可選知識"

var knowledgeHeading = regexp.MustCompile(`^###\s+(.+?)\s*\{#([a-z][a-z0-9-]*)\}\s*$`)

// KnowledgeBlock 是職責文字裡的一塊可選知識。
type KnowledgeBlock struct {
	ID      string
	Title   string
	Summary string
	Default bool
}

// knowledgePart 是職責文字切開後的一段：可選知識那一節裡的一塊（block 有值）或其他文字。
type knowledgePart struct {
	lines   []string
	block   *KnowledgeBlock
	section bool // 這段是 `## 可選知識` 那一節（標題行與第一塊之前的說明）
}

func isH2(l string) bool { return strings.HasPrefix(l, "## ") || l == "##" }

// splitKnowledge 把職責文字切成段：可選知識一節的開頭、每一塊、其他文字。沒有那一節就整份是一段。
func splitKnowledge(body string) []knowledgePart {
	lines := strings.Split(body, "\n")
	var parts []knowledgePart
	cur := knowledgePart{}
	flush := func(next knowledgePart) {
		if len(cur.lines) > 0 || cur.block != nil {
			parts = append(parts, cur)
		}
		cur = next
	}
	in := false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimRight(l, " \t\r")
		switch {
		case isH2(t) || strings.HasPrefix(t, "# "):
			in = isH2(t) && strings.TrimSpace(strings.TrimPrefix(t, "##")) == KnowledgeSection
			flush(knowledgePart{section: in})
			cur.lines = append(cur.lines, l)
		case in && knowledgeHeading.MatchString(t):
			m := knowledgeHeading.FindStringSubmatch(t)
			b := &KnowledgeBlock{ID: m[2], Title: m[1]}
			flush(knowledgePart{block: b})
			cur.lines = append(cur.lines, "### "+b.Title)
			// 標題下面連續的 `>` 行：第一行是預設、其餘是一句說明；開場不帶這幾行。
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), ">") {
				i++
				q := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), ">"))
				if v, ok := strings.CutPrefix(q, "預設："); ok {
					b.Default = strings.TrimSpace(v) == "勾"
				} else if q != "" {
					if b.Summary != "" {
						b.Summary += " "
					}
					b.Summary += q
				}
			}
		default:
			cur.lines = append(cur.lines, l)
		}
	}
	flush(knowledgePart{})
	return parts
}

// ParseKnowledge 找出職責文字裡的可選知識（照出現順序）；沒有回空。
func ParseKnowledge(body string) []KnowledgeBlock {
	var out []KnowledgeBlock
	for _, p := range splitKnowledge(body) {
		if p.block != nil {
			out = append(out, *p.block)
		}
	}
	return out
}

// KnowledgeChoice 回名冊紀錄的 knowledge（主人勾了的 id）；第二個值 false＝沒有這個欄位（照預設）。
func KnowledgeChoice(fields map[string]any) ([]string, bool) {
	v, ok := fields["knowledge"]
	if !ok || v == nil {
		return nil, false
	}
	return stringList(v), true
}

// EnabledKnowledge 算出哪幾塊有勾：紀錄有 knowledge 時，主人看過的塊（knowledge_seen）照他勾的，
// 沒看過的塊（官方之後新加的）照它的預設；紀錄沒有 knowledge 全部照預設。
// 有 knowledge 但沒有 knowledge_seen（欄位加上之前寫的）當成每塊都看過。
func EnabledKnowledge(blocks []KnowledgeBlock, fields map[string]any) map[string]bool {
	chosen, ok := KnowledgeChoice(fields)
	on := map[string]bool{}
	for _, id := range chosen {
		on[id] = true
	}
	seen, hasSeen := map[string]bool{}, false
	if v, present := fields["knowledge_seen"]; present && v != nil {
		hasSeen = true
		for _, id := range stringList(v) {
			seen[id] = true
		}
	}
	out := map[string]bool{}
	for _, b := range blocks {
		switch {
		case !ok, hasSeen && !seen[b.ID]:
			out[b.ID] = b.Default
		default:
			out[b.ID] = on[b.ID]
		}
	}
	return out
}

// KeepKnowledge 回只留勾了的可選知識的職責文字：塊的標題留著（拿掉 `{#id}`）、預設與說明那幾行 `>` 拿掉；
// 一塊都沒勾就把整節（含 `## 可選知識` 標題）拿掉。沒有可選知識的職責原樣回。
func KeepKnowledge(body string, enabled map[string]bool) string {
	parts := splitKnowledge(body)
	has := false
	for _, p := range parts {
		if p.block != nil {
			has = true
			break
		}
	}
	if !has {
		return body
	}
	kept := false
	for _, p := range parts {
		if p.block != nil && enabled[p.block.ID] {
			kept = true
		}
	}
	var lines []string
	for _, p := range parts {
		switch {
		case p.block != nil && !enabled[p.block.ID]:
			continue
		case p.section && !kept:
			continue
		}
		lines = append(lines, p.lines...)
	}
	return strings.Join(lines, "\n")
}

// rawDuties 回職責全文（沒篩過可選知識）：官方職位讀樣板，自建職位讀紀錄內文。
func (m *Module) rawDuties(r vault.Record) string {
	from := DutiesFrom(r.Fields)
	if from == "" {
		return r.Body
	}
	if m.duties != nil {
		if body, ok := m.duties(from); ok {
			return body
		}
	}
	return "（這個職位用的是官方職責「" + from + "」，但現在讀不到它的樣板。先照附加規則與主人交代的做，並跟主人說一聲。）"
}

// KnowledgeOf 列出這位秘書職責裡的可選知識，與現在有沒有勾（給看板、CLI 勾選用）。
func (m *Module) KnowledgeOf(r vault.Record) []sdk.AgentKnowledgeBlock {
	blocks := ParseKnowledge(m.rawDuties(r))
	on := EnabledKnowledge(blocks, r.Fields)
	out := make([]sdk.AgentKnowledgeBlock, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, sdk.AgentKnowledgeBlock{ID: b.ID, Title: b.Title, Summary: b.Summary, Default: b.Default, Enabled: on[b.ID]})
	}
	return out
}
