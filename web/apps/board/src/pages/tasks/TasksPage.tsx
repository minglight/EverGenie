import type { Task } from "@evergenie/sdk-web";
import { Card, cn, EmptyState, Segmented } from "@evergenie/ui";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Target } from "lucide-react";
import { useMemo, useState } from "react";
import { FilterEmpty } from "../../components/FilterEmpty";
import { NewTask } from "../../components/NewTask";
import { Pill } from "../../components/Pill";
import { RowCode } from "../../components/RowCode";
import { SearchBox } from "../../components/SearchBox";
import { SiteProblems } from "../../components/SiteNotice";
import { ItemSiteTag } from "../../components/SiteTag";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { TasksSection } from "../../components/TasksSection";
import {
  failedSites,
  flattenParts,
  itemKey,
  type OnSite,
  useAcrossSites,
} from "../../data/acrossSites";
import { countByAgent, matchAgent, useAgentFilter } from "../../data/agentFilter";
import { useSiteAgents } from "../../data/agents";
import { usePublishPageCounts } from "../../data/pageCounts";
import { goalsListQuery, openStatuses, type TaskRow, tasksQuery } from "../../data/queries";
import { homeSearch, type Site } from "../../data/sites";
import { type TextKey, t } from "../../i18n/zh-TW";
import { relativeAgo, waitedFor } from "../../lib/format";
import { HIT_44 } from "../../lib/hit";
import { useIsDesktop } from "../../lib/media";
import { useNow } from "../../lib/now";
import { statusText, statusTone } from "../../lib/taskText";
import {
  compareClosed,
  compareOpen,
  inStatuses,
  matchesQuery,
  type OpenStatus,
  parseStatuses,
  type TaskPageView,
  taskPageViews,
  toggleStatus,
} from "./taskViews";
import { filteredViewCount } from "./viewCounts";

const viewKeys = {
  open: "tasks.view.open",
  closed: "tasks.view.closed",
} as const satisfies Record<TaskPageView, TextKey>;

const emptyKeys = {
  open: "tasks.empty.open",
  closed: "tasks.empty.closed",
} as const satisfies Record<TaskPageView, TextKey>;

// 狀態標籤（篩選列與每一列左邊的小標籤共用）。
const statusLabel = {
  waiting_user: "tasks.view.waiting",
  doing: "tasks.view.active",
  waiting_external: "tasks.view.external",
  idea: "tasks.view.idea",
} as const satisfies Record<OpenStatus, TextKey>;

const pillButton =
  "flex min-h-11 items-center rounded-full bg-surface px-4 text-sm font-medium text-accent focus-visible:outline-2 focus-visible:outline-focus";

// 任務單（頁內分頁「任務單｜Goal｜排程」的第一個）：?view= 分頁 未結（預設）／已結束；未結上面一排狀態標籤可複選（?status=）；
// 再上面一個搜尋框（DEV-107；作者 2026-11-03 18:22 把六個分頁收成兩個＋標籤）。
export function TasksPage() {
  const desktop = useIsDesktop();
  const now = useNow();
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as {
    view?: "closed";
    status?: string;
    new?: boolean;
  };
  const view: TaskPageView = search.view === "closed" ? "closed" : "open";
  const statuses = useMemo(
    () => (view === "open" ? parseStatuses(search.status) : []),
    [view, search.status],
  );
  const [query, setQuery] = useState("");
  // 每台各抓一份、合成一張清單，每列標台名（S3-7b）；只有一台時就是主伺服器那一份。
  const siteAgents = useSiteAgents();
  const agentsOf = useMemo(
    () => new Map(siteAgents.map((a) => [a.site.key, a.view])),
    [siteAgents],
  );
  // 「未結」一次抓四種狀態（等了幾天前端用 waiting_since 算，不再另外叫 tasks.waiting）；已結束另一份。
  const parts = useAcrossSites((site) => tasksQuery(view === "closed" ? "closed" : "all", site));
  const openParts = useAcrossSites((site) => tasksQuery("all", site));
  const primary = parts[0];
  const { picked } = useAgentFilter();
  // 分頁與狀態標籤上的數字：加總每台，跟清單同一個秘書篩選與搜尋（WEB-96）；0 張也寫。
  const openCount = filteredViewCount(openParts, picked, (r) => matchesQuery(r.task, query));
  const statusCounts = Object.fromEntries(
    openStatuses.map((s) => [
      s,
      filteredViewCount(
        openParts,
        picked,
        (r) => r.task.status === s && matchesQuery(r.task, query),
      ),
    ]),
  ) as Record<OpenStatus, number | undefined>;
  // 每一列標出掛在哪個 Goal（REQ-232；2026-10-28 起不看模式）；Goal 跟單在同一台。
  const goalParts = useAcrossSites((site) => goalsListQuery(site));
  const goalTitle = useMemo(
    () =>
      new Map(
        goalParts.flatMap((p) =>
          (p.data ?? []).map((g) => [itemKey(p.site, g.id ?? ""), g.title ?? ""] as const),
        ),
      ),
    [goalParts],
  );
  const all = useMemo(() => {
    const rows = flattenParts(parts).filter(({ item }) => inStatuses(statuses, item.task));
    const cmp = view === "closed" ? compareClosed : compareOpen;
    return rows.sort((a, b) => cmp(a.item.task, b.item.task));
  }, [parts, view, statuses]);
  const rows = useMemo(
    () =>
      all.filter(
        ({ site, item }) =>
          matchAgent(picked, item.task.agent, site.key) && matchesQuery(item.task, query),
      ),
    [all, picked, query],
  );
  // 徽章＝目前看的（分頁＋狀態標籤＋搜尋字）裡這位的筆數，跟清單一致（WEB-96）；「全部」下面是總筆數（不受秘書篩選影響）。
  const counts = useMemo(() => {
    const out: Record<string, number> = {};
    for (const p of parts)
      Object.assign(
        out,
        countByAgent(
          (p.data ?? []).filter((r) => inStatuses(statuses, r.task) && matchesQuery(r.task, query)),
          (r) => r.task.agent,
          p.site.key,
        ),
      );
    return out;
  }, [parts, statuses, query]);
  const loaded = primary?.data !== undefined;
  usePublishPageCounts(loaded ? { counts, total: all.length } : null);

  const go = (next: { view?: "closed"; status?: OpenStatus[] }) =>
    void navigate({
      to: "/tasks",
      search: {
        view: next.view,
        status: next.status && next.status.length > 0 ? next.status.join(",") : undefined,
      },
      replace: true,
    });
  const goView = (next: TaskPageView) =>
    go(next === "closed" ? { view: "closed" } : { status: statuses });
  const clearFilters = () => {
    setQuery("");
    go({});
  };
  const renderRow = ({ site, item: { task, waitingDays } }: OnSite<TaskRow>) => (
    <TaskListRow
      key={itemKey(site, task.id ?? "")}
      site={site}
      task={task}
      waitingDays={waitingDays}
      view={view}
      now={now}
      agentName={
        task.agent ? (agentsOf.get(site.key) ?? agentsOf.get(""))?.(task.agent).name : undefined
      }
      goalName={task.goal ? goalTitle.get(itemKey(site, task.goal)) : undefined}
    />
  );
  const filtering = query.trim() !== "" || statuses.length > 0;

  return (
    <TasksSection tab="tasks">
      <NewTask
        autoFocus={fresh(search)}
        onCreated={() => {
          // 新單是待做：開完一定看得到剛開的那張（WEB-30）——清掉搜尋字；在已結束、或標籤篩掉了待做，
          // 回到「未結」全部；網址的 ?new=true 也一起拿掉，重新整理不會再自動聚焦開單框。
          setQuery("");
          if (view === "closed" || !inStatuses(statuses, { status: "idea" })) go({});
          else if (fresh(search)) go({ status: statuses });
        }}
      />
      <SearchBox
        value={query}
        onChange={setQuery}
        placeholder={t("tasks.search.placeholder")}
        label={t("tasks.search")}
      />
      <Segmented
        options={taskPageViews.map((v) => ({
          value: v,
          label: t(viewKeys[v]),
          count: v === "open" ? openCount : undefined,
        }))}
        value={view}
        onChange={goView}
        ariaLabel={t("tasks.views")}
        compact={desktop}
      />
      {view === "open" ? (
        <fieldset
          aria-label={t("tasks.filter.status")}
          data-testid="tasks-status-filter"
          className="m-0 flex min-w-0 flex-wrap gap-2 border-0 p-0"
        >
          {openStatuses.map((s) => {
            const on = statuses.includes(s);
            const n = statusCounts[s];
            return (
              <button
                key={s}
                type="button"
                aria-pressed={on}
                onClick={() => go({ status: toggleStatus(statuses, s) })}
                className={cn(
                  HIT_44,
                  "flex min-h-9 items-center gap-1.5 rounded-full border px-3 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-focus",
                  on
                    ? "border-accent bg-accent-soft text-accent"
                    : "border-border bg-surface text-fg hover:bg-hover",
                )}
              >
                {t(statusLabel[s])}
                <span className={cn("tabular-nums", on ? "text-accent" : "text-muted")}>
                  {n ?? "…"}
                </span>
              </button>
            );
          })}
        </fieldset>
      ) : null}
      <SiteProblems what={t("page.tasks")} failed={failedSites(parts)} />
      {!loaded ? (
        primary?.isError ? (
          <LoadFailed what={t("page.tasks")} onRetry={() => primary.refetch()} />
        ) : (
          <LoadingBlock />
        )
      ) : filtering && rows.length === 0 ? (
        <div className="flex flex-col items-center gap-3">
          <EmptyState
            className="self-stretch"
            title={
              query.trim() ? t("tasks.search.empty", { q: query.trim() }) : t("tasks.filter.empty")
            }
          />
          <div className="flex flex-wrap justify-center gap-2">
            <button
              type="button"
              data-testid="tasks-clear-filters"
              onClick={clearFilters}
              className={pillButton}
            >
              {t("tasks.filter.clear")}
            </button>
            {query.trim() && view !== "closed" ? (
              <button
                type="button"
                data-testid="tasks-search-closed"
                onClick={() => goView("closed")}
                className={pillButton}
              >
                {t("tasks.search.closed")}
              </button>
            ) : null}
          </div>
        </div>
      ) : picked.length > 0 && rows.length === 0 ? (
        <FilterEmpty />
      ) : rows.length === 0 ? (
        <EmptyState title={t(emptyKeys[view])} hint={t("tasks.empty.hint")} />
      ) : (
        <Card className="overflow-hidden">
          <ul aria-label={t("tasks.list")} className="m-0 list-none p-0">
            {rows.map(renderRow)}
          </ul>
        </Card>
      )}
    </TasksSection>
  );
}

const fresh = (s: { new?: boolean }) => s.new === true;

function TaskListRow({
  site,
  task,
  waitingDays,
  view,
  now,
  agentName,
  goalName,
}: {
  site: Site;
  task: Task;
  waitingDays: number;
  view: TaskPageView;
  now: Date;
  agentName: string | undefined;
  goalName: string | undefined;
}) {
  const status = task.status ?? "doing";
  const time =
    status === "waiting_user"
      ? waitedFor(task.waiting_since, now, waitingDays)
      : relativeAgo(view === "closed" ? task.closed_at || task.updated_at : task.updated_at, now);
  return (
    <li className="border-b border-border last:border-b-0">
      <Link
        to="/tasks/$id"
        params={{ id: task.id ?? "" }}
        search={homeSearch(site)}
        className="flex min-h-[64px] flex-col justify-center gap-[3px] px-4 py-3.5 hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus"
      >
        <span className="flex items-center gap-1.5">
          {view === "open" && isOpenStatus(status) ? (
            <span className="flex shrink-0 items-center gap-1" data-testid="task-status">
              {/* 等你處理的單直接寫是哪一種（要你驗收／要你回答，DEV-156 U-10），不再「等你」＋「驗收」兩段。 */}
              <Pill tone={status === "waiting_user" ? "attention" : "muted"}>
                {status === "waiting_user" ? statusText(task) : t(statusLabel[status])}
              </Pill>
            </span>
          ) : (
            <Pill tone={statusTone[status]}>{statusText(task)}</Pill>
          )}
          <ItemSiteTag site={site} />
          <span className="shrink-0 text-[13px] text-muted">
            {task.agent ? agentName : t("tasks.row.noAgent")}
          </span>
          {task.goal ? (
            <span
              data-testid="task-goal"
              title={`${task.goal} ${goalName ?? ""}`.trim()}
              className="flex min-w-0 items-center gap-1 truncate text-[13px] text-muted"
            >
              <Target className="size-3.5 shrink-0" aria-hidden="true" />
              <span className="sr-only">{t("goal.field")}</span>
              <span className="truncate">{goalName || task.goal}</span>
            </span>
          ) : null}
          <span className="ml-auto shrink-0 text-xs text-muted">{time}</span>
        </span>
        <span className={cn("text-[15px] leading-[1.45] font-semibold break-words")}>
          <RowCode id={task.id} />
          {task.title}
        </span>
      </Link>
    </li>
  );
}

const isOpenStatus = (s: string): s is OpenStatus =>
  (openStatuses as readonly string[]).includes(s);
