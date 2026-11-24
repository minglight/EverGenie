import type { RulesDiffLine, RulesVersionSource } from "@evergenie/sdk-web";
import type { TextKey } from "../../../i18n/zh-TW";

// 規矩頁（S7、D-44）的純函式：把文件切成一條一條、差異收合沒變的行。

export interface RuleItem {
  /** 這一條的標題（不含 #）；文件開頭沒有標題的那段是空的。 */
  heading: string;
  level: number;
  /** 內文（不含標題那行）。 */
  body: string;
}

const HEADING = /^(#{1,6})[ \t]+(.*?)[ \t#]*$/;
const FENCE = /^ {0,3}(```|~~~)/;

/**
 * 把文件切成一條一條給人讀：用文件裡最高一級的標題切（法律是「### 第 n 條」，憲法是「## 第 n 條」…），
 * 每條包含它底下的小標題；程式碼區塊裡的 # 不算。開頭沒有標題的文字自成一條（heading 空）。
 */
export function splitItems(text: string): RuleItem[] {
  const lines = text.replaceAll("\r\n", "\n").split("\n");
  let top = 7;
  let fence = "";
  const marks: { at: number; level: number; heading: string }[] = [];
  lines.forEach((line, at) => {
    const f = FENCE.exec(line);
    if (fence) {
      if (f && f[1] === fence) fence = "";
      return;
    }
    if (f) {
      fence = f[1] ?? "";
      return;
    }
    const h = HEADING.exec(line);
    if (h) {
      const level = h[1]?.length ?? 7;
      marks.push({ at, level, heading: (h[2] ?? "").trim() });
      top = Math.min(top, level);
    }
  });
  const cuts = marks.filter((m) => m.level === top);
  const items: RuleItem[] = [];
  const intro = lines
    .slice(0, cuts[0]?.at ?? lines.length)
    .join("\n")
    .trim();
  if (intro) items.push({ heading: "", level: 0, body: intro });
  cuts.forEach((c, i) => {
    const end = cuts[i + 1]?.at ?? lines.length;
    items.push({
      heading: c.heading,
      level: c.level,
      body: lines
        .slice(c.at + 1, end)
        .join("\n")
        .trim(),
    });
  });
  return items;
}

export type DiffSegment =
  | { kind: "line"; line: RulesDiffLine }
  | { kind: "skip"; count: number; key: number };

/** 差異只留改動附近的行：沒變的行連續超過 context*2+1 行就收成「N 行沒變」。 */
export function collapseDiff(diff: RulesDiffLine[], context = 2): DiffSegment[] {
  const changed = diff.map((l) => l.op === "add" || l.op === "del");
  const keep = diff.map((_, i) => {
    for (let d = -context; d <= context; d++) if (changed[i + d]) return true;
    return false;
  });
  const out: DiffSegment[] = [];
  let skipped = 0;
  diff.forEach((line, i) => {
    if (keep[i]) {
      if (skipped) out.push({ kind: "skip", count: skipped, key: i });
      skipped = 0;
      out.push({ kind: "line", line });
    } else {
      skipped++;
    }
  });
  if (skipped) out.push({ kind: "skip", count: skipped, key: diff.length });
  return out;
}

/** 差異有幾行加上、幾行拿掉。 */
export function diffCounts(diff: RulesDiffLine[]): { add: number; del: number } {
  let add = 0;
  let del = 0;
  for (const l of diff) {
    if (l.op === "add") add++;
    if (l.op === "del") del++;
  }
  return { add, del };
}

export const sourceKey = {
  save: "rules.source.save",
  restore: "rules.source.restore",
  proposal: "rules.source.proposal",
  outside: "rules.source.outside",
} as const satisfies Record<RulesVersionSource, TextKey>;
