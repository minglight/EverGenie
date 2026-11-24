import { Button, EmptyState, Spinner } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { Navigate } from "@tanstack/react-router";
import { useMemo } from "react";
import { FilterEmpty } from "../../components/FilterEmpty";
import { QuickInput } from "../../components/QuickInput";
import { SiteProblems } from "../../components/SiteNotice";
import { UpgradeBanner } from "../../components/UpgradeBanner";
import { matchAgent, useAgentFilter } from "../../data/agentFilter";
import { mergeSettled, useSettled } from "../../data/answers";
import { usePublishPageCounts } from "../../data/pageCounts";
import { homeQuery, rosterQuery, threadsQuery } from "../../data/queries";
import { t } from "../../i18n/zh-TW";
import { useIsDesktop, useIsWide } from "../../lib/media";
import { useNow } from "../../lib/now";
import { useAfter } from "../../lib/useAfter";
import { Slot } from "../../pkg/Slot";
import { AgentHero, AgentTasks } from "./AgentHome";
import { greetingText, todaySubtitle } from "./greeting";
import {
  failedSites,
  mergedCounts,
  mergedWaitingTotal,
  mergeWork,
  useFilteredUnread,
  useOverviews,
  useUnreadThreads,
} from "./merged";
import {
  NewMessageThreads,
  SectionHeading,
  TodayReminders,
  UnreadRow,
  WaitingThreads,
} from "./Sections";
import { WaitingCard } from "./WaitingCard";
import { WideToday } from "./WideToday";

/** 總覽等多久還沒到，就先把頁面其他部分畫出來（WEB-175）。 */
const TODAY_GRACE_MS = 1_500;

// 「今天」：等你處理的事（數字由伺服器算好，前端不算，FEN-N3）。
// 看多台時（S3-7a）各台的總覽併在一起、每筆標台名；某台讀不到只在上面說那一台，其他台照常。
export function OverviewPage() {
  const wide = useIsWide();
  const desktop = useIsDesktop();
  const now = useNow();
  const slow = useAfter(TODAY_GRACE_MS);
  const { parts, primary: overview } = useOverviews();
  useQuery(threadsQuery());
  const data = overview.data;
  const settled = useSettled();
  const { picked, one } = useAgentFilter();
  const filtered = picked.length > 0;
  // 秘書篩選：三段（等你處理、等你處理的串、今天的待辦）都只留選中的人。
  const work = useMemo(() => mergeWork(parts, picked), [parts, picked]);
  const { items, threads, reminders } = work;
  // 剛處理完的卡片先留在原位置（綠色一列），不然伺服器一更新就整張消失，看不到「已回」。
  const shown = useMemo(
    () => mergeSettled(items, settled).filter((i) => matchAgent(picked, i.agent, i.site.key)),
    [items, settled, picked],
  );
  const waitingTotal = mergedWaitingTotal(parts, picked);
  const unreadCount = useFilteredUnread(parts, picked);
  // 秘書說了、你還沒看的對話（DEV-153，例：新家第一位秘書上班後的招呼）；排在「要你回答的討論串」後面。
  const fresh = useUnreadThreads(threads, picked);
  // 上一次更新沒成功的結果與下一步（WEB-173）擺在別台的狀況前面：這台自己的事。
  const notice = (
    <>
      <UpgradeBanner />
      <SiteProblems what={t("sitePart.what.today")} failed={failedSites(parts)} />
    </>
  );

  // 徽章與「全部」下面的 N 件：「等你處理」只算要你同意、要你驗收或決定這兩類（跟頁首大字同一個定義，
  // 也就是伺服器的 waiting_total）；等你處理的聊天有聊天分頁的黃點、今天的待辦有自己的段落，都不算進來。
  // 不受篩選影響，桌機側欄也用同一份。看多台時各台相加、key 帶台名。
  const { counts, total } = useMemo(() => mergedCounts(parts), [parts]);
  usePublishPageCounts(data === undefined ? null : { counts, total });
  const roster = useQuery(rosterQuery());
  const userName = useQuery(homeQuery()).data?.userName ?? "";

  // 還沒有任何秘書上班：先帶去設定精靈選職位與角色（REQ-238）。
  if (roster.data !== undefined && roster.data.length === 0) {
    return <Navigate to="/settings/setup" replace />;
  }

  if (data === undefined) {
    const failed = overview.isError;
    // 正常幾百毫秒就讀到：這段只轉圈，不先畫一份再換（版面不跳）。
    if (!failed && !slow) {
      return (
        <div className="flex justify-center py-16">
          <Spinner label={t("app.loading")} />
        </div>
      );
    }
    // 等太久或讀失敗（WEB-175）：只有「等你處理」這一塊說讀不到，輸入框、別台的狀況、套件的卡片照常。
    const status = failed ? (
      <div
        key="status"
        role="alert"
        data-testid="today-unavailable"
        className="flex flex-col items-center gap-2 px-6 py-10 text-center"
      >
        <p className="text-[15px] font-semibold">{t("today.loadFailed")}</p>
        <p className="text-[13px] text-muted">{t("app.loadFailed.hint")}</p>
        <Button variant="primary" className="mt-2" onClick={() => void overview.refetch()}>
          {t("app.retry")}
        </Button>
      </div>
    ) : (
      <div
        key="status"
        role="status"
        data-testid="today-waiting"
        className="flex flex-col items-center gap-2 px-6 py-10 text-center text-[13px] text-muted"
      >
        <Spinner />
        <p>{t("today.slow")}</p>
      </div>
    );
    return (
      <div className="mx-auto flex max-w-2xl flex-col gap-[22px] px-4 pt-3 pb-7">
        {/* key 跟下面正常版面的一樣：總覽讀到了換版面時，輸入框與套件卡片留著不重掛（打到一半焦點不掉）。 */}
        {/* 手機：跟正常版面同一個順序（等你處理 → 輸入框 → 套件卡片，DEV-156 U-04）。 */}
        {desktop ? null : status}
        <QuickInput key="quick" variant="today" />
        {notice}
        <Slot
          key="cards"
          id="core.overview.cards"
          className="flex flex-col gap-3"
          compactEmpty={!desktop}
        />
        {desktop ? status : null}
      </div>
    );
  }

  if (wide)
    return (
      <WideToday
        items={items}
        cards={shown}
        total={waitingTotal ?? data.waiting_total}
        now={now}
        filterEmpty={filtered && items.length === 0}
        threads={threads}
        fresh={fresh}
        reminders={reminders}
        unread={unreadCount}
        notice={notice}
      />
    );

  const nothing =
    filtered &&
    shown.length === 0 &&
    threads.length === 0 &&
    fresh.length === 0 &&
    reminders.length === 0;
  const hero = one ? (
    <AgentHero
      id={one}
      waiting={waitingTotal ?? 0}
      reminders={reminders.length}
      ids={picked}
      compact
    />
  ) : null;
  const waitingCards = shown.map((item, index) => (
    <WaitingCard key={item.key} item={item} now={now} index={index} />
  ));
  const todos =
    filtered && reminders.length === 0 ? null : <TodayReminders reminders={reminders} />;

  // 手機（DEV-156 U-04，作者 2026-11-14）：秘書頭像列（外殼）→ 等你處理 → 快速輸入 → 今天的待辦 → 套件卡片。
  // 要主人出手的事在第一屏（FEN-8）；沒有等你處理的事時那一塊不佔位置（頁首已經寫「沒有等你處理的事」）；
  // 宣告自己是空的套件卡片縮成一行（Slot 的 compactEmpty）。
  if (!desktop) {
    return (
      <div className="mx-auto flex max-w-2xl flex-col gap-[22px] px-4 pt-3 pb-7">
        {hero}
        {shown.length > 0 ? (
          <section data-testid="today-waiting-section" className="flex flex-col gap-2.5">
            <SectionHeading title={t("today.waiting")} count={waitingTotal} />
            {waitingCards}
          </section>
        ) : null}
        <WaitingThreads threads={threads} now={now} />
        <NewMessageThreads threads={fresh} />
        <QuickInput key="quick" variant="today" />
        {notice}
        {nothing ? <FilterEmpty variant="today" /> : todos}
        <Slot key="cards" id="core.overview.cards" className="flex flex-col gap-3" compactEmpty />
        {nothing ? null : <UnreadRow unread={unreadCount} />}
        {one ? <AgentTasks ids={picked} /> : null}
      </div>
    );
  }

  // 桌機窄版（768～1023）：一欄，順序照舊。
  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-[22px] px-4 pt-3 pb-7">
      <div className="pt-4">
        <h1 className="text-2xl font-bold tracking-[-0.01em]">{greetingText(now, userName)}</h1>
        <p className="text-[13px] text-muted">{todaySubtitle(now, waitingTotal)}</p>
      </div>
      {hero}
      <QuickInput key="quick" variant="today" />
      {notice}
      {/* 桌機：套件卡片（健康與習慣）在輸入框下面、等你處理前面（HAB-15）；手機的順序在上面（WEB-177）。 */}
      <Slot key="cards" id="core.overview.cards" className="flex flex-col gap-3" />
      {nothing ? (
        <FilterEmpty variant="today" />
      ) : (
        <>
          {filtered && shown.length === 0 ? null : (
            <section className="flex flex-col gap-2.5">
              <SectionHeading title={t("today.waiting")} count={waitingTotal} />
              {shown.length === 0 ? (
                <EmptyState title={t("today.waiting.empty")} hint={t("today.waiting.emptyHint")} />
              ) : (
                waitingCards
              )}
            </section>
          )}
          <WaitingThreads threads={threads} now={now} />
          <NewMessageThreads threads={fresh} />
          {todos}
          <UnreadRow unread={unreadCount} />
        </>
      )}
      {one ? <AgentTasks ids={picked} /> : null}
    </div>
  );
}
