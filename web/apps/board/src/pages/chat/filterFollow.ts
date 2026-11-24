import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { agentFilterLocalPicks, useAgentFilter } from "../../data/agentFilter";
import { agentKey, primaryOf, splitAgentKey, useSiteAgents } from "../../data/agents";
import { homeSearch } from "../../data/sites";
import { useStore } from "../../data/store";

// 秘書篩選管全部，聊天也一樣（作者 2026-11-02「左邊的 filter for everything，包含聊天」，WEB-19）：
// - 開著一串對話時，在這個分頁的篩選換人：對話跟著換到她的主要串（同一台有她就留在這台，不然到她第一台）。
//   別的分頁改了篩選不拉這個分頁的對話。
// - 有選人時打開別位的對話（換秘書的浮窗、通知、切電腦分頁、網址）：篩選跟著換成她，左邊的頭像不會跟對話對不上。
// - 篩選是「全部」時兩邊都不動。

/** 篩選換人後要去哪一串：選中的那位在這一台有就這台，不然第一個；已經是她就不用動（回 null）。 */
export function followTarget(
  picked: readonly string[],
  current: string,
): { session: string; site: string } | null {
  if (picked.length === 0 || picked.includes(current)) return null;
  const here = splitAgentKey(current).site;
  const key = picked.find((k) => splitAgentKey(k).site === here) ?? picked[0];
  if (!key) return null;
  const { site, id } = splitAgentKey(key);
  return { session: id, site };
}

export type FollowStep = "none" | "navigate" | "pick";

/**
 * 這一次要做什麼。`userPicked`＝這個分頁的使用者剛改了篩選；`currentChanged`＝這串的秘書剛對上（名冊到了）或換了；
 * `aligned`＝這串打開以來已經對齊過一次。
 */
export function followStep(o: {
  picked: readonly string[];
  current: string;
  userPicked: boolean;
  currentChanged: boolean;
  aligned: boolean;
}): FollowStep {
  if (o.current === "" || o.picked.length === 0 || o.picked.includes(o.current)) return "none";
  if (o.userPicked && !o.currentChanged) return "navigate";
  // 剛打開這串（或這串的秘書剛對上）而篩選是別人：篩選換成她。其他（別的分頁改的、名冊重讀）不動。
  if (!o.aligned || o.currentChanged) return "pick";
  return "none";
}

/**
 * `agentId` 是這串的秘書（還不知道就 undefined）；被併的職位照卡算（primaryOf）；那台的名冊還沒到、或名冊裡對不到她時不動，
 * 免得拿還沒對好的 id 改掉篩選（這時在篩選換人，對話也不跟著換）。
 */
export function useFilterFollowsChat(agentId: string | undefined, siteKey: string) {
  const { picked, only } = useAgentFilter();
  const localPicks = useStore(agentFilterLocalPicks);
  const navigate = useNavigate();
  const rosters = useSiteAgents();
  const id = agentId ? primaryOf(agentId, siteKey) : "";
  const known =
    id !== "" &&
    (rosters.find((r) => r.site.key === siteKey)?.list ?? []).some((a) => a.enabled && a.id === id);
  const current = known ? agentKey(siteKey, id) : "";
  const lastPicks = useRef(localPicks);
  const lastCurrent = useRef(current);
  const aligned = useRef(false);

  useEffect(() => {
    const step = followStep({
      picked,
      current,
      userPicked: localPicks !== lastPicks.current,
      currentChanged: current !== lastCurrent.current,
      aligned: aligned.current,
    });
    lastPicks.current = localPicks;
    lastCurrent.current = current;
    if (current !== "") aligned.current = true;
    if (step === "navigate") {
      const to = followTarget(picked, current);
      if (to)
        void navigate({
          to: "/chat/$session",
          params: { session: to.session },
          search: homeSearch({ key: to.site }),
        });
    } else if (step === "pick") {
      only(current);
    }
  }, [picked, current, localPicks, only, navigate]);
}
