import { Loader2 } from "lucide-react";
import { cn } from "../lib/cn";

export interface SpinnerProps {
  label?: string;
  className?: string;
}

export function Spinner({ label, className }: SpinnerProps) {
  return (
    <Loader2
      role={label ? "status" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      className={cn("size-6 animate-spin text-muted motion-reduce:animate-none", className)}
    />
  );
}
