package notify

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

// Spec 回傳通知的資料種類宣告，給遷移工具直接註冊用（只新增匯出，模組行為不變）。
func Spec() vault.Spec { return messageSpec() }

func messageSpec() vault.Spec {
	sc := vault.Spec{Kind: "message", Fields: []vault.Field{
		{Name: "schema", Type: vault.Int, Required: true}, {Name: "id", Type: vault.String, Required: true},
		{Name: "created_at", Type: vault.DateTime, Required: true}, {Name: "updated_at", Type: vault.DateTime, Required: true}, {Name: "by", Type: vault.String, Required: true},
		{Name: "source", Type: vault.String}, {Name: "category", Type: vault.Enum, Required: true, Enum: categories},
		{Name: "title", Type: vault.String, Required: true}, {Name: "context", Type: vault.String},
		{Name: "todo", Type: vault.String, RequiredIf: func(r vault.Record) bool { return field(r, "category") == "approve" }},
		{Name: "options", Type: vault.List}, {Name: "url", Type: vault.String}, {Name: "image", Type: vault.String},
		{Name: "agent", Type: vault.String, Required: true}, {Name: "account", Type: vault.String}, {Name: "room", Type: vault.String},
		{Name: "task", Type: vault.Link, LinkKind: "task"}, {Name: "reminder", Type: vault.Link, LinkKind: "reminder"},
		{Name: "urgent", Type: vault.Bool}, {Name: "inline", Type: vault.Bool}, {Name: "typed", Type: vault.Bool}, {Name: "claim", Type: vault.String},
		{Name: "speaker", Type: vault.String}, {Name: "link", Type: vault.Map},
		{Name: "lane", Type: vault.Enum, Enum: []string{sdk.NotifyLaneAgent, sdk.NotifyLaneSystem}}, {Name: "delivery", Type: vault.List, Required: true},
		{Name: "state", Type: vault.Enum, Required: true, Enum: []string{"unread", "read", "answered", "expired", "failed"}},
		{Name: "answer", Type: vault.Map},
	}}
	sc.ClosedFunc = func(r vault.Record) (bool, time.Time) {
		if field(r, "state") == "answered" || field(r, "state") == "expired" {
			return true, r.UpdatedAt
		}
		return false, time.Time{}
	}
	sc.ArchiveAt = func(r vault.Record, loc *time.Location) (time.Time, bool) {
		if closed, at := sc.Closed(r); closed {
			return at.In(loc).AddDate(0, 0, 30), true
		}
		if field(r, "state") == "read" && (field(r, "category") == "digest" || field(r, "category") == "done") {
			return r.UpdatedAt.In(loc).AddDate(0, 0, 14), true
		}
		return time.Time{}, false
	}
	return sc
}
func decodeNotice(r vault.Record) (Notice, error) {
	b, err := json.Marshal(r.Fields)
	if err != nil {
		return nil, platform.Invalid("message", "通知資料無法讀取")
	}
	var n sdk.Notification
	if err := json.Unmarshal(b, &n); err != nil {
		return nil, platform.Invalid("message", "通知資料格式不正確")
	}
	n.ID = r.ID
	n.Body = r.Body
	n.Task = strings.TrimSuffix(strings.TrimPrefix(n.Task, "[["), "]]")
	n.Reminder = strings.TrimSuffix(strings.TrimPrefix(n.Reminder, "[["), "]]")
	return &n, nil
}
func cloneNotice(n Notice) Notice {
	if n == nil {
		return nil
	}
	out := *n
	out.Options = append([]string(nil), n.Options...)
	out.Delivery = append([]sdk.Delivery(nil), n.Delivery...)
	if n.Answer != nil {
		a := *n.Answer
		out.Answer = &a
	}
	if n.Link != nil {
		l := *n.Link
		out.Link = &l
	}
	return &out
}
func (m *Module) get(ctx context.Context, id string) (Notice, error) {
	r, err := m.d.Vault.Get(ctx, "message", id)
	if err != nil {
		return nil, err
	}
	m.records[id] = r
	return m.decode(r)
}

// decode 讀出一筆並補上 lane（舊檔照 laneOf 補，NTF-56）與 badge（NTF-57）。呼叫者持有 m.mu。
func (m *Module) decode(r vault.Record) (Notice, error) {
	n, err := decodeNotice(r)
	if err != nil {
		return nil, err
	}
	m.decorate(n)
	return n, nil
}
func (m *Module) save(ctx context.Context, n Notice, by string) (Notice, error) {
	// Use the revision read with the notice; never overwrite a later external edit.
	r, exists := m.records[n.ID]
	if !exists {
		r = vault.Record{Kind: "message", ID: n.ID, Fields: map[string]any{}}
	}
	fields := make(map[string]any, len(r.Fields))
	for k, v := range r.Fields {
		fields[k] = v
	}
	r.Fields = fields
	b, err := json.Marshal(n)
	if err != nil {
		return nil, platform.Internal("通知無法編碼")
	}
	var known map[string]any
	if err := json.Unmarshal(b, &known); err != nil {
		return nil, platform.Internal("通知無法編碼")
	}
	delete(known, "body")
	delete(known, "badge")           // 讀出來才算的（decorate），不存
	delete(known, "delivers_answer") // 同上（NTF-59）
	for _, f := range messageSpec().Fields {
		if f.Name != "schema" && f.Name != "updated_at" {
			delete(r.Fields, f.Name)
		}
	}
	for k, v := range known {
		r.Fields[k] = v
	}
	// 舊系統搬來的通知 delivery 是空的（JSON 省略）；照樣寫成空清單，不然必填檢查擋下回答與已讀（NTF-42）。
	if _, ok := r.Fields["delivery"]; !ok {
		r.Fields["delivery"] = []any{}
	}
	for _, k := range []string{"task", "reminder"} {
		if v, ok := r.Fields[k].(string); ok && v != "" {
			r.Fields[k] = "[[" + v + "]]"
		}
	}
	r.Body = n.Body
	saved, err := m.d.Vault.Put(ctx, r, by, r.Revision)
	if err != nil {
		return nil, err
	}
	m.records[n.ID] = saved
	return m.decode(saved)
}
func (m *Module) list(ctx context.Context, states []sdk.NotificationState, agent string, archived bool) ([]sdk.Notification, error) {
	records, err := m.d.Vault.List(ctx, "message", vault.Query{IncludeArchived: archived})
	if err != nil {
		return nil, err
	}
	out := []sdk.Notification{}
	for _, r := range records {
		m.records[r.ID] = r
		n, err := m.decode(r)
		if err != nil {
			return nil, err
		}
		if agent != "" && n.Agent != agent {
			continue
		}
		match := len(states) == 0
		for _, s := range states {
			if n.State == s {
				match = true
			}
		}
		if match {
			out = append(out, *n)
		}
	}
	return out, nil
}
func (m *Module) Get(ctx context.Context, id string) (Notice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	return m.get(ctx, id)
}
func (m *Module) List(ctx context.Context, states []sdk.NotificationState, agent string, archived bool) ([]sdk.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	return m.list(ctx, states, agent, archived)
}
func (m *Module) Unread() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.d.Vault == nil {
		return 0
	}
	n, err := m.unreadCount(context.Background())
	if err != nil {
		m.warn("無法讀取未讀數")
		return 0
	}
	return n
}
