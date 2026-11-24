// Package chat — 說明卡 ai_docs/modules/chat.md。
// 業務模組：對話與傳話的入口。對外是 sdk 的 session（開關、串流、授權與問答、討論串）、bus（傳話封包）、
// engine_host（引擎行程回報）三個服務；對內是 `evergenie sessions …`、`status`、`say` 指令。
// session 執行期（internal/base/session）與事件佇列（internal/base/bus）是底層，這裡只是它們的 HTTP 與指令。
package chat
