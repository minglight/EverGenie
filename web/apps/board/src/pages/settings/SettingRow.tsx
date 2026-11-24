import { cn } from "@evergenie/ui";
import type { ReactNode } from "react";

export interface SettingRowProps {
  title: string;
  hint?: string;
  /** 右邊的控制項（手機上換到下一行）。 */
  children?: ReactNode;
  /** 這一列伺服器拒絕的原因（FEN-19、FEN-22）：寫在這一列下面。 */
  error?: string;
  /** 列內再多一塊（例如選了「合併摘要」才出現的間隔）。 */
  extra?: ReactNode;
  titleId?: string;
  /** 右邊控制項比較寬（例如 5 個選項的分段按鈕）：桌機上放寬到 420px，不擠成兩行。 */
  wide?: boolean;
}

// 設定的一列：左邊標題（15px、600）加一行說明（13px、muted），右邊控制項；列與列用 border 分隔。
export function SettingRow({
  title,
  hint,
  children,
  error,
  extra,
  titleId,
  wide,
}: SettingRowProps) {
  return (
    <div className="px-4 py-3.5">
      <div className="flex flex-col gap-2.5 md:flex-row md:items-center md:justify-between md:gap-6">
        <div className="min-w-0 md:flex-1">
          <p id={titleId} className="text-[15px] leading-snug font-semibold">
            {title}
          </p>
          {hint ? <p className="mt-0.5 text-[13px] leading-snug text-muted">{hint}</p> : null}
        </div>
        {children ? (
          <div
            className={cn("min-w-0 md:shrink-0", wide ? "md:max-w-[420px]" : "md:max-w-[300px]")}
          >
            {children}
          </div>
        ) : null}
      </div>
      {extra}
      {error ? (
        <p role="alert" className="mt-2 text-xs leading-snug text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

/** 一組列的白卡：列與列之間 1px 分隔。 */
export function SettingGroup({
  title,
  children,
  className,
}: {
  title?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("flex flex-col gap-2", className)}>
      {title ? <h2 className="px-1 text-[13px] font-semibold text-muted">{title}</h2> : null}
      <div className="divide-y divide-border overflow-hidden rounded-[18px] border border-border bg-surface shadow-[var(--shadow-card)]">
        {children}
      </div>
    </section>
  );
}
