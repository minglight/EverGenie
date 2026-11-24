#!/usr/bin/env bash
# scripts/workshop-init.sh 與 scripts/split-workshop.py 的驗收（GOAL-2 S3、S4）。結束碼 0＝全過。
# 全程在暫存資料夾：假的日常家（自己造的 DEV 單、Goal、秘書、角色）＋假的 evergenie（只認 init 與 personas pack sync）；
# 不編譯、不起服務、不碰真的家。真資料的演練見 ai_docs/runbooks/release.md「工坊」。
set -uo pipefail
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它
cd "$(dirname "$0")/.."
ROOT="$PWD"
BASE="$(mktemp -d "${TMPDIR:-/tmp}/hg-workshop-test.XXXXXX")"
trap 'rm -rf "$BASE"' EXIT
pass=0; fail=0
ok() { echo "PASS  $1"; pass=$((pass + 1)); }
bad() { echo "FAIL  $1"; fail=$((fail + 1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1"; echo "      --- 輸出 ---"; printf '%s\n' "${OUT:-}" | sed 's/^/      /'; fi; }
tree_sum() { (cd "$1" && find . -type f ! -name '*.log' | LC_ALL=C sort | while read -r f; do printf '%s ' "$f"; shasum "$f" | cut -c1-40; done) | shasum | cut -c1-40; }
fm() { python3 -I -c 'import re,sys; t=open(sys.argv[1]).read(); h=re.match(r"---\n(.*?)\n---",t,re.S).group(1); m=re.search(r"^"+sys.argv[2]+r":\s*(.*)$",h,re.M); print(m.group(1).strip().strip("\"") if m else "")' "$1" "$2"; }

# ---- 假的 evergenie ----
mkdir -p "$BASE/bin"
cat >"$BASE/bin/evergenie" <<'SH'
#!/usr/bin/env bash
case "$1" in
  init)
    shift; home=""; port=8765; name="我"; tz="UTC"
    while [ $# -gt 0 ]; do case "$1" in --home) home="$2"; shift ;; --port) port="$2"; shift ;; --name) name="$2"; shift ;; --timezone) tz="$2"; shift ;; esac; shift; done
    if [ -e "$home" ] && [ -n "$(ls -A "$home")" ]; then echo "這裡已經有東西了：$home" >&2; exit 1; fi
    mkdir -p "$home/settings" "$home/vault" "$home/runtime/logs" "$home/personas"
    printf 'schema = 1\nmode = "simple"\n\n[user]\nname = "%s"\nlanguage = "zh-TW"\ntimezone = "%s"\n\n[slots]\ndefault = "life"\n\n[slots.life]\nlabel = "日常"\nremind = "08:30"\ncheck = "18:00"\n\n[accounts]\ndefault = "main"\n\n[accounts.main]\nengine = "claude"\n\n[web]\nport = %s\n\n[update]\nchannel = "release"\n' "$name" "$tz" "$port" >"$home/home.toml"
    echo "家建好了：$home" ;;
  personas) echo "這一版沒有角色包" ;;
  *) echo "假的 evergenie 不認得：$*" >&2; exit 2 ;;
esac
SH
chmod +x "$BASE/bin/evergenie"
export EVERGENIE_BIN="$BASE/bin/evergenie"
unset EVERGENIE_HOME EVERGENIE_AGENT EVERGENIE_SITE

# ---- 假的日常家 ----
D="$BASE/daily"
mkdir -p "$D/vault/admin/archive/2026-10" "$D/vault/goals" "$D/vault/messages" "$D/personas/avatars" "$D/packages/mine/arch/personas"
cat >"$D/home.toml" <<'TOML'
mode = "full"
schema = 1

[accounts]
  default = "personal"
  [accounts.personal]
    engine = "claude"
    remote_access = true
    usage_warmup = true
    [accounts.personal.engine_config]
      config_dir = "/Users/test/.claude"
      env_file = "~/.secret.sh"

[admin]
  agents = ["admin", "code", "architect"]
  dev_site = "office"
  public_personas = ["kaguya"]

[slots]
  default = "work"
  [slots.work]
    check = "15:00"
    label = "工作" # 註解
    remind = "09:00"

[user]
  name = "測試者"
  timezone = "Asia/Taipei"

[incidents]
  dispatcher = "admin"
  reviewer = "architect"
TOML
dev() { # dev <n> <status> [額外欄位…]
  local n="$1" st="$2"; shift 2
  { echo '---'; echo "title: 測試單 $n"; echo "status: $st"; for l in "$@"; do echo "$l"; done
    echo 'by: owner(cli)'; echo 'created_at: "2026-10-28T10:00:00+08:00"'; echo "id: DEV-$n"; echo 'origin: mini'; echo 'schema: 1'
    echo 'updated_at: "2026-10-29T10:00:00+08:00"'; echo '---'; echo "內文 $n"; } >"$D/vault/admin/DEV-$n.md"
}
dev 1 done 'closed_at: "2026-10-29T10:00:00+08:00"'
dev 2 idea 'agent: admin' 'links: []'
dev 3 doing 'agent: code' 'links:' '    - https://example.com/a' '    - DEV-1'
dev 4 verifying 'agent: code' 'result: |-' '    做好了' '    第二行' 'verify_how: 開看板看'
dev 5 cancelled 'closed_at: "2026-10-29T11:00:00+08:00"'
dev 7 verifying 'agent: admin'
mv "$D/vault/admin/DEV-5.md" "$D/vault/admin/archive/2026-10/DEV-5.md"
cat >"$D/vault/goals/GOAL-2.md" <<'MD'
---
title: 開發跟日常分開
text: 原話不能改
grants:
    - push
agent: owner(web)
state: active
confirmed_at: "2026-10-31T09:12:56+08:00"
confirmed_by: owner(web)
planbook_log:
    - at: "2026-10-31T09:39:11+08:00"
      by: architect
      note: 開工
by: architect
created_at: "2026-10-31T09:12:56+08:00"
id: GOAL-2
schema: 1
updated_at: "2026-10-31T09:39:11+08:00"
---
# Planbook
- 第一棒
MD
cp "$D/vault/goals/GOAL-2.md" "$BASE/goal2.orig"
printf -- '---\nschema: 1\nid: M-3\nroom: admin-dev-4\nstate: unread\n---\n' >"$D/vault/messages/M-3.md"
printf -- '---\nschema: 1\nid: M-4\ntask: "[[SEC-1]]"\nstate: unread\n---\n' >"$D/vault/messages/M-4.md"
for a in admin code architect life; do
  mkdir -p "$D/vault/agents/$a/journal"
  persona=kaguya; [ "$a" = code ] && persona=athena; [ "$a" = architect ] && persona=arch.draft
  printf -- '---\nschema: 1\nid: %s\npersona: %s\nenabled: true\nmode: resident\naccount: personal\nreminder_slot: work\nsites:\n    - office\n    - hub\nextras: {}\n---\n職責\n' "$a" "$persona" >"$D/vault/agents/$a/agent.md"
  printf -- '---\nschema: 1\nagent: %s\n---\n# 記憶\n' "$a" >"$D/vault/agents/$a/memory.md"
  printf -- '---\nschema: 1\nagent: %s\ndate: "2026-10-30"\n---\n日誌\n' "$a" >"$D/vault/agents/$a/journal/2026-10-30.md"
done
echo '附加規則' >"$D/vault/agents/admin/addendum.md"
for p in kaguya athena; do echo "# $p" >"$D/personas/$p.md"; echo png >"$D/personas/avatars/$p.png"; done
echo '# style' >"$D/personas/_style.md"
printf 'schema = 1\nid = "arch"\n' >"$D/packages/mine/arch/package.toml"
echo '# draft' >"$D/packages/mine/arch/personas/draft.md"

W="$BASE/ws"
OFF="$BASE/official"; mkdir -p "$OFF/engine-claude" "$OFF/admin" "$OFF/closet"
for p in engine-claude admin closet; do echo 'schema = 1' >"$OFF/$p/package.toml"; done
init() { EVERGENIE_OFFICIAL_PACKAGES_DIR="$OFF" bash scripts/workshop-init.sh "$W" --daily "$D" "$@" 2>&1; }
split() { python3 -I scripts/split-workshop.py --daily "$D" --workshop "$W" "$@" 2>&1; }

# ---- workshop-init.sh ----
OUT="$(EVERGENIE_OFFICIAL_PACKAGES_DIR="$OFF" bash scripts/workshop-init.sh "$W" 2>&1)"; rc=$?
check "init 少了 --daily 是用法錯誤、什麼都不建" '[ $rc -eq 2 ] && [ ! -e "$W" ]'
OUT="$(init --public-url https://office:8443)"; rc=$?
H="$W/home.toml"
check "init 建好家" '[ $rc -eq 0 ] && [ -f "$H" ]'
check "home.toml 有工坊的字首、服務名、看板埠、tmux socket" 'grep -q "^tasks = \"DEV-\"" "$H" && grep -q "^goals = \"DG-\"" "$H" && grep -q "^service_label = \"com.evergenie.workshop\"" "$H" && grep -q "^board_port = 8880" "$H" && grep -q "^tmux_socket = \"hg-ws\"" "$H"'
check "home.toml 有 keep_review_open、更新關掉、沒有跨台備份" 'grep -q "^keep_review_open = true" "$H" && ! grep -q "^\[backup\]" "$H" && grep -A1 "^\[update\]" "$H" | grep -q "channel = \"off\""'
check "帳號、使用者、提醒時段照日常抄；暖身不重複" 'grep -q "^\[accounts.personal.engine_config\]" "$H" && grep -q "config_dir = \"/Users/test/.claude\"" "$H" && grep -q "^usage_warmup = false" "$H" && grep -q "^name = \"測試者\"" "$H" && grep -q "^default = \"work\"" "$H" && grep -q "^public_url = \"https://office:8443\"" "$H"'
check "[admin] 照抄但拿掉 dev_site；[incidents] 照抄" '! grep -q dev_site "$H" && grep -q "^public_personas" "$H" && grep -q "^dispatcher = \"admin\"" "$H"'
check "套件：engine-claude、admin、帶過來的 mine 套件開，其他官方套件關" 'grep -q "^admin = true" "$W/settings/packages.toml" && grep -q "^engine-claude = true" "$W/settings/packages.toml" && grep -q "^arch = true" "$W/settings/packages.toml" && grep -q "^closet = false" "$W/settings/packages.toml"'
check "三位秘書都有、life 沒有；agent.md 拿掉 sites、其他欄位照原樣" '[ -f "$W/vault/agents/admin/agent.md" ] && [ -f "$W/vault/agents/code/agent.md" ] && [ -f "$W/vault/agents/architect/agent.md" ] && [ ! -e "$W/vault/agents/life" ] && ! grep -q "sites\|    - office" "$W/vault/agents/code/agent.md" && grep -q "^extras: {}" "$W/vault/agents/code/agent.md" && [ -f "$W/vault/agents/admin/addendum.md" ]'
check "角色卡、頭像、說話層、套件裡的角色都複製" '[ -f "$W/personas/kaguya.md" ] && [ -f "$W/personas/athena.md" ] && [ -f "$W/personas/avatars/athena.png" ] && [ -f "$W/personas/_style.md" ] && [ -f "$W/packages/mine/arch/personas/draft.md" ] && [ ! -e "$W/personas/luna.md" ]'
before="$(tree_sum "$W")"
OUT="$(init)"; rc=$?
check "init 重跑：什麼都不改" '[ $rc -eq 0 ] && [ "$(tree_sum "$W")" = "$before" ] && printf "%s" "$OUT" | grep -q "已有工坊的設定"'
sed -i.tmp 's/^board_port = 8880/board_port = 9999/' "$H" && rm -f "$H.tmp"
OUT="$(init)"; rc=$?
check "init 重跑遇到不一樣的設定：列出來、不改" '[ $rc -eq 0 ] && grep -q "^board_port = 9999" "$H" && printf "%s" "$OUT" | grep -q "board_port = 9999，應該是 8880"'
sed -i.tmp 's/^board_port = 9999/board_port = 8880/' "$H" && rm -f "$H.tmp"

# ---- split-workshop.py ----
dsum="$(tree_sum "$D")"; wsum="$(tree_sum "$W")"
OUT="$(split)"; rc=$?
check "預演：列出會搬的單、什麼都不寫" '[ $rc -eq 0 ] && printf "%s" "$OUT" | grep -q "任務單 DEV-4（review）" && [ "$(tree_sum "$D")" = "$dsum" ] && [ "$(tree_sum "$W")" = "$wsum" ]'
check "預演：工坊還沒收齊就不搬日常的 vault/admin" 'printf "%s" "$OUT" | grep -q "這次不搬"'
check "列出要封存的 DEV 通知（只有 DEV 相關的）" 'printf "%s" "$OUT" | grep -q "1 則：M-3"'
cp -R "$W" "$BASE/noids" && sed -i.tmp '/^tasks = "DEV-"/d' "$BASE/noids/home.toml"
OUT="$(python3 -I scripts/split-workshop.py --daily "$D" --workshop "$BASE/noids" --apply 2>&1)"; rc=$?
check "工坊沒有 [ids] tasks = DEV- 就拒絕、不寫" '[ $rc -ne 0 ] && [ ! -e "$BASE/noids/vault/tasks" ] && [ -d "$D/vault/admin" ]'
mkdir -p "$W/vault/agents/code/journal" && printf -- '---\nschema: 1\nagent: code\ndate: "2026-10-30"\n---\n工坊自己寫的\n' >"$W/vault/agents/code/journal/2026-10-30.md"
OUT="$(split --only workshop --apply)"; rc=$?
T="$W/vault/tasks"
check "工坊那一半：DEV 單變任務單（封存的進 archive/<月>）" '[ $rc -eq 0 ] && [ -f "$T/DEV-1.md" ] && [ -f "$T/DEV-2.md" ] && [ -f "$T/DEV-3.md" ] && [ -f "$T/DEV-4.md" ] && [ -f "$T/DEV-7.md" ] && [ -f "$T/archive/2026-10/DEV-5.md" ]'
check "狀態對照：verifying→review、其餘原樣" '[ "$(fm "$T/DEV-4.md" status)" = review ] && [ "$(fm "$T/DEV-2.md" status)" = idea ] && [ "$(fm "$T/DEV-3.md" status)" = doing ] && [ "$(fm "$T/DEV-1.md" status)" = done ] && [ "$(fm "$T/archive/2026-10/DEV-5.md" status)" = cancelled ]'
check "標題、內文、秘書、時間、links 原樣" '[ "$(fm "$T/DEV-3.md" title)" = "測試單 3" ] && grep -q "^內文 3" "$T/DEV-3.md" && [ "$(fm "$T/DEV-3.md" agent)" = code ] && [ "$(fm "$T/DEV-3.md" created_at)" = "2026-10-28T10:00:00+08:00" ] && grep -q "    - DEV-1" "$T/DEV-3.md" && grep -q "^    第二行" "$T/DEV-4.md"'
check "任務單必填補齊：type、nudges、review 的等待時間與建議結局、結果與怎麼驗" '[ "$(fm "$T/DEV-2.md" type)" = task ] && [ "$(fm "$T/DEV-2.md" nudges)" = 0 ] && [ "$(fm "$T/DEV-7.md" waiting_since)" = "2026-10-29T10:00:00+08:00" ] && [ "$(fm "$T/DEV-7.md" proposed)" = done ] && [ -n "$(fm "$T/DEV-7.md" result)" ] && [ "$(fm "$T/DEV-7.md" verify)" = unverifiable ] && [ "$(fm "$T/DEV-4.md" verify_how)" = "開看板看" ] && [ "$(fm "$T/DEV-4.md" result)" = "|-" ]'
check "結束的單補驗收紀錄（主人寫的記主人）" '[ "$(fm "$T/DEV-1.md" accepted_by)" = "owner(cli)" ] && [ "$(fm "$T/DEV-1.md" accepted_at)" = "2026-10-29T10:00:00+08:00" ]'
check "任務單計數接在最大號、Goal 計數到 1" 'grep -q "^task = 7" "$W/vault/.counters.toml" && grep -q "^goal = 1" "$W/vault/.counters.toml"'
G="$W/vault/goals/DG-1.md"
check "GOAL-2 → DG-1：原文、grants、確認原樣，Planbook 加一行，日常那份不動" '[ "$(fm "$G" id)" = DG-1 ] && [ "$(fm "$G" text)" = "原話不能改" ] && grep -q "^    - push" "$G" && [ "$(fm "$G" confirmed_by)" = "owner(web)" ] && grep -q "note: \"從日常 GOAL-2 搬來\"" "$G" && grep -q "note: 開工" "$G" && grep -q "從日常 GOAL-2 搬來（原號" "$G" && cmp -s "$D/vault/goals/GOAL-2.md" "$BASE/goal2.orig"'
check "記憶與日誌複製；工坊自己寫過的不蓋；life 的不搬" '[ -f "$W/vault/agents/admin/memory.md" ] && [ -f "$W/vault/agents/architect/journal/2026-10-30.md" ] && grep -q "工坊自己寫的" "$W/vault/agents/code/journal/2026-10-30.md" && printf "%s" "$OUT" | grep -q "不蓋" && [ ! -e "$W/vault/agents/life" ]'
check "只做工坊那一半時日常不動" '[ "$(tree_sum "$D")" = "$dsum" ]'
OUT="$(split --apply)"; rc=$?
today="$(date +%Y%m%d)"
check "日常那一半：vault/admin 搬到 vault 外的備份（照備份規則命名）" '[ $rc -eq 0 ] && [ ! -e "$D/vault/admin" ] && [ -f "$D/admin.bak-$today-workshop-split/DEV-1.md" ] && [ -f "$D/admin.bak-$today-workshop-split/archive/2026-10/DEV-5.md" ]'
check "印出部署當下要手動做的事" 'printf "%s" "$OUT" | grep -q "agents rm admin" && printf "%s" "$OUT" | grep -q "runtime.cards false" && printf "%s" "$OUT" | grep -q "incidents.dispatcher" && printf "%s" "$OUT" | grep -q "update.channel"'
dsum="$(tree_sum "$D")"; wsum="$(tree_sum "$W")"
OUT="$(split --apply)"; rc=$?
check "第二次 --apply：沒有變動、兩邊的檔一樣" '[ $rc -eq 0 ] && printf "%s" "$OUT" | grep -q "^沒有變動" && printf "%s" "$OUT" | grep -q "工坊已有 6 張" && [ "$(tree_sum "$D")" = "$dsum" ] && [ "$(tree_sum "$W")" = "$wsum" ]'

# 不認得的狀態：停下來，什麼都不寫
D2="$BASE/daily2"; W2="$BASE/ws2"; mkdir -p "$D2/vault/admin" && cp "$D/home.toml" "$D2/" && cp -R "$W" "$W2" && rm -rf "$W2/vault/tasks"
printf -- '---\ntitle: x\nstatus: weird\nid: DEV-9\n---\n' >"$D2/vault/admin/DEV-9.md"
OUT="$(python3 -I scripts/split-workshop.py --daily "$D2" --workshop "$W2" --apply 2>&1)"; rc=$?
check "不認得的狀態：失敗、什麼都不寫" '[ $rc -ne 0 ] && [ ! -e "$W2/vault/tasks" ] && [ -d "$D2/vault/admin" ] && printf "%s" "$OUT" | grep -q weird'

echo "通過 ${pass}、失敗 ${fail}"
[ "$fail" -eq 0 ]
