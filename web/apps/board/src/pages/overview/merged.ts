import type { Reminder, ThreadSummary, WebGetOverviewResponse } from "@evergenie/sdk-web";
import { useQueries, useQuery } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import { matchAgent } from "../../data/agentFilter";
import { type AgentView, agentKey, useSiteAgents } from "../../data/agents";
import { notificationsQuery, overviewQuery, threadsQuery } from "../../data/queries";
import { dataOf, type Site, type SiteRef, useSites } from "../../data/sites";
import {
  filteredWaitingTotal,
  itemKey,
  type WaitingItem,
  waitingCounts,
  workFor,
} from "../../data/waiting";
import { countsInBadge } from "../../lib/noticeLane";

// 「今天」合併各台（S3-7a；計畫 2026-11-01-board-multi-home 設計決定 4）：每台各自的 web.get_overview，
// 這裡把幾份併成一份給畫面；數字都是各台伺服器算好的再相加（FEN-N3：前端不自己算規則）。
// 只有一台時每個結果都跟直接用那一份 overview 一樣（同一個查詢、同樣的順序與 key）。

/** 一台的總覽：主伺服器一定在第一個；別台只有 ok 的才在（其他的由 SiteProblems 提示）。 */
export interface SiteOverview {
  site: Site;
  data: WebGetOverviewResponse | undefined;
  /** 連得上、但總覽讀失敗（還沒有資料）。 */
  failed: boolean;
}

/** 等你處理的討論串，帶著它在哪一台。 */
export interface SitedThread {
  site: SiteRef;
  /** 跨台不撞的 key（itemKey(台, 串名)）。 */
  key: string;
  thread: ThreadSummary;
}

/** 今天的待辦，帶著它在哪一台。 */
export interface SitedReminder {
  site: SiteRef;
  key: string;
  reminder: Reminder;
}

export interface MergedWork {
  items: WaitingItem[];
  threads: SitedThread[];
  reminders: SitedReminder[];
}

/**
 * 各台的「等你處理」、等你處理的串、今天的待辦併在一起（秘書篩選照各台的 key）。
 * 順序：要你同意的通知（各台依序）在前、等你的任務單在後，跟只有一台時一樣；串與待辦各台依序接起來。
 */
export function mergeWork(parts: readonly SiteOverview[], picked: readonly string[]): MergedWork {
  const works = parts.map((p) => ({ site: p.site, work: workFor(p.data, picked, p.site) }));
  const all = works.flatMap((w) => w.work.items);
  return {
    items: [...all.filter((i) => i.kind === "approve"), ...all.filter((i) => i.kind !== "approve")],
    threads: works.flatMap(({ site, work }) =>
      work.threads.map((thread) => ({ site, key: itemKey(site, thread.name ?? ""), thread })),
    ),
    reminders: works.flatMap(({ site, work }) =>
      work.reminders.map((reminder) => ({ site, key: itemKey(site, reminder.id ?? ""), reminder })),
    ),
  };
}

/** 篩選後「等你處理」的件數：各台伺服器算的相加；主伺服器還沒讀到是 undefined（不先顯示 0）。 */
export function mergedWaitingTotal(
  parts: readonly SiteOverview[],
  picked: readonly string[],
): number | undefined {
  if (parts[0]?.data === undefined) return undefined;
  let sum = 0;
  for (const p of parts) sum += filteredWaitingTotal(p.data, picked, p.site) ?? 0;
  return sum;
}

/** 側欄與徽章的件數（每位秘書的 key 帶台名）與總數（各台 waiting_total 相加）。 */
export function mergedCounts(parts: readonly SiteOverview[]): {
  counts: Record<string, number>;
  total: number;
} {
  const counts: Record<string, number> = {};
  let total = 0;
  for (const p of parts) {
    const one = waitingCounts(p.data, p.site);
    Object.assign(counts, one.counts);
    total += one.total;
  }
  return { counts, total };
}

/** 沒有篩選時的未讀數：各台伺服器算的 unread 相加。 */
export function mergedUnread(parts: readonly SiteOverview[]): number {
  return parts.reduce((sum, p) => sum + (p.data?.unread ?? 0), 0);
}

/** 讀不到總覽的別台（給 SiteProblems 的 failed）。 */
export function failedSites(parts: readonly SiteOverview[]): string[] {
  return parts.filter((p) => !p.site.primary && p.failed).map((p) => p.site.key);
}

interface Loaded {
  data: WebGetOverviewResponse | undefined;
  failed: boolean;
}

/** useQueries 的 combine：放在模組層（參考不變），資料沒變時回同一個陣列（跟 sites.ts 的 dataOf 同一個道理）。 */
function loadedOf(
  results: readonly { data: WebGetOverviewResponse | undefined; isError: boolean }[],
): Loaded[] {
  return results.map((r) => ({ data: r.data, failed: r.isError && r.data === undefined }));
}

/**
 * 各台的總覽查詢。主伺服器照舊用 overviewQuery()（只有一台時跟以前同一個查詢）；別台只抓 ok 的。
 * `enabled` 給外殼的手機頁首用（只有今天頁才要）。
 */
export function useOverviews(enabled = true) {
  const sites = useSites();
  const primary = useQuery({ ...overviewQuery(), enabled });
  const others = useMemo(() => sites.filter((s) => !s.primary && s.state === "ok"), [sites]);
  const loaded = useQueries({
    queries: others.map((site) => ({ ...overviewQuery(site), enabled })),
    combine: loadedOf,
  });
  const first = sites[0];
  const parts = useMemo<SiteOverview[]>(
    () => [
      ...(first ? [{ site: first, data: primary.data, failed: primary.isError }] : []),
      ...others.map((site, i) => ({
        site,
        data: loaded[i]?.data,
        failed: loaded[i]?.failed ?? false,
      })),
    ],
    [first, others, primary.data, primary.isError, loaded],
  );
  return { parts, primary };
}

/**
 * 篩選時的未讀數：各台的通知清單（只在有篩選時抓），照各台的秘書 key 算。
 * 跟伺服器的 unread 同一條（NTF-57）：秘書那道的未讀＋系統那道還要你回答的。
 */
export function useFilteredUnread(parts: readonly SiteOverview[], picked: readonly string[]) {
  const filtered = picked.length > 0;
  const lists = useQueries({
    queries: parts.map((p) => ({ ...notificationsQuery(p.site), enabled: filtered })),
  });
  if (!filtered) return mergedUnread(parts);
  let sum = 0;
  parts.forEach((p, i) => {
    sum += (lists[i]?.data ?? []).filter(
      (n) => countsInBadge(n) && matchAgent(picked, n.agent, p.site.key),
    ).length;
  });
  return sum;
}

/**
 * 有新訊息的對話（DEV-153）：各台開著的對話裡秘書說了、主人還沒看的（unread > 0），照台、照清單順序。
 * 已經列在「要你回答的討論串」的不重複（`waiting` 的 key）；秘書篩選照各台的 key。
 * 例：新家第一位秘書上班後主動打的招呼（agents AGS-11），主人回到今天頁就看得到。
 */
export function unreadThreads(
  lists: readonly { site: SiteRef; threads: readonly ThreadSummary[] | undefined }[],
  waiting: readonly SitedThread[],
  picked: readonly string[],
): SitedThread[] {
  const skip = new Set(waiting.map((w) => w.key));
  return lists.flatMap(({ site, threads }) =>
    (threads ?? [])
      .filter((thread) => thread.unread > 0 && thread.name)
      .map((thread) => ({ site, key: itemKey(site, thread.name ?? ""), thread }))
      .filter(({ key, thread }) => !skip.has(key) && matchAgent(picked, thread.agent, site.key)),
  );
}

/** 各台的對話清單（跟聊天分頁同一個查詢）挑出有新訊息的（`unreadThreads`）。 */
export function useUnreadThreads(waiting: readonly SitedThread[], picked: readonly string[]) {
  const rosters = useSiteAgents();
  const lists = useQueries({ queries: rosters.map((r) => threadsQuery(r.site)), combine: dataOf });
  return useMemo(
    () =>
      unreadThreads(
        rosters.map((r, i) => ({ site: r.site, threads: lists[i] })),
        waiting,
        picked,
      ),
    [rosters, lists, waiting, picked],
  );
}

/**
 * 每台的秘書顯示（頭像、名字、顏色）：`viewOf(台, 職位 id)`。只有連得上的台有名冊；
 * 找不到那台（剛斷線）時給那台 key 的空白名字，不拿主伺服器的名冊去猜（兩台的 id 會撞）。
 */
export function useSiteViews(): (site: SiteRef, id: string | undefined) => AgentView {
  const rosters = useSiteAgents();
  return useCallback(
    (site: SiteRef, id: string | undefined) => {
      const found = rosters.find((r) => r.site.key === site.key);
      if (found) return found.view(id);
      return { id: id ?? "", key: agentKey(site.key, id ?? ""), name: id ?? "", duty: "" };
    },
    [rosters],
  );
}

/** 用台的 key 找那台，只回連得上的（主伺服器一定回）；別台不是 ok 就是 undefined（不打它的 api）。 */
export function useUsableSite(key: string): Site | undefined {
  const sites = useSites();
  const site = sites.find((s) => s.key === key);
  return site && (site.primary || site.state === "ok") ? site : undefined;
}
