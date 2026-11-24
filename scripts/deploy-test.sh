#!/usr/bin/env bash
# scripts/deploy.sh 的驗收（發版流程缺口 4、REQ-249、REQ-250）。結束碼 0＝全過。
# 全程在暫存資料夾：假的 ssh／evergenie／make／curl 放在最前面的 PATH，repo 是本機的 bare origin＋兩個 clone；
# 不 ssh 到任何機器、不碰真的服務、不編譯。git 用真的（離線）。
set -uo pipefail
MH=test-mini # 測試設定檔給的 mini ssh 名稱（deploy.sh 沒有預設）
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它
for _deploy_env in $(compgen -e | grep '^EVERGENIE_DEPLOY_'); do unset "$_deploy_env"; done  # 開發者殼層 export 的部署設定（環境變數比設定檔優先）不能混進測試
cd "$(dirname "$0")/.."
ROOT="$PWD"
BASE="$(mktemp -d "${TMPDIR:-/tmp}/hg-deploy-test.XXXXXX")"
trap 'rm -rf "$BASE"' EXIT
# 測試用的部署設定檔（真的那份在 ~/.evergenie-release/deploy.env，測試從頭到尾不讀它；21 段另外測沒有設定檔與預設位置）：
# mini 的 ssh 名稱、mini 上要守的服務（假的另一個家 family：舊的命名 agent.family.*，新的 com.<產品名>.family）
cat >"$BASE/deploy.env" <<'EOF'
EVERGENIE_DEPLOY_MINI_HOST=test-mini
EVERGENIE_DEPLOY_MINI_GUARD='agent[.]family[.]|[.]family$'
EOF
export EVERGENIE_DEPLOY_ENV="$BASE/deploy.env"
pass=0; fail=0
ok() { echo "PASS  $1"; pass=$((pass + 1)); }
bad() { echo "FAIL  $1"; fail=$((fail + 1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1"; echo "      --- 輸出 ---"; printf '%s\n' "${OUT:-}" | sed 's/^/      /'; fi; }

# ---- 假的指令 ----------------------------------------------------------------------------------------
mkfakes() {
  local f="$1"
  mkdir -p "$f"
  cat >"$f/ssh" <<'EOF'
#!/usr/bin/env bash
# 假 ssh：記下呼叫，只認 test-mini（測試設定檔給的 mini），把最後的指令在本機跑
echo "ssh $*" >>"$EVERGENIE_FAKE/ssh.log"
host="$1"; shift
[ "$host" = test-mini ] || { echo "假 ssh：不認得的主機 $host" >&2; exit 255; }
eval "$*"
EOF
  cat >"$f/make" <<'EOF'
#!/usr/bin/env bash
echo "make $* (in $PWD)" >>"$EVERGENIE_FAKE/make.log"
v="$(scripts/version.sh)"
mkdir -p bin
if [ "$(cat "$EVERGENIE_FAKE/mode" 2>/dev/null)" = build ] && [ "$v" = "$(cat "$EVERGENIE_FAKE/bad-version" 2>/dev/null)" ]; then
  echo partial >bin/evergenie
  echo "假 make：編不過" >&2
  exit 2
fi
echo "built:$v" >bin/evergenie
EOF
  cat >"$f/evergenie" <<'EOF'
#!/usr/bin/env bash
mode="$(cat "$EVERGENIE_FAKE/mode" 2>/dev/null)"
# mini 開發家（EVERGENIE_HOME＝$EVERGENIE_FAKE/devhome；19af 搬到 $EVERGENIE_FAKE/home/devhome）跑的版本另外記，其他家共用 running
rf="$EVERGENIE_FAKE/running"; case "${EVERGENIE_HOME:-}" in "$EVERGENIE_FAKE/devhome" | "$EVERGENIE_FAKE/home/devhome") rf="$EVERGENIE_FAKE/running-dev" ;; esac
running="$(cat "$rf" 2>/dev/null)"
isbad() { [ "$running" = "$(cat "$EVERGENIE_FAKE/bad-version" 2>/dev/null)" ]; }
case "$1 $2" in
  "service restart")
    echo "restart ${EVERGENIE_HOME:-}" >>"$EVERGENIE_FAKE/restarts.log"
    sed 's/^built://' bin/evergenie >"$rf"
    # push-after-mini／dev-ahead：mini 正式家重開的當下 origin 又多了一個 commit（開發家之後才 fetch，看得到它）；
    # dev-ahead 另外有人在開發家的 repo 手動拉到那個更新的 commit。
    if { [ "$mode" = push-after-mini ] || [ "$mode" = dev-ahead ]; } && [ "${EVERGENIE_HOME:-}" != "$EVERGENIE_FAKE/devhome" ] && [ ! -e "$EVERGENIE_FAKE/pushed" ]; then
      : >"$EVERGENIE_FAKE/pushed"
      (cd "$EVERGENIE_FAKE/dev" && echo later >>feature.txt && git commit -q -am later && git push -q origin HEAD:main)
      if [ "$mode" = dev-ahead ]; then git -C "$EVERGENIE_FAKE/devrepo" pull -q --ff-only origin main; fi
    fi
    # guard-lose／guard-stop：模擬重開時「別人的服務」剛好不見／停了（部署工具不該碰它們，只該發現並大聲說）
    if [ "$mode" = guard-lose ]; then grep -v 'agent.family.board' "$EVERGENIE_FAKE/launchctl" >"$EVERGENIE_FAKE/l.tmp"; mv "$EVERGENIE_FAKE/l.tmp" "$EVERGENIE_FAKE/launchctl"; fi
    # guard-lose-dev：同上，但只在重開 mini 開發家的時候才少
    if [ "$mode" = guard-lose-dev ] && [ "${EVERGENIE_HOME:-}" = "$EVERGENIE_FAKE/devhome" ]; then grep -v 'agent.family.board' "$EVERGENIE_FAKE/launchctl" >"$EVERGENIE_FAKE/l.tmp"; mv "$EVERGENIE_FAKE/l.tmp" "$EVERGENIE_FAKE/launchctl"; fi
    if [ "$mode" = guard-lose-family ]; then grep -v '[.]family$' "$EVERGENIE_FAKE/launchctl" >"$EVERGENIE_FAKE/l.tmp"; mv "$EVERGENIE_FAKE/l.tmp" "$EVERGENIE_FAKE/launchctl"; fi
    if [ "$mode" = guard-stop ]; then sed 's/^761/-/' "$EVERGENIE_FAKE/launchctl" >"$EVERGENIE_FAKE/l.tmp"; mv "$EVERGENIE_FAKE/l.tmp" "$EVERGENIE_FAKE/launchctl"; fi
    # tick-idle／tick-fail／tick-lose：定時型的 tick 部署前在跑（測試先寫好 PID），重開後跑完待命／結束碼 1／不見了
    if [ "$mode" = tick-idle ]; then awk -F'\t' -v OFS='\t' '$3 == "agent.family.tick" { $1 = "-" } 1' "$EVERGENIE_FAKE/launchctl" >"$EVERGENIE_FAKE/l.tmp"; mv "$EVERGENIE_FAKE/l.tmp" "$EVERGENIE_FAKE/launchctl"; fi
    if [ "$mode" = tick-fail ]; then awk -F'\t' -v OFS='\t' '$3 == "agent.family.tick" { $1 = "-"; $2 = 1 } 1' "$EVERGENIE_FAKE/launchctl" >"$EVERGENIE_FAKE/l.tmp"; mv "$EVERGENIE_FAKE/l.tmp" "$EVERGENIE_FAKE/launchctl"; fi
    if [ "$mode" = tick-lose ]; then grep -v 'agent.family.tick' "$EVERGENIE_FAKE/launchctl" >"$EVERGENIE_FAKE/l.tmp"; mv "$EVERGENIE_FAKE/l.tmp" "$EVERGENIE_FAKE/launchctl"; fi
    echo "已重開" ;;
  "config get")
    # slow-port：剛重開時核心還沒起來，第一次 config get 連不到（不能因此去打預設的 8850）
    # （兩把 key 都問一次，所以前兩次都連不到）
    if [ "$mode" = slow-port ] && isbad && [ "$(wc -l <"$EVERGENIE_FAKE/slow-port" 2>/dev/null || echo 0)" -lt 2 ]; then echo x >>"$EVERGENIE_FAKE/slow-port"; echo "連不到 evergenie，請確認 serve 在跑" >&2; exit 1; fi
    echo 18850 ;;
  "status ")
    echo "秘書 狀態" ;;
  "service status")
    echo "status ${EVERGENIE_HOME:-}" >>"$EVERGENIE_FAKE/status.log"
    if [ -e "$EVERGENIE_FAKE/dev-stopped" ]; then echo "com.evergenie.dev：已載入，但現在沒在跑"; else echo "com.evergenie.dev：在跑，pid 4242"; fi
    echo "plist：/x/com.evergenie.dev.plist"
    # dev-wrong-service：開發家沒設自己的 service_label，查到的是別的家的服務
    if [ -e "$EVERGENIE_FAKE/dev-wrong-service" ]; then echo "注意：服務管的家（/x/evergenie-home）跟你現在指定的家（${EVERGENIE_HOME:-}）不同"; fi ;;
  "say "*)
    # 傳話：記下哪個家、給誰、內容；有 say-fail 就失敗（例如那個家沒有這個職位）
    echo "${EVERGENIE_HOME:-} $2 $3" >>"$EVERGENIE_FAKE/say.log"
    if [ -e "$EVERGENIE_FAKE/say-fail" ]; then echo "沒有職位 $2" >&2; exit 1; fi
    echo "已送出" ;;

  "doctor "*|"doctor")
    echo "doctor ${EVERGENIE_HOME:-}" >>"$EVERGENIE_FAKE/doctor.log"
    # doctor-ws：只有工坊那個家（EVERGENIE_HOME 結尾 /ws）在新版有紅字
    if [ "$mode" = doctor-ws ] && isbad && [ "${EVERGENIE_HOME##*/}" = ws ]; then echo "【資料】"; echo "  ✗ 工坊的資料庫打不開"; echo "1 項要處理"; exit 1; fi
    # slow：剛重開時新程式還沒起來，第一次 doctor 說沒在跑
    if [ "$mode" = slow ] && isbad && [ ! -e "$EVERGENIE_FAKE/slow-doctor" ]; then : >"$EVERGENIE_FAKE/slow-doctor"; echo "  ✗ evergenie 沒在跑"; echo "1 項要處理"; exit 1; fi
    if [ "$mode" = doctor ] && isbad; then echo "【資料】"; echo "  ✗ 資料庫打不開"; echo "1 項要處理"; exit 1; fi
    # doctor-dev：只有 mini 開發家在新版有紅字
    if [ "$mode" = doctor-dev ] && isbad && [ "${EVERGENIE_HOME:-}" = "$EVERGENIE_FAKE/devhome" ]; then echo "【資料】"; echo "  ✗ 開發家的資料庫打不開"; echo "1 項要處理"; exit 1; fi
    echo "【設定】"; echo "  ✓ 核心版本：$running"; echo "0 項要處理" ;;
  "packages list")
    if [ "$mode" = packages ] && isbad; then echo '{"id":"closet","state":"failed","enabled":true,"error":"載入失敗：連不上"}'; exit 0; fi
    if [ "$mode" = nopackages ] && isbad; then exit 0; fi
    if [ "$mode" = slow ] && isbad && [ ! -e "$EVERGENIE_FAKE/slow-packages" ]; then : >"$EVERGENIE_FAKE/slow-packages"; echo "連不上 evergenie" >&2; exit 1; fi
    echo '{"id":"habits","state":"running"}'; echo '{"id":"closet","state":"running"}'
    # disabled-pkg：停用的套件帶著原因（依賴的也停用了）不算載入失敗
    if [ "$mode" = disabled-pkg ]; then echo '{"id":"closet-mine","state":"rejected","enabled":false,"error":"需要套件「closet」，但它已停用"}'; fi ;;
  "migrate-home --preview")
    echo "preview ${EVERGENIE_HOME:-}" >>"$EVERGENIE_FAKE/data.log"
    if [ "$mode" = preview ]; then echo "套件「health」的資料轉換有 1 筆轉不過去" >&2; exit 1; fi
    echo "複本上試跑沒問題" ;;
  "upgrade --deploy")
    # 部署的資料保護（begin／undo／done）：記下順序與當時 bin/ 是哪一版（undo 要在換回 bin/ 之前，用新版跑）
    echo "$3 ${EVERGENIE_HOME:-} $(cat bin/evergenie)" >>"$EVERGENIE_FAKE/data.log"
    if [ "$mode" = backup ] && [ "$3" = begin ]; then echo "備份失敗，還沒有動任何東西" >&2; exit 1; fi
    echo "ok $3" ;;
  "upgrade --report")
    echo "report ${EVERGENIE_HOME:-}" >>"$EVERGENIE_FAKE/data.log"; echo "主人自己的套件都正常，沒有要交給秘書的" ;;
  "upgrade --status")
    if [ "$mode" = nostatus ]; then echo "當掉了" >&2; exit 1; fi
    if [ "$mode" = migrated ]; then echo '{"current":"x","awaiting_accept":{"from":"a","to":"b","migrations":["pkg.health.v2"]}}'; else echo '{"current":"x"}'; fi ;;
  *) echo "假 evergenie：不認得 $*" >&2; exit 64 ;;
esac
EOF
  cat >"$f/curl" <<'EOF'
#!/usr/bin/env bash
mode="$(cat "$EVERGENIE_FAKE/mode" 2>/dev/null)"
rf="$EVERGENIE_FAKE/running"; case "${EVERGENIE_HOME:-}" in "$EVERGENIE_FAKE/devhome" | "$EVERGENIE_FAKE/home/devhome") rf="$EVERGENIE_FAKE/running-dev" ;; esac
running="$(cat "$rf" 2>/dev/null)"
isbad() { [ "$running" = "$(cat "$EVERGENIE_FAKE/bad-version" 2>/dev/null)" ]; }
echo "curl $*" >>"$EVERGENIE_FAKE/curl.log"
if [ "$mode" = board ] && isbad; then exit 22; fi
# slow-port：預設的 8850 是同一台別的家，回的是它的舊 bundle
case "$*" in *127.0.0.1:8850/*) echo '<script type="module" src="/assets/index-OTHERHOME.js"></script>'; exit 0 ;; esac
# slow：剛重開時還是舊程式在回看板（舊 bundle），第二次才是新的
if [ "$mode" = slow ] && isbad && [ ! -e "$EVERGENIE_FAKE/slow-curl" ]; then : >"$EVERGENIE_FAKE/slow-curl"; echo '<script type="module" src="/assets/index-OLDOLD.js"></script>'; exit 0; fi
if [ "$mode" = bundle ] && isbad; then echo '<script type="module" src="/assets/index-ZZZZZZ.js"></script>'; exit 0; fi
echo '<script type="module" src="/assets/index-AAAAAA.js"></script>'
EOF
  cat >"$f/launchctl" <<'EOF'
#!/usr/bin/env bash
# 假 launchctl：認 list 與 print。list 的內容在 $EVERGENIE_FAKE/launchctl（PID Status Label）；有 launchctl-fail 就失敗
# print gui/<uid>/<名稱>：名稱列在 $EVERGENIE_FAKE/timers（一行一個）的是定時型，印 run interval；其他不印（真的 launchctl 也是這樣）
echo "launchctl $*" >>"$EVERGENIE_FAKE/launchctl.log"
[ -e "$EVERGENIE_FAKE/launchctl-fail" ] && exit 1
if [ "$1" = print ]; then
  label="${2##*/}"
  grep -qx "$label" "$EVERGENIE_FAKE/timers" 2>/dev/null && printf '\tstate = not running\n\trun interval = 60 seconds\n'
  exit 0
fi
[ "$1" = list ] || exit 64
printf 'PID\tStatus\tLabel\n'
cat "$EVERGENIE_FAKE/launchctl" 2>/dev/null
EOF
  chmod +x "$f"/*
}

# 每個情境一個全新的環境：T（假指令與記錄）、origin、dev（出新版用）、repo（被部署的那台）。repo 已經「部署過」舊版。
newenv() {
  T="$(mktemp -d "$BASE/env.XXXXXX")"
  export EVERGENIE_FAKE="$T"
  mkfakes "$T/fakebin"
  git init -q --bare -b main "$T/origin.git"
  git clone -q "$T/origin.git" "$T/dev" 2>/dev/null
  (
    cd "$T/dev" || exit 1
    git config user.email t@example.com; git config user.name t
    mkdir -p internal/base/app/board/dist/assets
    echo '<script type="module" src="/assets/index-AAAAAA.js"></script>' >internal/base/app/board/dist/index.html
    echo 'x' >internal/base/app/board/dist/assets/index-AAAAAA.js
    printf 'bin/\nbin.*/\n' >.gitignore
    mkdir -p scripts && cp "$ROOT/scripts/version.sh" scripts/version.sh
    echo one >feature.txt
    git add -A && git commit -q -m one && git push -q origin HEAD:main
  )
  git clone -q "$T/origin.git" "$T/repo" 2>/dev/null
  git -C "$T/repo" config user.email t@example.com; git -C "$T/repo" config user.name t
  OLDV="$("$T/repo/scripts/version.sh")"
  mkdir -p "$T/repo/bin"
  echo "built:$OLDV" >"$T/repo/bin/evergenie"
  echo "$OLDV" >"$T/repo/bin/.deployed-version"
  echo "$OLDV" >"$T/running"
  : >"$T/ssh.log"; : >"$T/restarts.log"; : >"$T/make.log"; : >"$T/curl.log"; : >"$T/doctor.log"; : >"$T/launchctl.log"; : >"$T/say.log"
  rm -f "$T/mode" "$T/bad-version" "$T/launchctl-fail" "$T/dev-stopped" "$T/dev-wrong-service" "$T/say-fail" "$T/pushed"
  echo agent.family.tick >"$T/timers"
  rm -rf "$T/home"; mkdir -p "$T/home/Library/LaunchAgents"
  printf '775\t0\tagent.family.tailscale\n-\t0\tagent.family.tick\n761\t0\tagent.family.board\n501\t0\tcom.evergenie.serve\n' >"$T/launchctl"
}
# 工坊跑的 repo（開發用主資料夾）：另一個 clone，停在舊版、bin/ 是舊版
newws() {
  git clone -q "$T/origin.git" "$T/wsrepo" 2>/dev/null
  git -C "$T/wsrepo" config user.email t@example.com; git -C "$T/wsrepo" config user.name t
  git -C "$T/wsrepo" reset -q --hard "$(git -C "$T/repo" rev-parse HEAD)"
  mkdir -p "$T/wsrepo/bin"
  echo "built:$OLDV" >"$T/wsrepo/bin/evergenie"
}
# mini 開發家（DEV-157）：跑的 repo 是 mini 上另一個 clone（停在舊版、bin/ 是舊版），家是 devhome（有 home.toml），服務在跑舊版
newdev() {
  git clone -q "$T/origin.git" "$T/devrepo" 2>/dev/null
  git -C "$T/devrepo" config user.email t@example.com; git -C "$T/devrepo" config user.name t
  git -C "$T/devrepo" reset -q --hard "$(git -C "$T/repo" rev-parse HEAD)"
  mkdir -p "$T/devrepo/bin" "$T/devhome"
  echo "built:$OLDV" >"$T/devrepo/bin/evergenie"
  echo '[runtime]' >"$T/devhome/home.toml"
  echo "$OLDV" >"$T/running-dev"
}
# 出一個新版（dev → origin），回傳它的版本（scripts/version.sh）到 NEWV
newcommit() {
  (cd "$T/dev" && echo "$RANDOM" >>feature.txt && git add -A && git commit -q -m "change $RANDOM" && git push -q origin HEAD:main)
  NEWV="$("$T/dev/scripts/version.sh")"
}
deploy() {
  OUT="$(cd "$ROOT" && HOME="$T/home" PATH="$T/fakebin:$PATH" EVERGENIE_FAKE="$T" EVERGENIE_DEPLOY_REPO="$T/repo" EVERGENIE_DEPLOY_SHELL="bash -c" EVERGENIE_DEPLOY_TRIES="${DTRIES:-2}" EVERGENIE_DEPLOY_HOMES_FILE="$T/homes" \
    EVERGENIE_DEPLOY_SELF_UPDATE=0 EVERGENIE_DEPLOY_GUARD_TRIES=2 EVERGENIE_DEPLOY_WORKSHOP_REPO="$T/wsrepo" EVERGENIE_DEPLOY_WORKSHOP_HOME="$T/ws" \
    EVERGENIE_DEPLOY_MINI_DEV_REPO="$T/devrepo" EVERGENIE_DEPLOY_MINI_DEV_HOME="$T/devhome" scripts/deploy.sh "$@" 2>&1)"
  RC=$?
}
restarts() { wc -l <"$T/restarts.log" | tr -d ' '; }
binv() { cat "$T/repo/bin/evergenie"; }

# 1. 成功路徑：mini 經 ssh（設定檔給的 test-mini），repo 快轉、bin.prev 是舊版、服務重開一次、版本記下來
newenv; newcommit
deploy mini
check "1a 成功：結束碼 0、印完成" "[ $RC = 0 ] && grep -q '✓ mini 部署完成' <<<\"\$OUT\""
check "1b 經 ssh 連 ${MH}（每次呼叫都是 ${MH}）" "grep -q '^ssh ${MH} ' '$T/ssh.log' && ! grep '^ssh ' '$T/ssh.log' | grep -v -q '^ssh ${MH} '"
check "1c repo 快轉到最新 main" "[ \"\$(git -C '$T/repo' rev-parse HEAD)\" = \"\$(git -C '$T/dev' rev-parse HEAD)\" ]"
check "1d 舊 bin/ 留成 bin.prev/，bin/ 是新版" "[ \"\$(cat '$T/repo/bin.prev/evergenie')\" = 'built:$OLDV' ] && [ \"\$(binv)\" = 'built:$NEWV' ]"
check "1e 服務只重開一次，跑著的是新版" "[ \"\$(restarts)\" = 1 ] && [ \"\$(cat '$T/running')\" = '$NEWV' ]"
check "1f 檢查都有跑（看板、doctor、套件各一個 OK）" "grep -q 'OK 看板首頁 200' <<<\"\$OUT\" && grep -q 'OK doctor 沒有紅字，核心版本 $NEWV' <<<\"\$OUT\" && grep -q 'OK 套件 2 個' <<<\"\$OUT\""
check "1g 成功後 bin/.deployed-version 是新版" "[ \"\$(cat '$T/repo/bin/.deployed-version')\" = '$NEWV' ]"
check "1h 有 make build" "grep -q '^make build' '$T/make.log'"

# 2. 再部署一次（已經是最新）：不重做、bin.prev 不被蓋、不重開
deploy mini
check "2a 已是最新：結束碼 0、說已經是最新版" "[ $RC = 0 ] && grep -q '已經是最新版' <<<\"\$OUT\""
check "2b bin.prev/ 還是上一版、沒有再重開、沒有再編" "[ \"\$(cat '$T/repo/bin.prev/evergenie')\" = 'built:$OLDV' ] && [ \"\$(restarts)\" = 1 ] && [ \"\$(grep -c '^make build' '$T/make.log')\" = 1 ]"

# 3. 部署後檢查沒過 → 自動換回（四種：doctor 紅字、bundle 不一致、看板打不開、套件載入失敗/沒有套件）
for m in doctor bundle board packages nopackages; do
  newenv; newcommit
  echo "$m" >"$T/mode"; echo "$NEWV" >"$T/bad-version"
  deploy mini
  check "3-$m 檢查沒過：結束碼 1，輸出有 FAIL 與白話原因、自動換回" "[ $RC = 1 ] && grep -q 'FAIL' <<<\"\$OUT\" && grep -q '部署後檢查沒過' <<<\"\$OUT\" && grep -q '自動換回上一版' <<<\"\$OUT\""
  check "3-$m bin/ 換回舊版、壞的留在 bin.failed/、不留 bin.prev/" "[ \"\$(binv)\" = 'built:$OLDV' ] && [ \"\$(cat '$T/repo/bin.failed/evergenie')\" = 'built:$NEWV' ] && [ ! -d '$T/repo/bin.prev' ]"
  check "3-$m 服務重開兩次（新版一次、換回一次），跑著的是舊版，.deployed-version 還是舊版" "[ \"\$(restarts)\" = 2 ] && [ \"\$(cat '$T/running')\" = '$OLDV' ] && [ \"\$(cat '$T/repo/bin/.deployed-version')\" = '$OLDV' ]"
done
newenv; newcommit; echo doctor >"$T/mode"; echo "$NEWV" >"$T/bad-version"; deploy mini
check "3a doctor 紅字時印出 doctor 的內容" "grep -q '資料庫打不開' <<<\"\$OUT\""
newenv; newcommit; echo bundle >"$T/mode"; echo "$NEWV" >"$T/bad-version"; deploy mini
check "3b bundle 不一致時說出兩邊的名稱" "grep -q 'index-ZZZZZZ.js' <<<\"\$OUT\" && grep -q 'index-AAAAAA.js' <<<\"\$OUT\""

# 3d. 資料保護（DEV-154）：成功的部署 begin → done；檢查沒過 begin → undo（用新版、在換回 bin/ 之前）；
#     試跑沒過或備份沒做成：服務不重開、bin/ 換回。
newenv; newcommit; deploy mini
check "3d-1 成功：先試跑、備份，重開，檢查過了才收尾與報告" "[ $RC = 0 ] && [ \"\$(cut -d' ' -f1 '$T/data.log' | tr '\n' ' ')\" = 'preview begin done report ' ]"
newenv; newcommit; echo doctor >"$T/mode"; echo "$NEWV" >"$T/bad-version"; deploy mini
check "3d-2 檢查沒過：備份過的家先還原（用新版跑）再換回 bin/，不收尾" "[ $RC = 1 ] && grep -q \"^undo .*built:$NEWV\" '$T/data.log' && ! grep -q '^done' '$T/data.log' && [ \"\$(binv)\" = 'built:$OLDV' ]"
newenv; newcommit; echo preview >"$T/mode"; deploy mini
check "3d-3 複本上試跑沒過：結束碼 1、說資料沒動、沒重開、沒備份、bin/ 換回" "[ $RC = 1 ] && grep -q '試跑沒過' <<<\"\$OUT\" && [ \"\$(restarts)\" = 0 ] && ! grep -q '^begin' '$T/data.log' && [ \"\$(binv)\" = 'built:$OLDV' ]"
newenv; newcommit; echo backup >"$T/mode"; deploy mini
check "3d-4 備份沒做成：結束碼 1、沒重開、bin/ 換回" "[ $RC = 1 ] && grep -q '備份沒做成' <<<\"\$OUT\" && [ \"\$(restarts)\" = 0 ] && [ \"\$(binv)\" = 'built:$OLDV' ]"
newenv; newcommit; deploy mini; echo migrated >"$T/mode"; deploy --rollback mini
check "3d-5 上次更新整理過資料：--rollback 不只換程式，說要用連資料一起退回的那條路" "[ $RC = 1 ] && grep -q 'upgrade --rollback' <<<\"\$OUT\" && [ \"\$(binv)\" = 'built:$NEWV' ]"
newenv; newcommit; deploy mini; echo nostatus >"$T/mode"; deploy --rollback mini
check "3d-6 查不到上次更新有沒有整理過資料：--rollback 先不換回" "[ $RC = 1 ] && grep -q '查不到上次更新有沒有整理過資料' <<<\"\$OUT\" && [ \"\$(binv)\" = 'built:$NEWV' ]"

# 3c. 重開後第一輪檢查時舊程式還在回看板、新程式還沒起來：重試到全部 OK，不誤判成壞掉、不換回
newenv; newcommit; echo slow >"$T/mode"; echo "$NEWV" >"$T/bad-version"; rm -f "$T"/slow-*
DTRIES=5 deploy mini  # 多給幾秒：機器很忙時第一輪檢查可能就超過 1 秒
check "3c 起得慢的新版：結束碼 0、部署完成、沒換回、只重開一次、沒印 FAIL" "[ $RC = 0 ] && grep -q '✓ mini 部署完成' <<<\"\$OUT\" && ! grep -q '自動換回' <<<\"\$OUT\" && ! grep -q 'FAIL' <<<\"\$OUT\" && [ \"\$(restarts)\" = 1 ] && [ -e '$T/slow-curl' ] && [ -e '$T/slow-doctor' ] && [ -e '$T/slow-packages' ]"
# 3c-2. 重開後第一輪讀 port 時核心還沒起來：不能退回預設 8850（同一台別的家），重讀到真的 port 再驗
newenv; newcommit; echo slow-port >"$T/mode"; echo "$NEWV" >"$T/bad-version"; rm -f "$T"/slow-*
DTRIES=5 deploy mini
check "3c-2 讀不到 port 時不打 8850：結束碼 0、部署完成、沒換回、沒打過 8850" "[ $RC = 0 ] && grep -q '✓ mini 部署完成' <<<\"\$OUT\" && ! grep -q '自動換回' <<<\"\$OUT\" && [ -e '$T/slow-port' ] && ! grep -q '127.0.0.1:8850' '$T/curl.log'"
# 3d. 停用的套件帶著原因（依賴的也停用了）：不算載入失敗
newenv; newcommit; echo disabled-pkg >"$T/mode"
deploy mini
check "3d 停用中的套件有 error 不擋部署：結束碼 0、套件 3 個" "[ $RC = 0 ] && grep -q '✓ mini 部署完成' <<<\"\$OUT\" && grep -q 'OK 套件 3 個' <<<\"\$OUT\""

# 4. 編譯失敗：不重開服務，換回舊 bin
newenv; newcommit; echo build >"$T/mode"; echo "$NEWV" >"$T/bad-version"
deploy mini
check "4a 編譯失敗：結束碼 1、說 make build 失敗、服務沒重開" "[ $RC = 1 ] && grep -q 'make build 失敗' <<<\"\$OUT\" && [ \"\$(restarts)\" = 0 ]"
check "4b bin/ 換回舊版（不是編到一半的）" "[ \"\$(binv)\" = 'built:$OLDV' ] && [ ! -d '$T/repo/bin.prev' ]"

# 5. 沒辦法快轉 / repo 不乾淨：不動 bin/
newenv; newcommit
echo local-edit >>"$T/repo/feature.txt"
deploy mini
check "5a repo 有沒提交的改動：結束碼 1、白話說明、沒動 bin/、沒重開" "[ $RC = 1 ] && grep -q '沒提交的改動' <<<\"\$OUT\" && [ \"\$(binv)\" = 'built:$OLDV' ] && [ \"\$(restarts)\" = 0 ] && [ ! -d '$T/repo/bin.prev' ]"
newenv; newcommit
(cd "$T/repo" && echo diverge >>feature.txt && git commit -q -am diverge)
deploy mini
check "5b main 分岔（不能快轉）：結束碼 1、沒動 bin/、沒重開" "[ $RC = 1 ] && grep -q 'ff-only' <<<\"\$OUT\" && [ \"\$(binv)\" = 'built:$OLDV' ] && [ \"\$(restarts)\" = 0 ]"

# 6. --rollback：成功部署後手動換回
newenv; newcommit; deploy mini
deploy --rollback mini
check "6a --rollback：結束碼 0、說已換回" "[ $RC = 0 ] && grep -q '已換回上一版' <<<\"\$OUT\""
check "6b bin/ 是舊版、bin.prev 沒了、重開、跑著的是舊版、版本記號是舊版" "[ \"\$(binv)\" = 'built:$OLDV' ] && [ ! -d '$T/repo/bin.prev' ] && [ \"\$(restarts)\" = 2 ] && [ \"\$(cat '$T/running')\" = '$OLDV' ] && [ \"\$(cat '$T/repo/bin/.deployed-version')\" = '$OLDV' ]"
check "6c 換回後有跑檢查（核心版本對到舊版）" "grep -q 'OK doctor 沒有紅字，核心版本 $OLDV' <<<\"\$OUT\""
deploy --rollback mini
check "6d 再換一次：沒有 bin.prev/，結束碼 1 並說明，不動 bin/" "[ $RC = 1 ] && grep -q '沒有 bin.prev' <<<\"\$OUT\" && [ \"\$(binv)\" = 'built:$OLDV' ]"
deploy --rollback all
check "6e --rollback all 是用法錯誤（結束碼 2）" "[ $RC = 2 ]"

# 7. --dry-run：什麼都不執行（沒有 ssh、repo 沒動、沒重開、沒編譯）
newenv; newcommit
HEAD0="$(git -C "$T/repo" rev-parse HEAD)"
deploy --dry-run mini
check "7a dry-run 結束碼 0，印出計畫（merge --ff-only、make build、service restart、檢查）" "[ $RC = 0 ] && grep -q 'git merge --ff-only origin/main' <<<\"\$OUT\" && grep -q 'make build' <<<\"\$OUT\" && grep -q 'evergenie service restart' <<<\"\$OUT\" && grep -q '檢查' <<<\"\$OUT\" && grep -q '${MH}' <<<\"\$OUT\""
check "7b 沒有 ssh、沒有 make、沒有重開、沒有 curl" "[ ! -s '$T/ssh.log' ] && [ ! -s '$T/make.log' ] && [ \"\$(restarts)\" = 0 ] && [ ! -s '$T/curl.log' ]"
check "7c repo 與 bin/ 都沒動" "[ \"\$(git -C '$T/repo' rev-parse HEAD)\" = '$HEAD0' ] && [ \"\$(binv)\" = 'built:$OLDV' ] && [ ! -d '$T/repo/bin.prev' ]"
deploy --dry-run --rollback office
check "7d --rollback --dry-run 也只印不做" "[ $RC = 0 ] && grep -q 'bin.prev' <<<\"\$OUT\" && [ ! -s '$T/ssh.log' ] && [ \"\$(restarts)\" = 0 ] && [ \"\$(binv)\" = 'built:$OLDV' ]"
deploy --dry-run all
check "7e dry-run all：mini 在 office 前面" "[ $RC = 0 ] && [ \"\$(grep -n '== mini（' <<<\"\$OUT\" | cut -d: -f1)\" -lt \"\$(grep -n '== office' <<<\"\$OUT\" | cut -d: -f1)\" ]"

# 8. office 是本機（不經 ssh）；all＝mini 先、mini 沒過就不碰 office
newenv; newcommit
deploy office
check "8a office 在本機跑：成功、沒有 ssh" "[ $RC = 0 ] && grep -q '✓ office 部署完成' <<<\"\$OUT\" && [ ! -s '$T/ssh.log' ] && [ \"\$(cat '$T/running')\" = '$NEWV' ]"
newenv; newcommit
deploy all
check "8b all：mini 先、office 後，都成功（同一個 repo，office 那次發現已是最新）" "[ $RC = 0 ] && [ \"\$(grep -n '== mini（' <<<\"\$OUT\" | cut -d: -f1)\" -lt \"\$(grep -n '== office' <<<\"\$OUT\" | cut -d: -f1)\" ] && grep -q '✓ mini 部署完成' <<<\"\$OUT\" && grep -q '已經是最新版' <<<\"\$OUT\""
newenv; newcommit; echo doctor >"$T/mode"; echo "$NEWV" >"$T/bad-version"
deploy all
check "8c all：mini 沒過，結束碼 1、說 office 沒有動、沒有 office 的段落" "[ $RC = 1 ] && grep -q 'office 沒有動' <<<\"\$OUT\" && ! grep -q '== office' <<<\"\$OUT\""

# 9. 用法
deploy
check "9a 沒給機器：結束碼 2 與用法" "[ $RC = 2 ] && grep -q '用法' <<<\"\$OUT\""
deploy staging
check "9b 不認得的機器：結束碼 2" "[ $RC = 2 ]"
deploy --bogus mini
check "9c 不認得的選項：結束碼 2" "[ $RC = 2 ]"

# 10. 換不回去（第一次部署，沒有舊 bin/ 可留）：結束碼 3、不假裝成功
newenv; newcommit
rm -rf "$T/repo/bin"
echo doctor >"$T/mode"; echo "$NEWV" >"$T/bad-version"
deploy mini
check "10 沒有舊版可換回：檢查沒過時結束碼 3，並說要人工處理" "[ $RC = 3 ] && grep -q '換不回去' <<<\"\$OUT\" && grep -q '人工處理' <<<\"\$OUT\""

# 12. 版號從 tag 來（REQ-277）：新版在 v0.3.0 上 → 部署記下 0.3.0（不是 commit 短碼）；之後又多一個 commit → 0.3.0-1-g<短碼>
newenv; newcommit
(cd "$T/dev" && git tag -a v0.3.0 -m x && git push -q origin v0.3.0)
NEWV="$("$T/dev/scripts/version.sh")"
deploy mini
check "12a 在 tag 上部署：版本是 0.3.0，doctor 對得上" "[ $RC = 0 ] && [ \"\$NEWV\" = 0.3.0 ] && [ \"\$(cat '$T/repo/bin/.deployed-version')\" = 0.3.0 ] && grep -q 'OK doctor 沒有紅字，核心版本 0.3.0' <<<\"\$OUT\""
newcommit
deploy mini
check "12b tag 之後多一個 commit：版本是 0.3.0-1-g<短碼>" "[ $RC = 0 ] && [[ \"\$NEWV\" == 0.3.0-1-g* ]] && [ \"\$(cat '$T/repo/bin/.deployed-version')\" = \"\$NEWV\" ]"

# 13. 一台兩個家（GOAL-2 W2，日常＋工坊）：~/.evergenie-homes 列兩個家（含註解與空行）→ 編一次、兩個家各重開一次、各跑一次檢查
newenv; newcommit
printf '# 這台的家\n%s\n\n  %s  # 工坊\n' "$T/daily" "$T/ws" >"$T/homes"
deploy mini
check "13a 兩個家：結束碼 0、說這台有 2 個家" "[ $RC = 0 ] && grep -q '這台有 2 個家' <<<\"\$OUT\" && grep -q '✓ mini 部署完成' <<<\"\$OUT\""
check "13b 只編一次，兩個家各重開一次（各帶自己的 EVERGENIE_HOME）" "[ \"\$(grep -c '^make build' '$T/make.log')\" = 1 ] && [ \"\$(restarts)\" = 2 ] && grep -qx 'restart $T/daily' '$T/restarts.log' && grep -qx 'restart $T/ws' '$T/restarts.log'"
check "13c 兩個家各跑一次 doctor 檢查" "grep -qx 'doctor $T/daily' '$T/doctor.log' && grep -qx 'doctor $T/ws' '$T/doctor.log' && [ \"\$(grep -c 'OK doctor 沒有紅字' <<<\"\$OUT\")\" = 2 ]"

# 14. 兩個家只有工坊沒過：整台換回舊版，兩個家都重開回舊版
newenv; newcommit
printf '%s\n%s\n' "$T/daily" "$T/ws" >"$T/homes"
echo doctor-ws >"$T/mode"; echo "$NEWV" >"$T/bad-version"
deploy mini
check "14a 工坊沒過：結束碼 1、印出工坊的紅字、自動換回" "[ $RC = 1 ] && grep -q '工坊的資料庫打不開' <<<\"\$OUT\" && grep -q '自動換回上一版' <<<\"\$OUT\""
check "14b 兩個家都重開兩次（新版、換回），跑著的是舊版" "[ \"\$(grep -cx 'restart $T/daily' '$T/restarts.log')\" = 2 ] && [ \"\$(grep -cx 'restart $T/ws' '$T/restarts.log')\" = 2 ] && [ \"\$(binv)\" = 'built:$OLDV' ] && [ \"\$(cat '$T/running')\" = '$OLDV' ]"

# 15. --rollback 也照家清單：每個家各重開一次
newenv; newcommit
deploy mini
printf '%s\n%s\n' "$T/daily" "$T/ws" >"$T/homes"
: >"$T/restarts.log"
deploy --rollback mini
check "15 --rollback 兩個家：結束碼 0、兩個家各重開一次" "[ $RC = 0 ] && grep -qx 'restart $T/daily' '$T/restarts.log' && grep -qx 'restart $T/ws' '$T/restarts.log' && [ \"\$(restarts)\" = 2 ]"


# 16. 守住 mini 上別人的服務（D-53；樣式在設定檔，這裡是假的家 family）：部署前後只讀 launchctl list 比對；不碰它們
newenv; newcommit
deploy mini
check "16a 服務都在：部署成功、印出部署前 3 個（2 個在跑）、部署後 OK" "[ $RC = 0 ] && grep -q '要守的服務（部署前）：3 個（2 個在跑）' <<<\"\$OUT\" && grep -q 'OK 要守的服務都還在' <<<\"\$OUT\""
check "16b 只讀 launchctl（list、print；沒有 load／unload／kickstart 之類）" "[ -s '$T/launchctl.log' ] && ! grep -v -E '^launchctl (list|print gui/[0-9]+/[^ ]+)$' '$T/launchctl.log' | grep -q ."
newenv; newcommit; echo guard-lose >"$T/mode"
deploy mini
check "16c 重開後少了一個：結束碼 4、大聲說少了哪個、新版照樣留著（換回也救不了）" "[ $RC = 4 ] && grep -q '要守的服務' <<<\"\$OUT\" && grep -q '不見的：agent.family.board' <<<\"\$OUT\" && grep -q '底線' <<<\"\$OUT\" && [ \"\$(binv)\" = 'built:$NEWV' ]"
newenv; newcommit; echo guard-stop >"$T/mode"
deploy mini
check "16d 本來在跑的停了：結束碼 4、說哪個停了" "[ $RC = 4 ] && grep -q '本來在跑、現在停了：agent.family.board' <<<\"\$OUT\""
# 換成新的命名 com.<產品名>.family 後，名稱不再有舊的 agent.family. 開頭，還是要守得到
newenv; newcommit; printf '775\t0\tagent.family.tailscale\n87987\t0\tcom.evergenie.family\n501\t0\tcom.evergenie.serve\n' >"$T/launchctl"; echo guard-lose-family >"$T/mode"
deploy mini
check "16d2 新命名的 com.evergenie.family 不見了：結束碼 4、說不見的是它" "[ $RC = 4 ] && grep -q '不見的：com.evergenie.family' <<<\"\$OUT\" && grep -q 'grep -E' <<<\"\$OUT\""
# 改名（DEV-158）前建的家，服務名的產品名是舊的：一樣要守得到（樣式只比 .family、不比產品名）
newenv; newcommit; printf '775\t0\tagent.family.tailscale\n87987\t0\tcom.prevname.family\n501\t0\tcom.evergenie.serve\n' >"$T/launchctl"; echo guard-lose-family >"$T/mode"
deploy mini
check "16d3 產品名不同的 com.prevname.family 不見了：結束碼 4、說不見的是它" "[ $RC = 4 ] && grep -q '不見的：com.prevname.family' <<<\"\$OUT\""
# 16n–16q（DEV-119）：定時型（StartInterval）的 agent.family.tick 每分鐘跑一次，跑完沒有 PID 是正常的
tickrun() { printf '775\t0\tagent.family.tailscale\n900\t0\tagent.family.tick\n761\t0\tagent.family.board\n501\t0\tcom.evergenie.serve\n' >"$T/launchctl"; }
newenv; newcommit; tickrun; echo tick-idle >"$T/mode"
deploy mini
check "16n 定時型部署前剛好在跑、部署後跑完待命（結束碼 0）：不誤報，結束碼 0" "[ $RC = 0 ] && grep -q '要守的服務（部署前）：3 個（3 個在跑）' <<<\"\$OUT\" && grep -q 'OK 要守的服務都還在' <<<\"\$OUT\" && grep -q 'agent.family.tick（定時待命）' <<<\"\$OUT\" && ! grep -q '現在停了' <<<\"\$OUT\""
newenv; newcommit; tickrun; echo tick-fail >"$T/mode"
deploy mini
check "16o 定時型跑完結束碼不是 0：結束碼 4、說是定時服務結束碼不對（不是說「停了」）" "[ $RC = 4 ] && grep -q '定時服務上一次結束碼不是 0（本來是好的）：agent.family.tick' <<<\"\$OUT\" && ! grep -q '本來在跑、現在停了' <<<\"\$OUT\""
newenv; newcommit; tickrun; echo tick-lose >"$T/mode"
deploy mini
check "16p 定時型服務不見了：結束碼 4、說不見的是它" "[ $RC = 4 ] && grep -q '不見的：agent.family.tick' <<<\"\$OUT\""
newenv; newcommit; printf '775\t0\tagent.family.tailscale\n-\t0\tagent.family.tick\n761\t0\tagent.family.board\n501\t0\tcom.evergenie.serve\n' >"$T/launchctl"; : >"$T/timers"
deploy mini
check "16q 不是定時型、沒 PID（常駐型本來就停著）：照舊算停著、不是正常待命" "[ $RC = 0 ] && grep -q 'agent.family.tick（停著）' <<<\"\$OUT\" && grep -q '3 個（2 個在跑）' <<<\"\$OUT\""
idle() { printf '775\t0\tagent.family.tailscale\n-\t%s\tagent.family.tick\n761\t0\tagent.family.board\n501\t0\tcom.evergenie.serve\n' "$1" >"$T/launchctl"; }
newenv; newcommit; idle 0; echo tick-fail >"$T/mode"
deploy mini
check "16r 定時型部署前是定時待命、部署後結束碼 1：結束碼 4、說定時服務結束碼不對" "[ $RC = 4 ] && grep -q 'agent.family.tick（定時待命）' <<<\"\$OUT\" && grep -q '定時服務上一次結束碼不是 0（本來是好的）：agent.family.tick' <<<\"\$OUT\""
newenv; newcommit; idle 1
deploy mini
check "16s 定時型部署前就已經結束碼不是 0（定時異常）、部署後還是：不怪部署，結束碼 0" "[ $RC = 0 ] && grep -q 'agent.family.tick（定時異常）' <<<\"\$OUT\" && grep -q 'OK 要守的服務都還在' <<<\"\$OUT\""
newenv; newcommit; idle 0; : >"$T/timers"; printf '<key>StartCalendarInterval</key>\n' >"$T/home/Library/LaunchAgents/agent.family.tick.plist"
deploy mini
check "16t launchctl print 看不出定時型、但 plist 有 StartCalendarInterval：算定時待命（後備路徑）" "[ $RC = 0 ] && grep -q 'agent.family.tick（定時待命）' <<<\"\$OUT\""
newenv; newcommit; echo doctor >"$T/mode"; echo "$NEWV" >"$T/bad-version"
deploy mini
check "16e 部署沒過換回、服務也都在：結束碼 1（不是 4）" "[ $RC = 1 ] && grep -q 'OK 要守的服務都還在' <<<\"\$OUT\""
newenv; newcommit; touch "$T/launchctl-fail"
deploy mini
check "16f 部署前讀不到 launchctl：結束碼 1、說先不部署、沒動 repo 與 bin/、沒重開" "[ $RC = 1 ] && grep -q '先不部署' <<<\"\$OUT\" && [ \"\$(binv)\" = 'built:$OLDV' ] && [ \"\$(restarts)\" = 0 ] && [ \"\$(git -C '$T/repo' rev-parse HEAD)\" != \"\$(git -C '$T/dev' rev-parse HEAD)\" ]"
newenv; newcommit
deploy office
check "16g office 預設不守（不叫 launchctl）" "[ $RC = 0 ] && [ ! -s '$T/launchctl.log' ]"
newenv; newcommit
deploy --dry-run mini
check "16h dry-run 說會守、但不叫 launchctl" "[ $RC = 0 ] && grep -q '要守的服務' <<<\"\$OUT\" && [ ! -s '$T/launchctl.log' ]"
newenv; newcommit; printf '501\t0\tcom.evergenie.serve\n' >"$T/launchctl"
deploy mini
check "16i 部署前一個要守的服務都沒看到（多半是讀錯地方）：先不部署、沒動 bin/、沒重開" "[ $RC = 1 ] && grep -q '都沒看到' <<<\"\$OUT\" && [ \"\$(binv)\" = 'built:$OLDV' ] && [ \"\$(restarts)\" = 0 ]"
newenv; newcommit; echo guard-lose >"$T/mode"
deploy all
check "16j all：mini 的服務少了就不碰 office、結束碼 4、最後再說一次" "[ $RC = 4 ] && grep -q 'office 沒有動：先處理 mini' <<<\"\$OUT\" && ! grep -q '== office' <<<\"\$OUT\""

# 17. 工坊（D-53）：--workshop 在 office 成功後接著做；工坊的 repo 是另一個（開發用主資料夾），家固定是工坊
newenv; newcommit; newws
deploy workshop
check "17a 只更新工坊：結束碼 0、工坊的 repo 快轉到最新 main、bin/ 是新版" "[ $RC = 0 ] && grep -q '✓ 工坊 部署完成' <<<\"\$OUT\" && [ \"\$(git -C '$T/wsrepo' rev-parse HEAD)\" = \"\$(git -C '$T/dev' rev-parse HEAD)\" ] && [ \"\$(cat '$T/wsrepo/bin/evergenie')\" = 'built:$NEWV' ]"
check "17b 只重開工坊那個家一次、跑了檢查；部署 checkout 沒動" "[ \"\$(cat '$T/restarts.log')\" = 'restart $T/ws' ] && grep -qx 'doctor $T/ws' '$T/doctor.log' && grep -q 'OK doctor 沒有紅字，核心版本 $NEWV' <<<\"\$OUT\" && [ \"\$(binv)\" = 'built:$OLDV' ]"
check "17c 工坊的 repo 不寫 .deployed-version、bin.prev/ 是舊版而且沒有舊記號" "[ ! -e '$T/wsrepo/bin/.deployed-version' ] && [ \"\$(cat '$T/wsrepo/bin.prev/evergenie')\" = 'built:$OLDV' ] && [ ! -e '$T/wsrepo/bin.prev/.deployed-version' ]"
deploy workshop
check "17d 工坊已經跑著這一版：不重開" "[ $RC = 0 ] && grep -q '工坊已經跑著這一版' <<<\"\$OUT\" && [ \"\$(restarts)\" = 1 ]"
deploy --rollback workshop
check "17e --rollback workshop：換回舊版、重開工坊" "[ $RC = 0 ] && grep -q '工坊 已換回上一版' <<<\"\$OUT\" && [ \"\$(cat '$T/wsrepo/bin/evergenie')\" = 'built:$OLDV' ] && [ \"\$(grep -cx 'restart $T/ws' '$T/restarts.log')\" = 2 ]"
newenv; newcommit; newws
deploy --workshop office
check "17f --workshop office：office 先部署完成、再到工坊（跑著的已是新版就不重開）" "[ $RC = 0 ] && [ \"\$(grep -n '== office' <<<\"\$OUT\" | cut -d: -f1)\" -lt \"\$(grep -n '== 工坊' <<<\"\$OUT\" | cut -d: -f1)\" ] && grep -q '✓ office 部署完成' <<<\"\$OUT\" && [ \"\$(git -C '$T/wsrepo' rev-parse HEAD)\" = \"\$(git -C '$T/dev' rev-parse HEAD)\" ]"
newenv; newcommit; newws
echo wip >>"$T/wsrepo/feature.txt"
deploy workshop
check "17g 工坊的 repo 有沒提交的改動：結束碼 1、說不能動、沒重開、bin/ 沒動" "[ $RC = 1 ] && grep -q '不能動' <<<\"\$OUT\" && grep -q 'feature.txt' <<<\"\$OUT\" && [ \"\$(restarts)\" = 0 ] && [ \"\$(cat '$T/wsrepo/bin/evergenie')\" = 'built:$OLDV' ]"
newenv; newcommit; newws
git -C "$T/wsrepo" checkout -q -b someone-else
deploy workshop
check "17h 工坊的 repo 在別的分支：結束碼 1、說分支名、沒重開" "[ $RC = 1 ] && grep -q 'someone-else' <<<\"\$OUT\" && [ \"\$(restarts)\" = 0 ]"
newenv; newcommit; newws; echo doctor >"$T/mode"; echo "$NEWV" >"$T/bad-version"
deploy --workshop office
check "17i office 沒過：說工坊沒有動、沒有工坊的段落" "[ $RC = 1 ] && grep -q 'office 沒過，工坊沒有動' <<<\"\$OUT\" && ! grep -q '== 工坊' <<<\"\$OUT\""
deploy --workshop mini
check "17j --workshop 配 mini 是用法錯誤（結束碼 2）" "[ $RC = 2 ]"
deploy --workshop --rollback office
check "17k --workshop 配 --rollback 是用法錯誤（結束碼 2）" "[ $RC = 2 ]"
deploy --dry-run --workshop all
check "17l dry-run all --workshop：mini、office、工坊的順序" "[ $RC = 0 ] && [ \"\$(grep -n '== mini（' <<<\"\$OUT\" | cut -d: -f1)\" -lt \"\$(grep -n '== office' <<<\"\$OUT\" | cut -d: -f1)\" ] && [ \"\$(grep -n '== office' <<<\"\$OUT\" | cut -d: -f1)\" -lt \"\$(grep -n '== 工坊' <<<\"\$OUT\" | cut -d: -f1)\" ]"
newenv; newcommit
OUT="$(cd "$ROOT" && env -u EVERGENIE_DEPLOY_WORKSHOP_REPO -u EVERGENIE_DEPLOY_WORKSHOP_HOME PATH="$T/fakebin:$PATH" EVERGENIE_FAKE="$T" EVERGENIE_DEPLOY_REPO="$T/repo" EVERGENIE_DEPLOY_SHELL="bash -c" EVERGENIE_DEPLOY_SELF_UPDATE=0 scripts/deploy.sh workshop 2>&1)"; RC=$?
check "17m 測試只給 EVERGENIE_DEPLOY_REPO、沒給工坊的位置：拒做（結束碼 2），不碰真的主資料夾" "[ $RC = 2 ] && grep -q 'EVERGENIE_DEPLOY_WORKSHOP_REPO' <<<\"\$OUT\" && [ \"\$(restarts)\" = 0 ] && [ ! -s '$T/make.log' ]"

# 19. mini 開發家（DEV-157）：mini 正式家部署成功後接著換成同一個 commit；它的 repo 是 mini 上另一個 clone、家是 devhome。
#     沒有 newdev 的情境（上面每一段）就是「這台沒有開發家」：跳過、不算失敗。
devv() { cat "$T/devrepo/bin/evergenie"; }
devrestarts() { grep -cx "restart $T/devhome" "$T/restarts.log"; }
says() { wc -l <"$T/say.log" | tr -d ' '; }
newenv; newcommit; newdev
deploy mini
check "19a 正常：結束碼 0，mini 先完成、再換 mini 開發家，都經 ssh ${MH}" "[ $RC = 0 ] && grep -q '✓ mini 部署完成' <<<\"\$OUT\" && grep -q '✓ mini 開發家 部署完成' <<<\"\$OUT\" && [ \"\$(grep -n '== mini（' <<<\"\$OUT\" | cut -d: -f1)\" -lt \"\$(grep -n '== mini 開發家（' <<<\"\$OUT\" | cut -d: -f1)\" ] && ! grep '^ssh ' '$T/ssh.log' | grep -v -q '^ssh ${MH} '"
check "19b 開發家的 repo 跟正式家同一個 commit、bin/ 是新版、bin.prev/ 是舊版、不寫 .deployed-version" "[ \"\$(git -C '$T/devrepo' rev-parse HEAD)\" = \"\$(git -C '$T/repo' rev-parse HEAD)\" ] && [ \"\$(devv)\" = 'built:$NEWV' ] && [ \"\$(cat '$T/devrepo/bin.prev/evergenie')\" = 'built:$OLDV' ] && [ ! -e '$T/devrepo/bin/.deployed-version' ]"
check "19c 開發家的服務重開一次（帶自己的 EVERGENIE_HOME）、跑著新版、跑了檢查；正式家也只重開一次" "[ \"\$(devrestarts)\" = 1 ] && [ \"\$(restarts)\" = 2 ] && [ \"\$(cat '$T/running-dev')\" = '$NEWV' ] && grep -qx 'doctor $T/devhome' '$T/doctor.log'"
check "19d 換版前後各傳一句話給開發家的 art：開頭報部署工具、寫哪一版換哪一版、成功" "[ \"\$(says)\" = 2 ] && [ \"\$(grep -c '^$T/devhome art 部署工具：' '$T/say.log')\" = 2 ] && sed -n 1p '$T/say.log' | grep -q '要從 $OLDV 換成 $NEWV' && sed -n 2p '$T/say.log' | grep -q '已從 $OLDV 換成 ${NEWV}，檢查都過了'"
deploy mini
check "19e 再部署一次：正式家已是最新、開發家已經跑著這一版，不重開、不再傳話" "[ $RC = 0 ] && grep -q 'mini 開發家已經跑著這一版' <<<\"\$OUT\" && [ \"\$(devrestarts)\" = 1 ] && [ \"\$(says)\" = 2 ]"
deploy --rollback mini-dev
check "19f --rollback mini-dev：只換回開發家（bin/ 舊版、重開它），正式家沒動" "[ $RC = 0 ] && grep -q 'mini 開發家 已換回上一版' <<<\"\$OUT\" && [ \"\$(devv)\" = 'built:$OLDV' ] && [ \"\$(devrestarts)\" = 2 ] && [ \"\$(binv)\" = 'built:$NEWV' ]"

# 同一個 commit：正式家部署之後 origin 又多了一個 commit，開發家換成正式家那一個，不是更新的那個
newenv; newcommit; newdev; echo push-after-mini >"$T/mode"
deploy mini
check "19g origin 在中途又多一個 commit：開發家換成正式家部署的那個 commit（不是 origin 最新的）" "[ $RC = 0 ] && [ -e '$T/pushed' ] && [ \"\$(git -C '$T/devrepo' rev-parse HEAD)\" = \"\$(git -C '$T/repo' rev-parse HEAD)\" ] && [ \"\$(git -C '$T/devrepo' rev-parse HEAD)\" != \"\$(git -C '$T/dev' rev-parse HEAD)\" ] && [ \"\$(devv)\" = 'built:$NEWV' ]"
newenv; newcommit; newdev; echo dev-ahead >"$T/mode"
deploy mini
check "19h 開發家的 repo 已經比這次部署的 commit 新（有人手動拉過）：不往回換、不重編、不重開、不算失敗" "[ $RC = 0 ] && grep -q '不往回換' <<<\"\$OUT\" && [ \"\$(git -C '$T/devrepo' rev-parse HEAD)\" = \"\$(git -C '$T/dev' rev-parse HEAD)\" ] && [ \"\$(devrestarts)\" = 0 ] && [ \"\$(devv)\" = 'built:$OLDV' ]"

# 不可以蓋掉：repo 有沒 commit 的改動（含沒追蹤的新檔）、不在 main、main 上有 origin 沒有的 commit
newenv; newcommit; newdev
DEVHEAD="$(git -C "$T/devrepo" rev-parse HEAD)"
echo wip >>"$T/devrepo/feature.txt"
deploy mini
check "19i 開發家的 repo 有沒 commit 的改動：結束碼 1、正式家照樣部署完成、白話說幾個檔與檔名" "[ $RC = 1 ] && grep -q '✓ mini 部署完成' <<<\"\$OUT\" && grep -q '有 1 個檔沒 commit' <<<\"\$OUT\" && grep -q 'feature.txt' <<<\"\$OUT\" && grep -q 'mini 正式家已經部署好了；mini 開發家這次沒換成' <<<\"\$OUT\""
check "19j 沒蓋掉：改動還在、commit 沒變、bin/ 沒動、沒留 bin.prev/、沒編、沒重開開發家" "[ \"\$(tail -1 '$T/devrepo/feature.txt')\" = wip ] && [ \"\$(git -C '$T/devrepo' rev-parse HEAD)\" = '$DEVHEAD' ] && [ \"\$(devv)\" = 'built:$OLDV' ] && [ ! -d '$T/devrepo/bin.prev' ] && ! grep -q 'devrepo' '$T/make.log' && [ \"\$(devrestarts)\" = 0 ]"
check "19k 傳一句話給 art 說沒換版與原因、該怎麼做" "[ \"\$(says)\" = 1 ] && grep -q '^$T/devhome art 部署工具：mini 開發家這次沒換版，照舊跑 ${OLDV}。原因：.*有 1 個檔沒 commit.*開分支、commit、把這份切回 main、告訴 admin' '$T/say.log'"
newenv; newcommit; newdev
echo wip >>"$T/devrepo/feature.txt"
deploy all
check "19l all：開發家沒換成不擋 office（office 段落照做），最後結束碼 1" "[ $RC = 1 ] && grep -q '== office' <<<\"\$OUT\" && grep -q '有 1 個檔沒 commit' <<<\"\$OUT\" && [ \"\$(tail -1 '$T/devrepo/feature.txt')\" = wip ] && [ \"\$(grep -n '== mini 開發家（' <<<\"\$OUT\" | cut -d: -f1)\" -lt \"\$(grep -n '== office' <<<\"\$OUT\" | cut -d: -f1)\" ]"
newenv; newcommit; newdev
echo new >"$T/devrepo/brand-new.txt"
deploy mini
check "19m 只有沒追蹤的新檔也算改動：結束碼 1、說檔名、新檔還在、沒重開" "[ $RC = 1 ] && grep -q 'brand-new.txt' <<<\"\$OUT\" && [ -f '$T/devrepo/brand-new.txt' ] && [ \"\$(devrestarts)\" = 0 ] && [ \"\$(devv)\" = 'built:$OLDV' ]"
newenv; newcommit; newdev
git -C "$T/devrepo" checkout -q -b sec-11-shot-comment
deploy mini
check "19n 開發家的 repo 不在 main：結束碼 1、說分支名、沒切分支、沒重開" "[ $RC = 1 ] && grep -q 'sec-11-shot-comment，不是 main' <<<\"\$OUT\" && [ \"\$(git -C '$T/devrepo' rev-parse --abbrev-ref HEAD)\" = sec-11-shot-comment ] && [ \"\$(devrestarts)\" = 0 ] && [ \"\$(says)\" = 1 ]"
newenv; newcommit; newdev
(cd "$T/devrepo" && echo mine >>feature.txt && git commit -q -am "direct commit on dev home")
DEVHEAD="$(git -C "$T/devrepo" rev-parse HEAD)"
deploy mini
check "19o 開發家的 main 上有 origin 沒有的 commit：結束碼 1、列出那個 commit、commit 沒變、沒動 bin/" "[ $RC = 1 ] && grep -q '有 origin 沒有的 commit' <<<\"\$OUT\" && grep -q 'direct commit on dev home' <<<\"\$OUT\" && [ \"\$(git -C '$T/devrepo' rev-parse HEAD)\" = '$DEVHEAD' ] && [ \"\$(devv)\" = 'built:$OLDV' ] && [ ! -d '$T/devrepo/bin.prev' ] && [ \"\$(devrestarts)\" = 0 ]"

# 健檢沒過：開發家自動退回舊版，正式家留在新版
newenv; newcommit; newdev; echo doctor-dev >"$T/mode"; echo "$NEWV" >"$T/bad-version"
deploy mini
check "19p 開發家檢查沒過：結束碼 1、印出它的紅字、自動換回；正式家留在新版" "[ $RC = 1 ] && grep -q '開發家的資料庫打不開' <<<\"\$OUT\" && grep -q '✗ mini 開發家 部署沒過' <<<\"\$OUT\" && grep -q '自動換回上一版' <<<\"\$OUT\" && grep -q '✓ mini 部署完成' <<<\"\$OUT\" && [ \"\$(binv)\" = 'built:$NEWV' ] && [ \"\$(cat '$T/running')\" = '$NEWV' ]"
check "19q 開發家 bin/ 換回舊版、壞的留 bin.failed/、重開兩次、跑著舊版" "[ \"\$(devv)\" = 'built:$OLDV' ] && [ \"\$(cat '$T/devrepo/bin.failed/evergenie')\" = 'built:$NEWV' ] && [ \"\$(devrestarts)\" = 2 ] && [ \"\$(cat '$T/running-dev')\" = '$OLDV' ]"
check "19r 第二句話說沒過、已自動退回哪一版" "[ \"\$(says)\" = 2 ] && sed -n 2p '$T/say.log' | grep -q '換成 $NEWV 沒過（部署後檢查沒過），已自動退回 $OLDV'"
newenv; newcommit; newdev; echo doctor-dev >"$T/mode"; echo "$NEWV" >"$T/bad-version"
deploy all
check "19s all：開發家退回舊版也不擋 office，最後結束碼 1" "[ $RC = 1 ] && grep -q '== office' <<<\"\$OUT\" && [ \"\$(devv)\" = 'built:$OLDV' ]"

# 開發家不在、沒在跑：跳過、說一聲、不算失敗
newenv; newcommit
deploy mini
check "19t 這台沒有開發家的 repo：結束碼 0、說跳過與原因" "[ $RC = 0 ] && grep -q 'mini 開發家這次跳過（不算失敗）：沒有它的 repo' <<<\"\$OUT\" && [ \"\$(says)\" = 0 ]"
newenv; newcommit; newdev; rm -f "$T/devhome/home.toml"
deploy mini
check "19u 開發家的資料夾不在：結束碼 0、說跳過、repo 沒動、沒重開" "[ $RC = 0 ] && grep -q '跳過（不算失敗）：沒有這個家' <<<\"\$OUT\" && [ \"\$(devv)\" = 'built:$OLDV' ] && [ \"\$(devrestarts)\" = 0 ] && [ \"\$(git -C '$T/devrepo' rev-parse HEAD)\" != \"\$(git -C '$T/dev' rev-parse HEAD)\" ]"
newenv; newcommit; newdev; touch "$T/dev-stopped"
deploy mini
check "19v 開發家的服務沒在跑：結束碼 0、說跳過、不把它開起來、repo 沒動" "[ $RC = 0 ] && grep -q '跳過（不算失敗）：服務沒在跑' <<<\"\$OUT\" && [ \"\$(devrestarts)\" = 0 ] && [ \"\$(devv)\" = 'built:$OLDV' ] && [ \"\$(says)\" = 0 ]"

# 傳話失敗（那個家沒有 art、或傳不到）不影響部署
newenv; newcommit; newdev; touch "$T/say-fail"
deploy mini
check "19w 傳不到 art：照樣換版成功、結束碼 0、說傳不到不算失敗" "[ $RC = 0 ] && grep -q '✓ mini 開發家 部署完成' <<<\"\$OUT\" && grep -q '傳不到 art，不算部署失敗' <<<\"\$OUT\" && [ \"\$(devv)\" = 'built:$NEWV' ] && [ \"\$(cat '$T/running-dev')\" = '$NEWV' ]"
newenv; newcommit; newdev; touch "$T/say-fail"; echo doctor-dev >"$T/mode"; echo "$NEWV" >"$T/bad-version"
deploy mini
check "19x 傳不到 art 而且檢查沒過：照樣自動退回舊版" "[ $RC = 1 ] && [ \"\$(devv)\" = 'built:$OLDV' ] && [ \"\$(cat '$T/running-dev')\" = '$OLDV' ]"

# 正式家沒過、要守的服務少了：不碰開發家
newenv; newcommit; newdev; echo doctor >"$T/mode"; echo "$NEWV" >"$T/bad-version"
deploy mini
check "19y mini 正式家沒過：開發家沒有動（沒有它的段落、沒重開、沒傳話）" "[ $RC = 1 ] && grep -q 'mini 沒過，mini 開發家沒有動' <<<\"\$OUT\" && ! grep -q '== mini 開發家' <<<\"\$OUT\" && [ \"\$(devrestarts)\" = 0 ] && [ \"\$(says)\" = 0 ] && [ \"\$(devv)\" = 'built:$OLDV' ]"
newenv; newcommit; newdev; echo guard-lose >"$T/mode"
deploy mini
check "19z mini 上要守的服務少了：結束碼 4、開發家沒有動" "[ $RC = 4 ] && ! grep -q '== mini 開發家' <<<\"\$OUT\" && [ \"\$(devrestarts)\" = 0 ] && [ \"\$(devv)\" = 'built:$OLDV' ]"

# 單獨的目標、dry-run、用法
newenv; newcommit; newdev
deploy mini-dev
check "19aa 只換開發家（mini-dev）：換到 origin/main、重開它、正式家沒動" "[ $RC = 0 ] && grep -q '✓ mini 開發家 部署完成' <<<\"\$OUT\" && ! grep -q '== mini（' <<<\"\$OUT\" && [ \"\$(git -C '$T/devrepo' rev-parse HEAD)\" = \"\$(git -C '$T/dev' rev-parse HEAD)\" ] && [ \"\$(devv)\" = 'built:$NEWV' ] && [ \"\$(binv)\" = 'built:$OLDV' ] && [ \"\$(cat '$T/restarts.log')\" = 'restart $T/devhome' ]"
newenv; newcommit; newdev
DEVHEAD="$(git -C "$T/devrepo" rev-parse HEAD)"
deploy --dry-run mini
check "19ab dry-run mini：印出開發家的計畫（確認在不在、傳話、快轉、編譯、重開），什麼都不執行" "[ $RC = 0 ] && grep -q '== mini 開發家（${MH}）部署 ==' <<<\"\$OUT\" && grep -q '任何一個不是就跳過' <<<\"\$OUT\" && grep -q 'evergenie say' <<<\"\$OUT\" && [ ! -s '$T/ssh.log' ] && [ ! -s '$T/make.log' ] && [ \"\$(restarts)\" = 0 ] && [ \"\$(says)\" = 0 ] && [ \"\$(git -C '$T/devrepo' rev-parse HEAD)\" = '$DEVHEAD' ]"
newenv; newcommit; newdev
deploy mini
deploy --rollback mini
check "19ac --rollback mini 只換回正式家（成功換回），不碰開發家：它的 bin/ 還是新版、沒有再重開、沒有它的段落" "[ $RC = 0 ] && grep -q 'mini 已換回上一版' <<<\"\$OUT\" && [ \"\$(binv)\" = 'built:$OLDV' ] && ! grep -q '== mini 開發家' <<<\"\$OUT\" && [ \"\$(devv)\" = 'built:$NEWV' ] && [ \"\$(devrestarts)\" = 1 ] && [ \"\$(says)\" = 2 ]"
newenv; newcommit
OUT="$(cd "$ROOT" && env -u EVERGENIE_DEPLOY_MINI_DEV_REPO -u EVERGENIE_DEPLOY_MINI_DEV_HOME PATH="$T/fakebin:$PATH" EVERGENIE_FAKE="$T" EVERGENIE_DEPLOY_REPO="$T/repo" EVERGENIE_DEPLOY_SHELL="bash -c" EVERGENIE_DEPLOY_SELF_UPDATE=0 scripts/deploy.sh mini 2>&1)"; RC=$?
check "19ad 測試只給 EVERGENIE_DEPLOY_REPO、沒給開發家的位置：整個拒做（結束碼 2），連正式家都還沒動" "[ $RC = 2 ] && grep -q 'EVERGENIE_DEPLOY_MINI_DEV_REPO' <<<\"\$OUT\" && [ \"\$(restarts)\" = 0 ] && [ ! -s '$T/make.log' ] && [ ! -s '$T/ssh.log' ]"
newenv; newcommit; newdev
OUT="$(cd "$ROOT" && HOME="$T/home" PATH="$T/fakebin:$PATH" EVERGENIE_FAKE="$T" EVERGENIE_DEPLOY_REPO="$T/repo" EVERGENIE_DEPLOY_SHELL="bash -c" EVERGENIE_DEPLOY_TRIES=2 EVERGENIE_DEPLOY_HOMES_FILE="$T/homes" EVERGENIE_DEPLOY_SELF_UPDATE=0 EVERGENIE_DEPLOY_GUARD_TRIES=2 EVERGENIE_DEPLOY_MINI_DEV_REPO="$T/devrepo" EVERGENIE_DEPLOY_MINI_DEV_HOME="$T/devhome" EVERGENIE_DEPLOY_MINI_DEV_NOTIFY= scripts/deploy.sh mini 2>&1)"; RC=$?
check "19ae EVERGENIE_DEPLOY_MINI_DEV_NOTIFY 設成空字串：照樣換版、不傳話" "[ $RC = 0 ] && grep -q '✓ mini 開發家 部署完成' <<<\"\$OUT\" && [ \"\$(says)\" = 0 ]"

# 預設位置是字面的 $HOME/…，要到那台才展開（mini 的家目錄不一定跟這台一樣）：位置給字面的 '$HOME/…'、那台的 HOME 指到暫存資料夾
newenv; newcommit; newdev
mv "$T/devrepo" "$T/home/devrepo"; mv "$T/devhome" "$T/home/devhome"
# shellcheck disable=SC2016
OUT="$(cd "$ROOT" && HOME="$T/home" PATH="$T/fakebin:$PATH" EVERGENIE_FAKE="$T" EVERGENIE_DEPLOY_REPO="$T/repo" EVERGENIE_DEPLOY_SHELL="bash -c" EVERGENIE_DEPLOY_TRIES=2 EVERGENIE_DEPLOY_HOMES_FILE="$T/homes" EVERGENIE_DEPLOY_SELF_UPDATE=0 EVERGENIE_DEPLOY_GUARD_TRIES=2 EVERGENIE_DEPLOY_MINI_DEV_REPO='$HOME/devrepo' EVERGENIE_DEPLOY_MINI_DEV_HOME='$HOME/devhome' scripts/deploy.sh mini 2>&1)"; RC=$?
check "19af 位置寫成字面的 \$HOME/…：到那台才展開，開發家真的換了、重開的是展開後的那個家" "[ $RC = 0 ] && grep -q '✓ mini 開發家 部署完成' <<<\"\$OUT\" && [ \"\$(cat '$T/home/devrepo/bin/evergenie')\" = 'built:$NEWV' ] && [ \"\$(grep -cx 'restart $T/home/devhome' '$T/restarts.log')\" = 1 ] && grep -qx 'doctor $T/home/devhome' '$T/doctor.log' && grep -q '^$T/home/devhome art 部署工具：' '$T/say.log'"

# 確認開發家在不在：那台多印了別的字也認得出來；服務管的是另一個家就不換（不然重開的會是別的家）
newenv; newcommit; newdev
printf '#!/usr/bin/env bash\necho "Last login: 假的登入訊息" >&2\nexec bash "$@"\n' >"$T/fakebin/noisy-shell"; chmod +x "$T/fakebin/noisy-shell"
OUT="$(cd "$ROOT" && HOME="$T/home" PATH="$T/fakebin:$PATH" EVERGENIE_FAKE="$T" EVERGENIE_DEPLOY_REPO="$T/repo" EVERGENIE_DEPLOY_SHELL="noisy-shell -c" EVERGENIE_DEPLOY_TRIES=2 EVERGENIE_DEPLOY_HOMES_FILE="$T/homes" EVERGENIE_DEPLOY_SELF_UPDATE=0 EVERGENIE_DEPLOY_GUARD_TRIES=2 EVERGENIE_DEPLOY_MINI_GUARD= EVERGENIE_DEPLOY_MINI_DEV_REPO="$T/devrepo" EVERGENIE_DEPLOY_MINI_DEV_HOME="$T/devhome" scripts/deploy.sh mini-dev 2>&1)"; RC=$?
check "19ag 那台的殼層每次多印一行別的字：不會被當成「開發家不在」而跳過，照樣換版" "[ $RC = 0 ] && grep -q '✓ mini 開發家 部署完成' <<<\"\$OUT\" && ! grep -q '這次跳過' <<<\"\$OUT\" && [ \"\$(devv)\" = 'built:$NEWV' ]"
newenv; newcommit; newdev; touch "$T/dev-wrong-service"
deploy mini
check "19ah 開發家的服務管的是另一個家：結束碼 1、說原因、不編不重開（重開會開到別的家）；正式家照樣完成" "[ $RC = 1 ] && grep -q '管的是另一個家' <<<\"\$OUT\" && grep -q 'service_label' <<<\"\$OUT\" && grep -q '✓ mini 部署完成' <<<\"\$OUT\" && [ \"\$(devrestarts)\" = 0 ] && [ \"\$(devv)\" = 'built:$OLDV' ] && [ ! -d '$T/devrepo/bin.prev' ]"

# 要守的服務在換開發家的時候才少：結束碼 4，說法不能是「開發家沒有動」
newenv; newcommit; newdev; echo guard-lose-dev >"$T/mode"
deploy mini
check "19ai 要守的服務在換開發家時少了：結束碼 4、說是換開發家的時候、不說開發家沒有動" "[ $RC = 4 ] && grep -q '在換 mini 開發家的時候少了或停了' <<<\"\$OUT\" && ! grep -q 'mini 開發家沒有動' <<<\"\$OUT\" && grep -q '✓ mini 部署完成' <<<\"\$OUT\""

# 18. 先換成 origin/main 的部署工具（D-53）：手上這份跟 origin/main 不一樣就換成那一份跑（同樣的參數）；dry-run 不換
newenv
(
  cd "$T/dev" || exit 1
  mkdir -p scripts && cp "$ROOT/scripts/deploy.sh" scripts/deploy.sh && git add -A && git commit -q -m tool && git push -q origin HEAD:main
)
git clone -q "$T/origin.git" "$T/tool" 2>/dev/null
(
  cd "$T/dev" || exit 1
  printf '#!/usr/bin/env bash\necho "ORIGIN-TOOL $*"\n' >scripts/deploy.sh && git commit -q -am "newer tool" && git push -q origin HEAD:main
)
# 換不成功時也只能碰暫存的 repo（假 ssh、EVERGENIE_DEPLOY_REPO 指到暫存）
tool() { OUT="$(env -u EVERGENIE_DEPLOY_SELF_UPDATE EVERGENIE_FAKE="$T" PATH="$T/fakebin:$PATH" EVERGENIE_DEPLOY_REPO="$T/repo" EVERGENIE_DEPLOY_SHELL="bash -c" EVERGENIE_DEPLOY_TRIES=1 EVERGENIE_DEPLOY_HOMES_FILE="$T/homes" EVERGENIE_DEPLOY_GUARD_TRIES=1 EVERGENIE_DEPLOY_WORKSHOP_REPO="$T/wsrepo" EVERGENIE_DEPLOY_WORKSHOP_HOME="$T/ws" EVERGENIE_DEPLOY_MINI_DEV_REPO="$T/devrepo" EVERGENIE_DEPLOY_MINI_DEV_HOME="$T/devhome" bash "$T/tool/scripts/deploy.sh" "$@" 2>&1)"; RC=$?; }
tool --force mini
check "18a 手上的舊：fetch 後換成 origin/main 那份、參數照傳" "[ $RC = 0 ] && grep -q '改用 origin/main 那一份' <<<\"\$OUT\" && grep -qx 'ORIGIN-TOOL --force mini' <<<\"\$OUT\""
check "18b 只動 remote-tracking：手上的工作資料夾沒變" "[ \"\$(git -C '$T/tool' status --porcelain)\" = '' ] && grep -q 'EVERGENIE_DEPLOY_SELF_UPDATE' '$T/tool/scripts/deploy.sh'"
tool --dry-run mini
check "18c dry-run 不換，只說會換" "[ $RC = 0 ] && grep -q '真的部署時會先抓 origin/main' <<<\"\$OUT\" && ! grep -q 'ORIGIN-TOOL' <<<\"\$OUT\" && [ ! -s '$T/ssh.log' ]"
tool --rollback mini
check "18d --rollback 不換工具（壞的可能正是新推的 deploy.sh）" "! grep -q 'ORIGIN-TOOL' <<<\"\$OUT\" && ! grep -q '改用 origin/main' <<<\"\$OUT\" && grep -q 'mini' '$T/ssh.log'"

# 20. dry-run 說清楚會部署什麼（ADM-35）：本機看到的 origin/main 多久前更新、office（本機）上次部署的版本到 origin/main 差哪些 commit；
#     只讀本機 git 與檔案，mini 說看不到，開發家不比；都不 ssh
newenv
(cd "$T/dev" && mkdir -p scripts && cp "$ROOT/scripts/deploy.sh" scripts/deploy.sh && git add -A && git commit -q -m tool && git push -q origin HEAD:main)
git clone -q "$T/origin.git" "$T/tool" 2>/dev/null
rdry() { OUT="$(EVERGENIE_DEPLOY_SELF_UPDATE=0 EVERGENIE_FAKE="$T" PATH="$T/fakebin:$PATH" EVERGENIE_DEPLOY_REPO="$T/repo" EVERGENIE_DEPLOY_SHELL="bash -c" EVERGENIE_DEPLOY_HOMES_FILE="$T/homes" EVERGENIE_DEPLOY_WORKSHOP_REPO="$T/wsrepo" EVERGENIE_DEPLOY_WORKSHOP_HOME="$T/ws" EVERGENIE_DEPLOY_MINI_DEV_REPO="$T/devrepo" EVERGENIE_DEPLOY_MINI_DEV_HOME="$T/devhome" bash "$T/tool/scripts/deploy.sh" --dry-run "$@" 2>&1)"; RC=$?; }
rdry office
check "20g 剛 clone、origin/main 沒有 reflog：不說多久前更新，其他照說" "[ $RC = 0 ] && grep -q '會部署：origin/main（本機看到的是 [0-9a-f]* tool；那台會自己 fetch' <<<\"\$OUT\" && grep -q '上次部署到 office 的版本：${OLDV}；origin/main 比它多 1 個 commit' <<<\"\$OUT\""
# clone 不留 origin/main 的 reflog：再出一版、fetch 一次，才有「多久前更新過」可說
newcommit
git -C "$T/tool" fetch -q origin
rdry all
check "20a office：說上次部署的版本、origin/main 比它多 2 個 commit，並列出那兩個；說本機的 origin/main 多久前更新" "[ $RC = 0 ] && grep -q '分鐘前更新過；那台會自己 fetch' <<<\"\$OUT\" && grep -q '上次部署到 office 的版本：${OLDV}；origin/main 比它多 2 個 commit' <<<\"\$OUT\" && grep -q '^  [0-9a-f]* tool$' <<<\"\$OUT\" && grep -q '^  [0-9a-f]* change ' <<<\"\$OUT\""
check "20b mini：說 dry-run 看不到它上次的版本；整段沒有 ssh、沒有 launchctl、repo 沒動" "grep -q '上次部署到 mini 的版本：dry-run 不連 mini' <<<\"\$OUT\" && [ ! -s '$T/ssh.log' ] && [ ! -s '$T/launchctl.log' ] && [ \"\$(cat '$T/repo/bin/.deployed-version')\" = '$OLDV' ]"
echo "0.0.1-5-g$(git -C "$T/tool" rev-parse --short origin/main)" >"$T/repo/bin/.deployed-version"
rdry office
check "20c 上次部署的就是 origin/main（X.Y.Z-N-g<短碼> 的寫法）：說一樣、不列 commit" "[ $RC = 0 ] && grep -q '跟本機看到的 origin/main 一樣' <<<\"\$OUT\" && ! grep -q '多 .* 個 commit' <<<\"\$OUT\""
echo "9.9.9" >"$T/repo/bin/.deployed-version"
rdry office
check "20d 上次部署的版本本機 git 找不到：照實說算不出來，結束碼 0" "[ $RC = 0 ] && grep -q '9.9.9（本機 git 找不到這個 commit' <<<\"\$OUT\""
(cd "$T/tool" && git reset -q --hard origin/main && git -c user.email=t@example.com -c user.name=t commit -q --allow-empty -m local-only)
git -C "$T/tool" rev-parse --short HEAD >"$T/repo/bin/.deployed-version"
rdry office
check "20e 上次部署的版本比本機看到的 origin/main 還新：警告本機可能沒 fetch" "[ $RC = 0 ] && grep -q '⚠ 上次部署到 office 的版本 .* 比本機看到的 origin/main 還新' <<<\"\$OUT\""
rm -f "$T/repo/bin/.deployed-version"
rdry office
check "20h 讀不到 bin/.deployed-version：說不知道，結束碼 0" "[ $RC = 0 ] && grep -q '上次部署到 office 的版本：不知道' <<<\"\$OUT\""
echo "$OLDV" >"$T/repo/bin/.deployed-version"
OUT="$(HOME="$T" EVERGENIE_DEPLOY_SELF_UPDATE=0 EVERGENIE_FAKE="$T" PATH="$T/fakebin:$PATH" EVERGENIE_DEPLOY_REPO='~/repo' EVERGENIE_DEPLOY_SHELL="bash -c" bash "$T/tool/scripts/deploy.sh" --dry-run office 2>&1)"; RC=$?
check "20f repo 位置用 ~/ 開頭也讀得到上次部署的版本" "[ $RC = 0 ] && grep -q '上次部署到 office 的版本：${OLDV}；' <<<\"\$OUT\""
rdry workshop
check "20i 開發家（不記上次部署的版本）：說會部署哪個 commit，不比差幾個" "[ $RC = 0 ] && grep -q '會部署：origin/main（本機看到的是' <<<\"\$OUT\" && ! grep -q '上次部署到' <<<\"\$OUT\""
rdry --rollback office
check "20j --rollback 的 dry-run 不說會部署什麼" "[ $RC = 0 ] && ! grep -q '會部署：' <<<\"\$OUT\" && ! grep -q '上次部署到' <<<\"\$OUT\""

# 21. 部署設定檔（DEV-158）：自己環境的值（mini 的 ssh 名稱、要守的服務）在 repo 外面的設定檔；沒有設定檔就沒有 mini 的預設、預設不守；
#     預設位置是 ~/.evergenie-release/deploy.env；環境變數（含空字串）比設定檔優先；設定檔壞了就停下來
# d21 [-u 變數]… [變數=值]… <指令…>：deploy() 那套假環境，但設定檔由每個情境自己給
d21() {
  local opts=()
  while [ "${1:-}" = -u ]; do opts+=(-u "$2"); shift 2; done
  OUT="$(cd "$ROOT" && env ${opts[@]+"${opts[@]}"} HOME="$T/home" PATH="$T/fakebin:$PATH" EVERGENIE_FAKE="$T" EVERGENIE_DEPLOY_REPO="$T/repo" EVERGENIE_DEPLOY_SHELL="bash -c" EVERGENIE_DEPLOY_TRIES=2 \
    EVERGENIE_DEPLOY_HOMES_FILE="$T/homes" EVERGENIE_DEPLOY_SELF_UPDATE=0 EVERGENIE_DEPLOY_GUARD_TRIES=2 EVERGENIE_DEPLOY_MINI_DEV_REPO="$T/devrepo" EVERGENIE_DEPLOY_MINI_DEV_HOME="$T/devhome" "$@" 2>&1)"
  RC=$?
}
newenv; newcommit
d21 EVERGENIE_DEPLOY_ENV= scripts/deploy.sh mini
check "21a 沒有設定檔、也沒設 EVERGENIE_DEPLOY_MINI_HOST：結束碼 2、說缺哪一個、什麼都沒動" "[ $RC = 2 ] && grep -q '不知道 mini 要 ssh 到哪台' <<<\"\$OUT\" && grep -q 'EVERGENIE_DEPLOY_MINI_HOST=' <<<\"\$OUT\" && [ ! -s '$T/ssh.log' ] && [ ! -s '$T/make.log' ] && [ \"\$(restarts)\" = 0 ] && [ \"\$(binv)\" = 'built:$OLDV' ]"
d21 EVERGENIE_DEPLOY_ENV= scripts/deploy.sh --dry-run all
check "21b 沒有 mini 的 ssh 名稱：dry-run all 照樣印計畫（mini 在 office 前）、先說真的部署會停在這裡、結束碼 0、不 ssh" "[ $RC = 0 ] && grep -q '不知道 mini 要 ssh 到哪台' <<<\"\$OUT\" && grep -q '真的部署會停在這裡' <<<\"\$OUT\" && grep -q '== mini（<mini 的 ssh 名稱>）' <<<\"\$OUT\" && [ \"\$(grep -n '== mini' <<<\"\$OUT\" | head -1 | cut -d: -f1)\" -lt \"\$(grep -n '== office' <<<\"\$OUT\" | head -1 | cut -d: -f1)\" ] && [ ! -s '$T/ssh.log' ]"
d21 EVERGENIE_DEPLOY_ENV= scripts/deploy.sh office
check "21c 沒有設定檔：office（本機）照樣部署完成、不守任何服務（不叫 launchctl）、不印 mini 的不守提醒" "[ $RC = 0 ] && grep -q '✓ office 部署完成' <<<\"\$OUT\" && [ ! -s '$T/launchctl.log' ] && ! grep -q '^提醒：沒有設定 mini' <<<\"\$OUT\""
newenv; newcommit
d21 EVERGENIE_DEPLOY_ENV= EVERGENIE_DEPLOY_MINI_HOST="$MH" scripts/deploy.sh mini
check "21d 沒有設定檔、mini 的 ssh 名稱用環境變數給：照樣部署完成、經 ssh 那個名稱、預設不守（不叫 launchctl）" "[ $RC = 0 ] && grep -q '✓ mini 部署完成' <<<\"\$OUT\" && grep -q '^ssh ${MH} ' '$T/ssh.log' && [ ! -s '$T/launchctl.log' ] && ! grep -q '要守的服務' <<<\"\$OUT\""
check "21d2 真的部署 mini 而沒設要守的服務：正式家與開發家兩段各印一行提醒說這次不守（結束碼照舊 0）" "[ $RC = 0 ] && [ \"\$(grep -c '^提醒：沒有設定 mini 上要守住哪些服務（EVERGENIE_DEPLOY_MINI_GUARD 沒設或是空字串），這次不守$' <<<\"\$OUT\")\" = 2 ] && grep -A3 '^== mini（${MH}）部署 ==' <<<\"\$OUT\" | grep -q '^提醒：沒有設定 mini'"
newenv; newcommit
mkdir -p "$T/home/.evergenie-release" && cp "$EVERGENIE_DEPLOY_ENV" "$T/home/.evergenie-release/deploy.env"
d21 -u EVERGENIE_DEPLOY_ENV scripts/deploy.sh mini
check "21e 沒設 EVERGENIE_DEPLOY_ENV：讀 ~/.evergenie-release/deploy.env（ssh 名稱、要守的服務都照它）" "[ $RC = 0 ] && grep -q '✓ mini 部署完成' <<<\"\$OUT\" && grep -q '要守的服務（部署前）：3 個（2 個在跑）' <<<\"\$OUT\" && grep -q 'OK 要守的服務都還在' <<<\"\$OUT\" && grep -q '^ssh ${MH} ' '$T/ssh.log'"
newenv; newcommit
d21 EVERGENIE_DEPLOY_MINI_HOST=other-mini EVERGENIE_DEPLOY_MINI_GUARD= scripts/deploy.sh --dry-run mini
check "21f 環境變數比設定檔優先（設成空字串也算）：dry-run 說讀了哪個設定檔、ssh 名稱用環境變數的、守的樣式空＝不守（dry-run 不印不守的提醒）" "[ $RC = 0 ] && grep -q '（部署設定：${EVERGENIE_DEPLOY_ENV}）' <<<\"\$OUT\" && grep -q '== mini（other-mini）部署 ==' <<<\"\$OUT\" && ! grep -q '要守的服務' <<<\"\$OUT\" && ! grep -q '^提醒：沒有設定 mini' <<<\"\$OUT\""
printf 'EVERGENIE_DEPLOY_MINI_HOST=%s\nif then\n' "$MH" >"$T/broken.env"
d21 EVERGENIE_DEPLOY_ENV="$T/broken.env" scripts/deploy.sh mini
check "21g 設定檔有語法錯誤：結束碼 2、說是設定檔的問題、什麼都沒動" "[ $RC = 2 ] && grep -q '部署設定檔 .*broken.env 沒有跑成功' <<<\"\$OUT\" && [ ! -s '$T/ssh.log' ] && [ ! -s '$T/make.log' ] && [ \"\$(restarts)\" = 0 ]"
d21 EVERGENIE_DEPLOY_ENV="$T/broken.env" scripts/deploy.sh --help
check "21h 設定檔壞了也照樣印用法（參數先檢查、還沒讀設定檔）：結束碼 2、印用法、不說設定檔的事" "[ $RC = 2 ] && grep -q '用法：' <<<\"\$OUT\" && ! grep -q '部署設定檔' <<<\"\$OUT\""
d21 EVERGENIE_DEPLOY_ENV="$T/no-such.env" EVERGENIE_DEPLOY_MINI_HOST="$MH" scripts/deploy.sh mini
check "21i 明講的設定檔位置找不到檔：結束碼 2、說找不到哪個檔、什麼都沒動（不默默當成沒有設定檔、不守就部署）" "[ $RC = 2 ] && grep -q '找不到部署設定檔 .*no-such.env' <<<\"\$OUT\" && [ ! -s '$T/ssh.log' ] && [ ! -s '$T/make.log' ] && [ \"\$(restarts)\" = 0 ]"
cat >"$T/odd.env" <<'EOF'
EVERGENIE_DEPLOY_MINI_HOST="${SOME_UNSET_VAR}test-mini"
TARGET=office DRY=0 ROLLBACK=1
echo 設定檔印的字
EOF
d21 EVERGENIE_DEPLOY_ENV="$T/odd.env" scripts/deploy.sh --dry-run mini
check "21j 設定檔用到沒設的變數、想改這支自己的變數、印字：照樣讀到 ssh 名稱、還是 dry-run mini、不 ssh、設定檔印的字不混進來" "[ $RC = 0 ] && grep -q '（dry-run：只印會做什麼' <<<\"\$OUT\" && grep -q '== mini（${MH}）部署 ==' <<<\"\$OUT\" && ! grep -q '== office' <<<\"\$OUT\" && ! grep -q '設定檔印的字' <<<\"\$OUT\" && [ ! -s '$T/ssh.log' ]"

echo
echo "通過 ${pass}，失敗 $fail"
[ "$fail" = 0 ]
