import type { Task, TasksCreateRequest, TaskType } from "@evergenie/sdk-web";
import { showToast } from "@evergenie/ui";
import type { QueryClient } from "@tanstack/react-query";
import { t } from "../i18n/zh-TW";
import { refreshAll } from "./live";
import type { TaskRow } from "./queries";
import { apiFor, PRIMARY, type SiteRef, siteKey } from "./sites";

// 看板開任務單（REQ-186、DEV-124）：新單是待做，不開專屬 session、不傳話、不推播；
// 主人按「開始做」改成進行中，伺服器才開工作 session 並把單送給秘書（SES-27）。「進行中」就是真的開工了。

export interface NewTaskInput {
  title: string;
  agent: string;
  type: TaskType;
  body?: string;
  goal?: string;
  /** 驗收條件（選填）：主人寫了，秘書照它驗過就自己結；沒寫，秘書會補一條，做完等主人結（法律第 1.4 條）。 */
  criteria?: string;
  /** 附件（TSK-28）：已經傳到那台的 `/share/<名字>` 連結。 */
  attachments?: string[];
}

export function taskRequest(input: NewTaskInput): TasksCreateRequest {
  const req: TasksCreateRequest = {
    title: input.title.trim(),
    agent: input.agent,
    type: input.type,
    status: "idea",
  };
  const body = input.body?.trim();
  if (body) req.body = body;
  if (input.goal) req.goal = input.goal;
  const criteria = input.criteria?.trim();
  if (criteria) req.criteria = criteria;
  if (input.attachments && input.attachments.length > 0) req.attachments = input.attachments;
  return req;
}

/** 清單的快取先放進這張新單（全部未結；任務單頁的狀態標籤都從它挑），畫面立刻有；之後整批重抓。 */
export function addToTaskLists(client: QueryClient, task: Task, site: SiteRef = PRIMARY) {
  const row: TaskRow = { task, waitingDays: 0 };
  client.setQueryData<TaskRow[]>(siteKey(site, "tasks", "all"), (rows) =>
    rows ? [row, ...rows.filter((r) => r.task.id !== task.id)] : rows,
  );
}

/**
 * 開單：成功後清單先更新、toast 說開給誰（帶「看這張單」）。
 * `site`＝建在哪台（預設主伺服器；S3-7b）：`input.agent` 是那台的職位 id，單開在那台、重抓那台。
 */
export async function createTask(
  client: QueryClient,
  input: NewTaskInput,
  agentName: string,
  goDetail: (id: string) => void,
  site: SiteRef = PRIMARY,
): Promise<Task> {
  const res = await apiFor(site).tasks.create(taskRequest(input));
  const task = res.task;
  if (!task?.id)
    throw new Error(t("tasks.new.failed", { reason: t("ui.loadFailed", { what: "任務單" }) }));
  addToTaskLists(client, task, site);
  const id = task.id;
  showToast(t("tasks.new.toast", { id, name: agentName }), {
    undo: { label: t("tasks.new.toast.open"), onClick: () => goDetail(id) },
  });
  await refreshAll(client, site);
  return task;
}
