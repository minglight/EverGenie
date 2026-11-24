import {
  type QueryKey,
  type QueryObserverResult,
  type UseQueryOptions,
  useQueries,
} from "@tanstack/react-query";
import { useMemo } from "react";
import { type Site, siteUsable, useSites } from "./sites";

// 合併好幾台的清單（S3-7b：任務、待辦、Goal、權限申請；FEN-28）。
// 每台各抓各的（key 經 siteKey 帶台名），畫面合成一份、每筆記得自己是哪台的。
// 只有一台時就是主伺服器那一份，跟 S3 之前同一個查詢、同一個 key。

/** 一筆資料跨台不撞的 key：S3-7a 的那一個（data/waiting.ts），這裡轉出來給合併清單用。 */
export { itemKey } from "./waiting";

/** 連得上、但這一頁的資料讀失敗的別台（台的 key），交給 SiteProblems 的 failed；主伺服器自己讀不到由頁面顯示。 */
export function failedSites(parts: readonly SitePart<unknown>[]): string[] {
  return parts
    .filter((p) => !p.site.primary && p.isError && p.data === undefined)
    .map((p) => p.site.key);
}

/** 可以抓資料的台：主伺服器一定在（它自己的錯誤照舊由頁面顯示）；別台只有 ok 的。 */
export function usableSites(sites: readonly Site[]): Site[] {
  return sites.filter(siteUsable);
}

/** 一台的那一份。 */
export interface SitePart<T> {
  site: Site;
  data: T | undefined;
  isError: boolean;
  refetch: () => void;
}

interface Slim<T> {
  data: T | undefined;
  isError: boolean;
  refetch: () => void;
}

function slim<T>(results: readonly QueryObserverResult<T, Error>[]): Slim<T>[] {
  return results.map((r) => ({
    data: r.data,
    isError: r.isError,
    refetch: () => void r.refetch(),
  }));
}

/**
 * 每台各抓一份（主伺服器在第一個；別台只抓 ok 的）。`make` 照台給 queryOptions（key 要經 siteKey）。
 * 只有一台時就是 `[主伺服器那一份]`。
 */
export function useAcrossSites<T>(
  make: (site: Site) => UseQueryOptions<T, Error, T, QueryKey>,
): SitePart<T>[] {
  const sites = useSites();
  const usable = useMemo(() => usableSites(sites), [sites]);
  const results = useQueries({ queries: usable.map(make), combine: slim<T> });
  return useMemo(
    () =>
      usable.map((site, i) => ({
        site,
        data: results[i]?.data,
        isError: results[i]?.isError ?? false,
        refetch: results[i]?.refetch ?? (() => {}),
      })),
    [usable, results],
  );
}

/** 每筆標上它是哪台的。 */
export interface OnSite<T> {
  site: Site;
  item: T;
}

/** 把每台的清單接成一份（照台的順序：主伺服器在前；排序交給呼叫端）。還沒抓到的台略過。 */
export function flattenParts<T>(parts: readonly SitePart<readonly T[]>[]): OnSite<T>[] {
  return parts.flatMap((p) => (p.data ?? []).map((item) => ({ site: p.site, item })));
}

/** 每台的計數（key 已經帶台名，例如 countByAgent 的結果）合成一份。 */
export function mergeCounts(parts: readonly Record<string, number>[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const part of parts) {
    for (const [k, n] of Object.entries(part)) out[k] = (out[k] ?? 0) + n;
  }
  return out;
}

/** 某個數字（例如「等你」幾張）加總所有抓到的台；全部都還沒抓到時是 undefined。 */
export function sumParts<T>(
  parts: readonly SitePart<T>[],
  count: (data: T) => number,
): number | undefined {
  let seen = false;
  let total = 0;
  for (const p of parts) {
    if (p.data === undefined) continue;
    seen = true;
    total += count(p.data);
  }
  return seen ? total : undefined;
}
