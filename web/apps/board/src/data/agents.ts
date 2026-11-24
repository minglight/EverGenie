import type { RosterAgent, ThreadSummary } from "@evergenie/sdk-web";
import { useQueries, useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { type TextKey, t } from "../i18n/zh-TW";
import { needsReply } from "../lib/statusTone";
import { rosterQuery } from "./queries";
import { dataOf, PRIMARY, type Site, type SiteRef, siteHref, siteUsable, useSites } from "./sites";

export interface AgentView {
  id: string;
  /** 跨台不撞的 key（`agentKey`）：主伺服器就是 id，別台是「台名/id」。 */
  key: string;
  name: string;
  color?: string;
  colorDark?: string;
  avatarUrl?: string;
  /** 職責（名冊的 duty）；沒有就是空字串。 */
  duty: string;
  /** 人設的循環影片；沒有就是 undefined（只放圖）。 */
  videoUrl?: string;
  /** 所有動態影片（同一張圖最多 6 支，每次隨機挑一支播，DEV-125）；沒有就是 undefined。 */
  videoUrls?: string[];
}

/**
 * 一位秘書跨台不撞的 key（篩選、計數、React key 用）：主伺服器就是職位 id（跟 S3 之前一樣）；
 * 別台是「台名/職位 id」——兩台都有 code 時是 `code` 與 `amy-office/code`。職位 id 與台名都不含「/」。
 */
export function agentKey(site: string, id: string): string {
  return site === "" ? id : `${site}/${id}`;
}

/** agentKey 拆回台名與職位 id（沒有「/」＝主伺服器）。 */
export function splitAgentKey(key: string): { site: string; id: string } {
  const cut = key.indexOf("/");
  return cut < 0 ? { site: "", id: key } : { site: key.slice(0, cut), id: key.slice(cut + 1) };
}

/** 每台一份：名冊裡被併進別人的職位 → 那張卡的職位 id（篩選、計數、狀態都照卡算）。key 是台的 key（主伺服器 ""）。 */
const primaryBySite = new Map<string, Map<string, string>>();

/** 這個職位在那台的看板上算哪一張卡（沒被合併就是自己）；site 預設主伺服器。 */
export function primaryOf(id: string, site = ""): string {
  return primaryBySite.get(site)?.get(id) ?? id;
}

/**
 * 看板上的秘書名單（WEB-49）：同一位角色（persona）坐幾個職位就合成一張，
 * 卡的 id 用名冊裡第一個職位，職責依序用「；」併在一起。回傳合併表（被併的職位 → 卡）。
 */
export function groupRoster(raw: readonly RosterAgent[]): {
  people: RosterAgent[];
  merged: Map<string, string>;
} {
  const people: RosterAgent[] = [];
  const merged = new Map<string, string>();
  const byPersona = new Map<string, RosterAgent>();
  for (const a of raw) {
    if (!a.id) continue;
    const owner = a.persona ? byPersona.get(a.persona) : undefined;
    if (!owner) {
      const card = { ...a };
      people.push(card);
      if (a.persona) byPersona.set(a.persona, card);
      continue;
    }
    merged.set(a.id, owner.id ?? "");
    if (a.duty) owner.duty = owner.duty ? `${owner.duty}；${a.duty}` : a.duty;
    owner.enabled = owner.enabled || a.enabled;
  }
  return { people, merged };
}

/**
 * 秘書一律顯示人名（web.get_roster），不顯示職位 id（定案 11）。
 * `list` 是看板上的名單（合併過）；`all` 是每個職位一列（設定頁、排程這種照職位分的地方用）。
 */
export interface Agents {
  list: RosterAgent[];
  all: RosterAgent[];
  view: (id: string | undefined) => AgentView;
}

/** 一台的名冊 → 看板上的名單（合併過、記下那台的合併表）與 view；頭像、影片網址補成那台的網址。 */
function agentsOf(site: SiteRef, data: RosterAgent[] | undefined): Agents {
  const raw = data ?? [];
  const { people, merged } = groupRoster(raw);
  primaryBySite.set(site.key, merged);
  const view = (id: string | undefined): AgentView => {
    const found = people.find((a) => a.id === id) ?? raw.find((a) => a.id === id);
    if (!found) {
      return {
        id: id ?? "",
        key: agentKey(site.key, id ?? ""),
        name: id === "system" || !id ? t("agent.system") : t("agent.unknown"),
        duty: "",
      };
    }
    return {
      id: found.id ?? "",
      key: agentKey(site.key, found.id ?? ""),
      name: found.name || t("agent.unknown"),
      color: found.color || undefined,
      colorDark: found.color_dark || undefined,
      avatarUrl: siteHref(site, found.avatar_url || undefined),
      duty: found.duty ?? "",
      videoUrl: siteHref(site, found.video_url || undefined),
      videoUrls: found.video_urls?.length
        ? found.video_urls.map((u) => siteHref(site, u) ?? u)
        : undefined,
    };
  };
  return { list: people, all: [...raw], view };
}

export function useAgents(site: SiteRef = PRIMARY): Agents {
  const { data } = useQuery(rosterQuery(site));
  return useMemo(() => agentsOf(site, data), [site, data]);
}

export interface SiteAgents extends Agents {
  site: Site;
}

/**
 * 每台各自的名冊（主伺服器一定在第一個；別台只有連得上的 ok 才抓）。只有一台時就是主伺服器那一份，
 * 跟 useAgents() 同一個查詢。跨台顯示秘書（篩選列、合併的清單）用這個，key 用 view(id).key。
 */
export function useSiteAgents(): SiteAgents[] {
  const sites = useSites();
  const usable = useMemo(() => sites.filter(siteUsable), [sites]);
  const rosters = useQueries({ queries: usable.map((s) => rosterQuery(s)), combine: dataOf });
  return useMemo(
    () => usable.map((site, i) => ({ site, ...agentsOf(site, rosters[i]) })),
    [usable, rosters],
  );
}

/**
 * 一位秘書（agentKey）在哪台：`site` 是台清單裡的那台（主伺服器一定找得到；別台設定拿掉了＝undefined），
 * `id` 是那台的職位 id，`usable`＝那台現在能讀寫（主伺服器或 ok 的別台）。不能用時呼叫端寫 SiteNotice、不退回主伺服器。
 */
export function useAgentSite(key: string): { site: Site | undefined; id: string; usable: boolean } {
  const sites = useSites();
  const { site: at, id } = splitAgentKey(key);
  const site = sites.find((s) => s.key === at);
  return { site, id, usable: site ? siteUsable(site) : false };
}

export type Presence = "working" | "waiting" | "idle";

/** 狀態的白話（工作中／等你／有空），各處共用一份。 */
export const presenceText: Record<Presence, TextKey> = {
  working: "agent.working",
  waiting: "agent.waiting",
  idle: "agent.idle",
};

export interface AgentPresence {
  presence: Presence;
  /** 點頭像要去的討論串；這位秘書沒有討論串就是 undefined。 */
  session?: string;
}

function latestAt(t: ThreadSummary): number {
  return Date.parse(t.last_assistant_at || t.status_since || "") || 0;
}

/** 這位秘書現在的樣子：有串在等你 → 等你；有串在工作 → 工作中；其他 → 有空。threads 與 agentId 都是 site 那台的。 */
export function presenceOf(threads: ThreadSummary[], agentId: string, site = ""): AgentPresence {
  const mine = threads.filter((th) => primaryOf(th.agent ?? "", site) === agentId);
  // 排隊等名額不算在等你（WEB-35、WEB-70）。
  const waiting = mine.find((th) => needsReply(th));
  if (waiting) return { presence: "waiting", session: waiting.name };
  const working = mine.find((th) => th.status === "working");
  if (working) return { presence: "working", session: working.name };
  const latest = [...mine].sort((a, b) => latestAt(b) - latestAt(a))[0];
  return { presence: "idle", session: latest?.name };
}
