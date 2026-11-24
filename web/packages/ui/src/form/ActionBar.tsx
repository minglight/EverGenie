import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export interface ActionBarProps {
  children: ReactNode;
  /**
   * sticky：貼在捲動區的底部（手機詳細頁，在底部列的上方）。
   * inline：跟著內容走（桌機詳細頁）。
   * pane：固定在欄位底部（桌機右欄，上邊框）。
   */
  placement?: "sticky" | "inline" | "pane";
  className?: string;
  label?: string;
}

// 動作列：主要動作一個、實心；其他動作次要（FEN-14）。
export function ActionBar({ children, placement = "inline", className, label }: ActionBarProps) {
  return (
    <div
      role="toolbar"
      aria-label={label}
      className={cn(
        "flex flex-wrap items-center gap-2",
        placement === "sticky" &&
          "sticky bottom-0 z-10 -mx-4 border-t border-border bg-bg px-4 py-3",
        placement === "pane" && "shrink-0 border-t border-border bg-surface px-9 py-3.5",
        placement === "inline" && "pt-2",
        className,
      )}
    >
      {children}
    </div>
  );
}
