import { Spinner } from "@evergenie/ui";
import { Check } from "lucide-react";
import { useRef } from "react";
import { t } from "../../i18n/zh-TW";
import type { SaveState } from "./useAutosavedPrefs";

// 頁首右邊的儲存狀態（FEN-22）：儲存中／已儲存（2 秒後淡出）／沒存成功（可重試的才有「重試」）。
export function SaveStatus({ state, onRetry }: { state: SaveState; onRetry: () => void }) {
  // 淡出時畫面上還要有字，所以記住最後一個不是 idle 的狀態。
  const last = useRef<SaveState>({ kind: "saved" });
  if (state.kind !== "idle") last.current = state;
  const shown = last.current;
  const visible = state.kind !== "idle";
  return (
    <div
      role="status"
      aria-live="polite"
      data-save-state={state.kind}
      className="flex h-6 items-center justify-end gap-1.5 text-[13px] transition-opacity duration-300"
      style={{ opacity: visible ? 1 : 0 }}
    >
      {visible ? <Line state={shown} onRetry={onRetry} /> : null}
    </div>
  );
}

function Line({ state, onRetry }: { state: SaveState; onRetry: () => void }) {
  if (state.kind === "saving") {
    return (
      <>
        <Spinner className="size-3.5" />
        <span className="text-muted">{t("settings.save.saving")}</span>
      </>
    );
  }
  if (state.kind === "failed") {
    return (
      <>
        <span className="font-medium text-danger">
          {t(state.retriable ? "settings.save.failed" : "settings.save.failedReverted")}
        </span>
        {state.retriable ? (
          <button
            type="button"
            onClick={onRetry}
            className="-my-2 min-h-11 px-2 font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus"
          >
            {t("settings.save.retry")}
          </button>
        ) : null}
      </>
    );
  }
  return (
    <>
      <Check className="size-4 text-ok" strokeWidth={2.2} aria-hidden="true" />
      <span className="font-medium text-ok">{t("settings.save.saved")}</span>
    </>
  );
}
