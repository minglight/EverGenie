import { Sheet } from "@evergenie/ui";
import type { ReactNode } from "react";
import { useIsDesktop } from "../lib/media";
import { Modal } from "./Modal";

/** 手機用底部抽屜、桌機用置中對話框（FEN-24）；新增表單共用。 */
export function FormDialog({
  open,
  title,
  body,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  /** 一兩句說明；不寫就不顯示。 */
  body?: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const desktop = useIsDesktop();
  if (!desktop) {
    return (
      <Sheet open={open} onOpenChange={(next) => !next && onClose()} title={title}>
        {body ? <p className="px-5 pb-3 text-[13px] leading-snug text-muted">{body}</p> : null}
        {children}
      </Sheet>
    );
  }
  return (
    <Modal open={open} onOpenChange={(next) => !next && onClose()} title={title} description={body}>
      <div className="-mx-5 -mb-4">{children}</div>
    </Modal>
  );
}
