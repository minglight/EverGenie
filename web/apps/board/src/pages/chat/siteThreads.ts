import type { ThreadSummary } from "@evergenie/sdk-web";
import { useQueries } from "@tanstack/react-query";
import { useMemo } from "react";
import { countByAgent } from "../../data/agentFilter";
import { threadsQuery } from "../../data/queries";
import { type Site, siteUsable, useSites } from "../../data/sites";
import { needsReply } from "../../lib/statusTone";

// 聊天照電腦分組（S3-6，計畫設計決定 4、5）：每台各自的討論串清單（session.list_threads），key 帶台名（FEN-28）。
// 只抓主伺服器與 state＝ok 的台；別台不是 ok 時沒有資料，畫面只在那一段寫原因，其他台照常。
// 只有一台時就是 `[主伺服器]`，查詢跟 S3 之前的 useQuery(threadsQuery()) 是同一個 key。

export interface SiteThreads {
  site: Site;
  /** 這台可以抓資料、可以寫（主伺服器，或別台 state＝ok）。 */
  usable: boolean;
  data: ThreadSummary[] | undefined;
  isPending: boolean;
  isError: boolean;
  refetch: () => void;
}

interface ThreadsResult {
  data: ThreadSummary[] | undefined;
  isPending: boolean;
  isError: boolean;
  refetch: () => unknown;
}

/** useQueries 的 combine：放在模組層（參考不變），結果沒變時拿到同一個陣列，下面的 useMemo 才不會每次重算。 */
function pickThreads(results: readonly ThreadsResult[]): ThreadsResult[] {
  return results.map(({ data, isPending, isError, refetch }) => ({
    data,
    isPending,
    isError,
    refetch,
  }));
}

export function useSiteThreads(): SiteThreads[] {
  const sites = useSites();
  const results = useQueries({
    // 不留上一個 key 的資料：useQueries 照位置對，台的順序一變（刪一台、去重）就會把上一台的串畫在這台底下。
    queries: sites.map((site) => ({
      ...threadsQuery(site),
      enabled: siteUsable(site),
      placeholderData: undefined,
    })),
    combine: pickThreads,
  });
  return useMemo(
    () =>
      sites.map((site, i) => {
        const r = results[i];
        return {
          site,
          usable: siteUsable(site),
          data: r?.data,
          isPending: r?.isPending ?? true,
          isError: r?.isError ?? false,
          refetch: () => void r?.refetch(),
        };
      }),
    [sites, results],
  );
}

/** 某台某串的摘要（`?home` 指的那台）。 */
export function findThread(
  all: readonly SiteThreads[],
  siteKey: string,
  name: string,
): ThreadSummary | undefined {
  return all.find((s) => s.site.key === siteKey)?.data?.find((th) => th.name === name);
}

/** 秘書篩選的徽章（等你處理的串數，key 是 agentKey）與清單裡的總串數，加總每台。 */
export function chatCounts(all: readonly SiteThreads[]): {
  counts: Record<string, number>;
  total: number;
} {
  const counts: Record<string, number> = {};
  let total = 0;
  for (const s of all) {
    // 不能用的台：查詢停了但快取還在，舊的件數不算進徽章。
    if (!s.usable) continue;
    const list = s.data ?? [];
    total += list.length;
    Object.assign(
      counts,
      countByAgent(
        list.filter((th) => needsReply(th)),
        (th) => th.agent,
        s.site.key,
      ),
    );
  }
  return { counts, total };
}
