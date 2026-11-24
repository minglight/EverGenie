package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

var categories = []string{"approve", "done", "due", "digest", "opening"}

func validCategory(c sdk.Category) bool {
	for _, x := range categories {
		if string(c) == x {
			return true
		}
	}
	return false
}
func clonePrefs(p Prefs) Prefs {
	if p == nil {
		return nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	var out sdk.NotifyPrefs
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return &out
}

// presetModes 是三個預設組每個分類的送法（notify-prefs.md 的預設表），順序同 categories。
var presetModes = map[string][]sdk.DeliveryMode{
	"quiet":    {"instant", "board", "instant", "board", "off"},
	"standard": {"instant", "instant", "instant", "digest", "board"},
	"active":   {"instant", "instant", "instant", "instant", "instant"},
}

// presetTable 把預設表攤成 預設組 → 分類 → 送法，給看板畫簡單模式的三張卡（跟核心用同一份表，不在前端重算）。
func presetTable() map[string]map[string]*sdk.CategoryPref {
	out := map[string]map[string]*sdk.CategoryPref{}
	for name, modes := range presetModes {
		row := map[string]*sdk.CategoryPref{}
		for i, c := range categories {
			row[c] = &sdk.CategoryPref{Mode: modes[i], DigestHours: 3}
		}
		out[name] = row
	}
	return out
}

// withPresets 在回給呼叫者的偏好上附上預設表（只在回應裡；不存檔、不進 m.prefs）。
func withPresets(p Prefs) Prefs {
	if p != nil {
		p.Presets = presetTable()
	}
	return p
}

// ExpandPreset 把預設組展開成明確的 [categories.*]（簡單模式切到完整模式時，notify-prefs.md「切換時展開」）：
// 回傳的偏好每個分類都有明確的送法與摘要間隔，之後不再依賴預設表的變化。純函式，不動傳入的。
func ExpandPreset(in Prefs) (Prefs, error) {
	p, err := normalizePrefs(in)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func defaults(preset string) (Prefs, error) {
	values, ok := presetModes[preset]
	if !ok {
		return nil, platform.Invalid("preset", "通知預設組不存在")
	}
	p := &sdk.NotifyPrefs{Preset: preset, ChannelApp: true, ReaskAfterMinutes: 30, ReaskMaxAsks: 3, Categories: map[string]*sdk.CategoryPref{}, Tasks: defaultTaskFollow()}
	for i, c := range categories {
		p.Categories[c] = &sdk.CategoryPref{Mode: values[i], DigestHours: 3}
	}
	return p, nil
}
func defaultTaskFollow() *sdk.TaskFollowPrefs {
	return &sdk.TaskFollowPrefs{NudgeAt: "09:00", StaleDays: 3, NudgeEveryDays: 3, NudgeMax: 3}
}

// validateTaskFollow 檢查 [tasks] 四欄（REQ-148）；欄位名用 tasks.<欄>。
func validateTaskFollow(t *sdk.TaskFollowPrefs) error {
	if t.NudgeAt != "" {
		if at, err := time.Parse("15:04", t.NudgeAt); err != nil || at.Format("15:04") != t.NudgeAt {
			return platform.Invalid("tasks.nudge_at", "檢查時間請用 HH:MM")
		}
	}
	if t.StaleDays < 1 || t.StaleDays > 30 {
		return platform.Invalid("tasks.stale_days", "停住幾天請填 1 到 30")
	}
	if t.NudgeEveryDays < 1 || t.NudgeEveryDays > 30 {
		return platform.Invalid("tasks.nudge_every_days", "間隔幾天請填 1 到 30")
	}
	if t.NudgeMax < 0 || t.NudgeMax > 10 {
		return platform.Invalid("tasks.nudge_max", "最多推幾次請填 0 到 10")
	}
	return nil
}
func normalizePrefs(input Prefs) (Prefs, error) {
	if input == nil {
		return nil, platform.Invalid("prefs", "請提供通知偏好")
	}
	p := clonePrefs(input)
	if p.Preset == "" {
		p.Preset = "standard"
	}
	base, err := defaults(p.Preset)
	if err != nil {
		return nil, err
	}
	validate := func(c string, v *sdk.CategoryPref) error {
		if !validCategory(sdk.Category(c)) || v == nil {
			return platform.Invalid("categories", "通知分類不正確")
		}
		switch v.Mode {
		case "instant", "digest", "board", "off":
		default:
			return platform.Invalid("mode", "通知送法不正確")
		}
		if c == "approve" && v.Mode == "off" {
			return platform.Invalid("approve", "要你同意的通知不能關閉")
		}
		if v.Mode == "digest" && v.DigestHours <= 0 {
			return platform.Invalid("digest_hours", "摘要間隔必須大於零")
		}
		return nil
	}
	for c, v := range p.Categories {
		if err := validate(c, v); err != nil {
			return nil, err
		}
		base.Categories[c] = v
	}
	p.Categories = base.Categories
	seen := map[string]bool{}
	kept := p.Agents[:0:0]
	for _, a := range p.Agents {
		if a.Agent == "" || seen[a.Agent] {
			return nil, platform.Invalid("agents", "秘書名稱不可空白或重複")
		}
		seen[a.Agent] = true
		for c, v := range a.Categories {
			if err := validate(c, v); err != nil {
				return nil, err
			}
		}
		// 沒有任何另設的秘書不記（等於跟全域一樣）。
		if len(a.Categories) > 0 {
			kept = append(kept, a)
		}
	}
	p.Agents = kept
	p.Presets = nil
	if p.QuietHours != "" {
		parts := strings.Split(p.QuietHours, "-")
		if len(parts) != 2 {
			return nil, platform.Invalid("quiet_hours", "安靜時段需使用 HH:MM-HH:MM")
		}
		for _, part := range parts {
			if _, err := time.Parse("15:04", part); err != nil || len(part) != 5 {
				return nil, platform.Invalid("quiet_hours", "安靜時段需使用 HH:MM-HH:MM")
			}
		}
	}
	return p, nil
}

type prefFile struct {
	Schema   int    `toml:"schema"`
	Preset   string `toml:"preset"`
	Quiet    string `toml:"quiet_hours"`
	Channels struct {
		App  *bool `toml:"app"`
		Line *bool `toml:"line"`
	} `toml:"channels"`
	Reask struct {
		After *int32 `toml:"after_minutes"`
		Max   *int32 `toml:"max_asks"`
	} `toml:"reask"`
	Tasks struct {
		NudgeAt *string `toml:"nudge_at"`
		Stale   *int32  `toml:"stale_days"`
		Every   *int32  `toml:"nudge_every_days"`
		Max     *int32  `toml:"nudge_max"`
	} `toml:"tasks"`
	// Lanes：訊息分兩道（D-47）；只在這個檔裡改（看板不寫），SetPrefs 原樣保留。
	Lanes struct {
		SystemAgents *[]string `toml:"system_agents"`
	} `toml:"lanes"`
	Categories map[string]fileCategory `toml:"categories"`
	Agents     map[string]struct {
		Categories map[string]fileCategory `toml:"categories"`
	} `toml:"agents"`
}
type fileCategory struct {
	Mode  sdk.DeliveryMode `toml:"mode"`
	Hours int32            `toml:"digest_hours"`
}

func (m *Module) loadPrefs() error {
	data, err := os.ReadFile(filepath.Join(m.home, "settings", "notify.toml"))
	if err != nil && !os.IsNotExist(err) {
		return platform.Unavailable("無法讀取通知偏好")
	}
	cfg := prefFile{Schema: 1, Preset: "standard"}
	if err == nil {
		if _, err := toml.Decode(string(data), &cfg); err != nil {
			return platform.Invalid("prefs", "通知偏好格式不正確")
		}
	}
	if cfg.Schema != 1 {
		return platform.Invalid("schema", "通知偏好版本不支援")
	}
	p, err := defaults(cfg.Preset)
	if err != nil {
		return err
	}
	p.QuietHours = cfg.Quiet
	if cfg.Channels.App != nil {
		p.ChannelApp = *cfg.Channels.App
	}
	if cfg.Channels.Line != nil {
		p.ChannelLine = *cfg.Channels.Line
	}
	if cfg.Reask.After != nil {
		p.ReaskAfterMinutes = *cfg.Reask.After
	}
	if cfg.Reask.Max != nil {
		p.ReaskMaxAsks = *cfg.Reask.Max
	}
	if cfg.Tasks.NudgeAt != nil {
		p.Tasks.NudgeAt = *cfg.Tasks.NudgeAt
	}
	if cfg.Tasks.Stale != nil {
		p.Tasks.StaleDays = *cfg.Tasks.Stale
	}
	if cfg.Tasks.Every != nil {
		p.Tasks.NudgeEveryDays = *cfg.Tasks.Every
	}
	if cfg.Tasks.Max != nil {
		p.Tasks.NudgeMax = *cfg.Tasks.Max
	}
	for c, v := range cfg.Categories {
		p.Categories[c] = &sdk.CategoryPref{Mode: v.Mode, DigestHours: v.Hours}
	}
	for id, a := range cfg.Agents {
		ap := sdk.AgentPref{Agent: id, Categories: map[string]*sdk.CategoryPref{}}
		for c, v := range a.Categories {
			ap.Categories[c] = &sdk.CategoryPref{Mode: v.Mode, DigestHours: v.Hours}
		}
		p.Agents = append(p.Agents, ap)
	}
	sort.Slice(p.Agents, func(i, j int) bool { return p.Agents[i].Agent < p.Agents[j].Agent })
	m.systemAgents = defaultSystemAgents
	if cfg.Lanes.SystemAgents != nil {
		m.systemAgents = append([]string(nil), *cfg.Lanes.SystemAgents...)
	}
	m.prefs, err = normalizePrefs(p)
	return err
}
func (m *Module) Prefs() Prefs { m.mu.Lock(); defer m.mu.Unlock(); return clonePrefs(m.prefs) }
func (m *Module) GetPrefs(ctx context.Context) (Prefs, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return withPresets(m.Prefs()), nil
}

// SetPrefs 存偏好；沒有要展開（見 SetPrefsExpand）。
func (m *Module) SetPrefs(ctx context.Context, input Prefs, by string) (Prefs, error) {
	return m.SetPrefsExpand(ctx, input, by, false)
}

// SetPrefsExpand 存偏好。檔案裡 [categories.*] 只記「跟預設組不一樣」的分類（簡單模式選預設組就是一個都不記，
// 舊的逐類覆寫跟著清掉）；expand 為真時把五個分類都明確寫出來（簡單切到完整，之後不再依賴預設表）。
func (m *Module) SetPrefsExpand(ctx context.Context, input Prefs, by string, expand bool) (Prefs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	p, err := normalizePrefs(input)
	if err != nil {
		return nil, err
	}
	writeTasks := input.Tasks != nil
	if writeTasks {
		if err := validateTaskFollow(p.Tasks); err != nil {
			return nil, err
		}
		if p.Tasks.NudgeAt == "" {
			p.Tasks.NudgeAt = "09:00"
		}
	} else {
		// 沒帶 Tasks：[tasks] 原樣保留，回傳目前的值。
		p.Tasks = clonePrefs(m.prefs).Tasks
	}
	path := filepath.Join(m.home, "settings", "notify.toml")
	// Preserve settings owned by other modules (including tasks) and unknown fields.
	raw := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, platform.Unavailable("無法讀取通知偏好")
	}
	if err == nil {
		if _, err := toml.Decode(string(data), &raw); err != nil {
			return nil, platform.Invalid("prefs", "通知偏好格式不正確")
		}
	}
	raw["schema"] = 1
	raw["preset"] = p.Preset
	raw["quiet_hours"] = p.QuietHours
	table := func(parent map[string]any, key string) map[string]any {
		v, ok := parent[key].(map[string]any)
		if !ok {
			v = map[string]any{}
			parent[key] = v
		}
		return v
	}
	if writeTasks {
		tasks := table(raw, "tasks")
		tasks["nudge_at"] = p.Tasks.NudgeAt
		tasks["stale_days"] = p.Tasks.StaleDays
		tasks["nudge_every_days"] = p.Tasks.NudgeEveryDays
		tasks["nudge_max"] = p.Tasks.NudgeMax
	}
	channels := table(raw, "channels")
	channels["app"] = p.ChannelApp
	channels["line"] = p.ChannelLine
	reask := table(raw, "reask")
	reask["after_minutes"] = p.ReaskAfterMinutes
	reask["max_asks"] = p.ReaskMaxAsks
	setCategories := func(parent map[string]any, values map[string]*sdk.CategoryPref) {
		for c, v := range values {
			t := table(parent, c)
			t["mode"] = string(v.Mode)
			t["digest_hours"] = v.DigestHours
		}
	}
	explicit := map[string]*sdk.CategoryPref{}
	base, err := defaults(p.Preset)
	if err != nil {
		return nil, err
	}
	for c, v := range p.Categories {
		if b := base.Categories[c]; expand || b == nil || b.Mode != v.Mode || v.Mode == sdk.DeliveryModeDigest && b.DigestHours != v.DigestHours {
			explicit[c] = v
		}
	}
	delete(raw, "categories")
	if len(explicit) > 0 {
		setCategories(table(raw, "categories"), explicit)
	}
	agents := table(raw, "agents")
	for id, v := range agents {
		if a, ok := v.(map[string]any); ok {
			delete(a, "categories")
			agents[id] = a
		}
	}
	for _, a := range p.Agents {
		setCategories(table(table(agents, a.Agent), "categories"), a.Categories)
	}
	var out bytes.Buffer
	if err := toml.NewEncoder(&out).Encode(raw); err != nil {
		return nil, platform.Internal("無法編碼通知偏好")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, platform.Unavailable("無法建立通知設定目錄")
	}
	if err := platform.AtomicWrite(path, out.Bytes(), 0600); err != nil {
		return nil, platform.Unavailable("無法儲存通知偏好")
	}
	m.prefs = p
	return withPresets(clonePrefs(p)), nil
}
func (m *Module) preference(n Notice) *sdk.CategoryPref {
	p := m.prefs.Categories[string(n.Category)]
	for _, a := range m.prefs.Agents {
		if a.Agent == n.Agent {
			if v := a.Categories[string(n.Category)]; v != nil {
				p = v
			}
		}
	}
	return p
}
