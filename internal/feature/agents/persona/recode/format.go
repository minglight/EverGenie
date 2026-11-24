package recode

import (
	"fmt"
	"path/filepath"
	"strings"
)

// 給人看的輸出：白話、可以直接貼給主人；細節（每一步、每一處）用 --json 看。

const (
	maxNotes   = 30
	maxBackups = 20
)

func summaryLine(s Summary) string {
	parts := []string{"卡"}
	if s.Media > 0 {
		parts = append(parts, fmt.Sprintf("圖 %d 張", s.Media))
	}
	if len(s.Roster) > 0 {
		parts = append(parts, fmt.Sprintf("名冊 %d 個職位（%s）", len(s.Roster), strings.Join(s.Roster, "、")))
	}
	if s.Records > 0 {
		parts = append(parts, fmt.Sprintf("套件紀錄 %d 筆", s.Records))
	}
	if s.Settings > 0 {
		parts = append(parts, fmt.Sprintf("設定 %d 處", s.Settings))
	}
	return fmt.Sprintf("%s → %s：%s", s.Old, s.New, strings.Join(parts, "、"))
}

func (p *Plan) summaryOf(old string) (Summary, bool) {
	for _, s := range p.Summary {
		if s.Old == old {
			return s, true
		}
	}
	return Summary{}, false
}

// writeNotes 印列出不動的地方（最多 maxNotes 處）與不能做的原因。
func (p *Plan) writeNotes(b *strings.Builder) {
	if len(p.Notes) > 0 {
		fmt.Fprintf(b, "\n沒跟著改、要人看的地方（%d 處，這裡不改）\n", len(p.Notes))
		for i, n := range p.Notes {
			if i == maxNotes {
				fmt.Fprintf(b, "  …還有 %d 處（完整清單加 --json）\n", len(p.Notes)-maxNotes)
				break
			}
			where := n.Path
			if n.Line > 0 {
				where = fmt.Sprintf("%s:%d", n.Path, n.Line)
			}
			if n.Text != "" {
				fmt.Fprintf(b, "  %s　%s　（%s）\n", where, n.Text, n.Why)
			} else {
				fmt.Fprintf(b, "  %s　（%s）\n", where, n.Why)
			}
		}
	}
	if len(p.Problems) > 0 {
		b.WriteString("\n不能做的原因（要先解決）\n")
		for _, s := range p.Problems {
			fmt.Fprintf(b, "  %s\n", s)
		}
	}
}

// Text 是 home tidy 的輸出；applied＝已經套用了。
func (t *Tidy) Text(applied bool) string {
	var b strings.Builder
	if applied {
		fmt.Fprintf(&b, "整理好了：%s\n", t.Home)
	} else {
		fmt.Fprintf(&b, "整理這個家（預演，什麼都沒動）：%s\n", t.Home)
	}
	b.WriteString("\n一、自己的角色改成 my_ 開頭\n")
	if len(t.Mine) == 0 && len(t.Undecided) == 0 {
		b.WriteString("  都已經是 my_ 開頭，沒有要改的\n")
	}
	for _, c := range t.Mine {
		if s, ok := t.summaryOf(c.Old); ok {
			fmt.Fprintf(&b, "  %s\n", summaryLine(s))
		}
	}
	if len(t.Undecided) > 0 {
		b.WriteString("  沒自動改、請主人決定：\n")
		for _, n := range t.Undecided {
			fmt.Fprintf(&b, "    %s：%s\n", n.Path, n.Why)
		}
	}
	b.WriteString("\n二、放在官方位置、但不是官方的角色\n")
	if len(t.Misplaced) == 0 {
		b.WriteString("  沒有\n")
	} else {
		fmt.Fprintf(&b, "  %s\n  %s\n", strings.Join(t.Misplaced, "、"), t.MisplacedWhy)
	}
	for _, c := range t.Adopted {
		if s, ok := t.summaryOf(c.Old); ok {
			fmt.Fprintf(&b, "  收成自己的：%s（照片紀錄指著的圖搬進 personas/mine/ 的同一個位置，檔名不改）\n", summaryLine(s))
		}
	}
	b.WriteString("\n三、官方的卡與圖搬出家\n")
	if len(t.Official) > 0 {
		fmt.Fprintf(&b, "  還在家裡：%s\n", strings.Join(t.Official, "、"))
	}
	switch {
	case t.OfficialWhy != "":
		fmt.Fprintf(&b, "  %s\n", t.OfficialWhy)
	case len(t.Official) == 0:
		b.WriteString("  家裡沒有官方的卡與圖\n")
	}
	fmt.Fprintf(&b, "\n四、家裡散著的舊備份（只列出，不搬；%d 個）\n", len(t.Backups))
	for i, s := range t.Backups {
		if i == maxBackups {
			fmt.Fprintf(&b, "  …還有 %d 個（完整清單加 --json）\n", len(t.Backups)-maxBackups)
			break
		}
		fmt.Fprintf(&b, "  %s\n", s)
	}
	t.writeNotes(&b)
	switch {
	case applied:
		fmt.Fprintf(&b, "\n做了 %d 步，紀錄在 settings/home-tidy.json（改過的檔原文在 settings/home-tidy-originals/）。\n"+
			"要退回：evergenie home tidy --revert --apply；確定不退了：evergenie home tidy --forget --apply（收尾後才能再整理、做 home relayout）\n", len(t.Steps))
	case len(t.Steps) == 0:
		b.WriteString("\n沒有要做的。\n")
	case len(t.Problems) > 0:
		fmt.Fprintf(&b, "\n共 %d 步；先解決上面的原因才能套用。\n", len(t.Steps))
	default:
		fmt.Fprintf(&b, "\n共 %d 步。要套用：同樣的參數加 --apply（服務要先停）\n", len(t.Steps))
	}
	return strings.TrimRight(b.String(), "\n")
}

// PromoteReminder 是升成官方之後一定要講的兩件事。
const PromoteReminder = "提醒：\n" +
	"  - 衣服還是「自己加的」：要用展示間的衣服「升成官方」一套一套升（圖才會搬到官方位置；自己的隱藏版不能升）\n" +
	"  - 要帶到別的家：把這位加進 home.toml [admin] public_personas，再建角色包（evergenie personas pack build）"

// Text 是 personas promote 的輸出；applied＝已經套用了。
func (p *Promote) Text(applied bool) string {
	var b strings.Builder
	if applied {
		fmt.Fprintf(&b, "%s 升成官方 %s 了\n", p.Mine, p.Official)
	} else {
		fmt.Fprintf(&b, "升成官方（預演，什麼都沒動）：%s → %s\n", p.Mine, p.Official)
	}
	if s, ok := p.summaryOf(p.Mine); ok {
		fmt.Fprintf(&b, "  卡：%s（內容原樣）\n", s.Card)
		fmt.Fprintf(&b, "  圖：%d 張留在 personas/mine/，檔名不改（照片紀錄跟著新代號）\n", s.Media)
		if len(s.Roster) > 0 {
			fmt.Fprintf(&b, "  名冊：%s\n", strings.Join(s.Roster, "、"))
		}
		fmt.Fprintf(&b, "  套件紀錄：%d 筆　設定：%d 處\n", s.Records, s.Settings)
	}
	p.writeNotes(&b)
	b.WriteString("\n" + PromoteReminder + "\n")
	switch {
	case applied:
		fmt.Fprintf(&b, "\n紀錄在 settings/persona-promote/%s.json。要退回：evergenie personas promote %s --revert --apply；"+
			"確定不退了：evergenie personas promote %s --forget --apply（收尾後才能做 home relayout）\n", p.Mine, p.Mine, p.Mine)
	case len(p.Problems) > 0:
		b.WriteString("\n先解決上面的原因才能升。\n")
	default:
		b.WriteString("\n要升：同樣的參數加 --apply（服務要先停）\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// Text 是退回的預演或結果。
func (r *RevertPlan) Text(applied bool) string {
	var b strings.Builder
	switch {
	case applied:
		fmt.Fprintf(&b, "退回好了（%s，%d 步），家跟做之前一樣；紀錄檔與原文已經拿掉\n", r.Tool, r.Steps)
	default:
		fmt.Fprintf(&b, "照紀錄退回（預演，什麼都沒動）：%s\n  %s 做的，%s，共 %d 步\n", r.Journal, r.Tool, r.At, r.Steps)
		if !r.Done {
			b.WriteString("  上次沒做完（中途停了），退回只退做過的那些\n")
		}
	}
	for _, c := range r.Renames {
		fmt.Fprintf(&b, "  %s → %s 改回 %s\n", c.Old, c.New, c.Old)
	}
	if len(r.Problems) > 0 {
		b.WriteString("\n不能退回的原因（什麼都不會動）\n")
		for _, s := range r.Problems {
			fmt.Fprintf(&b, "  %s\n", s)
		}
	} else if !applied {
		b.WriteString("\n要退回：同樣的參數加 --apply（服務要先停）\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// Text 是收尾的預演或結果。
func (f *ForgetPlan) Text(applied bool) string {
	var b strings.Builder
	if applied {
		fmt.Fprintf(&b, "收尾好了（%s，%s 做的）：紀錄留成備份，之後不能再 --revert\n", f.Tool, f.At)
	} else {
		fmt.Fprintf(&b, "收尾（預演，什麼都沒動）：確定不退了，紀錄留成備份（30 天後可以刪）；收尾後不能再 --revert\n  %s 做的，%s\n", f.Tool, f.At)
	}
	for _, c := range f.Renames {
		fmt.Fprintf(&b, "  %s → %s\n", c.Old, c.New)
	}
	for _, m := range f.Moves {
		fmt.Fprintf(&b, "  %s → %s\n", m[0], filepath.Base(m[1]))
	}
	if len(f.Problems) > 0 {
		b.WriteString("\n不能收尾的原因（什麼都不會動）\n")
		for _, s := range f.Problems {
			fmt.Fprintf(&b, "  %s\n", s)
		}
	} else if !applied {
		b.WriteString("\n要收尾：同樣的參數加 --apply\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
