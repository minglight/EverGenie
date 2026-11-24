// Package push 是 Web Push 的傳送通道（底層模組：沒有 HTTP、沒有指令、不知道通知規則）：
// 用 VAPID 金鑰把一則內容送到一個訂閱端點，並管理訂閱端點（settings/push.toml）。
// 要不要推、推什麼、安靜時段與偏好都是 notify 的事。說明卡 ai_docs/modules/inbox.md。
package push
