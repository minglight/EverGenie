import type { Notification } from "@evergenie/sdk-web";

// 訊息分兩道（D-47、WEB-86）：「秘書」＝秘書跟主人說的話、問的事；「系統」＝系統提醒、套件、排程與測試、部署報告。
// 哪一道由核心算好放在 `lane`（NTF-56），看板不自己認秘書 id；舊版的台沒給 lane 時只照 source 退回。

export type Lane = "agent" | "system";

type LaneFields = Pick<Notification, "lane" | "source">;

/** 這則在哪一道。 */
export function laneOf(n: LaneFields): Lane {
  if (n.lane === "system" || n.lane === "agent") return n.lane;
  return n.source === "system.alert" || n.source === "package" ? "system" : "agent";
}

/**
 * 算不算進選單「訊息」的徽章與今天頁的未讀數（NTF-57）：核心讀出來時算好放在 `badge`，看板只照它數（FEN-N3）。
 * 舊版的台沒給 badge：照那台以前的算法（不是討論串內提問的未讀都算）。
 */
export function countsInBadge(n: Pick<Notification, "badge" | "inline" | "state">): boolean {
  if (n.badge !== undefined) return n.badge;
  return !n.inline && n.state === "unread";
}

/**
 * 同一種的系統通知：標題拿掉 commit 編號（7～40 位、至少一個數字的十六進位）與數字後，再加上誰發的、分類。
 * 例：「每晚測試沒過：8c4e971f（false）」與「每晚測試沒過：c703aef8（false）」是同一種。
 */
export function noticeKind(n: Pick<Notification, "agent" | "category" | "title">): string {
  const title = (n.title ?? "")
    .replace(/\b(?=[0-9a-f]*\d)[0-9a-f]{7,40}\b/gi, "#")
    .replace(/\d+/g, "#")
    .replace(/\s+/g, " ")
    .trim();
  return `${n.agent ?? ""}\n${n.category ?? ""}\n${title}`;
}

/**
 * 把連在一起、同一種的項目收成一組（WEB-86 的「<標題> ×N」）：`keyOf` 回 null 的項目不收（例：還要你回答的）。
 * 回傳每一段；只有一個的段就是單獨一則。順序不變。
 */
export function collapseRuns<T>(items: readonly T[], keyOf: (item: T) => string | null): T[][] {
  const out: T[][] = [];
  let lastKey: string | null = null;
  for (const item of items) {
    const key = keyOf(item);
    const run = out[out.length - 1];
    if (key !== null && key === lastKey && run) run.push(item);
    else out.push([item]);
    lastKey = key;
  }
  return out;
}
