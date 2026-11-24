package pkg

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 認領職位（SEC-302 S9、REQ-341～REQ-347）：套件建好、預演過之後，秘書用 ClaimNotify 請核心發一則「要你同意」，
// 選項「好，用 X｜換一個｜之後再說」。主人按「好」時由核心以主人身分套用：Sync{apply}＋啟用。
//
// 安全邊界（不是通用的「秘書指定 API、主人一鍵執行」）：
//   - 動作只有一種（認領職位），參數（套件、職位、角色）由核心在發通知時決定並記在 settings/packages.toml，
//     回答的文字只用來選「好／換一個／之後再說」，不會被當成參數。
//   - 只有主人的回答（notice.answered 的 by 以 owner( 開頭、不是自動回答）才動作；秘書自己回答不算。
//   - 套用前再驗一次：職位還在、還沒有人負責、角色還能指派；任何一項不成立就不動，發系統提醒請主人到設定頁那張卡。
//   - 秘書能做的只有「請核心發通知」；package.sync 的 apply 仍只有主人（PKG-12 不變）。

// ClaimHost 是認領流程需要的、別的模組的能力（組裝時接 agent 與 notify；測試用假的）。
type ClaimHost interface {
	// Personas 是角色庫（含狀態、坐在哪些職位、來源與所屬套件）。
	Personas(ctx context.Context) ([]sdk.PersonaCard, error)
	// Notify 發一則通知（source、claim 由 pkg 填好），回存好的通知（含 M-<n>）。
	Notify(ctx context.Context, n *sdk.Notification) (*sdk.Notification, error)
	// EnableAgent 讓一位秘書上班（agents set_field enabled true）。
	EnableAgent(ctx context.Context, id, by string) error
}

// SetClaimHost 接上認領流程；沒接時 ClaimNotify 回 UNAVAILABLE、回答不處理。
func (m *Module) SetClaimHost(h ClaimHost) { m.mu.Lock(); m.claim = h; m.mu.Unlock() }

// claimActor 是主人按「好」時套用用的身分（跟權限申請的 owner(notice) 同一個寫法）。
const claimActor = "owner(notice)"

// 通知的選項：第一個是「好，用 <名字>」（核心照名字組），後兩個固定。
const (
	claimChange = "換一個"
	claimLater  = "之後再說"
)

// claimRecord 是 settings/packages.toml 的 [claims.<職位 id>]：一個職位只問一次（之後再說、換一個都不再問）。
type claimRecord struct {
	Package string         `toml:"package"`
	Persona string         `toml:"persona,omitempty"`
	Notice  string         `toml:"notice,omitempty"`
	State   sdk.ClaimState `toml:"state"`
	By      string         `toml:"by,omitempty"` // 請核心發通知的是誰
	At      string         `toml:"at"`           // 最後一次改狀態的時間（RFC 3339）
	Detail  string         `toml:"detail,omitempty"`
}

// assignable：上線、沒被官方蓋住、所屬套件有效（跟看板 assignablePersonas 同一條規則，D-31）。
func assignable(c sdk.PersonaCard) bool {
	return c.Status == "live" && !c.Shadowed && c.Unassignable == ""
}

// SuggestPersona 是認領時建議的角色（跟看板 suggestPersona 同一套規則，REQ-326、REQ-342）：
// ①這個職位的套件（pkgID）自己帶的上線角色，還沒坐位置的優先 ②還沒坐任何位置的上線角色 ③其他上線角色（照清單順序）。
// 回角色代號與白話理由；沒有能指派的角色回兩個空字串。
func SuggestPersona(cards []sdk.PersonaCard, pkgID string) (slug, reason string) {
	var own, free, rest []sdk.PersonaCard
	for _, c := range cards {
		if !assignable(c) {
			continue
		}
		switch {
		case pkgID != "" && c.Source == "package" && c.Package == pkgID:
			own = append(own, c)
		case len(c.Seats) == 0:
			free = append(free, c)
		default:
			rest = append(rest, c)
		}
	}
	if len(own) > 0 {
		for _, c := range own {
			if len(c.Seats) == 0 {
				return c.Slug, "這個套件自己帶的角色"
			}
		}
		return own[0].Slug, "這個套件自己帶的角色（也在別的職位上班）"
	}
	if len(free) > 0 {
		return free[0].Slug, "已經上線、還沒有負責任何職位"
	}
	if len(rest) > 0 {
		return rest[0].Slug, "已經上線（也在別的職位上班）"
	}
	return "", ""
}

// dutyTitle 是職位的顯示名稱：職責的第一段（跟看板 functionTitle 同一條規則），沒有就用職位 id。
func dutyTitle(duty, id string) string {
	head := duty
	if i := strings.IndexAny(head, "（(：:，,"); i >= 0 {
		head = head[:i]
	}
	if head = strings.TrimSpace(head); head != "" {
		return head
	}
	return id
}

// claimTarget 是要認領的職位：套件、樣板、顯示名稱。
type claimTarget struct {
	pkg   string
	agent AgentTemplate
	title string
}

// findClaim 找要認領的職位（只看啟用、沒被拒的套件）。agent 空＝第一個名冊上還沒有、也還沒問過（asked 回 false）的職位，
// 跳過選用職位（樣板 optional: true，DEV-157：主人不主動加就永遠不問）；全部都問過就回第一個問過的（呼叫的人照它的紀錄回狀態）。
// 指名的職位（agent 不空）就算是選用的也照樣找。
func (m *Module) findClaim(ctx context.Context, pkgID, agentID string, host SyncHost, asked func(string) bool) (claimTarget, error) {
	m.mu.Lock()
	if err := m.readyLocked(); err != nil {
		m.mu.Unlock()
		return claimTarget{}, err
	}
	e := m.entries[pkgID]
	switch {
	case e == nil:
		m.mu.Unlock()
		return claimTarget{}, platform.NotFound(fmt.Sprintf("沒有套件「%s」", pkgID))
	case e.err != nil:
		m.mu.Unlock()
		return claimTarget{}, platform.Invalid("id", fmt.Sprintf("套件「%s」現在載入不了（%s），先把它修好", pkgID, e.err))
	case !e.enabled:
		m.mu.Unlock()
		return claimTarget{}, platform.Invalid("id", fmt.Sprintf("套件「%s」已停用，先啟用它", pkgID))
	}
	man, dir := e.man, e.dir
	m.mu.Unlock()
	target := func(a ContribAgent) (claimTarget, error) {
		t, err := parseAgentTemplate(filepath.Join(dir, filepath.FromSlash(a.Template)), a.ID, a.Template)
		if err != nil {
			return claimTarget{}, err
		}
		return claimTarget{pkg: pkgID, agent: t, title: dutyTitle(t.Duty, t.ID)}, nil
	}
	found := false
	var firstAsked *ContribAgent
	var optional []string // 沒指名時略過的選用職位
	for _, a := range man.Contributes.Agent {
		if agentID != "" && a.ID != agentID {
			continue
		}
		if agentID == "" {
			t, err := parseAgentTemplate(filepath.Join(dir, filepath.FromSlash(a.Template)), a.ID, a.Template)
			if err != nil {
				return claimTarget{}, err
			}
			if t.Optional {
				optional = append(optional, a.ID)
				continue
			}
		}
		found = true
		_, exists, err := agentDuties(ctx, host, a.ID)
		if err != nil {
			return claimTarget{}, err
		}
		if exists {
			if agentID != "" {
				return claimTarget{}, platform.Conflict(fmt.Sprintf("職位「%s」已經有秘書負責了，不用再問", a.ID))
			}
			continue
		}
		if agentID == "" && asked != nil && asked(a.ID) {
			if firstAsked == nil {
				firstAsked = &a
			}
			continue
		}
		return target(a)
	}
	if firstAsked != nil {
		return target(*firstAsked)
	}
	if !found && agentID != "" {
		return claimTarget{}, platform.NotFound(fmt.Sprintf("套件「%s」沒有職位「%s」", pkgID, agentID))
	}
	if !found && len(optional) > 0 {
		return claimTarget{}, platform.Conflict(fmt.Sprintf("套件「%s」只有選用職位（%s），主人沒指名就不問", pkgID, strings.Join(optional, "、")))
	}
	if !found {
		return claimTarget{}, platform.Invalid("id", fmt.Sprintf("套件「%s」沒有帶任何職位（agents/<id>/agent.md）", pkgID))
	}
	if len(optional) > 0 {
		return claimTarget{}, platform.Conflict(fmt.Sprintf("套件「%s」的一般職位都已經有秘書負責了；選用職位（%s）主人沒指名就不問", pkgID, strings.Join(optional, "、")))
	}
	return claimTarget{}, platform.Conflict(fmt.Sprintf("套件「%s」的職位都已經有秘書負責了，不用再問", pkgID))
}

func (m *Module) claimHosts() (SyncHost, ClaimHost) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sync, m.claim
}

func (m *Module) loadClaim(agentID string) (claimRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.loadSettings()
	if err != nil {
		return claimRecord{}, false, err
	}
	r, ok := st.Claims[agentID]
	return r, ok, nil
}

func (m *Module) saveClaim(agentID string, r claimRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.loadSettings()
	if err != nil {
		return err
	}
	if st.Claims == nil {
		st.Claims = map[string]claimRecord{}
	}
	r.At = m.d.Clock.Now().Format(time.RFC3339)
	st.Claims[agentID] = r
	return m.saveSettings(st)
}

// ClaimNotify 發認領通知（REQ-341、REQ-343、REQ-345）：秘書或主人都能請，但它只發通知、不建秘書。
// 同一個職位只問一次：之前問過（還沒回、之後再說、換一個、套用過）就不發，回那時的狀態。
func (m *Module) ClaimNotify(ctx context.Context, req *sdk.PackageClaimNotifyRequest) (*sdk.PackageClaimNotifyResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	who := strings.TrimSpace(actor(req.By))
	if who == "" {
		return nil, platform.Invalid("by", "請提供操作者")
	}
	sync, claim := m.claimHosts()
	if sync == nil || claim == nil {
		return nil, platform.Unavailable("認領職位的通知還沒接上")
	}
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	asked := func(id string) bool { _, ok, _ := m.loadClaim(id); return ok }
	target, err := m.findClaim(ctx, req.ID, req.Agent, sync, asked)
	if err != nil {
		return nil, err
	}
	out := &sdk.PackageClaimNotifyResponse{Package: target.pkg, Agent: target.agent.ID}
	if rec, ok, err := m.loadClaim(target.agent.ID); err != nil {
		return nil, err
	} else if ok {
		out.Persona, out.Notice, out.State = rec.Persona, rec.Notice, rec.State
		out.Detail = map[sdk.ClaimState]string{
			sdk.ClaimStateAsked:  "之前已經問過主人、他還沒回；不重複問",
			sdk.ClaimStateLater:  "主人說「之後再說」；不再問，設定→職位與秘書那張卡會一直留著",
			sdk.ClaimStateChange: "主人要自己在設定→職位與秘書那張卡挑角色；不再問",
			sdk.ClaimStateDone:   "之前已經設好了",
			sdk.ClaimStateFailed: "上次主人按了「好」但沒設成功（" + rec.Detail + "）；請主人到設定→職位與秘書那張卡重選，不再問",
		}[rec.State]
		return out, nil
	}
	cards, err := claim.Personas(ctx)
	if err != nil {
		return nil, err
	}
	persona, reason := strings.TrimSpace(req.Persona), "秘書建議的角色"
	if persona != "" {
		if err := sync.CheckPersona(persona); err != nil {
			return nil, platform.Invalid("persona", fmt.Sprintf("角色 %s 現在不能指派（%s）；不帶 --persona 讓核心挑，或先把她上線", persona, err))
		}
	} else if persona, reason = SuggestPersona(cards, target.pkg); persona == "" {
		return nil, platform.Invalid("persona", "還沒有上線的角色可以指派：先在看板的「角色」頁做一位角色並上線（evergenie personas live <代號>），再發認領通知")
	}
	name := persona
	for _, c := range cards {
		if c.Slug == persona && strings.TrimSpace(c.Name) != "" {
			name = strings.TrimSpace(c.Name)
		}
	}
	yes := "好，用 " + name
	agentLabel := who
	if platform.IsOwner(who) {
		agentLabel = "system"
	}
	n, err := claim.Notify(ctx, &sdk.Notification{
		Category: sdk.CategoryApprove,
		Source:   sdk.NotifySourcePackage,
		Claim:    target.agent.ID,
		Title:    fmt.Sprintf("職位「%s」還沒有人負責", target.title),
		Context: fmt.Sprintf("職位 `%s`（套件 `%s`）建好了，還沒有秘書負責。建議用 **%s**（`%s`）：%s。\n\n"+
			"按「%s」：核心用 %s 建好這位秘書，馬上開始上班。按「%s」：打開設定→職位與秘書那張卡自己挑。按「%s」：不會再問，那張卡一直留著。",
			target.agent.ID, target.pkg, name, persona, reason, yes, name, claimChange, claimLater),
		Todo:    fmt.Sprintf("要讓 %s 負責「%s」嗎？", name, target.title),
		Options: []string{yes, claimChange, claimLater},
		Agent:   agentLabel,
		By:      who,
	})
	if err != nil {
		return nil, err
	}
	if err := m.saveClaim(target.agent.ID, claimRecord{Package: target.pkg, Persona: persona, Notice: n.ID, State: sdk.ClaimStateAsked, By: who}); err != nil {
		return nil, err
	}
	out.Persona, out.PersonaName, out.Reason, out.Notice, out.State, out.Sent = persona, name, reason, n.ID, sdk.ClaimStateAsked, true
	m.info("發出認領職位的通知", "package", target.pkg, "agent", target.agent.ID, "persona", persona, "notice", n.ID, "by", who)
	return out, nil
}

// handleClaimAnswer 處理認領通知的回答（REQ-344、REQ-346）：只認主人、只認 pkg 自己發的、還在等回答的那則。
func (m *Module) handleClaimAnswer(ctx context.Context, payload map[string]any) error {
	str := func(k string) string { s, _ := payload[k].(string); return strings.TrimSpace(s) }
	if str("source") != sdk.NotifySourcePackage || str("via") == string(sdk.AnswerViaAuto) || str("id") == "" {
		return nil
	}
	if !platform.IsOwner(str("by")) {
		// 不是主人回答的（秘書自己按、或來路不明）：核心什麼都不做（REQ-344）。
		m.warn("認領職位的通知被非主人回答，不動作")
		return nil
	}
	sync, claim := m.claimHosts()
	if sync == nil || claim == nil {
		return nil
	}
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	agentID, rec, ok, err := m.claimByNotice(str("id"))
	if err != nil || !ok || rec.State != sdk.ClaimStateAsked {
		return err
	}
	text := str("text")
	switch {
	case text == claimLater:
		rec.State = sdk.ClaimStateLater
		return m.saveClaim(agentID, rec)
	case strings.HasPrefix(text, "好"):
	default: // 「換一個」或打了別的字：主人自己到設定頁挑；不再問。
		rec.State = sdk.ClaimStateChange
		return m.saveClaim(agentID, rec)
	}
	if err := m.applyClaim(ctx, agentID, rec, sync, claim); err != nil {
		rec.State, rec.Detail = sdk.ClaimStateFailed, err.Error()
		if m.d.Bus != nil {
			_ = m.d.Bus.Publish(ctx, "system.alert", map[string]any{
				"title":   fmt.Sprintf("職位「%s」沒有設好", agentID),
				"context": fmt.Sprintf("你按了「%s」，但沒有設成功：%s。請到設定→職位與秘書那張卡重新選角色。", text, err.Error()),
				"agent":   "",
			})
		}
		m.warn("認領職位套用失敗")
		return m.saveClaim(agentID, rec)
	}
	rec.State, rec.Detail = sdk.ClaimStateDone, ""
	m.info("主人從通知認領了職位", "package", rec.Package, "agent", agentID, "persona", rec.Persona)
	return m.saveClaim(agentID, rec)
}

// applyClaim 送出前再驗一次（職位還在、還沒人負責、角色能指派），再以主人身分 Sync＋啟用。
func (m *Module) applyClaim(ctx context.Context, agentID string, rec claimRecord, sync SyncHost, claim ClaimHost) error {
	if _, err := m.findClaim(ctx, rec.Package, agentID, sync, nil); err != nil {
		return err
	}
	if err := sync.CheckPersona(rec.Persona); err != nil {
		return platform.Invalid("persona", fmt.Sprintf("角色 %s 現在不能指派（%s）", rec.Persona, err))
	}
	if _, err := m.Sync(ctx, &sdk.PackageSyncRequest{ID: rec.Package, Agent: agentID, Persona: rec.Persona, Apply: true, By: &sdk.Actor{Name: claimActor}}); err != nil {
		return err
	}
	return claim.EnableAgent(ctx, agentID, claimActor)
}

func (m *Module) claimByNotice(id string) (string, claimRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.loadSettings()
	if err != nil {
		return "", claimRecord{}, false, err
	}
	for agentID, r := range st.Claims {
		if r.Notice == id {
			return agentID, r, true, nil
		}
	}
	return "", claimRecord{}, false, nil
}

func (m *Module) info(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.log != nil {
		m.log.Info(msg, args...)
	}
}

func (m *Module) warn(msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.log != nil {
		m.log.Warn(msg)
	}
}
