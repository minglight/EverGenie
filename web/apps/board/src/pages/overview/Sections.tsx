import type { WaitingReason } from "@evergenie/sdk-web";
import { Avatar, Card, cn, EmptyState } from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Bell, ChevronRight } from "lucide-react";
import type { ReactNode } from "react";
import { SiteRefTag } from "../../components/SiteTag";
import { itemKey } from "../../data/acrossSites";
import { useDeferredIds } from "../../data/deferred";
import { markDone } from "../../data/reminderActions";
import { canLinkDetail } from "../../data/siteLinks";
import { homeSearch } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { clockText, parseIso, preview, timeLabel, waitedFor } from "../../lib/format";
import { useNow } from "../../lib/now";
import { toneBadge } from "../../lib/statusTone";
import { type SitedReminder, type SitedThread, useSiteViews } from "./merged";

export function SectionHeading({
  title,
  count,
  action,
}: {
  title: string;
  count?: number;
  action?: ReactNode;
}) {
  return (
    <div className="flex items-baseline justify-between">
      <h2 className="font-display text-lg font-bold">
        {title}
        {count !== undefined ? (
          <span className="ml-1.5 font-medium text-muted">{count}</span>
        ) : null}
      </h2>
      {action}
    </div>
  );
}

const reasonKeys = {
  permission: "waiting.permission",
  question: "waiting.question",
  approval: "waiting.approval",
  reminder: "waiting.reminder",
  work_queue: "waiting.workQueue",
} as const satisfies Record<WaitingReason, string>;

// 等你處理的討論串：點了到 /chat/$session（別台的帶 ?home=）。看多台時每列標台名。
export function WaitingThreads({ threads, now }: { threads: SitedThread[]; now: Date }) {
  const viewOf = useSiteViews();
  if (threads.length === 0) return null;
  return (
    <section className="flex flex-col gap-2.5">
      <SectionHeading title={t("today.threads")} count={threads.length} />
      <Card className="divide-y divide-border overflow-hidden">
        {threads.map(({ site, key, thread }) => {
          const agent = viewOf(site, thread.agent);
          const reason = thread.waiting?.[0];
          const line = [reason ? t(reasonKeys[reason]) : "", preview(thread.last_preview, 40)]
            .filter(Boolean)
            .join(" · ");
          return (
            <Link
              key={key}
              to="/chat/$session"
              params={{ session: thread.name ?? "" }}
              search={homeSearch(site)}
              className="flex min-h-16 items-center gap-3 px-3.5 py-2.5 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus"
            >
              <Avatar
                name={agent.name}
                color={agent.color}
                colorDark={agent.colorDark}
                src={agent.avatarUrl}
                size={40}
                waiting
              />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="flex min-w-0 items-center gap-1.5">
                  <span className="truncate text-[15px] font-semibold">
                    {thread.title || agent.name}
                  </span>
                  <SiteRefTag site={site} />
                </span>
                {line ? <span className="truncate text-[13px] text-muted">{line}</span> : null}
              </span>
              <span className="shrink-0 text-xs text-muted">
                {waitedFor(thread.status_since, now)}
              </span>
            </Link>
          );
        })}
      </Card>
    </section>
  );
}

// 有新訊息的對話（DEV-153）：秘書說了、你還沒看的（例：新家第一位秘書上班後的招呼）。已經在「要你回答的討論串」的不重複。
// 點了到 /chat/$session（別台的帶 ?home=），看過就不列了。看多台時每列標台名。
export function NewMessageThreads({ threads }: { threads: SitedThread[] }) {
  const viewOf = useSiteViews();
  if (threads.length === 0) return null;
  return (
    <section data-testid="today-new-messages" className="flex flex-col gap-2.5">
      <SectionHeading title={t("today.newMessages")} count={threads.length} />
      <Card className="divide-y divide-border overflow-hidden">
        {threads.map(({ site, key, thread }) => {
          const agent = viewOf(site, thread.agent);
          const line = preview(thread.last_preview, 40);
          return (
            <Link
              key={key}
              to="/chat/$session"
              params={{ session: thread.name ?? "" }}
              search={homeSearch(site)}
              className="flex min-h-16 items-center gap-3 px-3.5 py-2.5 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus"
            >
              <Avatar
                name={agent.name}
                color={agent.color}
                colorDark={agent.colorDark}
                src={agent.avatarUrl}
                size={40}
              />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="flex min-w-0 items-center gap-1.5">
                  <span className="truncate text-[15px] font-semibold">
                    {thread.title || agent.name}
                  </span>
                  <SiteRefTag site={site} />
                </span>
                {line ? <span className="truncate text-[13px] text-muted">{line}</span> : null}
              </span>
              <span
                role="img"
                aria-label={t("chat.row.unread", { n: thread.unread })}
                className={cn(
                  "box-border flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full px-1.5 text-xs font-semibold",
                  toneBadge.fresh,
                )}
              >
                {thread.unread}
              </span>
            </Link>
          );
        })}
      </Card>
    </section>
  );
}

const followKeys = {
  none: "follow.none",
  remind: "follow.remind",
  confirm: "follow.confirm",
  check: "follow.check",
  discuss: "follow.discuss",
} as const;

// 今天的待辦：白卡、列之間 1px 分隔、右邊是等寬字的時間。
export function TodayReminders({ reminders }: { reminders: SitedReminder[] }) {
  const viewOf = useSiteViews();
  const client = useQueryClient();
  const now = useNow();
  const hidden = useDeferredIds();
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const rows = reminders.filter(({ site, reminder: r }) => !hidden.has(itemKey(site, r.id ?? "")));
  return (
    <section className="flex flex-col gap-2.5">
      <SectionHeading
        title={t("today.todos")}
        action={
          <Link
            to="/reminders"
            className="flex h-11 items-center text-sm font-medium text-accent focus-visible:outline-2 focus-visible:outline-focus"
          >
            {t("today.todos.all")}
          </Link>
        }
      />
      {rows.length === 0 ? (
        <EmptyState title={t("today.todos.empty")} hint={t("today.todos.emptyHint")} />
      ) : (
        <Card className="divide-y divide-border overflow-hidden">
          {rows.map(({ site, key, reminder: r }) => {
            const owner = viewOf(site, r.owner).name;
            const follow = r.follow ? t(followKeys[r.follow]) : "";
            // 前幾天就到期還沒做的（RMD-18）：寫日期、標紅；今天的只寫時間。
            const at = parseIso(r.at);
            const overdue = at !== null && at.getTime() < startOfToday;
            const body = (
              <>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[15px]">{r.content}</span>
                  <span className="flex min-w-0 items-center gap-1.5 text-xs text-muted">
                    <span className="truncate">{[owner, follow].filter(Boolean).join(" · ")}</span>
                    <SiteRefTag site={site} />
                  </span>
                </span>
                <span
                  data-overdue={overdue || undefined}
                  className={cn(
                    "font-mono text-[13px] whitespace-nowrap",
                    overdue ? "text-danger" : "text-muted",
                  )}
                >
                  {(overdue ? timeLabel(r.at, now) : clockText(r.at)) || t("today.todo.noTime")}
                </span>
              </>
            );
            return (
              <div key={key} className={REMINDER_ROW}>
                {/* 圓圈就是「做完」：跟待辦頁同一個動作（可以復原的提示）。 */}
                <button
                  type="button"
                  aria-label={
                    r.content
                      ? t("reminder.markDone.named", { content: r.content })
                      : t("reminder.markDone")
                  }
                  onClick={() => markDone(client, r, site)}
                  className="flex size-11 shrink-0 items-center justify-center rounded-full focus-visible:outline-2 focus-visible:outline-focus"
                >
                  <span className="box-border size-[22px] rounded-full border-2 border-border-strong" />
                </button>
                {/* 別台的待辦詳情頁還不認 ?home（S3-7b 前）：只顯示、不連，免得打開這台同編號的那筆。 */}
                {canLinkDetail(site, "/reminders/$id") ? (
                  <Link
                    to="/reminders/$id"
                    params={{ id: r.id ?? "" }}
                    search={homeSearch(site)}
                    className={REMINDER_LINK}
                  >
                    {body}
                  </Link>
                ) : (
                  <div className={REMINDER_LINK}>{body}</div>
                )}
              </div>
            );
          })}
        </Card>
      )}
    </section>
  );
}

const REMINDER_ROW = "flex min-h-[60px] items-center gap-1 py-1 pr-3.5 pl-1";
const REMINDER_LINK =
  "flex min-h-11 min-w-0 flex-1 items-center gap-2 rounded-lg py-1 focus-visible:outline-2 focus-visible:outline-focus";

// 未讀訊息 N 則：一列，連到 /messages。
export function UnreadRow({ unread }: { unread: number }) {
  return (
    <Link
      to="/messages"
      className="block rounded-[18px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
    >
      <Card className="flex min-h-[52px] items-center gap-3 px-4">
        <Bell className="size-[18px] text-accent" strokeWidth={1.8} aria-hidden="true" />
        <span
          className={
            unread > 0 ? "flex-1 text-[15px] font-medium" : "flex-1 text-[15px] text-muted"
          }
        >
          {unread > 0 ? t("today.unread", { n: unread }) : t("today.unread.none")}
        </span>
        <ChevronRight className="size-[18px] text-muted" aria-hidden="true" />
      </Card>
    </Link>
  );
}
