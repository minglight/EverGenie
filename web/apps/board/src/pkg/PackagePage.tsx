import type { PageInfo } from "@evergenie/sdk-web";
import { Spinner } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { useLocation, useParams } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { PageAgentBanner } from "../components/PageAgentBanner";
import { PageTabs, tabClass } from "../components/PageTabs";
import { sectionHome, sectionOf, sectionTitleOf } from "../components/SectionTabs";
import { SectionToggles } from "../components/SectionToggles";
import { useAgents } from "../data/agents";
import { menuQuery, pagesQuery } from "../data/queries";
import { t } from "../i18n/zh-TW";
import { useIsDesktop, useIsXWide } from "../lib/media";
import { PageChat } from "../pages/chat/PageChat";
import { ErrorPanel } from "../shell/ErrorPanel";
import { NotFoundPage } from "../shell/NotFound";
import { useBundle } from "./loadBundle";
import { PackageBoundary } from "./PackageBoundary";
import { packagePageRoute } from "./routes";
import { TierBadge } from "./TierBadge";

// 套件頁面的路由 /p/$pkg/$page：標題用 PageInfo.title，內容是編譯好的 default export。
export function PackagePage() {
  const { pkg, page } = useParams({ strict: false }) as { pkg: string; page: string };
  const pages = useQuery(pagesQuery());
  if (pages.data === undefined) {
    if (pages.isError) return <ErrorPanel />;
    return (
      <div className="flex justify-center py-16">
        <Spinner label={t("app.loading")} />
      </div>
    );
  }
  const info = pages.data.find((p) => p.id === `${pkg}.${page}`);
  if (!info || info.slot) return <NotFoundPage />;
  if (info.error) return <ErrorPanel message={info.error} />;
  return <Loaded pkg={pkg} page={page} info={info} />;
}

function Loaded({ pkg, page, info }: { pkg: string; page: string; info: PageInfo }) {
  const desktop = useIsDesktop();
  const xwide = useIsXWide();
  const menu = useQuery(menuQuery());
  const { all: list } = useAgents();
  const route = packagePageRoute(pkg, page);
  // 同一頁換查詢（?id=1 → ?id=2）走前端路由後不會重載（FEN-29）：每筆歷史連錯誤畫面一起重掛，跟以前整頁重載一樣從新的網址開始；
  // 套件自己用 replaceState 記分頁、日期時要帶著 history.state，同一筆歷史不重掛。
  const entry = useLocation({ select: (l) => l.state.__TSR_key });
  const siblings = sectionOf(menu.data?.pool, route);
  const section = siblings[0]?.section ?? "";
  const tabbed = siblings.length > 1;
  const pageTitle = info.title ?? page;
  // 同一個 section 的頁（例：飲食、運動、身體；核心的介紹＋衣櫃、展示間）是同一頁的分頁：標題用 section 的名稱，上方是分頁（核心在前）。
  const title = section ? sectionTitleOf(siblings) || pageTitle : pageTitle;
  const chatKey = section || `${pkg}.${page}`;
  // 頁面宣告的負責秘書在名冊上而且啟用：頂端放她的橫幅、聊天固定跟她（REQ-236）；不然照舊選人（REQ-235）。
  const owner = info.chat_agent ?? "";
  const fixed = info.chat && list.some((a) => a.id === owner && a.enabled) ? owner : "";
  const sideChat = Boolean(info.chat) && xwide;
  const chatTab = Boolean(info.chat) && !xwide;
  // 窄螢幕的「聊天」分頁：換頁或變寬就回到記錄。
  const [chatting, setChatting] = useState(false);
  // biome-ignore lint/correctness/useExhaustiveDependencies: 換頁時才重來
  useEffect(() => setChatting(false), [route]);
  const showChat = chatTab && chatting;

  const tabs =
    tabbed || chatTab || section ? (
      <PageTabs
        label={t("pageTabs.label", { title })}
        tabs={(tabbed ? siblings : [{ id: route, route, title: pageTitle }]).map((it) => ({
          key: it.id ?? "",
          to: it.route ?? "",
          label: it.title ?? "",
          current: it.route === route && !showChat,
        }))}
        onPick={() => setChatting(false)}
        trailing={
          section ? (
            <SectionToggles
              key={route}
              section={section}
              title={title}
              current={pkg}
              home={sectionHome(siblings)}
            />
          ) : undefined
        }
      >
        {chatTab ? (
          <button
            type="button"
            data-testid="page-chat-tab"
            aria-pressed={showChat}
            onClick={() => setChatting(true)}
            className={tabClass(showChat)}
          >
            {t("pageChat.tab")}
          </button>
        ) : null}
      </PageTabs>
    ) : null;

  const content = (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 px-4 pt-3 pb-7">
      {/* 標題旁標「我的」「分享自 <來源>」，官方不標（REQ-350）；手機的標題在外殼，標籤單獨一行。 */}
      {desktop ? (
        <div className="flex flex-wrap items-center gap-2 pt-4">
          <h1 className="text-2xl font-bold tracking-[-0.01em]">{title}</h1>
          <TierBadge tier={info.tier} source={info.source} />
        </div>
      ) : info.tier === "mine" || info.tier === "installed" ? (
        <div>
          <TierBadge tier={info.tier} source={info.source} />
        </div>
      ) : null}
      {fixed && !showChat ? <PageAgentBanner id={fixed} /> : null}
      {tabs}
      {showChat ? (
        <PageChat
          variant="inline"
          chatKey={chatKey}
          fallback={owner}
          fixed={fixed || undefined}
          className="h-[calc(100dvh-210px)] min-h-[420px]"
        />
      ) : null}
      {/* 切到聊天時頁面只藏起來不卸載，填到一半的表單還在。 */}
      <div hidden={showChat}>
        <PackageBoundary key={entry} title={pageTitle}>
          <Body pkg={pkg} page={page} url={info.bundle_url ?? ""} />
        </PackageBoundary>
      </div>
    </div>
  );
  // 寬螢幕（≥ 1280）而且頁面宣告了 chat：右邊放一欄聊天，邊看邊跟秘書說。
  if (!sideChat) return content;
  return (
    <div className="flex h-full min-h-0">
      <div className="min-w-0 flex-1 overflow-y-auto">{content}</div>
      <PageChat key={chatKey} chatKey={chatKey} fallback={owner} fixed={fixed || undefined} />
    </div>
  );
}

function Body({ pkg, page, url }: { pkg: string; page: string; url: string }) {
  const { Component, error } = useBundle(url);
  if (error) throw error;
  if (!Component) {
    return (
      <div className="flex justify-center py-16">
        <Spinner label={t("app.loading")} />
      </div>
    );
  }
  return <Component pkg={pkg} page={page} search={window.location.search} />;
}
