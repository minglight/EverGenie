import type { Difficulty, Follow, ReminderStatus } from "@evergenie/sdk-web";
import type { PillTone } from "../../components/Pill";
import { t } from "../../i18n/zh-TW";
import { parseIso } from "../../lib/format";

const followKeys = {
  none: "follow.none",
  remind: "follow.remind",
  confirm: "follow.confirm",
  check: "follow.check",
  discuss: "follow.discuss",
} as const satisfies Record<Follow, string>;

/** 清單與詳細頁的說明字（第三人稱）：「秘書會提醒」。 */
export function followText(f: Follow): string {
  return t(followKeys[f]);
}

export const reminderStatusKey = {
  todo: "reminder.status.todo",
  doing: "reminder.status.doing",
  asking: "reminder.status.asking",
  shelved: "reminder.status.shelved",
  done: "reminder.status.done",
  cancelled: "reminder.status.cancelled",
} as const satisfies Record<ReminderStatus, string>;

export const reminderStatusTone: Record<ReminderStatus, PillTone> = {
  todo: "muted",
  doing: "accent",
  asking: "attention",
  shelved: "muted",
  done: "ok",
  cancelled: "muted",
};

/** 難度：沒寫或看板不認得（別台較新的版本）都當普通（WEB-162）。 */
export function difficultyOf(r: { difficulty?: string }): Difficulty {
  return r.difficulty === "easy" || r.difficulty === "hard" ? r.difficulty : "normal";
}

/** 會集點的跟進方式（RMD-26，DEV-149）：只有「問我做了沒」「秘書幫我確認」；其他的不標難度、不給點。 */
export function collectsPoints(r: { follow?: string }): boolean {
  return r.follow === "confirm" || r.follow === "check";
}

/** 時間已經過了、還沒做完也沒擱置：詳細頁的狀態寫「過期了」（DEV-156 U-17）。沒設時間的不算。
 * 這裡看的是時刻（今天早上的、中午看就過期了）；清單的「逾期」那組是照日子分的（reminderGroups：今天以前），兩個問的不是同一件事。 */
export function isLate(status: string, at: string | undefined, now: Date): boolean {
  if (status !== "todo" && status !== "doing" && status !== "asking") return false;
  const when = parseIso(at);
  return !!when && when.getTime() < now.getTime();
}
