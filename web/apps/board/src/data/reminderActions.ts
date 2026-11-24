import { ApiError, type Reminder, type RemindersUpdateRequest } from "@evergenie/sdk-web";
import { showToast } from "@evergenie/ui";
import type { QueryClient } from "@tanstack/react-query";
import { t } from "../i18n/zh-TW";
import { describeWhen } from "../lib/format";
import { itemKey } from "./acrossSites";
import { deferAction } from "./deferred";
import { refreshAll } from "./live";
import { apiFor, PRIMARY, type SiteRef } from "./sites";

// 待辦的動作（補充契約 S5 第 4 點）。「做了」「取消」是可逆的（FEN-21）：畫面先生效、5 秒內沒按復原才送；
// 「還在做」「延後」馬上送（結果看得到，不需要復原）。看板的身分由伺服器補（前端不送 by）。
// 每個動作都送到那個待辦所在的台（`site`，預設主伺服器；S3-7b）；延後動作的 key 用 itemKey（兩台編號可能一樣）。

const reminderName = (r: Reminder) => r.content || r.id || "";

/** 完成會拿幾點（RMD-24、RMD-26）：伺服器算好的 points；不集點、今天給過是 0，就不寫。 */
function pointsText(r: Reminder): string {
  return r.points ? t("reminder.toast.points", { n: r.points }) : "";
}

export function markDone(client: QueryClient, r: Reminder, site: SiteRef = PRIMARY) {
  const id = r.id ?? "";
  deferAction({
    key: itemKey(site, id),
    message: t("reminder.toast.done", { content: reminderName(r) }) + pointsText(r),
    run: async () => {
      await apiFor(site).reminders.done({ id });
    },
    settled: () => refreshAll(client, site),
  });
}

export function markCancelled(client: QueryClient, r: Reminder, site: SiteRef = PRIMARY) {
  const id = r.id ?? "";
  deferAction({
    key: itemKey(site, id),
    message: t("reminder.toast.cancelled", { content: reminderName(r) }),
    run: async () => {
      await apiFor(site).reminders.cancel({ id });
    },
    settled: () => refreshAll(client, site),
  });
}

export async function markDoing(
  client: QueryClient,
  r: Reminder,
  site: SiteRef = PRIMARY,
): Promise<void> {
  try {
    await apiFor(site).reminders.doing({ id: r.id ?? "" });
    showToast(t("reminder.toast.doing", { content: reminderName(r) }));
    await refreshAll(client, site);
  } catch (err) {
    showToast(
      t("ui.reasonFailed", { reason: err instanceof ApiError ? err.message : String(err) }),
    );
  }
}

/** until：Go 的 duration（"1h"）或 RFC3339。 */
export async function snoozeReminder(
  client: QueryClient,
  r: Reminder,
  until: string,
  at: Date | null,
  site: SiteRef = PRIMARY,
): Promise<void> {
  await apiFor(site).reminders.snooze({ id: r.id ?? "", until });
  showToast(
    t("reminder.toast.snoozed", {
      when: at ? describeWhen(at).slice(2) : t("reminder.snooze.hour"),
      content: reminderName(r),
    }),
  );
  await refreshAll(client, site);
}

export async function updateReminder(
  client: QueryClient,
  id: string,
  patch: Omit<RemindersUpdateRequest, "id" | "by">,
  site: SiteRef = PRIMARY,
): Promise<Reminder | undefined> {
  const res = await apiFor(site).reminders.update({ id, ...patch });
  await refreshAll(client, site);
  return res.reminder;
}

export interface SnoozePreset {
  key: "hour" | "tonight" | "tomorrow";
  label: string;
  until: string;
  at: Date | null;
}

/** 延後選單的三個預設（今晚 20:00 過了就是明晚）。 */
export function snoozePresets(now: Date = new Date()): SnoozePreset[] {
  const tonight = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 20, 0);
  if (tonight.getTime() <= now.getTime()) tonight.setDate(tonight.getDate() + 1);
  const tomorrow = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, 9, 0);
  return [
    { key: "hour", label: t("reminder.snooze.hour"), until: "1h", at: null },
    {
      key: "tonight",
      label: t("reminder.snooze.tonight"),
      until: tonight.toISOString(),
      at: tonight,
    },
    {
      key: "tomorrow",
      label: t("reminder.snooze.tomorrow"),
      until: tomorrow.toISOString(),
      at: tomorrow,
    },
  ];
}
