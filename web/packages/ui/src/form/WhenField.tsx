import { CalendarDays } from "lucide-react";
import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { cn } from "../lib/cn";
import { Field } from "./Field";

export interface WhenShortcut {
  label: string;
  date: () => Date;
}

export interface WhenFieldProps {
  label: string;
  value: Date | null;
  onChange: (value: Date | null) => void;
  /** 把輸入框的字讀成時間；讀不懂回 null（解析器在 apps/board/src/lib/when.ts，用瀏覽器的時區）。 */
  parse: (text: string) => Date | null;
  /** 解析結果的白話，例如「→ 10/25（六）09:00」。 */
  describe: (date: Date) => string;
  /** 把時間寫回輸入框的字；parse 要讀得回來。 */
  toText: (date: Date) => string;
  shortcuts: WhenShortcut[];
  /** 輸入框裡的範例字。 */
  placeholder: string;
  /** 留空時的說明（「留空會用預設時間」）。 */
  emptyHint: string;
  /** 讀不懂時寫在欄位下的字。 */
  invalidText: string;
  calendarLabel: string;
  /** 伺服器回的錯誤（field 對得到這個欄位時）。 */
  error?: string;
  autoFocus?: boolean;
  /** ⌘/Ctrl+Enter。 */
  onSubmit?: () => void;
  className?: string;
}

const pad = (n: number) => String(n).padStart(2, "0");

/** <input type="datetime-local"> 的值（本地時間）。 */
function localInputValue(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// 時間欄位（FEN-15）：一個輸入框收自然語言，下方即時顯示解析結果；旁邊三個快捷 chip＋「用日曆選」；
// 留空＝交給伺服器套預設時間（REQ-62），所以留空時寫一行小字說明。
export function WhenField({
  label,
  value,
  onChange,
  parse,
  describe,
  toText,
  shortcuts,
  placeholder,
  emptyHint,
  invalidText,
  calendarLabel,
  error,
  autoFocus,
  onSubmit,
  className,
}: WhenFieldProps) {
  const [text, setText] = useState(() => (value ? toText(value) : ""));
  const [touched, setTouched] = useState(false);
  const [calendar, setCalendar] = useState(false);
  // 最後一次是我們自己回報給外面的時間：外面換成別的值時（例如點了別處的 chip），輸入框跟著更新。
  const emitted = useRef<number | null>(value ? value.getTime() : null);

  // biome-ignore lint/correctness/useExhaustiveDependencies: toText 只是格式化，不是觸發條件
  useEffect(() => {
    const at = value ? value.getTime() : null;
    if (at !== emitted.current) {
      emitted.current = at;
      setText(value ? toText(value) : "");
    }
  }, [value]);

  const emit = (next: Date | null) => {
    emitted.current = next ? next.getTime() : null;
    onChange(next);
  };

  const change = (raw: string) => {
    setText(raw);
    emit(raw.trim() === "" ? null : parse(raw));
  };

  const pick = (d: Date) => {
    setText(toText(d));
    emit(d);
  };

  const parsed = text.trim() === "" ? null : parse(text);
  const invalid = touched && text.trim() !== "" && parsed === null;

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      onSubmit?.();
    }
  };

  return (
    <Field
      label={label}
      error={error || (invalid ? invalidText : undefined)}
      hint={parsed ? undefined : emptyHint}
      className={className}
    >
      {(control) => (
        <>
          <input
            {...control}
            type="text"
            value={text}
            onChange={(e) => change(e.target.value)}
            onBlur={() => setTouched(true)}
            onKeyDown={onKeyDown}
            placeholder={placeholder}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            enterKeyHint="done"
            // biome-ignore lint/a11y/noAutofocus: 開啟編輯面板時聚焦第一個欄位（FEN-24）
            autoFocus={autoFocus}
            className="h-11 w-full rounded-xl border border-border-strong bg-surface px-3 text-base text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus"
          />
          {parsed ? (
            <p role="status" className="-mt-0.5 text-[13px] font-medium text-accent">
              {describe(parsed)}
            </p>
          ) : null}
          <div className="flex flex-wrap items-center gap-1.5">
            {shortcuts.map((s) => (
              <button
                key={s.label}
                type="button"
                onClick={() => pick(s.date())}
                className="min-h-11 rounded-full bg-accent-soft px-3.5 text-[13px] font-medium text-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
              >
                {s.label}
              </button>
            ))}
            <button
              type="button"
              aria-expanded={calendar}
              onClick={() => setCalendar((v) => !v)}
              className={cn(
                "flex min-h-11 items-center gap-1.5 rounded-full border border-border px-3.5 text-[13px] font-medium text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
                calendar && "bg-surface-2",
              )}
            >
              <CalendarDays className="size-4" aria-hidden="true" />
              {calendarLabel}
            </button>
          </div>
          {calendar ? (
            <input
              type="datetime-local"
              aria-label={calendarLabel}
              value={parsed ? localInputValue(parsed) : ""}
              onChange={(e) => {
                const d = e.target.value ? new Date(e.target.value) : null;
                if (d && !Number.isNaN(d.getTime())) pick(d);
              }}
              className="h-11 w-full rounded-xl border border-border-strong bg-surface px-3 text-base text-fg outline-none focus-visible:outline-2 focus-visible:outline-focus"
            />
          ) : null}
        </>
      )}
    </Field>
  );
}
