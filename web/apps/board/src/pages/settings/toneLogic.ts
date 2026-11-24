import type { PersonaTestRun } from "@evergenie/sdk-web";

// 測口吻（P6-5b、REQ-130）的純函式：把結果的 Markdown 拆成情境、兩份對齊、草稿卡全文、按鈕狀態。

export interface ToneSection {
  id: string;
  title: string;
  /** 送進去的訊息（引用的那幾行，去掉「> 」）。 */
  quote: string;
  /** 模型的回覆（或「（這則沒跑成：…）」）。 */
  reply: string;
}

/** 結果檔的內文：`## <id>｜<標題>`、`> ` 引用的訊息、回覆。檔頭（第一個 ## 之前）原樣放 head。 */
export function parseToneMarkdown(markdown: string): { head: string; sections: ToneSection[] } {
  const lines = markdown.replaceAll("\r\n", "\n").split("\n");
  const headLines: string[] = [];
  const sections: ToneSection[] = [];
  let cur: { id: string; title: string; body: string[] } | null = null;
  const flush = () => {
    if (!cur) return;
    const quote: string[] = [];
    const reply: string[] = [];
    for (const line of cur.body) {
      if (reply.length === 0 && (line.startsWith("> ") || line === ">")) quote.push(line.slice(2));
      else if (reply.length === 0 && line.trim() === "") continue;
      else reply.push(line);
    }
    sections.push({
      id: cur.id,
      title: cur.title,
      quote: quote.join("\n").trim(),
      reply: reply.join("\n").trim(),
    });
    cur = null;
  };
  for (const line of lines) {
    if (line.startsWith("## ")) {
      flush();
      const [id = "", ...rest] = line.slice(3).split("｜");
      cur = { id: id.trim(), title: rest.join("｜").trim(), body: [] };
    } else if (cur) {
      cur.body.push(line);
    } else {
      headLines.push(line);
    }
  }
  flush();
  return { head: headLines.join("\n").trim(), sections };
}

export interface ToneRow {
  id: string;
  title: string;
  quote: string;
  left?: string;
  right?: string;
}

/** 兩份結果依情境 id 對齊：左邊的順序在前，只有右邊有的接在後面；某邊沒有該情境就是 undefined。 */
export function alignRuns(left: ToneSection[], right: ToneSection[]): ToneRow[] {
  const rows: ToneRow[] = left.map((s) => ({
    id: s.id,
    title: s.title,
    quote: s.quote,
    left: s.reply,
    right: right.find((r) => r.id === s.id)?.reply,
  }));
  for (const s of right) {
    if (!left.some((l) => l.id === s.id)) {
      rows.push({ id: s.id, title: s.title, quote: s.quote, right: s.reply });
    }
  }
  return rows;
}

/** 勾選並排比較：最多兩份，再勾第三份就換掉最早勾的那份。 */
export function togglePick(picks: string[], run: string): string[] {
  if (picks.includes(run)) return picks.filter((p) => p !== run);
  return [...picks, run].slice(-2);
}

/** 看板上編輯中、還沒存的內容，包成帶檔頭的卡全文（伺服器取內文與名字）。 */
export function draftCard(name: string, body: string): string {
  return `---\nname: ${JSON.stringify(name.trim())}\n---\n${body}`;
}

export type ToneButton = "ready" | "running" | "unsupported";

/** 「測口吻」按鈕的狀態：引擎不支援一次性對話（伺服器說的）→ 停用；有一份在跑 → 測試中；其他可按。 */
export function toneButton(opts: {
  runs: readonly Pick<PersonaTestRun, "status">[];
  starting: boolean;
  unsupported: boolean;
}): ToneButton {
  if (opts.unsupported) return "unsupported";
  if (opts.starting || opts.runs.some((r) => r.status === "running")) return "running";
  return "ready";
}

/** 伺服器說「這個引擎不支援一次性對話」。 */
export function isUnsupportedReason(reason: string): boolean {
  return reason.includes("不支援一次性對話");
}
