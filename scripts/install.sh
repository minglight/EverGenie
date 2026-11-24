#!/bin/bash
# evergenie 安裝程式（DEV-155 S3；REQ-279）：下載編好的程式、驗檢查碼、建一個新家、裝成服務、印出看板網址。
# 給 AI 照 repo 根目錄的 INSTALL.md 跑（主人在 Claude Code 貼 repo 連結說「幫我安裝」），也能自己跑。完全不互動。
#
#   bash install.sh [--name <名字>] [--home <家>] [--port <看板 port>] [--source <版本清單網址>] [選項…]
#
#   --name               秘書怎麼稱呼你（預設用電腦的使用者名稱）
#   --home               你的資料放哪（預設 ~/evergenie-<登入名>）；必須不存在或是空的
#   --port               看板的 port（預設 8765，被占用就往上找）
#   --timezone           時區（預設照這台電腦的設定）
#   --source             版本清單（manifest.json）的網址：https://…（預設是下面的 DEFAULT_SOURCE；測試可以用本機的 http://127.0.0.1… 或 file:///…）
#   --dir                程式放哪（預設 ~/.evergenie/app；從原始碼裝預設 ~/.evergenie/core）；必須不存在或是空的
#   --state              暫存、密鑰、官方角色放哪（家的外面；預設 ~/.evergenie/state/<家的資料夾名>）；必須不存在或是空的
#   --from-backup        從備份建家：給一個舊電腦做的備份檔（.egbackup），建好的家裡就是那份備份（換電腦用，DEV-153）
#   --no-service         不裝成服務（不啟動、不等看板；之後自己 <程式>/current/bin/evergenie serve --home <家>）
#   --skip-claude-check  不檢查 Claude 的指令工具有沒有裝、有沒有登入（只給測試用）
#   --from-source        改從原始碼裝（給開發用：要 git、Go、make、tmux）：--repo <git 網址>、--ref <vX.Y.Z|latest|main>
#
# 下載版裝出來的樣子：<程式>/versions/<版本>/（bin/、fixed/、packages/official/、sdk/、ai_docs/、VERSION、LICENSE、LICENSES/）、<程式>/current → versions/<版本>、
#   <程式>/fixed/（附帶的 tmux〔檔名是產品名〕與啟動程式 disclaim 的固定位置：升級時內容變了才換）、
#   <程式>/evergenie.local.toml（這份程式屬於哪個家）、~/.local/bin/evergenie → <程式>/current/bin/evergenie（那裡本來有別的就不動）。
#   服務跑 <程式>/current/bin/evergenie；升級（evergenie upgrade）換 current。只用 macOS 內建的工具（curl、tar、shasum、osascript）。
#   看板起來之後請服務同步一次這一版的角色包（角色的頭像與衣服；最多等 3 分鐘，沒成功或超時只印「注意：」，服務之後自己再試）。
#
# 結束碼（給 AI 判斷；每種失敗都會先印一句「沒裝成功（結束碼 N）：<白話原因>」，並把這次建的東西收掉，本來就有的不碰；
#   這次建的家已經有日誌的話，收掉之前先複製到 ~/Library/Logs/<產品名>-install-<時間>/，那句話最後寫「日誌留在 <那裡>/。」
#   （裝好時順手刪掉這個名字、超過 30 天的舊日誌資料夾）；服務起來過的家不刪，整個搬到旁邊，另印一行「注意：」說搬去哪，見下面）：
#    0  裝好了（最後幾行有看板網址；「注意：」開頭的是要轉告主人的提醒）
#    1  被中斷了（Ctrl-C 或逾時）；這次建的已經收掉，可以重跑
#    2  用法不對（不認得的選項、少了值、路徑裡有 . 或 ..）
#   10  這台不是 Mac、macOS 太舊（要 11 以上），或晶片認不得
#   11  找不到 Claude 的指令工具（claude）
#   12  Claude 還沒登入
#   13  家或狀態資料夾已經有東西（不會動它）
#   14  這台已經裝過（程式資料夾已經有東西，或已經有一個家用了服務名稱 com.evergenie.serve）、另一個安裝正在跑，
#       或上次沒裝完的那個家已經被用過、或開過服務卻搬不開（不會動它）
#   20  版本清單讀不到或看不懂（網路不通、網址不對、還沒有正式版、清單裡的下載網址不合下載來源的規則）
#   21  版本清單裡沒有這台晶片的包
#   22  下載失敗或下載到一半斷掉
#   23  下載的檔案跟清單的檢查碼對不上（檔案壞了或被換過）
#   24  包的內容不對（解不開、缺檔、版本對不上，或這台跑不動它）
#   25  硬碟寫不進去（建不了資料夾、空間不夠、沒有權限）
#   30  建家失敗（evergenie init）
#   31  裝成服務失敗
#   32  服務裝好了，但看板一直沒有起來
#   40  從原始碼裝：缺 git、Go、make 或 tmux
#   41  從原始碼裝：抓程式、找版本或編譯失敗
#
# 被強制停掉（kill -9、工具逾時）來不及收拾時：程式資料夾裡留著 .install-unfinished（記著這次建了什麼、有沒有裝服務、建家當時家的指紋）；
#   同一行再跑一次，會先照它收掉上次建的（只收這次也要用的位置；上層資料夾空的才刪）、再重新裝。上次那個還在跑就結束碼 14；
#   上次建的家已經不是剛建好的樣子（家裡的檔跟建家當時不一樣＝被用過，或建了家還沒記下指紋就被停掉）也是結束碼 14，什麼都不動。
#   裝好了卻刪不掉這個檔時改寫成 done（下次重跑當成已經裝好），不會把用過的家收掉。
# 服務起來過的家一律不刪（重跑時收上次的，或這次裝到服務之後才失敗、被中斷）：服務一開，主人就可能在看板（含套件頁，指紋看不出來）記了東西。
#   停掉服務後，家與狀態資料夾整個搬到旁邊：<家>.bak-<日期>-install-unfinished、<狀態資料夾>.bak-<日期>-install-unfinished（同一個尾巴；
#   照備份規則，同名已經有了就用 .bak-<日期>-<時分>-install-unfinished，再有就在最後加 -2、-3…，不蓋掉），印「注意：…整個搬到 <那裡>…」。
#   重跑時搬不開（上層寫不進去、名字都被用掉、停不掉服務）：結束碼 14，什麼都不動（已經停掉的服務重開，重開不了會說）；這次的失敗時搬不開：留在原處、不刪。
#   搬的那一下不理 Ctrl-C（不會停在家搬了、狀態資料夾還沒搬的中間）。這次的失敗時停不掉服務：這次裝的東西全部留著（.install-unfinished 也留著），
#   重跑時再照它收（停得掉才收）。
# --home、--state 不能是捷徑（符號連結），程式資料夾（--dir）跟家、狀態資料夾不能互相包含：收拾時會認錯，結束碼 2。
#
# 環境變數：EVERGENIE_SERVICE_MODE=process 時服務改成背景行程（測試用，不碰 launchd）；EVERGENIE_INSTALL_BOARD_WAIT 是等看板起來的秒數（預設 60）；
#   EVERGENIE_INSTALL_PACK_WAIT 是裝好之後等角色包同步的秒數上限（預設 180；超時只印一行「注意：」，不算安裝失敗）；
#   EVERGENIE_INSTALL_SOURCE 蓋過 DEFAULT_SOURCE；EVERGENIE_INSTALL_REPO 蓋過從原始碼裝的 DEFAULT_REPO；
#   EVERGENIE_INSTALL_DOWNLOAD_TIMEOUT 是下載包的秒數上限（預設 420；加上讀清單 60、等看板 60，在 AI 的指令逾時 15 分鐘內；另外每秒不到 20 KB 持續 60 秒也算斷掉）；
#   EVERGENIE_INSTALL_TEST_FAULT 只給 scripts/install-test.sh 用（在某一步故意失敗、被強制停掉，或假裝刪不掉 .install-unfinished、搬不開服務起來過的家、
#   停不掉服務；可以用逗號隔開給好幾個）。
# macOS 內建的是 bash 3.2：不用關聯陣列、${x,,}、mapfile；空陣列不展開。
set -uo pipefail

# ---- 名稱與網址 -------------------------------------------------------------------------------------------
# 產品名與網址只有這一段（DEV-158 已從舊名改成 evergenie）；要再改名照下面的清單改，make ci 綠了（TestProductNameSameEverywhere、
# TestInstallScriptNames、TestNoHardCodedProductName）就對了：
#   1. 這裡的 PRODUCT：dist.sh 拿它當包名與附帶 tmux 的檔名；APP_ROOT（~/.<名>）、SERVICE_LABEL、指標檔 <名>.local.toml、
#      失敗日誌 ~/Library/Logs/<名>-install-…、bin/<名>、「<名> 修復」（~/Applications/<名> 修復.command，程式裡是 platform.RescueName）都從它來。
#   2. Go 的 sdk/product.go 的 ProductName（核心的 platform.ProductName）：指標檔、預設狀態資料夾、服務名稱、bin/<名>、
#      引擎執行檔名都從它來，要跟 PRODUCT 一樣。
#   3. 執行檔名與資料夾：Makefile 的 build（與 e2e 用的 engine-fake）、scripts/dist.sh 的 BINS（兩邊同一份）、cmd/<名>/ 與各官方套件的
#      cmd/<名>-<套件>/ 資料夾。
#   4. 看得到的字：指令用法與訊息裡的 evergenie、INSTALL.md（開頭的表與內文）、README.md、
#      跟著發行檔帶的說明文件（ai_docs/bundled.txt 上的那幾份，主人那台的秘書用 evergenie docs 讀；秘書讀的文字裡的 evergenie docs show 也是指令）。
#      開發用的腳本寫死的 bin/evergenie（scripts/compat.sh、deploy*.sh、*-test.sh 等；跑它們的測試就會發現）一起換。
#   5. 換 repo：下面的 DEFAULT_SOURCE、DEFAULT_REPO；Go 的 module 路徑（go.mod 與 import）。
#   6. 給使用者的 sdk 模組路徑 github.com/minglight/evergenie/sdk（含 GitHub 帳號與產品名）：發行檔附 sdk/ 原始碼，主人自己的、
#      別人分享的附程式套件在使用者的電腦上 import 它編譯（packages PKG-66；程式裡從型別讀，不另寫）。改了，已經寫好的套件的 import
#      要跟著改；要改的地方：sdk/go.mod、各 go.mod 的 require／replace 與所有 import、
#      技能 create-feature 給的套件範本、scripts/*-test.sh 裡的小套件、ai_docs 的寫法（conventions/packages.md 的 go.mod 範例、
#      architecture.md D-17）、Go 測試裡寫死的 import 字串（internal/tools/doccheck/doccheck_test.go 等，git grep 找得到）。
#   7. 格式識別字（套件 manifest 的 api = "evergenie.v1"、匯出檔與角色包的 kind、看板請求的 X-Requested-With）、環境變數的字首
#      （sdk/env.go 的 EnvPrefix）、備份檔的副檔名（platform.HomeBackupExt）：改了已經裝好的家裡的舊檔要另外轉。
#   已經裝好的電腦：狀態資料夾、服務名稱、指標檔都會換名字，要另外做搬家。
PRODUCT="evergenie"
APP_ROOT="$HOME/.$PRODUCT"           # 程式（app/ 或 core/）與狀態資料夾（state/<家>）的預設上層
SERVICE_LABEL="com.$PRODUCT.serve"   # 服務名稱：要跟程式裡的 host.DefaultLabel 一樣（TestInstallScriptNames 會比）
# 版本清單的網址：scripts/dist.sh 沒給 --source 時讀這一行編進程式（升級從那裡找新版），兩邊才會一樣；這行的格式不要改。
# 公開 repo 的 GitHub Releases：最新正式版的清單（GitHub 把 releases/latest/download/<檔名> 轉到最新的 release；要跟下載來源的規則合，
# TestInstallScriptNames 會檢查）。
DEFAULT_SOURCE="https://github.com/minglight/EverGenie/releases/latest/download/manifest.json"
# 從原始碼裝的 git 網址（公開 repo；只給開發用的 --from-source）。
DEFAULT_REPO="https://github.com/minglight/EverGenie.git"
# -------------------------------------------------------------------------------------------------------------

# 下載來源的規則（PLT-25）：https://<主機>…；http:// 只收本機（127.0.0.1、localhost、[::1]，測試用）；file:///…。
# 跟程式的 platform.DownloadURLPattern、scripts/dist.sh 的 DOWNLOAD_URL_RE 一字不差（TestDownloadSourceRuleSameEverywhere 比對）。
DOWNLOAD_URL_RE='^(https://[A-Za-z0-9.-]+(:[0-9]+)?(/[^[:space:][:cntrl:]]*)?|http://(127\.0\.0\.1|localhost|\[::1\])(:[0-9]+)?(/[^[:space:][:cntrl:]]*)?|file:///[^[:space:][:cntrl:]]+)$'
valid_source() {
  local LC_ALL=C
  [[ "$1" =~ $DOWNLOAD_URL_RE ]]
}

# 別的家、別的 session 的設定不能跟著進來（init 會問錯引擎、連錯家）。
unset EVERGENIE_HOME EVERGENIE_API_SOCKET EVERGENIE_ENGINE_SOCKET EVERGENIE_AGENT EVERGENIE_LAUNCH EVERGENIE_SESSION EVERGENIE_ENV_FILE EVERGENIE_ENGINE_BIN_DIR EVERGENIE_OFFICIAL_PACKAGES_DIR EVERGENIE_PACKAGE_BIN_DIR
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它

usage() {
  if [ -r "$0" ] && head -1 "$0" 2>/dev/null | grep -q '^#!'; then
    awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$0" >&2
  else
    echo "用法：bash install.sh [--name <名字>] [--home <家>] [--port <port>] [--source <版本清單網址>] …（完整說明在 scripts/install.sh 開頭）" >&2
  fi
  exit 2
}
say() { printf '%s\n' "$*"; }
fault() { case ",${EVERGENIE_INSTALL_TEST_FAULT:-}," in *",$1,"*) return 0 ;; esac; return 1; } # 測試用（見開頭），可以用逗號隔開給好幾個
ON_SIG=0 # INT／TERM／HUP 現在是不是交給 on_signal（aside_move 搬完照它還原）
bad_usage() { # bad_usage <白話原因>：用法不對（結束碼 2），先印一句再印用法
  printf '\n沒裝成功（結束碼 2）：%s\n' "$1" >&2
  usage
}

MODE=download SOURCE="" REPO="" REF="latest" DIR="" HOME_DIR="" STATE_DIR="" NAME="" PORT="" TZ_NAME="" SERVICE=1 CLAUDE_CHECK=1 BACKUP_FILE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --from-source) MODE=source ;;
    --no-service) SERVICE=0 ;;
    --skip-claude-check) CLAUDE_CHECK=0 ;;
    -h | --help) usage ;;
    --source | --repo | --ref | --dir | --home | --state | --name | --port | --timezone | --from-backup)
      [ $# -ge 2 ] && [ -n "$2" ] || bad_usage "$1 後面要接值。"
      case "$1" in
        --source) SOURCE="$2" ;;
        --repo) REPO="$2" ;;
        --ref) REF="$2" ;;
        --dir) DIR="$2" ;;
        --home) HOME_DIR="$2" ;;
        --state) STATE_DIR="$2" ;;
        --name) NAME="$2" ;;
        --port) PORT="$2" ;;
        --timezone) TZ_NAME="$2" ;;
        --from-backup) BACKUP_FILE="$2" ;;
      esac
      shift ;;
    *) bad_usage "不認得的選項：$1" ;;
  esac
  shift
done
if [ "$MODE" = download ]; then
  [ -z "$REPO" ] || bad_usage "--repo 只能跟 --from-source 一起用。"
  [ "$REF" = latest ] || bad_usage "--ref 只能跟 --from-source 一起用。"
  [ -n "$DIR" ] || DIR="$APP_ROOT/app"
  [ -n "$SOURCE" ] || SOURCE="${EVERGENIE_INSTALL_SOURCE:-$DEFAULT_SOURCE}"
  valid_source "$SOURCE" || bad_usage "--source 要是 https:// 開頭的網址（現在是 ${SOURCE}）。"
else
  [ -z "$SOURCE" ] || bad_usage "--source 不能跟 --from-source 一起用。"
  [ -n "$DIR" ] || DIR="$APP_ROOT/core"
  [ -n "$REPO" ] || REPO="${EVERGENIE_INSTALL_REPO:-$DEFAULT_REPO}"
fi
case "$PORT" in "" | [1-9] | [1-9][0-9] | [1-9][0-9][0-9] | [1-9][0-9][0-9][0-9] | [1-9][0-9][0-9][0-9][0-9]) ;; *) bad_usage "--port 要是 1–65535 的數字。" ;; esac
[ -z "$PORT" ] || [ "$PORT" -le 65535 ] || bad_usage "--port 要是 1–65535 的數字。"
if [ -n "$BACKUP_FILE" ]; then
  case "$BACKUP_FILE" in /*) ;; *) BACKUP_FILE="$PWD/$BACKUP_FILE" ;; esac
  [ -f "$BACKUP_FILE" ] || bad_usage "--from-backup 找不到這個備份檔：${BACKUP_FILE}。"
fi

abs() { # abs <路徑>：~ 展開、相對路徑以現在的資料夾為準；有 . 或 .. 這一層的不收（失敗時收不準），印空字串
  local p="$1"
  case "$p" in "~") p="$HOME" ;; "~/"*) p="$HOME/${p#\~/}" ;; esac
  case "$p" in /*) ;; *) p="$PWD/$p" ;; esac
  while [ "${#p}" -gt 1 ] && [ "${p%/}" != "$p" ]; do p="${p%/}"; done
  case "$p/" in */./* | */../*) return 0 ;; esac
  printf '%s' "$p"
}
abs_or_usage() { # abs_or_usage <選項名> <路徑>
  local p
  p="$(abs "$2")"
  [ -n "$p" ] || bad_usage "$1 的路徑裡不能有 . 或 .. 這一層（$2）：請寫完整的路徑。"
  printf '%s' "$p"
}
[ -n "$HOME_DIR" ] || HOME_DIR="$HOME/$PRODUCT-$(id -un)"
DIR="$(abs_or_usage --dir "$DIR")" || exit 2
HOME_DIR="$(abs_or_usage --home "$HOME_DIR")" || exit 2
STATE_GIVEN=0
if [ -n "$STATE_DIR" ]; then
  STATE_GIVEN=1
  STATE_DIR="$(abs_or_usage --state "$STATE_DIR")" || exit 2
else
  STATE_DIR="$APP_ROOT/state/$(basename "$HOME_DIR")" # 跟 init 的預設同一個位置；只用來檢查與失敗時收掉
fi
# 家與狀態資料夾不收捷徑（符號連結）：收拾時看不準裡面有沒有主人的東西。程式資料夾跟家、狀態資料夾不能互相包含：收程式資料夾時會連家一起刪
[ ! -L "$HOME_DIR" ] || bad_usage "--home 不能是捷徑（符號連結）：${HOME_DIR}。請寫它指到的真正位置。"
[ ! -L "$STATE_DIR" ] || bad_usage "--state 不能是捷徑（符號連結）：${STATE_DIR}。請寫它指到的真正位置。"
overlap() { case "$1/" in "$2/"*) return 0 ;; esac; case "$2/" in "$1/"*) return 0 ;; esac; return 1; }
! overlap "$DIR" "$HOME_DIR" || bad_usage "程式資料夾（${DIR}）跟家（${HOME_DIR}）不能放在一起或一個在另一個裡面，請分開放。"
! overlap "$DIR" "$STATE_DIR" || bad_usage "程式資料夾（${DIR}）跟狀態資料夾（${STATE_DIR}）不能放在一起或一個在另一個裡面，請分開放。"

# ---- 失敗時收掉這次建的 ------------------------------------------------------------------------------------
TMP="$(mktemp -d "${TMPDIR:-/tmp}/$PRODUCT-install.XXXXXX")" || { echo "沒裝成功（結束碼 25）：建不了暫存資料夾。" >&2; exit 25; }
TAB="$(printf '\t')"
# PLACED：這次建的或填的位置，一行一筆「<路徑><TAB><種類><TAB><最上層>」，新的在前。失敗時照它收：
#   created  路徑原本不存在（最上層＝路徑上第一個原本不存在的那層）：刪掉路徑本身，再往上把「空的」上層刪到最上層為止
#            （上層用 rmdir：別人這段時間放了東西進去就留著，例如 ~/.evergenie 裡別的家的 state/）
#   filled   路徑原本就在、而且是空的：清空裡面，資料夾留著
#   bindir   ~/.local/bin 原本不存在：只收連結，資料夾空的才往上刪（裡面的東西絕不清；原本就在的 ~/.local/bin 不記）
PLACED=""
SVC_BIN="" SVC_HOME=""          # 這次裝了服務（失敗時卸掉；先確認還是這個家的）
SVC_UP=0                        # 這次（重跑時：上次沒裝完的那次）裝過服務：家一律不刪，搬到旁邊（keep_home）
KEPT=""                         # 搬到旁邊的家的「注意：」（失敗、被中斷時也印）
LINK_CREATED="" LINK_TARGET=""  # 這次建的指令連結與它指到哪（失敗時還指著那裡才刪）
BIN="" NOTES="" HOME_ID=""     # HOME_ID：建家當時家的指紋（home_fp；記進 MARK，重跑收拾前比對）
MARK="$DIR/.install-unfinished" # 還沒裝完的紀錄（被強制停掉時，下次照它收；見開頭）
MARK_READY=0                    # 程式資料夾建好（從原始碼裝：clone 完）才寫，免得擋到 git clone
note() { NOTES="${NOTES}注意：$*
"; }
mark_save() { # 把「這次建了什麼」寫進 MARK（程式資料夾還沒建好就不寫）
  [ "$MARK_READY" = 1 ] && [ -d "$DIR" ] || return 0
  {
    printf 'pid\t%s\n' "$$"
    [ -n "$HOME_ID" ] && printf 'home\t%s\t%s\n' "$HOME_DIR" "$HOME_ID"
    [ -n "$SVC_BIN" ] && printf 'service\t%s\t%s\n' "$SVC_BIN" "$SVC_HOME"
    [ -n "$LINK_CREATED" ] && printf 'link\t%s\t%s\n' "$LINK_CREATED" "$LINK_TARGET"
    printf '%s\n' "$PLACED" | sed '/^$/d; s/^/place\t/'
  } >"$MARK.tmp" 2>/dev/null && mv -f "$MARK.tmp" "$MARK" 2>/dev/null
  return 0
}
first_missing() { # first_missing <路徑>：路徑上第一個還不存在的那層（都存在就印空字串）
  local p="$1" top=""
  while [ ! -e "$p" ] && [ ! -L "$p" ]; do top="$p"; p="$(dirname "$p")"; done
  printf '%s' "$top"
}
is_empty_dir() { [ -d "$1" ] && [ -z "$(ls -A "$1" 2>/dev/null)" ]; }
claim() { # claim <路徑> <first_missing 的結果> [bindir]：在動手建之前叫；記下這次建的或這次填的（見 PLACED）
  if [ -n "$2" ]; then
    PLACED="$1$TAB$([ "${3:-}" = bindir ] && echo bindir || echo created)$TAB$2
$PLACED"
  elif [ "${3:-}" != bindir ] && is_empty_dir "$1"; then
    PLACED="$1${TAB}filled$TAB$1
$PLACED"
  fi
  mark_save
}
rmdir_up() { # rmdir_up <路徑> <最上層>：從路徑往上，空的才刪，到最上層為止
  local d="$1"
  while :; do
    case "$d/" in "$2/"*) ;; *) return 0 ;; esac
    rmdir "$d" 2>/dev/null || return 0
    [ "$d" = "$2" ] && return 0
    d="$(dirname "$d")"
  done
}
undo_place() { # undo_place <路徑> <種類> <最上層>
  case "$1" in "" | / | "$HOME" | [!/]*) return 0 ;; esac
  case "$1/" in "$3/"*) ;; *) return 0 ;; esac # 最上層要是路徑本身或它的上層
  case "$2" in
    created) rm -rf "$1" && rmdir_up "$(dirname "$1")" "$3" ;;
    filled) [ -d "$1" ] && find "$1/" -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null ;;
    bindir) rmdir_up "$1" "$3" ;;
  esac
  return 0
}
svc_ours() { # 要卸的服務還是這個家的：背景行程模式 uninstall 只看那個家自己的 pid 檔；launchd 的服務名稱大家共用，要 plist 寫的是這個家
  [ -n "$SVC_BIN" ] && [ -x "$SVC_BIN" ] || return 1
  [ "${EVERGENIE_SERVICE_MODE:-}" = process ] && return 0
  grep -qF "<string>$SVC_HOME</string>" "${EVERGENIE_LAUNCH_AGENTS_DIR:-$HOME/Library/LaunchAgents}/$SERVICE_LABEL.plist" 2>/dev/null
}
cleanup_failed() { # 回傳 1＝服務停不掉，什麼都沒收（MARK 留著，重跑時再照它收）
  local p kind top pass
  if svc_ours && { fault svc-stop-fail || ! "$SVC_BIN" service uninstall --home "$SVC_HOME" >/dev/null 2>&1; }; then
    KEPT="${KEPT}注意：這次裝的服務停不掉，為了不弄壞它、也不弄丟家裡可能已經記的東西，這次裝的東西（程式資料夾、家、指令連結）都留在原處；之後同一行再跑一次安裝，會再試著停掉、收好（家照樣搬到旁邊留著）。
"
    return 1
  fi
  if [ -n "$LINK_CREATED" ] && [ -L "$LINK_CREATED" ] && [ "$(readlink "$LINK_CREATED")" = "$LINK_TARGET" ]; then
    rm -f "$LINK_CREATED"
  fi
  [ "$SVC_UP" = 1 ] && keep_home # 服務起來過：家與狀態資料夾不刪（搬到旁邊，搬不開就留在原處），從 PLACED 拿掉
  # 程式資料夾最後收：MARK 在裡面，收到一半被停掉，下次還照得到
  for pass in others dir; do
    while IFS="$TAB" read -r p kind top; do
      [ -n "$p" ] || continue
      if [ "$pass" = others ]; then [ "$p" != "$DIR" ] || continue; else [ "$p" = "$DIR" ] || continue; fi
      undo_place "$p" "$kind" "$top"
    done <<EOF
$PLACED
EOF
  done
  # MARK 是這次寫的才收（還沒寫就失敗的，例如結束碼 10～12，那個 MARK 是上一次或另一個正在跑的安裝的）
  [ "$MARK_READY" = 1 ] && rm -f "$MARK" "$MARK.tmp" 2>/dev/null
  SVC_BIN="" SVC_HOME="" SVC_UP=0 LINK_CREATED="" LINK_TARGET="" PLACED=""
}
placed_here() { # placed_here <路徑>：這次建的或填的（失敗時會被收掉）
  local p kind top
  while IFS="$TAB" read -r p kind top; do
    [ "$p" = "$1" ] && [ "$kind" != bindir ] && return 0
  done <<EOF
$PLACED
EOF
  return 1
}
keep_logs() { # 收掉之前：這次建的家／狀態資料夾裡有日誌就複製到 ~/Library/Logs/<產品名>-install-<時間>/（不會被收），印那個路徑
  local d src dest=""
  for d in "$STATE_DIR" "$HOME_DIR"; do
    src="$d/runtime/logs"
    placed_here "$d" && [ -d "$src" ] && [ -n "$(ls -A "$src" 2>/dev/null)" ] || continue
    if [ -z "$dest" ]; then
      dest="$HOME/Library/Logs/$PRODUCT-install-$(date +%Y%m%d-%H%M%S)"
      [ ! -e "$dest" ] || dest="$dest-$$"
      mkdir -p "$dest" 2>/dev/null || return 0
    fi
    cp -R "$src/." "$dest/" 2>/dev/null || true
  done
  [ -n "$dest" ] && [ -n "$(ls -A "$dest" 2>/dev/null)" ] && printf '%s' "$dest"
  return 0
}
stop_untouched() { # stop_untouched <結束碼> <白話原因>：不收拾就結束（那些東西不是這次的，或已經在用）
  trap - INT TERM HUP
  ON_SIG=0
  printf '\n沒裝成功（結束碼 %s）：%s\n' "$1" "$2" >&2
  exit "$1"
}
has_files() { # has_files <資料夾>：裡面（含子資料夾）有任何檔（只有空資料夾＝沒有東西會弄丟）
  [ -d "$1" ] && [ -n "$(find "$1/" -type f -print 2>/dev/null | head -1)" ]
}
# ---- 服務起來過的家：不刪，搬到旁邊 ----------------------------------------------------------------------------
# 服務一開，主人就可能在看板記了東西（套件頁的資料在 vault/pkg/，家的指紋不算它，看不出來），所以收拾時家與狀態資料夾一律不刪：
# 照備份規則（system 卡 BSW-4）搬到原本的旁邊，家與狀態資料夾用同一個尾巴（預設的狀態資料夾＝~/.<產品名>/state/<家的資料夾名>，
# 搬過去的家照樣找得到自己的）。ASIDE_HOME、ASIDE_STATE：要搬去的位置（空的＝那一個不用搬：不是這次建的或裡面沒有檔）。
ASIDE_HOME="" ASIDE_STATE=""
aside_need() { placed_here "$1" && has_files "$1"; }
free_name() { [ ! -e "$1" ] && [ ! -L "$1" ]; }
unplace() { # unplace <路徑>：從 PLACED 拿掉（不收它）
  PLACED="$(printf '%s\n' "$PLACED" | P="$1" LC_ALL=C awk -F "$TAB" '$1 != ENVIRON["P"]')
"
}
aside_plan() { # 決定搬去的名字；名字都被用掉、上層寫不進去就失敗（什麼都還沒動）
  local d t n=0 sfx h s
  d="$(date +%Y%m%d)" t="$(date +%H%M)"
  ASIDE_HOME="" ASIDE_STATE=""
  while [ "$n" -le 99 ]; do
    case "$n" in # 同一天第二份照備份規則加時分，再有就加序號；不蓋掉已經有的
      0) sfx=".bak-$d-install-unfinished" ;;
      1) sfx=".bak-$d-$t-install-unfinished" ;;
      *) sfx=".bak-$d-$t-install-unfinished-$n" ;;
    esac
    n=$((n + 1))
    h="" s=""
    aside_need "$HOME_DIR" && h="$HOME_DIR$sfx"
    aside_need "$STATE_DIR" && s="$STATE_DIR$sfx"
    [ -z "$h" ] || free_name "$h" || continue
    [ -z "$s" ] || free_name "$s" || continue
    [ -z "$h" ] || [ -w "$(dirname "$HOME_DIR")" ] || return 1
    [ -z "$s" ] || [ -w "$(dirname "$STATE_DIR")" ] || return 1
    ASIDE_HOME="$h" ASIDE_STATE="$s"
    return 0
  done
  return 1
}
aside_move() { # 照 aside_plan 搬（同一個資料夾裡改名）。搬的時候不理 Ctrl-C（不會停在兩個 mv 中間）。
  # 回傳 0＝搬好；1＝沒搬（家搬過去了、狀態資料夾搬不過去，就把家搬回來）；2＝家搬過去了、狀態資料夾搬不過去、家也搬不回來
  # （ASIDE_STATE 清空：狀態資料夾留在原處）
  local rc=0
  ! fault aside-fail || return 1
  trap '' INT TERM HUP
  if [ -n "$ASIDE_HOME" ] && ! mv "$HOME_DIR" "$ASIDE_HOME" 2>/dev/null; then
    rc=1
  elif [ -n "$ASIDE_STATE" ] && ! mv "$STATE_DIR" "$ASIDE_STATE" 2>/dev/null; then
    if [ -z "$ASIDE_HOME" ] || mv "$ASIDE_HOME" "$HOME_DIR" 2>/dev/null; then rc=1; else ASIDE_STATE="" rc=2; fi
  fi
  if [ "$ON_SIG" = 1 ]; then trap on_signal INT TERM HUP; else trap - INT TERM HUP; fi
  return "$rc"
}
kept_msg() { # kept_msg <開頭>：搬好之後給主人的話（KEPT；失敗、被中斷時也印，裝好時跟其他「注意：」一起印）
  local where back
  if [ -n "$ASIDE_HOME" ]; then
    where="$ASIDE_HOME" back="$ASIDE_HOME 改回 $HOME_DIR"
    [ -z "$ASIDE_STATE" ] || { where="${where}（狀態資料夾搬到 ${ASIDE_STATE}）" back="${back}、$ASIDE_STATE 改回 $STATE_DIR"; }
  else
    where="${ASIDE_STATE}（只有狀態資料夾）" back="$ASIDE_STATE 改回 $STATE_DIR"
  fi
  KEPT="${KEPT}注意：${1}開過服務，可能已經記了東西，所以沒有刪，整個搬到 ${where}。要拿回來：${back}（做法見 INSTALL.md「搬到旁邊的家」）；名字裡的日期是搬過去的那天，確定不要了再刪（最多留 30 天）。
"
}
keep_home() { # 這次失敗、被中斷時（服務已經停掉），服務起來過的家與狀態資料夾不刪：搬到旁邊，搬不開就留在原處
  local h=0 s=0 rc=1
  aside_need "$HOME_DIR" && h=1
  aside_need "$STATE_DIR" && s=1
  [ "$h$s" != 00 ] || return 0
  if aside_plan; then
    aside_move
    rc=$?
  fi
  if [ "$rc" = 1 ]; then
    KEPT="${KEPT}注意：$HOME_DIR 這個家開過服務，可能已經記了東西，所以沒有刪；也沒辦法搬到旁邊（上層寫不進去或名字都被用掉了），留在原處（狀態資料夾 $STATE_DIR 也是）。
"
  else
    kept_msg "$HOME_DIR 這個家"
    [ "$rc" = 0 ] || KEPT="${KEPT}注意：狀態資料夾 $STATE_DIR 搬不過去，留在原處、沒有刪。
"
  fi
  [ "$h" = 0 ] || unplace "$HOME_DIR"
  [ "$s" = 0 ] || unplace "$STATE_DIR"
  mark_save
}
home_fp() { # home_fp <家>：家的指紋＝每個檔的路徑、大小、修改時間；家不在就空字串
  # 主人用過（看板改了設定、秘書寫了紀錄、放了自己的角色）就會多出檔或改到檔，指紋就不一樣。
  # 服務一開就會自己寫的不算：settings/migrations.toml、寫到一半的 .atomic-* 暫存檔、套件的起始資料 vault/pkg/、
  # 核心產生的索引（vault/notes/索引.md、vault/agents/<職位>/索引.md，DEV-161；內容都是從別的檔算出來的）、
  # 核心啟動時補的主題筆記分類說明檔（vault/notes/<分類>/<分類>.md，DEV-161；主人只改了它也認不出來，同上靠 keep_home）、
  # 核心啟動時自動放的每週備份掃描排程（vault/jobs/backup-sweep.md，DEV-163；同上靠 keep_home）
  # （所以主人只在套件裡記了東西、別的都沒動，認不出來：服務起來過的家因此一律不刪，見 keep_home）。
  # 服務開起來多寫了別處，install-test 的 3q、3s 會抓到，要加進這裡。
  [ -d "$1" ] || return 0
  (cd "$1" && find . -type f ! -path ./settings/migrations.toml ! -name '.atomic-*' ! -path './vault/pkg/*' ! -path './vault/notes/索引.md' ! -path './vault/agents/*/索引.md' ! -path ./vault/jobs/backup-sweep.md -exec stat -f '%N %z %m' {} + 2>/dev/null) |
    LC_ALL=C awk -F/ '!($2 == "vault" && $3 == "notes" && NF == 5 && index($5, $4 ".md ") == 1)' | LC_ALL=C sort | shasum -a 256 | awk '{print $1}'
}
recover_unfinished() { # 上次被強制停掉、留下 MARK：照它收掉上次建的（只收這次也要用的那幾個位置；順序照檔案，新的在前）
  local kind a b c pid last_home="" last_id="" logs stopped=0 rc
  SVC_BIN="" SVC_HOME="" SVC_UP=0 LINK_CREATED="" LINK_TARGET="" PLACED=""
  # 上次其實裝好了、只是刪不掉 MARK（改寫成 done 或清空了）：不收，照一般的檢查走（家或程式資料夾有東西＝裝過了）
  if [ ! -s "$MARK" ] || grep -qx done "$MARK" 2>/dev/null; then
    say "注意：上次的安裝其實已經裝好了（$MARK 沒能刪掉），不會收它。"
    rm -f "$MARK" 2>/dev/null
    return 0
  fi
  say "上次的安裝沒有跑完（被強制停掉了），先收掉上次建的東西再重裝。"
  while IFS="$TAB" read -r kind a b c; do
    case "$kind" in
      pid) pid="$a" ;;
      home) last_home="$a" last_id="$b" ;; # 上次建的家與建家當時家的指紋
      service) SVC_UP=1; case "$a" in "$DIR"/*) SVC_BIN="$a" SVC_HOME="$b" ;; esac ;;
      link) [ "$a" = "$HOME/.local/bin/$PRODUCT" ] && LINK_CREATED="$a" LINK_TARGET="$b" ;;
      place)
        case "$a" in
          "$DIR" | "$HOME_DIR" | "$STATE_DIR" | "$HOME/.local/bin")
            PLACED="$PLACED$a$TAB$b$TAB$c
" ;;
          *) say "注意：上次沒裝完時建的 $a 跟這次的位置不一樣，沒有動它；不要的話自己刪掉。" ;;
        esac
        ;;
    esac
  done <"$MARK"
  # 上次那個還在跑（例如在背景重試、前一次還沒結束）：不能收它正在裝的東西
  if [ -n "${pid:-}" ] && [ "$pid" != "$$" ] && kill -0 "$pid" 2>/dev/null &&
    ps -o command= -p "$pid" 2>/dev/null | grep -q install; then
    stop_untouched 14 "另一個安裝正在跑（pid ${pid}），沒有動它。等它跑完再看結果，不要同時跑兩次。" # 不收拾：那些是另一個安裝的
  fi
  # 上次建的家要還是剛建好的樣子才收：家的指紋（每個檔的路徑、大小、修改時間）跟建家當時記下的一樣。不一樣（多了紀錄、
  # 改過設定＝已經被用過），或建了家卻還沒記下指紋就被停掉（認不出來）：什麼都不動（服務、連結、程式也不動，那是那個家在用的）。
  if [ -n "$last_home" ] && has_files "$last_home" && [ "$(home_fp "$last_home")" != "$last_id" ]; then
    stop_untouched 14 "上次沒裝完時建的家 $last_home 已經不是剛建好的樣子（可能已經在用了），為了不弄丟裡面的東西，什麼都沒有動。這個家要繼續用就不用再裝；確定不要了，請主人自己把它移走再重裝。"
  fi
  if [ -z "$last_home" ] && placed_here "$HOME_DIR" && has_files "$HOME_DIR"; then
    stop_untouched 14 "上次沒裝完時建的家 $HOME_DIR 認不出是不是剛建好的（上次在建家的時候被停掉），為了不弄丟裡面的東西，什麼都沒有動。確定不要了，請主人自己把它移走再重裝。"
  fi
  # 指紋一樣、但服務起來過：主人可能只在套件頁記了東西（指紋看不出來），不刪，停掉服務後整個搬到旁邊（見開頭）。
  # 搬不開就結束碼 14、什麼都不動（停掉的服務重開）。服務從沒起來過的家照上面的指紋規則收掉。
  if [ "$SVC_UP" = 1 ] && { aside_need "$HOME_DIR" || aside_need "$STATE_DIR"; }; then
    local cant="上次沒裝完的家 $HOME_DIR 開過服務，可能已經記了東西，要整個搬到旁邊保留"
    aside_plan || stop_untouched 14 "${cant}，但搬不開（$(dirname "$HOME_DIR") 或狀態資料夾的上層寫不進去，或備份的名字都被用掉了），為了不弄丟裡面的東西，什麼都沒有動。這個家要繼續用就不用再裝；確定不要了，請主人自己把它移走再重裝。"
    if svc_ours; then
      { ! fault svc-stop-fail && "$SVC_BIN" service uninstall --home "$SVC_HOME" >/dev/null 2>&1; } ||
        stop_untouched 14 "${cant}，但停不掉它的服務，為了不弄丟裡面的東西，什麼都沒有動。這個家要繼續用就不用再裝。"
      stopped=1
    fi
    aside_move
    rc=$?
    if [ "$rc" = 1 ]; then
      local svc="服務沒有動"
      if [ "$stopped" = 1 ]; then
        if "$SVC_BIN" service install --home "$SVC_HOME" --exe "$SVC_BIN" >/dev/null 2>&1; then svc="服務照原樣開著"; else svc="服務停掉了、沒能重開，這個家要繼續用就請秘書重開它的服務"; fi
      fi
      stop_untouched 14 "${cant}，但搬不開（${ASIDE_HOME:-$ASIDE_STATE}），為了不弄丟裡面的東西，什麼都沒有動（${svc}）。這個家要繼續用就不用再裝；確定不要了，請主人自己把它移走再重裝。"
    fi
    kept_msg "上次沒裝完的家 $HOME_DIR "
    # 家搬過去了、狀態資料夾搬不過去（家也搬不回來）：其他都不動，下次重跑再搬狀態資料夾
    [ "$rc" = 0 ] || stop_untouched 14 "${KEPT}但狀態資料夾 $STATE_DIR 搬不過去，為了不弄丟東西，其他都沒有動（服務停了）；同一行再跑一次會再試。"
    [ -z "$ASIDE_HOME" ] || unplace "$HOME_DIR"
    [ -z "$ASIDE_STATE" ] || unplace "$STATE_DIR"
    SVC_BIN="" SVC_HOME="" # 服務已經停掉了（下面收拾時不用再停）
    printf '%s' "$KEPT"
  fi
  logs="$(keep_logs)"
  [ -z "$logs" ] || say "注意：上次留下的日誌複製到 ${logs}/。"
  cleanup_failed || stop_untouched 14 "上次沒裝完時裝的服務停不掉，為了不弄壞它、也不弄丟家裡可能已經記的東西，什麼都沒有動。這個家要繼續用就不用再裝；不要了，跟任何一位秘書說「幫我停掉這個家的服務」再重裝。"
  rm -f "$MARK" "$MARK.tmp" 2>/dev/null
}
on_exit() { rm -rf "$TMP"; }
on_signal() {
  local logs what="這次建的東西已經收掉"
  trap - INT TERM HUP
  ON_SIG=0
  logs="$(keep_logs)"
  cleanup_failed
  [ -z "$KEPT" ] || what="這次建的東西收掉了（下面「注意：」說的除外）"
  printf '\n沒裝成功（結束碼 1）：被中斷了。%s，可以重跑。%s\n' "$what" "${logs:+日誌留在 ${logs}/。}" >&2
  printf '%s' "$KEPT" >&2
  exit 1
}
trap on_exit EXIT
trap on_signal INT TERM HUP
ON_SIG=1 # 現在 INT／TERM／HUP 交給 on_signal（die、on_signal、stop_untouched 改回預設時設 0；aside_move 搬完照它還原）
die() { # die <結束碼> <白話原因>：收掉這次建的；收之前把日誌留到 ~/Library/Logs/（keep_logs），訊息最後寫在哪裡
  local code="$1" logs
  shift
  trap - INT TERM HUP
  ON_SIG=0
  logs="$(keep_logs)"
  printf '\n沒裝成功（結束碼 %s）：%s%s\n' "$code" "$*" "${logs:+日誌留在 ${logs}/。}" >&2
  cleanup_failed
  printf '%s' "$KEPT" >&2
  exit "$code"
}

limited() { # limited <秒> <指令…>：限時跑，輸出（stdout＋stderr）印出來；超時結束碼 124
  local secs="$1" out="$TMP/limited.out" pid i=0 rc
  shift
  "$@" >"$out" 2>&1 </dev/null &
  pid=$!
  while kill -0 "$pid" 2>/dev/null; do
    if [ "$i" -ge $((secs * 10)) ]; then
      kill "$pid" 2>/dev/null
      wait "$pid" 2>/dev/null
      cat "$out"
      return 124
    fi
    sleep 0.1
    i=$((i + 1))
  done
  wait "$pid"
  rc=$?
  cat "$out"
  return "$rc"
}

# ---- 1. 這台電腦 -------------------------------------------------------------------------------------------
[ "$(uname -s)" = Darwin ] || die 10 "這支安裝程式只支援 Mac（macOS 11 以上）。"
OS_VER="$(sw_vers -productVersion 2>/dev/null)"
OS_MAJOR="${OS_VER%%.*}"
case "$OS_MAJOR" in "" | *[!0-9]*) OS_MAJOR=0 ;; esac
[ "$OS_MAJOR" -ge 11 ] || die 10 "這台 Mac 的系統是 ${OS_VER:-看不出來的版本}，要 macOS 11 以上才裝得起來。"
if [ "$(sysctl -n hw.optional.arm64 2>/dev/null)" = 1 ]; then
  ARCH=arm64 # Apple 晶片；就算這個終端機是用 Rosetta 開的也裝 arm64 版
else
  case "$(uname -m)" in
    x86_64) ARCH=amd64 ;;
    arm64) ARCH=arm64 ;;
    *) die 10 "認不得這台 Mac 的晶片（$(uname -m)）。" ;;
  esac
fi

# ---- 2. Claude 的指令工具（秘書靠它工作；不幫忙裝） ------------------------------------------------------------
if [ "$CLAUDE_CHECK" = 1 ]; then
  CLAUDE_BIN=""
  # 服務找 claude 的地方（evergenie service 的 PATH）先看；都沒有才看現在的 PATH
  for d in "$HOME/.local/bin" /opt/homebrew/bin /usr/local/bin /usr/bin; do
    if [ -x "$d/claude" ] && [ ! -d "$d/claude" ]; then CLAUDE_BIN="$d/claude"; break; fi
  done
  if [ -z "$CLAUDE_BIN" ]; then
    CLAUDE_BIN="$(command -v claude 2>/dev/null || true)"
    case "$CLAUDE_BIN" in /*) note "Claude 的指令工具裝在 ${CLAUDE_BIN}，秘書的服務只會到 ~/.local/bin、/opt/homebrew/bin、/usr/local/bin 找，可能找不到。用 Claude Code 官方的安裝方式重裝一次（會放在 ~/.local/bin）就好。" ;; *) CLAUDE_BIN="" ;; esac
  fi
  [ -n "$CLAUDE_BIN" ] || die 11 "這台電腦找不到 Claude 的指令工具（claude）。請先照 Claude Code 官方說明裝好並登入，再重跑一次。"
  AUTH="$(limited 30 "$CLAUDE_BIN" auth status)"
  if grep -Eq '"loggedIn"[[:space:]]*:[[:space:]]*true' <<<"$AUTH"; then
    :
  elif grep -Eq '"loggedIn"[[:space:]]*:[[:space:]]*false' <<<"$AUTH"; then
    die 12 "Claude 還沒登入。請先在終端機打 claude、照畫面登入一次，再重跑一次。"
  else
    note "看不出 Claude 有沒有登入（這版的 claude 可能太舊，沒有 claude auth status）；先繼續裝，裝完看板會再檢查一次。"
  fi
fi

# ---- 3. 要放東西的地方都要是空的 ---------------------------------------------------------------------------------
if [ -f "$MARK" ]; then
  recover_unfinished
fi
if [ -e "$HOME_DIR" ] && ! is_empty_dir "$HOME_DIR"; then
  die 13 "你的資料資料夾 $HOME_DIR 已經有東西了，不會動它。要換位置請加 --home <資料夾>；已經裝過要升級請用 $PRODUCT upgrade。"
fi
if [ -e "$STATE_DIR" ] && ! is_empty_dir "$STATE_DIR"; then
  die 13 "狀態資料夾 $STATE_DIR 已經有東西了（之前裝過同名的家？），不會動它。換一個家的位置（--home），或用 --state <資料夾> 指定別的狀態資料夾。"
fi
if [ "$MODE" = download ] && [ -e "$DIR" ] && ! is_empty_dir "$DIR"; then
  if [ -L "$DIR/current" ] || [ -d "$DIR/versions" ]; then
    die 14 "這台已經裝過了（程式在 ${DIR}）。要升級請用 $PRODUCT upgrade；要另外再裝一份，加 --dir <別的資料夾>。"
  fi
  die 14 "$DIR 已經有別的東西了，不會動它。要把程式裝在別的地方請加 --dir <資料夾>。"
fi
# 一台只能有一個家用這個服務名稱：已經有了（別的家，或以前裝過的），裝服務會把它換掉、失敗時還會把它卸掉，所以不裝。
# 測試用的 EVERGENIE_LAUNCH_AGENTS_DIR（跟程式同一個變數）有給就只看那裡；背景行程模式（EVERGENIE_SERVICE_MODE=process，測試用）不碰 launchd、不會撞名。
service_taken() {
  if [ -n "${EVERGENIE_LAUNCH_AGENTS_DIR:-}" ]; then
    [ -e "$EVERGENIE_LAUNCH_AGENTS_DIR/$SERVICE_LABEL.plist" ]
    return
  fi
  [ "${EVERGENIE_SERVICE_MODE:-}" != process ] || return 1
  [ -e "$HOME/Library/LaunchAgents/$SERVICE_LABEL.plist" ] || launchctl print "gui/$(id -u)/$SERVICE_LABEL" >/dev/null 2>&1
}
if [ "$SERVICE" = 1 ] && service_taken; then
  die 14 "這台已經有一個 $PRODUCT 的家裝成服務了（${SERVICE_LABEL}），不會動它。要升級請用 $PRODUCT upgrade；一台只裝一個家。"
fi

# ---- 4a. 下載版：清單 → 下載 → 檢查碼 → 解開 → current ---------------------------------------------------------------
json_asset() { # json_asset <清單檔> <darwin-晶片>：印「版本<TAB>網址<TAB>sha256<TAB>大小」；看不懂 ERR、沒有這個晶片 NOASSET
  osascript -l JavaScript - "$1" "$2" <<'JS' 2>/dev/null
function run(argv) {
  var s = $.NSString.stringWithContentsOfFileEncodingError(argv[0], $.NSUTF8StringEncoding, null);
  if (!s || s.isNil()) return "ERR";
  var m;
  try { m = JSON.parse(ObjC.unwrap(s)); } catch (e) { return "ERR"; }
  if (!m || typeof m !== "object" || m.schema !== 1 || typeof m.version !== "string" || !m.assets || typeof m.assets !== "object") return "ERR";
  var a = m.assets[argv[1]];
  if (!a) return "NOASSET";
  return [m.version, String(a.url || ""), String(a.sha256 || ""), String(a.size || "")].join("\t");
}
JS
}
resolve_url() { # resolve_url <清單網址> <包的網址>：包的網址可以是相對於清單的路徑
  local base="${1%%\?*}" scheme rest host
  case "$2" in *://*) printf '%s' "$2"; return ;; esac
  case "$2" in
    /*)
      scheme="${base%%://*}" rest="${base#*://}"
      host="${rest%%/*}"
      printf '%s://%s%s' "$scheme" "$host" "$2" ;;
    *) printf '%s/%s' "${base%/*}" "$2" ;;
  esac
}
# curl：-q 不讀主人的 ~/.curlrc（裡面可能關掉了憑證檢查）；清單是 https 的，清單與包都只走 https，清單才不會被換掉。
# 轉址一律只收 https（PLT-25：https 不能被轉成 http；本機的 http 也不跟到別處）。
case "$SOURCE" in
  https://*) CURL_PROTO=(--proto '=https' --proto-redir '=https') ;;
  *) CURL_PROTO=(--proto '=https,http,file' --proto-redir '=https') ;; # 測試用的本機 http、file 來源
esac

install_download() {
  case "$SOURCE" in
    *.invalid/* | *.invalid) die 20 "還沒有公開的下載點（正式版還沒放上網路）。要裝測試版請給 --source <版本清單網址>。" ;;
  esac
  say "== 1/6 讀版本清單（${SOURCE}）"
  curl -q -fsSL "${CURL_PROTO[@]}" --connect-timeout 20 --max-time 60 --max-filesize 1048576 -o "$TMP/manifest.json" "$SOURCE" 2>"$TMP/curl.err" ||
    die 20 "讀不到版本清單（${SOURCE}）：網路不通，或網址不對。"
  local info ver url sha size
  info="$(json_asset "$TMP/manifest.json" "darwin-$ARCH")"
  case "$info" in
    "" | ERR) die 20 "版本清單看不懂（$SOURCE 拿到的不是正確的版本清單）。" ;;
    NOASSET) die 21 "版本清單裡沒有這台 Mac（darwin-${ARCH}）能用的包。" ;;
  esac
  IFS="$(printf '\t')" read -r ver url sha size <<EOF
$info
EOF
  sha="$(printf '%s' "$sha" | tr 'A-F' 'a-f')"
  case "$ver" in "" | .* | *[!0-9A-Za-z._+-]*) die 20 "版本清單看不懂（版本「${ver}」不能當資料夾名稱）。" ;; esac
  case "$sha" in *[!0-9a-f]*) sha="" ;; esac
  case "$size" in "" | *[!0-9]*) size="" ;; esac
  if [ -z "$url" ] || [ "${#sha}" != 64 ] || [ -z "$size" ] || [ "$size" -le 0 ] || [ "$size" -gt 1073741824 ]; then
    die 20 "版本清單看不懂（darwin-$ARCH 那包的網址、檢查碼或大小不對）。"
  fi
  url="$(resolve_url "$SOURCE" "$url")"
  valid_source "$url" || die 20 "版本清單看不懂（裡面的下載網址不能用：${url}）。"
  case "$SOURCE" in https://*) case "$url" in https://*) ;; *) die 20 "版本清單看不懂（清單是 https 的，但裡面的下載網址不是 https：${url}）。" ;; esac ;; esac
  VERSION="$ver"

  say "== 2/6 下載 $PRODUCT ${ver}（darwin-${ARCH}）"
  local pkg="$TMP/pkg.tar.gz" got
  curl -q -fsSL "${CURL_PROTO[@]}" --connect-timeout 20 --max-time "${EVERGENIE_INSTALL_DOWNLOAD_TIMEOUT:-420}" --speed-limit 20000 --speed-time 60 \
    --max-filesize "$size" -o "$pkg" "$url" 2>"$TMP/curl.err" ||
    die 22 "下載失敗（${url}）：網路不通，或下載到一半斷掉了。過一下再重跑一次。"
  got="$(wc -c <"$pkg" | tr -d ' ')"
  [ "$got" -ge "$size" ] || die 22 "下載到一半斷掉了（拿到 ${got}／$size bytes）。過一下再重跑一次。"
  got="$(shasum -a 256 "$pkg" | awk '{print $1}')"
  [ "$got" = "$sha" ] || die 23 "下載的檔案跟版本清單的檢查碼對不上（檔案壞了或被換過），沒有裝。過一下再重跑一次；一直這樣請找提供程式的人。"

  say "== 3/6 解開到 $DIR/versions/$ver"
  # 包裡只能有一個最上層資料夾，裡面只有資料夾與一般檔（不收符號連結、硬連結、絕對路徑、..）
  local list top="" n first
  list="$(tar -tzf "$pkg" 2>/dev/null)" || die 24 "下載的包解不開。"
  while IFS= read -r n; do
    n="${n#./}"
    [ -n "$n" ] || continue
    first="${n%%/*}"
    case "$first" in "" | . | ..) die 24 "下載的包內容不對（有不該有的路徑）。" ;; esac
    [ -n "$top" ] || top="$first"
    [ "$first" = "$top" ] || die 24 "下載的包內容不對（不只一個最上層資料夾）。"
    case "/$n/" in */../*) die 24 "下載的包內容不對（有不該有的路徑）。" ;; esac
  done <<EOF
$list
EOF
  [ -n "$top" ] || die 24 "下載的包是空的。"
  local kinds
  kinds="$(tar -tvzf "$pkg" 2>/dev/null | cut -c1 | sort -u | tr -d '\n')"
  case "$kinds" in "" | *[!d-]*) die 24 "下載的包內容不對（有符號連結或其他特殊檔案）。" ;; esac
  local missing
  missing="$(first_missing "$DIR")"
  claim "$DIR" "$missing"
  mkdir -p "$DIR/versions" || die 25 "建不了資料夾 ${DIR}（沒有權限？）。"
  MARK_READY=1
  mark_save
  local unpack="$DIR/versions/.unpack-$$" dest="$DIR/versions/$ver"
  mkdir "$unpack" || die 25 "建不了資料夾 ${unpack}。"
  tar -xzf "$pkg" -C "$unpack" --strip-components 1 2>"$TMP/tar.err" || die 25 "解開失敗（硬碟空間夠嗎？）。"
  [ "$(tr -d ' \n\r' <"$unpack/VERSION" 2>/dev/null)" = "$ver" ] || die 24 "下載的包內容不對（裡面的 VERSION 不是 ${ver}）。"
  [ -f "$unpack/bin/$PRODUCT" ] && [ -x "$unpack/bin/$PRODUCT" ] || die 24 "下載的包內容不對（沒有 bin/${PRODUCT}）。"
  mv "$unpack" "$dest" || die 25 "搬不了 $unpack → ${dest}。"
  ln -s "versions/$ver" "$DIR/current" || die 25 "建不了 $DIR/current。"
  BIN="$DIR/current/bin/$PRODUCT"
  got="$(limited 30 "$BIN" version | tr -d '\r' | tail -1)"
  [ "$got" = "$ver" ] || die 24 "這台 Mac 跑不動下載的程式（$BIN version 印的是「${got:-什麼都沒有}」）。"
  # 包裡 fixed/ 的檔（附帶的 tmux，檔名是產品名，與它的啟動程式；engine-claude ECL-65、system 卡 UPD-15）放到程式資料夾的 fixed/：
  # 不在帶版本號的資料夾裡，之後升級時內容沒變就不換（路徑與內容都不變，主人在系統權限視窗按過的允許一直算數）。
  # 先寫暫存檔再 mv，跟升級時 evergenie 做的一樣。
  if [ -d "$dest/fixed" ]; then
    local f name
    mkdir -p "$DIR/fixed" || die 25 "建不了資料夾 $DIR/fixed。"
    for f in "$dest/fixed"/*; do
      [ -f "$f" ] || continue
      name="$(basename "$f")"
      cp -p "$f" "$DIR/fixed/.$name.new-$$" && mv -f "$DIR/fixed/.$name.new-$$" "$DIR/fixed/$name" ||
        die 25 "放不進 $DIR/fixed/${name}（硬碟空間夠嗎？）。"
    done
    if [ -f "$DIR/fixed/$PRODUCT" ]; then
      got="$(limited 10 "$DIR/fixed/$PRODUCT" -V | tr -d '\r' | tail -1)"
      case "$got" in "tmux "*) ;; *) die 24 "這台 Mac 跑不動跟著程式帶的 tmux（$DIR/fixed/$PRODUCT -V 印的是「${got:-什麼都沒有}」）。" ;; esac
    fi
  fi
}

# ---- 4b. 從原始碼裝（開發用）：抓程式 → 切版本 → 編譯 ---------------------------------------------------------------
install_source() {
  command -v git >/dev/null 2>&1 || die 40 "這台電腦沒有 git。從原始碼裝要先裝好 git（macOS：在終端機輸入 git --version，照畫面指示安裝）；一般安裝不用加 --from-source。"
  command -v go >/dev/null 2>&1 || die 40 "要先裝 Go（從原始碼裝要在這台電腦上編譯）：到 https://go.dev/dl/ 下載安裝。一般安裝不用加 --from-source。"
  command -v make >/dev/null 2>&1 || die 40 "這台電腦沒有 make。macOS 請在終端機輸入 xcode-select --install。"
  # 從原始碼編的沒有附帶的 tmux（那是發行檔才有的），秘書的對話要用 PATH 上的 tmux。
  command -v tmux >/dev/null 2>&1 || die 40 "從原始碼裝要先裝 tmux（秘書的對話跑在 tmux 裡）：用 Homebrew：brew install tmux。一般安裝不用加 --from-source（會一起帶）。"
  say "== 1/6 抓程式（${REPO}）"
  if [ -d "$DIR/.git" ]; then
    local have
    have="$(git -C "$DIR" remote get-url origin 2>/dev/null || true)"
    [ "$have" = "$REPO" ] || die 14 "$DIR 已經有一份程式，但來源不是 ${REPO}（是 ${have:-不明}）。要裝在別的地方請加 --dir <資料夾>。"
    MARK_READY=1
    git -C "$DIR" fetch --tags --quiet origin || die 41 "更新程式失敗（連不上 ${REPO}？）"
  elif [ -e "$DIR" ] && ! is_empty_dir "$DIR"; then
    die 14 "$DIR 已經有東西了，而且不是 $PRODUCT 的程式。要裝在別的地方請加 --dir <資料夾>。"
  else
    local missing
    missing="$(first_missing "$DIR")"
    mkdir -p "$(dirname "$DIR")" || die 25 "建不了資料夾 $(dirname "$DIR")"
    claim "$DIR" "$missing"
    git clone --quiet "$REPO" "$DIR" || die 41 "抓不到程式（git clone $REPO 失敗：網址對嗎？有權限嗎？）"
    MARK_READY=1
    mark_save
    git -C "$DIR" fetch --tags --quiet origin 2>/dev/null || true
  fi
  local target=""
  case "$REF" in
    latest)
      target="$(git -C "$DIR" tag --list 'v[0-9]*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1)"
      if [ -z "$target" ]; then
        say "還沒有正式版，裝的是開發中的版本（main）。"
        target="origin/main"
      fi
      ;;
    main) target="origin/main" ;;
    *) target="$REF" ;;
  esac
  git -C "$DIR" rev-parse --verify --quiet "$target^{commit}" >/dev/null || die 41 "找不到版本 ${target}（--ref 要寫 vX.Y.Z、latest 或 main）。"
  say "== 2/6 切到 $target"
  git -C "$DIR" checkout --quiet --detach "$target" || die 41 "切不到 ${target}（$DIR 裡有改動嗎？）"
  say "== 3/6 編譯（要幾分鐘）"
  (cd "$DIR" && make build) >"$TMP/build.log" 2>&1 || {
    tail -5 "$TMP/build.log" >&2
    die 41 "編譯失敗。"
  }
  BIN="$DIR/bin/$PRODUCT"
  [ -x "$BIN" ] || die 41 "編譯完沒有找到 ${BIN}。"
  VERSION="$("$BIN" version 2>/dev/null || true)"
}

VERSION=""
if [ "$MODE" = download ]; then install_download; else install_source; fi

# ---- 5. 建家 ------------------------------------------------------------------------------------------------
say "== 4/6 建立你的資料夾（${HOME_DIR}）"
init_args=(--home "$HOME_DIR")
[ -n "$NAME" ] && init_args+=(--name "$NAME")
[ -n "$PORT" ] && init_args+=(--port "$PORT")
[ -n "$TZ_NAME" ] && init_args+=(--timezone "$TZ_NAME")
[ "$STATE_GIVEN" = 1 ] && init_args+=(--state "$STATE_DIR")
[ -n "$BACKUP_FILE" ] && init_args+=(--from-backup "$BACKUP_FILE")
HOME_MISSING="$(first_missing "$HOME_DIR")" STATE_MISSING="$(first_missing "$STATE_DIR")"
claim "$HOME_DIR" "$HOME_MISSING"
claim "$STATE_DIR" "$STATE_MISSING"
if ! init_out="$("$BIN" init "${init_args[@]}" 2>&1)"; then
  printf '%s\n' "$init_out" >&2
  die 30 "建立你的資料夾失敗（上面是 $PRODUCT init 的訊息）。"
fi
HOME_ID="$(home_fp "$HOME_DIR")"
mark_save
init_note="$(printf '%s\n' "$init_out" | sed -n 's/^注意：//p')"
while IFS= read -r init_line; do # 一行一則（從備份建家會有好幾行「還要你做的事」）
  [ -z "$init_line" ] || note "$init_line"
done <<INIT_NOTES
$init_note
INIT_NOTES
BOARD_PORT="$(awk '/^\[web\]/ { w = 1; next } /^\[/ { w = 0 } w && /^port[ \t]*=/ { gsub(/[^0-9]/, ""); print; exit }' "$HOME_DIR/home.toml" 2>/dev/null)"
[ -n "$BOARD_PORT" ] || die 30 "讀不到看板的 port（$HOME_DIR/home.toml）。"
BOARD="http://127.0.0.1:$BOARD_PORT/"
if [ "$MODE" = download ]; then
  # 指標檔：這份程式屬於哪個家（evergenie 指令沒給 --home 時用它；PLT-21）。放在程式資料夾，升級換版也還在。
  esc="$(printf '%s' "$HOME_DIR" | sed 's/\\/\\\\/g; s/"/\\"/g')"
  printf '# %s 安裝程式寫的：這份程式屬於哪個家（%s 指令沒給 --home 時用它）\nhome = "%s"\n' "$PRODUCT" "$PRODUCT" "$esc" >"$DIR/$PRODUCT.local.toml" ||
    die 25 "寫不了 $DIR/$PRODUCT.local.toml。"
fi

# ---- 6. 裝成服務、指令連結、等看板 -------------------------------------------------------------------------------
if [ "$SERVICE" = 1 ]; then
  say "== 5/6 裝成服務並啟動"
  SVC_BIN="$BIN" SVC_HOME="$HOME_DIR" SVC_UP=1 # 先記：裝到一半失敗（寫了設定、沒載入）也要收（卸載時本來就沒有也沒關係）；家從這裡起不刪
  mark_save
  if ! svc_out="$("$BIN" service install --home "$HOME_DIR" --exe "$BIN" 2>&1)"; then
    printf '%s\n' "$svc_out" >&2
    die 31 "裝成服務失敗（上面是 $PRODUCT service install 的訊息）。"
  fi
  ! fault fail-service || die 31 "裝成服務失敗（測試：故意的）。"
  ! fault kill-after-service || kill -KILL $$
else
  say "== 5/6 略過服務（--no-service）"
fi
LINK_NOTE=""
if [ "$MODE" = download ]; then
  LINK="$HOME/.local/bin/$PRODUCT"
  if [ -L "$LINK" ] || [ -e "$LINK" ]; then
    if [ -L "$LINK" ] && [ "$(readlink "$LINK")" = "$BIN" ]; then
      LINK_NOTE="$LINK"
    else
      note "$LINK 本來就有別的東西，沒有動它；要用這次裝的指令請打 ${BIN}。"
    fi
  else
    missing="$(first_missing "$HOME/.local/bin")"
    claim "$HOME/.local/bin" "$missing" bindir
    mkdir -p "$HOME/.local/bin" || die 25 "建不了資料夾 $HOME/.local/bin。"
    LINK_CREATED="$LINK" LINK_TARGET="$BIN"
    mark_save
    ln -s "$BIN" "$LINK" || { LINK_CREATED="" LINK_TARGET=""; die 25 "建不了指令連結 ${LINK}。"; }
    ! fault kill-after-link || kill -KILL $$
    LINK_NOTE="$LINK"
  fi
  if [ -n "$LINK_NOTE" ]; then
    case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) note "~/.local/bin 不在 PATH 裡：在終端機要打完整路徑 ${LINK_NOTE}（或把 ~/.local/bin 加進 PATH）。" ;; esac
  fi
fi
if [ "$SERVICE" = 1 ]; then
  say "== 6/6 等看板起來"
  wait_s="${EVERGENIE_INSTALL_BOARD_WAIT:-60}"
  case "$wait_s" in "" | *[!0-9]*) wait_s=60 ;; esac
  deadline=$((SECONDS + wait_s))
  ! fault fail-board || die 32 "服務裝好了，但看板一直沒有起來（測試：故意的）。"
  until ! fault hang-board && curl -q -fsS --max-time 3 -o /dev/null "$BOARD" 2>/dev/null; do
    [ "$SECONDS" -lt "$deadline" ] || die 32 "服務裝好了，但看板一直沒有起來（${BOARD}）。" # 日誌由 die 留到 ~/Library/Logs/
    sleep 1
  done
fi

trap - INT TERM HUP
ON_SIG=0
if fault mark-stuck || ! rm -f "$MARK" 2>/dev/null || [ -e "$MARK" ]; then
  # 刪不掉：改寫成 done，下次重跑才不會當成沒裝完去收（把用過的家收掉）；連改都改不了，重跑時還有家的指紋擋著（用過就不一樣）
  if printf 'done\n' >"$MARK" 2>/dev/null; then
    note "收不掉 ${MARK}（已經改成「裝好了」的紀錄，不影響使用）；方便的話自己刪掉它。"
  else
    note "收不掉也改不了 ${MARK}：請自己刪掉它。刪掉之前不要再跑一次安裝程式。"
  fi
fi
# 角色的頭像與衣服（DEV-157、PRS-6）：請剛啟動的服務照這一版的角色包同步一次（服務寫衣服的目錄，衣櫃馬上重算）。已經裝好了才做：
# 中途被打斷不會把家收掉；沒成功（沒網路、套件服務還沒準備好）不算安裝失敗，服務之後會自己再試。
# 最多等 EVERGENIE_INSTALL_PACK_WAIT 秒（預設 180）：超時只印一行注意、照樣算裝好了——同步是服務在做，不等它也會做完或之後自己補。
if [ "$SERVICE" = 1 ]; then
  say "== 下載角色的頭像與衣服"
  pack_wait="${EVERGENIE_INSTALL_PACK_WAIT:-180}"
  case "$pack_wait" in '' | *[!0-9]*) pack_wait=180 ;; esac
  if fault hang-pack; then pack_cmd=(sleep 600); else pack_cmd=("$BIN" personas pack sync --home "$HOME_DIR"); fi
  pack_out="$(limited "$pack_wait" "${pack_cmd[@]}")"
  pack_rc=$?
  if [ "$pack_rc" = 124 ]; then
    note "角色的頭像與衣服等了 ${pack_wait} 秒還沒弄好，先不等了；服務之後會自己補上。"
  elif [ "$pack_rc" != 0 ]; then
    note "角色的頭像與衣服這次還沒弄好，服務之後會自動再試（$(printf '%s\n' "$pack_out" | tail -1)）。"
  fi
fi
# 「修復」入口（DEV-153、HST-11）：在 ~/Applications 放可以點兩下的「<產品名> 修復.command」（檢查、重開服務、把結果開給主人看）。
# 已經裝好了才做；放不成（舊版程式沒有這個指令、資料夾寫不進去）只提醒一行，不算安裝失敗。
RESCUE=""
if [ "$SERVICE" = 1 ]; then
  RESCUE="$("$BIN" rescue install --home "$HOME_DIR" --exe "$BIN" 2>/dev/null | tail -1)"
  case "$RESCUE" in */*.command) [ -x "$RESCUE" ] || RESCUE="" ;; *) RESCUE="" ;; esac
  [ -n "$RESCUE" ] || note "沒能在「應用程式」資料夾放好「$PRODUCT 修復」（不影響使用）；之後看板打不開時，請重新跑一次安裝或問提供程式的人。"
fi
# 以前安裝失敗留下的日誌（keep_logs 複製的 ~/Library/Logs/<產品名>-install-<時間>/）：超過 30 天的順手刪掉，只認這個名字
find "$HOME/Library/Logs" -maxdepth 1 -type d -name "$PRODUCT-install-[0-9]*" -mtime +30 -exec rm -rf {} + 2>/dev/null
say ""
say "裝好了${VERSION:+（版本 ${VERSION}）}。"
say "看板網址：$BOARD"
say "你的資料：$HOME_DIR"
say "程式：$DIR"
say "指令：${LINK_NOTE:-$BIN}"
if [ "$SERVICE" = 1 ]; then
  say "下一步：用瀏覽器打開看板，第一次會先看到歡迎頁，照它選好第一位秘書就能開始用。"
  say "想用手機看：在這台 Mac 打開 ${BOARD}settings/phone（看板的「設定 → 手機連線」），照四步做。"
  [ -z "$RESCUE" ] || say "出問題時：按 ⌘＋空白鍵搜尋「$PRODUCT 修復」打開它（放在「應用程式」資料夾：${RESCUE}），它會檢查、重開，再把結果開給你看。"
else
  say "還沒有啟動。要啟動：\"$BIN\" serve --home \"$HOME_DIR\""
fi
if [ "$MODE" = source ]; then
  say "把 \"$DIR/bin\" 加進 PATH 就能用 $PRODUCT 指令；要升級：$PRODUCT upgrade --check。"
fi
printf '%s' "$KEPT$NOTES"
exit 0
