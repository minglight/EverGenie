package board

import "embed"

// Dist 是 vite build 的輸出（`make web` 產生、進 git，使用者不用裝 Node，D-11）。
//
//go:embed all:dist
var Dist embed.FS
