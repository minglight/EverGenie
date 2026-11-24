import { ApiError, type Task, type TaskStatus } from "@evergenie/sdk-web";
import { keepPreviousData, type QueryClient, queryOptions, useQuery } from "@tanstack/react-query";
import { apiFor, PRIMARY, type SiteRef, siteKey } from "./sites";

export { homeQuery } from "./sites";

// 跨頁面共用的查詢（兩個以上的頁面資料夾、shell 或 components 會用到）放這裡；
// 只有一個頁面用的放 pages/<頁面>/queries.ts（ai_docs/conventions/frontend.md FEN-26）。
// 每個查詢都收一台 `site`（預設主伺服器）：key 經 siteKey 帶台名、資料向那台的 api 拿（FEN-28）。

// 選單用 keepPreviousData：徽章更新時選單不重畫、不跳動（定案 10）。
export const menuQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "menu"),
    queryFn: async () => (await apiFor(site).web.getMenu({})).menu ?? {},
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });

// 頁面清單（核心頁＋套件頁，含 slot 與編譯結果）；Slot 與套件頁共用同一份快取（P6-2a）。
export const pagesQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "web.get_pages"),
    queryFn: async () => (await apiFor(site).web.getPages({})).pages ?? [],
    staleTime: 30_000,
  });

export const rosterQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "roster"),
    queryFn: async () => (await apiFor(site).web.getRoster({})).agents ?? [],
    staleTime: 60_000,
  });

export const overviewQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "overview"),
    queryFn: () => apiFor(site).web.getOverview({}),
    placeholderData: keepPreviousData,
    staleTime: 15_000,
  });

export const threadsQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "threads"),
    queryFn: async () =>
      (await apiFor(site).session.listThreads({ include_stopped: false })).threads ?? [],
    placeholderData: keepPreviousData,
    staleTime: 15_000,
  });

export const notificationsQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "notifications"),
    queryFn: async () =>
      (await apiFor(site).notify.list({ include_archived: false })).notifications ?? [],
    placeholderData: keepPreviousData,
    staleTime: 15_000,
  });

// 找不到（編號打錯、已被整理掉）重試也沒用：直接顯示「找不到」。
export const retryUnlessMissing = (failures: number, error: Error) =>
  failures < 1 && !(error instanceof ApiError && error.code === "not_found");

export const notificationQuery = (id: string, site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "notification", id),
    queryFn: async () => (await apiFor(site).notify.get({ id })).notification ?? null,
    staleTime: 10_000,
    retry: retryUnlessMissing,
  });

// 資料層只有兩份清單：全部未結、已結束；任務單頁的狀態標籤從「全部未結」依狀態挑（pages/tasks/taskViews.ts，DEV-107）。
// 「等你」的待處理清單（首頁、數字）走 data/waiting.ts，不在這裡。
export type TaskView = "all" | "closed";
export const taskViews: TaskView[] = ["all", "closed"];

export interface TaskRow {
  task: Task;
  /** 等了幾天的後備值（看板用 waiting_since 自己算；新開的單先放 0）。 */
  waitingDays: number;
}

/** 「未結」的狀態，順序就是頁面分組的順序：球在你手上的最前面，沒開始的最後（唯一來源，頁面的分組由這份來）。 */
export const openStatuses = [
  "waiting_user",
  "doing",
  "waiting_external",
  "idea",
] as const satisfies readonly TaskStatus[];
const closedStatuses: TaskStatus[] = ["done", "cancelled"];

async function fetchTasks(site: SiteRef, view: TaskView): Promise<TaskRow[]> {
  const api = apiFor(site);
  const res = await api.tasks.list(
    view === "all"
      ? { statuses: [...openStatuses], ideas_only: false, include_archived: false }
      : { statuses: closedStatuses, ideas_only: false, include_archived: true },
  );
  return (res.tasks ?? []).map((task) => ({ task, waitingDays: 0 }));
}

export const tasksQuery = (view: TaskView, site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "tasks", view),
    queryFn: () => fetchTasks(site, view),
    placeholderData: keepPreviousData,
    staleTime: 10_000,
    // 不輪詢：任務一變 bus 的 vault.changed 就重抓（live.ts，REQ-389）。
  });

export const taskQuery = (id: string, site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "task", id),
    queryFn: async () => (await apiFor(site).tasks.get({ id })).task ?? null,
    staleTime: 10_000,
    retry: retryUnlessMissing,
  });

export const reminderQuery = (id: string, site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "reminder", id),
    queryFn: async () => (await apiFor(site).reminders.get({ id })).reminder ?? null,
    staleTime: 10_000,
    retry: retryUnlessMissing,
  });

/** 對話的顯示方式（home.toml chat_display）；看板只看自己這個家的設定，連到別台也一樣。 */
export const chatDisplayQuery = () =>
  queryOptions({
    queryKey: siteKey(PRIMARY, "system.get_mode.chat_display"),
    queryFn: async () => (await apiFor(PRIMARY).system.getMode({})).chat_display ?? "default",
    staleTime: 60_000,
    retry: 1,
  });

// 介面模式（簡單／完整，home.toml 的 mode，D-9）：外殼啟動時讀一次，切換後失效重讀（P6-6）。
export const modeQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "system.get_mode"),
    queryFn: async () => (await apiFor(site).system.getMode({})).mode ?? "simple",
    staleTime: 60_000,
    retry: 1, // 外殼等它讀到才畫選單（Goal 只在完整模式顯示），失敗不要卡太久
  });

/**
 * 要不要露內部代號（職位 id 這類）：只有完整模式才露；簡單模式（預設）與還沒讀到都不露（DEV-156 U-07）。
 * 跟外殼共用同一份 modeQuery 快取，不多打請求。
 */
export function useShowCodes(): boolean {
  return useQuery(modeQuery()).data === "full";
}

// 這個家是誰（label、env、版本、看板介面等級）：homeQuery 定義在 sites.ts（台清單也用它），上面 re-export。

// 掛在任務／待辦上的 Goal（完整模式的詳情才顯示）；讀不到就只顯示編號。
export const goalQuery = (id: string, site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "goal", id),
    queryFn: async () => (await apiFor(site).goals.get({ id })).goal ?? null,
    staleTime: 30_000,
    retry: false,
  });

// Goal 清單（Goal 頁與任務單頁都要用）；Goal 頁自己的其他查詢在 pages/goals/goals.ts。
export const goalsListQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "goals", "list"),
    queryFn: async () => (await apiFor(site).goals.list({})).goals ?? [],
    placeholderData: keepPreviousData,
    staleTime: 10_000,
  });

// 開任務單時可以掛的 Goal（完整模式才用）：只列進行中的。
export const activeGoalsQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "goals", "active"),
    queryFn: async () => (await apiFor(site).goals.list({ states: ["active"] })).goals ?? [],
    staleTime: 30_000,
  });

/** 串流收到事件時要重抓的 key（主伺服器的；別台用 `siteKey(site, ...key)` 加前綴）。 */
export const liveKeys = [
  ["menu"],
  ["overview"],
  ["threads"],
  ["notifications"],
  ["notification"],
  ["tasks"],
  ["task"],
  ["reminders"],
  ["reminder"],
  ["goals"],
  ["goal"],
] as const;

/** 套件動作做完要重抓的東西：套件清單、擴充點、選單、頁面清單（選單項目與套件頁會跟著變）。 */
export function invalidatePackageViews(
  client: QueryClient,
  site: SiteRef = PRIMARY,
): Promise<unknown> {
  return Promise.all(
    ["package.list", "package.list_extensions", "package.outdated", "menu", "web.get_pages"].map(
      (name) => client.invalidateQueries({ queryKey: siteKey(site, name) }),
    ),
  );
}

export const packageListQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "package.list"),
    queryFn: () => apiFor(site).package.list({}),
    staleTime: 10_000,
  });

// 版本與更新（REQ-281）：設定的「版本與更新」卡（WEB-51）與今天頁的升級結果橫幅（WEB-173）共用；
// 升級進行中由 components/UpgradeStatus 的 useUpgradeStatus 決定要不要每 3 秒再抓。
export const updateStatusQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "system.update_status"),
    queryFn: () => apiFor(site).system.updateStatus({}),
    staleTime: 15_000,
  });
