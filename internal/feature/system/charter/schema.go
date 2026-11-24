package charter

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 兩種資料（ai_docs/schemas/rules.md）：版本 rules_version（RV-<n>，內文是當時的全文）、
// 修改提案 rules_proposal（RP-<n>，內文是提案的新內容）。放在 vault/rules/ 底下，不靠家是不是 git repo。
const (
	versionKind  = "rules_version"
	proposalKind = "rules_proposal"
)

func field(r vault.Record, k string) string { s, _ := r.Fields[k].(string); return s }

func timeField(r vault.Record, k string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, field(r, k))
	return t
}

func versionSpec() vault.Spec {
	return vault.Spec{Kind: versionKind, IDMode: "seq", IDPrefix: "RV-", Path: func(id string) string { return "vault/rules/versions/" + id + ".md" }, Fields: []vault.Field{
		// kind：vault/rules/ 不在 vault 認資料夾的清單裡，重新掃描時靠檔頭的 kind 認種類。
		{Name: "kind", Type: vault.Enum, Required: true, Enum: []string{versionKind}},
		{Name: "doc", Type: vault.String, Required: true},
		{Name: "title", Type: vault.String, Required: true},
		{Name: "source", Type: vault.Enum, Required: true, Enum: []string{
			string(sdk.RulesVersionSourceSave), string(sdk.RulesVersionSourceRestore), string(sdk.RulesVersionSourceProposal), string(sdk.RulesVersionSourceOutside)}},
		{Name: "note", Type: vault.String},
		{Name: "proposal", Type: vault.String},
		{Name: "restored_from", Type: vault.String},
		{Name: "created_at", Type: vault.DateTime, Required: true},
	}}
}

func proposalSpec() vault.Spec {
	decided := func(r vault.Record) bool { return field(r, "status") != string(sdk.RulesProposalStatusPending) }
	return vault.Spec{Kind: proposalKind, IDMode: "seq", IDPrefix: "RP-", Path: func(id string) string { return "vault/rules/proposals/" + id + ".md" }, Fields: []vault.Field{
		{Name: "kind", Type: vault.Enum, Required: true, Enum: []string{proposalKind}},
		{Name: "doc", Type: vault.String, Required: true},
		{Name: "section", Type: vault.String},
		{Name: "from", Type: vault.String, Required: true},
		{Name: "why", Type: vault.String, Required: true},
		{Name: "status", Type: vault.Enum, Required: true, Enum: []string{
			string(sdk.RulesProposalStatusPending), string(sdk.RulesProposalStatusAccepted), string(sdk.RulesProposalStatusRejected)}},
		{Name: "base", Type: vault.String},
		{Name: "created_at", Type: vault.DateTime, Required: true},
		{Name: "decided_at", Type: vault.DateTime, RequiredIf: decided},
		{Name: "decided_by", Type: vault.String, RequiredIf: decided},
		{Name: "note", Type: vault.String},
		{Name: "notice", Type: vault.String},
		{Name: "version", Type: vault.String},
	}}
}

func decodeVersion(r vault.Record, withText bool) sdk.RulesVersion {
	v := sdk.RulesVersion{
		ID: r.ID, Doc: field(r, "doc"), Title: field(r, "title"), Source: sdk.RulesVersionSource(field(r, "source")),
		Note: field(r, "note"), By: field(r, "by"), Proposal: field(r, "proposal"), RestoredFrom: field(r, "restored_from"),
		CreatedAt: timeField(r, "created_at"),
	}
	if withText {
		v.Text = r.Body
	}
	return v
}

func decodeProposal(r vault.Record) sdk.RulesProposal {
	return sdk.RulesProposal{
		ID: r.ID, Doc: field(r, "doc"), Section: field(r, "section"), From: field(r, "from"), Why: field(r, "why"), Text: r.Body,
		Status: sdk.RulesProposalStatus(field(r, "status")), Base: field(r, "base"), CreatedAt: timeField(r, "created_at"),
		DecidedAt: timeField(r, "decided_at"), DecidedBy: field(r, "decided_by"), Note: field(r, "note"),
		Notice: field(r, "notice"), Version: field(r, "version"),
	}
}

// seqOf 是 RV-12 → 12（排序用；格式不對的排最前面）。
func seqOf(id string) int {
	_, n, _ := strings.Cut(id, "-")
	v, _ := strconv.Atoi(n)
	return v
}

// newestFirst 依編號由新到舊排。
func newestFirst(rs []vault.Record) {
	sort.Slice(rs, func(i, j int) bool { return seqOf(rs[i].ID) > seqOf(rs[j].ID) })
}
