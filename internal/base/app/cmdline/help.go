package cmdline

import (
	"sort"
	"strings"
)

// Group 是 help 裡的分段。段的標題與順序固定在這裡（通用的分類，不認得任何業務模組）；業務模組的指令在自己的
// Commands() 選一段。沒選的（GroupOther）排在最後的「其他」。
type Group int

const (
	GroupOther    Group = iota // 其他
	GroupWork                  // 任務、提醒與通知
	GroupAgents                // 秘書與對話
	GroupFeatures              // 套件、角色與資料
	GroupSchedule              // 排程
	GroupSystem                // 這台電腦與家
)

var groupOrder = []Group{GroupWork, GroupAgents, GroupFeatures, GroupSchedule, GroupSystem, GroupOther}

var groupTitles = map[Group]string{
	GroupWork:     "任務、提醒與通知",
	GroupAgents:   "秘書與對話",
	GroupFeatures: "套件、角色與資料",
	GroupSchedule: "排程",
	GroupSystem:   "這台電腦與家",
	GroupOther:    "其他",
}

// helpHeader 是 help 的開頭：通用的用法與旗標規則（cmdline 的 Spec 怎麼解析旗標）。
const helpHeader = `用法：evergenie <指令> [參數] [--旗標 值]
旗標名稱就是請求的欄位名（例：--verify_how），可重複給的欄位（例：--add_links）重複寫旗標即可。
第一個位置參數是編號（例：evergenie tasks get SEC-1）。子指令加 --help 看它有哪些旗標（例：evergenie reminders add --help）。
`

// usageIndent 是多行用法後面幾行的縮排（對齊用法第一行後面的說明欄）。
const usageIndent = "                                    "

func topWord(name string) string {
	if i := strings.IndexByte(name, ' '); i >= 0 {
		return name[:i]
	}
	return name
}

// Help 從合併後的指令表組出 help：開頭的通用說明，接著依 Group 分段，每段依頂層指令名稱排序，一個頂層指令一行
// （多行用法的後面幾行縮排對齊）。沒寫 Usage 的頂層指令列「名稱 子指令|子指令」。
func Help(cmds map[string]Command) string {
	type top struct {
		usage string
		group Group
		subs  []string
	}
	tops := map[string]*top{}
	for name, c := range cmds {
		w := topWord(name)
		t := tops[w]
		if t == nil {
			t = &top{}
			tops[w] = t
		}
		if sub := strings.TrimPrefix(name, w); sub != "" {
			t.subs = append(t.subs, strings.TrimSpace(sub))
		}
		if c.Usage != "" {
			t.usage, t.group = c.Usage, c.Group
		}
	}
	byGroup := map[Group][]string{}
	for w, t := range tops {
		byGroup[t.group] = append(byGroup[t.group], w)
	}
	var b strings.Builder
	b.WriteString(helpHeader)
	for _, g := range groupOrder {
		names := byGroup[g]
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		b.WriteString("\n" + groupTitles[g] + "\n")
		for _, w := range names {
			t := tops[w]
			usage := t.usage
			if usage == "" {
				sort.Strings(t.subs)
				usage = w
				if len(t.subs) > 0 {
					usage += " " + strings.Join(t.subs, "|")
				}
			}
			for i, line := range strings.Split(usage, "\n") {
				if i == 0 {
					b.WriteString("  " + line + "\n")
				} else {
					b.WriteString(usageIndent + line + "\n")
				}
			}
		}
	}
	return b.String()
}

// With 回帶了用法與分段的複本（給 HostCommand、SubHost 這種回 Command 的殼用）。
func (c Command) With(usage string, g Group) Command {
	c.Usage, c.Group = usage, g
	return c
}
