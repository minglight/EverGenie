import {
  type KeyboardEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import { Button } from "../components/Button";
import { cn } from "../lib/cn";
import { Field } from "./Field";

export interface InlineEditControl {
  /** 存這個值；失敗時丟例外，訊息會寫在欄位下。 */
  commit: (next: string) => Promise<void>;
  cancel: () => void;
  busy: boolean;
}

export interface InlineEditProps {
  /** 可見的標籤（小字、灰）。 */
  label: string;
  value: string;
  /** 存起來；丟出的 Error.message 就是顯示在欄位下的原因（伺服器回的白話）。 */
  onSave: (next: string) => Promise<void> | void;
  multiline?: boolean;
  /** 編輯時輸入框的範例字。 */
  placeholder?: string;
  /** 沒有值時，閱讀版面顯示的字。 */
  emptyText: string;
  /** 存完欄位旁邊顯示 1.5 秒的字（「已儲存」）。 */
  savedText: string;
  /** 必填：空白存不了，錯誤寫在欄位下。 */
  requiredText?: string;
  /** 閱讀版面自訂顯示（例如 Markdown）；點它一樣進入編輯。 */
  display?: ReactNode;
  /**
   * 閱讀版面裡有連結、圖這類可以點的東西時給（例如 Markdown 描述）：有值時內容不包在按鈕裡
   * （按鈕裡不能再放連結或按鈕，點圖也會誤進編輯），改在標籤旁放一顆寫這個字的編輯鈕；沒有值時照舊點空白字編輯。
   */
  editLabel?: string;
  /** 自訂編輯器（分段按鈕、時間欄位…）；有的話不用文字輸入框。 */
  renderEditor?: (control: InlineEditControl) => ReactNode;
  /**
   * 多行才用：給了就在輸入框下放「存」「取消」兩顆鈕，而且失焦不存（寫到一半切走不會存半句；手機沒有 Esc，
   * 要有路取消）。內容重要、存了會有後果的欄位用（例如任務單的需求與驗收條件）。
   */
  actions?: { save: string; cancel: string };
  className?: string;
}

const SAVED_MS = 1500;

// 就地編輯（FEN-13）：點文字變輸入框，Enter 或失焦存、Esc 還原；多行用 ⌘/Ctrl+Enter 存。
// 存完旁邊顯示「已儲存」1.5 秒；伺服器拒絕就留在編輯狀態，原因寫在欄位下。
export function InlineEdit({
  label,
  value,
  onSave,
  multiline,
  placeholder,
  emptyText,
  savedText,
  requiredText,
  display,
  editLabel,
  renderEditor,
  actions,
  className,
}: InlineEditProps) {
  const labelId = useId();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  // Esc 之後輸入框會失焦：那次失焦不算「存」。
  const skipBlur = useRef(false);
  // 開始編輯時的內容：沒改過就不存，就算這段時間別人把內容改了，也不會把舊的蓋回去。
  const base = useRef(value);
  const savedTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => () => clearTimeout(savedTimer.current), []);

  const flashSaved = useCallback(() => {
    setSaved(true);
    clearTimeout(savedTimer.current);
    savedTimer.current = setTimeout(() => setSaved(false), SAVED_MS);
  }, []);

  const cancel = useCallback(() => {
    skipBlur.current = true;
    setEditing(false);
    setError("");
    setDraft(value);
  }, [value]);

  const commit = useCallback(
    async (raw: string) => {
      const next = raw.trim();
      if (next === base.current.trim() || next === value.trim()) {
        setEditing(false);
        setError("");
        return;
      }
      if (next === "" && requiredText) {
        setError(requiredText);
        return;
      }
      setBusy(true);
      setError("");
      try {
        await onSave(next);
        setEditing(false);
        flashSaved();
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setBusy(false);
      }
    },
    [onSave, requiredText, value, flashSaved],
  );

  const begin = () => {
    skipBlur.current = false;
    base.current = value;
    setDraft(value);
    setError("");
    setEditing(true);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      cancel();
      return;
    }
    if (e.key !== "Enter") return;
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    if (multiline && !(e.metaKey || e.ctrlKey)) return;
    e.preventDefault();
    void commit(draft);
  };

  const onBlur = () => {
    if (skipBlur.current) {
      skipBlur.current = false;
      return;
    }
    if (actions && multiline) return;
    // 上一次存失敗、內容沒再改：失焦不再重送，讓使用者看完原因。
    if (error) return;
    void commit(draft);
  };

  if (editing) {
    if (renderEditor) {
      return (
        <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
          <span className="text-xs text-muted">{label}</span>
          {renderEditor({ commit, cancel, busy })}
          {error ? (
            <p role="alert" className="text-xs text-danger">
              {error}
            </p>
          ) : null}
        </div>
      );
    }
    const common = {
      value: draft,
      onChange: (e: { target: { value: string } }) => {
        setDraft(e.target.value);
        if (error) setError("");
      },
      onKeyDown,
      onBlur,
      readOnly: busy,
      placeholder,
      autoComplete: "off",
      className:
        "w-full rounded-[10px] border border-accent bg-bg px-3 py-2 text-base text-fg outline-none focus-visible:outline-2 focus-visible:outline-focus",
    };
    return (
      <Field label={label} error={error} className={className}>
        {(control) =>
          multiline ? (
            <>
              <textarea
                {...control}
                {...common}
                // 依原本的行數開大一點（任務單的需求常常很長），最少 3 行、最多 12 行。
                rows={Math.min(12, Math.max(3, value.split("\n").length + 1))}
                // biome-ignore lint/a11y/noAutofocus: 點了欄位才進入編輯，直接輸入
                autoFocus
              />
              {actions ? (
                <div className="mt-2 flex gap-2">
                  <Button type="button" disabled={busy} onClick={() => void commit(draft)}>
                    {actions.save}
                  </Button>
                  <Button type="button" variant="ghost" disabled={busy} onClick={cancel}>
                    {actions.cancel}
                  </Button>
                </div>
              ) : null}
            </>
          ) : (
            // biome-ignore lint/a11y/noAutofocus: 點了欄位才進入編輯，直接輸入
            <input {...control} {...common} type="text" enterKeyHint="done" autoFocus />
          )
        }
      </Field>
    );
  }

  const savedBadge = saved ? (
    <span role="status" className="shrink-0 text-xs font-medium text-ok">
      {savedText}
    </span>
  ) : null;

  if (editLabel && value !== "") {
    return (
      <div className={cn("flex min-w-0 flex-col gap-0.5", className)}>
        <div className="flex min-w-0 items-center gap-2">
          <span id={labelId} className="text-xs text-muted">
            {label}
          </span>
          <button
            type="button"
            aria-describedby={labelId}
            onClick={begin}
            className="relative -my-1 rounded-md px-1.5 py-1 text-xs font-semibold text-accent after:absolute after:-inset-2.5 hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus"
          >
            {editLabel}
          </button>
          {savedBadge}
        </div>
        <div className="min-w-0 text-[14px]">
          {display ?? <span className="whitespace-pre-wrap">{value}</span>}
        </div>
      </div>
    );
  }

  return (
    <div className={cn("flex min-w-0 flex-col gap-0.5", className)}>
      <span id={labelId} className="text-xs text-muted">
        {label}
      </span>
      <div className="flex min-w-0 items-center gap-2">
        <button
          type="button"
          aria-describedby={labelId}
          onClick={begin}
          className="-mx-2 -my-1.5 min-h-11 min-w-0 rounded-lg px-2 py-1.5 text-left text-[14px] font-medium hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus"
        >
          {display ?? (
            <span className={cn("whitespace-pre-wrap", value === "" && "text-muted")}>
              {value === "" ? emptyText : value}
            </span>
          )}
        </button>
        {savedBadge}
      </div>
    </div>
  );
}
