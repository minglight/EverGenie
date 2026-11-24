import { cn } from "../lib/cn";

export interface StatusDotProps {
  tone?: "attention" | "accent" | "muted";
  /** 直徑（px）。 */
  size?: number;
  pulse?: boolean;
  className?: string;
}

const tones = {
  attention: "bg-attention",
  accent: "bg-accent",
  muted: "bg-border-strong",
};

// 小圓點：黃點（等你處理）、進行中（光圈動畫，prefers-reduced-motion 時關掉）。
export function StatusDot({ tone = "attention", size = 9, pulse, className }: StatusDotProps) {
  return (
    <span
      aria-hidden="true"
      data-tone={tone}
      className={cn(
        "inline-block shrink-0 rounded-full",
        tones[tone],
        pulse && "hg-pulse",
        className,
      )}
      style={{ width: size, height: size }}
    />
  );
}
