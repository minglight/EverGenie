import type { ThreadSummary } from "@evergenie/sdk-web";
import { cn } from "@evergenie/ui";
import { Link } from "@tanstack/react-router";
import { useShowCodes } from "../../data/queries";
import { homeSearch, PRIMARY, type SiteRef } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { useToolDetails } from "../../lib/chatDisplay";
import { useNow } from "../../lib/now";
import { needsReply, toneBadge } from "../../lib/statusTone";
import {
  CACHE_TICK_MS,
  cacheLeftMinutes,
  cacheView,
  statusView,
  toneClass,
  workTitle,
} from "./threads";

/** 秘書底下的一個工作 session（P7-5、D-33）：單號、單的標題、狀態、hot cache 倒數。整列是一個連結，點進去看對話與中途補話。 */
export function WorkRow({
  thread: th,
  site = PRIMARY,
  selected,
}: {
  thread: ThreadSummary;
  /** 這串在哪台（連過去帶 ?home=）；不給＝主伺服器。 */
  site?: SiteRef;
  selected: boolean;
}) {
  const now = useNow(CACHE_TICK_MS);
  const toolDetails = useToolDetails();
  const status = statusView(th, now, toolDetails);
  const left = cacheLeftMinutes(th, now);
  const cache = left === null ? null : cacheView(left);
  const title = workTitle(th);
  // 簡單模式不露單號（DEV-156 U-22）：有標題就只寫標題；沒標題才用單號當名字。
  const showCode = useShowCodes() || !title;
  return (
    <Link
      to="/chat/$session"
      params={{ session: th.name ?? "" }}
      search={homeSearch(site)}
      aria-current={selected ? "true" : undefined}
      data-thread={th.name}
      data-site={site.key}
      data-thread-status={th.status}
      data-work-task={th.task}
      className={cn(
        "flex min-h-[48px] items-center gap-2.5 rounded-xl px-2.5 py-2 text-left text-fg hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus",
        selected && "bg-selected shadow-[inset_0_0_0_1px_var(--accent)] hover:bg-selected",
      )}
    >
      {showCode ? (
        <span
          className={cn(
            "shrink-0 rounded-md bg-hover px-1.5 py-0.5 font-mono text-xs font-semibold",
            needsReply(th) && "bg-attention-soft text-attention-fg",
          )}
        >
          {th.task}
        </span>
      ) : null}
      <span className="min-w-0 flex-1 truncate text-sm">{title || th.task}</span>
      <span className="flex shrink-0 items-center gap-2 text-xs">
        {status.text ? (
          <span className={cn("font-medium", toneClass[status.tone])} data-testid="work-status">
            {status.text}
          </span>
        ) : null}
        {cache !== null ? (
          <span
            className={cn(cache.soon ? "font-medium text-fg" : "text-muted")}
            data-testid="work-cache"
            data-cache-soon={cache.soon ? "true" : undefined}
            title={t("chat.work.cacheHint")}
          >
            {cache.text}
          </span>
        ) : null}
        {th.unread > 0 ? (
          <span
            role="img"
            aria-label={t("chat.row.unread", { n: th.unread })}
            className={cn(
              "box-border flex h-5 min-w-5 items-center justify-center rounded-full px-1.5 font-semibold",
              toneBadge.fresh,
            )}
          >
            {th.unread}
          </span>
        ) : null}
      </span>
    </Link>
  );
}
