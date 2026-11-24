package roster

// 新家第一位秘書上班後主動打招呼（DEV-153，作者 2026-11-20；規則 ai_docs/modules/agents.md AGS-11）：
// 名冊上一筆秘書紀錄都沒有（含封存的）時建出來的那一位，Create 在她的紀錄記 first_day = true；
// 她第一次被啟用（SetField enabled true）時，核心排一則系統提示給她（routine、from system），請她先開口：
// 自我介紹＋照自己職責開始第一天的引導；那一次就拿掉 first_day（排不排得進去都是）。停用的秘書收不到話（session SES-58），
// 所以不在建的時候送。只在建她的那一刻判斷，之後再請別的秘書、重開服務、升級上來的舊家（名冊本來就有人）都不會送。
//
// 排進去之後照現有的排隊與派送：對話開不起來、額度用完都留在佇列裡等，開好、閒著才貼，不另外重試；
// idempotency key 固定，同一位不會排兩則。排不進去（佇列寫不進去）記一行 WARN、不再排（寧可不送也不送過期的）；
// 請秘書照樣成功，不卡住看板。

import (
	"context"

	"github.com/minglight/evergenie/sdk"
)

// Mailer 是 bus 的送封包能力：新家第一位秘書的招呼提示經它排進她的佇列（沒接就不送）。
type Mailer interface {
	Send(context.Context, *sdk.Envelope) (*sdk.Envelope, error)
}

// FirstDayField 是名冊紀錄上「她是新家第一位秘書、還沒第一次啟用」的欄位（schemas/agent.md）。
const FirstDayField = "first_day"

// FirstDayKey 是第一位秘書招呼提示的 idempotency key（同一位只排一則）。
func FirstDayKey(id string) string { return "first-day:" + id }

// FirstDayPrompt 是核心傳給新家第一位秘書的系統提示。開頭的「［核心］」讓看板把它放在秘書那邊、縮成一小排
// （board WEB-158 的 CORE_FRAME_RE），不畫成主人的泡泡；她那一輪是核心的、不算主人的話（routine、from system）。
// 不寫平台名稱，也不寫哪個套件或職位：各職位怎麼開始第一天寫在她自己的職責裡。
const FirstDayPrompt = "［核心］主人剛請你上班，你是這個家的第一位秘書。" +
	"請你先開口：用你自己的口吻跟主人打招呼、簡單自我介紹，接著照你職責裡第一天上班的做法開始，先問主人第一個問題（一次一題）。" +
	"如果主人已經先跟你說話了，就直接接著回他，不用重新打招呼。" +
	"這則是核心給你的提示，不是主人說的話；回話時不用提到這則提示。"

// rosterEmpty 回這個家的名冊上是不是一筆秘書紀錄都沒有（含停用、封存的，經底層 agent 讀）。讀不到當成不是空的：寧可不送。
func (m *Module) rosterEmpty(ctx context.Context) bool {
	had, err := m.d.Roster.HadAgents(ctx)
	if err != nil {
		if m.log != nil {
			m.log.Warn("讀不到名冊，不記第一位秘書", "error", err)
		}
		return false
	}
	return !had
}

// greetFirst：剛被設成啟用的這筆紀錄有 first_day 就排招呼提示，回最新的紀錄（呼叫時握著名冊的鎖）。
// 只在第一次啟用時判斷一次：先拿掉 first_day（記成 system 做的）再排，不管排不排得進去都不留，
// 之後停用再啟用、改 mode 都不會送出過期的招呼。她的 mode 是 none（沒有對話）或沒接佇列就不排。
// 拿不掉欄位就不排（寧可不送也不重複送）；排不進去只記 WARN，不讓設定失敗。
func (m *Module) greetFirst(ctx context.Context, r sdk.Record) sdk.Record {
	if pending, _ := r.Fields[FirstDayField].(bool); !pending {
		return r
	}
	out, err := m.d.Roster.PatchAgent(ctx, r.ID, nil, []string{FirstDayField}, "system")
	if err != nil {
		if m.log != nil {
			m.log.Warn("拿不掉 first_day，這次不排招呼提示", "agent", r.ID, "error", err)
		}
		return r
	}
	if mode, _ := r.Fields["mode"].(string); mode == "none" || m.d.Mail == nil {
		return out
	}
	if _, err := m.d.Mail.Send(ctx, &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, Route: sdk.EnvelopeRouteCore, From: "system", To: r.ID, Body: FirstDayPrompt, IdempotencyKey: FirstDayKey(r.ID)}); err != nil && m.log != nil {
		m.log.Warn("第一位秘書的招呼提示沒排進去", "agent", r.ID, "error", err)
	}
	return out
}
