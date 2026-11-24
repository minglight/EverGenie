#!/usr/bin/env bash
# scripts/release.sh 與 scripts/version.sh 的驗收（P8 第二輪 S1、REQ-277、REQ-278）。結束碼 0＝全過。
# 全程在暫存資料夾：bare origin＋發行用 clone＋開發用 clone，真的 git（離線）；測試步驟用 true／false 代替 make，不編譯、不跑真的測試。
# 沒有 --push 時 origin 不會多出任何 tag；不碰 ~/.evergenie-release、不推任何東西。
set -uo pipefail
cd "$(dirname "$0")/.."
ROOT="$PWD"
BASE="$(mktemp -d "${TMPDIR:-/tmp}/hg-release-test.XXXXXX")"
trap 'rm -rf "$BASE"' EXIT
pass=0; fail=0
ok() { echo "PASS  $1"; pass=$((pass + 1)); }
bad() { echo "FAIL  $1"; fail=$((fail + 1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1"; echo "      --- 輸出 ---"; printf '%s\n' "${OUT:-}" | sed 's/^/      /'; fi; }

# 每個情境一個全新的環境：origin（bare）、dev（出新 commit 用）、repo（發行用，tag 打在這裡）、state。
newenv() {
  T="$(mktemp -d "$BASE/env.XXXXXX")"
  git init -q --bare -b main "$T/origin.git"
  git clone -q "$T/origin.git" "$T/dev" 2>/dev/null
  (
    cd "$T/dev" || exit 1
    git config user.email t@example.com; git config user.name t
    mkdir -p scripts && cp "$ROOT/scripts/version.sh" scripts/version.sh
    echo one >f.txt
    git add -A && git commit -q -m "chore: first" && git push -q origin HEAD:main
  )
  git clone -q "$T/origin.git" "$T/repo" 2>/dev/null
  git -C "$T/repo" config user.email t@example.com; git -C "$T/repo" config user.name t
  STATE="$T/state"
}
commit() { # commit "<訊息>"：開發端出一個新 commit 並推到 origin
  (cd "$T/dev" && echo "$RANDOM" >>f.txt && git add -A && git commit -q -m "$1" && git push -q origin HEAD:main)
}
rel() { # rel <steps> <參數…>：跑 release.sh
  local steps="$1"; shift
  OUT="$(cd "$ROOT" && EVERGENIE_RELEASE_REPO="$T/repo" EVERGENIE_RELEASE_STATE="$STATE" EVERGENIE_RELEASE_STEPS="$steps" PATH="$BASE/fakebin:$PATH" scripts/release.sh "$@" 2>&1)"
  RC=$?
}
today() { date +%Y-%m-%d; }
origin_tags() { git -C "$T/origin.git" tag --list | tr '\n' ' '; }
repo_tags() { git -C "$T/repo" tag --list | tr '\n' ' '; }
jget() { python3 -c "import json,sys; v=json.load(open(sys.argv[1]))
for k in sys.argv[2].split('.'): v = v[int(k)] if isinstance(v, list) else v[k]
print(v)" "$1" "$2"; }
# 假的 evergenie：只記下被叫的參數。PATH 上真的那支會把「每晚測試沒過」發進這台的正式家
# （2026-11-02 曾經一天被塞了 15 則「每晚測試沒過：<假 commit>（false）」）。
mkdir -p "$BASE/fakebin"
NOTIFY_LOG="$BASE/notify.log"
# notify --help 照新版印 --lane；有 $BASE/fake-old 時裝成還不認得 --lane 的舊版（D-47）。
FAKE_OLD="$BASE/fake-old"
cat >"$BASE/fakebin/evergenie" <<FAKE
#!/usr/bin/env bash
case " \$* " in
*" --help "*) [ -e "$FAKE_OLD" ] || echo "  --lane      文字"; exit 0 ;;
esac
printf '%s\n' "\$*" >>"$NOTIFY_LOG"
FAKE
chmod +x "$BASE/fakebin/evergenie"

# ---- version.sh ----------------------------------------------------------------------------------------
newenv
OUT="$("$T/dev/scripts/version.sh")"
check "version.sh：沒有任何 v tag → commit 短碼" '[ "$OUT" = "$(git -C "$T/dev" rev-parse --short HEAD)" ]'
git -C "$T/dev" tag -a v0.2.0 -m x
OUT="$("$T/dev/scripts/version.sh")"
check "version.sh：在 v0.2.0 上 → 0.2.0（沒有開頭的 v）" '[ "$OUT" = "0.2.0" ]'
git -C "$T/dev" tag -a safe/2099-01-01 -m x
OUT="$("$T/dev/scripts/version.sh")"
check "version.sh：safe tag 不算版號" '[ "$OUT" = "0.2.0" ]'
commit "fix: after tag"
OUT="$("$T/dev/scripts/version.sh")"
check "version.sh：tag 之後多一個 commit → 0.2.0-1-g<短碼>" '[ "$OUT" = "0.2.0-1-g$(git -C "$T/dev" rev-parse --short HEAD)" ]'
echo dirty >>"$T/dev/f.txt"
OUT="$("$T/dev/scripts/version.sh")"
check "version.sh：有沒提交的改動 → 結尾 -dirty" '[[ "$OUT" == 0.2.0-1-g*-dirty ]]'

# ---- nightly -------------------------------------------------------------------------------------------
newenv; commit "feat: a"
rel "true;true" nightly
head="$(git -C "$T/origin.git" rev-parse main)"
check "nightly 全過：結束 0、打 safe/<今天>、指到 origin/main 的 commit" '[ $RC = 0 ] && [ "$(git -C "$T/repo" rev-parse "safe/$(today)^{commit}")" = "$head" ]'
check "nightly：tag 是 annotated，說明寫了步驟" '[ "$(git -C "$T/repo" cat-file -t "safe/$(today)")" = tag ] && git -C "$T/repo" tag -l --format="%(contents)" "safe/$(today)" | grep -q "全過"'
check "nightly：nightly.json 記 ok、兩步、safe_tag" '[ "$(jget "$STATE/nightly.json" ok)" = True ] && [ "$(jget "$STATE/nightly.json" safe_tag)" = "safe/$(today)" ] && [ "$(jget "$STATE/nightly.json" steps.0.name)" = true ] && [ "$(jget "$STATE/nightly.json" commit)" = "$head" ]'
check "nightly：沒有 --push，origin 沒有新 tag" '[ -z "$(origin_tags)" ]'
check "nightly：全過後順便跑了 prepare（next.json 建議 0.1.0）" '[ "$(jget "$STATE/next.json" version)" = 0.1.0 ]'
check "nightly：staging 清掉了" '[ -z "$(ls "$STATE/staging" 2>/dev/null)" ]'

rel "true" nightly
check "nightly：同一個 commit 不重跑" '[ $RC = 0 ] && echo "$OUT" | grep -q "測過了" && [ "$(repo_tags)" = "safe/$(today) " ]'
commit "fix: b"
rel "true" nightly
check "nightly：同一天第二個 safe 叫 .2" '[ $RC = 0 ] && git -C "$T/repo" rev-parse -q --verify "refs/tags/safe/$(today).2" >/dev/null'
rel "true" nightly --force
check "nightly --force：同一個 commit 重跑，變 .3" '[ $RC = 0 ] && git -C "$T/repo" rev-parse -q --verify "refs/tags/safe/$(today).3" >/dev/null'
rel "true" nightly --push --force
check "nightly --push：safe 推到 origin" 'origin_tags | grep -q "safe/$(today).4"'

# 呼叫者的 EVERGENIE_AGENT／EVERGENIE_HOME 不能漏進測試步驟（DEV-32：開發用秘書跑 nightly 時 CLI 測試誤判成不是主人）
newenv; commit "feat: a"
EVERGENIE_AGENT=admin EVERGENIE_HOME=/nonexistent rel '[ -z "${EVERGENIE_AGENT:-}" ] && [ -z "${EVERGENIE_HOME:-}" ]' nightly
check "nightly：步驟看不到呼叫者的 EVERGENIE_AGENT／EVERGENIE_HOME" '[ $RC = 0 ]'

# 附帶的 tmux：步驟裡有 dist-test／install-test／release-e2e 才備；快取在 $STATE/tmux，build-tmux.sh 沒變就不重編（--force 也不），
# 變了就重編；用 EVERGENIE_DIST_TMUX 交給步驟；編失敗跟步驟失敗一樣。build-tmux.sh 是假的：只建兩種晶片的 tmux 與 disclaim、記一筆被叫了幾次。
TMUX_BUILDS="$BASE/tmux-builds.log"
fake_build_tmux() { # fake_build_tmux <結束碼>：在開發端放一支假的 scripts/build-tmux.sh 並推上去
  cat >"$T/dev/scripts/build-tmux.sh" <<FAKE
#!/usr/bin/env bash
# ${RANDOM}${RANDOM}（每次內容不一樣）
echo "\$*" >>"$TMUX_BUILDS"
[ "$1" = 0 ] || exit "$1"
while [ \$# -gt 0 ]; do case "\$1" in --out) out="\$2"; shift ;; esac; shift; done
mkdir -p "\$out/arm64" "\$out/x86_64" "\$out/LICENSES"
printf '#!/bin/sh\necho tmux\n' >"\$out/arm64/tmux"; cp "\$out/arm64/tmux" "\$out/x86_64/tmux"; chmod +x "\$out"/*/tmux
cp "\$out/arm64/tmux" "\$out/arm64/disclaim"; cp "\$out/arm64/tmux" "\$out/x86_64/disclaim"; chmod +x "\$out"/*/disclaim
FAKE
  (cd "$T/dev" && git add -A && git commit -q -m "chore: build-tmux" && git push -q origin HEAD:main)
}
newenv; : >"$TMUX_BUILDS"; fake_build_tmux 0
rel '[ -x "$EVERGENIE_DIST_TMUX/arm64/tmux" ] # install-test' nightly
check "nightly 附帶的 tmux：先編一次（記成第一步）、步驟看得到 EVERGENIE_DIST_TMUX" '[ $RC = 0 ] && [ "$(wc -l <"$TMUX_BUILDS" | tr -d " ")" = 1 ] && [ -x "$STATE/tmux/arm64/tmux" ] && jget "$STATE/nightly.json" steps.0.name | grep -q "編附帶的 tmux" && grep -q -- "--out $STATE/tmux.new --src $STATE/tmux-src" "$TMUX_BUILDS"'
rel '[ -x "$EVERGENIE_DIST_TMUX/x86_64/tmux" ] # dist-test' nightly --force
check "nightly 附帶的 tmux：build-tmux.sh 沒變就用快取（--force 也不重編）" '[ $RC = 0 ] && [ "$(wc -l <"$TMUX_BUILDS" | tr -d " ")" = 1 ] && ! jget "$STATE/nightly.json" steps.0.name | grep -q "編附帶的 tmux"'
fake_build_tmux 0
rel 'true # release-e2e' nightly
check "nightly 附帶的 tmux：build-tmux.sh 變了就重編" '[ $RC = 0 ] && [ "$(wc -l <"$TMUX_BUILDS" | tr -d " ")" = 2 ]'
rel 'true' nightly --force
check "nightly 附帶的 tmux：步驟用不到就不備" '[ $RC = 0 ] && [ "$(wc -l <"$TMUX_BUILDS" | tr -d " ")" = 2 ] && [ "$(jget "$STATE/nightly.json" steps.0.name)" = true ]'
newenv; : >"$TMUX_BUILDS"; fake_build_tmux 7
rel 'true # install-test' nightly
check "nightly 附帶的 tmux 編不起來：結束 1、不打 tag、後面的步驟不跑、快取沒換" '[ $RC = 1 ] && [ -z "$(repo_tags)" ] && [ "$(jget "$STATE/nightly.json" steps.0.ok)" = False ] && python3 -c "import json,sys; sys.exit(0 if len(json.load(open(\"$STATE/nightly.json\"))[\"steps\"])==1 else 1)" && [ ! -e "$STATE/tmux" ]'
: >"$NOTIFY_LOG"

newenv; commit "feat: a"
rel "true;false;true" nightly
check "nightly 某一步失敗：結束 1、不打 tag、後面的步驟不跑" '[ $RC = 1 ] && [ -z "$(repo_tags)" ] && [ "$(jget "$STATE/nightly.json" ok)" = False ] && [ "$(jget "$STATE/nightly.json" steps.1.ok)" = False ] && ! python3 -c "import json,sys; sys.exit(0 if len(json.load(open(\"$STATE/nightly.json\"))[\"steps\"])==3 else 1)"'
check "nightly 失敗：staging 留著、log 在 logs/" '[ -n "$(ls "$STATE/staging" 2>/dev/null)" ] && [ -n "$(ls "$STATE/logs" 2>/dev/null)" ]'
check "nightly 失敗：發一則通知（測試裡是假的 evergenie，不會進真的家）" 'echo "$OUT" | grep -q "沒過" && grep -q "notify .*--lane system .*每晚測試沒過" "$NOTIFY_LOG"'
check "nightly 失敗：通知只發一次" '[ "$(grep -c "每晚測試沒過" "$NOTIFY_LOG")" = 1 ]'
rel "true" nightly
check "nightly：失敗過的 commit 不算測過，下次會重跑並打 tag" '[ $RC = 0 ] && [ -n "$(repo_tags)" ]'
: >"$NOTIFY_LOG"; touch "$FAKE_OLD"
rel "false" nightly --force
rm -f "$FAKE_OLD"
check "nightly 失敗：舊版 evergenie 不認得 --lane 就不帶，照樣發一次" '[ "$(grep -c "每晚測試沒過" "$NOTIFY_LOG")" = 1 ] && ! grep -q -- "--lane" "$NOTIFY_LOG"'

# ---- prepare -------------------------------------------------------------------------------------------
newenv; commit "fix: only fix"
rel "true" nightly >/dev/null
rel "true" prepare
check "prepare：沒有上一個正式版 → 0.1.0" '[ $RC = 0 ] && [ "$(jget "$STATE/next.json" version)" = 0.1.0 ] && [ "$(jget "$STATE/next.json" base)" = 0.0.0 ]'
git -C "$T/repo" tag -a v0.3.1 "$(git -C "$T/repo" rev-parse HEAD)" -m "舊版"
commit "docs: later"; commit "fix(web): 修一個畫面"; rel "true" nightly >/dev/null
rel "true" prepare
check "prepare：之後只有 fix → patch+1（0.3.2）" '[ "$(jget "$STATE/next.json" version)" = 0.3.2 ]'
check "prepare：發版說明分段、去掉 type(scope): 前綴" 'jget "$STATE/next.json" notes | grep -q "## 修正" && jget "$STATE/next.json" notes | grep -q -- "- 修一個畫面" && jget "$STATE/next.json" notes | grep -q "## 其他"'
commit "feat(admin): 新增 x"; rel "true" nightly >/dev/null
rel "true" prepare
check "prepare：有 feat → minor+1、patch 歸零（0.4.0）" '[ "$(jget "$STATE/next.json" version)" = 0.4.0 ] && jget "$STATE/next.json" notes | grep -q "## 新增"'
newenv
rel "true" prepare
check "prepare：還沒有 safe → 結束 1" '[ $RC = 1 ] && echo "$OUT" | grep -q "還沒有測過的 safe"'

# DEV-113：commit 寫中文、不寫 feat:／fix: 也要判得出來；只重新打包看板的不列
newenv; git -C "$T/repo" tag -a v0.1.0 "$(git -C "$T/repo" rev-parse HEAD)" -m "舊版"
commit "看板：任務單可以放附件（DEV-104）"; commit "dev-111: review 修正——快甩不算往上捲"; commit "看板：重新打包 dist（DEV-104 附件）"
commit "notify: 單檔被刪掉就收掉訊息"; commit "docs: 文件"
commit "S3-8 即時更新、在場；重建看板 dist"; commit "S3-7a 重建看板 dist"; commit "看板：修好按鈕"; commit "修正：聊天捲動"; commit "看板：修改標題"
rel "true" nightly >/dev/null
rel "true" prepare
check "prepare：0.x 有沒寫前綴的改動 → minor+1（0.2.0）" '[ $RC = 0 ] && [ "$(jget "$STATE/next.json" version)" = 0.2.0 ]'
check "prepare：中文 commit 分到「改動」「修正」；「notify: …」不當成標準前綴" 'n="$(jget "$STATE/next.json" notes)"; printf "%s" "$n" | grep -q "## 改動" && printf "%s" "$n" | grep -q -- "- 看板：任務單可以放附件" && printf "%s" "$n" | grep -q -- "- notify: 單檔被刪掉" && printf "%s" "$n" | sed -n "/## 修正/,/##/p" | grep -q "dev-111"'
check "prepare：「看板：修好…」「修正：…」算修正，「看板：修改…」算改動" 'fixes="$(jget "$STATE/next.json" notes | sed -n "/## 修正/,/^## 其他/p")"; printf "%s" "$fixes" | grep -q -- "- 看板：修好按鈕" && printf "%s" "$fixes" | grep -q -- "- 修正：聊天捲動" && ! printf "%s" "$fixes" | grep -q "修改標題" && jget "$STATE/next.json" notes | sed -n "/## 改動/,/^## 修正/p" | grep -q -- "- 看板：修改標題"'
check "prepare：只重新打包看板的 commit 不列；一般 commit 結尾的「；重建看板 dist」只剪掉那段、commit 照列" 'n="$(jget "$STATE/next.json" notes)"; ! printf "%s" "$n" | grep -q "重新打包 dist" && ! printf "%s" "$n" | grep -q "S3-7a" && printf "%s" "$n" | grep -qx -- "- S3-8 即時更新、在場" && printf "%s" "$n" | head -1 | grep -q "8 個 commit，另有 2 個只重新打包看板" && printf "%s" "$n" | head -1 | grep -q "白話" && [ "$(jget "$STATE/next.json" commits)" = 8 ] && [ "$(jget "$STATE/next.json" draft)" = True ]'
newenv; git -C "$T/repo" tag -a v0.1.0 "$(git -C "$T/repo" rev-parse HEAD)" -m "舊版"
commit "修好聊天捲動"; commit "看板：照獨立 review 修按鈕"; commit "docs: 文件"; rel "true" nightly >/dev/null
rel "true" prepare
check "prepare：只有修正（中文）與維護 → patch+1（0.1.1）" '[ "$(jget "$STATE/next.json" version)" = 0.1.1 ]'
newenv; git -C "$T/repo" tag -a v1.2.0 "$(git -C "$T/repo" rev-parse HEAD)" -m "舊版"
commit "看板：加一個東西"; rel "true" nightly >/dev/null
rel "true" prepare
check "prepare：1.0 起只有 feat 才 minor+1（沒寫前綴 → 1.2.1）" '[ "$(jget "$STATE/next.json" version)" = 1.2.1 ]'
commit "Feat: 大寫的前綴"; rel "true" nightly >/dev/null
rel "true" prepare
check "prepare：1.0 起有 feat（前綴大小寫不拘）→ minor+1（1.3.0）" '[ "$(jget "$STATE/next.json" version)" = 1.3.0 ] && jget "$STATE/next.json" notes | grep -q "## 新增"'
newenv; git -C "$T/repo" tag -a v0.1.0 "$(git -C "$T/repo" rev-parse HEAD)" -m "舊版"
commit "重建看板 dist"; rel "true" nightly >/dev/null
rel "true" prepare
check "prepare：全部都只是重新打包 → 0 個 commit、patch+1（0.1.1）" '[ "$(jget "$STATE/next.json" version)" = 0.1.1 ] && [ "$(jget "$STATE/next.json" commits)" = 0 ]'
newenv; git -C "$T/repo" tag -a v0.1.0 "$(git -C "$T/repo" rev-parse HEAD)" -m "舊版"
(cd "$T/dev" && for i in $(seq 1 205); do git commit -q --allow-empty -m "改動 $i"; done && git push -q origin HEAD:main)
rel "true" nightly >/dev/null
rel "true" prepare
check "prepare：超過 200 個 commit 也全部列（以前只取 200 條）" '[ "$(jget "$STATE/next.json" commits)" = 205 ] && jget "$STATE/next.json" notes | grep -qx -- "- 改動 1"'

# ---- notes（只換說明）----------------------------------------------------------------------------------
printf '## 新增\n- 任務單可以放附件\n' >"$T/plain.md"
rel "true" notes --notes "$T/plain.md" --dry-run
check "notes --dry-run：只印、不改草稿" '[ $RC = 0 ] && echo "$OUT" | grep -q "dry-run" && [ "$(jget "$STATE/next.json" draft)" = True ]'
rel "true" status
check "status：草稿還是原樣時說出來" 'echo "$OUT" | grep -q "還是原樣的 commit 清單"'
rel "true" notes --notes "$T/plain.md" --version 9.9.9
check "notes：帶 --version／--push 拒絕（版號在發正式版時給）" '[ $RC = 1 ] && echo "$OUT" | grep -q "不收"'
rel "true" notes --notes "$T/plain.md"
check "notes：說明換成檔案內容、版號不動、不再是草稿" '[ $RC = 0 ] && [ "$(jget "$STATE/next.json" notes)" = "$(printf "## 新增\n- 任務單可以放附件")" ] && [ "$(jget "$STATE/next.json" version)" = 0.2.0 ] && [ "$(jget "$STATE/next.json" draft)" = False ]'
rel "true" notes --notes "$T/nope.md"
check "notes：找不到檔 → 結束 1" '[ $RC = 1 ] && echo "$OUT" | grep -q "找不到"'
: >"$T/empty.md"; rel "true" notes --notes "$T/empty.md"
check "notes：空檔 → 結束 1" '[ $RC = 1 ] && echo "$OUT" | grep -q "空的"'
printf '  \n\t\n' >"$T/blank.md"; rel "true" notes --notes "$T/blank.md"
check "notes：只有空白的檔 → 結束 1" '[ $RC = 1 ] && echo "$OUT" | grep -q "空的"'
git -C "$T/repo" tag -a safe/2099-01-01 "$(git -C "$T/repo" rev-parse HEAD)" -m x
rel "true" notes --notes "$T/plain.md"
check "notes：草稿不是最新 safe 的 → 結束 1，叫人先 prepare" '[ $RC = 1 ] && echo "$OUT" | grep -q "草稿是舊的"'
rm -f "$STATE/next.json"; rel "true" notes --notes "$T/plain.md"
check "notes：沒有草稿 → 結束 1" '[ $RC = 1 ] && echo "$OUT" | grep -q "沒有下一版的草稿"'

# ---- stable --------------------------------------------------------------------------------------------
newenv; commit "feat: a"; rel "true" nightly >/dev/null
safec="$(git -C "$T/repo" rev-parse "safe/$(today)^{commit}")"
before_files="$(git -C "$T/repo" status --porcelain | wc -l | tr -d ' ')"; before_head="$(git -C "$T/repo" rev-parse HEAD)"; before_main="$(git -C "$T/origin.git" rev-parse main)"
rel "true" stable --version 0.1.0
check "stable：在 safe 的 commit 打 annotated v0.1.0、說明是發版說明" '[ $RC = 0 ] && [ "$(git -C "$T/repo" rev-parse v0.1.0^{commit})" = "$safec" ] && [ "$(git -C "$T/repo" cat-file -t v0.1.0)" = tag ] && git -C "$T/repo" tag -l --format="%(contents)" v0.1.0 | grep -q "## 新增"'
check "stable：用還沒改寫的草稿發，tag 說明不留「（草稿：…請改寫成白話）」那行" '! git -C "$T/repo" tag -l --format="%(contents)" v0.1.0 | grep -q "草稿"'
check "stable：沒有 --push，origin 沒有新 tag；不加 commit、不改檔案；next.json 刪了" '[ -z "$(origin_tags)" ] && [ "$(git -C "$T/repo" rev-parse HEAD)" = "$before_head" ] && [ "$(git -C "$T/origin.git" rev-parse main)" = "$before_main" ] && [ "$(git -C "$T/repo" status --porcelain | wc -l | tr -d " ")" = "$before_files" ] && [ ! -f "$STATE/next.json" ]'
check "stable：用 version.sh 看，那個 commit 的版本是 0.1.0" '[ "$(git -C "$T/repo" checkout -q --detach v0.1.0 && "$T/repo/scripts/version.sh")" = 0.1.0 ]'
git -C "$T/repo" checkout -q main
rel "true" stable --version 0.1.0
check "stable：tag 已存在 → 拒絕" '[ $RC = 1 ] && echo "$OUT" | grep -q "比上一個正式版"'
rel "true" stable --version 0.0.9
check "stable：版號沒有比上一個正式版大 → 拒絕" '[ $RC = 1 ] && echo "$OUT" | grep -q "比上一個正式版"'
rel "true" stable --version 1.0
check "stable：版號格式不對 → 拒絕" '[ $RC = 1 ] && echo "$OUT" | grep -q "X.Y.Z"'
git -C "$T/repo" tag -a v0.1.1 "$safec" -m x
commit "fix: more"; rel "true" nightly >/dev/null
printf '自己寫的說明\n第二行\n' >"$T/notes.md"
rel "true" stable --version 0.2.0 --notes "$T/notes.md" --push
check "stable --notes --push：tag 說明是檔案內容、origin 有 v0.2.0" '[ $RC = 0 ] && git -C "$T/repo" tag -l --format="%(contents)" v0.2.0 | grep -q "自己寫的說明" && origin_tags | grep -q "v0.2.0"'

newenv; commit "feat: a"; rel "true" nightly >/dev/null
# safe 不在 origin/main 上：origin 的 main 被改寫掉（force push 一個不含 safe 那個 commit 的歷史）
(cd "$T/dev" && git reset -q --hard HEAD~1 && echo other >g.txt && git add -A && git commit -q -m "other" && git push -q -f origin HEAD:main)
rel "true" stable --version 0.1.0
check "stable：safe 的 commit 不在 origin/main 上 → 拒絕" '[ $RC = 1 ] && echo "$OUT" | grep -q "不在 origin/main"'

# ---- dry-run / status / 用法 ---------------------------------------------------------------------------
newenv; commit "feat: a"
rel "false" nightly --dry-run
check "nightly --dry-run：只印、不 clone、不打 tag、不寫 state" '[ $RC = 0 ] && echo "$OUT" | grep -q "dry-run" && [ -z "$(repo_tags)" ] && [ ! -e "$STATE" ]'
rel "true" nightly >/dev/null
rel "true" prepare --dry-run
rel "true" stable --version 0.1.0 --dry-run
check "stable --dry-run：不打 tag" '[ $RC = 0 ] && ! git -C "$T/repo" rev-parse -q --verify refs/tags/v0.1.0 >/dev/null && [ -f "$STATE/next.json" ]'
rel "true" status
check "status：印出 safe、每晚測試、下一版草稿" '[ $RC = 0 ] && echo "$OUT" | grep -q "最新 safe：safe/" && echo "$OUT" | grep -q "ok=true" && echo "$OUT" | grep -q "下一版草稿：0.1.0"'
rel "true" status --json
check "status --json：是合法 JSON，有 safe、nightly、next" '[ $RC = 0 ] && printf "%s" "$OUT" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d[\"safe\"][\"name\"].startswith(\"safe/\") and d[\"nightly\"][\"ok\"] is True and d[\"next\"][\"version\"]==\"0.1.0\" and d[\"latest\"] is None"'
rel "true" bogus
check "用法錯誤 → 結束 2" '[ $RC = 2 ]'
rel "true" nightly --bogus
check "不認得的選項 → 結束 2" '[ $RC = 2 ]'

echo
echo "release-test：通過 ${pass}、失敗 $fail"
[ "$fail" = 0 ]
