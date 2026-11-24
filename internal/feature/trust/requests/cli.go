package requests

import (
	"fmt"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie policy …` 的指令表（套件層級函式，不是 Module 的方法）：
// 權限變更申請（Q7，D-26）：秘書用 request／list／show；主人用 decide／revoke（秘書呼叫會被核心拒絕）。
// 指令行程只是 client，不建 Module，只用 sdk 的 client 打 API（CLI-1）。
func Commands() map[string]app.Command {
	return map[string]app.Command{
		"policy":         {Usage: "policy request|list|show|decide|revoke   權限變更申請（秘書被擋時送申請，主人審；evergenie policy --help 看細節）", Group: cmdline.GroupSystem, Run: cmdHelp},
		"policy request": {Run: cmdRequest},
		"policy list":    {Run: cmdList},
		"policy show":    {Run: cmdShow},
		"policy decide":  {Run: cmdDecide},
		"policy revoke":  {Run: cmdRevoke},
	}
}

func api(e *cmdline.Env) sdk.PolicyAPI { return sdk.NewPolicyClient(e.Client) }

const usage = `用法：evergenie policy <子指令>
  request --kind rule --action read|write|commit|push|delete [--scope <資料夾>] [--agent <職位>] --why <被擋的情境> [--risk <風險>]
  request --kind guidance --text <一句話> --why <被擋的情境> [--risk <風險>]
                                    送一張申請（只寫申請，不改任何設定；主人在看板或通知上審）
  list [--status pending|applied|rejected|failed|revoked|expired] [--agent <職位>]
  show <P-n>                        看一張申請（含影響預覽與預覽指紋）
  decide <P-n> --approve --fingerprint <指紋> [--scope <更窄的資料夾>] [--text <一句話>] [--note <備註>]
  decide <P-n> --reject [--note <備註>]
                                    審（只有主人；核准後一直有效，要收回用 revoke）
  revoke <P-n> [--note <備註>]      撤銷已套用的權限（只有主人）
`

func cmdHelp(e *cmdline.Env, a []string) error {
	for _, x := range a {
		if x != "--help" && x != "-h" && x != "help" {
			return cmdline.Usagef("不認得的子指令：policy %s（evergenie policy --help 看用法）", x)
		}
	}
	_, err := fmt.Fprint(e.Out, usage)
	return err
}

func cmdRequest(e *cmdline.Env, a []string) error {
	return cmdline.One(e, a, cmdline.Spec{}, api(e).Request, func(r *sdk.PolicyRequestResponse) any { return r.Request })
}

func cmdList(e *cmdline.Env, a []string) error {
	return cmdline.Each(e, a, cmdline.Spec{Rename: map[string]string{"status": "statuses"}}, api(e).List, nil,
		func(r *sdk.PolicyListResponse) ([]sdk.PolicyRequest, string) { return r.Requests, "" })
}

// cmdShow 印一張申請的白話摘要（含指紋，核准時要帶）。
func cmdShow(e *cmdline.Env, a []string) error {
	a, asJSON := takeBool(a, "json")
	p, err := cmdline.Prepare[sdk.PolicyGetRequest](e, a, cmdline.Spec{ID: true})
	if err != nil {
		return err
	}
	resp, err := api(e).Get(e.Ctx, p.Req)
	if err != nil {
		return err
	}
	q := resp.Request
	if q == nil {
		return fmt.Errorf("沒有找到這張申請")
	}
	if asJSON {
		return e.Line(q)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s　%s　（%s 送的，%s）\n", q.ID, q.Status, q.From, q.CreatedAt.Format("2006-01-02 15:04"))
	if q.Kind == sdk.PolicyKindGuidance {
		fmt.Fprintf(&b, "種類：guidance\n一句話：%s\n", q.Text)
	} else {
		scope := q.Scope
		if scope == "" {
			scope = "（任何地方）"
		}
		fmt.Fprintf(&b, "種類：rule\n秘書：%s\n動作：%s\n範圍：%s\n", q.Agent, q.Action, scope)
	}
	if q.Broad {
		b.WriteString("⚠ 範圍很寬\n")
	}
	fmt.Fprintf(&b, "影響：%s\n為什麼：%s\n", q.Impact, q.Why)
	if q.Risk != "" {
		fmt.Fprintf(&b, "風險：%s\n", q.Risk)
	}
	switch {
	case !q.ExpiresAt.IsZero():
		fmt.Fprintf(&b, "期限：%s\n", q.ExpiresAt.Format("2006-01-02 15:04"))
	case q.Status == sdk.PolicyStatusApplied:
		b.WriteString("期限：一直有效（撤銷才失效）\n")
	}
	if q.DecidedBy != "" {
		fmt.Fprintf(&b, "審核：%s　%s\n", q.DecidedBy, q.DecidedAt.Format("2006-01-02 15:04"))
	}
	if q.Note != "" {
		fmt.Fprintf(&b, "備註：%s\n", q.Note)
	}
	fmt.Fprintf(&b, "指紋：%s\n", q.Fingerprint)
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

// cmdDecide 一定要明講 --approve 或 --reject，避免漏打旗標就變成拒絕。
func cmdDecide(e *cmdline.Env, a []string) error {
	a, reject := takeBool(a, "reject")
	p, err := cmdline.Prepare[sdk.PolicyDecideRequest](e, a, cmdline.Spec{ID: true})
	if err != nil {
		return err
	}
	switch {
	case reject && p.Set["approve"]:
		return cmdline.Usagef("--approve 和 --reject 只能給一個")
	case !reject && !p.Req.Approve:
		return cmdline.Usagef("用法：evergenie policy decide <P-n> --approve --fingerprint <指紋> | --reject")
	case p.Req.Approve && p.Req.Fingerprint == "":
		return cmdline.Usagef("核准要帶 --fingerprint（先 evergenie policy show %s 看過）", p.Req.ID)
	}
	return cmdline.Send(e, p, api(e).Decide, func(r *sdk.PolicyDecideResponse) any { return r.Request })
}

func cmdRevoke(e *cmdline.Env, a []string) error {
	return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Revoke, func(r *sdk.PolicyRevokeResponse) any { return r.Request })
}

// takeBool 把布林旗標 --name 從參數中取出。
func takeBool(args []string, name string) (rest []string, found bool) {
	for i, a := range args {
		if a == "--" {
			return append(rest, args[i:]...), found
		}
		if a == "--"+name {
			found = true
			continue
		}
		rest = append(rest, a)
	}
	return rest, found
}
