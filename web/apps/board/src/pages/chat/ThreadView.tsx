import type { Pending, Session, ThreadSummary } from "@evergenie/sdk-web";
import { Avatar, cn, EmptyState, Markdown, Spinner } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowDown, ChevronDown, ChevronLeft, Terminal } from "lucide-react";
import {
  type ReactNode,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { BackLink } from "../../components/BackLink";
import { NudgeCard } from "../../components/NudgeCard";
import { PageTitle } from "../../components/PageTitle";
import { useMultiSite } from "../../components/SiteTag";
import { type AgentView, presenceOf, useAgents } from "../../data/agents";
import { threadsQuery } from "../../data/queries";
import { apiFor, type Site, useSites } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { useToolDetails } from "../../lib/chatDisplay";
import { timeMark } from "../../lib/format";
import { useIsDesktop } from "../../lib/media";
import { useNow } from "../../lib/now";
import { needsReply, statusTone } from "../../lib/statusTone";
import { useFilterFollowsChat } from "./filterFollow";
import {
  avatarKeys,
  buildFlow,
  dropReceived,
  type FlowItem,
  type LocalNote,
  mergePending,
  type QueuedMessage,
  type ToolUse,
  withoutTools,
  withTimeMarks,
} from "./flow";
import { modelLine } from "./modelLine";
import { type Answered, PendingCard } from "./PendingCard";
import { PersonaPane } from "./PersonaPane";
import { QueuedBubble, UserText } from "./QueuedBubble";
import { RestartBar } from "./RestartBar";
import { SiteTabs } from "./SiteTabs";
import { StopBar } from "./StopBar";
import { TaskBanner } from "./TaskBanner";
import { ThreadComposer } from "./ThreadComposer";
import { ThreadHeader } from "./ThreadHeader";
import {
  CACHE_TICK_MS,
  cacheLeftMinutes,
  cacheView,
  isWork,
  limitedText,
  type StatusView,
  showTypingDots,
  statusView,
} from "./threads";
import {
  type ChatData,
  useChatData,
  useMarkSeen,
  useSessionLive,
  useWakeOnOpen,
} from "./useThreadData";
import { WorkAsk } from "./WorkAsk";

const NEAR_BOTTOM_PX = 200;
// 手機：往上捲離底部超過這個距離，頂端的角色區收成小頭像（REQ-156）。
const COLLAPSE_PX = 40;
// 捲到等你回答的卡片時，卡片上面留一點訊息區的空間。
const CARD_TOP_GAP_PX = 12;

export interface ThreadViewProps {
  name: string;
  /** 清單裡這一串的摘要（狀態、活動）；沒有就用 session 自己的狀態。 */
  summary?: ThreadSummary;
  /** 有返回鈕（手機與 < 1024px）；兩欄版面（≥ 1024px）不需要。同時決定頂端是不是角色影片。 */
  showBack: boolean;
  /** 桌機 ≥ 1280：標題欄多一顆「對話清單」鈕；open 時右側是角色欄（REQ-156）。 */
  persona?: { open: boolean; onToggle: () => void };
  /** 不要標題欄：嵌在套件頁、旁邊已經有這位秘書的橫幅與小標題（REQ-236）。 */
  bare?: boolean;
  /**
   * 這一串在哪台（S3-6）：讀寫、上傳、頭像與附件網址都對那台；不給＝主伺服器（套件頁旁邊的聊天只看主伺服器）。
   * 呼叫端要先確認那台能用（主伺服器或 state＝ok），這裡不再檢查、也不退回主伺服器。
   */
  home?: Site;
}

// 討論串：標題欄、訊息（最後是等你回答的卡片）、底部固定區（輸入框）。
export function ThreadView({ name, summary, showBack, persona, bare, home }: ThreadViewProps) {
  const sites = useSites();
  const site = home ?? (sites[0] as Site);
  const multi = useMultiSite();
  const { session, notFound, refresh } = useSessionLive(name, site);
  const waking = useWakeOnOpen(name, session, site);
  const chat = useChatData(name, site);
  // 這次畫面送的＋伺服器記得的（退出再進來、送失敗、中途補話），REQ-302。
  const bubbles = useMemo(
    () => dropReceived(mergePending(chat.queued, session?.messages), chat.messages),
    [chat.queued, session?.messages, chat.messages],
  );
  // 剛送出的話要讓伺服器的狀態跟上（她在忙時狀態不會變，不會有推送）。
  const sentKey = chat.queued.map((q) => `${q.key}:${q.status}`).join(",");
  useEffect(() => {
    if (sentKey) refresh();
  }, [sentKey, refresh]);
  useMarkSeen(name, chat.messages, site);
  const { view } = useAgents(site);
  // 工作 session 要倒數；你的話在排隊時 10 秒更新一次，排太久標題才會寫「她那邊沒回應」（WEB-175）。
  const now = useNow(
    summary && isWork(summary) ? CACHE_TICK_MS : summary?.status === "queued" ? 10_000 : 0,
  );
  const agent = view(summary?.agent ?? session?.agent ?? name);
  const { data: allThreads } = useQuery(threadsQuery(site));
  const presence = presenceOf(allThreads ?? [], agent.id, site.key).presence;
  // 篩選管全部：對話跟篩選對上（WEB-19）。Goal 與套件頁面旁邊的聊天欄（bare）不算。
  useFilterFollowsChat(bare ? undefined : (summary?.agent ?? session?.agent), site.key);
  // 手機的角色區：預設展開；往上捲或聚焦輸入框就收合，點收合列再展開。換串（key）時重來。
  const [collapsed, setCollapsed] = useState(false);
  // 「再說一次」放回輸入框的那句（n 讓同一句按兩次也會再放一次）。
  const [retype, setRetype] = useState<{ text: string; n: number } | null>(null);

  const [notes, setNotes] = useState<LocalNote[]>([]);
  const [dismissed, setDismissed] = useState<string>("");
  const lastUuid = chat.messages[chat.messages.length - 1]?.uuid ?? "";
  const lastRef = useRef(lastUuid);
  const submitAnchor = useRef("");
  lastRef.current = lastUuid;
  const [answered, setAnswered] = useState<(Answered & { anchor: string }) | null>(null);
  // 送出中、或「已經在別處回答了」還在顯示的那張卡：就算 session.watch 已經說 pending 沒了也先留著。
  const [held, setHeld] = useState<Pending | null>(null);
  const pending = session?.pending;

  // 換串：本地的膠囊清掉。
  // biome-ignore lint/correctness/useExhaustiveDependencies: 換串才重來
  useEffect(() => {
    setNotes([]);
    setDismissed("");
    setAnswered(null);
    setHeld(null);
    setRetype(null);
  }, [name]);

  // session.watch 收到 pending 消失後卡片收起，在訊息流最後加一個狀態小膠囊（只在這次畫面裡）。
  useEffect(() => {
    if (!answered || pending?.id === answered.pendingId) return;
    setAnswered(null);
    const text = answered.note;
    if (text) {
      setNotes((all) => [
        ...all,
        { id: `${answered.pendingId}-${all.length}`, anchor: answered.anchor, text },
      ]);
    }
  }, [answered, pending?.id]);

  const onAnswered = useCallback((a: Answered) => {
    if (a.note === null) {
      setDismissed(a.pendingId);
      return;
    }
    setAnswered({ ...a, anchor: submitAnchor.current });
  }, []);

  // 指令做成了（WEB-83）：訊息流最後加一行「你下了指令 /clear」（只在這次畫面裡；伺服器另記 session.last_command）。
  const onCommand = useCallback(
    (label: string) => {
      setNotes((all) => [
        ...all,
        { id: `cmd-${all.length}`, anchor: lastRef.current, text: t("slash.note", { cmd: label }) },
      ]);
      refresh(); // 換了模型、力度：標題欄馬上跟上（推送也會到，這是保險，WEB-84）
    },
    [refresh],
  );
  // 標題欄的「現在用的模型 · 思考力度」：點了在輸入框打開「/model」選單（WEB-84）。
  const line = modelLine(session);
  const pickModel = useCallback(() => {
    setRetype((r) => ({ text: "/model ", n: (r?.n ?? 0) + 1 }));
    document.getElementById("hg-composer")?.focus();
  }, []);

  // 膠囊排在「按下去那一刻」的最後一則之後：秘書接著說的話（可能比送出的回應還早到）會排在膠囊後面。
  const onHold = useCallback((p: Pending | null) => {
    if (p) submitAnchor.current = lastRef.current;
    setHeld(p);
  }, []);

  // 「重開對話」說明列（DEV-106）：標題欄的鈕開關它；換串就收起來。
  const [restartOpen, setRestartOpen] = useState(false);
  const restartBusy = useRef(false); // 重開進行中：標題欄的鈕不收這一列，免得重開兩次
  // biome-ignore lint/correctness/useExhaustiveDependencies: 換串（name 變了）才收
  useEffect(() => setRestartOpen(false), [name]);

  const toolDetails = useToolDetails();
  if (notFound && !summary) return <ThreadNotFound showBack={showBack} />;

  const status = summary
    ? headerStatus(summary, now, toolDetails)
    : { text: fallbackStatus(session), tone: statusTone(session?.state) };
  // 清單裡還沒有這串的摘要（剛開、或清單還沒抓到）時，狀態直接看 session 自己的。
  const working = summary ? summary.status === "working" : session?.state === "busy";
  // 橘點只給真的在等你（排隊等名額不算，WEB-70）。
  const waiting = needsReply(summary) || Boolean(pending);
  const card = pending && pending.id !== dismissed ? pending : held;
  const typing = showTypingDots({
    working,
    toolDetails,
    lastIsUser: lastMessageIsUser(chat),
    awaitingAnswer: waiting || Boolean(card),
  });
  const taskId = session?.task || summary?.task || "";

  const main = (
    <div
      className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-bg"
      data-thread-status={summary?.status ?? "idle"}
    >
      {bare ? null : (
        <ThreadHeader
          agent={agent}
          site={multi ? site : undefined}
          presence={presence}
          status={status}
          working={working}
          waiting={waiting}
          showBack={showBack}
          hero={showBack}
          collapsed={collapsed}
          onExpand={() => setCollapsed(false)}
          paneToggle={persona ? { pressed: !persona.open, onToggle: persona.onToggle } : undefined}
          model={line ? { line, onPick: pickModel } : undefined}
          restart={
            session
              ? {
                  open: restartOpen,
                  onToggle: () => {
                    if (!restartBusy.current) setRestartOpen((o) => !o);
                  },
                  agentName: agent.name,
                }
              : undefined
          }
        />
      )}
      {!bare && restartOpen && session ? (
        <RestartBar
          key={name}
          api={apiFor(site)}
          session={session}
          agentName={agent.name}
          onClose={() => setRestartOpen(false)}
          onDone={refresh}
          onBusy={(b) => {
            restartBusy.current = b;
          }}
        />
      ) : null}
      {bare ? null : <SiteTabs agentId={agent.id} site={site} />}
      {taskId ? <TaskBanner id={taskId} site={site} /> : null}
      <Messages
        chat={chat}
        site={site}
        pending={bubbles}
        notes={notes}
        agent={agent}
        typing={typing}
        onAway={showBack ? () => setCollapsed(true) : undefined}
        onSayAgain={(text) => setRetype((r) => ({ text, n: (r?.n ?? 0) + 1 }))}
        cardKey={card?.id ?? ""}
        card={
          card ? (
            <PendingCard
              api={apiFor(site)}
              session={name}
              pending={card}
              agentName={agent.name}
              onHold={onHold}
              onAnswered={onAnswered}
            />
          ) : null
        }
      />
      {/* biome-ignore lint/a11y/noStaticElementInteractions: 只是接住輸入框冒泡上來的 focus，這個區塊本身不是控制項 */}
      <div
        className="flex shrink-0 flex-col gap-2 border-t border-border bg-bg px-3 pt-2 pb-[max(env(safe-area-inset-bottom,0px),8px)] lg:pb-3.5"
        onFocus={(e) => {
          // 鍵盤要空間：聚焦輸入框就把角色區收起來。
          if (showBack && e.target instanceof HTMLTextAreaElement) setCollapsed(true);
        }}
      >
        {!card &&
        summary?.status === "waiting_user" &&
        (summary.waiting ?? []).some((w) => w !== "work_queue") ? (
          <p
            role="status"
            className="rounded-2xl bg-attention-soft px-3.5 py-2.5 text-[13px] text-attention-fg"
          >
            {t("chat.waiting.nocard", {
              reasons: (summary.waiting ?? [])
                .map((w) => t(`waiting.${w === "work_queue" ? "workQueue" : w}` as never))
                .join("、"),
            })}
          </p>
        ) : null}
        {/* 「工作 session 超過上限」的提問讀那台的通知、回答送那台（S3-7b）；
            「還在等你」的提醒每台各自回報，卡片只畫這一串那台的，「知道了」送那台（S3-8）。 */}
        <WorkAsk session={name} site={site} />
        <NudgeCard session={name} site={site.key} />
        {waking ? (
          <p
            role="status"
            data-testid="waking"
            className="rounded-xl bg-surface-2 px-3.5 py-2.5 text-[13px] leading-snug text-muted"
          >
            {t("chat.waking", { name: agent.name })}
          </p>
        ) : null}
        {/* 只看她自己的狀態：討論串的「處理中」也算封包還掛著的時候，那時沒有東西可停。 */}
        {session?.state === "busy" && !card && !waiting ? (
          <StopBar api={apiFor(site)} session={name} agentName={agent.name} />
        ) : null}
        <ThreadComposer
          key={name}
          name={site.primary ? name : `${site.key}/${name}`}
          session={name}
          api={apiFor(site)}
          base={site.base}
          agentName={agent.name}
          onSend={chat.send}
          onCommand={onCommand}
          retype={retype}
        />
      </div>
    </div>
  );
  if (!persona?.open) return main;
  return (
    <div className="flex h-full min-h-0">
      {main}
      <PersonaPane agentId={agent.id} site={site} waitingHere={Boolean(card)} />
    </div>
  );
}

// 標題欄的狀態字：跟清單那一列同一個字（要你同意／要你回答，DEV-156 U-10）。
function headerStatus(summary: ThreadSummary, now: Date, toolDetails: boolean): StatusView {
  const base = statusView(summary, now, toolDetails);
  // 工作 session：狀態後面接 hot cache 倒數（上一輪結束起 60 分鐘，快過期換提醒的說法；D-34）。
  const left = isWork(summary) ? cacheLeftMinutes(summary, now) : null;
  if (left === null) return base;
  const cache = cacheView(left);
  return {
    text: `${base.text || t("chat.status.idle")} · ${cache.text}`,
    // 快過期只換說法，不換成橘色（橘只給等你處理，WEB-70）。
    tone: base.tone,
  };
}

// 清單還沒有摘要時看 session 自己的狀態；不認得的（含舊版那台送來的 stuck）不寫字、灰色（S5）。
function fallbackStatus(session: Session | undefined): string {
  if (!session) return "";
  if (session.state === "stopped") return t("chat.row.stopped");
  if (session.state === "starting") return t("chat.row.starting");
  if (session.state === "limited") return limitedText(session.limited_until);
  if (session.state === "busy") return t("chat.row.working");
  if (session.state === "waiting") return t("chat.status.waiting");
  if (session.state === "idle") return t("chat.status.idle");
  return "";
}

function lastMessageIsUser(chat: ChatData): boolean {
  const last = [...chat.messages]
    .reverse()
    .find((m) => m.kind !== "tool_result" && m.kind !== "tool_call");
  return last?.kind === "user" || last?.kind === "notice";
}

/** 找不到這一串；別台不能用時也用這個版面（title 給 null、children 放那台的原因）。 */
export function ThreadNotFound({
  showBack,
  title = t("chat.notFound"),
  hint = t("chat.notFoundHint"),
  children,
}: {
  showBack: boolean;
  title?: string | null;
  hint?: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex h-full flex-col bg-bg">
      {showBack ? (
        <div className="flex h-[60px] shrink-0 items-center border-b border-border bg-surface pl-1">
          <BackLink className="flex size-11 items-center justify-center text-fg">
            <ChevronLeft className="size-6" strokeWidth={1.8} aria-hidden="true" />
          </BackLink>
        </div>
      ) : null}
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6">
        {title ? <PageTitle className="text-lg font-bold">{title}</PageTitle> : null}
        {children ?? <p className="text-sm text-muted">{hint}</p>}
        <Link
          to="/chat"
          replace
          className="flex h-11 items-center rounded-xl bg-accent px-5 text-[15px] font-semibold text-accent-fg"
        >
          {t("chat.notFoundBack")}
        </Link>
      </div>
    </div>
  );
}

function Messages({
  chat,
  site,
  pending,
  notes,
  agent,
  typing,
  onAway,
  onSayAgain,
  card,
  cardKey,
}: {
  chat: ChatData;
  /** 這一串在哪台：附件（/share/…）的網址、使用者泡泡上「誰說的」都照那台。 */
  site: Site;
  pending: QueuedMessage[];
  /** 這一輪沒做完的那句按「再說一次」：放回輸入框讓主人自己決定要不要送（不自動重送，REQ-371）。 */
  onSayAgain: (text: string) => void;
  notes: LocalNote[];
  agent: AgentView;
  typing: boolean;
  /** 使用者往上捲、離底部超過 COLLAPSE_PX（手機的角色區收合）。 */
  onAway?: () => void;
  /** 等你回答的卡片：排在訊息流最後，跟對話一起滑（WEB-157）。 */
  card: ReactNode;
  /** 這張卡片是哪一張：換了或收起來就回到自動捲。 */
  cardKey: string;
}) {
  const agentName = agent.name;
  const now = useNow();
  const toolDetails = useToolDetails();
  const flow = useMemo(() => {
    const items = buildFlow(chat.messages, notes, t("chat.interrupted"), (label) =>
      t("chat.routine", { label }),
    );
    return withTimeMarks(toolDetails ? items : withoutTools(items));
  }, [chat.messages, notes, toolDetails]);
  const withAvatar = useMemo(() => avatarKeys(flow), [flow]);
  const desktop = useIsDesktop();
  const avatarSize = desktop ? 32 : 30;
  const scroller = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  // 按了「↓ 新訊息」後平滑捲動的途中，捲動事件會誤判成「使用者往上捲」；這段時間不取消自動捲到底。
  const jumping = useRef(false);
  const [away, setAway] = useState(false);
  // 用各段的數量組成一個鍵，不用加總：「輸入中」換成正式回覆時總數不變，加總會漏捲（長回覆底部被切掉）。
  const shape = `${flow.length}/${pending.length}/${typing ? 1 : 0}/${chat.draft.length}/${cardKey}`;

  const toBottom = useCallback((smooth = false) => {
    const el = scroller.current;
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior: smooth ? "smooth" : "auto" });
  }, []);

  const placedCard = useRef("");
  const cardTop = useRef(0);

  // 新訊息進來、或打開時，捲到最底；使用者自己往上捲超過 200px 時不自動捲，改顯示「↓ 新訊息」。
  // 有卡片（WEB-157）：新卡片不管剛才捲到哪都定位到卡片開頭（比訊息區矮時 scrollTo 會停在底部，整張都看得到）；
  // 之後卡片上面多了訊息，卡片留在畫面原位，免得主人正要按的選項被換掉；回答完回到自動捲。
  // biome-ignore lint/correctness/useExhaustiveDependencies: shape 變了才處理（shape 含 cardKey）
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const changed = placedCard.current !== cardKey;
    placedCard.current = cardKey;
    if (changed) {
      stick.current = true;
      setAway(false);
    }
    const pendingCard = el.querySelector<HTMLElement>("[data-pending-card]");
    if (pendingCard) {
      if (changed) el.scrollTo({ top: pendingCard.offsetTop - CARD_TOP_GAP_PX });
      else el.scrollTop += pendingCard.offsetTop - cardTop.current;
      cardTop.current = pendingCard.offsetTop;
      return;
    }
    if (stick.current) toBottom();
    else setAway(true);
  }, [shape, toBottom]);

  // 上面的角色區收合或展開時，這個區域的高度會變：原本貼在底部就繼續貼著，不跳。
  useEffect(() => {
    const el = scroller.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => {
      if (!stick.current) return;
      // 有卡片：放得下才貼底（按鈕不被切掉）；比訊息區高、或主人正在卡片裡打字（鍵盤開合也會改高度）就不動。
      const pendingCard = el.querySelector<HTMLElement>("[data-pending-card]");
      if (
        pendingCard &&
        (pendingCard.offsetHeight > el.clientHeight || pendingCard.contains(document.activeElement))
      )
        return;
      toBottom();
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, [toBottom]);

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const gap = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (gap > COLLAPSE_PX) onAway?.();
    const far = gap > NEAR_BOTTOM_PX;
    if (far && jumping.current) return;
    jumping.current = false;
    stick.current = !far;
    if (!far) setAway(false);
  };

  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: 只是接住卡片輸入格冒泡上來的 focus，這個區塊本身不是控制項
    <div
      className="relative min-h-0 flex-1"
      onFocus={(e) => {
        // 鍵盤要空間：在卡片裡打字也把角色區收起來（跟輸入框一樣）。
        if (e.target instanceof HTMLTextAreaElement || e.target instanceof HTMLInputElement)
          onAway?.();
      }}
    >
      <div
        ref={scroller}
        onScroll={onScroll}
        role="log"
        aria-label={t("chat.messages")}
        // 有卡片時卡片的位置由上面的 layout effect 自己顧；瀏覽器的捲動錨點（iOS 沒有）再調一次就會多移一次。
        className={cn(
          "hg-scroll flex h-full flex-col gap-3 overflow-y-auto px-4 pt-4 pb-3",
          card ? "[overflow-anchor:none]" : "",
        )}
      >
        {!chat.ready ? (
          <div className="flex flex-1 items-center justify-center">
            <Spinner label={t("chat.loading")} />
          </div>
        ) : flow.length === 0 && pending.length === 0 ? (
          <div className="flex flex-1 items-center justify-center">
            <EmptyState
              variant="plain"
              title={t("chat.empty")}
              hint={t("chat.emptyHint", { name: agentName })}
            />
          </div>
        ) : null}
        {flow.map((item) => (
          <FlowRow
            key={item.key}
            item={item}
            agent={agent}
            avatarSize={avatarSize}
            avatar={withAvatar.has(item.key)}
            now={now}
            site={site}
          />
        ))}
        {pending.map((q) => (
          <QueuedBubble
            key={q.key}
            item={q}
            agentName={agentName}
            shareBase={site.base}
            onResend={() => (q.fromServer ? chat.send(q.text) : chat.resend(q.key))}
            onSayAgain={() => onSayAgain(q.text)}
          />
        ))}
        {typing ? <TypingDots agent={agent} size={avatarSize} text={chat.draft} /> : null}
        {card}
      </div>
      {away ? (
        <button
          type="button"
          onClick={() => {
            stick.current = true;
            jumping.current = true;
            window.setTimeout(() => {
              jumping.current = false;
            }, 1500);
            setAway(false);
            toBottom(true);
          }}
          aria-label={t("chat.newMessagesJump")}
          className="absolute right-4 bottom-3 flex h-10 items-center gap-1.5 rounded-full bg-accent px-3.5 text-[13px] font-semibold text-accent-fg shadow-[var(--shadow-pop)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
        >
          <ArrowDown className="size-4" strokeWidth={2} aria-hidden="true" />
          {t("chat.newMessages")}
        </button>
      ) : null}
    </div>
  );
}

// 靜態的時間分隔線（REQ-157）：要放文字所以不能用 <hr>，也不是可聚焦的分隔條，所以不帶 aria-valuenow。
const timeSeparator = (label: string) => ({ role: "separator", "aria-label": label }) as const;

function FlowRow({
  item,
  agent,
  avatarSize,
  avatar,
  now,
  site,
}: {
  item: FlowItem;
  agent: AgentView;
  avatarSize: number;
  avatar: boolean;
  now: Date;
  site: Site;
}) {
  switch (item.type) {
    case "time": {
      const label = timeMark(item.at, now);
      return (
        <div
          {...timeSeparator(label)}
          className="self-center py-1.5 text-center text-[11px] text-muted"
        >
          {label}
        </div>
      );
    }
    case "user":
      return <UserBubble text={item.text} site={site} />;
    case "assistant":
      return (
        <AgentRow agent={agent} size={avatarSize} avatar={avatar}>
          <div
            data-testid="assistant-bubble"
            className="min-w-0 max-w-full rounded-[20px_20px_20px_6px] border border-border bg-surface px-3.5 py-2.5 text-fg"
          >
            <Markdown shareBase={site.base}>{item.text}</Markdown>
          </div>
        </AgentRow>
      );
    case "tools":
      return (
        <AgentRow agent={agent} size={avatarSize} avatar={avatar}>
          <ToolsPill count={item.count} tools={item.tools} />
        </AgentRow>
      );
    case "aside":
      return (
        <AgentRow agent={agent} size={avatarSize} avatar={avatar}>
          <AsideRow label={item.label} detail={item.detail} from={item.from} site={site} />
        </AgentRow>
      );
    case "note":
      return (
        <div className="hg-rise self-center rounded-full bg-surface-2 px-2.5 py-1 text-xs text-muted">
          {item.text}
        </div>
      );
  }
}

// 秘書的訊息：左邊一格頭像的位置（手機 30、桌機 32）；連續的訊息只有最後一則放頭像，其他留同寬空白對齊。
function AgentRow({
  agent,
  size,
  avatar,
  children,
}: {
  agent: AgentView;
  size: number;
  avatar: boolean;
  children: ReactNode;
}) {
  return (
    <div className="hg-rise flex max-w-[88%] items-end gap-2 self-start lg:max-w-[78%] lg:gap-2.5">
      <span
        aria-hidden="true"
        className="shrink-0"
        style={{ width: size, height: avatar ? size : 0 }}
      >
        {avatar ? (
          <Avatar
            name={agent.name}
            color={agent.color}
            colorDark={agent.colorDark}
            src={agent.avatarUrl}
            size={size}
          />
        ) : null}
      </span>
      <div className="flex min-w-0 flex-col items-start">{children}</div>
    </div>
  );
}

function UserBubble({ text, site }: { text: string; site: Site }) {
  return (
    <div className="hg-rise flex max-w-[78%] flex-col items-end gap-1 self-end">
      <UserText text={text} shareBase={site.base} />
    </div>
  );
}

// 不是主人說的話（平台通知、排程、別的秘書、核心轉來的）：秘書那邊的一小排，太長的點開看全文（WEB-158）。
function AsideRow({
  label,
  detail,
  from,
  site,
}: {
  label: string;
  detail?: string;
  from?: string;
  site: Site;
}) {
  const { view } = useAgents(site);
  const [open, setOpen] = useState(false);
  const text = from ? `${t("chat.from", { name: view(from).name })}：${label}` : label;
  const face = "min-w-0 truncate";
  return (
    <div data-testid="chat-aside" className="flex min-w-0 max-w-full flex-col items-start gap-1.5">
      {detail ? (
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
          // 看起來 28px 高；點擊範圍用 after 補到 44px（FEN-7）
          className="relative flex h-7 max-w-full items-center gap-1 rounded-full bg-surface-2 px-2.5 text-xs text-muted after:absolute after:inset-x-0 after:-inset-y-2 after:content-[''] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
        >
          <span className={face}>{text}</span>
          <ChevronDown
            className={cn("size-3 shrink-0 transition-transform", open && "rotate-180")}
            strokeWidth={1.8}
            aria-hidden="true"
          />
        </button>
      ) : (
        <div className="flex h-7 max-w-full items-center rounded-full bg-surface-2 px-2.5 text-xs text-muted">
          <span className={face}>{text}</span>
        </div>
      )}
      {open && detail ? (
        <div className="hg-rise min-w-0 max-w-full break-words rounded-xl bg-surface-2 px-3 py-2.5 text-xs text-muted">
          <Markdown shareBase={site.base}>{detail}</Markdown>
        </div>
      ) : null}
    </div>
  );
}

// 連續的工具呼叫收成一個膠囊鈕：點開列工具的白話名稱，同一種合併成「跑指令 ×12」（WEB-125；不顯示工具的輸出）。
function ToolsPill({ count, tools }: { count: number; tools: ToolUse[] }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex flex-col items-start gap-1.5">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        // 看起來是原型的 36px 高；點擊範圍用 after 補到 44px（FEN-7）
        className="relative flex h-9 items-center gap-1.5 rounded-full border border-border bg-surface px-3 text-[13px] text-muted after:absolute after:inset-x-0 after:-inset-y-1 after:content-[''] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
      >
        <Terminal className="size-[15px]" strokeWidth={1.8} aria-hidden="true" />
        {t("chat.tools", { n: count })}
        <ChevronDown
          className={cn("size-3.5 transition-transform", open && "rotate-180")}
          strokeWidth={1.8}
          aria-hidden="true"
        />
      </button>
      {open ? (
        <ul className="hg-rise m-0 flex list-none flex-col gap-1 rounded-xl bg-surface-2 px-3 py-2.5 text-xs text-muted">
          {tools.map((u) => (
            <li key={u.label} className="break-all">
              {u.count > 1 ? t("chat.toolTimes", { name: u.label, n: u.count }) : u.label}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

// 秘書正在回覆（什麼時候畫看 showTypingDots）：原型的三點動畫，帶她的頭像。
/** 打字中：有還在打的那段（text）就先淡色顯示那段，點點接在後面（DEV-150）。 */
function TypingDots({ agent, size, text }: { agent: AgentView; size: number; text?: string }) {
  return (
    <div className="flex items-end gap-2 self-start lg:gap-2.5">
      <span aria-hidden="true" className="shrink-0" style={{ width: size, height: size }}>
        <Avatar
          name={agent.name}
          color={agent.color}
          colorDark={agent.colorDark}
          src={agent.avatarUrl}
          size={size}
        />
      </span>
      <div
        role="status"
        aria-label={t("chat.typing", { name: agent.name })}
        className="flex max-w-[min(80%,640px)] flex-col gap-2 rounded-[20px_20px_20px_6px] border border-border bg-surface px-4 py-3"
      >
        {text ? (
          <p
            data-draft
            className="whitespace-pre-wrap break-words text-[15px] leading-relaxed text-muted"
          >
            {text}
          </p>
        ) : null}
        <span className="flex gap-1">
          <span className="hg-dot size-[7px] rounded-full bg-muted" />
          <span className="hg-dot size-[7px] rounded-full bg-muted" />
          <span className="hg-dot size-[7px] rounded-full bg-muted" />
        </span>
      </div>
    </div>
  );
}
