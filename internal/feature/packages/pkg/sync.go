package pkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 套件帶來的秘書職責與排程樣板（D-28、REQ-174）：`packages sync <套件>` 預設只預演，主人同意才 --apply；
// 掃描套件時絕不自動改任何秘書或排程。

var (
	agentIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)
	// sectionPattern：選單項目的 section（同一頁的分頁）。
	sectionPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	colorPattern   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// AgentTemplate 是 agents/<id>/agent.md：YAML 標頭 duty（必填）、color、color_dark、mode、persona、optional（選填），內文是職責。
type AgentTemplate struct {
	ID        string
	Duty      string // 一句話職責（名冊用）
	Color     string
	ColorDark string
	Mode      string // resident｜ondemand｜none；空＝用預設
	Persona   string // 預設角色（人設代號或 <套件id>.<slug>）；空＝沒指定。Sync 沒給 --persona 時新增秘書用它（SEC-302 S3，REQ-324）
	// Optional 是選用職位（標頭 optional: true，DEV-157）：不自動帶出來——Sync 沒指定職位時略過、
	// ClaimNotify 沒指定職位時不挑它、設定精靈不列；主人指名（--agent、設定頁「選用的職位」那張卡）才加。
	Optional bool
	Body     string // 職責全文
}

// SyncHost 是 Sync 需要的、別的模組的能力（組裝時接 agent 與 scheduler；測試用假的）。
type SyncHost interface {
	// DefaultPersona 是 home.toml 的 [defaults] persona；沒設定回空字串。
	DefaultPersona() string
	// AgentDuties 讀一位秘書現在的職責全文；沒有這位秘書 exists 是 false；封存過的 archived 是 true（id 不能重用由 packages 判斷）。
	AgentDuties(ctx context.Context, id string) (body string, exists, archived bool, err error)
	// CreateAgent 建一位新秘書（停用的），人設、職責照給的；persona 一定不是空的（預設人設由 packages 選好）。
	// knowledge 是要勾的可選知識 id（nil＝照職責的預設；不認得的 id 由名冊擋，DEV-161）。
	CreateAgent(ctx context.Context, id, persona, duty string, knowledge []string, by string) error
	// SetAgentField 改新秘書的一個欄位（packages 用來套上樣板的 color、color_dark、mode）。
	SetAgentField(ctx context.Context, id, field, value, by string) error
	// WriteDuties 寫官方職責（agent.md 內文，by 一律是 "system"）；addendum 不動。
	WriteDuties(ctx context.Context, id, body, by string) error
	// JobIDs 列出這位秘書全部排程的完整 id（<秘書>/<id>），含已移除（封存）的。
	JobIDs(ctx context.Context, agent string) ([]string, error)
	// CheckPersona 這個人設現在能不能指派給新秘書（存在、已上線、套件有效）；不能就回白話原因。
	CheckPersona(persona string) error
	// SaveJob 新增一個排程（已存在不改）。
	SaveJob(ctx context.Context, j *sdk.Job, by string) error
}

// SetSyncHost 接上 Sync 的做法；沒接時 Sync 回 UNAVAILABLE。
func (m *Module) SetSyncHost(h SyncHost) { m.mu.Lock(); m.sync = h; m.mu.Unlock() }

// parseAgentTemplate 讀並驗證職位樣板。錯誤的 field 是 contributes.agent，說明寫哪個檔哪一欄。
func parseAgentTemplate(path, id, rel string) (AgentTemplate, error) {
	t := AgentTemplate{ID: id}
	bad := func(format string, a ...any) error {
		return platform.Invalid("contributes.agent", fmt.Sprintf("職位「%s」的樣板 %s：", id, rel)+fmt.Sprintf(format, a...))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return t, bad("讀不了這個檔案")
	}
	r, _, err := vault.Decode(data)
	if err != nil {
		return t, bad("要有 YAML 標頭（開頭一行 ---，標頭結束再一行 ---）")
	}
	for k := range r.Fields {
		switch k {
		case "duty", "color", "color_dark", "mode", "persona", "optional":
		default:
			return t, bad("標頭有不認得的欄位「%s」（只能寫 duty、color、color_dark、mode、persona、optional）", k)
		}
	}
	str := func(key string) (string, bool) {
		v, present := r.Fields[key]
		if !present {
			return "", true
		}
		s, ok := v.(string)
		return strings.TrimSpace(s), ok
	}
	var ok bool
	if t.Duty, ok = str("duty"); !ok || t.Duty == "" {
		return t, bad("標頭要用一句話寫 duty（職責）")
	}
	if t.Color, ok = str("color"); !ok || (t.Color != "" && !colorPattern.MatchString(t.Color)) {
		return t, bad("color 要寫成 #rrggbb")
	}
	if t.ColorDark, ok = str("color_dark"); !ok || (t.ColorDark != "" && !colorPattern.MatchString(t.ColorDark)) {
		return t, bad("color_dark 要寫成 #rrggbb")
	}
	if t.Mode, ok = str("mode"); !ok || (t.Mode != "" && t.Mode != "resident" && t.Mode != "ondemand" && t.Mode != "none") {
		return t, bad("mode 要是 resident、ondemand 或 none")
	}
	if t.Persona, ok = str("persona"); !ok || (t.Persona != "" && !personaref.ValidSlug(t.Persona) && !isPersonaCode(t.Persona)) {
		return t, bad("persona 要是人設代號（小寫英文加數字，例 athena）或套件內角色代號（<套件id>.<slug>，例 garden.fairy）")
	}
	if v, present := r.Fields["optional"]; present {
		b, isBool := v.(bool)
		if !isBool {
			return t, bad("optional 要寫 true 或 false（true＝選用職位，主人指名才加）")
		}
		t.Optional = b
	}
	t.Body = strings.TrimRight(r.Body, "\n")
	if strings.TrimSpace(t.Body) == "" {
		return t, bad("內文（職責全文）不能是空的")
	}
	return t, nil
}

func isPersonaCode(s string) bool { _, _, ok := personaref.SplitPackageCode(s); return ok }

// parseJobTemplate 讀排程樣板：格式同 vault 的排程檔（schemas/job.md）——標頭 name、cron（或 windows…）、enabled、catchup、action、sites、silent，內文是給秘書的指令。
func parseJobTemplate(path string, c ContribJob, rel string) (*sdk.Job, error) {
	bad := func(format string, a ...any) error {
		return platform.Invalid("contributes.job", fmt.Sprintf("排程「%s/%s」的樣板 %s：", c.Agent, c.ID, rel)+fmt.Sprintf(format, a...))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, bad("讀不了這個檔案")
	}
	r, _, err := vault.Decode(data)
	if err != nil {
		return nil, bad("要有 YAML 標頭（開頭一行 ---，標頭結束再一行 ---）")
	}
	j := &sdk.Job{ID: c.Agent + "/" + c.ID, Agent: c.Agent, Enabled: true, Body: strings.TrimRight(r.Body, "\n")}
	list := func(key string) ([]string, error) {
		v, present := r.Fields[key]
		if !present {
			return nil, nil
		}
		switch x := v.(type) {
		case string:
			return []string{x}, nil
		case []any:
			out := make([]string, 0, len(x))
			for _, item := range x {
				s, ok := item.(string)
				if !ok {
					return nil, bad("%s 要是文字（或文字清單）", key)
				}
				out = append(out, s)
			}
			return out, nil
		}
		return nil, bad("%s 要是文字（或文字清單）", key)
	}
	num := func(key string) (int, error) {
		switch x := r.Fields[key].(type) {
		case nil:
			return 0, nil
		case int:
			return x, nil
		case float64:
			return int(x), nil
		}
		return 0, bad("%s 要是整數", key)
	}
	for k, v := range r.Fields {
		var err error
		switch k {
		case "name", "catchup", "action":
			s, ok := v.(string)
			if !ok {
				return nil, bad("%s 要是文字", k)
			}
			switch k {
			case "name":
				j.Name = s
			case "catchup":
				j.Catchup = s
			default:
				j.Action = s
			}
		case "enabled", "silent":
			b, ok := v.(bool)
			if !ok {
				return nil, bad("%s 要寫 true 或 false", k)
			}
			if k == "enabled" {
				j.Enabled = b
			} else {
				j.Silent = b
			}
		case "cron":
			j.Cron, err = list(k)
		case "windows":
			j.Windows, err = list(k)
		case "weekend_windows":
			j.WeekendWindows, err = list(k)
		case "sites":
			continue // 舊套件的跨台限制已廢除
		case "per_day":
			j.PerDay, err = num(k)
		case "idle_min":
			j.IdleMin, err = num(k)
		default:
			return nil, bad("標頭有不認得的欄位「%s」", k)
		}
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(j.Body) == "" {
		return nil, bad("內文（給秘書的指令）不能是空的")
	}
	return j, nil
}

// Sync 預演或套用套件帶來的秘書職責與排程樣板（REQ-174）。
func (m *Module) Sync(ctx context.Context, req *sdk.PackageSyncRequest) (*sdk.PackageSyncResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	who := actor(req.By)
	if req.Apply && !platform.IsOwner(who) {
		return nil, platform.Forbidden("只有主人能套用套件的職責與排程")
	}
	if req.Knowledge != nil && req.Agent == "" {
		return nil, platform.Invalid("knowledge", "要勾可選知識，請同時指定是哪個職位（agent）")
	}
	m.mu.Lock()
	if err := m.readyLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	host := m.sync
	e := m.entries[req.ID]
	var man *Manifest
	var dir string
	switch {
	case e == nil:
		m.mu.Unlock()
		return nil, platform.NotFound(fmt.Sprintf("沒有套件「%s」", req.ID))
	case e.err != nil:
		m.mu.Unlock()
		return nil, platform.Invalid("id", fmt.Sprintf("套件「%s」現在載入不了（%s），先把它修好", req.ID, e.err))
	case !e.enabled:
		m.mu.Unlock()
		return nil, platform.Invalid("id", fmt.Sprintf("套件「%s」已停用，先啟用它再同步", req.ID))
	}
	man, dir = e.man, e.dir
	official := e.tier == sdk.TierOfficial // 官方套件帶的職位：職責跟著程式走（名冊記 duties_from，開場讀樣板），同步不寫職責（DEV-154）
	m.mu.Unlock()
	if host == nil {
		return nil, platform.Unavailable("套件同步還沒接上")
	}
	// 不是官方套件的職位，職責要建好秘書之後才寫進家裡，建的當下驗不了 id：先建好，再用 agents knowledge 勾（DEV-161）。
	if req.Knowledge != nil && !official {
		return nil, platform.Invalid("knowledge", "只有官方職位能在派職位時一起勾可選知識；這個職位請建好後用 evergenie agents knowledge 勾")
	}

	// 先把全部樣板讀完、驗完，再看現況；任何一個樣板有問題就整個不動。
	// 沒指定職位時略過名冊上還沒有的選用職位（連同它的排程）：主人要指名（--agent）才加（DEV-157）；
	// 已經在名冊上的選用職位照一般職位走（職責差異、排程照常）。封存過的也算「還沒有」，略過、不回衝突。
	var templates []AgentTemplate
	skipped := map[string]bool{}
	out := &sdk.PackageSyncResponse{}
	found := req.Agent == ""
	for _, a := range man.Contributes.Agent {
		if req.Agent != "" && a.ID != req.Agent {
			continue
		}
		found = true
		t, err := parseAgentTemplate(filepath.Join(dir, filepath.FromSlash(a.Template)), a.ID, a.Template)
		if err != nil {
			return nil, err
		}
		if t.Optional && req.Agent == "" {
			_, exists, _, err := host.AgentDuties(ctx, t.ID)
			if err != nil {
				return nil, err
			}
			if !exists {
				skipped[t.ID] = true
				out.Optional = append(out.Optional, t.ID)
				continue
			}
		}
		templates = append(templates, t)
	}
	if !found {
		return nil, platform.NotFound(fmt.Sprintf("套件「%s」沒有職位「%s」", req.ID, req.Agent))
	}
	var jobs []*sdk.Job
	for _, c := range man.Contributes.Job {
		if (req.Agent != "" && c.Agent != req.Agent) || skipped[c.Agent] {
			continue
		}
		j, err := parseJobTemplate(filepath.Join(dir, filepath.FromSlash(c.Path)), c, c.Path)
		if err != nil {
			return nil, err
		}
		j.Source = req.ID
		jobs = append(jobs, j)
	}

	for _, t := range templates {
		current, exists, err := agentDuties(ctx, host, t.ID)
		if err != nil {
			return nil, err
		}
		a := sdk.SyncAgent{ID: t.ID}
		switch {
		case !exists:
			a.Action = "create"
			a.Added = int32(len(splitLines([]byte(t.Body))))
			a.Persona, a.PersonaFrom = pickPersona(req.Persona, t.Persona, host.DefaultPersona())
			if a.PersonaFrom == "template" { // 樣板預設的角色要真的能用（沒給 --persona 才檢查；給了就以主人指定的為準，由建立時檢查）
				if err := host.CheckPersona(a.Persona); err != nil {
					return nil, platform.Invalid("persona", fmt.Sprintf("職位「%s」的樣板預設用角色 %s，但現在不能用（%s）；這次加 --persona <人設> 指定別的角色", t.ID, a.Persona, err))
				}
			}
		case official:
			a.Action = "same"
		case strings.TrimRight(current, "\n") != t.Body:
			a.Action = "update"
			added, removed := lineChanges(current, t.Body)
			a.Added, a.Removed = int32(added), int32(removed)
		default:
			a.Action = "same"
		}
		out.Agents = append(out.Agents, a)
	}
	have := map[string]map[string]bool{}
	for _, j := range jobs {
		if have[j.Agent] == nil {
			ids, err := host.JobIDs(ctx, j.Agent)
			if err != nil {
				return nil, err
			}
			have[j.Agent] = map[string]bool{}
			for _, id := range ids {
				have[j.Agent][id] = true
			}
		}
		action := "create"
		if have[j.Agent][j.ID] {
			action = "exists"
		}
		out.Jobs = append(out.Jobs, sdk.SyncJob{Agent: j.Agent, ID: strings.TrimPrefix(j.ID, j.Agent+"/"), Action: action})
	}
	if !req.Apply {
		return out, nil
	}

	for _, a := range out.Agents {
		if a.Action == "create" && a.Persona == "" {
			return nil, platform.Invalid("persona", "要新增秘書，但還沒有預設人設：請先指定人設（evergenie config set defaults.persona <人設>，或這次加 --persona <人設>），或自己用 evergenie agents new 建好這位秘書再同步")
		}
	}
	for i, a := range out.Agents {
		switch a.Action {
		case "create":
			if err := createAgent(ctx, host, templates[i], a.Persona, req.Knowledge, who); err != nil {
				return nil, err
			}
			if !official { // 官方的不抄進家裡：建秘書時名冊已經記了 duties_from
				if err := host.WriteDuties(ctx, a.ID, templates[i].Body, "system"); err != nil {
					return nil, err
				}
			}
		case "update":
			if err := host.WriteDuties(ctx, a.ID, templates[i].Body, "system"); err != nil {
				return nil, err
			}
		}
	}
	for i, sj := range out.Jobs {
		if sj.Action == "create" {
			if err := host.SaveJob(ctx, jobs[i], who); err != nil {
				return nil, err
			}
		}
	}
	out.Applied = true
	m.mu.Lock()
	if m.log != nil {
		m.log.Info("套用套件的職責與排程", "id", req.ID, "agents", len(out.Agents), "jobs", len(out.Jobs), "by", who)
	}
	m.mu.Unlock()
	return out, nil
}

// pickPersona 決定新秘書用的角色：主人這次指定的 > 樣板預設的 > home.toml 的預設人設。回角色與來源（request｜template｜home；都沒有回空）。
func pickPersona(requested, template, home string) (string, string) {
	switch {
	case strings.TrimSpace(requested) != "":
		return strings.TrimSpace(requested), "request"
	case template != "":
		return template, "template"
	case home != "":
		return home, "home"
	}
	return "", ""
}

// lineChanges 算兩段文字逐行差異的增刪行數（LCS）；太大時算全部換掉。
func lineChanges(oldText, newText string) (added, removed int) {
	x, y := splitLines([]byte(oldText)), splitLines([]byte(newText))
	if len(x)*len(y) > diffMaxCells {
		return len(y), len(x)
	}
	prev := make([]int, len(y)+1)
	cur := make([]int, len(y)+1)
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				cur[j] = prev[j+1] + 1
			} else {
				cur[j] = max(prev[j], cur[j+1])
			}
		}
		prev, cur = cur, prev
	}
	common := prev[0]
	return len(y) - common, len(x) - common
}

// agentDuties 讀秘書現在的職責；封存過的 id 不能重用（2026-11-01 從 cmd/evergenie/packagesync.go 搬來的規則）。
func agentDuties(ctx context.Context, host SyncHost, id string) (string, bool, error) {
	body, exists, archived, err := host.AgentDuties(ctx, id)
	if err != nil {
		return "", false, err
	}
	if archived {
		return "", false, platform.Conflict(fmt.Sprintf("秘書「%s」封存過，id 不能重用；請換一個職位 id，或先把它還原", id))
	}
	return body, exists, nil
}

// createAgent 照樣板建新秘書：人設空的就用預設人設（都沒有就擋），建好再套上樣板的 color、color_dark、mode
// （2026-11-01 從 cmd/evergenie/packagesync.go 搬來的規則）。
func createAgent(ctx context.Context, host SyncHost, t AgentTemplate, persona string, knowledge []string, by string) error {
	if persona == "" {
		persona = host.DefaultPersona()
	}
	if persona == "" {
		return platform.Invalid("persona", "還沒有預設人設：請先指定人設（evergenie config set defaults.persona <人設>）")
	}
	if err := host.CreateAgent(ctx, t.ID, persona, t.Duty, knowledge, by); err != nil {
		return err
	}
	for _, f := range []struct{ field, value string }{{"color", t.Color}, {"color_dark", t.ColorDark}, {"mode", t.Mode}} {
		if f.value == "" {
			continue
		}
		if err := host.SetAgentField(ctx, t.ID, f.field, f.value, by); err != nil {
			return err
		}
	}
	return nil
}
