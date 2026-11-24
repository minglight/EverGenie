#!/usr/bin/env bash
# 建「工坊」家（GOAL-2 S3，計畫 2026-10-31-dev-workshop-split W2～W4）：office 上的另一個家（法律第 7 條：每台一個家、不跟別的家連線），
# 開發 evergenie 用；任務單字首 DEV-、Goal 字首 DG-、自己的 launchd 服務名稱／看板埠／tmux socket。備份由主人自己處理。
#
# 用法：scripts/workshop-init.sh <家資料夾> --daily <日常家> [--public-url <工坊看板網址>]
#   <家資料夾>    要建的工坊家（完整路徑；正式是 ~/Workspace/<登入名>/evergenie-workshop）
#   --daily       日常的家（只讀）：帳號、使用者、提醒時段、[admin]、[secrets]、[incidents] 照抄；
#                 admin、code、architect 的 agent.md（拿掉 sites）、addendum.md、她們的角色卡與頭像、
#                 package 角色（例 architect.draft）所在的 packages/mine/<套件> 都從這裡複製
#   --public-url  [web] public_url（推播附的連結根，例 https://genie-dev-<owner>-office.example.ts.net；之後 tailscale setup 會改寫）
#
# 可以重跑：已經存在的檔一律不蓋（home.toml 已存在時只比對、把不一樣的列出來）。
# 能用 evergenie 指令的就用：新家用 `evergenie init` 建（資料夾與基本檔），角色包用 `evergenie personas pack sync`。
# 不起服務、不動 launchd／tailscale／tmux；下一步印在最後。
# 環境變數：EVERGENIE_BIN（evergenie 執行檔，預設 PATH 上的 evergenie）、EVERGENIE_OFFICIAL_PACKAGES_DIR（官方套件資料夾，預設這個 repo 的 packages/official）。
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
HG="${EVERGENIE_BIN:-evergenie}"
usage() { sed -n '5,10p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }

[ $# -ge 1 ] || usage
home="$1"; shift
daily=""; public_url=""
while [ $# -gt 0 ]; do
  case "$1" in
    --daily) daily="${2:-}"; shift ;;
    --public-url) public_url="${2:-}"; shift ;;
    *) usage ;;
  esac
  shift
done
[ -n "$daily" ] || usage
case "$home" in /*) ;; *) echo "家資料夾要給完整路徑：$home" >&2; exit 2 ;; esac
[ -f "$daily/home.toml" ] || { echo "找不到日常家的 home.toml：$daily" >&2; exit 2; }
[ "$(cd "$daily" && pwd -P)" != "$(mkdir -p "$home" && cd "$home" && pwd -P)" ] || { echo "工坊家不能跟日常家同一個資料夾" >&2; exit 2; }

fresh=0
if [ ! -f "$home/home.toml" ]; then
  name="$(python3 -I "$here/workshop-home.py" --daily "$daily" --print user.name)"
  tz="$(python3 -I "$here/workshop-home.py" --daily "$daily" --print user.timezone)"
  args=(init --home "$home" --port 8880)
  [ -n "$name" ] && args+=(--name "$name")
  [ -n "$tz" ] && args+=(--timezone "$tz")
  echo "建新家：$HG ${args[*]}"
  "$HG" "${args[@]}"
  fresh=1
else
  echo "家已經有 home.toml：只補沒有的檔，home.toml 只比對不改"
fi

official="${EVERGENIE_OFFICIAL_PACKAGES_DIR:-$here/../packages/official}"
python3 -I "$here/workshop-home.py" --home "$home" --daily "$daily" \
  --public-url "$public_url" --official "$official" --fresh "$fresh"

# 角色包：這一版有指標就下載公開角色（沒有指標什麼都不做）；失敗不擋（上面已從日常家複製秘書用到的卡）。
if ! "$HG" personas pack sync --home "$home"; then
  echo "注意：personas pack sync 沒成功（不擋；秘書用到的角色卡已從日常家複製）"
fi

cat <<EOF

下一步（腳本不做）：
  1. 搬資料：python3 -I scripts/split-workshop.py --daily <日常家（或 mini 的唯讀副本）> --workshop $home   （先預演，看過再加 --apply）
  2. 檢查：EVERGENIE_HOME=$home evergenie doctor
  3. 裝服務：evergenie service install --home $home   （服務名稱照 [runtime] service_label）
  4. 之後部署：scripts/deploy.sh --workshop office（工坊跑主資料夾的 bin/，不放 ~/.evergenie-homes；D-53）
  5. evergenie tailscale setup --home $home --service genie-dev-<owner>-<machine>   （例 genie-dev-<owner>-office；Tailscale 服務，D-43）
詳見 ai_docs/runbooks/release.md「工坊」。
EOF
