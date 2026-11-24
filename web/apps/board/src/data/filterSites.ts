import { useQueries } from "@tanstack/react-query";
import { useMemo } from "react";
import { t } from "../i18n/zh-TW";
import { agentKey, type Presence, presenceOf, useSiteAgents } from "./agents";
import { threadsQuery } from "./queries";
import { dataOf, type Site, useSites } from "./sites";

// 秘書篩選「只看這位」（桌機側欄、手機小卡列）的名單：連多台時要有別台的秘書（S3 收尾，作者 2026-11-02「agent filter 那邊也要有」）；畫面用 mergeFilterSites 把兩台同一個職位合成一個頭像（作者 2026-11-02）。
// 每台一段：主伺服器在第一個；能用的台列她那台啟用中的秘書（看板上的卡，合併過），不能用的台那段沒有秘書、
// 畫面寫 SiteNotice 的一行灰字（不整段消失）。只有一台時就是主伺服器那一段，畫面跟 S3 之前一樣。

export interface FilterAgent {
  /** agentKey：主伺服器就是職位 id，別台是「台名/職位 id」（篩選、件數、React key 都用它）。 */
  key: string;
  id: string;
  name: string;
  duty: string;
  color?: string;
  colorDark?: string;
  /** 頭像，已補成那台的網址。 */
  avatarUrl?: string;
  presence: Presence;
}

export interface FilterSite {
  site: Site;
  /** 那台啟用中的秘書；不能用的台（或名冊還沒到）是空的。 */
  agents: FilterAgent[];
}

export function useFilterSites(): FilterSite[] {
  const sites = useSites();
  const rosters = useSiteAgents();
  // 每台能用的才抓（useSiteAgents 只回能用的台）；主伺服器跟 S3 之前的 useQuery(threadsQuery()) 是同一個 key。
  const threads = useQueries({
    queries: rosters.map(({ site }) => threadsQuery(site)),
    combine: dataOf,
  });
  return useMemo(
    () =>
      sites.map((site): FilterSite => {
        const at = rosters.findIndex((r) => r.site.key === site.key);
        const roster = rosters[at];
        if (!roster) return { site, agents: [] };
        const mine = threads[at] ?? [];
        const agents = roster.list
          .filter((a) => a.enabled && a.id)
          .map((a): FilterAgent => {
            const id = a.id ?? "";
            const v = roster.view(id);
            return {
              key: agentKey(site.key, id),
              id,
              name: a.name ?? "",
              duty: a.duty ?? "",
              color: a.color || undefined,
              colorDark: a.color_dark || undefined,
              avatarUrl: v.avatarUrl,
              presence: presenceOf(mine, id, site.key).presence,
            };
          });
        return { site, agents };
      }),
    [sites, rosters, threads],
  );
}

/** 篩選小卡的讀屏名字：「名字（職責）」，連多台時後面接「，在 <台名>」（兩台都有同一位時分得出來）。 */
export function filterChipLabel(agent: Pick<FilterAgent, "name" | "duty">, site: string): string {
  const base = agent.duty
    ? t("filter.chip", { name: agent.name, duty: agent.duty })
    : t("filter.chip.noDuty", { name: agent.name });
  return site ? `${base}${t("filter.chip.site", { site })}` : base;
}

/** 篩選上的一個頭像：兩台都有同一個職位時合成一位（作者 2026-11-02「同職位的兩個 Site 的圖像變成一個」）。 */
export interface FilterPerson extends FilterAgent {
  /** 有沒有哪一台的她在工作（頭像的光圈）：不能只看合起來的狀態，不然一台等你、一台工作時光圈就不見了。 */
  working: boolean;
  /** 她在每台的 agentKey（照台的順序；`key` 是第一個）。件數加總、點了選的都是這幾個。 */
  keys: string[];
  /** 她在哪幾台（照台的順序）。 */
  sites: Site[];
}

/**
 * 每台一段 → 一排頭像：照台、照名冊順序，同一個職位 id 併成一位（名字、頭像、presence 用第一台的；頭像缺了用別台補；
 * 有一台在工作就 working＝亮工作中的光圈；不把兩台的狀態合成一個，作者 2026-11-02「比較要緊那不就有一個看不到」）。`down` 是名冊沒有秘書的台（不能用、還沒到），畫面在頭像後面寫一行灰字（SiteNotice）。
 */
export function mergeFilterSites(sections: readonly FilterSite[]): {
  people: FilterPerson[];
  down: Site[];
} {
  const people: FilterPerson[] = [];
  const byId = new Map<string, FilterPerson>();
  const down: Site[] = [];
  for (const { site, agents } of sections) {
    if (agents.length === 0) down.push(site);
    for (const a of agents) {
      const seen = byId.get(a.id);
      if (!seen) {
        const p: FilterPerson = {
          ...a,
          keys: [a.key],
          sites: [site],
          working: a.presence === "working",
        };
        byId.set(a.id, p);
        people.push(p);
        continue;
      }
      seen.keys.push(a.key);
      seen.sites.push(site);
      if (a.presence === "working") seen.working = true;
      if (!seen.avatarUrl && a.avatarUrl) seen.avatarUrl = a.avatarUrl;
    }
  }
  return { people, down };
}

/** 一位在這一頁的件數：她在每台的件數加起來。 */
export function personCount(p: Pick<FilterPerson, "keys">, counts: Record<string, number>): number {
  return p.keys.reduce((n, k) => n + (counts[k] ?? 0), 0);
}

/** 一台的一位（今天的秘書卡片用）：id 是職位 id，key 是那台的 agentKey，n 是她在那台的件數。 */
export interface SiteRow<T> {
  id: string;
  key: string;
  site: Site;
  n: number;
  presence: Presence;
  data: T;
}

/**
 * 照台、照名冊順序的一列列 → 一位一張：同一個職位 id 合起來（key、data、presence 用第一台的；件數加總；
 * sites 記她在哪幾台、each 記每台各自的狀態，畫面在兩台時一台一行，不合成一個）。跟 mergeFilterSites 同一套規則（作者 2026-11-02：兩台同一個職位是同一位）。
 */
export interface MergedRow<T> extends SiteRow<T> {
  sites: Site[];
  /** 每台的她各自的狀態（照台的順序）：卡片在兩台時一台一行，不只寫合起來的那個。 */
  each: { site: Site; presence: Presence }[];
}

export function mergeSiteRows<T>(rows: readonly SiteRow<T>[]): MergedRow<T>[] {
  const out: MergedRow<T>[] = [];
  const byId = new Map<string, MergedRow<T>>();
  for (const r of rows) {
    const seen = byId.get(r.id);
    if (!seen) {
      const card = { ...r, sites: [r.site], each: [{ site: r.site, presence: r.presence }] };
      byId.set(r.id, card);
      out.push(card);
      continue;
    }
    seen.sites.push(r.site);
    seen.each.push({ site: r.site, presence: r.presence });
    seen.n += r.n;
  }
  return out;
}
