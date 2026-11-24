#!/usr/bin/env bash
# 正式發版工具（給別的使用者的版本；P8 第二輪 S1、REQ-278、REQ-132）。在發行者的電腦（office）跑。
#
#   scripts/release.sh nightly [--push] [--force] [--dry-run]       全新 clone → 跑全部測試 → 全過才打 safe/<日期>
#   scripts/release.sh prepare [--dry-run]                          建議下一個正式版的版號，並寫發版說明草稿
#   scripts/release.sh notes --notes 檔案 [--dry-run]               只換掉草稿的發版說明（admin 秘書改寫成白話後用；DEV-113）
#   scripts/release.sh stable [--version X.Y.Z] [--notes 檔案] [--push] [--dry-run]
#                                                                   把最新的 safe 升格成正式版 vX.Y.Z
#   scripts/release.sh status [--json]                              上一個正式版、最新 safe、上次每晚測試、下一版草稿
#
# 兩層（沒有 beta）：`safe/YYYY-MM-DD[.N]`＝每晚測過全部通過的 commit；`vX.Y.Z`＝主人按了才把最新的 safe 升格成的正式版。
# 版號從 tag 來（scripts/version.sh）：不放 VERSION 檔，stable 不加 commit、不改任何檔案，只在 safe 的那個 commit 上打 tag。
# 推 tag 是對外動作：預設只打在本機，加 --push 才 `git push origin <tag>`。--dry-run 只印會做什麼，不 clone、不跑、不打 tag、不寫檔。
#
# 環境變數（都可選）：
#   EVERGENIE_RELEASE_REPO   發行用的 repo（預設 $HOME/Workspace/<登入名>/evergenie）；tag 打在這裡，nightly 從它的 origin/main 測
#   EVERGENIE_RELEASE_STATE  放 staging/、logs/、nightly.json、next.json 的地方（預設 $HOME/.evergenie-release）
#   EVERGENIE_RELEASE_STEPS  nightly 依序跑的步驟，分號分隔（預設 "make ci;make web-ci-full;make e2e-full;make compat;make release-e2e"）
#   EVERGENIE_RELEASE_NOTIFY_AGENT  失敗時用哪個職位 id 發通知（預設 admin）
# 附帶的 tmux 與它的啟動程式（engine-claude ECL-63、ECL-65）：步驟裡有 release-e2e、dist-test 或 install-test 時，先備好 scripts/build-tmux.sh 的產物
#   （全新 clone 裡沒有 dist/tmux）：快取在 $EVERGENIE_RELEASE_STATE/tmux，build-tmux.sh 內容沒變就不重編（--force 也不重編），
#   用 EVERGENIE_DIST_TMUX 交給步驟。要編時記成第一步「編附帶的 tmux」，失敗就跟其他步驟失敗一樣（不打 tag、後面不跑）。
# 結束碼：0 成功（或這個 commit 測過了）；1 沒過或不能做（原因在輸出）；2 用法錯誤。
set -uo pipefail
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它

usage() {
  cat >&2 <<'EOF'
用法：
  scripts/release.sh nightly [--push] [--force] [--dry-run]
  scripts/release.sh prepare [--dry-run]
  scripts/release.sh notes --notes 檔案 [--dry-run]
  scripts/release.sh stable [--version X.Y.Z] [--notes 檔案] [--push] [--dry-run]
  scripts/release.sh status [--json]
nightly：全新 clone origin/main、跑 EVERGENIE_RELEASE_STEPS，全過打 safe/<日期>；同一個 commit 測過就不重跑（--force 重跑）。
prepare：依上一個正式版到最新 safe 的 commit 建議版號並寫發版說明草稿。notes：只換掉草稿的發版說明。
stable：把最新 safe 升格成正式版。--push 才推 tag。
EOF
  exit 2
}

REPO="${EVERGENIE_RELEASE_REPO:-$HOME/Workspace/$(id -un)/evergenie}"
STATE="${EVERGENIE_RELEASE_STATE:-$HOME/.evergenie-release}"
STEPS="${EVERGENIE_RELEASE_STEPS:-make ci;make web-ci-full;make e2e-full;make compat;make release-e2e}"
NOTIFY_AGENT="${EVERGENIE_RELEASE_NOTIFY_AGENT:-admin}"

CMD="${1:-}"
[ -n "$CMD" ] || usage
shift
DRY=0 PUSH=0 FORCE=0 JSON=0 VERSION="" NOTES_FILE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1 ;;
    --push) PUSH=1 ;;
    --force) FORCE=1 ;;
    --json) JSON=1 ;;
    --version) shift; VERSION="${1:-}" ;;
    --notes) shift; NOTES_FILE="${1:-}" ;;
    -h | --help) usage ;;
    *) echo "不認得的選項：$1" >&2; usage ;;
  esac
  shift
done
case "$CMD" in nightly | prepare | notes | stable | status) ;; *) usage ;; esac

say() { printf '%s\n' "$*"; }
die() { printf '%s\n' "$*" >&2; exit 1; }
g() { git -C "$REPO" "$@"; }
command -v git >/dev/null 2>&1 || die "這台沒有 git"
command -v python3 >/dev/null 2>&1 || die "這台沒有 python3（發版工具用它寫 JSON）"
[ -d "$REPO/.git" ] || [ -f "$REPO/.git" ] || die "找不到 repo：${REPO}（用 EVERGENIE_RELEASE_REPO 指定）"

# 上一個正式版（沒有就空字串）。--merged 不加：正式版 tag 不會在別的分支上。
last_stable() { g tag --list 'v[0-9]*' --sort=-v:refname | head -1; }
last_safe() { g tag --list 'safe/*' --sort=-version:refname | head -1; }
# 版號 X.Y.Z → 三個數字，方便比較
ver_cmp() { # ver_cmp A B：A>B 印 1、A<B 印 -1、相等 0
  python3 - "$1" "$2" <<'EOF'
import sys
a, b = [tuple(int(x) for x in v.split('.')) for v in sys.argv[1:3]]
print((a > b) - (a < b))
EOF
}
json_get() { # json_get 檔案 欄位
  python3 - "$1" "$2" <<'EOF'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
    v = d.get(sys.argv[2], "")
    print(v if not isinstance(v, bool) else str(v).lower())
except Exception:
    print("")
EOF
}
iso_of() { g log -1 --format=%cI "$1" 2>/dev/null; }

# ---- nightly ----------------------------------------------------------------------------------------
# 附帶的 tmux 的快取（見開頭）：$STATE/tmux 裡記著編它的 build-tmux.sh 的 sha256，兩種晶片的 tmux 與啟動程式 disclaim 都在才算數。
tmux_sum() { [ -f "$1/scripts/build-tmux.sh" ] && shasum -a 256 "$1/scripts/build-tmux.sh" | awk '{print $1}'; }
tmux_ready() { # tmux_ready <staging>
  local want
  want="$(tmux_sum "$1")" || return 1
  [ -n "$want" ] && [ "$(cat "$STATE/tmux/.build-tmux.sha256" 2>/dev/null)" = "$want" ] &&
    [ -x "$STATE/tmux/arm64/tmux" ] && [ -x "$STATE/tmux/x86_64/tmux" ] && [ -x "$STATE/tmux/arm64/disclaim" ] && [ -x "$STATE/tmux/x86_64/disclaim" ]
}
tmux_build() { # tmux_build <staging>：編到 tmux.new，成功才換掉 tmux（原始碼快取在 tmux-src，檢查碼對就不重抓）
  local want
  want="$(tmux_sum "$1")" || { echo "這個 commit 沒有 scripts/build-tmux.sh"; return 1; }
  rm -rf "$STATE/tmux.new"
  (cd "$1" && bash scripts/build-tmux.sh --out "$STATE/tmux.new" --src "$STATE/tmux-src") || return 1
  [ -x "$STATE/tmux.new/arm64/tmux" ] && [ -x "$STATE/tmux.new/x86_64/tmux" ] && [ -x "$STATE/tmux.new/arm64/disclaim" ] && [ -x "$STATE/tmux.new/x86_64/disclaim" ] ||
    { echo "build-tmux.sh 跑完了，但少了 arm64 或 x86_64 的 tmux 或啟動程式 disclaim"; return 1; }
  printf '%s\n' "$want" >"$STATE/tmux.new/.build-tmux.sha256" || return 1
  rm -rf "$STATE/tmux" && mv "$STATE/tmux.new" "$STATE/tmux"
}

do_nightly() {
  if [ "$DRY" = 1 ]; then
    say "（dry-run：只印會做什麼，不 clone、不跑測試、不打 tag、不寫檔）"
    say "  git -C $REPO fetch origin"
    say "  head = origin/main 的 commit；跟 $STATE/nightly.json 的 commit 一樣就結束（測過了，--force 例外）"
    say "  git clone $REPO $STATE/staging/<短碼> → checkout 那個 commit"
    local s steps_dry=()
    IFS=';' read -ra steps_dry <<<"$STEPS"
    case "$STEPS" in *release-e2e* | *dist-test* | *install-test*) say "  附帶的 tmux：$STATE/tmux（scripts/build-tmux.sh 沒變就用快取，變了先重編），EVERGENIE_DIST_TMUX 指過去" ;; esac
    for s in "${steps_dry[@]}"; do say "  在 staging 裡跑：$s"; done
    say "  全過 → 在 $REPO 打 safe/$(date +%Y-%m-%d)（annotated）$([ "$PUSH" = 1 ] && echo '並推到 origin' || echo '（沒有 --push 不推）')；寫 $STATE/nightly.json；跑 prepare"
    say "  沒過 → 不打 tag，nightly.json 記 ok=false，有裝 evergenie 就發通知，結束 1"
    return 0
  fi
  g fetch --quiet origin || die "抓不到 origin（git fetch 失敗）：$REPO"
  local head short
  head="$(g rev-parse --verify --quiet origin/main^{commit})" || die "repo 裡沒有 origin/main：$REPO"
  short="${head:0:7}"
  mkdir -p "$STATE/logs" "$STATE/staging" || die "建不了 $STATE"
  if [ "$FORCE" = 0 ] && [ -f "$STATE/nightly.json" ] && [ "$(json_get "$STATE/nightly.json" commit)" = "$head" ] && [ "$(json_get "$STATE/nightly.json" ok)" = true ]; then
    say "這個 commit 測過了（${short}，$(json_get "$STATE/nightly.json" at)）：不重跑。要重跑請加 --force。"
    return 0
  fi
  local stage="$STATE/staging/$short"
  rm -rf "$stage"
  say "== nightly：測 $short =="
  git clone --quiet "$REPO" "$stage" || die "clone 失敗：$REPO → $stage"
  git -C "$stage" checkout --quiet --detach "$head" || die "切不到 $short"
  local rows="$STATE/staging/$short.steps.tsv" ok=1 n=0 name slug log t0 t1 rc
  : >"$rows"
  local step_list=() step_env=()
  case "$STEPS" in
    *release-e2e* | *dist-test* | *install-test*)
      step_env=(EVERGENIE_DIST_TMUX="$STATE/tmux")
      tmux_ready "$stage" || {
        n=$((n + 1))
        name="編附帶的 tmux（scripts/build-tmux.sh → $STATE/tmux）"
        log="$STATE/logs/$short-$n-build-tmux.log"
        t0=$SECONDS
        say "── 第 $n 步：$name"
        tmux_build "$stage" >"$log" 2>&1
        rc=$?
        t1=$((SECONDS - t0))
        if [ "$rc" = 0 ]; then
          printf '%s\t1\t%s\t%s\n' "$name" "$t1" "$log" >>"$rows"
          say "   ✔ ${t1} 秒"
        else
          printf '%s\t0\t%s\t%s\n' "$name" "$t1" "$log" >>"$rows"
          say "   ✘ ${t1} 秒（結束碼 ${rc}，log：${log}）"
          ok=0
        fi
      }
      ;;
  esac
  IFS=';' read -ra step_list <<<"$STEPS"
  for name in "${step_list[@]}"; do
    [ "$ok" = 1 ] || break
    name="$(printf '%s' "$name" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
    [ -n "$name" ] || continue
    n=$((n + 1))
    slug="$(printf '%s' "$name" | tr -c 'A-Za-z0-9\n' '-' | sed 's/-*$//')"
    log="$STATE/logs/$short-$n-$slug.log"
    t0=$SECONDS
    say "── 第 $n 步：$name"
    # 測試步驟不能繼承呼叫者（開發用秘書的 session）的身分與家：EVERGENIE_AGENT／EVERGENIE_HOME 等漏進去會讓 CLI 測試誤判成「不是主人」而紅（DEV-32）
    (cd "$stage" && env -u EVERGENIE_HOME -u EVERGENIE_AGENT -u EVERGENIE_SESSION -u EVERGENIE_LAUNCH -u EVERGENIE_ENGINE_SOCKET -u EVERGENIE_ENGINE_BIN_DIR -u EVERGENIE_API_SOCKET -u EVERGENIE_ENV_FILE ${step_env[@]+"${step_env[@]}"} bash -c "$name") >"$log" 2>&1
    rc=$?
    t1=$((SECONDS - t0))
    if [ "$rc" = 0 ]; then
      printf '%s\t1\t%s\t%s\n' "$name" "$t1" "$log" >>"$rows"
      say "   ✔ ${t1} 秒"
    else
      printf '%s\t0\t%s\t%s\n' "$name" "$t1" "$log" >>"$rows"
      say "   ✘ ${t1} 秒（結束碼 ${rc}，log：${log}）"
      ok=0
      break
    fi
  done
  [ "$n" -gt 0 ] || { rm -rf "$stage" "$rows"; die "沒有要跑的步驟（EVERGENIE_RELEASE_STEPS 是空的）"; }
  local tag="" day suffix=2
  if [ "$ok" = 1 ]; then
    day="$(date +%Y-%m-%d)"
    tag="safe/$day"
    while g rev-parse --verify --quiet "refs/tags/$tag" >/dev/null; do
      tag="safe/$day.$suffix"
      suffix=$((suffix + 1))
    done
    g tag -a "$tag" "$head" -m "nightly：$(printf '%s' "$STEPS" | tr ';' ',') 全過" || die "打不了 tag $tag"
    say "✓ 全過：在 $short 打了 $tag"
    if [ "$PUSH" = 1 ]; then
      g push --quiet origin "$tag" || say "（推 $tag 失敗：本機的 tag 還在，之後可以手動推）"
    else
      say "（沒有 --push：$tag 只在本機，沒推出去）"
    fi
  fi
  python3 - "$rows" "$STATE/nightly.json" "$head" "$short" "$ok" "$tag" <<'EOF'
import json, sys, datetime
rows, out, head, short, ok, tag = sys.argv[1:7]
steps = []
for line in open(rows, encoding="utf-8"):
    name, sok, secs, log = line.rstrip("\n").split("\t")
    steps.append({"name": name, "ok": sok == "1", "seconds": int(secs), "log": log})
doc = {
    "commit": head, "short": short,
    "at": datetime.datetime.now().astimezone().isoformat(timespec="seconds"),
    "ok": ok == "1", "steps": steps, "safe_tag": tag,
}
tmp = out + ".tmp"
with open(tmp, "w", encoding="utf-8") as f:
    json.dump(doc, f, ensure_ascii=False, indent=2)
import os
os.replace(tmp, out)
EOF
  rm -f "$rows"
  if [ "$ok" = 1 ]; then
    rm -rf "$stage"
    # prepare 的 die 會 exit：包在子 shell，草稿寫不成也不讓已經打了 safe 的 nightly 變成失敗
    ( do_prepare ) || say "（safe 打好了，但下一版草稿沒寫成：之後手動跑 scripts/release.sh prepare）"
    return 0
  fi
  say "✗ 每晚測試沒過：${short}。staging 留在 $stage 方便看。"
  if command -v evergenie >/dev/null 2>&1; then
    # 自動報告走系統那道（D-47：不推手機、不算進訊息徽章）。先問 --help 認不認得 --lane（舊版 evergenie 不認得就不帶）；
    # 只發一次，不用「失敗就再發」——第一次可能已經寫進去才逾時，再發會重複。
    local lane=()
    if evergenie notify --help 2>&1 | grep -q -- '--lane'; then lane=(--lane system); fi
    evergenie notify --agent "$NOTIFY_AGENT" --category done ${lane[@]+"${lane[@]}"} --title "每晚測試沒過：${short}（${name}）" >/dev/null 2>&1 || true
  fi
  return 1
}

# ---- prepare ----------------------------------------------------------------------------------------
do_prepare() {
  local safe base base_ver range
  safe="$(last_safe)"
  [ -n "$safe" ] || die "還沒有測過的 safe：先跑 scripts/release.sh nightly"
  base="$(last_stable)"
  base_ver="${base#v}"
  [ -n "$base_ver" ] || base_ver="0.0.0"
  if [ -n "$base" ]; then range="$base..$safe"; else range="$safe"; fi
  if [ "$DRY" = 1 ]; then
    say "（dry-run：只印）上一個正式版：${base:-（沒有）}；最新 safe：${safe}；會算 $range 的 commit、寫 $STATE/next.json"
    return 0
  fi
  local subjects="$STATE/prepare.subjects.txt"
  mkdir -p "$STATE" || die "建不了 $STATE"
  # 範圍裡全部的 commit（DEV-113：以前只取 200 條，v0.1.0 之後 261 條時前面的就漏了）；merge commit 不算。
  g log --no-merges --format=%s "$range" >"$subjects" 2>/dev/null
  python3 - "$STATE/next.json" "$base_ver" "$safe" "$(g rev-parse "$safe^{commit}")" "$subjects" <<'EOF' || { rm -f "$subjects"; die "寫不了 next.json"; }
import json, re, sys, os
out, base, safe, safe_commit, subj_file = sys.argv[1:6]
subjects = [s for s in open(subj_file, encoding="utf-8").read().split("\n") if s.strip()]
# 分類（DEV-113）：這個 repo 的 commit 多半寫中文、不寫 feat:／fix:，只認前綴會把 261 個改動全當成「其他」、只建議 patch。
#   feat → 新增；fix、或以「修」開頭（含去掉「dev-111：」「看板：」這類開頭後）、或寫了「review 修」的 → 修正；
#   docs／chore／test… 這些標準前綴 → 其他（維護，不算改動）；其餘沒寫前綴的 → 改動。
#   **只**重新打包看板 dist 的 commit 是雜訊，不列；一般 commit 結尾的「；重建看板 dist」只剪掉那一段，commit 照常分類。
CONV_TYPES = ("feat", "fix", "docs", "chore", "test", "tests", "ci", "build", "refactor", "style", "perf", "revert")
conv = re.compile(r"^(\w+)(\([^)]*\))?(!)?:\s*(.*)$")
lead = re.compile(r"^[^\s：:，,]{1,24}[：:]\s*")
tail_rebuild = re.compile(r"\s*[；;，,]\s*(重新打包|重建|rebuild)\s*(看板)?\s*(dist|看板)[^；;]*$", re.I)
only_rebuild = re.compile(r"^(rebase.*後)?(重新打包|重建|rebuild)(看板)?(dist|看板)$", re.I)
fixish = re.compile(r"^(修正|修好|修(?!改))|review\s*修", re.I)
def is_noise(s):
    rest = lead.sub("", s, count=1)
    rest = re.sub(r"^[A-Za-z]+\d*-[\w.-]*\s+", "", rest)  # 「S3-7a 重建看板 dist」這種步驟編號開頭
    rest = re.sub(r"[（(][^（()）]*[）)]\s*$", "", rest)
    return bool(only_rebuild.match(re.sub(r"\s+", "", rest)))
feat, fix, change, other, dropped = [], [], [], [], 0
for s in subjects:
    if is_noise(s):
        dropped += 1
        continue
    s = tail_rebuild.sub("", s)
    m = conv.match(s)
    # 只認標準的類型（大小寫不拘）；「notify: …」「dev-111: …」這種是模組或單號開頭，照沒寫前綴算
    if m and m.group(1).lower() in CONV_TYPES:
        kind, title = m.group(1).lower(), m.group(4)
        (feat if kind == "feat" else fix if kind == "fix" else other).append(title)
        continue
    rest = lead.sub("", s, count=1)
    (fix if fixish.search(s) or fixish.search(rest) else change).append(s)
major, minor, patch = (int(x) for x in base.split("."))
# 0.x：有新增或沒寫前綴的改動就 minor+1（1.0 之前 minor 就是「有新東西」）；只有修正與維護才 patch+1。
# 1.0 起照語意化版本：只有 feat 才 minor+1。major 不自動動。
if (major, minor, patch) == (0, 0, 0):
    version = "0.1.0"
elif feat or (major == 0 and change):
    version = "%d.%d.0" % (major, minor + 1)
else:
    version = "%d.%d.%d" % (major, minor, patch + 1)
parts = []
for head, items in (("新增", feat), ("改動", change), ("修正", fix), ("其他", other)):
    if items:
        parts.append("## " + head + "\n" + "\n".join("- " + i for i in items))
body = "\n\n".join(parts) if parts else "這一版沒有新的改動。"
kept = len(subjects) - dropped
summary = "（草稿：上一個正式版之後 %d 個 commit%s，原樣列出；發之前請改寫成使用者看得懂的白話。）" % (
    kept, ("，另有 %d 個只重新打包看板的沒列" % dropped) if dropped else "")
notes = summary + "\n\n" + body if subjects else body
doc = {"version": version, "base": base, "safe_tag": safe, "safe_commit": safe_commit, "notes": notes,
       "commits": kept, "draft": True}
tmp = out + ".tmp"
with open(tmp, "w", encoding="utf-8") as f:
    json.dump(doc, f, ensure_ascii=False, indent=2)
os.replace(tmp, out)
print("建議版號：" + version + "（上一個正式版 " + base + "，測過的 " + safe + "）")
print(notes)
EOF
  rm -f "$subjects"
}

# ---- notes ------------------------------------------------------------------------------------------
# 只換掉 next.json 的發版說明，版號、safe 不動（DEV-113）：prepare 的草稿是原樣的 commit 標題，
# admin 秘書跑完發版測試後改寫成白話，用這個寫回去，主人在「發版」頁看到的就是白話版。
do_notes() {
  local next="$STATE/next.json" safe draft_safe
  [ -z "$VERSION" ] && [ "$PUSH" = 0 ] && [ "$FORCE" = 0 ] || die "notes 只換說明：不收 --version／--push／--force（版號在發正式版時給）"
  [ -n "$NOTES_FILE" ] || die "要用 --notes 指定說明檔"
  [ -f "$NOTES_FILE" ] || die "找不到發版說明檔：$NOTES_FILE"
  [ -s "$NOTES_FILE" ] && grep -q '[^[:space:]]' "$NOTES_FILE" || die "發版說明檔是空的：$NOTES_FILE"
  [ -f "$next" ] || die "沒有下一版的草稿（${next}）：先跑 scripts/release.sh prepare"
  safe="$(last_safe)"
  draft_safe="$(json_get "$next" safe_tag)"
  [ "$draft_safe" = "$safe" ] || die "草稿是舊的（寫的是 ${draft_safe:-（沒有）}，最新測過的是 ${safe:-（沒有）}）：先跑 scripts/release.sh prepare"
  if [ "$DRY" = 1 ]; then
    say "（dry-run：只印）會把 $next 的發版說明換成 $NOTES_FILE 的內容（版號 $(json_get "$next" version) 不動）"
    return 0
  fi
  python3 - "$next" "$NOTES_FILE" <<'EOF' || die "寫不了 next.json"
import json, os, sys
path, notes_file = sys.argv[1:3]
doc = json.load(open(path, encoding="utf-8"))
doc["notes"] = open(notes_file, encoding="utf-8").read().strip()
doc["draft"] = False
tmp = path + ".tmp"
with open(tmp, "w", encoding="utf-8") as f:
    json.dump(doc, f, ensure_ascii=False, indent=2)
os.replace(tmp, path)
EOF
  say "✓ 發版說明換好了（版號 $(json_get "$next" version)，${safe}）"
}

# ---- stable -----------------------------------------------------------------------------------------
do_stable() {
  local next="$STATE/next.json" version notes safe safe_commit base_ver
  version="$VERSION"
  if [ -z "$version" ]; then
    [ -f "$next" ] || die "沒有 --version，也沒有 ${next}：先跑 scripts/release.sh prepare"
    version="$(json_get "$next" version)"
  fi
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "版號要寫成 X.Y.Z（例如 0.2.0），不是「${version}」"
  base_ver="$(last_stable)"; base_ver="${base_ver#v}"; [ -n "$base_ver" ] || base_ver="0.0.0"
  [ "$(ver_cmp "$version" "$base_ver")" = 1 ] || die "版號 $version 要比上一個正式版 $base_ver 大"
  if g rev-parse --verify --quiet "refs/tags/v$version" >/dev/null; then die "tag v$version 已經有了"; fi
  safe="$(last_safe)"
  [ -n "$safe" ] || die "還沒有測過的 safe：先跑 scripts/release.sh nightly"
  if [ -n "$NOTES_FILE" ]; then
    [ -f "$NOTES_FILE" ] || die "找不到發版說明檔：$NOTES_FILE"
    notes="$(cat "$NOTES_FILE")"
  elif [ -f "$next" ]; then
    # 還是原樣草稿（沒跑過 notes）時，拿掉開頭那行「（草稿：…請改寫成白話。）」，不讓它進 tag 的說明（DEV-113）
    notes="$(python3 - "$next" <<'EOF'
import json, sys
d = json.load(open(sys.argv[1], encoding="utf-8"))
n = d.get("notes", "")
if d.get("draft") and n.startswith("（草稿："):
    n = n.split("\n", 1)[1].lstrip("\n") if "\n" in n else ""
print(n)
EOF
)"
  else
    notes=""
  fi
  [ -n "$notes" ] || notes="正式版 $version"
  safe_commit="$(g rev-parse "$safe^{commit}")"
  g fetch --quiet origin 2>/dev/null || true
  if ! g merge-base --is-ancestor "$safe_commit" origin/main 2>/dev/null; then
    die "$safe 的 commit 不在 origin/main 上，不能升格成正式版"
  fi
  if [ "$DRY" = 1 ]; then
    say "（dry-run：只印）會在 ${safe}（${safe_commit:0:7}）打 annotated tag v$version$([ "$PUSH" = 1 ] && echo '，並推到 origin' || echo '（沒有 --push 不推）')；說明："
    printf '%s\n' "$notes" | sed 's/^/  /'
    return 0
  fi
  # 說明用檔案、--cleanup=verbatim 交給 git：用 -m 的話，以 # 開頭的行（Markdown 標題）會被當成註解刪掉。
  mkdir -p "$STATE" || die "建不了 $STATE"
  printf '%s\n' "$notes" >"$STATE/tag-message.txt"
  g tag -a --cleanup=verbatim -F "$STATE/tag-message.txt" "v$version" "$safe_commit" || { rm -f "$STATE/tag-message.txt"; die "打不了 tag v$version"; }
  rm -f "$STATE/tag-message.txt"
  say "✓ 正式版 v${version}：打在 ${safe}（${safe_commit:0:7}）。"
  if [ "$PUSH" = 1 ]; then
    g push --quiet origin "v$version" || die "tag 打好了但推 origin 失敗（本機的 v$version 還在，可以手動推）"
    say "  已推到 origin。"
  else
    say "  （沒有 --push：只在本機，沒推出去）"
  fi
  rm -f "$next"
}

# ---- status -----------------------------------------------------------------------------------------
do_status() {
  local base safe
  base="$(last_stable)"; safe="$(last_safe)"
  if [ "$JSON" = 1 ]; then
    python3 - "$REPO" "$STATE" "$base" "$safe" <<'EOF'
import json, os, subprocess, sys
repo, state, base, safe = sys.argv[1:5]
def git(*a):
    r = subprocess.run(["git", "-C", repo, *a], capture_output=True, text=True)
    return r.stdout.strip() if r.returncode == 0 else ""
def tag(name):
    if not name: return None
    c = git("rev-parse", name + "^{commit}")
    return {"name": name, "commit": c, "at": git("log", "-1", "--format=%cI", c),
            "notes": git("tag", "-l", "--format=%(contents)", name)}
def load(n):
    try: return json.load(open(os.path.join(state, n)))
    except Exception: return None
print(json.dumps({"latest": tag(base), "safe": tag(safe), "nightly": load("nightly.json"), "next": load("next.json")}, ensure_ascii=False, indent=2))
EOF
    return 0
  fi
  if [ -n "$base" ]; then
    say "上一個正式版：${base}（$(g rev-parse --short "$base^{commit}")，$(iso_of "$base^{commit}")）"
    say "  說明第一行：$(g tag -l --format='%(contents:subject)' "$base")"
  else
    say "上一個正式版：（還沒有）"
  fi
  if [ -n "$safe" ]; then
    say "最新 safe：${safe}（$(g rev-parse --short "$safe^{commit}")，$(iso_of "$safe^{commit}")）"
  else
    say "最新 safe：（還沒有）"
  fi
  if [ -f "$STATE/nightly.json" ]; then
    say "上次每晚測試：$(json_get "$STATE/nightly.json" short)，$(json_get "$STATE/nightly.json" at)，ok=$(json_get "$STATE/nightly.json" ok)"
  else
    say "上次每晚測試：（還沒跑過）"
  fi
  if [ -f "$STATE/next.json" ]; then
    say "下一版草稿：$(json_get "$STATE/next.json" version)（說明$([ "$(json_get "$STATE/next.json" draft)" = true ] && echo '還是原樣的 commit 清單' || echo '已改寫')，${STATE}/next.json）"
  else
    say "下一版草稿：（沒有；跑 prepare）"
  fi
}

case "$CMD" in
  nightly) do_nightly ;;
  prepare) do_prepare ;;
  notes) do_notes ;;
  stable) do_stable ;;
  status) do_status ;;
esac
