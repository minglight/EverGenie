package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/feature/packages/pkg"
)

// cuesClosing 是每份官方職責「聊到什麼先看什麼」那一節固定的最後一句（SCR-5）。
const cuesClosing = agent.TopicCuesClosing

// 官方職責（四個預設職位、health、開發工坊的 art）照固定段落順序寫（DEV-161，ai_docs/modules/secretaries.md SCR-5），
// 可選知識每塊都解析得出 id、預設與一句說明；`## 可選知識` 底下的 ### 都要有 {#id}（不然開場篩不掉）。
// 六份都要有「聊到什麼先看什麼」：3–5 條，最後一行是固定的那一句。
func TestOfficialDutiesKnowledgeFormat(t *testing.T) {
	order := []string{"她是誰", "基本知識", "可選知識", agent.TopicCuesSection, "回答時怎麼用", "你主動照顧的事", "第一天上班", "這個職位特有的界線"}
	want := map[string]struct {
		pkg    string
		blocks []agent.KnowledgeBlock
	}{
		"life":   {"secretaries", []agent.KnowledgeBlock{{ID: "parenting", Default: false}, {ID: "balance", Default: true}}},
		"work":   {"secretaries", []agent.KnowledgeBlock{{ID: "leading", Default: true}}},
		"system": {"secretaries", nil},
		"code":   {"secretaries", nil},
		"health": {"health", nil},
		"art":    {"admin", nil},
	}
	official, err := filepath.Abs(filepath.Join("..", "..", "packages", "official"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", official)
	all := pkg.OfficialDuties(t.TempDir())
	for id, w := range want {
		d, ok := all[id]
		if !ok || d.Package != w.pkg {
			t.Fatalf("找不到官方職位 %s（套件 %s）", id, w.pkg)
		}
		blocks := w.blocks
		var heads, cues []string
		inKnowledge, inCues, h3 := false, false, 0
		for _, l := range strings.Split(d.Body, "\n") {
			if strings.HasPrefix(l, "## ") {
				heads = append(heads, strings.TrimPrefix(l, "## "))
				inKnowledge = l == "## 可選知識"
				inCues = l == "## "+agent.TopicCuesSection
				continue
			}
			if inKnowledge && strings.HasPrefix(l, "### ") {
				h3++
			}
			if inCues && strings.TrimSpace(l) != "" {
				cues = append(cues, l)
			}
		}
		if len(cues) == 0 || cues[len(cues)-1] != cuesClosing {
			t.Fatalf("%s 要有「## %s」，最後一行是固定的那一句：%q", id, agent.TopicCuesSection, cues)
		}
		if n := countBullets(cues); n < 3 || n > 5 {
			t.Fatalf("%s 的「%s」要 3–5 條，現在 %d 條", id, agent.TopicCuesSection, n)
		}
		k := 0
		for _, h := range heads {
			for k < len(order) && order[k] != h {
				k++
			}
			if k == len(order) {
				t.Fatalf("%s 的段落 %v 要照順序 %v（沒有可選知識可以省略那一段）", id, heads, order)
			}
		}
		got := agent.ParseKnowledge(d.Body)
		if h3 != len(got) {
			t.Fatalf("%s：可選知識底下有 %d 個 ###，解析出 %d 塊（每塊標題要寫 {#id}）", id, h3, len(got))
		}
		var ids []agent.KnowledgeBlock
		for _, b := range got {
			if b.Title == "" || b.Summary == "" {
				t.Fatalf("%s 的 %s 要有標題與一句說明：%+v", id, b.ID, b)
			}
			ids = append(ids, agent.KnowledgeBlock{ID: b.ID, Default: b.Default})
		}
		if !reflect.DeepEqual(ids, blocks) {
			t.Fatalf("%s 的可選知識：%+v 要 %+v", id, ids, blocks)
		}
	}
}

func countBullets(lines []string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "- ") {
			n++
		}
	}
	return n
}
