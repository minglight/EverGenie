import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export type BadgeTone = "attention" | "accent" | "muted";

const tones: Record<BadgeTone, string> = {
  attention: "bg-attention text-on-attention",
  accent: "bg-accent text-accent-fg",
  muted: "bg-surface-2 text-muted",
};

export interface BadgeProps {
  tone?: BadgeTone;
  children: ReactNode;
  className?: string;
}

// 數字徽章（選單、底部列）：11px 粗體，最小 18px 高的圓角。
export function Badge({ tone = "accent", children, className }: BadgeProps) {
  return (
    <span
      className={cn(
        "box-border inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-full px-[5px] text-[11px] font-bold",
        tones[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}
