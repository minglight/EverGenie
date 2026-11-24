import type { HTMLAttributes } from "react";
import { cn } from "../lib/cn";

export function Kbd({ className, ...props }: HTMLAttributes<HTMLElement>) {
  return (
    <kbd
      className={cn(
        "rounded-[5px] border border-border-strong bg-surface px-[5px] py-px font-mono text-[11px] text-muted",
        className,
      )}
      {...props}
    />
  );
}
