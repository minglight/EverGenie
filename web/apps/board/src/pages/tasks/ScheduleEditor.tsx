import { cn, Field, Segmented } from "@evergenie/ui";
import { useState } from "react";
import { t } from "../../i18n/zh-TW";
import {
  buildCron,
  dayName,
  describeCrons,
  modeOf,
  parseSimple,
  type TimeMode,
  WEEK_ORDER,
} from "../../lib/cron";

export interface ScheduleEditorProps {
  /** 目前的 cron（可以多組）；空陣列＝還沒填。 */
  value: readonly string[];
  /** 每次改動都回傳新的 cron；填不完整（例如每週沒選天）回空陣列。 */
  onChange: (specs: string[]) => void;
  /** 伺服器拒絕時的原因，寫在欄位下（FEN-19）。 */
  error?: string;
}

const DEFAULT_TIME = "09:00";
const inputClass =
  "h-11 rounded-xl border border-border-strong bg-bg px-3 text-base text-fg outline-none focus-visible:outline-2 focus-visible:outline-focus";

function initial(value: readonly string[]) {
  const first = value[0] ? parseSimple(value[0]) : null;
  return {
    mode: modeOf(value),
    time: first?.times[0] ?? DEFAULT_TIME,
    days: first?.days ?? [],
    text: value.join("\n"),
  };
}

// 排程時間編輯器（排程頁的詳情與新增共用）：分段按鈕「每天／每週幾／進階」。
// 每天＝一個時間；每週＝星期 chip 多選＋時間；進階＝直接打 cron（可多組），下方即時寫白話。
export function ScheduleEditor({ value, onChange, error }: ScheduleEditorProps) {
  const [state, setState] = useState(() => initial(value));
  const { mode, time, days, text } = state;

  const emit = (next: typeof state) => {
    setState(next);
    if (next.mode === "advanced") {
      onChange(
        next.text
          .split("\n")
          .map((l) => l.trim())
          .filter(Boolean),
      );
    } else if (next.time === "") {
      onChange([]);
    } else if (next.mode === "daily") {
      onChange([buildCron([], next.time)]);
    } else {
      onChange(next.days.length === 0 ? [] : [buildCron(next.days, next.time)]);
    }
  };

  const switchMode = (next: TimeMode) => {
    if (next === mode) return;
    // 換分頁時帶著目前的時間；從進階換回去就用預設時間（進階的內容不一定看得懂）。
    const base = { ...state, mode: next };
    if (next === "advanced") {
      const specs = mode === "daily" ? [buildCron([], time)] : buildWeekly(days, time);
      emit({ ...base, text: specs.join("\n") });
    } else {
      emit(base);
    }
  };

  const specs =
    mode === "advanced"
      ? text.split("\n").filter((l) => l.trim())
      : time === ""
        ? []
        : mode === "daily"
          ? [buildCron([], time)]
          : buildWeekly(days, time);
  const preview = describeCrons(specs);

  return (
    <div className="flex flex-col gap-3">
      <Segmented
        ariaLabel={t("jobs.time.mode")}
        value={mode}
        onChange={switchMode}
        options={[
          { value: "daily", label: t("jobs.time.daily") },
          { value: "weekly", label: t("jobs.time.weekly") },
          { value: "advanced", label: t("jobs.time.advanced") },
        ]}
      />
      {mode !== "advanced" ? (
        <>
          {mode === "weekly" ? (
            <fieldset className="m-0 flex min-w-0 flex-col gap-1.5 border-0 p-0">
              <legend className="mb-1.5 text-[13px] font-semibold">{t("jobs.time.days")}</legend>
              <div className="flex flex-wrap gap-1.5">
                {WEEK_ORDER.map((d) => {
                  const on = days.includes(d);
                  return (
                    <button
                      key={d}
                      type="button"
                      aria-pressed={on}
                      onClick={() =>
                        emit({
                          ...state,
                          days: on ? days.filter((x) => x !== d) : [...days, d],
                        })
                      }
                      className={cn(
                        "h-11 min-w-11 rounded-full border px-3 text-[14px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
                        on
                          ? "border-accent bg-accent-soft font-semibold text-accent"
                          : "border-border bg-surface text-fg",
                      )}
                    >
                      {dayName(d)}
                    </button>
                  );
                })}
              </div>
              {days.length === 0 ? (
                <p className="text-xs text-muted">{t("jobs.time.days.required")}</p>
              ) : null}
            </fieldset>
          ) : null}
          <Field label={t("jobs.time.at")} error={error}>
            {(control) => (
              <input
                {...control}
                type="time"
                value={time}
                onChange={(e) => emit({ ...state, time: e.target.value })}
                className={cn(inputClass, "w-40")}
              />
            )}
          </Field>
        </>
      ) : (
        <Field
          label={t("jobs.time.cron")}
          hint={t("jobs.time.cron.hint")}
          error={error}
          className="gap-1.5"
        >
          {(control) => (
            <textarea
              {...control}
              value={text}
              rows={Math.max(2, Math.min(5, text.split("\n").length))}
              onChange={(e) => emit({ ...state, text: e.target.value })}
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              autoComplete="off"
              className={cn(inputClass, "h-auto w-full resize-y py-2 font-mono text-[14px]")}
            />
          )}
        </Field>
      )}
      {preview ? (
        <p role="status" data-testid="schedule-preview" className="text-[13px] text-muted">
          {t("jobs.time.preview", { text: preview })}
        </p>
      ) : null}
    </div>
  );
}

function buildWeekly(days: readonly number[], time: string): string[] {
  return days.length === 0 ? [] : [buildCron(days, time)];
}
