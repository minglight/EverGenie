import { Dialog } from "@base-ui/react/dialog";
import { X } from "lucide-react";
import { type PointerEvent, type ReactNode, useRef, useState } from "react";

export interface SheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** 右上角關閉鈕給讀屏的名字（預設「關閉」）。 */
  closeLabel?: string;
  children: ReactNode;
}

const CLOSE_DRAG_PX = 80;

// 手機的底部抽屜：從底部滑出、圓角 20、有把手；右上角有關閉鈕，點背景、Esc 或把手往下拖也會關（FEN-24）。
export function Sheet({ open, onOpenChange, title, closeLabel = "關閉", children }: SheetProps) {
  const [dy, setDy] = useState(0);
  const startY = useRef<number | null>(null);

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    startY.current = e.clientY;
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (startY.current === null) return;
    setDy(Math.max(0, e.clientY - startY.current));
  };
  const onPointerEnd = () => {
    if (startY.current === null) return;
    const shouldClose = dy > CLOSE_DRAG_PX;
    startY.current = null;
    setDy(0);
    if (shouldClose) onOpenChange(false);
  };

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-scrim transition-opacity duration-200 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <Dialog.Popup
          aria-modal="true"
          className="fixed inset-x-0 bottom-0 z-50 rounded-t-[20px] bg-surface pb-[max(env(safe-area-inset-bottom),16px)] shadow-[var(--shadow-sheet)] outline-none transition-transform duration-200 data-[ending-style]:translate-y-full data-[starting-style]:translate-y-full"
          style={dy > 0 ? { transform: `translateY(${dy}px)`, transition: "none" } : undefined}
        >
          <div
            className="flex h-8 touch-none cursor-grab items-center justify-center"
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerEnd}
            onPointerCancel={onPointerEnd}
          >
            <span className="h-1 w-10 rounded-full bg-border-strong" aria-hidden="true" />
          </div>
          <Dialog.Title className="min-h-11 py-2 pr-14 pl-5 text-[17px] font-semibold">
            {title}
          </Dialog.Title>
          {children}
          {/* 右上角的關閉鈕：不必知道可以往下拉或點外面（DEV-156 U-27）。放在內容後面（畫面上在右上角）：
              打開時焦點照舊落在內容的第一個欄位，不會先落在「關閉」（FEN-24）。 */}
          <Dialog.Close
            aria-label={closeLabel}
            data-testid="sheet-close"
            className="absolute top-8 right-2 flex size-11 items-center justify-center rounded-full text-muted hover:bg-hover hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus"
          >
            <X className="size-5" aria-hidden="true" />
          </Dialog.Close>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
