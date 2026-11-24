package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 對話的健檢（doctor 的 sessions 群組；2026-11-01 從 doctor 搬來，doctor 只收集）：
// 常駐秘書有沒有在跑、今天主人的話多久送到、看板收到對話狀態的延遲。

// StateLatencyCheckID 是「狀態延遲」那一項的編號（REQ-390）。
const StateLatencyCheckID = "sessions.state_latency"

// receiveLatency 是 session「額外有」的統計：今天主人的話送出到收到（REQ-387）。假的 Sessions 沒有就不列這一項。
type receiveLatency interface {
	ReceiveLatency(ctx context.Context) (count, over int, p50, p90 time.Duration, err error)
}

// bootPromptLister 是 session「額外有」的查詢：現在卡在啟動對話框（信任資料夾等）的 session（DEV-87）。假的 Sessions 沒有就不查。
type bootPromptLister interface {
	BootPrompts(ctx context.Context) ([]*sdk.BootPrompt, error)
}

// stuckLister 是 session「額外有」的查詢：現在話送不進去的對話（SES-77，DEV-162）。假的 Sessions 沒有就不列這一項。
type stuckLister interface {
	Stuck(ctx context.Context) []platform.SessionStuck
}

// stuckFix 是「話送不進去」的下一步（健檢與看板共用同一句）。
const stuckFix = "到看板打開她的對話，按右上角「⋯」選「重開對話」；還是送不進去，到那台 Mac 打開「" + platform.RescueName + "」照畫面做"

// stuckMessage：「〈秘書〉有 N 則話送不進去，已經等了 M 分鐘」（不是主要對話就寫出是哪個對話）。
func stuckMessage(s platform.SessionStuck) string {
	who := s.AgentName
	if who == "" {
		who = s.Agent
	}
	if who == "" {
		who = s.Name
	}
	if s.Name != s.Agent && s.Name != who {
		who += "（" + s.Name + "）"
	}
	return fmt.Sprintf("%s有 %d 則話送不進去，已經等了 %d 分鐘", who, s.Count, s.Minutes)
}

// stuckCheck：每個話送不進去的對話一條紅字（要處理）；都沒有給一條綠字。
func stuckCheck(ctx context.Context, sl stuckLister) []app.CheckResult {
	var out []app.CheckResult
	for _, s := range sl.Stuck(ctx) {
		out = append(out, app.CheckResult{Status: app.StatusError, Message: stuckMessage(s), Fix: stuckFix})
	}
	if len(out) == 0 {
		return []app.CheckResult{{Status: app.StatusOK, Message: "沒有送不進去的話"}}
	}
	return out
}

// Checks 登記對話的健檢。沒接 Sessions 或 Roster 就不列常駐秘書；狀態延遲一定有（只看自己記的樣本）。
func (m *Module) Checks() []app.Check {
	var out []app.Check
	if m.d.Sessions != nil && m.d.Roster != nil {
		out = append(out, app.Check{Name: "sessions.resident", RunAll: m.checkResident})
	}
	if sl, ok := m.d.Sessions.(stuckLister); ok {
		out = append(out, app.Check{Name: "sessions.stuck", RunAll: func(ctx context.Context) []app.CheckResult { return stuckCheck(ctx, sl) }})
	}
	if rl, ok := m.d.Sessions.(receiveLatency); ok {
		out = append(out, app.Check{Name: "sessions.latency", Run: func(ctx context.Context) app.CheckResult { return latencyCheck(ctx, rl) }})
	}
	return append(out, app.Check{Name: StateLatencyCheckID, Run: func(context.Context) app.CheckResult { return m.stateLatencyCheck() }})
}

// failMessage 把模組回的錯誤換成白話；不是我們的錯誤型別就不帶內容。
func failMessage(err error) string {
	if e := platform.SDKError(err); e.Code != sdk.ErrorCodeInternal {
		return e.Message
	}
	return "發生內部錯誤"
}

// checkResident：每位啟用中的常駐秘書，停在啟動提示或（家設定自動啟動時）沒在跑各一條黃字；都正常給一條綠字。
// 引擎沒動靜不算問題（S5：不猜卡住），所以沒有「可能卡住」這一條。
func (m *Module) checkResident(ctx context.Context) []app.CheckResult {
	sessions, err := m.d.Sessions.List(ctx, "", nil)
	if err != nil {
		return []app.CheckResult{{Status: app.StatusWarn, Message: "讀不到 session 清單：" + failMessage(err)}}
	}
	agents, err := m.d.Roster.List(ctx)
	if err != nil {
		return []app.CheckResult{{Status: app.StatusWarn, Message: "讀不到秘書名冊：" + failMessage(err)}}
	}
	autostart := true
	if m.home != "" {
		if h, err := platform.LoadHome(m.home); err == nil {
			autostart = h.Sessions.Autostart
		}
	}
	promptOn := map[string]*sdk.BootPrompt{} // session 名稱 → 它停著等人回答的啟動對話框
	if bl, ok := m.d.Sessions.(bootPromptLister); ok {
		call, cancel := context.WithTimeout(ctx, bootPromptsTimeout) // 當場問引擎：不回就當沒有，不拖住健檢（WEB-175）
		prompts, err := bl.BootPrompts(call)
		cancel()
		if err == nil {
			for _, p := range prompts {
				if promptOn[p.Session] == nil {
					promptOn[p.Session] = p
				}
			}
		}
	}
	var out []app.CheckResult
	residents := 0
	for _, a := range agents {
		if a == nil {
			continue
		}
		if enabled, ok := a.Fields["enabled"].(bool); ok && !enabled {
			continue
		}
		if mode, _ := a.Fields["mode"].(string); mode != "" && mode != "resident" {
			continue
		}
		residents++
		state := sdk.SessionStateStopped
		var prompt *sdk.BootPrompt
		for _, s := range sessions {
			if s != nil && s.Agent == a.ID && s.Kind == sdk.SessionKindResident {
				state, prompt = s.State, promptOn[s.Name]
				break
			}
		}
		label := agentLabel(a)
		switch {
		case prompt != nil:
			// 停在啟動對話框、或話貼不進去（引擎登記成同一種提示，DEV-159）的秘書什麼都收不到，狀態卻可能是「待命」或「啟動中」；一定要先於「正常」列出來。
			out = append(out, app.CheckResult{Status: app.StatusWarn, Message: label + " 卡住了：" + promptTitle(prompt), Fix: "要有人回答這個提示才會往下做；到看板最上面的提示回答"})
		case state == sdk.SessionStateStopped && autostart:
			out = append(out, app.CheckResult{Status: app.StatusWarn, Message: label + " 沒有在跑", Fix: "稍等一下（evergenie serve 會自動啟動常駐秘書）；一直沒起來請看 runtime/logs"})
		}
	}
	if len(out) > 0 {
		return out
	}
	switch {
	case residents == 0:
		return []app.CheckResult{{Status: app.StatusOK, Message: "沒有設定常駐秘書"}}
	case !autostart:
		return []app.CheckResult{{Status: app.StatusOK, Message: fmt.Sprintf("%d 位常駐秘書（家設定為不自動啟動）", residents)}}
	}
	return []app.CheckResult{{Status: app.StatusOK, Message: fmt.Sprintf("%d 位常駐秘書都正常", residents)}}
}

func promptTitle(p *sdk.BootPrompt) string {
	if p.Title != "" {
		return p.Title
	}
	return p.ID
}

func agentLabel(a *sdk.Record) string {
	if name, _ := a.Fields["display_name"].(string); name != "" {
		return name + "（" + a.ID + "）"
	}
	return a.ID
}

// latencyCheck：今天主人的話「送出→收到」p50／p90 與超過 3 秒的則數（REQ-387）。只是資訊（綠字），
// 不寫黃字：doctor 的黃字會進健康狀態的推播，慢了幾則不該吵主人；超過 3 秒的每一則都已經是掉包單（SES-51）。
func latencyCheck(ctx context.Context, rl receiveLatency) app.CheckResult {
	count, over, p50, p90, err := rl.ReceiveLatency(ctx)
	if err != nil {
		return app.CheckResult{Status: app.StatusWarn, Message: "讀不到今天的送達紀錄：" + failMessage(err)}
	}
	if count == 0 {
		return app.CheckResult{Status: app.StatusOK, Message: "今天還沒有對秘書說話"}
	}
	return app.CheckResult{Status: app.StatusOK, Message: fmt.Sprintf("今天對秘書說了 %d 句：送出到秘書收到 p50 %s、p90 %s，超過 %d 秒（或沒收到）%d 句",
		count, secs(p50), secs(p90), int(sdk.ReceiveWithin/time.Second), over)}
}

func secs(d time.Duration) string { return fmt.Sprintf("%.1f 秒", d.Seconds()) }

// stateLatencyCheck：今天（家的時區）看板收到對話狀態的延遲——引擎事件發生到核心寫進看板的推送串流，p50／p90 與超過 3 秒的
// 次數（REQ-390）。只在記憶體（serve 重開就從頭算）；只是資訊（綠字），理由同 latencyCheck。
func (m *Module) stateLatencyCheck() app.CheckResult {
	now := m.now()
	loc := time.Local
	if m.home != "" {
		if h, err := platform.LoadHome(m.home); err == nil {
			loc = h.Location()
		}
	}
	y, mo, d := now.In(loc).Date()
	took := m.samples.Since(time.Date(y, mo, d, 0, 0, 0, 0, loc))
	if len(took) == 0 {
		return app.CheckResult{Status: app.StatusOK, Message: "今天看板還沒收到對話狀態的變化（開著某個對話時才量）"}
	}
	over := 0
	for _, d := range took {
		if d > sdk.ReceiveWithin {
			over++
		}
	}
	return app.CheckResult{Status: app.StatusOK, Message: fmt.Sprintf("今天看板收到對話狀態 %d 次：從事件發生到推給看板 p50 %s、p90 %s，超過 %d 秒 %d 次",
		len(took), secs(percentileOf(took, 50)), secs(percentileOf(took, 90)), int(sdk.ReceiveWithin/time.Second), over)}
}

// percentileOf：排好序的資料取第 p 百分位（最近序位法，跟 session 的 ReceiveLatency 一樣）。
func percentileOf(sorted []time.Duration, p int) time.Duration {
	i := (p*len(sorted) + 99) / 100
	if i < 1 {
		i = 1
	}
	return sorted[i-1]
}
