import { Spinner } from "@evergenie/ui";
import { useParams } from "@tanstack/react-router";
import { useMemo } from "react";
import { SiteNotice } from "../../components/SiteNotice";
import { usePublishPageCounts } from "../../data/pageCounts";
import { type Site, siteUsable, useHomeParam, useHomeSite, useSites } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { useIsDesktop, useIsWide, useIsXWide } from "../../lib/media";
import { useAgentHotkeys } from "./agentHotkeys";
import { ChatGallery } from "./ChatGallery";
import { useChatPane } from "./chatPane";
import { chatCounts, findThread, useSiteThreads } from "./siteThreads";
import { ThreadList } from "./ThreadList";
import { ThreadNotFound, ThreadView } from "./ThreadView";

// /chat 與 /chat/$session 共用：≥ 1024px 兩欄（左 360 清單、右討論串）；比較窄時一次只顯示一個。
// ≥ 1280px 開著討論串時預設是「討論串｜右側角色欄」（清單收起來），標題欄的「對話清單」鈕切回兩欄（REQ-156）。
export function ChatPage() {
  const { session } = useParams({ strict: false }) as { session?: string };
  const wide = useIsWide();
  const desktop = useIsDesktop();
  const xwide = useIsXWide();
  const { pane, setPane } = useChatPane();
  // 這一串在哪台（?home=，S3-6）；沒寫＝主伺服器。
  const { site, pending } = useHomeSite();
  const all = useSiteThreads();
  const summary = session && site ? findThread(all, site.key, session) : undefined;
  // 桌機：⌥1–⌥9 直接換到第 N 位秘書。
  useAgentHotkeys(wide);

  // 秘書篩選的徽章＝這位「等你處理」的串數；「全部」下面是清單裡的串數（補充契約 S8 第 6 條）。連多台時加總每台。
  const loaded = all[0]?.data !== undefined;
  const { counts, total } = useMemo(() => chatCounts(all), [all]);
  // 討論串本身（手機）不顯示篩選，側欄也不跟著顯示件數。
  usePublishPageCounts(!loaded || (session && !wide) ? null : { counts, total });
  const selected = session && site ? { site: site.key, name: session } : undefined;
  const thread = (showBack: boolean, persona?: { open: boolean; onToggle: () => void }) =>
    session ? (
      <SiteThread
        key={`${site?.key ?? "?"}/${session}`}
        site={site}
        pending={pending}
        name={session}
        summary={summary}
        showBack={showBack}
        persona={persona}
      />
    ) : null;

  if (wide) {
    const personaMode = xwide && Boolean(session) && pane === "persona";
    return (
      <div className="flex h-full min-h-0">
        {personaMode ? null : (
          <section
            aria-label={t("chat.list")}
            className="flex w-[360px] shrink-0 flex-col border-r border-border bg-bg"
          >
            <h1 className="px-[18px] pt-[18px] pb-3 text-xl font-bold">{t("page.chat")}</h1>
            <nav
              aria-label={t("chat.list")}
              className="hg-scroll min-h-0 flex-1 overflow-y-auto px-2 pb-3"
            >
              <ThreadList selected={selected} />
            </nav>
          </section>
        )}
        <section className="flex min-w-0 flex-1 flex-col bg-surface">
          {session ? (
            thread(
              false,
              xwide
                ? {
                    open: personaMode,
                    onToggle: () => setPane(personaMode ? "list" : "persona"),
                  }
                : undefined,
            )
          ) : (
            <ChatGallery />
          )}
        </section>
      </div>
    );
  }
  if (session) return thread(true);
  return (
    <div className="px-2 pt-3 pb-6">
      {desktop ? <h1 className="px-2.5 pt-5 pb-3 text-xl font-bold">{t("page.chat")}</h1> : null}
      <nav aria-label={t("chat.list")}>
        <ThreadList />
      </nav>
    </div>
  );
}

// 打開的那一串在哪台：主伺服器直接開；別台要等試連（轉圈），不是 ok 就只在這裡寫原因（別台的對話不退回主伺服器開）。
function SiteThread({
  site,
  pending,
  name,
  summary,
  showBack,
  persona,
}: {
  site: Site | undefined;
  pending: boolean;
  name: string;
  summary: Parameters<typeof ThreadView>[0]["summary"];
  showBack: boolean;
  persona?: { open: boolean; onToggle: () => void };
}) {
  if (!site) {
    if (pending) {
      return (
        <div className="flex h-full items-center justify-center bg-bg">
          <Spinner label={t("app.loading")} />
        </div>
      );
    }
    return <NoSuchSite showBack={showBack} />;
  }
  if (!siteUsable(site)) {
    return (
      <ThreadNotFound showBack={showBack} title={null}>
        <SiteNotice site={site} className="max-w-md text-center" />
      </ThreadNotFound>
    );
  }
  return (
    <ThreadView name={name} home={site} summary={summary} showBack={showBack} persona={persona} />
  );
}

// ?home 對不到任何一台：多半是那台還沒連上過、不知道它的台名（這個分頁沒讀到過它的 get_mode，key 還是網址）。
// 還有這種台時寫它連不到，不說「設定裡沒有」；每台都知道台名還對不到，才是真的沒有這台。
function NoSuchSite({ showBack }: { showBack: boolean }) {
  const home = useHomeParam();
  const unnamed = useSites().filter((s) => !s.primary && !s.label && !siteUsable(s));
  const only = unnamed.length === 1 ? unnamed[0] : undefined;
  if (only) {
    return (
      <ThreadNotFound showBack={showBack} title={null}>
        <SiteNotice site={only} className="max-w-md text-center" />
      </ThreadNotFound>
    );
  }
  if (unnamed.length > 1) {
    return <ThreadNotFound showBack={showBack} hint={t("chat.site.unreached", { name: home })} />;
  }
  return <ThreadNotFound showBack={showBack} hint={t("chat.site.gone")} />;
}
