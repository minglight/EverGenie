import { ActionButton } from "./ActionButton";
import { Modal } from "./Modal";

export interface ConfirmDialogProps {
  open: boolean;
  title: string;
  body: string;
  confirmLabel: string;
  cancelLabel: string;
  /** 確認鈕的顏色：停用、解除安裝這類動作用 danger。 */
  tone?: "primary" | "danger";
  onConfirm: () => void;
  onCancel: () => void;
}

// 「確定要做嗎」：只有按確認鈕才執行；取消、點背景、Esc 都不做任何事。
export function ConfirmDialog({
  open,
  title,
  body,
  confirmLabel,
  cancelLabel,
  tone = "danger",
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  return (
    <Modal
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
      title={title}
      description={body}
    >
      <div className="mt-1 flex flex-wrap justify-end gap-2">
        <ActionButton tone="outline" onClick={onCancel}>
          {cancelLabel}
        </ActionButton>
        <ActionButton tone={tone} onClick={onConfirm}>
          {confirmLabel}
        </ActionButton>
      </div>
    </Modal>
  );
}
