package chat

import "github.com/minglight/evergenie/sdk"

// 對話狀態的白話（2026-11-01 從 base 的 cmdline 搬回來：是對話的業務，不是指令執行器的事）。

var stateNames = map[sdk.SessionState]string{
	sdk.SessionStateStopped:  "沒在跑",
	sdk.SessionStateStarting: "啟動中",
	sdk.SessionStateIdle:     "待命",
	sdk.SessionStateBusy:     "忙碌中",
	sdk.SessionStateWaiting:  "等你處理",
	sdk.SessionStateLimited:  "額度用完",
}

// StateLabel 把 session 的狀態換成白話一行（含在等的問題或授權、排隊中）。agents 的 `agents mv` 也用（registry 傳進 agents.Commands）。
func StateLabel(s sdk.Session) string {
	l := stateNames[s.State]
	if l == "" {
		l = string(s.State)
	}
	if s.Pending != nil {
		l += "（" + PendingLine(s.Pending) + "）"
	}
	if !s.QueuedAt.IsZero() {
		l += "（排隊等名額）"
	}
	return l
}

// PendingLine 把 session 在等的事換成一句白話。
func PendingLine(p *sdk.Pending) string {
	if p.Kind == sdk.PendingKindQuestion {
		if len(p.Questions) > 0 {
			return "問題：" + p.Questions[0].Text
		}
		return "問題"
	}
	if p.Summary != "" {
		return p.Tool + "：" + p.Summary
	}
	return p.Tool
}
