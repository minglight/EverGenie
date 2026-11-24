import { Field, Segmented } from "@evergenie/ui";
import { useEffect, useId, useState } from "react";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { type TextKey, t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";
import { AgentOverrides } from "./AgentOverrides";
import { ModeSwitch } from "./ModeSwitch";
import { useMode } from "./mode";
import { PresetCards } from "./NotifySimple";
import {
  type CategoryId,
  categoryIds,
  hoursOf,
  isClock,
  joinQuiet,
  modeOf,
  modesFor,
  parseHours,
  parseTaskNumber,
  quietParts,
  type TaskField,
  tasksOf,
  withHours,
  withMode,
  withQuiet,
  withTask,
} from "./prefs";
import { SettingGroup, SettingRow } from "./SettingRow";
import type { AutosavedPrefs } from "./useAutosavedPrefs";

const inputClass =
  "h-11 rounded-[10px] border border-border bg-bg px-3 text-base text-fg focus-visible:outline-2 focus-visible:outline-focus aria-[invalid=true]:border-danger";

function HoursField({
  category,
  value,
  onChange,
}: {
  category: CategoryId;
  value: number;
  onChange: (hours: number) => void;
}) {
  const [text, setText] = useState(String(value));
  const [error, setError] = useState("");
  // 還原或重新載入讓數字變了：輸入框跟著換。
  useEffect(() => {
    setText(String(value));
    setError("");
  }, [value]);
  return (
    <Field
      label={t("settings.notify.hours")}
      hint={t("settings.notify.hours.hint")}
      error={error}
      className="mt-3 rounded-[12px] bg-surface-2 p-3"
    >
      {(control) => (
        <input
          {...control}
          data-testid={`hours-${category}`}
          type="text"
          inputMode="numeric"
          pattern="[0-9]*"
          enterKeyHint="done"
          autoComplete="off"
          value={text}
          onChange={(e) => {
            const next = e.target.value;
            setText(next);
            const hours = parseHours(next);
            if (hours !== null) {
              setError("");
              if (hours !== value) onChange(hours);
            }
          }}
          onBlur={() => {
            // 失焦才檢查格式（FEN-19）；寫錯的不會送出，所以也不用還原。
            if (parseHours(text) === null) setError(t("settings.notify.hours.invalid"));
          }}
          className={`${inputClass} w-24`}
        />
      )}
    </Field>
  );
}

function CategoryRow({
  category,
  auto,
}: {
  category: CategoryId;
  auto: AutosavedPrefs & { prefs: NonNullable<AutosavedPrefs["prefs"]> };
}) {
  const desktop = useIsDesktop();
  const name = t(`settings.notify.cat.${category}` as TextKey);
  const mode = modeOf(auto.prefs, category);
  return (
    <SettingRow
      title={name}
      hint={t(`settings.notify.cat.${category}.hint` as TextKey)}
      error={auto.errors[`mode:${category}`] ?? auto.errors[`hours:${category}`]}
      extra={
        mode === "digest" ? (
          <HoursField
            category={category}
            value={hoursOf(auto.prefs, category)}
            onChange={(hours) =>
              auto.edit(`hours:${category}`, (p) => withHours(p, category, hours))
            }
          />
        ) : null
      }
    >
      <Segmented
        options={modesFor(category).map((m) => ({
          value: m,
          label: t(`settings.notify.mode.${m}` as TextKey),
        }))}
        value={mode}
        onChange={(next) => auto.edit(`mode:${category}`, (p) => withMode(p, category, next))}
        ariaLabel={t("settings.notify.mode", { name })}
        compact={desktop}
        className="md:min-w-[280px]"
      />
    </SettingRow>
  );
}

function QuietRow({
  quiet,
  error,
  onChange,
}: {
  quiet: string;
  error?: string;
  onChange: (quiet: string) => void;
}) {
  const parts = quietParts(quiet);
  const [from, setFrom] = useState(parts.from);
  const [to, setTo] = useState(parts.to);
  const [invalid, setInvalid] = useState(false);
  useEffect(() => {
    const p = quietParts(quiet);
    setFrom(p.from);
    setTo(p.to);
    setInvalid(false);
  }, [quiet]);

  const apply = (nextFrom: string, nextTo: string) => {
    setFrom(nextFrom);
    setTo(nextTo);
    const joined = joinQuiet(nextFrom, nextTo);
    if (joined === null) return;
    setInvalid(false);
    if (joined !== quiet) onChange(joined);
  };
  const partial = joinQuiet(from, to) === null;

  return (
    <SettingRow
      title={t("settings.notify.quiet.title")}
      hint={t("settings.notify.quiet.hint")}
      error={error ?? (invalid && partial ? t("settings.notify.quiet.invalid") : undefined)}
    >
      <div className="flex flex-wrap items-center gap-2">
        <label className="flex items-center gap-1.5 text-[13px] text-muted">
          {t("settings.notify.quiet.from")}
          <input
            type="time"
            value={from}
            onChange={(e) => apply(e.target.value, to)}
            onBlur={() => setInvalid(true)}
            className={inputClass}
          />
        </label>
        <label className="flex items-center gap-1.5 text-[13px] text-muted">
          {t("settings.notify.quiet.to")}
          <input
            type="time"
            value={to}
            onChange={(e) => apply(from, e.target.value)}
            onBlur={() => setInvalid(true)}
            className={inputClass}
          />
        </label>
        {from !== "" || to !== "" ? (
          <button
            type="button"
            onClick={() => apply("", "")}
            className="min-h-11 rounded-lg px-2 text-[13px] font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus"
          >
            {t("settings.notify.quiet.clear")}
          </button>
        ) : null}
      </div>
    </SettingRow>
  );
}

type TaskNumberField = Exclude<TaskField, "nudge_at">;

// 任務單跟進的一個數字：失焦後檢查範圍（FEN-19），範圍內的才會送出（FEN-22 自動儲存）。
function TaskNumberRow({
  field,
  value,
  error,
  unit,
  onChange,
}: {
  field: TaskNumberField;
  value: number;
  error?: string;
  unit: "days" | "times";
  onChange: (n: number) => void;
}) {
  const titleId = useId();
  const [text, setText] = useState(String(value));
  const [invalid, setInvalid] = useState(false);
  // 還原或重新載入讓數字變了：輸入框跟著換。
  useEffect(() => {
    setText(String(value));
    setInvalid(false);
  }, [value]);
  return (
    <SettingRow
      titleId={titleId}
      title={t(`settings.notify.tasks.${field}` as TextKey)}
      hint={t(`settings.notify.tasks.${field}.hint` as TextKey)}
      error={error ?? (invalid ? t(`settings.notify.tasks.range.${unit}` as TextKey) : undefined)}
    >
      <div className="flex items-center gap-2">
        <input
          aria-labelledby={titleId}
          data-testid={`tasks-${field}`}
          type="text"
          inputMode="numeric"
          pattern="[0-9]*"
          enterKeyHint="done"
          autoComplete="off"
          aria-invalid={invalid || error ? true : undefined}
          value={text}
          onChange={(e) => {
            const next = e.target.value;
            setText(next);
            const n = parseTaskNumber(field, next);
            if (n !== null) {
              setInvalid(false);
              if (n !== value) onChange(n);
            }
          }}
          onBlur={() => setInvalid(parseTaskNumber(field, text) === null)}
          className={`${inputClass} w-24`}
        />
        <span className="text-[13px] text-muted">
          {t(`settings.notify.tasks.${unit}` as TextKey)}
        </span>
      </div>
    </SettingRow>
  );
}

function TaskTimeRow({
  value,
  error,
  onChange,
}: {
  value: string;
  error?: string;
  onChange: (at: string) => void;
}) {
  const titleId = useId();
  const [text, setText] = useState(value);
  const [invalid, setInvalid] = useState(false);
  useEffect(() => {
    setText(value);
    setInvalid(false);
  }, [value]);
  return (
    <SettingRow
      titleId={titleId}
      title={t("settings.notify.tasks.nudge_at")}
      hint={t("settings.notify.tasks.nudge_at.hint")}
      error={error ?? (invalid ? t("settings.notify.tasks.nudge_at.invalid") : undefined)}
    >
      <input
        aria-labelledby={titleId}
        data-testid="tasks-nudge_at"
        type="time"
        aria-invalid={invalid || error ? true : undefined}
        value={text}
        onChange={(e) => {
          const next = e.target.value;
          setText(next);
          if (isClock(next)) {
            setInvalid(false);
            if (next !== value) onChange(next);
          }
        }}
        onBlur={() => setInvalid(!isClock(text))}
        className={inputClass}
      />
    </SettingRow>
  );
}

// 設定 → 通知。簡單模式：三張預設組卡片加安靜時段；完整模式：每個分類一列（即時／合併摘要／只放看板／關閉）、
// 單一秘書另設、安靜時段、任務單跟進。最上面的「簡單／完整」切換兩邊都有；全部自動儲存。
export function NotifySettings({ auto }: { auto: AutosavedPrefs }) {
  const { mode, loaded } = useMode();
  if (auto.loading || !loaded) return <LoadingBlock />;
  if (!auto.prefs) {
    return auto.loadError ? (
      <LoadFailed what={t("settings.loadFailed")} onRetry={auto.reload} />
    ) : (
      <LoadingBlock />
    );
  }
  const ready = { ...auto, prefs: auto.prefs };
  const tasks = tasksOf(auto.prefs);
  const quiet = (
    <SettingGroup title={t("settings.notify.quiet")}>
      <QuietRow
        quiet={auto.prefs.quiet_hours ?? ""}
        error={auto.errors.quiet}
        onChange={(q) => auto.edit("quiet", (p) => withQuiet(p, q))}
      />
    </SettingGroup>
  );
  if (mode !== "full") {
    return (
      <div className="flex flex-col gap-5" data-mode="simple">
        <ModeSwitch />
        <PresetCards auto={ready} />
        {quiet}
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-5" data-mode="full">
      <ModeSwitch />
      <SettingGroup title={t("settings.notify.categories")}>
        {categoryIds.map((category) => (
          <CategoryRow key={category} category={category} auto={ready} />
        ))}
      </SettingGroup>
      <AgentOverrides auto={ready} />
      {quiet}
      <SettingGroup title={t("settings.notify.tasks")}>
        <TaskTimeRow
          value={tasks.nudge_at}
          error={auto.errors["tasks:nudge_at"]}
          onChange={(at) => auto.edit("tasks:nudge_at", (p) => withTask(p, "nudge_at", at))}
        />
        <TaskNumberRow
          field="stale_days"
          unit="days"
          value={tasks.stale_days}
          error={auto.errors["tasks:stale_days"]}
          onChange={(n) => auto.edit("tasks:stale_days", (p) => withTask(p, "stale_days", n))}
        />
        <TaskNumberRow
          field="nudge_every_days"
          unit="days"
          value={tasks.nudge_every_days}
          error={auto.errors["tasks:nudge_every_days"]}
          onChange={(n) =>
            auto.edit("tasks:nudge_every_days", (p) => withTask(p, "nudge_every_days", n))
          }
        />
        <TaskNumberRow
          field="nudge_max"
          unit="times"
          value={tasks.nudge_max}
          error={auto.errors["tasks:nudge_max"]}
          onChange={(n) => auto.edit("tasks:nudge_max", (p) => withTask(p, "nudge_max", n))}
        />
      </SettingGroup>
    </div>
  );
}
