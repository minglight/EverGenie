import { ApiError, type Task } from "@evergenie/sdk-web";
import { ActionBar, ChipsInput, InlineEdit, Markdown } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronDown } from "lucide-react";
import { type ReactNode, useRef, useState } from "react";
import { useAgents } from "../data/agents";
import { useSettled } from "../data/answers";
import { refreshAll } from "../data/live";
import { taskQuery, useShowCodes } from "../data/queries";
import { canLinkDetail } from "../data/siteLinks";
import { homeSearch, PRIMARY, type SiteRef, useSite, useSiteApi } from "../data/sites";
import { itemKey } from "../data/waiting";
import { t } from "../i18n/zh-TW";
import { durationText, parseIso, relativeAgo } from "../lib/format";
import { knownText } from "../lib/knownText";
import { useIsDesktop } from "../lib/media";
import { useNow } from "../lib/now";
import { isDelivery, statusKey, statusText, statusTone } from "../lib/taskText";
import type { PaneActions } from "./AnswerPanel";
import { AttachmentSection } from "./AttachmentSection";
import { DeleteTask } from "./DeleteTask";
import { GoalFact } from "./GoalFact";
import { Pill } from "./Pill";
import { SiteNotice, siteNoticeText } from "./SiteNotice";
import { SiteRefTag } from "./SiteTag";
import { LoadFailed, LoadingBlock, NotFoundBlock } from "./StateViews";
import { isTaskOpen, TaskActions } from "./TaskActions";
import { TaskAnswer, useTaskAsks } from "./TaskAnswer";
import { TaskEdit } from "./TaskEdit";
import { TaskStatus } from "./TaskStatus";

export interface TaskViewProps {
  id: string;
  /** 桌機「今天」頁的右欄：不重複頁首，動作列固定在底部、按鈕旁有鍵帽。 */
  embedded?: boolean;
  bind?: (actions: PaneActions | null) => void;
  onDone?: (kind: "accept" | "reject" | "chat" | "delete") => void;
  /** 就地回答了秘書的問句（DEV-156 U-05；右欄用來跳到下一件）。 */
  onAnswered?: (text: string) => void;
  paneExtra?: ReactNode;
  /**
   * 這張單在哪台（預設主伺服器；S3-7a 今天頁右欄、S3-7b 任務詳情）：資料向那台拿，驗收、退回、去聊、
   * 改狀態、刪除、掛 Goal、加連結都送那台，連結帶 ?home=。那台現在不是 ok 就整塊換成那台的 SiteNotice
   * （說清楚為什麼不能動），不退回主伺服器（FEN-28）。
   */
  site?: SiteRef;
}

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
 * 要你驗收時其他欄位收在「詳細」裡（DEV-156 U-16）：簡單模式預設收起，完整模式預設展開；按了照主人的意思。
 */
function Details({ children }: { children: ReactNode }) {
  const full = useShowCodes();
  const [toggled, setToggled] = useState<boolean | null>(null);
  const open = toggled ?? full;
  return (
    <section className="flex flex-col gap-5">
      <button
        type="button"
        aria-expanded={open}
        data-testid="task-details-toggle"
        onClick={() => setToggled(!open)}
        className="-mx-1 flex min-h-11 items-center gap-1.5 self-start rounded-lg px-1 text-sm font-semibold text-muted hover:text-fg focus-visible:outline-2 focus-visible:outline-focus"
      >
        <ChevronDown
          className={
            open ? "size-4 transition-transform" : "size-4 -rotate-90 transition-transform"
          }
          aria-hidden="true"
        />
        {t("task.review.details")}
      </button>
      {open ? children : null}
    </section>
  );
}

/** 單號的字首（例 SEC-、DEV-）：家的編號字首可以設（home.toml [ids]），連結照這張單自己的字首認。 */
function idPrefix(id: string): string {
  const m = /^([A-Z]+-)\d+$/.exec(id);
  return m?.[1] ?? "SEC-";
}

function LinkChip({ value, prefix, site }: { value: string; prefix: string; site: SiteRef }) {
  if (/^https?:\/\//i.test(value)) {
    return (
      // hg-allow-new-tab: 站外連結（http 開頭的才走這裡）
      <a href={value} target="_blank" rel="noopener noreferrer" className="text-accent underline">
        {value}
      </a>
    );
  }
  if (
    value.startsWith(prefix) &&
    /^\d+$/.test(value.slice(prefix.length)) &&
    canLinkDetail(site, "/tasks/$id")
  ) {
    return (
      <Link
        to="/tasks/$id"
        params={{ id: value }}
        search={homeSearch(site)}
        className="text-accent underline"
      >
        {value}
      </Link>
    );
  }
  return <>{value}</>;
}

// 一張任務單的閱讀版面（FEN-13；/tasks/$id 與桌機「今天」頁右欄共用）：
// 標題、狀態、負責秘書、需求、結果、驗證與驗證方式、秘書建議的結局、連結；動作依狀態（FEN-14）。
// 秘書在問你：問句下面就地回答（TaskAnswer，DEV-156 U-05）。要你驗收：最上面一張驗收卡，其他收進「詳細」（DEV-156 U-16）。
// 沒結束的單可以就地改標題、需求、驗收條件（TSK-29），也可以按「編輯」整張表單一次改（WEB-161）。
export function TaskView({
  id,
  embedded,
  bind,
  onDone,
  onAnswered,
  paneExtra,
  site = PRIMARY,
}: TaskViewProps) {
  const client = useQueryClient();
  const { list, view } = useAgents(site);
  const now = useNow();
  const siteApi = useSiteApi(site.key);
  const found = useSite(site.key);
  const query = useQuery({ ...taskQuery(id, site), enabled: siteApi !== undefined });
  const task: Task | undefined = query.data ?? undefined;
  const settled = useSettled().get(itemKey(site, id));
  const desktop = useIsDesktop();
  const navigate = useNavigate();
  const pane = useRef<HTMLDivElement>(null);
  // 秘書在問你：問句下面就地回答（DEV-156 U-05）。
  const asks = useTaskAsks(query.data ?? undefined, site);

  if (!siteApi) {
    // 那台現在用不了（試連中、連不上、被拒、太舊）：講那台的狀況，不畫一張動不了的單。
    return found && siteNoticeText(found) ? <SiteNotice site={found} /> : <LoadingBlock />;
  }

  if (!task) {
    if (query.isError) {
      const missing = query.error instanceof ApiError && query.error.code === "not_found";
      if (missing) {
        return (
          <NotFoundBlock
            title={t("task.notFound")}
            hint={t("task.notFound.hint")}
            backLabel={t("task.back")}
            to="/tasks"
          />
        );
      }
      return <LoadFailed what={t("page.task")} onRetry={() => void query.refetch()} />;
    }
    return <LoadingBlock />;
  }

  const agent = view(task.agent);
  const status = task.status ?? "doing";
  const open = isTaskOpen(task);
  const waiting = status === "waiting_user";
  const delivery = isDelivery(task);
  const verify = task.verify ? t(`task.verify.${task.verify}`) : "";
  const proposed = task.proposed ? knownText(statusKey, task.proposed) : "";
  // 白話寫秘書的名字，不寫「她」（男角色也有，DEV-156 U-16）。
  const who = agent.name || t("boot.fallbackName");
  const accepted = parseIso(task.accepted_at);
  const waitingSince = parseIso(task.waiting_since);

  // 標題、需求、驗收條件就地改（TSK-29）：結束的單伺服器不收，只顯示。
  const saveText = async (patch: { title?: string; body?: string; criteria?: string }) => {
    await siteApi.tasks.update({ id, ...patch });
    await refreshAll(client, site);
  };
  const textActions = { save: t("task.edit.save"), cancel: t("ui.cancel") };
  const criteriaNote = (
    <p className="mt-1 text-xs text-muted">
      {task.criteria_owner || task.goal ? t("task.criteria.owner") : t("task.criteria.agent")}
    </p>
  );

  const Heading = embedded ? "h2" : "h1";
  const title = (
    <Heading className="text-2xl leading-[1.3] font-bold tracking-[-0.01em] break-words">
      {task.title}
    </Heading>
  );
  // 頁首右邊「編輯」（WEB-161）：整張表單一次改；結束的單伺服器不收，不給這顆。
  const agentOptions = list
    .filter((a) => a.enabled)
    .map((a) => ({
      id: a.id ?? "",
      name: a.name ?? "",
      color: a.color,
      colorDark: a.color_dark,
      avatarUrl: a.avatar_url,
    }));
  const header = (
    <div className="flex flex-col gap-2.5">
      <div className="flex items-start justify-between gap-3">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[13px] text-muted">
          <Pill tone={statusTone[status]}>{statusText(task)}</Pill>
          <span className="font-mono">{task.id}</span>
          <span aria-hidden="true">·</span>
          <span>{agent.name || t("tasks.row.noAgent")}</span>
          {task.updated_at ? (
            <>
              <span aria-hidden="true">·</span>
              <span>{relativeAgo(task.updated_at, now)}</span>
            </>
          ) : null}
          <SiteRefTag site={site} />
        </div>
        {open ? (
          <TaskEdit
            task={task}
            agents={agentOptions}
            onSave={async (patch) => {
              await siteApi.tasks.update({ id, ...patch });
              // 存進去了才算數：之後刷新失敗不當成「沒存成功」（免得再按一次重送）。
              await refreshAll(client, site).catch(() => undefined);
            }}
          />
        ) : null}
      </div>
      {open ? (
        <InlineEdit
          label={t("task.section.title")}
          value={task.title ?? ""}
          emptyText=""
          editLabel={t("task.title.edit")}
          savedText={t("ui.saved")}
          requiredText={t("task.title.required")}
          onSave={(next) => saveText({ title: next })}
          display={title}
        />
      ) : (
        title
      )}
    </div>
  );

  const body = (
    <>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        <Fact label={t("task.field.agent")}>{agent.name || t("tasks.row.noAgent")}</Fact>
        <Fact label={t("task.field.status")}>{statusText(task)}</Fact>
        <Fact label={t("task.field.type")}>{t(`task.type.${task.type ?? "task"}`)}</Fact>
        <GoalFact
          id={task.goal}
          site={site}
          onChange={
            open
              ? async (goal) => {
                  await siteApi.tasks.update({ id, goal });
                  await refreshAll(client, site);
                }
              : undefined
          }
        />
        {/* 要你驗收時驗證與建議寫在最上面的驗收卡，這裡不重複。 */}
        {verify && !delivery ? <Fact label={t("task.field.verify")}>{verify}</Fact> : null}
        {proposed && !delivery ? (
          <Fact label={t("task.field.proposed", { name: who })}>{proposed}</Fact>
        ) : null}
        {waiting && waitingSince ? (
          <Fact label={t("task.field.waiting")}>
            {durationText((now.getTime() - waitingSince.getTime()) / 1000)}
          </Fact>
        ) : null}
        {accepted ? (
          <Fact label={t("task.field.accepted")}>
            {t("task.accepted", {
              who: task.accepted_by || "",
              when: relativeAgo(task.accepted_at, now),
            }).trim()}
          </Fact>
        ) : null}
      </div>

      {status === "waiting_user" && !delivery && (task.ask || asks.length > 0) ? (
        <section className="flex flex-col gap-1.5 rounded-[14px] bg-attention-soft px-4 py-3">
          <h2 className="text-sm font-semibold text-attention-fg">{t("task.section.ask")}</h2>
          {task.ask ? <Markdown>{task.ask}</Markdown> : null}
          {settled ? null : (
            <TaskAnswer
              task={task}
              asks={asks}
              size={embedded ? "pane" : "page"}
              site={site}
              bind={bind}
              onAnswered={onAnswered}
              className="mt-1.5"
            />
          )}
        </section>
      ) : null}

      {open ? (
        <InlineEdit
          label={t("task.section.body")}
          value={task.body ?? ""}
          multiline
          emptyText={t("task.empty.body")}
          editLabel={t("task.body.edit")}
          savedText={t("ui.saved")}
          actions={textActions}
          onSave={(next) => saveText({ body: next })}
          display={task.body ? <Markdown className="font-normal">{task.body}</Markdown> : undefined}
        />
      ) : (
        <Section title={t("task.section.body")}>
          {task.body ? (
            <Markdown>{task.body}</Markdown>
          ) : (
            <p className="text-sm text-muted">{t("task.empty.body")}</p>
          )}
        </Section>
      )}
      <AttachmentSection
        attachments={task.attachments ?? []}
        site={site}
        open={open}
        pasteScope={embedded ? pane : undefined}
        onAdd={async (urls) => {
          await siteApi.tasks.update({ id, add_attachments: urls });
          await refreshAll(client, site);
        }}
        onRemove={async (url) => {
          await siteApi.tasks.update({ id, remove_attachments: [url] });
          await refreshAll(client, site);
        }}
      />
      {task.type !== "ask" && open ? (
        <div className="flex flex-col">
          <InlineEdit
            label={t("task.section.criteria")}
            value={task.criteria ?? ""}
            multiline
            emptyText={t("task.empty.criteria")}
            editLabel={t("task.criteria.edit")}
            savedText={t("ui.saved")}
            actions={textActions}
            onSave={(next) => saveText({ criteria: next })}
            display={
              task.criteria ? (
                <Markdown className="font-normal">{task.criteria}</Markdown>
              ) : undefined
            }
          />
          {task.criteria ? criteriaNote : null}
        </div>
      ) : null}
      {task.type !== "ask" && !open ? (
        <Section title={t("task.section.criteria")}>
          {task.criteria ? (
            <>
              <Markdown>{task.criteria}</Markdown>
              {criteriaNote}
            </>
          ) : (
            <p className="text-sm text-muted">{t("task.empty.criteria")}</p>
          )}
        </Section>
      ) : null}
      {task.result && !delivery ? (
        <Section title={t("task.section.result")}>
          <Markdown>{task.result}</Markdown>
        </Section>
      ) : null}
      {task.verify_how && !delivery ? (
        <Section title={t("task.section.verifyHow")}>
          <Markdown>{task.verify_how}</Markdown>
        </Section>
      ) : null}
      {task.review_note && !delivery && status !== "done" ? (
        <Section title={t("task.section.reviewNote")}>
          <Markdown>{task.review_note}</Markdown>
        </Section>
      ) : null}
      <ChipsInput
        label={t("task.link.label")}
        hint={t("task.link.hint")}
        placeholder={t("task.link.example")}
        values={task.links ?? []}
        renderChip={(v) => <LinkChip value={v} prefix={idPrefix(id)} site={site} />}
        onAdd={async (value) => {
          await siteApi.tasks.update({ id, add_links: [value] });
          await refreshAll(client, site);
        }}
      />
      {open ? (
        <TaskStatus task={task} site={site} />
      ) : (
        <p className="text-sm text-muted">{t("task.closed")}</p>
      )}
      <DeleteTask
        id={id}
        site={site}
        onDeleted={() =>
          embedded ? onDone?.("delete") : void navigate({ to: "/tasks", replace: true })
        }
      />
    </>
  );

  const actions =
    open && !settled ? (
      <TaskActions
        task={task}
        site={site}
        size={embedded ? "pane" : "page"}
        keycaps={embedded}
        bind={bind}
        onDone={onDone}
        answering={asks.length > 0}
      />
    ) : null;
  // 要你驗收（DEV-156 U-16）：最上面一張卡——〈名字〉做完了什麼、怎麼確認的、〈名字〉建議怎麼結，下面就是驗收／退回；
  // 其他欄位收進「詳細」。詳情頁的動作在卡裡；桌機今天頁右欄照右欄的做法，動作留在固定在底部、有鍵帽的動作列。
  const inCard = delivery && !embedded;
  const review = delivery ? (
    <section
      data-testid="task-review-card"
      aria-labelledby="task-review-title"
      className="flex flex-col gap-3 rounded-[14px] bg-attention-soft px-4 py-3.5"
    >
      <div className="flex flex-col gap-1.5">
        <h2 id="task-review-title" className="text-[15px] font-semibold text-attention-fg">
          {t("task.review.done", { name: who })}
        </h2>
        {task.result ? (
          <Markdown>{task.result}</Markdown>
        ) : (
          <p className="text-sm text-muted">{t("task.empty.result")}</p>
        )}
      </div>
      {task.verify_how || verify ? (
        <div className="flex flex-col gap-1.5">
          <h3 className="text-sm font-semibold">{t("task.review.how")}</h3>
          {task.verify_how ? <Markdown>{task.verify_how}</Markdown> : null}
          {verify ? <p className="text-[13px] text-muted">{verify}</p> : null}
        </div>
      ) : null}
      {task.proposed ? (
        <p data-testid="task-review-proposed" className="text-sm font-medium">
          {t("task.review.proposed", {
            name: who,
            outcome: t(
              task.proposed === "cancelled"
                ? "task.review.outcome.cancelled"
                : "task.review.outcome.done",
            ),
          })}
        </p>
      ) : null}
      {inCard ? actions : null}
    </section>
  ) : null;
  const barActions = inCard ? null : actions;
  const settledNote = settled ? (
    <p
      role="status"
      className="rounded-[14px] bg-ok-soft px-4 py-3 text-[15px] font-semibold text-ok"
    >
      {settled.label}
    </p>
  ) : null;

  if (embedded) {
    return (
      <div ref={pane} className="flex h-full min-h-0 flex-col">
        <div className="flex min-h-0 flex-1 flex-col gap-[22px] overflow-y-auto px-9 pt-7 pb-6">
          {header}
          {settledNote}
          {review}
          {delivery ? <Details key={task.id}>{body}</Details> : body}
        </div>
        <ActionBar placement="pane" label={t("page.task")}>
          {barActions}
          {paneExtra}
        </ActionBar>
      </div>
    );
  }
  return (
    <article className="mx-auto flex w-full max-w-[720px] flex-col gap-5 px-4 pt-3 pb-4">
      {header}
      {settledNote}
      {review}
      {delivery ? <Details key={task.id}>{body}</Details> : body}
      {barActions ? (
        <ActionBar placement={desktop ? "inline" : "sticky"} label={t("page.task")}>
          {barActions}
        </ActionBar>
      ) : null}
    </article>
  );
}
