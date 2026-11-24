#!/usr/bin/env python3
"""一次性的搬家腳本：把開發 EverGenie 用的資料從日常的家搬進另一個專門開發的家（工坊）。可以重跑。

用法：python3 -I scripts/split-workshop.py --daily <日常家> --workshop <工坊家> [--only workshop|daily] [--apply]
  --daily     日常的家（在工坊那台跑工坊那一半時，可以是日常家的唯讀副本）
  --workshop  工坊的家（先用 scripts/workshop-init.sh 建好；home.toml 要有 [ids] tasks = "DEV-"、goals = "DG-"）
  --only      只做一半：workshop＝寫工坊（在工坊那台跑），daily＝把日常的舊 vault/admin/ 搬去備份（在日常家那台跑）；沒給＝兩半都做
  --apply     真的寫；沒給只預演（列出會做什麼，什麼都不動）
跑兩次結果一樣（憲法第 8 條）：已經在的不蓋、不重寫；第二次會印「沒有變動」。

工坊那一半：
  - 日常 vault/admin/DEV-<n>.md（含 archive/<月>/）→ 工坊 vault/tasks/DEV-<n>.md（封存的進 tasks/archive/<月>/），照任務單 schema：
    狀態 idea→idea、doing→doing、verifying→review、done→done、cancelled→cancelled；標題、內文、負責秘書、建立／更新時間、
    links、結果、怎麼驗、退回意見、開單的人原樣留；任務單必填、DEV 單沒有的欄位補值（見 fill_task）。工坊的任務單計數接在最大號之後。
  - 日常的一張 Goal（SRC_GOAL）→ 工坊的 DST_GOAL（兩個號碼是下面的常數）：原文、grants、確認人與時間原樣，
    Planbook 加一行「從日常 <SRC_GOAL> 搬來」；日常那份不改。
  - admin、code、architect 的記憶（memory.md）與日誌（journal/）複製給工坊的同名秘書（已經有的不蓋）。
日常那一半：
  - 日常的舊 vault/admin/ 整個搬到家裡、vault/ 外面：<日常家>/admin.bak-<YYYYMMDD>-workshop-split（備份規則（system 卡 BSW-4）的命名；
    不在 vault/ 裡 doctor 才不會報 data.invalid；家的 .gitignore 是白名單，不會進 git）。兩半一起跑時，工坊沒收齊不搬。
最後印部署當下要另外用 CLI 做的事（腳本不做）。
"""
import argparse, datetime, json, os, re, shutil, sys
from pathlib import Path

# python3 -I 不會把腳本所在的資料夾放進 sys.path：明確加上，好讀同資料夾的 workshop_toml（3.9 沒有 tomllib）。
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import workshop_toml as toml  # noqa: E402

AGENTS = ["admin", "code", "architect"]
STATUS = {"idea": "idea", "doing": "doing", "verifying": "review", "done": "done", "cancelled": "cancelled"}
# 要搬的那一張 Goal 與它在工坊的新號（一次性的設定，寫死在這裡）
SRC_GOAL, DST_GOAL = "GOAL-2", "DG-1"
MOVED_NOTE = f"從日常 {SRC_GOAL} 搬來"
NO_RESULT = "（搬到工坊時補上：原本的 DEV 單沒有記結果）"
NO_VERIFY_HOW = "（搬到工坊時補上：原本的 DEV 單沒有記怎麼驗）"
DEV_FILE = re.compile(r"^DEV-([1-9][0-9]*)\.md$")


# ---- frontmatter：只做行層級的增改，不重排、不重新序列化（避免把 vault 的格式改壞） ----
class Doc:
    def __init__(self, text: str):
        m = re.match(r"---\n(.*?\n)---\n?(.*)\Z", text, re.S)
        if not m:
            raise ValueError("沒有 frontmatter")
        self.body = m.group(2)
        self.chunks = []  # [key, [lines]]
        for line in m.group(1).splitlines():
            km = re.match(r"^([A-Za-z_][A-Za-z0-9_]*):", line)
            if km:
                self.chunks.append([km.group(1), [line]])
            elif self.chunks:
                self.chunks[-1][1].append(line)
            else:
                raise ValueError("frontmatter 開頭不是欄位")

    def has(self, key):
        return any(k == key for k, _ in self.chunks)

    def get(self, key) -> str:
        """單行欄位的值（去掉引號）；多行的（區塊字串、清單）回原始第一行冒號後面的字；沒有回空字串。"""
        for k, lines in self.chunks:
            if k == key:
                v = lines[0].split(":", 1)[1].strip()
                if len(lines) == 1 and v[:1] in "\"'":
                    try:
                        return json.loads(v) if v[0] == '"' else v[1:-1].replace("''", "'")
                    except ValueError:
                        return v.strip("\"'")
                return v if v or len(lines) == 1 else "<multi>"
        return ""

    def set(self, key, value, after=None):
        line = f"{key}: {yaml(value)}"
        for c in self.chunks:
            if c[0] == key:
                c[1] = [line]
                return
        idx = len(self.chunks)
        if after:
            for i, (k, _) in enumerate(self.chunks):
                if k == after:
                    idx = i + 1
        self.chunks.insert(idx, [key, [line]])

    def lines(self, key):
        for k, lines in self.chunks:
            if k == key:
                return lines
        return None

    def text(self) -> str:
        head = "\n".join(l for _, lines in self.chunks for l in lines)
        return "---\n" + head + "\n---\n" + self.body


def yaml(v) -> str:
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, int):
        return str(v)
    if isinstance(v, list) and not v:
        return "[]"
    return json.dumps(v, ensure_ascii=False)  # JSON 的雙引號字串就是合法的 YAML


def fill_task(doc: Doc, src_id: str):
    """DEV 單 → 任務單（ai_docs/schemas/task.md）。回傳新的 Doc；不認得的狀態丟 ValueError。"""
    old = doc.get("status")
    if old not in STATUS:
        raise ValueError(f"{src_id} 的狀態 {old!r} 不認得")
    st = STATUS[old]
    doc.set("status", st)
    if not doc.has("type"):
        doc.set("type", "task", after="title")
    if not doc.has("nudges"):
        doc.set("nudges", 0, after="status")
    if not doc.has("links"):
        doc.set("links", [])
    empty = lambda k: doc.get(k) in ("", '""', "''")
    if st in ("review", "done"):
        if empty("result"):
            doc.set("result", NO_RESULT, after="status")
        if empty("verify"):
            doc.set("verify", "unverifiable", after="result")
        if empty("verify_how"):
            doc.set("verify_how", NO_VERIFY_HOW, after="verify")
    if st == "review":
        if empty("waiting_since"):
            doc.set("waiting_since", doc.get("updated_at") or doc.get("created_at"), after="status")
        if empty("proposed"):
            doc.set("proposed", "done", after="verify_how")
    if st in ("done", "cancelled"):
        if empty("closed_at"):
            doc.set("closed_at", doc.get("updated_at"), after="status")
        if empty("accepted_by"):
            by = doc.get("by")
            # DEV 單只有主人能驗收或取消；最後寫入的人是主人就記他，不是就照遷移工具的慣例記 migrate（tasks.LegacyAcceptedBy）。
            doc.set("accepted_by", by if by.startswith("owner(") else "migrate", after="closed_at")
        if empty("accepted_at"):
            doc.set("accepted_at", doc.get("closed_at"), after="accepted_by")
    return doc


# ---- 計畫：先收集動作，預演只印，--apply 才做 ----
class Plan:
    def __init__(self):
        self.actions, self.notes = [], []

    def add(self, desc, fn):
        self.actions.append((desc, fn))

    def note(self, msg):
        self.notes.append(msg)


def write_new(path: Path, text: str, mode_from: Path = None):
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name("." + path.name + ".tmp")
    tmp.write_text(text, encoding="utf-8")
    if mode_from is not None:
        os.chmod(tmp, os.stat(mode_from).st_mode & 0o777)
    os.replace(tmp, path)


def find_any(root: Path, name: str):
    """vault/<種類>/<name> 或 vault/<種類>/archive/<月>/<name>。"""
    if (root / name).exists():
        return root / name
    arch = root / "archive"
    if arch.is_dir():
        for d in sorted(arch.iterdir()):
            if (d / name).exists():
                return d / name
    return None


def dev_files(admin: Path):
    out = []
    if not admin.is_dir():
        return out
    for p in sorted(admin.iterdir()):
        if DEV_FILE.match(p.name):
            out.append((p, None))
    arch = admin / "archive"
    if arch.is_dir():
        for d in sorted(arch.iterdir()):
            if d.is_dir():
                for p in sorted(d.iterdir()):
                    if DEV_FILE.match(p.name):
                        out.append((p, d.name))
    return sorted(out, key=lambda x: int(DEV_FILE.match(x[0].name).group(1)))


def admin_src(daily: Path) -> Path:
    """DEV 單的來源：日常的 vault/admin/；已經搬去備份了就讀最新那份備份（重跑時照樣核對工坊收齊了沒）。"""
    if (daily / "vault" / "admin").is_dir():
        return daily / "vault" / "admin"
    baks = sorted(daily.glob("admin.bak-*-workshop-split"))
    return baks[-1] if baks else daily / "vault" / "admin"


def plan_tasks(plan: Plan, daily: Path, ws: Path):
    tasks = ws / "vault" / "tasks"
    src_dir = admin_src(daily)
    files = dev_files(src_dir)
    new = same = 0
    max_n = 0
    for src, month in files:
        n = int(DEV_FILE.match(src.name).group(1))
        max_n = max(max_n, n)
        if find_any(tasks, src.name):
            same += 1
            continue
        doc = fill_task(Doc(src.read_text(encoding="utf-8")), src.stem)
        dst = tasks / src.name if month is None else tasks / "archive" / month / src.name
        plan.add(f"任務單 {src.stem}（{STATUS[Doc(src.read_text(encoding='utf-8')).get('status')]}）→ {dst.relative_to(ws)}",
                 lambda dst=dst, text=doc.text(), src=src: write_new(dst, text, src))
        new += 1
    where = "" if src_dir.name == "admin" else f"（讀備份 {src_dir.name}）"
    plan.note(f"DEV 單：日常 {len(files)} 張{where}，要搬 {new} 張，工坊已有 {same} 張")
    return len(files), max_n


def plan_counter(plan: Plan, ws: Path, kind: str, at_least: int):
    path = ws / "vault" / ".counters.toml"
    cur = toml.loads(path.read_text(encoding="utf-8")) if path.exists() else {}
    if at_least <= 0 or int(cur.get(kind, 0)) >= at_least:
        return

    def do():
        data = toml.loads(path.read_text(encoding="utf-8")) if path.exists() else {}
        data[kind] = max(int(data.get(kind, 0)), at_least)
        write_new(path, "".join(f"{toml.key(k)} = {int(v)}\n" for k, v in sorted(data.items())))

    plan.add(f"計數器 {kind} 改成 {at_least}（下一張接 {at_least + 1}）", do)


def plan_goal(plan: Plan, daily: Path, ws: Path):
    src = find_any(daily / "vault" / "goals", SRC_GOAL + ".md")
    if src is None:
        plan.note(f"日常沒有 {SRC_GOAL}，Goal 不搬")
        return
    goals = ws / "vault" / "goals"
    have = find_any(goals, DST_GOAL + ".md")
    if have is not None:
        if MOVED_NOTE not in have.read_text(encoding="utf-8"):
            plan.note(f"注意：工坊已經有 {DST_GOAL}，但不是從 {SRC_GOAL} 搬來的；沒有搬，請人看過再決定")
        else:
            plan.note(f"Goal：工坊已有 {DST_GOAL}（從日常 {SRC_GOAL} 搬來）")
        return
    doc = Doc(src.read_text(encoding="utf-8"))
    doc.set("id", DST_GOAL)
    now = datetime.datetime.now().astimezone().isoformat()
    entry = [f"    - at: {yaml(now)}", "      by: architect", f"      note: {yaml(MOVED_NOTE)}"]
    lines = doc.lines("planbook_log")
    if lines is None or lines[0].rstrip().endswith("[]"):
        doc.set("planbook_log", [])
        doc.lines("planbook_log")[:] = ["planbook_log:"] + entry
    else:
        ind = re.match(r"^(\s*)-", lines[1]).group(1) if len(lines) > 1 and re.match(r"^\s*-", lines[1]) else "    "
        lines.extend(l.replace("    ", ind, 1) for l in entry)
    body = doc.body.rstrip("\n")
    doc.body = (body + "\n\n" if body else "") + f"- {now[:10]} {MOVED_NOTE}（原號 {SRC_GOAL}，工坊改號 {DST_GOAL}）。\n"
    plan.add(f"Goal {SRC_GOAL} → 工坊 {DST_GOAL}（日常那份不改）", lambda: write_new(goals / (DST_GOAL + ".md"), doc.text(), src))
    plan_counter(plan, ws, "goal", int(DST_GOAL.split("-")[1]))


def plan_memory(plan: Plan, daily: Path, ws: Path):
    for a in AGENTS:
        sdir, ddir = daily / "vault" / "agents" / a, ws / "vault" / "agents" / a
        if not sdir.is_dir():
            plan.note(f"日常沒有秘書 {a}，她的記憶與日誌不搬")
            continue
        if not (ddir / "agent.md").exists():
            plan.note(f"注意：工坊還沒有秘書 {a}（先跑 scripts/workshop-init.sh），她的記憶與日誌這次不搬")
            continue
        files = [sdir / "memory.md"] if (sdir / "memory.md").exists() else []
        if (sdir / "journal").is_dir():
            files += sorted(p for p in (sdir / "journal").rglob("*.md") if p.is_file())
        new = differ = 0
        for src in files:
            dst = ddir / src.relative_to(sdir)
            if dst.exists():
                if dst.read_bytes() != src.read_bytes():
                    differ += 1
                continue
            plan.add(f"{a}：{src.relative_to(sdir)}", lambda src=src, dst=dst: (dst.parent.mkdir(parents=True, exist_ok=True), shutil.copy2(src, dst)))
            new += 1
        msg = f"{a} 的記憶與日誌：{len(files)} 個檔，要複製 {new} 個"
        if differ:
            msg += f"，{differ} 個工坊那邊已經不一樣（她自己寫過，不蓋）"
        plan.note(msg)


def plan_daily(plan: Plan, daily: Path, ok_to_move: bool):
    admin = daily / "vault" / "admin"
    if not admin.exists():
        plan.note("日常的 vault/admin/ 已經不在（搬過了）")
        return
    stamp = datetime.date.today().strftime("%Y%m%d")
    dst = daily / f"admin.bak-{stamp}-workshop-split"
    if dst.exists():
        plan.note(f"注意：{dst.name} 已經存在，vault/admin/ 沒有搬；請人看過兩邊再決定")
        return
    if not ok_to_move:
        plan.note("工坊還沒收齊日常的 DEV 單：vault/admin/ 這次不搬（先 --apply 工坊那一半）")
        return
    plan.add(f"日常 vault/admin/ → {dst.relative_to(daily)}（照備份規則（system 卡 BSW-4），預設留 30 天）", lambda: os.rename(admin, dst))


def dev_notices(daily: Path):
    out = []
    mdir = daily / "vault" / "messages"
    if not mdir.is_dir():
        return out
    for p in sorted(mdir.glob("M-*.md")):
        try:
            d = Doc(p.read_text(encoding="utf-8"))
        except ValueError:
            continue
        if d.get("task").strip("[]").startswith("DEV-") or re.search(r"-dev-\d+$", d.get("room")):
            out.append(p.stem)
    return sorted(out, key=lambda s: int(s.split("-")[1]))


def manual_steps(daily: Path):
    ids = dev_notices(daily)
    print("\n部署當下要另外做的事（腳本不做；在日常家那台用主人身分，EVERGENIE_HOME=<日常家>）：")
    print("  1. 日常拿掉 admin 秘書：evergenie agents rm admin")
    print("  2. 日常不寫秘書卡（只讓工坊寫）：evergenie config set runtime.cards false")
    print("  3. 日常的自動事件改由日常的秘書處理：evergenie config set incidents.dispatcher '\"system\"'；"
          "evergenie config set incidents.reviewer '\"architect\"'")
    print("  4. 日常不自動升級：evergenie config set update.channel '\"off\"'")
    if ids:
        body = json.dumps({"ids": ids, "by": {"name": "owner(cli)"}}, ensure_ascii=False)
        print(f"  5. 封存日常裡跟 DEV 單有關的通知（{len(ids)} 則：{', '.join(ids)}）：在日常看板逐則刪，或")
        print(f"     curl --unix-socket <日常家>/runtime/sockets/api.sock -X POST http://x/api/notify/archive -d '{body}'")
    else:
        print("  5. 日常沒有跟 DEV 單有關的通知要封存")


def main():
    ap = argparse.ArgumentParser(description="把 evergenie 開發的資料從日常搬進工坊（預設只預演）")
    ap.add_argument("--daily", required=True)
    ap.add_argument("--workshop")
    ap.add_argument("--only", choices=["workshop", "daily"])
    ap.add_argument("--apply", action="store_true")
    a = ap.parse_args()
    daily = Path(a.daily).resolve()
    if not (daily / "vault").is_dir():
        sys.exit(f"日常家沒有 vault/：{daily}")
    do_ws = a.only in (None, "workshop")
    do_daily = a.only in (None, "daily")
    plan = Plan()
    ok_to_move = a.only == "daily"
    if do_ws:
        if not a.workshop:
            sys.exit("要寫工坊就要給 --workshop")
        ws = Path(a.workshop).resolve()
        if ws == daily:
            sys.exit("工坊跟日常是同一個資料夾")
        try:
            ids = toml.loads((ws / "home.toml").read_text(encoding="utf-8")).get("ids", {})
        except (FileNotFoundError, toml.TomlError):
            sys.exit(f"工坊家沒有 home.toml：{ws}（先跑 scripts/workshop-init.sh）")
        if ids.get("tasks") != "DEV-" or ids.get("goals") != "DG-":
            sys.exit('工坊家的 home.toml 要有 [ids] tasks = "DEV-"、goals = "DG-"（不然 DEV 單會被當成別的東西）')
        try:
            total, max_n = plan_tasks(plan, daily, ws)
        except ValueError as e:
            sys.exit(f"停下來，什麼都沒寫：{e}")
        plan_counter(plan, ws, "task", max_n)
        plan_goal(plan, daily, ws)
        plan_memory(plan, daily, ws)
        pending = sum(1 for d, _ in plan.actions if d.startswith("任務單 "))
        ok_to_move = pending == 0 or a.apply

        def verify():
            # 搬日常的 vault/admin/ 之前再核對一次：每張 DEV 單工坊都有。
            missing = [p.stem for p, _ in dev_files(admin_src(daily)) if not find_any(ws / "vault" / "tasks", p.name)]
            if missing:
                sys.exit("工坊少了這些單，日常的 vault/admin/ 沒有搬：" + ", ".join(missing))
    if do_daily:
        plan_daily(plan, daily, ok_to_move)

    print(("套用" if a.apply else "預演（加 --apply 才會寫）") + f"：{len(plan.actions)} 個變動")
    for n in plan.notes:
        print("  · " + n)
    for desc, fn in plan.actions:
        if a.apply and do_ws and desc.startswith("日常 vault/admin/"):
            verify()
        print("  + " + desc)
        if a.apply:
            fn()
    if not plan.actions:
        print("沒有變動")
    if a.apply and do_ws:
        verify()
    manual_steps(daily)


if __name__ == "__main__":
    main()
