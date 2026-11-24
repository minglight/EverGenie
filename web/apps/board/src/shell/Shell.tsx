import type { Menu } from "@evergenie/sdk-web";
import { Button, cn, ErrorBoundary, ToastHost } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { Navigate, Outlet, useLocation, useMatches } from "@tanstack/react-router";
import { X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AgentFilterBar } from "../components/AgentFilterBar";
import { BackBar, useBackTrail } from "../components/BackLink";
import { sectionOf, sectionTitleOf } from "../components/SectionTabs";
import { useLiveUpdates } from "../data/live";
import { menuQuery, modeQuery, pagesQuery } from "../data/queries";
import { useOkSites, useViewedHome } from "../data/sites";
import { usePresenceReporter } from "../data/viewing";
import { t } from "../i18n/zh-TW";
import { useIsDesktop, useIsWide, useVisibleHeightVar } from "../lib/media";
import { useAfter } from "../lib/useAfter";
import { agentSettingsQuery, personaQuery } from "../pages/settings/queries";
import { PACKAGE_ROUTE, packagePageRoute } from "../pkg/routes";
import { BootPromptBanner } from "./BootPromptBanner";
import { basicMenu, MENU_GRACE_MS, MENU_RETRY_MS } from "./basicMenu";
import { CommandPalette } from "./CommandPalette";
import { ErrorPanel } from "./ErrorPanel";
import { FunnelBanner } from "./FunnelBanner";
import { flattenTree } from "./flatten";
import { DevStrip, homeTitle, useDevThemeColor, useHome } from "./HomeMark";
import { useHideOnScroll } from "./hideOnScroll";
import { useInternalLinks } from "./internalLinks";
import { LoadingScreen } from "./LoadingScreen";
import { MobileHeader } from "./MobileHeader";
import { NavHost } from "./NavHost";
import { NudgeToasts } from "./NudgeToasts";
import { OfflineBanner } from "./OfflineBanner";
import { useMergedMenu } from "./siteBadges";
import { Welcome } from "./Welcome";
import { WELCOME_PATH, welcomeGate, welcomeSkipped } from "./welcomeFlow";

const CHAT_ID = "core.chat";

// 根元件：先拿選單（web.get_menu）；拿到之前畫面只有全螢幕的載入畫面，不畫任何選單（REQ-105）。
// 例外（WEB-175）：等了 MENU_GRACE_MS 還沒到、或讀失敗，就先用基本選單把外殼畫出來——
// 一個請求卡住不能讓主人整個進不去；真的選單到了自動換回來，讀失敗的每 MENU_RETRY_MS 自己再試。
export function Shell() {
  const menu = useQuery({ ...menuQuery(), retry: 2 });
  // 通知的簡單／完整：外殼啟動就讀一次，設定頁的 useMode（pages/settings/mode.ts）共用同一份快取。選單不再依模式變（2026-10-28，Goal 一律顯示）。
  useQuery(modeQuery());
  useLiveUpdates();
  const waited = useAfter(MENU_GRACE_MS);
  const missing = menu.data === undefined;
  // 讀失敗過就記著：再試的那幾秒 React Query 會回到「讀取中」，外殼不能因此退回載入畫面、提示也不要來回換字。
  const [failedBefore, setFailedBefore] = useState(false);
  const failedNow = missing && menu.isError;
  useEffect(() => {
    if (failedNow) setFailedBefore(true);
    else if (!missing) setFailedBefore(false);
  }, [failedNow, missing]);
  const failed = failedNow || (missing && failedBefore);
  const refetch = menu.refetch;
  // 每失敗一次（errorUpdatedAt 變了）就排下一次自己再試。
  const failedAt = failedNow ? menu.errorUpdatedAt : 0;
  useEffect(() => {
    if (failedAt === 0) return;
    const timer = setTimeout(() => void refetch(), MENU_RETRY_MS);
    return () => clearTimeout(timer);
  }, [failedAt, refetch]);
  const fallback = useMemo(() => basicMenu(), []);
  // 新家的歡迎頁（WEB-176）：名冊（含停用的）一位秘書都沒有時，任何一頁都先到全螢幕的歡迎頁，不畫看板的選單與頁首。
  // 名冊還沒讀到時先等（跟選單同一段寬限），免得新家先閃一下看板；讀不到就照常畫看板，不把主人擋在外面。
  const roster = useQuery(agentSettingsQuery);
  const pathname = useLocation({ select: (l) => l.pathname });
  const rosterPending = roster.data === undefined && !roster.isError;
  const gate = welcomeGate(
    roster.data ? (roster.data.agents ?? []).length : undefined,
    pathname,
    welcomeSkipped(),
  );
  if (gate === "welcome") return <Welcome />;
  if (gate === "toWelcome") return <Navigate to={WELCOME_PATH} replace />;
  if (gate === "toToday" || (gate === "wait" && !rosterPending)) return <Navigate to="/" replace />;
  if (gate === "wait") return <LoadingScreen />;
  if (!waited && ((missing && !failed) || rosterPending)) return <LoadingScreen />;
  return (
    <ShellLayout
      menu={menu.data ?? fallback}
      basic={
        missing ? { failed, retrying: menu.isFetching, retry: () => void refetch() } : undefined
      }
    />
  );
}

/** 現在用的是基本選單（真的選單還沒到）：failed＝讀失敗了，可以按再試一次。 */
interface BasicMenuState {
  failed: boolean;
  retrying: boolean;
  retry: () => void;
}

function ShellLayout({ menu: own, basic }: { menu: Menu; basic?: BasicMenuState }) {
  // 看多台時核心項目的徽章加上別台的（S3-7a）；只有一台時就是 own。
  const menu = useMergedMenu(own);
  const desktop = useIsDesktop();
  const wide = useIsWide();
  useInternalLinks();
  useBackTrail();
  // 鍵盤打開時「最高 N% 畫面」的上限跟著看得到的高度變矮（WEB-128、D-59）。
  useVisibleHeightVar();
  const { pathname, searchStr } = useLocation();
  const matches = useMatches();
  const [paletteOpen, setPaletteOpen] = useState(false);
  const menuNotice = useMenuNotice(menu, searchStr);
  const pages = useQuery({
    ...pagesQuery(),
    enabled: matches.some((m) => m.routeId === PACKAGE_ROUTE),
  });
  const items = useMemo(() => flattenTree(menu.tree), [menu.tree]);
  // 不吵的通知（P7-6、D-34）：看板在前景時回報正在看哪一頁（討論串另外帶 session），伺服器據此決定提醒顯示在哪裡。
  // 多台（S3-8）：每台能用的台都報，正在看的那一筆（`?home=`）所屬的台拿真正的路徑與討論串。
  const thread = (matches.at(-1)?.params as { session?: string } | undefined)?.session;
  const viewed = useViewedHome();
  usePresenceReporter(pathname, thread, viewed, useOkSites());

  const titled = [...matches].reverse().find((m) => m.staticData?.title);
  // 星號網址裡打錯的字（例：/settings/打錯）是找不到頁：不用那條路由的標題（splatNotFound，DEV-156 U-12）。
  const splat = (titled?.params as { _splat?: string } | undefined)?._splat?.replace(/\/+$/, "");
  const staticData = titled?.staticData.splatNotFound && splat ? undefined : titled?.staticData;
  const titleKey = staticData?.title ?? "page.notFound";
  // 套件頁面的標題來自 web.get_pages（PageInfo.title），其他頁面是固定的字串。
  const pkgMatch = matches.find((m) => m.routeId === PACKAGE_ROUTE);
  const pkgParams = pkgMatch?.params as { pkg?: string; page?: string } | undefined;
  // 合成一頁的分頁（section，例：健康）用 section 的名稱，跟頁面裡的標題一致（REQ-235）。
  // 套件自己沒寫 section_title 時用同 section 裡寫了的那一個（例：衣櫃用核心「介紹」的「角色」，WEB-136）。
  const sectionTitle = sectionTitleOf(
    sectionOf(menu.pool, packagePageRoute(pkgParams?.pkg ?? "", pkgParams?.page ?? "")),
  );
  const pkgTitle =
    sectionTitle || pages.data?.find((p) => p.id === `${pkgParams?.pkg}.${pkgParams?.page}`)?.title;
  const titleText = pkgMatch ? (pkgTitle ?? t("page.package")) : t(titleKey);
  // 角色頁（/personas/<代號>）的瀏覽器分頁標題帶角色名（DEV-143）；頁首照舊寫「角色」，名字在頁面裡。
  const personaSlug = (
    matches.find((m) => m.routeId === "/personas/$slug")?.params as { slug?: string } | undefined
  )?.slug;
  const personaName = useQuery({
    ...personaQuery(personaSlug ?? ""),
    enabled: Boolean(personaSlug),
  }).data?.persona?.name;
  const docTitle = personaSlug && personaName ? `${personaName}｜${titleText}` : titleText;
  const fullPage = Boolean(staticData?.hideMobileBar);
  // 手機的秘書篩選列在外殼畫（不在 ErrorBoundary key={pathname} 裡），換分頁時是同一個節點，不重掛（REQ-155）。
  // 桌機（≥ 768）由側欄的「只看這位」負責，這裡不畫，免得平板寬度出現兩個篩選。
  const filterBar = staticData?.agentFilter ? <AgentFilterBar /> : null;
  const filterBarRef = useRef<HTMLDivElement>(null);
  const collapse = useHideOnScroll(pathname, filterBarRef);
  const chatWaiting = items.find((i) => i.id === CHAT_ID)?.badge ?? 0;
  const home = useHome();
  useDevThemeColor(home.dev);

  // 分頁標題：等你處理的串數 > 0 時加前綴「● 」；PWA 圖示徽章同一個數字（REQ-142）。開發家再加「開發｜」（WEB-76）。
  useEffect(() => {
    document.title = homeTitle(
      home.dev,
      `${chatWaiting > 0 ? "● " : ""}${docTitle} · ${t("app.title")}`,
    );
  }, [chatWaiting, docTitle, home.dev]);
  useEffect(() => {
    const nav = navigator as Navigator & {
      setAppBadge?: (n: number) => Promise<void>;
      clearAppBadge?: () => Promise<void>;
    };
    const run = chatWaiting > 0 ? nav.setAppBadge?.(chatWaiting) : nav.clearAppBadge?.();
    run?.catch(() => {});
  }, [chatWaiting]);

  // ⌘K／Ctrl+K（全域）開關指令面板。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((open) => !open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const openPalette = () => setPaletteOpen(true);
  const nav = (
    <NavHost
      menu={menu}
      desktop={desktop}
      pathname={pathname}
      onSearch={openPalette}
      forceDefault={menuNotice.forceDefault}
      onFailed={menuNotice.fail}
    />
  );
  const basicNotice = basic ? (
    <div
      role="status"
      data-testid="menu-basic-notice"
      className="flex shrink-0 items-center gap-2 border-b border-border bg-attention-soft px-4 py-2 text-[13px] text-attention-fg"
    >
      <p className="min-w-0 flex-1">
        {t(basic.failed && !basic.retrying ? "menu.basic.failed" : "menu.basic.waiting")}
      </p>
      {basic.failed ? (
        <Button
          variant="secondary"
          className="-my-1 min-h-11 shrink-0"
          disabled={basic.retrying}
          onClick={basic.retry}
        >
          {t("app.retry")}
        </Button>
      ) : null}
    </div>
  ) : null;
  const rendererNotice = menuNotice.reason ? (
    <div
      role="status"
      data-testid="menu-renderer-notice"
      className="flex shrink-0 items-center gap-2 border-b border-border bg-attention-soft px-4 py-2 text-[13px] text-attention-fg"
    >
      <p className="min-w-0 flex-1">{t("menu.renderer.failed", { reason: menuNotice.reason })}</p>
      <button
        type="button"
        aria-label={t("menu.renderer.dismiss")}
        onClick={menuNotice.dismiss}
        className="-my-2 -mr-3 flex size-11 shrink-0 items-center justify-center rounded-full hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus"
      >
        <X className="size-4" aria-hidden="true" />
      </button>
    </div>
  ) : null;
  const notice = (
    <>
      {basicNotice}
      {rendererNotice}
    </>
  );
  const page = (
    <ErrorBoundary
      key={pathname}
      fallback={() => (
        <>
          {/* 討論串的上一頁畫在它自己的標題欄，當掉時就沒了；手機連底部選單也沒有，這裡補一條。 */}
          {fullPage && !wide ? <BackBar /> : null}
          <ErrorPanel />
        </>
      )}
    >
      <Outlet />
    </ErrorBoundary>
  );

  return (
    <>
      {desktop ? (
        <div className="flex h-dvh flex-col bg-bg text-fg">
          <DevStrip />
          <div className="flex min-h-0 flex-1">
            {nav}
            <div className="flex min-h-0 min-w-0 flex-1 flex-col">
              <OfflineBanner />
              <BootPromptBanner />
              <FunnelBanner />
              {notice}
              {/* 討論串在兩欄以下有自己的返回鈕（標題欄），不再畫一條。 */}
              {fullPage && !wide ? null : <BackBar />}
              <main className="relative min-h-0 min-w-0 flex-1 overflow-y-auto">{page}</main>
            </div>
          </div>
        </div>
      ) : (
        <div className="flex h-dvh flex-col bg-bg text-fg">
          <DevStrip />
          {fullPage ? null : (
            <MobileHeader
              title={staticData?.mobileTitle ? t(staticData.mobileTitle) : titleText}
              kind={staticData?.header}
              onSearch={openPalette}
            />
          )}
          <OfflineBanner />
          <BootPromptBanner />
          <FunnelBanner />
          {notice}
          {filterBar ? (
            <div
              data-testid="filter-bar-wrap"
              data-hidden={collapse.hidden || undefined}
              className={cn(
                "grid shrink-0 transition-[grid-template-rows] duration-200 motion-reduce:transition-none",
                collapse.hidden ? "grid-rows-[0fr]" : "grid-rows-[1fr]",
              )}
            >
              <div
                ref={filterBarRef}
                className="min-h-0 overflow-hidden"
                inert={collapse.hidden || undefined}
              >
                {filterBar}
              </div>
            </div>
          ) : null}
          {/* relative：頁面裡的 absolute（例如 sr-only 的檔案欄位）以 main 為準，不會把整頁撐出捲動（P6 收尾）。 */}
          <main
            className="relative min-h-0 flex-1 overflow-y-auto"
            onScroll={filterBar ? collapse.onScroll : undefined}
          >
            {page}
          </main>
          {fullPage ? null : nav}
        </div>
      )}
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} menuItems={items} />
      <ToastHost desktop={desktop} />
      <NudgeToasts pathname={pathname} viewed={viewed} />
    </>
  );
}

// 自訂選單元件失敗時的提醒（REQ-140）：伺服器說編不過／找不到（renderer_error），或瀏覽器端載入、執行失敗。
// 網址有 ?menu=default 就一律用預設、不提醒，而且這次開著期間一直算（換頁網址丟掉參數也一樣）。
function useMenuNotice(menu: Menu, searchStr: string) {
  const inUrl = new URLSearchParams(searchStr).get("menu") === "default";
  const [sticky, setSticky] = useState(inUrl);
  useEffect(() => {
    if (inUrl) setSticky(true);
  }, [inUrl]);
  const forceDefault = inUrl || sticky;
  // 失敗原因綁著當時的網址：換了自訂元件（改了套件、換了 renderer）舊的原因就作廢。
  const bundleUrl = menu.renderer_bundle_url;
  const [failure, setFailure] = useState<{ url?: string; reason: string } | null>(null);
  const [dismissed, setDismissed] = useState<string | null>(null);
  const fail = useCallback((reason: string) => setFailure({ url: bundleUrl, reason }), [bundleUrl]);
  const reason = forceDefault
    ? null
    : (menu.renderer_error ?? (failure?.url === bundleUrl ? failure?.reason : null) ?? null);
  return {
    forceDefault,
    fail,
    reason: reason && reason !== dismissed ? reason : null,
    dismiss: () => setDismissed(reason),
  };
}
