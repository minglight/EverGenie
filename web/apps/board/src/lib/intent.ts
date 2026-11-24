import { parseWhen } from "./when";

// 萬用輸入框（QuickInput）與指令面板共用的解析（補充契約 S5 第 2、3 點）：
// 開頭是 @ → 傳話給秘書；開頭是「交代」→ 開任務單（RMD-22）；其他 → 新增待辦（時間從句子裡抓）。

export interface AgentRef {
  id: string;
  name: string;
}

export type Intent =
  | { kind: "empty" }
  | {
      kind: "todo";
      content: string;
      /** 解析出來的時間；沒有就是 null（交給伺服器套預設時間）。 */
      at: Date | null;
      /** 從句子裡拿掉的時間原文（顯示用）。 */
      matched: string | null;
      /** 開頭是「交代」：這句是交代任務（開任務單），不是待辦；content 已拿掉「交代」（RMD-22）。 */
      task?: boolean;
    }
  | {
      kind: "say";
      /** @ 後面打的名字（可能還沒打完）。 */
      query: string;
      /** 對到的秘書；null＝還沒打或找不到。 */
      agent: AgentRef | null;
      /** 名字之後的內容。 */
      body: string;
      /** 名字還在打的時候，符合的秘書（讓人點）。 */
      suggestions: AgentRef[];
      /** 名字打完了（後面已經有空白）。 */
      named: boolean;
    };

export interface IntentOptions {
  now?: Date;
  /** 使用者按了時間膠囊的 ×：不要從句子裡抓時間。 */
  ignoreWhen?: boolean;
}

const norm = (s: string) => s.toLocaleLowerCase();

/**
 * 前綴比對用的 id：別台的秘書 id 是「台名/職位 id」（看板連多台），只比「/」後面的職位 id——
 * 打台名的字頭不會對到那台隨便一位；「台名/職位 id」要整串打完才對得到（完全相同）。
 */
const idTail = (id: string) => norm(id.slice(id.indexOf("/") + 1));
const startsLike = (a: AgentRef, q: string) =>
  norm(a.name).startsWith(q) || idTail(a.id).startsWith(q);

/** 對人名或職位 id（不分大小寫）：完全相同優先，其次前綴。 */
export function matchAgent(query: string, agents: AgentRef[]): AgentRef | null {
  const q = norm(query);
  if (q === "") return null;
  const exact = agents.find((a) => norm(a.name) === q || norm(a.id) === q);
  if (exact) return exact;
  return agents.find((a) => startsLike(a, q)) ?? null;
}

export function parseIntent(
  input: string,
  agents: AgentRef[],
  options: IntentOptions = {},
): Intent {
  const text = input.trim();
  if (text === "") return { kind: "empty" };

  if (text.startsWith("@") || text.startsWith("＠")) {
    const afterAt = text.slice(1);
    const split = /[\s　]/.exec(afterAt);
    const query = split ? afterAt.slice(0, split.index) : afterAt;
    const body = split ? afterAt.slice(split.index).trim() : "";
    const named = split !== null;
    const q = norm(query);
    const suggestions = named ? [] : agents.filter((a) => q === "" || startsLike(a, q));
    return { kind: "say", query, agent: matchAgent(query, agents), body, suggestions, named };
  }

  // 「交代 整理照片」「交代：整理照片」：交代任務＝開任務單（跟輸入框底下的「交代任務」入口同一件事）；任務單沒有時間，不抓。
  const assign = /^交代[\s　:：]+/.exec(text);
  if (assign) {
    const content = text.slice(assign[0].length).trim();
    return { kind: "todo", content, at: null, matched: null, task: true };
  }

  const parsed = options.ignoreWhen ? null : parseWhen(text, options.now);
  return {
    kind: "todo",
    content: parsed ? parsed.rest : text,
    at: parsed ? parsed.date : null,
    matched: parsed ? parsed.matched : null,
  };
}
