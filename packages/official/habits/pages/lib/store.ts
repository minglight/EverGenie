// 習慣套件讀寫資料的地方：全部走 @evergenie/sdk-web 的 api.vault（D-28，沒有套件自己的 RPC）。
// 頁面不算點數：status、wallet 由套件的程式（evergenie-habits）算好寫進 vault，頁面只讀。
import { api } from "@evergenie/sdk-web";
import {
  checkinKey,
  type Habit,
  type HabitStatus,
  type RecordLike,
  sortHabits,
  statusId,
  TASK_POINTS,
  toHabit,
  toStatus,
  toWallet,
  type Wallet,
} from "./habits";

export const HABIT = "habits.habit";
export const CHECKIN = "habits.checkin";
export const STATUS = "habits.status";
export const WALLET = "habits.wallet";

export async function loadHabits(): Promise<Habit[]> {
  const r = await api.vault.list({ kind: HABIT, include_archived: false });
  return sortHabits((r.records ?? []).map((x: RecordLike) => toHabit(x)));
}

export async function loadHabit(id: string): Promise<Habit | null> {
  try {
    const r = await api.vault.get({ kind: HABIT, id });
    return r.record ? toHabit(r.record) : null;
  } catch (e) {
    if (isNotFound(e)) return null;
    throw e;
  }
}

/** 以習慣編號的小寫（h-3）為 key。 */
export async function loadStatuses(): Promise<Record<string, HabitStatus>> {
  const r = await api.vault.list({ kind: STATUS, include_archived: false });
  const out: Record<string, HabitStatus> = {};
  for (const x of r.records ?? []) out[x.id ?? ""] = toStatus(x);
  return out;
}

export async function loadStatus(habitId: string): Promise<HabitStatus | null> {
  try {
    const r = await api.vault.get({ kind: STATUS, id: statusId(habitId) });
    return r.record ? toStatus(r.record) : null;
  } catch (e) {
    if (isNotFound(e)) return null;
    throw e;
  }
}

/** 錢包還沒算出來（程式剛起來、還沒有習慣）就是 0 點。 */
export async function loadWallet(): Promise<Wallet> {
  try {
    const r = await api.vault.get({ kind: WALLET, id: "main" });
    return toWallet(r.record);
  } catch (e) {
    if (isNotFound(e)) return toWallet(null);
    throw e;
  }
}

/** 名冊上每位秘書的人名（職位 id → 人名）。 */
export async function loadAgentNames(): Promise<Record<string, string>> {
  const r = await api.web.getRoster({});
  const out: Record<string, string> = {};
  for (const a of r.agents ?? []) if (a.id) out[a.id] = a.name || a.id;
  return out;
}

/** 展示間在不在：衣櫃套件啟用中、沒被拒才放連結（讀不到套件當作沒有）。 */
export async function loadClosetAvailable(): Promise<boolean> {
  try {
    const r = await api.package.get({ id: "closet" });
    return r.package?.enabled === true && r.package.state !== "rejected";
  } catch {
    return false;
  }
}

function isNotFound(e: unknown): boolean {
  return e instanceof Error && (e as { code?: string }).code === "not_found";
}

async function addCheckin(fields: Record<string, unknown>): Promise<void> {
  const { id } = await api.vault.nextId({ kind: CHECKIN });
  if (!id) throw new Error("沒有拿到新的編號");
  await api.vault.put({ record: { kind: CHECKIN, id, fields, archived: false } });
}

/** 打卡＝寫一筆 habits.checkin（source = manual、同一個 key 只算一次）；sub 是子項目名稱。 */
export async function checkin(habit: string, date: string, sub = ""): Promise<void> {
  await addCheckin({
    habit,
    date,
    source: "manual",
    key: checkinKey(habit, date, sub),
    ...(sub ? { sub } : {}),
  });
}

/** 撤銷＝再寫一筆 undo = true（取消同一天最後一筆還有效的手動打卡）。 */
export async function undoCheckin(habit: string, date: string, sub = ""): Promise<void> {
  await addCheckin({ habit, date, source: "manual", undo: true, ...(sub ? { sub } : {}) });
}

export async function patchHabit(
  id: string,
  set: Record<string, unknown>,
  unset: string[] = [],
): Promise<void> {
  await api.vault.patch({ kind: HABIT, id, set, unset });
}

/** 兌換＝寫一筆 habits.exchange；程式看筆數，用 TASK_POINTS（30）點待辦點數換 1 個錢包點，不夠的那次不換（HAB-18、DEV-149）。 */
export async function exchangeTaskPoints(): Promise<void> {
  const kind = "habits.exchange";
  const { id } = await api.vault.nextId({ kind });
  if (!id) throw new Error("沒有拿到新的編號");
  await api.vault.put({ record: { kind, id, fields: { points: TASK_POINTS }, archived: false } });
}

export function reasonOf(e: unknown): string {
  return e instanceof Error && e.message ? e.message : "連不上看板服務，請稍後再試";
}
