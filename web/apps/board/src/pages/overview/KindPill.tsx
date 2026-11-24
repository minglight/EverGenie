import { cn } from "@evergenie/ui";
import type { WaitingKind } from "../../data/waiting";
import { t } from "../../i18n/zh-TW";

const keys = { approve: "kind.approve", review: "kind.review", ask: "kind.ask" } as const;

// 分類小膠囊：三種都是要你動手的事，一律橘色（UXC-12；DEV-156 U-10：以前任務單用主色系，跟任務單頁的橘色對不起來）。
export function KindPill({ kind, className }: { kind: WaitingKind; className?: string }) {
  return (
    <span
      className={cn(
        "shrink-0 rounded-full bg-attention-soft px-2 py-0.5 text-xs font-semibold text-attention-fg",
        className,
      )}
    >
      {t(keys[kind])}
    </span>
  );
}
