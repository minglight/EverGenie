import { parseIso } from "../../lib/format";
import type { ReminderView } from "./queries";

/** 「未完成」依時間分的組：逾期（今天以前）→ 今天 → 這週（明天到週日）→ 之後 → 未定時間。 */
export type ReminderGroupKey = "late" | "today" | "week" | "later" | "none";
export const reminderGroupOrder: ReminderGroupKey[] = ["late", "today", "week", "later", "none"];

export function groupKeyOf(at: Date | null, now: Date): ReminderGroupKey {
  if (!at) return "none";
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const tomorrow = new Date(today);
  tomorrow.setDate(today.getDate() + 1);
  // 週一是一週的第一天（跟伺服器的「這週」一樣）。
  const nextMonday = new Date(today);
  nextMonday.setDate(today.getDate() + (7 - ((today.getDay() + 6) % 7)));
  if (at.getTime() < today.getTime()) return "late";
  if (at.getTime() < tomorrow.getTime()) return "today";
  if (at.getTime() < nextMonday.getTime()) return "week";
  return "later";
}

/** 網址 ?view= 的舊值（今天、逾期、這週、之後、沒時間、昨天做完）收成現在的三個分頁。 */
export function pageViewOf(raw: string | undefined): ReminderView {
  switch (raw) {
    case "waiting":
      return "waiting";
    case "done":
    case "done_yesterday":
      return "done";
    default:
      return "open";
  }
}

/** 「已完成」只列最近 7 天做完的。 */
export const doneWithinDays = 7;

/**
 * 「已完成」只列最近 7 天做完的：用 closed_at，缺值或零值（0001-01-01）時退到 updated_at，
 * 都沒有就算最近（寧可多列，不要讓做完的待辦無聲消失）。
 */
export function recentlyDone(r: { closed_at?: string; updated_at?: string }, now: Date): boolean {
  const at = [r.closed_at, r.updated_at]
    .map((iso) => parseIso(iso)?.getTime())
    .find((ms) => ms !== undefined && ms > 0);
  if (at === undefined) return true;
  return at >= now.getTime() - doneWithinDays * 86_400_000;
}

/** 搜尋：空白隔開的每個字都要出現（不分大小寫）；空白搜尋＝全部。 */
export function matchesText(text: string, query: string): boolean {
  const hay = text.toLowerCase();
  return query
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((w) => hay.includes(w));
}
