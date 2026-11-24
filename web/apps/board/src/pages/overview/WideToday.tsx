import { Avatar, cn, EmptyState, Segmented, showToast } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { type KeyboardEvent, type ReactNode, useCallback, useEffect, useMemo, useRef } from "react";
import type { PaneActions } from "../../components/AnswerPanel";
import { FilterEmpty } from "../../components/FilterEmpty";
import { NotificationView } from "../../components/NotificationView";
import { QuickInput } from "../../components/QuickInput";
import { RowCode } from "../../components/RowCode";
import { SiteRefTag } from "../../components/SiteTag";
import { TaskView } from "../../components/TaskView";
import { useAgentFilter } from "../../data/agentFilter";
import { homeQuery } from "../../data/queries";
import { canLinkDetail } from "../../data/siteLinks";
import { homeSearch } from "../../data/sites";
import { itemPreview, type WaitingItem, type WaitingKind } from "../../data/waiting";
import { t } from "../../i18n/zh-TW";
import { Slot } from "../../pkg/Slot";
import { AgentHero, AgentStrip, AgentTasks } from "./AgentHome";
import { greetingText, todaySubtitle } from "./greeting";
import { KeysHelp } from "./KeysHelp";
import { KindPill } from "./KindPill";
import { type SitedReminder, type SitedThread, useSiteViews } from "./merged";
import { NewMessageThreads, TodayReminders, UnreadRow, WaitingThreads } from "./Sections";
import { type TodayKey, todayKeyAction, todayPaneKeyAction } from "./todayKeys";
import { WaitingCard, waitingTime } from "./WaitingCard";

/** 「要你同意」跟「要你回答」對主人都是「等你處理」，篩選併成一格（DEV-30）。 */
const reply = (k: WaitingKind): WaitingKind => (k === "ask" ? "approve" : k);

export type TodayFilter = "all" | WaitingKind;

export interface TodaySearch {
  sel?: string;
  filter?: Exclude<TodayFilter, "all">;
}

function useTodaySearch(): TodaySearch {
  return useSearch({ strict: false }) as TodaySearch;
}

// 桌機（≥ 1024px）的「今天」：左欄 400 寬是等你處理的清單（上面有篩選），右欄是選取那件的摘要；
// 沒選的時候右欄是今天的總覽（問候、秘書卡片、等你處理的串、今天的待辦、未讀、套件卡片），跟手機的「今天」同一批內容。
// 秘書篩選只選一位時是她的首頁（半身照與影片、她的數字、她手上的任務單，不放套件卡片），排成一欄。
// 選取與篩選都寫在網址（/?sel=…&filter=…），重新整理還在（FEN-4）。
// 只看一位、或一件等你處理的事都沒有時，不分欄（REQ-234）：輸入框、問候或她的首頁、「等你處理」的卡片、其他段落排成一欄，
// 中間不留一條空的清單；網址帶著 sel 的時候照樣兩欄打開那一件。
// 看多台時（S3-7a）清單是各台合併的，sel 是 itemKey（別台「台名/編號」），右欄的動作送到那件所在的台。
export function WideToday({
  items,
  cards,
  total,
  now,
  filterEmpty,
  threads,
  fresh,
  reminders,
  unread,
  notice,
}: {
  items: WaitingItem[];
  /** 一欄時的卡片：剛處理完的先留在原位（綠色一列），跟手機一樣。 */
  cards: WaitingItem[];
  total: number;
  now: Date;
  /** 秘書篩選之後一件都沒有：顯示「<人名> 今天沒有等你處理的事」，不是「都處理完了」。 */
  filterEmpty?: boolean;
  threads: SitedThread[];
  /** 秘書說了、你還沒看的對話（NewMessageThreads，DEV-153）。 */
  fresh: SitedThread[];
  reminders: SitedReminder[];
  unread: number;
  /** 某台讀不到的提示（SiteProblems）；放在「等你處理」上面。 */
  notice?: ReactNode;
}) {
  const userName = useQuery(homeQuery()).data?.userName ?? "";
  const navigate = useNavigate();
  const viewOf = useSiteViews();
  const { one, picked } = useAgentFilter();
  // 只選一位＝她的首頁（右欄頂端是她的半身照與影片）；全部或好幾位＝總覽（多一排秘書卡片）。
  const single = one || undefined;
  const { sel, filter } = useTodaySearch();
  const listRef = useRef<HTMLFieldSetElement>(null);
  // 右欄動作列登記的 A／R 動作；清單有焦點時鍵盤才轉給它（輸入框裡不攔）。
  const actionsRef = useRef<PaneActions | null>(null);
  const bind = useCallback((actions: PaneActions | null) => {
    actionsRef.current = actions;
  }, []);

  const visible = useMemo(
    () => (filter ? items.filter((i) => reply(i.kind) === reply(filter)) : items),
    [items, filter],
  );
  const selected = visible.find((i) => i.key === sel);
  const count = (kind: WaitingKind) => items.filter((i) => reply(i.kind) === reply(kind)).length;

  const select = (id: string | undefined) =>
    void navigate({
      to: "/",
      search: (prev: TodaySearch) => ({ ...prev, sel: id }),
      replace: true,
    });
  const setFilter = (next: TodayFilter) =>
    void navigate({
      to: "/",
      search: (prev: TodaySearch) => ({
        ...prev,
        filter: next === "all" ? undefined : next,
        sel: undefined,
      }),
      replace: true,
    });

  const move = (delta: number) => {
    if (visible.length === 0) return;
    const at = visible.findIndex((i) => i.key === sel);
    const next =
      at < 0
        ? delta > 0
          ? 0
          : visible.length - 1
        : Math.min(visible.length - 1, Math.max(0, at + delta));
    select(visible[next]?.key);
  };
  // 動作做完，選取跳到下一件（沒有就上一件）。
  const latest = useRef({ visible, sel });
  latest.current = { visible, sel };
  const advance = useCallback(() => {
    const { visible: list, sel: current } = latest.current;
    const at = list.findIndex((i) => i.key === current);
    const next = list[at + 1] ?? list[at - 1];
    void navigate({
      to: "/",
      search: (prev: TodaySearch) => ({ ...prev, sel: next?.key }),
      replace: true,
    });
  }, [navigate]);

  const run = (action: TodayKey) => {
    if (action === "primary") actionsRef.current?.primary();
    else if (action === "secondary") actionsRef.current?.secondary();
    else move(action === "down" ? 1 : -1);
  };
  const runRef = useRef(run);
  runRef.current = run;

  const onKeyDown = (e: KeyboardEvent<HTMLFieldSetElement>) => {
    const action = todayKeyAction(e);
    if (action === null) return;
    e.preventDefault();
    run(action);
  };
  // 焦點不在清單也能用字母鍵（WEB-129）：用滑鼠在右欄點過按鈕、連結之後焦點在右欄；
  // 按完的那顆按鈕消失（選取跳下一件、右欄換內容）焦點掉回頁面本身。打字的地方不攔；
  // 右欄裡跳出的對話框是 portal，DOM 上不在右欄裡，也不攔（免得在確認框按 A 變成核准）。
  const paneRef = useRef<HTMLElement>(null);
  useEffect(() => {
    const onDoc = (e: globalThis.KeyboardEvent) => {
      if (e.defaultPrevented || !listRef.current) return;
      const target = e.target as Node | null;
      if (target !== document.body && !paneRef.current?.contains(target)) return;
      // 有對話框開著（按鈕停用時焦點可能掉回 body）：不攔，免得按 A 核准了背後那一件。
      if (document.querySelector('[aria-modal="true"]')) return;
      const action = todayPaneKeyAction(e);
      if (action === null) return;
      e.preventDefault();
      runRef.current(action);
    };
    document.addEventListener("keydown", onDoc);
    return () => document.removeEventListener("keydown", onDoc);
  }, []);

  useEffect(() => {
    if (!sel) return;
    listRef.current
      ?.querySelector(`[data-id="${CSS.escape(sel)}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [sel]);

  if (!selected && (single || items.length === 0)) {
    return (
      <div data-testid="today-one-column" className="h-full min-h-0 overflow-y-auto">
        <div className="mx-auto flex max-w-[720px] flex-col gap-[26px] px-8 pt-9 pb-10">
          {single ? (
            <AgentHero id={single} waiting={total} reminders={reminders.length} ids={picked} />
          ) : (
            <div className="flex flex-col gap-1">
              <h1 className="font-display text-[30px] leading-tight font-bold">
                {greetingText(now, userName)}
              </h1>
              <p className="text-sm text-muted">{todaySubtitle(now, total)}</p>
            </div>
          )}
          <QuickInput variant="today" />
          {notice}
          {single ? null : <Slot id="core.overview.cards" className="flex flex-col gap-3" />}
          {cards.length > 0 ? (
            <section aria-label={t("today.waiting")} className="flex flex-col gap-2.5">
              <div className="flex items-baseline gap-2">
                <h2 className="font-display text-lg font-bold">{t("today.waiting")}</h2>
                <span className="text-[15px] text-muted">{total}</span>
              </div>
              {cards.map((item, index) => (
                <WaitingCard key={item.key} item={item} now={now} index={index} />
              ))}
            </section>
          ) : filterEmpty ? null : (
            <EmptyState
              variant="plain"
              title={t("today.waiting.empty")}
              hint={t("today.waiting.emptyHint")}
            />
          )}
          {single ? null : <AgentStrip />}
          <WaitingThreads threads={threads} now={now} />
          <NewMessageThreads threads={fresh} />
          {single && reminders.length === 0 ? null : <TodayReminders reminders={reminders} />}
          {single ? <AgentTasks ids={picked} /> : null}
          <UnreadRow unread={unread} />
        </div>
      </div>
    );
  }

  const filterOptions: { value: TodayFilter; label: string }[] = [
    { value: "approve", label: `${t("today.filter.approve")} ${count("approve")}` },
    { value: "review", label: `${t("today.filter.review")} ${count("review")}` },
    { value: "all", label: `${t("today.filter.all")} ${items.length}` },
  ];

  const WaitingTitle = selected ? "h1" : "h2";
  return (
    <div className="flex h-full min-h-0">
      <section
        aria-label={t("today.waiting")}
        className="flex w-[400px] shrink-0 flex-col border-r border-border bg-bg"
      >
        <div className="flex flex-col gap-3 px-[18px] pt-[18px] pb-3">
          <QuickInput variant="today" />
          <div className="flex items-center gap-2 pt-1">
            {/* 這頁的第一層標題是右邊的問候語，「等你處理」是它下面的一段；右邊換成某一件事的細節時
                問候語不在了，這裡就是這頁的第一層標題（一頁一個 h1，DEV-156 U-31）。 */}
            <WaitingTitle className="text-xl font-bold">{t("today.waiting")}</WaitingTitle>
            <span className="text-[15px] text-muted">{total}</span>
            <KeysHelp />
          </div>
          <Segmented
            options={filterOptions}
            value={filter ?? "all"}
            onChange={setFilter}
            ariaLabel={t("today.filter")}
            compact
          />
          {notice}
        </div>
        <fieldset
          ref={listRef}
          // biome-ignore lint/a11y/noNoninteractiveTabindex: 清單容器要能用 J／K 操作，所以可聚焦（補充契約 S3 第 12 點）
          tabIndex={0}
          onKeyDown={onKeyDown}
          className="m-0 flex min-h-0 min-w-0 border-0 flex-1 flex-col gap-0.5 overflow-y-auto px-2.5 pb-2.5 outline-none focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-focus"
        >
          <legend className="sr-only">{t("today.list")}</legend>
          {visible.map((item) => {
            const agent = viewOf(item.site, item.agent);
            const on = item.key === selected?.key;
            return (
              <button
                key={item.key}
                type="button"
                data-id={item.key}
                aria-current={on ? "true" : undefined}
                onClick={() => select(item.key)}
                className={cn(
                  "hg-rise flex gap-3 rounded-xl px-3 py-3 text-left text-fg hover:bg-hover",
                  on && "bg-selected shadow-[inset_0_0_0_1px_var(--accent)] hover:bg-selected",
                )}
              >
                <Avatar
                  name={agent.name}
                  color={agent.color}
                  colorDark={agent.colorDark}
                  src={agent.avatarUrl}
                  size={34}
                />
                <span className="flex min-w-0 flex-1 flex-col gap-[3px]">
                  <span className="flex min-w-0 items-center gap-1.5">
                    <KindPill
                      kind={item.kind}
                      className="shrink-0 px-[7px] py-px text-[11px] font-bold whitespace-nowrap"
                    />
                    <span className="max-w-[45%] shrink-0 truncate text-xs text-muted">
                      {agent.name}
                    </span>
                    <SiteRefTag site={item.site} />
                    <span className="ml-auto shrink-0 text-xs whitespace-nowrap text-muted">
                      {waitingTime(item, now)}
                    </span>
                  </span>
                  <span className="text-sm leading-[1.4] font-semibold">
                    <RowCode id={item.id} />
                    {item.title}
                  </span>
                  <span className="truncate text-[13px] text-muted">{itemPreview(item)}</span>
                </span>
              </button>
            );
          })}
          {visible.length === 0 && filterEmpty ? <FilterEmpty variant="today" /> : null}
          {visible.length === 0 && !filterEmpty ? (
            <EmptyState
              variant="plain"
              title={t("today.waiting.empty")}
              hint={t("today.waiting.emptyHint")}
            />
          ) : null}
        </fieldset>
      </section>
      {selected ? (
        <section ref={paneRef} className="flex min-w-0 flex-1 flex-col bg-surface">
          <Detail key={selected.key} item={selected} bind={bind} advance={advance} />
        </section>
      ) : (
        <section aria-label={t("today.summary")} className="min-w-0 flex-1 overflow-y-auto">
          <div className="mx-auto flex max-w-[720px] flex-col gap-[26px] px-8 pt-9 pb-10">
            {/* 走到這裡一定是「全部或好幾位、有事等你」：只看一位或沒事的時候上面已經改成一欄。 */}
            <div className="flex flex-col gap-1">
              <h1 className="font-display text-[30px] leading-tight font-bold">
                {greetingText(now, userName)}
              </h1>
              <p className="text-sm text-muted">{todaySubtitle(now, total)}</p>
              {visible.length > 0 ? (
                <p className="pt-1 text-[13px] text-muted">{t("today.detail.none")}</p>
              ) : null}
            </div>
            <AgentStrip />
            <WaitingThreads threads={threads} now={now} />
            <NewMessageThreads threads={fresh} />
            <TodayReminders reminders={reminders} />
            <UnreadRow unread={unread} />
            <Slot id="core.overview.cards" className="flex flex-col gap-3" />
          </div>
        </section>
      )}
    </div>
  );
}

// 右欄：選取那件的完整內容（通知用 /messages/$id 的內容、任務單用 /tasks/$id 的內容）＋固定在底部的動作列。
function Detail({
  item,
  bind,
  advance,
}: {
  item: WaitingItem;
  bind: (actions: PaneActions | null) => void;
  advance: () => void;
}) {
  // 別台的單詳情頁還不認 ?home（S3-7b 前）：不給「打開完整頁面」，免得打開這台同編號的那張。
  const open = canLinkDetail(item.site, item.target.to) ? (
    <Link
      to={item.target.to}
      params={item.target.params}
      search={homeSearch(item.site)}
      className="ml-auto flex h-[38px] items-center rounded-[10px] px-3.5 text-sm font-medium text-accent focus-visible:outline-2 focus-visible:outline-focus"
    >
      {t("today.detail.open")}
    </Link>
  ) : null;
  if (item.kind === "approve") {
    return (
      <NotificationView
        id={item.id}
        site={item.site}
        embedded
        bind={bind}
        paneExtra={open}
        onAnswered={(text) => {
          showToast(t("answer.toast", { answer: text }));
          advance();
        }}
      />
    );
  }
  return (
    <TaskView
      id={item.id}
      site={item.site}
      embedded
      bind={bind}
      paneExtra={open}
      onAnswered={(text) => {
        showToast(t("answer.toast", { answer: text }));
        advance();
      }}
      onDone={(kind) => {
        if (kind === "chat") return;
        if (kind === "delete") {
          advance();
          return;
        }
        showToast(
          t(kind === "accept" ? "answer.task.toastAccepted" : "answer.task.toastRejected", {
            id: item.id,
          }),
        );
        advance();
      }}
    />
  );
}
