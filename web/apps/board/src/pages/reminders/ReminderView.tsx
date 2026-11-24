import {
  ApiError,
  type Cycle,
  type Difficulty,
  type Follow,
  type Reminder,
} from "@evergenie/sdk-web";
import {
  ActionBar,
  AgentPicker,
  InlineEdit,
  type InlineEditControl,
  Markdown,
  Segmented,
  type SegmentedOption,
} from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type ReactNode, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { AppWhenField } from "../../components/AppWhenField";
import { AttachmentSection } from "../../components/AttachmentSection";
import { GoalFact } from "../../components/GoalFact";
import { PageTitle } from "../../components/PageTitle";
import { Pill } from "../../components/Pill";
import { ItemSiteTag } from "../../components/SiteTag";
import { LoadFailed, LoadingBlock, NotFoundBlock } from "../../components/StateViews";
import { itemKey } from "../../data/acrossSites";
import { useAgents } from "../../data/agents";
import { useDeferredIds } from "../../data/deferred";
import { reminderQuery } from "../../data/queries";
import { markCancelled, markDoing, markDone, updateReminder } from "../../data/reminderActions";
import { homeSearch, PRIMARY, type Site, type SiteRef } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { fullTime, parseIso, relativeAgo } from "../../lib/format";
import { knownText } from "../../lib/knownText";
import { useIsDesktop } from "../../lib/media";
import { useNow } from "../../lib/now";
import { ReminderEdit } from "./ReminderEdit";
import { canDoing, SnoozeButton } from "./ReminderMenu";
import {
  collectsPoints,
  difficultyOf,
  followText,
  isLate,
  reminderStatusKey,
  reminderStatusTone,
} from "./reminderText";

const followKeys = ["none", "remind", "confirm", "check", "discuss"] as const satisfies Follow[];
const difficultyKeys = ["easy", "normal", "hard"] as const satisfies Difficulty[];
const cycleKeys = ["once", "daily", "weekly", "biweekly", "monthly"] as const satisfies Cycle[];

function errorOf(err: unknown): Error {
  return err instanceof ApiError ? err : err instanceof Error ? err : new Error(String(err));
}

// 時間的就地編輯器：WhenField＋儲存／取消（FEN-13）。
function AtEditor({ initial, control }: { initial: string; control: InlineEditControl }) {
  const [at, setAt] = useState<Date | null>(parseIso(initial));
  return (
    <div className="flex flex-col gap-3">
      <AppWhenField
        value={at}
        onChange={setAt}
        autoFocus
        onSubmit={() => at && void control.commit(at.toISOString())}
      />
      <div className="flex gap-2">
        <ActionButton
          size="card"
          tone="primary"
          busy={control.busy}
          disabled={at === null}
          onClick={() => at && void control.commit(at.toISOString())}
        >
          {t("when.apply")}
        </ActionButton>
        <ActionButton size="card" tone="quiet" onClick={control.cancel}>
          {t("ui.cancel")}
        </ActionButton>
      </div>
    </div>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="text-xs text-muted">{label}</span>
      <span className="text-sm font-medium break-words">{children}</span>
    </div>
  );
}

// 一個待辦的閱讀版面（FEN-13）：欄位點了就地編輯（reminders.update），也可以按「編輯」整張表單一次改（WEB-161）；
// 動作跟清單一樣（FEN-14）。
/**
 * 待辦的詳細內容。`site`＝待辦在哪台（預設主伺服器；S3-7b）：資料向那台拿、改動送那台、連結帶 ?home=。
 * 呼叫端要先確定那台能用（state ok）；給完整的 Site 才畫台名標籤。
 */
export function ReminderView({ id, site = PRIMARY }: { id: string; site?: SiteRef | Site }) {
  const client = useQueryClient();
  const { list, view } = useAgents(site);
  const now = useNow();
  const desktop = useIsDesktop();
  const query = useQuery(reminderQuery(id, site));
  const pendingIds = useDeferredIds();
  const r: Reminder | undefined = query.data ?? undefined;

  if (!r) {
    if (query.isError) {
      const missing = query.error instanceof ApiError && query.error.code === "not_found";
      if (missing) {
        return (
          <NotFoundBlock
            title={t("reminder.notFound")}
            hint={t("reminder.notFound.hint")}
            backLabel={t("reminder.back")}
            to="/reminders"
          />
        );
      }
      return <LoadFailed what={t("page.reminder")} onRetry={() => void query.refetch()} />;
    }
    return <LoadingBlock />;
  }

  const save = async (patch: Parameters<typeof updateReminder>[2]) => {
    try {
      await updateReminder(client, id, patch, site);
    } catch (err) {
      throw errorOf(err);
    }
  };

  const status = r.status ?? "todo";
  const ended = status === "done" || status === "cancelled";
  const queued = pendingIds.has(itemKey(site, id));
  const owner = view(r.owner);
  const follow: Follow = r.follow ?? "none";
  const cycle: Cycle = r.cycle ?? "once";
  const difficulty: Difficulty = difficultyOf(r);
  const atIso = parseIso(r.at) ? (r.at ?? "") : "";
  // 時間過了還沒做完（不含擱置）：狀態寫「過期了」，不寫「要你回答」（DEV-156 U-17）。
  const late = isLate(status, r.at, now);
  // 頁首膠囊與下面「狀態」欄同一個字（DEV-156 U-17 尾巴）。
  const statusWord = late ? t("reminder.status.late") : knownText(reminderStatusKey, status);
  const agents = list
    .filter((a) => a.enabled)
    .map((a) => ({
      id: a.id ?? "",
      name: a.name ?? "",
      color: a.color,
      colorDark: a.color_dark,
      avatarUrl: a.avatar_url,
    }));

  return (
    <article className="mx-auto flex w-full max-w-[720px] flex-col gap-5 px-4 pt-3 pb-4">
      <div className="flex flex-col gap-2.5">
        {/* 頁首右邊「編輯」（WEB-161）：整張表單一次改；結束的待辦不能改，不給這顆。 */}
        <div className="flex items-start justify-between gap-3">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[13px] text-muted">
            <Pill tone={late ? "danger" : reminderStatusTone[status]}>
              <span data-testid="reminder-status">{statusWord}</span>
            </Pill>
            {"state" in site ? <ItemSiteTag site={site} /> : null}
            <span className="font-mono">{r.id}</span>
            <span aria-hidden="true">·</span>
            <span>{owner.name}</span>
            {r.updated_at ? (
              <>
                <span aria-hidden="true">·</span>
                <span>{relativeAgo(r.updated_at, now)}</span>
              </>
            ) : null}
          </div>
          {ended ? null : <ReminderEdit reminder={r} agents={agents} site={site} />}
        </div>
        <PageTitle className="sr-only">{r.content}</PageTitle>
        <div>
          <InlineEdit
            label={t("reminder.field.content")}
            value={r.content ?? ""}
            emptyText={t("reminder.empty.content")}
            savedText={t("ui.saved")}
            requiredText={t("reminder.edit.required")}
            onSave={(next) => save({ content: next })}
            display={
              <span className="block text-2xl leading-[1.3] font-bold tracking-[-0.01em] break-words">
                {r.content}
              </span>
            }
          />
        </div>
      </div>

      {queued ? (
        <p
          role="status"
          className="rounded-[14px] bg-ok-soft px-4 py-3 text-[15px] font-semibold text-ok"
        >
          {t("reminder.queued")}
        </p>
      ) : null}

      <div className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3">
        <InlineEdit
          label={t("reminder.field.at")}
          value={atIso}
          emptyText={t("reminder.empty.at")}
          savedText={t("ui.saved")}
          display={
            <span className={atIso ? "" : "text-muted"}>
              {atIso ? fullTime(atIso, now) : t("reminder.empty.at")}
            </span>
          }
          onSave={(next) => save({ at: next })}
          renderEditor={(control) => <AtEditor initial={atIso} control={control} />}
        />
        <InlineEdit
          label={t("reminder.field.owner")}
          value={r.owner ?? ""}
          emptyText={t("reminder.empty.content")}
          savedText={t("ui.saved")}
          display={<span>{owner.name}</span>}
          onSave={(next) => save({ owner: next })}
          renderEditor={(control) => (
            <AgentPicker
              label={t("reminder.field.owner")}
              value={r.owner ?? ""}
              agents={agents}
              onChange={(next) => void control.commit(next)}
            />
          )}
        />
        <InlineEdit
          label={t("reminder.field.follow")}
          value={follow}
          emptyText=""
          savedText={t("ui.saved")}
          display={<span>{followText(follow)}</span>}
          onSave={(next) => save({ follow: next as Follow })}
          renderEditor={(control) => (
            <Segmented
              wrap
              ariaLabel={t("reminder.field.follow")}
              value={follow}
              onChange={(next) => void control.commit(next)}
              options={followKeys.map<SegmentedOption<Follow>>((k) => ({
                value: k,
                label: t(`follow.opt.${k}`),
              }))}
            />
          )}
        />
        <InlineEdit
          label={t("reminder.field.cycle")}
          value={cycle}
          emptyText=""
          savedText={t("ui.saved")}
          display={<span>{t(`cycle.${cycle}`)}</span>}
          onSave={(next) => save({ cycle: next as Cycle })}
          renderEditor={(control) => (
            <Segmented
              ariaLabel={t("reminder.field.cycle")}
              value={cycle}
              onChange={(next) => void control.commit(next)}
              options={cycleKeys.map<SegmentedOption<Cycle>>((k) => ({
                value: k,
                label: t(`cycle.${k}`),
              }))}
            />
          )}
        />
        {/* 難度只對會集點的跟進方式有意義（RMD-26，FEN-16 漸進揭露）。 */}
        {collectsPoints({ follow }) ? (
          <InlineEdit
            label={t("reminder.field.difficulty")}
            value={difficulty}
            emptyText=""
            savedText={t("ui.saved")}
            display={
              <span>
                <span>{t(`difficulty.${difficulty}`)}</span>
                {r.points ? (
                  <span data-testid="reminder-points" className="ml-1.5 text-muted">
                    {t("reminder.points.sr")} {t("reminder.points", { n: r.points })}
                  </span>
                ) : null}
              </span>
            }
            onSave={(next) => save({ difficulty: next as Difficulty })}
            renderEditor={(control) => (
              <Segmented
                ariaLabel={t("reminder.field.difficulty")}
                value={difficulty}
                onChange={(next) => void control.commit(next)}
                options={difficultyKeys.map<SegmentedOption<Difficulty>>((k) => ({
                  value: k,
                  label: t(`difficulty.${k}`),
                }))}
              />
            )}
          />
        ) : null}
        {follow === "check" ? (
          <InlineEdit
            label={t("reminder.field.check")}
            value={r.check ?? ""}
            emptyText={t("reminder.empty.check")}
            savedText={t("ui.saved")}
            placeholder={t("quick.check.example")}
            onSave={(next) => save({ check: next })}
            className="col-span-2 sm:col-span-3"
          />
        ) : null}
        <Fact label={t("reminder.field.status")}>
          <span data-testid="reminder-status-field">{statusWord}</span>
        </Fact>
        {r.room ? (
          <Fact label={t("reminder.field.room")}>
            <Link
              to="/chat/$session"
              params={{ session: r.room }}
              search={homeSearch(site)}
              className="text-accent underline"
            >
              {r.room}
            </Link>
          </Fact>
        ) : null}
        {r.source ? <Fact label={t("reminder.field.source")}>{r.source}</Fact> : null}
        <GoalFact
          id={r.goal}
          site={site}
          onChange={
            ended
              ? undefined
              : async (goal) => {
                  await updateReminder(client, id, { goal }, site);
                }
          }
        />
      </div>

      {/* 描述（RMD-23）：點了就地改（多行，⌘/Ctrl+Enter 存）；結束的待辦照規則不能改，只顯示。舊資料（以前快速輸入塞進來的
          「想聊什麼」與圖片行）一樣用 Markdown 畫出來。 */}
      {ended ? (
        r.body ? (
          <section className="flex flex-col gap-1.5">
            <h2 className="text-sm font-semibold">{t("reminder.body")}</h2>
            <Markdown shareBase={site.base}>{r.body}</Markdown>
          </section>
        ) : null
      ) : (
        <InlineEdit
          label={t("reminder.body")}
          value={r.body ?? ""}
          multiline
          emptyText={t("reminder.empty.body")}
          editLabel={t("reminder.body.edit")}
          savedText={t("ui.saved")}
          placeholder={t("reminder.body.placeholder")}
          onSave={(next) => save({ body: next })}
          display={
            r.body ? (
              <Markdown shareBase={site.base} className="font-normal">
                {r.body}
              </Markdown>
            ) : undefined
          }
        />
      )}

      <AttachmentSection
        attachments={r.attachments ?? []}
        site={site}
        open={!ended}
        onAdd={(urls) => updateReminder(client, id, { add_attachments: urls }, site)}
        onRemove={(url) => updateReminder(client, id, { remove_attachments: [url] }, site)}
      />

      {ended ? (
        <p className="text-sm text-muted">
          {t("reminder.ended")}
          {parseIso(r.closed_at)
            ? ` ${t("reminder.closedAt", { when: relativeAgo(r.closed_at, now) })}`
            : ""}
        </p>
      ) : queued ? null : (
        <ActionBar placement={desktop ? "inline" : "sticky"} label={t("page.reminder")}>
          <ActionButton size="page" tone="primary" onClick={() => markDone(client, r, site)}>
            {t("reminder.done")}
          </ActionButton>
          {canDoing(r) ? (
            <ActionButton
              size="page"
              tone="secondary"
              onClick={() => void markDoing(client, r, site)}
            >
              {t("reminder.doing")}
            </ActionButton>
          ) : null}
          <SnoozeButton reminder={r} site={site} />
          <ActionButton size="page" tone="quiet" onClick={() => markCancelled(client, r, site)}>
            {t("reminder.cancel")}
          </ActionButton>
        </ActionBar>
      )}
    </article>
  );
}
