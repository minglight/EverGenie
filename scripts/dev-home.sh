#!/usr/bin/env bash
# 【停用，2026-11-03 作者：開發家與測試家合一，office 只留工坊那個開發家；這個沙盒流程不再使用，檔案先留著。】
# 開發家的啟停與更新（DEV-14、REQ-266、ai_docs/runbooks/release.md「開發家」）。在 office 的 repo 跑。
#
#   scripts/dev-home.sh init                 第一次：把 fixture 家複製到開發家資料夾並寫好 [runtime]（已存在就不動）
#   scripts/dev-home.sh update [<ref>]       用那個 commit／分支（預設 main）編譯，重開開發家；做完一張單就跑一次，再請主人玩
#   scripts/dev-home.sh start | stop | restart | status
#
# 開發家是 fixture 資料＋假引擎：[runtime] sandbox = true（不推播、不自動升級、套件檢查放寬）、env = "dev"（看板、對話名、通知標成開發家）。用的是專屬 worktree（不碰正式家的 bin/）。
# 環境變數（都可選）：EVERGENIE_DEV_HOME（預設 ~/evergenie-dev）、EVERGENIE_DEV_WT（預設 ~/Workspace/<登入名>/evergenie-wt/devhome）、
#   EVERGENIE_DEV_PORT（預設 18870）、EVERGENIE_DEV_URL（init 時寫進 home.toml 的 [runtime] url，admin 套件的「開發家」頁顯示這個連結）。
# 預設不露到 tailnet。真的要手機連：在 Tailscale 後台另建一個服務（D-43：同一台第二個開發家加尾碼，例 genie-dev-<owner>-office-sandbox；
# 不要用工坊的 genie-dev-<owner>-office），再 `evergenie tailscale setup --home <開發家> --service <名>`。
set -uo pipefail

DEV_HOME="${EVERGENIE_DEV_HOME:-$HOME/evergenie-dev}"
WT="${EVERGENIE_DEV_WT:-$HOME/Workspace/$(id -un)/evergenie-wt/devhome}"
PORT="${EVERGENIE_DEV_PORT:-18870}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKUP="${DEV_HOME}-backup"

die() { echo "dev-home: $*" >&2; exit 1; }
pid() { pgrep -f "evergenie serve -home $DEV_HOME" | head -1; }

ensure_wt() {
  [ -d "$WT/.git" ] || [ -f "$WT/.git" ] || git -C "$REPO" worktree add --detach "$WT" main >/dev/null 2>&1 || die "建不出 worktree：$WT"
}

cmd_init() {
  [ -e "$DEV_HOME/home.toml" ] && { echo "開發家已經存在：${DEV_HOME}（不動）"; return 0; }
  mkdir -p "$(dirname "$DEV_HOME")" && cp -R "$REPO/cmd/evergenie/testdata/home" "$DEV_HOME" || die "複製 fixture 失敗"
  local url_line=""
  [ -n "${EVERGENIE_DEV_URL:-}" ] && url_line="url = \"$EVERGENIE_DEV_URL\""
  # fixture 的帳號引擎是測試專用的 test-engine，開發家開不了對話：換成假引擎 fake（2026-10-28 踩過）。
  { sed 's/^engine = "test-engine"$/engine = "fake"/' "$DEV_HOME/home.toml"; cat <<TOML
[runtime]
sandbox = true
env = "dev"
board_port = $PORT
tmux_socket = "hg-dev"
$url_line
TOML
  } > "$DEV_HOME/home.toml.new" && mv "$DEV_HOME/home.toml.new" "$DEV_HOME/home.toml"
  echo "開發家建好了：${DEV_HOME}（看板 port ${PORT}）"
  echo "預設不露到 tailnet。手機要連：後台另建一個服務（例 genie-dev-<owner>-<machine>-sandbox，不要用工坊那個），再 evergenie tailscale setup --home $DEV_HOME --service <名>"
}

cmd_build() {
  ensure_wt
  local ref="${1:-main}"
  git -C "$WT" checkout -q --detach "$ref" || die "切不到 $ref"
  (cd "$WT" && make build >/dev/null && go build -o bin/evergenie-engine-fake ./internal/base/engine/fake/cmd/evergenie-engine-fake) || die "編譯失敗"
  echo "已編譯 $(git -C "$WT" rev-parse --short HEAD)"
}

cmd_start() {
  [ -e "$DEV_HOME/home.toml" ] || die "還沒有開發家，先跑 init"
  [ -x "$WT/bin/evergenie" ] || cmd_build main
  [ -n "$(pid)" ] && { echo "開發家已經開著（pid $(pid)）"; return 0; }
  mkdir -p "$BACKUP"
  # 整串放背景、輸出與輸入都導走，不然呼叫者（管線、工具）會一直等到開發家結束。
  (cd "$WT" && EVERGENIE_BACKUP_DIR="$BACKUP" EVERGENIE_ENGINE_BIN_DIR="$WT/bin" EVERGENIE_OFFICIAL_PACKAGES_DIR="$WT/packages/official" \
    exec nohup ./bin/evergenie serve -home "$DEV_HOME") < /dev/null >> "$DEV_HOME/serve.log" 2>&1 &
  for _ in $(seq 1 30); do
    [ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/")" = 200 ] && { echo "開發家開了：http://127.0.0.1:$PORT"; return 0; }
    sleep 1
  done
  die "30 秒內看板沒起來，看 $DEV_HOME/serve.log"
}

cmd_stop() {
  local p; p="$(pid)"
  [ -z "$p" ] && { echo "開發家沒開"; return 0; }
  kill "$p"
  for _ in $(seq 1 15); do [ -z "$(pid)" ] && { echo "開發家關了"; return 0; }; sleep 1; done
  die "關不掉（pid ${p}）"
}

cmd_status() {
  local p; p="$(pid)"
  if [ -z "$p" ]; then echo "開發家沒開（${DEV_HOME}）"; return 0; fi
  echo "開發家開著：pid ${p}，http://127.0.0.1:${PORT}，版本 $(git -C "$WT" rev-parse --short HEAD 2>/dev/null)"
}

case "${1:-}" in
  init) cmd_init ;;
  update) cmd_stop && cmd_build "${2:-main}" && cmd_start ;;
  start) cmd_start ;;
  stop) cmd_stop ;;
  restart) cmd_stop && cmd_start ;;
  status) cmd_status ;;
  *) sed -n 2,12p "$0" >&2; exit 2 ;;
esac
