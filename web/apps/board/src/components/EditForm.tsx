import { ApiError } from "@evergenie/sdk-web";
import { Pencil } from "lucide-react";
import { type ReactNode, useState } from "react";
import { t } from "../i18n/zh-TW";
import { isEmptyPatch } from "../lib/editPatch";
import { ActionButton } from "./ActionButton";
import { FormDialog } from "./FormDialog";

/** 表單裡的文字輸入框（跟新增表單同一個樣子，高至少 44px）。 */
export const editInputClass =
  "min-h-11 w-full rounded-xl border border-border-strong bg-bg px-3 text-base text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";

/** 詳情頁右上角的「編輯」鈕（WEB-161）：手機上好按，高 44px、有字有圖示。 */
export function EditButton({ onClick }: { onClick: () => void }) {
  return (
    <ActionButton tone="outline" size="page" onClick={onClick} className="gap-1.5 px-4">
      <Pencil className="size-4" aria-hidden="true" />
      {t("edit.open")}
    </ActionButton>
  );
}

function reasonOf(err: unknown): string {
  return err instanceof ApiError || err instanceof Error ? err.message : String(err);
}

export interface EditDialogProps<P extends object> {
  open: boolean;
  title: string;
  onClose: () => void;
  /** 跟打開時比出來要送的欄位；空的就直接關，不呼叫 API。 */
  patch: () => P;
  /** 送出前就擋得下的錯：有的話不送（欄位下面自己畫）。 */
  invalid: boolean;
  /** 一次呼叫 update；失敗丟例外，原因寫在表單裡、表單不關。 */
  onSave: (patch: P) => Promise<void>;
  /** 按過一次儲存：欄位才開始顯示必填錯誤。 */
  onTried: () => void;
  children: ReactNode;
}

/**
 * 整張表單的外框（WEB-161）：手機底部抽屜、桌機置中對話框（FormDialog），欄位區可捲，
 * 底下「取消」「儲存」不會被推出畫面。儲存只送改過的欄位、一次呼叫；失敗顯示伺服器的原因、表單不關。
 */
export function EditDialog<P extends object>({
  open,
  title,
  onClose,
  patch,
  invalid,
  onSave,
  onTried,
  children,
}: EditDialogProps<P>) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const close = () => {
    if (busy) return;
    setError("");
    onClose();
  };
  const save = async () => {
    if (busy) return;
    onTried();
    if (invalid) return;
    const p = patch();
    if (isEmptyPatch(p)) {
      close();
      return;
    }
    setBusy(true);
    setError("");
    try {
      await onSave(p);
      setBusy(false);
      onClose();
    } catch (err) {
      setBusy(false);
      setError(t("edit.failed", { reason: reasonOf(err) }));
    }
  };

  return (
    <FormDialog open={open} title={title} onClose={close}>
      <div className="flex max-h-[calc(var(--hg-visible-h)*0.62)] flex-col gap-4 overflow-y-auto px-5 pt-1 pb-3">
        {children}
        {error ? (
          <p role="alert" className="text-[13px] text-danger">
            {error}
          </p>
        ) : null}
      </div>
      <div className="flex flex-wrap justify-end gap-2 border-t border-border px-5 pt-3 pb-1">
        <ActionButton tone="outline" onClick={close} disabled={busy}>
          {t("edit.cancel")}
        </ActionButton>
        <ActionButton tone="primary" busy={busy} onClick={() => void save()}>
          {t("edit.save")}
        </ActionButton>
      </div>
    </FormDialog>
  );
}

/** 有標題、底下可放說明的一組控制項（分段按鈕、頭像這類不是單一輸入框的）。 */
export function EditGroup({
  label,
  hint,
  error,
  children,
}: {
  label: string;
  hint?: string;
  error?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-[13px] font-semibold">{label}</span>
      {children}
      {error ? (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      ) : hint ? (
        <p className="text-xs text-muted">{hint}</p>
      ) : null}
    </div>
  );
}
