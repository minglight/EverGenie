import { Dialog } from "@base-ui/react/dialog";
import type { ReactNode } from "react";

export interface ModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** 一兩句說明；螢幕閱讀器會把它當成對話框的描述。 */
  description?: string;
  children?: ReactNode;
}

// 置中的小對話框（確認用）：點背景或 Esc 關閉；手機寬度左右各留 16px。
export function Modal({ open, onOpenChange, title, description, children }: ModalProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-scrim transition-opacity duration-200 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <Dialog.Popup
          aria-modal="true"
          className="fixed top-1/2 left-1/2 z-50 flex max-h-[85vh] w-[min(calc(100vw-32px),440px)] -translate-x-1/2 -translate-y-1/2 flex-col gap-3 overflow-y-auto rounded-[20px] bg-surface p-5 shadow-[var(--shadow-sheet)] outline-none"
        >
          <Dialog.Title className="text-[17px] font-semibold">{title}</Dialog.Title>
          {description ? (
            <Dialog.Description className="text-[14px] leading-relaxed text-muted">
              {description}
            </Dialog.Description>
          ) : null}
          {children}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
