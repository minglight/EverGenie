package requests

import (
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

const kind = "policy_request"

func field(r vault.Record, k string) string { s, _ := r.Fields[k].(string); return s }
func flag(r vault.Record, k string) bool    { b, _ := r.Fields[k].(bool); return b }

func timeField(r vault.Record, k string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, field(r, k))
	return t
}

func spec() vault.Spec {
	decided := func(r vault.Record) bool {
		s := field(r, "status")
		return s != "" && s != string(sdk.PolicyStatusPending)
	}
	return vault.Spec{Kind: kind, Path: func(id string) string { return "vault/policy/" + id + ".md" }, Fields: []vault.Field{
		{Name: "from", Type: vault.String, Required: true},
		{Name: "kind", Type: vault.Enum, Required: true, Enum: []string{string(sdk.PolicyKindRule), string(sdk.PolicyKindGuidance)}},
		{Name: "agent", Type: vault.String}, {Name: "action", Type: vault.String}, {Name: "scope", Type: vault.String}, {Name: "text", Type: vault.String},
		{Name: "why", Type: vault.String, Required: true}, {Name: "risk", Type: vault.String}, {Name: "impact", Type: vault.String, Required: true},
		{Name: "broad", Type: vault.Bool, Required: true, Default: false},
		{Name: "status", Type: vault.Enum, Required: true, Enum: []string{"pending", "applied", "rejected", "failed", "revoked", "expired"}},
		{Name: "permanent", Type: vault.Bool, Required: true, Default: false},
		{Name: "created_at", Type: vault.DateTime, Required: true},
		{Name: "expires_at", Type: vault.DateTime}, {Name: "decided_at", Type: vault.DateTime, RequiredIf: decided}, {Name: "decided_by", Type: vault.String, RequiredIf: decided},
		{Name: "note", Type: vault.String}, {Name: "notice", Type: vault.Link, LinkKind: "message"},
	}}
}

// decode 把一筆 vault 資料換成申請單（Fingerprint 由呼叫端依現況算）。
func decode(r vault.Record) *sdk.PolicyRequest {
	return &sdk.PolicyRequest{
		ID: r.ID, From: field(r, "from"), Kind: sdk.PolicyKind(field(r, "kind")), Agent: field(r, "agent"), Action: sdk.ActionClass(field(r, "action")),
		Scope: field(r, "scope"), Text: field(r, "text"), Why: field(r, "why"), Risk: field(r, "risk"), Impact: field(r, "impact"), Broad: flag(r, "broad"),
		Status: sdk.PolicyStatus(field(r, "status")), Permanent: flag(r, "permanent"), CreatedAt: timeField(r, "created_at"), ExpiresAt: timeField(r, "expires_at"),
		DecidedAt: timeField(r, "decided_at"), DecidedBy: field(r, "decided_by"), Note: field(r, "note"),
		Notice: strings.TrimSuffix(strings.TrimPrefix(field(r, "notice"), "[["), "]]"),
	}
}

// encode 把申請單寫回 vault 資料的欄位（沿用 r 的版本，才能做衝突檢查）。
func encode(r vault.Record, q *sdk.PolicyRequest) vault.Record {
	r.Kind, r.ID = kind, q.ID
	if r.Fields == nil {
		r.Fields = map[string]any{}
	}
	set := func(k, v string) {
		if v == "" {
			delete(r.Fields, k)
		} else {
			r.Fields[k] = v
		}
	}
	tm := func(k string, t time.Time) {
		if t.IsZero() {
			delete(r.Fields, k)
		} else {
			r.Fields[k] = t.Format(time.RFC3339Nano)
		}
	}
	r.Fields["from"], r.Fields["kind"], r.Fields["why"], r.Fields["impact"] = q.From, string(q.Kind), q.Why, q.Impact
	r.Fields["broad"], r.Fields["status"], r.Fields["permanent"] = q.Broad, string(q.Status), q.Permanent
	set("agent", q.Agent)
	set("action", string(q.Action))
	set("scope", q.Scope)
	set("text", q.Text)
	set("risk", q.Risk)
	set("decided_by", q.DecidedBy)
	set("note", q.Note)
	if q.Notice != "" {
		r.Fields["notice"] = "[[" + q.Notice + "]]"
	} else {
		delete(r.Fields, "notice")
	}
	tm("created_at", q.CreatedAt)
	tm("expires_at", q.ExpiresAt)
	tm("decided_at", q.DecidedAt)
	return r
}
