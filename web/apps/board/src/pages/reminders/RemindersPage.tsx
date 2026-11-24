import type { Difficulty, Reminder } from "@evergenie/sdk-web";
import { Card, EmptyState, Segmented } from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Check } from "lucide-react";
import { useMemo, useState } from "react";
import { CollapsibleGroup } from "../../components/CollapsibleGroup";
import { FilterEmpty } from "../../components/FilterEmpty";
import { Pill, type PillTone } from "../../components/Pill";
import { QuickInput } from "../../components/QuickInput";
import { SearchBox } from "../../components/SearchBox";
import { SiteProblems } from "../../components/SiteNotice";
import { ItemSiteTag } from "../../components/SiteTag";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import {
  failedSites,
  flattenParts,
  itemKey,
  type OnSite,
  useAcrossSites,
} from "../../data/acrossSites";
import { countByAgent, matchAgent, useAgentFilter } from "../../data/agentFilter";
import { useAgents } from "../../data/agents";
import { useDeferredIds } from "../../data/deferred";
import { usePublishPageCounts } from "../../data/pageCounts";
import { useShowCodes } from "../../data/queries";
import { markDone } from "../../data/reminderActions";
import { homeSearch, type Site } from "../../data/sites";
import { type TextKey, t } from "../../i18n/zh-TW";
import { clockText, parseIso, timeLabel } from "../../lib/format";
import { useIsDesktop } from "../../lib/media";
import { useNow } from "../../lib/now";
import { Slot } from "../../pkg/Slot";
import { type ReminderView, remindersQuery, reminderViews } from "./queries";
import { ReminderMenu } from "./ReminderMenu";
import {
  groupKeyOf,
  matchesText,
  pageViewOf,
  type ReminderGroupKey,
  recentlyDone,
  reminderGroupOrder,
} from "./reminderGroups";
import { collectsPoints, difficultyOf, followText } from "./reminderText";

const viewKeys = {
  open: "reminders.view.open",
  waiting: "reminders.view.waiting",
  done: "reminders.view.done",
} as const satisfies Record<ReminderView, TextKey>;

const emptyKeys = {
  open: "reminders.empty.open",
  waiting: "reminders.empty.waiting",
  done: "reminders.empty.done",
} as const satisfies Record<ReminderView, TextKey>;

const groupKeys = {
  late: "reminders.group.late",
  today: "reminders.group.today",
  week: "reminders.group.week",
  later: "reminders.group.later",
  none: "reminders.group.none",
} as const satisfies Record<ReminderGroupKey, TextKey>;

const groupTone: Record<ReminderGroupKey, PillTone> = {
  late: "danger",
  today: "accent",
  week: "busy",
  later: "muted",
  none: "muted",
};

// 每筆記得自己在哪台（S3-7b）：好幾台的待辦合成一份。
type Row = OnSite<Reminder>;

const stamp = (iso: string | undefined) => parseIso(iso)?.getTime() ?? 0;

// dayed：這列在「哪一天」的分組標題底下（今天、逾期、這週、之後）：右邊只寫幾點，日子標題已經寫了（RMD-21）。
// 每列標難度與做完可得幾點（WEB-162、RMD-26）：只有會集點的跟進方式才標；沒寫難度的、別台回來看板不認得的都算普通，已完成的不標；
// 點數是伺服器算的（難度＋過期每滿一週 1），今天給過或舊版伺服器沒帶就只寫難度。顏色只分輕重，attention 留給「等你處理」（WEB-70）。
const difficultyTone: Record<Difficulty, PillTone> = {
  easy: "ok",
  normal: "muted",
  hard: "accent",
};

function ReminderRow({
  r,
  site,
  now,
  dayed = false,
}: {
  r: Reminder;
  site: Site;
  now: Date;
  dayed?: boolean;
}) {
  const client = useQueryClient();
  const { view } = useAgents(site);
  const ended = r.status === "done" || r.status === "cancelled";
  const owner = view(r.owner).name;
  // 簡單模式的清單列不露編號（DEV-156 U-22）；詳情頁、搜尋照舊。
  const showCode = useShowCodes();
  const meta = [showCode ? r.id : "", owner, r.follow ? followText(r.follow) : ""]
    .filter(Boolean)
    .join(" · ");
  const time = dayed ? clockText(r.at) : timeLabel(r.at, now);
  return (
    <li className="flex min-h-[60px] items-center gap-1 border-b border-border py-1 pr-1 pl-1 last:border-b-0">
      <button
        type="button"
        aria-label={
          r.content ? t("reminder.markDone.named", { content: r.content }) : t("reminder.markDone")
        }
        disabled={ended}
        onClick={() => markDone(client, r, site)}
        className="flex size-11 shrink-0 items-center justify-center rounded-full focus-visible:outline-2 focus-visible:outline-focus disabled:cursor-default"
      >
        {ended ? (
          <span className="flex size-[22px] items-center justify-center rounded-full bg-ok text-accent-fg">
            <Check className="size-3.5" strokeWidth={3} aria-hidden="true" />
          </span>
        ) : (
          <span className="box-border size-[22px] rounded-full border-2 border-border-strong" />
        )}
      </button>
      <Link
        to="/reminders/$id"
        params={{ id: r.id ?? "" }}
        search={homeSearch(site)}
        className="flex min-h-11 min-w-0 flex-1 items-center gap-2 rounded-lg py-1 focus-visible:outline-2 focus-visible:outline-focus"
      >
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[15px]">{r.content}</span>
          <span className="flex min-w-0 items-center gap-1.5">
            <ItemSiteTag site={site} />
            {ended || !collectsPoints(r) ? null : (
              <Pill
                tone={difficultyTone[difficultyOf(r)]}
                className="py-0 font-medium whitespace-nowrap"
              >
                <span className="sr-only">{t("reminder.field.difficulty")}：</span>
                <span data-testid="reminder-difficulty">{t(`difficulty.${difficultyOf(r)}`)}</span>
                {r.points ? (
                  <span data-testid="reminder-points" className="ml-1">
                    <span className="sr-only">{t("reminder.points.sr")}</span>
                    {t("reminder.points", { n: r.points })}
                  </span>
                ) : null}
              </Pill>
            )}
            <span className="block truncate text-xs text-muted">{meta}</span>
          </span>
        </span>
        <span className="font-mono text-[13px] whitespace-nowrap text-muted">
          {time || t("reminders.row.noTime")}
        </span>
      </Link>
      {ended ? <span className="w-1" /> : <ReminderMenu reminder={r} site={site} />}
    </li>
  );
}

// 主人待辦：頂端萬用輸入框（新增）、搜尋框，?view= 分段 未完成（預設，依時間分組）／要你回答／已完成（DEV-107）。
export function RemindersPage() {
  const desktop = useIsDesktop();
  const now = useNow();
  const navigate = useNavigate();
  const { view: viewParam } = useSearch({ strict: false }) as { view?: string };
  const view = pageViewOf(viewParam);
  const [query, setQuery] = useState("");
  // 每台各抓一份、合成一張清單，每列標台名（S3-7b）；三個分頁各一份，分段上的數字用。
  const openParts = useAcrossSites((site) => remindersQuery("open", site));
  const waitingParts = useAcrossSites((site) => remindersQuery("waiting", site));
  const doneParts = useAcrossSites((site) => remindersQuery("done", site));
  const parts = view === "open" ? openParts : view === "waiting" ? waitingParts : doneParts;
  const primary = parts[0];
  const hidden = useDeferredIds();
  const { picked } = useAgentFilter();
  const mine = (all: Row[], v: ReminderView) =>
    all.filter(
      (r) =>
        !hidden.has(itemKey(r.site, r.item.id ?? "")) &&
        matchAgent(picked, r.item.owner, r.site.key) &&
        (v !== "done" || recentlyDone(r.item, now)),
    );
  const items = useMemo(
    () =>
      flattenParts(parts).filter(
        (r) =>
          !hidden.has(itemKey(r.site, r.item.id ?? "")) &&
          (view !== "done" || recentlyDone(r.item, now)),
      ),
    [parts, hidden, view, now],
  );
  const visible = useMemo(
    () =>
      items.filter(
        (r) =>
          matchAgent(picked, r.item.owner, r.site.key) &&
          matchesText(`${r.item.id ?? ""} ${r.item.content ?? ""} ${r.item.body ?? ""}`, query),
      ),
    [items, picked, query],
  );
  const groups = useMemo(() => {
    if (view !== "open") return [];
    const by = new Map<ReminderGroupKey, Row[]>();
    for (const r of visible) {
      const key = groupKeyOf(parseIso(r.item.at), now);
      by.set(key, [...(by.get(key) ?? []), r]);
    }
    return reminderGroupOrder
      .filter((k) => by.has(k))
      .map((key) => ({
        key,
        items: [...(by.get(key) ?? [])].sort((a, b) =>
          key === "none"
            ? stamp(b.item.created_at) - stamp(a.item.created_at)
            : stamp(a.item.at) - stamp(b.item.at),
        ),
      }));
  }, [visible, view, now]);
  const flat = useMemo(
    () =>
      [...visible].sort((a, b) =>
        view === "done"
          ? stamp(b.item.closed_at) - stamp(a.item.closed_at)
          : stamp(b.item.created_at) - stamp(a.item.created_at),
      ),
    [visible, view],
  );
  const viewCounts: Record<ReminderView, number | undefined> = {
    open: openParts[0]?.data ? mine(flattenParts(openParts), "open").length : undefined,
    waiting: waitingParts[0]?.data ? mine(flattenParts(waitingParts), "waiting").length : undefined,
    done: doneParts[0]?.data ? mine(flattenParts(doneParts), "done").length : undefined,
  };
  // 徽章＝目前檢視裡這位的筆數；「全部」下面是這個檢視的總筆數（不受篩選影響）。
  const counts = useMemo(() => {
    const out: Record<string, number> = {};
    for (const p of parts) {
      const here = items.filter((r) => r.site.key === p.site.key).map((r) => r.item);
      Object.assign(
        out,
        countByAgent(here, (r) => r.owner, p.site.key),
      );
    }
    return out;
  }, [parts, items]);
  const loaded = primary?.data !== undefined;
  usePublishPageCounts(loaded ? { counts, total: items.length } : null);

  const renderRow = (r: Row, dayed: boolean) => (
    <ReminderRow
      key={itemKey(r.site, r.item.id ?? "")}
      r={r.item}
      site={r.site}
      now={now}
      dayed={dayed}
    />
  );

  return (
    <div className="mx-auto flex max-w-[720px] flex-col gap-4 px-4 pt-3 pb-7">
      {desktop ? (
        <h1 className="pt-6 text-2xl font-bold tracking-[-0.01em]">{t("page.reminders")}</h1>
      ) : null}
      <Slot id="core.reminders.top" />
      <QuickInput
        variant="reminders"
        onTodoCreated={() => {
          // 新待辦都在「未完成」：現在看別的分頁就切回來，才看得到剛加的。
          if (view !== "open")
            void navigate({ to: "/reminders", search: { view: undefined }, replace: true });
        }}
      />
      <SearchBox
        value={query}
        onChange={setQuery}
        placeholder={t("reminders.search.placeholder")}
        label={t("reminders.search")}
      />
      <Segmented
        options={reminderViews.map((v) => ({
          value: v,
          label: t(viewKeys[v]),
          count: viewCounts[v],
        }))}
        value={view}
        onChange={(next) =>
          void navigate({
            to: "/reminders",
            search: { view: next === "open" ? undefined : next },
            replace: true,
          })
        }
        ariaLabel={t("reminders.views")}
        compact={desktop}
      />
      <SiteProblems what={t("page.reminders")} failed={failedSites(parts)} />
      {!loaded ? (
        primary?.isError ? (
          <LoadFailed what={t("page.reminders")} onRetry={() => primary.refetch()} />
        ) : (
          <LoadingBlock />
        )
      ) : query.trim() && visible.length === 0 ? (
        <EmptyState title={t("reminders.search.empty", { q: query.trim() })} />
      ) : picked.length > 0 && visible.length === 0 ? (
        <FilterEmpty />
      ) : items.length === 0 ? (
        <EmptyState title={t(emptyKeys[view])} hint={t("reminders.empty.hint")} />
      ) : view === "open" ? (
        <div className="flex flex-col gap-3">
          {groups.map((g) => (
            <CollapsibleGroup
              key={g.key}
              testId={`reminders-group-${g.key}`}
              title={t(groupKeys[g.key])}
              count={g.items.length}
              tone={groupTone[g.key]}
            >
              <ul className="m-0 list-none p-0">
                {g.items.map((r) => renderRow(r, g.key === "today"))}
              </ul>
            </CollapsibleGroup>
          ))}
        </div>
      ) : (
        <Card className="overflow-hidden">
          <ul className="m-0 list-none p-0">{flat.map((r) => renderRow(r, false))}</ul>
        </Card>
      )}
    </div>
  );
}
