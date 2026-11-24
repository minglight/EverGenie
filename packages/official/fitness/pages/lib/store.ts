// 運動套件讀寫資料的地方：全部走 @evergenie/sdk-web 的 api.vault（D-28，沒有套件自己的 RPC）。
import { api } from "@evergenie/sdk-web";
import {
  type Equip,
  type Note,
  type RecordLike,
  toEquip,
  toNote,
  toWorkout,
  type Workout,
} from "./workout";

export const WORKOUT = "fitness.workout";
export const EQUIPMENT = "fitness.equipment";
export const NOTE = "fitness.note";

export async function loadWorkouts(): Promise<Workout[]> {
  const r = await api.vault.list({ kind: WORKOUT, include_archived: false });
  return (r.records ?? []).map((x: RecordLike) => toWorkout(x));
}

export async function loadEquipment(): Promise<Equip[]> {
  const r = await api.vault.list({ kind: EQUIPMENT, include_archived: false });
  return (r.records ?? []).map((x: RecordLike) => toEquip(x));
}

export async function loadNotes(): Promise<Note[]> {
  const r = await api.vault.list({ kind: NOTE, include_archived: false });
  return (r.records ?? []).map((x: RecordLike) => toNote(x));
}

async function nextId(kind: string): Promise<string> {
  const { id } = await api.vault.nextId({ kind });
  if (!id) throw new Error("沒有拿到新的編號");
  return id;
}

/** 新增一筆訓練；欄位由編號算出來（跑步的檢討要知道自己的單號）。 */
export async function addWorkout(build: (id: string) => Record<string, unknown>): Promise<string> {
  const id = await nextId(WORKOUT);
  await api.vault.put({ record: { kind: WORKOUT, id, fields: build(id), archived: false } });
  return id;
}

export async function patchWorkout(id: string, set: Record<string, unknown>): Promise<void> {
  await api.vault.patch({ kind: WORKOUT, id, set });
}

export async function addEquipment(fields: Record<string, unknown>): Promise<string> {
  const id = await nextId(EQUIPMENT);
  await api.vault.put({ record: { kind: EQUIPMENT, id, fields, archived: false } });
  return id;
}

export async function patchEquipment(id: string, set: Record<string, unknown>): Promise<void> {
  await api.vault.patch({ kind: EQUIPMENT, id, set });
}

/** 刪除＝封存（檔案還在）。 */
export async function archiveRecord(kind: string, id: string): Promise<void> {
  await api.vault.archive({ kind, id });
}

export function reasonOf(e: unknown): string {
  return e instanceof Error && e.message ? e.message : "連不上看板服務，請稍後再試";
}
