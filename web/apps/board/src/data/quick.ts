import type {
  Cycle,
  Difficulty,
  Follow,
  Reminder,
  RemindersCreateRequest,
} from "@evergenie/sdk-web";
import { showToast } from "@evergenie/ui";
import type { QueryClient } from "@tanstack/react-query";
import { t } from "../i18n/zh-TW";
import { timeLabel } from "../lib/format";
import type { AgentRef } from "../lib/intent";
import { refreshAll } from "./live";
import { apiFor, PRIMARY, type SiteRef } from "./sites";

// 萬用輸入框與指令面板共用的送出（補充契約 S5 第 2、3 點）。

export interface TodoInput {
  content: string;
  owner: string;
  /** 沒指定就是：有時間 remind、沒時間 none。 */
  follow?: Follow;
  cycle?: Cycle;
  check?: string;
  /** 描述（RMD-23；「到時間找我聊」時就是想聊什麼）。 */
  body?: string;
  /** 附件（RMD-23）：已經傳到那台的 `/share/<名字>` 連結。 */
  attachments?: string[];
  /** 難度（RMD-24）：沒選＝普通。 */
  difficulty?: Difficulty;
  /** 沒時間就是 null：不送 `at`（送空字串會壞，由伺服器套預設時間）。 */
  at: Date | null;
}

export function todoRequest(input: TodoInput): RemindersCreateRequest {
  const req: RemindersCreateRequest = {
    content: input.content,
    owner: input.owner,
    follow: input.follow ?? (input.at ? "remind" : "none"),
    cycle: input.cycle ?? "once",
  };
  if (input.at) req.at = input.at.toISOString();
  if (input.check) req.check = input.check;
  if (input.body) req.body = input.body;
  if (input.difficulty) req.difficulty = input.difficulty;
  if (input.attachments && input.attachments.length > 0) req.attachments = input.attachments;
  return req;
}

/**
 * 新增待辦。成功後 toast 帶「復原」（建立後可撤銷：要先有 id，所以是送 cancel，不是延後送出）。
 * `site`＝建在哪台（預設主伺服器；S3-7b）：`input.owner` 是那台的職位 id；復原也送那台。
 */
export async function createReminder(
  client: QueryClient,
  input: TodoInput,
  site: SiteRef = PRIMARY,
): Promise<Reminder> {
  const target = apiFor(site);
  const res = await target.reminders.create(todoRequest(input));
  const reminder = res.reminder;
  if (!reminder?.id)
    throw new Error(t("quick.failed", { reason: t("ui.loadFailed", { what: "待辦" }) }));
  const when = input.at ? timeLabel(input.at.toISOString(), new Date()) : "";
  showToast(t("quick.toast.todo", { what: [when, input.content].filter(Boolean).join(" ") }), {
    undo: {
      label: t("ui.undo"),
      onClick: () => {
        target.reminders
          .cancel({ id: reminder.id ?? "" })
          .then(() => refreshAll(client, site))
          .then(() => showToast(t("quick.toast.undone")))
          .catch(() => {});
      },
    },
  });
  await refreshAll(client, site);
  return reminder;
}

/**
 * 傳話給一位秘書（bus.send；看板會補 from: owner(web)）。toast 帶「去聊」。
 * `site`＝那位秘書在哪台（預設主伺服器）：`agent.id` 是那台的職位 id，送到那台；呼叫端要先確認那台能用，這裡不退回主伺服器。
 */
export async function sayToAgent(
  client: QueryClient,
  agent: AgentRef,
  body: string,
  goChat: (agentId: string) => void,
  site: SiteRef = PRIMARY,
): Promise<void> {
  await apiFor(site).bus.send({ to: agent.id, kind: "chat", body });
  showToast(t("quick.toast.say", { name: agent.name }), {
    undo: { label: t("quick.toast.sayGo"), onClick: () => goChat(agent.id) },
  });
  await refreshAll(client, site);
}
