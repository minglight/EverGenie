import type { ThreadSummary } from "@evergenie/sdk-web";
import { Avatar, cn, EmptyState, Spinner } from "@evergenie/ui";
import { Link } from "@tanstack/react-router";
import { Check } from "lucide-react";
import { type ReactNode, useMemo } from "react";
import { FilterEmpty } from "../../components/FilterEmpty";
import { SiteNotice } from "../../components/SiteNotice";
import { SiteRing, SiteTag, siteName } from "../../components/SiteTag";
import { matchAgent, useAgentFilter } from "../../data/agentFilter";
import { useAgents } from "../../data/agents";
import { homeSearch, type Site } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { useToolDetails } from "../../lib/chatDisplay";
import { preview as plainPreview, relativeAgo } from "../../lib/format";
import { useNow } from "../../lib/now";
import { needsReply, toneBadge } from "../../lib/statusTone";
import { type SiteThreads, useSiteThreads } from "./siteThreads";
import { groupWork, sortThreads, statusView, toneClass } from "./threads";
import { WorkRow } from "./WorkRow";

export interface ThreadListProps {
  /** 桌機兩欄時，目前打開的那一串（高亮）：哪台（台的 key，主伺服器 ""）與串的名稱。 */
  selected?: { site: string; name: string };
}

// 討論串清單：session.list_threads。即時更新全靠外殼的 watch_threads，所以不定時重抓（WEB-69）。
// 連多台時照電腦分組（S3-6）：每台一段、標台名；不是 ok 的台只在那一段寫原因，其他台照常。只有一台時跟 S3 之前一樣。
export function ThreadList({ selected }: ThreadListProps) {
  const all = useSiteThreads();
  const { picked } = useAgentFilter();
  const groups = useMemo(
    () =>
      all.map((s) => ({
        ...s,
        sorted: sortThreads(s.data ?? []).filter((th) => matchAgent(picked, th.agent, s.site.key)),
      })),
    [all, picked],
  );
  if (groups.length === 1 && groups[0]) {
    const g = groups[0];
    return <SingleSite group={g} picked={picked.length > 0} selected={selected} />;
  }
  const loaded = groups.filter((g) => g.usable && g.data);
  // 篩選後每台都沒有：跟只有一台時一樣只寫一次「沒有東西」（連不上的台另外寫原因）。
  const filteredOut =
    picked.length > 0 && loaded.length > 0 && loaded.every((g) => g.sorted.length === 0);
  return (
    <div className="flex flex-col gap-3">
      {filteredOut ? (
        <div className="px-2 py-4">
          <FilterEmpty />
        </div>
      ) : null}
      {groups.map((g) => {
        if (filteredOut && g.usable && g.data) return null;
        // 篩選時這台沒有符合的就整段不畫（連不上的台照樣寫原因）。
        if (picked.length > 0 && g.usable && g.data && g.sorted.length === 0) return null;
        return <SiteGroup key={g.site.key || "primary"} group={g} selected={selected} />;
      })}
    </div>
  );
}

type Group = SiteThreads & { sorted: ThreadSummary[] };

// 只有一台：跟 S3 之前完全一樣（轉圈、載入失敗、篩選後沒有、空的、清單）。
function SingleSite({
  group: g,
  picked,
  selected,
}: {
  group: Group;
  picked: boolean;
  selected?: ThreadListProps["selected"];
}) {
  if (g.isPending) {
    return (
      <div className="flex justify-center py-12">
        <Spinner label={t("app.loading")} />
      </div>
    );
  }
  if (g.isError && !g.data) {
    return (
      <div className="mx-4 my-10 flex flex-col items-center gap-3 text-center">
        <p className="text-[15px] font-semibold">{t("chat.list.loadFailed")}</p>
        <button
          type="button"
          onClick={g.refetch}
          className="h-11 rounded-xl bg-accent px-5 text-[15px] font-semibold text-accent-fg"
        >
          {t("app.retry")}
        </button>
      </div>
    );
  }
  if (g.sorted.length === 0 && picked) {
    return (
      <div className="px-2 py-4">
        <FilterEmpty />
      </div>
    );
  }
  if (g.sorted.length === 0) {
    return (
      <EmptyState variant="plain" title={t("chat.list.empty")} hint={t("chat.list.emptyHint")} />
    );
  }
  return <Rows site={g.site} threads={g.sorted} multi={false} selected={selected} />;
}

// 連多台時的一段：台名標籤當標題；這台不能用就只寫原因，載入中、失敗、空的都只影響這一段。
function SiteGroup({
  group: g,
  selected,
}: {
  group: Group;
  selected?: ThreadListProps["selected"];
}) {
  const name = siteName(g.site);
  let body: ReactNode;
  if (!g.usable) body = <SiteNotice site={g.site} className="mx-2.5" />;
  else if (g.isPending) {
    body = (
      <div className="flex justify-center py-6">
        <Spinner label={t("app.loading")} />
      </div>
    );
  } else if (g.isError && !g.data) {
    body = (
      <div className="mx-2.5 flex items-center gap-3 rounded-xl bg-surface-2 px-3.5 py-2 text-[13px] text-muted">
        <span className="min-w-0 flex-1">{t("chat.site.loadFailed")}</span>
        <button
          type="button"
          onClick={g.refetch}
          className="min-h-11 shrink-0 px-2 font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus"
        >
          {t("app.retry")}
        </button>
      </div>
    );
  } else if (g.sorted.length === 0) {
    body = <p className="mx-2.5 text-[13px] text-muted">{t("chat.site.empty")}</p>;
  } else {
    body = <Rows site={g.site} threads={g.sorted} multi selected={selected} />;
  }
  return (
    <section
      aria-label={t("chat.site.section", { name })}
      data-site-group={g.site.key}
      className="flex flex-col gap-1"
    >
      <h2 className="flex px-2.5 pt-1">
        <SiteTag site={g.site} />
      </h2>
      {body}
    </section>
  );
}

function Rows({
  site,
  threads,
  multi,
  selected,
}: {
  site: Site;
  threads: ThreadSummary[];
  multi: boolean;
  selected?: ThreadListProps["selected"];
}) {
  const { top, nested } = groupWork(threads);
  const isSelected = (th: ThreadSummary) =>
    !!selected && selected.site === site.key && th.name === selected.name;
  return (
    <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
      {top.map((th) => {
        const work = nested.get(th.name ?? "");
        return (
          <li key={th.name}>
            <ThreadRow thread={th} site={site} multi={multi} selected={isSelected(th)} />
            {work?.length ? (
              <WorkList site={site} agent={th.agent ?? ""} threads={work} isSelected={isSelected} />
            ) : null}
          </li>
        );
      })}
    </ul>
  );
}

// 秘書底下展開她的工作 session（P7-5、D-33）：縮排、比主要討論串小一號。
function WorkList({
  site,
  agent,
  threads,
  isSelected,
}: {
  site: Site;
  agent: string;
  threads: ThreadSummary[];
  isSelected: (th: ThreadSummary) => boolean;
}) {
  const { view } = useAgents(site);
  return (
    <ul
      aria-label={t("chat.work.list", { name: view(agent).name })}
      className="m-0 ml-9 mt-0.5 flex list-none flex-col gap-0.5 border-l-2 border-border p-0 pl-2"
    >
      {threads.map((th) => (
        <li key={th.name}>
          <WorkRow thread={th} site={site} selected={isSelected(th)} />
        </li>
      ))}
    </ul>
  );
}

// 一列：48 頭像（等你處理有橘點、處理中有藍光圈）、人名＋職責＋時間、第二行狀態＋最後一句、未讀數（綠，有新回覆）。顏色照 lib/statusTone（WEB-70）。整列是一個連結。
function ThreadRow({
  thread: th,
  site,
  multi,
  selected,
}: {
  thread: ThreadSummary;
  site: Site;
  /** 連多台：頭像外框用那台的顏色。 */
  multi: boolean;
  selected: boolean;
}) {
  const { view, list } = useAgents(site);
  // 10 秒更新一次：排隊太久要寫「她那邊沒回應」（threads.ts 的 QUEUED_STUCK_MS），不能等一分鐘才出現。
  const now = useNow(10_000);
  const agent = view(th.agent);
  const duty = list.find((a) => a.id === th.agent)?.duty;
  // 同一位秘書有好幾串時，標題才分得出來；標題就是人名或 session 名稱時沒有資訊量，改顯示職責。
  const sub =
    th.title && th.title !== agent.name && th.title !== th.name && th.title !== th.agent
      ? th.title
      : duty;
  const toolDetails = useToolDetails();
  const status = statusView(th, now, toolDetails);
  const at = th.last_assistant_at || th.status_since;
  // 伺服器給的是原文前 80 字，這裡去掉 Markdown 符號再顯示（REQ-157）。
  const lastLine = th.status === "working" ? "" : plainPreview(th.last_preview, 80);
  return (
    <Link
      to="/chat/$session"
      params={{ session: th.name ?? "" }}
      search={homeSearch(site)}
      aria-current={selected ? "true" : undefined}
      data-thread={th.name}
      data-site={site.key}
      data-thread-status={th.status}
      className={cn(
        "flex min-h-[72px] items-center gap-3 rounded-[14px] px-2.5 py-2.5 text-left text-fg hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus",
        selected && "bg-selected shadow-[inset_0_0_0_1px_var(--accent)] hover:bg-selected",
      )}
    >
      <SiteRing site={site} on={multi}>
        <Avatar
          name={agent.name}
          color={agent.color}
          colorDark={agent.colorDark}
          src={agent.avatarUrl}
          size={48}
          pulse={th.status === "working"}
          waiting={needsReply(th)}
        />
      </SiteRing>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex items-baseline gap-2">
          <span className="shrink-0 text-base font-semibold">{agent.name}</span>
          {sub ? <span className="truncate text-xs text-muted">{sub}</span> : null}
          <span className="ml-auto shrink-0 text-xs text-muted">{relativeAgo(at, now)}</span>
        </span>
        <span className="flex items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-sm">
            {status.text ? (
              <span
                className={cn("inline-flex items-center gap-1 font-medium", toneClass[status.tone])}
                data-testid="row-status"
              >
                {th.status === "done" ? (
                  <Check className="size-3.5" strokeWidth={2.2} aria-hidden="true" />
                ) : null}
                {status.text}
              </span>
            ) : null}
            {status.text && lastLine ? <span className="text-muted"> · </span> : null}
            {lastLine ? (
              <span className={needsReply(th) ? "text-attention-fg" : "text-muted"}>
                {lastLine}
              </span>
            ) : null}
          </span>
          {th.unread > 0 ? (
            <span
              role="img"
              aria-label={t("chat.row.unread", { n: th.unread })}
              className={cn(
                "box-border flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full px-1.5 text-xs font-semibold",
                toneBadge.fresh,
              )}
            >
              {th.unread}
            </span>
          ) : null}
        </span>
      </span>
    </Link>
  );
}
