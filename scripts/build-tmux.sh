#!/usr/bin/env bash
# 從原始碼編一份跟著 evergenie 發的 tmux（DEV-155 S4；規則 engine-claude 說明卡 ECL-63）。
#
# 為什麼：秘書的對話跑在 tmux 裡；一般人的 Mac 沒有 tmux，也不該為了它先裝 Homebrew。
# 做法：下載釘住版本的 tmux、libevent、ncurses、utf8proc 原始碼（只走 https；檢查碼寫死在下面，不符就停、不編），
# 在暫存資料夾把三個函式庫編成靜態的（.a）再連進 tmux：產物只動態連 /usr/lib/ 的系統函式庫（libSystem、libresolv）。
# ncurses 只讀 macOS 內建的 /usr/share/terminfo（launchd 底下沒有終端機、沒有 TERMINFO 也找得到；人接上畫面時也找得到 xterm-256color）。
# 預設 TERM 寫死成 tmux-256color（跟這台 Homebrew 的 tmux 一樣；交叉編時 configure 不能實跑去偵測，不寫死兩種晶片會不一樣）。
# arm64 與 x86_64 各編一份；不是本機晶片的那份用 -arch x86_64（或 arm64）交叉編，只檢查連結、晶片與 terminfo 路徑格式，不實跑。
#
# 同時編一支很小的啟動程式 disclaim（engine-claude ECL-65；原始碼就在這支裡面，改它快取就會重編）：引擎開 tmux server 時先經過它，
# 它讓 tmux 自己當 macOS 權限視窗的「負責的程式」再換成 tmux，視窗上顯示 tmux 的檔名（發行檔裡取成產品名），允許記在那份 tmux 名下。
#
# 用法：scripts/build-tmux.sh [--out <資料夾>] [--arch arm64|x86_64|all] [--src <原始碼快取>] [--jobs N] [--keep] [--no-smoke] [--old <舊的 tmux>]
#   --out       產物放哪（預設 <repo>/dist/tmux）：<out>/<arch>/tmux 與 <out>/<arch>/disclaim，授權文字在 <out>/LICENSES/
#   --arch      只編一種晶片（預設 all＝兩種都編）
#   --src       下載的原始碼放哪（預設 <out>/src；已經有、檢查碼也對就不重抓）
#   --jobs      make 的並行數（預設核心數的一半）
#   --keep      留下暫存的編譯資料夾（查編譯錯誤用；路徑印在最後）
#   --no-smoke  不做煙霧測試
#   --old       新的 client 連舊的 server 時，舊的用哪一支（預設：編之前 <out>/<本機晶片>/tmux 那份，沒編成功會放回去；沒有就用剛編好的、同版本驗流程）
# 本機晶片那份編好會做一次煙霧測試：乾淨環境（env -i，PATH 只有 /usr/bin:/bin、空的 HOME）下
# 經 disclaim 開 server（跟引擎一樣）、new-session -d、send-keys、load-buffer＋paste-buffer -p、capture-pane -p／-p -e、人接上畫面（xterm-256color），
# 用自己的 socket（hg-s4test-<pid>），做完 kill-server；不碰任何家正在用的 socket。disclaim 另外驗參數、環境、結束碼照傳。
# 接著用 scripts/tmux-compat.sh 驗新的 client 連得上舊的 server（升級換了 tmux、秘書的對話還開在舊的 server 裡）。
# 只用 macOS 內建與 Xcode 指令列工具（curl、shasum、tar、clang、make、yacc、otool、lipo、codesign、strip、strings、script）；不用 Homebrew、不用 sudo。
# 授權文字：repo 的 LICENSES/ 是正本；這支會拿原始碼裡的授權檔跟它比，版本升了、文字變了就停，提醒先更新 LICENSES/。
set -euo pipefail

TMUX_VERSION=3.6a
TMUX_SHA256=b6d8d9c76585db8ef5fa00d4931902fa4b8cbe8166f528f44fc403961a3f3759
TMUX_URL="https://github.com/tmux/tmux/releases/download/$TMUX_VERSION/tmux-$TMUX_VERSION.tar.gz"
LIBEVENT_VERSION=2.1.12-stable
LIBEVENT_SHA256=92e6de1be9ec176428fd2367677e61ceffc2ee1cb119035037a27d346b0403bb
LIBEVENT_URL="https://github.com/libevent/libevent/releases/download/release-$LIBEVENT_VERSION/libevent-$LIBEVENT_VERSION.tar.gz"
NCURSES_VERSION=6.6
NCURSES_SHA256=355b4cbbed880b0381a04c46617b7656e362585d52e9cf84a67e2009b749ff11
NCURSES_URL="https://ftp.gnu.org/gnu/ncurses/ncurses-$NCURSES_VERSION.tar.gz"
UTF8PROC_VERSION=2.11.3
UTF8PROC_SHA256=abfed50b6d4da51345713661370290f4f4747263ee73dc90356299dfc7990c78
UTF8PROC_URL="https://github.com/JuliaStrings/utf8proc/archive/refs/tags/v$UTF8PROC_VERSION.tar.gz"
# 跟 Go 1.24 編出來的 evergenie 一樣最低 macOS 11。
MIN_MACOS=11.0
DEFAULT_TERM=tmux-256color

CALLER_PWD="$PWD"
cd "$(dirname "$0")/.."
ROOT="$PWD"

die() { echo "build-tmux：$*" >&2; exit 1; }
step() { echo "==> $*"; }

OUT="$ROOT/dist/tmux"
ARCHS="arm64 x86_64"
SRC=""
JOBS=""
KEEP=0
SMOKE=1
OLD_TMUX=""
while [ $# -gt 0 ]; do
  case "$1" in
    --out) [ $# -ge 2 ] || die "--out 要給資料夾"; OUT="$2"; shift ;;
    --arch)
      [ $# -ge 2 ] || die "--arch 要給 arm64、x86_64 或 all"
      case "$2" in arm64|x86_64) ARCHS="$2" ;; all) ARCHS="arm64 x86_64" ;; *) die "--arch 只收 arm64、x86_64、all：$2" ;; esac
      shift ;;
    --src) [ $# -ge 2 ] || die "--src 要給資料夾"; SRC="$2"; shift ;;
    --jobs) [ $# -ge 2 ] || die "--jobs 要給數字"; JOBS="$2"; shift ;;
    --keep) KEEP=1 ;;
    --no-smoke) SMOKE=0 ;;
    --old) [ $# -ge 2 ] || die "--old 要給舊的 tmux 執行檔"; OLD_TMUX="$2"; shift ;;
    -h|--help) sed -n '2,27p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "不認得的參數：$1（用 --help 看用法）" ;;
  esac
  shift
done
# 相對路徑照呼叫的人所在的資料夾算（跟一般指令一樣），不是 repo 根目錄。
case "$OUT" in /*) ;; *) OUT="$CALLER_PWD/$OUT" ;; esac
[ -n "$SRC" ] || SRC="$OUT/src"
case "$SRC" in /*) ;; *) SRC="$CALLER_PWD/$SRC" ;; esac
case "$OLD_TMUX" in ""|/*) ;; *) OLD_TMUX="$CALLER_PWD/$OLD_TMUX" ;; esac
[ -z "$OLD_TMUX" ] || [ -x "$OLD_TMUX" ] || die "--old 給的不是能執行的檔：$OLD_TMUX"

[ "$(uname -s)" = Darwin ] || die "只在 macOS 上編（產物是 macOS 的執行檔）"

# 乾淨的編譯環境：PATH 只留系統內建，拿掉會把 Homebrew 的標頭檔、函式庫、pkg-config 帶進來的變數，
# 這樣 configure 找不到 /opt/homebrew 的任何東西，產物也就不會連到它。
export PATH=/usr/bin:/bin:/usr/sbin:/sbin
unset CPATH C_INCLUDE_PATH OBJC_INCLUDE_PATH LIBRARY_PATH DYLD_LIBRARY_PATH DYLD_FALLBACK_LIBRARY_PATH \
  PKG_CONFIG PKG_CONFIG_PATH PKG_CONFIG_LIBDIR PKG_CONFIG_SYSROOT_DIR \
  CC CXX CPP CFLAGS CPPFLAGS CXXFLAGS LDFLAGS LIBS TERMINFO TERMINFO_DIRS 2>/dev/null || true
xcrun --find clang >/dev/null 2>&1 || die "要先裝 Xcode 指令列工具（xcode-select --install）：需要 clang、make、yacc"
for t in curl shasum tar make yacc otool lipo codesign strip strings; do
  command -v "$t" >/dev/null 2>&1 || die "缺系統工具 ${t}（Xcode 指令列工具裝好了嗎？）"
done
SDKROOT="$(xcrun --show-sdk-path)"
export SDKROOT MACOSX_DEPLOYMENT_TARGET="$MIN_MACOS"

NATIVE="$(uname -m)" # arm64 或 x86_64
triple() { case "$1" in arm64) echo aarch64-apple-darwin ;; x86_64) echo x86_64-apple-darwin ;; esac; }
[ -n "$JOBS" ] || JOBS=$(( $(sysctl -n hw.ncpu) / 2 ))
[ "$JOBS" -ge 1 ] 2>/dev/null || JOBS=1

WORK="$(mktemp -d "${TMPDIR:-/tmp}/hg-build-tmux.XXXXXX")"
# 升 tmux 時「舊的」就是編之前那一份：先留一份，編完拿來驗新的 client 連得上它開的 server。
# 沒編成功（例如新的連不上舊的 server）就把這份放回 <out>（見 cleanup）：重跑時「舊的」才還是真的舊版，不會變成新的跟自己比。
OLD_SAVED=0
if [ -z "$OLD_TMUX" ] && [ -x "$OUT/$NATIVE/tmux" ]; then
  cp -p "$OUT/$NATIVE/tmux" "$WORK/old-tmux"
  [ ! -f "$OUT/$NATIVE/disclaim" ] || cp -p "$OUT/$NATIVE/disclaim" "$WORK/old-disclaim"
  OLD_TMUX="$WORK/old-tmux" OLD_SAVED=1
fi
SOCK="hg-s4test-$$"
SMOKE_BIN=""
SOCK_PATH=""
# 煙霧測試的 tmux server 一定收掉，socket 檔也刪（tmux 結束時不一定會刪）；只動自己這個 socket。
stop_smoke() {
  if [ -n "$SMOKE_BIN" ]; then
    env -i HOME="$WORK/smoke-home" PATH=/usr/bin:/bin "$SMOKE_BIN" -L "$SOCK" kill-server >/dev/null 2>&1 || true
  fi
  if [ -n "$SOCK_PATH" ]; then rm -f "$SOCK_PATH"; fi
  SMOKE_BIN=""
  SOCK_PATH=""
}
cleanup() {
  local rc=$?
  stop_smoke
  if [ "$rc" != 0 ] && [ "$OLD_SAVED" = 1 ]; then
    cp -p "$WORK/old-tmux" "$OUT/$NATIVE/tmux" 2>/dev/null || true
    if [ -f "$WORK/old-disclaim" ]; then cp -p "$WORK/old-disclaim" "$OUT/$NATIVE/disclaim" 2>/dev/null || true; fi
    echo "build-tmux：沒編成功，$OUT/$NATIVE/ 放回編之前那份" >&2
  fi
  if [ "$KEEP" = 1 ]; then echo "暫存的編譯資料夾留著：$WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

sha256() { shasum -a 256 "$1" | awk '{print $1}'; }

# fetch <名稱> <網址> <sha256> <存成的檔名>：已經有、檢查碼也對就不重抓；抓下來不對就刪掉並停。
fetch() {
  local name="$1" url="$2" want="$3" file="$SRC/$4" got
  mkdir -p "$SRC"
  if [ -f "$file" ] && [ "$(sha256 "$file")" = "$want" ]; then
    echo "    $4（已經有，檢查碼對）"
    return
  fi
  rm -f "$file" "$file.part"
  curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --retry 3 -o "$file.part" "$url" ||
    die "下載 $name 失敗：$url"
  got="$(sha256 "$file.part")"
  if [ "$got" != "$want" ]; then
    rm -f "$file.part"
    die "$name 的檢查碼不對（要 ${want}，拿到 ${got}）：下載到的不是釘住的那一版，停下來不編"
  fi
  mv "$file.part" "$file"
  echo "    $4（下載，檢查碼對）"
}

step "下載原始碼並驗檢查碼 → $SRC"
fetch tmux "$TMUX_URL" "$TMUX_SHA256" "tmux-$TMUX_VERSION.tar.gz"
fetch libevent "$LIBEVENT_URL" "$LIBEVENT_SHA256" "libevent-$LIBEVENT_VERSION.tar.gz"
fetch ncurses "$NCURSES_URL" "$NCURSES_SHA256" "ncurses-$NCURSES_VERSION.tar.gz"
fetch utf8proc "$UTF8PROC_URL" "$UTF8PROC_SHA256" "utf8proc-$UTF8PROC_VERSION.tar.gz"

# run <log> <說明> <指令…>：輸出寫進 log；失敗時印 log 最後 40 行再停。
run() {
  local log="$1" what="$2"
  shift 2
  if ! "$@" >>"$log" 2>&1; then
    echo "----- ${log}（最後 40 行）-----" >&2
    tail -n 40 "$log" >&2
    die "$what 失敗（完整紀錄：${log}；加 --keep 留下暫存資料夾）"
  fi
}

# build <晶片>：在 $WORK/<晶片>/ 解開原始碼、依序編 utf8proc、libevent、ncurses（都只要 .a）、tmux。
build() {
  local arch="$1" b="$WORK/$1" p="$WORK/$1/prefix" cc cross=() log
  cc="clang -arch $arch -mmacosx-version-min=$MIN_MACOS"
  if [ "$arch" != "$NATIVE" ]; then
    cross=(--build="$(triple "$NATIVE")" --host="$(triple "$arch")")
  fi
  log="$b/build.log"
  mkdir -p "$b/src" "$p/lib" "$p/include"
  for t in "tmux-$TMUX_VERSION" "libevent-$LIBEVENT_VERSION" "ncurses-$NCURSES_VERSION" "utf8proc-$UTF8PROC_VERSION"; do
    tar -xzf "$SRC/$t.tar.gz" -C "$b/src"
  done

  step "${arch}：utf8proc $UTF8PROC_VERSION"
  run "$log" "utf8proc" make -C "$b/src/utf8proc-$UTF8PROC_VERSION" libutf8proc.a CC="$cc" CFLAGS=-O2 AR=ar
  cp "$b/src/utf8proc-$UTF8PROC_VERSION/libutf8proc.a" "$p/lib/"
  cp "$b/src/utf8proc-$UTF8PROC_VERSION/utf8proc.h" "$p/include/"

  step "${arch}：libevent $LIBEVENT_VERSION"
  (
    cd "$b/src/libevent-$LIBEVENT_VERSION"
    run "$log" "libevent configure" env CC="$cc" CFLAGS=-O2 ./configure ${cross[@]+"${cross[@]}"} --prefix="$p" \
      --disable-shared --enable-static --disable-openssl --disable-samples --disable-libevent-regress \
      --disable-debug-mode --disable-dependency-tracking
    run "$log" "libevent make" make -j"$JOBS"
    run "$log" "libevent install" make install
  )

  step "${arch}：ncurses ${NCURSES_VERSION}（只要 terminfo 函式庫，資料庫用系統的 /usr/share/terminfo）"
  (
    cd "$b/src/ncurses-$NCURSES_VERSION"
    # cf_cv_mixedcase=no：terminfo 資料夾用十六進位命名（/usr/share/terminfo/78/xterm-256color，跟 macOS 一樣）。
    # 交叉編時 configure 不能實跑去偵測檔案系統分不分大小寫，會猜「分」而改用字母資料夾（x/），Intel 那份就找不到 terminfo。
    run "$log" "ncurses configure" env CC="$cc" CFLAGS=-O2 BUILD_CC=clang cf_cv_mixedcase=no ./configure ${cross[@]+"${cross[@]}"} --prefix="$p" \
      --without-shared --with-normal --without-debug --with-termlib --disable-widec \
      --without-cxx --without-cxx-binding --without-ada --without-manpages --without-progs --without-tests \
      --disable-db-install --without-pkg-config \
      --with-default-terminfo-dir=/usr/share/terminfo --with-terminfo-dirs=/usr/share/terminfo
    run "$log" "ncurses make" make -j"$JOBS"
    run "$log" "ncurses install" make install
  )
  [ -f "$p/lib/libtinfo.a" ] && [ -f "$p/include/ncurses/ncurses.h" ] || die "${arch}：ncurses 沒產生 libtinfo.a 或標頭檔（看 ${log}）"

  step "${arch}：tmux $TMUX_VERSION"
  (
    cd "$b/src/tmux-$TMUX_VERSION"
    # 函式庫直接給 .a 的完整路徑（*_CFLAGS／*_LIBS 有給，configure 就不問 pkg-config）：一定是靜態連結，不會被 -l 找到系統或 Homebrew 的。
    run "$log" "tmux configure" env CC="$cc" CFLAGS=-O2 \
      LIBEVENT_CORE_CFLAGS="-I$p/include" LIBEVENT_CORE_LIBS="$p/lib/libevent_core.a" \
      LIBTINFO_CFLAGS="-I$p/include/ncurses -I$p/include" LIBTINFO_LIBS="$p/lib/libtinfo.a" \
      LIBUTF8PROC_CFLAGS="-I$p/include" LIBUTF8PROC_LIBS="$p/lib/libutf8proc.a" \
      ./configure ${cross[@]+"${cross[@]}"} --disable-dependency-tracking --enable-utf8proc --with-TERM="$DEFAULT_TERM"
    run "$log" "tmux make" make -j"$JOBS"
  )

  local bin="$OUT/$arch/tmux"
  mkdir -p "$OUT/$arch"
  rm -f "$bin"
  cp "$b/src/tmux-$TMUX_VERSION/tmux" "$bin"
  run "$log" "strip" strip -x "$bin"
  # 臨時簽名（Apple 晶片一定要有簽名才跑得起來；strip 之後重簽，兩種晶片一致）。
  run "$log" "codesign" codesign --force --sign - "$bin"
  verify "$arch" "$bin"
  build_launcher "$arch" "$cc"
}

# launcher_source：啟動程式 disclaim 的 C 原始碼（ECL-65）。FALLBACK_NOTE 要跟引擎的 launcherFallbackNote 一字不差
# （cmd/evergenie 的 TestLauncherNoteMatchesEngine 檢查）。
launcher_source() {
  cat <<'C_EOF'
/* disclaim（evergenie 發行檔附帶，engine-claude ECL-65）：讓要執行的程式自己當 macOS 權限視窗的「負責的程式」，再把自己換成它。
 * 用法：disclaim <程式的完整路徑> [參數…]（後面全部照傳，程式拿到的 argv[0] 是它的完整路徑；環境照傳）
 * 引擎開 tmux server 時經過它：之後 session 裡的程式碰到要問權限的資料夾，視窗顯示 tmux 的檔名（產品名），允許記在那份 tmux 名下，
 * 不記在每次升級都換的程式名下。responsibility_spawnattrs_setdisclaim 是 macOS 沒寫進公開標頭的介面（瀏覽器、終端機這類程式用了很多年），
 * 執行時才找：找不到或設不起來就印一行（開頭固定，引擎轉進紀錄）再照平常的方式換過去，session 一定開得起來。 */
#include <dlfcn.h>
#include <errno.h>
#include <spawn.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

#define FALLBACK_NOTE "disclaim：權限視窗沒辦法改記在 tmux 自己名下，照平常的方式開："

extern char **environ;
typedef int (*setdisclaim_fn)(posix_spawnattr_t *, int);

int main(int argc, char **argv) {
	if (argc < 2) {
		fprintf(stderr, "disclaim：用法 disclaim <程式的完整路徑> [參數…]\n");
		return 2;
	}
	const char *path = argv[1];
	if (access(path, X_OK) != 0) {
		fprintf(stderr, "disclaim：沒辦法執行 %s：%s\n", path, strerror(errno));
		return 127;
	}
	int r = ENOSYS;
	setdisclaim_fn setdisclaim = (setdisclaim_fn)dlsym(RTLD_DEFAULT, "responsibility_spawnattrs_setdisclaim");
	if (setdisclaim != NULL) {
		posix_spawnattr_t a;
		r = posix_spawnattr_init(&a);
		if (r == 0) {
			r = posix_spawnattr_setflags(&a, POSIX_SPAWN_SETEXEC);
			if (r == 0) r = setdisclaim(&a, 1);
			if (r == 0) r = posix_spawn(NULL, path, NULL, &a, argv + 1, environ); /* 成功就不會回來 */
			posix_spawnattr_destroy(&a);
		}
	}
	fprintf(stderr, FALLBACK_NOTE "%s\n", strerror(r));
	execv(path, argv + 1);
	fprintf(stderr, "disclaim：沒辦法執行 %s：%s\n", path, strerror(errno));
	return 127;
}
C_EOF
}

# build_launcher <晶片> <編譯指令>：編 disclaim → $OUT/<晶片>/disclaim（strip、臨時簽名、檢查晶片與連結）。
build_launcher() {
  local arch="$1" cc="$2" b="$WORK/$1/launcher" bin="$OUT/$1/disclaim" log="$WORK/$1/build.log"
  step "${arch}：啟動程式 disclaim"
  mkdir -p "$b"
  launcher_source > "$b/disclaim.c"
  rm -f "$bin"
  # shellcheck disable=SC2086 # $cc 是「clang -arch … -mmacosx-version-min=…」，要拆開
  run "$log" "disclaim 編譯" $cc -O2 -Wall -Werror -o "$bin" "$b/disclaim.c"
  run "$log" "disclaim strip" strip -x "$bin"
  run "$log" "disclaim codesign" codesign --force --sign - "$bin"
  verify "$arch" "$bin" launcher
}

# verify <晶片> <執行檔> [launcher]：晶片對、只連 /usr/lib/ 的系統函式庫、有臨時簽名；tmux 還要 terminfo 資料夾用十六進位命名（讀得到 /usr/share/terminfo）。
verify() {
  local arch="$1" bin="$2" kind="${3:-tmux}" archs deps d
  archs="$(lipo -archs "$bin")"
  [ "$archs" = "$arch" ] || die "$bin 的晶片是「${archs}」，不是 $arch"
  deps="$(otool -L "$bin" | tail -n +2 | awk '{print $1}')"
  [ -n "$deps" ] || die "${bin}：otool -L 讀不到相依的函式庫"
  for d in $deps; do
    case "$d" in
      /usr/lib/*) ;;
      *) die "$bin 連到了不是系統的函式庫：${d}（只能連 /usr/lib/）" ;;
    esac
  done
  codesign --verify "$bin" 2>/dev/null || die "${bin}：簽名驗不過"
  if [ "$kind" = launcher ]; then
    echo "    ${bin}：${archs}，只連 $(echo $deps | tr ' ' ',')，有臨時簽名"
    return
  fi
  # ncurses 用這個格式組 terminfo 檔的路徑；換成字母資料夾（%s/./%c/%s）的話，macOS 的 /usr/share/terminfo 讀不到。
  # grep 不用 -q：-q 找到就先走，strings 會收到 SIGPIPE，pipefail 底下整串算失敗。
  strings -a "$bin" | grep -xF '%s/./%02x/%s' >/dev/null || die "${bin}：terminfo 資料夾不是十六進位命名，讀不到 /usr/share/terminfo"
  echo "    ${bin}：${archs}，只連 $(echo $deps | tr ' ' ',')，terminfo 用十六進位資料夾"
}

# smoke <執行檔> <disclaim>：乾淨環境下照引擎的用法走一遍（ECL-63、ECL-65：server 經 disclaim 開），再用 script 開一個假終端機接上畫面，確認 terminfo 找得到。
smoke() {
  local bin="$1" launcher="$2" h="$WORK/smoke-home" marker="s4-$$" out="" i ver rc err
  mkdir -p "$h"
  SMOKE_BIN="$bin"
  tm() { env -i HOME="$h" PATH=/usr/bin:/bin "$bin" -L "$SOCK" "$@"; }
  ver="$(env -i HOME="$h" PATH=/usr/bin:/bin "$bin" -V)"
  [ "$ver" = "tmux $TMUX_VERSION" ] || die "smoke：-V 印的是「${ver}」，不是 tmux $TMUX_VERSION"
  echo "    -V：$ver"
  # disclaim：參數（含空白）、環境、結束碼照傳，設得起來就不印 FALLBACK_NOTE；沒給程式結束 2、程式不能執行結束 127。
  rc=0
  out="$(env -i EVERGENIE_SMOKE_X=帶得到 "$launcher" /bin/sh -c 'echo "$EVERGENIE_SMOKE_X:$1"; exit 7' sh 'a b' 2>"$WORK/disclaim.err")" || rc=$?
  err="$(cat "$WORK/disclaim.err")"
  [ "$rc" = 7 ] && [ "$out" = "帶得到:a b" ] && [ -z "$err" ] ||
    die "smoke：disclaim 沒照傳（結束碼 ${rc}，輸出「${out}」，stderr「${err}」）"
  rc=0; "$launcher" >/dev/null 2>&1 || rc=$?
  [ "$rc" = 2 ] || die "smoke：disclaim 沒給程式要結束 2，拿到 $rc"
  rc=0; "$launcher" "$WORK/no-such-program" >/dev/null 2>&1 || rc=$?
  [ "$rc" = 127 ] || die "smoke：disclaim 程式不能執行要結束 127，拿到 $rc"
  echo "    disclaim：參數、環境、結束碼照傳，設得起來（沒印照平常方式開的那一行）"
  # 開 server 的那一次經 disclaim（跟引擎一樣）；之後的指令直接叫 tmux。
  err="$(env -i HOME="$h" PATH=/usr/bin:/bin "$launcher" "$bin" -L "$SOCK" new-session -d -s smoke -x 160 -y 50 /bin/sh 2>&1)" ||
    die "smoke：經 disclaim 的 new-session -d 失敗：$err"
  [ -z "$err" ] || die "smoke：經 disclaim 開 server 多印了東西：$err"
  SOCK_PATH="$(tm display-message -p '#{socket_path}')" || die "smoke：讀不到 socket 的位置"
  case "$SOCK_PATH" in */"$SOCK") ;; *) die "smoke：socket 位置不是自己的（${SOCK_PATH}）" ;; esac
  tm has-session -t '=smoke' || die "smoke：has-session 找不到剛開的 session"
  # 打的那一行是 $((6*7))，畫面上出現 42-<記號> 才是真的跑了。
  tm send-keys -t '=smoke:' "echo \$((6*7))-$marker" Enter || die "smoke：send-keys 失敗"
  printf '%s' "echo \$((2+3))-$marker-paste" | tm load-buffer -b hg-smoke - || die "smoke：load-buffer 失敗"
  tm paste-buffer -p -d -b hg-smoke -t '=smoke:' || die "smoke：paste-buffer -p 失敗"
  tm send-keys -t '=smoke:' Enter || die "smoke：送 Enter 失敗"
  for i in $(seq 1 50); do
    out="$(tm capture-pane -p -t '=smoke:')" || die "smoke：capture-pane -p 失敗"
    case "$out" in *"42-$marker"*"5-$marker-paste"*) break ;; esac
    sleep 0.1
  done
  case "$out" in
    *"42-$marker"*"5-$marker-paste"*) echo "    new-session -d、send-keys、paste-buffer -p、capture-pane -p：正常" ;;
    *) printf '%s\n' "$out" >&2; die "smoke：capture-pane -p 沒看到指令跑出來的結果" ;;
  esac
  tm capture-pane -p -e -t '=smoke:' >/dev/null || die "smoke：capture-pane -p -e 失敗"
  # 人接上畫面（sessions attach 印的指令）：script 給它一個假終端機，TERM=xterm-256color 要在 /usr/share/terminfo 找得到。
  # 接的是另一個只跑 sleep 的 session：script 的 stdin 是 /dev/null，一開始會往假終端機送一個 ^D，接到 sh 上 sh 就結束了。
  tm new-session -d -s view -x 80 -y 24 /bin/sleep 600 || die "smoke：開給 attach 用的 session 失敗"
  (env -i HOME="$h" PATH=/usr/bin:/bin TERM=xterm-256color script -q /dev/null "$bin" -L "$SOCK" attach -t '=view' </dev/null >/dev/null 2>&1 &)
  out=""
  for i in $(seq 1 50); do
    out="$(tm list-clients -F '#{client_termname}' 2>/dev/null || true)"
    [ -n "$out" ] && break
    sleep 0.1
  done
  [ "$out" = "xterm-256color" ] || die "smoke：接上畫面失敗（terminfo 找不到 xterm-256color？list-clients：「${out}」）"
  echo "    attach（TERM=xterm-256color，terminfo 來自 /usr/share/terminfo）：正常"
  tm kill-session -t '=smoke' || die "smoke：kill-session 失敗"
  stop_smoke
}

# 授權文字：原始碼裡的授權檔要跟 repo 的 LICENSES/ 一字不差，再一起放進產物。
licenses() {
  local b="$WORK/licenses" pair name rel
  mkdir -p "$b" "$OUT/LICENSES"
  for t in "tmux-$TMUX_VERSION" "libevent-$LIBEVENT_VERSION" "ncurses-$NCURSES_VERSION" "utf8proc-$UTF8PROC_VERSION"; do
    [ -d "$b/$t" ] || tar -xzf "$SRC/$t.tar.gz" -C "$b"
  done
  for pair in "tmux:tmux-$TMUX_VERSION/COPYING" "libevent:libevent-$LIBEVENT_VERSION/LICENSE" \
    "ncurses:ncurses-$NCURSES_VERSION/COPYING" "utf8proc:utf8proc-$UTF8PROC_VERSION/LICENSE.md"; do
    name="${pair%%:*}"
    rel="${pair#*:}"
    [ -f "$b/$rel" ] || die "原始碼裡找不到授權檔 $rel"
    cmp -s "$b/$rel" "$ROOT/LICENSES/$name.txt" ||
      die "LICENSES/$name.txt 跟 $rel 不一樣：版本升了就先把新的授權文字放進 LICENSES/（cp 原始碼的 ${rel}）"
    cp "$ROOT/LICENSES/$name.txt" "$OUT/LICENSES/$name.txt"
  done
  tmux_notices "$b/tmux-$TMUX_VERSION" "$OUT/LICENSES/tmux-notices.txt"
  echo "    授權文字：$OUT/LICENSES/（tmux、libevent、ncurses、utf8proc；tmux 各檔的著作權聲明在 tmux-notices.txt）"
}

# tmux_notices <tmux 原始碼> <輸出檔>：tmux 的 COPYING 只是範本（Copyright (c) <author>），真正的著作權人與授權寫在每個原始檔開頭
# （多數 ISC，compat/ 有幾個 BSD 三條款，都要求發行時附上聲明）。把每個 .c／.h／.y 開頭第一段有 Copyright 的註解抽出來，相同的只留一份。
tmux_notices() {
  local src="$1" dst="$2" d="$WORK/notices" f blk sum n=0
  rm -rf "$d"
  mkdir -p "$d"
  : > "$d/order"
  while IFS= read -r f; do
    blk="$(awk '/^\/\*/ { inb = 1; blk = "" }
      inb { blk = blk $0 "\n"; if ($0 ~ /\*\//) { inb = 0; if (blk ~ /Copyright/) { printf "%s", blk; exit } } }
      NR > 80 { exit }' "$f")"
    [ -n "$blk" ] || continue
    sum="$(printf '%s\n' "$blk" | shasum -a 256 | cut -c1-16)"
    [ -f "$d/$sum" ] && continue
    printf '%s\n' "$blk" > "$d/$sum"
    echo "$sum" >> "$d/order"
    n=$((n + 1))
  done < <(find "$src" \( -name '*.c' -o -name '*.h' -o -name '*.y' \) -type f | LC_ALL=C sort)
  [ "$n" -gt 0 ] || die "tmux 原始碼裡抽不到任何著作權聲明"
  {
    echo "tmux ${TMUX_VERSION}：各原始檔開頭的著作權與授權聲明（scripts/build-tmux.sh 從原始碼抽出，相同的只留一份，共 $n 段）。"
    echo "授權總說明見同資料夾的 tmux.txt（原始碼的 COPYING）。"
    while IFS= read -r sum; do
      echo
      cat "$d/$sum"
    done < "$d/order"
  } > "$dst"
}

step "授權文字"
licenses
for arch in $ARCHS; do
  build "$arch"
  if [ "$arch" = "$NATIVE" ] && [ "$SMOKE" = 1 ]; then
    step "${arch}：煙霧測試（env -i 乾淨環境，socket ${SOCK}）"
    smoke "$OUT/$arch/tmux" "$OUT/$arch/disclaim"
    step "${arch}：新的 client 連舊的 server（升級時秘書的對話還開在舊的 server 裡）"
    "$ROOT/scripts/tmux-compat.sh" "${OLD_TMUX:-$OUT/$arch/tmux}" "$OUT/$arch/tmux" "$OUT/$arch/disclaim" ||
      die "新的 tmux 連不上舊的 tmux 開的 server：升級之後秘書的對話會失聯，這一版的 tmux 不能發（見 ai_docs/runbooks/release.md「附帶的 tmux」）"
  elif [ "$arch" != "$NATIVE" ]; then
    echo "    $arch 不是本機晶片（${NATIVE}）：只檢查晶片、連結、簽名與 terminfo 路徑格式，沒有實跑"
  fi
done
step "完成：$(for a in $ARCHS; do printf '%s ' "$OUT/$a/tmux" "$OUT/$a/disclaim"; done)"
