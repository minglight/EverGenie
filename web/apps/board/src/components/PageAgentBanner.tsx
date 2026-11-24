import { cn, PersonaMedia } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { presenceOf, presenceText, useAgentSite, useAgents } from "../data/agents";
import { threadsQuery } from "../data/queries";
import type { Site } from "../data/sites";
import { t } from "../i18n/zh-TW";
import { statusTone, toneDot } from "../lib/statusTone";
import { useDocumentVisible } from "../lib/visible";
import { SiteNotice } from "./SiteNotice";

// 套件頁頂端「負責這頁的秘書」橫幅（REQ-236，照舊看板的健身／飲食頁）：右邊是她現在穿的那套（循環影片，方形），
// 往左淡進底色；左邊是狀態、名字、職責。寬的畫面不把半身照硬裁成一條，臉才看得到。
// `id` 是 agentKey（主伺服器就是職位 id；別台的秘書是「台名/職位 id」，名冊、狀態、頭像都讀那台）；
// 那台現在不能用時只放一行 SiteNotice，不改顯示主伺服器的人。
export function PageAgentBanner({ id }: { id: string }) {
  const { site, id: agentId, usable } = useAgentSite(id);
  if (!site) return null;
  if (!usable) return <SiteNotice site={site} />;
  return <Banner site={site} id={agentId} />;
}

function Banner({ site, id }: { site: Site; id: string }) {
  const { view } = useAgents(site);
  const { data: threads } = useQuery(threadsQuery(site));
  const visible = useDocumentVisible();
  const agent = view(id);
  const { presence } = presenceOf(threads ?? [], id, site.key);
  return (
    <section
      aria-label={t("pageAgent.label", { name: agent.name })}
      data-testid="page-agent-banner"
      data-agent={id}
      className="relative h-[168px] overflow-hidden rounded-[22px] border border-border bg-surface-2 md:h-[208px]"
    >
      <PersonaMedia
        name={agent.name}
        src={agent.avatarUrl}
        video={agent.videoUrl}
        videos={agent.videoUrls}
        color={agent.color}
        colorDark={agent.colorDark}
        paused={!visible}
        scrimHeight={0}
        className="absolute inset-y-0 right-0 aspect-square h-full"
      />
      <div
        aria-hidden="true"
        className="absolute inset-y-0 right-0 aspect-square h-full bg-linear-to-r from-surface-2 via-transparent to-transparent"
      />
      <div className="relative flex h-full max-w-[calc(100%-120px)] flex-col justify-end gap-1 p-5 md:max-w-[calc(100%-200px)]">
        <p className="flex items-center gap-2 text-[13px] text-muted">
          <span className={cn("size-2 shrink-0 rounded-full", toneDot[statusTone(presence)])} />
          {t(presenceText[presence])}
        </p>
        <p className="font-display text-[28px] leading-tight font-bold break-words">{agent.name}</p>
        {agent.duty ? (
          <p className="line-clamp-2 text-[13px] leading-snug text-muted">{agent.duty}</p>
        ) : null}
      </div>
    </section>
  );
}
