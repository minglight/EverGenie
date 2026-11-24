// Package cmdline 是指令執行器：Command 的型別、一次指令執行的環境（Env）、Run（解析指令名稱、
// 執行、把錯誤轉成結束碼與白話訊息）。
//
// 原本在 internal/cli，2026-10-31 骨架重整時搬成匯出；internal/cli 已在 2026-11-01 刪除。
// 這裡不認得任何指令：各業務模組用套件層級函式 Commands() 提供自己的指令表，由 app.Commands 合併後交給 Run。
// 指令行程只是 API 的 client（只用 sdk），不建 Module、不啟動任何東西。
package cmdline
