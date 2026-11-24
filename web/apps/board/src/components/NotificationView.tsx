import { ApiError, type Notification } from "@evergenie/sdk-web";
import { ActionBar, Avatar, cn, Markdown, shareHref } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type ReactNode, useEffect, useRef } from "react";
import { type Agents, primaryOf, useAgents } from "../data/agents";
import { claimLink, claimOf } from "../data/claim";
import { refreshAll } from "../data/live";
import { noticeLinkOf, senderName, speakerAvatar } from "../data/noticeLink";
import { notificationQuery, useShowCodes } from "../data/queries";
import { canLinkDetail } from "../data/siteLinks";
import {
  homeSearch,
  PRIMARY,
  type SiteRef,
  siteHref,
  siteUsable,
  useSite,
  useSiteApi,
} from "../data/sites";
import { t } from "../i18n/zh-TW";
import { chatDraftKey, seedDraft } from "../lib/draft";
import { relativeAgo } from "../lib/format";
import { useNow } from "../lib/now";
import {
  AnswerButtons,
  AnsweredRow,
  AnswerPanel,
  AnswerTyped,
  canAnswer,
  type PaneActions,
  useAnswer,
} from "./AnswerPanel";
import { NoticeMeta } from "./NoticeMeta";
import { SiteNotice } from "./SiteNotice";
import { SiteRefTag } from "./SiteTag";
import { SoftPath } from "./SoftPath";
import { LoadFailed, LoadingBlock, NotFoundBlock } from "./StateViews";

export interface NotificationViewProps {
  id: string;
  /** 桌機「今天」頁的右欄：不重複頁首，動作列固定在底部、按鈕旁有鍵帽。 */
  embedded?: boolean;
  bind?: (actions: PaneActions | null) => void;
  /** 回答送出之後（右欄用來跳到下一件）。 */
  onAnswered?: (text: string) => void;
  /** 右欄動作列最右邊的東西（「打開完整頁面」）。 */
  paneExtra?: ReactNode;
  /** 右欄的按鈕旁要不要鍵帽 A／R（「今天」頁有鍵盤操作才要；訊息頁的右欄沒有）。預設要。 */
  keycaps?: boolean;
  /** 這則在哪一台（預設主伺服器）：從那台讀、回答與已讀送到那台、連結帶那台的 ?home=（S3-7a）。 */
  site?: SiteRef;
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-1.5">
      <h2 className="text-sm font-semibold">{title}</h2>
      {children}
    </section>
  );
}

function Links({ n, site, agents }: { n: Notification; site: SiteRef; agents: Agents }) {
  const items: ReactNode[] = [];
  const fullMode = useShowCodes();
  const home = homeSearch(site);
  const cls =
    "flex min-h-11 items-center rounded-xl border border-border bg-surface px-4 text-[15px] font-medium text-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";
  if (n.task && canLinkDetail(site, "/tasks/$id")) {
    items.push(
      <Link key="task" to="/tasks/$id" params={{ id: n.task }} search={home} className={cls}>
        {t("message.link.task", { id: n.task })}
      </Link>,
    );
  }
  if (n.reminder && canLinkDetail(site, "/reminders/$id")) {
    items.push(
      <Link
        key="reminder"
        to="/reminders/$id"
        params={{ id: n.reminder }}
        search={home}
        className={cls}
      >
        {t("message.link.reminder", { id: n.reminder })}
      </Link>,
    );
  }
  if (n.room) {
    items.push(
      <Link
        key="room"
        to="/chat/$session"
        params={{ session: n.room }}
        search={home}
        className={cls}
      >
        {t("message.link.room")}
      </Link>,
    );
  } else if (!n.speaker && n.agent && agents.all.some((a) => a.id === n.agent && a.enabled)) {
    // 角色本人在說話（speaker，新角色打招呼）時不給「跟秘書聊」：說話的不是那位秘書。
    // 沒有專屬的對話：到她的主要討論串，輸入框先起個頭「關於「標題」：」（已經有草稿就不蓋）。
    const session = primaryOf(n.agent, site.key);
    const draftKey = chatDraftKey(site.key === "" ? session : `${site.key}/${session}`);
    items.push(
      <Link
        key="chat"
        to="/chat/$session"
        params={{ session }}
        search={home}
        className={cls}
        data-testid="message-chat-link"
        onClick={() => seedDraft(draftKey, t("message.chatDraft", { title: n.title ?? "" }))}
      >
        {t("message.link.chatWith", { name: agents.view(n.agent).name })}
      </Link>,
    );
  }
  // 認領是設定頁的事，設定頁只看主伺服器（不合併）：別台的認領不給連結，免得改到這台。
  const claim = site.key === "" ? claimOf(n) : undefined;
  if (claim) {
    items.push(
      <Link key="claim" {...claimLink(claim)} className={cls} data-testid="message-claim-link">
        {fullMode ? t("message.link.claim", { id: claim }) : t("message.link.claim.plain")}
      </Link>,
    );
  }
  // 訊息帶的連結按鈕（NTF-66，例：新角色打招呼的「去角色頁看看」）。站內路徑，只在主伺服器開（設定頁只看主伺服器，
  // 跟認領同一個理由）；放在最前面，它就是這則要你看的地方。
  const link = site.key === "" ? noticeLinkOf(n) : null;
  if (link) {
    items.unshift(
      <Link
        key="link"
        to={link.to as "/"}
        search={link.search as never}
        className={cls}
        data-testid="message-link-button"
      >
        {link.label}
      </Link>,
    );
  }
  if (items.length === 0) return null;
  return (
    <Section title={t("message.links")}>
      <div className="flex flex-wrap gap-2">{items}</div>
    </Section>
  );
}

// 一則通知的完整內容（/messages/$id 與桌機「今天」頁右欄共用）：
// 標題 → 選項按鈕（要你同意而且還沒回答；手機也在第一屏，REQ-138）→「或打字回答」→ 說明、要做的事、圖片 → 相關連結。
export function NotificationView({
  id,
  embedded,
  bind,
  onAnswered,
  paneExtra,
  keycaps = true,
  site = PRIMARY,
}: NotificationViewProps) {
  const client = useQueryClient();
  const agents = useAgents(site);
  const { view } = agents;
  const now = useNow();
  // 別台只有連得上（ok）才讀與寫；不是 ok 時由外面（訊息頁）畫那台的提示，這裡不打請求。
  const siteApi = useSiteApi(site.key);
  const query = useQuery({ ...notificationQuery(id, site), enabled: siteApi !== undefined });
  const n = query.data ?? undefined;
  const marked = useRef("");

  // 打開就標成已讀（REQ-143 的精神：看過了；已讀的「要你同意」仍然算等你處理）。
  useEffect(() => {
    if (!n || n.state !== "unread" || marked.current === id || !siteApi) return;
    marked.current = id;
    siteApi.notify
      .markRead({ ids: [id] })
      .then(() => refreshAll(client, site))
      .catch(() => {});
  }, [n, id, client, site, siteApi]);

  const answerState = useAnswer(n, { site });
  const found = useSite(site.key);

  if (!n) {
    // 別台現在不能讀：只說那台怎麼了（還在試連就先轉圈）。
    if (!siteApi) {
      return found && !siteUsable(found) && found.state !== "checking" ? (
        <div className="mx-auto w-full max-w-[720px] px-4 pt-6">
          <SiteNotice site={found} />
        </div>
      ) : (
        <LoadingBlock />
      );
    }
    if (query.isError) {
      const missing = query.error instanceof ApiError && query.error.code === "not_found";
      if (missing) {
        return (
          <NotFoundBlock
            title={t("message.notFound")}
            hint={t("message.notFound.hint")}
            backLabel={t("message.back")}
            to="/messages"
          />
        );
      }
      return <LoadFailed what={t("page.message")} onRetry={() => void query.refetch()} />;
    }
    return <LoadingBlock />;
  }

  const agent = view(n.agent);
  const pending = canAnswer(n);
  const answered = answerState.answered;
  const replying = pending && answered === undefined;
  const image = n.image ? (siteHref(site, shareHref(n.image) ?? undefined) ?? null) : null;

  const Heading = embedded ? "h2" : "h1";
  // 角色本人在說話（新角色打招呼，NTF-66）：寄件人寫她、標題旁放她的頭像。
  const sender = senderName(n, agent.name);
  const face = speakerAvatar(n);
  const title = (
    <Heading className="text-2xl leading-[1.3] font-bold tracking-[-0.01em] break-words">
      <SoftPath text={n.title ?? ""} />
    </Heading>
  );
  const header = (
    <div className="flex flex-col gap-2.5">
      <NoticeMeta
        n={n}
        agentName={sender}
        time={relativeAgo(n.created_at, now)}
        site={<SiteRefTag site={site} />}
      />
      {face ? (
        <div className="flex items-center gap-3">
          <Avatar name={sender} src={siteHref(site, face)} size={48} className="shrink-0" />
          {title}
        </div>
      ) : (
        title
      )}
    </div>
  );

  // 那件事已經不在了（在別處處理了、綁的事不在了，伺服器自動收掉，DEV-49 S2）：只顯示一行，不顯示空的回答區。
  const settledElsewhere = n.answer?.via === "auto";
  const status = settledElsewhere ? (
    <p
      role="status"
      data-testid="message-settled"
      className="rounded-[14px] bg-surface-2 px-4 py-3 text-sm text-muted"
    >
      {t("message.settled")}
    </p>
  ) : answered !== undefined ? (
    <AnsweredRow notification={n} answer={answered} />
  ) : n.state === "expired" ? (
    <p className="rounded-[14px] bg-surface-2 px-4 py-3 text-sm text-muted">
      {t("message.expired")}
    </p>
  ) : n.state === "failed" ? (
    <p className="rounded-[14px] bg-danger-soft px-4 py-3 text-sm text-danger">
      {t("message.failed")}
    </p>
  ) : null;

  const details = (
    <>
      {n.context ? (
        <Section title={t("message.context")}>
          <Markdown>{n.context}</Markdown>
        </Section>
      ) : null}
      {/* 伺服器沒填要做的事時存「無」（NTF）：沒事要做就不畫這一塊。 */}
      {n.todo && n.todo.trim() !== "無" ? (
        // 「要你做的事」：還要你回答才是橘色；回答了、過期、被自動收掉就是灰色（橘只給等你處理，WEB-70）。
        <section
          data-testid="message-todo"
          data-tone={replying ? "reply" : "idle"}
          className={cn(
            "flex flex-col gap-1.5 rounded-[14px] px-4 py-3",
            replying ? "bg-attention-soft" : "bg-surface-2",
          )}
        >
          <h2
            className={cn("text-sm font-semibold", replying ? "text-attention-fg" : "text-muted")}
          >
            {t("message.todo")}
          </h2>
          <Markdown>{n.todo}</Markdown>
        </section>
      ) : null}
      {image ? (
        <Section title={t("message.image")}>
          <Markdown>{`![${t("message.image")}](${image})`}</Markdown>
        </Section>
      ) : null}
      {!pending && answered === undefined && n.category !== "approve" && n.state !== "expired" ? (
        <p className="text-sm text-muted">{t("message.noAnswerNeeded")}</p>
      ) : null}
      <Links n={n} site={site} agents={agents} />
    </>
  );

  if (embedded) {
    return (
      <div className="flex h-full min-h-0 flex-col">
        <div className="flex min-h-0 flex-1 flex-col gap-[22px] overflow-y-auto px-9 pt-7 pb-6">
          {header}
          {status}
          {details}
          {pending && answered === undefined && n.category !== "due" ? (
            <AnswerTyped notification={n} site={site} onAnswered={onAnswered} />
          ) : null}
        </div>
        <ActionBar placement="pane" label={t("answer.options")}>
          {pending && answered === undefined ? (
            <AnswerButtons
              notification={n}
              size="pane"
              keycaps={keycaps}
              bind={bind}
              site={site}
              onAnswered={onAnswered}
            />
          ) : null}
          {paneExtra}
        </ActionBar>
      </div>
    );
  }

  return (
    <article className={cn("mx-auto flex w-full max-w-[720px] flex-col gap-5 px-4 pt-3 pb-8")}>
      {header}
      {/* 手機版按鈕放上面好快速回；只收打字回答的（typed）要先看問題才知道怎麼答，輸入框放在問題下面（WEB-112）。 */}
      {pending && answered === undefined ? (
        n.typed ? null : (
          <AnswerPanel notification={n} site={site} onAnswered={onAnswered} />
        )
      ) : (
        status
      )}
      {details}
      {pending && answered === undefined && n.typed ? (
        <AnswerPanel notification={n} site={site} onAnswered={onAnswered} />
      ) : null}
    </article>
  );
}
