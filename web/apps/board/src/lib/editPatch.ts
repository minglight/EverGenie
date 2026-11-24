import type {
  Cycle,
  Difficulty,
  Follow,
  Reminder,
  RemindersUpdateRequest,
  Task,
  TasksUpdateRequest,
  TaskType,
} from "@evergenie/sdk-web";
import type { TextKey } from "../i18n/zh-TW";
import { parseIso } from "./format";

// 「編輯」整張表單（WEB-161）：打開時把現值抄成一份草稿，儲存時跟這份比，只送改過的欄位。
// 跟打開那一刻比（不是跟最新的資料比）：這段時間別人改了沒碰的欄位，也不會被表單蓋回去。

export interface ReminderDraft {
  content: string;
  at: Date | null;
  owner: string;
  follow: Follow;
  cycle: Cycle;
  difficulty: Difficulty;
  check: string;
  body: string;
}

export type ReminderPatch = Omit<RemindersUpdateRequest, "id" | "by">;

export function reminderDraftOf(
  r: Pick<
    Reminder,
    "content" | "at" | "owner" | "follow" | "cycle" | "difficulty" | "check" | "body"
  >,
): ReminderDraft {
  return {
    content: r.content ?? "",
    at: parseIso(r.at),
    owner: r.owner ?? "",
    follow: r.follow ?? "none",
    cycle: r.cycle ?? "once",
    difficulty: r.difficulty ?? "normal",
    check: r.check ?? "",
    body: r.body ?? "",
  };
}

/**
 * 待辦表單要送的欄位。時間清掉（null）不送：API 沒有「拿掉時間」，沒給＝不動。
 * 「要確認什麼」只在跟進方式是「確認」時才有意義（照詳情頁顯示它的條件），其他跟進方式不送。
 */
export function reminderPatch(before: ReminderDraft, after: ReminderDraft): ReminderPatch {
  const patch: ReminderPatch = {};
  const content = after.content.trim();
  if (content !== before.content.trim()) patch.content = content;
  if (after.at && after.at.getTime() !== before.at?.getTime()) patch.at = after.at.toISOString();
  if (after.owner !== before.owner) patch.owner = after.owner;
  if (after.follow !== before.follow) patch.follow = after.follow;
  if (after.cycle !== before.cycle) patch.cycle = after.cycle;
  if (after.difficulty !== before.difficulty) patch.difficulty = after.difficulty;
  const check = after.check.trim();
  if (after.follow === "check" && check !== before.check.trim()) patch.check = check;
  const body = after.body.trim();
  if (body !== before.body.trim()) patch.body = body;
  return patch;
}

/** 送出前就擋得下的錯（欄位 → 說明的 i18n key）；空物件＝可以送。 */
export function reminderDraftErrors(
  d: ReminderDraft,
): Partial<Record<keyof ReminderDraft, TextKey>> {
  const errors: Partial<Record<keyof ReminderDraft, TextKey>> = {};
  if (d.content.trim() === "") errors.content = "reminder.edit.required";
  if (d.follow === "check" && d.check.trim() === "") errors.check = "quick.check.required";
  return errors;
}

export interface TaskDraft {
  title: string;
  agent: string;
  type: TaskType;
  body: string;
  criteria: string;
}

export type TaskPatch = Omit<TasksUpdateRequest, "id" | "by">;

export function taskDraftOf(
  task: Pick<Task, "title" | "agent" | "type" | "body" | "criteria">,
): TaskDraft {
  return {
    title: task.title ?? "",
    agent: task.agent ?? "",
    type: task.type ?? "task",
    body: task.body ?? "",
    criteria: task.criteria ?? "",
  };
}

/**
 * 任務單表單要送的欄位。驗收條件跟就地編輯同一個語意（TSK-24、TSK-29）：看板送的就是主人，
 * 改了就算主人的條件；沒改就不送，原本是秘書寫的還是秘書的。問答類沒有驗收條件，不送。
 */
export function taskPatch(before: TaskDraft, after: TaskDraft): TaskPatch {
  const patch: TaskPatch = {};
  const title = after.title.trim();
  if (title !== before.title.trim()) patch.title = title;
  if (after.agent !== before.agent) patch.agent = after.agent;
  if (after.type !== before.type) patch.type = after.type;
  const body = after.body.trim();
  if (body !== before.body.trim()) patch.body = body;
  const criteria = after.criteria.trim();
  if (after.type === "task" && criteria !== before.criteria.trim()) patch.criteria = criteria;
  return patch;
}

export function taskDraftErrors(d: TaskDraft): Partial<Record<keyof TaskDraft, TextKey>> {
  return d.title.trim() === "" ? { title: "task.title.required" } : {};
}

export function isEmptyPatch(patch: object): boolean {
  return Object.keys(patch).length === 0;
}
