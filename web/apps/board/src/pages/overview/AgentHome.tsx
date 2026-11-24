import { Card, cn, PersonaMedia, StatusDot } from "@evergenie/ui";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, MessageCircle } from "lucide-react";
import { useState } from "react";
import { RowCode } from "../../components/RowCode";
import { ItemSiteTag, SiteRefTag, siteName, useMultiSite } from "../../components/SiteTag";
import { useAgentFilter } from "../../data/agentFilter";
import {
  presenceOf,
  presenceText,
  splitAgentKey,
  useAgents,
  useSiteAgents,
} from "../../data/agents";
import { mergeSiteRows } from "../../data/filterSites";
import { usePageCounts } from "../../data/pageCounts";
import { tasksQuery, threadsQuery } from "../../data/queries";
import { canLinkDetail } from "../../data/siteLinks";
import { dataOf, homeSearch, PRIMARY, useSites } from "../../data/sites";
import { type TextKey, t } from "../../i18n/zh-TW";
import { statusTone, toneText } from "../../lib/statusTone";
import { useDocumentVisible } from "../../lib/visible";
import { useUsableSite } from "./merged";
import { SectionHeading } from "./Sections";

// 桌機「今天」的秘書卡片（沒選人時）：直式半身照、名字、職責、等你處理幾件；
// 點了「只看這一位」，今天就變成她的首頁。影片只在滑鼠移上去（或鍵盤聚焦）時播。
// 看多台時兩台同一個職位合成一張卡（作者 2026-11-02，跟秘書篩選一樣）：件數加總；兩台都有她時一台一行寫各自的狀態，
// 只在一台的那位照舊一行狀態＋台名。卡的 key 是她第一台的 agentKey。
export function AgentStrip() {
  const rosters = useSiteAgents();
  const threadLists = useQueries({
    queries: rosters.map((r) => threadsQuery(r.site)),
    combine: dataOf,
  });
  const { only } = useAgentFilter();
  const counts = usePageCounts()?.counts ?? {};
  const [live, setLive] = useState<string | null>(null);
  const multi = useMultiSite();
  const cards = mergeSiteRows(
    rosters.flatMap((r, i) =>
      r.list
        .filter((a) => a.enabled && a.id)
        .map((a) => {
          const id = a.id ?? "";
          const agent = r.view(id);
          return {
            id,
            key: agent.key,
            site: r.site,
            n: counts[agent.key] ?? 0,
            presence: presenceOf(threadLists[i] ?? [], id, r.site.key).presence,
            data: agent,
          };
        }),
    ),
  );
  if (cards.length === 0) return null;
  return (
    <section aria-label={t("today.strip")} className="flex flex-col gap-2.5">
      <SectionHeading title={t("today.strip")} />
      <ul className="m-0 grid list-none grid-cols-[repeat(auto-fill,minmax(132px,1fr))] gap-3 p-0">
        {cards.map(({ key: id, data: agent, sites, each, n, presence }) => {
          return (
            <li
              key={id}
              onPointerEnter={() => setLive(id)}
              onPointerLeave={() => setLive((cur) => (cur === id ? null : cur))}
            >
              <button
                type="button"
                data-agent={id}
                data-sites={sites.length}
                aria-label={
                  t("today.strip.open", { name: agent.name }) +
                  (multi
                    ? t("filter.chip.site", {
                        site: (each.length > 1
                          ? each.map((e) =>
                              t("chat.site.tab", {
                                site: siteName(e.site),
                                state: t(presenceText[e.presence]),
                              }),
                            )
                          : sites.map(siteName)
                        ).join(t("filter.names.sep")),
                      })
                    : "")
                }
                onClick={() => only(id)}
                onFocus={() => setLive(id)}
                onBlur={() => setLive((cur) => (cur === id ? null : cur))}
                className="flex w-full flex-col gap-1.5 rounded-[18px] p-1 text-left text-fg transition-colors hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
              >
                <PersonaMedia
                  name={agent.name}
                  src={agent.avatarUrl}
                  video={live === id ? agent.videoUrl : undefined}
                  videos={live === id ? agent.videoUrls : undefined}
                  color={agent.color}
                  colorDark={agent.colorDark}
                  scrimHeight={80}
                  // 滑鼠移上去時照片會換成影片：按下時的 img 被換掉，瀏覽器就不送 click（2026-11-02 e2e 抓到）。
                  // 讓照片與影片不接滑鼠，按到的一律是外面的按鈕。
                  className="pointer-events-none aspect-[3/4] w-full rounded-[14px]"
                >
                  {n > 0 ? (
                    <span className="absolute top-2 right-2 rounded-full bg-attention px-2 py-0.5 text-xs font-bold text-on-attention">
                      {n}
                    </span>
                  ) : null}
                  <p className="font-display absolute right-3 bottom-2.5 left-3 truncate text-[17px] leading-tight font-bold text-on-media">
                    {agent.name}
                  </p>
                </PersonaMedia>
                {each.length > 1 ? (
                  // 兩台都有她：一台一行、各寫各的狀態（合成一個就有一台看不到，作者 2026-11-02）。
                  each.map((e) => (
                    <span
                      key={e.site.key || "primary"}
                      data-site={e.site.key}
                      data-presence={e.presence}
                      className={cn(
                        "flex items-center gap-1.5 px-1 text-xs font-medium",
                        toneText[statusTone(e.presence)],
                      )}
                    >
                      {e.presence === "waiting" ? <StatusDot size={7} /> : null}
                      <span className="truncate">{siteName(e.site)}</span>
                      <span className="ml-auto shrink-0">{t(presenceText[e.presence])}</span>
                    </span>
                  ))
                ) : (
                  <span
                    className={cn(
                      "flex items-center gap-1.5 px-1 text-xs font-medium",
                      toneText[statusTone(presence)],
                    )}
                  >
                    {presence === "waiting" ? <StatusDot size={7} /> : null}
                    {t(presenceText[presence])}
                    {sites[0] ? <SiteRefTag site={sites[0]} className="ml-auto" /> : null}
                  </span>
                )}
              </button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

// 只選一位時的「今天」頂端：她現在穿的那套半身照（循環影片）、名字、職責、狀態、幾個數字，跟去聊天、看全部。
// id 是篩選的 key（agentKey）：別台的秘書從那台讀名冊與討論串；那台現在讀不到就只畫名字（不拿主伺服器的資料去猜）。
export function AgentHero({
  id,
  waiting,
  reminders,
  ids = [],
  compact = false,
}: {
  id: string;
  /** 手機：照片小一點、名字小一點，跟文字並排放得下（2026-11-02 稽核：手機選一位也要有她的首頁）。 */
  compact?: boolean;
  /** 等你處理（要你同意、要你驗收或決定）幾件。 */
  waiting: number;
  /** 今天的待辦幾筆。 */
  reminders: number;
  /** 篩選選中的她在每台的 key（合成一位，作者 2026-11-02）：不只一台時狀態一台一行、各寫各的，不標台名。 */
  ids?: readonly string[];
}) {
  // 桌機寬版只看一位時，她的名字就是這頁的第一層標題（那裡沒有問候語）；compact（單欄）那頁已經有問候語或手機頁首（一頁一個 h1，DEV-156 U-31）。
  const Name = compact ? "h2" : "h1";
  const split = splitAgentKey(id);
  const usable = useUsableSite(split.site);
  const site = usable ?? PRIMARY;
  const { view } = useAgents(site);
  const { data: threads } = useQuery({ ...threadsQuery(site), enabled: usable !== undefined });
  const { clear } = useAgentFilter();
  const visible = useDocumentVisible();
  const agent = usable ? view(split.id) : { ...view(undefined), name: split.id };
  const mine = presenceOf(usable ? (threads ?? []) : [], split.id, site.key);
  const { session } = mine;
  // 別台的她（能用的台才讀）：跟卡片一樣一台一行寫各自的狀態。
  const sites = useSites();
  const others = ids.flatMap((k) => {
    const o = splitAgentKey(k);
    const at = sites.find((x) => x.key === o.site);
    return k !== id && at && (at.primary || at.state === "ok") ? [{ site: at, agent: o.id }] : [];
  });
  const otherThreads = useQueries({
    queries: others.map((o) => threadsQuery(o.site)),
    combine: dataOf,
  });
  // 兩台都有她時一台一行、各寫各的（合成一個就有一台看不到）；她自己那台在前。
  // 她第一台現在讀不到時那一行不畫（不把連不上的台說成有空）。
  const each = [
    ...(usable ? [{ key: site.key, name: siteName(usable), presence: mine.presence }] : []),
    ...others.map((o, i) => ({
      key: o.site.key,
      name: siteName(o.site),
      presence: presenceOf(otherThreads[i] ?? [], o.agent, o.site.key).presence,
    })),
  ];
  const everywhere = ids.length > 1;
  const stats: { label: string; n: number }[] = [
    { label: t("today.home.waiting"), n: waiting },
    { label: t("today.home.reminders"), n: reminders },
  ];
  const media = (
    <PersonaMedia
      name={agent.name}
      src={agent.avatarUrl}
      video={agent.videoUrl}
      videos={agent.videoUrls}
      color={agent.color}
      colorDark={agent.colorDark}
      paused={!visible}
      scrimHeight={0}
      className={cn(
        "aspect-[3/4] w-full shadow-[var(--shadow-card)]",
        compact ? "rounded-2xl" : "rounded-[22px]",
      )}
    />
  );
  return (
    <section
      aria-label={t("today.home.label", { name: agent.name })}
      data-testid="agent-hero"
      className={cn(
        "grid items-end",
        compact ? "grid-cols-[112px_1fr] gap-4" : "grid-cols-[minmax(180px,240px)_1fr] gap-7",
      )}
    >
      {/* 大頭像點了到核心的角色頁（那裡有「介紹｜衣櫃｜展示間」分頁）：手機底部五格放不下衣櫃，從她的照片進去（DEV-157、WEB-174）。
          別台的她不連（/personas 是這台的角色）；她那台現在讀不到時也不連。 */}
      {usable && site.key === "" ? (
        <Link
          to="/personas"
          aria-label={t("today.home.personas", { name: agent.name })}
          data-testid="hero-personas"
          className={cn(
            "block focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
            compact ? "rounded-2xl" : "rounded-[22px]",
          )}
        >
          {media}
        </Link>
      ) : (
        media
      )}
      <div className={cn("flex min-w-0 flex-col pb-1", compact ? "gap-2.5" : "gap-4")}>
        <div className="flex flex-col gap-1">
          {(everywhere && each.length > 1 ? each : each.slice(0, 1)).map((e) => (
            <p
              key={e.key || "primary"}
              data-testid="hero-presence"
              data-site={e.key}
              className={cn(
                "flex items-center gap-1.5 text-[13px] font-medium",
                toneText[statusTone(e.presence)],
              )}
            >
              {e.presence === "waiting" ? <StatusDot size={8} /> : null}
              {everywhere && each.length > 1 ? `${e.name} · ` : ""}
              {t(presenceText[e.presence])}
            </p>
          ))}
          <Name
            className={cn(
              "font-display leading-tight font-bold",
              compact ? "text-[26px]" : "text-[40px]",
            )}
          >
            {agent.name}
          </Name>
          {usable && !everywhere ? <SiteRefTag site={usable} className="w-fit" /> : null}
          {agent.duty ? (
            <p className={cn("text-sm text-muted", compact && "line-clamp-2 text-[13px]")}>
              {agent.duty}
            </p>
          ) : null}
        </div>
        <dl className={cn("m-0 flex", compact ? "gap-4" : "gap-6")}>
          {stats.map((s) => (
            <div key={s.label} className="flex flex-col">
              <dt className="order-2 text-xs text-muted">{s.label}</dt>
              <dd
                className={cn(
                  "order-1 m-0 font-bold tabular-nums",
                  compact ? "text-xl" : "text-2xl",
                )}
              >
                {s.n}
              </dd>
            </div>
          ))}
        </dl>
        <div className="flex flex-wrap items-center gap-2">
          {session ? (
            <Link
              to="/chat/$session"
              params={{ session }}
              search={homeSearch(site)}
              className="flex h-11 items-center gap-2 rounded-full bg-accent px-5 text-sm font-semibold text-accent-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
            >
              <MessageCircle className="size-4" strokeWidth={2} aria-hidden="true" />
              {t("today.home.chat", { name: agent.name })}
            </Link>
          ) : null}
          <button
            type="button"
            onClick={clear}
            className="flex h-11 items-center rounded-full px-4 text-sm font-medium text-muted hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus"
          >
            {t("today.home.all")}
          </button>
        </div>
      </div>
    </section>
  );
}

const TASK_LIMIT = 6;
const TASK_ROW =
  "flex min-h-12 items-center gap-3 px-3.5 py-2.5 hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus";

// 她手上還沒結束的任務單（進行中、等外部、待做；等你的已經在左邊的清單裡，這裡不重複）。
// ids 是篩選的 key（agentKey；兩台同一個職位是兩個，作者 2026-11-02 合成一個頭像）：每台從那台讀，連結帶 ?home=。
export function AgentTasks({ ids }: { ids: readonly string[] }) {
  const sites = useSites();
  const parts = ids.flatMap((id) => {
    const split = splitAgentKey(id);
    const site = sites.find((s) => s.key === split.site);
    return site && (site.primary || site.state === "ok") ? [{ site, agent: split.id }] : [];
  });
  const lists = useQueries({
    queries: parts.map((p) => tasksQuery("all", p.site)),
    combine: dataOf,
  });
  const mine = parts.flatMap((p, i) =>
    (lists[i] ?? [])
      .map((row) => row.task)
      .filter((task) => task.agent === p.agent && task.status !== "waiting_user")
      .map((task) => ({ task, site: p.site })),
  );
  if (mine.length === 0) return null;
  return (
    <section className="flex flex-col gap-2.5">
      <SectionHeading
        title={t("today.home.tasks")}
        count={mine.length}
        action={
          <Link to="/tasks" className="text-[13px] font-semibold text-accent">
            {t("today.home.tasks.all")}
          </Link>
        }
      />
      <Card className="divide-y divide-border overflow-hidden">
        {mine.slice(0, TASK_LIMIT).map(({ task, site }) => {
          const row = (
            <>
              <span className="shrink-0 rounded-md bg-surface-2 px-2 py-0.5 text-[11px] font-semibold text-muted">
                {t(`task.status.${task.status}` as TextKey)}
              </span>
              <ItemSiteTag site={site} />
              <span className="min-w-0 flex-1 truncate text-sm">
                <RowCode id={task.id} className="font-normal" />
                {task.title}
              </span>
              <ChevronRight className="size-4 shrink-0 text-muted" aria-hidden="true" />
            </>
          );
          // 別台的單詳情頁還不認 ?home（S3-7b 前）：只顯示、不連，免得打開這台同編號的那張。
          return canLinkDetail(site, "/tasks/$id") ? (
            <Link
              key={`${site.key}/${task.id}`}
              to="/tasks/$id"
              params={{ id: task.id ?? "" }}
              search={homeSearch(site)}
              className={TASK_ROW}
            >
              {row}
            </Link>
          ) : (
            <div key={`${site.key}/${task.id}`} className={TASK_ROW}>
              {row}
            </div>
          );
        })}
      </Card>
    </section>
  );
}
