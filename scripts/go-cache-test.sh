#!/usr/bin/env bash
# Go 側快取的驗收（T2 第 4 條）：沒改東西時 make ci 的 go test 全是 (cached)。結束碼 0＝全過。
#  ① make build 不重寫沒變的執行檔（bin/ 的 mtime 不變；go test 的快取鍵含 mtime，見 scripts/go-build-bin.sh）
#  ② make web 沒改時不動 internal/base/app/board/dist（同理）
#  ③ 連跑兩次 make ci，第二次每個 `ok` 套件都是 (cached)，而且結束碼 0
set -uo pipefail
cd "$(dirname "$0")/.."
pass=0; fail=0
ok() { echo "PASS  $1"; pass=$((pass + 1)); }
bad() { echo "FAIL  $1"; fail=$((fail + 1)); }
snap() { find "$@" -exec stat -f '%N %m %z' {} + 2>/dev/null | sort; }

make ci >/dev/null 2>&1 # 先讓一切是最新的（第一次可能還在編）
a="$(snap bin)"
make build >/dev/null 2>&1
b="$(snap bin)"
[ "$a" = "$b" ] && ok "① make build 沒改東西時 bin/ 的檔案與資料夾 mtime 都不變" || bad "① bin/ 被重寫了"

a="$(snap internal/base/app/board/dist)"
make web >/dev/null 2>&1
b="$(snap internal/base/app/board/dist)"
[ "$a" = "$b" ] && ok "② make web 沒改時 dist 不動" || bad "② dist 被重寫了"

log="$(mktemp "${TMPDIR:-/tmp}/hg-gocache.XXXXXX")"
make ci >"$log" 2>&1; rc=$?
total="$(grep -c '^ok' "$log" || true)"
cached="$(grep -c '^ok.*(cached)' "$log" || true)"
[ "$rc" = 0 ] && ok "③a 第二次 make ci 結束碼 0" || bad "③a make ci 結束碼 $rc"
if [ "$total" -gt 0 ] && [ "$total" = "$cached" ]; then ok "③b go test 的 ok 套件 $total 個全是 (cached)"; else bad "③b ok 套件 $total 個、cached $cached 個"; grep '^ok' "$log" | grep -v cached; fi
rm -f "$log"
echo "通過 ${pass}，失敗 ${fail}"
[ "$fail" -eq 0 ]
