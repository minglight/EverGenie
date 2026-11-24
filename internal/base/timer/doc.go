// Package timer 是純計時的底層模組（沒有 HTTP、沒有指令、沒有 goroutine、不發事件）：
// cron 解析與下次時間、時段型排程、到期判斷（補跑與錯過）。說明卡 ai_docs/modules/timer.md。
package timer
