#!/usr/bin/env python3
"""scripts/workshop-init.sh 的設定步驟（GOAL-2 S3）：寫工坊家的 home.toml、settings/packages.toml，複製三位秘書與她們的角色。

只給 workshop-init.sh 呼叫（用 python3 -I 跑）。已存在的檔一律不蓋：
- home.toml：--fresh 1（剛由 evergenie init 建出來）才整份改寫；否則只比對必要的設定，不一樣的列出來。
- settings/packages.toml：沒有才寫（只開 engine-claude、admin 與從日常家複製來的 mine 套件，其他官方套件關掉）。
- vault/agents/<id>/agent.md、addendum.md、personas/<代號>.md、頭像、packages/mine/<套件>：沒有才從日常家複製。
"""
import argparse, os, re, shutil, sys
from pathlib import Path

# python3 -I 不會把腳本所在的資料夾放進 sys.path：明確加上，好讀同資料夾的 workshop_toml（3.9 沒有 tomllib）。
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import workshop_toml as toml  # noqa: E402

AGENTS = ["admin", "code", "architect"]
ENABLED = ["engine-claude", "admin"]
AVATAR_EXT = (".png", ".jpg", ".jpeg", ".webp", ".mp4")

# 工坊一定要有的設定（GOAL-2 W2、W3；S2 的 keep_review_open）。
# 沒有跨台備份（法律第 7 條：每台一個家、兩家不連線，備份由主人自己處理）。
def required() -> dict:
    return {
        ("ids", "tasks"): "DEV-",
        ("ids", "goals"): "DG-",
        ("runtime", "service_label"): "com.evergenie.workshop",
        ("runtime", "board_port"): 8880,
        ("runtime", "tmux_socket"): "hg-ws",
        # 工坊就是 office 的開發家（2026-11-01）：標籤 <owner>-office@dev，靠 env 跟正式家分開，同名秘書才不撞名。
        # remote_suffix 2026-11-01 退休（讀到忽略）。
        ("runtime", "env"): "dev",
        ("runtime", "machine"): "office",
        ("tasks", "keep_review_open"): True,
        ("update", "channel"): "off",
    }


def atomic_write(path: Path, text: str):
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + ".tmp")
    tmp.write_text(text, encoding="utf-8")
    os.replace(tmp, path)


def home_toml(home: Path, daily: dict, public_url: str, fresh: bool):
    path = home / "home.toml"
    cur = toml.loads(path.read_text(encoding="utf-8"))
    req = required()
    if fresh:
        # 由 init 建的那份出發：使用者、提醒時段、帳號、[admin]、[secrets]、[incidents] 換成日常家的。
        new = {"schema": cur.get("schema", 1), "mode": daily.get("mode", cur.get("mode", "simple"))}
        for sec in ("user", "slots", "accounts"):
            new[sec] = daily.get(sec, cur.get(sec))
        # 帳號是同一批：額度暖身（usage_warmup）日常家已經在做，工坊不再重複送。
        for acc in new["accounts"].values():
            if isinstance(acc, dict) and "usage_warmup" in acc:
                acc["usage_warmup"] = False
        new["web"] = dict(cur.get("web", {}))
        new["web"]["port"] = 8880
        if public_url:
            new["web"]["public_url"] = public_url
        if "admin" in daily:
            adm = {k: v for k, v in daily["admin"].items() if k != "dev_site"}  # dev_site 是跨台時代的舊鍵，沒人讀了，不抄過去
            new["admin"] = adm
        for sec in ("secrets", "incidents"):
            if sec in daily:
                new[sec] = daily[sec]
        for (sec, key), v in req.items():
            new.setdefault(sec, {})[key] = v
        text = "# 工坊的家（GOAL-2；scripts/workshop-init.sh 建的，之後照一般的家改）\n" + toml.dumps(new).rstrip() + "\n"
        atomic_write(path, text)
        toml.loads(path.read_text(encoding="utf-8"))
        print(f"寫好 home.toml（帳號 {', '.join(sorted(new['accounts'].keys() - {'default'}))}）")
        return
    diffs = []
    for (sec, key), want in req.items():
        got = cur.get(sec, {}).get(key)
        if got != want:
            diffs.append(f"  [{sec}] {key} = {toml.value(got) if got is not None else '（沒寫）'}，應該是 {toml.value(want)}")
    if diffs:
        print("home.toml 跟工坊該有的不一樣（沒有改，要改請手動）：")
        print("\n".join(diffs))
    else:
        print("home.toml 已有工坊的設定")


def packages_toml(home: Path, official: Path, mine: list):
    path = home / "settings" / "packages.toml"
    if path.exists():
        cur = toml.loads(path.read_text(encoding="utf-8")).get("enabled", {})
        if cur.get("admin") is not True:
            print("注意：settings/packages.toml 已存在但沒有開 admin（沒有改）")
        else:
            print("settings/packages.toml 已存在")
        return
    enabled = {}
    if official.is_dir():
        for d in sorted(official.iterdir()):
            if (d / "package.toml").exists():
                enabled[d.name] = d.name in ENABLED
    for p in ENABLED:
        enabled[p] = True
    for p in mine:
        enabled[p] = True
    atomic_write(path, "schema = 1\n\n" + toml.dumps({"enabled": dict(sorted(enabled.items()))}))
    print("寫好 settings/packages.toml（開：" + ", ".join(k for k, v in sorted(enabled.items()) if v) + "）")


def frontmatter_value(text: str, key: str) -> str:
    m = re.search(r"^---\n(.*?)\n---", text, re.S)
    if not m:
        return ""
    v = re.search(r"^" + re.escape(key) + r":\s*(.*)$", m.group(1), re.M)
    return v.group(1).strip().strip('"\'') if v else ""


def drop_key(text: str, key: str) -> str:
    """拿掉 frontmatter 裡頂層的一個欄位（含它底下縮排的行）。"""
    head, sep, body = text.partition("\n---\n")
    lines, out, skip = head.split("\n"), [], False
    for line in lines:
        if re.match(r"^[A-Za-z_][A-Za-z0-9_]*:", line):
            skip = line.startswith(key + ":")
        elif line and not line.startswith((" ", "-")):
            skip = False
        if not skip:
            out.append(line)
    return "\n".join(out) + sep + body


def copy_new(src: Path, dst: Path, what: str, changed: list):
    if dst.exists() or not src.exists():
        return
    dst.parent.mkdir(parents=True, exist_ok=True)
    if src.is_dir():
        shutil.copytree(src, dst, symlinks=True)
    else:
        shutil.copy2(src, dst)
    changed.append(what)


def agents_and_personas(home: Path, daily_home: Path) -> list:
    changed, mine = [], []
    for a in AGENTS:
        src = daily_home / "vault" / "agents" / a / "agent.md"
        dst = home / "vault" / "agents" / a / "agent.md"
        if not src.exists():
            print(f"注意：日常家沒有秘書 {a}（{src}），工坊這位要另外建")
            continue
        text = src.read_text(encoding="utf-8")
        if not dst.exists():
            dst.parent.mkdir(parents=True, exist_ok=True)
            # 舊的 sites 欄位（指到別台的機器名）在每台一個家之後沒有意義，拿掉。
            dst.write_text(drop_key(text, "sites"), encoding="utf-8")
            changed.append(f"秘書 {a}")
        copy_new(src.parent / "addendum.md", dst.parent / "addendum.md", f"{a} 的附加規則", changed)
        persona = frontmatter_value(dst.read_text(encoding="utf-8"), "persona")
        if "." in persona:  # 套件裡的角色（<套件>.<代號>）：套件整個資料夾帶過去
            pkg = persona.split(".", 1)[0]
            copy_new(daily_home / "packages" / "mine" / pkg, home / "packages" / "mine" / pkg, f"套件 {pkg}（mine）", changed)
            if (home / "packages" / "mine" / pkg).exists():
                mine.append(pkg)
        elif persona:
            copy_new(daily_home / "personas" / f"{persona}.md", home / "personas" / f"{persona}.md", f"角色卡 {persona}", changed)
            for ext in AVATAR_EXT:
                copy_new(daily_home / "personas" / "avatars" / f"{persona}{ext}", home / "personas" / "avatars" / f"{persona}{ext}", f"頭像 {persona}{ext}", changed)
    copy_new(daily_home / "personas" / "_style.md", home / "personas" / "_style.md", "說話層 _style.md", changed)
    print("複製：" + ("、".join(changed) if changed else "沒有（都已經在了）"))
    return sorted(set(mine))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--home")
    ap.add_argument("--daily", required=True)
    ap.add_argument("--public-url", default="")
    ap.add_argument("--official")
    ap.add_argument("--fresh", default="0")
    ap.add_argument("--print", dest="show", help="只印日常家 home.toml 的一個值（例 user.name），給 workshop-init.sh 用")
    a = ap.parse_args()
    if a.show:
        v = toml.load(Path(a.daily) / "home.toml")
        for p in a.show.split("."):
            v = v.get(p, {}) if isinstance(v, dict) else {}
        print(v if isinstance(v, str) else "")
        return
    if not (a.home and a.official):
        sys.exit("要給 --home、--official")
    home, daily_home = Path(a.home), Path(a.daily)
    daily = toml.loads((daily_home / "home.toml").read_text(encoding="utf-8"))
    home_toml(home, daily, a.public_url, a.fresh == "1")
    mine = agents_and_personas(home, daily_home)
    packages_toml(home, Path(a.official), mine)


if __name__ == "__main__":
    main()
