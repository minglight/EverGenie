package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/timer"
	"github.com/minglight/evergenie/internal/base/vault"
)

// statuses 是任務單的六種狀態（2026-11-01 從九種收成六種）：待做（值沿用 idea）、進行中、等你、等外部、完成、不做了。
// 交付等主人驗收是「等你」且 proposed 有值（原本的 review）。
var statuses = []string{"idea", "doing", "waiting_user", "waiting_external", "done", "cancelled"}

// legacyStatuses 是舊的三種狀態：vault 照樣收（舊檔、封存區的單在家的遷移之前也讀得到），
// 讀進來由 upgradeLegacy 轉成新狀態；API 不收也不回這些值。
var legacyStatuses = []string{"review", "failed", "shelved"}

func text(r vault.Record, k string) string { s, _ := r.Fields[k].(string); return s }
func stamp(r vault.Record, k string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, text(r, k))
	return t
}
func ended(s string) bool   { return s == "done" || s == "cancelled" || s == "failed" }
func waiting(s string) bool { return s == "waiting_user" || s == "review" }

// delivered：交付等主人驗收（等你＋proposed 有值；舊資料的 review 也算）。
func delivered(r vault.Record) bool {
	s := text(r, "status")
	return s == "review" || s == "waiting_user" && text(r, "proposed") != ""
}
func oneOf(s string, vs ...string) bool {
	for _, v := range vs {
		if s == v {
			return true
		}
	}
	return false
}

// Spec 回傳任務單的資料種類宣告，給遷移工具直接註冊用（只新增匯出，模組行為不變）。
func Spec() vault.Spec { return taskSpec() }

func taskSpec() vault.Spec {
	statusIs := func(vs ...string) func(vault.Record) bool {
		return func(r vault.Record) bool { return oneOf(text(r, "status"), vs...) }
	}
	// 等你決定要寫要主人做什麼（ask）；交付等驗收的 ask 由 Submit 補上（舊版讀得懂），沒有也不算錯。
	asking := func(r vault.Record) bool { return text(r, "status") == "waiting_user" && text(r, "proposed") == "" }
	verified := func(r vault.Record) bool {
		return text(r, "type") == "task" && (delivered(r) || text(r, "status") == "done")
	}
	accepted := func(r vault.Record) bool { return text(r, "type") == "task" && ended(text(r, "status")) }
	resulted := func(r vault.Record) bool {
		return delivered(r) || oneOf(text(r, "status"), "done", "failed", "shelved")
	}
	sc := vault.Spec{Kind: "task", Path: func(id string) string { return "vault/tasks/" + id + ".md" }, Fields: []vault.Field{
		{Name: "title", Type: vault.String, Required: true}, {Name: "type", Type: vault.Enum, Required: true, Default: "task", Enum: []string{"task", "ask"}}, {Name: "agent", Type: vault.String}, {Name: "status", Type: vault.Enum, Required: true, Default: "doing", Enum: append(append([]string{}, statuses...), legacyStatuses...)}, {Name: "ask", Type: vault.String, RequiredIf: asking}, {Name: "waiting_since", Type: vault.DateTime, RequiredIf: statusIs("waiting_user", "review")}, {Name: "nudges", Type: vault.Int, Required: true, Default: 0}, {Name: "last_nudged_at", Type: vault.DateTime}, {Name: "result", Type: vault.String, RequiredIf: resulted}, {Name: "verify", Type: vault.Enum, Enum: []string{"verified", "unverifiable"}, RequiredIf: verified}, {Name: "verify_how", Type: vault.String, RequiredIf: verified}, {Name: "proposed", Type: vault.Enum, Enum: []string{"done", "cancelled", "failed"}, RequiredIf: statusIs("review")}, {Name: "review_note", Type: vault.String}, {Name: "accepted_by", Type: vault.String, RequiredIf: accepted}, {Name: "accepted_at", Type: vault.DateTime, RequiredIf: accepted}, {Name: "closed_at", Type: vault.DateTime, RequiredIf: statusIs("done", "failed", "cancelled")}, {Name: "links", Type: vault.List, Default: []string{}}, {Name: "attachments", Type: vault.List}, {Name: "goal", Type: vault.Link, LinkKind: "goal"}, {Name: "engine_session", Type: vault.String},
		// 驗收條件（法律第 1.4 條，2026-11-01）：送驗收、結單前由 service 要求；之前的舊單沒有。
		{Name: "criteria", Type: vault.String}, {Name: "criteria_owner", Type: vault.Bool, Default: false}}}
	sc.ClosedFunc = func(r vault.Record) (bool, time.Time) {
		s := text(r, "status")
		if ended(s) {
			return true, stamp(r, "closed_at")
		}
		if s == "shelved" { // 舊資料（家的遷移之前）
			return true, r.UpdatedAt
		}
		return false, time.Time{}
	}
	sc.ArchiveAt = func(r vault.Record, loc *time.Location) (time.Time, bool) {
		if text(r, "status") == "shelved" { // 舊資料（家的遷移之前）
			return r.UpdatedAt.AddDate(0, 0, 30), true
		}
		if ended(text(r, "status")) {
			at := stamp(r, "closed_at").In(loc)
			return time.Date(at.Year(), at.Month(), at.Day()+1, 0, 0, 0, 0, loc), true
		}
		return time.Time{}, false
	}
	return sc
}
func (m *Module) Start(ctx context.Context) error {
	if m.cancel != nil {
		return platform.Conflict("服務已啟動")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	m.loc = h.Location()
	h.Clock = m.d.Clock
	m.log = platform.NewLogger(h, "tasks")
	cfg, err := m.readFollow()
	if err != nil {
		return err
	}
	m.setFollow(cfg)
	m.transitions = transitionTable()
	if !m.registered {
		m.d.Vault.RegisterSchema(taskSpec())
		m.registered = true
	}
	if err := m.subscribe(ctx); err != nil {
		return err
	}
	if err := m.registerNudge(ctx, cfg.at); err != nil {
		m.cancel()
		<-m.done
		m.cancel = nil
		return err
	}
	return nil
}

// followCfg 是 settings/notify.toml 的 [tasks] 四欄（REQ-148）。
type followCfg struct {
	at                string
	stale, every, max int
}

// readFollow 讀 [tasks]；沒有檔或欄位就用預設 09:00／3／3／3。
func (m *Module) readFollow() (followCfg, error) {
	cfg := struct {
		Tasks struct {
			At    string `toml:"nudge_at"`
			Stale int    `toml:"stale_days"`
			Every int    `toml:"nudge_every_days"`
			Max   int    `toml:"nudge_max"`
		} `toml:"tasks"`
	}{}
	cfg.Tasks.At = "09:00"
	cfg.Tasks.Stale = 3
	cfg.Tasks.Every = 3
	cfg.Tasks.Max = 3
	data, err := os.ReadFile(filepath.Join(m.home, "settings/notify.toml"))
	if err != nil && !os.IsNotExist(err) {
		return followCfg{}, err
	}
	if err == nil {
		if _, err = toml.Decode(string(data), &cfg); err != nil {
			return followCfg{}, platform.Invalid("tasks", "任務提醒設定格式不正確")
		}
	}
	if cfg.Tasks.Stale <= 0 || cfg.Tasks.Every <= 0 || cfg.Tasks.Max < 0 {
		return followCfg{}, platform.Invalid("tasks", "提醒天數需大於零，次數不可小於零")
	}
	if at, err := time.Parse("15:04", cfg.Tasks.At); err != nil || at.Format("15:04") != cfg.Tasks.At {
		return followCfg{}, platform.Invalid("nudge_at", "提醒時間請用 HH:MM")
	}
	return followCfg{at: cfg.Tasks.At, stale: cfg.Tasks.Stale, every: cfg.Tasks.Every, max: cfg.Tasks.Max}, nil
}
func (m *Module) setFollow(c followCfg) {
	m.fmu.Lock()
	m.staleDays, m.nudgeDays, m.nudgeMax, m.nudgeAt = c.stale, c.every, c.max, c.at
	m.fmu.Unlock()
}
func (m *Module) registerNudge(ctx context.Context, at string) error {
	t, _ := time.Parse("15:04", at)
	return m.d.Scheduler.Cron(ctx, "tasks.nudge", []string{fmt.Sprintf("%d %d * * *", t.Minute(), t.Hour())}, timer.CronOpt{})
}

// loadFollow 每次跑跟進檢查前重讀 [tasks]，看板改了不用重開就生效；
// 讀到壞檔就沿用上一次的值並寫一筆 WARN（不帶內容）。檢查時間變了就重登排程。
func (m *Module) loadFollow(ctx context.Context) followCfg {
	cfg, err := m.readFollow()
	if err != nil {
		if m.log != nil {
			m.log.Warn("任務跟進設定讀取失敗，沿用上一次的值")
		}
		m.fmu.Lock()
		defer m.fmu.Unlock()
		return followCfg{at: m.nudgeAt, stale: m.staleDays, every: m.nudgeDays, max: m.nudgeMax}
	}
	m.fmu.Lock()
	changed := cfg.at != m.nudgeAt
	m.fmu.Unlock()
	m.setFollow(cfg)
	if changed && ctx.Err() == nil {
		if err := m.registerNudge(ctx, cfg.at); err != nil && m.log != nil {
			m.log.Warn("任務跟進排程重登失敗")
		}
	}
	return cfg
}
