import type { Goal } from "@evergenie/sdk-web";
import { Card, EmptyState, Segmented } from "@evergenie/ui";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { FilterEmpty } from "../../components/FilterEmpty";
import { Pill } from "../../components/Pill";
import { SiteProblems } from "../../components/SiteNotice";
import { ItemSiteTag } from "../../components/SiteTag";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { TasksSection } from "../../components/TasksSection";
import { failedSites, itemKey, type OnSite, useAcrossSites } from "../../data/acrossSites";
import { countByAgent, matchAgent, useAgentFilter } from "../../data/agentFilter";
import { useAgents } from "../../data/agents";
import { usePublishPageCounts } from "../../data/pageCounts";
import { goalsListQuery } from "../../data/queries";
import { homeSearch, type Site } from "../../data/sites";
import { type TextKey, t } from "../../i18n/zh-TW";
import { relativeAgo } from "../../lib/format";
import { knownText } from "../../lib/knownText";
import { useIsDesktop } from "../../lib/media";
import { useNow } from "../../lib/now";
import {
  type GoalCount,
  type GoalView,
  goalCountsQuery,
  goalProposer,
  goalViewCounts,
  goalViews,
  groupGoals,
} from "./goals";
import { openedByOwner, stateKey, stateTone } from "./goalText";
import { NewGoalDialog } from "./NewGoal";

const viewKeys = {
  active: "goals.view.active",
  draft: "goals.view.draft",
  ended: "goals.view.ended",
} as const satisfies Record<GoalView, TextKey>;

const emptyKeys = {
  active: ["goals.empty.active", "goals.empty.active.hint"],
  draft: ["goals.empty.draft", "goals.empty.draft.hint"],
  ended: ["goals.empty.ended", "goals.empty.ended.hint"],
} as const satisfies Record<GoalView, readonly [TextKey, TextKey]>;

const proposer = goalProposer;

// 好幾台的同一段合起來排：跟 groupGoals 一樣，進行中、要你同意看最近更新，已結束看結束時間，新的在前（同時間照台的順序）。
function mergeOrder(rows: OnSite<Goal>[], view: GoalView): OnSite<Goal>[] {
  const stamp = ({ item: g }: OnSite<Goal>) =>
    Date.parse((view === "ended" ? g.ended_at : "") || g.updated_at || g.created_at || "") || 0;
  return rows
    .map((r, i) => ({ r, i }))
    .sort((a, b) => stamp(b.r) - stamp(a.r) || a.i - b.i)
    .map(({ r }) => r);
}

// 「進行中 2 張 · 已結束 3 張 · 待辦 1 個」：一眼看出底下掛了多少事（REQ-232）。
function countText(c: GoalCount | undefined): string {
  if (!c || c.open + c.closed + c.reminders === 0) return t("goals.row.empty");
  const parts: string[] = [];
  if (c.open > 0) parts.push(t("goals.row.open", { n: c.open }));
  if (c.closed > 0) parts.push(t("goals.row.closed", { n: c.closed }));
  if (c.reminders > 0) parts.push(t("goals.row.reminders", { n: c.reminders }));
  return parts.join(" · ");
}

function GoalRow({ goal, site, count }: { goal: Goal; site: Site; count: GoalCount | undefined }) {
  const now = useNow();
  const { view } = useAgents(site);
  const state = goal.state ?? "active";
  const by = openedByOwner(goal)
    ? t("goal.by.owner")
    : t("goal.by.agent", { name: view(goal.agent || goal.by).name });
  const time = relativeAgo(
    (state === "done" || state === "withdrawn" ? goal.ended_at : "") ||
      goal.updated_at ||
      goal.created_at,
    now,
  );
  return (
    <li className="border-b border-border last:border-b-0">
      <Link
        to="/goals/$id"
        params={{ id: goal.id ?? "" }}
        search={homeSearch(site)}
        data-goal={goal.id}
        className="flex min-h-[64px] flex-col justify-center gap-[3px] px-4 py-3.5 hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus"
      >
        <span className="flex items-center gap-1.5">
          <Pill tone={stateTone[state]}>{knownText(stateKey, state)}</Pill>
          <ItemSiteTag site={site} />
          <span className="truncate text-[13px] text-muted">{by}</span>
          <span className="ml-auto shrink-0 text-xs text-muted">{time}</span>
        </span>
        <span className="text-[15px] leading-[1.45] font-semibold break-words">
          <span className="font-mono text-xs font-medium text-muted">{goal.id}</span> · {goal.title}
        </span>
        <span data-testid="goal-counts" className="text-[13px] text-muted">
          {countText(count)}
        </span>
      </Link>
    </li>
  );
}

// Goal 清單（REQ-203）：?view= 分段 進行中／要你同意／已結束；頂端「新增 Goal」（D-39）。
// 放在任務單頁的分頁「任務單｜Goal｜排程」裡（REQ-232）；每一列底下是掛了幾張任務單、幾個待辦。
export function GoalsPage() {
  const desktop = useIsDesktop();
  const navigate = useNavigate();
  const { view: viewParam } = useSearch({ strict: false }) as { view?: GoalView };
  const view: GoalView = viewParam ?? "active";
  // 每台各抓一份、合成一張清單，每列標台名（S3-7b）；只有一台時就是主伺服器那一份。
  const parts = useAcrossSites((site) => goalsListQuery(site));
  const countParts = useAcrossSites((site) => goalCountsQuery(site));
  const primary = parts[0];
  const counts = useMemo(() => {
    const out = new Map<string, GoalCount>();
    for (const p of countParts)
      for (const [goal, c] of Object.entries(p.data ?? {})) out.set(itemKey(p.site, goal), c);
    return out;
  }, [countParts]);
  const [creating, setCreating] = useState(false);
  // 分段照每台各自分好再接起來（groupGoals 的排序在同一台內；好幾台時再照同一個時間排一次）。
  const groups = useMemo(() => {
    const out: Record<GoalView, OnSite<Goal>[]> = { active: [], draft: [], ended: [] };
    for (const p of parts) {
      const g = groupGoals(p.data ?? []);
      for (const v of goalViews) out[v].push(...g[v].map((item) => ({ site: p.site, item })));
    }
    if (parts.length > 1) for (const v of goalViews) out[v] = mergeOrder(out[v], v);
    return out;
  }, [parts]);
  const { picked } = useAgentFilter();
  const all = groups[view];
  const viewCounts = useMemo(() => goalViewCounts(groups, picked), [groups, picked]);
  const rows = useMemo(
    () => all.filter(({ site, item }) => matchAgent(picked, proposer(item), site.key)),
    [all, picked],
  );
  // 秘書篩選列的徽章：這個檢視裡每位秘書提的 Goal 數。
  const agentCounts = useMemo(() => {
    const out: Record<string, number> = {};
    for (const p of parts) {
      const mine = all.filter((r) => r.site.key === p.site.key).map((r) => r.item);
      Object.assign(out, countByAgent(mine, proposer, p.site.key));
    }
    return out;
  }, [parts, all]);
  const loaded = primary?.data !== undefined;
  usePublishPageCounts(loaded ? { counts: agentCounts, total: all.length } : null);

  return (
    <TasksSection tab="goals">
      <p className="text-[13px] leading-snug text-muted">{t("goals.intro")}</p>
      <ActionButton
        tone="primary"
        size="page"
        onClick={() => setCreating(true)}
        className="self-start"
      >
        <Plus className="size-[18px]" aria-hidden="true" />
        {t("goals.new")}
      </ActionButton>
      <Segmented
        options={goalViews.map((v) => ({
          value: v,
          label: viewCounts[v] > 0 ? `${t(viewKeys[v])} ${viewCounts[v]}` : t(viewKeys[v]),
        }))}
        value={view}
        onChange={(next) =>
          void navigate({
            to: "/goals",
            search: { view: next === "active" ? undefined : next },
            replace: true,
          })
        }
        ariaLabel={t("goals.views")}
        compact={desktop}
      />
      <SiteProblems what={t("page.goals")} failed={failedSites(parts)} />
      {!loaded ? (
        primary?.isError ? (
          <LoadFailed what={t("goals.loadFailed")} onRetry={() => primary.refetch()} />
        ) : (
          <LoadingBlock />
        )
      ) : picked.length > 0 && rows.length === 0 ? (
        <FilterEmpty />
      ) : rows.length === 0 ? (
        <EmptyState title={t(emptyKeys[view][0])} hint={t(emptyKeys[view][1])} />
      ) : (
        <Card className="overflow-hidden">
          <ul aria-label={t("goals.list")} className="m-0 list-none p-0">
            {rows.map(({ site, item: g }) => (
              <GoalRow
                key={itemKey(site, g.id ?? "")}
                goal={g}
                site={site}
                count={counts.get(itemKey(site, g.id ?? ""))}
              />
            ))}
          </ul>
        </Card>
      )}
      <NewGoalDialog open={creating} onClose={() => setCreating(false)} />
    </TasksSection>
  );
}
