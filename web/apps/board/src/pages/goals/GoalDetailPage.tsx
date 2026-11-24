import { ApiError, type Goal, type Task } from "@evergenie/sdk-web";
import { Card, cn, Markdown } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";
import { type ComponentType, type ReactNode, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { PageTitle } from "../../components/PageTitle";
import { Pill } from "../../components/Pill";
import { SiteGate } from "../../components/SiteGate";
import { ItemSiteTag } from "../../components/SiteTag";
import { LoadFailed, LoadingBlock, NotFoundBlock } from "../../components/StateViews";
import { useAgents } from "../../data/agents";
import { goalQuery } from "../../data/queries";
import { homeSearch, PRIMARY, type Site, siteUsable, useHomeSite } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { parseIso, relativeAgo } from "../../lib/format";
import { knownText } from "../../lib/knownText";
import { useIsXWide } from "../../lib/media";
import { useNow } from "../../lib/now";
import { isDelivery, statusText, statusTone } from "../../lib/taskText";
import {
  answerDraftGoal,
  type GoalMembers,
  goalMembersQuery,
  goalRoomName,
  withdrawGoal,
} from "./goals";
import { openedByOwner, stateKey, stateTone } from "./goalText";

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="text-xs text-muted">{label}</span>
      <span className="text-sm font-medium break-words">{children}</span>
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-1.5">
      <h2 className="text-sm font-semibold">{title}</h2>
      {children}
    </section>
  );
}

/**
 * 跟負責的秘書聊這個 Goal 的畫面（WEB-82）。聊天元件在聊天頁，Goal 頁不能 import 它（FEN-27），
 * 所以由 router 從外殼（shell/GoalChat.tsx）交進來；沒給就沒有聊天。`site`＝這個 Goal 所在的台（?home=），
 * 對話開在那台、不退回主伺服器（FEN-28）。
 */
export interface GoalChatSlot {
  /** 寬螢幕右邊一欄。 */
  Aside: ComponentType<{ goal: Goal; site: Site }>;
  /** 窄螢幕頁面裡的一顆按鈕。 */
  Button: ComponentType<{ goal: Goal; site: Site }>;
}

/** 這個 Goal 能不能聊：有負責的秘書，而且她在那台沒合併的名冊上、啟用中（WEB-49）。 */
function useCanChat(goal: Goal | undefined, site: Site | undefined): boolean {
  const { all } = useAgents(site ?? PRIMARY);
  if (!goal || !site || !goalRoomName(goal)) return false;
  return all.some((a) => a.id === goal.agent && a.enabled);
}

export function GoalDetailPage({ chat }: { chat?: GoalChatSlot }) {
  const { id } = useParams({ strict: false }) as { id: string };
  const xwide = useIsXWide();
  // 右欄的聊天跟 Goal 同一台（?home=，跟 SiteGate 同一個解析）：那台能用才有。
  const { site: home } = useHomeSite();
  const site = home && siteUsable(home) ? home : undefined;
  const goal =
    useQuery({ ...goalQuery(id, site ?? PRIMARY), enabled: Boolean(site) }).data ?? undefined;
  const canChat = useCanChat(goal, site) && Boolean(chat);
  // 寬螢幕（≥ 1280）而且有負責的秘書：右邊一欄跟她聊這個 Goal；窄螢幕在頁面裡放一顆按鈕開到聊天頁。
  // 外層結構只看螢幕寬度，資料載完不換結構（不然 Goal 內容會整個重掛）。
  const Aside = chat && canChat && xwide && goal && site ? chat.Aside : undefined;
  const page = (
    <div className="pb-2">
      {/* ?home= 指的那台（沒寫＝主伺服器，跟 S3 之前一樣；S3-7b）。Goal、它的成員、動作、聊天都在那台。 */}
      <SiteGate back={{ to: "/goals", label: t("goal.back") }}>
        {(gated) => (
          <GoalView
            key={`${gated.key}/${id}`}
            id={id}
            site={gated}
            ChatButton={canChat && !xwide ? chat?.Button : undefined}
          />
        )}
      </SiteGate>
    </div>
  );
  if (!xwide) return page;
  return (
    <div className="flex h-full min-h-0">
      <div className="min-w-0 flex-1 overflow-y-auto">{page}</div>
      {Aside && goal && site ? (
        <Aside key={`${site.key}/${goal.id}`} goal={goal} site={site} />
      ) : null}
    </div>
  );
}

// 一個 Goal 的閱讀版面（FEN-13）：原話是唯讀的（確認後誰都不能改，D-23）；Planbook 是秘書寫的，這裡也唯讀。
// 動作依狀態只顯示能做的（FEN-14）：要你同意 → 確認／不要；進行中 → 收回。
function GoalView({
  id,
  site,
  ChatButton,
}: {
  id: string;
  site: Site;
  ChatButton?: GoalChatSlot["Button"];
}) {
  const client = useQueryClient();
  const now = useNow();
  const { view } = useAgents(site);
  const query = useQuery(goalQuery(id, site));
  const members = useQuery(goalMembersQuery(id, site));
  const [busy, setBusy] = useState<"confirm" | "decline" | "withdraw" | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState("");
  const goal: Goal | undefined = query.data ?? undefined;

  if (!goal) {
    if (query.isError) {
      if (query.error instanceof ApiError && query.error.code === "not_found") {
        return (
          <NotFoundBlock
            title={t("goal.notFound")}
            hint={t("goal.notFound.hint")}
            backLabel={t("goal.back")}
            to="/goals"
          />
        );
      }
      return <LoadFailed what={t("page.goal")} onRetry={() => void query.refetch()} />;
    }
    return <LoadingBlock />;
  }

  const state = goal.state ?? "active";
  const log = goal.planbook_log ?? [];
  const lastLog = log[log.length - 1];
  const since = parseIso(goal.confirmed_at) ?? parseIso(goal.created_at);
  const ended = parseIso(goal.ended_at);
  const owner = openedByOwner(goal);

  const run = async (kind: "confirm" | "decline" | "withdraw", action: () => Promise<void>) => {
    if (busy) return;
    setBusy(kind);
    setError("");
    try {
      await action();
      setConfirming(false);
    } catch (err) {
      setError(
        t("goal.action.failed", {
          reason: err instanceof ApiError ? err.message : t("settings.error.generic"),
        }),
      );
    } finally {
      setBusy(null);
    }
  };

  return (
    <article
      data-goal={goal.id}
      data-goal-state={state}
      className="mx-auto flex w-full max-w-[720px] flex-col gap-5 px-4 pt-3 pb-4"
    >
      <div className="flex flex-col gap-2.5">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[13px] text-muted">
          <Pill tone={stateTone[state]}>{knownText(stateKey, state)}</Pill>
          <ItemSiteTag site={site} />
          <span className="font-mono">{goal.id}</span>
          <span aria-hidden="true">·</span>
          <span>
            {owner ? t("goal.by.owner") : t("goal.by.agent", { name: view(goal.agent).name })}
          </span>
          {goal.updated_at ? (
            <>
              <span aria-hidden="true">·</span>
              <span>{relativeAgo(goal.updated_at, now)}</span>
            </>
          ) : null}
        </div>
        <PageTitle className="text-2xl leading-[1.3] font-bold tracking-[-0.01em] break-words">
          {goal.title}
        </PageTitle>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        <Fact label={t("goal.field.state")}>{knownText(stateKey, state)}</Fact>
        {goal.agent && !goal.agent.startsWith("owner(") ? (
          <Fact label={t("goal.field.agent")}>{view(goal.agent).name}</Fact>
        ) : null}
        {since && state !== "draft" ? (
          <Fact label={t("goal.field.since")}>{relativeAgo(since.toISOString(), now)}</Fact>
        ) : null}
        {ended ? (
          <Fact label={t("goal.field.ended")}>{relativeAgo(ended.toISOString(), now)}</Fact>
        ) : null}
      </div>

      {ChatButton ? <ChatButton goal={goal} site={site} /> : null}

      {state === "draft" ? (
        <section
          data-testid="goal-draft"
          className="flex flex-col gap-3 rounded-[14px] bg-attention-soft px-4 py-3"
        >
          <p className="text-[14px] leading-snug text-attention-fg">{t("goal.draft.note")}</p>
          <div className="flex flex-wrap gap-2">
            <ActionButton
              tone="primary"
              busy={busy === "confirm"}
              disabled={busy !== null}
              onClick={() => void run("confirm", () => answerDraftGoal(client, goal, true, site))}
            >
              {t("goal.confirm")}
            </ActionButton>
            <ActionButton
              tone="outline"
              busy={busy === "decline"}
              disabled={busy !== null}
              onClick={() => void run("decline", () => answerDraftGoal(client, goal, false, site))}
            >
              {t("goal.decline")}
            </ActionButton>
          </div>
        </section>
      ) : null}

      {state === "draft" ? null : <GoalBlockers members={members.data} site={site} />}

      <Section title={t("goal.section.planbook")}>
        <div data-testid="goal-planbook" className="contents">
          {goal.planbook?.trim() ? (
            <Markdown>{goal.planbook}</Markdown>
          ) : (
            <p className="text-sm text-muted">{t("goal.planbook.empty")}</p>
          )}
          {lastLog ? (
            <p className="text-xs text-muted">
              {t("goal.planbook.log", {
                n: log.length,
                who: view(lastLog.by).name,
                when: relativeAgo(lastLog.at, now),
              })}
            </p>
          ) : null}
        </div>
      </Section>

      <GoalMembersSection members={members.data} site={site} />

      <Section title={t("goal.section.text")}>
        <blockquote
          data-testid="goal-text"
          className="m-0 rounded-[14px] border-l-4 border-accent bg-surface-2 px-4 py-3 text-[15px] leading-relaxed break-words whitespace-pre-wrap"
        >
          {goal.text}
        </blockquote>
        <p className="text-xs text-muted">{t("goal.text.note")}</p>
      </Section>

      {goal.criteria ? (
        <Section title={t("goal.section.criteria")}>
          <p
            data-testid="goal-criteria"
            className="m-0 text-[15px] leading-relaxed break-words whitespace-pre-wrap"
          >
            {goal.criteria}
          </p>
        </Section>
      ) : null}

      {goal.summary ? (
        <Section title={t("goal.section.summary")}>
          <Markdown>{goal.summary}</Markdown>
        </Section>
      ) : null}

      {state === "active" ? (
        <div className="flex flex-col gap-3">
          <div>
            <ActionButton
              tone="outline"
              aria-expanded={confirming}
              disabled={busy !== null}
              onClick={() => setConfirming((v) => !v)}
            >
              {t("goal.withdraw")}
            </ActionButton>
          </div>
          {confirming ? (
            <section
              aria-label={t("goal.withdraw")}
              className="hg-rise flex flex-col gap-3 rounded-[14px] bg-attention-soft px-4 py-3"
            >
              <p className="text-[14px] leading-snug text-attention-fg">
                {t("goal.withdraw.ask", { title: goal.title ?? "" })}
              </p>
              <div className="flex flex-wrap gap-2">
                <ActionButton
                  tone="danger"
                  busy={busy === "withdraw"}
                  onClick={() => void run("withdraw", () => withdrawGoal(client, goal, site))}
                >
                  {t("goal.withdraw.yes")}
                </ActionButton>
                <ActionButton
                  tone="outline"
                  disabled={busy !== null}
                  onClick={() => setConfirming(false)}
                >
                  {t("goal.withdraw.no")}
                </ActionButton>
              </div>
            </section>
          ) : null}
        </div>
      ) : null}
      {error ? (
        <p role="alert" className="text-[13px] text-danger">
          {error}
        </p>
      ) : null}
    </article>
  );
}

const rowLink =
  "flex min-h-11 items-center gap-2 px-3.5 py-2 text-sm hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus";

function TaskLine({ task, site }: { task: Task; site: Site }) {
  const { view } = useAgents(site);
  const status = task.status ?? "doing";
  return (
    <li>
      <Link
        to="/tasks/$id"
        params={{ id: task.id ?? "" }}
        search={homeSearch(site)}
        className={rowLink}
      >
        <Pill tone={statusTone[status]}>{statusText(task)}</Pill>
        <span className="min-w-0 flex-1 truncate">
          <span className="font-mono text-xs text-muted">{task.id}</span> · {task.title}
        </span>
        {task.agent ? (
          <span className="shrink-0 text-xs text-muted">{view(task.agent).name}</span>
        ) : null}
      </Link>
    </li>
  );
}

// 卡在誰（2026-11-01，design-goal-task.md）：Goal 底下等你、等外部的單放在最上面，一眼看到這個 Goal 停在誰手上；
// 等你的單帶秘書要你做的事（驗收的帶結果），等外部的帶負責的秘書。都沒有就說沒卡住。
function GoalBlockers({ members, site }: { members: GoalMembers | undefined; site: Site }) {
  const { view } = useAgents(site);
  if (members === undefined) return null;
  const blocked = members.tasks.filter(
    (task) => task.status === "waiting_user" || task.status === "waiting_external",
  );
  const mine = blocked.filter((task) => task.status === "waiting_user");
  const outside = blocked.filter((task) => task.status === "waiting_external");
  return (
    <Section title={t("goal.section.blockers")}>
      {blocked.length === 0 ? (
        <p data-testid="goal-blockers-none" className="text-sm text-muted">
          {t("goal.blockers.none")}
        </p>
      ) : (
        <Card className="overflow-hidden">
          <ul
            data-testid="goal-blockers"
            className="m-0 flex list-none flex-col divide-y divide-border p-0"
          >
            {[...mine, ...outside].map((task) => {
              const note =
                task.status === "waiting_user"
                  ? isDelivery(task)
                    ? task.result
                    : task.ask
                  : task.agent
                    ? t("goal.blockers.external", { name: view(task.agent).name })
                    : "";
              return (
                <li key={task.id}>
                  <Link
                    to="/tasks/$id"
                    params={{ id: task.id ?? "" }}
                    search={homeSearch(site)}
                    className="flex min-h-11 flex-col gap-0.5 px-3.5 py-2.5 hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus"
                  >
                    <span className="flex min-w-0 items-center gap-2 text-sm">
                      <Pill tone={task.status === "waiting_user" ? "attention" : "muted"}>
                        {statusText(task)}
                      </Pill>
                      <span className="min-w-0 flex-1 truncate">{task.title}</span>
                    </span>
                    {note ? (
                      <span className="line-clamp-2 text-[13px] text-muted break-words">
                        {note}
                      </span>
                    ) : null}
                  </Link>
                </li>
              );
            })}
          </ul>
        </Card>
      )}
    </Section>
  );
}

// 掛在這個 Goal 上的事（REQ-232）：進行中的任務單、還沒做的待辦，已結束的任務單收在最下面（點開才列）。
// 放在「卡在誰」與 Planbook 下面、原話上面。
function GoalMembersSection({ members, site }: { members: GoalMembers | undefined; site: Site }) {
  const [showClosed, setShowClosed] = useState(false);
  if (members === undefined) return null;
  const { tasks, closed, reminders } = members;
  return (
    <Section title={t("goal.section.members")}>
      {tasks.length + closed.length + reminders.length === 0 ? (
        <p className="text-sm text-muted">{t("goal.members.none")}</p>
      ) : (
        <div data-testid="goal-members" className="flex flex-col gap-3">
          {tasks.length > 0 ? (
            <div className="flex flex-col gap-1">
              <p className="text-[13px] font-medium text-muted">
                {t("goal.members.tasks", { n: tasks.length })}
              </p>
              <Card className="overflow-hidden">
                <ul className="m-0 flex list-none flex-col divide-y divide-border p-0">
                  {tasks.map((task) => (
                    <TaskLine key={task.id} task={task} site={site} />
                  ))}
                </ul>
              </Card>
            </div>
          ) : null}
          {reminders.length > 0 ? (
            <div className="flex flex-col gap-1">
              <p className="text-[13px] font-medium text-muted">
                {t("goal.members.reminders", { n: reminders.length })}
              </p>
              <Card className="overflow-hidden">
                <ul className="m-0 flex list-none flex-col divide-y divide-border p-0">
                  {reminders.map((r) => (
                    <li key={r.id}>
                      <Link
                        to="/reminders/$id"
                        params={{ id: r.id ?? "" }}
                        search={homeSearch(site)}
                        className={rowLink}
                      >
                        <span className="min-w-0 flex-1 truncate">
                          <span className="font-mono text-xs text-muted">{r.id}</span> · {r.content}
                        </span>
                      </Link>
                    </li>
                  ))}
                </ul>
              </Card>
            </div>
          ) : null}
          {closed.length > 0 ? (
            <div className="flex flex-col gap-1">
              <button
                type="button"
                aria-expanded={showClosed}
                onClick={() => setShowClosed((v) => !v)}
                className="flex min-h-11 items-center gap-1 self-start text-[13px] font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus"
              >
                <ChevronRight
                  className={cn("size-4 transition-transform", showClosed && "rotate-90")}
                  aria-hidden="true"
                />
                {t("goal.members.closed", { n: closed.length })}
              </button>
              {showClosed ? (
                <Card className="overflow-hidden">
                  <ul className="m-0 flex list-none flex-col divide-y divide-border p-0">
                    {closed.map((task) => (
                      <TaskLine key={task.id} task={task} site={site} />
                    ))}
                  </ul>
                </Card>
              ) : null}
            </div>
          ) : null}
        </div>
      )}
    </Section>
  );
}
