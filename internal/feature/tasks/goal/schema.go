package goal

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

func field(r vault.Record, k string) string { s, _ := r.Fields[k].(string); return s }
func goalSpec() vault.Spec {
	confirmed := func(r vault.Record) bool { s := field(r, "state"); return s == "active" || s == "done" }
	ended := func(r vault.Record) bool { s := field(r, "state"); return s == "done" || s == "withdrawn" }
	sc := vault.Spec{Kind: "goal", Fields: []vault.Field{
		{Name: "title", Type: vault.String, Required: true}, {Name: "text", Type: vault.String, Required: true},
		{Name: "criteria", Type: vault.String}, // 驗收條件：新開的一定有（service 擋）；2026-11-01 之前的舊 Goal 沒有
		{Name: "agent", Type: vault.String, Required: true}, {Name: "state", Type: vault.Enum, Required: true, Enum: []string{"draft", "active", "done", "withdrawn"}},
		{Name: "notice", Type: vault.Link, LinkKind: "message", RequiredIf: func(r vault.Record) bool { return field(r, "state") == "draft" }}, // 主人自己開的 Goal 沒有確認通知（D-39）
		{Name: "confirmed_at", Type: vault.DateTime, RequiredIf: confirmed}, {Name: "confirmed_by", Type: vault.String, RequiredIf: confirmed},
		{Name: "ended_at", Type: vault.DateTime, RequiredIf: ended}, {Name: "ended_by", Type: vault.String, RequiredIf: ended},
		{Name: "summary", Type: vault.String, RequiredIf: func(r vault.Record) bool { return field(r, "state") == "done" }},
		{Name: "planbook_log", Type: vault.List, Required: true, Default: []any{}},
	}}
	sc.ClosedFunc = func(r vault.Record) (bool, time.Time) {
		if !ended(r) {
			return false, time.Time{}
		}
		at, err := time.Parse(time.RFC3339Nano, field(r, "ended_at"))
		return err == nil, at
	}
	sc.ArchiveAt = func(r vault.Record, loc *time.Location) (time.Time, bool) {
		closed, at := sc.Closed(r)
		return at.In(loc).AddDate(0, 0, 30), closed
	}
	return sc
}
func decode(r vault.Record) (Goal, error) {
	b, err := json.Marshal(r.Fields)
	if err != nil {
		return nil, platform.Invalid("goal", "Goal 資料無法讀取")
	}
	var g sdk.Goal
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, platform.Invalid("goal", "Goal 格式不正確")
	}
	g.ID = r.ID
	g.Planbook = r.Body
	g.Notice = strings.TrimSuffix(strings.TrimPrefix(g.Notice, "[["), "]]")
	return &g, nil
}

// Read 讀一個 Goal，給同塊的 task、reminder 掛 Goal 時用（D-25：只看存在與狀態）。goal 資料屬於 tasks 塊，
// 所以直接讀 vault、照 Goal 模組同一套解讀，不經 Goal 模組（S8-5 拿掉 task／reminder 的 Deps.Goals）。不存在回 vault 的 NotFound。
func Read(ctx context.Context, fromVault interface {
	Get(context.Context, string, string) (vault.Record, error)
}, id string) (Goal, error) {
	r, err := fromVault.Get(ctx, "goal", id)
	if err != nil {
		return nil, err
	}
	return decode(r)
}

// CanAttach：Goal 存在才能掛（D-25，任何狀態、不用主人確認）；不存在回 Forbidden。task 與 reminder 共用。
func CanAttach(ctx context.Context, fromVault interface {
	Get(context.Context, string, string) (vault.Record, error)
}, id string) error {
	if _, err := Read(ctx, fromVault, id); err != nil {
		if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
			return platform.Forbidden("Goal 不存在，請選擇現有的 Goal")
		}
		return err
	}
	return nil
}

func (m *Module) load(ctx context.Context, id string) (Goal, vault.Record, error) {
	r, err := m.d.Vault.Get(ctx, "goal", id)
	if err != nil {
		return nil, r, err
	}
	g, err := decode(r)
	return g, r, err
}
func (m *Module) save(ctx context.Context, g Goal, r vault.Record, by string) (Goal, error) {
	if r.Revision != "" {
		old, err := decode(r)
		if err != nil {
			return nil, err
		}
		if old.Title != g.Title || old.Text != g.Text {
			return nil, platform.Invalid("text", "Goal 原文不能改，要改請另開一個 Goal")
		}
		if old.Criteria != g.Criteria {
			return nil, platform.Invalid("criteria", "Goal 的驗收條件不能改，要改請另開一個 Goal")
		}
	}
	b, err := json.Marshal(g)
	if err != nil {
		return nil, platform.Internal("Goal 無法編碼")
	}
	values := map[string]any{}
	if err := json.Unmarshal(b, &values); err != nil {
		return nil, platform.Internal("Goal 無法編碼")
	}
	delete(values, "planbook")
	delete(values, "updated_at")
	for key, at := range map[string]time.Time{"created_at": g.CreatedAt, "confirmed_at": g.ConfirmedAt, "ended_at": g.EndedAt} {
		if at.IsZero() {
			delete(values, key)
		}
	}
	r.Kind = "goal"
	r.ID = g.ID
	if r.Fields == nil {
		r.Fields = map[string]any{}
	}
	for _, f := range goalSpec().Fields {
		delete(r.Fields, f.Name)
	}
	for k, v := range values {
		r.Fields[k] = v
	}
	if g.Notice != "" {
		r.Fields["notice"] = "[[" + g.Notice + "]]"
	}
	r.Body = g.Planbook
	saved, err := m.d.Vault.Put(ctx, r, by, r.Revision)
	if err != nil {
		return nil, err
	}
	return decode(saved)
}
