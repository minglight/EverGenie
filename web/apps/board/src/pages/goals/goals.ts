import {
  ApiError,
  type Goal,
  type GoalState,
  type Reminder,
  type Task,
  type TaskStatus,
} from "@evergenie/sdk-web";
import { showToast } from "@evergenie/ui";
import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { matchAgent } from "../../data/agentFilter";
import { apiFor, PRIMARY, type SiteRef, siteKey } from "../../data/sites";
import { t } from "../../i18n/zh-TW";

// Goal 頁的資料（REQ-203；D-39）。所有 Goal 一次抓（量很小），分段在畫面上切。
// 每台各一份（S3-7b）：查詢的 key 經 siteKey 帶台名（主伺服器跟 S3 之前一樣），動作送到 Goal 所在的台。
// Goal 不跨台：成員（任務單、待辦）只看同一台的。

export type GoalView = "active" | "draft" | "ended";
export const goalViews: GoalView[] = ["active", "draft", "ended"];

const viewOf: Record<GoalState, GoalView> = {
  active: "active",
  draft: "draft",
  done: "ended",
  withdrawn: "ended",
};

export function goalViewOf(g: Goal): GoalView {
  return viewOf[g.state ?? "active"] ?? "active";
}

const stamp = (g: Goal, ended: boolean) =>
  Date.parse((ended ? g.ended_at : "") || g.updated_at || g.created_at || "") || 0;

/** 依分段分好：進行中／要你同意 最近更新的在前，已結束看結束時間。 */
export function groupGoals(goals: readonly Goal[]): Record<GoalView, Goal[]> {
  const out: Record<GoalView, Goal[]> = { active: [], draft: [], ended: [] };
  for (const g of goals) out[goalViewOf(g)].push(g);
  out.active.sort((a, b) => stamp(b, false) - stamp(a, false));
  out.draft.sort((a, b) => stamp(b, false) - stamp(a, false));
  out.ended.sort((a, b) => stamp(b, true) - stamp(a, true));
  return out;
}

/** 篩選與徽章看「誰提的」：Goal 的負責秘書，沒有就是開的人。 */
export const goalProposer = (g: Goal) => g.agent || g.by;

/**
 * 分段按鈕上的數字（「進行中 N」…）：跟下面的清單同一個秘書篩選（WEB-98），只看一位時寫她的件數；沒選人就是全部。
 */
export function goalViewCounts(
  groups: Record<GoalView, readonly { site: Pick<SiteRef, "key">; item: Goal }[]>,
  picked: readonly string[],
): Record<GoalView, number> {
  const out: Record<GoalView, number> = { active: 0, draft: 0, ended: 0 };
  for (const v of goalViews) {
    out[v] = groups[v].filter(({ site, item }) =>
      matchAgent(picked, goalProposer(item), site.key),
    ).length;
  }
  return out;
}

const openTaskStatuses: TaskStatus[] = ["idea", "doing", "waiting_user", "waiting_external"];

const isOpenTask = (task: Task) => openTaskStatuses.includes(task.status ?? "doing");
const closedStamp = (task: Task) => Date.parse(task.closed_at || task.updated_at || "") || 0;
const openReminders = (site: SiteRef) =>
  apiFor(site).reminders.list({ statuses: ["todo", "doing", "asking"], include_archived: false });

export interface GoalMembers {
  /** 還沒結束的任務單。 */
  tasks: Task[];
  /** 已結束的任務單（含已封存的），最近結束的在前。 */
  closed: Task[];
  /** 還沒做的待辦。 */
  reminders: Reminder[];
}

/** 掛在這個 Goal 上的任務單（進行中＋已結束）與還沒做的待辦（Goal 檔沒有成員清單，是反查，GOL-7；REQ-232）。 */
export const goalMembersQuery = (id: string, site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "goals", "members", id),
    queryFn: async (): Promise<GoalMembers> => {
      const [tasks, reminders] = await Promise.all([
        apiFor(site).tasks.list({ goal: id, ideas_only: false, include_archived: true }),
        openReminders(site),
      ]);
      const all = tasks.tasks ?? [];
      return {
        tasks: all.filter(isOpenTask),
        closed: all
          .filter((task) => !isOpenTask(task))
          .sort((a, b) => closedStamp(b) - closedStamp(a)),
        reminders: (reminders.reminders ?? []).filter((r) => r.goal === id),
      };
    },
    staleTime: 10_000,
  });

export interface GoalCount {
  open: number;
  closed: number;
  reminders: number;
}

/** Goal 清單每一列底下的數字：進行中幾張、已結束幾張、待辦幾個（REQ-232）。 */
export const goalCountsQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "goals", "counts"),
    queryFn: async (): Promise<Record<string, GoalCount>> => {
      const [tasks, reminders] = await Promise.all([
        apiFor(site).tasks.list({ ideas_only: false, include_archived: true }),
        openReminders(site),
      ]);
      const out: Record<string, GoalCount> = {};
      const of = (goal: string) => {
        out[goal] ??= { open: 0, closed: 0, reminders: 0 };
        return out[goal];
      };
      for (const task of tasks.tasks ?? []) {
        if (!task.goal) continue;
        if (isOpenTask(task)) of(task.goal).open++;
        else of(task.goal).closed++;
      }
      for (const r of reminders.reminders ?? []) {
        if (r.goal) of(r.goal).reminders++;
      }
      return out;
    },
    staleTime: 15_000,
  });

export function refreshGoals(client: QueryClient, site: SiteRef = PRIMARY) {
  void client.invalidateQueries({ queryKey: siteKey(site, "goals") });
  void client.invalidateQueries({ queryKey: siteKey(site, "goal") });
}

// 主人按「確認」「不要」是回答秘書提案的那則通知，核心收到後才把 Goal 改狀態（事件是非同步的）：
// 回答完馬上重抓一次，稍後再抓兩次，畫面就會跟上。
function refreshGoalsSoon(client: QueryClient, site: SiteRef) {
  refreshGoals(client, site);
  setTimeout(() => refreshGoals(client, site), 800);
  setTimeout(() => refreshGoals(client, site), 2500);
}

export interface NewGoalInput {
  title: string;
  text: string;
  /** 驗收條件：怎樣算達成（必填，法律第 4 條）。 */
  criteria: string;
  /** 負責的秘書（職位 id）；空的＝不指定。 */
  agent: string;
}

/** 主人直接開 Goal（D-39）：建好就是進行中。`site`＝建在哪台（預設主伺服器；`input.agent` 是那台的職位 id）。 */
export async function createGoal(
  client: QueryClient,
  input: NewGoalInput,
  goDetail: (id: string) => void,
  site: SiteRef = PRIMARY,
): Promise<Goal> {
  const res = await apiFor(site).goals.create({
    title: input.title.trim(),
    text: input.text.trim(),
    criteria: input.criteria.trim(),
    agent: input.agent || undefined,
  });
  const goal = res.goal;
  if (!goal?.id) throw new Error(t("ui.loadFailed", { what: "Goal" }));
  client.setQueryData<Goal[]>(siteKey(site, "goals", "list"), (list) =>
    list ? [goal, ...list.filter((g) => g.id !== goal.id)] : list,
  );
  const id = goal.id;
  showToast(t("goal.new.toast", { id, title: goal.title ?? "" }), {
    undo: { label: t("goal.new.toast.open"), onClick: () => goDetail(id) },
  });
  refreshGoals(client, site);
  return goal;
}

/** 主人收回 Goal（GOL-4），送到 Goal 所在的台。 */
export async function withdrawGoal(
  client: QueryClient,
  goal: Goal,
  site: SiteRef = PRIMARY,
): Promise<void> {
  await apiFor(site).goals.withdraw({ id: goal.id ?? "" });
  showToast(t("goal.toast.withdrawn", { id: goal.id ?? "" }));
  refreshGoals(client, site);
}

/** 回答秘書提議的 Goal：「確認」開始接力，「不要」收回（GOL-1，走既有的通知回答路徑）。 */
export async function answerDraftGoal(
  client: QueryClient,
  goal: Goal,
  confirm: boolean,
  site: SiteRef = PRIMARY,
): Promise<void> {
  // 提議的通知在 Goal 那台（goal.notice 是那台的通知編號）。
  await apiFor(site).notify.answer({
    id: goal.notice ?? "",
    text: confirm ? "確認" : "不要",
    via: "button",
  });
  showToast(t(confirm ? "goal.toast.confirmed" : "goal.toast.declined", { id: goal.id ?? "" }));
  refreshGoalsSoon(client, site);
}

// ---- 跟秘書聊這個 Goal（一個 Goal 一個專屬對話，跟任務單的「開對話」同一種做法）----
// 對話開在 Goal 所在的那台（`site`，?home=），讀、開、送第一句都對那台，不退回主伺服器（FEN-28）。

/** 這個 Goal 的專屬對話名稱：`<負責的秘書>-<Goal 編號小寫>`（例：art-goal-3）；沒有負責的秘書回空字串。 */
export function goalRoomName(goal: Pick<Goal, "id" | "agent">): string {
  const agent = goal.agent ?? "";
  const id = (goal.id ?? "").toLowerCase();
  if (!agent || agent.startsWith("owner(") || !/^[a-z0-9-]+$/.test(id)) return "";
  return `${agent}-${id}`;
}

/** 第一次開這個對話時，先替主人說的那一句：讓秘書知道在談哪個 Goal、去哪裡讀原文與計畫書。 */
export function goalRoomFirstMessage(goal: Pick<Goal, "id" | "title">): string {
  return t("goal.chat.first", { id: goal.id ?? "", title: goal.title ?? "" });
}

/** 這個 Goal 的專屬對話（含已經停了的）；從沒開過回 null。 */
export const goalRoomQuery = (name: string, site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "goal-room", name),
    enabled: name !== "",
    queryFn: async () => {
      try {
        return (await apiFor(site).session.get({ name })).session ?? null;
      } catch (err) {
        if (err instanceof ApiError && err.code === "not_found") return null;
        throw err;
      }
    },
  });

/** 對話標題：`GOAL-n 標題`（不會被當成任務單號，清單上也看得出是哪個 Goal）。 */
export function goalRoomTopic(goal: Pick<Goal, "id" | "title">): string {
  return [goal.id, goal.title].filter(Boolean).join(" ");
}

/**
 * 開（或接回）這個 Goal 的專屬對話，回對話名稱。先問伺服器這個對話存不存在（停了的也算）：
 * 從沒開過才替主人送第一句，之後回來接著聊、不重送。
 */
export async function openGoalRoom(
  goal: Pick<Goal, "id" | "title" | "agent">,
  site: SiteRef = PRIMARY,
): Promise<string> {
  const name = goalRoomName(goal);
  if (!name) throw new Error(t("goal.chat.noAgent"));
  const siteApi = apiFor(site);
  let exists = true;
  try {
    await siteApi.session.get({ name });
  } catch (err) {
    if (!(err instanceof ApiError && err.code === "not_found")) throw err;
    exists = false;
  }
  const res = await siteApi.session.open({
    name,
    agent: goal.agent,
    kind: "room",
    topic: goalRoomTopic(goal),
    first_message: exists ? undefined : goalRoomFirstMessage(goal),
  });
  return res.session?.name || name;
}
