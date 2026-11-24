// Package host 是「這台電腦」的環境：launchd 服務、安裝前檢查、Tailscale。ai_docs/modules/system.md（REQ-164、REQ-165）。
// 只在命令列（cmd/evergenie）用，不在 serve 裡執行；所有外部指令都經 Runner，測試用假的，不碰真的 launchd、brew、tailscale。
package host
