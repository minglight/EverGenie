// 夜班清單（P6-6）的純文字解析與改寫：vault 的 note `system/night-queue`，內容是 Markdown，
// 三段「待做」「要主人」「做完（留最近 14 天）」，項目是 `- [ ] 做什麼｜加入 YYYY-MM-DD`。
// 格式跟命令列 `evergenie night`（internal/cli/night.go）一致；看板只新增與刪除「待做」，搬段是秘書用指令做。

export const NIGHT_KIND = "note";
export const NIGHT_ID = "system/night-queue";

const TODO = "## 待做";
const BLOCKED = "## 要主人";
const DONE = "## 做完（留最近 14 天）";

/** 檔案還不存在時第一次寫入的內容（跟 night.go 的 nightTemplate 同一份文字）。 */
export const nightTemplate = `# 夜班清單

主人排好、要在「帳號每週額度重置前最後兩晚」做的事。放進來的才會做，沒放就不做。
做法與停止條件寫在 system 的夜班排程（排程 night-shift 的內文）；這個家有那個排程才會自動做，沒有就請電腦秘書（system）設。

## 怎麼放
一項一行，由上往下做。跟任何一位秘書說「放進夜班清單」，對方就會用 \`evergenie night add "<做什麼>"\` 加在「待做」：
\`- [ ] 做什麼（一句話；要讀哪份文件、產出放哪裡、做到什麼程度）｜加入 YYYY-MM-DD\`
適合：吃額度但不急、做完可以事後檢查的事（整理、review、補測試、寫文件、研究）。
不適合（夜班會跳過並移到「要主人」）：要 sudo／密碼、要主人決定、花點數或錢、對外寫入（push、寄信）。

${TODO}

${BLOCKED}

${DONE}
`;

interface Section {
  heading: string;
  lines: string[];
}

const itemStart = /^- \[( |x|X)\] /;

function parse(body: string): Section[] {
  const secs: Section[] = [{ heading: "", lines: [] }];
  for (const line of body.replace(/\r\n/g, "\n").replace(/\n+$/, "").split("\n")) {
    if (line.startsWith("## ")) secs.push({ heading: line, lines: [] });
    else (secs[secs.length - 1] as Section).lines.push(line);
  }
  return secs;
}

function trimBlank(lines: string[]): string[] {
  let a = 0;
  let b = lines.length;
  while (a < b && (lines[a] ?? "").trim() === "") a++;
  while (b > a && (lines[b - 1] ?? "").trim() === "") b--;
  return lines.slice(a, b);
}

function render(secs: Section[]): string {
  const out: string[] = [];
  for (const s of secs) {
    if (s.heading) out.push(s.heading);
    out.push(...trimBlank(s.lines), "");
  }
  return out.join("\n");
}

function find(secs: Section[], canonical: string): Section {
  const prefix = canonical.replace("（留最近 14 天）", "");
  const found = secs.find(
    (s) =>
      s.heading === prefix ||
      s.heading.startsWith(`${prefix}（`) ||
      s.heading.startsWith(`${prefix} `),
  );
  if (found) return found;
  const created = { heading: canonical, lines: [] };
  secs.push(created);
  return created;
}

interface Range {
  start: number;
  end: number;
}

function itemsOf(s: Section): Range[] {
  const out: Range[] = [];
  for (let i = 0; i < s.lines.length; i++) {
    if (!itemStart.test(s.lines[i] ?? "")) continue;
    let end = i + 1;
    while (end < s.lines.length) {
      const line = s.lines[end] ?? "";
      if (itemStart.test(line) || line === "" || !(line.startsWith(" ") || line.startsWith("\t")))
        break;
      end++;
    }
    out.push({ start: i, end });
    i = end - 1;
  }
  return out;
}

export interface NightEntry {
  /** 做什麼（第一個「｜」之前）。 */
  text: string;
  /** 後面的欄位：加入日期、結果、卡住原因、完成日期。 */
  meta: string[];
  /** 縮排的進度說明。 */
  notes: string[];
}

export interface NightView {
  todo: NightEntry[];
  blocked: NightEntry[];
  done: NightEntry[];
}

function entries(s: Section): NightEntry[] {
  return itemsOf(s).map((r) => {
    const [text = "", ...meta] = (s.lines[r.start] ?? "").replace(itemStart, "").split("｜");
    return {
      text: text.trim(),
      meta: meta.map((m) => m.trim()).filter(Boolean),
      notes: s.lines.slice(r.start + 1, r.end).map((l) => l.trim()),
    };
  });
}

/** 三段的項目；body 空就是三段都空。 */
export function viewOf(body: string | undefined): NightView {
  const secs = parse(body ?? "");
  return {
    todo: entries(find(secs, TODO)),
    blocked: entries(find(secs, BLOCKED)),
    done: entries(find(secs, DONE)),
  };
}

/** 一行文字：換行和全形直線換掉、空白收乾淨。空的回 ""。 */
export function oneLine(s: string): string {
  return s.replace(/｜/g, "|").split(/\s+/).filter(Boolean).join(" ");
}

const pad = (n: number) => String(n).padStart(2, "0");
export const dayText = (d: Date) =>
  `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

/** 加在「待做」最下面；body 為 undefined（檔案還沒有）就用範本。內容是空的回 null。 */
export function addTodo(
  body: string | undefined,
  what: string,
  today: Date,
  details: string[] = [],
): string | null {
  const text = oneLine(what);
  if (!text) return null;
  const secs = parse(body ?? nightTemplate);
  const todo = find(secs, TODO);
  todo.lines = [
    ...trimBlank(todo.lines),
    `- [ ] ${text}｜加入 ${dayText(today)}`,
    // 補充欄位放成縮排的說明行（跟進度說明同格式），夜班的秘書 night list 看得到。
    ...details
      .map(oneLine)
      .filter(Boolean)
      .map((d) => `  ${d}`),
  ];
  return render(secs);
}

/** 刪掉「待做」的第 n 項（從 1 算，連它的進度說明）；序號不對回 null。 */
export function removeTodo(body: string | undefined, n: number): string | null {
  const secs = parse(body ?? nightTemplate);
  const todo = find(secs, TODO);
  const range = itemsOf(todo)[n - 1];
  if (!range) return null;
  todo.lines = [...todo.lines.slice(0, range.start), ...todo.lines.slice(range.end)];
  return render(secs);
}
