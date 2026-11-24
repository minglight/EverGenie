# 下載版幾支驗收腳本共用的小工具（DEV-155 S9）：scripts/install-test.sh、upgrade-download-test.sh、clean-install-test.sh、package-go-test.sh
# 用 `. "$ROOT/scripts/lib/download-e2e.sh"` 載入。這裡只定義函式（名稱都是 dl_ 開頭）：不跑任何東西、不改 shell 的選項、
# 不用呼叫端的變數（要的東西都由參數給）。macOS 內建的 bash 3.2 也要能跑。

# dl_mktemp <上層資料夾> <前綴>：建 <上層>/<前綴>.XXXXXX 暫存資料夾，印它解開符號連結後的完整路徑；建不出來、或拿到的不是
# 「<前綴>.六個字」這種名字就回非 0、什麼都不印。呼叫的人收尾時會 rm -rf 它：mktemp 失敗時空字串經 `cd ""` 會變成目前的資料夾
# （repo 根目錄），所以一定要用這支、看結束碼，不要直接 mktemp。
dl_mktemp() {
  local d real
  d="$(mktemp -d "$1/$2.XXXXXX" 2>/dev/null)" || return 1
  [ -n "$d" ] && [ -d "$d" ] || return 1
  real="$(cd "$d" && pwd -P)" || return 1
  case "${real##*/}" in "$2".??????) printf '%s\n' "$real" ;; *) return 1 ;; esac
}

# dl_freeport：印一個現在沒人用的 TCP port（127.0.0.1）。
dl_freeport() {
  python3 -I -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

# dl_safebin <資料夾> [絆線紀錄檔]：做「乾淨的 Mac 上只有系統內建工具」的 PATH 替身資料夾：/usr/bin、/bin、/usr/sbin、/sbin 的指令
# 都連過來，但拿掉 go、gofmt、make、gmake、git、brew、tmux、python3、pip3、claude（乾淨的 Mac 上那幾個不是沒有就是要另外裝；
# 這台開發機的 /usr/bin 裡那幾支是真的能用的開發者工具）。
# 給了絆線紀錄檔：git、make、python3、編譯器這幾支改成「記一行到紀錄檔、印一句英文、結束碼 1」的替身，像乾淨的 Mac 上
# /usr/bin 裡那些「要先裝開發者工具」的空殼（一叫就跳視窗、失敗）；跑完看紀錄檔是空的，才知道整條路真的沒叫到它們。
dl_safebin() {
  local dir="$1" trip="${2:-}" d f n
  mkdir -p "$dir" || return 1
  for d in /usr/bin /bin /usr/sbin /sbin; do
    for f in "$d"/*; do
      n="${f##*/}"
      case "$n" in go | gofmt | make | gmake | git | git-* | brew | tmux | python3* | pip3* | claude) continue ;; esac
      [ -e "$dir/$n" ] || ln -s "$f" "$dir/$n"
    done
  done
  [ -n "$trip" ] || return 0
  : >"$trip"
  for n in git make gmake python3 pip3 clang clang++ cc c++ gcc g++ xcrun swift lldb; do
    rm -f "$dir/$n"
    {
      echo '#!/bin/sh'
      echo "echo \"$n \$*\" >>'$trip'"
      echo 'echo "xcode-select: note: No developer tools were found, requesting install." >&2'
      echo 'exit 1'
    } >"$dir/$n" && chmod +x "$dir/$n" || return 1
  done
}

# dl_fake_claude <假 HOME>：在 <假 HOME>/.local/bin/claude 放一支假的 Claude Code（Claude 的原生安裝也放在 ~/.local/bin），
# 並寫 ~/.claude.json 讓它是「已經登入」的樣子（`claude auth status` 看那個檔有沒有 oauthAccount；刪掉那個檔就是沒登入）。
# 在 tmux 裡被叫起來（秘書的 session）時印一行、待著不走（像真的 claude 在等主人說話）；其他時候印一行就結束。
dl_fake_claude() {
  mkdir -p "$1/.local/bin" || return 1
  cat >"$1/.local/bin/claude" <<'SH'
#!/bin/sh
if [ "$1 $2" = "auth status" ]; then
  if grep -q oauthAccount "$HOME/.claude.json" 2>/dev/null; then echo '{"loggedIn": true, "authMethod": "claude.ai"}'; exit 0; fi
  echo '{"loggedIn": false, "authMethod": "none"}'
  exit 1
fi
if [ -n "${TMUX:-}" ]; then
  echo "假的 claude 在 tmux 裡：$1 $2"
  exec /bin/sleep 600
fi
echo "假的 claude：$*" >&2
exit 0
SH
  chmod +x "$1/.local/bin/claude" &&
    echo '{"oauthAccount": {"emailAddress": "t@example.com"}}' >"$1/.claude.json"
}

# dl_english_in：stdin 的文字裡的英文字（兩個字母以上，空白隔開印出來；沒有就印空行）。路徑（含 /）、檔名（serve.err.log 這種）、
# 版本號、evergenie 這個名字不算。
dl_english_in() {
  python3 -I -c 'import re, sys
t = sys.stdin.read()
t = re.sub(r"\S*/\S*", " ", t)
t = re.sub(r"[\w.-]+\.(log|tgz|json|toml|md)\b", " ", t)
t = re.sub(r"\bevergenie\b", " ", t)
print(" ".join(re.findall(r"[A-Za-z]{2,}", t)))'
}

# dl_pyj <Python 運算式>：stdin 是一份 JSON（叫 d），印出運算式的值，例如 dl_pyj 'd["state"]["phase"]'。
dl_pyj() {
  python3 -I -c 'import json, sys
d = json.loads(sys.stdin.read())
print(eval(sys.argv[1]))' "$1"
}

# dl_doctor_reds：stdin 是 `evergenie doctor --json` 的輸出，印出紅字（severity error）的項目，一行一個；沒有就什麼都不印。
dl_doctor_reds() {
  python3 -I -c 'import json, sys
d = json.loads(sys.stdin.read())
print("\n".join(c.get("name", "") + "｜" + c.get("detail", "") for c in d["checks"] if c.get("severity") == "error"))'
}

# dl_official_cards <repo>：repo 的公開角色卡（packages/official/personas/cards/*.md）有哪幾位，代號排序、空白隔開。
# 跟 dist.sh 收檔一樣：被 git 忽略的不算（這一步在測試自己的環境跑，不在模擬的乾淨環境裡）。
dl_official_cards() {
  (cd "$1/packages/official/personas/cards" && git ls-files -co --exclude-standard -- '*.md' | sed 's/\.md$//' | LC_ALL=C sort | tr '\n' ' ' | sed 's/ $//')
}

# dl_fix_manifest <版本的 dist 資料夾> <晶片>：那個資料夾裡的包重新打過之後，把 manifest.json 的檢查碼與大小改成新的包。
dl_fix_manifest() {
  python3 -I - "$1" "$2" <<'PY'
import hashlib, json, os, sys
d, arch = sys.argv[1:3]
p = os.path.join(d, "manifest.json")
m = json.load(open(p, encoding="utf-8"))
a = m["assets"]["darwin-" + arch]
data = open(os.path.join(d, os.path.basename(a["url"])), "rb").read()
a["sha256"], a["size"] = hashlib.sha256(data).hexdigest(), len(data)
json.dump(m, open(p, "w", encoding="utf-8"), ensure_ascii=False)
PY
}

# dl_wrap_bin <版本的 dist 資料夾> <版本> <晶片> <包裝腳本>：做故意壞掉的版本。包裡的 bin/evergenie 改名 evergenie.real，
# 換成那支包裝腳本（它決定哪個指令要壞、其他的 exec 給 evergenie.real），重新打包、更新 manifest.json。
# 解開的內容留在 <版本的 dist 資料夾>/re/（呼叫的人可以檢查）。
dl_wrap_bin() {
  local d="$1" v="$2" arch="$3" wrapper="$4" pkg="evergenie-$2-darwin-$3.tar.gz"
  rm -rf "$d/re" && mkdir -p "$d/re" &&
    tar xzf "$d/$pkg" -C "$d/re" &&
    mv "$d/re/evergenie-$v/bin/evergenie" "$d/re/evergenie-$v/bin/evergenie.real" &&
    cp "$wrapper" "$d/re/evergenie-$v/bin/evergenie" &&
    chmod +x "$d/re/evergenie-$v/bin/evergenie" &&
    (cd "$d/re" && COPYFILE_DISABLE=1 tar -czf "$d/$pkg" "evergenie-$v") &&
    dl_fix_manifest "$d" "$arch"
}

# dl_publish <網站資料夾> <版本的 dist 資料夾> <晶片> [花樣]：把那一版的包放到 <網站資料夾>/rel/，寫 <網站資料夾>/manifest.json
# （包的網址寫成相對路徑）。花樣：badsha 檢查碼改掉、cut 檔案只給一半（清單照原本的大小與檢查碼）、
# nochip 清單只有另一種晶片的包、garbage 清單不是 JSON。
dl_publish() {
  local www="$1" d="$2" arch="$3" mode="${4:-}" f
  f="$(python3 -I -c 'import json, os, sys; print(os.path.basename(json.load(open(sys.argv[1], encoding="utf-8"))["assets"]["darwin-" + sys.argv[2]]["url"]))' "$d/manifest.json" "$arch")" || return 1
  mkdir -p "$www/rel" && cp -f "$d/$f" "$www/rel/$f" || return 1
  python3 -I - "$d/manifest.json" "$www" "$arch" "$mode" <<'PY'
import json, os, sys
src, www, arch, mode = sys.argv[1:5]
other = "amd64" if arch == "arm64" else "arm64"
m = json.load(open(src, encoding="utf-8"))
a = m["assets"]["darwin-" + arch]
a["url"] = "rel/" + os.path.basename(a["url"])
if mode == "badsha":
    a["sha256"] = "0" * 64
elif mode == "cut":
    data = open(os.path.join(www, a["url"]), "rb").read()
    cut = "rel/cut-" + os.path.basename(a["url"])
    open(os.path.join(www, cut), "wb").write(data[: len(data) // 2])
    a["url"] = cut
elif mode == "nochip":
    m["assets"] = {"darwin-" + other: a}
tmp = os.path.join(www, ".manifest.tmp")
with open(tmp, "w", encoding="utf-8") as f:
    if mode == "garbage":
        f.write("<html>502 Bad Gateway</html>\n")
    else:
        json.dump(m, f, ensure_ascii=False)
os.replace(tmp, os.path.join(www, "manifest.json"))
PY
}
