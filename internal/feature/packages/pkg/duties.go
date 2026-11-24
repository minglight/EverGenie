package pkg

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// 官方職責（DEV-154，作者 2026-11-12：官方的東西不放家裡）：官方套件帶來的職位，職責全文跟著程式走，
// 秘書開場時直接讀樣板，家裡只留附加規則。這裡只回樣板的內容；誰用官方職責記在名冊（agent 紀錄的 duties_from）。

// OfficialDuty 是一個官方套件職位樣板的職責。
type OfficialDuty struct {
	Package string // 帶來這個職位的官方套件
	Agent   string // 職位 id
	Body    string // 樣板內文（職責全文）
	// Known 是樣板現在與以前各版出現過的行（去掉行尾空白，不含空白行；樣板旁的 history.txt 由 scripts/duty-history.sh 產生）。
	// 家的遷移用它分辨家裡舊職責內文的哪幾行是主人自己加的。
	Known map[string]bool
}

// OfficialDuties 列出家 home 用的官方套件帶來的職位樣板（不看套件有沒有啟用：職位還在，職責就要讀得到）。
// 樣板讀不起來的略過。key 是職位 id；兩個官方套件帶同一個職位時用套件 id 排前面的那個。
func OfficialDuties(home string) map[string]OfficialDuty { return officialDutiesIn(OfficialDir(home)) }

func officialDutiesIn(root string) map[string]OfficialDuty { return officialDutiesWhere(root, nil) }

// officialDutiesWhere 同 officialDutiesIn，只看 keep 回 true 的套件（keep 是 nil＝全部）。
func officialDutiesWhere(root string, keep func(pkgID string) bool) map[string]OfficialDuty {
	out := map[string]OfficialDuty{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, e := range entries { // ReadDir 已照名字排序
		name := e.Name()
		dir := filepath.Join(root, name)
		if !isDir(dir) || strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-") || (keep != nil && !keep(name)) || notAPackage(dir, sdk.TierOfficial) {
			continue
		}
		man, err := ParseManifest(dir, sdk.TierOfficial)
		if err != nil || man == nil || man.ID != name {
			continue
		}
		for _, a := range man.Contributes.Agent {
			if _, taken := out[a.ID]; taken {
				continue
			}
			path := filepath.Join(dir, filepath.FromSlash(a.Template))
			t, err := parseAgentTemplate(path, a.ID, a.Template)
			if err != nil {
				continue
			}
			d := OfficialDuty{Package: man.ID, Agent: a.ID, Body: t.Body, Known: map[string]bool{}}
			for _, l := range strings.Split(t.Body, "\n") {
				if l = strings.TrimRight(l, " \t\r"); l != "" {
					d.Known[l] = true
				}
			}
			if data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "history.txt")); err == nil {
				for _, l := range strings.Split(string(data), "\n") {
					if l = strings.TrimRight(l, " \t\r"); l != "" {
						d.Known[l] = true
					}
				}
			}
			out[a.ID] = d
		}
	}
	return out
}

// Ref 是記在名冊（agent 紀錄 duties_from）的寫法：<套件>/<樣板的職位>。
func (d OfficialDuty) Ref() string { return d.Package + "/" + d.Agent }

// movedDutyRefs 是搬過套件的官方樣板：名冊上記著舊的 ref，照樣讀到新位置那份（舊的 → 新的）。
// 生圖 art：DEV-157 放在 secretaries，2026-11-15 起在開發工坊 admin（DEV-160，ADM-39）；
// 中間那段時間從看板認領的 art 記的是 secretaries/art，職位還在、職責就要讀得到。
var movedDutyRefs = map[string]string{"secretaries/art": "admin/art"}

// officialDuty 回 ref（<套件>/<樣板的職位>）那份樣板；套件不在、不是它帶的職位、樣板讀不起來回 false。
func officialDuty(all map[string]OfficialDuty, ref string) (OfficialDuty, bool) {
	if to, moved := movedDutyRefs[ref]; moved {
		ref = to
	}
	pkgID, agentID, ok := strings.Cut(ref, "/")
	d, found := all[agentID]
	return d, ok && found && d.Package == pkgID
}

func (m *Module) officialRoot() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return resolveOfficialDir(m.officialAt, m.home)
}

// OfficialDuties 回官方職責樣板 ref（<套件>/<樣板的職位>）的職責全文（名冊組指令用）。
func (m *Module) OfficialDuties(ref string) (string, bool) {
	d, ok := officialDuty(officialDutiesIn(m.officialRoot()), ref)
	return d.Body, ok
}

// TemplateDutiesRef 回職位 id 對得上的官方樣板（<套件>/<職位>）；沒有回空字串。建秘書時用：對得上就用官方職責。
// 只看啟用中、沒被拒的官方套件（跟 TemplateDuty 一樣）：停用的套件帶的職位——例：一般家沒開的開發工坊帶的生圖 art
// （DEV-160）——不會因為主人自己建了同一個 id 的職位就套上它的官方職責；那是主人自己的職位，職責寫在家裡。
func (m *Module) TemplateDutiesRef(id string) string {
	root := m.officialRoot()
	on := map[string]bool{}
	m.mu.Lock()
	for pkgID, e := range m.entries {
		if e.tier == sdk.TierOfficial && e.err == nil && e.enabled {
			on[pkgID] = true
		}
	}
	m.mu.Unlock()
	if d, ok := officialDutiesWhere(root, func(pkgID string) bool { return on[pkgID] })[id]; ok {
		return d.Ref()
	}
	return ""
}
