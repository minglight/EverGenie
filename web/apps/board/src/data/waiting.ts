import type {
  Notification,
  Reminder,
  Task,
  ThreadSummary,
  WebGetOverviewResponse,
} from "@evergenie/sdk-web";
import { preview } from "../lib/format";
import { isDelivery } from "../lib/taskText";
import { matchAgent } from "./agentFilter";
import { agentKey } from "./agents";
import { PRIMARY, type SiteRef } from "./sites";

export type WaitingKind = "approve" | "review" | "ask";

/**
 * 一筆資料跨台不撞的鍵（網址 ?sel=、React key、剛處理完的綠色一列）：主伺服器就是編號（跟 S3 之前一樣），
 * 別台是「台名/編號」——兩台都有 M-3 時是 `M-3` 與 `amy-office/M-3`。寫法跟 agentKey 一樣。
 */
export function itemKey(site: Pick<SiteRef, "key">, id: string): string {
  return agentKey(site.key, id);
}

export interface WaitingItem {
  /** 通知或任務單自己的編號（那台的）。 */
  id: string;
  /** 跨台不撞的鍵（`itemKey`）：網址 ?sel= 與 React key 用它；主伺服器就是 id。 */
  key: string;
  /** 這筆在哪一台（主伺服器是 PRIMARY）：動作送到這台、連結帶它的 ?home=。 */
  site: SiteRef;
  kind: WaitingKind;
  title: string;
  /** 說明全文（可能是空字串）。 */
  detail: string;
  agent: string;
  /** 通知的建立時間／任務單開始等的時間。 */
  at?: string;
  /** 任務單：伺服器算的等待天數。 */
  waitingDays: number;
  target: { to: "/messages/$id" | "/tasks/$id"; params: { id: string } };
  /** 要你同意的通知（approve）：就地回答要用它的選項。 */
  notification?: Notification;
  /** 任務單（要你驗收／要你回答）：就地驗收、退回、去聊要用它。 */
  task?: Task;
}

/**
 * 通知沒有背景、請你時退回用 body（NTF-63）：body 的第一行是 notify 組的 `[職位｜帳號] 類型｜標題`，
 * 跟卡片上的標題、頭像重複（舊的還會是 `[code｜]` 這種空一格的），不顯示；空的「背景：」「請你：」也拿掉。
 */
export function bodyDetail(body: string | undefined): string {
  const lines = (body ?? "").split("\n");
  // 只認 notify 組的那種：`[…] 類型｜標題`（後面一定有「類型｜」），秘書自己寫的「[重要] …」照留。
  if (/^\[[^\]\n]*\] [^｜\n]+｜/.test(lines[0] ?? "")) lines.shift();
  return lines
    .filter((l) => !/^(背景|請你)：\s*$/.test(l))
    .join("\n")
    .trim();
}

function fromNotification(n: Notification, site: SiteRef): WaitingItem {
  const id = n.id ?? "";
  return {
    id,
    key: itemKey(site, id),
    site,
    kind: "approve",
    title: n.title ?? "",
    detail: [n.context, n.todo].filter(Boolean).join("\n\n") || bodyDetail(n.body),
    agent: n.agent ?? "",
    at: n.created_at,
    waitingDays: 0,
    target: { to: "/messages/$id", params: { id } },
    notification: n,
  };
}

function fromTask(task: Task, waitingDays: number, site: SiteRef): WaitingItem {
  const id = task.id ?? "";
  const review = isDelivery(task);
  return {
    id,
    key: itemKey(site, id),
    site,
    kind: review ? "review" : "ask",
    title: task.title ?? "",
    detail: (review ? task.result : task.ask) || task.ask || task.result || task.body || "",
    agent: task.agent ?? "",
    at: task.waiting_since,
    waitingDays,
    target: { to: "/tasks/$id", params: { id } },
    task,
  };
}

/** 總覽的「等你處理」清單：要你同意的通知，再來是等你的任務單（數字和內容都是伺服器算的）。`o` 是 `site` 那台的。 */
export function waitingItems(
  o: WebGetOverviewResponse | undefined,
  site: SiteRef = PRIMARY,
): WaitingItem[] {
  if (!o) return [];
  return [
    ...(o.approvals ?? []).map((n) => fromNotification(n, site)),
    ...(o.waiting_tasks ?? []).flatMap((w) =>
      w.task ? [fromTask(w.task, w.waiting_days, site)] : [],
    ),
  ];
}

export function itemPreview(item: WaitingItem): string {
  return preview(item.detail, 60);
}

/** 秘書篩選之後的「等你處理」件數；沒選任何人就是伺服器算的 waiting_total（FEN-N3：全部的數字不在前端算）。 */
export function filteredWaitingTotal(
  o: WebGetOverviewResponse | undefined,
  picked: readonly string[],
  site: SiteRef = PRIMARY,
): number | undefined {
  if (!o) return undefined;
  if (picked.length === 0) return o.waiting_total;
  return waitingItems(o, site).filter((i) => matchAgent(picked, i.agent, site.key)).length;
}

/**
 * 今天頁的徽章：每位秘書的「等你處理」件數，跟頁首大字同一個定義（`filteredWaitingTotal`）。
 * 等你處理的聊天有聊天分頁的黃點、今天的待辦有自己的段落，都不算進來（REQ-155）。
 */
export function waitingCounts(
  o: WebGetOverviewResponse | undefined,
  site: SiteRef = PRIMARY,
): {
  counts: Record<string, number>;
  total: number;
} {
  const counts: Record<string, number> = {};
  for (const id of new Set(waitingItems(o, site).map((i) => i.agent))) {
    if (!id) continue;
    const key = agentKey(site.key, id);
    counts[key] = filteredWaitingTotal(o, [key], site) ?? 0;
  }
  return { counts, total: o?.waiting_total ?? 0 };
}

/** 一組秘書（空＝全部）手上的事：要你同意、要你驗收或決定、等你處理的串、今天的待辦。 */
export interface AgentWork {
  items: WaitingItem[];
  threads: ThreadSummary[];
  reminders: Reminder[];
}

/** 今天頁與討論串右側角色欄共用同一份過濾（REQ-155、REQ-156）；數字與內容都是伺服器算好的。 */
export function workFor(
  o: WebGetOverviewResponse | undefined,
  picked: readonly string[],
  site: SiteRef = PRIMARY,
): AgentWork {
  return {
    items: waitingItems(o, site).filter((i) => matchAgent(picked, i.agent, site.key)),
    // 舊版那台會把只有「排隊等名額」的串也放進來：不是在等你，不列（S5 D6）。
    threads: (o?.waiting_threads ?? []).filter(
      (th) => !onlyWorkQueue(th) && matchAgent(picked, th.agent, site.key),
    ),
    reminders: (o?.today_reminders ?? []).filter((r) => matchAgent(picked, r.owner, site.key)),
  };
}

/** 只有「排隊等名額」（舊版那台送 waiting_user＋["work_queue"]）：不是在等你（S5 D6）。 */
function onlyWorkQueue(th: ThreadSummary): boolean {
  return th.waiting?.length === 1 && th.waiting[0] === "work_queue";
}
