import { useNavigate } from "@tanstack/react-router";
import { useCallback, useMemo } from "react";
import { useMultiSite } from "../../components/SiteTag";
import { type AgentView, type Presence, presenceOf, useSiteAgents } from "../../data/agents";
import { homeSearch, type Site, useSites } from "../../data/sites";
import { useSiteThreads } from "./siteThreads";

// 聊天頁的秘書們（換秘書的浮窗、標題欄那排頭像、桌機的角色卡）：連多台時每台各自一份，
// 同一位角色在兩台是兩張卡（計畫設計決定 5，記憶各自獨立），key 用 agentKey（主伺服器就是職位 id）。
// 只有一台時就是主伺服器的名冊，順序、內容跟 S3 之前一樣。

export interface ChatAgent extends AgentView {
  site: Site;
  enabled: boolean;
  presence: Presence;
  /** 點了要去的討論串（她的主要串）；沒有就去聊天清單。 */
  session?: string;
}

/**
 * `sites` 是看板連的所有台（主伺服器在第一個，含現在不能用的）：照電腦分段的畫面每台一段，
 * 不能用的台那段沒有秘書、寫 SiteNotice（不整段消失）。
 */
export function useChatAgents(): { agents: ChatAgent[]; multi: boolean; sites: Site[] } {
  const sites = useSites();
  const rosters = useSiteAgents();
  const threads = useSiteThreads();
  const agents = useMemo(
    () =>
      rosters.flatMap(({ site, list, view }) => {
        const mine = threads.find((s) => s.site.key === site.key)?.data ?? [];
        return list
          .filter((a) => a.id)
          .map((a): ChatAgent => {
            const id = a.id ?? "";
            const { presence, session } = presenceOf(mine, id, site.key);
            return { ...view(id), site, enabled: Boolean(a.enabled), presence, session };
          });
      }),
    [rosters, threads],
  );
  return { agents, multi: useMultiSite(), sites };
}

/** 到這位秘書的主要討論串（別台帶 ?home=）；她沒有討論串就到聊天清單。 */
export function useGoToAgent(): (agent: Pick<ChatAgent, "session" | "site">) => void {
  const navigate = useNavigate();
  return useCallback(
    (agent) => {
      if (agent.session)
        void navigate({
          to: "/chat/$session",
          params: { session: agent.session },
          search: homeSearch(agent.site),
        });
      else void navigate({ to: "/chat" });
    },
    [navigate],
  );
}
