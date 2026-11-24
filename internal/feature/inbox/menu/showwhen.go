// internal/feature/inbox/menu/showwhen.go — ai_docs/modules/inbox.md（MNU-3、MNU-11）。
package menu

import (
	"context"
	"errors"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 套件的能力紀錄（MNU-11）：套件自己算好「這個家支援哪些動作」，寫在自己的資料種類 `capabilities`、編號 `main`，
// 欄位 `supports` 是能力名稱的清單。核心不知道哪個套件有什麼能力，只照這個約定讀，不判斷是哪種家。
const (
	capabilitiesKind  = "capabilities"
	capabilitiesID    = "main"
	capabilitiesField = "supports"
)

// showWhen 判斷選單項目與 slot 頁面的 show_when（同一個寫法，MNU-3、MNU-11）。
// 一次請求開一個：名冊、各套件的能力紀錄第一次用到才讀，讀過就記著。讀不到一律當不成立（不顯示，fail closed）。
type showWhen struct {
	m       *Module
	ctx     context.Context
	pkgs    map[string]sdk.Package // 看得到的套件（啟用而且沒被拒）
	enabled map[string]bool        // 啟用中的職位；nil＝還沒讀
	caps    map[string]map[string]bool
}

func (m *Module) newShowWhen(ctx context.Context, pkgs []sdk.Package) *showWhen {
	byID := make(map[string]sdk.Package, len(pkgs))
	for _, p := range pkgs {
		byID[p.ID] = p
	}
	return &showWhen{m: m, ctx: ctx, pkgs: byID, caps: map[string]map[string]bool{}}
}

// show 回 true＝列出來。key 是項目或頁面的完整 id（警告只講一次用）。
func (w *showWhen) show(key, when string) bool {
	when = strings.TrimSpace(when)
	if when == "" {
		return true
	}
	if agent, ok := parseAgentEnabled(when); ok {
		return w.agentEnabled(agent)
	}
	if pkg, name, ok := parseCap(when); ok {
		return w.hasCap(pkg, name)
	}
	w.m.warnOnce("show-when:"+key, "不認得的 show_when 寫法，這一項不顯示（認得 agent:<職位 id>:enabled 與 cap:<套件>.<能力>）", "item", key, "show_when", when)
	return false
}

func (w *showWhen) agentEnabled(id string) bool {
	if w.enabled == nil {
		w.enabled = map[string]bool{}
		list, err := w.m.agents(w.ctx)
		if err != nil {
			w.m.warnOnce("show-when-roster", "讀不到名冊，show_when 要看秘書的項目先不顯示", "error", err)
		}
		for _, a := range list {
			if on, _ := a.Fields["enabled"].(bool); on {
				w.enabled[a.ID] = true
			}
		}
	}
	return w.enabled[id]
}

// hasCap：那個套件看得到、宣告了 capabilities 種類，而且 `<套件>.capabilities/main` 的 supports 有這個能力。
// 套件關掉（舊紀錄還在）、紀錄還沒寫、封存了、讀不到，都當沒有。
func (w *showWhen) hasCap(pkg, name string) bool {
	set, ok := w.caps[pkg]
	if !ok {
		set = w.readCaps(pkg)
		w.caps[pkg] = set
	}
	return set[name]
}

func (w *showWhen) readCaps(pkg string) map[string]bool {
	p, visible := w.pkgs[pkg]
	if !visible || !declaresKind(p, capabilitiesKind) {
		return nil
	}
	if w.m.d.Records == nil {
		return nil
	}
	r, err := w.m.d.Records.Get(w.ctx, pkg+"."+capabilitiesKind, capabilitiesID)
	if err != nil {
		if platform.CodeOf(err) != sdk.ErrorCodeNotFound && !errors.Is(err, context.Canceled) {
			w.m.warnOnce("show-when-cap:"+pkg, "讀不到套件的能力紀錄，show_when 要看它的項目先不顯示", "package", pkg, "error", err)
		}
		return nil
	}
	if r.Archived {
		return nil
	}
	set := map[string]bool{}
	switch list := r.Fields[capabilitiesField].(type) {
	case []any:
		for _, v := range list {
			if s, ok := v.(string); ok && s != "" {
				set[s] = true
			}
		}
	case []string:
		for _, s := range list {
			if s != "" {
				set[s] = true
			}
		}
	}
	return set
}

func declaresKind(p sdk.Package, kind string) bool {
	for _, d := range p.Data {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

// parseAgentEnabled 認得 `agent:<職位 id>:enabled`。
func parseAgentEnabled(when string) (string, bool) {
	parts := strings.Split(when, ":")
	if len(parts) == 3 && parts[0] == "agent" && parts[1] != "" && parts[2] == "enabled" {
		return parts[1], true
	}
	return "", false
}

// parseCap 認得 `cap:<套件 id>.<能力>`（套件 id 沒有點，能力是一段、沒有點與冒號）。
func parseCap(when string) (pkg, name string, ok bool) {
	rest, found := strings.CutPrefix(when, "cap:")
	if !found {
		return "", "", false
	}
	pkg, name, found = strings.Cut(rest, ".")
	if !found || pkg == "" || name == "" || strings.ContainsAny(pkg, ": \t") || strings.ContainsAny(name, ".: \t") {
		return "", "", false
	}
	return pkg, name, true
}
