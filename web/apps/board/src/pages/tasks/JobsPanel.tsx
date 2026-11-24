import { ApiError, api, type Job } from "@evergenie/sdk-web";
import {
  AgentPicker,
  Avatar,
  Card,
  cn,
  EmptyState,
  Field,
  InlineEdit,
  showToast,
} from "@evergenie/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { ChevronRight, Plus } from "lucide-react";
import { type FormEvent, useMemo, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { FilterEmpty } from "../../components/FilterEmpty";
import { FormDialog } from "../../components/FormDialog";
import { Pill } from "../../components/Pill";
import { LoadFailed, LoadingBlock, NotFoundBlock } from "../../components/StateViews";
import { Toggle } from "../../components/Toggle";
import { countByAgent, matchAgent, useAgentFilter } from "../../data/agentFilter";
import { useAgents } from "../../data/agents";
import { usePublishPageCounts } from "../../data/pageCounts";
import { t } from "../../i18n/zh-TW";
import { describeCrons, describeJobTime } from "../../lib/cron";
import { timeLabel } from "../../lib/format";
import { useNow } from "../../lib/now";
import { reasonOf } from "../../lib/reason";
import { JobLastRun } from "./JobLastRun";
import { groupJobs, jobsQuery, lastRunView, newJobId, sourceKind } from "./jobs";
import { ScheduleEditor } from "./ScheduleEditor";

const inputClass =
  "min-h-11 w-full rounded-xl border border-border-strong bg-bg px-3 text-base text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus";

/** 任務單 → 排程（REQ-18）：沒有 ?job= 是清單，有就是那一個排程的詳情（重新整理與上一頁回到同一個畫面）。 */
export function JobsPanel() {
  const { job } = useSearch({ strict: false }) as { job?: string };
  useJobCounts();
  return job ? <JobDetail key={job} id={job} /> : <JobList />;
}

/** 秘書篩選列的件數（「全部 N 件」、頭像上的徽章）：清單與詳情都報，打開一個排程時不會變成「— 件」（WEB-97）。 */
function useJobCounts() {
  const query = useQuery(jobsQuery);
  const live = useMemo(() => (query.data ?? []).filter((j) => !j.archived), [query.data]);
  const counts = useMemo(() => countByAgent(live, (j) => j.agent), [live]);
  usePublishPageCounts(query.data === undefined ? null : { counts, total: live.length });
}

// ── 清單 ──────────────────────────────────────────────────────────────────

function useToggleEnabled() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      api.scheduler.setJob({ id, enabled }),
    // 先改畫面（FEN-22）；失敗就還原並說原因。
    onMutate: async ({ id, enabled }) => {
      await client.cancelQueries({ queryKey: jobsQuery.queryKey });
      const before = client.getQueryData<Job[]>(jobsQuery.queryKey);
      client.setQueryData<Job[]>(jobsQuery.queryKey, (jobs) =>
        jobs?.map((j) => (j.id === id ? { ...j, enabled } : j)),
      );
      return { before };
    },
    onError: (err, _vars, ctx) => {
      client.setQueryData(jobsQuery.queryKey, ctx?.before);
      showToast(t("jobs.toggle.failed", { reason: reasonOf(err) }));
    },
    onSettled: () => void client.invalidateQueries({ queryKey: jobsQuery.queryKey }),
  });
}

function JobRow({ job, now }: { job: Job; now: Date }) {
  const toggle = useToggleEnabled();
  const name = job.name || job.id || "";
  const next = job.enabled && job.next_run ? timeLabel(job.next_run, now) : "";
  return (
    <li className="flex min-h-[64px] items-center gap-1 border-b border-border py-1 pr-1 pl-4 last:border-b-0">
      <Link
        to="/tasks/jobs"
        search={{ job: job.id }}
        aria-label={t("jobs.row.open", { name })}
        className="flex min-h-11 min-w-0 flex-1 items-center gap-2 rounded-lg py-1.5 focus-visible:outline-2 focus-visible:outline-focus"
      >
        <span className="min-w-0 flex-1">
          <span
            className={cn("block truncate text-[15px] font-medium", !job.enabled && "text-muted")}
          >
            {name}
          </span>
          <span className="block text-xs break-words text-muted">
            {describeJobTime(job) || t("jobs.row.noTime")}
            {next ? ` · ${t("jobs.row.next", { at: next })}` : ""}
            {job.enabled ? "" : ` · ${t("jobs.row.off")}`}
          </span>
          <JobLastRun job={job} now={now} />
        </span>
        <ChevronRight className="size-4 shrink-0 text-muted" aria-hidden="true" />
      </Link>
      <Toggle
        checked={job.enabled}
        label={t("jobs.row.toggle", { name })}
        onChange={(enabled) => toggle.mutate({ id: job.id ?? "", enabled })}
      />
    </li>
  );
}

function JobList() {
  const now = useNow();
  const { all: list, view } = useAgents();
  const query = useQuery(jobsQuery);
  const { picked } = useAgentFilter();
  const [creating, setCreating] = useState(false);
  const roster = useMemo(() => list.filter((a) => a.enabled).map((a) => a.id ?? ""), [list]);
  const live = useMemo(() => (query.data ?? []).filter((j) => !j.archived), [query.data]);
  const shown = useMemo(() => live.filter((j) => matchAgent(picked, j.agent)), [live, picked]);
  const groups = useMemo(() => groupJobs(shown, roster), [shown, roster]);

  return (
    <div className="flex flex-col gap-4">
      <ActionButton
        tone="primary"
        size="page"
        onClick={() => setCreating(true)}
        className="self-start"
      >
        <Plus className="size-[18px]" aria-hidden="true" />
        {t("jobs.new")}
      </ActionButton>
      {query.data === undefined ? (
        query.isError ? (
          <LoadFailed what={t("tasks.tab.jobs")} onRetry={() => void query.refetch()} />
        ) : (
          <LoadingBlock />
        )
      ) : picked.length > 0 && shown.length === 0 ? (
        <FilterEmpty />
      ) : live.length === 0 ? (
        <EmptyState title={t("jobs.empty")} hint={t("jobs.empty.hint")} />
      ) : (
        groups.map((g) => {
          const agent = view(g.agent);
          return (
            <section key={g.agent || "system"} className="flex flex-col gap-2">
              <h2 className="mx-1 flex items-center gap-2 text-[13px] font-semibold text-muted">
                {g.agent ? (
                  <Avatar
                    name={agent.name}
                    color={agent.color}
                    colorDark={agent.colorDark}
                    src={agent.avatarUrl}
                    size={20}
                  />
                ) : null}
                {g.agent ? agent.name : t("jobs.group.system")}
              </h2>
              <Card className="overflow-hidden">
                <ul aria-label={t("jobs.list")} className="m-0 list-none p-0">
                  {g.jobs.map((j) => (
                    <JobRow key={j.id} job={j} now={now} />
                  ))}
                </ul>
              </Card>
            </section>
          );
        })
      )}
      <NewJobDialog open={creating} onClose={() => setCreating(false)} />
    </div>
  );
}

// ── 新增 ──────────────────────────────────────────────────────────────────

function NewJobDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <FormDialog open={open} title={t("jobs.new.title")} body={t("jobs.new.body")} onClose={onClose}>
      {open ? <NewJobForm onClose={onClose} /> : null}
    </FormDialog>
  );
}

function NewJobForm({ onClose }: { onClose: () => void }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const { all: list } = useAgents();
  const { one } = useAgentFilter();
  const agents = list.filter((a) => a.enabled);
  const [agent, setAgent] = useState(() => (one && !one.includes("/") ? one : ""));
  const [name, setName] = useState("");
  const [body, setBody] = useState("");
  const [cron, setCron] = useState<string[]>(["0 9 * * *"]);
  const [tried, setTried] = useState(false);
  const [busy, setBusy] = useState(false);
  const [server, setServer] = useState<{ field?: string; text: string } | null>(null);

  const errors = {
    agent: agent === "" ? t("jobs.new.agent.required") : "",
    name: name.trim() === "" ? t("jobs.new.name.required") : "",
    body: body.trim() === "" ? t("jobs.new.body.required") : "",
    cron: cron.length === 0 ? t("jobs.time.required") : "",
  };
  const serverFor = (field: string) => (server?.field === field ? server.text : "");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setTried(true);
    if (Object.values(errors).some(Boolean)) return;
    setBusy(true);
    setServer(null);
    try {
      const res = await api.scheduler.saveJob({
        job: {
          id: newJobId(agent, name.trim(), Date.now()),
          agent,
          name: name.trim(),
          body: body.trim(),
          cron,
          enabled: true,
          archived: false,
          source: "user",
        },
      });
      await client.invalidateQueries({ queryKey: jobsQuery.queryKey });
      showToast(t("jobs.new.toast", { name: res.job?.name ?? name.trim() }));
      onClose();
      if (res.job?.id) void navigate({ to: "/tasks/jobs", search: { job: res.job.id } });
    } catch (err) {
      const field = err instanceof ApiError ? err.field : undefined;
      // 撞 id（同名排程）寫在名稱欄下（FEN-19）；其他對得到欄位的寫在該欄。
      if (err instanceof ApiError && err.code === "conflict") {
        setServer({ field: "name", text: err.message });
      } else if (field && ["agent", "name", "body", "cron"].includes(field)) {
        setServer({ field, text: reasonOf(err) });
      } else {
        setServer({ text: reasonOf(err) });
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={(e) => void submit(e)} noValidate className="flex flex-col">
      <div className="flex max-h-[calc(var(--hg-visible-h)*0.6)] flex-col gap-4 overflow-y-auto px-5 pb-3">
        <div className="flex flex-col gap-1.5">
          <span className="text-[13px] font-semibold">{t("jobs.new.agent")}</span>
          <AgentPicker
            label={t("jobs.new.agent")}
            value={agent}
            onChange={setAgent}
            agents={agents.map((a) => ({
              id: a.id ?? "",
              name: a.name ?? "",
              color: a.color,
              colorDark: a.color_dark,
              avatarUrl: a.avatar_url,
            }))}
          />
          {(tried && errors.agent) || serverFor("agent") ? (
            <p role="alert" className="text-xs text-danger">
              {errors.agent || serverFor("agent")}
            </p>
          ) : null}
        </div>
        <Field
          label={t("jobs.new.name")}
          hint={t("jobs.new.name.hint")}
          error={(tried && errors.name) || serverFor("name")}
        >
          {(control) => (
            <input
              {...control}
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (server?.field === "name") setServer(null);
              }}
              autoComplete="off"
              enterKeyHint="next"
              className={inputClass}
            />
          )}
        </Field>
        <Field
          label={t("jobs.new.bodyLabel")}
          hint={t("jobs.new.bodyLabel.hint")}
          error={(tried && errors.body) || serverFor("body")}
        >
          {(control) => (
            <textarea
              {...control}
              value={body}
              onChange={(e) => setBody(e.target.value)}
              rows={4}
              autoComplete="off"
              className={cn(inputClass, "py-2")}
            />
          )}
        </Field>
        <div className="flex flex-col gap-1.5">
          <span className="text-[13px] font-semibold">{t("jobs.field.time")}</span>
          <ScheduleEditor
            value={cron}
            onChange={setCron}
            error={(tried && errors.cron) || serverFor("cron")}
          />
        </div>
        {server && !server.field ? (
          <p role="alert" className="text-[13px] text-danger">
            {t("jobs.new.failed", { reason: server.text })}
          </p>
        ) : null}
      </div>
      <div className="flex flex-wrap justify-end gap-2 border-t border-border px-5 pt-3 pb-1">
        <ActionButton tone="outline" onClick={onClose} disabled={busy}>
          {t("jobs.new.cancel")}
        </ActionButton>
        <ActionButton type="submit" tone="primary" busy={busy}>
          {t("jobs.new.create")}
        </ActionButton>
      </div>
    </form>
  );
}

// ── 詳情 ──────────────────────────────────────────────────────────────────

function TimeEditorBox({
  initial,
  control,
}: {
  initial: readonly string[];
  control: { commit: (next: string) => Promise<void>; cancel: () => void; busy: boolean };
}) {
  const [specs, setSpecs] = useState<string[]>([...initial]);
  return (
    <div className="flex flex-col gap-3">
      <ScheduleEditor value={initial} onChange={setSpecs} />
      <div className="flex flex-wrap gap-2">
        <ActionButton
          tone="primary"
          busy={control.busy}
          disabled={specs.length === 0}
          onClick={() => void control.commit(specs.join("\n"))}
        >
          {t("jobs.time.save")}
        </ActionButton>
        <ActionButton tone="outline" onClick={control.cancel} disabled={control.busy}>
          {t("jobs.time.cancel")}
        </ActionButton>
      </div>
    </div>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="text-xs text-muted">{label}</span>
      <span className="text-sm font-medium break-words">{children}</span>
    </div>
  );
}

function JobDetail({ id }: { id: string }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const now = useNow();
  const { view } = useAgents();
  const query = useQuery(jobsQuery);
  const toggle = useToggleEnabled();
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState<"run" | "remove" | null>(null);
  const [actionError, setActionError] = useState("");

  const job = query.data?.find((j) => j.id === id && !j.archived);
  if (!job) {
    if (query.isError) {
      return <LoadFailed what={t("tasks.tab.jobs")} onRetry={() => void query.refetch()} />;
    }
    if (query.data === undefined) return <LoadingBlock />;
    return (
      <NotFoundBlock
        title={t("jobs.notFound")}
        hint={t("jobs.notFound.hint")}
        backLabel={t("jobs.back")}
        to="/tasks/jobs"
      />
    );
  }

  const agent = view(job.agent);
  const name = job.name || id;
  const isWindows = (job.windows?.length ?? 0) > 0;
  const kind = sourceKind(job.source);
  const refresh = () => client.invalidateQueries({ queryKey: jobsQuery.queryKey });
  const save = async (patch: { name?: string; body?: string; cron?: string[] }) => {
    try {
      await api.scheduler.setJob({ id, ...patch });
      await refresh();
    } catch (err) {
      throw new Error(reasonOf(err));
    }
  };

  const run = async () => {
    setBusy("run");
    setActionError("");
    try {
      await api.scheduler.runJob({ id });
      showToast(t("jobs.run.toast", { name: agent.name }));
      void refresh(); // 「最近一次」馬上換成這次
    } catch (err) {
      setActionError(t("jobs.run.failed", { reason: reasonOf(err) }));
    } finally {
      setBusy(null);
    }
  };

  const remove = async () => {
    setBusy("remove");
    setActionError("");
    try {
      const res = await api.scheduler.removeJob({ id });
      await refresh();
      setConfirming(false);
      if (res.archived) {
        showToast(t("jobs.removed", { name }));
        void navigate({ to: "/tasks/jobs", search: {}, replace: true });
      } else {
        showToast(t("jobs.disabled", { name }));
      }
    } catch (err) {
      setActionError(t("jobs.remove.failed", { reason: reasonOf(err) }));
    } finally {
      setBusy(null);
    }
  };

  const removable = kind === "user";
  return (
    <article data-testid="job-detail" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[13px] text-muted">
        <Pill tone={job.enabled ? "ok" : "muted"}>
          {job.enabled ? t("jobs.status.on") : t("jobs.status.off")}
        </Pill>
        <span>{job.agent ? agent.name : t("jobs.group.system")}</span>
      </div>
      <div className="flex flex-col gap-5 rounded-[18px] border border-border bg-surface p-4 shadow-[var(--shadow-card)]">
        <InlineEdit
          label={t("jobs.field.name")}
          value={job.name ?? ""}
          emptyText={t("jobs.field.name.empty")}
          savedText={t("jobs.saved")}
          requiredText={t("jobs.name.required")}
          onSave={(next) => save({ name: next })}
        />
        <InlineEdit
          label={t("jobs.field.body")}
          value={job.body ?? ""}
          multiline
          emptyText={t("jobs.field.body.empty")}
          savedText={t("jobs.saved")}
          requiredText={t("jobs.body.required")}
          onSave={(next) => save({ body: next })}
        />
        {isWindows ? (
          <Fact label={t("jobs.field.time")}>
            <span>{describeJobTime(job)}</span>
            <span className="mt-0.5 block text-xs font-normal text-muted">
              {t("jobs.windows.readonly")}
            </span>
          </Fact>
        ) : (
          <InlineEdit
            label={t("jobs.field.time")}
            value={(job.cron ?? []).join("\n")}
            emptyText={t("jobs.row.noTime")}
            savedText={t("jobs.saved")}
            requiredText={t("jobs.time.required")}
            display={<span>{describeCrons(job.cron ?? [])}</span>}
            renderEditor={(control) => <TimeEditorBox initial={job.cron ?? []} control={control} />}
            onSave={(next) =>
              save({
                cron: next
                  .split("\n")
                  .map((l) => l.trim())
                  .filter(Boolean),
              })
            }
          />
        )}
        <div className="flex items-center justify-between gap-3">
          <span className="text-[15px] font-semibold">{t("jobs.field.enabled")}</span>
          <Toggle
            checked={job.enabled}
            label={t("jobs.row.toggle", { name })}
            onChange={(enabled) => toggle.mutate({ id, enabled })}
          />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Fact label={t("jobs.field.next")}>
            {job.enabled && job.next_run ? timeLabel(job.next_run, now) : t("jobs.next.none")}
          </Fact>
          <Fact label={t("jobs.field.source")}>{t(`jobs.source.${kind}`)}</Fact>
        </div>
        <Fact label={t("jobs.field.last")}>
          {lastRunView(job, now) ? (
            <JobLastRun job={job} now={now} full />
          ) : (
            <span className="text-muted">{t("jobs.last.never")}</span>
          )}
        </Fact>
      </div>

      <div role="toolbar" aria-label={name} className="flex flex-wrap items-center gap-2">
        {job.agent ? (
          <ActionButton tone="secondary" busy={busy === "run"} onClick={() => void run()}>
            {t("jobs.run")}
          </ActionButton>
        ) : null}
        <ActionButton
          tone="outline"
          aria-expanded={confirming}
          disabled={busy !== null}
          onClick={() => setConfirming((v) => !v)}
        >
          {removable ? t("jobs.remove") : t("jobs.disable")}
        </ActionButton>
      </div>
      {confirming ? (
        <section
          aria-label={removable ? t("jobs.remove") : t("jobs.disable")}
          className="hg-rise flex flex-col gap-3 rounded-[14px] bg-attention-soft px-4 py-3"
        >
          <p className="text-[14px] leading-snug text-attention-fg">
            {removable
              ? t("jobs.remove.confirm.user", { name })
              : t("jobs.remove.confirm.other", { name })}
          </p>
          <div className="flex flex-wrap gap-2">
            <ActionButton tone="danger" busy={busy === "remove"} onClick={() => void remove()}>
              {removable ? t("jobs.remove.yes") : t("jobs.disable.yes")}
            </ActionButton>
            <ActionButton
              tone="outline"
              onClick={() => setConfirming(false)}
              disabled={busy !== null}
            >
              {t("jobs.remove.no")}
            </ActionButton>
          </div>
        </section>
      ) : null}
      {actionError ? (
        <p role="alert" className="text-[13px] text-danger">
          {actionError}
        </p>
      ) : null}
    </article>
  );
}
