import { cn, PersonaMedia, StatusDot } from "@evergenie/ui";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { SiteNotice } from "../../components/SiteNotice";
import { SiteTag, siteVars } from "../../components/SiteTag";
import { matchAgent, useAgentFilter } from "../../data/agentFilter";
import { presenceText } from "../../data/agents";
import { homeSearch } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { statusTone, toneText } from "../../lib/statusTone";
import { useChatAgents } from "./chatAgents";

// 桌機聊天沒選討論串時的右欄：秘書們的角色卡（直式人像、名字、職責、現在的狀態），點了進她最近的討論串。
// 影片只在滑鼠移上去（或鍵盤聚焦）時播，不讓一整排同時在動。跟著秘書篩選走。
// 連多台時每台各自的秘書都有卡（同一位角色在兩台是兩張），卡上標台名、外框用那台的顏色（S3-6）；
// 不能用的台沒有卡，卡片上方每台一行 SiteNotice 說為什麼。
export function ChatGallery() {
  const { agents: all, multi, sites } = useChatAgents();
  const { picked } = useAgentFilter();
  const [live, setLive] = useState<string | null>(null);
  const agents = all.filter((a) => a.enabled && matchAgent(picked, a.id, a.site.key));
  return (
    <div className="hg-scroll min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto flex max-w-[880px] flex-col gap-5 px-8 pt-9 pb-10">
        <div className="flex flex-col gap-1">
          <h2 className="font-display text-[26px] leading-tight font-bold">
            {t("chat.gallery.title")}
          </h2>
          <p className="text-sm text-muted">{t("chat.pick")}</p>
        </div>
        {sites.map((site) => (
          <SiteNotice key={site.key || "primary"} site={site} />
        ))}
        <ul className="m-0 grid list-none grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-4 p-0">
          {agents.map((agent) => {
            const id = agent.key;
            const { presence, session } = agent;
            const card = (
              <>
                <PersonaMedia
                  name={agent.name}
                  src={agent.avatarUrl}
                  video={live === id ? agent.videoUrl : undefined}
                  videos={live === id ? agent.videoUrls : undefined}
                  color={agent.color}
                  colorDark={agent.colorDark}
                  scrimHeight={110}
                  className={cn(
                    "aspect-[3/4] w-full rounded-[18px]",
                    multi && "shadow-[0_0_0_3px_var(--site-color)]",
                  )}
                >
                  {multi ? (
                    <span className="absolute top-2.5 left-2.5 flex max-w-[calc(100%-20px)]">
                      <SiteTag site={agent.site} className="bg-media-btn text-on-media" />
                    </span>
                  ) : null}
                  <div className="absolute right-4 bottom-3.5 left-4 text-on-media">
                    <p className="font-display truncate text-[22px] leading-tight font-bold">
                      {agent.name}
                    </p>
                    {agent.duty ? (
                      <p className="mt-0.5 truncate text-xs text-on-media-soft">{agent.duty}</p>
                    ) : null}
                  </div>
                </PersonaMedia>
                <span
                  className={cn(
                    "flex items-center gap-1.5 px-1 text-[13px] font-medium",
                    toneText[statusTone(presence)],
                  )}
                >
                  {presence === "waiting" ? <StatusDot size={8} /> : null}
                  {t(presenceText[presence])}
                  {session ? (
                    <span className="ml-auto text-muted">{t("chat.gallery.open")}</span>
                  ) : null}
                </span>
              </>
            );
            const cls =
              "flex flex-col gap-2 rounded-[20px] p-1.5 text-fg transition-colors hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";
            return (
              <li
                key={id}
                style={multi ? siteVars(agent.site) : undefined}
                data-site={agent.site.key}
                onPointerEnter={() => setLive(id)}
                onPointerLeave={() => setLive((cur) => (cur === id ? null : cur))}
                onFocus={() => setLive(id)}
                onBlur={() => setLive((cur) => (cur === id ? null : cur))}
              >
                {session ? (
                  <Link
                    to="/chat/$session"
                    params={{ session }}
                    search={homeSearch(agent.site)}
                    className={cls}
                  >
                    {card}
                  </Link>
                ) : (
                  <div className={cls}>{card}</div>
                )}
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}
