import type { Notification } from "@evergenie/sdk-web";
import { cn, EmptyState, Segmented, showToast } from "@evergenie/ui";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { ChevronDown, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { isPending } from "../../components/AnswerPanel";
import { FilterEmpty } from "../../components/FilterEmpty";
import { NoticePill } from "../../components/NoticeMeta";
import { NotificationView } from "../../components/NotificationView";
import { tabClass } from "../../components/PageTabs";
import { RowCode } from "../../components/RowCode";
import { SiteProblems } from "../../components/SiteNotice";
import { SiteTag, siteName } from "../../components/SiteTag";
import { SoftPath } from "../../components/SoftPath";
import { LoadFailed, LoadingBlock, NotFoundBlock } from "../../components/StateViews";
import { countByAgent, matchAgent, useAgentFilter } from "../../data/agentFilter";
import { useAgents } from "../../data/agents";
import { refreshAll } from "../../data/live";
import { senderName } from "../../data/noticeLink";
import { usePublishPageCounts } from "../../data/pageCounts";
import { notificationsQuery } from "../../data/queries";
import { apiFor, homeSearch, PRIMARY, type Site, type SiteRef, useSites } from "../../data/sites";
import { itemKey } from "../../data/waiting";
import { t } from "../../i18n/zh-TW";
import { parseIso, relativeAgo } from "../../lib/format";
import { HIT_44 } from "../../lib/hit";
import { useIsDesktop, useIsWide } from "../../lib/media";
import { collapseRuns, countsInBadge, type Lane, laneOf, noticeKind } from "../../lib/noticeLane";
import { useNow } from "../../lib/now";
import { noticeTone, toneBadge, toneDot } from "../../lib/statusTone";

/** 網址的 ?show=：沒寫＝未讀（預設、第一個分段）。 */
export type MessagesFilter = "approve" | "all";
type FilterOption = "unread" | MessagesFilter;
/** 網址的 ?lane=：沒寫＝「秘書」分頁（預設），system＝「系統」分頁（D-47、WEB-86）。 */
export type MessagesLane = "system";

/** 訊息頁網址的查詢參數（清單與一則都帶，回清單時還在同一個分頁、同一個分段）。 */
export interface MessagesSearch {
  show?: MessagesFilter;
  lane?: MessagesLane;
}

/** 一則訊息與它在哪一台（S3-7a：訊息頁合併各台）。key＝itemKey（主伺服器就是編號）。 */
export interface SitedNotice {
  site: Site;
  key: string;
  n: Notification;
}

const byNewest = (a: SitedNotice, b: SitedNotice) =>
  (parseIso(b.n.created_at)?.getTime() ?? 0) - (parseIso(a.n.created_at)?.getTime() ?? 0);

interface Loaded {
  data: Notification[] | undefined;
  failed: boolean;
}

/** useQueries 的 combine：放在模組層（參考不變），資料沒變時回同一個陣列。 */
function loadedOf(results: readonly { data: Notification[] | undefined; isError: boolean }[]) {
  return results.map((r): Loaded => ({ data: r.data, failed: r.isError && r.data === undefined }));
}

/** 一台的訊息清單（主伺服器一定在第一個；別台只有 ok 的才抓）。 */
export interface SiteNotices extends Loaded {
  site: Site;
}

/**
 * 各台的訊息合併成一份（新到舊）：主伺服器照舊用 notificationsQuery()（只有一台時跟以前同一個查詢、同樣的順序）；
 * 討論串內提問（inline）只在那個討論串裡，不進訊息頁（DEV-49 S2）。
 */
export function mergeNotices(parts: readonly SiteNotices[]): SitedNotice[] {
  return parts
    .flatMap((p) =>
      (p.data ?? [])
        .filter((n) => !n.inline)
        .map((n) => ({ site: p.site, key: itemKey(p.site, n.id ?? ""), n })),
    )
    .sort(byNewest);
}

function useSiteNotices() {
  const sites = useSites();
  const primary = useQuery(notificationsQuery());
  const others = useMemo(() => sites.filter((s) => !s.primary && s.state === "ok"), [sites]);
  const loaded = useQueries({
    queries: others.map((site) => notificationsQuery(site)),
    combine: loadedOf,
  });
  const first = sites[0];
  const parts = useMemo<SiteNotices[]>(
    () => [
      ...(first
        ? [{ site: first, data: primary.data, failed: primary.isError && !primary.data }]
        : []),
      ...others.map((site, i) => ({
        site,
        data: loaded[i]?.data,
        failed: loaded[i]?.failed ?? false,
      })),
    ],
    [first, others, primary.data, primary.isError, loaded],
  );
  return { primary, parts, sites };
}

/** 一組訊息照台分開（動作要送到各自的台）。 */
function bySite(entries: readonly SitedNotice[]): { site: Site; ids: string[] }[] {
  const out = new Map<string, { site: Site; ids: string[] }>();
  for (const e of entries) {
    const group = out.get(e.site.key) ?? { site: e.site, ids: [] };
    group.ids.push(e.n.id ?? "");
    out.set(e.site.key, group);
  }
  return [...out.values()];
}

/** 分段的字：載入完才接數字；載入中只有名稱（WEB-70）。 */
export function segmentLabel(label: string, loaded: boolean, n: number): string {
  return loaded ? `${label} ${n}` : label;
}

function matches(n: Notification, filter: MessagesFilter | undefined): boolean {
  if (filter === "approve") return isPending(n);
  if (filter === "all") return true;
  return n.state === "unread";
}

/** 兩段式按鈕：第一下變成「確定？」，3 秒內再按才做（舊看板的做法，不開彈出視窗）。 */
function useArmed(): [boolean, (run: () => void) => void] {
  const [armed, setArmed] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const press = useCallback(
    (run: () => void) => {
      clearTimeout(timer.current);
      if (armed) {
        setArmed(false);
        run();
        return;
      }
      setArmed(true);
      timer.current = setTimeout(() => setArmed(false), 3000);
    },
    [armed],
  );
  return [armed, press];
}

/** 清單上的動作（刪除、全部已讀）：照台分組，各送到那則所在的台。 */
export interface NoticeActions {
  archive: (entries: readonly SitedNotice[]) => Promise<void>;
  markRead: (entries: readonly SitedNotice[]) => Promise<void>;
}

/**
 * 照台分組送出：送之前再看一次那台現在是不是 ok（清單畫出來之後才斷的台不送、也不退回主伺服器，FEN-28）；
 * 一台失敗不影響別台，回傳做成的結果與沒做成的台名。
 */
export async function sendBySite<R>(
  entries: readonly SitedNotice[],
  sites: readonly Site[],
  run: (site: Site, ids: string[]) => Promise<R>,
): Promise<{ done: R[]; failed: string[] }> {
  const usable = (site: Site) =>
    site.primary || sites.some((s) => s.key === site.key && s.state === "ok");
  const groups = bySite(entries);
  const results = await Promise.allSettled(
    groups.map(({ site, ids }) =>
      usable(site) ? run(site, ids) : Promise.reject(new Error("site not ok")),
    ),
  );
  const done: R[] = [];
  const failed: string[] = [];
  results.forEach((r, i) => {
    if (r.status === "fulfilled") done.push(r.value);
    else failed.push(siteName(groups[i]?.site ?? { label: "", base: "" }));
  });
  return { done, failed };
}

function useNoticeActions(sites: readonly Site[]): NoticeActions {
  const client = useQueryClient();
  return useMemo(() => {
    const refresh = (entries: readonly SitedNotice[]) =>
      Promise.all(bySite(entries).map(({ site }) => refreshAll(client, site))).then(() => {});
    return {
      archive: async (entries) => {
        const { done, failed } = await sendBySite(entries, sites, (site, ids) =>
          apiFor(site).notify.archive({ ids }),
        );
        const n = done.reduce((sum, r) => sum + (r.archived?.length ?? 0), 0);
        const kept = done.reduce((sum, r) => sum + (r.skipped?.length ?? 0), 0);
        if (done.length === 0) showToast(t("messages.delete.failed"));
        else {
          const msg =
            kept > 0 ? t("messages.deleted.kept", { n, kept }) : t("messages.deleted", { n });
          showToast(
            failed.length > 0 ? `${msg}${t("sitePart.notDone", { site: failed.join("、") })}` : msg,
          );
        }
        await refresh(entries);
      },
      markRead: async (entries) => {
        const { failed } = await sendBySite(entries, sites, (site, ids) =>
          apiFor(site).notify.markRead({ ids }),
        );
        if (failed.length > 0) showToast(t("messages.readAll.failed"));
        await refresh(entries);
      },
    };
  }, [client, sites]);
}

function DeleteOne({
  entries,
  label,
  actions,
  onDeleted,
}: {
  /** 要刪的訊息：一則，或收起來的那一組全部（WEB-86）。 */
  entries: readonly SitedNotice[];
  label: string;
  actions: NoticeActions;
  onDeleted?: () => void;
}) {
  const [armed, press] = useArmed();
  return (
    <button
      type="button"
      aria-label={armed ? t("messages.delete.confirm") : label}
      title={t("messages.delete")}
      onClick={() =>
        press(() => {
          void actions.archive(entries).then(onDeleted);
        })
      }
      className={cn(
        "flex h-11 shrink-0 items-center justify-center gap-1 rounded-full text-[13px] font-semibold transition-colors focus-visible:outline-2 focus-visible:outline-focus",
        armed
          ? "bg-danger px-3.5 text-accent-fg"
          : "w-11 text-muted hover:bg-danger-soft hover:text-danger",
      )}
    >
      {armed ? (
        t("messages.delete.confirm")
      ) : (
        <Trash2 className="size-[18px]" strokeWidth={1.8} aria-hidden="true" />
      )}
    </button>
  );
}

function Toolbar({ shown, actions }: { shown: SitedNotice[]; actions: NoticeActions }) {
  const [armed, press] = useArmed();
  const unread = shown.filter((e) => e.n.state === "unread");
  // 主人說刪就刪，還沒回答的「要你同意」也一樣（NTF-41）。
  const removable = shown;
  const markRead = () => void actions.markRead(unread);
  if (unread.length === 0 && removable.length === 0) return null;
  const btn = `${HIT_44} flex h-9 items-center rounded-full px-3.5 text-[13px] font-medium focus-visible:outline-2 focus-visible:outline-focus`;
  return (
    <div className="flex flex-wrap gap-2">
      {unread.length > 0 ? (
        <button
          type="button"
          onClick={markRead}
          className={cn(btn, "bg-surface-2 text-fg hover:bg-hover")}
        >
          {t("messages.readAll", { n: unread.length })}
        </button>
      ) : null}
      {removable.length > 0 ? (
        <button
          type="button"
          onClick={() => press(() => void actions.archive(removable))}
          className={cn(
            btn,
            armed
              ? "bg-danger text-accent-fg"
              : "bg-surface-2 text-fg hover:bg-danger-soft hover:text-danger",
          )}
        >
          {armed
            ? t("messages.deleteAll.confirm", { n: removable.length })
            : t("messages.deleteAll", { n: removable.length })}
        </button>
      ) : null}
    </div>
  );
}

interface RowProps {
  actions: NoticeActions;
  /** 看不只一台：每列標台名。 */
  multi: boolean;
  now: Date;
  search: MessagesSearch;
  wide: boolean;
  /** 收起來那一組展開後的一則：縮排、不畫自己的底線。 */
  nested?: boolean;
}

function Row({
  entry,
  actions,
  multi,
  now,
  search,
  selected,
  wide,
  nested = false,
  onDeleted,
}: RowProps & {
  entry: SitedNotice;
  selected: boolean;
  onDeleted?: () => void;
}) {
  const { n, site } = entry;
  const { view } = useAgents(site);
  const unread = n.state === "unread";
  const dot = noticeTone(n);
  const agent = view(n.agent);
  const answered = n.state === "answered" ? n.answer?.text : undefined;
  return (
    <li
      className={cn(
        "flex items-start gap-1",
        wide || nested ? "rounded-xl pr-1.5" : "border-b border-border pr-2 last:border-b-0",
        // 底色和選取框都畫在整列（含刪除鈕），不要只畫在連結上，不然會多出一塊奇怪的框。
        selected ? "bg-selected shadow-[inset_0_0_0_1px_var(--accent)]" : wide && "hover:bg-hover",
      )}
    >
      <Link
        to="/messages/$id"
        params={{ id: n.id ?? "" }}
        search={{ ...search, ...homeSearch(site) }}
        data-unread={unread ? "true" : "false"}
        className={cn(
          "flex min-h-[64px] min-w-0 flex-1 items-start gap-3 py-3.5 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus",
          wide ? "rounded-xl px-3" : "px-4",
        )}
      >
        {/* 要你回答＝橘點（回答了、被自動收掉就沒有）；其他未讀＝綠點；看過的沒有點（WEB-70）。 */}
        <span
          aria-hidden="true"
          data-dot={dot ?? "none"}
          className={cn(
            "mt-[7px] size-2 shrink-0 rounded-full",
            dot ? toneDot[dot] : "bg-transparent",
          )}
        />
        <span className="flex min-w-0 flex-1 flex-col gap-[3px]">
          <span className="flex min-w-0 items-center gap-1.5">
            <NoticePill n={n} />
            <span className="max-w-[45%] shrink-0 truncate text-[13px] text-muted">
              {senderName(n, agent.name)}
            </span>
            {multi ? <SiteTag site={site} /> : null}
            <span className="ml-auto shrink-0 text-xs text-muted">
              {relativeAgo(n.created_at, now)}
            </span>
          </span>
          <span
            className={cn(
              "text-[15px] leading-[1.45] break-words",
              unread ? "font-semibold" : "font-normal",
            )}
          >
            <RowCode id={n.id} />
            <SoftPath text={n.title ?? ""} />
          </span>
          {answered ? (
            <span className="text-[13px] text-ok">
              {t("messages.row.answered", { answer: answered })}
            </span>
          ) : null}
        </span>
      </Link>
      <div className="pt-2.5">
        <DeleteOne
          entries={[entry]}
          label={t("messages.delete.one", { id: n.id ?? "" })}
          actions={actions}
          onDeleted={onDeleted}
        />
      </div>
    </li>
  );
}

/**
 * 收起來的一組同一種系統通知（WEB-86）：一列寫最新那則的標題＋「×N」，點了展開每一則；右邊的刪除一次刪整組。
 * 選取中的那則在組裡時一開始就展開。
 */
function GroupRow({
  run,
  selectedKey,
  onDeleted,
  ...row
}: RowProps & {
  run: readonly SitedNotice[];
  selectedKey: string | undefined;
  onDeleted?: () => void;
}) {
  const head = run[0];
  const [open, setOpen] = useState(() => run.some((e) => e.key === selectedKey));
  const { view } = useAgents(head?.site);
  if (!head) return null;
  const { n, site } = head;
  const unread = run.some((e) => e.n.state === "unread");
  const agent = view(n.agent);
  const { wide, multi, now } = row;
  return (
    <li
      data-testid="notice-group"
      data-count={run.length}
      className={cn(
        "flex flex-col",
        wide ? "rounded-xl" : "border-b border-border last:border-b-0",
      )}
    >
      <div className={cn("flex items-start gap-1", wide ? "pr-1.5" : "pr-2")}>
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
          className={cn(
            "flex min-h-[64px] min-w-0 flex-1 items-start gap-3 py-3.5 text-left focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus",
            wide ? "rounded-xl px-3 hover:bg-hover" : "px-4",
          )}
        >
          <span
            aria-hidden="true"
            data-dot={unread ? "fresh" : "none"}
            className={cn(
              "mt-[7px] size-2 shrink-0 rounded-full",
              unread ? toneDot.fresh : "bg-transparent",
            )}
          />
          <span className="flex min-w-0 flex-1 flex-col gap-[3px]">
            <span className="flex min-w-0 items-center gap-1.5">
              <NoticePill n={n} />
              <span className="max-w-[45%] shrink-0 truncate text-[13px] text-muted">
                {senderName(n, agent.name)}
              </span>
              {multi ? <SiteTag site={site} /> : null}
              <span className="ml-auto shrink-0 text-xs text-muted">
                {relativeAgo(n.created_at, now)}
              </span>
            </span>
            <span
              className={cn(
                "text-[15px] leading-[1.45] break-words",
                unread ? "font-semibold" : "font-normal",
              )}
            >
              <SoftPath text={n.title ?? ""} />{" "}
              <span className="ml-0.5 inline-flex rounded-full bg-surface-2 px-2 align-[1px] text-xs font-semibold text-muted">
                ×{run.length}
              </span>
            </span>
            <span className="flex items-center gap-0.5 text-[13px] text-muted">
              {open ? t("messages.group.hide") : t("messages.group.show", { n: run.length })}
              <ChevronDown
                aria-hidden="true"
                className={cn("size-3.5 transition-transform", open && "rotate-180")}
              />
            </span>
          </span>
        </button>
        <div className="pt-2.5">
          <DeleteOne
            entries={run}
            label={t("messages.delete.group", { n: run.length })}
            actions={row.actions}
            onDeleted={onDeleted}
          />
        </div>
      </div>
      {open ? (
        <ul
          aria-label={t("messages.group.list", { n: run.length })}
          className="m-0 mb-2 ml-6 flex list-none flex-col border-l border-border p-0 pl-1"
        >
          {run.map((e) => (
            <Row
              key={e.key}
              entry={e}
              {...row}
              nested
              selected={wide && e.key === selectedKey}
              onDeleted={wide && e.key === selectedKey ? onDeleted : undefined}
            />
          ))}
        </ul>
      ) : null}
    </li>
  );
}

/**
 * 「秘書｜系統」兩個分頁（D-47、WEB-86）：各自一個網址（?lane=），分頁上寫那一道的未讀數；
 * 系統那道有還等你處理的（要你回答、urgent），改寫那幾則的數字、橘色。換分頁時保留分段（?show=）。
 */
function LaneTabs({
  lane,
  filter,
  loaded,
  unread,
  systemPending,
}: {
  lane: Lane;
  filter: MessagesFilter | undefined;
  loaded: boolean;
  unread: Record<Lane, number>;
  systemPending: number;
}) {
  const tabs: { lane: Lane; label: string }[] = [
    { lane: "agent", label: t("messages.lane.agent") },
    { lane: "system", label: t("messages.lane.system") },
  ];
  return (
    <nav aria-label={t("messages.lanes")} className="flex items-end gap-1 border-b border-border">
      {tabs.map((x) => {
        // 系統那道有等你處理的：數字是那幾則、橘色（讀過還沒回的也算，不會變 0）；沒有就是灰色的未讀數。
        const pending = x.lane === "system" && systemPending > 0;
        const n = pending ? systemPending : unread[x.lane];
        const label = pending
          ? t("messages.lane.pending", { n })
          : t("messages.lane.unread", { n });
        return (
          <Link
            key={x.lane}
            to="/messages"
            search={{ show: filter, lane: x.lane === "system" ? "system" : undefined }}
            replace
            // 兩個分頁都連 /messages，只差 ?lane=：要連查詢字串一起比，不然 TanStack 會把兩個都標成目前這頁。
            activeOptions={{ exact: true, includeSearch: true }}
            aria-current={lane === x.lane ? "page" : undefined}
            data-lane={x.lane}
            className={cn(tabClass(lane === x.lane), "gap-1.5")}
          >
            {x.label}
            {loaded && n > 0 ? (
              <>
                <span
                  aria-hidden="true"
                  title={label}
                  className={cn(
                    "min-w-5 rounded-full px-1.5 text-center text-xs leading-5 font-semibold",
                    pending ? toneBadge.reply : toneBadge.idle,
                  )}
                >
                  {n}
                </span>
                <span className="sr-only">{label}</span>
              </>
            ) : null}
          </Link>
        );
      })}
    </nav>
  );
}

/** 在「秘書」分頁時：系統那道有還要你回答的，上面一行帶你過去（不讓它藏在分頁裡，NTF-57）。 */
function SystemWaiting({ n, filter }: { n: number; filter: MessagesFilter | undefined }) {
  if (n === 0) return null;
  return (
    <Link
      to="/messages"
      search={{ show: filter === "all" ? "all" : "approve", lane: "system" }}
      replace
      data-testid="system-waiting"
      className="flex min-h-11 items-center gap-2 rounded-xl bg-attention-soft px-3.5 text-sm font-medium text-attention-fg focus-visible:outline-2 focus-visible:outline-focus"
    >
      <span aria-hidden="true" className={cn("size-2 shrink-0 rounded-full", toneDot.reply)} />
      {t("messages.lane.systemWaiting", { n })}
    </Link>
  );
}

// 訊息：未封存的通知，新到舊；上方分段「未讀（預設）／要你同意／全部」（寫在網址 ?show=）。
// 每一列右邊可以原地刪除（兩段式確認，每一則都能刪）；上面有「全部標為已讀」「全部刪除」（只算目前看到的）。
// ≥ 1024px 兩欄：左清單、右邊是選取的那一則（/messages/$id，推播或別處的連結進來也在這裡打開）。
// 看多台時（S3-7a）清單是各台合併的，每列標台名；某台讀不到只在清單上面說那一台，其他台照常。
// selected 是那一則的編號、selectedSite 是它在哪一台（網址的 ?home=；沒寫＝主伺服器）。
export function MessagesPage({
  selected,
  selectedSite = PRIMARY,
  selectedPending = false,
}: {
  selected?: string;
  /**
   * 選取的那一則在哪一台。null＝網址的 ?home= 那台還找不到（看 selectedPending 分還在試連或設定裡沒有）：
   * 這時右欄不讀也不標已讀，絕不拿主伺服器同編號的那則來顯示（FEN-28）。用 null 不用 undefined，免得被預設值換成主伺服器。
   */
  selectedSite?: SiteRef | null;
  selectedPending?: boolean;
} = {}) {
  const desktop = useIsDesktop();
  const wide = useIsWide();
  const now = useNow();
  const navigate = useNavigate();
  const { show: filter, lane: laneParam } = useSearch({ strict: false }) as MessagesSearch;
  const { primary: query, parts, sites } = useSiteNotices();
  const actions = useNoticeActions(sites);
  const multi = sites.length > 1;
  const selectedKey =
    selected !== undefined && selectedSite ? itemKey(selectedSite, selected) : undefined;

  const { picked } = useAgentFilter();
  const everything = useMemo(() => mergeNotices(parts), [parts]);
  // 網址沒寫 ?lane= 卻選了一則（推播、今天頁、別處的連結進來）：清單跟著那一則所在的那道，選取的才在清單裡、組才會展開。
  const selectedEntry =
    selectedKey === undefined ? undefined : everything.find((e) => e.key === selectedKey);
  const lane: Lane =
    laneParam === "system" ? "system" : selectedEntry ? laneOf(selectedEntry.n) : "agent";
  const search: MessagesSearch = { show: filter, lane: lane === "system" ? "system" : undefined };
  const picks = useMemo(
    () => everything.filter((e) => matchAgent(picked, e.n.agent, e.site.key)),
    [everything, picked],
  );
  // 「秘書｜系統」分兩道（D-47）：下面的分段、數字、清單都只看目前這一道。
  const all = useMemo(() => picks.filter((e) => laneOf(e.n) === lane), [picks, lane]);
  const laneUnread: Record<Lane, number> = { agent: 0, system: 0 };
  // 系統那道還等你處理的：要你回答還沒回的、或核心標了算徽章的（urgent 未讀，NTF-57）——這些推了手機，不能藏在分頁裡。
  let systemPending = 0;
  for (const e of picks) {
    if (e.n.state === "unread") laneUnread[laneOf(e.n)] += 1;
    if (laneOf(e.n) === "system" && (isPending(e.n) || countsInBadge(e.n))) systemPending += 1;
  }
  // 打開的那一則就算剛變成已讀，也先留在「未讀」清單裡，不會一點就消失。
  const shown = useMemo(
    () => all.filter((e) => matches(e.n, filter) || (wide && e.key === selectedKey)),
    [all, filter, wide, selectedKey],
  );
  const count = (f: MessagesFilter | undefined) => all.filter((e) => matches(e.n, f)).length;
  // 系統那道：連在一起、同一種的收成一列「<標題> ×N」；還要你回答的不收，一則一列（WEB-86）。
  const runs = useMemo(
    () =>
      lane === "system"
        ? collapseRuns(shown, (e) =>
            isPending(e.n) || countsInBadge(e.n) ? null : `${e.site.key}\n${noticeKind(e.n)}`,
          )
        : shown.map((e) => [e]),
    [shown, lane],
  );
  // 徽章＝這位未讀的筆數（key 帶台名）；「全部」下面是目前分段的總筆數（不受篩選影響）。
  const counts = useMemo(() => {
    const out: Record<string, number> = {};
    for (const p of parts) {
      Object.assign(
        out,
        countByAgent(
          (p.data ?? []).filter((n) => !n.inline && n.state === "unread" && laneOf(n) === lane),
          (n) => n.agent,
          p.site.key,
        ),
      );
    }
    return out;
  }, [parts, lane]);
  const viewTotal = everything.filter((e) => laneOf(e.n) === lane && matches(e.n, filter)).length;
  const failed = parts.filter((p) => !p.site.primary && p.failed).map((p) => p.site.key);
  const notice = <SiteProblems what={t("sitePart.what.message")} failed={failed} />;
  usePublishPageCounts(query.data === undefined ? null : { counts, total: viewTotal });

  // 還沒載入完不寫數字（不先顯示 0 再跳成真的數字）。
  const loaded = query.data !== undefined;
  const options: { value: FilterOption; label: string }[] = [
    { value: "unread", label: segmentLabel(t("messages.filter.unread"), loaded, count(undefined)) },
    {
      value: "approve",
      label: segmentLabel(t("messages.filter.approve"), loaded, count("approve")),
    },
    { value: "all", label: segmentLabel(t("messages.filter.all"), loaded, count("all")) },
  ];
  const backToList = () => void navigate({ to: "/messages", search, replace: true });

  const segmented = (
    <Segmented
      options={options}
      value={filter ?? "unread"}
      onChange={(next) =>
        void navigate({
          to: "/messages",
          // 帶目前這一道（含網址沒寫、照選的那則推出來的系統道），換分段不會跳回「秘書」（WEB-102）。
          search: { show: next === "unread" ? undefined : next, lane: search.lane },
          replace: true,
        })
      }
      ariaLabel={t("messages.filter")}
      compact={desktop}
    />
  );

  const laneTabs = (
    <>
      <LaneTabs
        lane={lane}
        filter={filter}
        loaded={loaded}
        unread={laneUnread}
        systemPending={systemPending}
      />
      {lane === "agent" ? <SystemWaiting n={systemPending} filter={filter} /> : null}
    </>
  );

  const body =
    query.data === undefined ? (
      query.isError ? (
        <LoadFailed what={t("page.messages")} onRetry={() => void query.refetch()} />
      ) : (
        <LoadingBlock />
      )
    ) : picked.length > 0 && shown.length === 0 ? (
      <FilterEmpty />
    ) : shown.length === 0 ? (
      <EmptyState
        variant={wide ? "plain" : undefined}
        title={
          filter === "approve"
            ? t("messages.empty.approve")
            : filter === "all"
              ? t("messages.empty")
              : t("messages.empty.unread")
        }
        hint={lane === "system" ? t("messages.empty.hint.system") : t("messages.empty.hint")}
      />
    ) : (
      <ul
        aria-label={t("messages.list")}
        className={cn(
          "m-0 flex list-none flex-col p-0",
          wide
            ? "gap-0.5"
            : "overflow-hidden rounded-[18px] bg-surface shadow-[var(--shadow-card)]",
        )}
      >
        {runs.map((run) => {
          const e = run[0];
          const oldest = run[run.length - 1];
          if (!e || !oldest) return null;
          const row = { actions, multi, now, search, wide };
          if (run.length > 1)
            return (
              // key 用組裡最舊的那則：新的一則進來時不換 key，展開著的組不會被收回去。
              <GroupRow
                key={`group:${oldest.key}`}
                run={run}
                selectedKey={selectedKey}
                onDeleted={wide && run.some((x) => x.key === selectedKey) ? backToList : undefined}
                {...row}
              />
            );
          return (
            <Row
              key={e.key}
              entry={e}
              {...row}
              selected={wide && e.key === selectedKey}
              onDeleted={wide && e.key === selectedKey ? backToList : undefined}
            />
          );
        })}
      </ul>
    );

  if (wide) {
    return (
      <div className="flex h-full min-h-0">
        <section
          aria-label={t("messages.list")}
          className="flex w-[420px] shrink-0 flex-col border-r border-border bg-bg"
        >
          <div className="flex flex-col gap-3 px-[18px] pt-[18px] pb-3">
            <h1 className="text-xl font-bold">{t("page.messages")}</h1>
            {laneTabs}
            {segmented}
            <Toolbar shown={shown} actions={actions} />
            {notice}
          </div>
          <div className="hg-scroll min-h-0 flex-1 overflow-y-auto px-2.5 pb-3">{body}</div>
        </section>
        <section className="flex min-w-0 flex-1 flex-col bg-surface">
          {selected && selectedSite ? (
            <NotificationView
              key={selectedKey}
              id={selected}
              site={selectedSite}
              embedded
              keycaps={false}
            />
          ) : selected && selectedPending ? (
            <LoadingBlock />
          ) : selected ? (
            <NotFoundBlock
              title={t("message.notFound")}
              hint={t("message.notFound.hint")}
              backLabel={t("message.back")}
              to="/messages"
            />
          ) : (
            <div className="flex flex-1 items-center justify-center text-sm text-muted">
              {t("messages.pick")}
            </div>
          )}
        </section>
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-[720px] flex-col gap-3.5 px-4 pt-3 pb-7">
      {desktop ? (
        <h1 className="pt-6 text-2xl font-bold tracking-[-0.01em]">{t("page.messages")}</h1>
      ) : null}
      {laneTabs}
      {segmented}
      <Toolbar shown={shown} actions={actions} />
      {notice}
      {body}
    </div>
  );
}
