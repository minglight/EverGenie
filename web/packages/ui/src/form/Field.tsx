import { type ReactNode, useId } from "react";
import { cn } from "../lib/cn";

export interface FieldControlProps {
  id: string;
  "aria-describedby"?: string;
  "aria-invalid"?: true;
}

export interface FieldProps {
  /** 可見的標籤（FEN-18）；placeholder 只放範例。 */
  label: string;
  /** 欄位下方一行小字的說明。 */
  hint?: string;
  /** 錯誤寫在欄位正下方（FEN-19）；有錯誤時取代說明。 */
  error?: string;
  className?: string;
  /** 只給讀屏軟體看的標籤（版面上已經有別的文字當標籤時）。 */
  labelHidden?: boolean;
  children: (control: FieldControlProps) => ReactNode;
}

// 一個欄位：可見標籤＋控制項＋下方一行說明或錯誤（FEN-18、FEN-19）。
export function Field({ label, hint, error, className, labelHidden, children }: FieldProps) {
  const id = useId();
  const noteId = `${id}-note`;
  const note = error || hint;
  const control: FieldControlProps = {
    id,
    "aria-describedby": note ? noteId : undefined,
    "aria-invalid": error ? true : undefined,
  };
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      <label htmlFor={id} className={labelHidden ? "sr-only" : "text-[13px] font-semibold"}>
        {label}
      </label>
      {children(control)}
      {note ? (
        <p
          id={noteId}
          role={error ? "alert" : undefined}
          className={cn("text-xs leading-snug", error ? "text-danger" : "text-muted")}
        >
          {note}
        </p>
      ) : null}
    </div>
  );
}
