// 飲食套件讀寫資料的地方：全部走 @evergenie/sdk-web 的 api.vault（D-28，沒有套件自己的 RPC）。
import { api } from "@evergenie/sdk-web";
import {
  type Fav,
  type Goal,
  type Meal,
  type RecordLike,
  toFav,
  toGoal,
  toMeal,
} from "./nutrition";

export const MEAL = "nutrition.meal";
export const GOAL = "nutrition.goal";
export const FAV = "nutrition.fav";
export const GOAL_ID = "main";

export async function loadMeals(): Promise<Meal[]> {
  const r = await api.vault.list({ kind: MEAL, include_archived: false });
  return (r.records ?? []).map((x: RecordLike) => toMeal(x));
}

export async function loadFavs(): Promise<Fav[]> {
  const r = await api.vault.list({ kind: FAV, include_archived: false });
  return (r.records ?? []).map((x: RecordLike) => toFav(x));
}

/**
 * 目標只有一筆（id main）；還沒設就是 null。用 list 不用 get（DEV-157 新家第一天走查⑤）：還沒設目標是常態，
 * get 會回 404，瀏覽器每開一次頁就在主控台記一筆錯；list 沒有就回空的。封存的也算（跟 get 一樣），存的時候才不會重複新增。
 */
export async function loadGoal(): Promise<Goal | null> {
  const r = await api.vault.list({ kind: GOAL, include_archived: true });
  const rec = (r.records ?? []).find((x: RecordLike) => x.id === GOAL_ID);
  return rec ? toGoal(rec) : null;
}

async function addRecord(kind: string, fields: Record<string, unknown>): Promise<string> {
  const { id } = await api.vault.nextId({ kind });
  if (!id) throw new Error("沒有拿到新的編號");
  await api.vault.put({ record: { kind, id, fields, archived: false } });
  return id;
}

export const addMeal = (fields: Record<string, unknown>) => addRecord(MEAL, fields);
export const addFav = (fields: Record<string, unknown>) => addRecord(FAV, fields);

/** 刪除＝封存（檔案還在）。 */
export async function archiveRecord(kind: string, id: string): Promise<void> {
  await api.vault.archive({ kind, id });
}

/** 存目標：還沒有就新增，有了就只改這幾個欄位。 */
export async function saveGoal(goal: Goal, exists: boolean): Promise<void> {
  const fields = {
    mode: goal.mode,
    kcal: goal.kcal,
    protein: goal.protein,
    carb: goal.carb,
    fat: goal.fat,
  };
  if (exists) await api.vault.patch({ kind: GOAL, id: GOAL_ID, set: fields });
  else await api.vault.put({ record: { kind: GOAL, id: GOAL_ID, fields, archived: false } });
}

export function reasonOf(e: unknown): string {
  return e instanceof Error && e.message ? e.message : "連不上看板服務，請稍後再試";
}
