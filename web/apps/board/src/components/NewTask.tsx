import { ApiError, type TaskType } from "@evergenie/sdk-web";
import { cn, Field, Segmented, Sheet, Spinner, showToast } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowUp, ChevronDown, Plus } from "lucide-react";
import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { useAgentFilter } from "../data/agentFilter";
import { activeGoalsQuery } from "../data/queries";
import { homeSearch } from "../data/sites";
import { createTask } from "../data/taskCreate";
import { t } from "../i18n/zh-TW";
import { useDraft } from "../lib/draft";
import { HIT_44 } from "../lib/hit";
import { lastTaskAgentIn, writeLastTaskAgent } from "../lib/lastAgent";
import { useIsDesktop } from "../lib/media";
import { ActionButton } from "./ActionButton";
import { AttachChip, FileDraftList, useFileDrafts } from "./FileDrafts";
import { enabledAgentKeys, SiteAgentPicker, useSiteTarget } from "./SitePick";
import { SitePickChip, siteName } from "./SiteTag";

const FIELDS = ["title", "agent", "type", "body", "goal", "criteria"];
const inputClass =
  "min-h-11 w-full rounded-xl border border-border-strong bg-bg px-3 text-base text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus";
const chipBase = "inline-flex items-center rounded-full px-2.5 py-1 text-[13px] leading-snug";
// 膠囊視覺上高 26～28，點擊範圍用 ::after 補到 44（FEN-7）。
const hitArea = `${HIT_44} focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus`;

export interface NewTaskProps {
  /** 開好之後（任務單頁用它把檢視切到看得到新單的那一個）。 */
  onCreated?: () => void;
  /** 一進來就聚焦標題（從「今天」的「交代任務」來）。 */
  autoFocus?: boolean;
}

// 任務單頁頂端的一行開單（FEN-15、FEN-17、FEN-24；REQ-186）：打一行、選一位秘書、Enter 就開單。
// 類型、詳細說明、Goal 收在「更多選項」；手機用底部抽屜，桌機在輸入框下方展開。新單是待做，到單上按「開始做」才開工（DEV-124）。
export function NewTask({ onCreated, autoFocus }: NewTaskProps) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const desktop = useIsDesktop();
  const inputId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [title, setTitle, clearTitle] = useDraft("eg.draft.tasks.new.title");
  const [body, setBody, clearBody] = useDraft("eg.draft.tasks.new.body");
  const [criteria, setCriteria, clearCriteria] = useDraft("eg.draft.tasks.new.criteria");
  // 負責秘書預設：自己點的 > 秘書篩選剛好選一位 > 上一次開單給的那位；都沒有才要自己選。
  // 秘書用 agentKey（別台是「台名/職位 id」）：單開在那位秘書所在的台（S3-7b）。
  const { one } = useAgentFilter();
  const [type, setType] = useState<TaskType>("task");
  // 掛的 Goal 記住是哪台的：台換了（自己換，或預設秘書那台連上了）就不帶過去（Goal 不跨台）。
  const [goalOf, setGoalOf] = useState({ site: "", id: "" });
  const [panel, setPanel] = useState<null | "agent" | "more">(null);
  const [sheet, setSheet] = useState(false);
  const [busy, setBusy] = useState(false);
  const [tried, setTried] = useState(false);
  const [fieldError, setFieldError] = useState<Record<string, string>>({});
  useEffect(() => {
    if (autoFocus) inputRef.current?.focus();
  }, [autoFocus]);
  const target = useSiteTarget((agents) => one || lastTaskAgentIn(enabledAgentKeys(agents)));
  const agent = target.agent;
  const agentName = target.agentName;
  const goal = goalOf.site === target.site.key ? goalOf.id : "";
  const setGoal = (id: string) => setGoalOf({ site: target.site.key, id });
  // 掛的 Goal 要在同一台（Goal 不跨台）。
  const goals = useQuery(activeGoalsQuery(target.site));
  // 附件（TSK-28）：選檔或在標題、詳細說明裡貼上截圖；先傳到秘書那台，開單時一起帶進單。
  const drafts = useFileDrafts(target.site);
  const emptyTitle = title.trim() === "";
  const needAgent = agent === "";

  const reset = () => {
    clearTitle();
    clearBody();
    clearCriteria();
    setType("task");
    setGoal("");
    setPanel(null);
    setSheet(false);
    setTried(false);
    setFieldError({});
    drafts.clear();
  };

  const submit = async () => {
    if (busy) return;
    setTried(true);
    if (emptyTitle) return;
    if (needAgent) {
      // 還沒選秘書：把選擇打開（手機是抽屜），不送。
      openPanel("agent");
      return;
    }
    if (!target.ready) {
      // 秘書那台現在用不了：不送，也不改建到主伺服器。
      showToast(t("site.create.blocked", { name: siteName(target.site) }));
      return;
    }
    if (drafts.blocked) {
      // 附件還在傳或傳失敗：等它（或拿掉、重試），不開一張少了附件的單。
      setFieldError({ _: t("tasks.new.attach.wait") });
      return;
    }
    setBusy(true);
    setFieldError({});
    const site = target.site;
    try {
      const attachments = await drafts.urlsFor(site);
      await createTask(
        client,
        {
          title,
          agent: target.agentId,
          type,
          body,
          goal,
          criteria: type === "task" ? criteria : "",
          attachments,
        },
        agentName,
        (id) => void navigate({ to: "/tasks/$id", params: { id }, search: homeSearch(site) }),
        site,
      );
      writeLastTaskAgent(agent);
      reset();
      onCreated?.();
    } catch (err) {
      const field = err instanceof ApiError ? err.field : undefined;
      if (field && FIELDS.includes(field)) {
        setFieldError({ [field]: err instanceof ApiError ? err.message : "" });
        if (field !== "title") openPanel("more");
      } else {
        const reason = err instanceof ApiError ? err.message : t("settings.error.generic");
        setFieldError({ _: t("tasks.new.failed", { reason }) });
      }
    } finally {
      setBusy(false);
    }
  };

  const openPanel = (next: "agent" | "more") => {
    if (desktop) setPanel((cur) => (cur === next ? null : next));
    else setSheet(true);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== "Enter") return;
    // 輸入法選字中的 Enter 是確認選字，不是送出。
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    e.preventDefault();
    void submit();
  };
  const onBodyKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      void submit();
    }
  };

  const typeField = (
    <div className="flex flex-col gap-1.5">
      <span className="text-[13px] font-semibold">{t("tasks.new.type")}</span>
      <Segmented
        ariaLabel={t("tasks.new.type")}
        value={type}
        onChange={setType}
        options={[
          { value: "task", label: t("tasks.new.type.task") },
          { value: "ask", label: t("tasks.new.type.ask") },
        ]}
      />
      <p className="text-xs text-muted">{t("tasks.new.type.hint")}</p>
    </div>
  );
  const bodyField = (
    <Field label={t("tasks.new.body")} hint={t("tasks.new.body.hint")} error={fieldError.body}>
      {(control) => (
        <textarea
          {...control}
          value={body}
          onChange={(e) => setBody(e.target.value)}
          onKeyDown={onBodyKeyDown}
          onPaste={drafts.onPaste}
          rows={3}
          autoComplete="off"
          enterKeyHint="enter"
          className={cn(inputClass, "py-2")}
        />
      )}
    </Field>
  );
  // 驗收條件（法律第 1.4 條）：寫了，秘書照它驗過就自己結；不寫，秘書補一條，做完等你結。問答類不用。
  const criteriaField =
    type === "task" ? (
      <Field
        label={t("tasks.new.criteria")}
        hint={t("tasks.new.criteria.hint")}
        error={fieldError.criteria}
      >
        {(control) => (
          <textarea
            {...control}
            value={criteria}
            onChange={(e) => setCriteria(e.target.value)}
            onKeyDown={onBodyKeyDown}
            rows={2}
            autoComplete="off"
            enterKeyHint="enter"
            className={cn(inputClass, "py-2")}
          />
        )}
      </Field>
    ) : null;
  const goalField = (
    <Field label={t("tasks.new.goal")} hint={t("tasks.new.goal.hint")} error={fieldError.goal}>
      {(control) => (
        <select
          {...control}
          value={goal}
          onChange={(e) => setGoal(e.target.value)}
          className={inputClass}
        >
          <option value="">{t("tasks.new.goal.none")}</option>
          {(goals.data ?? []).map((g) => (
            <option key={g.id} value={g.id}>
              {g.id} {g.title}
            </option>
          ))}
        </select>
      )}
    </Field>
  );
  const agentError =
    needAgent && (tried || (!desktop && sheet)) ? t("tasks.new.agent.required") : "";
  const agentField = (
    <div className="flex flex-col gap-1.5">
      <span className="text-[13px] font-semibold">{t("tasks.new.agent")}</span>
      <SiteAgentPicker
        label={t("tasks.new.agent")}
        value={agent}
        agents={target.agents}
        onChange={(key) => {
          target.setAgent(key);
          if (desktop) setPanel(null);
        }}
      />
      {agentError || fieldError.agent ? (
        <p role="alert" className="text-xs text-danger">
          {agentError || fieldError.agent}
        </p>
      ) : null}
    </div>
  );

  const SendIcon = busy ? Spinner : ArrowUp;
  const moreOpen = panel === "more";
  return (
    <div className="flex flex-col">
      <div className="flex flex-col rounded-[18px] border border-border bg-surface shadow-[var(--shadow-card)]">
        <div className="flex items-center gap-1.5 pr-1.5 pl-3.5">
          <Plus className="size-[18px] shrink-0 text-accent" strokeWidth={2} aria-hidden="true" />
          <label htmlFor={inputId} className="sr-only">
            {t("tasks.new.label")}
          </label>
          <input
            id={inputId}
            ref={inputRef}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            onKeyDown={onKeyDown}
            onPaste={drafts.onPaste}
            placeholder={t("tasks.new.placeholder")}
            enterKeyHint="send"
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            aria-invalid={tried && emptyTitle ? true : undefined}
            className="h-[50px] min-w-0 flex-1 border-0 bg-transparent text-base text-fg outline-none placeholder:text-muted"
          />
          <button
            type="button"
            aria-label={t("tasks.new.send")}
            disabled={emptyTitle || busy || drafts.blocked}
            onClick={() => void submit()}
            className={cn(
              "flex size-11 shrink-0 items-center justify-center rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
              emptyTitle ? "bg-surface-2 text-muted" : "bg-accent text-accent-fg",
            )}
          >
            <SendIcon className="size-5" aria-hidden="true" />
          </button>
        </div>
        <FileDraftList drafts={drafts} className="border-t border-border px-3.5 py-2.5" />
        <div className="flex flex-wrap items-center gap-1.5 border-t border-border px-3.5 py-2.5">
          <button
            type="button"
            aria-expanded={desktop ? panel === "agent" : sheet}
            aria-label={
              agent ? t("tasks.new.agent.edit", { name: agentName }) : t("tasks.new.agent.pick")
            }
            onClick={() => openPanel("agent")}
            className={cn(
              hitArea,
              chipBase,
              agent
                ? "bg-accent-soft font-medium text-accent"
                : "border border-dashed border-border-strong text-muted",
            )}
          >
            {agent ? t("tasks.new.agent.chip", { name: agentName }) : t("tasks.new.agent.pick")}
          </button>
          <SitePickChip
            className={hitArea}
            sites={target.sites}
            value={target.site.key}
            onChange={(key) => {
              target.setSite(key);
            }}
          />
          <button
            type="button"
            aria-expanded={desktop ? moreOpen : sheet}
            onClick={() => openPanel("more")}
            className={cn(hitArea, chipBase, "gap-0.5 bg-surface-2 font-medium text-muted")}
          >
            {t("tasks.new.more")}
            <ChevronDown
              className={cn("size-3.5 transition-transform", moreOpen && "rotate-180")}
              aria-hidden="true"
            />
          </button>
          <AttachChip drafts={drafts} />
          <span className="ml-auto text-xs text-muted">{t("tasks.new.enter")}</span>
        </div>
        {tried && emptyTitle ? (
          <p role="alert" className="border-t border-border px-3.5 py-2.5 text-[13px] text-danger">
            {t("tasks.new.title.required")}
          </p>
        ) : null}
        {fieldError.title || fieldError._ ? (
          <p role="alert" className="border-t border-border px-3.5 py-2.5 text-[13px] text-danger">
            {fieldError.title || fieldError._}
          </p>
        ) : null}
        {desktop && panel === "agent" ? (
          <div className="hg-rise flex flex-col gap-2 border-t border-border px-3.5 py-3">
            {agentField}
          </div>
        ) : null}
        {desktop && panel === "more" ? (
          <div className="hg-rise flex flex-col gap-4 border-t border-border px-3.5 py-3">
            {typeField}
            {criteriaField}
            {bodyField}
            {goalField}
          </div>
        ) : null}
      </div>
      <p className="mx-1 mt-1.5 text-xs text-muted">{t("tasks.new.note")}</p>

      {!desktop ? (
        <Sheet open={sheet} onOpenChange={setSheet} title={t("tasks.new.title")}>
          <div className="flex max-h-[calc(var(--hg-visible-h)*0.62)] flex-col gap-4 overflow-y-auto px-5 pb-3">
            <Field
              label={t("tasks.new.sheet.title")}
              error={tried && emptyTitle ? t("tasks.new.title.required") : fieldError.title}
            >
              {(control) => (
                <input
                  {...control}
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  onPaste={drafts.onPaste}
                  placeholder={t("tasks.new.placeholder")}
                  autoComplete="off"
                  enterKeyHint="next"
                  className={inputClass}
                />
              )}
            </Field>
            {agentField}
            {typeField}
            {criteriaField}
            {bodyField}
            <div className="flex flex-col gap-1.5">
              <span className="text-[13px] font-semibold">{t("tasks.new.attach")}</span>
              <FileDraftList drafts={drafts} />
              <div>
                <AttachChip drafts={drafts} testId="task-new-file-sheet" />
              </div>
            </div>
            {goalField}
            {fieldError._ ? (
              <p role="alert" className="text-[13px] text-danger">
                {fieldError._}
              </p>
            ) : null}
          </div>
          <div className="flex flex-wrap justify-end gap-2 border-t border-border px-5 pt-3">
            <ActionButton tone="outline" onClick={() => setSheet(false)} disabled={busy}>
              {t("tasks.new.cancel")}
            </ActionButton>
            <ActionButton
              tone="primary"
              busy={busy}
              disabled={emptyTitle || needAgent || drafts.blocked}
              onClick={() => void submit()}
            >
              {t("tasks.new.create")}
            </ActionButton>
          </div>
        </Sheet>
      ) : null}
    </div>
  );
}
