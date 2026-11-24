import { Card } from "@evergenie/ui";
import { Link } from "@tanstack/react-router";
import { Check } from "lucide-react";
import type { CSSProperties } from "react";
import { AgentChatLink } from "../../components/AgentChatLink";
import { CardAnswer } from "../../components/AnswerPanel";
import { RowCode } from "../../components/RowCode";
import { SiteRefTag } from "../../components/SiteTag";
import { SoftPath } from "../../components/SoftPath";
import { TaskActions } from "../../components/TaskActions";
import { TaskAnswer, useTaskAsks } from "../../components/TaskAnswer";
import { useAgents } from "../../data/agents";
import { useSettled } from "../../data/answers";
import { canLinkDetail } from "../../data/siteLinks";
import { homeSearch } from "../../data/sites";
import { itemPreview, type WaitingItem } from "../../data/waiting";
import { relativeAgo, waitedFor } from "../../lib/format";
import { KindPill } from "./KindPill";

export function waitingTime(item: WaitingItem, now: Date): string {
  return item.kind === "approve"
    ? relativeAgo(item.at, now)
    : waitedFor(item.at, now, item.waitingDays);
}

// 剛處理完：綠色一列（UXC-9）。下次伺服器的資料更新後，這一列就不在清單裡了。
function SettledRow({ id, label }: { id: string; label: string }) {
  return (
    <div
      role="status"
      data-card={id}
      className="hg-rise flex min-h-[52px] items-center gap-2.5 rounded-[14px] bg-ok-soft px-3.5 text-sm text-ok"
    >
      <Check className="size-[18px] shrink-0" strokeWidth={2} aria-hidden="true" />
      <span className="min-w-0 flex-1 truncate">{label}</span>
    </div>
  );
}

// 「等你處理」的一張卡（單欄版）：上半部是連結（到訊息或任務單的詳細頁），下半部是就地動作——
// 同意／選項、驗收／退回、去聊；按下立刻收成綠色一列，不給復原（它會馬上通知秘書）。
// 別台的那件（item.site）：動作送到那台、連結帶 ?home=，名字旁標台名（S3-7a）。
const CARD_BODY =
  "flex flex-col gap-2.5 rounded-xl focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";

export function WaitingCard({ item, now, index }: { item: WaitingItem; now: Date; index: number }) {
  const { view } = useAgents(item.site);
  const settled = useSettled().get(item.key);
  // 秘書在問你：問句下面就地回答（DEV-156 U-05）；「去聊」退成次要。
  const asks = useTaskAsks(item.kind === "ask" ? item.task : undefined, item.site);
  if (settled) return <SettledRow id={item.key} label={settled.label} />;
  const agent = view(item.agent);
  const detail = itemPreview(item);
  const body = (
    <>
      <div className="flex min-w-0 items-center gap-2">
        {/* 頭像另外是一個連結（進她的討論串），疊在這個空位上，避免連結裡面再包連結。 */}
        <span className="size-[26px] shrink-0" aria-hidden="true" />
        {/* 秘書名、種類、等了多久都不折行也不被擠扁；放不下時縮的是台名標籤（DEV-156 U-23）。 */}
        <span
          data-testid="waiting-agent-name"
          className="max-w-[45%] shrink-0 truncate text-[13px] font-semibold"
        >
          {agent.name}
        </span>
        <SiteRefTag site={item.site} />
        <KindPill kind={item.kind} className="shrink-0 whitespace-nowrap" />
        <span className="ml-auto shrink-0 text-xs whitespace-nowrap text-muted">
          {waitingTime(item, now)}
        </span>
      </div>
      <div className="text-base leading-[1.45] font-semibold">
        <RowCode id={item.id} className="text-[13px]" />
        <SoftPath text={item.title} />
      </div>
      {detail ? <div className="text-sm leading-normal text-muted">{detail}</div> : null}
    </>
  );
  return (
    <Card data-card={item.key} className="hg-rise relative flex flex-col gap-2.5 px-4 pt-3.5 pb-4">
      {/* 左邊一條秘書顏色的細條：一眼看出是誰的事。 */}
      <span
        aria-hidden="true"
        className="hg-stripe absolute top-4 bottom-4 left-0 w-[3px] rounded-r-full"
        style={
          {
            "--agent-color": agent.color || undefined,
            "--agent-color-dark": agent.colorDark || undefined,
          } as CSSProperties
        }
      />
      {/* 別台的單詳情頁還不認 ?home（S3-7b 前）：卡片只顯示、不連，免得打開這台同編號的那張。 */}
      {canLinkDetail(item.site, item.target.to) ? (
        <Link
          to={item.target.to}
          params={item.target.params}
          search={homeSearch(item.site)}
          className={CARD_BODY}
        >
          {body}
        </Link>
      ) : (
        <div className={CARD_BODY}>{body}</div>
      )}
      <AgentChatLink
        agent={item.agent}
        site={item.site}
        size={26}
        className="absolute top-3.5 left-4 z-[1]"
      />
      {item.kind === "approve" && item.notification ? (
        <CardAnswer notification={item.notification} item={item} index={index} site={item.site} />
      ) : null}
      {item.kind === "ask" && item.task ? (
        <TaskAnswer
          task={item.task}
          asks={asks}
          size="card"
          item={item}
          index={index}
          site={item.site}
        />
      ) : null}
      {item.kind !== "approve" && item.task ? (
        <TaskActions
          task={item.task}
          size="card"
          item={item}
          index={index}
          site={item.site}
          answering={asks.length > 0}
        />
      ) : null}
    </Card>
  );
}
