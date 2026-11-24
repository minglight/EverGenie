import { Check, CircleHelp } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export interface EmptyStateProps {
  title: string;
  /** 下一步可以做什麼（UXC-7）。 */
  hint?: ReactNode;
  /** dashed：虛線框（預設）；plain：沒有框（清單欄裡用）。 */
  variant?: "dashed" | "plain";
  /**
   * ok（預設）：沒有東西要處理，綠色打勾；missing：找不到、讀不到這類不是好消息的，灰色問號，
   * 不用綠勾（DEV-156 U-28）。
   */
  tone?: "ok" | "missing";
  className?: string;
}

export function EmptyState({
  title,
  hint,
  variant = "dashed",
  tone = "ok",
  className,
}: EmptyStateProps) {
  return (
    <div
      className={cn(
        "flex flex-col items-center gap-1.5 text-center",
        variant === "dashed" &&
          "rounded-[18px] border border-dashed border-border-strong px-4 py-[22px]",
        variant === "plain" && "mx-4 my-10",
        className,
      )}
    >
      {tone === "missing" ? (
        <CircleHelp
          data-testid="empty-icon-missing"
          className="size-7 text-muted"
          strokeWidth={1.8}
          aria-hidden="true"
        />
      ) : (
        <Check className="size-7 text-ok" strokeWidth={1.8} aria-hidden="true" />
      )}
      <div className="text-[15px] font-semibold">{title}</div>
      {hint ? <div className="text-[13px] text-muted">{hint}</div> : null}
    </div>
  );
}
