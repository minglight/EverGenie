import { api } from "@evergenie/sdk-web";
import { useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { Modal } from "../../components/Modal";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";

export interface InstallGoDialogProps {
  open: boolean;
  /** 有幾個套件在等 Go。 */
  count: number;
  onClose: () => void;
  /** 安裝請求回來之後（成功或沒有套件在等）呼叫，讓外面重抓套件清單。 */
  onInstalled?: () => void;
}

type Outcome =
  | { kind: "ok"; hadGo: boolean; rebuilt: string[] }
  | { kind: "error"; message: string };

// 補裝 Go 的確認對話框（D-27、REQ-171）：只有按「安裝」才送 package.install_go{confirmed:true}；
// 按「先不要」、點背景、Esc 都不送任何請求。送出中按鈕停用並轉圈，結束後列出重新編譯的套件或白話錯誤。
export function InstallGoDialog({ open, count, onClose, onInstalled }: InstallGoDialogProps) {
  const [busy, setBusy] = useState(false);
  const [outcome, setOutcome] = useState<Outcome | null>(null);

  const close = () => {
    if (busy) return;
    setOutcome(null);
    onClose();
  };

  const install = async () => {
    setBusy(true);
    try {
      const res = await api.package.installGo({ confirmed: true });
      setOutcome({
        kind: "ok",
        hadGo: !res.command,
        rebuilt: (res.packages ?? []).map((p) => p.name || p.id || ""),
      });
      onInstalled?.();
    } catch (err) {
      setOutcome({
        kind: "error",
        message: t("settings.goDialog.failed", { reason: reasonOf(err) }),
      });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      open={open}
      onOpenChange={(next) => {
        if (!next) close();
      }}
      title={t("settings.goDialog.title")}
      description={outcome ? undefined : t("settings.goDialog.body", { n: count })}
    >
      {outcome ? (
        <div className="flex flex-col gap-3">
          {outcome.kind === "error" ? (
            <p role="alert" className="text-[14px] leading-relaxed text-danger">
              {outcome.message}
            </p>
          ) : (
            <div role="status" className="flex flex-col gap-1 text-[14px] leading-relaxed">
              <p className="font-semibold">
                {t(outcome.hadGo ? "settings.goDialog.already" : "settings.goDialog.installed")}
              </p>
              <p className="text-muted">
                {outcome.rebuilt.length > 0
                  ? t("settings.goDialog.rebuilt", { names: outcome.rebuilt.join("、") })
                  : t("settings.goDialog.none")}
              </p>
            </div>
          )}
          <div className="flex justify-end">
            <ActionButton tone="primary" onClick={close}>
              {t("settings.goDialog.done")}
            </ActionButton>
          </div>
        </div>
      ) : (
        <div className="mt-1 flex flex-wrap justify-end gap-2">
          <ActionButton tone="outline" disabled={busy} onClick={close}>
            {t("settings.goDialog.no")}
          </ActionButton>
          <ActionButton tone="primary" busy={busy} onClick={() => void install()}>
            {busy ? t("settings.goDialog.busy") : t("settings.goDialog.yes")}
          </ActionButton>
        </div>
      )}
    </Modal>
  );
}
