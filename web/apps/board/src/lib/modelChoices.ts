// 模型與思考力度的選項（引擎認得的別名）：設定的秘書下拉（settings/functions.ts）與聊天的「/」指令（pages/chat/slash.ts）共用這一份。
// 核心不認這些字（法律第 3 條）；引擎認不得會回「沒有這個選項」。

/** 模型的別名。 */
export const MODEL_CHOICES = ["sonnet", "opus", "fable", "haiku"] as const;

/** 思考力度的等級（不含「沒指定」與「交給 AI 自己決定」，各頁自己加）。 */
export const EFFORT_LEVELS = ["low", "medium", "high", "xhigh", "max"] as const;

// 畫面上的叫法（作者 2026-11-03「顯示 extra」）：xhigh 寫成 extra；送給引擎的還是原字。
const EFFORT_WORDS: Record<string, string> = { xhigh: "extra" };

/** 力度在畫面上怎麼寫（選單、標題欄、提示）。 */
export function effortWord(level: string): string {
  return EFFORT_WORDS[level] ?? level;
}

/** 主人打的字 → 引擎的字（「/effort extra」＝xhigh）；不認得就原樣。 */
export function effortFromWord(word: string): string {
  return Object.entries(EFFORT_WORDS).find(([, w]) => w === word)?.[0] ?? word;
}
