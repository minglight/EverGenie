import { cn } from "@evergenie/ui";
import type { ReactNode } from "react";

export type PillTone = "attention" | "accent" | "busy" | "ok" | "danger" | "muted";

// 狀態膠囊的 tone 從 lib/statusTone 的 tonePill 拿（WEB-70）：attention 只給「等你處理」。
const tones: Record<PillTone, string> = {
  attention: "bg-attention-soft text-attention-fg",
  accent: "bg-accent-soft text-accent",
  busy: "bg-busy-soft text-busy",
  ok: "bg-ok-soft text-ok",
  danger: "bg-danger-soft text-danger",
  muted: "bg-surface-2 text-muted",
};

// 分類、狀態的小膠囊（白話字）；顏色只用 token。
export function Pill({
  tone,
  children,
  className,
}: {
  tone: PillTone;
  children: ReactNode;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center rounded-full px-2 py-0.5 text-xs font-semibold",
        // 不認得的 tone（別台回了看板不認得的狀態）畫灰。
        tones[tone] ?? tones.muted,
        className,
      )}
    >
      {children}
    </span>
  );
}
