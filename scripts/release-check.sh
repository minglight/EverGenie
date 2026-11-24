#!/usr/bin/env bash
# 部署前檢查，一個指令跑完（D-53，ai_docs/runbooks/release.md 步驟 3～5）。`make release-check` 叫它。
#
#   scripts/release-check.sh [--dry-run] [--full]
#
# 依序跑（每項經過重型鎖，別人在跑就排隊）：make web-ci → make ci → make deploy-test → make compat → make web-ci-full → make e2e-full；
# --full 再加 make release-e2e（給別的使用者的發版線：安裝、升級、退版，約 2 分鐘起）。
# 一項沒過也照跑下一項（一次看到全部問題），最後印一張白話摘要：每項過沒過、花多久、沒過的 log 尾巴與 log 位置，
# 以及「測的是不是要部署的那一版」（HEAD 跟 origin/main 一不一樣、有沒有沒提交的改動）。
# 不部署、不推、不碰任何家；畫面要人自己在開發家看過（release.md 步驟 3），工具代替不了，摘要會提醒。
#
# 環境變數（都可選）：
#   EVERGENIE_RELEASE_CHECK_LOGS   log 放哪（預設 ${TMPDIR:-/tmp}/hg-release-check-<時間>）
#   EVERGENIE_RELEASE_CHECK_STEPS  要跑的 make 目標，空白分隔（測試用；有設就不看 --full）
#   EVERGENIE_RELEASE_CHECK_MAKE   make 指令（預設 make；測試用）
#   EVERGENIE_RELEASE_CHECK_FETCH=0  不 git fetch origin main（離線、測試用；只拿本機看到的 origin/main 比）
# 結束碼：0 全過；1 有沒過的；2 用法錯誤。
set -uo pipefail
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它
cd "$(dirname "$0")/.." || exit 2
ROOT="$PWD"

DRY=0 FULL=0
for a in "$@"; do
  case "$a" in
    --dry-run) DRY=1 ;;
    --full) FULL=1 ;;
    -h | --help) sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "不認得的參數：${a}（用法：scripts/release-check.sh [--dry-run] [--full]）" >&2; exit 2 ;;
  esac
done

# 秘書 session 的環境會讓測試讀到正式家（CLAUDE.local／AGENTS 的老問題），這裡一律清掉
unset EVERGENIE_HOME EVERGENIE_ENV_FILE EVERGENIE_AGENT EVERGENIE_LAUNCH EVERGENIE_ENGINE_SOCKET EVERGENIE_ENGINE_BIN_DIR EVERGENIE_API_SOCKET EVERGENIE_SESSION EVERGENIE_SITE

steps="web-ci ci deploy-test compat web-ci-full e2e-full"
[ "$FULL" = 1 ] && steps="$steps release-e2e"
steps="${EVERGENIE_RELEASE_CHECK_STEPS:-$steps}"
MAKE_CMD="${EVERGENIE_RELEASE_CHECK_MAKE:-make}"

describe() {
  case "$1" in
    web-ci) echo "前端：打包、biome、typecheck、改到的 vitest、dist 跟 git 一致" ;;
    ci) echo "Go：gofmt、build、vet、go test -race、lint" ;;
    deploy-test) echo "部署工具的情境測試（假 ssh，不碰機器）" ;;
    compat) echo "相容：sdk 只加不刪、fixture 家與上一版的家開得起來" ;;
    web-ci-full) echo "前端全跑：biome、typecheck、全部 vitest" ;;
    e2e-full) echo "e2e：全部 spec（桌機＋手機 spec）" ;;
    release-e2e) echo "發版整套：打版號、安裝、升級、壞版退回、套件、角色包" ;;
    *) echo "make $1" ;;
  esac
}

if [ "$DRY" = 1 ]; then
  echo "（dry-run：只印會跑什麼）"
  i=0
  for s in $steps; do i=$((i + 1)); printf '  %d. %s %s　— %s\n' "$i" "$MAKE_CMD" "$s" "$(describe "$s")"; done
  echo "  另外：比對 HEAD 跟 origin/main、看有沒有沒提交的改動；最後印摘要。"
  exit 0
fi

LOGS="${EVERGENIE_RELEASE_CHECK_LOGS:-${TMPDIR:-/tmp}/hg-release-check-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$LOGS" || exit 2
fmt_dur() { local t="$1"; if [ "$t" -ge 60 ]; then printf '%d 分 %02d 秒' $((t / 60)) $((t % 60)); else printf '%d 秒' "$t"; fi; }
strip() { sed -e $'s/\x1b\\[[0-9;]*[A-Za-z]//g'; }

notes=() stale=0
if [ "${EVERGENIE_RELEASE_CHECK_FETCH:-1}" != 0 ]; then
  git fetch -q origin main 2>/dev/null || notes+=("⚠ 抓不到 origin/main（網路？）：下面拿本機看到的 origin/main 比")
fi

echo "部署前檢查：$(printf '%s ' $steps)（log 在 ${LOGS}）"
start_all=$(date +%s)
results=() failed=0
for s in $steps; do
  log="$LOGS/$s.log"
  printf '→ make %s（%s）… ' "$s" "$(describe "$s")"
  t0=$(date +%s)
  (cd "$ROOT" && $MAKE_CMD "$s") >"$log" 2>&1
  rc=$?
  dt=$(( $(date +%s) - t0 ))
  if [ "$rc" = 0 ]; then
    echo "過（$(fmt_dur "$dt")）"
    results+=("✓|$s|$dt|")
  else
    echo "沒過（結束碼 ${rc}，$(fmt_dur "$dt")）"
    results+=("✗|$s|$dt|$log")
    failed=$((failed + 1))
  fi
done
total=$(( $(date +%s) - start_all ))

# ---- 測的是不是要部署的那一版（跑完才看：web-ci 之類可能改到 dist）----------------------------------------
# 部署只拿 origin/main。HEAD＝origin/main 最好；HEAD 只比 origin/main 多（快轉推得上去）也算數：推上去之後部署的就是測過的這一版。
# origin/main 有 HEAD 沒有的 commit、或有沒提交的改動：測的跟要部署的不一樣，結論不給「可以部署」（結束碼 1）。
head_c="$(git rev-parse --short HEAD 2>/dev/null)"
origin_c="$(git rev-parse --short origin/main 2>/dev/null)"
if [ -z "$origin_c" ]; then
  notes+=("⚠ 找不到 origin/main：沒辦法確認測的是不是要部署的那一版")
  stale=1
elif [ "$head_c" = "$origin_c" ]; then
  notes+=("✓ 測的就是 origin/main（${origin_c}）＝ scripts/deploy.sh 會部署的那一版")
else
  ahead="$(git rev-list --count origin/main..HEAD 2>/dev/null)"
  behind="$(git rev-list --count HEAD..origin/main 2>/dev/null)"
  if [ "${behind:-1}" = 0 ]; then
    notes+=("✓ 測的是 HEAD ${head_c}（比 origin/main ${origin_c} 多 ${ahead} 個 commit）：推上去之後部署的就是這一版；推之前別再改")
  else
    notes+=("⚠ origin/main ${origin_c} 有 ${behind} 個 commit 是 HEAD ${head_c} 沒有的：部署會帶上沒測過的東西，先 rebase 到 origin/main 再測")
    stale=1
  fi
fi
if ! git diff --quiet || ! git diff --cached --quiet; then
  notes+=("⚠ 有沒提交的改動：測到的內容不會被部署（部署只拿 origin/main），先 commit 再測")
  stale=1
fi
echo
echo "== 部署前檢查摘要（共 $(fmt_dur "$total")）=="
for r in "${results[@]}"; do
  IFS='|' read -r mark s dt log <<<"$r"
  printf '%s make %-12s %s\n' "$mark" "$s" "$(fmt_dur "$dt")"
done
for r in "${results[@]}"; do
  IFS='|' read -r mark s dt log <<<"$r"
  [ "$mark" = "✗" ] || continue
  echo
  echo "--- make $s 沒過，log 最後幾行（全文 ${log}）---"
  strip <"$log" | grep -v '^[[:space:]]*$' | tail -12 | sed 's/^/  /'
done
echo
for n in "${notes[@]}"; do echo "$n"; done
echo "人要做的（工具代替不了）：改到看板、聊天顯示、通知內容這類主人看得到的東西，部署前要在開發家實際操作、截圖對照過（release.md 步驟 3）。"
echo
if [ "$failed" = 0 ] && [ "$stale" = 0 ]; then
  push=""
  [ "${ahead:-0}" != 0 ] && push="先把 HEAD 推上 origin main，再"
  echo "結論：自動檢查全過。畫面也看過的話就可以部署：${push}scripts/deploy.sh mini，過了再 scripts/deploy.sh --workshop office。"
  exit 0
fi
if [ "$failed" = 0 ]; then
  echo "結論：自動檢查全過，但測的不是要部署的那一版（看上面的 ⚠），先不要部署。"
  exit 1
fi
echo "結論：有 ${failed} 項沒過，先不要部署。修好再跑一次 make release-check（go test 與 web-ci-full 有快取，沒改的部分會快）。"
exit 1
