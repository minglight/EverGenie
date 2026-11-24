import type { PersonaCard, PersonaTestRun } from "@evergenie/sdk-web";
import { api } from "@evergenie/sdk-web";
import { cn, Markdown, Spinner } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { Pill, type PillTone } from "../../components/Pill";
import { useAgents } from "../../data/agents";
import { t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";
import { reasonOf } from "../../lib/reason";
import { personaStyleQuery, personaTestsQuery } from "./queries";
import {
  alignRuns,
  draftCard,
  isUnsupportedReason,
  parseToneMarkdown,
  togglePick,
  toneButton,
} from "./toneLogic";

const selectClass =
  "min-h-11 w-full rounded-xl border border-border-strong bg-bg px-3 text-base text-fg outline-none focus-visible:outline-2 focus-visible:outline-focus";

const statusTone: Record<string, PillTone> = { running: "accent", done: "ok", failed: "danger" };

function whenOf(run: PersonaTestRun): string {
  const d = new Date(run.at ?? "");
  if (Number.isNaN(d.getTime())) return run.run ?? "";
  return d.toLocaleString("zh-TW", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

function styleLabel(level: string | undefined): string {
  if (!level) return "";
  return level === "custom"
    ? t("settings.personas.tone.style.custom")
    : t("settings.personas.tone.style.level", { level: level.toUpperCase() });
}

function Compare({
  left,
  right,
  onClose,
}: {
  left: PersonaTestRun;
  right: PersonaTestRun;
  onClose: () => void;
}) {
  const desktop = useIsDesktop();
  const rows = alignRuns(
    parseToneMarkdown(left.markdown ?? "").sections,
    parseToneMarkdown(right.markdown ?? "").sections,
  );
  const col = (label: string, text: string | undefined) => (
    <>
      <span className="mb-1.5 block text-[12px] font-semibold text-muted">{label}</span>
      {text === undefined ? (
        <p className="text-[13px] text-muted">{t("settings.personas.tone.compare.none")}</p>
      ) : (
        <Markdown>{text}</Markdown>
      )}
    </>
  );
  const leftLabel = t("settings.personas.tone.compare.left", { at: whenOf(left) });
  const rightLabel = t("settings.personas.tone.compare.right", { at: whenOf(right) });
  return (
    <div
      data-testid="tone-compare"
      className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4 shadow-[var(--shadow-card)]"
    >
      <div className="flex items-center justify-between gap-3">
        <h4 className="text-[14px] font-semibold">{t("settings.personas.tone.compare.title")}</h4>
        <ActionButton tone="quiet" size="pane" onClick={onClose}>
          {t("settings.personas.tone.compare.close")}
        </ActionButton>
      </div>
      {rows.map((row) => (
        <section
          key={row.id}
          data-testid="tone-compare-row"
          data-scenario={row.id}
          className="flex flex-col gap-2 border-t border-border pt-3"
        >
          <h5 className="text-[13px] font-semibold">{row.title || row.id}</h5>
          <blockquote className="m-0 border-l-2 border-border-strong pl-3 text-[13px] whitespace-pre-line text-muted">
            {row.quote}
          </blockquote>
          <div className={cn("grid gap-3", desktop && "grid-cols-2")}>
            <div className="min-w-0 rounded-xl bg-surface-2 p-3 text-[14px]" data-side="left">
              {col(leftLabel, row.left)}
            </div>
            <div className="min-w-0 rounded-xl bg-surface-2 p-3 text-[14px]" data-side="right">
              {col(rightLabel, row.right)}
            </div>
          </div>
        </section>
      ))}
    </div>
  );
}

function RunRow({
  run,
  open,
  picked,
  onToggleOpen,
  onTogglePick,
}: {
  run: PersonaTestRun;
  open: boolean;
  picked: boolean;
  onToggleOpen: () => void;
  onTogglePick: () => void;
}) {
  const { view } = useAgents();
  const status = run.status ?? "done";
  return (
    <li data-testid="tone-run-row" data-run={run.run} data-status={status} className="px-4 py-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <label className="flex min-h-11 items-center gap-2">
          <input
            type="checkbox"
            checked={picked}
            disabled={status === "running"}
            onChange={onTogglePick}
            data-testid="tone-pick"
            aria-label={t("settings.personas.tone.pick", { at: whenOf(run) })}
            className="size-5 accent-[var(--color-accent)]"
          />
          <span className="text-[14px] font-semibold">{whenOf(run)}</span>
        </label>
        <Pill tone={statusTone[status] ?? "muted"}>
          {t(`settings.personas.tone.status.${status}` as "settings.personas.tone.status.done")}
        </Pill>
        <span className="min-w-0 flex-1 text-[13px] break-words text-muted">
          {t("settings.personas.tone.row", {
            seat: view(run.seat).name,
            style: styleLabel(run.style),
            model: run.model ?? "",
          })}
        </span>
        {status !== "running" ? (
          <button
            type="button"
            aria-expanded={open}
            onClick={onToggleOpen}
            data-testid="tone-open"
            className="min-h-11 px-1 text-[13px] font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus"
          >
            {open ? t("settings.personas.tone.close") : t("settings.personas.tone.open")}
          </button>
        ) : null}
      </div>
      {run.reason ? (
        <p className="mt-1 text-[13px] text-danger">
          {t("settings.personas.tone.reason", { reason: run.reason })}
        </p>
      ) : null}
      {open ? (
        <div data-testid="tone-result" className="mt-2 flex flex-col gap-3 text-[14px]">
          {parseToneMarkdown(run.markdown ?? "").sections.map((s) => (
            <section key={s.id} className="flex flex-col gap-1.5">
              <h5 className="text-[13px] font-semibold">
                {s.id}｜{s.title}
              </h5>
              <blockquote className="m-0 border-l-2 border-border-strong pl-3 text-[13px] whitespace-pre-line text-muted">
                {s.quote}
              </blockquote>
              <div className="rounded-xl bg-surface-2 p-3">
                <Markdown>{s.reply}</Markdown>
              </div>
            </section>
          ))}
        </div>
      ) : null}
    </li>
  );
}

/** 角色庫詳情頁的「說話測試」：選座位與說話層、測口吻、看最近的結果、兩份並排比較（P6-5b、REQ-130）。 */
export function ToneTest({
  card,
  draft,
}: {
  card: PersonaCard;
  /** 編輯中還沒存的內容（沒有未存變更就是 null）；有就帶 card 全文去測。 */
  draft: { name: string; body: string } | null;
}) {
  const slug = card.slug ?? "";
  const client = useQueryClient();
  const { all: list, view } = useAgents();
  const runs = useQuery(personaTestsQuery(slug));
  const styles = useQuery(personaStyleQuery);
  const [seat, setSeat] = useState("");
  const [style, setStyle] = useState("");
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState("");
  const [unsupported, setUnsupported] = useState(false);
  const [watching, setWatching] = useState("");
  const [open, setOpen] = useState<string[]>([]);
  const [picks, setPicks] = useState<string[]>([]);
  const [comparing, setComparing] = useState(false);

  const data = runs.data ?? [];
  const state = toneButton({ runs: data, starting, unsupported });

  // 自己按的這一次跑完：自動展開結果。
  useEffect(() => {
    if (!watching) return;
    const run = data.find((r) => r.run === watching);
    if (run && run.status !== "running") {
      setOpen((o) => (o.includes(watching) ? o : [...o, watching]));
      setWatching("");
    }
  }, [data, watching]);

  const start = async () => {
    setStarting(true);
    setError("");
    try {
      const out = await api.personas.test({
        slug,
        seat: seat || undefined,
        style: style || undefined,
        card: draft ? draftCard(draft.name, draft.body) : undefined,
      });
      setWatching(out.run ?? "");
      await client.invalidateQueries({ queryKey: ["personas", "tests", slug] });
    } catch (err) {
      const reason = reasonOf(err);
      if (isUnsupportedReason(reason)) setUnsupported(true);
      else setError(t("settings.personas.tone.failed", { reason }));
    } finally {
      setStarting(false);
    }
  };

  const customOk = styles.data?.levels?.find((l) => l.level === "custom")?.exists === true;
  const picked = picks
    .map((id) => data.find((r) => r.run === id))
    .filter((r): r is PersonaTestRun => r !== undefined);
  const showCompare = comparing && picked.length === 2 && picked[0] && picked[1];

  return (
    <section
      aria-labelledby="tone-title"
      data-testid="persona-tone-test"
      className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4 shadow-[var(--shadow-card)]"
    >
      <div>
        <h3 id="tone-title" className="text-[15px] font-semibold">
          {t("settings.personas.tone.title")}
        </h3>
        <p className="text-[13px] leading-snug text-muted">{t("settings.personas.tone.hint")}</p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="flex flex-col gap-1 text-[13px] font-semibold">
          {t("settings.personas.tone.seat")}
          <select
            value={seat}
            onChange={(e) => setSeat(e.target.value)}
            data-testid="tone-seat"
            className={selectClass}
          >
            <option value="">{t("settings.personas.tone.seat.default")}</option>
            {list.map((a) => (
              <option key={a.id} value={a.id}>
                {view(a.id).name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1 text-[13px] font-semibold">
          {t("settings.personas.tone.style")}
          <select
            value={style}
            onChange={(e) => setStyle(e.target.value)}
            data-testid="tone-style"
            className={selectClass}
          >
            <option value="">{t("settings.personas.tone.style.default")}</option>
            {["a", "b", "c", "d"].map((l) => (
              <option key={l} value={l}>
                {styleLabel(l)}
              </option>
            ))}
            {customOk ? <option value="custom">{styleLabel("custom")}</option> : null}
          </select>
        </label>
      </div>
      {draft ? (
        <p
          role="note"
          className="text-[13px] leading-snug text-attention-fg"
          data-testid="tone-draft-note"
        >
          {t("settings.personas.tone.draft")}
        </p>
      ) : null}
      <div className="flex flex-wrap items-center gap-3">
        <ActionButton
          tone="primary"
          busy={state === "running"}
          disabled={state !== "ready"}
          onClick={() => void start()}
          data-testid="tone-run"
        >
          {t("settings.personas.tone.run")}
        </ActionButton>
        {state === "running" ? (
          <span
            role="status"
            data-testid="tone-status"
            className="flex items-center gap-2 text-[13px] text-muted"
          >
            <Spinner />
            {t("settings.personas.tone.running")}
          </span>
        ) : null}
      </div>
      {state === "unsupported" ? (
        <p role="note" className="text-[13px] text-danger" data-testid="tone-unsupported">
          {t("settings.personas.tone.unsupported")}
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="text-[13px] text-danger">
          {error}
        </p>
      ) : null}

      <div className="flex flex-col gap-2">
        <h4 className="text-[13px] font-semibold">{t("settings.personas.tone.recent")}</h4>
        {runs.isError && data.length === 0 ? (
          <p className="text-[13px] text-danger">{t("settings.personas.tone.loadFailed")}</p>
        ) : data.length === 0 ? (
          <p className="text-[13px] text-muted">{t("settings.personas.tone.empty")}</p>
        ) : (
          <ul className="m-0 flex list-none flex-col divide-y divide-border overflow-hidden rounded-xl border border-border p-0">
            {data.map((run) => (
              <RunRow
                key={run.run}
                run={run}
                open={open.includes(run.run ?? "")}
                picked={picks.includes(run.run ?? "")}
                onToggleOpen={() =>
                  setOpen((o) => {
                    const id = run.run ?? "";
                    return o.includes(id) ? o.filter((x) => x !== id) : [...o, id];
                  })
                }
                onTogglePick={() => {
                  setComparing(false);
                  setPicks((p) => togglePick(p, run.run ?? ""));
                }}
              />
            ))}
          </ul>
        )}
        {picked.length === 1 ? (
          <p className="text-[13px] text-muted">{t("settings.personas.tone.compare.hint")}</p>
        ) : null}
        {picked.length === 2 && !showCompare ? (
          <div>
            <ActionButton
              tone="outline"
              onClick={() => setComparing(true)}
              data-testid="tone-compare-open"
            >
              {t("settings.personas.tone.compare.title")}
            </ActionButton>
          </div>
        ) : null}
      </div>
      {showCompare && picked[0] && picked[1] ? (
        <Compare left={picked[0]} right={picked[1]} onClose={() => setComparing(false)} />
      ) : null}
    </section>
  );
}
