package requests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// ruleActions 是可以開成固定規則的動作；send_external、spend、settings 這類「一律問你」的動作不能固定放行，只能用 guidance 說明或每次問主人。
var ruleActions = map[sdk.ActionClass]string{
	sdk.ActionClassRead:   "讀取",
	sdk.ActionClassWrite:  "修改檔案",
	sdk.ActionClassCommit: "做 git commit",
	sdk.ActionClassPush:   "做 git push",
	sdk.ActionClassDelete: "刪除檔案",
}

// cleanScope 檢查並整理範圍：空＝任何地方；否則要是絕對路徑或 ~/ 開頭，不可含 ..。
func cleanScope(scope string) (string, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "", nil
	}
	if scope != "~" && !strings.HasPrefix(scope, "/") && !strings.HasPrefix(scope, "~/") {
		return "", platform.Invalid("scope", "範圍要寫成絕對路徑或 ~/ 開頭的資料夾")
	}
	for _, part := range strings.Split(scope, "/") {
		if part == ".." {
			return "", platform.Invalid("scope", "範圍不能含 ..")
		}
	}
	clean := path.Clean(scope)
	if strings.ContainsAny(clean, "*?[]{}\n") {
		return "", platform.Invalid("scope", "範圍只能是資料夾，不能用萬用字元")
	}
	return clean, nil
}

// broadScope：沒寫範圍，或資料夾太淺（從根算不到四層：根目錄、/Users、家目錄、~/Workspace 這種放所有專案的地方）算過寬。
func broadScope(scope string) bool {
	if scope == "" {
		return true
	}
	segs := 0
	if scope == "~" || strings.HasPrefix(scope, "~/") {
		segs = 2 // ~ 本身相當於 /Users/<名字>
		for _, p := range strings.Split(strings.TrimPrefix(strings.TrimPrefix(scope, "~"), "/"), "/") {
			if p != "" {
				segs++
			}
		}
	} else {
		for _, p := range strings.Split(scope, "/") {
			if p != "" {
				segs++
			}
		}
	}
	return segs < 4
}

func scopeText(scope string) string {
	if scope == "" {
		return "任何地方"
	}
	return scope
}

// nameOf：秘書 id 換成她現在對外的名字（NTF-61）；主人、查不到、沒接 Names 的照寫原字。
func (m *Module) nameOf(ctx context.Context, id string) string {
	if m.d.Names == nil || id == "" || platform.IsOwner(id) {
		return id
	}
	if name, err := m.d.Names.PersonaName(ctx, id); err == nil && strings.TrimSpace(name) != "" {
		return name
	}
	return id
}

// impactOf 算影響預覽（給主人看的白話）：誰多了什麼。秘書寫名字（NTF-61）。
func (m *Module) impactOf(ctx context.Context, q *sdk.PolicyRequest) string {
	switch q.Kind {
	case sdk.PolicyKindGuidance:
		return fmt.Sprintf("這句話會加進帳號的自動判斷規則：「%s」。使用同一個帳號的所有秘書都適用，不只是 %s；對話要重開才讀到。", q.Text, m.nameOf(ctx, q.From))
	}
	s := fmt.Sprintf("%s 之後新開（或重開）的對話，可以直接%s：%s，平台不會每次再問。已經在跑的對話要重開才生效。", m.nameOf(ctx, q.Agent), ruleActions[q.Action], scopeText(q.Scope))
	if q.Broad {
		s = "⚠ 範圍很寬。" + s
	}
	return s
}

// fingerprint 是預覽指紋：申請內容（含版本）與「有效規則的世代」。畫面上看到的和核准時的不一樣，就要求重看。
func fingerprint(rev string, q *sdk.PolicyRequest, gen int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%d", q.ID, rev, q.Status, q.Agent, q.Action, q.Scope, q.Text, gen)))
	return hex.EncodeToString(sum[:])[:12]
}

func (m *Module) agentExists(ctx context.Context, id string) error {
	r, err := m.d.Vault.Get(ctx, "agent", id)
	if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
		return platform.Invalid("agent", "找不到秘書 "+id)
	}
	if err != nil {
		return err
	}
	if r.Archived {
		return platform.Invalid("agent", "秘書 "+id+" 已經封存")
	}
	return nil
}

// Request 送一張申請。只寫申請與發通知，不碰任何設定。
func (m *Module) Request(ctx context.Context, in *sdk.PolicyRequestRequest, by string) (*sdk.PolicyRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	if by == "" {
		return nil, platform.Invalid("by", "請提供操作者")
	}
	why := strings.TrimSpace(in.Why)
	if why == "" {
		return nil, platform.Invalid("why", "請寫被擋住的情境（為什麼需要這個）")
	}
	q := &sdk.PolicyRequest{From: by, Kind: in.Kind, Why: why, Risk: strings.TrimSpace(in.Risk), Status: sdk.PolicyStatusPending, CreatedAt: m.d.Clock.Now().In(m.loc)}
	switch in.Kind {
	case sdk.PolicyKindRule:
		if in.Text != "" {
			return nil, platform.Invalid("text", "rule 不用寫 text；一句話的申請請用 --kind guidance")
		}
		if _, ok := ruleActions[in.Action]; !ok {
			return nil, platform.Invalid("action", "固定規則只能是 read、write、commit、push、delete；send_external、spend、settings 請改用 guidance 或每次問主人")
		}
		scope, err := cleanScope(in.Scope)
		if err != nil {
			return nil, err
		}
		q.Action, q.Scope, q.Agent = in.Action, scope, strings.TrimSpace(in.Agent)
		if q.Agent == "" && !platform.IsOwner(by) {
			q.Agent = by
		}
		if q.Agent == "" {
			return nil, platform.Invalid("agent", "請指定這條規則給哪位秘書")
		}
		if err := m.agentExists(ctx, q.Agent); err != nil {
			return nil, err
		}
		q.Broad = broadScope(scope)
	case sdk.PolicyKindGuidance:
		text := strings.TrimSpace(in.Text)
		if text == "" {
			return nil, platform.Invalid("text", "請寫一句話，告訴平台這類操作是主人要的")
		}
		if strings.ContainsAny(text, "\r\n") || utf8.RuneCountInString(text) > MaxGuidanceRunes {
			return nil, platform.Invalid("text", fmt.Sprintf("guidance 要寫成一行、不超過 %d 個字", MaxGuidanceRunes))
		}
		if in.Action != "" || in.Scope != "" || in.Agent != "" {
			return nil, platform.Invalid("kind", "guidance 不用寫 action、scope、agent")
		}
		q.Text = text
	default:
		return nil, platform.Invalid("kind", "kind 要是 rule 或 guidance")
	}
	pending, err := m.list(ctx, []sdk.PolicyStatus{sdk.PolicyStatusPending})
	if err != nil {
		return nil, err
	}
	n := 0
	for _, p := range pending {
		if p.From == by {
			n++
		}
	}
	if n >= MaxPendingPerSender {
		return nil, platform.Conflict(fmt.Sprintf("你已經有 %d 張待審的申請，等主人審完再送", n))
	}
	q.Impact = m.impactOf(ctx, q)
	if q.ID, err = m.d.Vault.NextID(ctx, kind); err != nil {
		return nil, err
	}
	// 通知掛在 system 底下：答案由本模組處理（handleAnswer），不轉給申請的秘書；結果另外用 echo 送回。
	title := fmt.Sprintf("權限申請 %s：%s 送的，%s", q.ID, m.nameOf(ctx, q.From), m.titleOf(ctx, q))
	if m.d.Notify != nil {
		ctxText := q.Impact + "\n\n被擋的情境：" + q.Why
		if q.Risk != "" {
			ctxText += "\n申請人說的風險：" + q.Risk
		}
		n, err := m.d.Notify.Send(ctx, &sdk.Notification{Category: sdk.CategoryApprove, Title: title, Context: ctxText, Todo: "請審核這張權限申請", Options: []string{"核准", "拒絕", "晚點再看"}, Agent: "system", By: "system", Source: sdk.NotifySourcePolicy})
		if err != nil {
			return nil, err
		}
		if n != nil {
			q.Notice = n.ID
		}
	}
	saved, err := m.d.Vault.Put(ctx, encode(vault.Record{}, q), by, "")
	if err != nil {
		return nil, err
	}
	out := decode(saved)
	out.Fingerprint = fingerprint(saved.Revision, out, m.gen)
	m.publish(ctx, out, by)
	return out, nil
}

func (m *Module) titleOf(ctx context.Context, q *sdk.PolicyRequest) string {
	if q.Kind == sdk.PolicyKindGuidance {
		return "一句話：" + q.Text
	}
	return fmt.Sprintf("%s 想要%s（%s）", m.nameOf(ctx, q.Agent), ruleActions[q.Action], scopeText(q.Scope))
}

// list 讀全部申請（新的在前）；呼叫端持有 m.mu。
func (m *Module) list(ctx context.Context, statuses []sdk.PolicyStatus) ([]sdk.PolicyRequest, error) {
	rs, err := m.d.Vault.List(ctx, kind, vault.Query{})
	if err != nil {
		return nil, err
	}
	var out []sdk.PolicyRequest
	for _, r := range rs {
		q := decode(r)
		if len(statuses) > 0 {
			ok := false
			for _, s := range statuses {
				ok = ok || q.Status == s
			}
			if !ok {
				continue
			}
		}
		q.Fingerprint = fingerprint(r.Revision, q, m.gen)
		out = append(out, *q)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

func (m *Module) List(ctx context.Context, statuses []sdk.PolicyStatus, agent string) ([]sdk.PolicyRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	for _, s := range statuses {
		switch s {
		case sdk.PolicyStatusPending, sdk.PolicyStatusApplied, sdk.PolicyStatusRejected, sdk.PolicyStatusFailed, sdk.PolicyStatusRevoked, sdk.PolicyStatusExpired:
		default:
			return nil, platform.Invalid("statuses", "狀態不正確")
		}
	}
	all, err := m.list(ctx, statuses)
	if err != nil || agent == "" {
		return all, err
	}
	var out []sdk.PolicyRequest
	for _, q := range all {
		if q.From == agent || q.Agent == agent {
			out = append(out, q)
		}
	}
	return out, nil
}

func (m *Module) Get(ctx context.Context, id string) (*sdk.PolicyRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	_, q, err := m.load(ctx, id)
	return q, err
}

func (m *Module) load(ctx context.Context, id string) (vault.Record, *sdk.PolicyRequest, error) {
	r, err := m.d.Vault.Get(ctx, kind, id)
	if err != nil {
		return r, nil, err
	}
	q := decode(r)
	q.Fingerprint = fingerprint(r.Revision, q, m.gen)
	return r, q, nil
}
