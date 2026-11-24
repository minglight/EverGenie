import { cn, PersonaMedia } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { RowCode } from "../../components/RowCode";
import { useAgents } from "../../data/agents";
import { overviewQuery } from "../../data/queries";
import { canLinkDetail } from "../../data/siteLinks";
import { homeSearch, PRIMARY, type SiteRef } from "../../data/sites";
import { type WaitingKind, workFor } from "../../data/waiting";
import { type TextKey, t } from "../../i18n/zh-TW";
import { useDocumentVisible } from "../../lib/visible";

const kindLabel: Record<WaitingKind | "todo", { key: TextKey; tone: string }> = {
  approve: { key: "chat.pane.approve", tone: "text-attention-fg" },
  review: { key: "chat.pane.review", tone: "text-accent" },
  ask: { key: "chat.pane.ask", tone: "text-accent" },
  todo: { key: "chat.pane.todo", tone: "text-muted" },
};

interface Row {
  key: string;
  kind: WaitingKind | "todo";
  id: string;
  title: string;
  to: "/messages/$id" | "/tasks/$id" | "/reminders/$id";
}

// 桌機（≥ 1280）討論串右側的角色欄：上半是她的影片，下半是「她手上的事」（REQ-156）。
// 別台的秘書（S3-6）：影片、她手上的事都讀那台，連過去帶 ?home=。
// waitingHere：這段對話正在等你回答（確認卡、選擇題，WEB-92）：總覽不列對話裡的提問，這裡多一列，不能寫「沒有等你處理的事」。
export function PersonaPane({
  agentId,
  site = PRIMARY,
  waitingHere = false,
}: {
  agentId: string;
  site?: SiteRef;
  waitingHere?: boolean;
}) {
  const { view } = useAgents(site);
  const agent = view(agentId);
  const visible = useDocumentVisible();
  const { data } = useQuery(overviewQuery(site));
  const work = workFor(data, [agentId]);
  const rows: Row[] = [
    ...work.items.map((i) => ({
      key: `${i.kind}-${i.id}`,
      kind: i.kind,
      id: i.id,
      title: i.title,
      to: i.target.to,
    })),
    ...work.reminders.map((r) => ({
      key: `todo-${r.id}`,
      kind: "todo" as const,
      id: r.id ?? "",
      title: r.content ?? "",
      to: "/reminders/$id" as const,
    })),
  ];
  return (
    <aside
      aria-label={t("chat.pane.label", { name: agent.name })}
      className="flex w-[360px] shrink-0 flex-col border-l border-border bg-side"
    >
      <PersonaMedia
        name={agent.name}
        src={agent.avatarUrl}
        video={agent.videoUrl}
        videos={agent.videoUrls}
        color={agent.color}
        colorDark={agent.colorDark}
        paused={!visible}
        scrimHeight={140}
        className="h-[480px] shrink-0"
      >
        <div className="absolute right-5 bottom-[18px] left-5 text-on-media">
          <p className="font-display truncate text-[28px] leading-tight font-bold">{agent.name}</p>
          {agent.duty ? (
            <p className="mt-1 truncate text-[13px] text-on-media-soft">{agent.duty}</p>
          ) : null}
        </div>
      </PersonaMedia>
      <div className="hg-scroll flex min-h-0 flex-1 flex-col gap-2.5 overflow-y-auto px-[18px] py-4">
        <h2 className="text-xs font-semibold text-muted">
          {t("chat.pane.work", { name: agent.name })}
        </h2>
        {waitingHere ? (
          <div data-testid="pane-chat-waiting" className={cn(rowCls, "cursor-default")}>
            <span className="text-xs font-semibold text-attention-fg">
              {t("chat.pane.chatWaiting")}
            </span>
            <span className="text-sm font-semibold break-words">
              {t("chat.pane.chatWaitingText")}
            </span>
          </div>
        ) : null}
        {rows.map((r) => {
          const body = (
            <>
              <span className={cn("text-xs font-semibold", kindLabel[r.kind].tone)}>
                {t(kindLabel[r.kind].key)}
              </span>
              <span className="text-sm font-semibold break-words">
                <RowCode id={r.id} sep=" " />
                {r.title}
              </span>
            </>
          );
          // 別台的事連到那台（?home=）；那個詳情頁還不認 ?home 的只列出來、不連（canLinkDetail），
          // 免得打開主伺服器同編號的那一筆。
          if (!canLinkDetail(site, r.to)) {
            return (
              <div key={r.key} data-work-row={r.id} className={cn(rowCls, "cursor-default")}>
                {body}
              </div>
            );
          }
          return (
            <Link
              key={r.key}
              to={r.to}
              params={{ id: r.id }}
              search={homeSearch(site)}
              data-work-row={r.id}
              className={cn(
                rowCls,
                "no-underline focus-visible:outline-2 focus-visible:outline-focus",
              )}
            >
              {body}
            </Link>
          );
        })}
        {rows.length === 0 && !waitingHere ? (
          <p className="text-[13px] text-muted">{t("chat.pane.empty")}</p>
        ) : null}
      </div>
    </aside>
  );
}

const rowCls =
  "flex flex-col gap-0.5 rounded-[10px] border border-border bg-surface px-3 py-2.5 text-fg";
