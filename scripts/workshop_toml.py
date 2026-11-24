"""workshop-home.py 與 split-workshop.py 共用的 TOML 讀寫（GOAL-2 S3、S4）。

mini 與 office 的 python3 是 3.9（沒有 tomllib），所以自己讀一個子集：表頭 [a.b]、key = 值（裸的或加引號的 key）、
字串（雙引號含跳脫、單引號）、整數、浮點、布林、陣列（可以跨行）、空的行內表 {}、註解。家的 home.toml、
settings/packages.toml、vault/.counters.toml 都只用到這些；遇到不支援的寫法直接停（不猜）。
"""
import json, re

_BARE = re.compile(r"[A-Za-z0-9_-]+")


class TomlError(ValueError):
    pass


def _strip_comment(line: str) -> str:
    out, q, i = [], None, 0
    while i < len(line):
        c = line[i]
        if q:
            out.append(c)
            if c == "\\" and q == '"' and i + 1 < len(line):
                out.append(line[i + 1]); i += 1
            elif c == q:
                q = None
        elif c in "\"'":
            q = c; out.append(c)
        elif c == "#":
            break
        else:
            out.append(c)
        i += 1
    return "".join(out).strip()


def _keys(s: str):
    parts, s = [], s.strip()
    while s:
        if s[0] == '"':
            m = re.match(r'"((?:[^"\\]|\\.)*)"', s)
            parts.append(json.loads('"' + m.group(1) + '"'))
            s = s[m.end():]
        elif s[0] == "'":
            m = re.match(r"'([^']*)'", s)
            parts.append(m.group(1)); s = s[m.end():]
        else:
            m = _BARE.match(s)
            if not m:
                raise TomlError("看不懂的 key：" + s)
            parts.append(m.group(0)); s = s[m.end():]
        s = s.strip()
        if s.startswith("."):
            s = s[1:].strip()
        elif s:
            raise TomlError("看不懂的 key：" + s)
    return parts


def _value(s: str):
    s = s.strip()
    if s.startswith('"'):
        if s.startswith('"""'):
            raise TomlError("不支援多行字串")
        return json.loads(s)
    if s.startswith("'"):
        return s[1:-1]
    if s in ("true", "false"):
        return s == "true"
    if s.startswith("["):
        inner, items, depth, cur, q = s[1:-1], [], 0, "", None
        for c in inner:
            if q:
                cur += c
                if c == q:
                    q = None
                continue
            if c in "\"'":
                q = c
            if c == "[":
                depth += 1
            if c == "]":
                depth -= 1
            if c == "," and depth == 0:
                if cur.strip():
                    items.append(_value(cur))
                cur = ""
                continue
            cur += c
        if cur.strip():
            items.append(_value(cur))
        return items
    if s == "{}":
        return {}
    if re.fullmatch(r"[+-]?[0-9_]+", s):
        return int(s.replace("_", ""))
    if re.fullmatch(r"[+-]?[0-9_]+\.[0-9_]+", s):
        return float(s.replace("_", ""))
    raise TomlError("不支援的值：" + s)


def loads(text: str) -> dict:
    root, cur, pending = {}, None, ""
    cur = root
    for raw in text.splitlines():
        line = _strip_comment(raw)
        if pending:
            pending += " " + line
            if pending.count("[") > pending.count("]"):
                continue
            line, pending = pending, ""
        if not line:
            continue
        if line.startswith("[["):
            raise TomlError("不支援表格陣列 [[…]]")
        if line.startswith("["):
            cur = root
            for k in _keys(line[1:-1]):
                cur = cur.setdefault(k, {})
            continue
        if "=" not in line:
            raise TomlError("看不懂這一行：" + raw)
        k, v = _split_kv(line)
        if v.startswith("[") and v.count("[") > v.count("]"):
            pending = line
            continue
        keys = _keys(k)
        t = cur
        for p in keys[:-1]:
            t = t.setdefault(p, {})
        t[keys[-1]] = _value(v)
    return root


def _split_kv(line: str):
    q = None
    for i, c in enumerate(line):
        if q:
            if c == q:
                q = None
        elif c in "\"'":
            q = c
        elif c == "=":
            return line[:i].strip(), line[i + 1:].strip()
    raise TomlError("看不懂這一行：" + line)


def load(path) -> dict:
    with open(path, encoding="utf-8") as f:
        return loads(f.read())


# ---- 輸出（字串、整數、布林、陣列、巢狀表） ----
def value(v) -> str:
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, (int, float)):
        return str(v)
    if isinstance(v, str):
        return json.dumps(v, ensure_ascii=False)
    if isinstance(v, list):
        return "[" + ", ".join(value(x) for x in v) + "]"
    if isinstance(v, dict) and not v:
        return "{}"
    raise TomlError("不支援的設定值型別：" + type(v).__name__)


def key(k: str) -> str:
    return k if _BARE.fullmatch(k) else json.dumps(k, ensure_ascii=False)


def dumps(d: dict, prefix=()) -> str:
    out = []
    scalars = [(k, v) for k, v in d.items() if not (isinstance(v, dict) and v)]
    tables = [(k, v) for k, v in d.items() if isinstance(v, dict) and v]
    if prefix and (scalars or not tables):
        out.append("[" + ".".join(key(p) for p in prefix) + "]")
    for k, v in scalars:
        out.append(f"{key(k)} = {value(v)}")
    if scalars or (prefix and not tables):
        out.append("")
    for k, v in tables:
        out.append(dumps(v, prefix + (k,)))
    return "\n".join(out)
