import type { Task } from "@evergenie/sdk-web";
import { openStatuses } from "../../data/queries";
import { isDelivery } from "../../lib/taskText";

/**
 * 任務單頁的分頁（DEV-107，作者 2026-11-03 18:22 定：分頁太多，收成兩個＋狀態標籤）：
 * 「未結」一條清單照狀態排（等你 → 進行中 → 等外部 → 待做），上面一排狀態標籤可複選篩選（?status=）；「已結束」另外讀。
 * 網址 ?view= 的舊值照舊能開：waiting 轉成「未結」＋「等你」篩選，active、all 轉成「未結」。
 */
export type TaskPageView = "open" | "closed";
export const taskPageViews: readonly TaskPageView[] = ["open", "closed"];

export type OpenStatus = (typeof openStatuses)[number];

/** 網址上 ?view= 認得的值：新的兩個，加上舊網址（以前的六個分頁）。 */
export const taskViewParams = [
  "open",
  "closed",
  "all",
  "waiting",
  "active",
  "idea",
  "external",
] as const;
type TaskViewParam = (typeof taskViewParams)[number];

// 舊分頁開進來順便帶上的篩選（waiting 是作者定的；idea、external 只在 DEV-107 分支上出現過，順手對上）。
const legacyStatus: Partial<Record<TaskViewParam, OpenStatus>> = {
  waiting: "waiting_user",
  idea: "idea",
  external: "waiting_external",
};

/** ?status=waiting_user,idea → 認得的未結狀態（照標籤的順序、去重）；沒有就是空的（＝全部）。 */
export function parseStatuses(raw: unknown): OpenStatus[] {
  if (typeof raw !== "string" && !Array.isArray(raw)) return [];
  const words = (Array.isArray(raw) ? raw.join(",") : raw).split(",").map((w) => w.trim());
  return openStatuses.filter((s) => words.includes(s));
}

/** 網址 → 頁面看的分頁與篩選。舊的 ?view=waiting 開進來是「未結」＋「等你」。 */
export function taskSearchOf(view: unknown, status: unknown): { view?: "closed"; status?: string } {
  const v = taskViewParams.find((p) => p === view);
  if (v === "closed") return { view: "closed" };
  const picked = parseStatuses(status);
  const legacy = v ? legacyStatus[v] : undefined;
  const statuses = picked.length > 0 ? picked : legacy ? [legacy] : [];
  return statuses.length > 0 ? { status: statuses.join(",") } : {};
}

/** 一張單在不在選的狀態裡；一個都沒選＝全部。 */
export function inStatuses(picked: readonly OpenStatus[], task: Pick<Task, "status">): boolean {
  return picked.length === 0 || picked.some((s) => s === task.status);
}

/** 點一個狀態標籤：沒選就加進去、選了就拿掉；回傳照標籤順序。 */
export function toggleStatus(picked: readonly OpenStatus[], s: OpenStatus): OpenStatus[] {
  const next = picked.includes(s) ? picked.filter((p) => p !== s) : [...picked, s];
  return openStatuses.filter((p) => next.includes(p));
}

const ms = (iso: string | undefined, missing: number) => {
  const n = iso ? Date.parse(iso) : Number.NaN;
  return Number.isNaN(n) ? missing : n;
};

/**
 * 「未結」的排法：先照狀態（等你 → 進行中 → 等外部 → 待做）；
 * 等你：送驗收的在要回覆的前面，同一種等最久的在前（waiting_since）；進行中、等外部：最近更新的在前；
 * 待做：開單早的在前（放久的不沉掉）。
 */
export function compareOpen(a: Task, b: Task): number {
  const rank = (t: Task) => {
    const i = openStatuses.indexOf(t.status as OpenStatus);
    return i < 0 ? openStatuses.length : i;
  };
  const byStatus = rank(a) - rank(b);
  if (byStatus !== 0) return byStatus;
  switch (a.status) {
    case "waiting_user":
      return (
        Number(isDelivery(b)) - Number(isDelivery(a)) ||
        ms(a.waiting_since, Number.MAX_SAFE_INTEGER) - ms(b.waiting_since, Number.MAX_SAFE_INTEGER)
      );
    case "idea":
      return ms(a.created_at, Number.MAX_SAFE_INTEGER) - ms(b.created_at, Number.MAX_SAFE_INTEGER);
    default:
      return ms(b.updated_at, 0) - ms(a.updated_at, 0);
  }
}

/** 「已結束」：最近結的在前。 */
export function compareClosed(a: Task, b: Task): number {
  return ms(b.closed_at || b.updated_at, 0) - ms(a.closed_at || a.updated_at, 0);
}

/** 搜尋：空白隔開的每個字都要出現在編號、標題、需求或結果裡（不分大小寫）；找已結束的單，結果欄最有線索。 */
export function matchesQuery(
  task: Pick<Task, "id" | "title" | "body" | "result">,
  query: string,
): boolean {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return true;
  const hay =
    `${task.id ?? ""} ${task.title ?? ""} ${task.body ?? ""} ${task.result ?? ""}`.toLowerCase();
  return terms.every((w) => hay.includes(w));
}
