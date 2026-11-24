#!/usr/bin/env bash
# 發版整套測試（P8 第二輪 S5、REQ-285）：把「發版 → 安裝 → 升級 → 壞版退回 → 手動退回 → 套件 → 角色包 → nightly／stable」用真的指令在暫存資料夾走一遍。
# 結束碼 0＝每一段都過；任何一段沒過就停在那一段、結束 1（要看之後的段落請修好再跑；EVERGENIE_E2E_CONTINUE=1 會硬跑完剩下的段落）。
#
#   scripts/release-e2e.sh [--help]
#
# 真的 git、真的 make build、真的 serve（EVERGENIE_SERVICE_MODE=process：背景行程，不碰 launchd）。全程在 mktemp -d 的暫存資料夾：
# 一個 bare 的假 origin（只有這次的 v0.0.1／v0.0.2／v0.0.3 tag）、被測的程式（這個 repo 目前的工作樹，沒提交的改動也會測到）的 clone、
# 一個用安裝程式裝出來的新使用者的程式與家。不推任何東西、不碰任何正式家、不碰 tmux、不開任何秘書的對話。
#
# 各段（印「── 第 N 段：…」與 ✔／✘ 和秒數）：
#   0 建角色包（發行者的 fixture 家 → personas pack build）　1 做假 origin 與三個 tag　2 一鍵安裝 v0.0.1　3 檢查新版（目標 0.0.3）
#   4 升到指定版 v0.0.2（含角色包同步）　5 壞版 0.0.3 自動退回並跳過　6 手動退回 0.0.1　7 角色包沒網路與不退版
#   8 套件從網址安裝與更新　9 發行者流程 nightly／prepare／stable　10 make compat　11 看板按「現在更新」走的路（API）
#   12 有自己東西的家：升級前準備（改過的官方套件 fork、改過的角色卡、自己的角色、分享來的套件與它帶的秘書；DEV-35）
#   13 有自己東西的家：升級後（0.0.1 → 0.0.5）每樣都還在、官方新版只提醒不覆蓋、角色包新版不蓋改過的、分享的套件更新、再退版也在（DEV-35）
#   （執行順序：…10 → 12 → 11 → 13；12 要在升級前、13 要在升級後）
#
# 環境變數：EVERGENIE_E2E_KEEP=1 結束後保留暫存資料夾（除錯用）；EVERGENIE_E2E_TMP 暫存資料夾的位置（預設 /tmp；要夠短，macOS 的 socket 路徑上限約 104 字元）；
# EVERGENIE_E2E_CONTINUE=1 某一段沒過也繼續跑（預設停在第一個沒過的段落）；EVERGENIE_E2E_WAIT 等看板或服務的秒數（預設 180，機器忙的時候可以調大）。
set -uo pipefail
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
fi
if [ $# -gt 0 ]; then
  echo "不認得的選項：$1（--help 看用法）" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
B="$(mktemp -d "${EVERGENIE_E2E_TMP:-/tmp}/hge.XXXXXX")"
WAIT="${EVERGENIE_E2E_WAIT:-180}"
TODAY="$(date +%Y-%m-%d)"

# 乾淨的環境：拿掉屬於別的家／別的 session 的 EVERGENIE_*，再明確設定這次要用的（服務一律背景行程）。
CLEAN=(env -u EVERGENIE_HOME -u EVERGENIE_ENV_FILE -u EVERGENIE_AGENT -u EVERGENIE_LAUNCH -u EVERGENIE_ENGINE_SOCKET -u EVERGENIE_ENGINE_BIN_DIR -u EVERGENIE_API_SOCKET -u EVERGENIE_SESSION
  -u EVERGENIE_PERSONAS_PACK_POINTER -u EVERGENIE_OFFICIAL_PACKAGES_DIR -u EVERGENIE_PACKAGE_BIN_DIR -u EVERGENIE_RELEASE_REPO -u EVERGENIE_RELEASE_STATE -u EVERGENIE_RELEASE_STEPS
  EVERGENIE_SITE=e2e EVERGENIE_SERVICE_MODE=process EVERGENIE_BACKUP_DIR="$B/backup" GIT_TERMINAL_PROMPT=0)

cleanup() {
  local f p
  for f in "$B"/state/runtime/serve.pid "$B"/compat/*/runtime/serve.pid; do
    [ -f "$f" ] && { p="$(cat "$f" 2>/dev/null)"; [ -n "$p" ] && kill "$p" 2>/dev/null; }
  done
  # 保險：任何還在跑、指向暫存資料夾的 serve 都收掉
  pkill -f "serve --home $B/" 2>/dev/null
  sleep 1
  pkill -9 -f "serve --home $B/" 2>/dev/null
  if [ "${EVERGENIE_E2E_KEEP:-0}" = 1 ]; then echo "（保留暫存資料夾：${B}）"; else rm -rf "$B"; fi
}
trap cleanup EXIT
trap 'echo; echo "中斷了，收掉暫存的服務。"; exit 130' INT TERM

say() { printf '%s\n' "$*"; }
SEG_FAIL=0
OUT="" RC=0
# chk <說明> <判斷式>：判斷式用 eval 跑，可以用 ${OUT}（最近一次指令的輸出）與 ${RC}（結束碼）。
chk() {
  if eval "$2"; then
    say "  ✔ $1"
  else
    say "  ✘ $1"
    SEG_FAIL=1
    if [ -n "$OUT" ]; then
      say "      --- 最近一次指令的輸出（結束碼 ${RC}）---"
      printf '%s\n' "$OUT" | tail -25 | sed 's/^/      /'
    fi
  fi
}
# run <指令…>：跑指令，輸出存 ${OUT}、結束碼存 ${RC}。
run() { OUT="$("$@" 2>&1)"; RC=$?; }
freeport() { python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'; }
pyj() { python3 -c 'import json, sys
d = json.loads(sys.stdin.read())
print(eval(sys.argv[1]))' "$1"; }

CORE="$B/core" HOMEDIR="$B/home" PORT=""
# 新裝的家是新佈局（DEV-154）：家裡只有主人的東西；暫存、升級紀錄、官方角色與角色包登記在家外面的狀態資料夾。
STATE="$B/state"
hg() { "${CLEAN[@]}" EVERGENIE_HOME="$HOMEDIR" "$CORE/bin/evergenie" "$@"; }
api() { curl -sS -m 30 --unix-socket "$STATE/runtime/sockets/api.sock" -X POST -H 'Content-Type: application/json' "http://x/api/system/$1" -d "$2" 2>&1; }
board() { curl -s -o /dev/null -m 5 -w '%{http_code}' "http://127.0.0.1:$PORT/" 2>/dev/null; }
wait_board() {
  local i
  for ((i = 0; i < WAIT; i++)); do
    [ "$(board)" = 200 ] && return 0
    sleep 1
  done
  return 1
}
doctor_json() { hg doctor --json 2>/dev/null; }
bin_version() { hg version 2>/dev/null; }
# 跑著的服務自己回報的核心版本（doctor 的「核心版本：X」；服務沒起來就是空的）。跟 bin_version 一起看，才知道「檔案換了」和「服務真的換了」都對。
core_version() {
  doctor_json | python3 -c 'import json, sys
try:
    d = json.loads(sys.stdin.read())
    print([c["detail"] for c in d["checks"] if c["detail"].startswith("核心版本：")][0].split("：", 1)[1])
except Exception:
    print("")'
}
versions() { echo "執行檔 $(bin_version)／服務 $(core_version)"; }
# 家裡每個檔的「雜湊  路徑」清單（排序過）：比對升級、角色包同步前後家裡的檔一個都沒動（DEV-154 第 1 件）。
# 不算寄給主人的訊息（「升級完成」通知是主人的信，vault/messages/ 與發編號的 .counters.toml）。
# 不算的三處：寄給主人的升級通知（vault/messages/ 與編號計數）、衣櫃自己每輪重算的點數檢視（vault/pkg/closet/state/，裡面有計算時間，服務在跑就會變）。
home_list() { (cd "$HOMEDIR" && find . -type f ! -path './vault/messages/*' ! -path './vault/.counters.toml' ! -path './vault/pkg/closet/state/*' | LC_ALL=C sort | while read -r f; do shasum -a 256 "$f"; done); }
tree_sum() { (cd "$1" && find . -type f | LC_ALL=C sort | while read -r f; do shasum -a 256 "$f"; done | shasum -a 256 | cut -d' ' -f1); }
git_ok() { git -C "$1" config user.email e2e@example.com && git -C "$1" config user.name e2e; }

# ---- 各段 ----------------------------------------------------------------------------------------------------

seg0() { # 建角色包（發行者的家沒有服務在跑：加 --no-records，只有圖與卡；帶紀錄的包在 clean-install-test.sh 驗，PRS-4）
  cp -R "$ROOT/cmd/evergenie/testdata/home" "$B/pub" || { SEG_FAIL=1; return; }
  python3 - "$B/pub/home.toml" <<'PY'
import sys
p = sys.argv[1]
t = open(p, encoding="utf-8").read()
assert "[admin]" not in t, "fixture 的 home.toml 已經有 [admin]"
t = t.rstrip("\n") + '\n\n[admin]\npublic_personas = ["helper", "builder"]\n'
open(p, "w", encoding="utf-8").write(t)
PY
  mkdir -p "$B/pub/personas/avatars"
  printf 'png-helper' >"$B/pub/personas/avatars/helper.png"
  printf 'png-builder' >"$B/pub/personas/avatars/builder.png"
  printf 'png-adult' >"$B/pub/personas/avatars/helper@adult.png" # 隱藏版：不能進包（REQ-135）
  OUT="$(cd "$ROOT" && "${CLEAN[@]}" go run ./cmd/evergenie personas pack build --home "$B/pub" --out "$B/pack" --version 2026.10.29 --no-records --json 2>"$B/gorun.err")"; RC=$?
  [ $RC = 0 ] || OUT="$(cat "$B/gorun.err")"
  chk "personas pack build 成功" '[ $RC = 0 ]'
  [ $RC = 0 ] || return
  PACK_VERSION="$(pyj 'd["version"]' <<<"$OUT")"
  PACK_SHA="$(pyj 'd["manifest_sha256"]' <<<"$OUT")"
  PACK_URL="file://$B/pack/m/$PACK_VERSION.json"
  chk "記下 manifest 的 sha256（跟檔案實際的 sha256 一樣）" '[ "$PACK_VERSION" = 2026.10.29 ] && [ "$PACK_SHA" = "$(shasum -a 256 "$B/pack/m/$PACK_VERSION.json" | cut -d" " -f1)" ]'
  OUT="$(python3 - "$B/pack/m/$PACK_VERSION.json" <<'PY'
import json, sys
paths = [f["path"] for f in json.load(open(sys.argv[1]))["files"]]
print("\n".join(paths))
PY
)"
  chk "包裡有兩位公開角色的卡與頭像" 'grep -qx "cards/helper.md" <<<"$OUT" && grep -qx "cards/builder.md" <<<"$OUT" && grep -qx "avatars/helper.png" <<<"$OUT"'
  chk "包裡沒有隱藏版" '! grep -qi adult <<<"$OUT"'
}

seg1() { # 假 origin
  git clone -q --no-tags "$ROOT" "$B/work" || { SEG_FAIL=1; return; }
  git_ok "$B/work"
  # 工作樹裡還沒提交的改動也要測到：疊上去，成一個 e2e 的底
  local f n=0
  while IFS= read -r -d '' f; do
    case "$f" in bin/* | bin.prev/* | bin.failed/* | .claude/*) continue ;; esac
    if [ -e "$ROOT/$f" ] || [ -L "$ROOT/$f" ]; then
      mkdir -p "$B/work/$(dirname "$f")" && cp -P "$ROOT/$f" "$B/work/$f"
    else
      rm -f "$B/work/$f"
    fi
    n=$((n + 1))
  done < <(cd "$ROOT" && { git ls-files -z -m; git ls-files -z -o --exclude-standard; } | sort -zu)
  (cd "$B/work" && git add -A && { git diff --cached --quiet || git commit -q -m "chore: e2e snapshot of the working tree"; })
  say "  被測的程式：$(git -C "$B/work" rev-parse --short HEAD)（工作樹有 $n 個沒提交的檔案疊在上面）"
  git init -q --bare -b main "$B/origin.git" || { SEG_FAIL=1; return; }
  (
    cd "$B/work" || exit 1
    git checkout -q -B main
    git remote remove origin
    git remote add origin "$B/origin.git"
    # 一個測試用的官方資料套件：v0.0.1 是 1.0.0、v0.0.2 起是 1.1.0（給「使用者改過的官方套件，核心升級後會怎樣」用，DEV-35）
    mkdir -p packages/official/e2e-official
    cat >packages/official/e2e-official/package.toml <<'TOML'
schema = 1
id = "e2e-official"
name = "e2e 官方套件"
version = "1.0.0"
description = "發版整套測試用的官方資料套件"
kind = "data"
api = "evergenie.v1"
TOML
    printf '# e2e 官方套件\n官方第一行\n官方第二行\n' >packages/official/e2e-official/README.md
    git add -A && git commit -q -m "chore: e2e official package 1.0.0"
    git tag -a v0.0.1 -m "e2e 0.0.1"
    sed -i.bak 's/^version = "1.0.0"/version = "1.1.0"/' packages/official/e2e-official/package.toml && rm -f packages/official/e2e-official/package.toml.bak
    printf '官方第三行（0.0.2 新增）\n' >>packages/official/e2e-official/README.md
    printf 'version = "%s"\nurl = "%s"\nsha256 = "%s"\n' "$PACK_VERSION" "$PACK_URL" "$PACK_SHA" >packages/official/personas/pack.toml
    git commit -q -am "chore: e2e 0.0.2 points at the persona pack" && git tag -a v0.0.2 -m "e2e 0.0.2"
    # 0.0.3：編得過、開不起來
    python3 - <<'PY'
p = "cmd/evergenie/serve.go"
s = open(p, encoding="utf-8").read()
head = "func serve(args []string, out, errOut io.Writer) int {\n"
assert head in s, "找不到 serve 的進入點"
open(p, "w", encoding="utf-8").write(s.replace(head, head + "\tos.Exit(3) // e2e: 故意壞掉的版本\n", 1))
PY
    git commit -q -am "chore: e2e 0.0.3 is broken on purpose" && git tag -a v0.0.3 -m "e2e 0.0.3 (broken)"
    git push -q origin main v0.0.1 v0.0.2 v0.0.3
  ) || { SEG_FAIL=1; return; }
  OUT="$(git -C "$B/origin.git" tag --list | tr '\n' ' ')"
  chk "假 origin 有三個 tag（v0.0.1 v0.0.2 v0.0.3）" '[ "$OUT" = "v0.0.1 v0.0.2 v0.0.3 " ]'
  OUT="$(git -C "$B/origin.git" show v0.0.2:packages/official/personas/pack.toml)"
  chk "v0.0.2 的角色包指標指到第 0 段的包" 'grep -q "$PACK_SHA" <<<"$OUT" && grep -q "$PACK_URL" <<<"$OUT"'
  OUT="$(git -C "$B/origin.git" show v0.0.3:cmd/evergenie/serve.go | head -22)"
  chk "v0.0.3 的 serve 一開始就 os.Exit(3)" 'grep -q "os.Exit(3)" <<<"$OUT"'
}

seg2() { # 一鍵安裝
  PORT="$(freeport)"
  OUT="$(cd "$B" && "${CLEAN[@]}" EVERGENIE_INSTALL_BOARD_WAIT="$WAIT" "$ROOT/scripts/install.sh" --from-source --skip-claude-check --repo "file://$B/origin.git" --ref v0.0.1 --dir "$CORE" --home "$HOMEDIR" --state "$STATE" --name 測試 --port "$PORT" 2>&1)"; RC=$?
  chk "install.sh --ref v0.0.1 成功、印「裝好了」與看板網址" '[ $RC = 0 ] && grep -q "裝好了" <<<"$OUT" && grep -q "http://127.0.0.1:$PORT/" <<<"$OUT"'
  [ $RC = 0 ] || return
  chk "看板首頁 200" '[ "$(board)" = 200 ]'
  OUT="$(doctor_json)"; RC=$?
  chk "doctor 的核心版本是 0.0.1" 'pyj '"'"'[c["detail"] for c in d["checks"] if c["detail"].startswith("核心版本")][0]'"'"' <<<"$OUT" | grep -qx "核心版本：0.0.1"'
  chk "doctor 沒有紅字" '[ "$(pyj '"'"'len([c for c in d["checks"] if c["severity"] == "error"])'"'"' <<<"$OUT")" = 0 ]'
  chk "安裝出來的家不是發行者、頻道是 release" 'grep -q "channel = \"release\"" "$HOMEDIR/home.toml"'
  chk "新裝的家是新佈局：家裡沒有 runtime/、secrets/，personas/ 只有 mine/" '[ ! -e "$HOMEDIR/runtime" ] && [ ! -e "$HOMEDIR/secrets" ] && [ "$(ls "$HOMEDIR/personas")" = mine ] && [ -d "$STATE/runtime/logs" ]'
}

seg3() { # 檢查新版
  run hg upgrade --check --json
  chk "upgrade --check --json 成功" '[ $RC = 0 ]'
  chk "目標是最大的版本 0.0.3（tag v0.0.3）" '[ "$(pyj '"'"'d["target"] + " " + d["tag"]'"'"' <<<"$OUT")" = "0.0.3 v0.0.3" ]'
  chk "現在的版本 0.0.1、頻道 release" '[ "$(pyj '"'"'d["current"] + " " + d["channel"]'"'"' <<<"$OUT")" = "0.0.1 release" ]'
}

seg4() { # 升到指定版
  HOME_BEFORE="$(home_list)"
  run hg upgrade --apply --to v0.0.2
  chk "upgrade --apply --to v0.0.2 成功（結束碼 0）" '[ $RC = 0 ]'
  [ $RC = 0 ] || return
  chk "版本是 0.0.2（執行檔與跑著的服務都是）" '[ "$(versions)" = "執行檔 0.0.2／服務 0.0.2" ]'
  chk "看板 200" 'wait_board'
  chk "history.jsonl 一筆、升級成功、0.0.1 → 0.0.2" '[ "$(wc -l <"$STATE/runtime/upgrade/history.jsonl" | tr -d " ")" = 1 ] && [ "$(pyj '"'"'d["kind"] + d["from"] + d["to"] + str(d["ok"])'"'"' <"$STATE/runtime/upgrade/history.jsonl")" = "upgrade0.0.10.0.2True" ]'
  chk "角色包已同步：登記的版號是這個包的版號" 'grep -q "^version = \"$PACK_VERSION\"" "$STATE/personas/pack-registry.toml"'
  chk "personas/ 有那幾個檔（兩張卡、兩個頭像），沒有隱藏版" '[ -f "$STATE/personas/helper.md" ] && [ -f "$STATE/personas/builder.md" ] && [ -f "$STATE/personas/avatars/helper.png" ] && [ -f "$STATE/personas/avatars/builder.png" ] && [ ! -e "$STATE/personas/avatars/helper@adult.png" ]'
  chk "備份在升級前做了" 'ls "$STATE"/runtime/backups/upgrade-0.0.1-0.0.2.tgz >/dev/null 2>&1'
  OUT="$(diff <(printf '%s\n' "$HOME_BEFORE") <(home_list))"
  chk "升級與角色包同步前後，家裡的檔案清單與雜湊完全相同（官方的東西不寫進家裡；寄給主人的升級通知、衣櫃重算的點數檢視不算）" '[ -z "$OUT" ]'
  chk "官方角色與衣服的紀錄不在家裡：家裡的 personas/ 還是只有 mine/" '[ "$(ls "$HOMEDIR/personas")" = mine ]'
  PERSONAS_SUM="$(tree_sum "$STATE/personas")"
  PACK_REG_SUM="$(shasum -a 256 "$STATE/personas/pack-registry.toml" | cut -d' ' -f1)"
}

seg5() { # 壞版自動退回
  run hg upgrade --apply
  chk "upgrade --apply（目標 0.0.3）失敗，結束碼 1" '[ $RC = 1 ]'
  chk "輸出說已換回 0.0.2" 'grep -q "已換回 0.0.2" <<<"$OUT"'
  chk "版本還是 0.0.2（執行檔與跑著的服務都是）" '[ "$(versions)" = "執行檔 0.0.2／服務 0.0.2" ]'
  chk "看板 200" 'wait_board'
  chk "bin.failed/ 存在（壞的執行檔留著）" '[ -x "$CORE/bin.failed/evergenie" ]'
  chk "skip.json 有 0.0.3" 'grep -q "0.0.3" "$STATE/runtime/upgrade/skip.json"'
  chk "state 是 rolled_back" '[ "$(pyj '"'"'d["phase"]'"'"' <"$STATE/runtime/upgrade/state.json")" = rolled_back ]'
  chk "history.jsonl 還是一筆（失敗的升級不記成升級）" '[ "$(wc -l <"$STATE/runtime/upgrade/history.jsonl" | tr -d " ")" = 1 ]'
  run hg upgrade --check --json
  chk "再 --check：沒有新版（0.0.3 跳過了）" '[ "$(pyj '"'"'"[" + d["target"] + "]"'"'"' <<<"$OUT")" = "[]" ] && pyj '"'"'d["skipped"]'"'"' <<<"$OUT" | grep -q 0.0.3'
}

seg6() { # 手動退回
  run hg upgrade --rollback
  chk "upgrade --rollback 成功（結束碼 0）" '[ $RC = 0 ]'
  [ $RC = 0 ] || return
  chk "版本是 0.0.1（執行檔與跑著的服務都是）" '[ "$(versions)" = "執行檔 0.0.1／服務 0.0.1" ]'
  chk "看板 200" 'wait_board'
  chk "history.jsonl 兩筆，最後一筆是 rollback 回 0.0.1" '[ "$(wc -l <"$STATE/runtime/upgrade/history.jsonl" | tr -d " ")" = 2 ] && [ "$(tail -1 "$STATE/runtime/upgrade/history.jsonl" | pyj '"'"'d["kind"] + d["to"]'"'"')" = rollback0.0.1 ]'
}

seg7() { # 角色包沒網路與不退版
  printf 'version = "2099.01.01"\nurl = "file://%s/nowhere/m/2099.01.01.json"\nsha256 = "%s"\n' "$B" "$(printf '0%.0s' $(seq 1 64))" >"$B/ptr-gone.toml"
  OUT="$("${CLEAN[@]}" EVERGENIE_HOME="$HOMEDIR" EVERGENIE_PERSONAS_PACK_POINTER="$B/ptr-gone.toml" "$CORE/bin/evergenie" personas pack sync --json 2>&1)"; RC=$?
  chk "指到不存在的網址：sync 失敗（結束碼 1）、狀態 failed" '[ $RC = 1 ] && [ "$(pyj '"'"'d["status"]'"'"' <<<"$OUT")" = failed ]'
  chk "personas/ 的檔案沒變、登記也沒變" '[ "$(tree_sum "$STATE/personas")" = "$PERSONAS_SUM" ] && [ "$(shasum -a 256 "$STATE/personas/pack-registry.toml" | cut -d" " -f1)" = "$PACK_REG_SUM" ]'
  chk "pending.json 有，記著要補的版號 2099.01.01" '[ "$(pyj '"'"'d["version"]'"'"' <"$STATE/runtime/personas-pack/pending.json")" = 2099.01.01 ]'
  OUT="$(doctor_json)"
  chk "doctor 有黃字（packages.personas_pack）" '[ "$(pyj '"'"'[c["severity"] for c in d["checks"] if c["id"] == "packages.personas_pack"]'"'"' <<<"$OUT")" = "['"'"'warn'"'"']" ]'
  chk "版本沒被退（還是 0.0.1，看板 200）" '[ "$(versions)" = "執行檔 0.0.1／服務 0.0.1" ] && [ "$(board)" = 200 ]'
  printf 'version = "2000.01.01"\nurl = "%s"\nsha256 = "%s"\n' "$PACK_URL" "$PACK_SHA" >"$B/ptr-old.toml"
  OUT="$("${CLEAN[@]}" EVERGENIE_HOME="$HOMEDIR" EVERGENIE_PERSONAS_PACK_POINTER="$B/ptr-old.toml" "$CORE/bin/evergenie" personas pack sync 2>&1)"; RC=$?
  chk "指到比登記舊的版號：不動、說「不退版」" '[ $RC = 0 ] && grep -q "不退版" <<<"$OUT" && [ "$(tree_sum "$STATE/personas")" = "$PERSONAS_SUM" ] && [ "$(shasum -a 256 "$STATE/personas/pack-registry.toml" | cut -d" " -f1)" = "$PACK_REG_SUM" ]'
}

seg8() { # 套件從網址
  local pk="$B/pkg"
  mkdir -p "$pk/schemas" && git init -q -b main "$pk" && git_ok "$pk" || { SEG_FAIL=1; return; }
  cat >"$pk/package.toml" <<'TOML'
schema = 1
id = "e2e-notes"
name = "e2e 筆記"
version = "1.0.0"
description = "發版整套測試用的資料型套件"
kind = "data"
api = "evergenie.v1"

[capabilities]
read = ["vault:pkg/e2e-notes"]
write = ["vault:pkg/e2e-notes"]

[[contributes.data]]
kind = "note"
schema = "schemas/note.toml"
ids = "given"
TOML
  cat >"$pk/schemas/note.toml" <<'TOML'
schema = 1
title = "筆記"

[[field]]
name = "text"
type = "string"
description = "內容"
TOML
  (cd "$pk" && git add -A && git commit -q -m "feat: notes 1.0.0" && git tag -a v1.0.0 -m v1.0.0) || { SEG_FAIL=1; return; }
  run hg packages install "file://$pk"
  chk "沒有 --yes：只印預覽、結束碼 0" '[ $RC = 0 ] && grep -q "e2e" <<<"$OUT"'
  OUT="$(hg packages list 2>&1)"
  chk "沒有 --yes 的安裝沒有裝" '! grep -q "\"id\":\"e2e-notes\"" <<<"$OUT"'
  run hg packages install "file://$pk" --yes
  chk "--yes 安裝成功" '[ $RC = 0 ]'
  OUT="$(hg packages list 2>&1 | grep '"id":"e2e-notes"')"
  chk "packages list 有 e2e-notes 1.0.0、來源是那個網址、tag v1.0.0" '[ "$(pyj '"'"'d["version"]'"'"' <<<"$OUT")" = 1.0.0 ] && grep -q "file://$pk" <<<"$OUT" && grep -q "v1.0.0" <<<"$OUT"'
  run hg data add e2e-notes.note --id n-1 --json '{"text":"我的筆記"}'
  chk "寫一筆這個套件的資料" '[ $RC = 0 ]'
  (cd "$pk" && sed -i.bak 's/^version = "1.0.0"/version = "1.1.0"/' package.toml && rm -f package.toml.bak && git commit -q -am "feat: notes 1.1.0" && git tag -a v1.1.0 -m v1.1.0) || { SEG_FAIL=1; return; }
  run hg packages outdated
  chk "packages outdated 列出 e2e-notes 1.0.0 → 1.1.0" '[ $RC = 0 ] && grep -q "e2e-notes" <<<"$OUT" && grep -q "1.1.0" <<<"$OUT"'
  run hg packages upgrade e2e-notes
  chk "packages upgrade e2e-notes 成功" '[ $RC = 0 ]'
  OUT="$(hg packages list 2>&1 | grep '"id":"e2e-notes"')"
  chk "版本變成 1.1.0" '[ "$(pyj '"'"'d["version"]'"'"' <<<"$OUT")" = 1.1.0 ]'
  run hg data get e2e-notes.note n-1
  chk "更新前寫的那筆資料還在" '[ $RC = 0 ] && grep -q "我的筆記" <<<"$OUT"'
  run hg packages outdated
  chk "更新後 outdated 沒有它了" '! grep -q "e2e-notes" <<<"$OUT"'
}

seg9() { # 發行者流程
  # origin 再出兩個 commit（修好 0.0.3 的壞、加一個改動），沒打正式版 tag
  (
    cd "$B/work" || exit 1
    git revert --no-commit v0.0.3 && git commit -q -m "fix: serve starts again" || exit 1
    echo "e2e" >e2e-note.txt && git add e2e-note.txt && git commit -q -m "chore: add a note" && git push -q origin main
  ) || { SEG_FAIL=1; return; }
  git clone -q "$B/origin.git" "$B/rel" || { SEG_FAIL=1; return; }
  git_ok "$B/rel"
  # 假的 evergenie 擋在 PATH 前面：nightly 失敗時的通知不能發進這台的正式家（2026-11-02，release-test.sh 同一個坑）
  mkdir -p "$B/fakebin" && printf '#!/bin/sh\nexit 0\n' >"$B/fakebin/evergenie" && chmod +x "$B/fakebin/evergenie"
  local rel=("${CLEAN[@]}" PATH="$B/fakebin:$PATH" EVERGENIE_RELEASE_REPO="$B/rel" EVERGENIE_RELEASE_STATE="$B/relstate" EVERGENIE_RELEASE_STEPS="make build")
  OUT="$(cd "$ROOT" && "${rel[@]}" scripts/release.sh nightly 2>&1)"; RC=$?
  chk "nightly 成功（全新 clone、make build 過）" '[ $RC = 0 ] && grep -q "全過" <<<"$OUT"'
  chk "打了 safe/$TODAY" 'git -C "$B/rel" rev-parse -q --verify "refs/tags/safe/$TODAY" >/dev/null'
  chk "nightly.json 記 ok、一步、safe_tag" '[ "$(pyj '"'"'str(d["ok"]) + str(len(d["steps"])) + d["safe_tag"]'"'"' <"$B/relstate/nightly.json")" = "True1safe/$TODAY" ]'
  OUT="$(cd "$ROOT" && "${rel[@]}" scripts/release.sh prepare 2>&1)"; RC=$?
  chk "prepare 建議版號 0.0.4（上一個正式版 0.0.3）" '[ $RC = 0 ] && grep -q "建議版號：0.0.4" <<<"$OUT" && [ "$(pyj '"'"'d["version"]'"'"' <"$B/relstate/next.json")" = 0.0.4 ]'
  local notes safe_commit
  # 草稿開頭那行「（草稿：…）」只給發版頁看，stable 不讓它進 tag
  notes="$(pyj 'd["notes"].split("\n", 1)[1].lstrip("\n")' <"$B/relstate/next.json")"
  safe_commit="$(git -C "$B/rel" rev-parse "safe/$TODAY^{commit}")"
  OUT="$(cd "$ROOT" && "${rel[@]}" scripts/release.sh stable --version 0.0.4 2>&1)"; RC=$?
  chk "stable --version 0.0.4 成功" '[ $RC = 0 ]'
  chk "v0.0.4 打在 safe 的 commit 上" '[ "$(git -C "$B/rel" rev-parse -q --verify "refs/tags/v0.0.4^{commit}")" = "$safe_commit" ]'
  chk "v0.0.4 的 tag 說明＝發版說明（含「修正」段）" '[ "$(git -C "$B/rel" tag -l --format="%(contents)" v0.0.4)" = "$notes" ] && grep -q "fix: serve starts again\|serve starts again" <<<"$notes" && ! grep -q "草稿" <<<"$notes"'
  OUT="$(git -C "$B/origin.git" tag --list | tr '\n' ' ')"
  chk "沒有 --push：假 origin 沒有新 tag（還是 v0.0.1 v0.0.2 v0.0.3）" '[ "$OUT" = "v0.0.1 v0.0.2 v0.0.3 " ]'
}

seg10() { # make compat
  git clone -q "$B/origin.git" "$B/cc" || { SEG_FAIL=1; return; }
  git_ok "$B/cc"
  OUT="$(cd "$B/cc" && "${CLEAN[@]}" EVERGENIE_COMPAT_TMP="${EVERGENIE_E2E_TMP:-/tmp}" make compat 2>&1)"; RC=$?
  chk "make compat 在暫存 clone 跑過（結束碼 0）" '[ $RC = 0 ] && grep -q "相容檢查：全過" <<<"$OUT"'
  chk "對上一個正式版 v0.0.3 比 sdk、用它建家再遷移（沒有跳過任何一項）" 'grep -q "上一個正式版：v0.0.3" <<<"$OUT" && ! grep -q "跳過" <<<"$OUT" && grep -q "migrate-home --apply 過了" <<<"$OUT"'
  printf '%s\n' "$OUT" | sed 's/^/      /'
  # 反向：刪掉 sdk 快照的一行，compat 要抓到
  local line
  line="$(sed -n 5p "$B/cc/sdk/testdata/api.golden")"
  sed -i.bak 5d "$B/cc/sdk/testdata/api.golden" && rm -f "$B/cc/sdk/testdata/api.golden.bak"
  OUT="$(cd "$B/cc" && "${CLEAN[@]}" scripts/compat.sh --only sdk 2>&1)"; RC=$?
  chk "刪掉 sdk 快照的一行：compat 抓到、結束碼 1、列出少了哪一行" '[ $RC = 1 ] && grep -qF "$line" <<<"$OUT"'
  git -C "$B/cc" checkout -q -- sdk/testdata/api.golden
}

seg11() { # 看板按「現在更新」走的路：API 檢查 → API 更新（升級在背景行程跑，服務被重開也不中斷）
  (cd "$B/work" && git tag -a v0.0.5 -m "e2e 0.0.5" && git push -q origin v0.0.5) || { SEG_FAIL=1; return; }
  local by='{"name":"owner(e2e)"}' i phase=""
  OUT="$(api update_check "{\"by\":$by}")"; RC=$?
  chk "system.update_check 找到 0.0.5（0.0.3、0.0.2 在跳過清單裡）" '[ "$(pyj '"'"'d["target"] + " " + d["tag"]'"'"' <<<"$OUT")" = "0.0.5 v0.0.5" ]'
  OUT="$(api update_apply "{\"version\":\"0.0.5\",\"by\":$by}")"; RC=$?
  chk "system.update_apply 立刻回、升級在背景開始" '[ $RC = 0 ] && [ "$(pyj '"'"'d["target"]'"'"' <<<"$OUT")" = 0.0.5 ]'
  for ((i = 0; i < WAIT; i++)); do
    phase="$(pyj 'd["phase"]' <"$STATE/runtime/upgrade/state.json" 2>/dev/null)"
    case "$phase" in done | failed | rolled_back) break ;; esac
    sleep 1
  done
  OUT="$(cat "$STATE/runtime/upgrade/state.json" 2>&1)"
  chk "背景升級走完，state 是 done" '[ "$phase" = done ]'
  chk "看板 200" 'wait_board'
  chk "版本是 0.0.5（執行檔與跑著的服務都是）" '[ "$(versions)" = "執行檔 0.0.5／服務 0.0.5" ]'
  chk "history.jsonl 三筆，最後一筆是 0.0.1 → 0.0.5 升級成功" '[ "$(wc -l <"$STATE/runtime/upgrade/history.jsonl" | tr -d " ")" = 3 ] && [ "$(tail -1 "$STATE/runtime/upgrade/history.jsonl" | pyj '"'"'d["kind"] + d["from"] + d["to"] + str(d["ok"])'"'"')" = "upgrade0.0.10.0.5True" ]'
  OUT="$(api update_status '{}')"
  chk "system.update_status：現在 0.0.5、升級結果成功、沒有新版" '[ "$(pyj '"'"'d["current"] + " " + str(d["state"]["ok"]) + " [" + d.get("target", "") + "]"'"'"' <<<"$OUT")" = "0.0.5 True []" ]'
}

pkgapi() { curl -sS -m 30 --unix-socket "$STATE/runtime/sockets/api.sock" -X POST -H 'Content-Type: application/json' "http://x/api/package/$1" -d "$2" 2>&1; }
sha() { shasum -a 256 "$1" | cut -d' ' -f1; }

seg12() { # 有自己東西的家：升級前準備（DEV-35）
  local by='{"name":"owner(e2e)"}'
  # (1) 改過的官方套件。整包複製的入口已關（DEV-154）：API 要拒絕；這裡照舊版 fork 留下的樣子手動擺一份
  #     （mine 同名、.fork-base、[fork] 區段），驗證以前複製過的家升級後還是原樣。
  OUT="$(pkgapi fork "{\"id\":\"e2e-official\",\"by\":$by}")"
  chk "整包複製官方套件的入口已關：回白話說明、packages/mine/ 沒有多東西" 'grep -q "不能整包複製" <<<"$OUT" && [ ! -e "$HOMEDIR/packages/mine/e2e-official" ]'
  local off="$HOMEDIR/packages/official/e2e-official"; [ -d "$off" ] || off="$CORE/packages/official/e2e-official"
  mkdir -p "$HOMEDIR/packages/mine" && cp -R "$off" "$HOMEDIR/packages/mine/e2e-official" && cp -R "$off" "$HOMEDIR/packages/mine/e2e-official/.fork-base"; RC=$?
  printf '\n[fork]\nof = "e2e-official"\nbase_version = "1.0.0"\nforked_at = "2026-01-01T00:00:00Z"\n' >>"$HOMEDIR/packages/mine/e2e-official/package.toml"
  chk "擺好舊版 fork 留下的那一份（1.0.0）" '[ $RC = 0 ] && [ -d "$HOMEDIR/packages/mine/e2e-official/.fork-base" ]'
  printf '我自己加的一行\n' >>"$HOMEDIR/packages/mine/e2e-official/README.md"
  run hg packages reload
  OUT="$(hg packages get e2e-official 2>&1)"
  chk "自己的版本在跑：tier mine、版本 1.0.0、fork 的基準 1.0.0" 'grep -q "\"tier\":\"mine\"" <<<"$OUT" && grep -q "\"version\":\"1.0.0\"" <<<"$OUT" && grep -q "base_version\":\"1.0.0\"" <<<"$OUT"'
  MINE_README_SUM="$(sha "$HOMEDIR/packages/mine/e2e-official/README.md")"
  # (2) 改過的角色卡、自己的角色
  printf '\n我自己加的口頭禪\n' >>"$STATE/personas/helper.md"
  mkdir -p "$HOMEDIR/personas/mine" && printf -- '---\nschema: 1\nname: "我的貓"\n---\n一隻我自己做的角色。\n' >"$HOMEDIR/personas/mine/mycat.md"
  HELPER_SUM="$(sha "$STATE/personas/helper.md")"
  MYCAT_SUM="$(sha "$HOMEDIR/personas/mine/mycat.md")"
  # (3) 別人分享的套件：自帶一位秘書（要有角色來擔任）；從網址裝、套上它的秘書、加自己的附加規則
  local sh="$B/share"
  mkdir -p "$sh/agents/e2e-host" && git init -q -b main "$sh" && git_ok "$sh" || { SEG_FAIL=1; return; }
  cat >"$sh/package.toml" <<'TOML'
schema = 1
id = "e2e-share"
name = "e2e 分享的套件"
version = "1.0.0"
description = "發版整套測試用：別人分享、自帶一位秘書的套件"
kind = "data"
api = "evergenie.v1"

[[contributes.agent]]
id = "e2e-host"
template = "agents/e2e-host/agent.md"
TOML
  printf -- '---\nduty: 分享套件的秘書\n---\n\n# e2e-host 的職責\n第一版職責：幫你整理筆記。\n' >"$sh/agents/e2e-host/agent.md"
  (cd "$sh" && git add -A && git commit -q -m "feat: share 1.0.0" && git tag -a v1.0.0 -m v1.0.0) || { SEG_FAIL=1; return; }
  run hg packages install "file://$sh" --yes
  chk "分享的套件 e2e-share 1.0.0 裝好" '[ $RC = 0 ]'
  run hg packages sync e2e-share
  chk "sync 預設只預演：說會新增秘書 e2e-host，還沒新增" '[ $RC = 0 ] && grep -q "e2e-host" <<<"$OUT" && ! hg agents list 2>&1 | grep -q "e2e-host"'
  run hg packages sync e2e-share --apply --persona helper
  chk "sync --apply --persona helper 成功（分享的套件自帶秘書，要有人設來擔任），秘書 e2e-host 出現在名冊" '[ $RC = 0 ] && hg agents list 2>&1 | grep -q "e2e-host"'
  run hg customize add e2e-host "我自己的附加規則：一律先說晚安。"
  chk "替這位秘書加自己的附加規則" '[ $RC = 0 ]'
  OUT="$(hg agents list 2>&1 | grep e2e-host)"
  HOST_ROW_BEFORE="$OUT"
  chk "每樣東西升級前都就位（記下指紋）" '[ -n "$MINE_README_SUM" ] && [ -n "$HELPER_SUM" ] && [ -n "$MYCAT_SUM" ]'
}

seg13() { # 有自己東西的家：升級後（DEV-35）
  local by='{"name":"owner(e2e)"}' i
  chk "已經是 0.0.5、看板 200" '[ "$(versions)" = "執行檔 0.0.5／服務 0.0.5" ] && wait_board'
  # (1) 裝來的套件（seg8）：還在、版本與來源不變、資料在
  OUT="$(hg packages list 2>&1 | grep '"id":"e2e-notes"')"
  chk "核心升級後，裝來的 e2e-notes 還在（1.1.0、來源還記著）、資料還在" '[ "$(pyj '"'"'d["version"]'"'"' <<<"$OUT")" = 1.1.0 ] && grep -q "file://$B/pkg" <<<"$OUT" && hg data get e2e-notes.note n-1 2>&1 | grep -q "我的筆記"'
  OUT="$(hg packages list 2>&1 | grep '"id":"e2e-share"')"
  chk "分享來的 e2e-share 還在、它的秘書 e2e-host 還在名冊" 'grep -q "\"version\":\"1.0.0\"" <<<"$OUT" && hg agents list 2>&1 | grep -q "e2e-host"'
  # (2) 改過的官方套件：官方 0.0.2 已經出了 1.1.0；自己的還是原樣，只提醒
  OUT="$(hg packages get e2e-official 2>&1)"
  chk "fork 的還是自己的那一份：tier mine、1.0.0、在跑" 'grep -q "\"tier\":\"mine\"" <<<"$OUT" && grep -q "\"version\":\"1.0.0\"" <<<"$OUT" && grep -q "\"state\":\"running\"" <<<"$OUT"'
  chk "自己改過的 README 一個字沒變" '[ "$(sha "$HOMEDIR/packages/mine/e2e-official/README.md")" = "$MINE_README_SUM" ]'
  run hg packages updates e2e-official
  chk "packages updates 看得到官方 1.0.0 → 1.1.0，並標出 README 你也改過" '[ $RC = 0 ] && grep -q "1.1.0" <<<"$OUT" && grep -q "conflicts_with_fork\":true\|你也改過\|conflicts" <<<"$OUT"'
  OUT="$(hg doctor --json 2>/dev/null)"
  chk "doctor 沒有紅字" '[ "$(pyj '"'"'len([c for c in d["checks"] if c["severity"] == "error"])'"'"' <<<"$OUT")" = 0 ]'
  # (3) 角色包出新版：沒改過的換、改過的不蓋、自己的角色不碰
  chk "改過的角色卡、自己的角色，核心升級後還是原樣" '[ "$(sha "$STATE/personas/helper.md")" = "$HELPER_SUM" ] && [ "$(sha "$HOMEDIR/personas/mine/mycat.md")" = "$MYCAT_SUM" ]'
  printf '\n發行者新版加的一句\n' >>"$B/pub/personas/helper.md"
  printf '\n發行者新版加的一句\n' >>"$B/pub/personas/builder.md"
  OUT="$(cd "$ROOT" && "${CLEAN[@]}" go run ./cmd/evergenie personas pack build --home "$B/pub" --out "$B/pack" --version 2026.10.30 --no-records --json 2>"$B/gorun.err")"; RC=$?
  [ $RC = 0 ] || OUT="$(cat "$B/gorun.err")"
  chk "發行者建出角色包新版 2026.10.30" '[ $RC = 0 ]'
  local v2="$(pyj 'd["manifest_sha256"]' <<<"$OUT")"
  printf 'version = "2026.10.30"\nurl = "file://%s/pack/m/2026.10.30.json"\nsha256 = "%s"\n' "$B" "$v2" >"$B/ptr-v2.toml"
  local sync=("${CLEAN[@]}" EVERGENIE_HOME="$HOMEDIR" EVERGENIE_PERSONAS_PACK_POINTER="$B/ptr-v2.toml" "$CORE/bin/evergenie" personas pack sync)
  OUT="$("${sync[@]}" --json 2>&1)"; RC=$?
  chk "sync 成功" '[ $RC = 0 ]'
  chk "沒改過的 builder 換成新版" 'grep -q "發行者新版加的一句" "$STATE/personas/builder.md"'
  chk "改過的 helper 沒被蓋、結果有列出「你改過的」" '[ "$(sha "$STATE/personas/helper.md")" = "$HELPER_SUM" ] && grep -qi "helper" <<<"$OUT"'
  chk "自己的角色 mycat 沒動" '[ "$(sha "$HOMEDIR/personas/mine/mycat.md")" = "$MYCAT_SUM" ]'
  OUT="$("${sync[@]}" --force 2>&1)"; RC=$?
  chk "--force 才蓋 helper，蓋之前有備份；mycat 還是沒動" '[ $RC = 0 ] && grep -q "發行者新版加的一句" "$STATE/personas/helper.md" && [ -n "$(ls "$STATE/runtime/backups/personas-pack" 2>/dev/null)" ] && [ "$(sha "$HOMEDIR/personas/mine/mycat.md")" = "$MYCAT_SUM" ]'
  # (4) 分享的套件出新版（職責改了）：更新後，秘書職責跟著新版、使用者的附加規則還在
  local sh="$B/share"
  printf -- '---\nduty: 分享套件的秘書\n---\n\n# e2e-host 的職責\n第二版職責：改成幫你排行程。\n' >"$sh/agents/e2e-host/agent.md"
  (cd "$sh" && sed -i.bak 's/^version = "1.0.0"/version = "1.1.0"/' package.toml && rm -f package.toml.bak && git commit -q -am "feat: share 1.1.0" && git tag -a v1.1.0 -m v1.1.0) || { SEG_FAIL=1; return; }
  run hg packages outdated
  chk "outdated 列出 e2e-share 1.0.0 → 1.1.0" '[ $RC = 0 ] && grep -q "e2e-share" <<<"$OUT" && grep -q "1.1.0" <<<"$OUT"'
  run hg packages upgrade e2e-share
  chk "packages upgrade e2e-share 成功（升級後的核心上）" '[ $RC = 0 ]'
  run hg packages sync e2e-share
  chk "sync 預演：看得到職責有差異、不會新增第二位" '[ $RC = 0 ] && grep -q "e2e-host" <<<"$OUT"'
  run hg packages sync e2e-share --apply
  chk "sync --apply 成功" '[ $RC = 0 ]'
  OUT="$(hg customize show e2e-host 2>&1)"
  chk "秘書職責換成第二版" 'grep -q "第二版職責" <<<"$OUT" && ! grep -q "第一版職責" <<<"$OUT"'
  chk "使用者自己的附加規則還在" 'grep -q "一律先說晚安" <<<"$OUT"'
  # (5) 再退版：這些東西都還在
  run hg upgrade --rollback
  chk "upgrade --rollback 成功" '[ $RC = 0 ]'
  chk "版本是 0.0.1、看板 200" '[ "$(versions)" = "執行檔 0.0.1／服務 0.0.1" ] && wait_board'
  OUT="$(hg packages list 2>&1)"
  chk "退版後：裝來的 e2e-notes、分享的 e2e-share、fork 的 e2e-official 都還在" 'grep -q "\"id\":\"e2e-notes\"" <<<"$OUT" && grep -q "\"id\":\"e2e-share\"" <<<"$OUT" && hg packages get e2e-official 2>&1 | grep -q "\"tier\":\"mine\""'
  chk "退版後：自己改過的 README、自己的角色、e2e-host 秘書都在，資料也在" '[ "$(sha "$HOMEDIR/packages/mine/e2e-official/README.md")" = "$MINE_README_SUM" ] && [ "$(sha "$HOMEDIR/personas/mine/mycat.md")" = "$MYCAT_SUM" ] && hg agents list 2>&1 | grep -q "e2e-host" && hg data get e2e-notes.note n-1 2>&1 | grep -q "我的筆記"'
}

# ---- 跑 ------------------------------------------------------------------------------------------------------
RESULTS=()
FAILED_SEG=""
run_seg() { # run_seg <編號> <標題> <函式>
  local n="$1" title="$2" fn="$3" t0=$SECONDS secs
  say "── 第 $n 段：$title"
  SEG_FAIL=0
  OUT="" RC=0
  "$fn"
  secs=$((SECONDS - t0))
  if [ "$SEG_FAIL" = 0 ]; then
    say "✔ 第 $n 段：${title}（${secs} 秒）"
    RESULTS+=("✔ 第 $n 段　${secs} 秒　$title")
  else
    say "✘ 第 $n 段：${title}（${secs} 秒）"
    RESULTS+=("✘ 第 $n 段　${secs} 秒　$title")
    FAILED_SEG="$FAILED_SEG $n"
    [ "${EVERGENIE_E2E_CONTINUE:-0}" = 1 ] || return 1
  fi
  say ""
  return 0
}

say "發版整套測試：暫存資料夾 ${B}（$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null)）"
say ""
T0=$SECONDS
run_seg 0 "先建角色包（發行者的 fixture 家 → personas pack build）" seg0 &&
  run_seg 1 "做一個假的 origin 和三個 tag（v0.0.1、v0.0.2 帶角色包、v0.0.3 故意壞）" seg1 &&
  run_seg 2 "一鍵安裝 v0.0.1（install.sh）" seg2 &&
  run_seg 3 "檢查新版（目標要是最大的 0.0.3）" seg3 &&
  run_seg 4 "升到指定版 v0.0.2（含角色包同步）" seg4 &&
  run_seg 5 "壞版 0.0.3 自動退回並跳過" seg5 &&
  run_seg 6 "手動退回到 0.0.1" seg6 &&
  run_seg 7 "角色包沒網路與不退版" seg7 &&
  run_seg 8 "套件從網址安裝與更新（資料保留）" seg8 &&
  run_seg 9 "發行者流程 nightly／prepare／stable（不推）" seg9 &&
  run_seg 10 "make compat（含刪掉 sdk 一行要抓到）" seg10 &&
  run_seg 12 "有自己東西的家：升級前準備（fork 官方套件、改過的角色卡、自己的角色、分享來的套件與它的秘書）" seg12 &&
  run_seg 11 "看板按「現在更新」走的路（API 檢查 → API 更新，背景行程）" seg11 &&
  run_seg 13 "有自己東西的家：升級後每樣都在、官方新版不蓋、角色包新版不蓋改過的、分享的套件更新、再退版也在" seg13

say ""
say "== 總結（共 $((SECONDS - T0)) 秒）"
printf '%s\n' "${RESULTS[@]}"
if [ -z "$FAILED_SEG" ]; then
  say "發版整套測試：全過"
  exit 0
fi
say "發版整套測試：沒過的段落：$FAILED_SEG"
exit 1
