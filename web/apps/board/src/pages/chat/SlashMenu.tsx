import { cn } from "@evergenie/ui";
import type { SlashItem } from "./slash";

export const SLASH_MENU_ID = "hg-slash-menu";

export function slashOptionId(i: number): string {
  return `${SLASH_MENU_ID}-${i}`;
}

// 輸入框上方的「/」指令選單（WEB-83）：每列是指令（或第二層的選項）＋白話說明；點一下或方向鍵＋Enter 選。
// 每列至少 44px 高（UXC-5）；按下時不搶走輸入框的焦點（鍵盤不收起來）。
export function SlashMenu({
  items,
  active,
  label,
  onPick,
  onHover,
}: {
  items: readonly SlashItem[];
  active: number;
  label: string;
  onPick: (item: SlashItem) => void;
  onHover: (i: number) => void;
}) {
  return (
    <div
      id={SLASH_MENU_ID}
      role="listbox"
      aria-label={label}
      data-testid="slash-menu"
      className="hg-rise absolute right-0 bottom-full left-0 z-20 mb-2 flex max-h-[min(50vh,360px)] flex-col overflow-y-auto rounded-2xl border border-border-strong bg-surface p-1 shadow-lg"
    >
      {items.map((item, i) => (
        <div
          key={`${item.spec.name}-${item.arg ?? ""}`}
          id={slashOptionId(i)}
          role="option"
          tabIndex={-1}
          aria-selected={i === active}
          onMouseDown={(e) => e.preventDefault()}
          onMouseEnter={() => onHover(i)}
          onClick={() => onPick(item)}
          onKeyDown={() => {}}
          className={cn(
            "flex min-h-11 cursor-pointer items-center gap-3 rounded-xl px-3 py-2 text-left",
            i === active ? "bg-surface-2" : "",
          )}
        >
          <span className="shrink-0 font-mono text-[14px] font-semibold text-fg">{item.label}</span>
          <span className="min-w-0 text-[13px] leading-snug text-muted">{item.hint}</span>
        </div>
      ))}
    </div>
  );
}
