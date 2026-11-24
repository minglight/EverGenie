import type { ReactNode } from "react";

export const fieldInputClass =
  "min-h-11 w-full min-w-0 rounded-[10px] border border-border bg-bg px-3 text-base text-fg focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-60";

/** 表單的一欄：標題＋輸入框＋一行提示（機器、帳號兩個新增表單共用）。 */
export function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    // biome-ignore lint/a11y/noLabelWithoutControl: 輸入框由呼叫端放進 children，label 包著它
    <label className="flex flex-col gap-1">
      <span className="text-[15px] font-semibold">{label}</span>
      {children}
      {hint ? <span className="text-[13px] leading-snug text-muted">{hint}</span> : null}
    </label>
  );
}
