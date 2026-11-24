import { X } from "lucide-react";
import { type KeyboardEvent, type ReactNode, useState } from "react";
import { cn } from "../lib/cn";
import { Field } from "./Field";

export interface ChipsInputProps {
  label: string;
  hint?: string;
  error?: string;
  /** 已經有的 chip。 */
  values: string[];
  /** Enter 或逗號：把輸入框的字變成一顆 chip。可以回傳 Promise；丟例外時錯誤顯示在欄位下。 */
  onAdd: (value: string) => Promise<void> | void;
  /** 有的話 chip 上有刪除鈕，輸入框空白時退格刪最後一顆；沒有就只能加不能減。 */
  onRemove?: (value: string) => Promise<void> | void;
  placeholder?: string;
  /** 把 chip 的字畫成別的東西（例如連結）。 */
  renderChip?: (value: string) => ReactNode;
  removeLabel?: (value: string) => string;
  className?: string;
}

// 清單資料用 chip 輸入（FEN-17）：Enter 或逗號成一顆，退格刪最後一顆（可刪時）。
export function ChipsInput({
  label,
  hint,
  error,
  values,
  onAdd,
  onRemove,
  placeholder,
  renderChip,
  removeLabel,
  className,
}: ChipsInputProps) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const run = async (job: () => Promise<void> | void) => {
    setBusy(true);
    setFailure("");
    try {
      await job();
      return true;
    } catch (err) {
      setFailure(err instanceof Error ? err.message : String(err));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const add = async (raw: string) => {
    const value = raw.trim();
    if (value === "" || busy) return;
    if (values.includes(value)) {
      setText("");
      return;
    }
    if (await run(() => onAdd(value))) setText("");
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    if (e.key === "Enter" || e.key === "," || e.key === "，") {
      e.preventDefault();
      void add(text);
    } else if (e.key === "Backspace" && text === "" && onRemove && values.length > 0) {
      const last = values[values.length - 1];
      if (last !== undefined) void run(() => onRemove(last));
    }
  };

  return (
    <Field label={label} hint={hint} error={error || failure} className={className}>
      {(control) => (
        <div
          className={cn(
            "flex min-h-11 flex-wrap items-center gap-1.5 rounded-xl border border-border-strong bg-surface px-2.5 py-1.5 focus-within:outline-2 focus-within:outline-focus",
          )}
        >
          {values.map((v) => (
            <span
              key={v}
              className="inline-flex max-w-full items-center gap-1 rounded-full bg-surface-2 px-2.5 py-1 text-[13px] text-fg"
            >
              <span className="min-w-0 truncate">{renderChip ? renderChip(v) : v}</span>
              {onRemove ? (
                <button
                  type="button"
                  aria-label={removeLabel ? removeLabel(v) : v}
                  onClick={() => void run(() => onRemove(v))}
                  className="-mr-1 flex size-6 items-center justify-center rounded-full text-muted hover:text-fg"
                >
                  <X className="size-3.5" aria-hidden="true" />
                </button>
              ) : null}
            </span>
          ))}
          <input
            {...control}
            type="text"
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={onKeyDown}
            placeholder={placeholder}
            readOnly={busy}
            autoComplete="off"
            enterKeyHint="done"
            className="-my-1.5 h-11 min-w-24 flex-1 border-0 bg-transparent text-base text-fg outline-none placeholder:text-muted"
          />
        </div>
      )}
    </Field>
  );
}
