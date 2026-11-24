import { Popover } from "@base-ui/react/popover";
import { Ellipsis } from "lucide-react";
import { type ReactNode, useState, useSyncExternalStore } from "react";
import { cn } from "../lib/cn";
import { Sheet } from "./Sheet";

export interface MoreMenuItem {
  key: string;
  label: string;
  /** 選了要做的事（選單先關起來再做，例：打開另一個抽屜）。跟 href 二選一。 */
  onSelect?: () => void;
  /** 站內網址：畫成一般連結（套件頁面沒有路由器，用 <a>）。 */
  href?: string;
  /** 文字前面的小圖示（要 aria-hidden）。 */
  icon?: ReactNode;
  testId?: string;
}

export interface MoreMenuProps {
  /** 觸發鈕給讀屏的名字，寫明裡面有什麼（例「更多：新增角色、匯入、匯出」）。 */
  label: string;
  items: MoreMenuItem[];
  /** 手機底部抽屜與桌機浮窗的標題（例「角色」）。要給：label 寫的是清單，不像標題；沒給才拿 label 頂著。 */
  title?: string;
  /** 觸發鈕的樣式（位置與顏色由用的地方決定；大小固定 44px，FEN-7）。 */
  className?: string;
  testId?: string;
  /** 抽屜右上角關閉鈕的名字（預設「關閉」）。 */
  closeLabel?: string;
}

const DESKTOP = "(min-width: 768px)";

function useDesktop(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      if (typeof window.matchMedia !== "function") return () => {};
      const mq = window.matchMedia(DESKTOP);
      mq.addEventListener("change", onChange);
      return () => mq.removeEventListener("change", onChange);
    },
    () => typeof window.matchMedia === "function" && window.matchMedia(DESKTOP).matches,
    () => false,
  );
}

const itemClass =
  "flex min-h-11 w-full items-center gap-2.5 rounded-[10px] px-3 text-left text-[15px] text-fg no-underline hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus";

const triggerClass =
  "flex size-11 shrink-0 items-center justify-center rounded-full focus-visible:outline-2 focus-visible:outline-focus";

function Items({ items, onClose }: { items: MoreMenuItem[]; onClose: () => void }) {
  return (
    <ul className="m-0 flex list-none flex-col p-0">
      {items.map((it) => (
        <li key={it.key}>
          {it.href ? (
            <a href={it.href} data-testid={it.testId} className={itemClass} onClick={onClose}>
              {it.icon}
              {it.label}
            </a>
          ) : (
            <button
              type="button"
              data-testid={it.testId}
              className={itemClass}
              onClick={() => {
                onClose();
                it.onSelect?.();
              }}
            >
              {it.icon}
              {it.label}
            </button>
          )}
        </li>
      ))}
    </ul>
  );
}

// 「⋯」選單（DEV-156 U-15、U-19）：不常用的動作收在一顆 44px 的鈕裡，裡面用字寫清楚。
// 桌機（≥ 768）是鈕下方的小浮窗，手機是底部抽屜（跟待辦的「⋯」一樣，FEN-24）；Esc、點外面關閉，關閉後焦點回到鈕。
export function MoreMenu({ label, items, title, className, testId, closeLabel }: MoreMenuProps) {
  const desktop = useDesktop();
  const [open, setOpen] = useState(false);
  const close = () => setOpen(false);
  const icon = <Ellipsis className="size-5" aria-hidden="true" />;
  if (items.length === 0) return null;
  if (!desktop) {
    return (
      <>
        <button
          type="button"
          aria-label={label}
          aria-haspopup="dialog"
          aria-expanded={open}
          data-testid={testId}
          onClick={() => setOpen(true)}
          className={cn(triggerClass, className)}
        >
          {icon}
        </button>
        <Sheet open={open} onOpenChange={setOpen} title={title ?? label} closeLabel={closeLabel}>
          <div className="px-3 pb-2">
            <Items items={items} onClose={close} />
          </div>
        </Sheet>
      </>
    );
  }
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger
        aria-label={label}
        data-testid={testId}
        className={cn(triggerClass, className)}
      >
        {icon}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner align="end" sideOffset={4} className="z-50">
          <Popover.Popup
            aria-label={title ?? label}
            className="hg-pop w-[240px] rounded-2xl border border-border bg-surface p-1.5 shadow-[var(--shadow-pop)] outline-none"
          >
            <Items items={items} onClose={close} />
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}
