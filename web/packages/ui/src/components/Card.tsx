import type { HTMLAttributes } from "react";
import { cn } from "../lib/cn";

// 白卡：1px 邊框、圓角 18；亮色有淡陰影，深色沒有（陰影在 token 裡換）。
export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "rounded-[18px] border border-border bg-surface shadow-[var(--shadow-card)]",
        className,
      )}
      {...props}
    />
  );
}
