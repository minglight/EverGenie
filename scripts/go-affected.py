#!/usr/bin/env python3
"""算出「這次改動影響到的 Go 套件」（T2 第 5 條，make check 用）。

輸出：
  ALL                          沒把握（改到套件以外的檔案、go.mod／go.sum、git 查不到）→ 呼叫端全跑，靠 go 的快取
  <模組資料夾>\t<套件> <套件>…   每個有受影響套件的模組一行（模組資料夾相對 repo 根目錄，主模組是 .）

作法：改到的檔案 → 所屬的套件資料夾（往上找最近的套件；testdata、嵌入的檔案都算那個套件的）→
用 `go list -e -test -f` 取得每個套件的 import（含測試 import）→ 反向閉包（誰直接或間接依賴改到的套件，
跨模組也算，用 import path 對）。只改了 *_test.go 的套件只算它自己（沒有別的套件會 import 測試檔）。改動來源 = 相對 EVERGENIE_BASE（預設 main）的 merge-base 的 git diff
（已提交的分支改動 + 工作目錄的改動）+ 未追蹤的檔案。
"""
import os
import re
import subprocess
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
BASE = os.environ.get("EVERGENIE_BASE", "main")
MODULES = [".", "sdk"] + sorted(
    os.path.relpath(os.path.dirname(p), ROOT)
    for p in (
        os.path.join(ROOT, "packages", "official", d, "go.mod")
        for d in os.listdir(os.path.join(ROOT, "packages", "official"))
    )
    if os.path.isfile(p)
)


def git(*args):
    return subprocess.run(["git", *args], cwd=ROOT, check=True, capture_output=True, text=True).stdout


def all_and_exit(why):
    print("ALL")
    print(f"# {why}", file=sys.stderr)
    sys.exit(0)


try:
    mb = git("merge-base", "HEAD", BASE).strip()
    changed = set(git("diff", "--name-only", mb).splitlines())
    changed |= set(git("ls-files", "--others", "--exclude-standard").splitlines())
except Exception as e:  # 沒有 git／沒有 main：沒把握
    all_and_exit(f"git 查不到改動（{e}）")

if not changed:
    sys.exit(0)  # 沒有改動：什麼都不用測（輸出空）

# ---- 各模組的套件與 import ----
pkgs = {}  # import path -> (模組資料夾, 絕對資料夾)
imports = {}  # import path -> set(import path)
for mod in MODULES:
    out = subprocess.run(
        ["go", "list", "-e", "-test", "-f", "{{.ImportPath}}|{{.Dir}}|{{join .Imports \",\"}}", "./..."],
        cwd=os.path.join(ROOT, mod), capture_output=True, text=True,
    )
    if out.returncode != 0:
        all_and_exit(f"go list 失敗（{mod}）")
    for line in out.stdout.splitlines():
        ip, d, imps = (line.split("|", 2) + ["", ""])[:3]
        if ip.endswith(".test") and "[" not in ip:
            continue  # 測試主程式
        base = re.sub(r" \[.*\]$", "", ip)
        if base.endswith("_test"):
            base = base[: -len("_test")]
        pkgs.setdefault(base, (mod, d))
        imports.setdefault(base, set()).update(i for i in imps.split(",") if i)

dir_to_pkg = {d: ip for ip, (_, d) in pkgs.items() if d}
mod_dirs = {m: os.path.normpath(os.path.join(ROOT, m)) for m in MODULES}


def module_of(abspath):
    best = None
    for m, md in mod_dirs.items():
        if abspath == md or abspath.startswith(md + os.sep):
            if best is None or len(md) > len(mod_dirs[best]):
                best = m
    return best


# 這些資料夾的檔案不會被「Go 套件的測試」讀到（讀整個 repo 的 internal/tools/ 掃描測試每次都跑，不靠這份清單）。
INERT = ("web/", "progress/", "scripts/", "Makefile")

seeds = set()  # 改到的套件本身要測，依賴它的也要測
self_only = set()  # 只改了 *_test.go：只有它自己的測試要重跑（別的套件 import 不到測試檔）
for f in changed:
    if f.startswith(INERT):
        continue
    ap = os.path.normpath(os.path.join(ROOT, f))
    m = module_of(ap)
    if m is None:
        all_and_exit(f"{f} 不在任何 Go 模組裡")
    if os.path.basename(f) in ("go.mod", "go.sum", "go.work", "go.work.sum"):
        seeds |= {ip for ip, (mm, _) in pkgs.items() if mm == m}
        continue
    d = os.path.dirname(ap)
    while True:
        if d in dir_to_pkg:
            if d == os.path.dirname(ap) and f.endswith("_test.go"):
                self_only.add(dir_to_pkg[d])
            else:
                seeds.add(dir_to_pkg[d])
            break
        if d == mod_dirs[m] or len(d) <= len(ROOT):
            all_and_exit(f"{f} 不屬於任何套件（可能被別的測試讀到）")
        d = os.path.dirname(d)

# ---- 反向閉包 ----
rev = {}
for ip, imps in imports.items():
    for i in imps:
        rev.setdefault(i, set()).add(ip)
affected = set(seeds) | self_only
todo = list(seeds)
while todo:
    cur = todo.pop()
    for r in rev.get(cur, ()):
        if r not in affected:
            affected.add(r)
            todo.append(r)

by_mod = {}
for ip in sorted(affected):
    if ip in pkgs:
        by_mod.setdefault(pkgs[ip][0], []).append(ip)
for m in MODULES:
    if m in by_mod:
        print(m + "\t" + " ".join(by_mod[m]))
