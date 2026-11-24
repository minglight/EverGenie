#!/usr/bin/env bash
# 部署 evergenie 到一台電腦（發版流程缺口 4、ai_docs/runbooks/release.md 步驟 5、6）。在 office 的 repo 跑，順序一定是先 mini 再 office。
#
#   scripts/deploy.sh [--dry-run] [--force] [--workshop] <mini|office|all>   部署（all＝先 mini、成功了才 office）
#   scripts/deploy.sh [--dry-run] [--force] <workshop|mini-dev>                只更新一個開發家（workshop＝office 開發家〔原名工坊〕、mini-dev＝mini 開發家）
#   scripts/deploy.sh --rollback [--dry-run] <mini|office|workshop|mini-dev>  手動換回上一版（bin.prev/）
#
# D-53（2026-11-03）：開始前先換成 origin/main 的部署工具（不用再開暫存 worktree 手動 checkout）；部署前後比對那台上要守的別人的服務
#   （launchctl 名稱符合設定檔寫的樣式，只讀）、少了就大聲報錯（結束碼 4）；--workshop 讓 office 成功後順便更新工坊（預設不做）。
# 開發家（DEV-157，主人 2026-11-14 定；ai_docs/runbooks/homes.md「部署工具怎麼換開發家」）：mini 部署成功後一定接著把 mini 開發家換成
#   同一個 commit；開發家不在、服務沒在跑就跳過；repo 不在 main 或有沒 commit 的改動就不動、說原因（結束碼 1，不擋別台）；
#   更新前後各傳一句話給那個家的 art。位置集中在下面的 devhome_conf。
#
# 做的事（每台）：遠端 repo `git fetch`＋`merge --ff-only origin/main` → 舊 `bin/` 留成 `bin.prev/` → `make build`
#   → `evergenie service restart` → 檢查（doctor 核心版本＝這次的 `scripts/version.sh`、沒有紅字；看板首頁 200 且 bundle 跟 dist 一致；
#   `packages list` 有套件、沒有載入失敗的）。任何一步沒過：把 `bin.prev/` 換回 `bin/`、重開服務、印白話原因、結束碼 1。
# --dry-run 只印會做什麼，不 ssh、不執行任何東西。已經是最新版（上次成功部署的版本＝這次的）就不重做，免得把 bin.prev/ 蓋成同一版；--force 照做。
#
# 一台跑多個家（GOAL-2 W2：日常＋工坊）：那台的 `~/.evergenie-homes` 一行一個家的資料夾（`#` 之後是註解、空行略過、可以 `~/` 開頭）。
#   有這個檔：編一次（同一個執行檔），每個家各自 `EVERGENIE_HOME=<家> evergenie service restart`（服務名稱照家的 [runtime] service_label）、各自檢查；
#   任何一個家沒過就換回、每個家都重開回舊版。沒有這個檔（或是空的）：照舊只有一個家（登入殼層的 EVERGENIE_HOME 或 EVERGENIE_DEPLOY_HOME）。
#
# 機器：mini → ssh 到 EVERGENIE_DEPLOY_MINI_HOST（沒有預設：沒設就停下來說缺什麼，結束碼 2）；office → 本機（這支就是在 office 跑）。
#   都可以改：EVERGENIE_DEPLOY_MINI_HOST、EVERGENIE_DEPLOY_OFFICE_HOST（`local` 或 ssh 名稱）。
# 設定檔（DEV-158）：跟自己環境有關的值（另一台的 ssh 名稱、要守的服務）不寫在 repo，寫在 repo 外面的 ~/.evergenie-release/deploy.env
#   （換位置用 EVERGENIE_DEPLOY_ENV，空字串＝不讀；明講的位置找不到檔就停下來，結束碼 2）。它是一段 bash，有就 source（在子殼層裡），
#   只帶回 EVERGENIE_DEPLOY_* 開頭的變數，所以只寫下面這些變數的設定，例：
#     EVERGENIE_DEPLOY_MINI_HOST=my-mini
#     EVERGENIE_DEPLOY_MINI_GUARD='[.]family$'
#   環境變數已經設了的（含設成空字串）以環境變數為準，設定檔只補沒設的。
# 下面這些變數都可以寫在設定檔或設成環境變數；只有 EVERGENIE_DEPLOY_MINI_HOST 在部署 mini、mini-dev、all 時一定要有，其他都可選：
#   EVERGENIE_DEPLOY_REPO   兩台都用這個 repo 位置（測試用）；沒設時 mini 用 EVERGENIE_DEPLOY_MINI_REPO、office 用 EVERGENIE_DEPLOY_OFFICE_REPO（都預設 $HOME/Workspace/<登入名>/evergenie-deploy，專用部署 checkout）
#   EVERGENIE_DEPLOY_HOME   那台正式家的位置；有設就以 EVERGENIE_HOME 帶進去（那台的登入殼層沒設 EVERGENIE_HOME 時才需要）
#   EVERGENIE_DEPLOY_SHELL  遠端執行用的殼層（預設 `bash -lc`）
#   EVERGENIE_DEPLOY_TRIES  重開後檢查重試的秒數：看板、doctor、套件三項全部 OK 才停（預設 30）
#   EVERGENIE_DEPLOY_HOMES_FILE  那台的家清單檔（預設 $HOME/.evergenie-homes；測試用）
#   EVERGENIE_DEPLOY_SELF_UPDATE=0  不換成 origin/main 的部署工具（測試、或正在改這支時）
#   EVERGENIE_DEPLOY_MINI_GUARD／EVERGENIE_DEPLOY_OFFICE_GUARD  那台要守的 launchd 名稱（awk 正規式；預設不守；空字串＝不守；mini 不守時真的部署會印一行提醒）
#   EVERGENIE_DEPLOY_GUARD_TRIES  部署後等要守的服務回來的秒數（預設 10）
#   EVERGENIE_DEPLOY_WORKSHOP_REPO／EVERGENIE_DEPLOY_WORKSHOP_HOME  工坊跑的 repo（預設 $HOME/Workspace/<登入名>/evergenie）與工坊的家（預設 $HOME/Workspace/<登入名>/evergenie-workshop）
#   EVERGENIE_DEPLOY_MINI_DEV_REPO／EVERGENIE_DEPLOY_MINI_DEV_HOME  mini 開發家跑的 repo（預設那台的 ~/Workspace/<登入名>/evergenie）與它的家（預設那台的 ~/Workspace/<登入名>/evergenie-dev）
#   EVERGENIE_DEPLOY_MINI_DEV_NOTIFY  mini 開發家換版前後傳話給哪個職位（預設 art；空字串＝不傳）
# 結束碼：0 成功（或已是最新、或開發家不在／沒在跑而跳過）；1 部署沒過（已自動換回，原因在輸出；跟著正式家的開發家沒換成也是 1，那時正式家已經部署好了）；
#   2 用法錯誤；3 沒過而且換不回（沒有 bin.prev/ 或換回也失敗，要人處理）；
#   4 要守的服務少了（不管部署本身過沒過；部署工具沒碰它們，要人馬上看）。3 比 4 優先；正式家的結束碼比開發家的優先。
set -uo pipefail

# 兩台用同一個登入名：資料夾預設放在 ~/Workspace/<登入名>/ 底下（都可以用下面的環境變數或設定檔改）。
ME="$(id -un)"

usage() {
  cat >&2 <<'EOF'
用法：
  scripts/deploy.sh [--dry-run] [--force] [--workshop] <mini|office|all>
  scripts/deploy.sh [--dry-run] [--force] <workshop|mini-dev>
  scripts/deploy.sh --rollback [--dry-run] <mini|office|workshop|mini-dev>
mini 先、office 後；all 會照這個順序做，mini 沒過就不碰 office。--dry-run 只印不做。
mini 部署成功後一定接著把 mini 開發家換成同一個 commit（不在、沒在跑就跳過）；只換它：mini-dev。
--workshop：office 部署成功後，順便把工坊（office 開發家）更新到 origin/main 並重開；預設不做。只換它：workshop。
EOF
  exit 2
}

ARGS=("$@")
DRY=0 FORCE=0 ROLLBACK=0 WORKSHOP=0 TARGET=""
for a in "$@"; do
  case "$a" in
    --dry-run) DRY=1 ;;
    --force) FORCE=1 ;;
    --rollback) ROLLBACK=1 ;;
    --workshop) WORKSHOP=1 ;;
    -h | --help) usage ;;
    -*) echo "不認得的選項：$a" >&2; usage ;;
    *) if [ -z "$TARGET" ]; then TARGET="$a"; else usage; fi ;;
  esac
done
case "$TARGET" in
  mini | office | workshop | mini-dev) ;;
  all) if [ "$ROLLBACK" = 1 ]; then echo "--rollback 一次只換一台：請指定 mini、office、workshop 或 mini-dev" >&2; usage; fi ;;
  *) usage ;;
esac
if [ "$WORKSHOP" = 1 ]; then
  case "$TARGET" in
    office | all) [ "$ROLLBACK" = 0 ] || { echo "--workshop 不能跟 --rollback 一起用：工坊要換回請用 --rollback workshop" >&2; usage; } ;;
    *) echo "--workshop 只跟 office 或 all 一起用（工坊在 office 上）；只更新工坊用 scripts/deploy.sh workshop" >&2; usage ;;
  esac
fi

# ---- 部署設定檔（repo 外面，DEV-158）--------------------------------------------------------------------
# 設定檔在子殼層裡 source、只帶回 EVERGENIE_DEPLOY_* 變數：蓋不到這支自己的變數（TARGET、DRY…），裡面用到沒設的變數也不會讓這支直接停掉。
# 先記下現在有設的 EVERGENIE_DEPLOY_*（含空字串），帶回設定檔的值之後再放回去：環境變數優先，設定檔只補沒設的。
# 明講的位置（EVERGENIE_DEPLOY_ENV 有設、不是空的）找不到檔就停下來（判斷不了就不做）；預設位置沒有檔＝沒有設定檔。
if [ -n "${EVERGENIE_DEPLOY_ENV:-}" ] && [ ! -f "$EVERGENIE_DEPLOY_ENV" ]; then
  echo "找不到部署設定檔 ${EVERGENIE_DEPLOY_ENV}（EVERGENIE_DEPLOY_ENV 指的位置）：先不部署。不要讀設定檔就把 EVERGENIE_DEPLOY_ENV 設成空字串。" >&2
  exit 2
fi
DEPLOY_ENV="${EVERGENIE_DEPLOY_ENV-$HOME/.evergenie-release/deploy.env}"
if [ -n "$DEPLOY_ENV" ] && [ -f "$DEPLOY_ENV" ]; then
  _keep="$(for _v in $(compgen -v EVERGENIE_DEPLOY_); do printf '%s=%q\n' "$_v" "${!_v-}"; done)"
  # shellcheck disable=SC1090
  if ! _from="$(set +u; . "$DEPLOY_ENV" >/dev/null || exit 1; for _v in $(compgen -v EVERGENIE_DEPLOY_); do printf '%s=%q\n' "$_v" "${!_v-}"; done)"; then
    echo "部署設定檔 ${DEPLOY_ENV} 沒有跑成功（語法錯誤，或最後一行的結束碼不是 0）：先不部署" >&2
    exit 2
  fi
  eval "$_from"
  eval "$_keep"
  unset _keep _from _v
else
  DEPLOY_ENV=""
fi

say() { printf '%s\n' "$*"; }

# ---- 先換成 origin/main 的部署工具（D-53）--------------------------------------------------------------
# 部署的一定是 origin/main，所以部署工具本身也該用 origin/main 那一份：手上這份可能在別的分支、或還沒拉新版。
# 以前要先開一個暫存 worktree `git checkout --detach origin/main` 再跑，現在這支自己做：
# 在自己的 repo `git fetch origin main`，origin/main 的 scripts/deploy.sh 跟這份不一樣，就把它寫到暫存檔、換成它跑（同樣的參數）。
# 只動 remote-tracking 的 origin/main，不切分支、不改工作資料夾。--dry-run 不 fetch、不換，只說會不會換。
# EVERGENIE_DEPLOY_SELF_UPDATE=0 關掉（測試、或正在改部署工具本身時）。
self_update() {
  if [ -n "${EVERGENIE_DEPLOY_SELF_FROM:-}" ]; then
    # 已經是換過的那一份：暫存檔 bash 已經開著了，可以先刪
    [ -n "${EVERGENIE_DEPLOY_SELF_TMP:-}" ] && rm -f "$EVERGENIE_DEPLOY_SELF_TMP"
    say "（部署工具：用 origin/main ${EVERGENIE_DEPLOY_SELF_FROM} 的 scripts/deploy.sh）"
    return 0
  fi
  local self repo latest sha tmp
  self="${BASH_SOURCE[0]}"
  repo="$(cd "$(dirname "$self")/.." 2>/dev/null && pwd)" || return 0
  git -C "$repo" rev-parse --git-dir >/dev/null 2>&1 || return 0
  # dry-run 順便說「會部署的是哪個 commit」（本機看到的 origin/main；只讀本機的 git）
  [ "$DRY" = 1 ] && SELF_ORIGIN="$(git -C "$repo" log -1 --format='%h %s' origin/main 2>/dev/null)" && SELF_REPO="$repo"
  [ "${EVERGENIE_DEPLOY_SELF_UPDATE:-1}" = 0 ] && return 0
  # --rollback 用手上這份：壞的可能正是剛推上去的 deploy.sh，緊急換回不能跟著用它
  [ "$ROLLBACK" = 1 ] && return 0
  if [ "$DRY" = 1 ]; then
    latest="$(git -C "$repo" show origin/main:scripts/deploy.sh 2>/dev/null)" || return 0
    if [ "$latest" != "$(cat "$self")" ]; then
      say "（這份部署工具跟本機看到的 origin/main 不一樣：真的部署時會先抓 origin/main、改用那一份；下面印的是這份的計畫）"
    fi
    return 0
  fi
  if ! git -C "$repo" fetch -q origin main 2>/dev/null; then
    say "⚠ 抓不到 origin/main（網路？）：先用手上這份部署工具（${repo}）。那台還是會自己 fetch 並部署 origin/main。"
    return 0
  fi
  latest="$(git -C "$repo" show origin/main:scripts/deploy.sh 2>/dev/null)" || return 0
  [ "$latest" = "$(cat "$self")" ] && return 0
  sha="$(git -C "$repo" rev-parse --short origin/main)"
  tmp="$(mktemp "${TMPDIR:-/tmp}/hg-deploy.XXXXXX")" || return 0
  printf '%s\n' "$latest" >"$tmp"
  say "手上的部署工具跟 origin/main（${sha}）的不一樣：改用 origin/main 那一份。"
  EVERGENIE_DEPLOY_SELF_FROM="$sha" EVERGENIE_DEPLOY_SELF_TMP="$tmp" exec bash "$tmp" "${ARGS[@]}"
}
SELF_ORIGIN="" SELF_REPO=""
if [ "$DRY" = 1 ]; then say "（dry-run：只印會做什麼，不 ssh、不執行）"; fi
self_update
if [ "$DRY" = 1 ] && [ -n "$DEPLOY_ENV" ]; then say "（部署設定：${DEPLOY_ENV}）"; fi

# mini 的 ssh 名稱沒有預設（那是自己電腦的名字，不寫在 repo）：會動到 mini 的目標缺了它就停下來，什麼都還沒做（結束碼 2）。
# dry-run 什麼都不做，照樣印計畫（用「<mini 的 ssh 名稱>」代替），先說真的部署會停在這裡、缺什麼。
MINI_HOST="${EVERGENIE_DEPLOY_MINI_HOST:-}"
case "$TARGET" in
  mini | mini-dev | all)
    if [ -z "$MINI_HOST" ]; then
      if [ -n "$DEPLOY_ENV" ]; then
        _need="在部署設定檔 ${DEPLOY_ENV} 寫一行 EVERGENIE_DEPLOY_MINI_HOST=<mini 的 ssh 名稱>，或設這個環境變數"
      elif [ -n "${EVERGENIE_DEPLOY_ENV+x}" ]; then
        _need="設環境變數 EVERGENIE_DEPLOY_MINI_HOST=<mini 的 ssh 名稱>（EVERGENIE_DEPLOY_ENV 是空的，這次不讀設定檔）"
      else
        _need="建部署設定檔 ~/.evergenie-release/deploy.env、寫一行 EVERGENIE_DEPLOY_MINI_HOST=<mini 的 ssh 名稱>，或設這個環境變數"
      fi
      if [ "$DRY" = 1 ]; then
        say "⚠ 不知道 mini 要 ssh 到哪台：${_need}。真的部署會停在這裡（結束碼 2）；下面的計畫用「<mini 的 ssh 名稱>」代替。"
        MINI_HOST="<mini 的 ssh 名稱>"
      else
        echo "不知道 mini 要 ssh 到哪台：${_need}。什麼都還沒做。" >&2
        exit 2
      fi
      unset _need
    fi
    ;;
esac

SHELL_CMD="${EVERGENIE_DEPLOY_SHELL:-bash -lc}"
TRIES="${EVERGENIE_DEPLOY_TRIES:-30}"
case "$TRIES" in ""|*[!0-9]*) say "EVERGENIE_DEPLOY_TRIES 要是秒數（整數），拿到「${TRIES}」"; exit 2 ;; esac
# 那台部署用的 repo：兩台的正式版都用專用的 checkout（只拉、只編，不在裡面開發）~/Workspace/<登入名>/evergenie-deploy（DEV-32）。
# 兩台的 ~/Workspace/<登入名>/evergenie 是開發家跑的開發程式（2026-11-01 起 mini 也有開發家），部署不碰。
# EVERGENIE_DEPLOY_REPO 有設就兩台都用它（測試用）。
REPO_EXPR=""
repo_of() {
  if [ -n "${EVERGENIE_DEPLOY_REPO:-}" ]; then echo "$EVERGENIE_DEPLOY_REPO"; return; fi
  case "$1" in
    office) echo "${EVERGENIE_DEPLOY_OFFICE_REPO:-\$HOME/Workspace/${ME}/evergenie-deploy}" ;;
    *) echo "${EVERGENIE_DEPLOY_MINI_REPO:-\$HOME/Workspace/${ME}/evergenie-deploy}" ;;
  esac
}
# 那台的登入殼層不一定帶 go／make／pnpm 與 evergenie 入口（~/.local/bin）的路徑（ssh 非互動的 bash 不讀 ~/.zprofile）：
# 在後面補常見的位置（不蓋掉原本的）。2026-10-28 第一次正式部署時發現少了 ~/.local/bin。
PREFIX='export PATH="$PATH:$HOME/.local/bin:$HOME/go/bin:/opt/homebrew/bin:/usr/local/go/bin:/usr/local/bin"; '
if [ -n "${EVERGENIE_DEPLOY_HOME:-}" ]; then PREFIX+="export EVERGENIE_HOME=$(printf %q "$EVERGENIE_DEPLOY_HOME"); "; fi

# 那台的家清單（一行一個家；在那台展開 ~ 與 $HOME）。EVERGENIE_DEPLOY_HOMES_FILE 是本機給的路徑字串，到那台才展開。
HOMES_FILE_EXPR="${EVERGENIE_DEPLOY_HOMES_FILE:-\$HOME/.evergenie-homes}"
HOMES=()

# shq：整段字串用單引號包起來交給遠端殼層（字串裡的單引號寫成 '\''）
shq() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }
# rq <路徑>：給那台殼層的一個字。開頭是字面的 $HOME/ 就留給那台展開（mini 的家目錄不一定跟這台一樣），其他照字面。
rq() { case "$1" in '$HOME/'*) printf '"$HOME"/%q' "${1#\$HOME/}" ;; *) printf '%q' "$1" ;; esac; }

# ---- 開發家：跑 repo 裡 bin/ 的家（不在家清單裡），位置集中在這裡（ai_docs/runbooks/homes.md）---------------------
# workshop＝office 開發家（原名工坊）：只有 --workshop 或 workshop 才換（D-53：那是秘書正在工作的家，主資料夾常有別人的改動）。
# mini-dev＝mini 開發家：mini 部署成功後一定接著換（主人 2026-11-14 定），換版前後傳話給 DEV_NOTIFY 那位秘書。
# 服務名稱不寫在這裡：`evergenie service restart` 照那個家的 [runtime] service_label。
devhome_conf() {
  case "$1" in
    workshop)
      DEV_NAME="工坊" DEV_MACHINE=office DEV_FOLLOWS=0 DEV_NOTIFY="" DEV_VAR=WORKSHOP
      DEV_REPO="${EVERGENIE_DEPLOY_WORKSHOP_REPO:-\$HOME/Workspace/${ME}/evergenie}"
      DEV_HOME="${EVERGENIE_DEPLOY_WORKSHOP_HOME:-$HOME/Workspace/${ME}/evergenie-workshop}"
      ;;
    mini-dev)
      DEV_NAME="mini 開發家" DEV_MACHINE=mini DEV_FOLLOWS=1 DEV_NOTIFY="${EVERGENIE_DEPLOY_MINI_DEV_NOTIFY-art}" DEV_VAR=MINI_DEV
      DEV_REPO="${EVERGENIE_DEPLOY_MINI_DEV_REPO:-\$HOME/Workspace/${ME}/evergenie}"
      DEV_HOME="${EVERGENIE_DEPLOY_MINI_DEV_HOME:-\$HOME/Workspace/${ME}/evergenie-dev}"
      ;;
  esac
}
# devhome_refuse <開發家>：測試給了 EVERGENIE_DEPLOY_REPO、卻沒給這個開發家的位置（會動到這台真的開發家）就說明並回 0。
devhome_refuse() {
  [ -n "${EVERGENIE_DEPLOY_REPO:-}" ] || return 1
  devhome_conf "$1"
  local r="EVERGENIE_DEPLOY_${DEV_VAR}_REPO" h="EVERGENIE_DEPLOY_${DEV_VAR}_HOME"
  [ -z "${!r:-}" ] || [ -z "${!h:-}" ] || return 1
  say "有 EVERGENIE_DEPLOY_REPO（測試用）時${DEV_NAME}也要給 ${r}、${h}，不然會動到真的開發家：先不做。"
}

# ---- 在 $HOST 上執行（dry-run 只印）-----------------------------------------------------------------
HOST="" MACHINE="" SWAP_FAILED=0 GUARD_BROKEN=0 OLD_COMMIT="" DEPLOYED_COMMIT="" LAST_FAIL=""

# exec_on <完整指令>：本機直接跑，其他台經 ssh。
exec_on() {
  if [ "$HOST" = local ]; then
    $SHELL_CMD "$1"
  else
    ssh "$HOST" "$SHELL_CMD $(shq "$1")"
  fi
}
wrap() { printf '%scd "%s" || { echo "找不到 repo：%s" >&2; exit 97; }; %s' "$PREFIX" "$REPO_EXPR" "$REPO_EXPR" "$1"; }

# rx <說明> <指令>：指令在那台的 repo 資料夾裡跑，輸出照樣印出來；dry-run 只印計畫、回 0。
rx() {
  if [ "$DRY" = 1 ]; then
    say "  [dry-run] $1"
    say "            $MACHINE: $2"
    return 0
  fi
  exec_on "$(wrap "$2")"
}
# rxo：同 rx，但計畫印到 stderr，讓呼叫者用 $(…) 收標準輸出（步驟間傳值用）；dry-run 回空字串。
rxo() {
  if [ "$DRY" = 1 ]; then
    say "  [dry-run] $1" >&2
    say "            $MACHINE: $2" >&2
    return 0
  fi
  exec_on "$(wrap "$2")"
}
# rxs：同 rx，但那台印的字也收進 RX_OUT（失敗時拿來說原因、傳話）；dry-run 跟 rx 一樣只印計畫。
RX_OUT=""
rxs() {
  local rc
  RX_OUT=""
  if [ "$DRY" = 1 ]; then rx "$@"; return 0; fi
  RX_OUT="$(exec_on "$(wrap "$2")" 2>&1)"; rc=$?
  [ -z "$RX_OUT" ] || printf '%s\n' "$RX_OUT"
  return $rc
}

# ---- 重開之後的檢查（在那台跑的一段 shell）-----------------------------------------------------------
# 每項印「OK …」或「FAIL …」，任何一項 FAIL 結束碼 1。$1 是預期的核心版本（空＝不比對版本）。
# 三項一起重試到全部 OK，最多 $tries 秒，印最後一輪的結果：`service restart` 回來時舊程式可能還在回看板
# （回的是舊 bundle）、新程式還沒起來（doctor 說沒在跑、packages list 失敗）。只看第一次回應會把好好的新版誤判成壞的、
# 白白換回（2026-11-03 踩過）。真的壞掉的版本照樣沒過，只是多等 $tries 秒。
check_script() {
  printf 'want=%q\ntries=%q\n' "$1" "$TRIES"
  cat <<'EOF'
built=$(grep -o 'assets/index-[A-Za-z0-9_-]*\.js' internal/base/app/board/dist/index.html | head -1)
# 看板的 port：`config get` 走核心的 API，剛重開還沒起來時讀不到。每一輪重讀；讀不到（連不到核心）這輪就算沒過，
# 不能退回預設 8850——那可能是同一台別的家（曾經打到同一台的另一個家、抓到它的舊 bundle，2026-11-04 踩過）。
board_port() {
  local p
  p=$(evergenie config get runtime.board_port 2>&1 | head -1 | tr -d '[:space:]')
  case "$p" in ''|0|*[!0-9]*) p=$(evergenie config get web.port 2>&1 | head -1 | tr -d '[:space:]') ;; esac
  case "$p" in *連不到*) echo "" ;; ''|*[!0-9]*) echo 8850 ;; *) echo "$p" ;; esac
}
attempt() {
  bad=0; res=""
  # 看板首頁
  port=$(board_port)
  if [ -z "$port" ]; then
    html=""
  else
    html=$(curl -fsS --max-time 5 "http://127.0.0.1:$port/" 2>/dev/null) || html=""
  fi
  if [ -z "$port" ]; then
    res="${res}FAIL 連不到核心，讀不到看板的 port（服務可能還沒起來）"$'\n'; bad=1
  elif [ -z "$html" ]; then
    res="${res}FAIL 看板打不開：http://127.0.0.1:$port/ 沒有回 200（服務可能沒起來，看家的暫存資料夾 logs/serve.err.log：第 1 版 <家>/runtime/，第 2 版在 home.toml [layout] state 指的狀態資料夾的 runtime/）"$'\n'; bad=1
  else
    served=$(printf '%s' "$html" | grep -o 'assets/index-[A-Za-z0-9_-]*\.js' | head -1)
    if [ -z "$served" ] || [ "$served" != "$built" ] || [ ! -f "internal/base/app/board/dist/$served" ]; then
      res="${res}FAIL 看板回的頁面用的程式（${served:-找不到}）跟 repo 的 dist（${built:-找不到}）不一致：跑著的可能是舊版，或這版的前端沒有一起編進去"$'\n'; bad=1
    else
      res="${res}OK 看板首頁 200，bundle $served 跟 dist 一致"$'\n'
    fi
  fi
  # doctor：沒有紅字、核心版本是剛部署的
  doc=$(evergenie doctor 2>&1); drc=$?
  got=$(printf '%s\n' "$doc" | sed -n 's/.*核心版本：//p' | head -1 | sed 's/[[:space:]]*$//')
  if [ "$drc" != 0 ]; then
    res="${res}FAIL evergenie doctor 有要處理的項目（結束碼 ${drc}）："$'\n'"$(printf '%s\n' "$doc" | sed 's/^/     /')"$'\n'; bad=1
  elif [ -n "$want" ] && [ "$got" != "$want" ]; then
    res="${res}FAIL 跑著的核心版本是「${got:-讀不到}」，應該是「${want}」（服務可能沒重開成功）"$'\n'; bad=1
  else
    res="${res}OK doctor 沒有紅字，核心版本 ${got:-（不知道）}"$'\n'
  fi
  # 套件：停用的套件帶著原因（例如「需要套件 X，但它已停用」）不算載入失敗
  pk=$(evergenie packages list 2>&1); prc=$?
  n=$(printf '%s\n' "$pk" | grep -c '"id"')
  # 主人自己的、別人分享的套件（明確標 mine／installed）壞了不算這一版的錯（DEV-154）：不把部署換回，列出來，部署完成後寫進升級報告
  # 交給管電腦的秘書。其他的（官方的、層級看不出來的）壞了照樣算部署失敗。
  broken=$(printf '%s\n' "$pk" | grep '"error":"' | grep -v '"enabled":false' | grep -v -e '"tier":"mine"' -e '"tier":"installed"')
  theirs=$(printf '%s\n' "$pk" | grep '"error":"' | grep -v '"enabled":false' | grep -e '"tier":"mine"' -e '"tier":"installed"')
  if [ "$prc" != 0 ] || [ "$n" -lt 1 ]; then
    res="${res}FAIL packages list 沒有套件或執行失敗（結束碼 ${prc}，$n 個）"$'\n'; bad=1
  elif [ -n "$broken" ]; then
    res="${res}FAIL 有套件載入失敗："$'\n'"$(printf '%s\n' "$broken" | sed 's/^/     /' | cut -c1-240)"$'\n'; bad=1
  else
    res="${res}OK 套件 $n 個都在，官方套件沒有載入失敗的"$'\n'
  fi
  if [ -n "$theirs" ]; then
    res="${res}注意 主人自己的或別人分享的套件有載入問題（不算部署失敗；部署完成後會寫進升級報告交給管電腦的秘書）："$'\n'"$(printf '%s\n' "$theirs" | sed 's/^/     /' | cut -c1-240)"$'\n'
  fi
}
start=$SECONDS
while :; do
  attempt
  [ "$bad" = 0 ] && break
  [ $((SECONDS - start)) -ge "$tries" ] && break
  sleep 1
done
printf '%s' "$res"
exit $bad
EOF
}

# 把 bin.prev/ 換回 bin/（壞的那份留在 bin.failed/）；沒有 bin.prev/ 就失敗。
SWAP='[ -d bin.prev ] || { echo "沒有 bin.prev/，換不回去"; exit 1; }; rm -rf bin.failed; { [ ! -d bin ] || mv bin bin.failed; } && mv bin.prev bin && echo "已把 bin.prev/ 換回 bin/（壞的那份留在 bin.failed/）"'

fail_msg() { LAST_FAIL="$1"; say ""; say "✗ $MACHINE 部署沒過：$1"; }

# ---- 守住別人的服務（D-53）---------------------------------------------------------------------------
# 同一台上可能還跑著不歸這次部署管的服務（例如家人的家：不在 ~/.evergenie-homes，部署不重開也不更新），底線是「服務與網址不能斷」。
# 要守的名稱寫在設定檔（awk 正規式）。樣式不要綁產品名：改名前建的服務名還是舊的產品名（只比後面那段，例 `[.]family$`）。
# 部署本來就不碰它們；這裡只讀 launchd（`launchctl list`、`launchctl print`、plist）：部署前記一份（名稱＋在不在跑），部署或換回之後再比一次。
# 少了一個，或本來正常的壞了（常駐型停了、定時型上一次結束碼不是 0；等 EVERGENIE_DEPLOY_GUARD_TRIES 秒還沒回來，預設 10），就大聲報錯、結束碼 4。
# 定時型（每分鐘跑一次的 tick）跑完沒有 PID 是正常的，不算停（DEV-119）。不重開、不改那些服務。
# 每台要守的名稱（awk 正規式，設成空字串＝不守）：EVERGENIE_DEPLOY_MINI_GUARD、EVERGENIE_DEPLOY_OFFICE_GUARD（都預設不守；寫在設定檔）。
GUARD_PAT="" GUARD_BEFORE="" GUARD_TAKEN=0
GUARD_TRIES="${EVERGENIE_DEPLOY_GUARD_TRIES:-10}"
guard_of() {
  case "$1" in
    mini) printf '%s' "${EVERGENIE_DEPLOY_MINI_GUARD-}" ;;
    office) printf '%s' "${EVERGENIE_DEPLOY_OFFICE_GUARD-}" ;;
  esac
}
# 在那台印「名稱 狀態」，一行一個、排序過。狀態四種（launchctl list 的欄位：PID Status Label）：
#   在跑      PID 有值
#   定時待命  沒 PID、結束碼 0、而且是定時型（StartInterval／StartCalendarInterval）：跑完就停著本來就正常（DEV-119）
#   定時異常  定時型、沒 PID、結束碼不是 0：上一次跑壞了
#   停著      其他沒 PID 的（常駐型停了）
# 定時型的判斷：launchctl print 有 run interval／calendarinterval，或 ~/Library/LaunchAgents/<名稱>.plist 有 StartInterval／StartCalendarInterval（任一個成立）。
guard_cmd() {
  local body
  body="$(cat <<'EOS'
l="$(launchctl list)" || exit 1
u="$(id -u)"
printf '%s\n' "$l" | awk -v p="$p" 'NR > 1 && $3 ~ p { print $1, $2, $3 }' | while read -r pid st label; do
  if [ "$pid" != - ]; then s=在跑
  elif launchctl print "gui/$u/$label" 2>/dev/null | grep -q -E 'run interval =|com\.apple\.launchd\.calendarinterval' ||
    grep -q -E 'StartInterval|StartCalendarInterval' "$HOME/Library/LaunchAgents/$label.plist" 2>/dev/null; then
    if [ "$st" = 0 ]; then s=定時待命; else s=定時異常; fi
  else s=停著
  fi
  printf '%s %s\n' "$label" "$s"
done | sort
EOS
)"
  printf '%sp=%q; %s' "$PREFIX" "$GUARD_PAT" "$body"
}
guard_summary() { # <快照>：「3 個（2 個在跑）：a、b、c」
  local n r names
  n="$(printf '%s\n' "$1" | grep -c .)"
  r="$(printf '%s\n' "$1" | grep -c ' 在跑$')"
  names="$(printf '%s\n' "$1" | awk 'NF { printf "%s%s（%s）", (n++ ? "、" : ""), $1, $2 }')"
  printf '%s 個（%s 個在跑）%s' "$n" "$r" "${names:+：$names}"
}
guard_before() {
  GUARD_TAKEN=0 GUARD_BEFORE=""
  if [ -z "$GUARD_PAT" ]; then
    # mini 上常有不歸這次部署管的服務：設定檔沒寫要守哪些就說一聲，免得以為有守（真的部署、換回時每段印一次；只提醒，結束碼不變；dry-run 不印）
    if [ "$DRY" = 0 ] && [ "$GWORD" = mini ]; then say "提醒：沒有設定 mini 上要守住哪些服務（EVERGENIE_DEPLOY_MINI_GUARD 沒設或是空字串），這次不守"; fi
    return 0
  fi
  if [ "$DRY" = 1 ]; then
    say "  [dry-run] 部署前記下要守的服務（只讀 launchd，名稱符合「${GUARD_PAT}」的；不碰它們）"
    say "  [dry-run] 部署或換回之後再比一次：少了、或本來正常的壞了（常駐型停了、定時型結束碼不是 0）→ 大聲報錯、結束碼 4（不重開、不改那些服務）"
    return 0
  fi
  GUARD_BEFORE="$(exec_on "$(guard_cmd)")" || return 1
  GUARD_TAKEN=1
  if [ -z "$GUARD_BEFORE" ]; then
    # 一個都沒看到多半是讀錯地方（例如 launchctl 看不到登入畫面那邊的服務），不是真的沒有：當成讀不到，先不部署
    say "✗ 部署前一個名稱符合「${GUARD_PAT}」的服務都沒看到：可能讀錯地方，也可能它們已經停了。先不部署，請先到 ${MACHINE} 看 launchctl list。"
    say "  確定這台不用守再用 EVERGENIE_DEPLOY_$(printf '%s' "$GWORD" | tr '[:lower:]' '[:upper:]')_GUARD= （空字串）跳過。"
    return 1
  else
    say "要守的服務（部署前）：$(guard_summary "$GUARD_BEFORE")"
  fi
}
guard_ok() { [ "$1" = 在跑 ] || [ "$1" = 定時待命 ]; } # 這個狀態算正常
guard_after() {
  [ "$GUARD_TAKEN" = 1 ] && [ -n "$GUARD_BEFORE" ] || return 0
  local i=0 now lost stopped failed label state cur
  while :; do
    lost="" stopped="" failed=""
    if ! now="$(exec_on "$(guard_cmd)")"; then now="" lost="（讀不到 launchctl list）"; fi
    if [ -z "$lost" ]; then
      while IFS=' ' read -r label state; do
        [ -n "$label" ] || continue
        cur="$(printf '%s\n' "$now" | awk -v l="$label" '$1 == l { print $2 }')"
        if [ -z "$cur" ]; then
          lost="$lost $label"
        elif guard_ok "$state"; then
          # 部署前正常（在跑或定時待命），現在不正常才算：常駐型停了＝停著；定時型上次結束碼不是 0＝定時異常
          case "$cur" in
            停著) stopped="$stopped $label" ;;
            定時異常) failed="$failed $label" ;;
          esac
        fi
      done <<<"$GUARD_BEFORE"
    fi
    [ -z "$lost$stopped$failed" ] && break
    i=$((i + 1))
    [ "$i" -ge "$GUARD_TRIES" ] && break
    sleep 1
  done
  if [ -z "$lost$stopped$failed" ]; then
    say "OK 要守的服務都還在：$(guard_summary "$now")"
    return 0
  fi
  GUARD_BROKEN=1
  say ""
  say "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
  say "✗✗ ${MACHINE} 上要守的服務（名稱符合「${GUARD_PAT}」）少了，這是底線（服務與網址不能斷），請馬上看："
  say "   部署前：$(guard_summary "$GUARD_BEFORE")"
  say "   現在：  $(guard_summary "$now")"
  [ -n "$lost" ] && say "   不見的：${lost# }"
  [ -n "$stopped" ] && say "   本來在跑、現在停了：${stopped# }"
  [ -n "$failed" ] && say "   定時服務上一次結束碼不是 0（本來是好的）：${failed# }"
  say "   部署工具沒有碰這些服務（只讀 launchd），換回 evergenie 也不會讓它們回來；"
  say "   在 ${MACHINE} 上看 launchctl list | grep -E '${GUARD_PAT}' 與那些服務自己的 log。"
  say "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
  return 1
}

# ---- 一台上的每個家 ---------------------------------------------------------------------------------
# load_homes：讀那台的家清單到 HOMES。沒有清單＝一個空字串（照舊的單一家，不另外指定 EVERGENIE_HOME）。dry-run 不讀，當成單一家並說明。
# 開發家（FIXED_HOME 有值）只有它自己那個家，不讀清單。
FIXED_HOME=""
load_homes() {
  HOMES=()
  local list line
  if [ -n "$FIXED_HOME" ]; then
    HOMES=("$FIXED_HOME")
    say "家：$FIXED_HOME"
    return 0
  fi
  if [ "$DRY" = 1 ]; then
    say "  [dry-run] 讀這台的家清單 ${HOMES_FILE_EXPR}（有的話每個家各自重開、各自檢查；沒有就是單一家）"
    HOMES=("")
    return 0
  fi
  list="$(rxo "讀這台的家清單" 'f="'"$HOMES_FILE_EXPR"'"; [ ! -f "$f" ] || sed -e "s/#.*//" -e "s/^[[:space:]]*//" -e "s/[[:space:]]*\$//" -e "s#^~/#$HOME/#" "$f" | grep -v "^\$" || true')" || return 1
  while IFS= read -r line; do
    [ -n "$line" ] && HOMES+=("$line")
  done <<<"$list"
  if [ "${#HOMES[@]}" = 0 ]; then
    HOMES=("")
  else
    say "這台有 ${#HOMES[@]} 個家：${HOMES[*]}"
  fi
}
# home_env <家>：在那台指定 EVERGENIE_HOME 的前綴；空字串＝單一家，不加。開頭是字面的 $HOME/ 時到那台才展開（rq）。
home_env() { if [ -n "$1" ]; then printf 'export EVERGENIE_HOME=%s; ' "$(rq "$1")"; fi; }
home_name() { if [ -n "$1" ]; then printf '（家 %s）' "$1"; fi; }

# restart_all：每個家各自重開服務；有一個失敗就回 1（其他家照樣重開，不留半套）。
restart_all() {
  local h rc=0
  for h in "${HOMES[@]}"; do
    rx "重開服務$(home_name "$h")" "$(home_env "$h")evergenie service restart" || rc=1
  done
  return $rc
}

# preview_all：重開之前，每個家先用新版在複本上試跑資料整理（DEV-154）；有一個沒過就回 1（真的家都還沒動）。
preview_all() {
  local h rc=0
  for h in "${HOMES[@]}"; do
    rx "在複本上試跑資料整理$(home_name "$h")" "$(home_env "$h")evergenie migrate-home --preview" || rc=1
  done
  return $rc
}

# 部署時的資料保護（DEV-154，internal/feature/system/update/deploy.go）：重開之前每個家先備份（begin）；沒過要換回時，
# 這段期間整理過資料的家用備份還原（undo）；檢查過了收尾（done：備份留著當最新一份、更早的收掉〔UPD-16〕；有整理資料就記一筆、可以連資料退回；不用請主人確認）。
# 三個都用新編好的 bin/ 跑，所以 undo 要在把 bin/ 換回去之前做。BACKED=1 表示這次 begin 過。
BACKED=0
backup_all() { # <舊版本>
  local h rc=0
  BACKED=1
  for h in "${HOMES[@]}"; do
    rx "備份家$(home_name "$h")" "$(home_env "$h")evergenie upgrade --deploy begin --from $(shq "${1:-上一版}")${OLD_COMMIT:+ --from-commit $OLD_COMMIT}" || rc=1
  done
  return $rc
}
undo_all() {
  local h rc=0
  [ "$BACKED" = 1 ] || return 0
  BACKED=0
  for h in "${HOMES[@]}"; do
    rx "整理過資料的話還原到部署前$(home_name "$h")" "$(home_env "$h")evergenie upgrade --deploy undo" || rc=1
  done
  return $rc
}
done_all() {
  local h
  BACKED=0
  for h in "${HOMES[@]}"; do
    rx "部署收尾$(home_name "$h")" "$(home_env "$h")evergenie upgrade --deploy done" || say "  （收尾沒做成，不算部署失敗；下次部署會自己收）"
  done
}

# report_all <舊版本>：部署成功後，主人自己的套件有載入問題的家，寫一份升級報告交給管電腦的秘書；傳不出去不算部署失敗。
report_all() {
  local h
  for h in "${HOMES[@]}"; do
    rx "升級報告$(home_name "$h")" "$(home_env "$h")evergenie upgrade --report --only-problems --from $(shq "${1:-上一版}")" 2>&1 | sed 's/^/  /' || true
  done
}

# check_all <預期版本>：每個家各自跑部署後檢查，輸出加縮排；有一個沒過就回 1。
check_all() {
  local h out rc all=0
  for h in "${HOMES[@]}"; do
    [ -n "$h" ] && say "  家 ${h}："
    out="$(rxo "檢查$(home_name "$h")" "$(home_env "$h")$(check_script "$1")" 2>&1)"; rc=$?
    printf '%s
' "$out" | sed 's/^/  /'
    [ "$rc" = 0 ] || all=1
  done
  return $all
}

# 自動換回：換 bin、（要的話）重開服務、看一眼 doctor。只負責做和說；換不回去記在 SWAP_FAILED（結束碼 3）。
swap_back() {
  local restart="$1" out rc
  say "→ 自動換回上一版："
  if ! undo_all; then
    say "✗ 有家的資料還原沒做成（備份還在，上面寫了路徑）。請人工處理後再重開。"
    SWAP_FAILED=1
  fi
  out="$(rxo "換回上一版" "$SWAP" 2>&1)"; rc=$?
  printf '%s\n' "$out" | sed 's/^/  /'
  if [ "$rc" != 0 ]; then
    say "✗ 換不回去。請人工處理：到 $MACHINE 的 repo 看 bin/ 與 bin.prev/，必要時 git checkout 上一版再 make build，然後 evergenie service restart。"
    SWAP_FAILED=1
    return 1
  fi
  if [ "$restart" = yes ]; then
    if restart_all; then
      local h
      for h in "${HOMES[@]}"; do
        out="$(rxo "舊版的狀況$(home_name "$h")" "$(home_env "$h")evergenie doctor 2>&1 | tail -3" 2>&1)"
        say "  舊版已重開$(home_name "$h")。doctor 摘要："
        printf '%s\n' "$out" | sed 's/^/    /'
      done
    else
      say "✗ 換回舊版後重開服務也失敗，請人工處理：evergenie service status、暫存資料夾的 logs/serve.err.log（第 1 版 <家>/runtime/，第 2 版在狀態資料夾的 runtime/）"
      SWAP_FAILED=1
      return 1
    fi
  fi
  say "  原因查清楚再部署；main 不用 revert。壞掉的那份編譯結果留在 bin.failed/。"
}

# ---- 一台的部署 -------------------------------------------------------------------------------------
deploy_one() {
  local old new deployed out rc
  if ! rx "確認 repo 沒有沒提交的改動" 'git diff --quiet && git diff --cached --quiet || { echo "repo 裡有沒提交的改動，先不部署：請先看 git status"; exit 1; }'; then
    fail_msg "連不上 ${MACHINE}、找不到 repo，或 repo 有沒提交的改動；沒動任何東西"
    return 1
  fi
  old="$(rxo "記下現在的 commit" 'git rev-parse --short HEAD')" || { fail_msg "讀不到 repo 的 commit"; return 1; }
  OLD_COMMIT="$(rxo "記下現在的 commit（完整）" 'git rev-parse HEAD' 2>/dev/null)"
  load_homes || { fail_msg "讀不到這台的家清單（${HOMES_FILE_EXPR}）"; return 1; }
  if ! rx "抓最新的 main，只接受快轉（不會產生合併 commit）" 'git fetch origin && git merge --ff-only origin/main'; then
    fail_msg "git fetch／merge --ff-only 失敗（那台的 main 跟 origin 分岔了，或連不上 origin）；沒動 bin/"
    return 1
  fi
  # mini 開發家要換成同一個 commit（mini_and_dev）
  DEPLOYED_COMMIT="$(rxo "這次要部署的 commit（完整）" 'git rev-parse HEAD' 2>/dev/null)"
  new="$(rxo "這次要部署的版本（make build 用同一支 scripts/version.sh 當版本）" 'scripts/version.sh')" || { fail_msg "讀不到版本"; return 1; }
  deployed="$(rxo "上次成功部署的版本" 'cat bin/.deployed-version 2>/dev/null || true')"
  if [ "$DRY" = 0 ] && [ "$FORCE" = 0 ] && [ -n "$new" ] && [ "$new" = "$deployed" ]; then
    say "已經是最新版（${new}）：不重做，bin.prev/ 保持是上一版。要照做請加 --force。"
    return 0
  fi
  say "版本：${deployed:-（不知道）} → ${new:-（dry-run 不查）}（原本的 commit ${old:-?}）"
  build_restart_check "$new" "$deployed" stamp
}

# build_restart_check <新版本> <舊版本> <stamp|nostamp>：留 bin.prev/ → make build → 每個家重開 → 檢查；沒過就換回。
# stamp：成功後寫 bin/.deployed-version（部署 checkout 用）；nostamp：不寫（開發家的 repo 也是開發用的資料夾，
# 別人 make build 不會更新這個檔，留著反而讓「換回後該有的版本」對錯），bin.prev/ 裡的舊記號也拿掉。
build_restart_check() {
  local new="$1" deployed="$2" stamp="$3" keep='rm -rf bin.prev && cp -R bin bin.prev'
  [ "$stamp" = stamp ] || keep="$keep && rm -f bin.prev/.deployed-version"
  if ! rx "舊 bin/ 留成 bin.prev/（退路）" 'if [ -d bin ]; then '"$keep"' && echo "已留下 bin.prev/"; else echo "還沒有 bin/（第一次部署）：沒有可留的舊版"; fi'; then
    fail_msg "留不下 bin.prev/；沒動 bin/"
    return 1
  fi
  if ! rx "編譯" 'make build'; then
    fail_msg "make build 失敗（編不過）；服務還沒重開"
    swap_back no
    return 1
  fi
  if ! preview_all; then
    fail_msg "新版的資料整理在複本上試跑沒過；家裡的資料沒有動，服務還沒重開"
    swap_back no
    return 1
  fi
  if ! backup_all "$deployed"; then
    fail_msg "重開之前的備份沒做成；家裡的資料沒有動，服務還沒重開"
    swap_back no
    return 1
  fi
  if ! restart_all; then
    fail_msg "evergenie service restart 失敗"
    swap_back yes
    return 1
  fi
  say "部署後檢查："
  if [ "$DRY" = 1 ]; then
    say "  [dry-run] 檢查：看板首頁 200 且 bundle 跟 internal/base/app/board/dist 一致、evergenie doctor 沒有紅字且核心版本＝scripts/version.sh、evergenie packages list 有套件"
    say "  [dry-run] 任何一項沒過：bin.prev/ 換回 bin/、evergenie service restart、結束碼 1"
    [ "$stamp" = stamp ] && rx "成功後記下版本" 'scripts/version.sh > bin/.deployed-version'
    return 0
  fi
  if ! check_all "$new"; then
    fail_msg "部署後檢查沒過（上面標 FAIL 的）"
    swap_back yes
    return 1
  fi
  done_all
  report_all "$deployed"
  [ "$stamp" = stamp ] && { rx "記下這次成功的版本" 'scripts/version.sh > bin/.deployed-version' >/dev/null || true; }
  out="$(rxo "這次部署的 commit" 'git log -1 --format="%h %s"' 2>/dev/null)"
  say ""
  say "✓ $MACHINE 部署完成：${deployed:-（不知道）} → ${new}${out:+（${out}）}。退路：scripts/deploy.sh --rollback $(target_word)"
}

# ---- 開發家（devhome_conf 那幾個：工坊 D-53、mini 開發家 DEV-157）------------------------------------------
# 開發家跑的是開發用 repo 的 bin/（不是部署 checkout），不在 ~/.evergenie-homes 裡，要另外換：那個 repo 在 main、沒有沒提交的改動
# 才 fetch＋快轉、然後跟正式家同一套 build_restart_check（留 bin.prev/、make build、重開、檢查、沒過自動換回；不寫 .deployed-version）。
# 在別的分支或有別人做到一半的改動就不動（照舊跑原本的版本），說清楚為什麼。已經跑著這一版（doctor 的核心版本＝scripts/version.sh）
# 就不重開；--force 照做。
# 跟著正式家換版的（DEV_FOLLOWS=1，mini 開發家）另外：
#   - repo、家的 home.toml 不在，或服務沒在跑：跳過、說一聲（不算失敗）。
#   - 沒追蹤的新檔也算改動（make build 會把它編進去）；main 上有 origin 沒有的 commit 也不動。
#   - 換成 DEV_TARGET：跟著 mini 部署時是正式家剛部署的同一個 commit；單獨跑 mini-dev 時是 origin/main。
#   - 換版前後各傳一句話給 DEV_NOTIFY（沒換成也說一句原因）；傳不到不算失敗。
DEV_TARGET=""

# dev_here_cmd：在那台印一行「HGDEV OK」、「HGDEV SKIP <跳過的白話原因>」或「HGDEV BAD <不能換的原因>」（一律結束碼 0；
# 連不上那台才會是別的結束碼）。開頭的記號是為了跟那台登入殼層、ssh 自己印的字分開：認不出這一行就當成沒確認到，不當成跳過。
# BAD：`service status` 說這個服務管的是另一個家（開發家的 home.toml 沒設自己的 [runtime] service_label 時，重開的會是別的家）。
dev_here_cmd() {
  printf '%sr=%s; h=%s; ' "$PREFIX" "$(rq "$DEV_REPO")" "$(rq "$DEV_HOME")"
  cat <<'EOS'
if [ ! -e "$r/.git" ]; then echo "HGDEV SKIP 沒有它的 repo（$r）"
elif [ ! -f "$h/home.toml" ]; then echo "HGDEV SKIP 沒有這個家（$h/home.toml）"
else
  st="$(cd "$r" && EVERGENIE_HOME="$h" evergenie service status 2>&1)"
  case "$st" in
    *"跟你現在指定的家"*) echo "HGDEV BAD evergenie service status 說這個服務管的是另一個家：$(printf '%s\n' "$st" | grep '注意' | head -1)" ;;
    *"在跑，pid"*) echo "HGDEV OK" ;;
    *) echo "HGDEV SKIP 服務沒在跑（evergenie service status：$(printf '%s\n' "$st" | head -1)）" ;;
  esac
fi
EOS
}

# dev_notify <內容>：用那個家的 evergenie say 傳一句話給 DEV_NOTIFY（開頭報「部署工具：」）；沒設、傳不到都不算部署失敗。
dev_notify() {
  [ "$DEV_FOLLOWS" = 1 ] && [ -n "$DEV_NOTIFY" ] || return 0
  if ! rx "傳話給 ${DEV_NOTIFY}" "$(home_env "$DEV_HOME")evergenie say $(shq "$DEV_NOTIFY") $(shq "部署工具：$1")" 2>&1 | sed 's/^/  /'; then
    say "  （傳不到 ${DEV_NOTIFY}，不算部署失敗）"
  fi
}

# dev_stop <給人看的一句> <給秘書的原因> <跑著的版本>：這次不換（照舊跑原本的版本）；跟著正式家的再傳一句話說原因與該怎麼做。
dev_stop() {
  local again="等那邊收好再跑 scripts/deploy.sh ${TWORD}"
  [ "$DEV_FOLLOWS" = 1 ] && again="收好之後下次部署 mini 會自己換（或跑 scripts/deploy.sh ${TWORD}）"
  fail_msg "${1}，照舊跑原本的版本；${again}"
  dev_notify "${DEV_NAME}這次沒換版，照舊跑 ${3:-原本的版本}。原因：${2}。正在跑的這份不要直接改：開分支、commit、把這份切回 main、告訴 admin；收好之後下次部署會自己換。"
}

devhome_one() {
  local new running here clean t rc why
  if [ "$DEV_FOLLOWS" = 1 ]; then
    if [ "$DRY" = 1 ]; then
      say "  [dry-run] 確認${DEV_NAME}在：repo ${DEV_REPO}、家 ${DEV_HOME}/home.toml、evergenie service status 在跑；任何一個不是就跳過（不算失敗）"
    else
      here="$(exec_on "$(dev_here_cmd)" 2>&1)" || { fail_msg "連不上 ${HOST}，沒辦法確認${DEV_NAME}：${here}"; return 1; }
      here="$(printf '%s\n' "$here" | sed -n 's/^HGDEV //p' | tail -1)"
      case "$here" in
        OK) ;;
        "SKIP "*)
          say "${DEV_NAME}這次跳過（不算失敗）：${here#SKIP }"
          return 0
          ;;
        "BAD "*)
          fail_msg "${DEV_NAME}現在不能換：${here#BAD }。請先在它的 home.toml 設好 [runtime] service_label 並 evergenie service install；沒動任何東西"
          return 1
          ;;
        *)
          fail_msg "沒辦法確認${DEV_NAME}在不在（那台沒有照預期回答）；沒動任何東西"
          return 1
          ;;
      esac
    fi
  fi
  load_homes
  running="$(rxo "${DEV_NAME}現在跑的版本（doctor 的核心版本）" "$(home_env "$FIXED_HOME")"'evergenie doctor 2>&1 | sed -n "s/.*核心版本：//p" | head -1 | sed "s/[[:space:]]*\$//"')"
  clean='n='"$(shq "$DEV_NAME")"'; b="$(git rev-parse --abbrev-ref HEAD)"; [ "$b" = main ] || { echo "${n}的 repo 現在在分支 ${b}，不是 main：別人可能正在用，先不動"; exit 1; }; '
  if [ "$DEV_FOLLOWS" = 1 ]; then
    clean+='s="$(git status --porcelain)"; [ -z "$s" ] || { echo "${n}的 repo 有 $(printf "%s\n" "$s" | wc -l | tr -d " ") 個檔沒 commit（含沒追蹤的新檔，可能是別人做到一半的）："; printf "%s\n" "$s" | head -10; exit 1; }'
  else
    clean+='git diff --quiet && git diff --cached --quiet || { echo "${n}的 repo 有沒提交的改動（可能是別人做到一半的）："; git status --short --untracked-files=no | head -10; exit 1; }'
  fi
  if ! rxs "確認${DEV_NAME}的 repo 在 main、沒有沒提交的改動（那是開發用的資料夾，別人可能正在用）" "$clean"; then
    why="$(printf '%s\n' "$RX_OUT" | head -1)"
    dev_stop "${DEV_NAME}的 repo 現在不能動（上面寫了原因）" "${why%：}" "$running"
    return 1
  fi
  OLD_COMMIT="$(rxo "記下現在的 commit（完整）" 'git rev-parse HEAD' 2>/dev/null)"
  if [ "$DEV_FOLLOWS" = 1 ]; then
    t="${DEV_TARGET:-origin/main}"
    if [ "$DRY" = 1 ] && [ -z "$DEV_TARGET" ] && [ "$TARGET" != mini-dev ]; then
      say "  （跟著 mini 部署時換成 mini 正式家這次部署的同一個 commit；dry-run 不查，下面先寫 origin/main）"
    fi
    rxs "抓最新的 main，快轉到 ${t}（main 上有 origin 沒有的 commit 就不動）" 't='"$(shq "$t")"'; n='"$(shq "$DEV_NAME")"'
git fetch -q origin || { echo "連不上 origin（git fetch 失敗）"; exit 1; }
git cat-file -e "$t^{commit}" 2>/dev/null || { echo "抓不到要換的 commit $t"; exit 1; }
if git merge-base --is-ancestor HEAD "$t"; then git merge --ff-only "$t"
elif git merge-base --is-ancestor HEAD origin/main; then echo "repo 已經比這次部署的 commit 還新（現在 $(git rev-parse --short HEAD)，有人手動拉過），不往回換、也不重編；要讓它跑 repo 現在這一版：scripts/deploy.sh mini-dev"; exit 3
else echo "${n}的 repo 的 main 上有 origin 沒有的 commit（有人直接在這份上 commit）："; git log --oneline origin/main..HEAD | head -10; exit 1; fi'
    rc=$?
    if [ "$rc" = 3 ]; then
      say "${DEV_NAME}這次不換（不算失敗）：$(printf '%s\n' "$RX_OUT" | head -1)"
      return 0
    fi
    if [ "$rc" != 0 ]; then
      why="$(printf '%s\n' "$RX_OUT" | head -1)"
      dev_stop "${DEV_NAME}沒有快轉（上面寫了原因）；沒動 bin/" "${why%：}" "$running"
      return 1
    fi
  elif ! rx "抓最新的 main，只接受快轉（不會產生合併 commit）" 'git fetch origin && git merge --ff-only origin/main'; then
    fail_msg "git fetch／merge --ff-only 失敗（${DEV_NAME}的 repo 的 main 跟 origin 分岔了，或連不上 origin）；沒動 bin/"
    return 1
  fi
  new="$(rxo "這次的版本" 'scripts/version.sh')" || { dev_stop "讀不到這次的版本（scripts/version.sh）；repo 已經快轉、沒動 bin/" "讀不到這次的版本" "$running"; return 1; }
  if [ "$DRY" = 0 ] && [ "$FORCE" = 0 ] && [ -n "$new" ] && [ "$new" = "$running" ]; then
    say "${DEV_NAME}已經跑著這一版（${new}）：不重開。要照做請加 --force。"
    return 0
  fi
  say "版本：${running:-（不知道）} → ${new:-（dry-run 不查）}"
  dev_notify "${DEV_NAME}要從 ${running:-原本的版本} 換成 ${new:-新版}，等一下會重開服務；檢查沒過會自動退回 ${running:-原本的版本}。"
  build_restart_check "$new" "$running" nostamp
  rc=$?
  if [ "$DRY" = 1 ]; then
    dev_notify "${DEV_NAME}已從 ${running:-原本的版本} 換成 ${new:-新版}，檢查都過了。（沒過時改說沒過的原因、退回了哪一版）"
  elif [ "$rc" = 0 ]; then
    dev_notify "${DEV_NAME}已從 ${running:-原本的版本} 換成 ${new}，檢查都過了。"
  elif [ "$SWAP_FAILED" = 1 ]; then
    dev_notify "${DEV_NAME}換成 ${new} 沒過（${LAST_FAIL%%（上面*}），而且退不回 ${running:-原本的版本}，要人處理；部署工具的輸出有細節。"
  else
    dev_notify "${DEV_NAME}換成 ${new} 沒過（${LAST_FAIL%%（上面*}），已自動退回 ${running:-原本的版本}。"
  fi
  return $rc
}

# ---- 手動換回 ---------------------------------------------------------------------------------------
rollback_one() {
  local want
  if ! rx "確認有 bin.prev/" '[ -d bin.prev ] || { echo "沒有 bin.prev/：這台還沒用 deploy.sh 部署過，或已經換回過一次"; exit 1; }'; then
    fail_msg "沒有可以換回的上一版"
    return 1
  fi
  load_homes || { fail_msg "讀不到這台的家清單（${HOMES_FILE_EXPR}）"; return 1; }
  # 上一次更新整理過資料格式的家（不管確認過沒有，資料都已是新格式）：只換程式的話舊版會讀不懂新格式的資料，要用會連資料一起退回的那條路。
  local h
  for h in "${HOMES[@]}"; do
    if ! rx "確認上次更新沒有整理過資料$(home_name "$h")" "$(home_env "$h")"'st="$(evergenie upgrade --status --json 2>/dev/null)" || st="$(./bin.prev/evergenie upgrade --status --json 2>/dev/null)" || { echo "查不到上次更新有沒有整理過資料（現在這版與上一版都跑不了 upgrade --status），先不換回"; exit 1; }; if printf "%s" "$st" | grep -q '"'"'"awaiting_accept":{[^}]*"migrations":\['"'"'; then echo "上次更新整理過資料格式：只換回程式的話舊版會讀不懂。請改用 evergenie upgrade --rollback（資料會一起回到更新前）"; exit 1; fi'; then
      fail_msg "上次更新整理過資料（或查不到有沒有整理過），不能只換回程式；沒動任何東西"
      return 1
    fi
  done
  if ! rx "換回上一版" "$SWAP"; then fail_msg "換回失敗"; SWAP_FAILED=1; return 1; fi
  if ! restart_all; then fail_msg "bin/ 已換回，但重開服務失敗"; SWAP_FAILED=1; return 1; fi
  if [ "$DRY" = 1 ]; then
    say "  [dry-run] 檢查：看板首頁、evergenie doctor（核心版本對照 bin/.deployed-version）、packages list"
    return 0
  fi
  want="$(rxo "換回後該有的版本" 'cat bin/.deployed-version 2>/dev/null || true')"
  if ! check_all "$want"; then
    fail_msg "換回上一版之後檢查也沒過（上面標 FAIL 的），要人處理"
    SWAP_FAILED=1
    return 1
  fi
  say ""
  say "✓ $MACHINE 已換回上一版${want:+（${want}）}。repo 還在新的 commit：下次 scripts/deploy.sh $(target_word) 會重新編新的。"
}

# ---- dry-run 說清楚會部署什麼（ADM-35）：只讀本機的 git 與檔案，不 ssh、不連網路、不 fetch ---------------------
# 本機的 origin/main 上次更新是多久以前：看它的 reflog（各 worktree 共用；fetch 有拿到新的 commit 才寫一筆）。
# 不看 FETCH_HEAD：那是每個 worktree 各一份，fetch 別的分支也會更新，會把舊的 origin/main 說成「剛 fetch」。
# 只說得出「至少這麼舊」：之後 fetch 過但沒有新 commit 就不會更新，所以措辭是「更新」不是「fetch」。沒有 reflog 就不說。
origin_age() {
  local t now m
  t="$(git -C "$1" log -g -1 --date=unix --format=%gd refs/remotes/origin/main 2>/dev/null | sed -n 's/.*@{\([0-9][0-9]*\)}$/\1/p')"
  [ -n "$t" ] || return 0
  now="$(date +%s)"; m=$(((now - t) / 60))
  [ "$m" -ge 0 ] || return 0
  if [ "$m" -lt 60 ]; then printf '%s 分鐘前更新過' "$m"
  elif [ "$m" -lt 2880 ]; then printf '上次更新是 %s 小時前' "$((m / 60))"
  else printf '上次更新是 %s 天前' "$((m / 1440))"; fi
}
# 版本字串（scripts/version.sh 印的）→ 本機 git 認得的 commit：X.Y.Z-N-g<短碼> 取短碼、X.Y.Z 取 tag vX.Y.Z、否則當短碼；去掉 -dirty
version_ref() {
  local v="${1%-dirty}"
  case "$v" in
    *-g*) printf '%s' "${v##*-g}" ;;
    [0-9]*.[0-9]*.[0-9]*) printf 'v%s' "$v" ;;
    *) printf '%s' "$v" ;;
  esac
}
# 這台上次部署的版本到 origin/main 之間有哪些 commit。只有本機那台（office）讀得到 bin/.deployed-version；別台要 ssh，dry-run 不看。
dry_pending() {
  local dir dv ref n
  if [ "$HOST" != local ]; then
    say "上次部署到 ${MACHINE} 的版本：dry-run 不連 ${MACHINE}，看不到（真的部署時會印「版本：舊 → 新」）"
    return 0
  fi
  dir="${REPO_EXPR/#\$HOME/$HOME}"; dir="${dir/#\~/$HOME}"
  dv="$(cat "$dir/bin/.deployed-version" 2>/dev/null)"
  if [ -z "$dv" ]; then
    say "上次部署到 ${MACHINE} 的版本：不知道（${dir}/bin/.deployed-version 讀不到）"
    return 0
  fi
  ref="$(version_ref "$dv")"
  if ! git -C "$SELF_REPO" rev-parse -q --verify "${ref}^{commit}" >/dev/null 2>&1; then
    say "上次部署到 ${MACHINE} 的版本：${dv}（本機 git 找不到這個 commit，算不出差幾個）"
    return 0
  fi
  n="$(git -C "$SELF_REPO" rev-list --count "${ref}..origin/main" 2>/dev/null)"
  case "$n" in
    '') say "上次部署到 ${MACHINE} 的版本：${dv}" ;;
    0)
      if [ "$(git -C "$SELF_REPO" rev-parse "${ref}^{commit}")" = "$(git -C "$SELF_REPO" rev-parse origin/main 2>/dev/null)" ]; then
        say "上次部署到 ${MACHINE} 的版本：${dv}，跟本機看到的 origin/main 一樣（真的跑會說已經是最新版、不重做）"
      else
        say "⚠ 上次部署到 ${MACHINE} 的版本 ${dv} 比本機看到的 origin/main 還新：本機的 origin/main 可能沒 fetch（真的部署時會先 fetch）。"
      fi
      ;;
    *)
      say "上次部署到 ${MACHINE} 的版本：${dv}；origin/main 比它多 ${n} 個 commit（最新的在上面，最多列 15 個）："
      git -C "$SELF_REPO" log --format='  %h %s' -n 15 "${ref}..origin/main" 2>/dev/null
      [ "$n" -le 15 ] || say "  …還有 $((n - 15)) 個"
      ;;
  esac
}

host_of() {
  case "$1" in
    mini) echo "$MINI_HOST" ;;
    office) echo "${EVERGENIE_DEPLOY_OFFICE_HOST:-local}" ;;
  esac
}

# 指令列上的名字（mini／office／workshop／mini-dev），給「退路：scripts/deploy.sh --rollback …」這類提示用；
# GWORD 是那台機器的名字（mini／office），給「EVERGENIE_DEPLOY_<機器>_GUARD」這類提示用。
TWORD="" GWORD=""
target_word() { printf '%s' "$TWORD"; }

run_target() {
  local rc
  TWORD="$1"
  FIXED_HOME=""
  case "$1" in
    workshop | mini-dev)
      devhome_conf "$1"
      MACHINE="$DEV_NAME" GWORD="$DEV_MACHINE"
      REPO_EXPR="$DEV_REPO" FIXED_HOME="$DEV_HOME"
      ;;
    *)
      MACHINE="$1" GWORD="$1"
      REPO_EXPR="$(repo_of "$1")"
      ;;
  esac
  HOST="$(host_of "$GWORD")"
  GUARD_PAT="$(guard_of "$GWORD")"
  if [ "$ROLLBACK" = 1 ]; then say "== ${MACHINE}（${HOST}）手動換回上一版 =="; else say "== ${MACHINE}（${HOST}）部署 =="; fi
  say "repo：${REPO_EXPR}"
  if [ "$DRY" = 1 ] && [ "$ROLLBACK" = 0 ] && [ -n "$SELF_ORIGIN" ]; then
    local age
    age="$(origin_age "$SELF_REPO")"
    say "會部署：origin/main（本機看到的是 ${SELF_ORIGIN}${age:+，${age}}；那台會自己 fetch，以那時的 origin/main 為準）"
    # 開發家不寫 bin/.deployed-version（ADM-38），沒有「上次部署的版本」可比
    [ -n "$FIXED_HOME" ] || dry_pending
  fi
  if ! guard_before; then
    fail_msg "讀不到 ${MACHINE} 的 launchctl list（連不上？），先不部署；沒動任何東西"
    return 1
  fi
  if [ "$ROLLBACK" = 1 ]; then
    rollback_one
  elif [ -n "$FIXED_HOME" ]; then
    devhome_one
  else
    deploy_one
  fi
  rc=$?
  guard_after
  return $rc
}

# 結束碼：換不回去（3）最優先，其次是要守的服務少了（4），再來是部署本身的結果
finish() {
  if [ "$SWAP_FAILED" = 1 ]; then exit 3; fi
  if [ "$GUARD_BROKEN" = 1 ]; then exit 4; fi
  exit "$1"
}

# mini 正式家部署成功、要守的服務也都在，才接著換 mini 開發家（主人 2026-11-14 定：每次部署 mini 都換，換成同一個 commit）。
# 回傳 mini 正式家的結果；開發家的結果放在 DEV_RC（沒換成不擋 office）。DEV_RAN=1 表示開發家那一段有跑（要守的服務之後才少的話，說法不同）。
DEV_RC=0 DEV_RAN=0
mini_and_dev() {
  run_target mini
  local rc=$?
  [ "$rc" = 0 ] && [ "$GUARD_BROKEN" = 0 ] || return $rc
  say ""
  if [ "$DRY" = 0 ] && [ -z "$DEPLOYED_COMMIT" ]; then
    # 不知道正式家部署的是哪個 commit 就不換：寧可不換，也不要讓開發家跑到跟正式家不同的版本
    say "✗ 讀不到 mini 正式家這次部署的 commit，mini 開發家這次不換（不影響其他家）。要補換：scripts/deploy.sh mini-dev"
    DEV_RC=1
    return 0
  fi
  DEV_TARGET="$DEPLOYED_COMMIT"
  DEV_RAN=1
  run_target mini-dev
  DEV_RC=$?
  if [ "$DEV_RC" != 0 ]; then
    say ""
    say "mini 正式家已經部署好了；mini 開發家這次沒換成（原因在上面），不影響其他家。"
  fi
  return 0
}

# office 部署成功後，有 --workshop 就接著做工坊；office 沒過就不碰工坊
office_then_workshop() {
  run_target office
  local rc=$?
  [ "$WORKSHOP" = 1 ] || return $rc
  say ""
  if [ "$rc" != 0 ]; then
    say "office 沒過，工坊沒有動。"
    return $rc
  fi
  run_target workshop
}

# 這次會動到的開發家；測試沒給它們的位置就整個不做（什麼都還沒動）
devhomes_touched() {
  case "$TARGET" in
    workshop | mini-dev) echo "$TARGET" ;;
    mini) [ "$ROLLBACK" = 1 ] || echo mini-dev ;;
    all) echo mini-dev ;;
  esac
  [ "$WORKSHOP" = 0 ] || echo workshop
}
for d in $(devhomes_touched); do
  devhome_refuse "$d" && exit 2
done

case "$TARGET" in
  all)
    mini_and_dev
    rc=$?
    if [ "$rc" != 0 ]; then
      say ""
      say "mini 沒過，office 沒有動（mini 開發家也沒有動）。"
      finish "$rc"
    fi
    if [ "$GUARD_BROKEN" = 1 ]; then
      say ""
      say "!!! mini 上要守的服務少了或停了（見上面 !!! 那段），$([ "$DEV_RAN" = 1 ] || printf 'mini 開發家沒有動、')office 沒有動：先處理 mini。"
      finish 4
    fi
    say ""
    office_then_workshop
    rc=$?
    [ "$rc" != 0 ] || rc="$DEV_RC"
    finish "$rc"
    ;;
  mini)
    [ "$ROLLBACK" = 0 ] || { run_target mini; finish $?; }
    mini_and_dev
    rc=$?
    if [ "$rc" != 0 ]; then
      say ""
      say "mini 沒過，mini 開發家沒有動。"
      finish "$rc"
    fi
    if [ "$GUARD_BROKEN" = 1 ]; then
      say ""
      if [ "$DEV_RAN" = 1 ]; then
        say "!!! mini 上要守的服務在換 mini 開發家的時候少了或停了（見上面 !!! 那段）：先處理 mini。"
      else
        say "!!! mini 上要守的服務少了或停了（見上面 !!! 那段），mini 開發家沒有動：先處理 mini。"
      fi
      finish 4
    fi
    finish "$DEV_RC"
    ;;
  office) office_then_workshop; finish $? ;;
  *) run_target "$TARGET"; finish $? ;;
esac
