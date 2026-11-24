import type { Reminder, ReminderStatus } from "@evergenie/sdk-web";
import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { apiFor, PRIMARY, type SiteRef, siteKey } from "../../data/sites";

// 待辦頁的三個分頁（DEV-107）：未完成（依時間分組）、要你回答、已完成。舊的七個分段改成「未完成」裡的分組。
export type ReminderView = "open" | "waiting" | "done";
export const reminderViews: ReminderView[] = ["open", "waiting", "done"];

// 未完成＝還沒結束的全部（含已經提醒、要你回答的 asking 與擱置的 shelved）；「要你回答」只是其中的一個篩選，
// 免得過期的提醒一發出去就從「未完成」消失、跑到別的分頁找不到。
const openStatuses: ReminderStatus[] = ["todo", "doing", "asking", "shelved"];

// 每台一份（S3-7b）：key 經 siteKey 帶台名，主伺服器是 ["reminders", view]。
export const remindersQuery = (view: ReminderView, site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "reminders", view),
    queryFn: async (): Promise<Reminder[]> => {
      const api = apiFor(site).reminders;
      const res =
        view === "open"
          ? await api.list({ statuses: openStatuses, include_archived: false })
          : view === "waiting"
            ? await api.list({ query: "waiting", include_archived: false })
            : await api.list({ statuses: ["done"], include_archived: false });
      return res.reminders ?? [];
    },
    placeholderData: keepPreviousData,
    staleTime: 10_000,
    // 不輪詢：待辦一變 vault.changed 就重抓（REQ-389）。
  });
