import { cn } from "@evergenie/ui";
import { Link } from "@tanstack/react-router";
import { tabClass } from "../../components/PageTabs";
import { siteNoticeText } from "../../components/SiteNotice";
import { siteName } from "../../components/SiteTag";
import { presenceText } from "../../data/agents";
import { homeSearch, type Site, siteUsable } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { statusTone, toneDot } from "../../lib/statusTone";
import { type ChatAgent, useChatAgents } from "./chatAgents";

// 切電腦的分頁（作者 2026-11-02「以前上面那條會有個切換的 tab [Office | Home]」，WEB-34，S3 的多台看板用回來）：
// 討論串標題欄下面一排底線分頁，列這位秘書（同一個職位 id）在哪幾台，每個分頁寫那台的她現在的狀態（兩台各看各的）。
// 點了到那台她的主要討論串（`/chat/<職位 id>`，別台帶 ?home=）。現在連不上的台也列成灰的分頁、寫「連不上」
// （不知道她在不在那台，但不讓那台無聲消失，frontend 規範「不能用的台只說那台」）。
// 分頁不到兩個（只連一台、她只在一台而且別台都能用）時整排不出現。

export type SiteTab =
  | { kind: "agent"; site: Site; agent: ChatAgent }
  | { kind: "unavailable"; site: Site };

/** 這位秘書（`id`）的分頁：她在的每台（照台的順序，只算啟用中的）＋現在不能用的台。不到兩個回空陣列。 */
export function siteTabsFor(
  agents: readonly ChatAgent[],
  sites: readonly Site[],
  id: string,
): SiteTab[] {
  const tabs: SiteTab[] = [];
  for (const site of sites) {
    const agent = agents.find((a) => a.enabled && a.id === id && a.site.key === site.key);
    if (agent) tabs.push({ kind: "agent", site, agent });
    else if (!siteUsable(site)) tabs.push({ kind: "unavailable", site });
  }
  return tabs.length >= 2 && tabs.some((x) => x.kind === "agent") ? tabs : [];
}

export function SiteTabs({ agentId, site }: { agentId: string; site: Site }) {
  const { agents, multi, sites } = useChatAgents();
  const tabs = multi ? siteTabsFor(agents, sites, agentId) : [];
  if (tabs.length === 0) return null;
  const agentName = tabs.find((x) => x.kind === "agent")?.agent.name ?? agentId;
  return (
    <nav
      aria-label={t("chat.site.tabs", { name: agentName })}
      data-testid="site-tabs"
      className="flex shrink-0 items-end gap-1 overflow-x-auto border-b border-border bg-surface px-3"
    >
      {tabs.map((tab) => {
        const name = siteName(tab.site);
        const key = tab.site.key || "primary";
        if (tab.kind === "unavailable") {
          const state = t(tab.site.state === "checking" ? "chat.site.checking" : "chat.site.down");
          return (
            <button
              key={key}
              type="button"
              disabled
              aria-label={t("chat.site.tab", { site: name, state })}
              title={siteNoticeText(tab.site) ?? undefined}
              data-site={tab.site.key}
              data-state={tab.site.state}
              className={cn(
                tabClass(false),
                "shrink-0 cursor-default gap-1.5 whitespace-nowrap opacity-60 hover:text-muted",
              )}
            >
              <span aria-hidden="true" className="size-2 shrink-0 rounded-full bg-muted" />
              <span>{name}</span>
              <span className="text-xs font-normal">{state}</span>
            </button>
          );
        }
        const here = tab.site.key === site.key;
        const state = t(presenceText[tab.agent.presence]);
        return (
          <Link
            key={key}
            to="/chat/$session"
            params={{ session: agentId }}
            search={homeSearch(tab.site)}
            activeOptions={{ exact: true, includeSearch: false }}
            aria-current={here ? "page" : undefined}
            aria-label={t("chat.site.tab", { site: name, state })}
            data-site={tab.site.key}
            data-presence={tab.agent.presence}
            className={cn(tabClass(here), "shrink-0 gap-1.5 whitespace-nowrap")}
          >
            <span
              aria-hidden="true"
              className={cn(
                "size-2 shrink-0 rounded-full",
                toneDot[statusTone(tab.agent.presence)],
              )}
            />
            <span>{name}</span>
            <span className="text-xs font-normal text-muted">{state}</span>
          </Link>
        );
      })}
    </nav>
  );
}
