import { type AnswerVia, ApiError, type Notification, type Task } from "@evergenie/sdk-web";
import type { QueryClient } from "@tanstack/react-query";
import { t } from "../i18n/zh-TW";
import { invalidateLive } from "./live";
import { apiFor, PRIMARY, type SiteRef } from "./sites";
import { createStore, useStore } from "./store";
import { itemKey, type WaitingItem } from "./waiting";

// 「就地回答」的狀態（補充契約 S5 第 1 點）：同意／回答／驗收／退回按下就送、不給復原（它會馬上通知秘書）。
// 請求的程式放在畫面外面：卡片按下去就收成綠色一列（元件換掉了），請求還是要跑完、失敗要能還原。
// 多台（S3-7a）：動作送到那筆所在的台（ctx.site，預設主伺服器）；兩個 store 的鍵是 itemKey（主伺服器就是編號）。
// 呼叫前要先確定那台是 ok 的（useSiteApi 拿得到）——這裡不退回主伺服器（FEN-28）。

export type SettledKind = "answer" | "accept" | "reject";

export interface SettledEntry {
  /** itemKey（主伺服器就是編號，別台「台名/編號」）。 */
  id: string;
  kind: SettledKind;
  /** 答案或結果的短字（「好」）。 */
  short: string;
  /** 綠色一列的字：「已回「好」· M-3 標題」。 */
  label: string;
  /** 今天頁的卡片：被移除之後還要在原位置留一下，讓人看到「已回」。 */
  item?: WaitingItem;
  index: number;
  at: number;
}

const settledStore = createStore<ReadonlyMap<string, SettledEntry>>(new Map());
const failureStore = createStore<ReadonlyMap<string, string>>(new Map());

export function useSettled(): ReadonlyMap<string, SettledEntry> {
  return useStore(settledStore);
}

export function useFailures(): ReadonlyMap<string, string> {
  return useStore(failureStore);
}

/** 綠色一列留在畫面上多久（之後伺服器的資料早就更新了）。 */
export const SETTLE_MS = 3000;

function withEntry<K, V>(map: ReadonlyMap<K, V>, key: K, value: V | null): Map<K, V> {
  const next = new Map(map);
  if (value === null) next.delete(key);
  else next.set(key, value);
  return next;
}

function settle(entry: SettledEntry) {
  settledStore.set(withEntry(settledStore.get(), entry.id, entry));
  setTimeout(() => {
    if (settledStore.get().get(entry.id)?.at === entry.at) {
      settledStore.set(withEntry(settledStore.get(), entry.id, null));
    }
  }, SETTLE_MS);
}

function unsettle(id: string) {
  settledStore.set(withEntry(settledStore.get(), id, null));
}

function setFailure(id: string, message: string | null) {
  failureStore.set(withEntry(failureStore.get(), id, message));
}

/** 畫面自己擋下的（例如那台現在連不上，沒有送出）：記成這筆的失敗原因，跟請求失敗顯示在同一個地方。 */
export function reportFailure(key: string, message: string) {
  setFailure(key, message);
}

function reasonOf(err: unknown): string {
  return err instanceof ApiError ? err.message : String(err);
}

export interface SettleContext {
  item?: WaitingItem;
  index?: number;
  /** 那張單在哪台（預設主伺服器）：動作送到那台、「已回」的標記用 itemKey（兩台的單號可能一樣，S3-7b）。 */
  site?: SiteRef;
  /**
   * 「已回」記在別的項目上（DEV-156 U-05）：在任務單上就地回答它的問句時，收成綠色一列的是那張單（鍵、編號用單的），
   * 不是通知本身。不給就記在通知上。
   */
  settleAs?: { key: string; id: string };
}

let tick = 0;
const stamp = () => Date.now() * 1000 + (tick++ % 1000);

/** 回答一則通知（按鈕或打字）。立刻標成已回；失敗就還原並記下原因。 */
export async function answerNotification(
  client: QueryClient,
  n: Notification,
  text: string,
  via: AnswerVia,
  ctx: SettleContext = {},
): Promise<boolean> {
  const id = n.id ?? "";
  // 別台的通知（例如別台對話裡的工作 session 提問）：回答送那台，「已回」標記用 itemKey（S3-7b）。
  const site = ctx.site ?? PRIMARY;
  const key = ctx.settleAs?.key ?? itemKey(site, id);
  setFailure(key, null);
  settle({
    id: key,
    kind: "answer",
    short: text,
    label: t("answer.done", { answer: text, id: ctx.settleAs?.id ?? id, title: n.title ?? "" }),
    item: ctx.item,
    index: ctx.index ?? 0,
    at: stamp(),
  });
  try {
    const res = await apiFor(site).notify.answer({ id, text, via });
    // 送到的時候這則剛好已經被自動收掉（例：催的時候換成新的一則、那件事已經不用回）：伺服器照舊回成功，
    // 但主人的回答沒記上。當成沒送出，留著草稿、說原因，下次重讀就是新的那則（DEV-156 review）。
    const done = res.notification?.answer;
    if (done?.via === "auto") {
      unsettle(key);
      setFailure(key, t("answer.retired", { reason: done.text ?? "" }));
      invalidateLive(client, site);
      return false;
    }
    invalidateLive(client, site);
    return true;
  } catch (err) {
    unsettle(key);
    setFailure(key, t("answer.failed", { reason: reasonOf(err) }));
    return false;
  }
}

/** 驗收一張要你驗收的任務單。 */
export async function acceptTask(
  client: QueryClient,
  task: Task,
  ctx: SettleContext = {},
): Promise<boolean> {
  const id = task.id ?? "";
  const site = ctx.site ?? PRIMARY;
  const key = itemKey(site, id);
  setFailure(key, null);
  settle({
    id: key,
    kind: "accept",
    short: t("task.accept"),
    label: t("answer.task.accepted", { id, title: task.title ?? "" }),
    item: ctx.item,
    index: ctx.index ?? 0,
    at: stamp(),
  });
  try {
    await apiFor(site).tasks.accept({ id });
    invalidateLive(client, site);
    return true;
  } catch (err) {
    unsettle(key);
    setFailure(key, t("answer.failed", { reason: reasonOf(err) }));
    return false;
  }
}

/** 退回一張任務單（理由必填，會通知秘書）。 */
export async function rejectTask(
  client: QueryClient,
  task: Task,
  note: string,
  ctx: SettleContext = {},
): Promise<boolean> {
  const id = task.id ?? "";
  const site = ctx.site ?? PRIMARY;
  const key = itemKey(site, id);
  setFailure(key, null);
  settle({
    id: key,
    kind: "reject",
    short: t("task.reject"),
    label: t("answer.task.rejected", { id, title: task.title ?? "" }),
    item: ctx.item,
    index: ctx.index ?? 0,
    at: stamp(),
  });
  try {
    await apiFor(site).tasks.reject({ id, note });
    invalidateLive(client, site);
    return true;
  } catch (err) {
    unsettle(key);
    setFailure(key, t("answer.failed", { reason: reasonOf(err) }));
    return false;
  }
}

/** 開這張單的專屬對話（在那張單所在的台），回傳對話的名稱（去 /chat/<名稱>，別台加 ?home=）。 */
export async function openTaskRoom(task: Task, site: SiteRef = PRIMARY): Promise<string> {
  const res = await apiFor(site).session.open({ agent: task.agent || undefined, topic: task.id });
  const name = res.session?.name;
  if (!name) throw new Error(t("task.openRoom.failed"));
  return name;
}

/** 把「剛處理完、還沒從伺服器的清單消失」的項目放回原本的位置。 */
export function mergeSettled(
  items: WaitingItem[],
  settled: ReadonlyMap<string, SettledEntry>,
): WaitingItem[] {
  const present = new Set(items.map((i) => i.key));
  const missing = [...settled.values()]
    .filter((e) => e.item && !present.has(e.id))
    .sort((a, b) => a.index - b.index);
  if (missing.length === 0) return items;
  const out = [...items];
  for (const e of missing) {
    if (e.item) out.splice(Math.min(e.index, out.length), 0, e.item);
  }
  return out;
}
