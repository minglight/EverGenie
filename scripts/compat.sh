#!/usr/bin/env bash
# 相容檢查（P8 第二輪 S5、REQ-284、REQ-136）：這一版的程式，對「上一個正式版」留下來的東西還認不認得。結束碼 0＝全過。
#
#   scripts/compat.sh [--only sdk|schema|fixture|prev] [--help]
#
#   sdk      sdk 的型別快照只加不刪：上一個正式版的 sdk/testdata/api.golden 每一行，現在都還在（少了的列出來）；
#            刻意刪的登記在 sdk/testdata/api.removed（附原因），不算少了。
#   schema   官方套件的紀錄格式只加欄位：上一個正式版 packages/official 的每個資料種類，現在拿掉欄位、換型別、拿掉選項、變必填，
#            都要在 package.toml 附 [[migration]]（升級時核心替使用者轉舊紀錄）；刻意不轉的登記在 packages/official/schema.removed。
#   fixture  fixture 家（cmd/evergenie/testdata/home 複製到暫存）用這一版的 bin/evergenie serve 開得起來：
#            API 起得來、doctor 沒有紅字、每個官方套件都載入了（沒有 failed／rejected，啟用的都在跑）。
#   prev     上一個正式版建出來的家（用它自己的 bin/evergenie init），換成這一版的執行檔跑 migrate-home --apply 再 serve，檢查同 fixture。
#
# 「上一個正式版」＝最大的 vX.Y.Z tag（發版是先測、測過才打 tag，所以測發版候選的時候 HEAD 還沒有它）；沒有就跳過 sdk、schema 與 prev 三項並說明。
# 環境變數：
#   EVERGENIE_COMPAT_PREV   指定「上一個正式版」是哪個 tag／commit（試跑用）
#   EVERGENIE_COMPAT_KEEP=1 結束後保留暫存資料夾（除錯用）
# 需要已經編好的 bin/evergenie（make compat 會先 make build）。全程在 mktemp -d 的暫存資料夾、用空的 port，不碰 launchd、不碰任何正式家。
set -uo pipefail
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它

usage() { # usage [結束碼]：--help 是 0，其他（不認得的選項）是 2
  sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//' >&2
  exit "${1:-2}"
}

ONLY=""
while [ $# -gt 0 ]; do
  case "$1" in
    --only) shift; ONLY="${1:-}"; case "$ONLY" in sdk | schema | fixture | prev) ;; *) usage ;; esac ;;
    -h | --help) usage 0 ;;
    *) echo "不認得的選項：$1" >&2; usage ;;
  esac
  shift
done

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# 暫存資料夾要短：macOS 的 unix socket 路徑上限約 104 字元，$TMPDIR（/var/folders/…）太長會讓引擎連不上。
BASE="$(mktemp -d "${EVERGENIE_COMPAT_TMP:-/tmp}/hgc.XXXXXX")"
PIDS=()
cleanup() {
  local p
  for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done
  sleep 0.3
  for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill -9 "$p" 2>/dev/null; done
  git -C "$ROOT" worktree remove --force "$BASE/prev" >/dev/null 2>&1 || true
  git -C "$ROOT" worktree prune >/dev/null 2>&1 || true
  if [ "${EVERGENIE_COMPAT_KEEP:-0}" = 1 ]; then echo "（保留暫存資料夾：${BASE}）"; else rm -rf "$BASE"; fi
}
trap cleanup EXIT

say() { printf '%s\n' "$*"; }
FAILED=0
ok() { say "  ✔ $*"; }
bad() { say "  ✘ $*"; FAILED=$((FAILED + 1)); }
skip() { say "  － 跳過：$*"; }
want() { [ -z "$ONLY" ] || [ "$ONLY" = "$1" ]; }

# 乾淨的環境：拿掉屬於別的家／別的 session 的 EVERGENIE_*，再明確設定這次要用的（服務一律背景行程，不碰 launchd）。
CLEAN=(env -u EVERGENIE_HOME -u EVERGENIE_ENV_FILE -u EVERGENIE_AGENT -u EVERGENIE_LAUNCH -u EVERGENIE_ENGINE_SOCKET -u EVERGENIE_ENGINE_BIN_DIR -u EVERGENIE_API_SOCKET -u EVERGENIE_SESSION
  EVERGENIE_SITE=compat EVERGENIE_SERVICE_MODE=process EVERGENIE_BACKUP_DIR="$BASE/backup" HOME="$BASE/uh" CLAUDE_CONFIG_DIR="$BASE/uh/claude-config")
# HOME 指到暫存區：新佈局的家，狀態資料夾預設在 ~/.evergenie/state/<家名>；不換掉就會寫進這台電腦真的狀態資料夾
#（留下垃圾、下一次同名就被擋，DEV-154）。舊版的 init 不認得 --state，所以用 HOME 換，哪一版都適用。
# CLAUDE_CONFIG_DIR 也明確指到暫存區（DEV-157）：init 會把「現在登入的帳號資料夾」寫進 home.toml（ENG-19），從 AI 的 session 跑時
# 環境裡是這台電腦真的 Claude 設定；只換 HOME 擋不住它。
REAL_HOME="$HOME" # 編譯要用真的家目錄（Go 的模組與編譯快取在那裡）
mkdir -p "$BASE/uh"
freeport() { python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'; }

# ---- 上一個正式版 ---------------------------------------------------------------------------------------------
PREV="${EVERGENIE_COMPAT_PREV:-}"
if [ -z "$PREV" ] && git -C "$ROOT" rev-parse --git-dir >/dev/null 2>&1; then
  PREV="$(git -C "$ROOT" tag --list 'v[0-9]*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1)"
fi
if [ -n "$PREV" ]; then say "上一個正式版：$PREV"; else say "上一個正式版：（還沒有）"; fi

# ---- 一個家開得起來嗎 -------------------------------------------------------------------------------------------
# check_home <執行檔> <家>：serve 起來 → API 通 → doctor 沒有紅字 → 官方套件都載入。印每項結果；全部過回 0。
check_home() {
  local exe="$1" home="$2" pid="" i up=0 out rc=0
  "${CLEAN[@]}" nohup "$exe" serve --home "$home" >"$home/serve.out" 2>&1 &
  pid=$!
  PIDS+=("$pid")
  for ((i = 0; i < 120; i++)); do
    out="$("${CLEAN[@]}" EVERGENIE_HOME="$home" "$exe" doctor --json 2>/dev/null)" || true
    if python3 -c 'import json,sys
d = json.loads(sys.stdin.read())
sys.exit(0 if d.get("checks") and not any(c["id"] in ("serve.down", "serve.doctor") for c in d["checks"]) else 1)' <<<"$out" 2>/dev/null; then
      up=1
      break
    fi
    kill -0 "$pid" 2>/dev/null || break
    sleep 1
  done
  if [ "$up" != 1 ]; then
    bad "serve 沒有起來（log：$(tail -3 "$home/serve.out" | tr '\n' ' ')）"
    return 1
  fi
  ok "serve 起來了、API 通"
  local reds
  reds="$(python3 -c 'import json,sys
d = json.loads(sys.stdin.read())
print("；".join(c["detail"] for c in d["checks"] if c.get("severity") == "error"))' <<<"$out")"
  if [ -n "$reds" ]; then bad "doctor 有紅字：$reds"; rc=1; else ok "doctor 沒有紅字"; fi
  local pk verdict n
  for ((n = 0; n < 90; n++)); do
    pk="$("${CLEAN[@]}" EVERGENIE_HOME="$home" "$exe" packages list 2>&1)"
    grep -q '"state":"building"' <<<"$pk" || break # 程式型的套件還在編譯／啟動，等它們
    sleep 1
  done
  verdict="$(python3 -c 'import json,sys
bad, n = [], 0
for line in sys.stdin:
    line = line.strip()
    if not line.startswith("{"):
        continue
    d = json.loads(line)
    if d.get("tier") != "official":
        continue
    n += 1
    st = d.get("state", "")
    if st in ("failed", "rejected") or (d.get("enabled") and st != "running"):
        bad.append("%s（%s%s）" % (d["id"], st, "：" + d["error"] if d.get("error") else ""))
print(("BAD " + "；".join(bad)) if bad else ("OK %d" % n) if n else "BAD 一個官方套件都沒有載入")' <<<"$pk")"
  case "$verdict" in
    "OK "*) ok "官方套件 ${verdict#OK } 個全部載入（沒有 failed／rejected，啟用的都在跑）" ;;
    *) bad "官方套件有問題：${verdict#BAD }"; rc=1 ;;
  esac
  kill "$pid" 2>/dev/null
  for ((i = 0; i < 20; i++)); do kill -0 "$pid" 2>/dev/null || break; sleep 0.5; done
  return $rc
}

NEW_EXE="$ROOT/bin/evergenie"

# ---- 1. sdk 只加不刪 --------------------------------------------------------------------------------------------
if want sdk; then
  say "── 1/4 sdk 型別快照只加不刪（對 ${PREV:-上一個正式版}）"
  if [ -z "$PREV" ]; then
    skip "還沒有正式版 tag，沒有東西可以比"
  elif ! git -C "$ROOT" show "$PREV:sdk/testdata/api.golden" >"$BASE/old.golden" 2>/dev/null; then
    skip "$PREV 還沒有 sdk/testdata/api.golden"
  elif [ ! -f "$ROOT/sdk/testdata/api.golden" ]; then
    bad "現在的 sdk/testdata/api.golden 不見了"
  else
    sort -u "$BASE/old.golden" >"$BASE/old.sorted"
    sort -u "$ROOT/sdk/testdata/api.golden" >"$BASE/new.sorted"
    # sdk/testdata/api.removed 登記的是刻意刪的（每行 tab 後面是原因），不算少了。
    { [ -f "$ROOT/sdk/testdata/api.removed" ] && grep -v '^#' "$ROOT/sdk/testdata/api.removed" | cut -f1 | grep -v '^$'; } | sort -u >"$BASE/removed.sorted"
    comm -23 "$BASE/old.sorted" "$BASE/new.sorted" | comm -23 - "$BASE/removed.sorted" >"$BASE/missing.txt"
    if [ -s "$BASE/missing.txt" ]; then
      bad "$PREV 有、現在沒有的 sdk 項目（$(wc -l <"$BASE/missing.txt" | tr -d ' ') 行；舊的程式會認不得）："
      sed 's/^/      /' "$BASE/missing.txt"
    else
      ok "$PREV 的 $(wc -l <"$BASE/old.sorted" | tr -d ' ') 行 sdk 項目現在都還在或登記為刻意刪除（api.removed $(wc -l <"$BASE/removed.sorted" | tr -d ' ') 行；新增 $(comm -13 "$BASE/old.sorted" "$BASE/new.sorted" | wc -l | tr -d ' ') 行）"
    fi
  fi
fi

# ---- 2. fixture 家 -------------------------------------------------------------------------------------------------
# ---- 2. 官方套件的紀錄格式只加欄位，要改就附轉換（DEV-154） ---------------------------------------------------------
if want schema; then
  say "── 2/4 官方套件的紀錄格式：只加欄位，要改就附資料轉換（對 ${PREV:-上一個正式版}）"
  if [ -z "$PREV" ]; then
    skip "還沒有正式版 tag，沒有東西可以比"
  elif ! mkdir -p "$BASE/oldpkgs" || ! git -C "$ROOT" archive "$PREV" packages/official 2>/dev/null | tar -x -C "$BASE/oldpkgs" 2>/dev/null; then
    skip "$PREV 還沒有 packages/official"
  else
    # 比的是紀錄格式，不是格式識別字：產品改名（DEV-158）前的版本 api 跟現在的不同，現在的規則讀不起來。
    # 先把暫存的那份（只動 $BASE 裡的複本）換成現在官方套件寫的 api，再比。
    CUR_API="$(sed -n -E 's/^api *= *"([^"]+)".*/\1/p' "$ROOT"/packages/official/*/package.toml | sort -u | head -1)"
    for f in "$BASE"/oldpkgs/packages/official/*/package.toml; do
      [ -f "$f" ] && [ -n "$CUR_API" ] && sed -i.orig -E "s/^api *= *\"[^\"]*\"/api = \"$CUR_API\"/" "$f" && rm -f "$f.orig"
    done
    OUT="$(cd "$ROOT" && go run ./internal/tools/schemacheck "$BASE/oldpkgs/packages/official" "$ROOT/packages/official" 2>&1)"
    if [ $? = 0 ]; then
      ok "$OUT"
    else
      bad "有紀錄格式改了卻沒有附資料轉換（使用者升級後舊紀錄會讀不懂）："
      sed 's/^/      /' <<<"$OUT"
    fi
  fi
fi

if want fixture; then
  say "── 3/4 fixture 家用這一版開得起來"
  if [ ! -x "$NEW_EXE" ]; then
    bad "找不到 ${NEW_EXE}：先 make build（make compat 會自己先編）"
  else
    FH="$BASE/fx"
    cp -R "$ROOT/cmd/evergenie/testdata/home" "$FH" || bad "複製不了 fixture 家"
    # fixture 的引擎是測試用的假名字，doctor 會報紅字；換成這一版內建的 claude（不會真的開對話：fixture 家 autostart = false）。
    sed -i.bak -e 's/^engine = "test-engine"/engine = "claude"/' -e "s/^port = .*/port = $(freeport)/" "$FH/home.toml" && rm -f "$FH/home.toml.bak"
    check_home "$NEW_EXE" "$FH"
  fi
fi

# ---- 3. 上一個正式版的家 ---------------------------------------------------------------------------------------------
if want prev; then
  say "── 4/4 上一個正式版（${PREV:-?}）建出來的家，換成這一版後遷移得過"
  if [ -z "$PREV" ]; then
    skip "還沒有正式版 tag"
  elif [ ! -x "$NEW_EXE" ]; then
    bad "找不到 ${NEW_EXE}：先 make build"
  else
    PW="$BASE/prev"
    if ! git -C "$ROOT" worktree add --detach "$PW" "$PREV" >"$BASE/wt.log" 2>&1; then
      bad "取不出 ${PREV}：$(tail -1 "$BASE/wt.log")"
    elif ! (cd "$PW" && "${CLEAN[@]}" HOME="$REAL_HOME" make build) >"$BASE/prev-build.log" 2>&1; then
      bad "$PREV 編不起來（log：$(tail -3 "$BASE/prev-build.log" | tr '\n' ' ')）"
    else
      ok "$PREV 編好了（$("${CLEAN[@]}" "$PW/bin/evergenie" version 2>/dev/null)）"
      PH="$BASE/ph"
      if ! out="$("${CLEAN[@]}" "$PW/bin/evergenie" init --home "$PH" --name 相容檢查 --port "$(freeport)" 2>&1)"; then
        bad "$PREV 的 evergenie init 建不了家：$(printf '%s' "$out" | tail -1)"
      else
        ok "用 $PREV 的 evergenie init 建了家"
        if ! out="$("${CLEAN[@]}" EVERGENIE_HOME="$PH" "$NEW_EXE" migrate-home --apply --home "$PH" 2>&1)"; then
          bad "這一版的 migrate-home --apply 沒過：$(printf '%s' "$out" | tail -2 | tr '\n' ' ')"
        else
          ok "這一版的 migrate-home --apply 過了"
          check_home "$NEW_EXE" "$PH"
        fi
      fi
    fi
  fi
fi

say ""
if [ "$FAILED" = 0 ]; then
  say "相容檢查：全過"
  exit 0
fi
say "相容檢查：有 $FAILED 項沒過"
exit 1
