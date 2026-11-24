import { Card, cn } from "@evergenie/ui";
import { ChevronDown } from "lucide-react";
import { type ReactNode, useState } from "react";

const dots = {
  attention: "bg-attention",
  accent: "bg-accent",
  busy: "bg-busy",
  ok: "bg-ok",
  danger: "bg-danger",
  muted: "bg-muted",
} as const;

// 清單的一組（DEV-107）：標題帶數字，點標題收合／展開；球在你手上的組用 attention 色邊框。
export function CollapsibleGroup({
  title,
  count,
  hint,
  tone = "muted",
  emphasis,
  testId,
  children,
}: {
  title: string;
  count: number;
  hint?: string;
  tone?: keyof typeof dots;
  emphasis?: boolean;
  testId?: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(true);
  return (
    <section data-testid={testId} className="flex flex-col">
      <Card className={cn("overflow-hidden", emphasis && "border-attention")}>
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
          className="flex min-h-11 w-full items-center gap-2 px-4 py-2 text-left focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus"
        >
          <span className={cn("size-2.5 shrink-0 rounded-full", dots[tone])} aria-hidden="true" />
          <span className="text-[14px] font-bold">{title}</span>
          <span className="text-[13px] text-muted tabular-nums">{count}</span>
          {hint ? <span className="ml-auto truncate text-xs text-muted">{hint}</span> : null}
          <ChevronDown
            className={cn(
              "size-4 shrink-0 text-muted transition-transform",
              hint ? "" : "ml-auto",
              !open && "-rotate-90",
            )}
            aria-hidden="true"
          />
        </button>
        {open ? <div className="border-t border-border">{children}</div> : null}
      </Card>
    </section>
  );
}
