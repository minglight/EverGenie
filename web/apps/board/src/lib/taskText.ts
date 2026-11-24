import type { Task, TaskStatus } from "@evergenie/sdk-web";
import type { PillTone } from "../components/Pill";
import { t } from "../i18n/zh-TW";
import { knownText } from "./knownText";

/** 任務單六種狀態（TSK-2，2026-11-01 從九種收成六種）。 */
export const statusKey = {
  idea: "task.status.idea",
  doing: "task.status.doing",
  waiting_user: "task.status.waiting_user",
  waiting_external: "task.status.waiting_external",
  done: "task.status.done",
  cancelled: "task.status.cancelled",
} as const satisfies Record<TaskStatus, string>;

export const statusTone: Record<TaskStatus, PillTone> = {
  idea: "muted",
  doing: "muted",
  waiting_user: "attention",
  waiting_external: "muted",
  done: "ok",
  cancelled: "muted",
};

/** 交付等驗收＝等你＋秘書建議了結局（原本的 review 狀態）；驗收、退回只對它。 */
export function isDelivery(task: Pick<Task, "status" | "proposed">): boolean {
  return task.status === "waiting_user" && !!task.proposed;
}

/** 秘書在問你（等你、沒有建議的結局）：問句下面就地回答（DEV-156 U-05）。 */
export function isAsking(task: Pick<Task, "status" | "proposed">): boolean {
  return task.status === "waiting_user" && !task.proposed;
}

/** 畫面上的狀態字：等你再分「要你驗收」與「要你回答」，其他照狀態。 */
export function statusText(task: Task): string {
  if (!task.status) return "";
  if (task.status === "waiting_user")
    return t(isDelivery(task) ? "task.status.delivered" : "task.status.asking");
  return knownText(statusKey, task.status);
}
