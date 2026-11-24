package requests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// ActiveRules 回傳這位秘書目前有效的規則（核心開 session 時交給引擎，REQ-168）。不取 mu：引擎可能在 Decide 套用時回頭讀。
func (m *Module) ActiveRules(agent string) []sdk.PolicyRule {
	m.am.RLock()
	defer m.am.RUnlock()
	var out []sdk.PolicyRule
	for _, q := range m.actives {
		if q.Kind == sdk.PolicyKindRule && q.Agent == agent {
			out = append(out, sdk.PolicyRule{Agent: q.Agent, Action: q.Action, Scope: q.Scope})
		}
	}
	return out
}

// ActiveGuidance 回傳目前有效的 guidance（依申請編號排序、去重），引擎把它寫進每個帳號產生的設定。
func (m *Module) ActiveGuidance() []string {
	m.am.RLock()
	defer m.am.RUnlock()
	seen := map[string]bool{}
	var out []string
	for _, q := range m.actives {
		if q.Kind == sdk.PolicyKindGuidance && q.Text != "" && !seen[q.Text] {
			seen[q.Text] = true
			out = append(out, q.Text)
		}
	}
	return out
}

// setActives 換掉套用中的清單（依編號排序，產物才穩定）並加世代；呼叫端持有 mu。
func (m *Module) setActives(list []sdk.PolicyRequest) {
	sortByID(list)
	m.am.Lock()
	m.actives = list
	m.am.Unlock()
	m.gen++
}

func (m *Module) activeList() []sdk.PolicyRequest {
	m.am.RLock()
	defer m.am.RUnlock()
	return append([]sdk.PolicyRequest(nil), m.actives...)
}

func sortByID(list []sdk.PolicyRequest) {
	num := func(id string) int {
		n := 0
		fmt.Sscanf(strings.TrimPrefix(id, "P-"), "%d", &n)
		return n
	}
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && num(list[j-1].ID) > num(list[j].ID); j-- {
			list[j-1], list[j] = list[j], list[j-1]
		}
	}
}

func without(list []sdk.PolicyRequest, id string) []sdk.PolicyRequest {
	out := make([]sdk.PolicyRequest, 0, len(list))
	for _, q := range list {
		if q.ID != id {
			out = append(out, q)
		}
	}
	return out
}

// push 把目前的 guidance 交給引擎；沒接引擎就略過。
func (m *Module) push(ctx context.Context) error {
	if m.d.Engine == nil {
		return nil
	}
	return m.d.Engine.PushPolicy(ctx)
}

// swap 把套用中的清單換成 next；有 guidance 變動就推給引擎，失敗就換回舊的並再推一次（還原），回傳原本的錯誤。
func (m *Module) swap(ctx context.Context, next []sdk.PolicyRequest, guidance bool) error {
	prev := m.activeList()
	m.setActives(next)
	if !guidance {
		return nil
	}
	err := m.push(ctx)
	if err == nil {
		return nil
	}
	m.setActives(prev)
	if rerr := m.push(ctx); rerr != nil {
		m.warn("權限還原時引擎也沒有回應")
	}
	return err
}

// within：narrow 是 wide 本身或底下的資料夾（wide 空＝任何地方）。
func within(narrow, wide string) bool {
	if wide == "" {
		return true
	}
	return narrow == wide || strings.HasPrefix(narrow, strings.TrimSuffix(wide, "/")+"/")
}

// Decide 核准或拒絕一張待審的申請。只有主人可以（by 以 owner( 開頭）；秘書回合一律 Forbidden（D-26、REQ-168）。
func (m *Module) Decide(ctx context.Context, in *sdk.PolicyDecideRequest, by string) (*sdk.PolicyRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	return m.decide(ctx, in, by, true)
}

// decide 是 Decide 的本體（呼叫端持有 mu）；checkPrint=false 給通知的按鈕答案用（通知內容就是申請當時的內容）。
func (m *Module) decide(ctx context.Context, in *sdk.PolicyDecideRequest, by string, checkPrint bool) (*sdk.PolicyRequest, error) {
	if in == nil || strings.TrimSpace(in.ID) == "" {
		return nil, platform.Invalid("id", "請指定申請編號")
	}
	if !platform.IsOwner(by) {
		return nil, platform.Forbidden("只有主人能審權限申請；秘書只能送申請")
	}
	r, q, err := m.load(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if q.Status != sdk.PolicyStatusPending {
		return nil, platform.Conflict(fmt.Sprintf("這張申請已經是「%s」，不能再審", statusText(q.Status)))
	}
	now := m.d.Clock.Now().In(m.loc)
	q.DecidedAt, q.DecidedBy, q.Note = now, by, strings.TrimSpace(in.Note)
	if !in.Approve {
		if in.Scope != "" || in.Text != "" || in.Permanent {
			return nil, platform.Invalid("approve", "拒絕不用改範圍、文字或期限")
		}
		q.Status = sdk.PolicyStatusRejected
		return m.finish(ctx, r, q, by, "主人拒絕了你的權限申請")
	}
	if checkPrint && in.Fingerprint != q.Fingerprint {
		return nil, platform.Conflict("這張申請或目前的權限在你看過之後變了，請重新看一次再核准")
	}
	if err := m.edit(ctx, q, in); err != nil {
		return nil, err
	}
	// Q7b（D-27⑤）：核准後一直有效，撤銷才失效；in.Permanent 收下但不影響結果。
	q.Permanent = true
	q.ExpiresAt = time.Time{}
	q.Status = sdk.PolicyStatusApplied
	guidance := q.Kind == sdk.PolicyKindGuidance
	if err := m.swap(ctx, append(m.activeList(), *q), guidance); err != nil {
		// 套用失敗：設定已還原，申請標成 failed（REQ-168）。
		m.warn("權限套用失敗，已還原")
		q.Status, q.ExpiresAt, q.Permanent = sdk.PolicyStatusFailed, time.Time{}, false
		q.Note = joinNote(q.Note, "套用失敗，已還原："+errText(err))
		return m.finish(ctx, r, q, by, "主人核准了你的權限申請，但套用失敗、已還原")
	}
	out, err := m.finish(ctx, r, q, by, "主人核准了你的權限申請")
	if err != nil {
		// 存不進去就不能算套用：把清單換回來。
		if rerr := m.swap(ctx, without(m.activeList(), q.ID), guidance); rerr != nil {
			m.warn("權限還原失敗")
		}
		return nil, err
	}
	return out, nil
}

// edit 套用「改後核准」：rule 只能把範圍改窄，guidance 可以換一句話；改了要重算影響與過寬。
func (m *Module) edit(ctx context.Context, q *sdk.PolicyRequest, in *sdk.PolicyDecideRequest) error {
	switch q.Kind {
	case sdk.PolicyKindRule:
		if in.Text != "" {
			return platform.Invalid("text", "rule 不能改文字，只能改範圍")
		}
		if in.Scope == "" {
			return nil
		}
		scope, err := cleanScope(in.Scope)
		if err != nil {
			return err
		}
		if !within(scope, q.Scope) {
			return platform.Invalid("scope", "改後的範圍要在原本的範圍裡面（只能改窄）")
		}
		q.Scope = scope
		q.Broad = broadScope(scope)
	case sdk.PolicyKindGuidance:
		if in.Scope != "" {
			return platform.Invalid("scope", "guidance 沒有範圍可以改")
		}
		if in.Text == "" {
			return nil
		}
		text := strings.TrimSpace(in.Text)
		if text == "" || strings.ContainsAny(text, "\r\n") || len([]rune(text)) > MaxGuidanceRunes {
			return platform.Invalid("text", fmt.Sprintf("guidance 要寫成一行、不超過 %d 個字", MaxGuidanceRunes))
		}
		q.Text = text
	}
	q.Impact = m.impactOf(ctx, q)
	return nil
}

// Revoke 撤銷一張已套用的申請，回到申請之前（只有主人）。
func (m *Module) Revoke(ctx context.Context, id, note, by string) (*sdk.PolicyRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, platform.Invalid("id", "請指定申請編號")
	}
	if !platform.IsOwner(by) {
		return nil, platform.Forbidden("只有主人能撤銷權限；秘書只能送申請")
	}
	r, q, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if q.Status != sdk.PolicyStatusApplied {
		return nil, platform.Conflict(fmt.Sprintf("這張申請是「%s」，只有已套用的能撤銷", statusText(q.Status)))
	}
	guidance := q.Kind == sdk.PolicyKindGuidance
	if err := m.swap(ctx, without(m.activeList(), q.ID), guidance); err != nil {
		return nil, platform.Unavailable("引擎沒有回應，權限沒有撤銷，請稍後重試")
	}
	q.Status, q.DecidedAt, q.DecidedBy = sdk.PolicyStatusRevoked, m.d.Clock.Now().In(m.loc), by
	q.Note = joinNote(q.Note, strings.TrimSpace(note))
	out, err := m.finish(ctx, r, q, by, "主人撤銷了你的權限")
	if err != nil {
		if rerr := m.swap(ctx, append(m.activeList(), *q), guidance); rerr != nil {
			m.warn("撤銷失敗後還原也失敗")
		}
		return nil, err
	}
	return out, nil
}

// finish 存檔、發事件、把結果送回申請的秘書（主人自己送的不用回）。
func (m *Module) finish(ctx context.Context, r vault.Record, q *sdk.PolicyRequest, by, headline string) (*sdk.PolicyRequest, error) {
	saved, err := m.d.Vault.Put(ctx, encode(r, q), by, r.Revision)
	if err != nil {
		return nil, err
	}
	out := decode(saved)
	out.Fingerprint = fingerprint(saved.Revision, out, m.gen)
	m.publish(ctx, out, by)
	if out.From != "" && !platform.IsOwner(out.From) && m.d.Bus != nil {
		body := fmt.Sprintf("%s：%s（%s）。%s", headline, out.ID, m.titleOf(ctx, out), resultText(out))
		if _, err := m.d.Bus.Send(ctx, &sdk.Envelope{Kind: sdk.EnvelopeKindEcho, Route: sdk.EnvelopeRouteCore, From: "system", To: out.From, Body: body, IdempotencyKey: "policy:" + out.ID + ":" + string(out.Status)}); err != nil {
			m.warn("權限申請的結果送不回申請的秘書")
		}
	}
	return out, nil
}

func resultText(q *sdk.PolicyRequest) string {
	switch q.Status {
	case sdk.PolicyStatusApplied:
		s := "已經套用，一直有效（要收回請撤銷）"
		if q.Kind == sdk.PolicyKindRule {
			s += "；新開（或重開）的對話才會生效"
		}
		return s + "。" + noteText(q.Note)
	case sdk.PolicyStatusRejected:
		return "沒有改任何設定。" + noteText(q.Note)
	case sdk.PolicyStatusFailed:
		return "設定維持原樣。" + noteText(q.Note)
	case sdk.PolicyStatusRevoked:
		return "已經回到申請之前。" + noteText(q.Note)
	case sdk.PolicyStatusExpired:
		return "已經回到申請之前。"
	}
	return ""
}

func noteText(note string) string {
	if note == "" {
		return ""
	}
	return "備註：" + note
}

func statusText(s sdk.PolicyStatus) string {
	switch s {
	case sdk.PolicyStatusPending:
		return "待審"
	case sdk.PolicyStatusApplied:
		return "已套用"
	case sdk.PolicyStatusRejected:
		return "已拒絕"
	case sdk.PolicyStatusFailed:
		return "套用失敗"
	case sdk.PolicyStatusRevoked:
		return "已撤銷"
	case sdk.PolicyStatusExpired:
		return "已到期"
	}
	return string(s)
}

func joinNote(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "；" + b
}

func errText(err error) string {
	var pe *platform.Err
	if errors.As(err, &pe) && pe.Message != "" {
		return pe.Message
	}
	var se *sdk.Error
	if errors.As(err, &se) && se.Message != "" {
		return se.Message
	}
	return "引擎沒有回應"
}
