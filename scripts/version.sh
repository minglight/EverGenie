#!/usr/bin/env bash
# 印出這份程式碼的核心版本（REQ-277）。`make build` 和 `scripts/deploy.sh` 都叫它，所以兩邊的版本字串一定一樣。
#   在 vX.Y.Z 的 tag 上        → X.Y.Z
#   tag 之後又有 N 個 commit   → X.Y.Z-N-g<短碼>
#   還沒有任何 v 開頭的 tag    → commit 短碼
#   有沒提交的改動             → 結尾加 -dirty
# safe/* 的 tag 不算版號（--match 只看 v<數字> 開頭的）。沒有 git 時印空字串。
cd "$(dirname "$0")/.." || exit 0
git describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null | sed 's/^v//'
